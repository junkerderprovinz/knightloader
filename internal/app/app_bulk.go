package app

// Operations on a whole selection, and the cleanup classes that work out the
// selection themselves. Everything here is a list of ids in and a list of ids
// out: the interface says which tasks it means, or which kind of tasks, and gets
// back what was actually touched — never a bare 204 that leaves it re-fetching
// the world to find out.

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/extract"
)

// SetEnabled switches links on or off. A disabled link keeps its place, its
// progress and its package; it is simply passed over by everything that starts
// downloads, which is what makes it different from removing it.
func (a *App) SetEnabled(ids []string, enabled bool) []string {
	return a.editAndDispatch(ids, func(t *core.Task) { t.Enabled = enabled })
}

// SetHold parks links, or lets them go again. Hold is deliberately not
// StatusPaused: "resume everything" must not start the links somebody chose to
// leave alone, and with one shared state there is no way to tell the two apart.
func (a *App) SetHold(ids []string, hold bool) []string {
	return a.editAndDispatch(ids, func(t *core.Task) { t.Hold = hold })
}

// SetForced marks links to be started ahead of the limits — the answer to
// "everything else can wait, fetch this one".
func (a *App) SetForced(ids []string, forced bool) []string {
	return a.editAndDispatch(ids, func(t *core.Task) { t.Forced = forced })
}

// editAndDispatch is editAll for the flags the dispatcher consults. Switching a
// link back on has to be able to start it: the dispatcher only skips what is off
// when it next runs, so without this the link sits queued until something
// unrelated — a finished download, a pasted link — happens to drive the queue
// again, and switching it on looks like it did nothing.
func (a *App) editAndDispatch(ids []string, edit func(*core.Task)) []string {
	touched := a.editAll(ids, edit)
	if len(touched) > 0 {
		a.mu.Lock()
		a.dispatchLocked()
		a.mu.Unlock()
	}
	return touched
}

// editAll applies one small change to every named task under a single lock and
// publishes the result. The edit runs with a.mu held, so it must not do anything
// that blocks: this is for setting flags, not for talking to a backend.
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

// RemoveTasks takes a selection off the list in one call, optionally deleting
// what was downloaded. It exists because the interface's "clear the list" is one
// gesture: a request per row turns a thousand-link list into a thousand round
// trips, a thousand store writes and a thousand broadcasts.
//
// deleteFiles is never the default and is never implied by anything else.
// Tidying the list must not destroy finished downloads.
func (a *App) RemoveTasks(ids []string, deleteFiles bool) []string {
	removed := make([]string, 0, len(ids))
	for _, id := range ids {
		a.mu.Lock()
		_, known := a.tasks[id]
		a.mu.Unlock()
		if !known {
			continue
		}
		// Through Remove rather than around it: it is the one place that unfiles
		// the link from the mirror set, clears the backend's own state and frees a
		// dispatch slot, and a bulk path that skipped any of those would leave the
		// list clean and the app wrong.
		a.Remove(id, deleteFiles)
		removed = append(removed, id)
	}
	return removed
}

// CleanupClass is one of the "clean up…" entries: a rule for working out which
// tasks the user means without making them select anything.
type CleanupClass string

const (
	// CleanupFinished is every completed download. It is the entry people use
	// daily, and the one that must never touch the files on disk.
	CleanupFinished CleanupClass = "finished"
	// CleanupOffline is links the host says are gone.
	CleanupOffline CleanupClass = "offline"
	// CleanupDisabled is links switched off and left switched off.
	CleanupDisabled CleanupClass = "disabled"
	// CleanupDuplicates is the same file staged more than once. One copy of each
	// stays — the one furthest along, so a half-finished download is never the
	// one thrown away in favour of an untouched second copy.
	CleanupDuplicates CleanupClass = "duplicates"
	// CleanupIncompleteArchives is multi-volume sets that can never be unpacked
	// because one of the parts is dead. Removing the survivors is the point: a
	// set missing one volume is not a partial download, it is a folder of bytes
	// that will never open.
	CleanupIncompleteArchives CleanupClass = "incompleteArchives"
)

// CleanupClasses is the list the menu is built from, so a button that exists is
// always a class the app implements.
func CleanupClasses() []CleanupClass {
	return []CleanupClass{
		CleanupFinished, CleanupOffline, CleanupDisabled,
		CleanupDuplicates, CleanupIncompleteArchives,
	}
}

// CleanupPreview reports which tasks a cleanup class would take, without taking
// them. Every one of these classes can select more than the user pictured, and a
// confirmation that can only say "12 downloads" is a confirmation nobody reads.
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
		// Only a host that answered and said the file is gone. Unknown and
		// uncheckable are deliberately left alone: one hoster refusing a probe must
		// never be the reason a package disappears.
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

// selectLocked is every task the test accepts, in list order so that two calls
// with the same list produce the same answer. Caller holds a.mu.
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

// duplicatesLocked finds tasks that are second copies of another task in the
// list and returns all but the best copy of each.
//
// The mirror set refuses a duplicate at the moment it is pasted, so these are
// the ones it cannot catch: a download that finished or failed stops blocking
// its own re-add, deliberately, and a link pasted again after that becomes a
// second row. Caller holds a.mu.
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
		// The keeper is the copy with the most bytes on disk, and the oldest of
		// those when nothing has been downloaded yet. Keeping the newest would
		// throw away a download that is halfway there in favour of one that has
		// not started.
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

