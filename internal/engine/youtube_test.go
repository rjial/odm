package engine

import (
	"strings"
	"testing"

	"odm/internal/ytdlp"
)

func TestYtDlpChoicesFromInfo(t *testing.T) {
	info := &ytdlp.Info{
		Title: "A Video",
		Formats: []ytdlp.Format{
			{FormatID: "137", Height: 1080, VCodec: "avc1", ACodec: "none", Filesize: 900},
			{FormatID: "248", Height: 1080, VCodec: "vp9", ACodec: "none", Filesize: 850}, // lower estimate, same height
			{FormatID: "136", Height: 720, VCodec: "avc1", ACodec: "none", Filesize: 500},
			{FormatID: "140", Height: 0, VCodec: "none", ACodec: "mp4a.40.2", ABR: 128, Filesize: 50},
			{FormatID: "251", Height: 0, VCodec: "none", ACodec: "opus", ABR: 160, Language: "en", Filesize: 60},
		},
	}
	c := ytDlpChoicesFromInfo(info)
	if c.Title != "A Video" {
		t.Fatalf("title: %q", c.Title)
	}
	if len(c.Qualities) != 3 { // 1080p, 720p, Audio only
		t.Fatalf("qualities: %+v", c.Qualities)
	}
	if c.Qualities[0].Label != "1080p" || c.Qualities[0].Height != 1080 || c.Qualities[0].Estimate != 900 {
		t.Fatalf("best 1080p: %+v", c.Qualities[0])
	}
	if c.Qualities[1].Label != "720p" {
		t.Fatalf("second quality: %+v", c.Qualities[1])
	}
	last := c.Qualities[len(c.Qualities)-1]
	if last.Label != "Audio only" || last.Height != 0 {
		t.Fatalf("audio-only entry: %+v", last)
	}

	// Every quality shares the same audio list: best-first, then the
	// highest-bitrate track first among the specific ones.
	for _, q := range c.Qualities {
		if len(q.Audios) != 3 {
			t.Fatalf("audios for %q: %+v", q.Label, q.Audios)
		}
		if q.Audios[0].Label != "Best available" || q.Audios[0].FormatID != "" {
			t.Fatalf("first audio choice: %+v", q.Audios[0])
		}
		if q.Audios[1].FormatID != "251" || !strings.Contains(q.Audios[1].Label, "160 kbps") {
			t.Fatalf("highest-bitrate audio: %+v", q.Audios[1])
		}
		if q.Audios[2].FormatID != "140" {
			t.Fatalf("second audio: %+v", q.Audios[2])
		}
	}
}

func TestYtDlpChoicesJob(t *testing.T) {
	c := &YtDlpChoices{
		Title: "A Video",
		Qualities: []YtDlpQuality{
			{
				Label: "1080p", Height: 1080, Estimate: 900,
				Audios: []YtDlpAudio{{Label: "Best available"}, {Label: "160 kbps · opus · en", FormatID: "251"}},
			},
			{Label: "Audio only", Height: 0, Audios: []YtDlpAudio{{Label: "Best available"}}},
		},
	}

	j, err := c.Job("https://youtube.com/watch?v=x", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if j.URL != "https://youtube.com/watch?v=x" || j.Title != "A Video" || j.Estimate != 900 {
		t.Fatalf("%+v", j)
	}
	if j.Format != "bestvideo[height<=1080]+bestaudio/best[height<=1080]" {
		t.Fatalf("default audio selector: %q", j.Format)
	}

	j, err = c.Job("u", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if j.Format != "bestvideo[height<=1080]+251/best[height<=1080]" {
		t.Fatalf("specific audio selector: %q", j.Format)
	}

	j, err = c.Job("u", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if j.Format != "bestaudio/best" {
		t.Fatalf("audio-only selector: %q", j.Format)
	}

	if _, err := c.Job("u", -1, 0); err == nil {
		t.Fatal("negative index should fail")
	}
	if _, err := c.Job("u", 2, 0); err == nil {
		t.Fatal("out of range index should fail")
	}
}
