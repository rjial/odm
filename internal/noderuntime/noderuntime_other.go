//go:build !windows

package noderuntime

import "os/exec"

func hide(*exec.Cmd) {}
