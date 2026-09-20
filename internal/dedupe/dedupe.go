// Package dedupe answers whether a link about to be added is already in the
// list. A duplicate is the same URL again, which is certain and always
// refused. A mirror is a different URL for the same file, which can only be
// guessed from name, size and any known hash: too eager and an unrelated file
// is lost, too shy and a release is downloaded once per hoster. Which signals
// count is a policy the user picks.
package dedupe

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Policy is the combination of signals that makes two different URLs count
// as mirrors of one file. Each value has its own failure mode.
type Policy string

const (
	// PolicyOff never merges mirrors. Duplicates are still detected.
	PolicyOff Policy = "off"
	// PolicyFilenameOnly merges on the normalised file name alone. It catches
	// one release on several hosters even when their sizes disagree, and
	// merges two unrelated files both called setup.exe.
	PolicyFilenameOnly Policy = "filename-only"
	// PolicySizeOnly merges on the exact byte count, for hosters that rename
	// what they store. Equal sizes are common, and volumes of a split archive
	// share a size by construction; couldBeSameFile keeps numbered siblings
	// apart under every policy.
	PolicySizeOnly Policy = "size-only"
	// PolicyFilenameAndSize needs both to agree, which unrelated files rarely
	// do. A link whose size is unknown can never be merged.
	PolicyFilenameAndSize Policy = "filename-and-size"
	// PolicyFilenameOrHash merges when either the name or a known digest
	// matches.
	PolicyFilenameOrHash Policy = "filename-or-hash"
	// PolicyHashOnly merges only on a matching digest. It is never wrong and
	// rarely fires, since a hash is seldom known before download.
	PolicyHashOnly Policy = "hash-only"
)

// DefaultPolicy is what an install that has never chosen gets.
const DefaultPolicy = PolicyFilenameAndSize

// Policies lists every policy once, from "never merge" towards "merge only on
// proof". It returns a fresh slice on every call.
func Policies() []Policy {
	return []Policy{
		PolicyOff,
		PolicyFilenameOnly,
		PolicySizeOnly,
		PolicyFilenameAndSize,
		PolicyFilenameOrHash,
		PolicyHashOnly,
	}
}

// Valid reports whether p is a policy this package implements.
func (p Policy) Valid() bool {
	switch p {
	case PolicyOff, PolicyFilenameOnly, PolicySizeOnly,
		PolicyFilenameAndSize, PolicyFilenameOrHash, PolicyHashOnly:
		return true
	}
	return false
}

// ParsePolicy maps a stored settings string onto a policy. Anything
// unrecognised, including "", becomes DefaultPolicy rather than an error, so
// a settings file from another build can neither block adding links nor
// silently disable mirror detection.
func ParsePolicy(s string) Policy {
	if p := Policy(strings.ToLower(strings.TrimSpace(s))); p.Valid() {
		return p
	}
	return DefaultPolicy
}

// Verdict is what a set knows about a candidate.
type Verdict int

const (
	// NotSeen is the zero value, so a Match nobody filled in means "add this
	// link"; losing a pasted link is the worse failure.
	NotSeen Verdict = iota
	// Duplicate is the same URL, already present.
	Duplicate
	// Mirror is a different URL that the policy says leads to the same file.
	Mirror
)

func (v Verdict) String() string {
	switch v {
	case Duplicate:
		return "duplicate"
	case Mirror:
		return "mirror"
	}
	return "new"
}

// Signal names the evidence a match rests on, so the interface can say why a
// link was folded away.
type Signal string

const (
	SignalURL      Signal = "url"
	SignalName     Signal = "name"
	SignalSize     Signal = "size"
	SignalNameSize Signal = "name+size"
	SignalHash     Signal = "hash"
)

// Hash is a digest already known for a file, from a checksum file or a CRC
// tag in a release name.
type Hash struct {
	// Kind names the algorithm ("crc32", "md5", "sha1", "sha256"). Digests
	// are only compared when their kinds match.
	Kind string
	// Hex is the digest; case does not matter.
	Hex string
}

// key is the bucket key for a hash, or "" when either half is missing.
func (h Hash) key() string {
	kind := strings.ToLower(strings.TrimSpace(h.Kind))
	hex := strings.ToLower(strings.TrimSpace(h.Hex))
	if kind == "" || hex == "" {
		return ""
	}
	return kind + ":" + hex
}

