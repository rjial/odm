// Package ytdlp finds a yt-dlp binary and uses it to download and probe
// videos ODM's own YouTube extractor (internal/youtube) cannot handle: a
// sign-in/age-gate wall, a format that needs a PO Token, or any other site
// yt-dlp supports that ODM does not understand on its own. Entirely
// optional: those downloads simply are not offered if it is not found.
package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNotFound means no yt-dlp was configured, bundled or on PATH.
var ErrNotFound = errors.New("yt-dlp not found: install it or set its path in Options")

func exeName() string {
	if runtime.GOOS == "windows" {
		return "yt-dlp.exe"
	}
	return "yt-dlp"
}

// Locate returns the yt-dlp to use, in order: the configured path (a file,
// or a folder containing yt-dlp), one next to the ODM executable, then
// PATH.
func Locate(configured string) (string, error) {
	if configured = strings.TrimSpace(configured); configured != "" {
		if st, err := os.Stat(configured); err == nil && st.IsDir() {
			configured = filepath.Join(configured, exeName())
		}
		if st, err := os.Stat(configured); err == nil && !st.IsDir() {
			return configured, nil
		}
		return "", fmt.Errorf("yt-dlp not found at %s", configured)
	}
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), exeName())
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p, nil
	}
	return "", ErrNotFound
}

// Version returns the first line of "yt-dlp --version", for Options.
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

// Format is one of the formats a video is offered in.
type Format struct {
	FormatID       string  `json:"format_id"`
	FormatNote     string  `json:"format_note"`
	Ext            string  `json:"ext"`
	Height         int     `json:"height"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	ABR            float64 `json:"abr"` // audio bitrate, kbps; 0 for a video-only format
	Language       string  `json:"language"`
	Filesize       int64   `json:"filesize"`
	FilesizeApprox int64   `json:"filesize_approx"`
}

// HasAudio reports whether this format carries an audio track at all (a
// muxed format, or an audio-only one) — "none" is yt-dlp's own marker for
// "no track of this kind".
func (f Format) HasAudio() bool {
	return f.ACodec != "" && f.ACodec != "none"
}

// AudioOnly reports whether this format is audio with no video track.
func (f Format) AudioOnly() bool {
	return f.HasAudio() && (f.VCodec == "" || f.VCodec == "none")
}

// Size is the best size estimate this format carries, 0 when unknown.
func (f Format) Size() int64 {
	if f.Filesize > 0 {
		return f.Filesize
	}
	return f.FilesizeApprox
}

// Info is what Probe needs from yt-dlp's dump, reduced from everything it
// actually reports.
type Info struct {
	Title   string   `json:"title"`
	Formats []Format `json:"formats"`
}

// Probe lists a video's title and formats, for building a quality picker.
// The caller bounds how long this may take via ctx.
func Probe(ctx context.Context, bin, url string) (*Info, error) {
	cmd := exec.CommandContext(ctx, bin, "-J", "--no-playlist", "--no-warnings", url)
	hide(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("yt-dlp: %s", errTail(stderr.String(), err))
	}
	var info Info
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("yt-dlp: unreadable output: %w", err)
	}
	return &info, nil
}

// progressPrefix tags the one progress line Run asks yt-dlp to print per
// update, so it can be told apart from yt-dlp's own banners and warnings on
// the same stream.
const progressPrefix = "ODMPROGRESS:"

// progressTemplate: downloaded bytes : total bytes (0 if unknown) : total
// bytes estimate (0 if unknown), one line per update.
const progressTemplate = progressPrefix +
	"%(progress.downloaded_bytes)s:%(progress.total_bytes)s:%(progress.total_bytes_estimate)s"

// pathPrefix tags the line Run asks yt-dlp to print once the file is in
// its final place (after any merge/remux), carrying the exact path yt-dlp
// wrote to. The caller must not assume a path ahead of time: yt-dlp's own
// extension/merge-format logic decides the real name, and fighting it by
// forcing an exact -o path has been observed to make yt-dlp rewrite that
// path's own trailing component (e.g. appending the merge format's
// extension instead of replacing a non-media one), so the file Run reports
// here can differ from whatever -o template the caller passed.
const pathPrefix = "ODMPATH:"

// Run downloads url with yt-dlp, calling onProgress as it reports progress,
// and returns the absolute path of the file it finally wrote (after any
// merge/remux) on success. args are yt-dlp's own flags (format selection,
// output template, and so on); Run appends the ones that make its progress
// and final path machine-readable.
func Run(ctx context.Context, bin string, args []string, onProgress func(done, total int64)) (finalPath string, err error) {
	full := append(append([]string{}, args...),
		// yt-dlp suppresses all progress output when stdout isn't a TTY
		// (true here: it's a Go exec.Command pipe) unless --progress
		// forces it back on; --progress-template alone only sets the
		// format of a progress line that would otherwise never print.
		"--progress", "--newline", "--progress-template", progressTemplate,
		"--print", "after_move:"+pathPrefix+"%(filepath)s")
	cmd := exec.CommandContext(ctx, bin, full...)
	hide(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr errTailWriter
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if rest, ok := strings.CutPrefix(line, pathPrefix); ok {
			finalPath = rest
			continue
		}
		rest, ok := strings.CutPrefix(line, progressPrefix)
		if !ok || onProgress == nil {
			continue
		}
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) != 3 {
			continue
		}
		done := parseInt64(parts[0])
		total := parseInt64(parts[1])
		if total == 0 {
			total = parseInt64(parts[2])
		}
		onProgress(done, total)
	}
	err = cmd.Wait()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("yt-dlp: %s", errTail(stderr.String(), err))
	}
	if finalPath == "" {
		return "", fmt.Errorf("yt-dlp: finished but did not report the file it wrote")
	}
	return finalPath, nil
}

func parseInt64(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// errTailWriter keeps only the last few ERROR lines yt-dlp wrote to
// standard error: its failures are not reliably at the very end of the
// stream the way ffmpeg's are.
type errTailWriter struct {
	lines []string
	buf   bytes.Buffer
}

func (w *errTailWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err == io.EOF {
			w.buf.WriteString(line)
			break
		}
		line = strings.TrimRight(line, "\n")
		if strings.HasPrefix(line, "ERROR:") {
			w.lines = append(w.lines, line)
			if len(w.lines) > 5 {
				w.lines = w.lines[len(w.lines)-5:]
			}
		}
	}
	return len(p), nil
}

func (w *errTailWriter) String() string {
	if len(w.lines) > 0 {
		return strings.Join(w.lines, "; ")
	}
	return strings.TrimSpace(w.buf.String())
}

// errTail builds a one-line failure message from captured stderr and the
// process error.
func errTail(msg string, err error) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return err.Error()
	}
	return msg
}
