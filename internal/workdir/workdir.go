// Package workdir keeps a download that is still being written out of the
// folder its finished file belongs in, and carries the finished result the rest
// of the way once nothing is owed on it any more.
//
// It is not tidiness. A .part file written straight to its destination is a
// half file sitting in a folder other programs are watching, and both of the
// programs that matter here are right to act on what they see: Unraid's mover
// takes it off the cache and copies half a film onto the array, and a library
// scanner adds an unfinished mkv, fails to play it once, and then never looks
// again because nothing about the file changed afterwards. What is wrong is
// showing them a file that is not finished, so the bytes land somewhere nobody
// watches and the result is put in place by a rename, which is the one
// filesystem operation neither of them can catch half done.
//
// # Running out of space halfway through a cross-filesystem move
//
// A working folder on another filesystem turns the move into a copy, and a copy
// can fail with the source already half read. Nothing here ever deletes the
// source before the copy is complete: the bytes go to a temporary name inside
// the destination folder, are flushed to disk, and only then renamed into place
// and the source removed. A copy that fails - out of space, a network share
// that went away, a cancelled shutdown - removes its own partial copy and
// leaves the source exactly where it was, so what is lost is the time the copy
// took and never the download. The failure is reported to the caller, which
// puts it on the task, and the delivery can be tried again once the disk has
// room.
//
// # Leftovers after a crash
//
// Nothing sweeps them on the way past, and that is deliberate: a .part in the
// working folder is precisely what a resumed download needs to find, and a
// finished file that never got delivered is bytes somebody paid for in
// bandwidth. So a crash leaves the working folder exactly as it stood, the same
// download writes into the same folder again after the restart (For is a pure
// function of the destination, so it answers the same path), and a delivery
// that was interrupted is retried when the download settles a second time. What
// does not come back is a working folder whose destination no task points at
// any more - a task somebody removed while its download was running - and Sweep
// is the one thing that removes those, by name shape, by age, and only for keys
// the caller says are nobody's.
package workdir

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
)

// keyDigest is how many bytes of the destination's digest go into a working
// folder's name. Four bytes is eight hex characters: long enough that two
// destinations on one box will not collide, short enough that the readable half
// of the name is still what somebody looking at the folder sees first.
const keyDigest = 4

// tmpPrefix names the copy in flight during a cross-filesystem move. It is
// created in the DESTINATION folder, because a copy has to land on the
// destination's own filesystem before a rename can put it in place atomically -
// which is the whole point of the exercise. The dot keeps it out of the way of
// the scanners this package exists to protect: an entry starting with one is
// skipped by every library scanner worth the name, and it is removed either way
// the copy ends.
const tmpPrefix = ".knightloader-move-"

// copyChunk is how much is written between two cancellation checks, matching
// internal/extract's own copy loop: small enough that a shutdown is felt at
// once, large enough that the check is not what the copy spends its time on.
const copyChunk = 256 << 10

// bufPool holds the copy buffers, so a folder of ten thousand small files is
// not ten thousand allocations of a quarter megabyte each.
var bufPool = sync.Pool{New: func() any { b := make([]byte, copyChunk); return &b }}

// ErrUnsupportedEntry reports something a copy cannot carry across a filesystem
// boundary: a socket, a device node, a named pipe. A rename moves them without
// caring what they are, so this can only be reached on the copy path, and it is
// an error rather than a silent skip - a delivery that quietly dropped part of
// what it was given would be found much later, by the file not being there.
var ErrUnsupportedEntry = errors.New("workdir: this kind of file cannot be copied to another filesystem")

