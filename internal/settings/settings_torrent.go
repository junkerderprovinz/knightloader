package settings

// What a BitTorrent download does once it starts: how long it keeps giving
// bytes back after it finishes, how hard it announces itself to the swarm, the
// DHT and PEX switches a private tracker's rules need this app to get right
// without anybody asking per torrent, which of its files it fetches and which
// trackers it may or may not use. See internal/resolver/torrent for the intake
// side and internal/engine for what starts a torrent task; this file is the
// configuration those two read.

import (
	"encoding/json"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// Torrent is the seed, port, DHT, PEX, file and tracker policy for every
// BitTorrent download this instance starts: one block per instance rather than
// one per task, as the qBittorrent KnightLoader runs has one listen port, one
// upload limit and one pair of DHT and PEX switches for all of its torrents.
type Torrent struct {
	// SeedRatioTarget is uploaded over downloaded; 0 means no ratio target, so
	// only SeedDurationSeconds applies if that is non-zero.
	SeedRatioTarget float64 `json:"seedRatioTarget"`
	// SeedDurationSeconds is how long to keep seeding after the download
	// finishes; 0 means no duration target. Whichever of the two is reached
	// first stops the seeding, and both zero seeds forever. KnightLoader
	// stops a torrent at its target itself, from its own totals, so
	// qBittorrent's share limits stay off.
	SeedDurationSeconds int `json:"seedDurationSeconds"`

	// UploadLimit caps upload bandwidth in bytes per second for all torrents
	// together, like SpeedLimit for downloads; 0 is unlimited.
	UploadLimit int64 `json:"uploadLimit"`

	// Port is the port qBittorrent listens on for peers; 0 keeps the one it
	// picked on its first start.
	Port int `json:"port"`

	// DHTEnabled and PEXEnabled are this instance's default participation in
	// the swarm's peer discovery for an ordinary, non-private torrent. A
	// torrent whose metadata marks it private (BEP 27's info.private) gets
	// neither, whatever these say: libtorrent turns both off per torrent, and
	// for a magnet as soon as its metadata says so. See EffectiveDHT.
	DHTEnabled bool `json:"dhtEnabled"`
	// PEXEnabled is the Peer Exchange half of the pair. See DHTEnabled.
	PEXEnabled bool `json:"pexEnabled"`

	// Interface is the network interface, by its system name, that every
	// torrent's traffic is tied to: peers, trackers and the DHT. Empty is
	// any. While it is missing or down, qBittorrent sends and receives
	// nothing and the torrents wait, a kill switch for a VPN interface such
	// as wg0. Torrents a debrid service fetches come over HTTP from the
	// service and are not tied to it.
	Interface string `json:"interface"`

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

	// KeepOnService leaves a torrent on the debrid account once its files are
	// here. Off, it is deleted there, so torrents do not pile up against the
	// account's limits.
	KeepOnService bool `json:"keepOnService"`
	// DebridCachedOnly hands a torrent to a debrid service only when the
	// service has it cached already, and to KnightLoader's qBittorrent
	// otherwise,
	// even where the service would take it. A service without a cache check
	// is asked by adding the torrent and deleting it again unless it is ready
	// at once, and a question that goes unanswered counts as not cached.
	DebridCachedOnly bool `json:"debridCachedOnly"`
	// DebridStallMinutes is how long a debrid service may make no progress on
	// a torrent it fetches before the torrent is deleted there and goes on to
	// the next backend; 0 waits however long it takes.
	DebridStallMinutes int `json:"debridStallMinutes"`
	// SeedAfterDebrid hands a torrent a debrid service fetched to a client to
	// seed once its files are here, the one SeedIn names. Its files are checked
	// against the torrent first, and a torrent whose files are not all there is
	// not seeded rather than fetched again.
	SeedAfterDebrid bool `json:"seedAfterDebrid"`
	// SeedIn is where SeedAfterDebrid seeds: SeedInBuiltIn, the qBittorrent
	// KnightLoader runs, under the targets above, or SeedInQBittorrent, one
	// somebody runs themselves. Empty is the first.
	SeedIn      string      `json:"seedIn"`
	QBittorrent QBittorrent `json:"qbittorrent"`
	// EngineVersion is the qBittorrent version KnightLoader runs, one of
	// nox's version table. Empty is the table's default. It is held where it
	// is and never moved by the automatic component update, since private
	// trackers admit only the versions on their list.
	EngineVersion string `json:"engineVersion"`
	// CrossSeed seeds a finished torrent on more trackers from its files.
	CrossSeed CrossSeed `json:"crossSeed"`
}

// The clients a torrent a debrid service fetched can seed in.
const (
	SeedInBuiltIn     = "builtin"
	SeedInQBittorrent = "qbittorrent"
)

// QBittorrent is the external qBittorrent that takes the torrents a debrid
// service fetched to seed, for cross-seeding tools that watch it.
type QBittorrent struct {
	// URL is the address of its Web UI.
	URL      string `json:"url"`
	Username string `json:"username"`
	// Password is masked by Redacted and put back by WithSecretsFrom.
	Password string `json:"password"`
	// Category files each torrent in qBittorrent; empty files it nowhere.
	Category string `json:"category"`
	// DownloadsPath is KnightLoader's download folder as qBittorrent sees it,
	// for two containers that mount it under different paths. Empty means the
	// same path.
	DownloadsPath string `json:"downloadsPath"`
}

// Redacted returns a copy with the password masked, as reconnect masks the
// router password.
func (q QBittorrent) Redacted() QBittorrent {
	if q.Password != "" {
		q.Password = reconnect.RedactedPassword
	}
	return q
}

// WithSecretsFrom puts back the password Redacted masked, as long as the
// address is the one it was stored for: a client that was never shown the
// password cannot have it sent to another host. An empty password stays
// empty, which is how it is cleared.
func (q QBittorrent) WithSecretsFrom(prev QBittorrent) QBittorrent {
	if q.Password != reconnect.RedactedPassword {
		return q
	}
	q.Password = ""
	if strings.TrimSpace(q.URL) == strings.TrimSpace(prev.URL) {
		q.Password = prev.Password
	}
	return q
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

// defaultTorrent is Torrent's starting values for a fresh install: a ratio of
// one or two hours of seeding, whichever comes first, and DHT and PEX on,
// which is what an ordinary BitTorrent client does on a public swarm.
func defaultTorrent() Torrent {
	return Torrent{
		SeedRatioTarget:     1.0,
		SeedDurationSeconds: 2 * 60 * 60,
		DHTEnabled:          true,
		PEXEnabled:          true,
		SeedIn:              SeedInBuiltIn,
		DebridStallMinutes:  DefaultDebridStallMinutes,
		CrossSeed:           CrossSeed{LinkMode: LinkNone, Indexers: []TorznabIndexer{}},
	}
}

// DefaultDebridStallMinutes is long enough for a service to read a magnet
// link and take a torrent from its queue, and short enough that qBittorrent,
// which starts on the swarm at once, has not been kept waiting long.
// An install from before the field reads it too, since Load decodes over the
// defaults.
const DefaultDebridStallMinutes = 10

// maxDebridStallMinutes is a day. A service that has not moved for longer has
// given up on the torrent, whatever it reports.
const maxDebridStallMinutes = 24 * 60

// sanitizeTorrent floors every number that has no honest negative meaning back
// to its unset zero, and keeps Port inside the range a TCP port has. A negative
// seed target, a negative limit and a port outside 0 to 65535 are a form that
// let a minus sign or a stray digit through, not a choice somebody made.
func sanitizeTorrent(n Settings) Settings {
	t := &n.Torrent
	if t.SeedRatioTarget < 0 {
		t.SeedRatioTarget = 0
	}
	if t.SeedDurationSeconds < 0 {
		t.SeedDurationSeconds = 0
	}
	if t.UploadLimit < 0 {
		t.UploadLimit = 0
	}
	if t.Port < 0 || t.Port > 65535 {
		// Out of range collapses to "let the OS pick" rather than being
		// refused, the direction Chunks folds an unusable number in
		// sanitizeNetwork: nothing typed into a spinner should cost somebody
		// the rest of the page.
		t.Port = 0
	}
	t.DebridStallMinutes = min(max(t.DebridStallMinutes, 0), maxDebridStallMinutes)
	t.TorrentFileRules = t.TorrentFileRules.sanitized()
	t.ExtraTrackers = trimmedLines(t.ExtraTrackers)
	t.BannedTrackers = trimmedLines(t.BannedTrackers)
	t.TrackerListURL = strings.TrimSpace(t.TrackerListURL)
	t.Interface = strings.TrimSpace(t.Interface)
	t.EngineVersion = strings.TrimSpace(t.EngineVersion)
	// A client this build does not know seeds in KnightLoader's own, as empty
	// does.
	if t.SeedIn != "" && t.SeedIn != SeedInQBittorrent {
		t.SeedIn = SeedInBuiltIn
	}
	q := &t.QBittorrent
	q.URL = strings.TrimSpace(q.URL)
	q.Category = strings.TrimSpace(q.Category)
	q.DownloadsPath = strings.TrimSpace(q.DownloadsPath)
	t.CrossSeed = t.CrossSeed.sanitized()
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
// before a byte is written, and for a magnet once the client has the swarm's
// answer. A bare bool rather than that Metadata type keeps this
// package off the resolver for one field.
//
// It is a stated policy rather than an enforcement point: internal/engine
// hands the torrent client DHTEnabled itself, and for a private torrent the
// answer matches what libtorrent already enforces. See DHTEnabled.
func (t Torrent) EffectiveDHT(private bool) bool {
	return t.DHTEnabled && !private
}

// EffectivePEX is the Peer Exchange half. See EffectiveDHT.
func (t Torrent) EffectivePEX(private bool) bool {
	return t.PEXEnabled && !private
}

// migrateUploadLimit reads the uploadLimitKiBs an earlier build wrote as the
// bytes per second UploadLimit holds, so an old file keeps its exact limit. A
// document that already has uploadLimit has been saved by this build.
func migrateUploadLimit(raw []byte, n Settings) Settings {
	var old struct {
		Torrent struct {
			KiBs  *int64 `json:"uploadLimitKiBs"`
			Bytes *int64 `json:"uploadLimit"`
		} `json:"torrent"`
	}
	if json.Unmarshal(raw, &old) != nil || old.Torrent.KiBs == nil || old.Torrent.Bytes != nil {
		return n
	}
	n.Torrent.UploadLimit = max(*old.Torrent.KiBs, 0) * 1024
	return n
}
