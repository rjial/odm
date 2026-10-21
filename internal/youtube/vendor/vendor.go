// Package vendor embeds yt-dlp/ejs's challenge-solver scripts: the same
// ones yt-dlp itself downloads and runs through node/deno/bun/quickjs to
// solve the "n" parameter YouTube's streaming URLs carry. ODM runs them
// through Node.js (see internal/youtube/nsig.go); it does not re-derive
// this logic itself, so an update here is "fetch the next release," not a
// rewrite.
//
// Source: https://github.com/yt-dlp/ejs (Unlicense), release 0.8.0
// (2026-03-17), the `*.min.js` assets, last checked against that release
// on 2026-10-20.
package vendor

import _ "embed"

//go:embed yt.solver.lib.min.js
var Lib string

//go:embed yt.solver.core.min.js
var Core string

// Version is the yt-dlp/ejs release these scripts were fetched from.
const Version = "0.8.0"
