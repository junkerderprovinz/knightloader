# KnightLoader: one static Go binary with the UI embedded, plus yt-dlp and
# ffmpeg for the media path. web/dist is committed, so there is no Node stage.

FROM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build
WORKDIR /src

# Warm the module cache first so source edits don't refetch dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
# .dockerignore excludes .git, so the revision has to be passed in:
#
#   docker build --build-arg VERSION=preview --build-arg COMMIT=$(git rev-parse HEAD) .
#
# Without it the build warns but finishes, and the binary reports an empty
# commit.
ARG COMMIT=
# Pure Go (modernc SQLite), so a static binary needs no cgo.
RUN [ -n "${COMMIT}" ] || echo 'WARNING: no --build-arg COMMIT, so this image will not know its revision: GET /api/health answers an empty commit and the About crest cannot turn' >&2; \
    CGO_ENABLED=0 go build \
      -ldflags="-s -w -X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=${VERSION} -X github.com/junkerderprovinz/knightloader/internal/buildinfo.Commit=${COMMIT}" \
      -o /out/knightloader ./cmd/knightloader

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
# The JRE runs the private headless JDownloader that KL_PROVISION_JD starts by
# default, which DLC and other container links need.
RUN apk add --no-cache ca-certificates yt-dlp ffmpeg tzdata openjdk21-jre-headless \
    && adduser -D -u 1000 knight
COPY --from=build /out/knightloader /usr/local/bin/knightloader

# Data (SQLite, settings, encrypted accounts) and downloads live on volumes.
ENV KL_DATA=/data \
    KL_ADDR=:8749 \
    KL_YTDLP=/usr/bin/yt-dlp
VOLUME ["/data"]
EXPOSE 8749

# Click'n'Load is on and binds 127.0.0.1 only, so it serves a browser on the
# same host and the bridge (docs/clicknload.md); other machines use the
# extension. The protocol has no authentication, so binding 0.0.0.0 would let
# anyone on the LAN add links.

USER knight
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -qO- http://127.0.0.1:8749/api/health >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/usr/local/bin/knightloader"]
