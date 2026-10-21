package youtube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
)

var jsURLPattern = regexp.MustCompile(`"jsUrl":"([^"]+\.js)"`)

// fetchWatchPage reads the ordinary watch page (a normal page load, not an
// Innertube call) just to find the current player JS URL. videoDetails and
// streamingData in this page's own ytInitialPlayerResponse are not used:
// the default "web" client response is SABR-only (confirmed empirically —
// adaptiveFormats with no url/signatureCipher, no hlsManifestUrl, only
// serverAbrStreamingUrl); FetchPlayer asks a different client for that.
func fetchWatchPage(ctx context.Context, hc *http.Client, videoID string) (playerURL string, err error) {
	u := "https://www.youtube.com/watch?v=" + url.QueryEscape(videoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("watch page: %s", resp.Status)
	}
	m := jsURLPattern.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("watch page: no player JS URL found (page layout changed?)")
	}
	ref, err := url.Parse(string(m[1]))
	if err != nil {
		return "", fmt.Errorf("watch page: bad player JS URL: %w", err)
	}
	base, _ := url.Parse("https://www.youtube.com")
	return base.ResolveReference(ref).String(), nil
}

// playerJSCache keeps the last few player scripts fetched (2-3 MB each),
// keyed by URL: the same player stays current across many videos until
// YouTube ships a new one, and re-fetching it for every video would be
// wasteful.
type playerJSCache struct {
	mu    sync.Mutex
	order []string
	data  map[string]string
}

const playerJSCacheSize = 4

var sharedPlayerJSCache = &playerJSCache{data: map[string]string{}}

func (c *playerJSCache) get(url string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	js, ok := c.data[url]
	return js, ok
}

func (c *playerJSCache) put(url, js string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[url]; !ok {
		c.order = append(c.order, url)
		for len(c.order) > playerJSCacheSize {
			delete(c.data, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.data[url] = js
}

// FetchPlayerJS returns playerURL's script, from the shared cache when
// already fetched.
func FetchPlayerJS(ctx context.Context, hc *http.Client, playerURL string) (string, error) {
	if js, ok := sharedPlayerJSCache.get(playerURL); ok {
		return js, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playerURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("player script: %s", resp.Status)
	}
	js := string(data)
	sharedPlayerJSCache.put(playerURL, js)
	return js, nil
}
