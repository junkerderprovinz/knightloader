<p align="center">
  <img src="https://raw.githubusercontent.com/junkerderprovinz/knightloader/main/.github/assets/knightloader-banner.png" alt="KnightLoader" width="100%">
</p>

<p align="center">
  <a href="https://hub.docker.com/r/junkerderprovinz/knightloader"><img src="https://img.shields.io/docker/pulls/junkerderprovinz/knightloader?style=for-the-badge&logo=docker&logoColor=white&label=Pulls&color=1d99f3" alt="Docker Pulls" height="36"></a>&nbsp;
  <a href="https://hub.docker.com/r/junkerderprovinz/knightloader"><img src="https://img.shields.io/docker/image-size/junkerderprovinz/knightloader/latest?style=for-the-badge&logo=docker&logoColor=white&label=Size&color=1d99f3" alt="Image Size" height="36"></a>&nbsp;
  <img src="https://img.shields.io/badge/Arch-amd64%20%7C%20arm64-success?style=for-the-badge&logo=linux&logoColor=white" alt="Arch" height="36">&nbsp;
  <a href="https://github.com/junkerderprovinz/knightloader/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-AGPL--3.0-blue?style=for-the-badge&logo=gnu&logoColor=white" alt="License: AGPL-3.0" height="36"></a>&nbsp;
  <a href="https://junkerderprovinz.github.io/knightloader/"><img src="https://img.shields.io/badge/Docs-online-526CFE?style=for-the-badge&logo=materialformkdocs&logoColor=white" alt="Documentation" height="36"></a>
</p>

A self-hosted download manager that puts debrid services, torrents, yt-dlp and
a headless JDownloader behind one web interface. The download engine, the API
and the web UI are one Go binary. The image carries yt-dlp, ffmpeg and the Java
runtime the headless JDownloader needs, so nothing else has to be installed.

## Run it

```sh
docker run -d --name knightloader \
  --restart unless-stopped \
  -p 8749:8749 \
  -v /path/to/appdata:/data \
  -v /path/to/downloads:/data/downloads \
  -e TZ=Europe/Berlin \
  junkerderprovinz/knightloader:latest
```

Then open `http://<host>:8749`.

On Unraid, add `--user 99:100` and `-e UMASK=000`, so the account you use over
SMB can move and delete what KnightLoader downloads.

`latest` is the newest release. Each release is also tagged with its version,
such as `1.3.0`, and with `1.3` and `1`.

## More

- [Documentation](https://junkerderprovinz.github.io/knightloader/): installing, configuration, reverse proxies
- [Source, desktop apps, Android app and browser extension](https://github.com/junkerderprovinz/knightloader)
- [Issues](https://github.com/junkerderprovinz/knightloader/issues)
