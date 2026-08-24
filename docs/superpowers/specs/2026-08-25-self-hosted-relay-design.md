# Self-hosted relay for instance pairing/federation — design

## Why

The pairing feature built the previous round (`internal/api/routes_pairing.go`,
`internal/api/routes_remote.go`) only works when at least one of the two
instances is independently network-reachable — same LAN, or a domain behind a
reverse proxy / VPN. Two consequences jdp flagged as too complicated:

- The desktop build has **no real network listener at all** (`desktop/main.go`
  wires Wails' `AssetServer` directly to `api.Handler`, never calls
  `http.ListenAndServe`) — it cannot be paired today, full stop.
- Two instances that are each behind NAT, on different networks, with nothing
  reachable between them (e.g. two desktop installs, one at home and one at
  work) cannot pair at all, regardless of how the pairing UI is simplified —
  this is a networking fact, not a UX problem.

jdp wants something close to JDownloader's My.JDownloader experience — "just
log in, devices see each other" — on every platform, across networks. That
requires a third point both sides can reach. The question this spec answers
is not "relay yes/no" but "whose infrastructure plays that role," because
KnightLoader's own product principle (stated to the user directly in the
Remote access card's own copy) is that it will never run an always-on hosted
service with the cost and liability that implies for every future user of a
project that is still private and pre-release.

## Decision

Build the relay as **its own, separate, self-hostable, open-source
component** — not a service KnightLoader operates on anyone's behalf.

- jdp runs his own instance of it, on his own Bottich, for his own devices.
  No new external dependency, no new cost, no new liability beyond
  infrastructure he already runs.
- Once the repo goes public, anyone who wants relay-based pairing runs their
  own instance too, the same way they already self-host KnightLoader itself.
  Nobody's data crosses jdp's infrastructure by default.
- Whether to *additionally* offer an official, jdp-operated default relay for
  users who don't want to self-host one is an explicit, separate decision,
  deferred past this spec.

This is additive. The existing LAN / domain+reverse-proxy / manual
pairing-code flow (Round 9) is untouched and keeps working exactly as built —
the relay is one more way to reach another instance, not a replacement.

## Non-goals (this round)

