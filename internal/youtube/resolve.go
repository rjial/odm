package youtube

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"odm/internal/noderuntime"
)

// nInPath matches the "n" challenge YouTube's manifest/segment URLs carry
// in their path (not a query parameter): .../n/<challenge>/... — the same
// pattern yt-dlp's own get_manifest_n_challenge uses.
var nInPath = regexp.MustCompile(`/n/([^/]+)/`)

// httpTimeout bounds each network step (watch page, player script,
// Innertube) so a slow/unreachable host fails fast rather than hanging the
// New Download probe.
const httpTimeout = 15 * time.Second

var sharedClient = &http.Client{Timeout: httpTimeout}

// Result is what Resolve found: exactly one of DirectURL (a file, ready to
// hand to ODM's ordinary download path, no stream engine involved) or
// ManifestURL (an HLS playlist, for the existing stream engine) is set.
type Result struct {
	Title       string
	Ext         string // only meaningful with DirectURL
	DirectURL   string
	ManifestURL string
}

// Resolve finds a way to download watchURL without a PO Token (BotGuard),
// trying each client in clients.go in turn. In order of preference, since
// each skips work the next would otherwise need:
//  1. A format already carrying a ready-to-use URL (seen in practice: the
//     legacy muxed itag 18, 360p, needs no JS at all).
//  2. A format carrying a signatureCipher: solved via Node.js (see
//     resolveCipher) — not observed from any client tried while building
//     this, kept for whatever combination does hit it.
//  3. An HLS manifest: its "n" query solved via Node.js (see SolveN) —
//     mainly live streams, as those are the formats observed to still
//     offer hlsManifestUrl outside of a PO Token.
//
// Everything else a client offers (adaptiveFormats with neither field,
// serverAbrStreamingUrl) is the SABR/UMP protocol this package does not
// speak — PO Token territory, left to the yt-dlp fallback.
//
// Any error here means "try the yt-dlp fallback instead" to the caller
// (internal/engine.ProbeYouTube): none of these failure modes are specific
// enough to one video to tell the download-level caller more than that.
func Resolve(ctx context.Context, nodePath, watchURL string) (*Result, error) {
	videoID, ok := IsWatchURL(watchURL)
	if !ok {
		return nil, fmt.Errorf("not a YouTube video URL")
	}

	var lastErr error
	var playerURL, playerJS, nodeBin string
	nodeLocated := false

	for _, c := range hlsClients {
		pr, err := fetchPlayer(ctx, sharedClient, c, videoID)
		if err != nil {
			lastErr = err
			continue
		}
		if pr.PlayabilityStatus.Status != "OK" {
			lastErr = fmt.Errorf("%s (%s client)", playabilityMessage(pr), c.Name)
			continue
		}

		if f, ok := bestDirect(pr.StreamingData.Formats); ok {
			return &Result{Title: pr.VideoDetails.Title, Ext: mimeExt(f.MimeType), DirectURL: f.URL}, nil
		}

		needsNode := false
		cf, hasCipher := bestCipher(pr.StreamingData.Formats)
		if !hasCipher {
			cf, hasCipher = bestCipher(pr.StreamingData.AdaptiveFormats)
		}
		hlsManifest := pr.StreamingData.HLSManifestURL
		nMatch := nInPath.FindStringSubmatchIndex(hlsManifest)
		needsNode = hasCipher || (hlsManifest != "" && nMatch != nil)

		if hlsManifest != "" && nMatch == nil {
			// No "n" to solve: usable without Node.js, whether or not a
			// cipher format was also found.
			return &Result{Title: pr.VideoDetails.Title, ManifestURL: hlsManifest}, nil
		}

		if needsNode && !nodeLocated {
			nodeBin, err = noderuntime.Locate(nodePath)
			nodeLocated = true
			if err == nil {
				playerURL, err = fetchWatchPage(ctx, sharedClient, videoID)
			}
			if err == nil {
				playerJS, err = FetchPlayerJS(ctx, sharedClient, playerURL)
			}
			if err != nil {
				nodeBin = "" // marks Node.js as unusable for the rest of this call
				lastErr = err
			}
		}
		if needsNode && nodeBin == "" {
			lastErr = fmt.Errorf("%w (%s client)", lastErr, c.Name)
			continue
		}

		if hasCipher {
			direct, err := resolveCipher(ctx, nodeBin, playerJS, cf.SignatureCipher)
			if err != nil {
				lastErr = err
				continue
			}
			return &Result{Title: pr.VideoDetails.Title, Ext: mimeExt(cf.MimeType), DirectURL: direct}, nil
		}

		if hlsManifest != "" {
			if nMatch == nil {
				return &Result{Title: pr.VideoDetails.Title, ManifestURL: hlsManifest}, nil
			}
			challenge := hlsManifest[nMatch[2]:nMatch[3]]
			solved, err := SolveN(ctx, nodeBin, playerJS, challenge)
			if err != nil {
				lastErr = err
				continue
			}
			hlsManifest = hlsManifest[:nMatch[2]] + solved + hlsManifest[nMatch[3]:]
			return &Result{Title: pr.VideoDetails.Title, ManifestURL: hlsManifest}, nil
		}

		lastErr = fmt.Errorf("no usable format without a PO Token (%s client)", c.Name)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no client configured")
	}
	return nil, lastErr
}
