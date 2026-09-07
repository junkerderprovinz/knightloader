// Package reclaim answers one question, before a single byte is fetched: is
// what this task would download already lying in the folder it would download
// it into.
//
// It exists because of what a move, a restored backup or an emptied list costs
// today. The links go back in, nothing on this side ever watched those files
// arrive, and the box spends a week of line time fetching what is on the disk
// beside it.
//
// IT DECIDES AND IT NEVER ACTS. Nothing here writes, moves or removes a file,
// and nothing here settles a task: the caller does both. The split is not
// tidiness, it is the risk profile. The cheapest wrong answer this package can
// give is one download too many; the most expensive one anybody could build on
// top of it is a deleted file, so the deleting is kept somewhere a person has
// to press it.
//
// The verdicts are deliberately more than "yes" and "no". "The right length,
// and nothing to check it against" is a real answer and rounding it to either
// of the other two is how a feature like this either does nothing at all or
// quietly keeps rubbish.
package reclaim

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
)

// PartSuffix marks a transfer that is still arriving.
//
// It is internal/resolver/remotefs's own partSuffix, spelled again here
// because that constant is unexported and that package is the one WRITING
// these files while this one only reads them. The two must stay equal: if
// remotefs ever renames its scratch suffix, a part file stops being
// recognised here and starts being reported as an orphan of a task that is
// running right now.
const PartSuffix = ".klpart"

// Trust is how much this app is allowed to believe about a file it did not
// watch arrive, and it is the whole of the answer to "what counts as
// matching".
//
// THE CHEAP TEST AND THE HONEST TEST DO DIFFERENT JOBS, so they are not two
// settings of one dial:
//
//   - Size is free and it is used on every candidate no matter what is
//     selected here, but only ever to DISQUALIFY or to measure. A file shorter
//     than the expected length is not the download, it is the start of one, and
//     its length is an honest offset. That costs one stat.
//   - A checksum is the only thing that can CONFIRM, and it is paid for exactly
//     once per candidate, only on a file the cheap test has already narrowed to
//     the right length. Hashing forty gigabytes to save a forty gigabyte
//     download always pays; hashing them to discover the file is four gigabytes
//     short never does, which is why the order is fixed and not configurable.
//
// The reason size is not allowed to confirm on its own by default is not
// theoretical, it is this build. The embedded engine's download library
// creates its destination file at the FULL final size before the first byte
// arrives (gopeed v1.9.3, internal/controller's Touch: os.Create followed by
// os.Truncate(name, size)). So an engine download that died at two per cent
// leaves a file at the final name with exactly the expected byte count, and
// "name plus size" cannot tell it apart from the finished article. On this
// build, right length and wrong content is not the exotic case, it is what
// every interrupted download looks like.
type Trust string

const (
	// TrustChecksum settles a task only on a checksum that was actually
	// verified. Everything else is reported and left alone. It is the honest
	// answer and it is also the one that does nothing for the majority of
	// downloads, because most files arrive with no published hash at all.
	TrustChecksum Trust = "checksum"
	// TrustRecord adds this instance's OWN record of what it finished: the
	// download history, which survives the list being cleared, trimmed or
	// emptied precisely so a question like this one can still be answered. A
	// history row is not proof about the bytes, it is a note this app wrote
	// itself at the moment the last byte landed, which is a great deal more
	// than a coincidence of length.
	TrustRecord Trust = "record"
	// TrustSize adds bare name plus length, for somebody who knows what is in
	// their folder and is prepared to say so. See the type comment for the
	// preallocation trap this opts into.
	TrustSize Trust = "size"
)

// DefaultTrust is the record tier.
//
// TrustChecksum would be the timid answer and it would make the feature a
// no-op for anyone whose hosters do not publish hashes, which is most of them.
// TrustSize would be the eager one and on this build it is wrong often enough
// to matter. The history is the tier that actually fits the three situations
// this package was built for: a moved box, a restored backup and an emptied
// list all carry the database, and the database is where the record of what
// was finished lives.
const DefaultTrust = TrustRecord

