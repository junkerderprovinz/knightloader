package app

// What an unpacking created, kept on the parts of the set so a removal with
// files can take it along, after a restart as well. Only what the unpacking
// made is recorded (see extract.Outcome.Created), with every file it wrote
// there, so a folder something else has written into since stays.

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
// holds more than the unpacking made.
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

// entryMove is what a move did with what an unpacking wrote: from is the
// folder it took, whole or with contents entry by entry, and landed where each
// of those went, "" for one that went onto a name that was taken (see
// arrivals).
type entryMove struct {
	from     string
	contents bool
	landed   map[string]string
}

// relocate is where paths are after the move: under the entry they were moved
// with, and the moved entries themselves for an output folder whose contents
// were moved out of it. What went onto a taken name drops out.
func (m entryMove) relocate(paths []string) []string {
	var out []string
	for _, p := range paths {
		if to, ok := m.arrival(p); ok {
			if to != "" {
				out = append(out, to)
			}
			continue
		}
		if m.contents && samePath(p, m.from) {
			for _, to := range m.landed {
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
func (m entryMove) arrival(p string) (string, bool) {
	for src, dst := range m.landed {
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

// carry is relocate for a record: o goes with the entry it was moved with, or
// for a folder whose contents were moved out of it, each moved entry takes its
// share of the files o records and the folder keeps the rest.
func (m entryMove) carry(o core.UnpackOutput) []core.UnpackOutput {
	if to, ok := m.arrival(o.Path); ok {
		if to == "" {
			return nil
		}
		o.Path = to
		return []core.UnpackOutput{o}
	}
	if !m.contents || !samePath(o.Path, m.from) {
		return []core.UnpackOutput{o}
	}
	var out []core.UnpackOutput
	rest := maps.Clone(o.Files)
	for _, src := range slices.Sorted(maps.Keys(m.landed)) {
		entry, _ := filepath.Rel(m.from, src)
		moved := core.UnpackOutput{Path: m.landed[src], Files: map[string]int64{}, KeptBy: o.KeptBy}
		for rel, size := range o.Files {
			if inner, ok := under(rel, entry); ok {
				delete(rest, rel)
				moved.Files[inner] = size
			}
		}
		if moved.Path != "" {
			out = append(out, moved)
		}
	}
	o.Files = rest
	return append(out, o)
}

// under is rel, a path relative to a folder, relative to entry in that folder
// instead, and false when entry does not hold it.
func under(rel, entry string) (string, bool) {
	if rel == entry {
		return ".", true
	}
	inner, ok := strings.CutPrefix(rel, entry+string(filepath.Separator))
	return inner, ok
}

// within reports whether p lies inside dir.
func within(p, dir string) bool {
	if p == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// overlaps reports whether one of x and y is the other or lies inside it.
func overlaps(x, y string) bool {
	return samePath(x, y) || within(x, y) || within(y, x)
}

// measureOutput lists the files at path with their sizes. It is false when
// nothing is there.
func measureOutput(path string) (core.UnpackOutput, bool, error) {
	o := core.UnpackOutput{Path: path, Files: map[string]int64{}}
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return o, false, nil
	}
	if err != nil {
		return o, true, err
	}
	if !fi.IsDir() {
		o.Files["."] = fi.Size()
		return o, true, nil
	}
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(path, p)
		o.Files[rel] = info.Size()
		return nil
	})
	return o, true, err
}

// sizesOf is the size of each of files that is still there.
func sizesOf(files []string) map[string]int64 {
	out := make(map[string]int64, len(files))
	for _, f := range files {
		if fi, err := os.Lstat(f); err == nil && !fi.IsDir() {
			out[f] = fi.Size()
		}
	}
	return out
}

// grown is o with the files of written that lie in it added at their present
// size, or false when nothing is at o's path any more.
func grown(o core.UnpackOutput, written map[string]int64) (core.UnpackOutput, bool) {
	if _, err := os.Lstat(o.Path); err != nil {
		return o, false
	}
	files := make(map[string]int64, len(o.Files))
	maps.Copy(files, o.Files)
	for f, size := range written {
		if samePath(f, o.Path) || within(f, o.Path) {
			rel, _ := filepath.Rel(o.Path, f)
			files[rel] = size
		}
	}
	o.Files = files
	return o, true
}

// merged adds o to record, joined with an output of the same path there.
func merged(record []core.UnpackOutput, o core.UnpackOutput) []core.UnpackOutput {
	at := slices.IndexFunc(record, func(r core.UnpackOutput) bool { return samePath(r.Path, o.Path) })
	if at < 0 {
		return append(record, o)
	}
	files := maps.Clone(record[at].Files)
	maps.Copy(files, o.Files)
	record[at].Files = files
	record[at].KeptBy = keptBy(record[at].KeptBy, o.KeptBy...)
	return record
}

// keptBy is ids with more added, each once.
func keptBy(ids []string, more ...string) []string {
	out := slices.Clone(ids)
	for _, id := range more {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func sameOutput(x, y core.UnpackOutput) bool {
	return x.Path == y.Path && maps.Equal(x.Files, y.Files) && slices.Equal(x.KeptBy, y.KeptBy)
}

func sameRecord(x, y []core.UnpackOutput) bool {
	return slices.EqualFunc(x, y, sameOutput)
}

// unpackRecord is what a finished job's parts record: what it made, what they
// recorded from an earlier unpacking, and what other tasks recorded that it
// wrote into as well, which then stays until all of them are removed with their
// files. What it made holds only what it wrote; an earlier record keeps the
// files it had and gains those the job wrote into it, so a file added by hand
// in between stays foreign to it. shared is the share of the other tasks, which
// they record the same way.
func (a *App) unpackRecord(parts []string, made, reused, written []string) (record, shared []core.UnpackOutput) {
	touched := slices.Concat(made, reused)
	a.mu.Lock()
	var earlier, theirs []core.UnpackOutput
	for id, t := range a.tasks {
		mine := slices.Contains(parts, id)
		for _, o := range t.Unpacked {
			switch {
			case mine:
				earlier = append(earlier, o)
			case slices.ContainsFunc(touched, func(q string) bool { return samePath(q, o.Path) || within(q, o.Path) }):
				theirs = append(theirs, o)
			}
		}
	}
	a.mu.Unlock()

	for _, p := range made {
		if o, ok, err := measureOutput(p); ok && err == nil {
			record = merged(record, o)
		}
	}
	sizes := sizesOf(written)
	for _, o := range slices.Concat(earlier, theirs) {
		// Gone before the job made it again, so all it holds is the job's.
		if slices.ContainsFunc(made, func(p string) bool { return samePath(o.Path, p) || within(o.Path, p) }) {
			continue
		}
		if o, ok := grown(o, sizes); ok {
			record = merged(record, o)
		}
	}
	for _, o := range record {
		if slices.ContainsFunc(theirs, func(t core.UnpackOutput) bool { return samePath(t.Path, o.Path) }) {
			shared = append(shared, o)
		}
	}
	return record, shared
}

// editRecordsLocked replaces each output every task records with what edit
// makes of it, and returns the ids of the tasks whose record changed. Caller
// holds a.mu.
func (a *App) editRecordsLocked(edit func(id string, o core.UnpackOutput) []core.UnpackOutput) []string {
	var changed []string
	for id, t := range a.tasks {
		if len(t.Unpacked) == 0 {
			continue
		}
		var next []core.UnpackOutput
		for _, o := range t.Unpacked {
			next = append(next, edit(id, o)...)
		}
		if !sameRecord(next, t.Unpacked) {
			t.Unpacked = next
			changed = append(changed, id)
		}
	}
	return changed
}

// shareLocked puts shared on every task that recorded the same path, and
// returns the ids it changed. Caller holds a.mu.
func (a *App) shareLocked(shared []core.UnpackOutput) []string {
	return a.editRecordsLocked(func(_ string, o core.UnpackOutput) []core.UnpackOutput {
		if at := slices.IndexFunc(shared, func(s core.UnpackOutput) bool { return samePath(s.Path, o.Path) }); at >= 0 {
			return []core.UnpackOutput{shared[at]}
		}
		return []core.UnpackOutput{o}
	})
}

// keepSharedLocked follows a removal of t from the list that keeps its files:
// what other tasks recorded in or around what t's unpacking created holds
// those files, so it is marked as kept by t, and no removal with files takes
// it. It returns the rows it marked. Caller holds a.mu.
func (a *App) keepSharedLocked(t *core.Task) []taskCopy {
	if len(t.Unpacked) == 0 {
		return nil
	}
	ids := a.editRecordsLocked(func(id string, o core.UnpackOutput) []core.UnpackOutput {
		if id != t.ID && slices.ContainsFunc(t.Unpacked, func(m core.UnpackOutput) bool { return overlaps(m.Path, o.Path) }) {
			o.KeptBy = keptBy(o.KeptBy, t.ID)
		}
		return []core.UnpackOutput{o}
	})
	return a.copiesOfLocked(ids)
}

// unkeepLocked takes back what keepSharedLocked marked for a task an undo has
// brought back to the list, on every row, the restored ones included: a row
// put back by another undo may carry the mark from before. Caller holds a.mu.
func (a *App) unkeepLocked() []taskCopy {
	listed := func(id string) bool { return a.tasks[id] != nil }
	changed := a.editRecordsLocked(func(_ string, o core.UnpackOutput) []core.UnpackOutput {
		if slices.ContainsFunc(o.KeptBy, listed) {
			o.KeptBy = slices.DeleteFunc(slices.Clone(o.KeptBy), listed)
		}
		return []core.UnpackOutput{o}
	})
	return a.copiesOfLocked(changed)
}

func (a *App) copiesOfLocked(ids []string) []taskCopy {
	out := make([]taskCopy, 0, len(ids))
	for _, id := range ids {
		out = append(out, a.copyLocked(a.tasks[id]))
	}
	return out
}

// ownOutputsLocked is what t's unpacking created that a removal of t with its
// files may delete: none of it is recorded by a task outside gone, holds what
// such a task recorded, or holds such a task's file. gone holds t and the
// tasks the same removal took before it. Caller holds a.mu.
func (a *App) ownOutputsLocked(t *core.Task, gone map[string]bool) []core.UnpackOutput {
	var out []core.UnpackOutput
	for _, o := range t.Unpacked {
		if !a.outputUsedByOtherLocked(o.Path, gone) {
			out = append(out, o)
		}
	}
	return out
}

func (a *App) outputUsedByOtherLocked(path string, gone map[string]bool) bool {
	holds := func(p string) bool { return samePath(p, path) || within(p, path) }
	for id, t := range a.tasks {
		if gone[id] {
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

// spared is what o's path holds now and why it stays there: it was kept by a
// removal from the list, cannot be read, or holds a file the unpacking did not
// write or one that changed since. why is "" when all of it may go, and ok is
// false when nothing is there.
func spared(o core.UnpackOutput) (now core.UnpackOutput, ok bool, why string) {
	now, ok, err := measureOutput(o.Path)
	switch {
	case !ok:
		return now, false, ""
	case len(o.KeptBy) > 0:
		return now, true, "a download that unpacked there as well was removed from the list with its files kept"
	case err != nil:
		return now, true, fmt.Sprintf("it could not be read to see what it holds: %v", err)
	}
	for rel, size := range now.Files {
		if was, ok := o.Files[rel]; !ok || was != size {
			return now, true, "it holds files its unpacking did not write"
		}
	}
	return now, true, ""
}

// dropUnpacked deletes what an unpacking created, unless spared says it
// stays. It runs off a.mu.
func dropUnpacked(o core.UnpackOutput, taskID string) {
	_, ok, why := spared(o)
	switch {
	case !ok:
		return
	case why != "":
		log.Printf("left %s where it is: %s%s", o.Path, why, taskTag(taskID))
		return
	}
	if err := os.RemoveAll(o.Path); err != nil {
		log.Printf("could not delete %s: %v%s", o.Path, err, taskTag(taskID))
	}
}

// UnpackedWeight is how many files and bytes of what unpacking wrote a removal
// of ids with their files would delete, taking them in that order as
// RemoveTasks does.
func (a *App) UnpackedWeight(ids []string) (files int, bytes int64) {
	a.mu.Lock()
	gone := map[string]bool{}
	var doomed []core.UnpackOutput
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil || gone[id] {
			continue
		}
		gone[id] = true
		doomed = append(doomed, a.ownOutputsLocked(t, gone)...)
	}
	a.mu.Unlock()

	// Outer folders first, so what lies inside one that goes is counted once.
	slices.SortFunc(doomed, func(x, y core.UnpackOutput) int { return len(x.Path) - len(y.Path) })
	var counted []string
	for _, o := range doomed {
		if slices.ContainsFunc(counted, func(p string) bool { return samePath(o.Path, p) || within(o.Path, p) }) {
			continue
		}
		now, ok, why := spared(o)
		if !ok || why != "" {
			continue
		}
		counted = append(counted, o.Path)
		for _, size := range now.Files {
			files++
			bytes += size
		}
	}
	return files, bytes
}
