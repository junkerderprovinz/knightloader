# KnightLoader: one static Go binary with the UI embedded, plus yt-dlp, ffmpeg
# and Deno for the media path. web/dist is committed, so there is no Node stage.

FROM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
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
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS unrar
# unrarsrc-7.2.7 is UnRAR 7.23, the release the desktop builds ship
# (desktop/unrar.json).
ARG UNRAR_VERSION=7.2.7
ARG UNRAR_SHA256=01d903a7dcf413cb2925696d7796e48e38d471f79bfe7ef3ad2aebf6c12dbefd
# rarlab.com is a single host that times out now and then, so the fetch
# retries and falls back to the Internet Archive's copy, which the sum below
# holds to the same bytes.
RUN apk add --no-cache g++ make curl \
    && src="https://www.rarlab.com/rar/unrarsrc-${UNRAR_VERSION}.tar.gz" \
    && { curl -fsSL --retry 4 --retry-all-errors --connect-timeout 20 -o /tmp/unrarsrc.tar.gz "${src}" \
      || curl -fsSL --retry 4 --retry-all-errors --connect-timeout 20 -o /tmp/unrarsrc.tar.gz "https://web.archive.org/web/2026id_/${src}"; } \
    && echo "${UNRAR_SHA256}  /tmp/unrarsrc.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/unrarsrc.tar.gz -C /tmp \
    && make -C /tmp/unrar -f makefile -j"$(nproc)" \
    && install -Dm755 /tmp/unrar/unrar /out/unrar \
    && install -Dm644 /tmp/unrar/license.txt /out/license.txt

# Deno publishes glibc builds only, and Alpine's deno package trails its
# releases by months. The official binary gets the few glibc libraries it links
# from distroless, kept in /usr/local/lib/glibc apart from Alpine's musl ones,
# and patchelf points its loader and library path there, as denoland/deno's own
# Alpine image does.
FROM gcr.io/distroless/cc-debian13:latest@sha256:e8343d4a7da8bb210df0c3ea29de38bbadbba6c773bed0cd8bde506fca3a5c44 AS glibc

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS deno
# Deno's GitHub releases are immutable, so the .sha256sum beside the zip holds
# it as firmly as a checksum written here, and a version bump is all an update
# takes.
# renovate: datasource=github-releases depName=denoland/deno
ARG DENO_VERSION=2.9.8
ARG TARGETARCH
WORKDIR /tmp
RUN --mount=type=bind,from=glibc,source=/usr/lib,target=/glibc \
    case "${TARGETARCH}" in \
      amd64) arch=x86_64; loader=ld-linux-x86-64.so.2 ;; \
      arm64) arch=aarch64; loader=ld-linux-aarch64.so.1 ;; \
      *) echo "Deno publishes no Linux build for ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && apk add --no-cache patchelf curl \
    && zip="deno-${arch}-unknown-linux-gnu.zip" \
    && url="https://github.com/denoland/deno/releases/download/v${DENO_VERSION}/${zip}" \
    && curl -fsSL --retry 4 --retry-all-errors --connect-timeout 20 -o "/tmp/${zip}" "${url}" \
    && curl -fsSL --retry 4 --retry-all-errors --connect-timeout 20 -o /tmp/deno.sha256sum "${url}.sha256sum" \
    && sha256sum -c deno.sha256sum \
    && unzip -q "/tmp/${zip}" deno -d /tmp \
    && mkdir -p /usr/local/lib/glibc \
    && for lib in "${loader}" libc.so.6 libm.so.6 libdl.so.2 libpthread.so.0 librt.so.1 libgcc_s.so.1; do \
         cp -L "/glibc/${arch}-linux-gnu/${lib}" /usr/local/lib/glibc/; \
       done \
    && patchelf --set-interpreter "/usr/local/lib/glibc/${loader}" --set-rpath /usr/local/lib/glibc /tmp/deno \
    && install -Dm755 /tmp/deno /usr/local/bin/deno \
    && deno --version

