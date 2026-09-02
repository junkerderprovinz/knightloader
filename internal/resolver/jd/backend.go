package jd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Backend performs delegated downloads through headless JD and mirrors the live
// progress into KnightLoader tasks via onUpdate. It satisfies the same contract
// as the Gopeed engine (Download/Pause/Resume/Remove), so the app treats both
// backends the same way.
type Backend struct {
	c        *Client
	onUpdate func(taskID string, u core.Update)

	// Dir answers "where must this task's file land". Nil, or an empty answer,
	// leaves the folder to JD - which is only ever right for a JD somebody else
	// runs and configured themselves.
	//
	// A callback rather than a parameter on Download, because the app's shared
	// backend interface has no destination in it and the two other delegated
	// backends do not need one. This is the same shape ytdlp.Backend.Dir already
	// uses, deliberately: one pattern for "a backend that writes files needs to
	// be told where", not two.
	Dir func(taskID string) string

	mu   sync.Mutex
	stop map[string]chan struct{} // taskID -> poll stopper
}

func NewBackend(base string, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		c:        NewClient(base),
		onUpdate: onUpdate,
		stop:     map[string]chan struct{}{},
	}
}

// SetDownloadFolder points the JD behind this backend at path. See
// Client.SetDownloadFolder for why this is not optional.
func (b *Backend) SetDownloadFolder(path string) error { return b.c.SetDownloadFolder(path) }

func (b *Backend) dirFor(taskID string) string {
	if b.Dir == nil {
		return ""
	}
	return b.Dir(taskID)
}

// Reachable reports whether the configured JD instance answers.
func (b *Backend) Reachable() error { return b.c.Ping() }

func (b *Backend) pkgName(taskID string) string { return "KL-" + taskID }

// pollInterval paces awaitContainerLinks's polling of JD's link grabber. A
// var, not a const, so a test does not have to sit through it for real (the
// same convention internal/provision's stopGrace already uses).
var pollInterval = time.Second

// AddContainer hands JD an encrypted link container to open — a DLC, CCF or
// RSDF, which need a key issued to registered clients and which JD holds one
// for.
//
// It takes a URL and not a path because that is what JD accepts: the API's
// addLinks takes links, and a filesystem path would have to name a file on JD's
// own machine, which in the normal deployment is a different container
// altogether. The caller serves the uploaded bytes over HTTP and passes that
// address.
//
// autostart is false. The decrypted links land in JD's LinkGrabber, where they
// can be looked at, rather than starting a download of an unknown number of
// files on a machine whose queue the user is not looking at.
// AddContainer opens an encrypted link container and returns the links inside
// it. JD holds the key; we hold the download list.
//
// The crawl is waited out rather than fired and forgotten, which is what the
// first version did: JD decrypted the container perfectly into its own grabber
// and nothing ever read it back, so the upload reported success and the user's
// list stayed empty. Waiting is also why the links are handed back as
// resolver.Results instead of being started in JD - coming back through the
// ordinary staging path means the link filter, the packagizer and the
// duplicate check all still apply to them, which they would not if JD simply
// started downloading. Each Result carries the crawl's own Name and Size, not
// only the URL, so the collector does not have to wait for a second crawl at
// download time to learn what this one already found.
func (b *Backend) AddContainer(url, packageName string, timeout time.Duration) ([]resolver.Result, error) {
	// A name of our own, not the caller's: the caller's package name is where
	// the links should land in OUR list, while this one exists only to find them
	// again in JD's. Using the caller's would collide the moment two containers
	// were opened into the same package.
	marker := fmt.Sprintf("KL-%d", time.Now().UnixNano())
	if _, err := b.c.AddContainerLinks(url, marker); err != nil {
		return nil, err
	}
	return b.awaitContainerLinks(marker, timeout)
}

