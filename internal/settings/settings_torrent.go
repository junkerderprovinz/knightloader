package settings

// What a BitTorrent download does once it starts: how long it keeps giving
// bytes back after it finishes, how hard it announces itself to the swarm, the
// DHT and PEX switches a private tracker's rules need this app to get right
// without anybody asking per torrent, which of its files it fetches and which
// trackers it may or may not use. See internal/resolver/torrent for the intake
// side and internal/engine for what starts a torrent task; this file is the
// configuration those two read.

import "strings"

// Torrent is the seed, port, DHT, PEX, file and tracker policy for every
// BitTorrent download this instance starts: one block per instance rather than
// one per task.
//
// That matches gopeed's bt fetcher, where DHT and PEX participation and the
// listen port are properties of the one embedded torrent client every task
// shares. bt.Fetcher.initClient builds it lazily on the first torrent the
// process touches and never rebuilds it, so a per-task override of either is a
// shape gopeed's surface does not offer.
type Torrent struct {
	// SeedRatioTarget is uploaded over downloaded; 0 means no ratio target, so
	// only SeedDurationSeconds applies if that is non-zero. 1.0 is gopeed's own
	// default (bt.FetcherManager.DefaultConfig), mirrored here rather than
	// picking a number nobody has reason to prefer over the engine's.
	SeedRatioTarget float64 `json:"seedRatioTarget"`
	// SeedDurationSeconds is how long to keep seeding after the download
	// finishes; 0 means no duration target. 7200 is gopeed's own default.
	//
	// Whichever of the two is reached first stops the seeding, which is also
	// what gopeed does with them: bt/fetcher.go's doUpload checks SeedRatio and
	// SeedTime in the same loop, each closing the fetcher on its own, with no
	// ordering between them. Both zero seeds forever, because that loop only
	// stops on a target above zero being met, which is why gopeed's separate
	// SeedKeep switch is not exposed here.
	//
	// Engine.SetTorrentConfig carries both numbers into ProtocolConfig["bt"],
	// called from app.afterSettingsChange on every save and once at boot. Each
	// new torrent's Fetcher.Setup reads that config fresh, so a value saved
	// here is live for the next torrent rather than after a restart.
	SeedDurationSeconds int `json:"seedDurationSeconds"`

	// UploadLimitKiBs caps upload bandwidth in KiB/s; 0 is unlimited, the
	// convention Settings.SpeedLimit uses for downloads.
	//
	// Nothing honours it yet. gopeed's per-protocol config (bt.config) has no
	// upload-rate field, and its client leaves UploadRateLimiter at
	// anacrolix/torrent's unlimited default, so the number has nowhere to go
	// through gopeed's public surface. It is stored so the settings page and
	// the API shape can exist ahead of an engine that can honour it.
	UploadLimitKiBs int `json:"uploadLimitKiBs"`

	// Port is the TCP port this instance's torrent client listens on; 0 lets
	// gopeed, and in turn anacrolix/torrent, pick. gopeed's own
	// DefaultConfig sets ListenPort to 0, which overrides anacrolix's 42069,
	// because initClient assigns cfg.ListenPort unconditionally.
	//
	// It reaches gopeed through the same ProtocolConfig["bt"] the two seed
	// fields use, but unlike those it only takes if no torrent has started yet
	// in this process: gopeed's bt client is a lazy singleton built once and
	// never rebuilt. A later save is stored correctly and still reaches
	// gopeed's config, but no torrent this process starts will read it.
	Port int `json:"port"`

	// DHTEnabled and PEXEnabled are this instance's default participation in
	// the swarm's peer discovery for an ordinary, non-private torrent. A
	// torrent whose metadata marks it private (BEP 27's info.private) gets
	// neither, with no toggle able to set either back to true for it. See
	// EffectiveDHT and EffectivePEX below.
	//
	// For an ordinary torrent neither field is wired, and a UI must not imply
	// otherwise. gopeed's bt.config carries five fields (ListenPort, Trackers,
	// SeedKeep, SeedRatio, SeedTime) and neither DHT nor PEX; the vendored
	// gopeed source mentions NoDHT and DisablePEX nowhere, and initClient
	// builds the shared torrent.Client from NewDefaultClientConfig, overriding
	// six fields that do not include them. Both therefore sit at anacrolix's
	// defaults, DHT and PEX on, for the life of the process. Wiring them would
	// also stay process-wide: NoDHT and DisablePEX are ClientConfig fields
	// consumed once by torrent.NewClient, on a client built once.
	//
	// The private-torrent half needs no wiring from here. gopeed reads
	// info.Private only to skip adding extra trackers, but anacrolix/torrent
	// v1.61.1-0.20260525011549 gates five places on the torrent's own
	// info.Private: dhtAnnouncer's per-iteration announce loop, PEX connection
	// init, and both directions of Local Peer Discovery. That is per torrent
	// rather than per client, one layer below gopeed. For a magnet the privacy
	// takes effect once metadata arrives, which is what the per-iteration
	// re-check is for.
	DHTEnabled bool `json:"dhtEnabled"`
	// PEXEnabled is the Peer Exchange half of the pair. See DHTEnabled.
	PEXEnabled bool `json:"pexEnabled"`

	// TorrentFileRules choose the files of a torrent nobody chose by hand. A
	// category can carry its own in their place, see Category.TorrentFiles.
	// Embedded, so its three fields sit directly in this block's JSON.
	TorrentFileRules

	// ExtraTrackers are announce addresses added to every torrent that is not
	// private, and TrackerListURL a public list of more, fetched at most once
	// a day (internal/trackerlist). With both empty, the default, none is
	// added. See resolver/torrent.ExtraTrackers for how a magnet's privacy is
	// judged before its metadata says, which a private tracker that does not
	// put a key in its address gets past.
	ExtraTrackers  []string `json:"extraTrackers"`
	TrackerListURL string   `json:"trackerListUrl"`
	// BannedTrackers are host names, or addresses whose host counts. A torrent
	// that announces to one is held back at intake with the reason, like a
	// link the link filter refuses, and none is ever added as an extra.
	BannedTrackers []string `json:"bannedTrackers"`

	// KeepOnService leaves on a debrid account what KnightLoader fetched from
	// it: a torrent once its files are here, and a download imported from the
	// account also when its task is removed. Off, both are deleted there, so
	// they do not pile up against the account's limits.
	KeepOnService bool `json:"keepOnService"`
}

