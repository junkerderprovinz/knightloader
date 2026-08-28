# Bookmarklet, browser extension, and the PWA share target

Three ways to hand KnightLoader a link from somewhere that is not KnightLoader
itself: a page you are already looking at, a right-click menu, or your
device's own Share sheet. All three are configured from **Settings > Browser
tools**. The bookmarklet and the share target land on `/quickadd`
(`web/src/pages/QuickAdd.tsx`); the extension no longer does, and the section
on it below says what it does instead and why.

This overlaps with Click'n'Load (`docs/clicknload.md`) rather than avoiding
it, which was not true when this page was first written. CnL answers a button
a *website* put on its own page, aimed at a download manager on the same
machine at a fixed loopback port; the tools here answer a link with no button
anywhere. Since the extension learned to intercept CnL submissions itself
(`extension/src/cnl-main.js`), the two meet: the extension catches the button
a website offers and routes it through the relay, so it reaches an instance
that is not on that machine at all — which the loopback port cannot do.

## Why the bookmarklet and the share target open a window

`internal/api/api.go`'s `sameOrigin` middleware refuses any request that
carries an Origin header not matching this instance's own host — deliberately,
so that no other page can drive a signed-in browser's session against the
API. There is no bearer token a bookmarklet could hold instead of a session
cookie. So neither of them calls `/api/links` from where it runs: each opens a
small window at `<this instance>/quickadd?...`, same-origin, where the ordinary
session cookie (and the ordinary sign-in screen, if the instance is
password-locked) applies exactly as if you had typed the address in yourself.
`/quickadd` is what actually stages the link.

This is also why neither needs a copy of the instance's password: they only
ever need to know *where* the instance is, never a credential of their own.

The extension used to work the same way, and stopped — opening a window needs
an ADDRESS, and the extension no longer knows any.

## Bookmarklet

Settings > Browser & App shows a link built from `window.location.origin` —
whatever address you are looking at the settings page on. Drag it to your
bookmarks bar. Clicking it on any page opens `/quickadd` with that page's URL
and title, plus whatever text you had selected (useful for a page listing
several links in prose, none of them individually a "download button").

Generated client-side (`web/src/lib/browserTools.ts`'s `buildBookmarklet`),
never server-rendered with a fixed address baked in at build time — a
self-hosted app has no fixed address to bake in, and whichever address you
used to reach the settings page is, by construction, one that already works
for you.

## Browser extension (Manifest V3)

Source lives in `extension/src`, embedded into the Go binary
(`extension/embed.go`) and packaged on demand by
`GET /api/browser-extension.zip` (`internal/api/routes_browsertools.go`).
Built against MV3 because MV2 is being retired across browsers; there is no
MV2 fallback.

The zip a running instance serves is **byte-identical** to `extension/src` in
the repository. It used to bake that instance's address into a
`config.default.json`; the file is gone, because the extension holds no
addresses at all any more. That also makes the store package reproducible:
anyone can build it from a checkout and compare.

### Setup is one connection phrase

The options page asks for the same twelve words the instances themselves are
paired with (`docs/connecting.md`, `internal/seedphrase`) and for nothing
else — no name, no address, no password. `extension/src/phrase.js` is a
WebCrypto port of the Go derivation, so the browser derives the identical two
keys: one that joins the relay group, one that encrypts the frames.

From there `extension/src/relay.js` is a relay client in the extension itself.
The group roster is read live at the moment a window opens, which is why the
popup has a loading state: an instance that is switched off is not offered,
and one that came online a minute ago is, with nobody telling this browser
anything. Sends go out as `POST /api/links` **through the relay**, admitted
because membership in the group is the credential — the `sameOrigin` guard is
not worked around, it simply is not on that path.

Four context-menu entries (page, link, image, selection), a toolbar popup, and
the group drawn as instance cards in both, the same card the web UI's own
Instances tab draws, with a **Standard** badge on the default and a right-click
to move it.

Permissions: `contextMenus`, `storage` (the phrase, the default instance, the
language, the appearance, the Click'n'Load switch), `activeTab` (the popup
reading the current tab at the moment you press send), `scripting`, and
`<all_urls>`. The last two exist for one feature only:

### Click'n'Load, in the browser

`cnl-main.js` runs in the page's MAIN world and takes over the four ways a
Click'n'Load button submits (`fetch`, `XHR`, `HTMLFormElement.submit`, and a
capture-phase `submit` listener), decodes the payload with `cnl.js` — AES-128-CBC,
key equals IV, both padding conventions found in the wild — and hands the links
to the service worker, which relays them to the chosen instance with
`origin: 'cnl'`. The page is answered `success\r\n`, exactly as a local
JDownloader would answer it. Detection works because `cnl-main.js` sets
`window.jdownloader` at `document_start`, before the site's `jdcheck.js` looks.

It is **on from the first second** — it is what most people install this for —
and the switch on the options page really removes it:
`chrome.scripting.unregisterContentScripts` takes both scripts away, verifiable
with `chrome.scripting.getRegisteredContentScripts()`, which is more than a
static `content_scripts` entry could ever offer.

This is the answer to the case a loopback port cannot serve: KnightLoader on a
server, a browser on a laptop, and a CnL button on a website that only knows how
to talk to `127.0.0.1:9666`.

**Selection Rules** — pre-defining which instance a given file type goes to, the
way MyJDownloader's extension can — is still not built. Choosing per send and
setting a default are; a rule engine on top of that is real but niche, and
nothing about this shape blocks it later.

## PWA share target

`web/public/manifest.webmanifest` declares `share_target` pointing at
`/quickadd` with a plain `GET` (`url`/`text`/`title` become query
parameters) — the same shape the bookmarklet and the extension already use,
so there is exactly one page that knows how to turn a shared blob into a
staged link. `web/public/sw.js` is a deliberately empty pass-through service
worker; it exists only because most browsers gate the install prompt behind
"has a fetch-handling service worker", not to cache anything (see that
file's own comment for why caching this app's assets a second time, next to
`internal/api/api.go`'s existing ETag scheme, would be a bug and not an
optimisation).

Installing is what turns the share target on: an uninstalled tab has no
Share-menu entry to offer. `web/src/lib/pwaInstall.ts` exports
`useInstallPrompt()`, a small shared hook around the browser's
`beforeinstallprompt` event — Settings > Browser & App uses it for its own
"Install" button. That tab is now the only caller: the app card moved there
from the Zugang tab, so the two install buttons that used to sit on separate
pages are one, and the event is captured once because there is only one place
left that wants it.

## What this deliberately does not do

No hosted relay, and no attempt to speak MyJDownloader's own vocabulary or
protocol — the same ruling `/api/help` states for the API generally applies
here: KnightLoader reaches exactly the instance you configured, directly,
and there is nothing else to sign into.
