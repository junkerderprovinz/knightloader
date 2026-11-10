package app

// What an unpacking created, kept on the parts of the set so a removal with
// files can take it along, after a restart as well. Only what the unpacking
// made is recorded (see extract.Outcome.Created), and what it holds is
// measured, so a folder something else has written into since stays.

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// takenAt is where each entry of dir would land in dst under its own name, for
// the names dst already holds.
func takenAt(dir, dst string) map[string]string {
	entries, _ := os.ReadDir(dir)
	out := map[string]string{}
	for _, e := range entries {
		at := filepath.Join(dst, collide.SafeName(e.Name()))
		if _, err := os.Lstat(at); err == nil {
			out[filepath.Join(dir, e.Name())] = at
		}
	}
	return out
}

// arrivals is where each moved entry landed, or "" for one that went onto a
// name that was taken: merged into a folder or over a file that was there, it
// is no longer something the unpacking made on its own.
func arrivals(landed, taken map[string]string) map[string]string {
	out := make(map[string]string, len(landed))
	for src, dst := range landed {
		if at, ok := taken[src]; ok && samePath(at, dst) {
			dst = ""
		}
		out[src] = dst
	}
	return out
}

// relocate is where paths are after the move: under the entry they were moved
// with, and the moved entries themselves for an output folder whose contents
// were moved out of it. What went onto a taken name drops out.
func (d delivery) relocate(paths []string) []string {
	var out []string
	for _, p := range paths {
		if to, ok := d.arrival(p); ok {
			if to != "" {
				out = append(out, to)
			}
			continue
		}
		if d.contents && samePath(p, d.from) {
			for _, to := range d.landed {
				if to != "" {
					out = append(out, to)
				}
			}
		}
		out = append(out, p)
	}
	return out
}

// arrival is where p went with the entry that held it, and false when nothing
// that held it was moved.
func (d delivery) arrival(p string) (string, bool) {
	for src, dst := range d.landed {
		if !samePath(p, src) && !within(p, src) {
			continue
		}
		if dst == "" {
			return "", true
		}
		rel, _ := filepath.Rel(src, p)
		return filepath.Join(dst, rel), true
	}
	return "", false
}

// within reports whether p lies inside dir.
func within(p, dir string) bool {
	if p == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// measureOutput counts the files at path and their bytes. It is false when
// nothing is there.
func measureOutput(path string) (core.UnpackOutput, bool, error) {
	o := core.UnpackOutput{Path: path}
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return o, false, nil
	}
	if err != nil {
		return o, true, err
	}
	if !fi.IsDir() {
		o.Files, o.Bytes = 1, fi.Size()
		return o, true, nil
	}
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		o.Files++
		o.Bytes += info.Size()
		return nil
	})
	return o, true, err
}

// unpackRecord is what a finished job's parts record: what it made, what they
// recorded from an earlier unpacking, and what other tasks recorded that it
// wrote into as well, which then stays until all of them are removed with their
// files. Each is measured as it is now; shared is the share among them, to be
// measured again on the tasks that recorded it first.
func (a *App) unpackRecord(parts []string, made, reused []string) (record, shared []core.UnpackOutput) {
	a.mu.Lock()
	paths := slices.Clone(made)
	var theirs []string
	for id, t := range a.tasks {
		mine := slices.Contains(parts, id)
		for _, o := range t.Unpacked {
			switch {
			case mine:
				paths = append(paths, o.Path)
			case slices.ContainsFunc(slices.Concat(made, reused), func(q string) bool {
				return samePath(q, o.Path) || within(q, o.Path)
			}):
				theirs = append(theirs, o.Path)
			}
		}
	}
	a.mu.Unlock()

	seen := map[string]bool{}
	for _, p := range slices.Concat(paths, theirs) {
		if seen[filepath.Clean(p)] {
			continue
		}
		seen[filepath.Clean(p)] = true
		o, ok, err := measureOutput(p)
		if !ok || err != nil {
			continue
		}
		record = append(record, o)
		if slices.Contains(theirs, p) {
			shared = append(shared, o)
		}
	}
	return record, shared
}

// shareLocked puts the measures of shared on every task that recorded the same
// path, and returns the ids it changed. Caller holds a.mu.
func (a *App) shareLocked(shared []core.UnpackOutput) []string {
	var changed []string
	for id, t := range a.tasks {
		var next []core.UnpackOutput
		for i, o := range t.Unpacked {
			at := slices.IndexFunc(shared, func(s core.UnpackOutput) bool { return samePath(s.Path, o.Path) })
			if at < 0 || shared[at] == o {
				continue
			}
			if next == nil {
				next = slices.Clone(t.Unpacked)
			}
			next[i] = shared[at]
		}
		if next != nil {
			t.Unpacked = next
			changed = append(changed, id)
		}
	}
	return changed
}

// ownOutputsLocked is what t's unpacking created that a removal of t with its
// files may delete: none of it is recorded by another task, holds what
// another task recorded, or holds another task's file. Caller holds a.mu.
func (a *App) ownOutputsLocked(t *core.Task) []core.UnpackOutput {
	var out []core.UnpackOutput
	for _, o := range t.Unpacked {
		if !a.outputUsedByOtherLocked(t.ID, o.Path) {
			out = append(out, o)
		}
	}
	return out
}

func (a *App) outputUsedByOtherLocked(id, path string) bool {
	holds := func(p string) bool { return samePath(p, path) || within(p, path) }
	for other, t := range a.tasks {
		if other == id {
			continue
		}
		if holds(t.File) || mayBeOneFile(t.File, path) {
			return true
		}
		for _, o := range t.Unpacked {
			if holds(o.Path) {
				return true
			}
		}
	}
	return false
}

// dropUnpacked deletes what an unpacking created, unless it holds more files
// or bytes than when the unpacking was over. It runs off a.mu.
func dropUnpacked(o core.UnpackOutput, taskID string) {
	now, ok, err := measureOutput(o.Path)
	switch {
	case !ok:
		return
	case err != nil:
		log.Printf("left %s where it is: it could not be read to see what it holds: %v%s", o.Path, err, taskTag(taskID))
		return
	case now.Files > o.Files || now.Bytes > o.Bytes:
		log.Printf("left %s where it is: it holds files its unpacking did not write%s", o.Path, taskTag(taskID))
		return
	}
	if err := os.RemoveAll(o.Path); err != nil {
		log.Printf("could not delete %s: %v%s", o.Path, err, taskTag(taskID))
	}
}
