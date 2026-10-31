package extract

// RAR sets are unpacked by RARLAB's own UnRAR when one can be started, the
// decoder WinRAR uses, with rardecode as the fallback. UnRAR writes into a
// folder of its own beside the destination, and every file is then moved into
// place through the same target the Go readers write through, so the root
// folder, the barred paths and the job's record of what it wrote hold alike.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nwaples/rardecode/v2"

	"github.com/junkerderprovinz/knightloader/internal/filemode"
	"github.com/junkerderprovinz/knightloader/internal/nowindow"
)

// unrarName is the program's file name on this system.
var unrarName = func() string {
	if runtime.GOOS == "windows" {
		return "UnRAR.exe"
	}
	return "unrar"
}()

// UnrarPath is the UnRAR this process starts, or "" when there is none and RAR
// sets go to rardecode.
var UnrarPath = func() string { return unrarFound().path }

// UnrarSource says where UnrarPath came from: "env" for KL_UNRAR, "bundled"
// for the program's own folder, where the desktop build puts it, "path" for
// PATH, and "none".
func UnrarSource() string { return unrarFound().source }

// unrarLookup is where UnRAR was found and how.
type unrarLookup struct{ path, source string }

// unrarFound looks UnRAR up once, in the order UnrarSource names.
var unrarFound = sync.OnceValue(func() unrarLookup {
	if p := strings.TrimSpace(os.Getenv("KL_UNRAR")); p != "" {
		return unrarLookup{p, "env"}
	}
	if exe, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(exe), unrarName)
		if fi, err := os.Stat(beside); err == nil && !fi.IsDir() {
			return unrarLookup{beside, "bundled"}
		}
	}
	if p, err := exec.LookPath(unrarName); err == nil {
		return unrarLookup{p, "path"}
	}
	return unrarLookup{"", "none"}
})

// unrarVersionLine is the first line UnRAR prints about itself, such as "7.23".
var unrarVersionLine = regexp.MustCompile(`(?i)UNRAR\s+(\d+\.\d+(?:\s*beta\s*\d+)?)`)

// UnrarVersion is the version of the UnRAR at UnrarPath, or "" when there is
// none or it does not start.
var UnrarVersion = sync.OnceValue(func() string {
	bin := UnrarPath()
	if bin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := nowindow.CommandContext(ctx, bin)
	out, _ := cmd.Output()
	if m := unrarVersionLine.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
})

// errUnrarDidNotStart is an UnRAR that could not be started at all, which
// sends the archive to rardecode instead.
var errUnrarDidNotStart = errors.New("extract: unrar did not start")

// The exit codes UnRAR documents (errhnd.hpp) that mean something here.
const (
	unrarWarning     = 1
	unrarCRC         = 3
	unrarWrite       = 5
	unrarOpen        = 6
	unrarCreate      = 9
	unrarNoFiles     = 10
	unrarBadPassword = 11
	unrarRead        = 12
	unrarBadArchive  = 13
)

// unrarArgs is the command line for one pass over the set at archive into dir.
// No password is -p-, which answers UnRAR's question with "none" instead of
// waiting at a prompt nobody sees.
func unrarArgs(archive, dir, password string) []string {
	pw := "-p-"
	if password != "" {
		pw = "-p" + password
	}
	// x keeps the folders, -y answers every question, -o+ replaces what is
	// there (the folder is UnRAR's own and empty), -c- skips the comment,
	// -idq prints errors only, -ai leaves the permissions to the umask as the
	// Go readers do, and "--" ends the switches before names that start with
	// a dash.
	return []string{"x", "-y", "-o+", "-c-", "-idq", "-ai", pw, "--", archive, dir + string(filepath.Separator)}
}

// unrarEnv is the environment UnRAR runs in: a UTF-8 locale, without which it
// cuts a name such as "Hölle" at the first letter it cannot spell.
func unrarEnv() []string {
	locale := "C.UTF-8"
	if runtime.GOOS == "darwin" {
		locale = "en_US.UTF-8"
	}
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "LC_ALL=") && !strings.HasPrefix(kv, "LANG=") {
			env = append(env, kv)
		}
	}
	return append(env, "LC_ALL="+locale, "LANG="+locale)
}

// missingVolume finds the volume UnRAR says it could not find in its output.
var missingVolume = regexp.MustCompile(`(?i)cannot find volume\s+(.+)`)

