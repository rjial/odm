package noderuntime

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for node: with FAKE_NODE set it
// behaves like a tiny node instead of running the tests.
func TestMain(m *testing.M) {
	switch os.Getenv("FAKE_NODE") {
	case "":
		os.Exit(m.Run())
	case "ok":
		args := os.Args[1:]
		if len(args) == 1 && args[0] == "--version" {
			os.Stdout.WriteString("v22.1.0-fake\nextra\n")
			os.Exit(0)
		}
		if len(args) == 1 && args[0] == "-" {
			in, _ := io.ReadAll(os.Stdin)
			os.Stdout.WriteString("got: " + string(in))
			os.Exit(0)
		}
		os.Exit(1)
	case "fail":
		os.Stderr.WriteString("ReferenceError: jsc is not defined\n")
		os.Exit(1)
	case "hang":
		time.Sleep(time.Minute)
	}
}

func fake(t *testing.T, dir, mode string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, exeName())
	if err := os.WriteFile(p, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_NODE", mode)
	return p
}

func TestLocate(t *testing.T) {
	dir := t.TempDir()
	bin := fake(t, dir, "ok")
	if got, err := Locate(bin); err != nil || got != bin {
		t.Fatalf("file: %q %v", got, err)
	}
	if got, err := Locate(dir); err != nil || got != bin {
		t.Fatalf("folder: %q %v", got, err)
	}
	if _, err := Locate(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing path accepted")
	}
	t.Setenv("PATH", dir)
	if got, err := Locate(""); err != nil || got == "" {
		t.Fatalf("PATH: %q %v", got, err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Locate(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty PATH: %v", err)
	}
}

func TestVersion(t *testing.T) {
	bin := fake(t, t.TempDir(), "ok")
	v, err := Version(context.Background(), bin)
	if err != nil || v != "v22.1.0-fake" {
		t.Fatalf("%q %v", v, err)
	}
}

func TestRun(t *testing.T) {
	bin := fake(t, t.TempDir(), "ok")
	out, err := Run(context.Background(), bin, "console.log(1)")
	if err != nil || out != "got: console.log(1)" {
		t.Fatalf("%q %v", out, err)
	}

	bin = fake(t, t.TempDir(), "fail")
	_, err = Run(context.Background(), bin, "console.log(1)")
	if err == nil || !strings.Contains(err.Error(), "jsc is not defined") {
		t.Fatalf("failure: %v", err)
	}

	bin = fake(t, t.TempDir(), "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := Run(ctx, bin, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
}
