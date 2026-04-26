package adapter

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Bhattisahb121/cli-sidecar/internal/config"
)

// GenericAdapter works with any CLI tool that accepts a prompt as argument
// and outputs the response to stdout.
type GenericAdapter struct {
	cfg config.ToolConfig
}

// NewGenericAdapter creates a new adapter for any CLI tool.
func NewGenericAdapter(cfg config.ToolConfig) *GenericAdapter {
	return &GenericAdapter{cfg: cfg}
}

func (a *GenericAdapter) Name() string {
	return a.cfg.Name
}

func (a *GenericAdapter) Available() bool {
	_, err := exec.LookPath(a.cfg.Command)
	return err == nil
}

func (a *GenericAdapter) buildCmd(ctx context.Context, prompt string) *exec.Cmd {
	args := make([]string, len(a.cfg.Args))
	copy(args, a.cfg.Args)
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, a.cfg.Command, args...)

	// Inherit environment and add custom env vars
	cmd.Env = os.Environ()
	for k, v := range a.cfg.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	return cmd
}

func (a *GenericAdapter) Execute(ctx context.Context, prompt string) (*Response, error) {
	cmd := a.buildCmd(ctx, prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Include stderr in error for debugging
		errMsg := err.Error()
		if stderr.Len() > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, strings.TrimSpace(stderr.String()))
		}
		return &Response{
			Output: stdout.String(),
			Error:  errMsg,
		}, nil
	}

	return &Response{
		Output: strings.TrimSpace(stdout.String()),
	}, nil
}

func (a *GenericAdapter) Stream(ctx context.Context, prompt string) (<-chan StreamChunk, error) {
	cmd := a.buildCmd(ctx, prompt)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)

		// Read stderr in background for error reporting
		var stderrBuf bytes.Buffer
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				stderrBuf.WriteString(scanner.Text() + "\n")
			}
		}()

		// Stream stdout line by line
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
				ch <- StreamChunk{Error: "cancelled", Done: true}
				return
			default:
				ch <- StreamChunk{Text: scanner.Text() + "\n"}
			}
		}

		err := cmd.Wait()
		if err != nil {
			errMsg := err.Error()
			if stderrBuf.Len() > 0 {
				errMsg = strings.TrimSpace(stderrBuf.String())
			}
			ch <- StreamChunk{Error: errMsg, Done: true}
			return
		}

		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}