// For is the working folder that stands in for one destination folder. An empty
// root answers the destination itself, which is the off switch: with no working
// folder configured every caller keeps writing exactly where it wrote before.
//
// IT IS KEYED BY THE DESTINATION AND NEVER BY THE TASK, and that is the
// difference between an archive that unpacks and one that does not. A five-part
// rar is five separate downloads, and internal/extract finds the other four
// volumes by listing the folder the first one is in. A folder per task would
// give every part a private folder of its own, and every multi-volume set in
// the app would quietly stop being a set.
//
// The name carries the destination's own last segment so a folder can be
// recognised by eye, plus a digest of the whole path so that "/tv/Show/Season 1"
// and "/films/Show/Season 1" are two working folders rather than one shared
// between them. The digest folds case, because Windows reaches one folder under
// several spellings and a set split between "D:\dl" and "D:\DL" would be two
// half sets; the readable half is taken as written, so on a case-sensitive
// filesystem the two spellings still get two folders.
func For(root, dest string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return dest
	}
	return filepath.Join(root, Key(dest))
}

// Key is the folder name For builds, exported because Sweep's caller has to be
// able to say which keys still belong to something.
func Key(dest string) string {
	clean := filepath.Clean(dest)
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.ToSlash(clean))))
	name := collide.SafeName(filepath.Base(clean))
	// A destination that is a filesystem root has no last segment worth
	// printing ("/" and "C:\" both come back as themselves), so the digest
	// carries the whole name on its own rather than being appended to a
	// separator that cannot appear in a folder name anyway.
	if name == "" || name == "." || name == string(filepath.Separator) || strings.ContainsAny(name, `/\:`) {
		return "root-" + hex.EncodeToString(sum[:keyDigest])
	}
	return name + "-" + hex.EncodeToString(sum[:keyDigest])
}

// Options is what a move does about a destination that is already taken, plus
// the one seam a test needs.
type Options struct {
	// Policy is collide's own vocabulary, read here with two deviations that
	// are stated rather than hidden.
	//
	// collide.Ask is folded onto Rename. There is nobody to ask: a move happens
	// after the download and the extraction are already over, on a box with no
	// dialog, and honouring "ask" would mean a result that sits in the working
	// folder for ever with nothing on screen able to release it. Rename is
	// collide's own default for the same reason - it is the only policy that
	// neither destroys a file the user has nor stalls something nobody is
	// watching.
	//
	// collide.Overwrite on a FOLDER merges rather than replaces. collide
	// refuses it outright there (ErrFolderOverwrite) because "overwrite" on a
	// folder would mean deleting a tree of unknown size, and that refusal is
	// right for a download about to be written. Here the source is a folder of
	// finished files being put into a folder that is already there, and the
	// useful reading of the same word is the one a person expects from dragging
	// one folder onto another: the entries move in, a file already at that name
	// is replaced, and everything else in the destination is left alone.
	Policy collide.Policy
	// MaxAttempts caps how many counted names Rename tries. Zero means
	// collide's own cap.
	MaxAttempts int
	// PruneSourceDir removes the folder a moved entry came out of once it is
	// empty. It is off by default because the caller is the only one that knows
	// whether that folder was ours: emptying a working folder is tidying up,
	// and removing a folder of the user's that happened to hold one file is not.
	PruneSourceDir bool
	// Rename replaces os.Rename. A test uses it to force the cross-filesystem
	// path, which cannot be produced on demand on a single-disk build agent;
	// nil means the real one.
	Rename func(oldpath, newpath string) error
}

func (o Options) rename(oldpath, newpath string) error {
	if o.Rename != nil {
		return o.Rename(oldpath, newpath)
	}
	return os.Rename(oldpath, newpath)
}

// policy is the collide policy this move applies. Empty means collide's own
// default, exactly as collide reads an empty policy, and Ask folds onto the
// same value - see Options.Policy.
func (o Options) policy() collide.Policy {
	p := collide.ParsePolicy(string(o.Policy))
	if p == collide.Ask {
		return collide.Rename
	}
	return p
}

