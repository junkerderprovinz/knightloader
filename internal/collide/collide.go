// Package collide decides what happens when a download would land on a name
// that is already taken. A headless server has nobody to ask, so the answer is
// a policy chosen in advance.
//
// Reserve never reports a name it has not already claimed: it creates the
// file with O_CREATE|O_EXCL and returns the open handle, so two downloads
// finishing at once cannot both pick "name (2).txt". Handover makes the same
// decision for a backend that insists on creating the file itself.
package collide

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	gopeed "github.com/GopeedLab/gopeed/pkg/util"
)

// Policy is what to do about a name that is already taken.
type Policy string

const (
	// Overwrite truncates whatever is there. It is the only policy that can
	// lose existing data, so it is never the fallback for unknown input.
	Overwrite Policy = "overwrite"
	// Skip keeps the existing file and writes nothing.
	Skip Policy = "skip"
	// Rename writes alongside it as "name (2).txt", "name (3).txt" and so on.
	Rename Policy = "rename"
	// Ask stops the task until a person decides.
	Ask Policy = "ask"
)

// DefaultPolicy is what an unset or unreadable setting means. Rename neither
// destroys a file nor stalls a queue nobody is watching.
const DefaultPolicy = Rename

// DefaultMaxAttempts caps the counter. A thousand copies of one name means a
// bug upstream, such as a watch folder re-adding the same list.
const DefaultMaxAttempts = 1000

// maxBaseName is the longest name SafeName leaves untouched, well below the
// filesystem limit. The download library clips names to it regardless, so a
// longer candidate would be reserved under one name and written under
// another.
const maxBaseName = gopeed.MaxFilenameLength

// SafeName is the name that will actually appear on disk for a requested one.
// It calls the download library's own function because the library rewrites
// every name on its way to the fetcher; a reservation under any other name
// would reserve nothing, and a copy of its platform-dependent rules would
// drift.
func SafeName(name string) string { return gopeed.SafeFilename(name) }

// Policies lists the policies in the order a settings dropdown should offer
// them.
func Policies() []Policy { return []Policy{Rename, Skip, Overwrite, Ask} }

// ParsePolicy folds stored or user-supplied text onto a known policy.
// Anything unrecognised becomes DefaultPolicy, so a typo in a hand-edited
// settings file leaves the server downloading. Reserve itself is stricter.
func ParsePolicy(s string) Policy {
	switch p := Policy(strings.ToLower(strings.TrimSpace(s))); p {
	case Overwrite, Skip, Rename, Ask:
		return p
	}
	return DefaultPolicy
}

// Action is what Reserve actually did.
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
	// NeedsDecision means the task must stop and wait for a person.
	NeedsDecision Action = "needs-decision"
)

// ErrNeedsDecision accompanies the NeedsDecision action, so a caller checking
// either the action or the error sees the stall.
var ErrNeedsDecision = errors.New("a file with this name already exists and the collision policy is to ask")

// ErrNoFreeName reports that the counter hit its cap.
var ErrNoFreeName = errors.New("no free name left")

// ErrUnknownPolicy rejects a policy that never went through ParsePolicy,
// rather than silently picking a behaviour for a buggy caller.
var ErrUnknownPolicy = errors.New("unknown collision policy")

// ErrFolderOverwrite refuses Overwrite for a folder, where it would delete a
// tree of unknown size rather than truncate one file.
var ErrFolderOverwrite = errors.New("the collision policy is overwrite and the destination is a folder, which would delete everything inside it")

// Result is the outcome of a reservation.
type Result struct {
	// Path is where the caller must write. For Skipped and NeedsDecision it is
	// the target itself, naming the file in the way. Its file name has been
	// through SafeName.
	Path   string
	Action Action
	// File is the reserved file, open read-write at offset zero. It is nil for
	// Skipped, NeedsDecision, a folder, and after a Handover. The caller must
	// write through it; reopening by name brings the race back.
	File *os.File

	// Unexported so a zero Result cannot be talked into deleting anything.
	remove  func(string) error
	created bool
	dir     bool
}

func (r Result) removeFn() func(string) error {
	if r.remove != nil {
		return r.remove
	}
	return os.Remove
}

