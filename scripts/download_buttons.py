"""The download buttons the README shows, read by gen_download_buttons.py.

Each entry names a button the generator knows and where it leads. The rows,
their order, the colours and the words are the generator's, the same in every
repository.
"""

REPO = "knightloader"
RELEASE = "https://github.com/junkerderprovinz/knightloader/releases/latest/download/"
CHROME_WEB_STORE = "https://chromewebstore.google.com/detail/knightloader/elofnnhhimbaeknbmncmlhhfkbncdpdf"

BUTTONS = {
    "windows": RELEASE + "knightloader-windows-amd64-installer.exe",
    "windows-arm": RELEASE + "knightloader-windows-arm64-installer.exe",
    "macos": RELEASE + "knightloader-macos-universal.zip",
    "linux": RELEASE + "knightloader-linux-amd64.zip",
    "linux-arm": RELEASE + "knightloader-linux-arm64.zip",
    "unraid": "https://unraid.net/community/apps?q=knightloader",
    "mos": "https://mos-official.net/apps.html?search=KnightLoader",
    "compose": "https://raw.githubusercontent.com/junkerderprovinz/knightloader/main/compose.yaml",
    # A release's "Source code (zip)" is the whole repository at that tag, and
    # GitHub gives the newest one no fixed address, so this leads to the release
    # that lists it.
    "source": "https://github.com/junkerderprovinz/knightloader/releases/latest",
    "docs": "https://junkerderprovinz.github.io/knightloader/",
    # No listing yet, so the button is drawn without a link. The listing's
    # address goes here once it exists.
    "google-play": None,
    "apk": RELEASE + "knightloader-android.apk",
    # Brave, Opera and Vivaldi install from the Chrome Web Store, so their
    # buttons lead to Chrome's listing. Edge has a listing of its own.
    "chrome": CHROME_WEB_STORE,
    "edge": "https://microsoftedge.microsoft.com/addons/detail/knightloader/fjmmdihlohkllidleeofpmhbekibfkfl",
    "brave": CHROME_WEB_STORE,
    "opera": CHROME_WEB_STORE,
    "vivaldi": CHROME_WEB_STORE,
    # Firefox takes only an add-on Mozilla has signed, and the signed builds
    # come from the Firefox Add-ons listing.
    "firefox": "https://addons.mozilla.org/addon/knightloader/",
    "parleyport": "https://github.com/junkerderprovinz/parleyport",
}
