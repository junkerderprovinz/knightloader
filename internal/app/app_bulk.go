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
// when deleteFiles is set.
func (a *App) RemoveTasks(ids []string, deleteFiles bool) []string {
	removed := make([]string, 0, len(ids))
	for _, id := range ids {
		a.mu.Lock()
		_, known := a.tasks[id]
		a.mu.Unlock()
		if !known {
			continue
		}
		// Remove also unfiles the mirror set, clears backend state and frees a
		// dispatch slot.
		a.Remove(id, deleteFiles)
		removed = append(removed, id)
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
		return a.selectLocked(func(t *core.Task) bool { return !t.Enabled }), nil
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
func duplicateKey(t *core.Task) string {
	if t.Name != "" && t.Name != t.URL && t.Size > 0 {
		return fmt.Sprintf("file\x00%s\x00%d", strings.ToLower(t.Name), t.Size)
	}
	if t.URL != "" {
		return "url\x00" + strings.ToLower(t.URL)
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

// ContainerBackendConfigured reports whether anything can open an encrypted
// container. It is asked before an upload is stored, so the upload can be
// refused with the reason.
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
