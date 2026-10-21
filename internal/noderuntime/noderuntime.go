// Package noderuntime finds a Node.js binary and runs a script with it.
//
// ODM's own YouTube support needs Node.js to run yt-dlp/ejs's signature
// solver (see internal/youtube): unlike ffmpeg, which only improves a
// stream download when present, Node.js is required for that path to work
// at all. Its absence is not fatal to ODM as a whole: YouTube downloads
// then fall straight to the yt-dlp fallback, if that is set up instead.
package noderuntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNotFound means no Node.js was configured, bundled or on PATH.
var ErrNotFound = errors.New("Node.js not found: install it or set its path in Options")

func exeName() string {
	if runtime.GOOS == "windows" {
		return "node.exe"
	}
	return "node"
}

// Locate returns the node binary to use, in order: the configured path (a
// file, or a folder containing node), one next to the ODM executable, then
// PATH.
func Locate(configured string) (string, error) {
	if configured = strings.TrimSpace(configured); configured != "" {
		if st, err := os.Stat(configured); err == nil && st.IsDir() {
			configured = filepath.Join(configured, exeName())
		}
		if st, err := os.Stat(configured); err == nil && !st.IsDir() {
			return configured, nil
		}
		return "", fmt.Errorf("Node.js not found at %s", configured)
	}
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), exeName())
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if p, err := exec.LookPath("node"); err == nil {
		return p, nil
	}
	return "", ErrNotFound
}

// Version returns the first line of "node --version", for Options.
func Version(ctx context.Context, bin string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "--version")
	hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line), nil
}

// Run feeds script to node's standard input and returns what it wrote to
// standard output. On failure the error carries the end of node's stderr.
func Run(ctx context.Context, bin, script string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "-")
	hide(cmd)
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 500 {
			msg = "…" + msg[len(msg)-500:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("node failed: %s", msg)
	}
	return stdout.String(), nil
}