- An officially jdp-operated default relay for the general public.
- Multiple relay keys / multiple "families" per instance (v1 assumes one
  relay key = one person's own instances).
- Changes to the mobile companion app (being built in a parallel session) —
  its own relay integration is a follow-up once this backend exists, noted
  as an integration point below, not designed here.
- A full cryptographic audit beyond "the transport is TLS and the key is
  stored the way other credentials in this app already are."

## Architecture

### New component: `cmd/knightloader-relay`

A small, separate Go binary in the *same repo* (shares the wire-protocol
types with the main app; a separate repo can happen later if it ever needs
its own release cadence — no reason to pay that cost now). Its only job is
message routing between instances that present the same relay key. It never
touches a download, never sees a file byte.

### Identity: the relay key

A single random secret groups an owner's instances — no email, no password,
no user database on the relay.

- Generated once (e.g. a 256-bit token, base58-encoded for readability),
  either by the relay on first use or client-side by the first instance.
- Every other instance the owner wants relay-connected gets the same key,
  entered once (Settings, same place `InstanceName`/`KnownDomains` from
  Round 9 already live) — comparable effort to a JD login, no account
  service behind it.
- Possession of the key is the only authorization check, the same trust
  model WireGuard preshared keys and Syncthing device IDs already use.
- **Stored like a credential, not like a setting**: through the existing
  encrypted account store (`internal/accounts`, AES-256-GCM, per-install
  `.keyring`) the same way TorBox/AllDebrid/RealDebrid keys already are —
  not plaintext in `settings.json`. `InstanceName`/`KnownDomains` are public
  identity, the relay key is a secret; they don't belong in the same
  sanitize path.

### Transport

Each instance opens one outbound, persistent WebSocket (WSS) connection to
the relay's public address, authenticating with the relay key in the
handshake. This is a pure reverse-connection pattern (the same shape ngrok
and Tailscale's own coordination servers use) — it works regardless of which
side is behind NAT, because neither side needs to accept an inbound
connection from the relay, only from — nobody. Both dial out.

The relay keeps an in-memory registry: `key -> [live connections]`. No
database for v1 — state is rebuilt as instances reconnect. A relay restart
loses nothing but the current connection list, which repopulates within
seconds.

### Desktop

Desktop needs **no new inbound capability**. Relay mode is a pure outbound
WebSocket client added alongside the existing Wails `AssetServer` wiring —
it does not touch, replace, or weaken anything about how desktop currently
serves its own local UI. This is deliberate: it closes desktop's pairing gap
without opening a new listening port to secure.

### Message shapes (over the relay WebSocket)

Reuses existing types wherever possible rather than inventing a parallel
protocol:

- `announce` (instance → relay → broadcast to every other connection on the
  same key): instance ID, name (`InstanceName` if set, else hostname — same
  precedence `pairingSelf()` already uses), deployment kind
  (container/desktop). This is what makes "log in once, see everything"
  real: two instances sharing a key see each other on the Instances page the
  moment both are connected, with **no manual pairing-code exchange** for
  relay-connected instances.
- `presence`: online/offline as connections open/close, so the Instances
  page shows live, accurate status instead of the existing 2-second poll's
  best guess.
- `proxy-request` / `proxy-response`: wraps calls to the instance's own
  existing REST API (task list, add link, pause/resume, …) as
  request/response frames. `internal/federation`'s `Proxy(ctx, name, method,
  path, body)` already exists for exactly this shape against a direct HTTP
  address — the relay is a **second transport** for the same call, not a new
  API surface. The app/UI layer does not need to know which transport is in
  play for a given peer.

File bytes never travel this channel. A remote "start this download"
executes on the target instance, which writes to its own disk — identical to
every other resolver path already in this app.

### Where this plugs into existing code

- `internal/federation`: add a relay-backed implementation of the transport
  `Proxy` already abstracts, alongside the existing direct-HTTP one.
- Instances page: a relay-connected peer appears the same way a
  manually-paired one does, just backed by `announce`/`presence` instead of
  a stored `instances.json` URL.
- Settings: new `internal/settings/settings_relay.go` (relay URL is public,
  follows the `settings_identity.go` pattern; the key itself lives in
  `internal/accounts`, see above).
- New Settings UI: a "Vermittler" card, most naturally on the Access page
  next to the existing pairing card built in Round 9 — enter relay URL +
  key, see which siblings are currently relay-visible.

## Error handling

- **Relay unreachable**: the instance simply has no relay-connected peers
  until it reconnects. Everything else — its own downloads, LAN pairing,
  domain-based pairing — keeps working, unaffected. A relay outage must
  never touch an instance's own local operation; that resilience is the
  entire point of making this optional and self-hosted rather than a
  dependency the whole app leans on.
- **Key rotation/revocation**: v1 is manual — generate a new key, re-enter
  it on every instance that should keep relay access. Matches the effort of
  the existing pairing-code flow; a self-service revocation UI is a
  reasonable follow-up, not required for v1.
- **TLS**: the relay must run behind TLS (WSS) for anything beyond pure LAN
  testing. It can sit behind the same reverse proxy already fronting other
  self-hosted services (Nginx Proxy Manager, `192.168.20.11` on the
  Bottich) — no new TLS-terminating infrastructure needed.
- **Multiple instances reusing one relay**: out of scope for v1 — the relay
  is single-owner-oriented (one key, one person's own instances), not
  multi-tenant.

## Testing

- Unit tests for the relay's connection registry (key → connections,
  announce/presence broadcast) — pure in-memory, no real network.
- Integration test mirroring the existing pairing tests' shape
  (`internal/api/pairing_test.go`'s `httptest`-style two-real-instances
  pattern): spin up a relay plus two or three fake instances in-process,
  verify announce/proxy-request/response round-trip correctly and that a
  disconnect updates presence.
- Live verification: deploy an actual `knightloader-relay` container to the
  Bottich (own br0.20 IP, from the start this time — see the deploy gotcha
  in this project's own `Notizen.md`), connect the primary and secondary
  preview instances to it, confirm they see each other with zero manual
  pairing once both are configured with the same key.

## Rollout

Purely additive — ships alongside the existing pairing flow, does not
change it. Order of work for a future implementation plan:

1. Relay binary: connection registry, WebSocket handshake/auth by key,
   announce/presence broadcast. Unit + integration tests.
2. `internal/federation` relay transport + wiring into the Instances page.
3. Relay key storage via `internal/accounts`; Settings UI (relay URL + key
   entry, connected-siblings list).
4. Desktop: add the outbound relay client alongside the existing Wails
   `AssetServer` wiring.
5. Deploy `cmd/knightloader-relay` to the Bottich (br0.20 IP, template
   labels done correctly from the start, behind Nginx Proxy Manager for
   TLS), live-verify against the two existing preview instances.
6. Mobile app integration — coordinate with the parallel session building
   the companion app; out of scope to implement here, but the relay's wire
   protocol should be reviewed against what that app can realistically
   speak (WebSocket client) before finalizing message shapes.
