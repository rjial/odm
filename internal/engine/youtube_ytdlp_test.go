package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeYtDlp writes a shell script standing in for yt-dlp: "ok" resolves the
// %(ext)s in the -o template itself (as real yt-dlp does, picking the
// extension from --merge-output-format) to a name that does NOT match the
// template literally, writes content bytes to it, and reports it via
// ODMPATH — reproducing the exact shape of the bug this guards against:
// the final file's name is never assumed from the -o argument, only from
// what yt-dlp itself reports.
func fakeYtDlp(t *testing.T, mode string, content string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as yt-dlp")
	}
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	var script string
	switch mode {
	case "ok":
		script = `#!/bin/sh
out=""
for a; do
  if [ "$prev" = "-o" ]; then out="$a"; fi
  prev="$a"
done
real=$(echo "$out" | sed 's/%(ext)s$/mp4/')
printf '%s' '` + content + `' > "$real"
echo "` + fmt.Sprintf("ODMPROGRESS:%d:%d:0", len(content), len(content)) + `"
echo "ODMPATH:$real"
`
	case "fail":
		script = "#!/bin/sh\necho 'ERROR: Sign in to confirm you'\\''re not a bot' >&2\nexit 1\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestRunYtDlpRenamesToFinalName(t *testing.T) {
	bin := fakeYtDlp(t, "ok", "hello world")
	setYtDlpPath(bin)
	defer setYtDlpPath("")

	dir := t.TempDir()
	d := New(newID(), "https://youtube.com/watch?v=x", dir, 1, nil, 0, nil)
	d.YtDlp = &YtDlpJob{URL: "https://youtube.com/watch?v=x", Format: "best", Title: "My Video"}

	d.runYtDlp(context.Background())

	if d.Status != StatusComplete {
		t.Fatalf("status = %v, err = %q", d.Status, d.Err)
	}
	if d.FileName != "My Video.mp4" {
		t.Fatalf("file name = %q", d.FileName)
	}
	data, err := os.ReadFile(filepath.Join(dir, d.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("content = %q", data)
	}
	// No leftover work-template file.
	matches, _ := filepath.Glob(filepath.Join(dir, ".odm-ytdlp-*"))
	if len(matches) != 0 {
		t.Fatalf("leftovers: %v", matches)
	}
}

func TestRunYtDlpAvoidsNameCollision(t *testing.T) {
	bin := fakeYtDlp(t, "ok", "second")
	setYtDlpPath(bin)
	defer setYtDlpPath("")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "My Video.mp4"), []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New(newID(), "https://youtube.com/watch?v=x", dir, 1, nil, 0, nil)
	d.YtDlp = &YtDlpJob{URL: "https://youtube.com/watch?v=x", Format: "best", Title: "My Video"}

	d.runYtDlp(context.Background())

	if d.Status != StatusComplete {
		t.Fatalf("status = %v, err = %q", d.Status, d.Err)
	}
	if d.FileName == "My Video.mp4" {
		t.Fatalf("expected a numbered variant, kept the taken name")
	}
	if !strings.Contains(d.FileName, "My Video") {
		t.Fatalf("file name = %q", d.FileName)
	}
}

func TestRunYtDlpFailureCleansUpAndReportsError(t *testing.T) {
	bin := fakeYtDlp(t, "fail", "")
	setYtDlpPath(bin)
	defer setYtDlpPath("")

	dir := t.TempDir()
	d := New(newID(), "https://youtube.com/watch?v=x", dir, 1, nil, 0, nil)
	d.YtDlp = &YtDlpJob{URL: "https://youtube.com/watch?v=x", Format: "best", Title: "My Video"}

	d.runYtDlp(context.Background())

	if d.Status != StatusError {
		t.Fatalf("status = %v", d.Status)
	}
	if !strings.Contains(d.Err, "Sign in to confirm") {
		t.Fatalf("error = %q", d.Err)
	}
}
