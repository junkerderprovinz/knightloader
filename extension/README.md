# KnightLoader browser extension

A Manifest V3 extension: right-click a link, a selection, or a page and send
it to your own KnightLoader instance. See `docs/browser-tools.md` at the repo
root for the full picture, including how this relates to the bookmarklet and
the PWA share target.

## Loading it

**From a running instance (recommended for most people):** Settings > Browser
tools > Download extension. The zip is generated with this instance's own
address already filled in, so there is nothing to configure afterwards.

**From this checkout (for development):**

1. `chrome://extensions` (or the equivalent `about:addons` /
   `edge://extensions` page) → enable Developer mode.
2. "Load unpacked" → pick `extension/src`.
3. Open the extension's Options and set the instance URL by hand — a copy
   loaded this way has no request to bake an address into, so
   `config.default.json` ships with `instanceUrl` empty on purpose rather
   than pointing every developer checkout at somebody else's server.

## Why it opens a window instead of calling the API directly

KnightLoader's main API refuses any request carrying a foreign `Origin`
header (`internal/api/api.go`'s `sameOrigin` middleware) and authenticates by
session cookie only — there is no bearer token this extension could hold
instead. So `background.js` never calls `/api/*` itself: every action opens
a small window at `<instance>/quickadd?...`, same-origin, where the normal
web session (and normal sign-in, if the instance is password-locked) applies
exactly as it would if you had typed the address in yourself. That page is
`web/src/pages/QuickAdd.tsx` — the bookmarklet and the PWA share target land
on the identical page, so all three entrances share one implementation of
"stage this and say what happened."
