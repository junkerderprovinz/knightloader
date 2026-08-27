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

The Instances page shows what it heard under **Found on your network**, with an
**Add** button on each row.

Being on the same network is *not* consent, so nothing is connected until that
button is pressed. A guest laptop and an IoT device sit on that network too.

Adding stores an address. It exchanges nothing: no credential travels in
either direction, which is deliberate - a credential exchange triggered by an
announce that anything on the LAN can send would mean any device there could
help itself to a token.

So if the instance you add has a password set, it will refuse the calls that
follow, and the page says so. An address is not a credential; the connection
phrase is, and it covers every instance in the group at once instead of one
peer at a time. An instance with no password works immediately.

Nothing in an announce is trusted. Anything on the network can send one, so the
fields are length-capped on arrival and the list is bounded - a device
announcing endless invented instances cannot grow it without limit. What a row
shows is a claim, which is exactly why the address is displayed next to the name
and why adding is a decision rather than a consequence.

Multicast blocked, or no network at all, means an empty list. Everything below
still works.

## Across networks: the connection phrase

**Settings → Access → Connect instances and remote access → Create a phrase.** Twelve words
come back. Type them into every other KnightLoader you run, and they find each
other - across networks, behind NAT, with no port forward, no domain, no
account and nothing to log into.

There is no account because there is nothing to log in to. The words *are* the
credential. Whoever has them reaches every instance in the group, which is the
one thing to understand before reading them out over the phone.

What the words carry is a 128-bit secret and nothing else - no address, no
name. The relay's address is compiled into the binary, which is exactly what
keeps this to twelve words instead of a URL plus a key.

The list they are drawn from is BIP39's, the one hardware wallets use. Not for
any cryptocurrency reason, and the UI never calls this a wallet seed: those
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
runs it - us, at `relay.knightloader.app`, or you - cannot reconstruct
anybody's words.

**And it cannot read what it forwards.** A *second* key comes out of the same
secret under its own domain, `SHA-256("knightloader/relay/frame-key/v1" ||
secret)`, and every proxy frame is sealed with AES-256-GCM under it. The relay
sees which instance a frame is addressed to and which request it answers,
because it routes on those, and nothing else: not the path, not the body, not
the API token a phone attaches. The two domains are what makes this work - the
relay is handed the group key in every hello frame, so a frame key derived
from *that* would be one it already holds.

The routing fields are bound into the seal as additional data, so a relay
cannot take a frame addressed to one instance and deliver it as another's. The
one field it does author is the error it returns when nobody is connected
under a target id, which it has to, holding no key. That lets a hostile relay
claim an instance is absent - a denial of service it could equally perform by
dropping the frame - but not fabricate an answer: a response with no sealed
payload is never mistaken for one.

If you configure a relay by hand-entered key instead of by phrase, there is no
secret to derive from and the frame key comes from the relay key itself. That
still seals the traffic against anything sitting *between* you and the relay -
a reverse proxy, a TLS terminator, a captured log - but not against the relay
operator, who is handed that key. It is the right trade for the case it exists
for, which is somebody hosting the relay themselves.

To run your own, put its address in `relayUrl` under **Settings → Advanced**
on every instance in the group; the same phrase then works against it, because
the phrase carries the secret and not the address. That page lists every
setting this instance has, so the self-hosting knobs live there rather than on
the card - the card is for the twelve words, which is what almost everybody
needs. `relayServe` is on the same page, for the case where one instance IS
the relay.

**Showing the phrase again** needs the instance password re-entered, when one
is set. A live session is not enough: it may have been opened hours ago on a
screen nobody is sitting at, and what is behind that button is not this
instance's password but the key to every instance in the group.

An instance with **no** password says so, loudly, before it mints anything -
and then mints it anyway if you say so. The phrase reaches every instance you
connect with it, so an unprotected one is a door into all of them, but that is
a judgement about your own network rather than something to be refused on your
behalf.

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

There is an official relay - `wss://relay.knightloader.app/relay/connect`,
what a phrase points at unless you override it - and running your own is a
first-class option, not a fallback. `docker compose` it anywhere both ends can
reach, put the same key in both, done. Set `KL_RELAY_DOMAIN` and it terminates
TLS itself, getting and renewing its own certificate over TLS-ALPN-01: no
reverse proxy, no certbot, no renewal cron, and no port 80 - the challenge
completes inside a handshake on 443, so the firewall in front of it opens one
port.

