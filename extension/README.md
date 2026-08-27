# KnightLoader browser extension

A Manifest V3 extension: right-click a link, a selection, or a page and send
it to your own KnightLoader instance. See `docs/browser-tools.md` at the repo
root for the full picture, including how this relates to the bookmarklet and
the PWA share target.

## Versions

This extension carries its own version in `src/manifest.json` and is tagged on
its own (`extension/vX.Y.Z`), separately from KnightLoader itself - it is
installed separately and upgrades separately, so one shared number would be
wrong in one direction or the other. See the Versioning section of the root
`CHANGELOG.md`.

The copy most people run does not come from that tag. The zip served by
Settings > Browser tools is built from the copy embedded in whatever server
binary is running (`embed.go`), so that one tracks the server. The tag exists
for a browser store submission and for anyone who wants a fixed download.

## Loading it

**From a running instance (recommended for most people):** Settings > Browser
tools > Download extension. The zip is generated with this instance's own
address already filled in, so there is nothing to configure afterwards.

**From this checkout (for development):**

1. `chrome://extensions` (or the equivalent `about:addons` /
   `edge://extensions` page) → enable Developer mode.
2. "Load unpacked" → pick `extension/src`.
3. Open the extension's Options and add an instance. The form arrives
   pre-filled with `http://localhost:8749`, which is right for a KnightLoader
   running on the same machine — it is a suggestion, not a silent default:
   `config.default.json` ships with `instanceUrl` empty on purpose, so a copy
   loaded from a checkout never points at an address nobody looked at.

## Instances it cannot open a connection to

Some peers have no address at all: a desktop build opens no API listener, and
a relay-only peer is reachable purely through the relay. Neither can be opened
in a browser tab.

They are still usable here. **Sync known instances** keeps such a peer and
records which instance told the extension about it; sending to it opens
`<that instance>/quickadd?to=<peer>`, and that instance forwards over whichever
transport it already has. So the extension needs no relay client, no second
copy of the relay key, and no persistent socket in a service worker the browser
is free to kill.

Host permissions are asked for the exact origins already configured, from the
sync button's own click, never wider. Opening Options prompts for nothing: the
automatic check only touches origins an earlier click already granted.

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
