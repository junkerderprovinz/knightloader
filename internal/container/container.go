// Package container reads the link-container files a download manager is
// expected to open: a plain list of links, and the encrypted formats the
// scene has used for twenty years — DLC, CCF and RSDF.
//
// Only the plain formats are decoded here, and that is a deliberate line
// rather than a gap. A .dlc cannot be opened offline by anyone: the key lives
// with a service that hands it out to registered clients, which is why no open
// client generates or decrypts one on its own. Rather than borrow somebody
// else's application key and pretend to be their client, KnightLoader hands
// the container to the headless JDownloader it already ships as its catch-all
// backend, which has its own key and does this legitimately.
//
// So this package answers two questions and refuses to guess at a third:
// what kind of container is this, and — when it is a plain one — which links
// are inside it. For an encrypted one it returns ErrNeedsBackend, having first
// checked that the file really is what its name claims, so the caller can say
// "this needs the JDownloader backend, which is not configured" instead of
// "something went wrong".
package container

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/junkerderprovinz/knightloader/internal/linkscan"
)

// Kind is the container format.
type Kind string

const (
	// KindText is a plain list of links, one per line — the format every
	// forum post and every "links.txt" in a zip actually uses.
	KindText Kind = "text"
	KindDLC  Kind = "dlc"
	KindCCF  Kind = "ccf"
	KindRSDF Kind = "rsdf"
	// KindUnknown is a file we should not pretend to understand.
	KindUnknown Kind = "unknown"
)

// ErrNeedsBackend means the container is real and well-formed but encrypted,
// so a backend holding the key has to open it. The caller is expected to hand
// the bytes to that backend rather than to report a failure: this is not a
// broken file, it is a file we deliberately do not decrypt ourselves.
var ErrNeedsBackend = errors.New("this container is encrypted and has to be opened by the JDownloader backend")

// ErrEmpty is a container with nothing usable in it. Kept separate from a
// parse failure because "you dropped an empty file" and "this file is not what
// it claims to be" are different mistakes with different fixes.
var ErrEmpty = errors.New("no links in this file")

// maxBytes caps what will be read as a container. Link lists and containers
// are kilobytes; a hundred megabytes of anything is either a mistake or an
// attempt to make the server allocate it, and neither deserves the memory.
const MaxBytes = 8 << 20

// dlcKeyLen is the length of the key block a DLC carries at its very end. It
// is fixed by the format, and a file too short to hold one cannot be a DLC no
// matter what its name says.
const dlcKeyLen = 88

// Detect names the format. The extension is a hint, never the answer: a file
// saved as "links.dlc" from a browser that appended .txt, or a .dlc renamed by
// a forum's uploader, are both routine. The content decides, and the extension
// only breaks ties between formats that look alike.
func Detect(name string, data []byte) Kind {
	ext := strings.ToLower(name)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i+1:]
	} else {
		ext = ""
	}

	// A link list is recognisable without any guessing: it contains a scheme
	// we can act on. Checked first because it is the only format we can fully
	// serve, and because a text file whose extension somebody renamed is the
	// most common case of all.
	if looksLikeLinks(data) {
		return KindText
	}

	switch {
	case ext == "txt" || ext == "text":
		// A .txt with no scheme in it is still a link list as far as the user
		// is concerned — they dropped it meaning to add links. Classifying it
		// as text lets the answer be "no links in this file", which names what
		// to fix, instead of "unrecognised format", which does not.
		return KindText
	case isDLC(data):
		return KindDLC
	case ext == "rsdf" && isHexBlob(data):
		return KindRSDF
	case ext == "ccf":
		// CCF has no signature worth trusting; it is a legacy format whose
		// only reliable marker is its name. Sending it to a backend that will
		// reject it beats refusing a file that might well be valid.
		return KindCCF
	case ext == "dlc":
		// Named .dlc but structurally not one. Reported as DLC anyway so the
		// caller's error names the format the user believes they have.
		return KindDLC
	}
	return KindUnknown
}

