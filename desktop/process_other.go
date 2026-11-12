//go:build !windows

package main

import "os/exec"

func configureCommand(*exec.Cmd) {}
