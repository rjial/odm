package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"odm/internal/ffmpeg"
	"odm/internal/youtube"
	"odm/internal/ytdlp"
)

// ytdlpSetting is the configured yt-dlp path (Settings.YtDlpPath), read
// from runYtDlp, which runs detached from the Manager (like ffmpegSetting
// in stream.go), hence atomic. Settings.NodePath has no equivalent: it is
// only ever needed from ProbeYouTube, which already has the Manager (and
// so Settings()) at hand.
var ytdlpSetting atomic.Pointer[string]

func setYtDlpPath(p string) { ytdlpSetting.Store(&p) }

func ytdlpConfigured() string {
	if p := ytdlpSetting.Load(); p != nil {
		return *p
	}
	return ""
}

// YtDlpJob is the persisted description of a download handed to the yt-dlp
// fallback: ODM's own YouTube extractor (internal/youtube) could not
// resolve it without a PO Token, or it is some other site yt-dlp supports
// that ODM does not understand on its own.
type YtDlpJob struct {
	URL      string `json:"url"`             // the page yt-dlp resolves itself
	Format   string `json:"format"`          // yt-dlp -f selector
	Title    string `json:"title,omitempty"` // used to name the file
	Estimate int64  `json:"estimate,omitempty"`
}

// YtDlpAudio is one audio track the yt-dlp fallback offers, mirroring
// StreamAudio. FormatID "" means "let yt-dlp pick the best one itself"
// (always the first entry, "Best available").
type YtDlpAudio struct {
	Label    string
	FormatID string
}

// YtDlpQuality is one video quality choice the yt-dlp fallback offers, and
// the audio tracks that go with it (the same list for every quality: a
// YouTube video's audio tracks don't depend on which video height was
// picked). Height 0 marks the trailing "Audio only" entry.
type YtDlpQuality struct {
	Label    string
	Height   int
	Estimate int64
	Audios   []YtDlpAudio
}

// YtDlpChoices is what the yt-dlp fallback offers, for the New Download
// window's quality picker — the same role StreamChoices has for HLS/DASH.
type YtDlpChoices struct {
	Title     string
	Qualities []YtDlpQuality
}

// Job returns the YtDlpJob for quality index q and audio index a (into
// q's Audios, ignored when it has none).
func (c *YtDlpChoices) Job(url string, q, a int) (YtDlpJob, error) {
	if q < 0 || q >= len(c.Qualities) {
		return YtDlpJob{}, fmt.Errorf("no such quality")
	}
	sq := c.Qualities[q]
	audio := "bestaudio"
	if a >= 0 && a < len(sq.Audios) && sq.Audios[a].FormatID != "" {
		audio = sq.Audios[a].FormatID
	}
	format := audio + "/best"
	if sq.Height > 0 {
		format = fmt.Sprintf("bestvideo[height<=%d]+%s/best[height<=%d]", sq.Height, audio, sq.Height)
	}
	return YtDlpJob{URL: url, Format: format, Title: c.Title, Estimate: sq.Estimate}, nil
}

// ytdlpAudioLabel describes one audio format for the picker: bitrate,
// codec and language, whichever of those it carries.
func ytdlpAudioLabel(f ytdlp.Format) string {
	var parts []string
	if f.ABR > 0 {
		parts = append(parts, fmt.Sprintf("%.0f kbps", f.ABR))
	}
	if f.ACodec != "" && f.ACodec != "none" {
		parts = append(parts, f.ACodec)
	}
	if f.Language != "" {
		parts = append(parts, f.Language)
	}
	if len(parts) == 0 {
		return f.FormatID
	}
	return strings.Join(parts, " · ")
}