// Modes lists the tiers in the order an interface should offer them, strictest
// first. Built fresh per call so a caller cannot reorder the menu for
// everybody else.
func Modes() []Trust { return []Trust{TrustChecksum, TrustRecord, TrustSize} }

// ParseTrust reads a stored value as one of the tiers.
//
// An unrecognised value folds onto DefaultTrust rather than onto the strictest
// tier, which is the opposite of what settings.ParseResumeOnStart does with a
// value it does not know, and the difference is deliberate. That one decides
// whether downloads start by themselves after a reboot, so an unknown value
// must never be read as "start". Nothing here starts, deletes or overwrites
// anything on its own: the pass only runs when somebody asks for it, and the
// cost of guessing wrong is one download too many or one row that has to be
// restarted by hand.
func ParseTrust(s string) Trust {
	switch t := Trust(strings.ToLower(strings.TrimSpace(s))); t {
	case TrustChecksum, TrustRecord, TrustSize:
		return t
	}
	return DefaultTrust
}

// Verdict is what one candidate turned out to be.
type Verdict string

const (
	// Absent is nothing at that name and no part file either. The ordinary
	// answer, and the one that means "download it, the way you were going to".
	Absent Verdict = "absent"
	// Partial is a beginning: a part file, or a file shorter than the expected
	// length. Whether it can actually be continued is the backend's business
	// and not this package's, but the number is real either way and a task
	// carrying a true byte count is a task whose progress bar is not lying.
	Partial Verdict = "partial"
	// Complete is the file, and the reason to believe so is in Basis.
	Complete Verdict = "complete"
	// Mismatch is the right length with the wrong bytes, and it is the one
	// verdict this package can be certain about in the negative: a checksum
	// exists and it disagrees. Download it again. Better one transfer nobody
	// needed than a row that says "done" over a file that will not open.
	Mismatch Verdict = "mismatch"
	// Unproven is a full-length file with nothing to check it against, or
	// anything else this pass cannot honestly call. It is not a failure and it
	// is not a match; it is the answer being reported instead of guessed.
	Unproven Verdict = "unproven"
	// Recheck is a torrent. See scanTorrent for why a torrent gets a verdict of
	// its own instead of being measured like a file.
	Recheck Verdict = "recheck"
)

// Basis is what a Complete rests on, so a report can say which tier answered
// and a person can disagree with it.
type Basis string

const (
	BasisNone     Basis = ""
	BasisChecksum Basis = "checksum"
	BasisRecord   Basis = "record"
	BasisSize     Basis = "size"
)

// Request is one task as the scan takes it. It is deliberately not a
// core.Task: this package decides about a file, and everything about a task
// that is not the file's name, its expected length and where it goes would
// only be another thing to keep in step.
type Request struct {
	TaskID string
	// Dir is the folder this task downloads into, already expanded.
	Dir string
	// Name is the file name the task expects, without any path in it.
	Name string
	// Size is the expected total, or 0 when nobody has resolved it yet. Zero
	// is a real answer and not an error: with no expected length, size can
	// neither disqualify nor measure, and only a checksum can still speak.
	Size int64
	// ExpectedHash is a checksum that came with the link, in either
	// "sha256:<hex>" or bare "<hex>" form. See ParseHash.
	ExpectedHash string
	// Torrent marks a task whose bytes are a swarm's business rather than a
	// file's. See scanTorrent.
	Torrent bool
}

// Finding is one answer.
type Finding struct {
	TaskID  string  `json:"taskId"`
	Path    string  `json:"path"`
	Verdict Verdict `json:"verdict"`
	Basis   Basis   `json:"basis,omitempty"`
	// Bytes is what is on the disk right now at Path, or in the part file for
	// a Partial. Zero for Absent.
	Bytes int64 `json:"bytes"`
	// Detail is the sentence to put in front of a person. Every verdict
	// carries one, including the boring ones, because a report that explains
	// only its exceptions is a report nobody believes about the rest.
	Detail string `json:"detail"`
}

