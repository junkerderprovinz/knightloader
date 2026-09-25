package app

// Operations on a whole selection, and the cleanup classes that work out the
// selection themselves. Each takes ids in and returns the ids it touched, so
// the client does not have to refetch everything to find out.

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// SetEnabled switches links on or off. A disabled link keeps its place,
// progress and package; everything that starts downloads passes it over.
func (a *App) SetEnabled(ids []string, enabled bool) []string {
	return a.editAndDispatch(ids, func(t *core.Task) { t.Enabled = enabled })
}

// SetHold parks links or releases them. Hold is separate from StatusPaused so
// that "resume everything" leaves held links alone.
func (a *App) SetHold(ids []string, hold bool) []string {
	return a.editAndDispatch(ids, func(t *core.Task) { t.Hold = hold })
}

// SetForced marks links to be started ahead of the limits.
func (a *App) SetForced(ids []string, forced bool) []string {
	return a.editAndDispatch(ids, func(t *core.Task) { t.Forced = forced })
}

// PauseTasks pauses the running and waiting links among ids and leaves every
// other link as it is, so a whole package can be named without staged,
// finished or unpacking links changing state. Nothing is dispatched until all
// of them are out of the queue, or the slot the first one frees would go to
// the next one on the list.
func (a *App) PauseTasks(ids []string) []string {
	type pausing struct{ id, resolver string }
	a.mu.Lock()
	var stopped []pausing
	var touched []string
	var copies []core.Task
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil || (t.Status != core.StatusRunning && t.Status != core.StatusQueued) {
			continue
		}
		if a.active[id] {
			delete(a.active, id)
			stopped = append(stopped, pausing{id, t.Resolver})
		}
		a.dequeueLocked(id)
		t.Status = core.StatusPaused
		t.Speed = 0
		t.StalledSince = time.Time{}
		touched = append(touched, id)
		copies = append(copies, *t)
	}
	if len(touched) > 0 {
		a.dispatchLocked()
	}
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	// As in stop: the state the app commanded is recorded first, and a later
	// event from the backend can still correct it.
	for _, p := range stopped {
		a.backendFor(p.resolver).Pause(p.id)
	}
	return touched
}

// ResumeTasks puts the paused links among ids back in the wait queue and
// leaves the rest alone.
func (a *App) ResumeTasks(ids []string) []string {
	a.mu.Lock()
	var resumed []*core.Task
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil || t.Status != core.StatusPaused || a.active[id] {
			continue
		}
		t.Status = core.StatusQueued
		t.Speed = 0
		a.dequeueLocked(id)
		a.queue = append(a.queue, id)
		resumed = append(resumed, t)
	}
	if len(resumed) > 0 {
		a.dispatchLocked()
	}
	// Copied after dispatching, as in startTasks.
	copies := make([]core.Task, 0, len(resumed))
	for _, t := range resumed {
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return idsOf(resumed)
}

// editAndDispatch is editAll for flags the dispatcher reads, followed by a
// dispatch pass so that switching a link back on starts it right away.
func (a *App) editAndDispatch(ids []string, edit func(*core.Task)) []string {
	touched := a.editAll(ids, edit)
	if len(touched) > 0 {
		a.mu.Lock()
		a.dispatchLocked()
		a.mu.Unlock()
	}
	return touched
}

// editAll applies edit to every named task under one lock and publishes the
// result. edit runs with a.mu held and must not block.
func (a *App) editAll(ids []string, edit func(*core.Task)) []string {
	a.mu.Lock()
	touched := make([]string, 0, len(ids))
	copies := make([]core.Task, 0, len(ids))
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		edit(t)
		touched = append(touched, id)
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return touched
}