// AddCryptedV1 hands JD a Click'n'Load v1 ("addcrypted") submission's raw
// content and waits for it exactly as AddContainer does. There is no URL to
// submit it by: unlike a .dlc a user saves and later uploads, this payload
// was never a file anywhere, only one CnL form field, so it goes in as inline
// content (see Client.AddContainerData) instead of a fetchable address. The
// wait-and-harvest half is otherwise identical, which is the point — this is
// AddContainer's own reasoning ("JD holds the key; we hold the download
// list"), not a second mechanism for the same problem.
func (b *Backend) AddCryptedV1(data []byte, packageName string, timeout time.Duration) ([]resolver.Result, error) {
	marker := fmt.Sprintf("KL-%d", time.Now().UnixNano())
	if _, err := b.c.AddContainerData("dlc", data, marker); err != nil {
		return nil, err
	}
	return b.awaitContainerLinks(marker, timeout)
}

// awaitContainerLinks polls JD's link grabber for the package named marker,
// waits for it to settle, harvests the links (URL, name and size) out of it
// and removes it so JD does not start it itself. Shared by AddContainer and
// AddCryptedV1, whose only difference is how the container's bytes reach JD
// in the first place.
func (b *Backend) awaitContainerLinks(marker string, timeout time.Duration) ([]resolver.Result, error) {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()

	var pkg int64
	var links []CrawledLink
	// A crawler that yields incrementally reports "not collecting" in the gaps
	// between its own sub-crawls, so one quiet reading means nothing. Measured
	// on a real DLC: at one second the package held 1 of its 11 links and the
	// crawler was momentarily idle, and harvesting there took that single link
	// and threw the other ten away. The count has to hold still instead.
	settled := 0
	const settledEnough = 3
	for {
		<-tick.C

		// The package appears only once the crawl has produced something, which
		// is also why "is it still collecting?" cannot be asked first: right
		// after the container is handed over JD has not started yet, so it
		// answers no, and a harvest there reads an empty grabber and concludes
		// the container was empty. The package existing is the real starting
		// gun.
		if pkg == 0 {
			var err error
			if pkg, err = b.c.CrawledPackageUUID(marker); err != nil {
				return nil, err
			}
		}
		if pkg != 0 {
			busy, err := b.c.Collecting()
			if err != nil {
				return nil, err
			}
			found, err := b.c.CrawledLinks(pkg)
			if err != nil {
				return nil, err
			}
			switch {
			case busy || len(found) == 0 || len(found) != len(links):
				// Still moving: either the crawler says so, or the count just
				// changed under us. Either way the container is not all here.
				settled = 0
			default:
				settled++
			}
			links = found
			if settled >= settledEnough {
				break
			}
		}

		if time.Now().After(deadline) {
			_ = b.c.RemoveCrawledPackage(pkg)
			if pkg == 0 {
				return nil, fmt.Errorf("jd did not open the container within %s", timeout)
			}
			return nil, fmt.Errorf("jd opened the container but produced no links within %s", timeout)
		}
	}

	// Name and Size ride along rather than being dropped here and re-learned at
	// download time: this crawl already answered both (the same numbers JD's
	// own link-grabber window would show), and a caller that discards them
	// only forces JD to crawl the identical links a second time to say the
	// same thing again - which is exactly what left the collector showing the
	// bare URL and no size until the download itself started.
	out := make([]resolver.Result, 0, len(links))
	for _, l := range links {
		if l.URL != "" {
			out = append(out, resolver.Result{DirectURL: l.URL, Name: l.Name, Size: l.Size})
		}
	}
	// Best effort: we have the links, and failing to tidy JD's grabber is not a
	// reason to tell the user their container did not open.
	_ = b.c.RemoveCrawledPackage(pkg)
	return out, nil
}

