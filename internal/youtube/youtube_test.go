package youtube

import "testing"

func TestIsWatchURL(t *testing.T) {
	cases := []struct {
		url     string
		videoID string
		ok      bool
	}{
		{"https://www.youtube.com/watch?v=jNQXAC9IVRw", "jNQXAC9IVRw", true},
		{"https://youtube.com/watch?v=abc123", "abc123", true},
		{"https://m.youtube.com/watch?v=abc123", "abc123", true},
		{"https://music.youtube.com/watch?v=abc123", "abc123", true},
		{"https://youtu.be/abc123", "abc123", true},
		{"https://youtu.be/abc123?t=10", "abc123", true},
		{"https://www.youtube.com/watch", "", false},
		{"https://www.youtube.com/", "", false},
		{"https://www.youtube.com/playlist?list=xyz", "", false},
		{"https://example.com/watch?v=abc123", "", false},
		{"not a url", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		id, ok := IsWatchURL(c.url)
		if id != c.videoID || ok != c.ok {
			t.Errorf("IsWatchURL(%q) = %q, %v, want %q, %v", c.url, id, ok, c.videoID, c.ok)
		}
	}
}

func TestMimeExt(t *testing.T) {
	cases := map[string]string{
		`video/mp4; codecs="avc1.42001E, mp4a.40.2"`: "mp4",
		`audio/webm; codecs="opus"`:                  "webm",
		`video/mp4`:                                  "mp4",
		``:                                           "",
		`garbage`:                                    "",
	}
	for in, want := range cases {
		if got := mimeExt(in); got != want {
			t.Errorf("mimeExt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMuxed(t *testing.T) {
	if !muxed(Format{MimeType: `video/mp4; codecs="avc1.42001E, mp4a.40.2"`}) {
		t.Error("two codecs should be muxed")
	}
	if muxed(Format{MimeType: `video/mp4; codecs="avc1.4D400C"`}) {
		t.Error("one codec should not be muxed")
	}
}

func TestBestDirect(t *testing.T) {
	formats := []Format{
		{Itag: 134, MimeType: `video/mp4; codecs="avc1.4D400C"`, Bitrate: 300, URL: ""}, // SABR-only, no URL
		{Itag: 18, MimeType: `video/mp4; codecs="avc1.42001E, mp4a.40.2"`, Bitrate: 500, URL: "https://a"},
		{Itag: 22, MimeType: `video/mp4; codecs="avc1.4D401F, mp4a.40.2"`, Bitrate: 900, URL: "https://b"},
	}
	f, ok := bestDirect(formats)
	if !ok || f.Itag != 22 {
		t.Fatalf("got %+v, %v", f, ok)
	}

	if _, ok := bestDirect(nil); ok {
		t.Fatal("no formats should report not found")
	}

	// A muxed, lower-bitrate format still wins over a higher-bitrate
	// video-only one: nothing is left to join it with.
	mixed := []Format{
		{MimeType: `video/mp4; codecs="avc1.4D400C"`, Bitrate: 2000, URL: "https://video-only"},
		{MimeType: `video/mp4; codecs="avc1.42001E, mp4a.40.2"`, Bitrate: 500, URL: "https://muxed"},
	}
	f, ok = bestDirect(mixed)
	if !ok || f.URL != "https://muxed" {
		t.Fatalf("got %+v, %v", f, ok)
	}
}

func TestBestCipher(t *testing.T) {
	formats := []Format{
		{Itag: 1, Bitrate: 100, SignatureCipher: "s=low"},
		{Itag: 2, Bitrate: 900, SignatureCipher: "s=high"},
		{Itag: 3, Bitrate: 2000, URL: "https://already-direct"},
	}
	f, ok := bestCipher(formats)
	if !ok || f.SignatureCipher != "s=high" {
		t.Fatalf("got %+v, %v", f, ok)
	}
	if _, ok := bestCipher(nil); ok {
		t.Fatal("no formats should report not found")
	}
}

func TestNInPath(t *testing.T) {
	m := nInPath.FindStringSubmatch("https://manifest.googlevideo.com/api/manifest/hls_variant/.../n/SOME_CHALLENGE/.../file/index.m3u8")
	if m == nil || m[1] != "SOME_CHALLENGE" {
		t.Fatalf("got %v", m)
	}
	if nInPath.FindStringSubmatch("https://example.com/no/challenge/here") != nil {
		t.Fatal("should not match a URL with no n segment")
	}
}
