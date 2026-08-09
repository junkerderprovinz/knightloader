package extract

// The archive policies: where an extraction writes, and what becomes of the
// archive once it has.
//
// A seam of its own, beside the readers and beside the worker rather than
// inside either. A reader answers "what is in this file" from the bytes, and a
// job answers "how far has it got"; everything here answers "and what should
// happen to it", which is a decision a person made on a settings page. So it is
// all explicit, it is reversible wherever it can be, and none of it is ever
// decided on the user's behalf by code that was only asked to unpack something.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
)

// Disposal is what happens to an archive whose extraction succeeded.
//
// Three answers and not a boolean. "Keep" and "delete" were the only two on
// offer for a long time, and the gap between them is where everybody actually
// lives: the archive is forty gigabytes and the extraction looks right, but
// "looks right" is not "is right", and a delete cannot be taken back by
// changing the setting afterwards.
type Disposal string

const (
	// DisposalKeep leaves the archive where it is.
	DisposalKeep Disposal = "keep"
	// DisposalTrash moves it into the trash folder, from where the sweep takes
	// it once it is old enough. See TrashName for what that word can honestly
	// mean here.
	DisposalTrash Disposal = "trash"
	// DisposalDelete removes it at once and for good.
	DisposalDelete Disposal = "delete"
)

// DefaultDisposal is what an unset setting means. Keeping is the only one of
// the three that cannot destroy something the user wanted, and the archive is
// the one copy of bytes they paid for in bandwidth.
const DefaultDisposal = DisposalKeep

// Disposals lists the values in the order a chooser should offer them, so the
// API, the settings page and ParseDisposal cannot drift apart.
func Disposals() []Disposal { return []Disposal{DisposalKeep, DisposalTrash, DisposalDelete} }

// ParseDisposal folds stored or user-supplied text onto a known value.
//
// Anything unrecognised becomes DefaultDisposal rather than an error, and the
// case that matters is not a typo: it is a settings file written by a build
// older than this one, where this key was a boolean and here it is a word.
// settings.Load maps that file on the way in; this is the second net, and it
// errs towards keeping the archive.
func ParseDisposal(s string) Disposal {
	switch d := Disposal(strings.ToLower(strings.TrimSpace(s))); d {
	case DisposalKeep, DisposalTrash, DisposalDelete:
		return d
	}
	return DefaultDisposal
}

// TrashName is the folder a trashed archive is moved into, directly under the
// download folder.
//
// A hidden folder, and not a recycle bin, because there is no recycle bin to
// put anything in. A container has no desktop session, no trash daemon and no
// XDG user directories, and the host's own recycle bin sits on the far side of
// a bind mount where a rename cannot reach it. So "trash" here means exactly
// one thing, a rename into this folder plus the age-based sweep below, and the
// help text beside the setting has to say so in as many words. A recycle bin
// that is really a hidden folder is a promise broken quietly, which is the
// worst way to break one: nobody finds out until they go looking for the file.
const TrashName = ".knightloader-trash"

// DefaultTrashDays is how long a trashed archive survives. Two weeks is longer
// than it takes to notice that an extraction produced the wrong thing, and
// shorter than it takes for a disk to fill with archives nobody chose to keep.
const DefaultTrashDays = 14

// DefaultCollision is what an extraction does when its destination folder is
// already there, absent a setting.
//
// Overwrite, deliberately, even though collide calls it the one policy that can
// lose data. It is what extraction has always done here - every file is opened
// O_TRUNC - so it is the only value that leaves an upgraded install behaving as
// it did yesterday. It is also the only one that survives the ordinary reason
// an extraction is run a second time, a password supplied on the second
// attempt, without leaving a second copy of a large folder behind.
const DefaultCollision = collide.Overwrite

// ParseCollision folds a stored policy onto one an extraction can honour.
//
// collide.Ask is the one that has to go. It parks a task until a human answers,
// and an extraction has no way to raise the question and no state to sit in
// while it waits, so honouring it would mean an unpack that never finishes and
// never says why.
func ParseCollision(s string) collide.Policy {
	switch p := collide.Policy(strings.ToLower(strings.TrimSpace(s))); p {
	case collide.Rename, collide.Skip, collide.Overwrite:
		return p
	}
	return DefaultCollision
}

