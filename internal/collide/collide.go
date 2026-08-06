// Package collide decides what happens when a download would land on a name
// that is already taken. JDownloader puts that question in a dialog; a
// self-hosted server has nobody sitting in front of it, so the answer has to be
// a policy chosen in advance.
//
// The one thing this package exists to get right is the race. Two downloads
// finishing in the same instant must not both look at the folder, both find
// "name (2).txt" free, and both write it. Reserve therefore never reports a
// name it has not already claimed: it creates the file with O_CREATE|O_EXCL and
// hands back the open handle, so the name cannot be taken between the decision
// and the first byte written.
package collide

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Policy is what to do about a name that is already taken.
type Policy string

const (
	// Overwrite truncates whatever is there. It is the only policy that can
	// lose data the user already had, so it is never the fallback for input we
	// did not understand.
	Overwrite Policy = "overwrite"
	// Skip keeps the existing file and writes nothing.
	Skip Policy = "skip"
	// Rename writes alongside it as "name (2).txt", "name (3).txt" and so on.
	Rename Policy = "rename"
	// Ask stops the task so a human can decide. On a headless server there is no
	// dialog to raise, so "ask" means the task stalls until somebody answers -
	// which is exactly why it is not the default.
	Ask Policy = "ask"
)

// DefaultPolicy is what an unset or unreadable setting means. Rename is the
// only policy that neither destroys a file the user already has nor stalls a
// queue nobody is watching, which is what an unattended server needs.
const DefaultPolicy = Rename

// DefaultMaxAttempts caps the counter. A folder holding a thousand copies of
// one name is a bug further upstream (a watch folder re-adding the same list),
// and a counter that just keeps climbing turns that bug into a directory nobody
// can list and a loop that never ends.
const DefaultMaxAttempts = 1000

// maxBaseName is the longest single file name ext4, APFS and NTFS all accept.
// Appending " (12)" to a name that is already at the limit fails with a plain
// "file name too long", which reads like a broken download rather than what it
// is, so generated candidates are clipped to fit instead.
const maxBaseName = 255

// Policies lists the policies in the order a settings dropdown should offer
// them, so the API and the UI cannot drift from what Reserve accepts.
func Policies() []Policy { return []Policy{Rename, Skip, Overwrite, Ask} }

// ParsePolicy folds stored or user-supplied text onto a known policy. Anything
// unrecognised becomes DefaultPolicy instead of an error, because this is what
// a hand-edited settings file runs through: a typo there must leave the server
// downloading. Reserve itself is stricter, so a typo in Go code still surfaces.
func ParsePolicy(s string) Policy {
	switch p := Policy(strings.ToLower(strings.TrimSpace(s))); p {
	case Overwrite, Skip, Rename, Ask:
		return p
	}
	return DefaultPolicy
}

// Action is what Reserve actually did. It is a string so it can be stored with
// the task and rendered without a lookup table.
type Action string

const (
	// Created means the name was free and is now reserved.
	Created Action = "created"
	// Renamed means a counter was appended; Result.Path is the new name.
	Renamed Action = "renamed"
	// Overwritten means an existing file was truncated to zero.
	Overwritten Action = "overwritten"
	// Skipped means the file was already there and nothing was reserved.
	Skipped Action = "skipped"
	// NeedsDecision means the task must stop and wait for a human.
	NeedsDecision Action = "needs-decision"
)

// ErrNeedsDecision reports that Ask hit a real collision. It accompanies the
// NeedsDecision action rather than replacing it, so neither a caller that
// switches on the action nor one that only checks the error can miss the stall.
var ErrNeedsDecision = errors.New("a file with this name already exists and the collision policy is to ask")

// ErrNoFreeName reports that the counter hit its cap.
var ErrNoFreeName = errors.New("no free name left")

// ErrUnknownPolicy guards against a policy string that never went through
// ParsePolicy. Falling back to a default here would silently pick either a
// destructive or a lossy behaviour on behalf of a caller that had a bug.
var ErrUnknownPolicy = errors.New("unknown collision policy")

// Result is the outcome of a reservation.
type Result struct {
	// Path is where the caller must write. For Skipped and NeedsDecision it is
	// the unchanged target, so the caller can still report which file was in the
	// way.
	Path string
	// Action says what happened to get that path.
	Action Action
	// File is the reserved file, open for reading and writing and positioned at
	// zero. It is nil for Skipped and NeedsDecision, where nothing was claimed.
	// Holding it open is what makes Path safe to use: dropping it and reopening
	// by name later reintroduces the very race this package removes.
	File *os.File

	// remove and created are what Release needs; they are unexported so a
	// zero-value Result cannot be talked into deleting anything.
	remove  func(string) error
	created bool
}

