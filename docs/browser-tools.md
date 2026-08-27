# Bookmarklet, browser extension, and the PWA share target

Three ways to hand KnightLoader a link from somewhere that is not KnightLoader
itself: a page you are already looking at, a right-click menu, or your
device's own Share sheet. All three are configured from **Settings > Browser
tools**, and all three land on the same page, `/quickadd`
(`web/src/pages/QuickAdd.tsx`), which is the only place that actually calls
`POST /api/links`.

This is a different problem from Click'n'Load (`docs/clicknload.md`): CnL
answers a button a *website* chose to put on its own page, at a fixed port a
browser reaches over loopback. Right-click-send-to-KnightLoader on an
arbitrary link, with no button anywhere, was always CnL's stated limit — see
that document's closing section. These three tools are what fills it in, and
none of them touch port 9666 or the CnL protocol at all.

## Why all three open a window instead of calling the API directly

`internal/api/api.go`'s `sameOrigin` middleware refuses any request that
carries a Origin header not matching this instance's own host — deliberately,
so that no other page can drive a signed-in browser's session against the
API. There is no bearer token these tools could hold instead of a session
cookie. So none of them call `/api/links` from wherever they are running:
each one opens a small window at `<this instance>/quickadd?...`, same-origin,
where the ordinary session cookie (and the ordinary sign-in screen, if the
instance is password-locked) applies exactly as if you had typed the address
in yourself. `/quickadd` is what actually stages the link.

This is also why none of them need a copy of the instance's password: the
bookmarklet and the extension both only ever need to know *where* the
instance is, never a credential of their own.

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

Downloading the zip from a running instance bakes that instance's own address
into `config.default.json`, so "load unpacked" and go — no options page visit
needed for the single-instance case. A copy loaded straight from a git
checkout (`extension/src`, unpacked) has nothing to bake it from, so it ships
with an empty default and opens its own Options page on first run instead —
see `extension/README.md`.

It offers four context-menu entries (page, link, image, selection) and a
toolbar popup ("send this page"). Each one is `chrome.windows.create({ type: 'popup',
url: quickAddUrl(...) })` — no `host_permissions`, no direct fetch to the
API, for the same `sameOrigin` reason above. Permissions are deliberately
narrow: `contextMenus`, `storage` (for the configured address) and
`activeTab` (for the popup to read the current tab without a broad host
grant).

**Selection Rules** — pre-defining which of several KnightLoader instances a
given file type goes to, the way MyJDownloader's extension can — is not
built. One configured address is what "point the extension at the instance
directly" (the census's own phrasing for this row) means; routing between
several instances by rule is real but niche, and nothing about this shape
blocks adding it later.

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