// Collisions lists the policies an extraction can honour, in the order a
// chooser should offer them.
//
// It is collide's own list filtered through ParseCollision, never a second list
// written out here. A policy that package gains appears in the menu only once
// this one has learnt to keep it, and a policy it drops leaves the menu by
// itself. That is the direction that fails safe: a hard-coded copy fails the
// other way round, by offering a word the extractor quietly does something else
// about.
func Collisions() []collide.Policy {
	all := collide.Policies()
	out := make([]collide.Policy, 0, len(all))
	for _, p := range all {
		if ParseCollision(string(p)) == p {
			out = append(out, p)
		}
	}
	return out
}

// infoSuffixes are the files a release carries beside its archive: the
// description, the checksum list, the advert. Nothing else is ever swept, and
// even these are only looked for among files the caller has already narrowed to
// one package. See Options.InfoFilesIn.
var infoSuffixes = []string{".nfo", ".sfv", ".diz", ".url"}

// Options is the archive half of the settings, plus the two facts about the
// download that only the caller knows.
type Options struct {
	// Passwords are tried in order when the archive turns out to be encrypted.
	Passwords []string

	// Dest collects extractions in one folder instead of leaving each beside
	// its archive. Empty means beside the archive, which is what this package
	// has always done. It is an absolute path with any template already
	// expanded: this package never sees a task, so it could not expand one.
	Dest string
	// Package is the package the archive belongs to, for Subfolder.
	Package string
	// Subfolder puts each package in its own folder below Dest.
	//
	// It does nothing without Dest, and that is not an oversight: beside the
	// archive, the download folder already is the package folder whenever
	// subfolder-by-package is on for downloads, and a second one inside it
	// would give "Films/Film/Film".
	Subfolder bool

	// Collision is what to do when the destination folder already exists. It is
	// decided per folder and never per file inside the archive - see
	// destination.
	Collision collide.Policy

	// Disposal is what happens to the archive volumes afterwards.
	Disposal Disposal
	// TrashRoot is the folder DisposalTrash creates its TrashName folder in,
	// normally the download folder. Empty falls back to the archive's own
	// folder, so trash degrades to a different folder and never to a delete.
	TrashRoot string
	// TrashMaxAge is how long a trashed file survives a sweep. Zero never
	// sweeps, which is a real answer for somebody who tidies up by hand.
	TrashMaxAge time.Duration

	// InfoFiles sweeps the .nfo/.sfv/.diz/.url that came with the same package.
	InfoFiles bool
}

// ErrDestinationTaken says an extraction did not run because its destination
// was already there and the policy is to skip. Nothing is wrong with the
// archive and nothing was written, so a caller reporting failures should put
// this one in different words from a broken file.
var ErrDestinationTaken = errors.New("extract: the destination already exists and the collision policy is to skip")

// Extract unpacks path under these options and reports what it wrote.
//
// It disposes of nothing. Whether a volume is still somebody else's file is a
// question about the download list, which this package cannot see and must not
// guess at; the caller narrows the list and calls Dispose.
func (o Options) Extract(path string) (*Result, error) {
	dest, err := o.destination(path)
	if err != nil {
		return nil, err
	}
	return extractInto(path, dest, o.Passwords)
}

// extractInto is the password walk every entry point shares: the unencrypted
// attempt first, so an ordinary archive never pays for the list, then each
// password in turn.
//
// dest is NOT created here. A single gzipped file unpacks beside the archive
// rather than into a folder named after it, and creating that folder up front
// would both leave an empty directory behind and fail outright when a file of
// that name already sits there - which for "dump.sql.gz" next to an existing
// "dump.sql" is the normal case.
func extractInto(path, dest string, passwords []string) (*Result, error) {
	res, err := extractOnce(path, dest, "")
	if err == nil || !errors.Is(err, ErrPasswordRequired) {
		return res, err
	}
	for _, pw := range passwords {
		if pw == "" {
			continue
		}
		res, err = extractOnce(path, dest, pw)
		if err == nil || !errors.Is(err, ErrPasswordRequired) {
			return res, err
		}
	}
	return nil, ErrPasswordRequired
}

