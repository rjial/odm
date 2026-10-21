//go:build !windows

package ytdlp

import "os/exec"

func hide(*exec.Cmd) {}
