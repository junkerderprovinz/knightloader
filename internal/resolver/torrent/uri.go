package torrent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
)

// dataPrefix is the data-URI prefix gopeed's bt fetcher accepts (its
// FilterTypeBase64 "APPLICATION/X-BITTORRENT"), so a resolved URI goes to the
// engine unchanged.
const dataPrefix = "data:application/x-bittorrent;base64,"

// MaxMagnetBytes caps a magnet URI. Fifty trackers and a display name fit in
// a couple of kilobytes.
const MaxMagnetBytes = 16 << 10

var (
	// ErrBadMagnet is a magnet: URI this app will not act on.
	ErrBadMagnet = errors.New("this magnet link is not usable")
	// ErrBadDataURI is a .torrent data URI whose base64 will not decode.
	ErrBadDataURI = errors.New("this uploaded .torrent could not be decoded")
)

// EncodeBytes turns validated .torrent bytes into the URI the rest of the app
// carries a torrent as. Everything downstream of intake takes a string, and a
// temp file path would not survive until a restart re-resolves the task.
//
// The caller must have run Parse over these bytes first; this checks nothing,
// so there is no second, laxer gate.
func EncodeBytes(b []byte) string {
	return dataPrefix + base64.StdEncoding.EncodeToString(b)
}

// DecodeBytes is EncodeBytes backwards, size-limited.
func DecodeBytes(uri string) ([]byte, error) {
	if !hasDataPrefix(uri) {
		return nil, ErrBadDataURI
	}
	enc := uri[len(dataPrefix):]
	// Checked before decoding, so an oversized input is never allocated.
	if base64.StdEncoding.DecodedLen(len(enc)) > MaxTorrentBytes {
		return nil, fmt.Errorf("%w: it decodes to more than %d bytes", ErrTooLarge, MaxTorrentBytes)
	}
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadDataURI, err)
	}
	if len(b) > MaxTorrentBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(b))
	}
	return b, nil
}

func hasDataPrefix(s string) bool {
	return len(s) > len(dataPrefix) && strings.EqualFold(s[:len(dataPrefix)], dataPrefix)
}

// IsMagnet reports whether s has the magnet: scheme.
func IsMagnet(s string) bool {
	return len(s) > len("magnet:") && strings.EqualFold(s[:len("magnet:")], "magnet:")
}

// IsURI reports whether s is one of the two shapes the torrent backend takes.
// The engine uses it to recognise a torrent job.
func IsURI(s string) bool {
	return IsMagnet(s) || hasDataPrefix(s)
}

// LooksLikeTorrent reports whether b looks like a bencoded torrent: a
// dictionary with an "info" key near the front. It judges content because an
// uploaded file's name is the uploader's claim. It is a cheap test for Match;
// Parse is the real gate.
func LooksLikeTorrent(b []byte) bool {
	if len(b) < 3 || b[0] != 'd' {
		return false
	}
	// "4:info" is the bencoded key. The scan is bounded because Match runs on
	// untrusted input for every link.
	head := b
	if len(head) > 64<<10 {
		head = head[:64<<10]
	}
	return strings.Contains(string(head), "4:info")
}

// magnetInfo is what a magnet says about itself before any network happens.
type magnetInfo struct {
	InfoHash    string
	DisplayName string
	Trackers    []string
}

// checkMagnet refuses a magnet link this app cannot act on, at paste time,
// with a reason. anacrolix's ParseMagnetV2Uri accepts "magnet:?dn=something"
// without an info hash, and the client would then wait for metadata forever.
func checkMagnet(uri string) (magnetInfo, error) {
	if len(uri) > MaxMagnetBytes {
		return magnetInfo{}, fmt.Errorf("%w: %d characters, the limit is %d", ErrBadMagnet, len(uri), MaxMagnetBytes)
	}
	m, err := metainfo.ParseMagnetV2Uri(uri)
	if err != nil {
		return magnetInfo{}, fmt.Errorf("%w: %v", ErrBadMagnet, err)
	}
	if !m.InfoHash.Ok && !m.V2InfoHash.Ok {
		return magnetInfo{}, fmt.Errorf("%w: it names no info hash, so there is nothing to look for", ErrBadMagnet)
	}
	// The next two refusals prevent a crash: anacrolix/torrent's
	// Client.AddTorrentOpt panics on a zero v1 info hash, and gopeed calls it
	// from a fetcher goroutine where the panic kills the process. An all-zero
	// btih and a v2-only magnet (gopeed fills v1 with UnwrapOrZeroValue())
	// both reach it.
	if !m.InfoHash.Ok {
		return magnetInfo{}, fmt.Errorf(
			"%w: it is a v2-only magnet link, which this build cannot start; a link that also carries the older urn:btih hash will work", ErrBadMagnet)
	}
	if m.InfoHash.Value.IsZero() {
		return magnetInfo{}, fmt.Errorf("%w: its info hash is all zeroes, which is not a torrent", ErrBadMagnet)
	}
	out := magnetInfo{
		DisplayName: strings.TrimSpace(m.DisplayName),
		InfoHash:    m.InfoHash.Value.HexString(),
	}
	if len(m.Trackers) > MaxTrackers {
		return magnetInfo{}, fmt.Errorf("%w: %d trackers, the limit is %d", ErrBadMagnet, len(m.Trackers), MaxTrackers)
	}
	for _, t := range m.Trackers {
		for _, r := range t {
			if r < 0x20 || r == 0x7f {
				return magnetInfo{}, fmt.Errorf("%w: a tracker URL contains a control character", ErrBadMagnet)
			}
		}
	}
	out.Trackers = m.Trackers
	// The display name becomes a task name and possibly a file name, so an
	// unsafe one is dropped.
	if out.DisplayName != "" && safeComponent(out.DisplayName) != nil {
		out.DisplayName = ""
	}
	return out, nil
}
