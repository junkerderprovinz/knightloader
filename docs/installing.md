# Installing

!!! warning "Read the notice on the start page first"
    KnightLoader is not ready to install yet. Everything below works, and it is
    here so the builds can be tested.

## As a container

Every release tag publishes `ghcr.io/junkerderprovinz/knightloader` for amd64
and arm64. The image carries yt-dlp, ffmpeg and the Java runtime the headless
JDownloader needs, so nothing else has to be installed.

```sh
docker run -d --name knightloader \
  --restart unless-stopped \
  -p 8749:8749 \
  -v /path/to/appdata:/data \
  -v /path/to/downloads:/data/downloads \
  -v /path/to/watch:/watch \
  -e TZ=Europe/Berlin \
  ghcr.io/junkerderprovinz/knightloader:latest
```

Then open `http://<host>:8749`. On Unraid add `--user 99:100`, so finished
files land as `nobody:users`.

### Behind a reverse proxy

KnightLoader works on a host name of its own, such as `https://kl.example.com`,
and just as well in a folder of another one, such as `https://example.com/kl/`.
Either way the proxy has to pass the WebSocket upgrade through and, when it
terminates TLS, send `X-Forwarded-Proto`. The self-test under Settings,
Diagnostics checks both from your browser.

For a folder, KnightLoader has to know the path. There are two ways to tell it:

- Set `KL_BASE_PATH=/kl` in the container's environment. The proxy may pass
  the prefix on or strip it.
- Set nothing, and let the proxy strip the prefix and name it in
  `X-Forwarded-Prefix`. Traefik's StripPrefix middleware sends that header by
  itself; in nginx add `proxy_set_header X-Forwarded-Prefix /kl;`.

If both are there, `KL_BASE_PATH` wins. The prefix has to be a plain path:
letters, digits and `-._~` between the slashes. It cannot begin with `/api` or
`/relay`, since the instance answers those without the prefix too.

nginx, passing the prefix on, with `KL_BASE_PATH=/kl`:

```nginx
location /kl/ {
    proxy_pass http://knightloader:8749;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

Traefik, stripping it, with nothing set:

```yaml
labels:
  - traefik.http.routers.knightloader.rule=Host(`example.com`) && PathPrefix(`/kl`)
  - traefik.http.routers.knightloader.middlewares=knightloader-strip
  - traefik.http.middlewares.knightloader-strip.stripprefix.prefixes=/kl
  - traefik.http.services.knightloader.loadbalancer.server.port=8749