// CheckLinks asks JD's own hoster plugins whether a batch of plain links is
// still there, without downloading or unlocking anything: stages them under a
// private marker package, waits for the crawl to settle the same way
// awaitContainerLinks already does, reads back each entry's availability, then
// removes the package again so JD's own link-grabber window is not left
// holding what was only ever a question.
//
// This exists because a generic HTTP probe cannot answer the question for a
// premium hoster - see app_tasks.go's analyze, which is deliberately never
// used for a JD-routed link, because an anonymous response often looks the
// same whether the file is there or not. JD's own plugin for that specific
// host knows the difference (measured live against rapidgator.net: a real
// link came back ONLINE, a fabricated one OFFLINE, neither needing a premium
// account) - asking it is the only way to get a verdict that is not a guess
// without KnightLoader growing hoster-specific code of its own.
//
// ctx bounds the wait instead of a fixed timeout constant: the caller
// (app.runCheck) already sets one deadline for the whole batch, and a second,
// independent one here could time this method out first while runCheck is
// still willing to wait, or the reverse.
func (b *Backend) CheckLinks(ctx context.Context, urls []string) ([]core.Availability, error) {
	marker := fmt.Sprintf("KL-check-%d", time.Now().UnixNano())
	if _, err := b.c.AddPlainLinks(strings.Join(urls, "\n"), marker); err != nil {
		return nil, err
	}

	var pkg int64
	var links []CrawledLink
	settled := 0
	const settledEnough = 3
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if pkg != 0 {
				_ = b.c.RemoveCrawledPackage(pkg)
			}
			return nil, ctx.Err()
		case <-tick.C:
		}

		if pkg == 0 {
			var err error
			if pkg, err = b.c.CrawledPackageUUID(marker); err != nil {
				return nil, err
			}
		}
		if pkg == 0 {
			continue // the package appears only once the crawl has produced something
		}

		busy, err := b.c.Collecting()
		if err != nil {
			return nil, err
		}
		found, err := b.c.CrawledLinks(pkg)
		if err != nil {
			return nil, err
		}
		if busy || len(found) != len(urls) {
			settled = 0
		} else {
			settled++
		}
		links = found
		if settled >= settledEnough {
			break
		}
	}
	_ = b.c.RemoveCrawledPackage(pkg)

	// Keyed by the URL JD echoed back rather than by position - the same
	// defence AllDebrid's own CheckLinks already needs (see
	// internal/resolver/debrid/alldebrid.go): a link this loop never hears
	// about again must not silently shift every verdict after it onto the
	// wrong link.
	verdict := make(map[string]core.Availability, len(links))
	for _, l := range links {
		verdict[l.URL] = jdAvailability(l.Availability)
	}
	out := make([]core.Availability, len(urls))
	for i, u := range urls {
		out[i] = verdict[u]
	}
	return out, nil
}

// jdAvailability maps JD's own answer to this app's Availability. Anything
// but the two verdicts JD actually states - TEMP_UNKNOWN, UNKNOWN, or simply
// absent because the link never settled - stays uncheckable rather than
// guessed at either way.
func jdAvailability(jd string) core.Availability {
	switch jd {
	case "ONLINE":
		return core.AvailOnline
	case "OFFLINE":
		return core.AvailOffline
	default:
		return core.AvailUncheckable
	}
}

// Download hands the link to JD (auto-crawl + start) and polls its progress.
func (b *Backend) Download(taskID, url string, _ map[string]string, _ int) {
	go func() {
		if _, err := b.c.AddLinks(url, b.pkgName(taskID), b.dirFor(taskID), true); err != nil {
			b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: " + err.Error()})
			return
		}
		b.poll(taskID)
	}()
}

