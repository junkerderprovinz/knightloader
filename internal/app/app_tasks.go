package app

// One task at a time: reading the list, editing a task, checking a link, taking
// a task away, and the two helpers everything else uses to persist a change and
// put it on screen.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// Tasks returns a snapshot sorted oldest-first.
func (a *App) Tasks() []*core.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*core.Task, 0, len(a.tasks))
	for _, t := range a.tasks {
		c := *t
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// SetPackage moves tasks into a package (an empty name ungroups them).
func (a *App) SetPackage(ids []string, pkg string) {
	pkg = strings.TrimSpace(pkg)
	a.mu.Lock()
	var copies []core.Task
	for _, id := range ids {
		if t := a.tasks[id]; t != nil {
			t.Package = pkg
			copies = append(copies, *t)
		}
	}
	a.mu.Unlock()
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// setAvailability records what a check learned about a link. It is separate
// from Update because availability is a property of the link, not of a download
// attempt: a staged link can be known-dead before anything is started.
func (a *App) setAvailability(id string, avail core.Availability, msg string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	t.Online = avail
	// The probe answers a question about the link; the error field on a settled
	// task answers a different one, about what happened to it. A HEAD started
	// while the link sat in the collector routinely lands after the dispatcher has
	// already refused the task — for a filter rule, or a destination that was
	// taken — and letting it write here replaces that reason with "offline: ...",
	// or, on a link that turned out to be fine, with nothing at all.
	if t.Status != core.StatusError {
		t.Error = msg
	}
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// RecheckTasks re-runs resolution and the availability probe for collected
// tasks, so a link that was dead an hour ago can be tried again without
// re-pasting it. An empty id list rechecks everything in the collector.
func (a *App) RecheckTasks(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0

	a.mu.Lock()
	var targets []core.Task
	for id, t := range a.tasks {
		if t.Status == core.StatusCollected && (all || want[id]) {
			targets = append(targets, *t)
		}
	}
	a.mu.Unlock()

	for i := range targets {
		t := targets[i]
		res := a.Registry.For(t.URL)
		if res == nil {
			a.setAvailability(t.ID, core.AvailOffline, "no backend handles this link")
			continue
		}
		result, err := res.Resolve(context.Background(), resolver.Request{URL: t.URL})
		if err != nil {
			a.setAvailability(t.ID, core.AvailOffline, err.Error())
			continue
		}
		a.mu.Lock()
		if live := a.tasks[t.ID]; live != nil {
			live.Resolver = res.Info().ID
			if result.Name != "" {
				live.Name = result.Name
			}
		}
		a.mu.Unlock()
		if res.Info().ID == "direct" {
			a.analyze(t.ID, result.DirectURL)
		} else {
			// Other backends cannot probe without starting; clear a stale error
			// and let the attempt decide.
			a.setAvailability(t.ID, core.AvailUnknown, "")
		}
	}
}

// analyze probes a plain file link with a HEAD request to fill in its size and
// flag it offline, updating the collected task in place.
func (a *App) analyze(id, rawurl string) {
	req, err := http.NewRequest(http.MethodHead, rawurl, nil)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.setAvailability(id, core.AvailOffline, "offline: "+err.Error())
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		a.setAvailability(id, core.AvailOffline, "offline (HTTP "+strconv.Itoa(resp.StatusCode)+")")
		return
	}
	a.setAvailability(id, core.AvailOnline, "")
	if resp.ContentLength > 0 {
		a.onUpdate(id, core.Update{Size: resp.ContentLength})
	}
}

// publishTasks writes tasks that are already settled to the store and out to
// every connected browser. It is what a caller holding mu cannot do itself.
func (a *App) publishTasks(tasks []core.Task) {
	for i := range tasks {
		c := tasks[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// TriBool is a bool a request may also send as null. Inside an options struct a
// *bool cannot express that: absent and null both decode to nil, and here the
// two have to mean different things. Absent is "leave this alone", null is
// "back to inheriting the global", true and false are the override itself.
//
// The three values exist because the alternative is a data loss nobody would
// connect to the release that caused it. Auto-extract is nullable in the store,
// so every task already in it inherits the global switch; a plain bool would
// decode "no opinion" as false and quietly stop unpacking for the whole list.
type TriBool struct {
	// Set is whether the field was present in the request at all.
	Set bool
	// Value is the override, or nil for "inherit" when Set is true.
	Value *bool
}

func (t *TriBool) UnmarshalJSON(b []byte) error {
	t.Set = true
	if string(b) == "null" {
		t.Value = nil
		return nil
	}
	var v bool
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	t.Value = &v
	return nil
}

// MarshalJSON keeps the round trip honest for anything that echoes an options
// struct back: an override nobody set is null, never false.
func (t TriBool) MarshalJSON() ([]byte, error) { return json.Marshal(t.Value) }

// TaskOptions are the per-task overrides the UI can set. A nil field means
// "leave as it is", which keeps a partial edit from wiping the other values.
type TaskOptions struct {
	Dir      *string `json:"dir,omitempty"`
	Password *string `json:"password,omitempty"`
	// Filename is the name the finished file is put under, and it is deliberately
	// not Name: the backend downloads under the name it chose and the file is
	// renamed once the bytes have stopped moving. The engine keys its .part file
	// on its own name, so a file renamed mid-flight cannot be resumed after a
	// restart. An empty string takes the override off again.
	Filename *string `json:"filename,omitempty"`
	// Chunks is how many connections this download opens. Zero hands the decision
	// back to the resolver's answer and the built-in default.
	Chunks *int `json:"chunks,omitempty"`
	// AutoExtract is the per-task unpacking switch, read at extraction time
	// rather than at download time — turning it on for something that finished an
	// hour ago unpacks it now.
	AutoExtract TriBool `json:"autoExtract"`
}

// SetTaskOptions applies per-task overrides. Changing the folder of a running
// task only affects a later restart — the bytes already on disk stay where they
// are — but a rename and the unpacking switch are acted on immediately for
// anything that has already finished, because a per-task setting that does
// nothing to the task you are looking at is a setting the user re-saves and
// never sees work.
func (a *App) SetTaskOptions(ids []string, o TaskOptions) error {
	// Everything is checked before a single task is touched. Refusing halfway
	// through would leave a selection with the first eight rows edited, the rest
	// as they were, and an error message that says nothing about which is which.
	if o.Dir != nil && *o.Dir != "" {
		if err := settings.Validate(*o.Dir); err != nil {
			return err
		}
	}
	var newName string
	if o.Filename != nil {
		newName = strings.TrimSpace(*o.Filename)
		if newName != "" && !usableFilename(newName) {
			return fmt.Errorf("%q is not a file name; it has to be a single path segment", newName)
		}
	}
	if o.Chunks != nil && (*o.Chunks < 0 || *o.Chunks > rules.MaxChunks) {
		return fmt.Errorf("chunk count %d is outside 0..%d", *o.Chunks, rules.MaxChunks)
	}

	a.mu.Lock()
	touched := map[string]*core.Task{}
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		if o.Dir != nil {
			t.Dir = strings.TrimSpace(*o.Dir)
		}
		if o.Password != nil {
			t.Password = strings.TrimSpace(*o.Password)
		}
		if o.Chunks != nil {
			t.Chunks = *o.Chunks
		}
		if o.Filename != nil {
			t.Filename = newName
			if t.Status == core.StatusDone {
				a.renameFinishedLocked(t)
			}
		}
		if o.AutoExtract.Set {
			// Written across the whole volume set rather than onto this part alone.
			// The first volume is the one that gets opened, so an override sitting
			// only on part02 is an unpacking switch that silently does nothing.
			for _, part := range a.volumeSetLocked(t) {
				// A copy per task, never the request's own pointer: one pointer shared
				// by five rows is five rows that change together the next time anything
				// writes through it.
				part.AutoExtract = copyBool(o.AutoExtract.Value)
				touched[part.ID] = part
			}
		}
		touched[t.ID] = t
	}
	// Extraction is decided only after every override in the request has landed,
	// so switching a whole multi-volume selection on in one call reads the set as
	// the user left it and not as it stood halfway through the loop.
	if o.AutoExtract.Set {
		cfg := a.Settings.Get()
		for _, id := range ids {
			if t := a.tasks[id]; t != nil {
				if target := a.extractNowLocked(t, cfg); target != nil {
					touched[target.ID] = target
				}
			}
		}
	}
	copies := make([]core.Task, 0, len(touched))
	for _, t := range touched {
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return nil
}

// copyBool detaches a caller's pointer, so a value written onto several tasks
// is several values.
func copyBool(v *bool) *bool {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// usableFilename reports whether a name is one path segment and nothing else. A
// name carrying a separator is not a name, it is a way out of the folder the
// download was meant to land in.
func usableFilename(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

// renameFinishedLocked puts a finished download under the name a Packagizer
// rule or the user asked for, which is the only way a rename action reaches the
// disk. It runs once the bytes have stopped moving and never before, because
// the engine keys its .part file on the name it chose itself and a file renamed
// mid-flight cannot be resumed after a restart.
//
// A rename that cannot be carried out leaves the file alone and says so on the
// task. Keeping the old name in silence is the failure worth guarding against:
// the list would show the name the rule promised while the disk holds another,
// and extraction and checksum verification both build their path by joining the
// folder with that name. Caller holds a.mu.
func (a *App) renameFinishedLocked(t *core.Task) {
	want := strings.TrimSpace(t.Filename)
	// A task whose name is still its URL has not been resolved, so there is no
	// file under the old name to move.
	if want == "" || want == t.Name || t.Name == "" || t.Name == t.URL || !filesAreLocal(t) {
		return
	}
	if !usableFilename(want) {
		t.Error = "not renamed: " + strconv.Quote(want) + " is not a single file name"
		return
	}
	if len(a.volumeSetLocked(t)) > 1 {
		// Renaming one part out of a multi-volume set is how an archive becomes
		// impossible to open: extract.SetKey groups the parts by their names, and a
		// rule with a fixed name would hand every part the same one and overwrite
		// the whole set down to a single file.
		t.Error = "not renamed: " + t.Name + " is one part of a multi-volume archive"
		return
	}
	dir := a.dirFor(t)
	to := filepath.Join(dir, want)
	// Checked rather than left to Rename, which on most platforms replaces the
	// destination without a word. The file already sitting there belongs to
	// somebody, and a rule is not a reason to destroy it.
	if _, err := os.Stat(to); err == nil {
		t.Error = "not renamed: " + to + " already exists"
		return
	}
	if err := os.Rename(filepath.Join(dir, t.Name), to); err != nil {
		t.Error = "not renamed: " + err.Error()
		return
	}
	t.Name = want
}

// saveAndBroadcast persists task snapshots and pushes them to connected UIs.
func (a *App) saveAndBroadcast(copies []core.Task) {
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// Remove drops a task from the list. deleteFiles additionally erases what was
// downloaded — never the default: tidying the list must not destroy finished
// files, which is also how JDownloader behaves.
func (a *App) Remove(id string, deleteFiles bool) {
	a.mu.Lock()
	t := a.tasks[id]
	// Unfiled before the task goes, or a deleted download keeps blocking its own
	// re-add for the life of the process.
	a.forgetLinkLocked(t)
	delete(a.tasks, id)
	delete(a.active, id)
	delete(a.started, id)
	a.dequeueLocked(id)
	a.dispatchLocked()
	a.mu.Unlock()
	if t != nil {
		a.backendFor(t.Resolver).Remove(id, deleteFiles)
	}
	_ = a.Store.Delete(id)
	a.Hub.Broadcast("removed", map[string]string{"id": id})
}

// put stages a task: the one moment a link becomes real, entering the task map,
// the store and every connected browser at once.
//
// The mirror check happens here rather than at the call site because the
// decision and the insert have to be one critical section. Two pastes of the
// same file that both finished resolving would otherwise both be told the link
// is new, which is the one case a check before the lock cannot catch.
//
// It reports the entry that refused the link, so the caller can say which
// download it was folded into instead of dropping it in silence.
func (a *App) put(t *core.Task) (dedupe.Match, bool) {
	a.mu.Lock()
	if m := a.dupes.Check(linkEntry(t)); m.Seen() {
		a.mu.Unlock()
		return m, false
	}
	if t.ID == "" {
		t.ID = a.freshIDLocked()
	}
	a.tasks[t.ID] = t
	a.dupes.Add(linkEntry(t))
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
	return dedupe.Match{}, true
}

// linkEntry is how a task is described to the mirror set. The name is passed as
// it stands: an unresolved task's name is still its URL, and the set recognises
// that as "not known yet" rather than comparing two links on it.
func linkEntry(t *core.Task) dedupe.Entry {
	return dedupe.Entry{ID: t.ID, URL: t.URL, Name: t.Name, Size: t.Size}
}

// forgetLinkLocked takes a task's link back out of the mirror set, but only
// while the set still points at that task. A settled download the user re-added
// has been replaced in the set by its successor, and removing it by URL alone
// would unblock a third copy of a link that is live right now. Caller holds mu.
func (a *App) forgetLinkLocked(t *core.Task) {
	if t == nil || a.dupes == nil {
		return
	}
	if m := a.dupes.Check(dedupe.Entry{URL: t.URL}); m.Verdict == dedupe.Duplicate && m.Of.ID == t.ID {
		a.dupes.Remove(t.URL)
	}
}

// verifyTask checks a finished file against a checksum, when one is available:
// a hash in the file name, or a sums file that was downloaded alongside it. A
// download nobody can verify is left unmarked rather than shown as passing,
// because a green tick that means "not checked" is worse than no tick.
func (a *App) verifyTask(id, path string) {
	name := filepath.Base(path)
	dir := filepath.Dir(path)

	var sum checksum.Sum
	if s, ok := checksum.FromName(name); ok {
		sum = s
	} else if s, ok := a.sumFromSiblingFile(dir, name); ok {
		sum = s
	} else {
		return
	}

	ok, err := checksum.Verify(path, sum)
	verdict := "ok"
	if err != nil {
		log.Printf("checksum %s: %v", name, err)
		return
	}
	if !ok {
		verdict = "failed"
		log.Printf("checksum mismatch for %s", name)
	}

	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	t.Checksum = verdict
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// sumFromSiblingFile looks for a checksum listing that arrived with the batch
// and pulls this file's entry out of it.
func (a *App) sumFromSiblingFile(dir, name string) (checksum.Sum, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return checksum.Sum{}, false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		var parse func(io.Reader) ([]checksum.Sum, error)
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".sfv":
			parse = checksum.ParseSFV
		case ".md5", ".sha1", ".sha256", ".sha256sum", ".md5sum":
			parse = checksum.ParseHashFile
		default:
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		sums, err := parse(f)
		f.Close()
		if err != nil {
			// Both parsers are strict: one malformed line yields nothing. Left
			// silent, that is indistinguishable from "no checksum file here",
			// and every download in the batch would quietly show as unverified.
			log.Printf("checksum file %s is unusable: %v", e.Name(), err)
			continue
		}
		for _, s := range sums {
			if strings.EqualFold(filepath.Base(s.Name), name) {
				return s, true
			}
		}
	}
	return checksum.Sum{}, false
}