# yt-dlp from its own releases, because Alpine's package trails the fixes the
# big video sites keep forcing. The musl builds run on Alpine without python3
# and carry yt-dlp's YouTube solver scripts (yt-dlp-ejs).
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS ytdlp
# renovate: datasource=github-releases depName=yt-dlp/yt-dlp versioning=loose extractVersion=^(?<version>.+)$
ARG YTDLP_VERSION=2026.11.23
ARG TARGETARCH
# The sums are signed with yt-dlp's release key, the same file and fingerprint
# the app checks its own yt-dlp updates with (internal/mediatools/signature.go).
ARG YTDLP_SIGNER=AC0CBBE6848D6A873464AF4E57CF65933B5A7581
COPY internal/mediatools/ytdlp-signing-key.asc /tmp/
RUN apk add --no-cache gpg gpgv curl \
    && case "${TARGETARCH}" in \
      amd64) asset=yt-dlp_musllinux ;; \
      arm64) asset=yt-dlp_musllinux_aarch64 ;; \
      *) echo "yt-dlp publishes no musl build for ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && base="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}" \
    && cd /tmp \
    && curl -fsSL --retry 4 --retry-all-errors --connect-timeout 20 -O "${base}/SHA2-256SUMS" \
    && curl -fsSL --retry 4 --retry-all-errors --connect-timeout 20 -O "${base}/SHA2-256SUMS.sig" \
    && gpg --batch --dearmor -o ytdlp.gpg ytdlp-signing-key.asc \
    && gpgv --status-fd 1 --keyring ./ytdlp.gpg SHA2-256SUMS.sig SHA2-256SUMS \
      | grep -q "^\[GNUPG:\] VALIDSIG .* ${YTDLP_SIGNER}\$" \
    && curl -fsSL --retry 4 --retry-all-errors --connect-timeout 20 -o "${asset}" "${base}/${asset}" \
    && sum=$(grep -x "[0-9a-f]\{64\}  ${asset}" SHA2-256SUMS) \
    && echo "${sum}" | sha256sum -c - \
    && install -Dm755 "${asset}" /out/yt-dlp \
    && [ "$(/out/yt-dlp --version)" = "${YTDLP_VERSION}" ]

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
# The JRE runs the private headless JDownloader that KL_PROVISION_JD starts by
# default, which DLC and other container links need. tini is PID 1 so the
# ffmpeg left behind when a yt-dlp download is stopped gets reaped. yt-dlp
# runs its YouTube solver in the Deno above, which it is handed with
# --js-runtimes (KL_DENO). Chromium is the sign-in browser
# (internal/signinbrowser), started only while somebody signs in to a site from
# the web interface; Noto covers the scripts a login page falls back to where
# its own web fonts do not. apk upgrade takes the fixes Alpine published after
# the base image was built.
RUN apk add --no-cache ca-certificates ffmpeg tzdata openjdk21-jre-headless tini libstdc++ \
      chromium font-noto \
    && apk upgrade --no-cache \
    && adduser -D -u 1000 knight
COPY --from=build /out/knightloader /usr/local/bin/knightloader
COPY --from=ytdlp /out/yt-dlp /usr/bin/yt-dlp
COPY --from=unrar /out/unrar /usr/local/bin/unrar
COPY --from=unrar /out/license.txt /usr/share/licenses/unrar/license.txt
COPY --from=deno /usr/local/lib/glibc /usr/local/lib/glibc
COPY --from=deno /usr/local/bin/deno /usr/local/bin/deno

# Dashboards that read container labels list KnightLoader with these. The
# address depends on the host, so the template sets homepage.href.
LABEL homepage.group="Downloads" \
      homepage.name="KnightLoader" \
      homepage.icon="https://raw.githubusercontent.com/junkerderprovinz/unraid-apps/main/knightloader/icon.png?v=4" \
      homepage.description="Download manager for hosters, torrents and Usenet" \
      glance.name="KnightLoader" \
      glance.icon="https://raw.githubusercontent.com/junkerderprovinz/unraid-apps/main/knightloader/icon.png?v=4" \
      glance.description="Download manager for hosters, torrents and Usenet"

# Data (SQLite, settings, encrypted accounts) and downloads live on volumes.
ENV KL_DATA=/data \
    KL_ADDR=:8749 \
    KL_YTDLP=/usr/bin/yt-dlp \
    KL_DENO=/usr/local/bin/deno \
    KL_CHROMIUM=/usr/lib/chromium/chromium
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