// Options is the policy and the two things the scan cannot work out for
// itself.
type Options struct {
	// Trust is the tier. An empty value is DefaultTrust.
	Trust Trust
	// Sum finds a checksum published beside the file: a .sfv or a sums file
	// that came down with the same batch. Nil means there is no such lookup,
	// which is not an error, only one fewer way to be certain.
	Sum func(dir, name string) (checksum.Sum, bool)
	// Finished reports whether this instance's own history says it already
	// fetched a file of exactly this name and this length. Nil disables the
	// record tier entirely, which is what a caller with no history to consult
	// should pass rather than a function that always says no.
	Finished func(name string, size int64) bool
}

// Scan answers one request. It is the whole of this package's decision.
//
// The order below is the design and not an accident of writing:
//
//  1. A torrent leaves immediately, because none of what follows applies to
//     one.
//  2. The part file, before anything else, because it is the only evidence
//     here that cannot be a coincidence: this app writes one while a transfer
//     is running and removes it when the transfer finishes, so its presence is
//     a statement by this app about this exact destination. Whatever sits at
//     the final name beside it is an older file, and the honest answer is the
//     beginning that was left, even in the awkward case where the older file
//     would have verified. That costs at most one download; the other reading
//     costs somebody the bytes they had.
//  3. Length, which disqualifies and measures but never confirms.
//  4. A checksum, which is the only thing that confirms, and the only thing
//     that can refuse.
//  5. The trust tiers, for the full-length file no checksum can speak for.
func (o Options) Scan(r Request) Finding {
	if r.Torrent {
		return o.scanTorrent(r)
	}
	path, err := safePath(r.Dir, r.Name)
	if err != nil {
		return Finding{TaskID: r.TaskID, Verdict: Unproven, Detail: err.Error()}
	}
	f := Finding{TaskID: r.TaskID, Path: path}

	if n, ok := sizeOf(path + PartSuffix); ok && n > 0 {
		f.Verdict, f.Bytes = Partial, n
		f.Detail = fmt.Sprintf("%d bytes of this download are already in %s%s", n, r.Name, PartSuffix)
		return f
	}

	fi, statErr := os.Stat(path)
	switch {
	case statErr != nil:
		f.Verdict = Absent
		f.Detail = "nothing of this download is in the folder yet"
		return f
	case fi.IsDir():
		f.Verdict = Unproven
		f.Detail = "a folder sits where this download's file would go"
		return f
	}
	f.Bytes = fi.Size()
	if f.Bytes == 0 {
		// A zero-length file is what an interrupted create leaves behind, and
		// on this build it is also what a preallocation that failed leaves. It
		// carries none of the download either way.
		f.Verdict = Absent
		f.Detail = "the file at this name is empty"
		return f
	}
	if r.Size > 0 && f.Bytes < r.Size {
		f.Verdict = Partial
		f.Detail = fmt.Sprintf("%d of %d bytes are already there", f.Bytes, r.Size)
		return f
	}
	if r.Size > 0 && f.Bytes > r.Size {
		// Longer than the download is supposed to be. Nothing sensible can be
		// concluded: it is somebody else's file, or the expected size is
		// wrong, and both readings are worth reporting rather than acting on.
		f.Verdict = Unproven
		f.Detail = fmt.Sprintf("the file at this name is %d bytes, longer than the %d this download expects", f.Bytes, r.Size)
		return f
	}

	if sum, ok := o.sumFor(r); ok {
		good, err := checksum.Verify(path, sum)
		switch {
		case err != nil:
			f.Verdict = Unproven
			f.Detail = fmt.Sprintf("the file at this name could not be read to check it: %v", err)
		case good:
			f.Verdict, f.Basis = Complete, BasisChecksum
			f.Detail = fmt.Sprintf("the file that is there matches its published %s checksum", sum.Kind)
		default:
			f.Verdict = Mismatch
			f.Detail = fmt.Sprintf("the file at this name is the right length but does not match its published %s checksum, so it is not this download", sum.Kind)
		}
		return f
	}

	trust := o.Trust
	if trust == "" {
		trust = DefaultTrust
	}
	if r.Size <= 0 {
		// No expected length and no checksum leaves nothing to compare at all.
		// The file being there is not evidence of anything.
		f.Verdict = Unproven
		f.Detail = "nothing has resolved how big this download should be, and no checksum came with it"
		return f
	}
	if trust != TrustChecksum && o.Finished != nil && o.Finished(r.Name, r.Size) {
		f.Verdict, f.Basis = Complete, BasisRecord
		f.Detail = fmt.Sprintf("this instance's own history says it finished %s at %d bytes, and that file is in the folder", r.Name, r.Size)
		return f
	}
	if trust == TrustSize {
		f.Verdict, f.Basis = Complete, BasisSize
		f.Detail = fmt.Sprintf("a file of this name and exactly %d bytes is in the folder, and this instance is set to take name and size as enough", r.Size)
		return f
	}
	f.Verdict = Unproven
	f.Detail = fmt.Sprintf("a file of this name and exactly %d bytes is in the folder, but nothing here can prove it is this download", r.Size)
	return f
}

