package settings

// settings_torrent.go: what a BitTorrent download does once it starts -
// how long it keeps giving bytes back after it finishes, how hard it
// announces itself to the swarm, and the two knobs (DHT, PEX) a private
// tracker's rules require this app to get right without the user having to
// remember to ask per torrent. See internal/resolver/torrent for the intake
// side (recognising a magnet or an uploaded .torrent) and internal/engine
// for what actually starts a torrent task - this file is the configuration
// those two read, not either of them.

// Torrent is the seed/port/DHT/PEX policy for every BitTorrent download this
// instance starts - one block per instance, not one per task. That matches
// gopeed's own bt fetcher: DHT/PEX participation and the listen port are
// properties of the ONE embedded torrent client every task shares
// (github.com/GopeedLab/gopeed@v1.9.3's internal/protocol/bt.Fetcher.
// initClient builds it once, lazily, on the first torrent this process ever
// touches, and never rebuilds it) - a per-task override of either through
// GOPEED'S OWN SURFACE is not a KnightLoader design choice being deferred,
// it is a shape that surface does not offer at all. See DHTEnabled's own
// doc comment for what was verified about that, for the one per-torrent
// override that reaches around gopeed's surface entirely (a private
// torrent's own DHT/PEX refusal), and for what the rest still means for the
// ordinary-torrent half of the private-torrent promise below.
type Torrent struct {
	// SeedRatioTarget is uploaded-over-downloaded; 0 means no ratio target
	// (only SeedDurationSeconds, if that is also non-zero, applies). See
	// SeedDurationSeconds' own doc comment for the combination rule and where
	// it actually comes from.
	//
	// 1.0 - seed until every byte received has been given back once - is
	// gopeed's own built-in default
	// (internal/protocol/bt.FetcherManager.DefaultConfig), mirrored here
	// rather than inventing a KnightLoader-specific number nobody has reason
	// to prefer over the one the engine underneath already ships with.
	SeedRatioTarget float64 `json:"seedRatioTarget"`
	// SeedDurationSeconds is how long to keep seeding after the download
	// itself finishes; 0 means no duration target (only SeedRatioTarget, if
	// that is also non-zero, applies). Both zero seeds forever - see below.
	//
	// 7200 (two hours) is gopeed's own default (SeedTime: 120 * 60 in the
	// same DefaultConfig SeedRatioTarget's default came from), mirrored for
	// the same reason.
	//
	// WHICHEVER OF THE TWO IS REACHED FIRST STOPS SEEDING. docs/torrent-
	// support.md flags this as a rule KnightLoader decided rather than one
	// the grilling settled explicitly - it is the only combination that
	// makes sense of having both fields at once (a second target that only
	// matters once the first has somehow not been enough would need
	// different names), so it is implemented on that basis, said plainly
	// here rather than presented as a settled decision.
	//
	// It is also, independently, already what gopeed itself does once these
	// two numbers reach it - confirmed by reading
	// internal/protocol/bt/fetcher.go's doUpload: it checks
	// config.SeedRatio and config.SeedTime in the same loop, each closing
	// the fetcher on its own the instant it is met, with no ordering between
	// the two. KnightLoader's job is to surface these two numbers to gopeed,
	// not to re-implement the stop condition against them - see Port's doc
	// comment for how that surfacing happens (ProtocolConfig["bt"]) and for
	// how, unlike Port, it reaches every torrent added from here on rather
	// than only the first. Engine.SetTorrentConfig
	// (internal/engine/engine.go) is what carries it, called from
	// internal/app's afterSettingsChange on every save and once at boot
	// (internal/app/app.go) - each new torrent's own Fetcher.Setup reads
	// ProtocolConfig["bt"] fresh (internal/protocol/bt/fetcher.go), so a
	// value saved here is live for the very next torrent added, not only
	// after a restart.
	//
	// Both zero seeds forever without needing gopeed's separate SeedKeep
	// switch: its loop only stops on a ">0" target being met, so with
	// neither one set it never stops on its own. This settings block does
	// not expose SeedKeep for that reason - a zero/zero pair already means
	// the same thing, and a THIRD field that agrees with a state two others
	// already reach is one more way for a save to disagree with itself.
	SeedDurationSeconds int `json:"seedDurationSeconds"`

	// UploadLimitKiBs caps upload bandwidth in KiB/s; 0 is unlimited, the
	// same convention Settings.SpeedLimit already uses for downloads.
	//
	// UNWIRED, as of this wave: gopeed's own per-protocol config
	// (internal/protocol/bt.config, five fields, read by reading the
	// vendored v1.9.3 source directly since the package is internal/ and
	// unreachable from here) has no upload-rate field at all, and its
	// client is built with UploadRateLimiter left at anacrolix/torrent's own
	// "unlimited" default. This number has nowhere to go through gopeed's
	// public surface yet. It is stored anyway so the settings page and the
	// API shape can exist ahead of an engine that can honour it - the same
	// reasoning core.TorrentFile's own doc comment gives for a field
	// persisted before everything that will read it exists.
	UploadLimitKiBs int `json:"uploadLimitKiBs"`

	// Port is the TCP port this instance's torrent client listens on; 0 lets
	// gopeed (in turn anacrolix/torrent) pick - which is also gopeed's own
	// shipped default. Verified by reading both:
	// internal/protocol/bt.FetcherManager.DefaultConfig sets ListenPort: 0,
	// which OVERRIDES anacrolix/torrent's own non-zero 42069 default,
	// because initClient unconditionally assigns
	// cfg.ListenPort = f.config.ListenPort - gopeed's zero always wins.
	//
	// Unlike UploadLimitKiBs this one IS represented on gopeed's own config
	// surface (internal/protocol/bt.config.ListenPort, reached through
	// DownloaderStoreConfig.ProtocolConfig["bt"]) - carrying this value
	// there is internal/engine's job (Engine.SetTorrentConfig,
	// internal/engine/engine.go), called from internal/app's
	// afterSettingsChange on every save and once at boot, the same call as
	// SeedRatioTarget/SeedDurationSeconds above. UNLIKE THOSE TWO, a value
	// saved here only takes if no torrent has started yet this process: it
	// is subject to the same once-only construction DHTEnabled describes
	// just below - only the very first torrent task this process ever
	// starts can make a changed port stick, because gopeed's bt client is a
	// lazy singleton (internal/protocol/bt.Fetcher.initClient, "if client !=
	// nil { return }"), built once and never rebuilt for the rest of the
	// process's life. A save after that point is still stored correctly and
	// still reaches gopeed's own config; it simply has no torrent left this
	// process will ever start that could read it before the client is
	// already built.
	Port int `json:"port"`

	// DHTEnabled and PEXEnabled are this instance's DEFAULT participation in
	// the swarm's own peer discovery - Distributed Hash Table lookups and
	// Peer Exchange - for an ORDINARY, non-private torrent. A torrent whose
	// own metadata marks it private (BEP 27's info.private) gets neither,
	// automatically, with no user toggle able to set either back to true for
	// that torrent - and unlike the ordinary-torrent default, that half of
	// decision 5 needed no wiring from this package or from internal/engine
	// to start being true. See EffectiveDHT/EffectivePEX below for that
	// per-torrent decision as this package states it, and the third bullet
	// below for why stating it was already enough for the private half.
	//
	// READ THIS BEFORE CHANGING EITHER FIELD'S DEFAULT, OR BEFORE TELLING A
	// USER THIS SETTING DOES SOMETHING FOR AN ORDINARY TORRENT. Verified by
	// reading the vendored dependency tree end to end - originally against
	// github.com/GopeedLab/gopeed v1.9.3 and its then-pinned
	// github.com/anacrolix/torrent v1.60.1-0.20251217073903, re-verified
	// after bumping the latter to v1.61.1-0.20260525011549 (go.mod's own
	// comment on that require line names the commit and the upstream PR) -
	// not assumed, the same discipline internal/resolver/torrent's own
	// package doc comment used for the fact this whole feature rests on:
	//
	//   - Neither field has anywhere to go FOR AN ORDINARY TORRENT, still.
	//     gopeed's own per-protocol config (internal/protocol/bt.config) has
	//     five fields - ListenPort, Trackers, SeedKeep, SeedRatio, SeedTime -
	//     and none of them is DHT or PEX. A whole-module grep of the
	//     vendored gopeed source for "NoDHT" or "DisablePEX" (the two
	//     anacrolix/torrent ClientConfig fields that gate this at the
	//     CLIENT level, for every torrent it will ever handle) returns ZERO
	//     matches, in either gopeed's public packages or its internal ones -
	//     unchanged by the anacrolix/torrent bump below, since gopeed's own
	//     bt.Fetcher.initClient still builds the shared torrent.Client from
	//     torrent.NewDefaultClientConfig() and still overrides exactly six
	//     fields (Seed, Bep20, ExtendedHandshakeClientVersion, ListenPort,
	//     HTTPProxy, TrackerDialContext), never NoDHT, never DisablePEX.
	//     Both therefore still sit at anacrolix's own defaults (both false:
	//     DHT on, PEX on) for the life of the process, for every torrent,
	//     and nothing KnightLoader passes through gopeed's public API can
	//     change that today. A user who sets either field to false wanting
	//     it to hold for their own ordinary public torrents is saving a
	//     preference gopeed's client keeps ignoring - this half of decision
	//     5 is not what got fixed below.
	//   - It would still not be per-torrent through THAT door even if it
	//     were wired. NoDHT and DisablePEX are torrent.ClientConfig fields,
	//     consumed once by torrent.NewClient at the moment gopeed's bt
	//     client is first built - a package-level singleton
	//     (internal/protocol/bt's own `client` var) whose initClient returns
	//     immediately ("if client != nil { return }") for every torrent
	//     after the first, for the rest of the process. A client-level
	//     NoDHT/DisablePEX, even if gopeed exposed one, would only ever be a
	//     process-wide, decided-once knob.
	//   - THE PRIVATE-TORRENT HALF OF DECISION 5 IS DELIVERED, through a
	//     third door neither bullet above accounts for. gopeed's own
	//     handling is unchanged and still narrower than
	//     docs/torrent-support.md originally guessed: reading info.Private
	//     is real (internal/protocol/bt/fetcher.go's addTorrent does it, for
	//     an uploaded .torrent - a magnet's own branch never attempts it,
	//     since a magnet carries no info dict to read one out of before the
	//     swarm answers), but the only thing gopeed itself does with the
	//     result is skip adding EXTRA trackers to a private torrent's
	//     announce list - confirmed again by re-reading the whole of
	//     addTorrent after the bump. gopeed never touches DHT or PEX for
	//     that torrent, and no longer needs to: anacrolix/torrent
	//     v1.61.1-0.20260525011549 (upstream PR #1053, "Implement BEP 27
	//     (private torrents)", merged 2026-05-25) added a Torrent.isPrivate()
	//     check directly inside dhtAnnouncer's own per-iteration announce
	//     loop (torrent.go), PEX's connection init (pexconn.go) and both
	//     directions of Local Peer Discovery (client.go) - five gates, all
	//     reading the TORRENT's own already-parsed info.Private, none of
	//     them reading the CLIENT's NoDHT/DisablePEX. That is the
	//     per-torrent override the first two bullets could never reach on a
	//     shared, once-built client - solved one layer lower than gopeed,
	//     inside the library gopeed is itself built on, with nothing left
	//     for gopeed or KnightLoader to wire. KnightLoader's own go.mod
	//     already named anacrolix/torrent as a DIRECT requirement, not
	//     merely transitive through gopeed, so Go's own
	//     minimum-version-selection let this be a version bump on our side
	//     alone. End-to-end wiring verified by reading the bumped source
	//     directly, not assumed from the PR description: spec.go's
	//     TorrentSpecFromMetaInfoErr carries info.Private through in
	//     InfoBytes untouched, and client.go's AddTorrentOpt parses those
	//     bytes into the Torrent's own info, under the client lock, before
	//     the DHT-announcer goroutines it just spawned can take that same
	//     lock to read it - no race window for an uploaded .torrent. A
	//     magnet has no info dict to read this from until the swarm
	//     answers, so its privacy is enforced from the moment metadata
	//     arrives, not before - upstream's own comment on the per-iteration
	//     re-check ("we re-check every loop because info may be loaded
	//     later, e.g. via magnet") names the same gap this settings block
	//     would otherwise have had to.
	//
	// DHTEnabled, PEXEnabled and EffectiveDHT/EffectivePEX below remain a
	// declared POLICY for the ORDINARY-torrent half of decision 5 only - the
	// private half above needs none of the three to already hold true.
	// Wiring the ordinary-torrent half is still real future work, not a
	// formality: short of a gopeed change (upstream patch, or a fork) that
	// exposes ClientConfig.NoDHT/DisablePEX on its public surface, or
	// KnightLoader reaching gopeed's client through something other than
	// its current public API, the best available today is accepting DHT/PEX
	// can only be decided process-wide, once, before the first torrent of
	// the process's life. Whoever wires it must not ship a UI that implies
	// the ordinary-torrent preference already holds - that is the identical
	// "looked correct on paper" shape Wave 10's own review already had to
	// walk back once, over a check that was a tautology by construction
	// rather than by anyone's intent.
	DHTEnabled bool `json:"dhtEnabled"`
	// PEXEnabled - see DHTEnabled immediately above; everything there
	// applies here identically, PEX and DHT being gated by sibling
	// ClientConfig fields for the ordinary-torrent case that remains
	// unwired, and by sibling isPrivate() gates for the private-torrent case
	// that no longer needs wiring at all.
	PEXEnabled bool `json:"pexEnabled"`
}

