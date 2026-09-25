# KnightLoader

A self-hosted download manager that puts debrid services, torrents, yt-dlp and
a headless JDownloader behind one web interface.

!!! warning "Not ready to install yet"
    KnightLoader is under development. The releases and the container image
    exist so the builds can be tested, and it is not listed in Community
    Applications. What is here changes daily, including the storage format, the
    settings document and the wire protocol instances use to reach each other,
    and none of those changes comes with a migration path yet. If you install
    it now, expect to lose your configuration and your queue.

## What makes it different

It is **one Go process**. The download engine, the REST and WebSocket API, the
web interface and the Click'n'Load listener live in the same binary, so there
is no aria2 to supervise, no separate front end to keep in step, and nothing to
install beside it.

Hoster coverage comes from **resolvers**, not from a plugin collection that
somebody has to keep alive. Plain file links go to the embedded engine.
Supported hosters are unlocked through a debrid service you already pay for.
Magnets and `.torrent` files go to the BitTorrent client in the same engine,
media pages go to yt-dlp, and whatever is left goes to a headless JDownloader
that KnightLoader sets up itself and keeps at arm's length. Your accounts stay
yours, stored encrypted on your own machine.

It reaches you **from other networks with twelve words**. Read a phrase off one
instance and type it into the phone app, the browser extension or another
instance, and they find each other without an account, a login, a port forward
or a domain.

## Where to go next

- [Installing](installing.md) the container, a desktop app, the Android app or
  the browser extension.
- [What it does](features.md), area by area.
- [Configuration](configuration.md): the environment variables and what each
  one is for.
- [Getting links in](getting-links-in.md): pasting, watched folders, container
  files, your own servers and sites that want their own headers.
- [Click'n'Load](clicknload.md), and how it reaches an instance on another
  machine.
- [Bookmarklet, extension and share target](browser-tools.md).
- [Connecting instances and apps](connecting.md), with the twelve words or on
  your own network.
- [Where files land](where-files-land.md): folder templates, telling a media
  library to rescan, and starting a program of your own.

Problems, wishes or suggestions? Open an
[issue](https://github.com/junkerderprovinz/knightloader/issues).
