package youtube

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// mimeExt turns a streamingData format's mimeType ("video/mp4; codecs=...")
// into a file extension ("mp4"). "" when it cannot tell.
func mimeExt(mimeType string) string {
	main, _, _ := strings.Cut(mimeType, ";")
	_, sub, ok := strings.Cut(main, "/")
	if !ok {
		return ""
	}
	return strings.TrimSpace(sub)
}

// bestDirect returns the best already-usable (no cipher, no SABR-only
// placeholder) format among formats, preferring one with both audio and
// video (muxed: a mimeType whose codecs list has more than one entry) and
// the highest bitrate.
func bestDirect(formats []Format) (Format, bool) {
	var best Format
	have := false
	for _, f := range formats {
		if f.URL == "" {
			continue
		}
		if !have || betterDirect(f, best) {
			best, have = f, true
		}
	}
	return best, have
}

func betterDirect(a, b Format) bool {
	if muxed(a) != muxed(b) {
		return muxed(a) // muxed (has its own audio) wins: nothing left to join
	}
	return a.Bitrate > b.Bitrate
}

func muxed(f Format) bool {
	return strings.Count(f.MimeType, "codecs") > 0 && strings.Count(f.MimeType, ",") > 0
}

// bestCipher is bestDirect's counterpart for formats that still need their
// signatureCipher solved.
func bestCipher(formats []Format) (Format, bool) {
	var best Format
	have := false
	for _, f := range formats {
		if f.SignatureCipher == "" {
			continue
		}
		if !have || f.Bitrate > best.Bitrate {
			best, have = f, true
		}
	}
	return best, have
}

// resolveCipher decodes a signatureCipher field into a ready-to-use URL:
// it is itself a query string carrying the real url, the signature to
// solve (s) and the name of the query parameter it belongs in (sp,
// "signature" when absent, the pre-2017 default).
func resolveCipher(ctx context.Context, nodeBin, playerJS, cipher string) (string, error) {
	values, err := url.ParseQuery(cipher)
	if err != nil {
		return "", fmt.Errorf("signatureCipher: %w", err)
	}
	raw := values.Get("url")
	s := values.Get("s")
	if raw == "" || s == "" {
		return "", fmt.Errorf("signatureCipher: missing url or s")
	}
	sp := values.Get("sp")
	if sp == "" {
		sp = "signature"
	}
	solved, err := solveSig(ctx, nodeBin, playerJS, s)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("signatureCipher: bad url: %w", err)
	}
	q := u.Query()
	q.Set(sp, solved)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