// scanTorrent is the answer for a torrent, and it is a verdict of its own
// rather than a measurement, because measuring is the wrong operation.
//
// A torrent's files are already the right length from the first second: the
// client lays out the whole file set before it fetches a piece of it, so
// "name and size match" is true of a torrent that has downloaded nothing.
// What a torrent means by "is this already here" is a piece hash pass, and
// the only thing that can run one is the library holding the torrent.
//
// That pass is not something this package has to trigger. Starting a torrent
// against the folder its files are in IS the recheck: the client reads its
// piece completion record and, for every piece that record calls complete,
// re-checks the constituent files' lengths before it believes it (anacrolix,
// storage/file-piece.go's checkCompleteFileSizes).
//
// What that cannot survive is losing the record, and that is worth writing
// down because it is the case this whole package was built for. gopeed v1.9.3
// never sets torrent.ClientConfig.DataDir, so the completion record is a bolt
// file called ".torrent.bolt.db" in the PROCESS WORKING DIRECTORY rather than
// anywhere near the data or the app's own data directory. Carry the downloads
// folder to a new box without it and every piece reads as missing. The cure is
// anacrolix's Torrent.VerifyData, and it is not reachable: gopeed keeps the
// torrent handle on an unexported field of an internal package. So this
// reports the torrent and says what it cannot answer, instead of guessing from
// file lengths that would say yes to an empty download.
func (o Options) scanTorrent(r Request) Finding {
	return Finding{
		TaskID:  r.TaskID,
		Path:    filepath.Join(r.Dir, r.Name),
		Verdict: Recheck,
		Detail:  "a torrent is checked piece by piece by the download library when it is started against its own folder; nothing here can prove it from the file lengths",
	}
}

// sumFor finds the best checksum available for this candidate, cheapest and
// most specific first: the one the link itself carried, then the one the
// release name carries, then a sums file that came down with the batch, which
// is the only one of the three that costs a directory read and a parse.
func (o Options) sumFor(r Request) (checksum.Sum, bool) {
	if s, ok := ParseHash(r.Name, r.ExpectedHash); ok {
		return s, true
	}
	if s, ok := checksum.FromName(r.Name); ok {
		return s, true
	}
	if o.Sum != nil {
		return o.Sum(r.Dir, r.Name)
	}
	return checksum.Sum{}, false
}

