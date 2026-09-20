// Package torrent recognises magnet links and uploaded .torrent files,
// refuses malformed or hostile ones, and hands the rest to the embedded
// download engine.
//
// It downloads nothing itself: gopeed's downloader already includes a
// BitTorrent fetcher (internal/engine.New leaves the default http, bt and
// ed2k fetchers in place). The target it returns is the magnet URI itself, or
// the uploaded .torrent as a data: URI (see EncodeBytes), both of which the bt
// fetcher accepts as they are.
package torrent

import (
	"context"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Resolver claims magnet links and uploaded .torrent files. It carries no
// configuration; seeding, limits, port, DHT and PEX belong to the engine and
// settings.
type Resolver struct{}

// Info sets priority 50, above resolver.Direct (40). No other Match looks at
// magnet: or data: URIs, and the high number keeps a later broad Match from
// intercepting torrents.
func (Resolver) Info() resolver.Info { return resolver.Info{ID: "torrent", Prio: 50} }

// Match recognises a magnet by its scheme and an uploaded .torrent by its
// content, never by a ".torrent" suffix the uploader chose. The info hash is
// checked in Resolve, where a refusal can carry a reason.
//
// A plain "https://host/thing.torrent" is not matched: that is an HTTP
// download the direct resolver already handles.
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
// last place a hostile torrent can be refused with an explanation.
//
// An uploaded .torrent is fully parsed and checked here: size, piece
// geometry, file count and every file path. A magnet has no file list until
// the swarm sends it, so only its shape is checked here and the engine checks
// the paths later (see Contained).
func (r Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	if IsMagnet(req.URL) {
		m, err := checkMagnet(req.URL)
		if err != nil {
			return resolver.Result{}, err
		}
		name := m.DisplayName
		if name == "" {
			// Until the swarm supplies the real name, the info hash at least
			// identifies the torrent.
			name = m.InfoHash
		}
		// Connections is a per-host chunk ceiling and means nothing to a
		// swarm.
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
	// Re-encoded from the parsed bytes, so the engine fetches exactly what was
	// checked.
	return resolver.Result{Name: md.Name, DirectURL: EncodeBytes(b), Size: md.TotalSize}, nil
}

// Resolver implements no resolver.Checker. checkMagnet only parses the URI,
// which says nothing about whether any peer still has the data, and asking
// the swarm means a DHT and tracker lookup per link on every recheck. Links
// routed here stay core.AvailUncheckable.

// Describe returns the parsed torrent behind a matched link, for an intake
// that shows a file tree before staging. Resolve cannot carry a file list in
// its Result. A magnet has no files yet.
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

// ParseUpload checks an uploaded .torrent and returns both the tree to show
// and the URI to stage, so no caller can stage bytes that were not checked.
func ParseUpload(b []byte) (Metadata, string, error) {
	md, err := Parse(b)
	if err != nil {
		return Metadata{}, "", err
	}
	return md, EncodeBytes(b), nil
}
