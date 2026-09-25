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

A running instance offers it under Settings, App, on your browser's tile. The
same ZIP is on the
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

For Firefox, open the
[signed add-on](https://github.com/junkerderprovinz/knightloader/releases/download/extension/latest/knightloader-extension.xpi)
and confirm the installation. What the extension does is in
[Bookmarklet, extension and share target](browser-tools.md).

## From a checkout

```sh
go run ./cmd/knightloader      # then open http://localhost:8749
```

The web interface is committed prebuilt in `web/dist` and embedded into the
binary, so a plain `go build` gives a working server.
