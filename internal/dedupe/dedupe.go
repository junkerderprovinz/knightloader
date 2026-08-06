// Package dedupe answers one question about a link that is about to be added:
// is it already in the list? It keeps two answers apart on purpose. A duplicate
// is the same URL a second time, which is cheap to prove and always worth
// refusing. A mirror is a different URL that leads to the same file, which can
// only ever be guessed at from the file name, the byte count and whatever hash
// happens to be known. Guessing too eagerly merges two unrelated files and the
// user never gets the second one; guessing too shyly downloads the same release
// once per hoster it was pasted from. Which signals count is therefore a policy
// the user picks, not something this package decides for them.
package dedupe

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Policy is the combination of signals that makes two different URLs count as
// mirrors of one file. Every value gets something wrong, which is the reason
// there is a choice at all: each one documents what it costs.
type Policy string

const (
	// PolicyOff never merges anything. Duplicate detection is unaffected by it:
	// the same URL twice is a fact rather than a guess, and facts are not
	// configurable.
	PolicyOff Policy = "off"
	// PolicyFilenameOnly merges on the normalised file name alone. It catches
	// the case people actually paste (one release, five hosters) even when the
	// hosters disagree about the size, and it happily merges two unrelated
	// files that are both called setup.exe.
	PolicyFilenameOnly Policy = "filename-only"
	// PolicySizeOnly merges on the exact byte count and ignores names, which is
	// the only thing left when a hoster renames what it stores. It is also the
	// most reckless: files of identical size are common, and every volume of a
	// split archive has the same size by construction. Numbered siblings are
	// held apart by the two structural rules in couldBeSameFile no matter what
	// the policy says, so this stays merely reckless instead of destructive.
	PolicySizeOnly Policy = "size-only"
	// PolicyFilenameAndSize needs both to agree. Two files that share a generic
	// name almost never share a byte count as well, so this is the safe middle
	// and the default. The price is that a link whose size nobody knows yet can
	// never be merged.
	PolicyFilenameAndSize Policy = "filename-and-size"
	// PolicyFilenameOrHash merges when either the name or a known digest
	// matches. It is the widest net: a hash match is proof, a name match is the
	// usual heuristic, and a link that carries neither is left alone.
	PolicyFilenameOrHash Policy = "filename-or-hash"
	// PolicyHashOnly merges only on a matching digest. It never merges anything
	// it should not, and it misses nearly everything, because a hash is rarely
	// known before the file has been downloaded.
	PolicyHashOnly Policy = "hash-only"
)

// DefaultPolicy is what an install that has never chosen gets. Name and size
// together catch the mirrors people really paste without either single-signal
// policy's failure mode.
const DefaultPolicy = PolicyFilenameAndSize

// Policies lists every policy once, in the order an interface should offer them:
// from "never merge" towards "merge only on proof". The slice is built fresh on
// every call so a caller sorting or filtering it cannot reorder the menu for
// everybody else.
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

// ParsePolicy maps a stored settings string onto a policy. Anything it does not
// recognise, the empty string included, becomes DefaultPolicy rather than an
// error: a settings file written by another build must never be able to stop
// links from being added, and a typo that silently disabled mirror detection
// would be discovered months later in the download folder.
func ParsePolicy(s string) Policy {
	if p := Policy(strings.ToLower(strings.TrimSpace(s))); p.Valid() {
		return p
	}
	return DefaultPolicy
}

// Verdict is what a set knows about a candidate.
type Verdict int