// ytDlpChoicesFromInfo turns yt-dlp's probe dump into a quality list: one
// entry per distinct height (descending) plus a trailing audio-only entry,
// each offering the same audio tracks (best first, then every separate
// one yt-dlp lists), mirroring StreamChoices' Quality/Audio pair for
// HLS/DASH.
func ytDlpChoicesFromInfo(info *ytdlp.Info) *YtDlpChoices {
	heights := map[int]int64{} // height -> best estimate seen for it
	var audioFormats []ytdlp.Format
	for _, f := range info.Formats {
		switch {
		case f.Height > 0:
			if sz := f.Size(); sz > heights[f.Height] {
				heights[f.Height] = sz
			}
		case f.AudioOnly():
			audioFormats = append(audioFormats, f)
		}
	}
	sort.Slice(audioFormats, func(i, j int) bool { return audioFormats[i].ABR > audioFormats[j].ABR })
	audios := []YtDlpAudio{{Label: "Best available"}}
	seen := map[string]bool{}
	for _, f := range audioFormats {
		if seen[f.FormatID] {
			continue
		}
		seen[f.FormatID] = true
		audios = append(audios, YtDlpAudio{Label: ytdlpAudioLabel(f), FormatID: f.FormatID})
	}

	ordered := make([]int, 0, len(heights))
	for h := range heights {
		ordered = append(ordered, h)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ordered)))

	c := &YtDlpChoices{Title: info.Title}
	for _, h := range ordered {
		c.Qualities = append(c.Qualities, YtDlpQuality{
			Label: fmt.Sprintf("%dp", h), Height: h, Estimate: heights[h], Audios: audios,
		})
	}
	c.Qualities = append(c.Qualities, YtDlpQuality{Label: "Audio only", Audios: audios})
	return c
}

// YouTubeProbe is what ProbeYouTube found: exactly one of DirectURL, Stream
// or YtDlp is set.
type YouTubeProbe struct {
	Title string
	// DirectURL: a plain file, ready for the ordinary download path — no
	// stream engine, no yt-dlp. File carries its size/type, from probing
	// DirectURL the normal way.
	DirectURL string
	Ext       string
	File      *Probe
	// Stream: an HLS manifest ODM's existing stream engine can read.
	// ManifestURL is the resolved address (its "n" challenge already
	// solved) Stream.Job must be called with — not the original watch
	// page URL.
	Stream      *StreamChoices
	ManifestURL string
	// YtDlp: yt-dlp's own format choices, used whenever it is set up and
	// working — see ProbeYouTube.
	YtDlp *YtDlpChoices
}

// ProbeYouTube resolves watchURL. A configured, working yt-dlp is always
// preferred when available: ODM's own extractor (internal/youtube — no PO
// Token, no BotGuard) can only ever offer the single muxed low-quality
// format YouTube still serves without one, so there is no reason to settle
// for that over yt-dlp's real quality ladder when it is there to ask.
// ODM's own extractor is the one actually used only when yt-dlp is not set
// up, or fails on this particular video (yt-dlp has its own, separately
// maintained PO Token handling, so its failures here are usually a real
// wall — sign-in, age-gate — not something trying again differently would
// get past).
func (m *Manager) ProbeYouTube(ctx context.Context, watchURL string) (*YouTubeProbe, error) {
	st := m.Settings()

	var ytdlpErr error
	if bin, err := ytdlp.Locate(st.YtDlpPath); err != nil {
		ytdlpErr = err
	} else if info, err := ytdlp.Probe(ctx, bin, watchURL); err != nil {
		ytdlpErr = err
	} else {
		return &YouTubeProbe{Title: info.Title, YtDlp: ytDlpChoicesFromInfo(info)}, nil
	}

	res, nativeErr := youtube.Resolve(ctx, st.NodePath, watchURL)
	if nativeErr == nil {
		switch {
		case res.DirectURL != "":
			info, err := m.Probe(ctx, res.DirectURL, nil)
			if err == nil {
				return &YouTubeProbe{Title: res.Title, DirectURL: res.DirectURL, Ext: res.Ext, File: info}, nil
			}
			nativeErr = err
		case res.ManifestURL != "":
			choices, err := m.ProbeStream(ctx, res.ManifestURL, nil)
			if err == nil {
				return &YouTubeProbe{Title: res.Title, Stream: choices, ManifestURL: res.ManifestURL}, nil
			}
			nativeErr = err
		default:
			nativeErr = fmt.Errorf("no usable format")
		}
	}
	return nil, fmt.Errorf("yt-dlp: %v; ODM's own extractor: %v", ytdlpErr, nativeErr)
}

