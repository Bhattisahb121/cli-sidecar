package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Bhattisahb121/cli-sidecar/internal/adapter"
	"github.com/Bhattisahb121/cli-sidecar/internal/session"
)

// Server is the HTTP API server.
type Server struct {
	registry   *adapter.Registry
	sessionMgr *session.Manager
	httpServer *http.Server
}

// New creates a new API server.
func New(registry *adapter.Registry, sessionMgr *session.Manager, host string, port int) *Server {
	s := &Server{
		registry:   registry,
		sessionMgr: sessionMgr,
	}

	mux := http.NewServeMux()

	// Tool endpoints
	mux.HandleFunc("/api/tools", s.handleListTools)

	// Synchronous execution
	mux.HandleFunc("/api/run", s.handleRun)

	// Streaming execution (SSE)
	mux.HandleFunc("/api/stream", s.handleStream)

	// Session management
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionByID)

	// Health check
	mux.HandleFunc("/health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", host, port),
		Handler:      CORSMiddleware(LogMiddleware(mux)),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute, // Long timeout for streaming
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	log.Printf("Starting cli-sidecar on %s", s.httpServer.Addr)
	log.Printf("Available tools: %v", s.registry.List())
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// --- Request/Response types ---

type runRequest struct {
	Tool       string `json:"tool"`
	Prompt     string `json:"prompt"`
	OutputMode string `json:"output_mode,omitempty"` // override: "raw", "plain", "markdown", "dumb"
}

