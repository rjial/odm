// Package youtube extracts an HLS manifest for a YouTube watch-page URL
// without a PO Token (BotGuard), by using a client Google's current
// GVS_PO_TOKEN_POLICY does not require one for over HLS, and solving that
// manifest's "n" parameter the same way yt-dlp does (see nsig.go).
//
// This is deliberately narrow: no DASH, no PO-Token-gated formats, no
// sign-in. Anything outside that falls to the yt-dlp fallback in
// internal/engine (see ProbeYouTube). It will also need upkeep as YouTube
// changes client behavior — see clients.go's per-client comments for what
// was last verified and when.
package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// innertubeAPIKey is YouTube's long-standing public Innertube key, used by
// every client below; it identifies the API surface, not a user.
const innertubeAPIKey = "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8"

var watchHostPattern = regexp.MustCompile(`(?i)^(?:www\.|m\.|music\.)?youtube\.com$|^youtu\.be$`)

// IsWatchURL reports whether raw is a YouTube single-video URL, and the
// video ID if so.
func IsWatchURL(raw string) (videoID string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if !watchHostPattern.MatchString(host) {
		return "", false
	}
	if strings.EqualFold(host, "youtu.be") {
		id := strings.Trim(u.Path, "/")
		if id == "" {
			return "", false
		}
		return id, true
	}
	if u.Path != "/watch" {
		return "", false
	}
	id := u.Query().Get("v")
	if id == "" {
		return "", false
	}
	return id, true
}

// Format is one entry of streamingData.formats or .adaptiveFormats. Most of
// these, on the clients this package uses, carry neither URL nor
// SignatureCipher at all — YouTube now serves them only through the
// opaque, PO-Token-gated SABR/UMP protocol (ServerAbrStreamingUrl), which
// this package does not attempt to speak. A format is only usable here
// when it still carries one of the other two.
type Format struct {
	Itag            int    `json:"itag"`
	MimeType        string `json:"mimeType"`
	Bitrate         int    `json:"bitrate"`
	URL             string `json:"url"`             // ready to use as-is
	SignatureCipher string `json:"signatureCipher"` // needs solveCipher
}

// PlayerResponse is what ResolveHLS needs from Innertube's player endpoint,
// reduced from everything it actually returns.
type PlayerResponse struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	StreamingData struct {
		HLSManifestURL     string   `json:"hlsManifestUrl"`
		Formats            []Format `json:"formats"`
		AdaptiveFormats    []Format `json:"adaptiveFormats"`
		ServerABRStreaming string   `json:"serverAbrStreamingUrl"`
	} `json:"streamingData"`
	VideoDetails struct {
		Title string `json:"title"`
	} `json:"videoDetails"`
}

// fetchPlayer asks one Innertube client for videoID's player response.
func fetchPlayer(ctx context.Context, hc *http.Client, c client, videoID string) (*PlayerResponse, error) {
	body, err := json.Marshal(map[string]any{
		"videoId": videoID,
		"context": map[string]any{"client": c.context()},
	})
	if err != nil {
		return nil, err
	}
	endpoint := "https://www.youtube.com/youtubei/v1/player?key=" + innertubeAPIKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("X-Youtube-Client-Name", c.ClientNameID)
	req.Header.Set("X-Youtube-Client-Version", c.ClientVersion)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("innertube (%s client): %s", c.Name, resp.Status)
	}
	var pr PlayerResponse
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("innertube (%s client): unreadable response: %w", c.Name, err)
	}
	return &pr, nil
}

func playabilityMessage(pr *PlayerResponse) string {
	if pr.PlayabilityStatus.Reason != "" {
		return pr.PlayabilityStatus.Reason
	}
	return pr.PlayabilityStatus.Status
}
