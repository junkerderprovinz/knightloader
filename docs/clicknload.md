# Click'n'Load

Click'n'Load ("CnL") is how a website hands a batch of links straight to a
download manager. It is a de-facto standard rather than a specification: it is
whatever JDownloader implemented, and every site that offers a "Click'n'Load"
button talks to that. KnightLoader answers the same protocol, so those buttons
work without the site knowing or caring which program is listening.

## How a site actually does it

Three steps, all aimed at `127.0.0.1:9666` — the port JDownloader has listened on
for two decades, and which every CnL button hard-codes.

**1. Detection.** The page loads

```html
<script src="http://127.0.0.1:9666/jdcheck.js"></script>
```

and checks whether the global `jdownloader` came out true. KnightLoader answers
with `jdownloader=true;` so the button appears.

**2. Submission.** Clicking posts a form to `/flash/addcrypted2`:

| Field | Contents |
|---|---|
| `crypted` | the link list, AES-128-CBC encrypted, base64 encoded |
| `jk` | a one-line JavaScript function returning the hex key; the key doubles as the IV |
| `passwords` | archive passwords for the download, newline separated (optional) |
| `source` / `package` | where the links came from, used as the package name (optional) |

The encryption is not security — the key travels with the ciphertext. It exists
so a link list is not sitting in plain text in the page source for a scraper to
harvest.

KnightLoader **extracts** the hex key from `jk` with a regular expression rather
than executing the snippet. `jk` is attacker-controlled JavaScript from an
arbitrary website; running it would be an obvious way to lose the machine.

**3. Plain variant.** Older or simpler sites post an unencrypted list to
`/flash/add` with a `urls` field. Both are supported, over POST and GET.

## The three things that stop it working

### Private Network Access

Chrome will not let a page on the public internet open a connection to a private
or loopback address unless the target opts in. The browser sends an `OPTIONS`
preflight first and expects `Access-Control-Allow-Private-Network: true` back.

Without that header the failure is quiet in the worst way: sites that submit
with a plain HTML form still work, while sites that use `fetch` or `XHR` fail
with nothing in the UI to explain it. KnightLoader answers the preflight.

### Cross-origin rules

A page at `https://example.com` posting to `http://127.0.0.1:9666` is a
cross-origin request, so the CnL routes send `Access-Control-Allow-Origin: *`.

That wildcard is deliberate and is confined to the CnL listener. It is the
protocol's own requirement — any site must be able to reach it, that is the
entire point — and the listener binds `127.0.0.1` only, so nothing off the
machine can talk to it. The main API on port 8749 does the exact opposite and
refuses foreign origins outright. Do not "fix" one to match the other.

### Mixed content

An HTTPS page loading `http://127.0.0.1:9666/jdcheck.js` looks like mixed
content, which browsers normally block. Loopback is the exception: both Chrome
and Firefox treat `127.0.0.1` and `localhost` as potentially trustworthy, so the
script tag is allowed. Nothing to do here, but it explains why the address must
stay literally `127.0.0.1` rather than a hostname that happens to resolve there.

## When KnightLoader is not on your desktop

This is the case that matters for the primary deployment, and the one that plain
CnL cannot solve: the container runs on a NAS, and `127.0.0.1` inside that
container is not the `127.0.0.1` your browser means. The site posts into the
void.

Publishing port 9666 does not help. The address is hard-coded in the site's
JavaScript; it will never dial your NAS.

**The bridge.** Run the same binary on your own machine in bridge mode. It
listens on `127.0.0.1:9666`, speaks CnL to the website, and forwards what it
decodes to the remote instance over the normal REST API:

```sh
knightloader -bridge http://nas:8749
# with a password-locked instance:
knightloader -bridge http://nas:8749 -bridge-password 'your-ui-password'
```

It downloads nothing itself and needs no data directory — it is a few hundred
kilobytes of forwarding. The remote does the work, and the links land in its
collector exactly as if you had pasted them.

**The desktop build** needs none of this: it is the server, running on your
machine, so its own CnL listener is already the one the browser means.

## Configuration

| Var | Default | Meaning |
|---|---|---|
| `KL_CNL` | `9666` | listener port on `127.0.0.1`; `0` disables it |

In the container `KL_CNL` defaults to `0`, because a listener on a loopback
address inside a container can never be reached and starting one would only be
misleading. Use the bridge instead.

## Checking it by hand

With KnightLoader (or a bridge) running:

```sh
curl -s http://127.0.0.1:9666/jdcheck.js
# jdownloader=true;

curl -s -X POST http://127.0.0.1:9666/flash/add \
  --data-urlencode 'urls=https://example.com/file.bin' \
  --data-urlencode 'package=Manual test'
# success
```

The links appear in the collector. If `jdcheck.js` answers but a real site's
button does nothing, the preflight is the first thing to look at — open the
browser's network panel and check whether the `OPTIONS` request came back with
`Access-Control-Allow-Private-Network`.

## What CnL does not cover

Right-click-send-to-KnightLoader on an arbitrary link is a browser extension's
job, not CnL's: CnL only exists where a site chose to put a button. Container
files (`.dlc`, `.ccf`, `.rsdf`) are also out of scope — `.dlc` decryption
requires a call to a jdownloader.org service with a registered application key,
which is not something this project can honestly ship.
