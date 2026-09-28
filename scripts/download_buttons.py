"""The download buttons the README shows, read by gen_download_buttons.py.

Each entry names a button the generator knows and where it leads. The rows,
their order, the colours and the words are the generator's, the same in every
repository.
"""

REPO = "knightloader"
RELEASE = "https://github.com/junkerderprovinz/knightloader/releases/latest/download/"

BUTTONS = {
    "windows": RELEASE + "knightloader-windows-amd64-installer.exe",
    "windows-arm": RELEASE + "knightloader-windows-arm64-installer.exe",
    "macos": RELEASE + "knightloader-macos-universal.zip",
    "linux": RELEASE + "knightloader-linux-amd64.zip",
    "linux-arm": RELEASE + "knightloader-linux-arm64.zip",
    # No listing yet, so the button is drawn without a link. The Community
    # Applications address goes here once KnightLoader is listed there.
    "unraid": None,
    # A browser cannot download an image, so this opens the package page, which
    # carries the pull command and every tag.
    "docker": "https://github.com/junkerderprovinz/knightloader/pkgs/container/knightloader",
    # A release's "Source code (zip)" is the whole repository at that tag, and
    # GitHub gives the newest one no fixed address, so this leads to the release
    # that lists it.
    "source": "https://github.com/junkerderprovinz/knightloader/releases/latest",
    "docs": "https://junkerderprovinz.github.io/knightloader/",
    # No listing yet, so the button is drawn without a link. The listing's
    # address goes here once it exists.
    "google-play": None,
    "apk": RELEASE + "knightloader-android.apk",
    # One zip for every Chromium browser.
    "chrome": RELEASE + "knightloader-extension.zip",
    # Firefox takes only an add-on Mozilla has signed, and the signed builds
    # come from the Firefox Add-ons listing. Its address goes here once the
    # listing is live.
    "firefox": None,
}