// RemoveTasks removes a selection in one call, deleting downloaded files only
// when deleteFiles is set. A yt-dlp link that loses its last shown row loses
// its set-aside rows too (see strandedBy); they are not in the answer, which
// counts the rows somebody could see.
func (a *App) RemoveTasks(ids []string, deleteFiles bool) []string {
	aside := a.strandedBy(ids)
	removed := make([]string, 0, len(ids))
	wake := false
	for _, id := range ids {
		a.mu.Lock()
		_, known := a.tasks[id]
		a.mu.Unlock()
		if !known {
			continue
		}
		// removeTask also unfiles the mirror set, clears backend state and frees
		// a dispatch slot.
		if a.removeTask(id, deleteFiles) {
			wake = true
		}
		removed = append(removed, id)
	}
	// No countdown counts a set-aside row, so these need no wake.
	for _, id := range aside {
		a.removeTask(id, false)
	}
	if wake {
		a.wakeAutoConfirm()
	}
	return removed
}

// CleanupClass is one "clean up" entry: a rule that selects tasks for the user.
type CleanupClass string

const (
	// CleanupFinished is every completed download. It never touches files on
	// disk.
	CleanupFinished CleanupClass = "finished"
	// CleanupOffline is links the host says are gone.
	CleanupOffline CleanupClass = "offline"
	// CleanupDisabled is links that are switched off.
	CleanupDisabled CleanupClass = "disabled"
	// CleanupDuplicates is the same file staged more than once. The copy
	// furthest along is kept.
	CleanupDuplicates CleanupClass = "duplicates"
	// CleanupIncompleteArchives is every part of a multi-volume set that has a
	// dead part and so can never be unpacked.
	CleanupIncompleteArchives CleanupClass = "incompleteArchives"
)

// CleanupClasses lists the classes the menu is built from.
func CleanupClasses() []CleanupClass {
	return []CleanupClass{
		CleanupFinished, CleanupOffline, CleanupDisabled,
		CleanupDuplicates, CleanupIncompleteArchives,
	}
}

// CleanupPreview reports which tasks a cleanup class would remove, so the
// confirmation can name them.
func (a *App) CleanupPreview(class CleanupClass) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cleanupTargetsLocked(class)
}

// Cleanup removes everything in a class and returns what it removed.
func (a *App) Cleanup(class CleanupClass, deleteFiles bool) ([]string, error) {
	ids, err := a.CleanupPreview(class)
	if err != nil {
		return nil, err
	}
	return a.RemoveTasks(ids, deleteFiles), nil
}

// cleanupTargetsLocked works out the selection for a class. Caller holds a.mu.
func (a *App) cleanupTargetsLocked(class CleanupClass) ([]string, error) {
	switch class {
	case CleanupFinished:
		return a.selectLocked(func(t *core.Task) bool { return t.Status == core.StatusDone }), nil
	case CleanupOffline:
		// Only a definite "gone"; a hoster refusing a probe must not make a
		// package disappear.
		return a.selectLocked(func(t *core.Task) bool { return t.Online == core.AvailOffline }), nil
	case CleanupDisabled:
		// A row a preset set aside is switched off too, but nobody sees it, and
		// removing it would leave nothing to bring back when the kind is ticked.
		return a.selectLocked(func(t *core.Task) bool { return !t.Enabled && !t.VariantOff }), nil
	case CleanupDuplicates:
		return a.duplicatesLocked(), nil
	case CleanupIncompleteArchives:
		return a.incompleteArchivesLocked(), nil
	}
	return nil, fmt.Errorf("%q is not a cleanup class; the app knows %s", class, joinClasses(CleanupClasses()))
}

// selectLocked returns every task keep accepts, in list order. Caller holds
// a.mu.
func (a *App) selectLocked(keep func(*core.Task) bool) []string {
	var out []*core.Task
	for _, t := range a.tasks {
		if keep(t) {
			out = append(out, t)
		}
	}
	sortByAge(out)
	ids := make([]string, 0, len(out))
	for _, t := range out {
		ids = append(ids, t.ID)
	}
	return ids
}

