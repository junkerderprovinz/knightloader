# Connecting

Four things can talk to a KnightLoader: the web UI it serves itself, a desktop
build, the Android app, and the browser extension. Any of them can also talk to
a *second* KnightLoader. This is how each of those connections is made, and what
happens when one of them cannot be.

The short version: on one network, nothing needs configuring. Across networks,
twelve words connect every instance you run. The relay section after that is
the same machinery reached the manual way, for anyone who wants to name the
relay and the key themselves.

## On one network

An instance announces itself every five seconds over UDP multicast, to an
administratively-scoped group that routers do not forward off the local network.
Every other instance listening picks it up.

The Instances page shows what it heard under **Found on your network**, with
an **Add** button on each row.

Being on the same network is *not* consent, so nothing is connected until that
button is pressed. A guest laptop and an IoT device sit on that network too.

Adding stores an address. It exchanges nothing: no credential travels in
either direction. A credential exchange triggered by an announce that anything
on the LAN can send would let any device there help itself to a token.

So if the instance you add has a password set, it will refuse the calls that
follow, and the page says so. An address is not a credential; the connection
phrase is, and it covers every instance in the group at once instead of one
peer at a time. An instance with no password works immediately.

Nothing in an announce is trusted. Anything on the network can send one, so the
fields are length-capped on arrival and the list is bounded, so a device
announcing endless invented instances cannot grow it without limit. What a row
shows is a claim. That is why the address is displayed next to the name, and
why nothing is added until you decide to add it.

Multicast blocked, or no network at all, means an empty list. Everything below
still works.

Instances in the same phrase group use the same announce to find each other.
A member tags its announce with a MAC over everything in it, its time
included, under a key derived from the phrase
(`SHA-256("knightloader/peer-auth/v1" || secret)`). Another member recognises
the tag and files the announcer as a member it can call directly; to anyone
else the tag is noise and says nothing about the phrase. An announce older
than two minutes does not pass, so one played back later does not bring a
departed member back. Members on one network call each other over plain HTTP
at `/api/group/call`: every call is sealed under the frame key, as over the
relay, signed with the peer-auth key and refused when it is stale or has run
before. The relay only carries what cannot go directly. They do not show under
**Found on your network**, because they are already on the page as cards.

## Across networks: the connection phrase

**Settings → Pairing → Generate phrase** on the first instance. Twelve
words come back, in a window beside the QR code for the Android app, with Copy
next to Close. On every other KnightLoader you run, press **Enter phrase** in
the same place and paste them into the window it opens, and they find each
other on one network and, through the relay, across networks and behind NAT,
with no port forward, no domain and no account. The **Pairing** button on the Instances page leads
there, and stands in the middle of that page while there is no group yet.

The section opens with the same three steps as numbered cards. The field for the
words takes a paste as it comes: one word per line, numbered, or separated by
commas. Twelve numbered slots fill as you type, a word that is not on the list
is named with its position straight away, and **Pair** stays off until all
twelve are known words. **Paste** beside it reads the clipboard into the field
the same way. A browser that will not hand the clipboard to the page, outside
HTTPS or with the permission refused, gets one line asking to paste with
Ctrl+V or a long press instead.

A badge at the end of the phrase card's first line says where the instance
stands. How the relay is doing is on the relay card, not here.

| Badge | When |
| --- | --- |
| New group | right after **Generate phrase**, while the next instance has not come |
| Searching | the first minute after entering the phrase, or while members that came are out of reach |
| Still alone | a minute has passed and no other instance or phone has shown up |
| Paired | another instance of the group, or the Android app, is there right now |

Below the badge the card lists the group, one row each and all of one height:
this instance, the other instances with Direct or Via relay, and the phones
that are connected. **Show phrase** opens the words in the same window
**Generate phrase** does, after the login password if one is set.

The server keeps when the instance joined its group and whether anyone has
come since, so the minute counts on its clock and survives a reload. A group
from a build before this counts as joined when the instance starts.

**Still alone** comes with what to do, in two tiles side by side. **Nothing
entered over there yet?** opens the window with the twelve words. **Generated
a phrase over there too?** is the usual cause, two instances that each
started a group of their own: it opens the window **Enter phrase** opens, and
**Pair** there joins the other group in one step, since nobody is in this
one. Without a relay the card
adds that an instance on another network cannot find this one. With a relay
that cannot be reached, one line says so and **What to check** lists it:
outgoing HTTPS on port 443 to `parleyport.halleluja.design`, a firewall or DNS
filter, and for your own relay whether it runs and its address is right.