// ProbeYtDlp tries yt-dlp on any URL — not just YouTube — as the New
// Download window's last resort, after its own direct-file and
// stream-manifest probing both fail to recognize the address: yt-dlp
// supports roughly 1800 sites, so a URL ODM itself doesn't understand may
// still be one yt-dlp does.
func (m *Manager) ProbeYtDlp(ctx context.Context, rawURL string) (*YtDlpChoices, error) {
	bin, err := ytdlp.Locate(m.Settings().YtDlpPath)
	if err != nil {
		return nil, err
	}
	info, err := ytdlp.Probe(ctx, bin, rawURL)
	if err != nil {
		return nil, err
	}
	return ytDlpChoicesFromInfo(info), nil
}

// ytdlpWorkTemplate is the -o template Run is given: unique to this
// download (so a second one in the same folder cannot collide), but
// otherwise fully yt-dlp's own to decide — in particular the extension,
// which depends on the merge format and isn't known ahead of time. Forcing
// an exact final name here (ODM's usual "claim a name, write to
// <name>.odmpart" convention) was tried first and broke: yt-dlp's own
// extension-fixup rewrites a -o path whose trailing component isn't a
// container extension it recognises (our deliberate ".odmpart" marker
// counted as one), by appending its own rather than replacing it — so the
// file lands at a name Run never learns, and finish() errors not finding
// it. --print after_move:filepath (see internal/ytdlp.Run) sidesteps this
// by asking yt-dlp to simply report whatever it actually wrote.
func ytdlpWorkTemplate(dir, id string) string {
	return filepath.Join(dir, ".odm-ytdlp-"+id+".%(ext)s")
}

// removeYtDlpLeftovers cleans up this download's own temp/partial files
// (its -o template's matches) after a real failure. Left alone on pause:
// yt-dlp's own .part files under the same template let a later run
// continue instead of starting over.
func removeYtDlpLeftovers(dir, id string) {
	matches, _ := filepath.Glob(filepath.Join(dir, ".odm-ytdlp-"+id+".*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// runYtDlp is Start for a download handed to the yt-dlp fallback.
func (d *Download) runYtDlp(ctx context.Context) {
	d.mu.Lock()
	job := *d.YtDlp
	dir, id := d.Dir, d.ID
	d.mu.Unlock()

	bin, err := ytdlp.Locate(ytdlpConfigured())
	if err != nil {
		d.fail(err)
		return
	}

	args := []string{"-f", job.Format, "--no-playlist", "--merge-output-format", "mp4"}
	if ffBin, ffErr := ffmpeg.Locate(ffmpegConfigured()); ffErr == nil {
		args = append(args, "--ffmpeg-location", filepath.Dir(ffBin))
	}
	args = append(args, "-o", ytdlpWorkTemplate(dir, id), job.URL)

	finalPath, err := ytdlp.Run(ctx, bin, args, func(done, total int64) {
		d.mu.Lock()
		d.ytdlpDone = done
		if total > 0 {
			d.Total = total
		}
		d.mu.Unlock()
		d.notify(false)
	})

	if d.isRemoved() {
		if finalPath != "" {
			_ = os.Remove(finalPath)
		}
		removeYtDlpLeftovers(dir, id)
		return
	}
	switch {
	case err == nil:
		name := SanitizeName(job.Title) + filepath.Ext(finalPath)
		if job.Title == "" {
			name = filepath.Base(finalPath)
		}
		dest := filepath.Join(dir, name)
		if _, statErr := os.Stat(dest); statErr == nil {
			alt, ferr := freeName(dir, name)
			if ferr != nil {
				d.fail(ferr)
				return
			}
			name = alt
			dest = filepath.Join(dir, name)
		}
		if rerr := os.Rename(finalPath, dest); rerr != nil {
			d.fail(rerr)
			return
		}
		d.mu.Lock()
		d.FileName = name
		d.Status = StatusComplete
		if st, serr := os.Stat(dest); serr == nil {
			d.Total, d.ytdlpDone = st.Size(), st.Size()
		}
		d.mu.Unlock()
	case ctx.Err() != nil:
		d.mu.Lock()
		d.Status = StatusPaused
		d.mu.Unlock()
	default:
		removeYtDlpLeftovers(dir, id)
		d.fail(err)
	}
}

// httpTimeout bounds how long AddAuto waits to resolve a YouTube URL added
// with no New Download window (API, clipboard, a context-menu add with
// that window turned off) — long enough for Node.js plus a couple of
// network round trips, short enough not to hang the caller indefinitely.
const youtubeAutoProbeTimeout = 25 * time.Second
