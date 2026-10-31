# KnightLoader: one static Go binary with the UI embedded, plus yt-dlp and
# ffmpeg for the media path. web/dist is committed, so there is no Node stage.

FROM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
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
# The DLC service key reaches a release build as two BuildKit secrets, which
# leave no trace in a layer or the build history (docs/decisions.md):
#
#   docker build --secret id=kl_dlc_key,env=KL_DLC_KEY --secret id=kl_dlc_iv,env=KL_DLC_IV .
#
# Without them every .dlc goes to the JDownloader backend.
#
# Pure Go (modernc SQLite), so a static binary needs no cgo.
RUN --mount=type=secret,id=kl_dlc_key --mount=type=secret,id=kl_dlc_iv \
    [ -n "${COMMIT}" ] || echo 'WARNING: no --build-arg COMMIT, so this image will not know its revision: GET /api/health answers an empty commit and the About crest cannot turn' >&2; \
    dlc=""; \
    if [ -s /run/secrets/kl_dlc_key ] && [ -s /run/secrets/kl_dlc_iv ]; then \
      key=$(tr -d ' \r\n' < /run/secrets/kl_dlc_key); iv=$(tr -d ' \r\n' < /run/secrets/kl_dlc_iv); \
      if [ ${#key} -ne 16 ] || [ ${#iv} -ne 16 ]; then echo 'the DLC key and IV must be 16 characters each' >&2; exit 1; fi; \
      dlc="-X github.com/junkerderprovinz/knightloader/internal/container.dlcKey=${key} -X github.com/junkerderprovinz/knightloader/internal/container.dlcIV=${iv}"; \
      echo 'building with the DLC key'; \
    else \
      echo 'building without the DLC key'; \
    fi; \
    CGO_ENABLED=0 go build \
      -ldflags="-s -w ${dlc} -X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=${VERSION} -X github.com/junkerderprovinz/knightloader/internal/buildinfo.Commit=${COMMIT}" \
      -o /out/knightloader ./cmd/knightloader

# RARLAB's own UnRAR, the decoder WinRAR uses, built from the source RARLAB
# publishes: its Linux binary exists for x86-64 only and is linked against
# glibc. The licence allows UnRAR inside other software and goes into the image
# with it.
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS unrar
# unrarsrc-7.2.7 is UnRAR 7.23, the release the desktop builds ship
# (desktop/unrar.json).
ARG UNRAR_VERSION=7.2.7
ARG UNRAR_SHA256=01d903a7dcf413cb2925696d7796e48e38d471f79bfe7ef3ad2aebf6c12dbefd
RUN apk add --no-cache g++ make \
    && wget -qO /tmp/unrarsrc.tar.gz "https://www.rarlab.com/rar/unrarsrc-${UNRAR_VERSION}.tar.gz" \
    && echo "${UNRAR_SHA256}  /tmp/unrarsrc.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/unrarsrc.tar.gz -C /tmp \
    && make -C /tmp/unrar -f makefile -j"$(nproc)" \
    && install -Dm755 /tmp/unrar/unrar /out/unrar \
    && install -Dm644 /tmp/unrar/license.txt /out/license.txt

FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
# The JRE runs the private headless JDownloader that KL_PROVISION_JD starts by
# default, which DLC and other container links need. tini is PID 1 so the
# ffmpeg left behind when a yt-dlp download is stopped gets reaped.
RUN apk add --no-cache ca-certificates yt-dlp ffmpeg tzdata openjdk21-jre-headless tini libstdc++ \
    && adduser -D -u 1000 knight
COPY --from=build /out/knightloader /usr/local/bin/knightloader
COPY --from=unrar /out/unrar /usr/local/bin/unrar
COPY --from=unrar /out/license.txt /usr/share/licenses/unrar/license.txt

# Data (SQLite, settings, encrypted accounts) and downloads live on volumes.
ENV KL_DATA=/data \
    KL_ADDR=:8749 \
    KL_YTDLP=/usr/bin/yt-dlp
VOLUME ["/data"]
EXPOSE 8749

# The container opens no Click'n'Load port unless KL_CNL is set: 127.0.0.1 in
# here is not the one a browser posts to, short of host networking, and the
# protocol has no authentication, so binding 0.0.0.0 would let anyone on the LAN
# add links. The browser extension, the desktop app and the bridge catch
# Click'n'Load instead (docs/clicknload.md).

# No USER: Docker creates a missing bind-mount source as root, so the server
# starts as root, gives the empty folders it was just handed to PUID:PGID
# (knight, 1000:1000, by default) and switches to that account before it does
# anything else (internal/privdrop). --user skips all of that, as on Unraid.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -qO- http://127.0.0.1:8749/api/health >/dev/null 2>&1 || exit 1
# -s keeps the reaping when docker run --init puts its own init in front.
ENTRYPOINT ["/sbin/tini", "-s", "--", "/usr/local/bin/knightloader"]