Before the instance has a phrase it dials no relay, since the relay key comes
from the words, so the relay card says **No group yet** rather than Not
connected. **Generate phrase** pressed by mistake is undone with **Enter
phrase** under "Already have a phrase?", which leaves the empty group and
opens the window for the words.

Every instance tells its group where its web interface is: its first known
domain (the addresses under **Settings → Pairing**), else its address on its
network, such as `192.168.20.86:8749`. The address travels in the announce
members already send, sealed on the relay and signed on the network. An
instance drops it from an announce that comes from outside the group. The Instances page shows it under each
instance's name and **Open** goes there. A phone shows no address.

The words *are* the credential. Whoever has them reaches every instance in the
group, so keep that in mind before you read them out over the phone.

The words carry a 128-bit secret and nothing else: no address, no name. The
default relay is `parleyport.halleluja.design`, run by the project, and its
address is compiled into every build, which is what keeps this to twelve words
instead of a URL plus a key. An instance on a relay of its own puts that
relay's address in the QR code beside the words, so the Android app finds the
group there when it scans the code (see [The Android app](#the-android-app)).

The list they are drawn from is BIP39's, the one hardware wallets use. That
has nothing to do with cryptocurrency, and the UI never calls this a wallet
seed: those
2048 words were chosen so no two share their first four letters, none are
near-homophones and none carry accents, which is what makes a phrase safe to
read down a phone line and to type on a mobile keyboard. Four checksum bits
ride along, so a mistyped or swapped word is refused on the spot, naming the
word and its position, rather than becoming a connection that silently never
finds its sibling.

**The relay never learns the phrase.** What an instance sends is
`SHA-256("knightloader/relay/group-key/v1" || secret)`. The relay matches
connections that present the same derived key and forwards frames between
them; it has no account list, no registration step and no database. So whoever
runs it (us at `parleyport.halleluja.design`, or you) cannot reconstruct anybody's
words.

**And it cannot read what it forwards.** A *second* key comes out of the same
secret under its own domain, `SHA-256("knightloader/relay/frame-key/v1" ||
secret)`, and every proxy frame is sealed with AES-256-GCM under it. The relay
sees which instance a frame is addressed to and which request it answers,
because it routes on those, and nothing else: not the path, not the body, not
the API token a phone attaches. The two domains are what makes this work: the
relay is handed the group key in every hello frame, so a frame key derived
from *that* would be one it already holds.

The routing fields are bound into the seal as additional data, so a relay
cannot take a frame addressed to one instance and deliver it as another's. The
one field it does author is the error it returns when nobody is connected
under a target id, which it has to, holding no key. That lets a hostile relay
claim an instance is absent (a denial of service it could equally perform by
dropping the frame), but not fabricate an answer: a response with no sealed
payload is never mistaken for one.

If you configure a relay by hand-entered key instead of by phrase, there is no
secret to derive from and the frame key comes from the relay key itself. That
still seals the traffic against anything sitting *between* you and the relay
(a reverse proxy, a TLS terminator, a captured log), but not against the relay
operator, who is handed that key. That trade fits the case it exists for:
somebody hosting the relay themselves.

The **Relay** card under the phrase card picks the relay: **Project relay**,
**Own relay** or **No relay**, with a badge that says Connected, Not connected
or No relay. The phrase card shows the same badge beside its state. For your own relay the
card names the two ways to run one, the [ParleyPort](https://github.com/junkerderprovinz/parleyport) container or any
instance of the group that can be reached from outside with **Serve as relay**
switched on, and takes its address, which every instance in the group needs;
the same phrase then works against it, because the phrase carries the secret
and not the address. An address starting with `ws://` or `http://` is allowed,
for a relay on your own network, with a warning that the relay key then
crosses the network unencrypted. The Android app does not follow such a relay:
it only connects over `wss://`. **How does it work?** opens the route
picture, what the relay sees next to what it never sees, and how the traffic
is encrypted. The same fields are `relayMode`, `relayUrl` and `relayServe`
under **Settings → Advanced**.

**No relay** keeps the group to one network: its members still find and call
each other there, but nothing outside it reaches them. The Android app and
the browser extension always come through a relay, so they need one.

**A login password is recommended, not required.** Without one the phrase
card says that anyone who can open this web interface can see the twelve
words and so control every instance in the group, with a button to the
Security page, where the password is set. The note can be closed, and stays
closed in that browser. With a password set, **showing the phrase again**
asks for it once more. A live session is not enough: it may have been opened
hours ago on a screen nobody is sitting at, and what is behind that button is
not this instance's password but the key to every instance in the group.

**Leaving** forgets the secret and stops dialling. The other instances keep
going without it; a phrase is a group, not a pairing.

## When neither can reach the other: the relay

Both ends dial *out* to a relay and meet there, so neither needs a public
address, a port forward or a domain. Traffic is JSON frames over one WebSocket
each.

This is the same machinery the connection phrase uses, reached the manual way:
a URL and a key you choose and type into both ends, rather than twelve words
that carry the key and already know the address. The phrase is the shorter
road to the same place; this one exists for anyone who wants to name the relay
and the key themselves.

There is an official relay, `wss://parleyport.halleluja.design/relay/connect`,
which is what a phrase points at unless you override it, and running your own
is a first-class option, not a fallback. Run [ParleyPort](https://github.com/junkerderprovinz/parleyport) anywhere both ends
can reach, put the same key in both, done. Set `PARLEYPORT_DOMAIN` and it
terminates TLS itself, getting and renewing its own certificate over
TLS-ALPN-01: no reverse proxy, no certbot, no renewal cron, and no port 80. The
challenge completes inside a handshake on 443, so the firewall in front of it
opens one port.

`PARLEYPORT_DOMAIN` takes a comma-separated list, and the certificate covers
every name in it. That is for one situation and it is worth knowing before you
need it: moving a relay to a new address. Old clients keep dialling the old
name, and a whitelist of one would stop issuing a certificate for it the moment
you switch, so those clients fail in the handshake instead of moving across.
Run both names for as long as anything still dials the old one, then drop it.

The relay takes all of its settings from the environment. Its one flag,
`-version`, prints the version and the commit it was built from and exits
without opening a port. Any other flag or argument is refused, so a mistyped
flag cannot start a relay. `knightloader -version` does the same
for the server.

### Or let one instance be the relay

**Settings → Pairing → Relay → Own relay → Serve as relay**
(`relayServe`). The relay then answers under `/relay/connect` on the address
that instance already uses, behind the same reverse proxy and the same
certificate, and the other instances put that address in their own relay
address field. It needs no second container, no second port and no second
certificate.

For an instance a proxy serves under a path, enter the address with the path,
such as `https://example.com/kl`, and the others dial `/relay/connect` below
it. A `relayUrl` whose path already ends in `/connect` is dialled as it stands,
for a relay a proxy has mounted somewhere else.

It admits only the relay key that instance stores, so switching it on does not
turn a published address into a meeting place for whoever finds it. With the
switch off, `/relay/connect` answers 404, the same as any build that never had
the feature.

What this does *not* change is the one requirement a relay has: it is the third
point both sides dial out to, so it has to be reachable by both. Turning it on
inside a desktop install that nothing can reach from outside gives the other
instances nothing to dial. The instance that hosts it is the one with the
address (a server, a NAS, anything already behind a domain), and the ones
behind NAT are what it exists to connect.

The relay operator carries your frames, so they see who is talking and when,
and a relay you do not run is a relay you are trusting with that. Run your own
if it matters. They cannot read your phrase: they only ever receive a hash of
it.

**Being on the relay is what authenticates a sibling.** A request arriving
this way came off a socket the relay only joins to other connections
presenting the same group key, so the sender has already proved it holds the
phrase before any handler runs. There is no second credential to exchange,
which is what retired the pairing code: whoever can present the group key
could have joined the group and been handed one anyway.

That makes the reachable surface the thing to bound, and it is an allowlist
rather than a property each route happens to have. A sibling may read and
drive **tasks, links and the queue**, answer or skip the captchas holding them
up, load a widget captcha's page for the phone and say when it will not load,
and may read whether a password is set,
who else is in the group, and the instance's own accent and corner shape. It
cannot read the settings or the accounts, change the password, mint an API
token, or ask for the phrase back. A route added later is outside the list
until somebody puts it in.

A relay peer is never written to disk. It exists for as long as the relay sees
it, and one remembered across a restart would be a peer that cannot be reached
and cannot be explained.

## The Android app

- **The phrase** is the one way in, as it is in the browser extension: twelve
  words, typed or scanned from the QR the web UI shows beside them, and every
  instance in the group appears at once, with no address, no token and nothing
  to look up. The phone derives the same key its siblings do and dials the same
  relay, which is what authenticates it, so a password on an instance costs
  nothing extra here. The field reads a paste the way the web UI's does: twelve
  numbered slots fill as the words arrive, a word not on the list is named with
  its position, and **Connect** stays off until all twelve are known words.
- **Which relay** comes from the QR code. Typed or pasted words pair on the
  default relay. An instance set to its own relay adds that relay's address to
  its QR code on a line under the words, and a phone that scans the code pairs
  there. The app takes such an address only if it starts with `wss://`, and
  says so when it does not. On the default relay the code holds the twelve
  words alone, so every build of the app reads it; a build too old for the
  second line refuses the longer code because it counts thirteen words.
- **The phone has a card of its own** on the Instances page of every instance
  in the group: the device name Android reports, Connected or Not connected,
  and when it was last seen. It announces itself on the relay with that name
  and the kind `mobile`, which keeps it apart from the instances and from the
  browser extension. A connected phone counts as somebody in the group, so an
  instance whose only partner is a phone shows Paired.
- **A connection saved by address** in an earlier build of the app keeps
  working if the address starts with `https://`, but the app no longer makes
  one. One saved with an `http://` address does not, since the app permits no
  cleartext traffic: its card says it has to be added again with the twelve
  words. Those builds took the address
  typed in, scanned from the Access tab's QR or found with **Find on this
  network**, plus a token where the instance had a password.

  Find on this network, in those builds, asks every address on the phone's own
  /24 for `/api/health` over HTTP and fills in the address of anything that
  answers as a KnightLoader. React Native has no UDP socket, so the app cannot
  join the multicast group the servers use.

  That route is frozen because of this. It answers `{"status":"ok","version":…}`
  on a 200 for as long as the process is up, whatever is actually wrong with the
  instance, and those builds compare that string literally, so an instance that
  answered `degraded` would stop being findable by every phone still running
  one, and phones update on their own schedule rather than with the
  container. The container's own `HEALTHCHECK` and the Click'n'Load bridge read
  it the same way. New fields may be added to it; the two that are there may not
  move, and the status may not stop being `ok`.

  The real state is a second, session-guarded readout: `GET /api/health/detail`
  answers every part of the instance with a state of its own, the queue by why
  it is waiting and why it failed, room on the target folders and how long the
  process has been up. `GET /api/metrics` is the same reading as Prometheus
  exposition text and answers 404 until the switch on the Health settings page
  is turned on. Neither is open, and neither is forwarded to a peer over the
  federation or the relay: both describe *this* machine's disks and sidecar, and
  a row of them drawn under a peer's name would name the wrong box.
- **Captchas** are answered on the phone. A card on the instance's downloads,
  a count on its overview card and a banner over the open screen lead to a list
  of what is waiting, and picture and click captchas are answered right there.
  reCAPTCHA, hCaptcha and Cloudflare Turnstile open in a window of their own,
  on either kind of connection. The app fetches the instance's widget page,
  over the relay as well, and shows it under the address of the hoster's page
  the captcha came from. The captcha service then sees the hoster's website,
  as it would in a browser on that page. Many hosters tie their captcha to
  their own domains, and every Turnstile key is tied that way, so anywhere
  else the service refuses to run.
  The banner also says when a captcha timed out or was answered somewhere else,
  as the web UI's messages do.

  The app watches the instance it has open, and only while it is in front; the
  overview counts what waits on the others. A captcha that arrives while the
  app is in the background is announced when you come back to it, as long as
  Android has kept the app in memory. After Android has closed it, the card on
  the downloads still shows what is waiting, but no banner comes up. The app
  also keeps its own connection in the background and posts a notification for
  a new captcha, even when it is closed and after the phone restarts. With
  **Stay connected** switched off in its **Notifications** settings, it does so
  only while one of your instances is busy. The background connection does not count as watching.
  While the app watches, the instance
  counts you as watching for the captchas the app can answer, which is every
  kind except a captcha service KnightLoader does not know. With **Only when
  nobody is watching** switched on on the Captcha settings page, the captcha
  accounts wait for your answer on those first, and a captcha the app cannot
  answer does not wait for it. Nor does a widget captcha that will not load in
  the app, until Refresh loads it after all. The card
  says what the captcha accounts are doing, as the web UI's captcha window does.

## The browser extension

The extension takes the same twelve words and nothing else. There is no address
field, no name field, no token field and no sync button on the options page any
more. The extension carries its own relay client (`extension/src/relay.js`) and derives
the group key from the phrase with a WebCrypto port of `internal/seedphrase`
(`extension/src/phrase.js`), so it is a group member in its own right rather
than a guest of one configured instance.

That replaces the whole previous shape and everything that hung off it:

- **The roster is read live**, when a window opens, instead of being stored and
  synced. An instance that is switched off is not offered; one that came online
  a minute ago is. Nothing tells this browser anything. It asks.
- **A peer with no address of its own** (a desktop build, or one reachable
  only through a relay) is now reached *directly* through the relay, not
  forwarded on its behalf by a sibling that happens to have an address.
- **Sends are `POST /api/links` over the relay**, admitted because membership
  is the credential. No window opens, no session cookie is involved, and the
  `sameOrigin` guard is not worked around, because it is not on that path.

The extension asks for no site access at install time. Click'n'Load, taking
over downloads and finding media each ask for it when switched on; see
`docs/browser-tools.md`.

## API tokens and their rights

A script, Sonarr, a dashboard, or the phone app connected by address rather
than by phrase signs in with an API token: **Settings → Security → API
tokens → New token**. Every token has a name, can be revoked on its own, and
carries some of four rights:

| Right | What it covers |
|---|---|
| Read | the download list, the queue, the history, the statistics, the health readout and the live stream |
| Add | links, torrents, NZB files handed over by Sonarr or Radarr, and containers |
| Control | pausing, resuming, removing and reordering downloads, the queue's own switches, captchas, unpacking, and the accent, corners and rainbow the apps take over |
| Admin | settings, accounts, tokens, instances, scripts, the logs, backups and restarts, and picking the folder a download goes to |

The window that creates a token offers four presets: **Full access** (all
four), **Add and read**, **Read only**, and **Custom**, which shows one switch
per right.

**Add and read** is enough for Sonarr and Radarr, through either of their
doors. Through SABnzbd's they send their key in the address (`?apikey=`), where
it ends up in the log of any proxy in between, and a key that can only add and
read is a smaller loss than one that can change the password. When Sonarr clears a finished download after importing it, the
bridge stops reporting it. The download itself stays in your list unless the
token also has Control; then it is removed, with its files if Sonarr asks for
that. Removing a download that is still running needs Control: without it
Sonarr shows the refusal, and the download carries on and stays in Sonarr's
queue.

Through qBittorrent's door Sonarr also resumes every torrent right after adding
it, which Add covers as long as nothing in the torrent was paused. Resuming a
paused torrent needs Control, and so do the qBittorrent settings in Sonarr that
act on a torrent after the add: Priority "First", Initial State "Force Started"
and a Post-Import Category. With Add and read the torrent stays as it was.

Such a key cannot pick where files land either. A `dir` in `POST /api/links`
or `POST /api/tasks/options`, or a save path in qBittorrent's `torrents/add`
or `torrents/createCategory`, needs Admin, whatever the route needs otherwise,
because a folder of the caller's choosing could be any folder the instance can
write to. Without one, links go where the download folder, a category or a
Packagizer rule puts them.

**Read only** suits a dashboard, or a monitoring system reading `/api/metrics`.
The phone app uses Read, Add and Control and never needs Admin. The Modules
page warns when no token has the rights the Sonarr bridge or `/api/metrics`
needs.

The phone app's Play button asks for a play link with `POST
/api/tasks/{id}/play`, which needs Read. A player app opens the link without a
token, so the link itself is the key: it opens that one file and nothing
else, for twelve hours or until the instance restarts, and anyone who has it
can play the file in that time. For a torrent of several files that is the
file Play picked, not the rest of the torrent.

A call the token has no right to is answered with a 403 that names the missing
right:

```json
{"error": "this API token does not have the \"control\" right", "code": "tokenScope", "params": {"scope": "control"}}
```

The SABnzbd door puts the same sentence in SABnzbd's own error document,
because that is what Sonarr shows, and the qBittorrent door sends it as the
text of its 403. `GET /api/help` lists the right each route
needs as its `scope`. The table behind it is `internal/api/scopes.go`, and a
test fails for any route missing from it, so a new route cannot be reached with
a narrowed token until somebody has decided which right it needs.

Only tokens are narrowed. A browser signed in with the password, a sibling that
came in over the relay with the phrase, and the tokens instances hand each
other when they pair all have every right. A token made before tokens had
rights keeps all four, so scripts and apps set up earlier keep working. A token
that may manage tokens can only issue ones with rights it holds itself. A call
a token forwards to another instance is checked here, against the right it
would need on this instance, before it leaves; forwarding needs no right of its
own, so a token that may only add can add links on a peer too.

The rights are kept in `token-scopes.json` beside `tokens.json` in the data
directory. A token stays as narrow as it was even after going back to a
release without rights for a while and upgrading again.

The secret is shown once, when the token is made. The instance keeps a hash of
it and never sends it back.
