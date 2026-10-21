#!/usr/bin/env bash
# Build ODM.
#
# Desktop builds use the Fyne UI (build tag "fyne") and therefore need cgo:
#   Windows  -> mingw-w64            (CC_WINDOWS, default x86_64-w64-mingw32-gcc)
#   Linux    -> libgl1-mesa-dev xorg-dev
#   macOS    -> Xcode command line tools
# The "cli" target builds the engine + web UI with no cgo and no desktop UI.
set -euo pipefail
cd "$(dirname "$0")/.."

OUT="${OUT:-dist}"
mkdir -p "$OUT"

desktop() {
  local goos="$1" goarch="$2" name="$3" ldflags="-s -w"
  [ "$goos" = "windows" ] && ldflags="-s -w -H=windowsgui"
  echo "building desktop $goos/$goarch -> $OUT/$name"
  if [ "$goos" = "windows" ]; then
    CGO_ENABLED=1 CC="${CC_WINDOWS:-x86_64-w64-mingw32-gcc}" GOOS="$goos" GOARCH="$goarch" \
      go build -tags fyne -trimpath -ldflags "$ldflags" -o "$OUT/$name" ./cmd/odm
  else
    CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" \
      go build -tags fyne -trimpath -ldflags "$ldflags" -o "$OUT/$name" ./cmd/odm
  fi
  echo "built $OUT/$name"
}

cli() {
  local goos="$1" goarch="$2" name="$3"
  echo "building cli $goos/$goarch -> $OUT/$name"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w" -o "$OUT/$name" ./cmd/odm
  echo "built $OUT/$name"
}

case "${1:-windows}" in
  windows) desktop windows amd64 ODM.exe ;;
  linux) desktop linux amd64 odm ;;
  darwin) desktop darwin arm64 odm ;;
  darwin-intel) desktop darwin amd64 odm ;;
  cli) cli linux amd64 odm-cli ;;
  all)
    desktop windows amd64 ODM.exe
    desktop linux amd64 odm
    desktop darwin arm64 odm
    ;;
  *) echo "usage: $0 [windows|linux|darwin|darwin-intel|cli|all]" >&2; exit 2 ;;
esac
