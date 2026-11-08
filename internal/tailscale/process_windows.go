//go:build windows

package tailscale

import (
	"os/exec"
	"syscall"
)

// A status refresh in the GUI should not flash a console window.
func configureCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
