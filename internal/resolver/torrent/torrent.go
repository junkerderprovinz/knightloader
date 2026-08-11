// Package torrent is KnightLoader's BitTorrent intake: it recognises magnet
// links and uploaded .torrent files, refuses the ones that are malformed or
// built to be hostile, and hands the rest to the embedded download engine.
//
// IT DOES NOT DOWNLOAD ANYTHING, and that is the whole architectural point of
// the wave it was built in. gopeed's embedded downloader already carries a
// BitTorrent fetcher: KnightLoader builds its DownloaderConfig with no
// FetchManagers override (internal/engine.New), and gopeed's own Init then
// defaults them to http + bt + ed2k - verified live, cmd/spike-torrent prints
// the three names it settles on. So there is no second download pipeline to
// build here and none is built. This resolver does what every other resolver in
// this tree does: it says "yes, that one is mine", it says what the link is,
// and it hands back a target the engine knows how to start.
//
// The target it hands back is the magnet URI itself, or the uploaded .torrent
// re-expressed as a data: URI (see EncodeBytes). Both are shapes the download
// library's own bt fetcher already registers as intake, so the string that
// leaves Resolve is a string the engine can pass straight through.
package torrent

import (
	"context"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Resolver claims magnet links and uploaded .torrent files.
//
// It carries no configuration. Everything a torrent download can be told - the
// seed target, the upload limit, the port, DHT and PEX - is engine and settings
// business, not routing business, and putting a copy of it here would give the
// routing table an opinion it would then have to be kept in sync about.
type Resolver struct{}

// Prio sits above every other resolver in the tree (direct is 40) because
// nothing else can possibly want these links: no other Match looks at a
// magnet: or a data: URI at all. The number is high anyway rather than
// arbitrary, so that a later resolver added with a broad Match cannot quietly
// start intercepting torrents.
func (Resolver) Info() resolver.Info { return resolver.Info{ID: "torrent", Prio: 50} }

// Match recognises the two intake shapes.
//
// A magnet is recognised by its scheme, because a magnet has nothing else - it
// is a query string, and the info hash inside it is checked in Resolve where a
// refusal can carry a reason. An uploaded .torrent is recognised BY ITS
// CONTENT: the bytes are decoded out of the data URI and tested for a bencoded
// dictionary with an info key, never by a ".torrent" suffix, because the suffix
// is chosen by whoever uploaded the file and the bytes are not.
//
// A plain "https://host/thing.torrent" is deliberately NOT matched. That link
// is an HTTP download of a file that happens to be a torrent, and the existing
// direct resolver already fetches it correctly; claiming it here would turn one
// unambiguous HTTP GET into a swarm join against a file nobody has read yet.
func (Resolver) Match(raw string) bool {
	if IsMagnet(raw) {
		return true
	}
	if !hasDataPrefix(raw) {
		return false
	}
	b, err := DecodeBytes(raw)
	return err == nil && LooksLikeTorrent(b)
}

// Resolve turns a matched link into a target the engine can start, and is the
// last place a hostile torrent can be stopped with an explanation instead of a
// confusing failure ten seconds later in somebody else's code.
//
// For an uploaded .torrent the whole file is parsed here and every check in
// Parse runs: size, piece geometry, file count, and above all the path of every
// file it contains. For a magnet there is nothing yet to parse - the file list
// arrives from the swarm minutes later, if at all - so what happens here is the
// magnet's own shape, and the file paths are checked by the engine instead, on
// what the resolve came back with, before a byte is written. See Contained.
func (r Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	if IsMagnet(req.URL) {
		m, err := checkMagnet(req.URL)
		if err != nil {
			return resolver.Result{}, err
		}
		name := m.DisplayName
		if name == "" {
			// The info hash is a poor name and a true one. A magnet with no dn is
			// ordinary, and the swarm supplies the real name at resolve time; until
			// then the row says which torrent it is rather than "download".
			name = m.InfoHash
		}
		// No Connections. It is read by the dispatcher as a per-host chunk ceiling
		// and means nothing at all to a swarm - see resolver.Direct for the same
		// silence and the bug that saying a number caused there.
		return resolver.Result{Name: name, DirectURL: req.URL}, nil
	}
	b, err := DecodeBytes(req.URL)
	if err != nil {
		return resolver.Result{}, err
	}
	md, err := Parse(b)
	if err != nil {
		return resolver.Result{}, err
	}
	// Re-encoded from the bytes that were actually parsed rather than passed
	// through verbatim. It is the same string in every honest case, and in a
	// dishonest one it is the difference between the engine fetching what was
	// checked and the engine fetching what was sent.
	return resolver.Result{Name: md.Name, DirectURL: EncodeBytes(b), Size: md.TotalSize}, nil
}

// No resolver.Checker here, and it is the same shape of decision as
// internal/resolver/torbox's: the honest question and the cheap question are
// two different questions, and this package cannot afford to ask the honest
// one.
//
// The honest question a torrent availability check would answer is "does the
// swarm have this," and answering it for real means joining the DHT and
// asking trackers - the one thing this package's own top-of-file comment says
// it does not do (IT DOES NOT DOWNLOAD ANYTHING; that is the embedded
// engine's job, once a task starts). checkMagnet, what Resolve above actually
// calls, is a local parse of the magnet URI's own text - it validates that the
// link NAMES an info hash, never that anyone still HAS the data behind it, so
// resolving successfully proves nothing about availability and building a
// Checker on top of it would be TorBox's mistake again: answering a different
// question in the availability column and calling a link "offline" that
// nobody has actually failed to reach.
//
// A Checker that asked for real would mean a live swarm join per link, on
// every recheck, for however many magnets are in the batch - ytdlp's own
// comment turns down its check for exactly this reason (no batched form, full
// cost per link), and a DHT lookup is not cheaper than a yt-dlp extraction
// just because it is a different kind of network call. Links routed here
// answer core.AvailUncheckable, the same honest "cannot ask" every other
// resolver without a Checker already answers with.

// Describe is the parsed torrent behind a matched link, for the intake that
// has to show a file tree before staging goes any further.
//
// It exists as its own entry point because Resolve cannot carry it: the
// Resolver interface's Result is a download target - a name, a URL, a size -
// and widening it with a torrent's file list would put a field on every
// resolver in the tree that only one of them will ever fill. A magnet answers
// ErrBadMagnet-free but with no files, which is the honest answer: nobody knows
// them yet.
func (Resolver) Describe(raw string) (Metadata, error) {
	if IsMagnet(raw) {
		m, err := checkMagnet(raw)
		if err != nil {
			return Metadata{}, err
		}
		return Metadata{InfoHash: m.InfoHash, Name: m.DisplayName, Trackers: m.Trackers}, nil
	}
	b, err := DecodeBytes(raw)
	if err != nil {
		return Metadata{}, err
	}
	return Parse(b)
}

// ParseUpload is the whole intake gate for an uploaded .torrent, in one call:
// the bytes are checked, and what comes back is both the tree to show and the
// URI to stage. A route that calls this cannot accidentally stage bytes it did
// not check, because there is no way to get the URI without the parse.
func ParseUpload(b []byte) (Metadata, string, error) {
	md, err := Parse(b)
	if err != nil {
		return Metadata{}, "", err
	}
	return md, EncodeBytes(b), nil
}
