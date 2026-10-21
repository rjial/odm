# Builds ODM's Linux desktop binary (Fyne UI, needs cgo) in a clean,
# reproducible environment, independent of whatever GL/Wayland packages (or
# conflicts) the host already has. Mirrors .github/workflows/release.yml's
# Linux job: same base, same apt packages.
#
# Usage:
#   docker build -f docker/build.Dockerfile -o dist .
#   ./dist/odm -version
#
# To also run the test suite (including the "fyne"-tagged UI package) in
# the same environment instead of building:
#   docker build -f docker/build.Dockerfile --target test .

FROM golang:1.23-bookworm AS deps
RUN apt-get update && apt-get install -y --no-install-recommends \
      gcc libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM deps AS test
RUN gofmt -l . | tee /tmp/gofmt.out && test ! -s /tmp/gofmt.out
RUN go vet -tags fyne ./...
RUN go test -tags fyne ./...

FROM deps AS build
ARG VERSION=dev
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -tags fyne -trimpath \
    -ldflags "-s -w -X odm/internal/version.Version=${VERSION}" \
    -o /out/odm ./cmd/odm

FROM scratch AS default
COPY --from=build /out/odm /odm
