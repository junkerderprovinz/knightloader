# Installing

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

Then open `http://<host>:8749`.

On Unraid, put `--user 99:100` in Extra Parameters and add the variable
`UMASK` with the value `000`. Finished files then land as `nobody:users` with
mode 0666 and their folders with 0777, so the account you use over SMB can
move, rename and delete them. `UMASK` is read the way linuxserver.io images
read it, as an octal mask such as `000`, `002` or `022`. `PUID` and `PGID` are
not read: the image runs as a fixed user, and `--user` is how you choose
another one. Settings, Diagnostics shows which user and mask are in force.

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
letters, digits and `-._~` between the slashes. It cannot begin with `/api`,
`/relay` or `/dav`, since the instance answers those without the prefix too.

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
the installed web app, its share target and the bookmarklet all include it.
Where you name the instance yourself, include the path too: for the
Click'n'Load bridge (`-bridge https://example.com/kl`), for another instance
added by address on the Instances page, and as the relay address on the
others when this one has "Serve as relay" switched on in the relay card under
Settings, Pairing. The phone app and the browser extension reach the instance
through the relay with the twelve words and need no address.

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
Settings, Apps, and picks the matching build when the browser reports the
processor.

Windows also gets an **installer** beside each zip. It puts KnightLoader under
`C:\Program Files\KnightLoader` for everyone on the computer and asks for an
administrator once, while it installs; later updates ask nobody (see
[Updates](#updates)). A page asks whether you want a Start menu entry and a
desktop shortcut, both ticked at first, and both go to every user. The next
install, silent or not, starts from your answer and removes a shortcut you left
out. On the way it removes an installation for you alone under
`AppData\Local\Programs`, and it never touches your settings, accounts and
private JDownloader in `%APPDATA%\KnightLoader`. The `KnightLoader.exe` in the zip is
the same program as a portable copy that needs no installing.

The desktop app brings no Java, yt-dlp or ffmpeg of its own:

- Java, on `PATH` or under `JAVA_HOME`, for the private JDownloader. Without
  it, file hoster links have no catch-all and `.dlc` container files cannot be
  opened. Pointing `KL_JD` at a JDownloader
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

### Updates

The desktop app updates itself from GitHub. It asks for the newest release,
downloads the zip for your system in the background and checks it against the
release's `checksums.txt`. The new version starts the next time you open
KnightLoader, so nothing changes while it runs, and a note in the corner of the
window says when it is ready. Pre-releases are never installed. **Update
automatically** on the **General** page of Settings turns this off; it is on
from the start.

**The installed copy** under Program Files cannot replace itself, since no user
may write there. A scheduled task named **KnightLoader Update** does it instead,
once a day and five minutes after the computer starts, whether KnightLoader is
open or not. It replaces `KnightLoader.exe`, sets the version shown in the list
of installed apps and writes what it did, or why it did not, to
`%ProgramData%\KnightLoader\update.log`. There is one installation, so the
switch applies to everyone on the computer: it lives in
`%ProgramData%\KnightLoader\settings.json`, which every user may change, and the
task skips its run while it is off. An open window notices within ten minutes
when the task has put a new version in place and shows the note. The replaced
program waits beside the new one as `KnightLoader.exe.old` until a later run
removes it.

**A portable copy** updates itself while it runs: a minute after it starts and
once a day after that, in its own folder, and it stays portable. The app on
macOS and Linux does the same. A read-only folder, or a macOS app your account
cannot change, stays as it is, and the log says why.

#### Why the task runs as SYSTEM

Program Files belongs to the administrators. A program that replaces itself
there without asking anyone needs an account that may write there, and a
scheduled task under the system account is how Windows provides one; Firefox's
maintenance service and Chrome's updater task work the same way. The task
writes to three places and nowhere else: the installation folder,
`C:\Program Files\KnightLoader`; its entry in the list of installed apps, under
`HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\KnightLoader`; and
`%ProgramData%\KnightLoader`, for its log. It starts no JDownloader, opens no
window, listens on no port and reads nothing from any user's profile. The folder
under ProgramData belongs to the administrators, and only `settings.json` in it
is open to other users. The installer has no page for choosing another folder.
That leaves no file a user could swap for one the system account would run or
write to. The task downloads only from GitHub over HTTPS and installs a file
only when it matches `checksums.txt`.

#### Uninstalling

Uninstalling KnightLoader from the list of installed apps asks for an
administrator and removes the program, its shortcuts and its entry, the
scheduled task and `%ProgramData%\KnightLoader` with the switch and the log. The
webview cache of the person uninstalling, `%APPDATA%\KnightLoader.exe`, goes as
well. Every user's settings, accounts and private JDownloader in
`%APPDATA%\KnightLoader` stay, so a later install picks them up, and so do the
files you downloaded.

A container does not update itself. The same page tells you when a newer
release exists; pull the new image the way you deployed this one.

### Building it from source

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.27
node scripts/desktop.mjs
```

The bundle lands in `desktop/build/bin`; `--installer` adds the Windows
installer, which needs NSIS. It is a dev build, which never updates itself;
only a build stamped with its version, as the release workflow makes, does (see
`desktop/README.md`). Windows and macOS need only their usual toolchains. Linux
needs GTK 3 and WebKit: `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` to build.
There, install the CLI with `go install -tags gtk3 ...` as well. Without the tag
the CLI looks for GTK 4, and the error names a missing package rather than the
tag.

## The Android app

The APK is on the
[latest release](https://github.com/junkerderprovinz/knightloader/releases/latest/download/knightloader-android.apk),
with the same version number as the server beside it.
It reaches an instance on your own network or, with the twelve words, from
anywhere else: see [Connecting instances and apps](connecting.md).

## The browser extension

Chrome, Brave, Opera and Vivaldi install it from the
[Chrome Web Store](https://chromewebstore.google.com/detail/knightloader/elofnnhhimbaeknbmncmlhhfkbncdpdf),
Edge from
[Edge Add-ons](https://microsoftedge.microsoft.com/addons/detail/knightloader/fjmmdihlohkllidleeofpmhbekibfkfl).
Settings, Apps on a running instance has a button for each browser that opens
its listing, with the steps beside it:

1. Press **Add to Chrome** (in Edge **Get**) and confirm. Opera first asks
   for its Install Chrome Extensions add-on.
2. Pin it. Chrome does not put a new extension on the toolbar; it waits
   behind the puzzle-piece button at the right of the address bar.
3. Paste your connection phrase into the Remote access card on the options
   page, which opens by itself on a fresh install.

A Chromium browser without a store, or a build you want to test, takes the
ZIP from the
[latest release](https://github.com/junkerderprovinz/knightloader/releases/latest/download/knightloader-extension.zip):
unpack it, open `chrome://extensions`, switch on Developer mode and choose
**Load unpacked** with the unpacked folder.

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