// TorrentFileRules are the file selection resolver/torrent.FileRules applies
// to a torrent at its start, when a magnet's file list arrives or a .torrent
// is started whose file list nobody changed. MinFileSize is in bytes, 0 for
// no minimum; the two lists are regular expressions, one per line. All empty,
// the default, fetches every file.
//
// No omitempty on the lists, see CrawlInclude.
type TorrentFileRules struct {
	MinFileSize  int64    `json:"minFileSize"`
	IncludeFiles []string `json:"includeFiles"`
	ExcludeFiles []string `json:"excludeFiles"`
}

// TorrentFileRulesFor is the file selection for a torrent in this category:
// the category's own set when it has one, the Torrents page's otherwise.
func (s Settings) TorrentFileRulesFor(id string) TorrentFileRules {
	if r := s.CategoryFor(id).TorrentFiles; r != nil {
		return *r
	}
	return s.Torrent.TorrentFileRules
}

// defaultTorrent is Torrent's starting values for a fresh install. The numbers
// are gopeed's own rather than invented ones, see the fields' doc comments. DHT
// and PEX default on, which is what the client does regardless of this setting
// and what an ordinary BitTorrent client does on a public swarm.
func defaultTorrent() Torrent {
	return Torrent{
		SeedRatioTarget:     1.0,
		SeedDurationSeconds: 2 * 60 * 60,
		DHTEnabled:          true,
		PEXEnabled:          true,
	}
}

// sanitizeTorrent floors every number that has no honest negative meaning back
// to its unset zero, and keeps Port inside the range a TCP port has. A negative
// seed target, a negative KiB/s and a port outside 0 to 65535 are a form that
// let a minus sign or a stray digit through, not a choice somebody made.
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
		// refused, the direction Chunks folds an unusable number in
		// sanitizeNetwork: nothing typed into a spinner should cost somebody
		// the rest of the page.
		t.Port = 0
	}
	t.TorrentFileRules = t.TorrentFileRules.sanitized()
	t.ExtraTrackers = trimmedLines(t.ExtraTrackers)
	t.BannedTrackers = trimmedLines(t.BannedTrackers)
	t.TrackerListURL = strings.TrimSpace(t.TrackerListURL)
	return n
}

// sanitized floors a negative minimum and drops blank lines, as sanitizeIntake
// does, since a blank exclude pattern matches every file. A pattern that does
// not compile stays as typed; the API refuses it at save time, and a torrent
// that meets it fails with the reason rather than fetching what somebody asked
// to skip.
func (r TorrentFileRules) sanitized() TorrentFileRules {
	if r.MinFileSize < 0 {
		r.MinFileSize = 0
	}
	r.IncludeFiles = nonBlank(r.IncludeFiles)
	r.ExcludeFiles = nonBlank(r.ExcludeFiles)
	return r
}

// trimmedLines is nonBlank for lines whose surrounding spaces mean nothing,
// such as addresses.
func trimmedLines(in []string) []string {
	out := nonBlank(in)
	if len(out) == 0 {
		return out
	}
	trimmed := make([]string, len(out))
	for i, s := range out {
		trimmed[i] = strings.TrimSpace(s)
	}
	return trimmed
}

// EffectiveDHT is the per-torrent answer: this instance's default, unless the
// torrent is private, in which case false. A private torrent gets no vote, from
// this setting or from anything done to it afterwards.
//
// private is BEP 27's info.private, read wherever a torrent's metadata is first
// parsed: resolver/torrent.Metadata.Private for an uploaded .torrent, checked
// before a byte is written, and for a magnet once internal/engine's resolve has
// the swarm's answer. A bare bool rather than that Metadata type keeps this
// package off the resolver for one field.
//
// It is a stated policy rather than an enforcement point. Nothing in
// internal/engine reads either return value, and for an ordinary torrent
// gopeed's public API gives this package nothing to set. For a private torrent
// the answer matches what anacrolix/torrent already enforces inside the
// library, so it would hold even without this function. See DHTEnabled.
func (t Torrent) EffectiveDHT(private bool) bool {
	return t.DHTEnabled && !private
}

// EffectivePEX is the Peer Exchange half. See EffectiveDHT.
func (t Torrent) EffectivePEX(private bool) bool {
	return t.PEXEnabled && !private
}
