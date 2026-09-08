package startupcheck

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// probePrefix is the leading part of the file name the write test uses.
//
// THE LEADING DOT IS NOT COSMETIC. internal/watch/poller.go picks up any
// .txt/.crawljob dropped into the watched folder whose name does NOT start with
// a dot, so a probe called "knightloader-check.txt" landing in a watched folder
// is read as a link list and its contents are staged as downloads. Both probes
// this app already has (settings_paths.go and watch/watcher.go) start with a
// dot for the same reason.
//
// AND THE REST OF THE NAME IS UNIQUE PER PASS, which is where this one differs
// from both of them. They use fixed names, and a fixed name breaks in two ways
// this feature would hit immediately: two KnightLoader instances sharing one
// share - which is the federation case this app is built for - each delete the
// other's probe and both report failure, and a probe left behind by a process
// that was killed means the next boot's Remove deletes a stranger's file and
// calls it a pass.
const probePrefix = ".knightloader-startup-"

// probeName is one pass's probe file name: the process id, so two instances on
// one share never collide, plus eight random hex characters, so two passes in
// one process never do either.
func probeName() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Falls back to the clock rather than to a constant. A constant here
		// would quietly reintroduce the fixed name this whole comment is about.
		return fmt.Sprintf("%s%d-%08x", probePrefix, os.Getpid(), time.Now().UnixNano()&0xffffffff)
	}
	return fmt.Sprintf("%s%d-%02x%02x%02x%02x", probePrefix, os.Getpid(), b[0], b[1], b[2], b[3])
}