const (
	// NotSeen is the zero value on purpose. A Match that nobody filled in has to
	// mean "add this link": losing a link the user pasted is the worse of the
	// two failures, and it must not be reachable by forgetting to set a field.
	NotSeen Verdict = iota
	// Duplicate is the same URL, already present.
	Duplicate
	// Mirror is a different URL that the policy in force says leads to the same
	// file.
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
// link was folded away. A link that disappears without a stated reason is
// indistinguishable from a bug, and gets reported as one.
type Signal string

const (
	SignalURL      Signal = "url"
	SignalName     Signal = "name"
	SignalSize     Signal = "size"
	SignalNameSize Signal = "name+size"
	SignalHash     Signal = "hash"
)

// Hash is a digest already known for a file, from a checksum file that came with
// it or from the CRC tag release names carry.
type Hash struct {
	// Kind names the algorithm ("crc32", "md5", "sha1", "sha256"). Two digests
	// are only ever compared when their kinds match: a digest carries no record
	// of what produced it, so comparing an MD5 with a SHA-256 would be comparing
	// two unrelated numbers and calling the mismatch meaningful.
	Kind string
	// Hex is the digest itself. Case does not matter; .sfv files are
	// traditionally upper-case and coreutils writes lower-case.
	Hex string
}

// key is the bucket key for a hash, empty when the hash is not usable. A half
// filled Hash must produce nothing rather than a key that every other half
// filled Hash also produces.
func (h Hash) key() string {
	kind := strings.ToLower(strings.TrimSpace(h.Kind))
	hex := strings.ToLower(strings.TrimSpace(h.Hex))
	if kind == "" || hex == "" {
		return ""
	}
	return kind + ":" + hex
}

// Entry is one download the set knows about, or one candidate being checked
// against it.
type Entry struct {
	// ID is the caller's handle for the entry, carried through untouched so a
	// Match can point back at the task the user already has.
	ID string
	// URL is what identifies the entry. An entry without one is ignored: a link
	// with no URL is not a download.
	URL string
	// Name is the file name as it will be shown. It may still be the URL when
	// nothing has resolved yet, which this package detects and treats as "not
	// known" rather than as a name to compare.
	Name string
	// Size is the total byte count, 0 when it is not known. An unknown size
	// never matches another unknown size.
	Size int64
	// Hash is a digest already known for the file, if any is.
	Hash Hash
}

// Match is the verdict for one candidate.
type Match struct {
	Verdict Verdict
	// Of is the entry already in the set that the candidate matched; the zero
	// Entry when Verdict is NotSeen.
	Of Entry
	// Signal is what matched; empty when Verdict is NotSeen.
	Signal Signal
}

// Seen reports whether the set already covers the candidate, which is the only
// question most callers have.
func (m Match) Seen() bool { return m.Verdict != NotSeen }

// keySep joins the parts of a composite bucket key. It is a byte that cannot
// occur in a file name or a URL, so "a" plus "b|c" can never collide with "a|b"
// plus "c".
const keySep = "\x00"

// Name is a file name split into the parts that decide identity.
type Name struct {
	// Display is the name exactly as it arrived, untouched, because everything
	// below is a comparison form nobody should ever be shown.
	Display string
	// Base is that comparison form: the name without its directory and without
	// its volume marker, lower-cased, with runs of whitespace collapsed to a
	// single space. It is empty when the name says nothing worth comparing.
	Base string
	// Volume is an opaque token saying which part of a multi-part set the name
	// refers to, empty for a name that carries no marker. Only equality means
	// anything; it is not an ordinal and must not be sorted.
	Volume string
}

// key is the bucket key for a name, empty when there is nothing to compare. The
// volume marker is part of the key: the parts of one archive share a base name,
// and letting them share a bucket is the first half of the mistake that costs a
// user their archive.
func (n Name) key() string {
	if n.Base == "" {
		return ""
	}
	return n.Base + keySep + n.Volume
}

// volumeMarkers matches the trailing part marker of a multi-volume file and maps
// it to a token standing for "which part of the set this is". The number is
// stripped of leading zeros so .part01.rar and .part1.rar, which packers write
// interchangeably for the same volume, come out the same.
//
// These are the marker shapes internal/extract groups a volume set by, and the
// difference is deliberate: extract wants every part of one archive to collapse
// onto a single key so it can tell when the set is complete, and this package
// needs exactly the opposite, because two parts of one archive are precisely the
// two files a mirror check must never merge.
//
// Order matters, most specific first: .part01.rar has to be recognised before
// .rar, and .7z.001 before .7z. A generic .NNN run is accepted only at exactly
// three digits, which is what the splitters write, and which keeps a release
// year ("Film.2024") from being read as volume 2024.
//
// The table is deliberately kept narrow rather than widened until it catches
// every possible numbering, because every widening buys another shape at the
// price of misreading an ordinary name. The shapes it does not know are caught
// instead by numberedSiblings, which needs no table.
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

// Normalize splits a file name into the parts that decide identity, keeping the
// original for display.
//
// A name that is still a URL is treated as unknown rather than as a name. Links
// are staged before anything has resolved them, and every unresolved link would
// otherwise land in one bucket keyed on a URL-shaped string - which under a
// filename policy makes the second link a "mirror" of the first and drops it.
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
	// Trimmed again because cutting the marker off "film .rar" leaves a trailing
	// space, and a base that differs from its twin by an invisible byte is a
	// missed merge nobody can see in the interface.
	n.Base = strings.TrimSpace(base)
	return n
}