// defaultTorrent is Torrent's starting values for a fresh install. The
// numbers are gopeed's own (see SeedRatioTarget/SeedDurationSeconds/Port's
// doc comments for exactly where each is confirmed from the vendored
// source), not invented - inventing one ships a build with an opinion nobody
// actually decided. DHT and PEX default on, matching what they already are
// today regardless of this setting (see DHTEnabled's doc comment) and
// matching ordinary BitTorrent client behaviour for a public swarm.
func defaultTorrent() Torrent {
	return Torrent{
		SeedRatioTarget:     1.0,
		SeedDurationSeconds: 2 * 60 * 60,
		DHTEnabled:          true,
		PEXEnabled:          true,
	}
}

// sanitizeTorrent floors every number that has no honest negative meaning
// back to its off/unset zero, and keeps Port inside what a TCP port actually
// is. None of the three folds away a real choice someone made: a negative
// seed target, a negative KiB/s and a port outside 0-65535 are not settings
// anybody meant, they are a form that let a minus sign or a stray digit
// through.
func sanitizeTorrent(n Settings) Settings {
	t := &n.Torrent
	if t.SeedRatioTarget < 0 {
		t.SeedRatioTarget = 0
	}
	if t.SeedDurationSeconds < 0 {
		t.SeedDurationSeconds = 0
	}
	if t.UploadLimitKiBs < 0 {
		t.UploadLimitKiBs = 0
	}
	if t.Port < 0 || t.Port > 65535 {
		// Out of range collapses to "let the OS pick" rather than being
		// refused outright - the same direction Chunks folds an unusable
		// number in sanitizeNetwork, because nothing typed into a settings
		// spinner should cost somebody the rest of the page.
		t.Port = 0
	}
	return n
}