// hashKinds maps the length of a bare hex digest to the hash that produced it.
// internal/checksum keeps the identical four lengths in its own unexported
// table. This is a second copy on purpose rather than a widening of that
// package: the lengths are a fact about MD5, SHA-1, SHA-256 and CRC32, not
// about either package, and none of the four is going to grow a fifth one.
var hashKinds = map[int]checksum.Kind{
	8:  checksum.CRC32,
	32: checksum.MD5,
	40: checksum.SHA1,
	64: checksum.SHA256,
}

// ParseHash reads a checksum that came with a link, in either "sha256:<hex>"
// or bare "<hex>" form.
//
// A label that disagrees with its own digest length is refused rather than
// resolved in favour of one half. Both halves were typed by the same person or
// produced by the same script, so one of them being wrong says the value
// cannot be trusted at all, and checking a download against a hash nobody
// actually wrote is worse than checking it against nothing.
func ParseHash(name, raw string) (checksum.Sum, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return checksum.Sum{}, false
	}
	var kind checksum.Kind
	if i := strings.IndexByte(s, ':'); i >= 0 {
		kind = checksum.Kind(strings.ToLower(strings.TrimSpace(s[:i])))
		s = strings.TrimSpace(s[i+1:])
	}
	s = strings.ToLower(s)
	if !isHex(s) {
		return checksum.Sum{}, false
	}
	byLength, known := hashKinds[len(s)]
	if !known {
		return checksum.Sum{}, false
	}
	if kind != "" && kind != byLength {
		return checksum.Sum{}, false
	}
	return checksum.Sum{Name: name, Kind: byLength, Hex: s}, true
}

// Orphan is a part file that no task in the list claims.
type Orphan struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// Orphans lists the part files in dir that belong to no task, given the set of
// part-file paths the live list accounts for.
//
// IT REPORTS AND IT DELETES NOTHING, and that is the whole answer to "what
// about half a part file with no task behind it". An orphan is evidence: it is
// what is left of a download whose row was cleared, and it is thirty
// gigabytes somebody either wants back or wants gone, with nothing here able
// to tell which. Removing a row and deleting what was downloaded have been two
// different actions in this app since the day conflating them cost somebody
// their finished downloads, and an unattended sweep of somebody's download
// folder is that same mistake with a wider blast radius. So the pass counts
// them, names them and hands the list over.
//
// One folder, not a walk. The caller knows every folder its tasks download
// into and passes them one at a time; walking from the download root would
// descend into whatever else the user keeps under it, which is not this app's
// to inventory.
func Orphans(dir string, claimed map[string]bool) ([]Orphan, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Orphan
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), PartSuffix) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if claimed[p] {
			continue
		}
		n, ok := sizeOf(p)
		if !ok {
			// Gone between the read and the stat, which for a part file is the
			// ordinary case: a transfer finished and renamed it while this was
			// running.
			continue
		}
		out = append(out, Orphan{Path: p, Bytes: n})
	}
	return out, nil
}

// PartPath is where the part file for one download sits. It is exported so a
// caller building the claimed set for Orphans spells it exactly the way
// Orphans reads it, rather than joining the suffix on itself in a second
// place.
func PartPath(dir, name string) string { return filepath.Join(dir, name+PartSuffix) }

func sizeOf(path string) (int64, bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return 0, false
	}
	return fi.Size(), true
}

// safePath joins name under dir and refuses anything that is not a plain file
// name in it.
//
// A task's Name arrives from a backend, which got it from a hoster, which got
// it from whoever uploaded the file. This package opens and hashes whatever it
// resolves to, so the same class of bug as zip-slip applies: a name of
// "../../etc/shadow" must not turn into a report about a file outside the
// download folder. Backslashes count as separators on every platform, because
// these names are routinely written on Windows and "..\..\x" would otherwise
// sail through as an ordinary file name on Linux.
func safePath(dir, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("reclaim: this task has no file name yet")
	}
	p := filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(name, `\`, "/")))
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("reclaim: %q does not name a file inside the download folder", name)
	}
	return p, nil
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