// Result is what one move did.
type Result struct {
	// Path is where the entry ended up, which is the SOURCE path when nothing
	// moved: a caller that reports where something is must not be handed a path
	// nothing is at.
	Path string
	// Copied says the rename could not reach and the bytes were copied. It is
	// worth reporting because it is the expensive answer, and because a
	// configuration that copies every download between two filesystems is
	// usually a mistake somebody wants to know about.
	Copied bool
	// Skipped says the destination was already taken and the policy is to skip,
	// so the source is still in the working folder on purpose.
	Skipped bool
}

// Report is what a whole content move did.
type Report struct {
	// Dir is the folder the entries were moved into.
	Dir string
	// Moved and Skipped count entries, not files: one entry can be a folder
	// holding a season of television.
	Moved   int
	Skipped int
	// Copied is set when any one of the entries had to be copied.
	Copied bool
}

// Move takes one entry - a file or a whole folder - out of the working folder
// and puts it in dstDir under the collision policy.
//
// The name is decided with collide.Handover, which claims it against a folder
// nobody else can slip a file into meanwhile and then gives the claim up so the
// rename can have it. That leaves the same microsecond window Handover already
// documents, and it is still the right trade: the alternative is deciding the
// name off a stat taken before a copy that may run for an hour.
func Move(ctx context.Context, src, dstDir string, o Options) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{Path: src}, err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return Result{Path: src}, err
	}

	target, skipped, err := o.target(src, dstDir, info.IsDir())
	if err != nil {
		return Result{Path: src}, err
	}
	if skipped {
		return Result{Path: src, Skipped: true}, nil
	}
	// A folder already sitting at the target under Overwrite is the merge case:
	// the entries move in one at a time and the folder itself stays where it is.
	if info.IsDir() && o.policy() == collide.Overwrite && isDir(target) {
		rep, err := MoveContents(ctx, src, target, o)
		if err != nil {
			return Result{Path: src, Copied: rep.Copied}, err
		}
		// MoveContents has already removed the emptied source folder; what is
		// left is the folder that held it, which the same rule applies to.
		if rep.Skipped == 0 {
			o.prune(filepath.Dir(src))
		}
		return Result{Path: target, Copied: rep.Copied}, nil
	}

	if err := o.rename(src, target); err == nil {
		o.prune(filepath.Dir(src))
		return Result{Path: target}, nil
	}
	// The rename is the test, rather than a device number worked out in
	// advance: os.Rename is the operation that has to succeed, and Windows has
	// no portable device number to compare in the first place. Every other
	// reason a rename can fail - a destination folder that will not take a new
	// entry, a source somebody removed - fails the copy below as well, and the
	// copy's own error is the more specific of the two.
	if err := copyAcross(ctx, src, target, info); err != nil {
		return Result{Path: src}, err
	}
	if err := os.RemoveAll(src); err != nil {
		// The bytes are at the destination and the source could not be removed,
		// which is a leftover rather than a lost file. Saying so is better than
		// failing a delivery that in every way that matters succeeded, because
		// a failure here would send the caller to retry a move whose source is
		// already gone from the caller's point of view.
		return Result{Path: target, Copied: true}, fmt.Errorf("workdir: %s was copied to %s but could not be removed: %w", src, target, err)
	}
	o.prune(filepath.Dir(src))
	return Result{Path: target, Copied: true}, nil
}

// MoveContents moves everything INSIDE srcDir into dstDir, rather than moving
// srcDir itself.
//
// This is the difference between "unpack into this folder" and "put the
// unpacked files here". A release unpacked as "Show.S01.COMPLETE/ep01.mkv"
// dropped whole into a library folder gives "Serien/Show/Staffel 1/
// Show.S01.COMPLETE/ep01.mkv", which is one folder level no library wanted; the
// entries moved in one at a time give the episodes where the episodes go.
//
// It is a per-entry decision, so a name already taken is settled by the policy
// for that one entry and the rest of the release still lands.
func MoveContents(ctx context.Context, srcDir, dstDir string, o Options) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rep := Report{Dir: dstDir}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return rep, err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		r, err := Move(ctx, filepath.Join(srcDir, e.Name()), dstDir, o)
		if err != nil {
			return rep, err
		}
		rep.Copied = rep.Copied || r.Copied
		if r.Skipped {
			rep.Skipped++
			continue
		}
		rep.Moved++
	}
	if rep.Skipped == 0 {
		// Emptied by this call, so the shell it came in is ours to remove. A
		// folder that still holds something a policy refused to move is left
		// standing, because what is in it is the very thing somebody has to
		// look at.
		o.prune(srcDir)
	}
	return rep, nil
}