### Or let one instance be the relay

**Settings → Advanced → `relayServe`.** The relay then answers under
`/relay/connect` on the address that instance already uses, behind the same
reverse proxy and the same certificate, and the other instances put that
address in their own `relayUrl`. No second container, no second port, no
second certificate.

It admits only the relay key that instance stores, so switching it on does not
turn a published address into a meeting place for whoever finds it. With the
switch off, `/relay/connect` answers 404, the same as any build that never had
the feature.

What this does *not* change is the one requirement a relay has: it is the third
point both sides dial out to, so it has to be reachable by both. Turning it on
inside a desktop install that nothing can reach from outside gives the other
instances nothing to dial. The instance that hosts it is the one with the
address - a server, a NAS, anything already behind a domain - and the ones
behind NAT are what it exists to connect.

What the relay operator can see is stated plainly rather than implied: they
carry your frames, so they see who is talking and when, and a relay you do not
run is a relay you are trusting with that. Run your own if it matters. What
they cannot do is read your phrase - they only ever receive a hash of it.

**Being on the relay is what authenticates a sibling.** A request arriving
this way came off a socket the relay only joins to other connections
presenting the same group key, so the sender has already proved it holds the
phrase before any handler runs. There is no second credential to exchange,
which is what retired the pairing code: whoever can present the group key
could have joined the group and been handed one anyway.

That makes the reachable surface the thing to bound, and it is an allowlist
rather than a property each route happens to have. A sibling may read and
drive **tasks, links and the queue**, and may read whether a password is set,
who else is in the group, and the instance's own accent and corner shape. It
cannot read the settings or the accounts, change the password, mint an API
token, or ask for the phrase back. A route added later is outside the list
until somebody puts it in.

A relay peer is never written to disk. It exists for as long as the relay sees
it, and one remembered across a restart would be a peer that cannot be reached
and cannot be explained.

## The Android app

- **Find on this network** sweeps the phone's own /24 over HTTP and fills in
  the address of anything that answers as a KnightLoader. React Native has no
  UDP socket, so the app cannot join the multicast group the servers use; asking
  every address on the subnet for `/api/health` gets to the same place with the
  one thing the app already has.

  The addresses are probed by a pool of workers, not all at once. Android routes
  every `fetch` through OkHttp, which allows 64 concurrent requests and queues
  the rest - so firing all 253 off together meant the queued ones hit their own
  timeout while still waiting for a slot, and were abandoned without ever being
  sent. A server anywhere past the first batch, which is to say anywhere in a
  typical DHCP pool, was never contacted and the screen said it found nothing.

  It also does nothing at all when the phone has no usable address of its own.
  That is worth stating because it is easy to get wrong: on Android the IP comes
  from the Wi-Fi interface specifically, and reads `0.0.0.0` when Wi-Fi is off -
  which passes a naive check and would sweep `0.0.0.1` through `0.0.0.254` over
  a metered mobile connection.
- **Scan the QR** from the Access tab fills in the address. There is one kind
  of QR here now; it used to have to tell an address QR apart from a pairing
  one before it knew what it had scanned.
- **A token** is the one thing still typed by hand on this path, and only when
  that instance has a password.
- **The phrase** is the other way in, and the better one: twelve words, and
  every instance in the group appears at once - no address, no token, nothing
  to look up. The phone derives the same key its siblings do and dials the
  same relay, which is what authenticates it, so a password on an instance
  costs nothing extra here.

## The browser extension

A fresh install suggests `http://localhost:8749` - the ordinary case, one click
to accept, and never a silent default pointed at an address nobody looked at.

**Sync known instances** asks every instance already configured what peers *it*
knows about, and folds in what is new. Two things it now does that it did not:

- A peer with no address of its own - a desktop build, or one reachable only
  through a relay - is kept rather than dropped, and reached through the
  instance that told the extension about it. That instance forwards on its
  behalf, so the extension needs no relay client, no second copy of the relay
  key, and no persistent socket in a service worker the browser is free to kill.
- Every outcome says what actually happened. Not signed in, no answer in time,
  unreachable, an error, nothing configured yet and genuinely nothing new used
  to share one sentence: "No new instances found." Five problems, five different
  fixes, and a message that pointed at none of them.

Host permissions are requested for the exact origins already configured, from
the button's own click, and never wider. Opening the options page can prompt for
nothing - the automatic check only ever touches origins a previous click already
granted.
