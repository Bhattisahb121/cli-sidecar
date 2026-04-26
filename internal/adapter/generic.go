package adapter

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Bhattisahb121/cli-sidecar/internal/ansi"
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

	cmd.Env = os.Environ()
	for k, v := range a.cfg.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// In "dumb" mode, set TERM=dumb to suppress ANSI output
	if a.cfg.OutputMode == config.OutputDumb {
		cmd.Env = append(cmd.Env, "TERM=dumb")
		cmd.Env = append(cmd.Env, "NO_COLOR=1")
	}

	return cmd
}

// processOutput applies the configured output mode to raw CLI output.
func (a *GenericAdapter) processOutput(raw string) string {
	switch a.cfg.OutputMode {
	case config.OutputPlain:
		return ansi.ToPlainText(raw)
	case config.OutputMarkdown:
		return ansi.ToMarkdown(raw)
	case config.OutputDumb, config.OutputRaw, "":
		return raw
	default:
		return raw
	}
}

func (a *GenericAdapter) Execute(ctx context.Context, prompt string) (*Response, error) {
	if a.cfg.UsePTY {
		return a.executePTY(ctx, prompt)
	}
	return a.executePipe(ctx, prompt)
}

func (a *GenericAdapter) executePipe(ctx context.Context, prompt string) (*Response, error) {
	cmd := a.buildCmd(ctx, prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		errMsg := err.Error()
		if stderr.Len() > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, strings.TrimSpace(stderr.String()))
		}
		return &Response{
			Output: a.processOutput(stdout.String()),
			Error:  errMsg,
		}, nil
	}

	return &Response{
		Output: strings.TrimSpace(a.processOutput(stdout.String())),
	}, nil
}

func (a *GenericAdapter) executePTY(ctx context.Context, prompt string) (*Response, error) {
	cmd := a.buildCmd(ctx, prompt)

	ptmx, err := startPTY(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}
	defer ptmx.Close()

	var output bytes.Buffer
	done := make(chan error, 1)

	go func() {
		_, err := io.Copy(&output, ptmx)
		_ = err // PTY read returns error on process exit, which is normal
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return &Response{
			Output: a.processOutput(output.String()),
			Error:  "cancelled",
		}, nil
	case err := <-done:
		if err != nil {
			return &Response{
				Output: a.processOutput(output.String()),
				Error:  err.Error(),
			}, nil
		}
		return &Response{
			Output: strings.TrimSpace(a.processOutput(output.String())),
		}, nil
	}
}

func (a *GenericAdapter) Stream(ctx context.Context, prompt string) (<-chan StreamChunk, error) {
	if a.cfg.UsePTY {
		return a.streamPTY(ctx, prompt)
	}
	return a.streamPipe(ctx, prompt)
}

func (a *GenericAdapter) streamPipe(ctx context.Context, prompt string) (<-chan StreamChunk, error) {
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

		var stderrBuf bytes.Buffer
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				stderrBuf.WriteString(scanner.Text() + "\n")
			}
		}()

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
				text := a.processOutput(scanner.Text() + "\n")
				ch <- StreamChunk{Text: text}
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

func (a *GenericAdapter) streamPTY(ctx context.Context, prompt string) (<-chan StreamChunk, error) {
	cmd := a.buildCmd(ctx, prompt)

	ptmx, err := startPTY(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)
		defer ptmx.Close()

		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
				ch <- StreamChunk{Error: "cancelled", Done: true}
				return
			default:
				n, err := ptmx.Read(buf)
				if n > 0 {
					text := a.processOutput(string(buf[:n]))
					ch <- StreamChunk{Text: text}
				}
				if err != nil {
					// PTY read error on process exit is normal
					_ = cmd.Wait()
					ch <- StreamChunk{Done: true}
					return
				}
			}
		}
	}()

	return ch, nil
}