// target decides the name at the destination and reports whether the policy
// refused the move outright.
func (o Options) target(src, dstDir string, folder bool) (string, bool, error) {
	want := filepath.Join(dstDir, filepath.Base(src))
	p := o.policy()
	co := collide.Options{MaxAttempts: o.MaxAttempts}
	if folder {
		if p == collide.Overwrite {
			// collide refuses Overwrite on a folder, so the name is settled
			// here instead - see Options.Policy for what the word means in a
			// move. MkdirAll rather than nothing, because the destination
			// folder may not exist yet and the rename below cannot make it.
			if err := os.MkdirAll(dstDir, 0o755); err != nil {
				return "", false, fmt.Errorf("workdir: %s: %w", dstDir, err)
			}
			return filepath.Join(dstDir, collide.SafeName(filepath.Base(src))), false, nil
		}
		r, err := co.HandoverFolder(want, p)
		if err != nil {
			return "", false, err
		}
		return r.Path, r.Action == collide.Skipped, nil
	}
	r, err := co.Handover(want, p)
	if err != nil {
		return "", false, err
	}
	return r.Path, r.Action == collide.Skipped, nil
}

// prune removes dir when it is empty and the caller asked for it. os.Remove
// refuses a directory that still holds anything, which is what makes this safe
// to call while another download is writing into the same working folder.
func (o Options) prune(dir string) {
	if !o.PruneSourceDir || strings.TrimSpace(dir) == "" {
		return
	}
	_ = os.Remove(dir)
}

// copyAcross carries one entry to a filesystem a rename could not reach.
//
// THE ORDER IS THE WHOLE GUARANTEE. The bytes go to a temporary name inside the
// destination folder, they are flushed, and only then is that name renamed into
// place - so the destination never holds a partially written file under the
// name anything is watching for, and the source is still untouched at every
// point up to that rename. A failure anywhere in between removes the temporary
// copy and returns; the caller still has the source, and the delivery can be
// tried again when whatever failed has been fixed.
func copyAcross(ctx context.Context, src, target string, info fs.FileInfo) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workdir: %s: %w", dir, err)
	}
	tmp, err := stageCopy(ctx, src, dir, info)
	if err != nil {
		if tmp != "" {
			_ = os.RemoveAll(tmp)
		}
		return err
	}
	// A folder merged into one that is already there never reaches this
	// function: Move settles that case entry by entry before it gets here, so
	// what is renamed below is always a name nothing is at.
	if err := os.Rename(tmp, target); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("workdir: %s: %w", target, err)
	}
	return nil
}

// stageCopy writes the copy under a temporary name in dir and reports that
// name, so the caller can remove it whichever way the copy ends.
//
// The permissions are put back at the end, and on a box where several users
// share one library that is not a detail. MkdirTemp makes a folder nobody but
// the owner can enter (0700) and CreateTemp a file nobody else can read (0600),
// which for a temporary name is right and for the file the move is about to
// name is a download the media server cannot open. Copying the source's own
// mode over is the answer, because that is the mode everything else in the same
// folder already has.
func stageCopy(ctx context.Context, src, dir string, info fs.FileInfo) (string, error) {
	if info.IsDir() {
		tmp, err := os.MkdirTemp(dir, tmpPrefix)
		if err != nil {
			return "", fmt.Errorf("workdir: %s: %w", dir, err)
		}
		if err := copyTree(ctx, src, tmp); err != nil {
			return tmp, err
		}
		return tmp, os.Chmod(tmp, info.Mode().Perm())
	}
	f, err := os.CreateTemp(dir, tmpPrefix)
	if err != nil {
		return "", fmt.Errorf("workdir: %s: %w", dir, err)
	}
	tmp := f.Name()
	if err := copyInto(ctx, f, src, info); err != nil {
		return tmp, err
	}
	return tmp, os.Chmod(tmp, info.Mode().Perm())
}