// Release gives up a reservation that was never used, which is what a caller
// must do when the download it reserved for fails before it writes anything.
// Without it every failed dispatch leaves an empty file behind, and the next
// attempt at the same download would then rename itself out of the way of its
// own leftovers.
//
// It removes the file only when Reserve created it and nothing has been written
// since: a file that already has bytes is a partial download, and a file that
// was merely overwritten existed before we got there. Deleting either would
// destroy data this package was asked to protect.
func (r Result) Release() error {
	if r.File == nil {
		return nil
	}
	// The size is read through the handle we already hold rather than by name,
	// because by now the name may point at a different file entirely.
	info, statErr := r.File.Stat()
	closeErr := r.File.Close()
	if !r.created || statErr != nil || info.Size() != 0 {
		return closeErr
	}
	remove := r.remove
	if remove == nil {
		remove = os.Remove
	}
	if err := remove(r.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Join(closeErr, err)
	}
	return closeErr
}

// Options holds the filesystem seam. The zero value talks to the real
// filesystem, which is what the package-level Reserve and Check use, so app.go
// never has to build one.
//
// Open deliberately yields a real *os.File: a download writer seeks and writes
// at offsets, and handing it an interface would only push the type assertion
// one layer out. A test therefore fakes the failures it cares about and lets
// the successes fall through to os.OpenFile in a temp directory.
type Options struct {
	// Perm is the mode a reserved file is created with. Zero means 0644.
	Perm fs.FileMode
	// DirPerm is the mode a missing parent gets. Zero means 0755, which is what
	// internal/app and internal/watch create download folders with.
	DirPerm fs.FileMode
	// MaxAttempts caps how many names Rename tries, counting the unsuffixed one.
	// Zero means DefaultMaxAttempts.
	MaxAttempts int

	// Open, Stat, MkdirAll and Remove replace the os functions of the same name.
	// A nil field means the real one.
	Open     func(name string, flag int, perm fs.FileMode) (*os.File, error)
	Stat     func(name string) (fs.FileInfo, error)
	MkdirAll func(name string, perm fs.FileMode) error
	Remove   func(name string) error
}

// Reserve claims a name for target under p and reports what it had to do to get
// one. See Options.Reserve.
func Reserve(target string, p Policy) (Result, error) {
	return Options{}.Reserve(target, p)
}

// Check reports whether something already sits at target. See Options.Check.
func Check(target string) (bool, error) {
	return Options{}.Check(target)
}

// Reserve claims a name for target under p, creating the parent directory if it
// does not exist yet, and returns the path to actually write to together with
// the open file that holds it.
//
// Every returned path is already claimed on disk. That is the whole point: a
// caller that instead checked for a free name and opened it afterwards would
// hand two simultaneous downloads the same file.
func (o Options) Reserve(target string, p Policy) (Result, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return Result{}, errors.New("collide: no target path")
	}
	// An empty policy is an old settings file that predates the setting, not a
	// mistake; anything else unrecognised is a caller that skipped ParsePolicy.
	if p == "" {
		p = DefaultPolicy
	}
	dir := filepath.Dir(target)
	if err := o.mkdirAll(dir, o.dirPerm()); err != nil {
		return Result{}, fmt.Errorf("collide: %s: %w", dir, err)
	}

	switch p {
	case Overwrite:
		// A plain Stat is enough even though another writer could create the
		// file between it and the open: O_TRUNC empties whatever is there
		// either way, so the worst a lost race does is label the action Created
		// and let Release remove a file that is by then already empty.
		action := Created
		if o.exists(target) {
			action = Overwritten
		}
		f, err := o.open(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, o.perm())
		if err != nil {
			return Result{}, fmt.Errorf("collide: %s: %w", target, err)
		}
		return Result{Path: target, Action: action, File: f, remove: o.Remove, created: action == Created}, nil

	case Skip, Ask:
		// Claiming first and reading the failure is what keeps these two honest
		// under concurrency: asking "does it exist" and creating it afterwards
		// lets two callers both decide the name was free.
		f, err := o.claim(target)
		if err == nil {
			return Result{Path: target, Action: Created, File: f, remove: o.Remove, created: true}, nil
		}
		if !o.taken(err, target) {
			return Result{}, fmt.Errorf("collide: %s: %w", target, err)
		}
		if p == Ask {
			return Result{Path: target, Action: NeedsDecision}, fmt.Errorf("collide: %s: %w", target, ErrNeedsDecision)
		}
		return Result{Path: target, Action: Skipped}, nil

	case Rename:
		max := o.maxAttempts()
		stem, ext := splitName(filepath.Base(target))
		for n := 1; n <= max; n++ {
			candidate := target
			if n > 1 {
				candidate = filepath.Join(dir, counted(stem, ext, n))
			}
			f, err := o.claim(candidate)
			if err == nil {
				action := Created
				if n > 1 {
					action = Renamed
				}
				return Result{Path: candidate, Action: action, File: f, remove: o.Remove, created: true}, nil
			}
			if !o.taken(err, candidate) {
				return Result{}, fmt.Errorf("collide: %s: %w", candidate, err)
			}
		}
		return Result{}, fmt.Errorf("collide: %s: %w after %d names", target, ErrNoFreeName, max)
	}

	return Result{}, fmt.Errorf("collide: %w: %q", ErrUnknownPolicy, string(p))
}