// duplicatesLocked returns every copy but the best of each duplicated
// download. The mirror set refuses duplicates at paste time, but a finished or
// failed download does not block a re-add. Caller holds a.mu.
func (a *App) duplicatesLocked() []string {
	groups := map[string][]*core.Task{}
	for _, t := range a.tasks {
		k := duplicateKey(t)
		groups[k] = append(groups[k], t)
	}
	var out []string
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		sortByAge(g)
		// Keep the copy with the most bytes, or the oldest when none has
		// started.
		keep := 0
		for i, t := range g {
			if t.Loaded > g[keep].Loaded {
				keep = i
			}
		}
		for i, t := range g {
			if i != keep {
				out = append(out, t.ID)
			}
		}
	}
	sort.Strings(out)
	return out
}

// duplicateKey identifies a download: by file name and size, which matches the
// same file on two hosters, else by URL, else by id so it groups with nothing.
//
// The variant kind is part of both keys. The rows of one yt-dlp link share
// its URL and its name, and the rows whose size is not known yet would
// otherwise all read as copies of one file, so confirming the link would hold
// back all but one of them.
func duplicateKey(t *core.Task) string {
	kind, _ := variantDecode(t.Variant)
	if t.Name != "" && t.Name != t.URL && t.Size > 0 {
		return fmt.Sprintf("file\x00%s\x00%s\x00%d", kind, strings.ToLower(t.Name), t.Size)
	}
	if t.URL != "" {
		return "url\x00" + string(kind) + "\x00" + strings.ToLower(t.URL)
	}
	return "id\x00" + t.ID
}

// incompleteArchivesLocked returns every task of a multi-volume set with a dead
// part (failed or offline). Sets whose parts are merely still running are left
// alone. Caller holds a.mu.
func (a *App) incompleteArchivesLocked() []string {
	sets := map[string][]*core.Task{}
	for _, t := range a.tasks {
		key, isVolume := extract.SetKey(t.Name)
		if !isVolume {
			continue
		}
		// Two releases with the same part names in different folders are two
		// sets.
		k := a.dirFor(t) + "\x00" + key
		sets[k] = append(sets[k], t)
	}
	var out []string
	for _, g := range sets {
		broken := false
		for _, t := range g {
			if t.Status == core.StatusError || t.Online == core.AvailOffline {
				broken = true
				break
			}
		}
		if !broken {
			continue
		}
		for _, t := range g {
			out = append(out, t.ID)
		}
	}
	sort.Strings(out)
	return out
}

// sortByAge sorts a selection in list order, so a preview and the removal that
// follows agree.
func sortByAge(in []*core.Task) {
	sort.Slice(in, func(i, j int) bool {
		if !in[i].CreatedAt.Equal(in[j].CreatedAt) {
			return in[i].CreatedAt.Before(in[j].CreatedAt)
		}
		return in[i].ID < in[j].ID
	})
}

// joinClasses lists the known classes for an error message.
func joinClasses(in []CleanupClass) string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		out = append(out, string(c))
	}
	return strings.Join(out, ", ")
}

// containerAdder is a backend that can open an encrypted link container. Only
// the shipped headless JD can, because the encrypted formats need a key issued
// to registered clients.
type containerAdder interface {
	AddContainer(url, packageName string, timeout time.Duration) ([]resolver.Result, error)
}

// containerCrawlLimit is how long the backend gets to open a container. A
// container can carry a captcha that JD waits on, but the relay URL it fetches
// from expires.
const containerCrawlLimit = 3 * time.Minute

// ErrNoContainerBackend is returned for an encrypted container when no JD
// backend is configured to open it.
var ErrNoContainerBackend = fmt.Errorf(
	"this container is encrypted, and only the headless JDownloader backend can open it; " +
		"none is configured (set KL_JD to a reachable JD)")

// ErrJDOff is returned for an encrypted container while JDownloader is
// switched off on the modules page.
var ErrJDOff = errors.New(
	"this container is encrypted, and only the headless JDownloader backend can open it; " +
		"\"JDownloader backend\" is switched off on the Modules page")