// fatalPackageStatus reports whether JD's package status is a standing refusal
// rather than a passing condition - something no amount of waiting fixes.
//
// It matters because of the shape of the failure it was written for. JD reports
// "Invalid download directory" as a PACKAGE status and nowhere else: the links
// underneath look ordinary, so the poller aggregated them into a perfectly
// healthy "running at 0 bytes" and sat there. The task held a concurrency slot,
// the row said nothing was wrong, and after forty-five minutes the only thing
// anybody was told was "no progress for 45m0s" - a sentence about the symptom
// that names neither the cause nor anything to do about it.
//
// Matched on the substring rather than the whole string because JD appends
// detail to some of these, and case-insensitively because it is a display
// string, not an enum. Deliberately a SHORT list of conditions that are
// certainly permanent: anything not on it keeps the old patient behaviour,
// since being wrong here fails a download that would have worked.
func fatalPackageStatus(status string) bool {
	s := strings.ToLower(status)
	for _, fatal := range []string{
		"invalid download directory",
		"no write permission",
		"not enough space",
	} {
		if strings.Contains(s, fatal) {
			return true
		}
	}
	return false
}

func (b *Backend) poll(taskID string) {
	stop := make(chan struct{})
	b.mu.Lock()
	b.stop[taskID] = stop
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.stop, taskID)
		b.mu.Unlock()
	}()

	pkg := b.pkgName(taskID)
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()

	// Two separate patiences, because they answer two different questions.
	// appearBy asks whether JD ever accepted the link at all — crawling and
	// captchas take minutes, not hours. stall asks whether a download that did
	// start has stopped moving. A single wall-clock deadline conflated the two
	// and killed healthy multi-hour downloads at the thirty-minute mark.
	appearBy := time.Now().Add(appearLimit)
	var seen bool
	var pinned bool
	var lastBytes int64
	lastMoved := time.Now()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !seen && time.Now().After(appearBy) {
				b.onUpdate(taskID, core.Update{
					Status: core.StatusError,
					Err:    "jd: the link never reached JDownloader's download list",
				})
				return
			}
			if seen && time.Since(lastMoved) > stallLimit {
				b.onUpdate(taskID, core.Update{
					Status: core.StatusError,
					Err:    "jd: no progress for " + stallLimit.String(),
				})
				return
			}

			p, err := b.c.Package(pkg)
			if err != nil || p == nil || p.UUID == 0 {
				continue // still crawling / not in the download list yet
			}
			puuid := p.UUID
			// Said out loud the moment JD says it, instead of being waited out.
			if fatalPackageStatus(p.Status) {
				b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: " + p.Status})
				return
			}
			// The folder is pinned ONCE, on the tick the package first appears.
			// addLinks' own destinationFolder is only the parent JD hangs the
			// package name under, so without this the file lands in a folder named
			// after a task id that means nothing to anybody.
			if !pinned {
				pinned = true
				if dir := b.dirFor(taskID); dir != "" {
					// Best effort: a folder JD refuses to move to is a worse reason to
					// abandon a download than to let it finish where JD put it.
					_ = b.c.SetPackageDirectory(dir, []int64{puuid})
				}
			}
			links, err := b.c.QueryDownloads(puuid)
			if err != nil || len(links) == 0 {
				continue
			}
			seen = true

			// JD may have crawled one link into several files. Reporting only
			// the first would show a fraction of the real size and call the
			// task done while the rest is still downloading, so the whole
			// package is summed instead.
			u := aggregate(links)
			if u.Loaded != lastBytes {
				lastBytes = u.Loaded
				lastMoved = time.Now()
			}
			b.onUpdate(taskID, u)
			if u.Status == core.StatusDone {
				return
			}
		}
	}
}

// appearLimit is how long JD gets to turn a submitted link into a download.
// Crawling, container decryption and a captcha all happen in here.
const appearLimit = 15 * time.Minute

// stallLimit is how long a started download may make no progress before it is
// given up on. Generous on purpose: a hoster cool-down is minutes, and JD
// handles its own waiting.
const stallLimit = 45 * time.Minute