// unrarError turns how UnRAR ended into the errors the Go readers return, so
// a failure reads the same whichever decoder met it. volume names the set's
// first file, for the failures UnRAR does not tie to a part.
func unrarError(code int, stderr, volume string) error {
	msg := strings.TrimSpace(stderr)
	cause := errors.New("unrar: " + firstLine(msg))
	if m := missingVolume.FindStringSubmatch(msg); m != nil {
		return &PartError{Part: filepath.Base(strings.TrimSpace(m[1])), Problem: ErrPartMissing, Err: cause}
	}
	switch code {
	case unrarBadPassword:
		return ErrPasswordRequired
	case unrarCRC, unrarBadArchive, unrarRead:
		// A wrong password on contents that are encrypted shows as a checksum
		// error, as it does in rardecode.
		if strings.Contains(strings.ToLower(msg), "password") {
			return ErrPasswordRequired
		}
		return &PartError{Part: filepath.Base(volume), Problem: ErrDamaged, Err: cause}
	case unrarWrite, unrarCreate:
		if lower := strings.ToLower(msg); strings.Contains(lower, "no space") || strings.Contains(lower, "disk full") ||
			strings.Contains(lower, "not enough space") {
			return fmt.Errorf("%w: %w", cause, syscall.ENOSPC)
		}
		return cause
	case unrarOpen:
		return &PartError{Part: filepath.Base(volume), Problem: ErrPartMissing, Err: cause}
	case unrarNoFiles:
		return &PartError{Part: filepath.Base(volume), Problem: ErrUnsupported, Err: cause}
	}
	return cause
}

func firstLine(s string) string {
	if s == "" {
		return "exited without a message"
	}
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// unrarContext is the job's context, so a cancelled job stops UnRAR too.
func unrarContext() context.Context {
	if s := bound.Load(); s != nil && s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

// unrarWatchEvery is how often the bytes UnRAR has written are counted for
// the job's progress.
const unrarWatchEvery = 500 * time.Millisecond

// extractRarWith unpacks the set at path with the UnRAR at bin. It reports
// errUnrarDidNotStart, and writes nothing, when UnRAR could not be started.
func extractRarWith(bin, path string, dest target, password string) (*Result, error) {
	stage, err := os.MkdirTemp(dest.dir, ".unrar-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	cmd := nowindow.CommandContext(unrarContext(), bin, unrarArgs(path, stage, password)...)
	cmd.Env = unrarEnv()
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: %w", errUnrarDidNotStart, err)
	}
	stop := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		watchStage(stage, stop)
	}()
	err = cmd.Wait()
	close(stop)
	<-watched
	if err != nil {
		if ctxErr := unrarContext().Err(); ctxErr != nil {
			return nil, ctxErr
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, err
		}
		// A warning is a file UnRAR could not set a time or attribute on,
		// which leaves every byte where it belongs.
		if exit.ExitCode() != unrarWarning {
			return nil, unrarError(exit.ExitCode(), stderr.String(), path)
		}
	}
	res := &Result{Dir: dest.dir}
	if err := placeStaged(stage, dest, res); err != nil {
		return nil, err
	}
	res.Volumes = rarVolumes(path, password)
	return res, nil
}

// watchStage feeds the job the bytes written under stage so far, until stop
// closes. A file UnRAR has not finished counts with what it holds already.
func watchStage(stage string, stop <-chan struct{}) {
	s := bound.Load()
	if s == nil {
		return
	}
	var counted int64
	tick := time.NewTicker(unrarWatchEvery)
	defer tick.Stop()
	for {
		var total int64
		_ = filepath.WalkDir(stage, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				if fi, err := d.Info(); err == nil {
					total += fi.Size()
				}
			}
			return nil
		})
		if total > counted {
			s.add(total - counted)
			counted = total
			s.emit(false)
		}
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

// placeStaged moves every file UnRAR wrote under stage to where the target
// puts it, and records each one as the job's own.
func placeStaged(stage string, dest target, res *Result) error {
	return filepath.WalkDir(stage, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == stage {
			return err
		}
		rel, err := filepath.Rel(stage, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if d.IsDir() {
			// An empty folder in the archive is kept; one the root folder is
			// taken off of is the destination itself.
			if dest.root != "" && name == dest.root {
				return nil
			}
			dir, err := dest.file(name)
			if err != nil {
				return err
			}
			return os.MkdirAll(dir, filemode.Dir)
		}
		dst, err := dest.file(name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), filemode.Dir); err != nil {
			return err
		}
		if err := os.Rename(p, dst); err != nil {
			return err
		}
		noteWatched(dst)
		res.Files++
		return nil
	})
}

// rarVolumes is every file of the set, for disposal: the reader steps from
// header to header, which crosses the volumes without decoding anything.
func rarVolumes(path, password string) []string {
	var opts []rardecode.Option
	if password != "" {
		opts = append(opts, rardecode.Password(password))
	}
	rc, err := rardecode.OpenReader(path, opts...)
	if err != nil {
		return []string{path}
	}
	defer rc.Close()
	for {
		if _, err := rc.Next(); err != nil {
			break
		}
	}
	var out []string
	for _, v := range rc.Volumes() {
		out = append(out, filepath.Join(filepath.Dir(path), v))
	}
	return out
}