// comparableName reduces a name to the form everything in this package compares
// on: no directory, lower case, runs of whitespace collapsed. It is empty when
// the name says nothing worth comparing.
//
// Separators are deliberately left alone. Folding '.', '-' and '_' into spaces
// would merge "The.Movie.2024" with "The Movie 2024", which is usually right and
// occasionally very wrong ("v1.2" against "v1 2"), and the user has no knob to
// turn it off with. The policy chooses which signals to trust, not how hard to
// guess at a name.
func comparableName(name string) string {
	s := strings.TrimSpace(name)
	if s == "" || strings.Contains(s, "://") {
		return ""
	}
	return strings.Join(strings.Fields(strings.ToLower(baseName(s))), " ")
}

// baseName drops any directory part. Names reach us from resolvers, crawlers and
// checksum files, and one source calling a file "sub/film.rar" while another
// calls it "film.rar" is one file. Backslashes count as separators too, because
// those lists are routinely written on Windows.
func baseName(s string) string {
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// trimZeros normalises a captured volume number without parsing it, so a marker
// with more digits than an int can hold still produces a usable token.
func trimZeros(s string) string {
	if out := strings.TrimLeft(s, "0"); out != "" {
		return out
	}
	return "0"
}

// normalizeURL folds the two parts of a URL that are case-insensitive by
// definition - the scheme and the host, per RFC 3986 - and drops a port that is
// the scheme's own default, so the same link copied from two places is
// recognised as the same link.
//
// Nothing else is touched. The fragment in particular stays: several hosters
// carry the file's decryption key there, so a tidy-up that dropped it would
// declare two unrelated downloads identical and throw the key away with the
// link it discarded. A URL that will not parse is compared verbatim rather than
// skipped, because an unparseable link is still a link somebody can paste twice.
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	// A magnet link is identified by its xt parameter, and both spellings of one
	// - hex and base32 - are case-insensitive, as is every other field that could
	// tell two of them apart. Folding the whole URI therefore cannot merge two
	// different torrents, and it stops the same magnet pasted from two sites,
	// one of which upper-cased the infohash, from being fetched twice.
	if u.Scheme == "magnet" {
		return strings.ToLower(raw)
	}
	u.Host = strings.ToLower(u.Host)
	if p := u.Port(); (u.Scheme == "http" && p == "80") || (u.Scheme == "https" && p == "443") {
		u.Host = strings.TrimSuffix(u.Host, ":"+p)
	}
	return u.String()
}

// signature is one bucket a record can be found in, paired with the reason a hit
// in it should be reported as.
type signature struct {
	signal Signal
	key    string
}

