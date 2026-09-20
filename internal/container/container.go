// Package container reads link-container files: a plain list of links, and
// the encrypted DLC, CCF and RSDF formats.
//
// Only plain lists are decoded here. A .dlc can only be opened with a key a
// service hands out to registered clients, so KnightLoader passes encrypted
// containers to its bundled headless JDownloader, which has its own key,
// rather than borrowing another client's. For an encrypted file this package
// checks that it really is what its name claims and returns ErrNeedsBackend.
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
	// KindText is a plain list of links, one per line.
	KindText Kind = "text"
	KindDLC  Kind = "dlc"
	KindCCF  Kind = "ccf"
	KindRSDF Kind = "rsdf"
	// KindUnknown is a file this package does not understand.
	KindUnknown Kind = "unknown"
)

// ErrNeedsBackend means the container is well-formed but encrypted, and the
// caller should hand the bytes to the JDownloader backend rather than report
// a failure.
var ErrNeedsBackend = errors.New("this container is encrypted and has to be opened by the JDownloader backend")

// ErrEmpty is a container with nothing usable in it, kept apart from a parse
// failure because the fix is different.
var ErrEmpty = errors.New("no links in this file")

// MaxBytes caps what will be read as a container. Real ones are kilobytes.
const MaxBytes = 8 << 20

// dlcKeyLen is the fixed length of the key block at the end of a DLC.
const dlcKeyLen = 88

// Detect names the format. The content decides and the extension only breaks
// ties, since renamed files are routine.
func Detect(name string, data []byte) Kind {
	ext := strings.ToLower(name)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i+1:]
	} else {
		ext = ""
	}

	// A link list is the only format served fully here and the most common
	// one to arrive under a wrong extension.
	if looksLikeLinks(data) {
		return KindText
	}

	switch {
	case ext == "txt" || ext == "text":
		// A .txt without links is still meant as a link list, so the answer
		// becomes "no links in this file" rather than "unrecognised format".
		return KindText
	case isDLC(data):
		return KindDLC
	case ext == "rsdf" && isHexBlob(data):
		return KindRSDF
	case ext == "ccf":
		// CCF has no signature worth trusting; its name is the only marker.
		return KindCCF
	case ext == "dlc":
		// Not structurally a DLC, but reported as one so the error names the
		// format the user believes they have.
		return KindDLC
	}
	return KindUnknown
}

// Links returns the links in a plain container. For an encrypted one it
// returns ErrNeedsBackend, and for anything unrecognised an error naming the
// file.
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

// ValidateDLC checks that a file really is a DLC, so a truncated download or
// an HTML error page saved as .dlc gets a clear reason here instead of an
// unexplained decryption failure later.
func ValidateDLC(data []byte) error {
	body := strings.TrimSpace(string(data))
	if len(body) <= dlcKeyLen {
		return fmt.Errorf("this file is %d bytes, too short to be a DLC (the key block alone is %d)", len(body), dlcKeyLen)
	}
	if !isBase64(body) {
		return errors.New("this file is not a DLC: a DLC is base64 from end to end, and this contains other bytes (a truncated download or an error page saved under the wrong name)")
	}
	// The last 88 characters are the key block, itself base64.
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

// isDLC is Detect's structural test: base64 throughout and long enough to
// carry a key block. ValidateDLC is stricter and explains a rejection.
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

// looksLikeLinks reports whether the first 8 KiB contain a link scheme. Being
// printable is not enough, or a README would be queued word by word.
func looksLikeLinks(data []byte) bool {
	head := data
	if len(head) > 8<<10 {
		head = head[:8<<10]
	}
	s := strings.ToLower(string(head))
	return strings.Contains(s, "http://") || strings.Contains(s, "https://") ||
		strings.Contains(s, "magnet:?")
}

// parseText pulls the links out of a text file with the scanner every other
// intake path uses. A container is always scanned fully, since somebody
// handed it over as a link list.
func parseText(s string) []string {
	return linkscan.Extract(s)
}
