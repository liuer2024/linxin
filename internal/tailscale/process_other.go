//go:build !windows

package tailscale

import "os/exec"

func configureCommand(cmd *exec.Cmd) {}
