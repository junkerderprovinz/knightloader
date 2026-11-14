package torrent

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// dataPrefix is the data-URI prefix an uploaded .torrent is carried under.
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
// with a reason. A link without an info hash would have the client wait for
// metadata forever.
func checkMagnet(uri string) (magnetInfo, error) {
	if len(uri) > MaxMagnetBytes {
		return magnetInfo{}, fmt.Errorf("%w: %d characters, the limit is %d", ErrBadMagnet, len(uri), MaxMagnetBytes)
	}
	m, err := parseMagnet(uri)
	if err != nil {
		return magnetInfo{}, fmt.Errorf("%w: %v", ErrBadMagnet, err)
	}
	if m.v1 == "" && !m.v2 {
		return magnetInfo{}, fmt.Errorf("%w: it names no info hash, so there is nothing to look for", ErrBadMagnet)
	}
	// qBittorrent's libtorrent 1.2 has no BitTorrent v2.
	if m.v1 == "" {
		return magnetInfo{}, fmt.Errorf(
			"%w: it is a v2-only magnet link, which this build cannot start; a link that also carries the older urn:btih hash will work", ErrBadMagnet)
	}
	if strings.Trim(m.v1, "0") == "" {
		return magnetInfo{}, fmt.Errorf("%w: its info hash is all zeroes, which is not a torrent", ErrBadMagnet)
	}
	out := magnetInfo{
		DisplayName: strings.TrimSpace(m.name),
		InfoHash:    m.v1,
	}
	if len(m.trackers) > MaxTrackers {
		return magnetInfo{}, fmt.Errorf("%w: %d trackers, the limit is %d", ErrBadMagnet, len(m.trackers), MaxTrackers)
	}
	for _, t := range m.trackers {
		for _, r := range t {
			if r < 0x20 || r == 0x7f {
				return magnetInfo{}, fmt.Errorf("%w: a tracker URL contains a control character", ErrBadMagnet)
			}
		}
	}
	out.Trackers = m.trackers
	// The display name becomes a task name and possibly a file name, so an
	// unsafe one is dropped.
	if out.DisplayName != "" && safeComponent(out.DisplayName) != nil {
		out.DisplayName = ""
	}
	return out, nil
}

// magnet is what a magnet link says: its v1 info hash in lower-case hex,
// whether it carries a v2 one, its display name and its trackers.
type magnet struct {
	v1       string
	v2       bool
	name     string
	trackers []string
}

func parseMagnet(uri string) (magnet, error) {
	if !IsMagnet(uri) {
		return magnet{}, errors.New("it is not a magnet link")
	}
	q, err := url.ParseQuery(strings.TrimPrefix(uri[len("magnet:"):], "?"))
	if err != nil {
		return magnet{}, err
	}
	var m magnet
	for _, xt := range q["xt"] {
		switch {
		case strings.HasPrefix(xt, "urn:btih:"):
			h, err := btih(xt[len("urn:btih:"):])
			if err != nil {
				return magnet{}, err
			}
			m.v1 = h
		case strings.HasPrefix(xt, "urn:btmh:"):
			mh := xt[len("urn:btmh:"):]
			if len(mh) != 68 || !strings.HasPrefix(mh, "1220") {
				return magnet{}, fmt.Errorf("the v2 hash %q is not a sha2-256 multihash", mh)
			}
			if _, err := hex.DecodeString(mh); err != nil {
				return magnet{}, fmt.Errorf("the v2 hash %q is not hex", mh)
			}
			m.v2 = true
		}
	}
	m.name = q.Get("dn")
	m.trackers = q["tr"]
	return m, nil
}

// btih reads a v1 info hash in hex or, as older links write it, base32.
func btih(s string) (string, error) {
	switch len(s) {
	case 40:
		if _, err := hex.DecodeString(s); err == nil {
			return strings.ToLower(s), nil
		}
	case 32:
		if b, err := base32.StdEncoding.DecodeString(strings.ToUpper(s)); err == nil {
			return hex.EncodeToString(b), nil
		}
	}
	return "", fmt.Errorf("the info hash %q is neither 40 hex digits nor 32 base32 ones", s)
}