// destination is the folder this extraction writes into, with the collision
// policy already applied to it.
//
// The policy is applied to the FOLDER and never to the individual files inside
// the archive, which is a deliberate narrowing of what a desktop unpacker
// offers. Its default there is "ask for each file", and a headless server has
// nobody to ask: a queue that stops on a dialog no one will ever see is worse
// than any of the three answers below. At folder granularity the same three
// words still mean something a person can predict:
//
//	rename     unpack into "Film (2)", so nothing already there is touched
//	skip       leave the folder alone and unpack nothing
//	overwrite  unpack into the folder that is there, so the files the archive
//	           holds are replaced and every other file in it is left alone
//
// Overwrite never reaches collide, which refuses it on a folder outright
// (ErrFolderOverwrite) because there the word would mean deleting a tree of
// unknown size. Here it means very nearly the opposite, so it is handled by not
// asking.
//
// One case escapes the policy: a single compressed stream that turns out not to
// hold a tar is written beside the archive under the payload's own name, and
// that name is settled inside the reader from the decompressed bytes. Nothing
// out here can know it in advance, so such a file still replaces a namesake of
// its own name. Closing that gap means moving the decision into the writer, and
// the help text is worded for the folder so it does not claim otherwise.
func (o Options) destination(path string) (string, error) {
	dest := o.baseDest(path)
	policy := ParseCollision(string(o.Collision))
	if policy == collide.Overwrite {
		return dest, nil
	}
	// Handover rather than a plain existence check: it decides the name against
	// a folder nobody else can slip a directory into meanwhile, and then gives
	// the claim up so the extractor's own MkdirAll can have it. The window that
	// leaves is microseconds against a decision that would otherwise rest on a
	// stat taken well before the first byte is written.
	r, err := collide.HandoverFolder(dest, policy)
	if err != nil {
		return "", err
	}
	if r.Action == collide.Skipped {
		return "", fmt.Errorf("%w: %s", ErrDestinationTaken, dest)
	}
	return r.Path, nil
}

// baseDest is where the archive unpacks before the collision policy has looked
// at it: beside the archive, or under the collect folder.
//
// The last element goes through collide.SafeName even when no policy is
// applied, because collide runs every target it is given through it. Without
// that, the folder an overwrite writes into and the folder a rename decides
// could be two spellings of one archive's name, and changing the setting would
// quietly start a second folder.
func (o Options) baseDest(path string) string {
	dest := destDir(path)
	if root := strings.TrimSpace(o.Dest); root != "" {
		if o.Subfolder {
			if pkg := collide.SafeName(strings.TrimSpace(o.Package)); pkg != "" {
				root = filepath.Join(root, pkg)
			}
		}
		dest = filepath.Join(root, filepath.Base(dest))
	}
	return filepath.Join(filepath.Dir(dest), collide.SafeName(filepath.Base(dest)))
}

