// Package checksum verifies finished downloads against the hashes that ship
// with them: .sfv listings, md5sum/sha1sum/sha256sum files, and the CRC32 tag
// that release names carry in the file name itself. Pure Go, streaming, safe to
// point at a folder full of multi-GB archives.
package checksum

import (
	"bufio"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Kind is a supported hash.
type Kind string

// The hashes that turn up next to downloads. MD5 and SHA1 are broken as
// security primitives, but that is not the job here: uploaders publish them and
// we have to speak the same language to check a transfer arrived intact.
const (
	CRC32  Kind = "crc32"
	MD5    Kind = "md5"
	SHA1   Kind = "sha1"
	SHA256 Kind = "sha256"
)

// Sum is an expected hash for one file.
type Sum struct {
	Name string // file name as written in the source, may include a path
	Kind Kind
	Hex  string // lower-case expected digest
}

// digestKinds maps the length of a hex digest to the hash that produced it.
// The lengths do not collide, so one parser reads md5sum, sha1sum and
// sha256sum output alike.
var digestKinds = map[int]Kind{
	8:  CRC32,
	32: MD5,
	40: SHA1,
	64: SHA256,
}

// bufSize is the streaming read size used while hashing multi-GB files.
const bufSize = 1 << 20

// errIllegalPath marks a sums entry whose name would resolve outside the
// directory being verified.
var errIllegalPath = errors.New("checksum: illegal path")

// bom is the UTF-8 byte order mark Windows editors put in front of the first
// line of a sums file.
const bom = "\ufeff"

// ParseSFV reads a .sfv listing (name and CRC32, ';' comments). A malformed
// line is an error rather than skipped, since skipping it would leave a file
// unverified while the run reports success.
func ParseSFV(r io.Reader) ([]Sum, error) {
	var out []Sum
	sc := bufio.NewScanner(r)
	for line := 1; sc.Scan(); line++ {
		s := trimLine(sc.Text(), line == 1)
		if s == "" || strings.HasPrefix(s, ";") {
			continue
		}
		// The CRC is the last field; everything before it is the name, which is
		// free to contain spaces and in release listings usually does.
		i := strings.LastIndexAny(s, " \t")
		if i < 0 {
			return nil, fmt.Errorf("checksum: sfv line %d: %q has no crc after the file name", line, s)
		}
		name, digest := strings.TrimSpace(s[:i]), s[i+1:]
		if name == "" || len(digest) != 8 || !isHex(digest) {
			return nil, fmt.Errorf("checksum: sfv line %d: %q is not a name plus a crc32", line, s)
		}
		out = append(out, Sum{Name: name, Kind: CRC32, Hex: strings.ToLower(digest)})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseHashFile reads the md5sum/sha1sum/sha256sum format ("<hex>  <name>",
// one per line), inferring the kind from the digest length.
func ParseHashFile(r io.Reader) ([]Sum, error) {
	var out []Sum
	sc := bufio.NewScanner(r)
	for line := 1; sc.Scan(); line++ {
		s := trimLine(sc.Text(), line == 1)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		// coreutils puts a backslash in front of the line when it had to escape
		// the file name; the digest that follows is unaffected.
		s = strings.TrimPrefix(s, `\`)
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return nil, fmt.Errorf("checksum: hash file line %d: %q has no file name after the digest", line, s)
		}
		digest := s[:i]
		// A leading '*' is the binary-mode marker, not part of the name.
		name := strings.TrimPrefix(strings.TrimLeft(s[i:], " \t"), "*")
		kind, ok := digestKinds[len(digest)]
		if !ok || !isHex(digest) {
			return nil, fmt.Errorf("checksum: hash file line %d: %q is not a supported digest", line, digest)
		}
		if name == "" {
			return nil, fmt.Errorf("checksum: hash file line %d: empty file name", line)
		}
		out = append(out, Sum{Name: name, Kind: kind, Hex: strings.ToLower(digest)})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// crcTag matches the CRC32 a release carries in its own file name, in the
// common delimiter and "CRC" spellings. Only bracketed forms count, since a
// bare eight-character hex run is too easy to hit by accident.
var crcTag = regexp.MustCompile(`(?i)[\[({]\s*(?:crc[-_ ]?(?:32)?[-_ ]?)?([0-9a-f]{8})\s*[\])}]`)

// looksLikeCRC keeps a bracketed run of eight digits such as "[20260803]",
// which is usually a date, from being read as a checksum and failing an
// intact download. A real CRC32 tag almost always contains one of a-f.
func looksLikeCRC(hex string) bool {
	for i := 0; i < len(hex); i++ {
		c := hex[i] | 0x20
		if c >= 'a' && c <= 'f' {
			return true
		}
	}
	return false
}

// FromName pulls a CRC32 tag such as "[ABCD1234]" or "{CRC-ABCD1234}" out of a
// file name, and returns ok=false when there is none.
func FromName(name string) (Sum, bool) {
	m := crcTag.FindAllStringSubmatch(name, -1)
	if len(m) == 0 {
		return Sum{}, false
	}
	// A release name usually carries several bracketed tags (group, resolution,
	// source). The CRC is by convention the last one, sitting right before the
	// extension, so the last plausible match is the one to trust.
	for i := len(m) - 1; i >= 0; i-- {
		if looksLikeCRC(m[i][1]) {
			return Sum{Name: name, Kind: CRC32, Hex: strings.ToLower(m[i][1])}, true
		}
	}
	return Sum{}, false
}

// Verify hashes the file at path and reports whether it matches.
func Verify(path string, s Sum) (bool, error) {
	got, err := hashFile(path, s.Kind)
	if err != nil {
		return false, err
	}
	return equalHex(got, s.Hex), nil
}

// Result is the outcome of checking a single Sum.
type Result struct {
	Sum Sum
	OK  bool
	Got string // digest actually computed, empty when the file could not be read
	Err error
}

// VerifyDir checks every Sum against files in dir, returning one Result per
// sum. A missing file sets Err on its Result rather than stopping the run, so
// a partial download still reports which parts are good.
func VerifyDir(dir string, sums []Sum) []Result {
	out := make([]Result, 0, len(sums))
	for _, s := range sums {
		res := Result{Sum: s}
		path, err := safePath(dir, s.Name)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		got, err := hashFile(path, s.Kind)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		res.Got = got
		res.OK = equalHex(got, s.Hex)
		out = append(out, res)
	}
	return out
}

// hashFile streams the file through the hash with one fixed-size buffer, so
// peak memory does not depend on the file size.
func hashFile(path string, k Kind) (string, error) {
	h, err := newHash(k)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.CopyBuffer(h, f, make([]byte, bufSize)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func newHash(k Kind) (hash.Hash, error) {
	switch k {
	case CRC32:
		// IEEE is the polynomial .sfv and the name tags use.
		return crc32.NewIEEE(), nil
	case MD5:
		return md5.New(), nil
	case SHA1:
		return sha1.New(), nil
	case SHA256:
		return sha256.New(), nil
	}
	return nil, fmt.Errorf("checksum: unknown hash %q", string(k))
}

// equalHex compares digests case-insensitively, since .sfv files are usually
// upper-case and coreutils writes lower-case. An empty expectation never
// matches.
func equalHex(got, want string) bool {
	return want != "" && strings.EqualFold(got, want)
}

// safePath joins name under dir and refuses anything that climbs out of it.
// A sums file is untrusted content, so this is the zip-slip check for it.
func safePath(dir, name string) (string, error) {
	// Backslashes count as separators everywhere, or "..\..\x" from a
	// Windows-written file would pass on Linux as a plain name.
	p := filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(name, `\`, "/")))
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w %q", errIllegalPath, name)
	}
	return p, nil
}

// trimLine strips trailing whitespace and, on the first line, a UTF-8 BOM. Both
// are routine in sums files exported by Windows tools.
func trimLine(s string, first bool) string {
	if first {
		s = strings.TrimPrefix(s, bom)
	}
	return strings.TrimRight(s, " \t\r")
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