// copyTree reproduces src under dst, which is an empty folder on the
// destination's own filesystem.
func copyTree(ctx context.Context, src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if rel == "." {
				return nil // the temporary folder is already there
			}
			return os.Mkdir(out, info.Mode().Perm())
		case d.Type()&fs.ModeSymlink != 0:
			// Recreated as a link rather than followed. A tarball's own
			// "latest -> v2.1" is a link to something inside the same release,
			// and copying what it points at would turn one release into two
			// copies of every file it links to.
			at, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(at, out)
		case d.Type().IsRegular():
			f, err := os.OpenFile(out, os.O_RDWR|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
			if err != nil {
				return err
			}
			return copyInto(ctx, f, p, info)
		}
		return fmt.Errorf("%w: %s", ErrUnsupportedEntry, p)
	})
}

// copyInto copies src into the already-open dst and closes it either way.
//
// THE FLUSH IS NOT OPTIONAL, and it is what costs. A write that has only
// reached the page cache is a file the kernel has promised to write later, and
// the very next thing this package does is delete the only other copy. On a
// forty gigabyte film the flush is the price of the source still being there
// after a power cut two seconds later, which is the failure the whole package
// exists for.
func copyInto(ctx context.Context, dst *os.File, src string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		dst.Close()
		return err
	}
	defer in.Close()
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	buf := *bp
	for {
		if err := ctx.Err(); err != nil {
			dst.Close()
			return err
		}
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				dst.Close()
				return werr
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			dst.Close()
			return rerr
		}
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		return err
	}
	name := dst.Name()
	if err := dst.Close(); err != nil {
		return err
	}
	// The modification time is carried over, and it is not cosmetic: a library
	// sorted by "date added" reads it, and a mover that stamped every delivered
	// file with the moment it was delivered would put a two-year-old archive's
	// contents at the top of that list. Folder times are deliberately not
	// carried - a folder's own time changes as its children are written, so
	// there is no honest value to copy.
	return os.Chtimes(name, time.Now(), info.ModTime())
}

// Sweep removes working folders that belong to nothing any more.
//
// It is conservative in three ways at once, because it is the one function here
// that deletes something nobody asked it to. It only looks at entries whose
// name has the shape For builds, so a folder somebody else put in the working
// root is never touched. It only removes an entry the caller says is nobody's,
// which is what live answers. And it only removes one that has not been written
// to for minAge, so a download that has been running since yesterday survives
// even if the caller's own bookkeeping has lost sight of it.
//
// The age is read from the entry AND from what is directly inside it. A folder's
// own modification time only moves when something is created or removed in it,
// so a forty gigabyte download three days into writing one .part file would
// otherwise look untouched since the day it started.
func Sweep(root string, live func(key string) bool, minAge time.Duration) (int, error) {
	if strings.TrimSpace(root) == "" || minAge <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-minAge)
	removed := 0
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || !looksLikeKey(e.Name()) {
			continue
		}
		if live != nil && live(e.Name()) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if newest(dir).After(cutoff) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

// looksLikeKey reports a name this package would have built: something, a dash,
// and eight hex characters.
func looksLikeKey(name string) bool {
	const n = keyDigest * 2
	if len(name) < n+2 || name[len(name)-n-1] != '-' {
		return false
	}
	for _, c := range name[len(name)-n:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// newest is the latest modification time of dir or of anything directly in it.
func newest(dir string) time.Time {
	var at time.Time
	if info, err := os.Stat(dir); err == nil {
		at = info.ModTime()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return at
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(at) {
			at = info.ModTime()
		}
	}
	return at
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
