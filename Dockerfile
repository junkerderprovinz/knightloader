# KnightLoader — a single static Go binary with the UI embedded, plus the two
# external tools its media path uses (yt-dlp for extraction, ffmpeg for muxing).
# The frontend is built into web/dist and committed, so no Node stage is needed.

FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build
WORKDIR /src

# Warm the module cache first so source edits don't refetch dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
# The source revision, and it has to be passed IN rather than read here: the
# .dockerignore beside this file excludes .git, so the Go toolchain in this
# stage has no repository to read and stamps no vcs.revision of its own
# (buildinfo.Revision documents the whole arrangement). Left unset the binary
# simply does not know its commit and says so - an empty default is correct
# here, a made-up one would not be.
#
#   docker build --build-arg VERSION=preview --build-arg COMMIT=$(git rev-parse HEAD) .
#
# AND IT SAYS SO WHEN IT IS MISSING. An empty default is the correct VALUE, but
# a silent one is how the documented preview build spent its life producing
# images that answered {"commit":""}: nothing failed, nothing was logged, and
# the only symptom was a crest on the About card that would not turn. A line in
# the build log is what turns "wrong" into "noticed". It is a warning and not an
# error on purpose - building without a repository to hand is legitimate, and a
# build that refuses to finish over a missing identifier would be worse than the
# identifier being missing.
ARG COMMIT=
# Pure Go (modernc SQLite), so a static binary needs no cgo.
RUN [ -n "${COMMIT}" ] || echo 'WARNING: no --build-arg COMMIT, so this image will not know its revision: GET /api/health answers an empty commit and the About crest cannot turn' >&2; \
    CGO_ENABLED=0 go build \
      -ldflags="-s -w -X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=${VERSION} -X github.com/junkerderprovinz/knightloader/internal/buildinfo.Commit=${COMMIT}" \
      -o /out/knightloader ./cmd/knightloader

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
# openjdk21-jre-headless: KL_PROVISION_JD (on by default, see main.go) downloads
# and runs a private headless JDownloader on first start so an encrypted DLC/
# container link works with no separate JD sidecar to set up - that JVM needs
# a `java` on PATH, which this image did not carry before (provision.Ensure
# failed silently with "no Java runtime found" and the app just ran without a
# jd backend, the exact gap behind "container is encrypted ... none is
# configured" on first use).
RUN apk add --no-cache ca-certificates yt-dlp ffmpeg tzdata openjdk21-jre-headless \
    && adduser -D -u 1000 knight
COPY --from=build /out/knightloader /usr/local/bin/knightloader

# Data (SQLite, settings, encrypted accounts) and downloads live on volumes.
ENV KL_DATA=/data \
    KL_ADDR=:8749 \
    KL_YTDLP=/usr/bin/yt-dlp
VOLUME ["/data"]
EXPOSE 8749

# Click'n'Load is ON by default here, as it is in the binary (jdp, 2026-09-07:
# "Warum ist das CnL Modul standardmäßig deaktiviert? können wir das nicht
# standardmäßig aktivieren"). The image used to set KL_CNL=0 and that was the
# whole reason the switch looked off on every container install.
#
# It still binds 127.0.0.1 only, which is the part worth being precise about: a
# browser on ANOTHER machine cannot reach it, because the "Click'n'Load" button
# on a hoster page posts to localhost by definition. What it does serve is a
# browser running on the same host as the container (and the bridge, see
# docs/clicknload.md). For a browser elsewhere the extension is the path, and
# the Linkeingang card says so.
#
# Not bound to 0.0.0.0 to "fix" that: the Click'n'Load protocol carries no
# authentication of any kind, so a listener on the LAN is an open "add these
# links to your downloader" endpoint for everyone on the network.

USER knight
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -qO- http://127.0.0.1:8749/api/health >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/usr/local/bin/knightloader"]