// Release gives up a reservation whose download failed before writing, so
// the next attempt does not rename itself around the leftover. It removes the
// file only if Reserve created it and it is still empty; anything else is a
// partial download or a file that existed before.
func (r Result) Release() error {
	// A folder claim is the empty directory itself, and rmdir refuses one that
	// has been filled.
	if r.dir {
		if !r.created {
			return nil
		}
		if err := r.removeFn()(r.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if r.File == nil {
		return nil
	}
	// Stat the handle, not the name, which may point at another file by now.
	info, statErr := r.File.Stat()
	closeErr := r.File.Close()
	if !r.created || statErr != nil || info.Size() != 0 {
		return closeErr
	}
	if err := r.removeFn()(r.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Join(closeErr, err)
	}
	return closeErr
}

// handoff gives the claim up and keeps only the name. Unlike Release it
// removes unconditionally, since the writer must find the name free; an
// Overwrite placeholder was already truncated, so nothing more is lost.
func (r Result) handoff() (Result, error) {
	if !r.created && r.Action != Overwritten {
		return r, nil // Skipped and NeedsDecision claimed nothing to give up
	}
	var closeErr error
	if r.File != nil {
		closeErr = r.File.Close()
		r.File = nil
	}
	if err := r.removeFn()(r.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return r, errors.Join(closeErr, err)
	}
	return r, closeErr
}

// Options holds the filesystem seam. The zero value uses the real
// filesystem. Open returns a real *os.File because a download writer seeks
// and writes at offsets; tests fake the failures and let successes fall
// through to a temp directory.
type Options struct {
	// Perm is the mode a reserved file is created with. Zero means 0644.
	Perm fs.FileMode
	// DirPerm is the mode a missing parent gets. Zero means 0755.
	DirPerm fs.FileMode
	// MaxAttempts caps how many names Rename tries, counting the unsuffixed
	// one. Zero means DefaultMaxAttempts.
	MaxAttempts int

	// Open, Stat, Mkdir, MkdirAll and Remove replace the os functions of the
	// same name. A nil field means the real one.
	Open     func(name string, flag int, perm fs.FileMode) (*os.File, error)
	Stat     func(name string) (fs.FileInfo, error)
	Mkdir    func(name string, perm fs.FileMode) error
	MkdirAll func(name string, perm fs.FileMode) error
	Remove   func(name string) error
}

// Reserve calls Options.Reserve on the real filesystem.
func Reserve(target string, p Policy) (Result, error) {
	return Options{}.Reserve(target, p)
}

// Handover calls Options.Handover on the real filesystem.
func Handover(target string, p Policy) (Result, error) {
	return Options{}.Handover(target, p)
}

// HandoverFolder calls Options.HandoverFolder on the real filesystem.
func HandoverFolder(target string, p Policy) (Result, error) {
	return Options{}.HandoverFolder(target, p)
}

// Check calls Options.Check on the real filesystem.
func Check(target string) (bool, error) {
	return Options{}.Check(target)
}

// Reserve claims a name for target under p, creating the parent directory if
// needed, and returns the path to write to with the open file that holds it.
// The file name goes through SafeName first, so the claimed name is the one a
// writer would produce.
func (o Options) Reserve(target string, p Policy) (Result, error) {
	return o.reserve(target, p, false)
}

// Handover is Reserve for a backend that creates the destination itself, as
// the embedded download library does. It claims a name under the policy and
// then deletes the placeholder: the claim decides the name against a folder
// nobody can slip a file into, and the delete is needed because the library
// runs its own duplicate check and would otherwise write "name (2) (2).ext".
// Between the delete and the library's create the name is briefly free.
func (o Options) Handover(target string, p Policy) (Result, error) {
	r, err := o.reserve(target, p, false)
	if err != nil {
		return r, err
	}
	return r.handoff()
}

// HandoverFolder is Handover for a folder destination, used for a resource
// with more than one file. A folder is counted as a whole ("Show.S01 (2)"),
// since its dot is not an extension, and Overwrite is refused.
func (o Options) HandoverFolder(target string, p Policy) (Result, error) {
	r, err := o.reserve(target, p, true)
	if err != nil {
		return r, err
	}
	return r.handoff()
}

func (o Options) reserve(target string, p Policy, folder bool) (Result, error) {
	target, err := safeTarget(target)
	if err != nil {
		return Result{}, err
	}
	// An empty policy comes from a settings file older than the setting.
	if p == "" {
		p = DefaultPolicy
	}
	dir := filepath.Dir(target)
	if err := o.mkdirAll(dir, o.dirPerm()); err != nil {
		return Result{}, fmt.Errorf("collide: %s: %w", dir, err)
	}

	switch p {
	case Overwrite:
		if folder {
			return Result{}, fmt.Errorf("collide: %s: %w", target, ErrFolderOverwrite)
		}
		// A racing create between Stat and open is harmless: O_TRUNC empties
		// the file either way, and at worst Release removes an empty file.
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
		// Claim first and read the failure; checking existence first would let
		// two callers both find the name free.
		f, err := o.claim(target, folder)
		if err == nil {
			return Result{Path: target, Action: Created, File: f, remove: o.Remove, created: true, dir: folder}, nil
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
		base := filepath.Base(target)
		stem, ext := base, ""
		if !folder {
			stem, ext = splitName(base)
		}
		for n := 1; n <= max; n++ {
			candidate := target
			if n > 1 {
				candidate = filepath.Join(dir, counted(stem, ext, n))
			}
			f, err := o.claim(candidate, folder)
			if err == nil {
				action := Created
				if n > 1 {
					action = Renamed
				}
				return Result{Path: candidate, Action: action, File: f, remove: o.Remove, created: true, dir: folder}, nil
			}
			if !o.taken(err, candidate) {
				return Result{}, fmt.Errorf("collide: %s: %w", candidate, err)
			}
		}
		return Result{}, fmt.Errorf("collide: %s: %w after %d names", target, ErrNoFreeName, max)
	}

	return Result{}, fmt.Errorf("collide: %w: %q", ErrUnknownPolicy, string(p))
}

// safeTarget rewrites the last element of target to the name that will
// actually be written.
func safeTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("collide: no target path")
	}
	base := filepath.Base(target)
	// A last element of a separator, "." or ".." names a directory, and
	// sanitising it would invent a file name.
	if base == "" || base == "." || base == ".." || os.IsPathSeparator(base[0]) {
		return "", fmt.Errorf("collide: %q names nothing to reserve", target)
	}
	return filepath.Join(filepath.Dir(target), SafeName(base)), nil
}

// Check reports whether something already sits at the sanitised target,
// without reserving anything. The answer can be stale at once, which is why
// Reserve claims rather than checks; Check is for warning in the UI and for
// the queue under Skip.
func (o Options) Check(target string) (bool, error) {
	target, err := safeTarget(target)
	if err != nil {
		return false, err
	}
	if _, err := o.stat(target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("collide: %s: %w", target, err)
	}
	return true, nil
}

// claim creates name exclusively, failing if anything is there. A file is
// opened O_RDWR because a chunked writer reads back to resume; a folder is
// claimed by Mkdir, which fails with EEXIST just as O_EXCL does.
func (o Options) claim(name string, folder bool) (*os.File, error) {
	if folder {
		return nil, o.mkdir(name, o.dirPerm())
	}
	return o.open(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, o.perm())
}

// taken reports whether a failed exclusive create means the name is in use.
// On Windows a directory on that path yields a permission error rather than
// EEXIST, and extraction leaves such folders beside the downloads.
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

func (o Options) mkdir(name string, perm fs.FileMode) error {
	if o.Mkdir != nil {
		return o.Mkdir(name, perm)
	}
	return os.Mkdir(name, perm)
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

// counted builds the nth candidate name, clipped to maxBaseName so the writer
// does not cut it to a name that was never reserved.
func counted(stem, ext string, n int) string {
	suffix := fmt.Sprintf(" (%d)", n)
	// An "extension" that long is really a query string or a hash, and is
	// clipped too.
	if room := maxBaseName - len(suffix); len(ext) > room {
		ext = clipBytes(ext, room)
	}
	if room := maxBaseName - len(suffix) - len(ext); len(stem) > room {
		stem = clipBytes(stem, room)
	}
	return stem + suffix + ext
}

// clipBytes cuts s to at most n bytes, dropping at most the one rune the cut
// landed in. It does not trim until the string is valid UTF-8: a name in a
// legacy encoding is never valid, and that loop would eat the whole name.
func clipBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	cut := n
	for i := 0; i < utf8.UTFMax-1 && cut > 0 && !utf8.RuneStart(s[cut]); i++ {
		cut--
	}
	if !utf8.RuneStart(s[cut]) {
		// Not UTF-8 at this point, so keep every byte that fits.
		return s[:n]
	}
	return s[:cut]
}

// splitName splits a file name into the stem the counter goes after and the
// extension that stays at the end. "archive.tar.gz" becomes
// "archive (2).tar.gz"; only ".tar" is treated as part of the extension,
// since release names are full of dots ("Show.S01E02.1080p.mkv").
func splitName(base string) (stem, ext string) {
	ext = filepath.Ext(base)
	stem = strings.TrimSuffix(base, ext)
	// A dotfile such as ".gitignore" counts as all stem.
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