// Dispose applies the disposal to files the caller has already decided may go.
//
// A file that is no longer there is not an error: two volumes of one set can
// resolve to the same path, and a user removing the archive by hand between the
// extraction and this call is an ordinary thing to do.
func (o Options) Dispose(paths []string) error {
	disposal := ParseDisposal(string(o.Disposal))
	if disposal == DisposalKeep || len(paths) == 0 {
		return nil
	}
	var errs []error
	for _, p := range paths {
		var err error
		if disposal == DisposalTrash {
			err = o.trash(p)
		} else {
			err = os.Remove(p)
		}
		if err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// trash moves one file into the trash folder and stamps it with the moment it
// got there.
func (o Options) trash(path string) error {
	dir, err := o.trashDir(path)
	if err != nil {
		return err
	}
	// Two releases can carry an archive of the same name, and the second one
	// must not silently replace the first one's copy in the one folder that
	// exists to be able to give it back. Handover decides a free name and steps
	// aside for the rename, which is a writer that insists on creating the file
	// itself in the only sense that matters here.
	r, err := collide.Handover(filepath.Join(dir, filepath.Base(path)), collide.Rename)
	if err != nil {
		return err
	}
	if err := os.Rename(path, r.Path); err != nil {
		return err
	}
	// The modification time is when the download finished, which can be days or
	// years before the file was trashed - a re-added file keeps whatever the
	// hoster sent. The sweep ages files from when they arrived HERE, so the
	// stamp is set now: without it an archive trashed today can be swept in the
	// same minute.
	now := time.Now()
	_ = os.Chtimes(r.Path, now, now)
	return nil
}

// trashDir is the folder to move a file into, created if it is not there yet.
//
// TrashRoot is normally the download folder, so there is one trash to look in
// rather than one per release. It is not always reachable by a rename, though:
// a container with several bind mounts puts the archive and the download folder
// on different filesystems often enough, and os.Rename cannot cross that. The
// fallback is a trash folder beside the archive - a different folder, still a
// trash. What it deliberately never falls back to is deleting the file, which
// would turn the one policy that promises to be reversible into the one that
// cannot be.
func (o Options) trashDir(path string) (string, error) {
	beside := filepath.Join(filepath.Dir(path), TrashName)
	root := strings.TrimSpace(o.TrashRoot)
	if root == "" {
		return beside, os.MkdirAll(beside, 0o755)
	}
	dir := filepath.Join(root, TrashName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return beside, os.MkdirAll(beside, 0o755)
	}
	if !renameReaches(path, dir) {
		return beside, os.MkdirAll(beside, 0o755)
	}
	return dir, nil
}

// renameReaches reports whether a file sitting beside `file` can be renamed
// into dir, by trying it with a probe of its own.
//
// Tried rather than worked out from device numbers, because os.Rename is the
// operation that has to succeed and Windows has no portable device number to
// compare in the first place. The probe is created next to the file and not in
// the folder, so the direction under test is the one that matters.
func renameReaches(file, dir string) bool {
	probe := filepath.Join(filepath.Dir(file), TrashName+".probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		// The probe name is taken, or the folder will not take a new file.
		// Neither answers the question that was asked, so let the real rename be
		// the test and take its fallback if it fails.
		return true
	}
	_ = f.Close()
	moved := filepath.Join(dir, filepath.Base(probe))
	if err := os.Rename(probe, moved); err != nil {
		_ = os.Remove(probe)
		return false
	}
	_ = os.Remove(moved)
	return true
}

// SweepTrash removes everything in root's trash folder that has been there
// longer than maxAge, and reports how many entries went.
//
// A zero maxAge never sweeps, which is what "keep it until I say so" has to
// mean. A root with no trash folder is not an error either: there is nothing to
// sweep, and reporting that as a failure would put a red mark on every
// extraction of an install that has never trashed anything.
func SweepTrash(root string, maxAge time.Duration) (int, error) {
	if strings.TrimSpace(root) == "" || maxAge <= 0 {
		return 0, nil
	}
	dir := filepath.Join(root, TrashName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	var errs []error
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue // it went away under us, which is what was wanted anyway
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		// RemoveAll, but only ever one level inside a folder this package made
		// and only ever renames whole files into. An archive that was itself a
		// folder cannot get here; a directory left behind by an interrupted move
		// can, and leaving it would hold its name forever.
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

// InfoFilesIn narrows one package's own files down to the info files among
// them: the .nfo, the .sfv, the .diz, the .url advert.
//
// The caller passes the files of ONE package, and that is the entire safety
// property of this sweep. The obvious implementation - list the folder and
// delete every .nfo in it - is a data-loss bug on the default layout, because
// subfolder-by-package is off out of the box and one folder therefore holds
// several releases. A neighbour's .nfo is not ours to remove, and nobody would
// ever connect its disappearance with having unpacked something else.
//
// It also answers nothing at all while the disposal is Keep. The sweep has no
// disposal of its own: whether a swept file is trashed or deleted follows what
// was chosen for the archive, so "keep everything" cannot coherently mean "keep
// the archive and destroy the notes beside it".
func (o Options) InfoFilesIn(paths []string) []string {
	if !o.InfoFiles || ParseDisposal(string(o.Disposal)) == DisposalKeep {
		return nil
	}
	var out []string
	for _, p := range paths {
		if slices.Contains(infoSuffixes, strings.ToLower(filepath.Ext(p))) {
			out = append(out, p)
		}
	}
	return out
}

// Formats lists the archive extensions this build actually opens, one spelling
// per format, for the capability line the settings page shows.
//
// Derived, never written down. A list typed into the interface drifts from the
// readers the moment one is added or retired, and the drift is invisible until
// somebody's download is refused by a page that promised to handle it. Every
// candidate here comes out of the extractor's own suffix table and is then put
// through Supported, the same gate that decides whether a finished download is
// offered to the unpacker at all, so an entry can only appear if a real file of
// that name would really be taken.
//
// The table is longest-suffix-first, so that ".tar.gz" beats ".gz" when a name
// is being shortened. That is the wrong end for a capability line, where ".gz"
// is the spelling that names the format, so the walk runs backwards and takes
// the shortest spelling of each; the result is turned round again to read in
// table order.
func Formats() []string {
	seen := make(map[archiveFormat]bool, len(archiveSuffixes))
	out := make([]string, 0, len(archiveSuffixes))
	for i := len(archiveSuffixes) - 1; i >= 0; i-- {
		suffix := archiveSuffixes[i]
		f := formatFromName(suffix)
		if f == formatUnknown || seen[f] || !Supported("archive"+suffix) {
			continue
		}
		seen[f] = true
		out = append(out, suffix)
	}
	slices.Reverse(out)
	return out
}
