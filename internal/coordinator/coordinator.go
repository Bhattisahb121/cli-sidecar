package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Bhattisahb121/cli-sidecar/internal/container"
)

// Coordinator routes requests to the appropriate shim containers.
type Coordinator struct {
	containerMgr *container.Manager
	httpClient   *http.Client
}

// PromptRequest is the request for executing a prompt.
type PromptRequest struct {
	Tool       string `json:"tool"`
	Account    string `json:"account"`
	Prompt     string `json:"prompt"`
	OutputMode string `json:"output_mode,omitempty"`
}

// PromptResponse is the response from a prompt execution.
type PromptResponse struct {
	Container string `json:"container"`
	Tool      string `json:"tool"`
	Account   string `json:"account"`
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
}

// SpawnRequest is the request for creating a new container.
type SpawnRequest struct {
	Tool       string            `json:"tool"`
	Account    string            `json:"account"`
	Image      string            `json:"image,omitempty"`
	CLICommand string            `json:"cli_command"`
	CLIArgs    string            `json:"cli_args,omitempty"`
	OutputMode string            `json:"output_mode,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

// New creates a new Coordinator.
func New(containerMgr *container.Manager) *Coordinator {
	return &Coordinator{
		containerMgr: containerMgr,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Route sends a prompt to the appropriate container's shim.
func (c *Coordinator) Route(ctx context.Context, req PromptRequest) (*PromptResponse, error) {
	// Find an available container for this tool+account
	info := c.containerMgr.FindAvailable(req.Tool, req.Account)
	if info == nil {
		return nil, fmt.Errorf("no running container for tool=%s account=%s; spawn one first via POST /api/containers", req.Tool, req.Account)
	}

	// Forward the prompt to the shim
	shimReq := map[string]string{
		"prompt": req.Prompt,
	}
	if req.OutputMode != "" {
		shimReq["output_mode"] = req.OutputMode
	}

	body, err := json.Marshal(shimReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, info.ShimAddr+"/prompt", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to reach shim at %s: %w", info.ShimAddr, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read shim response: %w", err)
	}

	var shimResp struct {
		Output string `json:"output"`
		Error  string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &shimResp); err != nil {
		return nil, fmt.Errorf("invalid shim response: %w", err)
	}

	return &PromptResponse{
		Container: info.Name,
		Tool:      req.Tool,
		Account:   req.Account,
		Output:    shimResp.Output,
		Error:     shimResp.Error,
	}, nil
}

// Spawn creates a new container for a tool+account combination.
func (c *Coordinator) Spawn(req SpawnRequest) (*container.Info, error) {
	cfg := container.Config{
		Tool:       req.Tool,
		Account:    req.Account,
		Image:      req.Image,
		CLICommand: req.CLICommand,
		CLIArgs:    req.CLIArgs,
		OutputMode: req.OutputMode,
		Env:        req.Env,
		Network:    "", // Manager handles network
	}

	info, err := c.containerMgr.Create(cfg)
	if err != nil {
		return nil, err
	}

	// Wait for shim to be ready
	if err := c.waitForShim(info.ShimAddr, 30*time.Second); err != nil {
		log.Printf("Warning: shim not ready in container %s: %s", info.Name, err)
	}

	return info, nil
}

func (c *Coordinator) waitForShim(shimAddr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := c.httpClient.Get(shimAddr + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("shim not ready after %s", timeout)
}

// HealthCheck checks if a container's shim is healthy.
func (c *Coordinator) HealthCheck(name string) (bool, error) {
	info, err := c.containerMgr.Get(name)
	if err != nil {
		return false, err
	}

	resp, err := c.httpClient.Get(info.ShimAddr + "/health")
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// ContainerManager returns the underlying container manager.
func (c *Coordinator) ContainerManager() *container.Manager {
	return c.containerMgr
}

// --- HTTP Handlers for the coordinator API ---

// RegisterRoutes adds coordinator routes to a ServeMux.
func (c *Coordinator) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/prompt", c.handlePrompt)
	mux.HandleFunc("/api/containers", c.handleContainers)
	mux.HandleFunc("/api/containers/", c.handleContainerByName)
}

func (c *Coordinator) handlePrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req PromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	defer r.Body.Close()

	if req.Tool == "" || req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tool and prompt are required"})
		return
	}
	if req.Account == "" {
		req.Account = "default"
	}

	resp, err := c.Route(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (c *Coordinator) handleContainers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		containers := c.containerMgr.List()
		writeJSON(w, http.StatusOK, containers)

	case http.MethodPost:
		var req SpawnRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		defer r.Body.Close()

		if req.Tool == "" || req.CLICommand == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tool and cli_command are required"})
			return
		}
		if req.Account == "" {
			req.Account = "default"
		}

		info, err := c.Spawn(req)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusCreated, info)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (c *Coordinator) handleContainerByName(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/containers/")
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]

	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "container name required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Check for sub-routes
		if len(parts) > 1 {
			switch parts[1] {
			case "health":
				healthy, err := c.HealthCheck(name)
				if err != nil {
					writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusOK, map[string]interface{}{"healthy": healthy})
				return
			case "logs":
				logs, err := container.ContainerLogs(name, 100)
				if err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
				return
			}
		}

		info, err := c.containerMgr.Get(name)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, info)

	case http.MethodDelete:
		if err := c.containerMgr.Remove(name); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})

	case http.MethodPost:
		if len(parts) > 1 && parts[1] == "stop" {
			if err := c.containerMgr.Stop(name); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