type runResponse struct {
	SessionID string `json:"session_id"`
	Tool      string `json:"tool"`
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type toolInfo struct {
	Name       string `json:"name"`
	Available  bool   `json:"available"`
	OutputMode string `json:"output_mode,omitempty"`
	UsePTY     bool   `json:"use_pty,omitempty"`
}

// --- Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tools := make([]toolInfo, 0)
	for _, name := range s.registry.List() {
		a, _ := s.registry.Get(name)
		tools = append(tools, toolInfo{
			Name:      name,
			Available: a.Available(),
		})
	}

	writeJSON(w, http.StatusOK, tools)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	defer r.Body.Close()

	if req.Tool == "" || req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "tool and prompt are required")
		return
	}

	sess, err := s.sessionMgr.Create(req.Tool, req.Prompt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Poll until done (with timeout from request context)
	ctx := r.Context()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.sessionMgr.Cancel(sess.ID)
			writeError(w, http.StatusGatewayTimeout, "request cancelled")
			return
		case <-ticker.C:
			current, _ := s.sessionMgr.Get(sess.ID)
			if current.Status == "done" || current.Status == "error" || current.Status == "cancelled" {
				writeJSON(w, http.StatusOK, runResponse{
					SessionID: current.ID,
					Tool:      current.Tool,
					Output:    current.Output,
					Error:     current.Error,
				})
				return
			}
		}
	}
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	defer r.Body.Close()

	if req.Tool == "" || req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "tool and prompt are required")
		return
	}

	sessionID, ch, err := s.sessionMgr.Stream(req.Tool, req.Prompt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Session-ID", sessionID)
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Send session ID as first event
	fmt.Fprintf(w, "event: session\ndata: %s\n\n", sessionID)
	flusher.Flush()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			s.sessionMgr.Cancel(sessionID)
			fmt.Fprintf(w, "event: error\ndata: cancelled\n\n")
			flusher.Flush()
			return
		case chunk, ok := <-ch:
			if !ok {
				fmt.Fprintf(w, "event: done\ndata: complete\n\n")
				flusher.Flush()
				return
			}

			if chunk.Error != "" {
				data, _ := json.Marshal(map[string]string{"error": chunk.Error})
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
				flusher.Flush()
				if chunk.Done {
					return
				}
				continue
			}

			if chunk.Done {
				fmt.Fprintf(w, "event: done\ndata: complete\n\n")
				flusher.Flush()
				return
			}

			data, _ := json.Marshal(map[string]string{"text": chunk.Text})
			fmt.Fprintf(w, "event: token\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessions := s.sessionMgr.List()
		writeJSON(w, http.StatusOK, sessions)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from URL: /api/sessions/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.SplitN(path, "/", 2)
	sessionID := parts[0]

	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		sess, err := s.sessionMgr.Get(sessionID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sess)

	case http.MethodDelete:
		// Check if it's a cancel request
		if len(parts) > 1 && parts[1] == "cancel" {
			if err := s.sessionMgr.Cancel(sessionID); err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
			return
		}

		if err := s.sessionMgr.Delete(sessionID); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	case http.MethodPost:
		// POST /api/sessions/{id}/cancel
		if len(parts) > 1 && parts[1] == "cancel" {
			if err := s.sessionMgr.Cancel(sessionID); err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
			return
		}
		writeError(w, http.StatusBadRequest, "unknown action")

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// --- Middleware ---

// CORSMiddleware adds CORS headers.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// LogMiddleware logs requests.
func LogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// --- Exported handler factories for reuse by coordinator ---

// HandleListToolsFunc returns the tools list handler.
func HandleListToolsFunc(registry *adapter.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		tools := make([]toolInfo, 0)
		for _, name := range registry.List() {
			a, _ := registry.Get(name)
			tools = append(tools, toolInfo{
				Name:      name,
				Available: a.Available(),
			})
		}
		writeJSON(w, http.StatusOK, tools)
	}
}

// HandleRunFunc returns the synchronous run handler.
func HandleRunFunc(sessionMgr *session.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req runRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		defer r.Body.Close()
		if req.Tool == "" || req.Prompt == "" {
			writeError(w, http.StatusBadRequest, "tool and prompt are required")
			return
		}
		sess, err := sessionMgr.Create(req.Tool, req.Prompt)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		ctx := r.Context()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				sessionMgr.Cancel(sess.ID)
				writeError(w, http.StatusGatewayTimeout, "request cancelled")
				return
			case <-ticker.C:
				current, _ := sessionMgr.Get(sess.ID)
				if current.Status == "done" || current.Status == "error" || current.Status == "cancelled" {
					writeJSON(w, http.StatusOK, runResponse{
						SessionID: current.ID,
						Tool:      current.Tool,
						Output:    current.Output,
						Error:     current.Error,
					})
					return
				}
			}
		}
	}
}

// HandleStreamFunc returns the SSE streaming handler.
func HandleStreamFunc(sessionMgr *session.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req runRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		defer r.Body.Close()
		if req.Tool == "" || req.Prompt == "" {
			writeError(w, http.StatusBadRequest, "tool and prompt are required")
			return
		}
		sessionID, ch, err := sessionMgr.Stream(req.Tool, req.Prompt)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Session-ID", sessionID)
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}
		fmt.Fprintf(w, "event: session\ndata: %s\n\n", sessionID)
		flusher.Flush()
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				sessionMgr.Cancel(sessionID)
				return
			case chunk, ok := <-ch:
				if !ok {
					fmt.Fprintf(w, "event: done\ndata: complete\n\n")
					flusher.Flush()
					return
				}
				if chunk.Error != "" {
					data, _ := json.Marshal(map[string]string{"error": chunk.Error})
					fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
					flusher.Flush()
					if chunk.Done {
						return
					}
					continue
				}
				if chunk.Done {
					fmt.Fprintf(w, "event: done\ndata: complete\n\n")
					flusher.Flush()
					return
				}
				data, _ := json.Marshal(map[string]string{"text": chunk.Text})
				fmt.Fprintf(w, "event: token\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

// HandleSessionsFunc returns the sessions list handler.
func HandleSessionsFunc(sessionMgr *session.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		sessions := sessionMgr.List()
		writeJSON(w, http.StatusOK, sessions)
	}
}