// record is an entry plus everything derived from it, so a bucket hit never has
// to re-parse the entry it landed on.
type record struct {
	entry Entry
	name  Name
	// full is the comparison name with its volume marker still attached. Base
	// has the marker cut off, which is what makes two mirrors of one part match
	// - but it also erases the only thing that tells ".r99" from ".r100", so the
	// numbered-sibling check needs the name before that cut.
	full string
	hash string
	sigs []signature
}

// newRecord derives everything the set compares on, once. Add and Check both go
// through it so a candidate is never measured with a different ruler than the
// entries it is being checked against.
func newRecord(e Entry) record {
	return record{
		entry: e,
		name:  Normalize(e.Name),
		full:  comparableName(e.Name),
		hash:  e.Hash.key(),
	}
}

// Set is a list of known downloads that can be asked about a candidate without
// walking it. Build one with New.
//
// A Set is not safe for concurrent use. It is meant to be built from the task
// list under whatever lock already protects that list, and then either thrown
// away with the batch or kept and maintained through Add and Remove.
type Set struct {
	policy  Policy
	byURL   map[string]record
	buckets map[string][]string // signature key -> normalised URLs
}

// New returns an empty set that merges mirrors according to p. An unrecognised
// policy is read as DefaultPolicy for the same reason ParsePolicy does not fail.
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

// Add files an entry the set should recognise from now on.
//
// Adding a URL the set already holds replaces it instead of filing it twice, so
// re-seeding a set from a task list that has changed cannot leave a stale record
// behind pointing at a task the user has since renamed or removed.
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

// Remove forgets a URL, so a set that outlives the batch it was built for can
// follow a deletion instead of reporting a duplicate of something that is no
// longer in the list.
func (s *Set) Remove(rawURL string) { s.remove(normalizeURL(rawURL)) }

func (s *Set) remove(u string) {
	r, ok := s.byURL[u]
	if !ok {
		return
	}
	for _, sig := range r.sigs {
		b := slices.DeleteFunc(s.buckets[sig.key], func(v string) bool { return v == u })
		// An emptied bucket is deleted rather than left as an empty slice: a
		// long-running set that only ever grew its key space would hold onto a
		// key for every download the user has ever removed.
		if len(b) == 0 {
			delete(s.buckets, sig.key)
		} else {
			s.buckets[sig.key] = b
		}
	}
	delete(s.byURL, u)
}

// Check reports what the set already knows about a candidate.
//
// It does not add it. Whether a mirror is dropped, kept as an alternative source
// or shown to the user is the caller's decision, and a query that quietly
// mutated the set could not be asked the same question twice.
func (s *Set) Check(cand Entry) Match {
	u := normalizeURL(cand.URL)
	if u == "" {
		return Match{}
	}
	// The exact URL is checked first and regardless of policy: it is the one
	// answer that needs no guessing, and it is the common case by a wide margin
	// (the same list pasted twice).
	if r, ok := s.byURL[u]; ok {
		return Match{Verdict: Duplicate, Of: r.entry, Signal: SignalURL}
	}
	c := newRecord(cand)
	for _, sig := range s.signatures(c) {
		// Only the entries that already share this exact signature are looked
		// at, so a query costs the size of one collision bucket - one or two for
		// a name or a digest - and never the size of the list. Ten thousand
		// staged links is an ordinary evening, and a scan over them per pasted
		// URL is a hang, not a slowdown.
		for _, other := range s.buckets[sig.key] {
			r := s.byURL[other]
			if couldBeSameFile(c, r) {
				return Match{Verdict: Mirror, Of: r.entry, Signal: sig.signal}
			}
		}
	}
	return Match{}
}

