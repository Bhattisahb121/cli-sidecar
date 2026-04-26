package adapter

import (
	"context"
	"fmt"

	"github.com/Bhattisahb121/cli-sidecar/internal/config"
)

// Response holds the result from a CLI tool execution.
type Response struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// StreamChunk is a piece of streaming output.
type StreamChunk struct {
	Text  string `json:"text"`
	Done  bool   `json:"done"`
	Final bool   `json:"final,omitempty"` // true when this chunk contains the fully processed output
	Error string `json:"error,omitempty"`
}

// Adapter defines the interface for interacting with a CLI tool.
type Adapter interface {
	// Name returns the tool name.
	Name() string

	// Execute runs a prompt and returns the full response.
	Execute(ctx context.Context, prompt string) (*Response, error)

	// Stream runs a prompt and streams output chunks via a channel.
	Stream(ctx context.Context, prompt string) (<-chan StreamChunk, error)

	// Available checks if the CLI tool is installed and accessible.
	Available() bool
}

// Registry holds all registered adapters.
type Registry struct {
	adapters map[string]Adapter
}

// NewRegistry creates a registry and initializes adapters from config.
func NewRegistry(tools []config.ToolConfig) *Registry {
	r := &Registry{adapters: make(map[string]Adapter)}

	for _, t := range tools {
		if !t.Enabled {
			continue
		}
		a := NewGenericAdapter(t)
		r.adapters[t.Name] = a
	}

	return r
}

// Get returns an adapter by name.
func (r *Registry) Get(name string) (Adapter, error) {
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("adapter not found: %s", name)
	}
	return a, nil
}

// List returns all registered adapter names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	return names
}

// ListAvailable returns only adapters whose CLI tool is installed.
func (r *Registry) ListAvailable() []string {
	var names []string
	for name, a := range r.adapters {
		if a.Available() {
			names = append(names, name)
		}
	}
	return names
}