// Check reports whether something already sits at target, without creating,
// touching or reserving anything.
//
// The answer is advisory and can be stale the moment it is returned, which is
// precisely why Reserve claims the name itself instead of calling this first.
// It is here so the UI can warn about a collision before a download starts.
func (o Options) Check(target string) (bool, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, errors.New("collide: no target path")
	}
	if _, err := o.stat(target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("collide: %s: %w", target, err)
	}
	return true, nil
}

// claim creates name and fails if anything is already there. O_RDWR rather than
// O_WRONLY because a chunked writer reads back what it wrote to resume.
func (o Options) claim(name string) (*os.File, error) {
	return o.open(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, o.perm())
}

// taken reports whether a failed exclusive create means the name is in use.
//
// EEXIST is the ordinary answer, but it is not the only one: on Windows a
// *directory* sitting on that path comes back as a permission error instead,
// and extraction leaves folders named after their archives right beside the
// downloads. Treating only EEXIST as "taken" would abandon the whole rename
// loop over one such name and report it as a hard failure.
func (o Options) taken(err error, name string) bool {
	if errors.Is(err, fs.ErrExist) {
		return true
	}
	return o.exists(name)
}

func (o Options) exists(name string) bool {
	_, err := o.stat(name)
	return err == nil
}

func (o Options) open(name string, flag int, perm fs.FileMode) (*os.File, error) {
	if o.Open != nil {
		return o.Open(name, flag, perm)
	}
	return os.OpenFile(name, flag, perm)
}

func (o Options) stat(name string) (fs.FileInfo, error) {
	if o.Stat != nil {
		return o.Stat(name)
	}
	return os.Stat(name)
}

func (o Options) mkdirAll(name string, perm fs.FileMode) error {
	if o.MkdirAll != nil {
		return o.MkdirAll(name, perm)
	}
	return os.MkdirAll(name, perm)
}

func (o Options) perm() fs.FileMode {
	if o.Perm != 0 {
		return o.Perm
	}
	return 0o644
}

func (o Options) dirPerm() fs.FileMode {
	if o.DirPerm != 0 {
		return o.DirPerm
	}
	return 0o755
}

func (o Options) maxAttempts() int {
	if o.MaxAttempts > 0 {
		return o.MaxAttempts
	}
	return DefaultMaxAttempts
}

// counted builds the nth candidate name, clipping the stem if the counter would
// push the name past what the filesystem accepts. The original target is never
// clipped: the caller asked for that exact name, and quietly writing a shorter
// one would be a different file.
func counted(stem, ext string, n int) string {
	suffix := fmt.Sprintf(" (%d)", n)
	if room := maxBaseName - len(suffix) - len(ext); len(stem) > room {
		stem = clipBytes(stem, room)
	}
	return stem + suffix + ext
}

// clipBytes cuts s to at most n bytes, dropping at most the one rune the cut
// landed inside so the name is not left with half a character in it.
//
// Shortening until utf8.ValidString passes looks like the same thing and is
// not. Download names routinely carry bytes from a legacy encoding - a Latin-1
// "café" out of an archive from 2004 - and no suffix of such a name is ever
// valid UTF-8, so that loop keeps eating until it reaches the bad byte and can
// strip the entire name away, leaving a download called " (2).txt". Only the
// byte the cut fell on can be half a rune, so only that one is examined.
func clipBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	cut := n
	// A rune is at most utf8.UTFMax bytes long, so walking back over
	// continuation bytes terminates after three steps even on input that was
	// never UTF-8 to begin with.
	for i := 0; i < utf8.UTFMax-1 && cut > 0 && !utf8.RuneStart(s[cut]); i++ {
		cut--
	}
	if !utf8.RuneStart(s[cut]) {
		// There is no rune boundary to respect here, so keeping the bytes that
		// fit preserves more of the name the user asked for than dropping them.
		return s[:n]
	}
	return s[:cut]
}

// splitName splits a file name into the part the counter is appended to and the
// extension that has to stay at the end.
//
// The extension is not simply filepath.Ext: "archive.tar.gz" has to become
// "archive (2).tar.gz", because "archive (2).gz" is a file no unpacker
// recognises any more. The rule is deliberately narrow and keys on ".tar"
// rather than on "there is another dot", because release names are full of
// dots - a general rule would turn "Show.S01E02.1080p.mkv" into
// "Show (2).S01E02.1080p.mkv" and lose the episode from the name.
func splitName(base string) (stem, ext string) {
	ext = filepath.Ext(base)
	stem = strings.TrimSuffix(base, ext)
	// A dotfile such as ".gitignore" is all extension by that reading, leaving
	// nothing to count. Treat the whole name as the stem so it becomes
	// ".gitignore (2)" instead of " (2).gitignore".
	if stem == "" {
		return base, ""
	}
	if inner := filepath.Ext(stem); strings.EqualFold(inner, ".tar") {
		if rest := strings.TrimSuffix(stem, inner); rest != "" {
			return rest, inner + ext
		}
	}
	return stem, ext
}
