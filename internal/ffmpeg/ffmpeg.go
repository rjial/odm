// Package ffmpeg finds an ffmpeg binary and uses it to join a video stream
// and an audio stream into one file without re-encoding.
//
// ODM does not ship ffmpeg; it is optional and only needed for formats that
// come as separate video and audio streams (most adaptive HLS/DASH sources).
//
// Gotchas:
//   - ODM on Windows is a GUI program; a console child flashes a window
//     unless it is started hidden (see hide in ffmpeg_windows.go).
//   - The output is written under a temporary name and renamed when ffmpeg
//     succeeds, so a failed or cancelled merge never leaves a half file
//     with the final name. The muxer is passed with -f because the
//     temporary name has no extension ffmpeg recognises.
//   - "-c copy" only works when the codecs fit the container (H.264/AV1 +
//     AAC in MP4, VP9 + Opus in WebM). The container follows the output
//     name (see muxer); there is no retry in another one, so a pair that
//     does not fit fails with ffmpeg's error. Stream downloads ask for
//     WebM when both tracks are WebM and MP4 otherwise; Matroska is only
//     used for an output name that is neither.
package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNotFound means no ffmpeg was configured, bundled or on PATH.
var ErrNotFound = errors.New("ffmpeg not found: install it or set its path in Options")

func exeName() string {
	if runtime.GOOS == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

// Locate returns the ffmpeg to use, in order: the configured path (a file,
// or a folder containing ffmpeg), one next to the ODM executable, then PATH.
func Locate(configured string) (string, error) {
	if configured = strings.TrimSpace(configured); configured != "" {
		if st, err := os.Stat(configured); err == nil && st.IsDir() {
			configured = filepath.Join(configured, exeName())
		}
		if st, err := os.Stat(configured); err == nil && !st.IsDir() {
			return configured, nil
		}
		return "", fmt.Errorf("ffmpeg not found at %s", configured)
	}
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), exeName())
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	return "", ErrNotFound
}

// Version returns the first line of "ffmpeg -version", for Options.
func Version(ctx context.Context, bin string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "-version")
	hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line), nil
}

// muxer maps the output extension to ffmpeg's muxer name.
func muxer(out string) string {
	switch strings.ToLower(filepath.Ext(out)) {
	case ".mp4", ".m4a", ".m4v", ".mov":
		return "mp4"
	case ".webm":
		return "webm"
	}
	return "matroska"
}

// genptsArgs asks ffmpeg to regenerate presentation timestamps instead of
// trusting the ones in the input: a join across an HLS EXT-X-DISCONTINUITY
// (encoding parameters reset mid-stream, e.g. an ad break) otherwise keeps
// whatever jump or reset was there, which can desync audio and video from
// that point on. Harmless to pass when nothing needs repairing.
func genptsArgs(repairTimeline bool) []string {
	if repairTimeline {
		return []string{"-fflags", "+genpts"}
	}
	return nil
}

func mergeArgs(video, audio, tmp, format string, repairTimeline bool) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}
	args = append(args, genptsArgs(repairTimeline)...)
	return append(args,
		"-i", video, "-i", audio,
		"-map", "0:v:0", "-map", "1:a:0",
		"-c", "copy",
		"-f", format, tmp,
	)
}

// Merge joins video and audio into out. The inputs are left in place; the
// caller removes them once it no longer needs them for a retry.
// repairTimeline: see genptsArgs.
func Merge(ctx context.Context, bin, video, audio, out string, repairTimeline bool) error {
	tmp := out + ".merging"
	return run(ctx, bin, mergeArgs(video, audio, tmp, muxer(out), repairTimeline), nil, tmp, out, "merge")
}

// remuxArgs copies the video and audio of an MPEG-TS read from standard
// input into another container. Only audio and video are mapped: an
// MPEG-TS stream often carries a timed ID3 metadata track, which MP4
// cannot hold and would make ffmpeg fail. No -nostdin here: the input is
// standard input (ffmpeg turns off its keyboard handling by itself then).
func remuxArgs(tmp, format string, repairTimeline bool) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	args = append(args, genptsArgs(repairTimeline)...)
	return append(args,
		"-f", "mpegts", "-i", "pipe:0",
		"-map", "0:v?", "-map", "0:a?",
		"-c", "copy",
		"-f", format, tmp,
	)
}

// RemuxTS rewrites the MPEG-TS read from in into the container out's name
// asks for, without re-encoding (a recording into MP4, which more players
// open and seek in). Reading from in rather than a file saves writing the
// joined MPEG-TS to disk first. repairTimeline: see genptsArgs.
func RemuxTS(ctx context.Context, bin string, in io.Reader, out string, repairTimeline bool) error {
	tmp := out + ".remuxing"
	return run(ctx, bin, remuxArgs(tmp, muxer(out), repairTimeline), in, tmp, out, "remux")
}

func repairArgs(in, tmp, format string) []string {
	return []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-fflags", "+genpts",
		"-i", in,
		"-c", "copy",
		"-f", format, tmp,
	}
}

// Repair re-mixes a single already-complete file back into its own
// container (-c copy: no re-encode), regenerating timestamps the same way
// Merge/RemuxTS can. For the one assembly path that otherwise never calls
// ffmpeg at all (a single track, concatenated as is): the only way to
// still repair a discontinuity there is this extra pass.
func Repair(ctx context.Context, bin, in, out string) error {
	tmp := out + ".repairing"
	return run(ctx, bin, repairArgs(in, tmp, muxer(out)), nil, tmp, out, "repair")
}

// run runs ffmpeg writing tmp and renames it to out on success. On failure
// tmp is removed and the error carries the end of ffmpeg's message.
func run(ctx context.Context, bin string, args []string, stdin io.Reader, tmp, out, what string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	hide(cmd)
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 { // keep the end: that is where ffmpeg says why
			msg = "…" + msg[len(msg)-300:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s failed: %s", what, msg)
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