```

Caddy, passing it on, with `KL_BASE_PATH=/kl`. The `redir` line sends the bare
`/kl` to `/kl/`, which `handle /kl/*` would not match. `handle_path` in place of
`handle` works too: it strips the prefix, and `KL_BASE_PATH` still names it.

```
example.com {
    redir /kl /kl/
    handle /kl/* {
        reverse_proxy knightloader:8749
    }
}
```

Once the path is known, the instance uses it everywhere: the session cookie,
the addresses and QR code on the Remote access page, the installed web app, its
share target and the bookmarklet all include it. Where you name the instance
yourself, include the path too: for the Click'n'Load bridge
(`-bridge https://example.com/kl`), for another instance on the Instances page,
and as the relay address on the others when this one is switched to "Use this
instance as the relay". The phone app and the browser extension reach the
instance through the relay with the twelve words and need no address.

The path does not separate the instance from the other applications on the
host. They share one origin with it, so a page served by any of them can call
KnightLoader's API with your session, whatever path the cookie carries. Only a
host name of its own keeps them apart, so give it one unless you trust
everything else on that host.

The container's health check and the LAN address `http://<host>:8749` keep
working without the prefix.

### Building the image yourself

```sh
docker build --build-arg VERSION=preview --build-arg COMMIT="$(git rev-parse HEAD)" -t knightloader:preview .
```

Pass both arguments. `.dockerignore` keeps `.git` out of the build context, so
the toolchain inside the image has no repository to read, and without `COMMIT`
the binary cannot tell which revision it is. `VERSION` shows under the
wordmark, and `COMMIT` answers as `commit` on `GET /api/health`.

## As a desktop app

The desktop app is the same binary in a native window instead of a browser tab.
The engine, the resolvers, the API and the interface are identical, because the
window is served by the same HTTP handler the container serves.

Every release tag builds `windows/amd64`, `windows/arm64`, `darwin/universal`,
`linux/amd64` and `linux/arm64`, and attaches each archive to
[that release](https://github.com/junkerderprovinz/knightloader/releases/latest).
Take x64 for most computers and ARM64 for one with an ARM processor, such as a
laptop with a Snapdragon chip. The macOS bundle is universal, so it runs on
Intel and on Apple silicon. A running instance offers the same downloads under
Settings, App, and picks the matching build when the browser reports the
processor.

The desktop app brings no Java, yt-dlp or ffmpeg of its own:

- Java, on `PATH` or under `JAVA_HOME`, for the private JDownloader. Without
  it, file hoster links have no catch-all and encrypted container files
  (`.dlc`, `.ccf`, `.rsdf`) cannot be opened. Pointing `KL_JD` at a JDownloader
  that runs elsewhere works too.
- yt-dlp for video sites. The Resolvers settings page can fetch a copy and
  keep it current.
- ffmpeg on `PATH`, for merging video with its audio and for converting.

On Linux the window needs WebKitGTK: `libwebkit2gtk-4.1-0`.

While a download is running, or its files are being checked, unpacked or moved,
the desktop app keeps the computer from going to sleep, and once nothing is
left to do it lets it sleep again. The screen can still turn off. The switch is on the Automation page, at the top of the Idle card. On
Linux the app asks logind for a sleep lock, which a local desktop session gets
without a password.

### Building it from source

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
cd desktop && wails build
```

The bundle lands in `desktop/build/bin`. Windows and macOS need only their
usual toolchains. Linux needs GTK and WebKit: `libgtk-3-dev` and
`libwebkit2gtk-4.1-dev` to build. Build it there with
`wails build -tags webkit2_41`. Without the tag Wails looks for webkit2gtk
**4.0**, which Ubuntu 24.04 and its relatives no longer package, and the error
names a missing package rather than a dropped version.

## The Android app

The APK is on the
[latest app release](https://github.com/junkerderprovinz/knightloader/releases/download/mobile/latest/knightloader-android.apk).
It reaches an instance on your own network or, with the twelve words, from
anywhere else: see [Connecting instances and apps](connecting.md).

## The browser extension

A running instance offers it under Settings, App: the Chrome button gives the
ZIP for every Chromium browser, the Firefox button the add-on. The same ZIP is
on the
[latest extension release](https://github.com/junkerderprovinz/knightloader/releases/download/extension/latest/knightloader-extension.zip),
one file for Chrome, Edge, Brave and Opera:

1. Unpack the ZIP.
2. Open `chrome://extensions` (in Edge `edge://extensions`) and switch on
   Developer mode.
3. Choose **Load unpacked** and pick the unpacked folder.
4. Pin it. Chrome does not put a newly loaded extension on the toolbar; it
   waits behind the puzzle-piece button at the right of the address bar.
5. Paste your connection phrase into the Remote access card on the options
   page, which opens by itself on a fresh install.

Firefox installs only add-ons Mozilla has signed. Those come from the
add-on's page on Firefox Add-ons, which is not listed yet. Until it is,
`about:debugging`, This Firefox, **Load Temporary Add-on** loads the ZIP until
the next restart. What the extension does is in
[Bookmarklet, extension and share target](browser-tools.md).

## From a checkout

```sh
go run ./cmd/knightloader      # then open http://localhost:8749
```

The web interface is committed prebuilt in `web/dist` and embedded into the
binary, so a plain `go build` gives a working server.
