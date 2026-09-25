# What it does

| | |
|---|---|
| **Collector** | Links are analysed and staged before anything downloads: name, size, availability, which backend will take them. Nothing is ever dropped in silence, and a link that no backend handles is still shown, with the reason on it. |
| **Rules** | A Packagizer and a link filter, one engine used twice: match on filename, URL, hoster, source, type or size, then set the package, folder, filename, priority or comment, or refuse the link, with the rule's name attached. A test box shows what a rule would do before it does it. |
| **Download list** | A real table: choose your columns, resize and reorder them, sort by any of them, fold packages away. The layout is remembered per instance, and sorting is a view: the queue keeps its own order. |
| **Crawling** | Paste a page and each file it links to is staged as its own task. |
| **Containers** | `.txt` link lists are read here. `.dlc`, `.ccf` and `.rsdf` are handed to the JDownloader backend, which holds the key that opens them. Their contents then come back through the ordinary path, so the filter and the Packagizer apply to them like anything else. |
| **Queue** | Global and per-host concurrency, priorities, manual order, a stop mark, automatic retries with a growing delay, and a timetable that pauses or throttles by the clock. |
| **Speed limit** | A total for everything, applied while downloads run rather than only to the next one. |
| **Duplicates** | The same URL twice is refused; the same file under two URLs is recognised as a mirror and handled by a policy you pick. Refused links are held with their reason, not deleted. |
| **Extraction** | zip, rar (incl. multi-volume), 7z, tar, gz, bz2, xz, zst. A multi-part set waits for every part. Encrypted zip (WinZip AES and the legacy ZipCrypto), rar and 7z take passwords, tried from a list in order. Pure Go, with no external unrar or 7z binary in the image. |
| **Integrity** | A finished file is checked against an `.sfv`/`.md5`/`.sha*` that came with it, or a CRC in its own name. Nothing is marked as passing that was not checked. |
| **Collisions** | What happens when the file already exists is your choice (overwrite, skip or number it), and the name is reserved atomically, so two downloads finishing together cannot pick the same one. |
| **Connections** | Several outbound routes with order, credentials and a per-host filter, handed out round-robin up to a cap each. Passwords are stored, never served back. |
| **Reconnect** | Get a new address when a hoster's limit is keyed to the one you have: run a command, replay a recorded HTTP exchange, ask the gateway over UPnP (which needs no router details at all), or run a script through a named interpreter. A recorded LiveHeader script imports as it is. An unchanged address counts as a failure. |
| **Torrents** | Magnet links and uploaded `.torrent` files are a resolver like any other, so they land in the same collector, the same queue and the same folder rules. They need no account and no setup. |
| **Intake** | Paste, drop, [Click'n'Load](clicknload.md) from a site's own button, a container file, a watched folder for `.txt` and `.crawljob` files, or [Sonarr and Radarr](getting-links-in.md#sonarr-and-radarr), which see KnightLoader as qBittorrent or SABnzbd. |
| **Multi-instance** | Register other KnightLoaders and drive them all from one dashboard. Instances on the same network announce themselves and take one click to add, with nothing to configure. |
| **Twelve words** | Read a phrase off one instance, type it into the next, and they find each other across networks without an account, a login, a port forward or a domain. The words carry a secret; the relay only ever sees a hash of it, so nobody running one can reconstruct them. Use ours or run your own. See [Connecting instances and apps](connecting.md). |
| **Your own relay** | Don't want to use ours? Point both ends at a relay you run and the same twelve words work against it. It terminates its own TLS over TLS-ALPN-01, so it needs no proxy, certbot or cron, and only port 443 open. |
| **Everywhere** | The web UI, a desktop build, an Android app and a browser extension all talk to the same instance, and to each other's. |
| **Access** | An optional password lock, off by default. Named API tokens, each limited to the rights it needs: read, add, control or admin. See [API tokens and their rights](connecting.md#api-tokens-and-their-rights). Same-origin API, origin-checked WebSocket. |
| **Languages** | 42, each fetched only when chosen, right-to-left included. |
