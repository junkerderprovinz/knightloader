"""The download buttons the README shows, read by gen_download_buttons.py.

Each entry names a button the generator knows and where it leads. The rows,
their order, the colours and the words are the generator's, the same in every
repository.
"""

REPO = "knightloader"
RELEASE = "https://github.com/junkerderprovinz/knightloader/releases/latest/download/"
# The app and the extension are released on tags of their own, so the
# product's /releases/latest/ never carries them. Their workflows copy the
# newest build into a standing release each, under a name without a version.
NEWEST = "https://github.com/junkerderprovinz/knightloader/releases/download/%s/latest/"

BUTTONS = {
    "windows": RELEASE + "knightloader-windows-amd64.zip",
    "macos": RELEASE + "knightloader-macos-universal.zip",
    "linux": RELEASE + "knightloader-linux-amd64.zip",
    # A browser cannot download an image, so this opens the package page, which
    # carries the pull command and every tag.
    "docker": "https://github.com/junkerderprovinz/knightloader/pkgs/container/knightloader",
    # A release's "Source code (zip)" is the whole repository at that tag, and
    # GitHub gives the newest one no fixed address, so this leads to the release
    # that lists it.
    "source": "https://github.com/junkerderprovinz/knightloader/releases/latest",
    "apk": NEWEST % "mobile" + "knightloader-android.apk",
    # One zip for every Chromium browser; Firefox takes only a signed add-on,
    # which release-extension.yml has Mozilla sign.
    "chrome": NEWEST % "extension" + "knightloader-extension.zip",
    "firefox": NEWEST % "extension" + "knightloader-extension.xpi",
}