// Folder reports what one folder is, and - only when probe is true - whether
// this process can write in it.
//
// dir must be ABSOLUTE and must already be cut back to the fixed prefix of
// whatever template it came from; see FolderTarget.Dir. A relative path is
// refused rather than resolved, because it resolves against whatever the
// process's working directory happens to be, which is the same reason
// settings.sanitizePaths refuses to store one.
//
// IT NEVER CREATES ANYTHING. When the folder is not there the answer is
// CodeMissing plus the deepest folder above it that IS there, which is the
// sentence that tells an operator in one second that a mount is down:
// "/mnt/user/media is not there, the nearest existing folder is /mnt". See the
// package comment for what creating it instead would cost.
func Folder(ctx context.Context, dir, role string, probe bool, timeout time.Duration) Check {
	c := Check{ID: IDFolder, Role: role, Subject: dir}

	if !filepath.IsAbs(dir) {
		c.Verdict = VerdictFail
		c.Code = CodeRelative
		return c
	}
	// Checked before anything is started rather than only selected on below:
	// once the pass's total deadline has blown there is nothing to be gained by
	// firing another stat at a mount that is already the reason it blew, and
	// every one of those goroutines is parked inside a syscall that nothing can
	// interrupt.
	if ctx.Err() != nil {
		c.Verdict = VerdictWarn
		c.Code = CodeTimeout
		return c
	}

	if timeout <= 0 {
		timeout = DefaultFolderTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The work runs on its own goroutine because os.Stat and os.WriteFile take
	// no context and cannot be interrupted: a folder on a mount that has gone
	// away blocks for as long as that mount takes to give up, which on a
	// hard-mounted NFS export is for ever.
	//
	// SO THIS LEAKS A GOROUTINE ON A DEAD MOUNT, and that is the deliberate
	// trade rather than an oversight. The alternatives are worse in both
	// directions: waiting for it turns a diagnostic into the boot hang it was
	// written to prevent (with Dockerfile's HEALTHCHECK then restarting the
	// container into a loop), and there is no third option - Go cannot cancel a
	// blocked syscall. The goroutine holds one buffered slot and nothing else,
	// it finishes the moment the mount answers or the kernel gives up on it,
	// and it still removes its own probe file if it got as far as writing one.
	done := make(chan Check, 1)
	go func() { done <- inspect(dir, role, probe) }()

	select {
	case res := <-done:
		return res
	case <-ctx.Done():
		// Warn and not fail: on Unraid a share whose array disk is spun down
		// routinely takes several seconds to answer, and a red row for a disk
		// that is merely asleep is the false alarm that teaches people to stop
		// reading this page.
		c.Verdict = VerdictWarn
		c.Code = CodeTimeout
		return c
	}
}

// inspect is the blocking half of Folder, on its own so the deadline above can
// walk away from it.
func inspect(dir, role string, probe bool) Check {
	c := Check{ID: IDFolder, Role: role, Subject: dir}

	fi, err := os.Stat(dir)
	if err != nil {
		c.Measured = DeepestExisting(dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// Warn, not fail, and this is the row most likely to be
			// misread. A download folder that does not exist yet is the
			// NORMAL state of a fresh install - it is created by whoever
			// writes the first file into it - and it is also exactly what a
			// share that failed to mount looks like. The two cannot be told
			// apart from in here, so the row says which folder is missing
			// and which one above it is not, and lets the person who knows
			// their own box decide.
			c.Verdict = VerdictWarn
			c.Code = CodeMissing
		case errors.Is(err, fs.ErrPermission):
			c.Verdict = VerdictFail
			c.Code = CodeDenied
			c.Err = clamp(err.Error())
		default:
			c.Verdict = VerdictFail
			c.Code = CodeError
			c.Err = clamp(err.Error())
		}
		return c
	}

	c.Measured = dir
	if !fi.IsDir() {
		c.Verdict = VerdictFail
		c.Code = CodeNotADir
		return c
	}
	if !probe {
		// The folder is there and nothing was written into it. Ok as far as it
		// goes, and Probed being false is the part that says how far that is.
		c.Verdict = VerdictOK
		return c
	}

	name := filepath.Join(dir, probeName())
	if err := os.WriteFile(name, []byte("knightloader startup check\n"), 0o600); err != nil {
		c.Verdict = VerdictFail
		c.Code = writeCode(err)
		c.Err = clamp(err.Error())
		return c
	}
	if err := os.Remove(name); err != nil {
		// ITS OWN FINDING, with its own remedy. A folder where a file can be
		// created and not deleted is a real state - an SMB share with a
		// delete-denying ACL, a directory with the sticky bit, an exhausted
		// inode table - and its consequence is different from "cannot write":
		// a download arrives and can then never be renamed, moved out of the
		// working folder, or cleaned up. Unchecked it is also the long-run
		// mess, leaving one dotfile per press in the download folder for years.
		c.Verdict = VerdictFail
		c.Code = CodeNotRemoved
		c.Err = clamp(err.Error())
		return c
	}
	c.Verdict = VerdictOK
	c.Probed = true
	return c
}

// writeCode says which kind of "cannot write here" this was, because the four
// have four different remedies and one code for all of them sends somebody to
// check permissions on a filesystem that is mounted read-only.
//
// Matched on the errno rather than on the message, so it survives a translated
// libc. On Windows none of the three POSIX numbers is what the OS actually
// returns (it has its own ERROR_WRITE_PROTECT and ERROR_DISK_FULL), so a
// desktop install falls through to CodeError and shows the system's own
// sentence instead - which is a worse answer than a code, and still a true one.
// Mapping the Windows numbers as well would mean a build-tagged file per
// platform for a case that has never been reported on the build where downloads
// go to a mounted share.
func writeCode(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return CodeDenied
	case errors.Is(err, syscall.EROFS):
		return CodeReadOnly
	case errors.Is(err, syscall.ENOSPC):
		return CodeFull
	default:
		return CodeError
	}
}

// DeepestExisting is dir itself when it is a directory today, else the nearest
// folder above it that is.
//
// THE THIRD COPY OF THIS WALK IN THE TREE, and written down as such rather than
// quietly added: app_diskreport.go's deepestExistingDir and
// routes_folders.go's own are the other two. It is not imported from either
// because both live in packages that import settings, and this package's whole
// point is that it does not (see the package comment). If one of the three ever
// grows a rule the others do not have, this comment is where somebody finds out
// there are two more to fix.
func DeepestExisting(dir string) string {
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}