// EffectiveDHT and EffectivePEX are the per-torrent decision that decision 5
// of the grilling actually asks for: this instance's own default, unless the
// torrent itself is private, in which case always false - a private torrent
// gets no vote, from this setting or from anything a user does to it
// afterwards, because there is no argument here for one to override it
// with.
//
// private is BEP 27's info.private, read wherever a torrent's metadata is
// first parsed - internal/resolver/torrent.Metadata.Private for an uploaded
// .torrent, checked before a byte is written; for a magnet the swarm has to
// answer first, which happens inside internal/engine's own resolve, not at
// paste time. Taking a bare bool rather than that Metadata type keeps this
// package from depending on the resolver for one field of it, and keeps this
// function usable by a caller that only ever has the bool to begin with.
//
// CALLING THIS ONLY MATTERS FOR THE ORDINARY-TORRENT (private=false) CASE.
// See DHTEnabled's own doc comment for the full account: nothing in
// internal/engine reads either return value, for either branch, and for
// private=false that means gopeed's public API still gives this package
// nothing to set - that half is a policy, correct and tested on its own
// terms (see settings_torrent_test.go), waiting on an enforcement point
// that does not exist yet. The private=true branch is different: it already
// matches what anacrolix/torrent enforces on its own, inside the library,
// whether or not anything ever calls this function - see DHTEnabled's doc
// comment for how. This function stating "false" for a private torrent is
// therefore correct but not load-bearing; it would already be false in
// effect even if this function did not exist.
func (t Torrent) EffectiveDHT(private bool) bool {
	return t.DHTEnabled && !private
}

// EffectivePEX - see EffectiveDHT immediately above.
func (t Torrent) EffectivePEX(private bool) bool {
	return t.PEXEnabled && !private
}
