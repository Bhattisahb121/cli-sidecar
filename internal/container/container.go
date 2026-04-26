package container

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Info holds metadata about a running container.
type Info struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Tool        string    `json:"tool"`
	Account     string    `json:"account"`
	Seq         int       `json:"seq"`
	ShimPort    int       `json:"shim_port"`
	ShimAddr    string    `json:"shim_addr"`
	Status      string    `json:"status"` // "creating", "running", "stopped", "error"
	CreatedAt   time.Time `json:"created_at"`
	Error       string    `json:"error,omitempty"`
}

// Config holds settings for creating a container.
type Config struct {
	Tool       string            `json:"tool"`
	Account    string            `json:"account"`
	Image      string            `json:"image"`
	CLICommand string            `json:"cli_command"`
	CLIArgs    string            `json:"cli_args"`
	OutputMode string            `json:"output_mode"`
	Env        map[string]string `json:"env,omitempty"`
	Network    string            `json:"network"`
}

// Manager manages Docker containers for CLI tools.
type Manager struct {
	mu         sync.RWMutex
	containers map[string]*Info
	network    string
	seqCounter int
	shimImage  string
}

// NewManager creates a new container manager.
func NewManager(network, shimImage string) *Manager {
	return &Manager{
		containers: make(map[string]*Info),
		network:    network,
		shimImage:  shimImage,
	}
}

// EnsureNetwork creates the Docker network if it doesn't exist.
func (m *Manager) EnsureNetwork() error {
	// Check if network exists
	cmd := exec.Command("docker", "network", "inspect", m.network)
	if err := cmd.Run(); err == nil {
		return nil // Network already exists
	}

	cmd = exec.Command("docker", "network", "create", m.network)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create network %s: %w (%s)", m.network, err, stderr.String())
	}

	log.Printf("Created Docker network: %s", m.network)
	return nil
}

// Create creates and starts a new container.
func (m *Manager) Create(cfg Config) (*Info, error) {
	m.mu.Lock()
	m.seqCounter++
	seq := m.seqCounter
	m.mu.Unlock()

	name := fmt.Sprintf("%s-%s-%03d", cfg.Tool, cfg.Account, seq)

	// Find available port for shim
	shimPort := 8831 + seq

	info := &Info{
		Name:      name,
		Tool:      cfg.Tool,
		Account:   cfg.Account,
		Seq:       seq,
		ShimPort:  shimPort,
		Status:    "creating",
		CreatedAt: time.Now(),
	}

	m.mu.Lock()
	m.containers[name] = info
	m.mu.Unlock()

	// Build docker run args
	image := cfg.Image
	if image == "" {
		image = m.shimImage
	}

	args := []string{
		"run", "-d",
		"--name", name,
		"--network", m.network,
		"-e", fmt.Sprintf("CLI_COMMAND=%s", cfg.CLICommand),
		"-e", fmt.Sprintf("SHIM_PORT=%d", 8831),
		"-e", fmt.Sprintf("OUTPUT_MODE=%s", cfg.OutputMode),
	}

	if cfg.CLIArgs != "" {
		args = append(args, "-e", fmt.Sprintf("CLI_ARGS=%s", cfg.CLIArgs))
	}

	for k, v := range cfg.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, image)

	cmd := exec.Command("docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		m.mu.Lock()
		info.Status = "error"
		info.Error = fmt.Sprintf("%s: %s", err.Error(), strings.TrimSpace(stderr.String()))
		m.mu.Unlock()
		return info, fmt.Errorf("failed to create container: %w (%s)", err, stderr.String())
	}

	containerID := strings.TrimSpace(stdout.String())

	m.mu.Lock()
	info.ID = containerID[:12]
	info.Status = "running"
	// In Docker network, containers can reach each other by name
	info.ShimAddr = fmt.Sprintf("http://%s:%d", name, 8831)
	m.mu.Unlock()

	log.Printf("Container started: %s (ID: %s, shim: %s)", name, info.ID, info.ShimAddr)
	return info, nil
}

// Stop stops a container.
func (m *Manager) Stop(name string) error {
	m.mu.RLock()
	info, ok := m.containers[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("container not found: %s", name)
	}

	cmd := exec.Command("docker", "stop", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	m.mu.Lock()
	info.Status = "stopped"
	m.mu.Unlock()

	return nil
}

// Remove stops and removes a container.
func (m *Manager) Remove(name string) error {
	cmd := exec.Command("docker", "rm", "-f", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	m.mu.Lock()
	delete(m.containers, name)
	m.mu.Unlock()

	log.Printf("Container removed: %s", name)
	return nil
}

// Get returns container info by name.
func (m *Manager) Get(name string) (*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.containers[name]
	if !ok {
		return nil, fmt.Errorf("container not found: %s", name)
	}
	return info, nil
}

// List returns all containers.
func (m *Manager) List() []*Info {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Info, 0, len(m.containers))
	for _, info := range m.containers {
		result = append(result, info)
	}
	return result
}

// ListByTool returns containers for a specific tool.
func (m *Manager) ListByTool(tool string) []*Info {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Info
	for _, info := range m.containers {
		if info.Tool == tool {
			result = append(result, info)
		}
	}
	return result
}

// FindAvailable finds a running container for the given tool and account.
func (m *Manager) FindAvailable(tool, account string) *Info {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, info := range m.containers {
		if info.Tool == tool && info.Account == account && info.Status == "running" {
			return info
		}
	}
	return nil
}

// Cleanup removes all managed containers and the network.
func (m *Manager) Cleanup() {
	m.mu.RLock()
	names := make([]string, 0, len(m.containers))
	for name := range m.containers {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		m.Remove(name)
	}
}

// InspectDocker checks if Docker is available.
func InspectDocker() error {
	cmd := exec.Command("docker", "info", "--format", "{{.ServerVersion}}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker is not available: %w", err)
	}
	log.Printf("Docker version: %s", strings.TrimSpace(stdout.String()))
	return nil
}

// ContainerLogs returns recent logs from a container.
func ContainerLogs(name string, lines int) (string, error) {
	args := []string{"logs", "--tail", fmt.Sprintf("%d", lines), name}
	cmd := exec.Command("docker", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	return stdout.String(), nil
}

// ContainerHealth checks if the shim inside a container is healthy.
func ContainerHealth(shimAddr string) (bool, error) {
	// Use docker exec to curl the health endpoint from within the network
	cmd := exec.Command("curl", "-sf", "--connect-timeout", "2", shimAddr+"/health")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false, nil
	}

	var resp map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return false, nil
	}
	return resp["status"] == "ok", nil
}