// Entry is one download the set knows about, or a candidate being checked.
type Entry struct {
	// ID is the caller's handle, carried through into a Match.
	ID string
	// URL identifies the entry; an entry without one is ignored.
	URL string
	// Name is the file name as shown. A name that is still the URL counts as
	// unknown.
	Name string
	// Size is the total byte count, 0 when unknown. Unknown sizes never
	// match.
	Size int64
	Hash Hash
}

// Match is the verdict for one candidate.
type Match struct {
	Verdict Verdict
	// Of is the entry the candidate matched; zero when Verdict is NotSeen.
	Of Entry
	// Signal is what matched; empty when Verdict is NotSeen.
	Signal Signal
}

// Seen reports whether the set already covers the candidate.
func (m Match) Seen() bool { return m.Verdict != NotSeen }

// keySep joins the parts of a composite bucket key. It cannot occur in a file
// name or URL, so parts cannot run into each other.
const keySep = "\x00"

// Name is a file name split into the parts that decide identity.
type Name struct {
	// Display is the name as it arrived.
	Display string
	// Base is the comparison form: no directory, no volume marker, lower
	// case, whitespace collapsed. Empty when there is nothing to compare.
	Base string
	// Volume is an opaque token for which part of a multi-part set the name
	// refers to, or "". Only equality means anything.
	Volume string
}

// key is the bucket key for a name, or "". It includes the volume so the
// parts of one archive never share a bucket.
func (n Name) key() string {
	if n.Base == "" {
		return ""
	}
	return n.Base + keySep + n.Volume
}

// volumeMarkers maps the trailing part marker of a multi-volume file to a
// token. Leading zeros are stripped, so .part01.rar and .part1.rar agree.
//
// internal/extract groups volumes by the same shapes for the opposite
// purpose: it collapses a set onto one key, while this package keeps the
// parts apart. Patterns go from most specific to least, and a bare .NNN run
// needs exactly three digits so a year like "Film.2024" is not a volume.
// Numbering schemes not listed here are caught by numberedSiblings.
var volumeMarkers = []struct {
	re    *regexp.Regexp
	token string // %s is replaced with the captured number
}{
	{regexp.MustCompile(`(?i)\.part(\d+)\.rar$`), "rar-part%s"},
	{regexp.MustCompile(`(?i)\.r(\d\d)$`), "rar-r%s"},
	{regexp.MustCompile(`(?i)\.rar$`), "rar-first"},
	{regexp.MustCompile(`(?i)\.7z\.(\d{3,})$`), "7z-part%s"},
	{regexp.MustCompile(`(?i)\.7z$`), "7z-first"},
	{regexp.MustCompile(`(?i)\.z(\d\d)$`), "zip-part%s"},
	{regexp.MustCompile(`(?i)\.zip$`), "zip-last"},
	{regexp.MustCompile(`(?i)\.(\d{3})$`), "split-part%s"},
}

// Normalize splits a file name into the parts that decide identity. A name
// that is still a URL counts as unknown, or every unresolved link would share
// one bucket and each new one would be a "mirror" of the first.
func Normalize(name string) Name {
	n := Name{Display: name}
	base := comparableName(name)
	if base == "" {
		return n
	}
	for _, m := range volumeMarkers {
		g := m.re.FindStringSubmatch(base)
		if g == nil {
			continue
		}
		num := ""
		if len(g) > 1 {
			num = trimZeros(g[1])
		}
		n.Volume = strings.Replace(m.token, "%s", num, 1)
		// Every pattern is anchored at the end, so the match is the tail.
		base = base[:len(base)-len(g[0])]
		break
	}
	// Cutting the marker off "film .rar" leaves a trailing space.
	n.Base = strings.TrimSpace(base)
	return n
}

// comparableName reduces a name to its comparison form: no directory, lower
// case, whitespace collapsed; "" when there is nothing to compare. Separators
// are left alone, since folding '.' into ' ' would merge "v1.2" with "v1 2".
func comparableName(name string) string {
	s := strings.TrimSpace(name)
	if s == "" || strings.Contains(s, "://") {
		return ""
	}
	return strings.Join(strings.Fields(strings.ToLower(baseName(s))), " ")
}

