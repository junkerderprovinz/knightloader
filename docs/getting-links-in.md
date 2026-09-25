# Getting links in

Pasting works, and so does dropping text onto the collector. Beyond that:

- **[Click'n'Load](clicknload.md)**: a site's own CnL button hands its links
  straight over. Every site addresses `127.0.0.1`, and a container's loopback is
  not the browser's, so when KnightLoader runs on a NAS there are two ways to
  bridge that gap: the [browser extension](browser-tools.md), which catches the
  submission in the page itself and routes it through the relay to whichever
  instance you pick, or the same binary run as a bridge on your desktop
  (`knightloader -bridge http://nas:8749`) for a browser with no extension in it.
- **Watched folder**: drop a `.txt` or a JDownloader `.crawljob` onto a share and
  the box picks it up, with its package name, destination and archive password.
  Point Settings at the folder to switch it on.
- **A page**: paste one, and the files it links to are staged instead.
- **A container file**: upload a `.txt`, `.dlc`, `.ccf` or `.rsdf`. A link list is
  read on the spot. The encrypted formats cannot be opened by anyone offline,
  because their key is issued to registered clients. They are handed to the
  JDownloader backend, which has one. That backend is provisioned on first run
  by default (`KL_PROVISION_JD`), so this normally works with nothing set. With
  no backend at all, a container is recognised and refused, with the missing
  backend named as the reason.
- **Your own server**: see below.

## Own servers (FTP, SFTP, WebDAV)

A seedbox, a NAS or your own Nextcloud is a source like any other. Paste
`ftp://`, `ftps://`, `sftp://`, `webdav://` or `webdavs://` and the file is
staged, named and sized before it starts.

**Credentials live in Accounts, never in the link.** Add an account with the
service *Own server (FTP, SFTP, WebDAV)* and give it the **hostname** as its
account name, for example `seedbox.example.net`. That name is what a pasted link
is looked up by, so a login stored under anything else is never found. A password
written into a URL is refused rather than quietly stripped, because it would be
saved to the task list in plain text.

A plain `https://` link is claimed as WebDAV only when an account exists for that
exact host, so no ordinary download is ever taken over. Public FTP archives need
no account at all.

A link to a **folder** stages one task per file inside it, subfolders included,
the way a torrent's file list does. Paused downloads continue where they stopped:
FTP restarts at an offset with `REST`, SFTP reads at an offset directly, and
WebDAV uses HTTP byte ranges. A server that cannot do it says so and the download
fails loudly instead of quietly writing a corrupt file.

The first time an SFTP server is seen, its host key is written to
`known_hosts` in the data directory and has to match on every connection after
that, the same rule `ssh` follows once you have answered its prompt.

## Sites that want their own headers

Some links only work with something extra on the request: a forum that checks
the referrer, a private Nextcloud behind basic auth, a page that needs the
cookie your browser already has. A **header profile** stores that per host, in
the same encrypted store as the account credentials, and you can paste a cookie
block or a whole `curl` line in and have the headers pulled out of it.

Two rules make this safe to use, and the code enforces both. A
stored header **never appears anywhere it could be read back**: not in a log,
not in an error message, not in a diagnostic bundle, and not in
`settings.json`, where a Packagizer rule only ever names the profile it wants.
And a header **never follows a redirect off its own site**: a forum that hands
you on to a CDN does not get to pass your session token along with you.