// signatures is the list of buckets a record belongs in under this set's policy,
// in the order Check should try them: strongest evidence first, so a match is
// reported with the best reason it has.
//
// A signal that is not known produces no bucket at all. This is the difference
// between "these two links have the same name" and "neither of these two links
// has a name yet": keying the second case would put every nameless link in one
// bucket and make each new one a mirror of the first.
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
		// Both have to be known. An unknown size does not fall back to the name:
		// the user asked for two signals, and handing them one is how the policy
		// they picked for its caution quietly becomes the reckless one.
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
		// No signature, so nothing can ever collide and nothing is merged.
	}
	return nil
}

// sizeKey is the bucket key for a byte count. Zero and negative sizes mean "not
// known" and get no key, so two links whose size nobody has established yet are
// never mirrors of each other.
func sizeKey(size int64) string {
	if size <= 0 {
		return ""
	}
	return strconv.FormatInt(size, 10)
}

// couldBeSameFile applies the two facts that outrank any policy. Both are
// structural rather than reported: they prove the files are different, whereas a
// matching name or byte count only ever suggests they might be the same. That is
// why they are checked after the bucket hit and are allowed to overturn it.
//
// Size is deliberately not among them. A size is a number a hoster told us and
// is routinely wrong or missing, so vetoing on a size mismatch would silently
// turn filename-only into filename-and-size and make the offered policies a lie.
func couldBeSameFile(a, b record) bool {
	// The parts of a multi-volume archive share a base name and, because that is
	// how splitting works, almost always share an exact byte count as well.
	// Merging two of them means the second is never downloaded, and an archive
	// missing one volume cannot be unpacked at all - the user loses the whole
	// set, not one file. So a differing volume marker on the same base name is a
	// veto under every policy, including the ones that never look at a name.
	if a.name.Base != "" && a.name.Base == b.name.Base && a.name.Volume != b.name.Volume {
		return false
	}
	// The rule above can only fire for the marker shapes volumeMarkers knows, and
	// a set that numbers its parts any other way - "split -a 4" writing .0001, a
	// rar set past .r99, "archive.tar.gz.01" - leaves the two parts with
	// different base names and no veto at all. Under size-only, where the parts
	// collide by construction, that merged part 2 into part 1 and lost the
	// archive. So two names that differ in nothing but one number are held apart
	// as well, whatever produced the number.
	if numberedSiblings(a.full, b.full) {
		return false
	}
	// Two digests of the same algorithm that disagree are proof of two different
	// files, so this overturns a name match under filename-or-hash as well.
	// Digests of different algorithms say nothing about each other and are left
	// out of it.
	if a.hash != "" && b.hash != "" && a.hash != b.hash && sameHashKind(a.hash, b.hash) {
		return false
	}
	return true
}

// numberedSiblings reports whether two comparison names differ in nothing but a
// single number: "film.mkv.0001" against "film.mkv.0002", "film.r99" against
// "film.r100", "setup_v1.2.exe" against "setup_v1.3.exe". Those are consecutive
// members of a set, never two copies of one file.
//
// The numbers are compared with their leading zeros stripped, because "part1"
// and "part01" are two spellings packers use for the same volume, and reading
// them as siblings would refuse the merge this package exists to make.
//
// It deliberately outranks a matching digest, like the volume rule above does.
// The digests this package sees include the CRC32 that release names carry, and
// thirty-two bits is few enough that agreement is a coincidence rather than a
// proof - whereas a wrongly merged archive part is gone.
func numberedSiblings(a, b string) bool {
	// Names nobody has resolved yet are unknown, not equal, and cannot show that
	// two files are different.
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

// splitLastNumber cuts a name around its rightmost run of decimal digits, which
// is where a part number sits in every naming scheme in use: at the very end, or
// just before the extension. Earlier runs are left in the head, so a resolution
// or a year does not shadow the number that actually varies.
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

// sameHashKind reports whether two hash keys were produced by the same
// algorithm, which is the only case in which comparing them means anything.
func sameHashKind(a, b string) bool {
	ka, _, _ := strings.Cut(a, ":")
	kb, _, _ := strings.Cut(b, ":")
	return ka == kb
}
