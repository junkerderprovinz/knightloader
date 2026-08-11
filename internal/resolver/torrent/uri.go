package torrent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
)

// dataPrefix is the exact data-URI prefix the download library's own bt fetcher
// registers as one of its three intake shapes (its FetcherManager.Filters
// declares FilterTypeBase64 "APPLICATION/X-BITTORRENT"). Matching it here is
// therefore not this app inventing an encoding: it is naming the one the
// library already answers to, so the URI a resolve produces is a URI the engine
// can hand straight over.
const dataPrefix = "data:application/x-bittorrent;base64,"

// MaxMagnetBytes caps a magnet URI. A magnet with fifty trackers and a display
// name runs to a couple of kilobytes; sixteen leaves room and still refuses a
// link whose length is the point.
const MaxMagnetBytes = 16 << 10

var (
	// ErrBadMagnet is a magnet: URI this app will not act on.
	ErrBadMagnet = errors.New("this magnet link is not usable")
	// ErrBadDataURI is a .torrent data URI whose base64 will not decode.
	ErrBadDataURI = errors.New("this uploaded .torrent could not be decoded")
)

// EncodeBytes turns validated .torrent bytes into the URI the rest of the app
// carries a torrent as.
//
// A .torrent is a FILE and every seam downstream of intake - the resolver
// interface's Request.URL, core.Task.URL, the store column, the engine's own
// Job - is a string. The two ways to bridge that are a temp file whose path
// travels instead, or the bytes themselves in a URI. This is the second one,
// because the first has a lifetime problem with no good answer: the task
// outlives the request, a restart re-resolves it, and a path to a temp file
// cleaned up in between is a task that can never be restarted.
//
// The caller must have run Parse over these bytes first. This does no checking
// of its own on purpose - a second, laxer gate beside the real one is how
// something gets in through the wrong door.
func EncodeBytes(b []byte) string {
	return dataPrefix + base64.StdEncoding.EncodeToString(b)
}

// DecodeBytes is EncodeBytes backwards, size-limited.
func DecodeBytes(uri string) ([]byte, error) {
	if !hasDataPrefix(uri) {
		return nil, ErrBadDataURI
	}
	enc := uri[len(dataPrefix):]
	// Checked before decoding, not after: base64 grows by a third, so a decoded
	// length test still allocates the whole thing first.
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

// IsMagnet is a scheme check and nothing more, because a magnet URI has nothing
// else to look at - there are no bytes to inspect until the swarm sends them.
func IsMagnet(s string) bool {
	return len(s) > len("magnet:") && strings.EqualFold(s[:len("magnet:")], "magnet:")
}

// IsURI reports whether this string is one of the two shapes the torrent
// backend takes. It is the engine's own test for whether a job is a torrent
// job, which is why it is here and not duplicated there.
func IsURI(s string) bool {
	return IsMagnet(s) || hasDataPrefix(s)
}

// LooksLikeTorrent reports whether these bytes are a bencoded torrent, BY
// CONTENT.
//
// Every other resolver in this tree that can check bytes checks bytes, and the
// reason is sharper here than anywhere else: the intake this feeds is a file
// upload, so the only other thing available to judge by is a filename that the
// person uploading it chose. A ".torrent" suffix is a claim, not evidence.
//
// It is a cheap shape test, not a parse - a bencoded dictionary that has an
// "info" key somewhere near the front. Parse is the real gate and this exists
// to answer Match without doing Parse's work on every pasted line.
func LooksLikeTorrent(b []byte) bool {
	if len(b) < 3 || b[0] != 'd' {
		return false
	}
	// "4:info" is the bencoded spelling of the one key a torrent cannot be
	// without. Bounded because this runs on untrusted input in a Match, which
	// gets called once per registered resolver per link.
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
// with a reason.
//
// The case worth naming is the one the parser itself allows: the library
// gopeed hands the URI to (anacrolix's ParseMagnetV2Uri, via
// TorrentSpecFromMagnetUri) returns NO ERROR for "magnet:?dn=something" - a
// magnet with no info hash at all. It parses, it produces a spec with a zero
// hash, and the torrent client then sits waiting forever for metadata about
// nothing. That is precisely the "accepted now, confusing failure later" shape
// this check exists to convert into a sentence the user reads immediately.
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
	// THE TWO REFUSALS BELOW STOP A CRASH, not a bad download, and both were
	// found by running the malformed cases rather than by reading the code.
	//
	// The torrent client this app ends up in asserts on a non-zero v1 info hash
	// with a bare panic and no error return - anacrolix/torrent's
	// Client.AddTorrentOpt opens with panicif.Zero(opts.InfoHash). gopeed calls
	// it from a fetcher goroutine, so the panic is not recoverable by its
	// caller and takes the whole process down.
	//
	// Two ordinary-looking magnet links reach it. One states an all-zeroes
	// btih, which parses perfectly and decodes to the zero hash. The other is a
	// v2-only magnet: gopeed fills the v1 field with
	// "UnwrapOrZeroValue()" when only a btmh is present, and the assertion does
	// not care that a perfectly good v2 hash sits in the field beside it.
	// Either one is a line of text somebody can paste.
	if !m.InfoHash.Ok {
		return magnetInfo{}, fmt.Errorf(
			"%w: it is a v2-only magnet link, which this build cannot start - a link that also carries the older urn:btih hash will work", ErrBadMagnet)
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
	// The display name is a stranger's string that becomes a task name and,
	// through the ordinary naming path, part of a filename. Stripped of anything
	// that is not a name here rather than left for whoever renders it.
	if out.DisplayName != "" && safeComponent(out.DisplayName) != nil {
		out.DisplayName = ""
	}
	return out, nil
}
