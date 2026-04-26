package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Bhattisahb121/cli-sidecar/internal/ansi"
	"github.com/Bhattisahb121/cli-sidecar/internal/config"
)

// CLIProcess manages a persistent CLI tool running in a PTY.
type CLIProcess struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	ptmx       *os.File
	outputMode config.OutputMode
	ready      bool
}

// promptRequest is the HTTP request body for sending prompts.
type promptRequest struct {
	Prompt     string `json:"prompt"`
	OutputMode string `json:"output_mode,omitempty"`
}

// promptResponse is the HTTP response body.
type promptResponse struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

func main() {
	port := os.Getenv("SHIM_PORT")
	if port == "" {
		port = "8831"
	}

	cliCommand := os.Getenv("CLI_COMMAND")
	if cliCommand == "" {
		log.Fatal("CLI_COMMAND environment variable is required")
	}

	cliArgs := os.Getenv("CLI_ARGS")
	outputMode := config.OutputMode(os.Getenv("OUTPUT_MODE"))
	if outputMode == "" {
		outputMode = config.OutputRaw
	}

	proc := &CLIProcess{
		outputMode: outputMode,
	}

	// Start the CLI process in interactive mode
	if err := proc.Start(cliCommand, cliArgs); err != nil {
		log.Fatalf("Failed to start CLI process: %s", err)
	}
	defer proc.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/prompt", proc.handlePrompt)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/info", proc.handleInfo)

	srv := &http.Server{
		Addr:         "0.0.0.0:" + port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Shim listening on :%s (CLI: %s)", port, cliCommand)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %s", err)
		}
	}()

	sig := <-sigCh
	log.Printf("Received %s, shutting down...", sig)
	proc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// Start launches the CLI tool in a PTY for interactive use.
func (p *CLIProcess) Start(command, args string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var cmdArgs []string
	if args != "" {
		cmdArgs = strings.Fields(args)
	}

	p.cmd = exec.Command(command, cmdArgs...)
	p.cmd.Env = os.Environ()

	if p.outputMode == config.OutputDumb {
		p.cmd.Env = append(p.cmd.Env, "TERM=dumb", "NO_COLOR=1")
	}

	ptmx, err := startPTY(p.cmd)
	if err != nil {
		return fmt.Errorf("failed to start PTY: %w", err)
	}

	p.ptmx = ptmx
	p.ready = true

	// Drain initial output (welcome message, prompt, etc.)
	go func() {
		time.Sleep(2 * time.Second)
		p.drainOutput(3 * time.Second)
	}()

	log.Printf("CLI process started: %s %s (PID: %d)", command, args, p.cmd.Process.Pid)
	return nil
}

// Stop kills the CLI process.
func (p *CLIProcess) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
	}
	if p.ptmx != nil {
		p.ptmx.Close()
	}
	p.ready = false
}

// SendPrompt writes a prompt to the CLI and reads the response.
func (p *CLIProcess) SendPrompt(prompt string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.ready || p.ptmx == nil {
		return "", fmt.Errorf("CLI process is not running")
	}

	// Drain any buffered output first
	p.drainOutput(500 * time.Millisecond)

	// Write prompt to PTY stdin
	_, err := p.ptmx.Write([]byte(prompt + "\n"))
	if err != nil {
		return "", fmt.Errorf("failed to write prompt: %w", err)
	}

	// Read response with timeout
	output, err := p.readResponse(2 * time.Minute)
	if err != nil {
		return output, err
	}

	return output, nil
}

// drainOutput reads and discards any buffered output.
func (p *CLIProcess) drainOutput(timeout time.Duration) {
	buf := make([]byte, 4096)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		p.ptmx.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, err := p.ptmx.Read(buf)
		if err != nil {
			break
		}
	}
	p.ptmx.SetReadDeadline(time.Time{})
}

// readResponse reads output from the PTY until the CLI tool is done responding.
// It detects "done" by a period of silence (no new output).
func (p *CLIProcess) readResponse(timeout time.Duration) (string, error) {
	var output strings.Builder
	buf := make([]byte, 4096)

	deadline := time.Now().Add(timeout)
	silenceThreshold := 3 * time.Second
	lastRead := time.Now()

	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining < 200*time.Millisecond {
			break
		}

		readTimeout := 500 * time.Millisecond
		if readTimeout > remaining {
			readTimeout = remaining
		}

		p.ptmx.SetReadDeadline(time.Now().Add(readTimeout))
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			output.WriteString(chunk)
			lastRead = time.Now()
		}
		if err != nil {
			if isTimeoutError(err) {
				// Check if we've been silent long enough
				if time.Since(lastRead) > silenceThreshold && output.Len() > 0 {
					break
				}
				continue
			}
			if err == io.EOF {
				break
			}
			// Other errors might be temporary
			continue
		}
	}

	p.ptmx.SetReadDeadline(time.Time{})

	result := output.String()

	// Remove echo of our prompt from the beginning
	result = removePromptEcho(result)

	// Clean up the output
	result = cleanOutput(result)

	return result, nil
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "i/o timeout") ||
		strings.Contains(err.Error(), "deadline exceeded")
}

// removePromptEcho removes the echoed prompt from PTY output.
func removePromptEcho(output string) string {
	lines := strings.SplitN(output, "\n", 2)
	if len(lines) > 1 {
		return lines[1]
	}
	return output
}

// cleanOutput removes trailing prompts, whitespace, and control chars.
func cleanOutput(output string) string {
	// Remove carriage returns
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "")

	// Trim trailing whitespace and control characters
	output = strings.TrimRightFunc(output, func(r rune) bool {
		return !utf8.ValidRune(r) || r == '\n' || r == ' ' || r == '\t'
	})

	return output
}

func (p *CLIProcess) processOutput(raw string) string {
	switch p.outputMode {
	case config.OutputPlain:
		return ansi.ToPlainText(raw)
	case config.OutputMarkdown:
		return ansi.ToMarkdown(raw)
	default:
		return raw
	}
}

// --- HTTP Handlers ---

func (p *CLIProcess) handlePrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, promptResponse{Error: "method not allowed"})
		return
	}

	var req promptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, promptResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	defer r.Body.Close()

	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, promptResponse{Error: "prompt is required"})
		return
	}

	output, err := p.SendPrompt(req.Prompt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, promptResponse{
			Output: p.processOutput(output),
			Error:  err.Error(),
		})
		return
	}

	// Apply per-request output mode override if specified
	if req.OutputMode != "" {
		mode := config.OutputMode(req.OutputMode)
		switch mode {
		case config.OutputPlain:
			output = ansi.ToPlainText(output)
		case config.OutputMarkdown:
			output = ansi.ToMarkdown(output)
		default:
			output = p.processOutput(output)
		}
	} else {
		output = p.processOutput(output)
	}

	writeJSON(w, http.StatusOK, promptResponse{Output: output})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *CLIProcess) handleInfo(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()

	info := map[string]interface{}{
		"ready":       p.ready,
		"output_mode": string(p.outputMode),
	}
	if p.cmd != nil && p.cmd.Process != nil {
		info["pid"] = p.cmd.Process.Pid
	}
	writeJSON(w, http.StatusOK, info)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
