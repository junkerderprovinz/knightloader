KnightLoader connects to your VPN by itself: pick a provider under Settings, Integrations, choose which downloads go through it, and torrents get a forwarded port where the provider offers one. AI assistants can reach your downloads over MCP, and every module is switched on its own settings page.

**Before you update:** JDownloader and yt-dlp are always on, so a backend you had switched off is on again after the update. A default yt-dlp resolution below 360p is raised to 360p.

## ✨ Added

- **A VPN inside KnightLoader.** Settings, Integrations connects to a WireGuard VPN without a VPN app or an extra container. hide.me, Mullvad, Obscura, AzireVPN, NordVPN, Private Internet Access and TorGuard sign in with your account, Proton VPN, Surfshark, Windscribe, AirVPN and 22 others take the WireGuard file from your account, and a WireGuard server of your own works with its config. Providers that allow WireGuard only in their own app, such as ExpressVPN and CyberGhost, are shown greyed out.
- **You choose what goes through the VPN.** Torrents, hosters and direct links, yt-dlp and streams, debrid services and Usenet each have a switch, and torrents start switched on. A running download moves over as soon as you flip its switch, and the Connection column says VPN for a download in the tunnel.
- **Downloads wait while the VPN is down.** A download of a switched-on kind waits until the tunnel is back and never falls back to your own connection, also when the VPN cannot be taken up again after a restart.
- **Forwarded ports for torrents.** With Proton VPN and hide.me (NAT-PMP), Private Internet Access and AzireVPN (through their APIs) and cryptostorm, KnightLoader asks for a port, keeps it alive and moves the torrent client to a new one without a restart, so torrents find more peers and can seed. The VPN card shows where others reach the port, and for providers where you forward it in your account, such as TorGuard, AirVPN or Windscribe, it names the port to request. This resolves [#112](https://github.com/junkerderprovinz/knightloader/issues/112).
- **An MCP server for AI assistants.** Cursor, GitHub Copilot, LM Studio and other clients of the Model Context Protocol can read your downloads and, with the rights you give them, add links and control the queue through 13 tools. Each client gets a token of its own with Read only, Add and read, or Read, add and control, and KnightLoader shows the configuration for exactly that client. It stays off until you add a client under Settings, Integrations.

## 🎨 Design

- **Integrations is a new settings page after Accounts.** It holds the VPN, the MCP server and the debrid drive, which no longer has a page of its own.
- **The Modules page is gone.** Each module is switched on the page of what it switches, and one that is off folds down to its title, its switch and a line on its state.
- **Health and diagnostics are one page** with three cards: the state of every part with the self-test, the log, and the system with the bug report. The reverse proxy check is on Network, and database upkeep and the metrics address are on Advanced.
- **Every field stands beside its name in one row,** in the settings, in dialogs and on the sign-in page.
- **Every list card has its own Add button** at the end of its heading, and it opens the right window straight away. On Accounts it takes the place of the floating button, and API tokens, passkeys, cookie jars and the yt-dlp host presets are added the same way.
- **Logo tiles light up in their brand's colour** when you point at them in the account, VPN and MCP pickers. Debrid and captcha services show sharp logos where many had small, blurry icons.
- **The Resolvers page is regrouped.** JDownloader is a row in the Tools card beside yt-dlp and ffmpeg, the video card sits above the audio card, and playlists, the file check and livestreams share one Files card. The track picture draws each track larger.

## ⚡ Improved

- **JDownloader and yt-dlp are always on** and have no switch of their own.
- **yt-dlp has a default video format.** Format (Best, MP4, MKV or WebM) and resolution are picked separately, and a video without a format of its own takes tracks in the chosen one first. The default resolution starts at 360p; a smaller picture is still a choice for a single download.

## 🐛 Fixed

- **Removing a torrent with its files on Windows deletes them** also while the torrent is still starting, where the files stayed behind because they were still open.
- **Removing a torrent with its files no longer waits** for the check of the files already on disk to finish.
- **New connections for a stalled download open without a 30 second wait,** which held up every other stalled download behind it.

## 🌐 Translations

- **Every new text is in all 42 languages** of the web interface.