// baseName drops any directory part, treating backslashes as separators too.
func baseName(s string) string {
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// trimZeros strips leading zeros without parsing, so a number too long for an
// int still works.
func trimZeros(s string) string {
	if out := strings.TrimLeft(s, "0"); out != "" {
		return out
	}
	return "0"
}

// normalizeURL lower-cases the host and drops a default port, so the same
// link copied from two places compares equal. Nothing else changes; the
// fragment in particular stays, since some hosters keep the file's decryption
// key there. An unparseable URL is compared verbatim.
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	// Every field that tells magnets apart is case-insensitive, so folding
	// the whole URI catches an upper-cased infohash without merging two
	// torrents.
	if u.Scheme == "magnet" {
		return strings.ToLower(raw)
	}
	u.Host = strings.ToLower(u.Host)
	if p := u.Port(); (u.Scheme == "http" && p == "80") || (u.Scheme == "https" && p == "443") {
		u.Host = strings.TrimSuffix(u.Host, ":"+p)
	}
	return u.String()
}

// signature is one bucket a record can be found in, with the signal a hit in
// it reports.
type signature struct {
	signal Signal
	key    string
}

// record is an entry plus everything derived from it.
type record struct {
	entry Entry
	name  Name
	// full is the comparison name with its volume marker still attached,
	// which numberedSiblings needs to tell ".r99" from ".r100".
	full string
	hash string
	sigs []signature
}

// newRecord derives everything the set compares on, so Add and Check measure
// the same way.
func newRecord(e Entry) record {
	return record{
		entry: e,
		name:  Normalize(e.Name),
		full:  comparableName(e.Name),
		hash:  e.Hash.key(),
	}
}

// Set is a list of known downloads that answers queries without scanning it.
// It is not safe for concurrent use; build it under the lock that protects
// the task list.
type Set struct {
	policy  Policy
	byURL   map[string]record
	buckets map[string][]string // signature key -> normalised URLs
}

// New returns an empty set that merges mirrors according to p. An
// unrecognised policy is read as DefaultPolicy.
func New(p Policy) *Set {
	if !p.Valid() {
		p = DefaultPolicy
	}
	return &Set{
		policy:  p,
		byURL:   make(map[string]record),
		buckets: make(map[string][]string),
	}
}

// Policy is the policy this set was built with.
func (s *Set) Policy() Policy { return s.policy }

// Len is how many entries the set holds.
func (s *Set) Len() int { return len(s.byURL) }

// Add files an entry. Adding a URL the set already holds replaces the old
// record.
func (s *Set) Add(e Entry) {
	u := normalizeURL(e.URL)
	if u == "" {
		return
	}
	s.remove(u)
	r := newRecord(e)
	r.sigs = s.signatures(r)
	for _, sig := range r.sigs {
		s.buckets[sig.key] = append(s.buckets[sig.key], u)
	}
	s.byURL[u] = r
}

// Remove forgets a URL.
func (s *Set) Remove(rawURL string) { s.remove(normalizeURL(rawURL)) }

func (s *Set) remove(u string) {
	r, ok := s.byURL[u]
	if !ok {
		return
	}
	for _, sig := range r.sigs {
		b := slices.DeleteFunc(s.buckets[sig.key], func(v string) bool { return v == u })
		// Delete empty buckets so a long-lived set does not keep a key for
		// every removed download.
		if len(b) == 0 {
			delete(s.buckets, sig.key)
		} else {
			s.buckets[sig.key] = b
		}
	}
	delete(s.byURL, u)
}

// Check reports what the set knows about a candidate without adding it.
func (s *Set) Check(cand Entry) Match {
	u := normalizeURL(cand.URL)
	if u == "" {
		return Match{}
	}
	// The exact URL is checked first, whatever the policy.
	if r, ok := s.byURL[u]; ok {
		return Match{Verdict: Duplicate, Of: r.entry, Signal: SignalURL}
	}
	c := newRecord(cand)
	for _, sig := range s.signatures(c) {
		// Only entries sharing this signature are compared, so a query costs
		// one small bucket rather than the whole list.
		for _, other := range s.buckets[sig.key] {
			r := s.byURL[other]
			if couldBeSameFile(c, r) {
				return Match{Verdict: Mirror, Of: r.entry, Signal: sig.signal}
			}
		}
	}
	return Match{}
}

