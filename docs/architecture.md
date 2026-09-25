# Architecture

```
                    +------------------------------------------+
  browser --------> |  api      REST + WebSocket + embedded UI  |
  CnL button -----> |  cnl      127.0.0.1:9666                  |
  dropped file ---> |  watch    link lists, .torrent, .nzb      |
                    +--------------------+---------------------+
                                         |
                              +----------v----------+
                              |  app                |  tasks, scheduler,
                              |                     |  packages, retries
                              +----------+----------+
                                         |
              +--------------+-----------+-----------+--------------+
              |              |           |           |              |
        +-----v-----+  +-----v-----+ +---v----+ +----v-----+  +-----v-----+
        | crawler   |  | resolver  | | engine | | extract  |  | checksum  |
        | page ->   |  | direct    | | Gopeed | | zip rar  |  | sfv md5   |
        | files     |  | debrid    | | + rate | | 7z tar   |  | sha crc   |
        |           |  | torrent   | |  limit | | gz xz    |  |           |
        |           |  | yt-dlp    | |  proxy | | bz2 zst  |  |           |
        |           |  | headless  | |        | |          |  |           |
        |           |  | JD        | |        | |          |  |           |
        +-----------+  +-----------+ +--------+ +----------+  +-----------+
```

Every link goes to a resolver, which decides how it is fetched: a debrid
service such as TorBox, a hoster account, the torrent client, yt-dlp, your own
FTP, SFTP or WebDAV server, the direct path, or the headless JDownloader as the
catch-all. When more than one service can take the same link, the order on the
Accounts page decides. JDownloader, yt-dlp and the direct download always come
last, because which of them fits depends on the link.

The rate limit lives in a loopback proxy because the embedded engine offers no
hook for one. Everything else is a plain Go package with its own tests.

Built on [Gopeed](https://github.com/GopeedLab/gopeed) (download engine),
[yt-dlp](https://github.com/yt-dlp/yt-dlp) (media),
[JDownloader](https://jdownloader.org/) (hoster catch-all, via its local API),
[Wails](https://github.com/wailsapp/wails) (desktop) and
[React](https://react.dev) with [Tailwind](https://tailwindcss.com).
