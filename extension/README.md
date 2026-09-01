# KnightLoader browser extension

A Manifest V3 extension. Right-click a link, a selection, an image or a page
and send it to one of your own KnightLoader instances — and catch the
Click'n'Load buttons websites offer, so those reach an instance running
anywhere rather than only a download manager on this machine.

See `docs/browser-tools.md` at the repo root for the full picture, including
how this relates to the bookmarklet and the PWA share target, and
`docs/clicknload.md` for the protocol itself.

## Setup is one connection phrase

The options page asks for the twelve words your instances already share, and
for nothing else — no name, no address, no password. `src/phrase.js` is a
WebCrypto port of `internal/seedphrase`, so this browser derives the same two
keys the instances do: one to join the relay group, one to encrypt the frames.
The phrase itself never leaves the browser.

Everything after that is live. `src/relay.js` is a relay client running in the
extension: the roster is read when a window opens, so an instance that is
switched off is not offered and one that came online a minute ago is. Sends go
out as `POST /api/links` through the relay, admitted because membership in the
group is the credential.

## Versions

This extension carries its own version in `src/manifest.json` and is tagged on
its own (`extension/vX.Y.Z`), separately from KnightLoader itself - it is
installed separately and upgrades separately, so one shared number would be
wrong in one direction or the other. See the Versioning section of the root
`CHANGELOG.md`.

The copy most people run does not come from that tag. The zip served by
Settings > Browser & App is built from the copy embedded in whatever server
binary is running (`embed.go`), so that one tracks the server. The tag exists
for a browser store submission and for anyone who wants a fixed download.

Both are **byte-identical to `src/`**. The download used to bake the serving
instance's address into a `config.default.json`; that file is gone, because the
extension holds no addresses any more — which is also what makes a store build
reproducible from a checkout.

## Loading it

**From a running instance:** Settings > Browser & App > Download extension.

**From this checkout (for development):**

1. `chrome://extensions` (or the equivalent `about:addons` /
   `edge://extensions` page) → enable Developer mode.
2. "Load unpacked" → pick `extension/src`.
3. **Pin it.** Chrome does not put a newly loaded extension in the toolbar; it
   sits behind the puzzle-piece button at the right of the address bar until
   you click the pin beside it there. Chrome says "extension loaded" and then
   deliberately shows no icon, which reads exactly like a failed install and has
   been reported as one. Measured rather than assumed: on Chrome 151 the
   extension reports `state: ENABLED`, `installWarnings: []`, `manifestErrors:
   []` and `chrome.action.getUserSettings() → {isOnToolbar: false}` — loaded and
   working, simply not on the toolbar. A fresh Brave profile does the same
   thing; a Brave that shows it has been pinned at some point.
4. Paste your connection phrase into the Remote access card on the options
   page, which opens by itself on a fresh install.

**`--load-extension` no longer works.** Chrome 137 and later ignore the command
line flag silently: no error, no entry in the profile, no rule cache written.
Anything that loads the extension for a test has to go through the DevTools
protocol (`Extensions.loadUnpacked`) instead, which is what a Playwright run
needs too. Worth knowing before spending an afternoon concluding the package is
broken.

Chrome writes a compiled rule cache into the source directory
(`_metadata/generated_indexed_rulesets/`) the first time it loads an unpacked
extension that ever declared `declarativeNetRequest`. It is in `.gitignore`,
and it must never end up in a package.

## Click'n'Load

`cnl-main.js` runs in the page's MAIN world and takes over the four ways such a
button submits — `fetch`, `XHR`, `HTMLFormElement.submit`, and a capture-phase
`submit` listener — decodes the payload (`cnl.js`: AES-128-CBC, key equals IV,
key from `jk`, and both zero and PKCS#7 padding, because both occur in the
wild) and hands the links to the service worker, which relays them. The page
gets `success\r\n` back, the same answer a local JDownloader gives. Sites find
a downloader at all because `cnl-main.js` sets `window.jdownloader` at
`document_start`, before their own `jdcheck.js` looks for it.

It is **on by default**: it is the reason most people install this, and a
switch that reads "on" while waiting for someone to find a permission dialog
would be a lie. That is what `<all_urls>` in the manifest is for, and it is
used for nothing else — such a button can be on any site, so the set cannot be
narrowed in advance.

Switching it off in the options unregisters both content scripts
(`chrome.scripting.unregisterContentScripts`), which
`chrome.scripting.getRegisteredContentScripts()` will confirm. Off means gone,
not dormant — more than a static `content_scripts` entry could offer.

## Why it no longer opens a window

Every send used to open a small window at `<instance>/quickadd`, same-origin,
so the session cookie carried it past `internal/api/api.go`'s `sameOrigin`
guard. That needed an **address**, which is why the options page went on asking
for a name and a URL long after the rest of the product had moved to the
connection phrase — and it could never reach a peer that has no address at all,
such as a desktop build or a relay-only instance.

The relay path replaces it without weakening that guard: nothing here strips an
`Origin` header. A relayed call arrives at the instance marked as coming from a
group sibling and is admitted on that basis alone (`relayForwardable` in
`internal/relay`). The bookmarklet and the PWA share target still open
`/quickadd`, because they have no phrase and no relay client — see
`docs/browser-tools.md`.

## Surfaces

`popup.html` (toolbar), `picker.html` (the send-to window), `options.html`.
All three draw the same instance card and the same GlimStone
(`glimstone.css`) — one implementation each in `shared.js`, because three pages
of one product drawing their own version of the same card is how three pages
become three slightly different products. Appearance, including the rainbow,
follows the same engines as the web UI (`appearance.js`), and can be adopted
from the default instance in one switch.

42 languages in `i18n.js`, checked against the web UI's set by
`check-locales.mjs`, which fails on both a missing key and an unused one.
