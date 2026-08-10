package jd

import (
	"fmt"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Backend performs delegated downloads through headless JD and mirrors the live
// progress into KnightLoader tasks via onUpdate. It satisfies the same contract
// as the Gopeed engine (Download/Pause/Resume/Remove), so the app treats both
// backends the same way.
type Backend struct {
	c        *Client
	onUpdate func(taskID string, u core.Update)

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
// list stayed empty. Waiting is also why the links are handed back as plain URLs
// instead of being started in JD — coming back through the ordinary staging path
// means the link filter, the packagizer and the duplicate check all still apply
// to them, which they would not if JD simply started downloading.
func (b *Backend) AddContainer(url, packageName string, timeout time.Duration) ([]string, error) {
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
func (b *Backend) AddCryptedV1(data []byte, packageName string, timeout time.Duration) ([]string, error) {
	marker := fmt.Sprintf("KL-%d", time.Now().UnixNano())
	if _, err := b.c.AddContainerData("dlc", data, marker); err != nil {
		return nil, err
	}
	return b.awaitContainerLinks(marker, timeout)
}

// awaitContainerLinks polls JD's link grabber for the package named marker,
// waits for it to settle, harvests the plain URLs out of it and removes it so
// JD does not start it itself. Shared by AddContainer and AddCryptedV1, whose
// only difference is how the container's bytes reach JD in the first place.
func (b *Backend) awaitContainerLinks(marker string, timeout time.Duration) ([]string, error) {
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

	urls := make([]string, 0, len(links))
	for _, l := range links {
		if l.URL != "" {
			urls = append(urls, l.URL)
		}
	}
	// Best effort: we have the links, and failing to tidy JD's grabber is not a
	// reason to tell the user their container did not open.
	_ = b.c.RemoveCrawledPackage(pkg)
	return urls, nil
}

// Download hands the link to JD (auto-crawl + start) and polls its progress.
func (b *Backend) Download(taskID, url string, _ map[string]string, _ int) {
	go func() {
		if _, err := b.c.AddLinks(url, b.pkgName(taskID), true); err != nil {
			b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: " + err.Error()})
			return
		}
		b.poll(taskID)
	}()
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

			puuid, err := b.c.PackageUUID(pkg)
			if err != nil || puuid == 0 {
				continue // still crawling / not in the download list yet
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

func (b *Backend) Pause(taskID string)  { b.setEnabled(taskID, false) }
func (b *Backend) Resume(taskID string) { b.setEnabled(taskID, true) }

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
