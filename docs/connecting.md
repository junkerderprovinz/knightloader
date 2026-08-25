# Connecting

Four things can talk to a KnightLoader: the web UI it serves itself, a desktop
build, the Android app, and the browser extension. Any of them can also talk to
a *second* KnightLoader. This is how each of those connections is made, and what
happens when one of them cannot be.

The short version: on one network, nothing needs configuring. Across networks,
one code connects two instances. When neither can reach the other, a relay you
host yourself carries the traffic.

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
follow, and the page says so and points at a pairing code. That is the only
path that hands credentials over. An instance with no password works
immediately.

Nothing in an announce is trusted. Anything on the network can send one, so the
fields are length-capped on arrival and the list is bounded - a device
announcing endless invented instances cannot grow it without limit. What a row
shows is a claim, which is exactly why the address is displayed next to the name
and why adding is a decision rather than a consequence.

Multicast blocked, or no network at all, means an empty list. Everything below
still works.

## Between two instances: a pairing code

Settings → Access issues a code, as text or as a QR. Pasting it into the other
instance's Instances tab connects the two.

One code, both directions:

- The redeemer registers the code's issuer.
- The issuer registers the redeemer back, in the same exchange.
- Each side mints an API token for the other and hands it over, so a
  password-protected instance is reachable the moment pairing finishes instead
  of answering 401 forever. Both tokens are named after the peer and can be
  revoked individually from the Access tab.
- Pairing again with the same peer replaces that peer's token rather than
  adding another, so the Access tab does not fill with identical rows. An
  attempt that FAILS changes nothing: the credential from the pairing that
  worked is left alone.

The name a peer is addressed by is not always the name it calls itself. That
name has to work as a URL path segment, so it is folded into something
addressable - "Bürglers Keller" becomes "Burglers Keller", and a hostname
longer than 32 characters is cut to fit. Before that folding existed, an
instance with an umlaut in its name could not be paired at all, and the far side
reported it as an invalid name the person redeeming the code had never typed and
could not see from there.

Reachability is reported per direction, because it fails per direction.

**Setting a password later breaks an existing pairing**, and it is meant to: an
instance revokes every credential it ever issued when its password is set or
changed, because a credential minted while there was none is a standing bypass
of the protection just put in place. The peer then refuses this instance, and
the Instances page says so rather than showing it as offline - a machine that is
answering and saying no is not a machine that is switched off, and only one of
those is fixed by pairing again. Which is the fix: issue a fresh code. A
pairing where only one side can dial the other is kept and labelled, not thrown
away: the half that works is worth having, and a peer that is merely asleep
should not cost you the pairing.

A **desktop build has no address**, ever. It opens no API listener at all - it
hands its handler to the window - so nothing can dial it. It can still find and
add other instances itself, and, once it is on a relay, it can issue and redeem
pairing codes like anything else: the code then carries its relay identifier
rather than a URL.

## When neither can reach the other: the relay

Both ends dial *out* to a relay and meet there, so neither needs a public
address, a port forward or a domain. Traffic is JSON frames over one WebSocket
each.

The relay is self-hosted. There is no official one, and the design note that
deferred that decision still stands. `docker compose` it anywhere both ends can
reach, put the same key in both, done.

What the relay operator can see is stated plainly rather than implied: a
proxied request carries the caller's bearer token, so whoever runs the relay can
read a reusable credential out of it. Run your own.

**Pairing works over the relay too.** A code from an instance with no address
carries its relay identifier instead of a URL, and redeeming it sends the
completion through the relay rather than over HTTP. Both ends arrive at the
same handler, so there is no second implementation of pairing to keep in step.

A code may carry both a URL and a relay identifier, and then the direct route
is tried first: it is one hop, it does not depend on a third machine being up,
and it does not put the exchange in front of a relay operator. The relay takes
over when the address does not carry - which happens more often than it sounds,
because an address in a code is what the issuing instance believes it is
reachable at, and a NAS announcing its LAN address is right for everyone on
that LAN and wrong for everyone else.

The fallback is only for a connection that never landed, never for an answer:
a peer that replied at all has already seen that code, and a pairing code is
single-use.

Two consequences worth knowing:

- **A desktop build can now issue a pairing code**, which it never could
  before. It still has no address - that has not changed - but the relay
  addresses it by identifier, and both ends dial out.
- **A peer's credential is filed under every key it can be addressed by** - its
  pairing name over a direct connection, its instance id over the relay -
  because which one ends up being used is not knowable while pairing. An
  address is what the far side believes it is reachable at, and a container
  behind NAT announces one that is right for its own network and wrong for
  everywhere else. Filing under a guess is why two NAT'd containers could pair
  successfully and then go on refusing each other: both offered an address, so
  both filed under a name, while both were addressed by id.

A relay peer is still never written to disk. It exists for as long as the relay
sees it, and one remembered across a restart would be a peer that cannot be
reached and cannot be explained. Only the credential is kept.

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
- **Scan the QR** from the Access tab fills in the address, and, for a pairing
  QR, the instance name too.
- **A token** is the one thing still typed by hand. A pairing code's token is a
  short-lived handshake secret, not a bearer token, so it cannot stand in for
  one.
- **Relay** is offered as a way out of a dead end rather than as an equal first
  choice, because it only helps when nothing direct can work and it costs a
  shared key.

## The browser extension

A fresh install suggests `http://localhost:8749` - the ordinary case, one click
to accept, and never a silent default pointed at an address nobody looked at.

**Sync paired instances** asks every instance already configured what peers *it*
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
