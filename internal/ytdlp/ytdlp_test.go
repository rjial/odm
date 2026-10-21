package ytdlp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for yt-dlp: with FAKE_YTDLP set it
// behaves like a tiny yt-dlp instead of running the tests.
func TestMain(m *testing.M) {
	switch os.Getenv("FAKE_YTDLP") {
	case "":
		os.Exit(m.Run())
	case "version":
		os.Stdout.WriteString("2026.10.01\n")
		os.Exit(0)
	case "probe":
		os.Stdout.WriteString(`{"title":"A Video","formats":[` +
			`{"format_id":"137","ext":"mp4","height":1080,"vcodec":"avc1","acodec":"none","filesize":123456},` +
			`{"format_id":"140","ext":"m4a","height":0,"vcodec":"none","acodec":"mp4a"}]}` + "\n")
		os.Exit(0)
	case "probe-fail":
		os.Stderr.WriteString("ERROR: Video unavailable\n")
		os.Exit(1)
	case "download":
		os.Stdout.WriteString("ODMPROGRESS:100:1000:0\n")
		os.Stdout.WriteString("ODMPROGRESS:500:1000:0\n")
		os.Stdout.WriteString("ODMPROGRESS:1000:1000:0\n")
		os.Stdout.WriteString("ODMPATH:/tmp/some dir/A Video.mp4\n")
		os.Exit(0)
	case "download-no-path":
		os.Stdout.WriteString("ODMPROGRESS:1000:1000:0\n")
		os.Exit(0)
	case "download-fail":
		os.Stdout.WriteString("ODMPROGRESS:100:1000:0\n")
		os.Stderr.WriteString("WARNING: ignore me\n")
		os.Stderr.WriteString("ERROR: Sign in to confirm you're not a bot\n")
		os.Exit(1)
	case "hang":
		time.Sleep(time.Minute)
	}
}

func fake(t *testing.T, mode string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), exeName())
	if err := os.WriteFile(p, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_YTDLP", mode)
	return p
}

func TestLocate(t *testing.T) {
	bin := fake(t, "version")
	dir := filepath.Dir(bin)
	if got, err := Locate(bin); err != nil || got != bin {
		t.Fatalf("file: %q %v", got, err)
	}
	if got, err := Locate(dir); err != nil || got != bin {
		t.Fatalf("folder: %q %v", got, err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Locate(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty PATH: %v", err)
	}
}

func TestVersion(t *testing.T) {
	bin := fake(t, "version")
	v, err := Version(context.Background(), bin)
	if err != nil || v != "2026.10.01" {
		t.Fatalf("%q %v", v, err)
	}
}

func TestProbe(t *testing.T) {
	bin := fake(t, "probe")
	info, err := Probe(context.Background(), bin, "https://youtube.com/watch?v=x")
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "A Video" || len(info.Formats) != 2 {
		t.Fatalf("%+v", info)
	}
	if info.Formats[0].Size() != 123456 {
		t.Fatalf("size: %d", info.Formats[0].Size())
	}

	bin = fake(t, "probe-fail")
	_, err = Probe(context.Background(), bin, "https://youtube.com/watch?v=x")
	if err == nil || !strings.Contains(err.Error(), "Video unavailable") {
		t.Fatalf("failure: %v", err)
	}
}

func TestRun(t *testing.T) {
	bin := fake(t, "download")
	var got [][2]int64
	path, err := Run(context.Background(), bin, []string{"-o", "out"}, func(done, total int64) {
		got = append(got, [2]int64{done, total})
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/some dir/A Video.mp4" {
		t.Fatalf("final path: %q", path)
	}
	want := [][2]int64{{100, 1000}, {500, 1000}, {1000, 1000}}
	if len(got) != len(want) || got[2] != want[2] {
		t.Fatalf("progress: %v", got)
	}

	bin = fake(t, "download-no-path")
	_, err = Run(context.Background(), bin, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "did not report the file") {
		t.Fatalf("missing final path: %v", err)
	}

	bin = fake(t, "download-fail")
	_, err = Run(context.Background(), bin, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "Sign in to confirm") || strings.Contains(err.Error(), "ignore me") {
		t.Fatalf("failure: %v", err)
	}

	bin = fake(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := Run(ctx, bin, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
}
