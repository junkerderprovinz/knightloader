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
`/flash/add` with a `urls` field.

**4. addcrypted (v1).** The oldest variant, from before addcrypted2 existed:
the site posts a `crypted` field encrypted against JDownloader's own RSA
public key rather than a key that travels with it, so nobody but a real
JDownloader can open it. KnightLoader does not hold that key and never will —
see "Container files" below for why that is a deliberate line rather than a
gap, and how it is answered anyway when a JD backend is configured.

Every one of these four is POST only, deliberately: a GET here would be a
browser "simple request" — no preflight, no user gesture, no navigation — so
any page in the world, an ad iframe or an email preview image tag included,
could queue arbitrary downloads and archive passwords into this instance. A
handful of read-only routes some sites and extensions probe before ever
trying to submit — `/flash/addcnl`, `/flashgot`, `/alive`, `/favicon.ico`,
`/crossdomain.xml`, alongside `jdcheck.js` and the bare detection paths above —
answer GET only and never touch a link or a password.

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

There are two answers, and the browser extension is the one most people want.

**The browser extension.** Switch on Click'n'Load in the extension's options
(Settings → Click'n'Load) and it catches the submission *inside the page*,
before it is ever sent, then hands the links to whichever instance you pick —
the same chooser every other send from the extension uses. Nothing runs on your
desktop, no port is owned, and it works wherever the instance is.

How, because it looks impossible at first: an extension cannot listen on a TCP
port, so it never receives the POST. It patches the page's own `fetch`, `XHR`
and form submission at `document_start` and takes the payload before it leaves
(`extension/src/cnl-main.js`), decodes it in the browser
(`extension/src/cnl.js`, the same AES-128-CBC this file describes), and answers
the site with the identical `success\r\n` a real listener would. The detection
step is answered the same way: the interceptor declares `jdownloader = true`
before any script the page brings, so the button appears.

That means running code in every page you visit, which is a real permission and
is treated as one: **nothing is registered until you switch the feature on.**
The extension asks for site access at that moment, registers the two content
scripts if you agree, and unregisters them the moment you switch it off. A
fresh install has no access to any website at all. (JDownloader's own extension
requires that access up front, for everybody; this does not.)

**The bridge.** For someone who wants no extension at all, or a browser without
one. Run the same binary on your own machine in bridge mode. It
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

| Var / flag | Default | Meaning |
|---|---|---|
| `KL_CNL` | `9666` | listener port on `127.0.0.1`; `0` disables it |
| `-bridge-clipboard` | off | bridge mode only: watch the OS clipboard for hoster links (needs a `-tags bridgeclipboard` build) |

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

## Container files, and addcrypted (v1)

A `.dlc`, `.ccf` or `.rsdf` is encrypted the same way addcrypted (v1) is: the
key is issued to registered clients, and no open-source client generates or
holds one on its own. Rather than borrow somebody else's application key and
pretend to be their client, KnightLoader hands the bytes to the headless
JDownloader backend it already ships as its catch-all resolver, which has its
own key and does this legitimately (see `internal/container`'s package doc).
Handing KnightLoader one of these files (`POST /api/containers`) takes this
path; so does a site's addcrypted (v1) submission, by the identical route —
the payload is handed to JD as inline content instead of a fetchable URL,
because unlike an uploaded file it was never a file anywhere to fetch.

Without `KL_JD` configured, both are refused with that reason stated plainly
rather than a vague failure — see the main README's configuration table.

## Ambient clipboard watching (bridge only)

The bridge can also watch the OS clipboard and forward anything that looks
like a hoster link, without waiting for a CnL button or an explicit paste —
useful for a site with no Click'n'Load button at all. Off by default:

```sh
knightloader -bridge http://nas:8749 -bridge-clipboard
```

This is scoped to the bridge specifically because it is the one build with an
unambiguous claim on a user's own OS clipboard — the user started it, by hand,
on their own machine. It also needs a build with `-tags bridgeclipboard`: the
ordinary release binary (and the container image) does not carry the
clipboard-reading dependency at all, not merely leave it switched off, because
a headless server has no legitimate reason to touch a clipboard in the first
place. Passing `-bridge-clipboard` against a plain build logs that plainly
rather than doing nothing silently.

Watching is narrow on purpose: only a line that is essentially just a link
qualifies, not prose that happens to contain one, because nobody is watching
the result to catch an accidental selection before it queues. A small ring of
recently-forwarded content is kept so the same clipboard is not resubmitted on
every poll tick, and so KnightLoader's own "copy link" writing a link back
into the clipboard does not get read in as if it arrived from somewhere else.

## What CnL does not cover

Right-click-send-to-KnightLoader on an arbitrary link is a browser extension's
job, not CnL's: CnL only exists where a site chose to put a button. See
`docs/browser-tools.md` for that extension, the bookmarklet, and the PWA
share target.

The extension now covers both — the right-click entries, which have nothing to
do with this protocol, and Click'n'Load interception, which is entirely this
protocol. They stay separate features with separate permissions: the first
works on a fresh install, the second is off until switched on.