// Links returns the links in a plain container. For an encrypted one it
// returns ErrNeedsBackend, and for anything unrecognised an error naming what
// was actually seen — a caller that logs "unsupported container" without
// saying what it looked at leaves the user with nothing to act on.
func Links(name string, data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, ErrEmpty
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("container is %d bytes, over the %d byte limit", len(data), MaxBytes)
	}
	switch k := Detect(name, data); k {
	case KindText:
		links := parseText(string(data))
		if len(links) == 0 {
			return nil, ErrEmpty
		}
		return links, nil
	case KindDLC:
		if err := ValidateDLC(data); err != nil {
			return nil, err
		}
		return nil, ErrNeedsBackend
	case KindCCF, KindRSDF:
		return nil, ErrNeedsBackend
	default:
		return nil, fmt.Errorf("%s is not a link list or a container we recognise", name)
	}
}

// ValidateDLC checks that a file really is a DLC before anything is done with
// it. The point is the error message: a truncated download, an HTML error page
// saved with a .dlc name, or a file the browser gzipped are all common, and
// each produces a different, useless failure much later — inside a backend, or
// in a service request — unless it is caught here.
func ValidateDLC(data []byte) error {
	body := strings.TrimSpace(string(data))
	if len(body) <= dlcKeyLen {
		return fmt.Errorf("this file is %d bytes, too short to be a DLC (the key block alone is %d)", len(body), dlcKeyLen)
	}
	if !isBase64(body) {
		return errors.New("this file is not a DLC: a DLC is base64 from end to end, and this contains other bytes (a truncated download or an error page saved under the wrong name)")
	}
	// The last 88 characters are the key block, itself base64 around another
	// base64 string. If that does not decode, the file is damaged in a way
	// that will otherwise only surface as a decryption failure with no cause.
	key := body[len(body)-dlcKeyLen:]
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("the DLC key block is damaged: %w", err)
	}
	if len(raw) == 0 {
		return errors.New("the DLC key block is empty")
	}
	return nil
}

// isDLC is the structural test Detect uses: base64 throughout and long enough
// to carry a key block. Deliberately looser than ValidateDLC, which is there
// to explain a rejection rather than to classify.
func isDLC(data []byte) bool {
	body := strings.TrimSpace(string(data))
	return len(body) > dlcKeyLen && isBase64(body)
}

func isBase64(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '+', r == '/', r == '=':
		case r == '\r', r == '\n':
		default:
			return false
		}
	}
	return true
}

func isHexBlob(data []byte) bool {
	n := 0
	for _, r := range strings.TrimSpace(string(data)) {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
			n++
		case unicode.IsSpace(r):
		default:
			return false
		}
	}
	return n > 0
}

// looksLikeLinks reports whether the bytes are a link list. It requires an
// actual scheme rather than merely being printable, because "printable" is
// also true of a README, and staging a README's every word as a download is
// worse than refusing the file.
func looksLikeLinks(data []byte) bool {
	// Only the head is examined: a link list announces itself immediately, and
	// scanning eight megabytes to answer a yes/no question is wasted work.
	head := data
	if len(head) > 8<<10 {
		head = head[:8<<10]
	}
	s := strings.ToLower(string(head))
	return strings.Contains(s, "http://") || strings.Contains(s, "https://") ||
		strings.Contains(s, "magnet:?")
}

// parseText pulls the links out of a text file, sharing the one scanner
// every other intake path uses (internal/linkscan) rather than a second,
// looser splitter of its own - a wave that only fixed the paste box would
// leave a container's own .txt list unable to rejoin a mail-wrapped link or
// tell a matched Wikipedia-style bracket from an unmatched one, exactly the
// gap this delegation exists to close. Links above has already turned the
// file down when it holds nothing scheme-shaped at all (see looksLikeLinks
// and Detect), so parseText itself always applies the full scanner, with
// none of the settings-driven off switch POST /api/links has: a container
// is a file somebody deliberately handed over as a link list, not free-form
// prose that might not be one.
func parseText(s string) []string {
	return linkscan.Extract(s)
}