// aggregate folds every file JD produced for one link into a single update, so
// the size and progress shown are the package's, not the first file's.
func aggregate(links []DownloadLink) core.Update {
	u := core.Update{Status: core.StatusRunning, Name: links[0].Name}
	done := 0
	for i := range links {
		u.Size += links[i].BytesTotal
		u.Loaded += links[i].BytesLoaded
		u.Speed += links[i].Speed
		if links[i].Finished || links[i].Status == "Finished" {
			done++
		}
	}
	if len(links) > 1 {
		u.Name = fmt.Sprintf("%s (+%d)", links[0].Name, len(links)-1)
	}
	if done == len(links) {
		u.Status = core.StatusDone
		u.Speed = 0
	}
	return u
}

// Pause disables the links in JD and stops watching them, and the second half
// is not housekeeping.
//
// Left running, the poller keeps reporting whatever JD's list says every 750 ms
// - and a link JD has merely been asked to disable is still in that list. Every
// tick therefore sent "running" for a task the user had just stopped. It also
// runs into the wrong end: `stallLimit` turns a watched download that stops
// moving into an error after 45 minutes, so a task somebody paused deliberately
// would report "jd: no progress for 45m0s" three quarters of an hour later,
// with nothing on screen connecting it to a button pressed before lunch.
//
// The app guards against the first half too (see onUpdate's `stale`), and it
// has to: that guard covers every backend, including ones written later. This
// is the other half of the same fix - not polling at all beats reporting into a
// guard - and it is the half that stops the false error.
//
// Resume DOES need a counterpart, and getting that wrong here cost a day. See
// Resume's own comment below.
func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	if s, ok := b.stop[taskID]; ok {
		close(s)
		delete(b.stop, taskID)
	}
	b.mu.Unlock()
	b.setEnabled(taskID, false)
}

// Resume re-enables the links in JD AND starts watching them again.
//
// The second half is a regression of my own making, found the same day it
// shipped. Pause used to leave the poller running; closing it there was right
// for the reasons its own comment gives, and it took away something the code
// was quietly relying on somewhere else: dispatchLocked does NOT hand an
// already-started task back to Start. It puts it straight into a.active and
// calls Resume (app_dispatch.go, the `a.started[id]` branch), on the assumption
// that whatever was watching it still is.
//
// So after the change, a JD task that was stopped and started again took a
// slot, told JD to carry on, and had nothing left to report on it. It sat at
// "running" with zero bytes for ever, holding a place in the concurrency limit
// that nothing would ever free. Two of exactly those were sitting on the live
// instance while this was written.
//
// The lesson is the one worth keeping: **when a fix removes something, ask what
// else was carrying it.** The poller was not only reporting progress; it was
// also the thing that made the resume path work at all.
func (b *Backend) Resume(taskID string) {
	b.setEnabled(taskID, true)
	b.mu.Lock()
	_, watched := b.stop[taskID]
	b.mu.Unlock()
	// Only when nobody is watching. Resume is also reachable while a poller is
	// still alive - a plain unpause that never went through the dispatcher - and
	// a second goroutine on the same task would double every reported byte.
	if !watched {
		go b.poll(taskID)
	}
}

func (b *Backend) setEnabled(taskID string, enabled bool) {
	ids := b.linkIDs(taskID)
	if len(ids) > 0 {
		_ = b.c.SetEnabled(enabled, ids)
	}
}

func (b *Backend) Remove(taskID string, _ bool) {
	b.mu.Lock()
	if s, ok := b.stop[taskID]; ok {
		close(s)
		delete(b.stop, taskID)
	}
	b.mu.Unlock()
	if puuid, err := b.c.PackageUUID(b.pkgName(taskID)); err == nil && puuid != 0 {
		_ = b.c.RemoveLinks(nil, []int64{puuid})
	}
}

func (b *Backend) linkIDs(taskID string) []int64 {
	puuid, err := b.c.PackageUUID(b.pkgName(taskID))
	if err != nil || puuid == 0 {
		return nil
	}
	links, err := b.c.QueryDownloads(puuid)
	if err != nil {
		return nil
	}
	ids := make([]int64, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.UUID)
	}
	return ids
}
