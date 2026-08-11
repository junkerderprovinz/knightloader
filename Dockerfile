# KnightLoader — a single static Go binary with the UI embedded, plus the two
# external tools its media path uses (yt-dlp for extraction, ffmpeg for muxing).
# The frontend is built into web/dist and committed, so no Node stage is needed.

FROM golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build
WORKDIR /src

# Warm the module cache first so source edits don't refetch dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
# Pure Go (modernc SQLite), so a static binary needs no cgo.
RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w -X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=${VERSION}" \
      -o /out/knightloader ./cmd/knightloader

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk add --no-cache ca-certificates yt-dlp ffmpeg tzdata \
    && adduser -D -u 1000 knight
COPY --from=build /out/knightloader /usr/local/bin/knightloader

# Data (SQLite, settings, encrypted accounts) and downloads live on volumes.
ENV KL_DATA=/data \
    KL_ADDR=:8749 \
    KL_YTDLP=/usr/bin/yt-dlp
VOLUME ["/data"]
EXPOSE 8749

# Click'n'Load binds 127.0.0.1 only, so it is off by default in a container;
# set KL_CNL to a port and publish it if you want browser integration.
ENV KL_CNL=0

USER knight
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -qO- http://127.0.0.1:8749/api/health >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/usr/local/bin/knightloader"]
