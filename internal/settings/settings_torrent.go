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
// touches, and never rebuilds it) - a per-task override of either is not a
// KnightLoader design choice being deferred, it is a shape gopeed's public
// surface does not offer at all. See DHTEnabled's own doc comment for what
// was verified about that and what it means for the private-torrent promise
// below.
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
	// why it is not yet wired as of this wave.
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
	// there is internal/engine's job, not this package's, and is not done as
	// of this wave (see the report this wave's build left behind for
	// whoever picks that up). It is also subject to the same once-only
	// construction DHTEnabled describes just below: only the very first
	// torrent task this process ever starts can make a changed port stick,
	// because gopeed's bt client is a lazy singleton, built once and never
	// rebuilt for the rest of the process's life.
	Port int `json:"port"`

	// DHTEnabled and PEXEnabled are this instance's DEFAULT participation in
	// the swarm's own peer discovery - Distributed Hash Table lookups and
	// Peer Exchange - overridden to false for any one torrent whose own
	// metadata marks it private (BEP 27's info.private), with no user
	// toggle able to set it back to true for that torrent. See
	// EffectiveDHT/EffectivePEX below for that per-torrent decision.
	//
	// READ THIS BEFORE WIRING EITHER FIELD TO THE ENGINE, OR BEFORE TELLING
	// A USER THIS SETTING DOES SOMETHING. Verified by reading the vendored
	// dependency tree end to end - github.com/GopeedLab/gopeed v1.9.3 and
	// its pinned github.com/anacrolix/torrent v1.60.1-0.20251217073903 - not
	// assumed, the same discipline internal/resolver/torrent's own package
	// doc comment used for the fact this whole feature rests on:
	//
	//   - Neither field has anywhere to go. gopeed's own per-protocol config
	//     (internal/protocol/bt.config) has five fields - ListenPort,
	//     Trackers, SeedKeep, SeedRatio, SeedTime - and none of them is DHT
	//     or PEX. A whole-module grep of the vendored gopeed source for
	//     "NoDHT" or "DisablePEX" (the two anacrolix/torrent ClientConfig
	//     fields that actually gate this) returns ZERO matches, in either
	//     gopeed's public packages or its internal ones. gopeed's own
	//     bt.Fetcher.initClient builds the shared torrent.Client from
	//     torrent.NewDefaultClientConfig() and overrides exactly six fields
	//     (Seed, Bep20, ExtendedHandshakeClientVersion, ListenPort,
	//     HTTPProxy, TrackerDialContext) - never NoDHT, never DisablePEX.
	//     Both therefore sit at anacrolix's own defaults (both false: DHT
	//     on, PEX on) for the life of the process, for every torrent, and
	//     nothing KnightLoader passes through gopeed's public API can
	//     change that today.
	//   - It would not be per-torrent even if it were wired. NoDHT and
	//     DisablePEX are torrent.ClientConfig fields, consumed once by
	//     torrent.NewClient at the moment gopeed's bt client is first built
	//     - a package-level singleton (internal/protocol/bt's own `client`
	//     var) whose initClient returns immediately ("if client != nil {
	//     return }") for every torrent after the first, for the rest of the
	//     process. Confirmed by reading client.go's AddTorrentOpt: it starts
	//     a t.dhtAnnouncer for every torrent added, unconditionally, gated
	//     only on the CLIENT-level PeriodicallyAnnounceTorrentsToDht; and
	//     torrent.go's own PEX gates, at every call site, read
	//     t.cl.config.DisablePEX - the client's copy, not the torrent's.
	//     There is no anacrolix/torrent API that disables DHT or PEX for one
	//     Torrent while leaving a shared Client's DHT server and PEX support
	//     running for its others.
	//   - gopeed's OWN private-torrent handling is narrower than
	//     docs/torrent-support.md guessed it might be. Reading info.Private
	//     is real (internal/protocol/bt/fetcher.go's addTorrent does it, for
	//     an uploaded .torrent - a magnet's own branch never attempts it,
	//     since a magnet carries no info dict to read one out of before the
	//     swarm answers) - but the ONLY thing gopeed does with the result is
	//     skip adding EXTRA trackers to a private torrent's announce list.
	//     It never touches DHT or PEX for that torrent, confirmed by reading
	//     the whole of addTorrent. docs/torrent-support.md's guess ("gopeed
	//     almost certainly reads and honours this flag itself") is confirmed
	//     half right: it reads it, it does not honour it for DHT/PEX.
	//
	// DHTEnabled, PEXEnabled and EffectiveDHT/EffectivePEX below are
	// therefore a declared POLICY - what this instance intends, and what a
	// specific torrent's own privacy demands of it - with NO ENFORCEMENT
	// POINT in internal/engine as of this wave (engine.go and torrent.go are
	// 11.5A's files; this settings block's own file list did not include
	// them, and this finding is reported rather than acted on for exactly
	// that reason). Wiring one is real future work, not a formality: short
	// of patching gopeed, it likely means either accepting that DHT/PEX can
	// only be decided process-wide, once, before the first torrent of the
	// process's life (weaker than what decision 5 of the grilling actually
	// asked for) or reaching gopeed's client through something other than
	// its current public API. Whoever wires it must not ship a UI that
	// implies the per-torrent promise already holds - that is the identical
	// "looked correct on paper" shape Wave 10's own review already had to
	// walk back once, over a check that was a tautology by construction
	// rather than by anyone's intent.
	DHTEnabled bool `json:"dhtEnabled"`
	// PEXEnabled - see DHTEnabled immediately above; everything there
	// applies here identically, PEX and DHT being gated by sibling fields on
	// the same torrent.ClientConfig, in the same currently-unreachable way.
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
// CALLING THIS DOES NOT YET MAKE IT TRUE OF A RUNNING DOWNLOAD. See
// DHTEnabled's own doc comment for the verified, load-bearing reason:
// nothing in internal/engine reads either return value as of this wave,
// because gopeed's public API gives it nothing to set them with. This pair
// is the policy, correct and tested on its own terms (see
// settings_torrent_test.go), waiting on an enforcement point that does not
// exist yet.
func (t Torrent) EffectiveDHT(private bool) bool {
	return t.DHTEnabled && !private
}

// EffectivePEX - see EffectiveDHT immediately above.
func (t Torrent) EffectivePEX(private bool) bool {
	return t.PEXEnabled && !private
}
