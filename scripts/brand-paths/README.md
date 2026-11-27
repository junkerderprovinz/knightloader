# Marks on the download buttons

The path data `../gen_download_buttons.py` draws into the README's download
buttons. `<name>.txt` holds the `d` attribute of each of the mark's paths, one
per line, and `<name>.box.txt` the viewBox it was drawn in, which is what lets the generator
scale marks of different widths to one optical size.

## Source and licence

**Font Awesome Free 6.7.2**, from <https://fontawesome.com>: Windows, Apple,
Linux, Docker, Android, Google Play, Chrome, Edge, Brave, Opera and Firefox
(`firefox-browser`) from the `brands` set, the ZIP (`file-zipper`) and the book (`book`) from the `solid` set. The icons are **CC BY 4.0**,
which asks for attribution and nothing else. Copyright 2024 Fonticons, Inc.

**Simple Icons 16.33.0** (`simple-icons/simple-icons`, <https://simpleicons.org>):
Vivaldi, which Font Awesome does not have. Simple Icons lists it under
**CC BY 4.0**, from Vivaldi's press kit (<https://vivaldi.com/press>).
Copyright Vivaldi Technologies AS.

**Simple Icons** (<https://simpleicons.org>): Proxmox, Synology, QNAP, TrueNAS
and Flatpak, from its `proxmox.svg`, `synology.svg`, `qnap.svg`, `truenas.svg`
and `flatpak.svg`. **CC0 1.0** for the path data. Flatpak's mark itself is
**CC BY 3.0** (<https://flatpak.org/press>).

**F-Droid's 2015 logo** from its artwork repository
(<https://gitlab.com/fdroid/artwork>), in one ink with the eyes, ring and F cut
out. **CC BY-SA 3.0** or **GPL-3.0-or-later**.

**Dashboard Icons** (`homarr-labs/dashboard-icons`, <https://dashboardicons.com>):
Unraid, Umbrel and UGREEN, Unraid's `unraid.svg` with the gradient left out,
since the button draws it in one ink. **Apache-2.0**, whose licence text is
beside this file as `LICENSE-dashboard-icons.txt`. Copyright the Homarr Labs
team and contributors.

MOS's mark is the `mos.svg` in mos-nas/mos-releases, and ZimaOS's the
`zimaos-logo.svg` of its web interface.

ParleyPort's mark is its own logo, from
<https://github.com/junkerderprovinz/parleyport>, one path per shape.

## Trademarks

Every platform mark here is a trademark of its owner. They are used the one way a
trademark may be used without permission, which is to refer to the thing they
name: each sits on a download button for that platform, unmodified, and nothing
here claims endorsement by or affiliation with Microsoft, Apple, Linus
Torvalds, Docker, Google, F-Droid, Mozilla, Brave Software, Opera, Vivaldi
Technologies, the Flatpak project, Lime Technology, MOS, IceWhale, Umbrel,
Proxmox, Synology, QNAP, UGREEN or iXsystems. The ZIP and the book are no one's
marks. They stand for the source archive and the manual.

## Adding one

Take the SVG, keep its `viewBox` verbatim in `<name>.box.txt`, and put the `d`
attribute of each path on a line of its own in `<name>.txt`, a `rect` written as
a path. The button draws every path in one ink, so the mark's colours are lost.