// ContainerBackendConfigured reports whether a JD backend that can open an
// encrypted container is wired, switched on or not. It is asked before an
// upload is stored, so the upload can be refused with the reason.
func (a *App) ContainerBackendConfigured() bool {
	a.bmu.RLock()
	be := a.jd
	a.bmu.RUnlock()
	_, ok := be.(containerAdder)
	return ok
}

// HandContainerToJD gives the JD backend a URL to fetch an encrypted container
// from, since JD usually runs in another container and takes links, not local
// paths. It returns once JD has the handover and stages the links on a
// goroutine, because JD may wait for a captcha. A failure is recorded where the
// user looks for links that did not make it.
func (a *App) HandContainerToJD(rawurl, name, pkg string) error {
	if a.ModuleOff("jd") {
		return ErrJDOff
	}
	a.bmu.RLock()
	be := a.jd
	a.bmu.RUnlock()
	adder, ok := be.(containerAdder)
	if !ok {
		return ErrNoContainerBackend
	}
	// a.spawn, so Close waits for the store writes below.
	a.spawn(func() {
		links, err := adder.AddContainer(rawurl, pkg, containerCrawlLimit)
		if err != nil {
			log.Printf("container %s: %v", name, err)
			a.recordSkippedReason(name, "container", err.Error())
			return
		}
		// The ordinary path, so the filter, Packagizer and duplicate check
		// apply, keeping the names and sizes JD's crawl found.
		created := a.AddResolvedLinksFrom(links, pkg, OriginContainer)
		log.Printf("container %s: %d links, %d staged", name, len(links), len(created))
	})
	return nil
}

// cryptedV1Adder is a backend that accepts a Click'n'Load v1 ("addcrypted")
// payload inline. The payload only ever exists as a form field, so there is no
// URL to hand over; the shipped JD's Deprecated API takes the bytes directly.
type cryptedV1Adder interface {
	AddCryptedV1(data []byte, packageName string, timeout time.Duration) ([]resolver.Result, error)
}

// CryptedV1BackendConfigured reports whether Click'n'Load's addcrypted can be
// served, so the listener only claims support it has.
func (a *App) CryptedV1BackendConfigured() bool {
	if a.ModuleOff("jd") {
		return false
	}
	a.bmu.RLock()
	be := a.jd
	a.bmu.RUnlock()
	_, ok := be.(cryptedV1Adder)
	return ok
}

// AddContainerCnL implements the Click'n'Load listener's ContainerAdder. The
// "crypted" field is encrypted against JDownloader's own key, so only the JD
// backend can open it. JDownloader itself writes the field to a temporary .dlc
// (ExternInterfaceImpl#addcrypted); this passes the bytes inline instead.
func (a *App) AddContainerCnL(data []byte, pkg string) error {
	if len(data) == 0 {
		return errors.New("no crypted content")
	}
	if a.ModuleOff("jd") {
		return ErrJDOff
	}
	a.bmu.RLock()
	be := a.jd
	a.bmu.RUnlock()
	adder, ok := be.(cryptedV1Adder)
	if !ok {
		return ErrNoContainerBackend
	}
	a.spawn(func() {
		links, err := adder.AddCryptedV1(data, pkg, containerCrawlLimit)
		if err != nil {
			log.Printf("addcrypted (v1): %v", err)
			a.recordSkippedReason("Click'n'Load (addcrypted v1)", "container", err.Error())
			return
		}
		created := a.AddResolvedLinksFrom(links, pkg, OriginCnL)
		log.Printf("addcrypted (v1): %d links, %d staged", len(links), len(created))
	})
	return nil
}

// UIState and SetUIState store opaque interface state between reloads, such as
// column widths and folded packages, so a new column needs no schema change.
func (a *App) UIState(key string) (string, error) { return a.Store.UIState(key) }

func (a *App) SetUIState(key, value string) error { return a.Store.SetUIState(key, value) }
