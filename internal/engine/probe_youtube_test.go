package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fakeYtDlpProbe(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as yt-dlp")
	}
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	script := `#!/bin/sh
echo '{"title":"Fake Video","formats":[{"format_id":"137","height":1080,"vcodec":"avc1","acodec":"none","filesize":900},{"format_id":"140","height":0,"vcodec":"none","acodec":"mp4a.40.2","abr":128}]}'
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestProbeYouTubePrefersYtDlp confirms yt-dlp is consulted first when
// configured and working: it answers even for a URL ODM's own extractor
// (internal/youtube) could not possibly resolve (not a real video), proving
// native resolution was never attempted.
func TestProbeYouTubePrefersYtDlp(t *testing.T) {
	m := NewManager(Settings{Dir: t.TempDir(), YtDlpPath: fakeYtDlpProbe(t)}, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10_000_000_000)
	defer cancel()
	yp, err := m.ProbeYouTube(ctx, "https://www.youtube.com/watch?v=not-a-real-video-id")
	if err != nil {
		t.Fatalf("expected yt-dlp to answer first, got: %v", err)
	}
	if yp.YtDlp == nil || yp.Title != "Fake Video" {
		t.Fatalf("expected a yt-dlp result, got %+v", yp)
	}
	if yp.DirectURL != "" || yp.Stream != nil {
		t.Fatalf("native path should not have been used: %+v", yp)
	}
}
