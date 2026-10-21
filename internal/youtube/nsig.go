package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"odm/internal/noderuntime"
	"odm/internal/youtube/vendor"
)

// solverResponse is yt-dlp/ejs's jsc() output, reduced to what SolveN
// needs. One request in, one response out, in the same order.
type solverResponse struct {
	Type      string `json:"type"` // "result" or "error"
	Error     string `json:"error"`
	Responses []struct {
		Type  string            `json:"type"` // "result" or "error"
		Error string            `json:"error"`
		Data  map[string]string `json:"data"`
	} `json:"responses"`
}

// SolveN solves playerJS's "n" challenge for challenge.
func SolveN(ctx context.Context, nodeBin, playerJS, challenge string) (string, error) {
	return solveChallenge(ctx, nodeBin, playerJS, "n", challenge)
}

// solveSig solves playerJS's legacy signature ("sig") challenge for
// challenge — the s= value inside a signatureCipher. Not exercised by any
// response seen while building this (every client tried either gave a
// ready-to-use URL or no URL at all, never a signatureCipher), but it is
// the same mechanism as SolveN, which is exercised, so it is kept for
// whatever client/video combination does hit it.
func solveSig(ctx context.Context, nodeBin, playerJS, challenge string) (string, error) {
	return solveChallenge(ctx, nodeBin, playerJS, "sig", challenge)
}

// solveChallenge runs yt-dlp/ejs's solver (internal/youtube/vendor)
// through Node.js — the same script yt-dlp itself runs, through the same
// kind of real JS engine. A pure-Go JS engine (goja) was tried first and
// measured against this exact shape of request: it was still running after
// three minutes and 5+ CPU cores on a real player.js, against Node's
// instant result, so this is not a style preference — the obfuscation
// needs a real engine.
func solveChallenge(ctx context.Context, nodeBin, playerJS, kind, challenge string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"type":                "player",
		"player":              playerJS,
		"requests":            []map[string]any{{"type": kind, "challenges": []string{challenge}}},
		"output_preprocessed": true,
	})
	if err != nil {
		return "", err
	}
	var script strings.Builder
	script.WriteString(vendor.Lib)
	script.WriteString("\nObject.assign(globalThis, lib);\n")
	script.WriteString(vendor.Core)
	script.WriteString("\nconsole.log(JSON.stringify(jsc(")
	script.Write(payload)
	script.WriteString(")));\n")

	out, err := noderuntime.Run(ctx, nodeBin, script.String())
	if err != nil {
		return "", fmt.Errorf("solving the %s challenge: %w", kind, err)
	}
	var resp solverResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		return "", fmt.Errorf("solving the %s challenge: unreadable output: %w", kind, err)
	}
	if resp.Type == "error" {
		return "", fmt.Errorf("solving the %s challenge: %s", kind, resp.Error)
	}
	if len(resp.Responses) == 0 {
		return "", fmt.Errorf("solving the %s challenge: no response", kind)
	}
	r := resp.Responses[0]
	if r.Type == "error" {
		return "", fmt.Errorf("solving the %s challenge: %s", kind, r.Error)
	}
	solved, ok := r.Data[challenge]
	if !ok || solved == "" {
		return "", fmt.Errorf("solving the %s challenge: no answer for it", kind)
	}
	return solved, nil
}