// duplicateKey is what makes two rows the same download. The file name and byte
// count come first because that is what recognises the same file fetched from
// two hosters; the URL is the fallback while nothing has been resolved yet, and
// a task with neither gets its own id so it can never be grouped with anything.
func duplicateKey(t *core.Task) string {
	if t.Name != "" && t.Name != t.URL && t.Size > 0 {
		return fmt.Sprintf("file\x00%s\x00%d", strings.ToLower(t.Name), t.Size)
	}
	if t.URL != "" {
		return "url\x00" + strings.ToLower(t.URL)
	}
	return "id\x00" + t.ID
}

// incompleteArchivesLocked finds multi-volume sets that can never be unpacked
// and returns every task in them.
//
// A set counts as broken when one of its parts is dead — failed, or reported
// gone by the host. The rest of the set is then not "downloads in progress", it
// is bytes that will never open, and leaving them is how a download folder fills
// up with archives nobody can extract. A set whose parts are merely still
// running is left alone. Caller holds a.mu.
func (a *App) incompleteArchivesLocked() []string {
	sets := map[string][]*core.Task{}
	for _, t := range a.tasks {
		key, isVolume := extract.SetKey(t.Name)
		if !isVolume {
			continue
		}
		// Keyed on the destination as well, because two unrelated releases with the
		// same part naming going to two folders are two sets, not one.
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

// sortByAge puts a selection in the order the list shows it, so that a preview
// and the removal that follows name the tasks in the same order.
func sortByAge(in []*core.Task) {
	sort.Slice(in, func(i, j int) bool {
		if !in[i].CreatedAt.Equal(in[j].CreatedAt) {
			return in[i].CreatedAt.Before(in[j].CreatedAt)
		}
		return in[i].ID < in[j].ID
	})
}

// joinClasses names the classes in an error, so a client sending a class this
// build does not have is told which ones it does have.
func joinClasses(in []CleanupClass) string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		out = append(out, string(c))
	}
	return strings.Join(out, ", ")
}

// containerAdder is a backend that can be handed an encrypted link container to
// open. Only the shipped headless JD can: the encrypted formats need a key that
// is issued to registered clients, and JD holds one legitimately.
type containerAdder interface {
	AddContainer(url, packageName string, timeout time.Duration) ([]string, error)
}

// containerCrawlLimit is how long the backend gets to open a container. Minutes
// rather than seconds: a container can carry a captcha, and JD sits on that
// until it is answered. It is not unbounded, because the relay URL the backend
// fetches from expires and a wait past that point can only fail.
const containerCrawlLimit = 3 * time.Minute

// ErrNoContainerBackend is an encrypted container with nowhere to send it. The
// wording matters: this is not a broken file and not an unsupported format, it
// is a file this instance deliberately does not decrypt itself and has no
// backend configured to decrypt for it.
var ErrNoContainerBackend = fmt.Errorf(
	"this container is encrypted, and only the headless JDownloader backend can open it; " +
		"none is configured (set KL_JD to a reachable JD)")

// ContainerBackendConfigured reports whether anything here can open an
// encrypted container. It is asked before an upload is stored anywhere, so an
// instance with no JD can refuse with the reason instead of preparing a
// handover nobody will ever collect.
func (a *App) ContainerBackendConfigured() bool {
	a.bmu.RLock()
	be := a.jd
	a.bmu.RUnlock()
	_, ok := be.(containerAdder)
	return ok
}

// HandContainerToJD gives the JD backend a URL to fetch an encrypted container
// from. The URL rather than the file: JD's API takes links, and a filesystem
// path would have to exist on JD's own machine, which in the normal deployment
// is a different container.
// HandContainerToJD gives an encrypted container to the backend that can open
// it and stages whatever comes back.
//
// It returns as soon as the backend has accepted the handover, and finishes the
// job on a goroutine: a container can hold a captcha, and JD waits for that to
// be answered, which is not something an HTTP request should sit through. The
// links appear in the list when they appear — the interface is already live on
// the task stream, so that is the feedback, and a failure is recorded where the
// user already looks for links that did not make it.
func (a *App) HandContainerToJD(rawurl, name, pkg string) error {
	a.bmu.RLock()
	be := a.jd
	a.bmu.RUnlock()
	adder, ok := be.(containerAdder)
	if !ok {
		return ErrNoContainerBackend
	}
	go func() {
		urls, err := adder.AddContainer(rawurl, pkg, containerCrawlLimit)
		if err != nil {
			log.Printf("container %s: %v", name, err)
			// Recorded rather than logged only: a container that opened into
			// nothing is exactly the case where the user is left staring at an
			// unchanged list wondering whether the upload arrived.
			a.recordSkippedReason(name, "container", err.Error())
			return
		}
		// Back through the ordinary path, so the link filter, the packagizer and
		// the duplicate check apply to a container's contents exactly as they do
		// to a paste. A container is a delivery mechanism, not an exemption.
		created := a.AddLinks(urls, pkg)
		log.Printf("container %s: %d links, %d staged", name, len(urls), len(created))
	}()
	return nil
}

// UIState and SetUIState carry whatever the interface needs to remember between
// reloads: column widths, which packages are folded shut, the last page. The
// blob is opaque here on purpose — a settings field per column would mean a
// schema change, a migration and a translated label every time a list gains one.
func (a *App) UIState(key string) (string, error) { return a.Store.UIState(key) }

func (a *App) SetUIState(key, value string) error { return a.Store.SetUIState(key, value) }
