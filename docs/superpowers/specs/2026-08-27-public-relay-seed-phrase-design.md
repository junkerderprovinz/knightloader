# Public relay with a seed phrase — design

Follow-up to `2026-08-25-self-hosted-relay-design.md`, which built the relay
but explicitly deferred one question:

> Whether to *additionally* offer an official, jdp-operated default relay for
> users who don't want to self-host one is an explicit, separate decision,
> deferred past this spec.

This spec answers that question: **yes**, and specifies how it is reached
without an account, without a login, and without ever leaving KnightLoader's
own interface.

## Why

The requirement chain that led here, in jdp's own words across several
rounds:

- "Das ist alles viel zu kompliziert für User" — the address+key pair had to
  go.
- "man soll auf dem handy nicht auch noch tailscale installieren müssen" —
  nothing may be required on a phone, a browser, or the extension.
- "Können wir den anmeldeprozess nicht vollständig in KL integrieren? …
  damit wir keine Fremdseite mehr brauchen?" — no third-party site, no
  third-party account.
- "eine zeichenfolge oder eine Seed phrase … die man dann in allen anderen
  Instanzen einfügen kann" — one thing to copy, nothing to configure.

Two facts settled the shape:

1. **Someone must be able to accept an inbound connection.** Two machines
   that can both only dial out cannot meet. UPnP self-forwarding was
   evaluated and rejected: it fails silently behind carrier-grade NAT and on
   the many routers that ship with UPnP disabled, which makes it unusable as
   *the* answer (jdp: "Ist ja quatsch wenn die automatische Portfreigabe
   nicht zuverlässig funktioniert").
2. **JDownloader is not login-free either.** My.JDownloader requires
   registering at `my.jdownloader.org` and entering those credentials in
   every instance. Matching "the JD feeling" therefore does not require
   beating JD's friction, only matching it — and a seed phrase with no
   registration step is strictly less friction than JD's account.

## Decision

KnightLoader ships with a **default relay address compiled into the binary**,
operated by jdp on a rented VPS. Instances join a group by presenting a
**seed phrase**: 12 words that encode one 128-bit secret and nothing else.

Because the address is a constant, the phrase carries only the secret. That
is what removes both the second input field and the login: there is no
account service to authenticate against, only a key to present — the trust
model the relay was already built with ("possession of the key is the entire
authorization model").

**Additive, and reversible.** The self-hosted relay from the previous spec
stays fully supported: the same phrase mechanism works against a
user-supplied relay address. This is not politeness — it is the exit path.
If the operated relay ever stops, users are not stranded, and jdp is not
locked into operating it forever.

## Non-goals

- Any account, email address, password, or registration on the relay.
- Storing anything about a user on the relay. It stays in-memory,
  database-free, exactly as built.
- Routing download traffic. The relay carries control messages only; no file
  byte ever crosses it (unchanged from the previous spec, and the reason a
  20 TB traffic allowance is not a constraint).
- Multiple groups per instance. One phrase = one person's own instances.
- Replacing Tailscale/Funnel or the domain+reverse-proxy path. Both stay.

## Architecture

### The relay address is a constant

```go
// internal/relay: the address every instance dials unless the user
// overrides it. A constant, not a setting, because a value nobody has to
// type is the entire reason the seed phrase can carry only a secret.
const DefaultRelayURL = "wss://relay.<domain>/relay/connect"
```

Overridable per instance for the self-hosted case (the existing relay-URL
field stays, moved under the "advanced" disclosure). Empty override = use
the constant.

### The seed phrase

**Format:** 12 words from the BIP39 English wordlist, encoding 128 bits of
entropy plus BIP39's own 4-bit checksum.

Why this wordlist specifically — it is not about cryptocurrency, it is about
transcription: 2048 words chosen so that no two share their first four
letters, no confusable pairs, no accents. That property is what makes a
phrase safe to read aloud over the phone and safe to type on a mobile
keyboard. Reusing it costs nothing and inventing a worse one costs
correctness.

**Why 128 bits, not the 256 the previous spec specified:** 256 bits is 24
words. At 2^128, guessing is not a threat model any rate limit needs to
help with, and 12 words is the difference between a phrase somebody will
actually type on a phone and one they will not. The checksum catches typos
before a failed connection attempt does.

**Never call it a wallet seed in the UI.** It is a *Verbindungsphrase* /
connection phrase. Same wordlist, unrelated purpose; the association would
scare people for no reason.

### Joining a group

```
Instance A (first)                      Instance B (later)
──────────────────                      ──────────────────
click "Fernzugriff aktivieren"
  │
  ├─ generate 128 random bits
  ├─ encode as 12 words  ──────────────► paste phrase (or scan QR)
  ├─ store encrypted                          │
  └─ dial DefaultRelayURL, present key        ├─ decode to 128 bits
                                              ├─ store encrypted
                                              └─ dial DefaultRelayURL, same key
                                                          │
                              relay groups both by key ───┘
```

No round trip to register, no confirmation step, no state on the relay
before the first connection. The relay learns a key exists at the moment
someone connects with it.

The QR code path reuses the existing pairing-code QR component — a phone or
a second machine scans instead of typing.

### Re-displaying the phrase, and the password gate

The phrase must be retrievable after setup, or adding a third instance later
means tearing down the group. But it is a **group credential**: whoever reads
it reaches every instance in the group, not just the one displaying it. An
unprotected instance leaking it therefore escalates beyond itself.

Two rules follow:

1. **Remote access can only be activated on an instance that has a password
   set.** Making an instance reachable from anywhere with no password is
   already the condition the exposure warning shouts about; here it also
   risks the *other* instances, so it becomes a hard gate rather than a
   warning. The UI explains this at the point of refusal, with the password
   field one click away.
2. **Revealing the phrase requires re-entering that password**, not merely
   holding a session. The same pattern GitHub uses before showing a token
   again. A session that was opened hours ago on an unattended screen is not
   evidence that the person now looking at it is the owner.

Storage is unchanged from the previous spec: the encrypted account store
(`internal/accounts`, AES-256-GCM, per-install `.keyring`), the same place
the debrid keys live — never plaintext in `settings.json`.

### Hardening for public exposure

The relay was built for "I host it for my own instances." Operated publicly,
three things change:

| Concern | Today | Change |
|---|---|---|
| Key length floor | `minKeyLength = 16` characters | 128 bits, enforced as decoded entropy, not string length |
| Guessing | nothing | per-IP rate limit on failed handshakes, exponential backoff |
| Group size | `maxClientsPerKey = 64` | unchanged, already sane |
| Total load | `maxTotalClients` | unchanged, raise only with measurement |

The rate limit is the only genuinely new mechanism. At 2^128 it is not what
makes guessing infeasible — it is what stops a bad actor from turning the
relay into their own load generator while failing.

### What the relay still never learns

Unchanged, and worth restating because operating it publicly invites the
question: no account list, no registration, no database, in-memory only. It
sees connection metadata (which keys are live, how many connections) and the
control messages it routes. It never sees a download, a file, a URL's
content, or a credential.

## Failure and exit

**Relay down.** LAN discovery and direct/domain connections are unaffected —
only relay-routed peers go quiet. The existing "unreachable" state already
covers this; it must not read as "your setup is broken."

**Relay retired.** If jdp ever stops operating it, the same phrase works
against a self-hosted relay by filling in the address override. The exit path
is the feature, not an afterthought; it is also what keeps this from being a
promise that cannot be withdrawn.

## Operating it

What jdp actually runs:

| | |
|---|---|
| Host | Hetzner Cloud CX22 — 2 vCPU, 4 GB RAM, 40 GB, 20 TB traffic, IPv4 included |
| Cost | 3.79 €/month + domain |
| Location | Nuremberg or Falkenstein |
| OS | Debian 12 |
| Exposed | 443 only, via Hetzner Cloud Firewall (free) |
| TLS | Let's Encrypt |
| State | none — no database, no volume, no backup to keep |

Hetzner over the alternatives for one reason beyond price: operating this
means processing other people's connection metadata, and a German company
with a German datacentre is the simplest position to be in for that.

Deployment is one binary (`cmd/knightloader-relay`) plus a systemd unit. A
restart costs the current connection list, which repopulates in seconds.

## Testing

- Seed phrase round-trips: generate → words → decode → identical 128 bits,
  across many iterations.
- Checksum rejects a single-word typo, and the error names *which* word.
- Two instances with the same phrase see each other through a real relay
  process; two with different phrases do not.
- Activation is refused with no password set, and the refusal explains why.
- Reveal requires the password; a valid session alone is not enough.
- Rate limit: repeated bad handshakes from one address back off, and a
  legitimate handshake from another address is unaffected.
- Address override reaches a self-hosted relay instead of the constant.

## Rollout

1. Seed phrase encode/decode + tests (no UI, no network).
2. Relay hardening: entropy floor, rate limit + tests.
3. Constant + override plumbing.
4. UI: activate, display, reveal-behind-password, QR, paste.
5. jdp provisions the VPS, DNS, TLS; deploy; verify against both preview
   containers.
6. `docs/connecting.md` rewritten around the phrase as the primary path.

## Open questions for jdp

1. **Domain** — `relay.braethoria.com`, or a KnightLoader-specific domain
   worth registering before the repo goes public?
2. **Hard password gate** — agreed, or should it be a loud warning that can
   be clicked past? (Recommendation: hard gate. The phrase reaches every
   instance in the group, not only this one.)
3. **Default for existing installs** — instances that already use Tailscale
   or a self-hosted relay: leave them exactly as they are, or offer a
   one-time "switch to the simpler way" prompt? (Recommendation: leave them;
   a working setup should never be interrupted by a new option.)
