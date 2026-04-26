//go:build windows

package adapter

import (
	"fmt"
	"os"
	"os/exec"
)

// startPTY is not supported on Windows.
func startPTY(cmd *exec.Cmd) (*os.File, error) {
	return nil, fmt.Errorf("PTY mode is not supported on Windows; use pipe mode instead (set use_pty: false)")
}