// signatures lists the buckets a record belongs in under the policy,
// strongest evidence first. An unknown signal produces no bucket, or every
// nameless link would be a mirror of the first.
func (s *Set) signatures(r record) []signature {
	name, size, hash := r.name.key(), sizeKey(r.entry.Size), r.hash
	switch s.policy {
	case PolicyFilenameOnly:
		if name != "" {
			return []signature{{SignalName, "name" + keySep + name}}
		}
	case PolicySizeOnly:
		if size != "" {
			return []signature{{SignalSize, "size" + keySep + size}}
		}
	case PolicyFilenameAndSize:
		// Both must be known; an unknown size does not fall back to the name.
		if name != "" && size != "" {
			return []signature{{SignalNameSize, "namesize" + keySep + name + keySep + size}}
		}
	case PolicyFilenameOrHash:
		var sigs []signature
		if hash != "" {
			sigs = append(sigs, signature{SignalHash, "hash" + keySep + hash})
		}
		if name != "" {
			sigs = append(sigs, signature{SignalName, "name" + keySep + name})
		}
		return sigs
	case PolicyHashOnly:
		if hash != "" {
			return []signature{{SignalHash, "hash" + keySep + hash}}
		}
	case PolicyOff:
	}
	return nil
}

// sizeKey is the bucket key for a byte count, or "" for an unknown size.
func sizeKey(size int64) string {
	if size <= 0 {
		return ""
	}
	return strconv.FormatInt(size, 10)
}

// couldBeSameFile applies the structural facts that prove two files differ.
// They are checked after a bucket hit and can overturn it under any policy.
// A size mismatch is not one of them, since hoster-reported sizes are often
// wrong, and a veto on it would turn filename-only into filename-and-size.
func couldBeSameFile(a, b record) bool {
	// Parts of one archive share a base name and usually a size. Merging two
	// loses a volume and with it the whole set.
	if a.name.Base != "" && a.name.Base == b.name.Base && a.name.Volume != b.name.Volume {
		return false
	}
	// Parts numbered in a shape volumeMarkers does not know (.0001, .r100,
	// .tar.gz.01) differ only in one number.
	if numberedSiblings(a.full, b.full) {
		return false
	}
	// Differing digests of the same algorithm prove two different files.
	if a.hash != "" && b.hash != "" && a.hash != b.hash && sameHashKind(a.hash, b.hash) {
		return false
	}
	return true
}

// numberedSiblings reports whether two comparison names differ only in one
// number, such as "film.r99" and "film.r100": members of a set, not copies.
// Leading zeros are ignored so "part1" and "part01" stay mergeable. It
// outranks a matching digest, because a CRC32 from a release name agrees by
// chance often enough, while a wrongly merged part is lost.
func numberedSiblings(a, b string) bool {
	// Unresolved names are unknown and prove nothing.
	if a == "" || b == "" || a == b {
		return false
	}
	headA, numA, tailA, okA := splitLastNumber(a)
	headB, numB, tailB, okB := splitLastNumber(b)
	if !okA || !okB || headA != headB || tailA != tailB {
		return false
	}
	return trimZeros(numA) != trimZeros(numB)
}

// splitLastNumber cuts a name around its rightmost run of digits, where a
// part number sits in every naming scheme, so a resolution or year earlier in
// the name stays in the head.
func splitLastNumber(s string) (head, num, tail string, ok bool) {
	end := -1
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] >= '0' && s[i] <= '9' {
			if end < 0 {
				end = i + 1
			}
			continue
		}
		if end >= 0 {
			return s[:i+1], s[i+1 : end], s[end:], true
		}
	}
	if end < 0 {
		return "", "", "", false
	}
	return "", s[:end], s[end:], true
}

// sameHashKind reports whether two hash keys come from the same algorithm.
func sameHashKind(a, b string) bool {
	ka, _, _ := strings.Cut(a, ":")
	kb, _, _ := strings.Cut(b, ":")
	return ka == kb
}
