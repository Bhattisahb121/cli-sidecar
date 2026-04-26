package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Bhattisahb121/cli-sidecar/internal/adapter"
)

// Session represents a single interaction with a CLI tool.
type Session struct {
	ID        string    `json:"id"`
	Tool      string    `json:"tool"`
	Prompt    string    `json:"prompt"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	Status    string    `json:"status"` // "pending", "running", "done", "error", "cancelled"
	CreatedAt time.Time `json:"created_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"`

	cancelFn context.CancelFunc
}

// Manager manages concurrent CLI sessions.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	registry *adapter.Registry
	counter  int64
}

// NewManager creates a new session manager.
func NewManager(registry *adapter.Registry) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		registry: registry,
	}
}

// Create creates and starts a new session.
func (m *Manager) Create(tool, prompt string) (*Session, error) {
	a, err := m.registry.Get(tool)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.counter++
	id := fmt.Sprintf("sess_%d_%d", time.Now().Unix(), m.counter)
	s := &Session{
		ID:        id,
		Tool:      tool,
		Prompt:    prompt,
		Status:    "pending",
		CreatedAt: time.Now(),
	}
	m.sessions[id] = s
	m.mu.Unlock()

	// Run in background
	go m.run(s, a)

	return s, nil
}

func (m *Manager) run(s *Session, a adapter.Adapter) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	s.cancelFn = cancel
	defer cancel()

	m.mu.Lock()
	s.Status = "running"
	m.mu.Unlock()

	resp, err := a.Execute(ctx, s.Prompt)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	s.DoneAt = &now

	if err != nil {
		s.Status = "error"
		s.Error = err.Error()
		return
	}

	if resp.Error != "" {
		s.Status = "error"
		s.Output = resp.Output
		s.Error = resp.Error
		return
	}

	s.Status = "done"
	s.Output = resp.Output
}

// Stream creates a session and returns a streaming channel.
func (m *Manager) Stream(tool, prompt string) (string, <-chan adapter.StreamChunk, error) {
	a, err := m.registry.Get(tool)
	if err != nil {
		return "", nil, err
	}

	m.mu.Lock()
	m.counter++
	id := fmt.Sprintf("sess_%d_%d", time.Now().Unix(), m.counter)
	s := &Session{
		ID:        id,
		Tool:      tool,
		Prompt:    prompt,
		Status:    "running",
		CreatedAt: time.Now(),
	}
	m.sessions[id] = s
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	s.cancelFn = cancel

	ch, err := a.Stream(ctx, prompt)
	if err != nil {
		cancel()
		m.mu.Lock()
		s.Status = "error"
		s.Error = err.Error()
		m.mu.Unlock()
		return "", nil, err
	}

	// Wrap channel to update session state on completion
	outCh := make(chan adapter.StreamChunk, 64)
	go func() {
		defer close(outCh)
		defer cancel()

		var fullOutput string
		for chunk := range ch {
			outCh <- chunk
			if chunk.Text != "" {
				fullOutput += chunk.Text
			}
			if chunk.Done {
				m.mu.Lock()
				now := time.Now()
				s.DoneAt = &now
				s.Output = fullOutput
				if chunk.Error != "" {
					s.Status = "error"
					s.Error = chunk.Error
				} else {
					s.Status = "done"
				}
				m.mu.Unlock()
				return
			}
		}

		// Channel closed without Done flag
		m.mu.Lock()
		now := time.Now()
		s.DoneAt = &now
		s.Output = fullOutput
		s.Status = "done"
		m.mu.Unlock()
	}()

	return id, outCh, nil
}

// Get returns a session by ID.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return s, nil
}

// Cancel cancels a running session.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	if s.cancelFn != nil {
		s.cancelFn()
	}
	s.Status = "cancelled"
	now := time.Now()
	s.DoneAt = &now
	return nil
}

// List returns all sessions.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// Delete removes a session.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	delete(m.sessions, id)
	return nil
}
