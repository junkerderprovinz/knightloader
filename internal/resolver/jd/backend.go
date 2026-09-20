package jd

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
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

	// Dir returns the folder a task's file must land in. Nil or "" leaves the
	// folder to JD. It is a callback, like ytdlp.Backend.Dir, because the
	// shared backend interface carries no destination.
	Dir func(taskID string) string

	mu   sync.Mutex
	stop map[string]chan struct{} // taskID -> poll stopper
	// held counts the link-grabber packages in use, by name; Download and its
	// poller can hold the same name at once. sweepGrabber treats every other
	// package in our namespace as abandoned.
	held map[string]int
	// filterSaid is whether the jobUUIDs verdict has already been logged.
	filterSaid bool
}

func NewBackend(base string, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		c:        NewClient(base),
		onUpdate: onUpdate,
		stop:     map[string]chan struct{}{},
		held:     map[string]int{},
	}
}

// SetDownloadFolder points the JD behind this backend at path (see
// Client.SetDownloadFolder).
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

// pollInterval paces the polling of JD's link grabber. Tests shorten it.
var pollInterval = time.Second

// ourGrabberPackage matches the names KnightLoader gives its own link-grabber
// packages: "KL-" plus a task id or a crawl marker's timestamp, and
// "KL-check-" plus a timestamp. It is kept tight because a match gets deleted
// and the grabber also holds the user's own packages.
var ourGrabberPackage = regexp.MustCompile(`^KL-(?:check-)?[0-9a-f]+$`)

// holdGrabber marks one grabber package name as in use by this backend, so the
// sweep leaves it alone. Every caller pairs it with releaseGrabber.
func (b *Backend) holdGrabber(name string) {
	b.mu.Lock()
	b.held[name]++
	b.mu.Unlock()
}

func (b *Backend) releaseGrabber(name string) {
	b.mu.Lock()
	if b.held[name] <= 1 {
		delete(b.held, name)
	} else {
		b.held[name]--
	}
	b.mu.Unlock()
}

// sweepGrabber removes KnightLoader's abandoned packages from JD's link
// grabber.
//
// JD's duplicate manager silently drops a crawled link the grabber already
// holds, and JD reloads the grabber on every start, so a leftover "KL-" package
// (from a link that went offline, a captcha JD gave up on, or a restart) makes
// every later container carrying that link open into nothing. Only packages in
// our name shape that no crawl, check or poller holds are removed.
func (b *Backend) sweepGrabber() {
	pkgs, err := b.c.CrawledPackages()
	if err != nil {
		return
	}
	b.mu.Lock()
	var stale []int64
	var names []string
	for _, p := range pkgs {
		if p.UUID == 0 || b.held[p.Name] > 0 || !ourGrabberPackage.MatchString(p.Name) {
			continue
		}
		stale = append(stale, p.UUID)
		names = append(names, p.Name)
	}
	b.mu.Unlock()
	if len(stale) == 0 {
		return
	}
	if err := b.c.RemoveCrawled(nil, stale); err != nil {
		log.Printf("jd: could not clear %d abandoned package(s) out of the link grabber: %v", len(stale), err)
		return
	}
	log.Printf("jd: cleared %d abandoned package(s) out of the link grabber (%s); JD drops new links that duplicate them",
		len(names), strings.Join(names, ", "))
}

// dropGrabberPackage removes one named package from the link grabber, if
// present: the targeted form of sweepGrabber for a known task.
func (b *Backend) dropGrabberPackage(name string) {
	pkgs, err := b.c.CrawledPackages()
	if err != nil {
		return
	}
	var ids []int64
	for _, p := range pkgs {
		if p.Name == name && p.UUID != 0 {
			ids = append(ids, p.UUID)
		}
	}
	if len(ids) == 0 {
		return
	}
	_ = b.c.RemoveCrawled(nil, ids)
}

// AddContainer has JD open an encrypted link container (DLC, CCF, RSDF; JD
// holds the keys) and returns the links inside it with their crawled name,
// size and availability.
//
// url is an HTTP address serving the uploaded bytes, since JD usually runs in
// another container and cannot read our paths. The links come back as
// Results instead of being started in JD, so the link filter, packagizer and
// duplicate check still apply to them.
func (b *Backend) AddContainer(url, packageName string, timeout time.Duration) ([]resolver.Result, error) {
	// A marker of our own, so two containers opened into the same package
	// cannot collide in JD's grabber.
	marker := fmt.Sprintf("KL-%d", time.Now().UnixNano())
	b.holdGrabber(marker)
	defer b.releaseGrabber(marker)
	b.sweepGrabber()
	job, err := b.c.AddContainerLinks(url, marker)
	if err != nil {
		return nil, err
	}
	return b.awaitContainerLinks(job, marker, timeout)
}

// AddCryptedV1 is AddContainer for a Click'n'Load v1 ("addcrypted")
// submission, whose payload has no URL and goes to JD as inline content (see
// Client.AddContainerData).
func (b *Backend) AddCryptedV1(data []byte, packageName string, timeout time.Duration) ([]resolver.Result, error) {
	marker := fmt.Sprintf("KL-%d", time.Now().UnixNano())
	b.holdGrabber(marker)
	defer b.releaseGrabber(marker)
	b.sweepGrabber()
	job, err := b.c.AddContainerData("dlc", data, marker)
	if err != nil {
		return nil, err
	}
	return b.awaitContainerLinks(job, marker, timeout)
}

// settleReadings is how many identical readings in a row count as a finished
// crawl. An incremental crawler pauses between sub-crawls; one second into a
// real DLC the package held 1 of its 11 links.
const settleReadings = 3

// awaitContainerLinks waits for one crawl job to finish, harvests every link
// it produced and removes them from JD's grabber so JD does not start them.
//
// The crawl is followed by job id and marker name together (see
// Client.AddContainerLinks). It counts as finished once its link count stays
// unchanged for settleReadings rounds; JD's global isCollecting flag only
// adds one confirming reading.
func (b *Backend) awaitContainerLinks(job int64, marker string, timeout time.Duration) ([]resolver.Result, error) {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()

	var pkgs []int64
	var links []CrawledLink
	settled := 0
	probe := b.newJobFilterProbe()
	for {
		<-tick.C

		found, foundPkgs, err := b.crawlOutput(job, marker, probe)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 || len(found) != len(links) {
			settled = 0
		} else {
			settled++
		}
		links, pkgs = found, foundPkgs

		// isCollecting can only lengthen the wait by one reading, since it
		// also reads false between sub-crawls.
		need := settleReadings
		if settled >= need && b.collecting() {
			need++
		}
		if settled >= need {
			break
		}

		if time.Now().After(deadline) {
			// A crawl that never settles, such as one slow hoster among forty
			// links, keeps what it has found by the deadline. Finding nothing
			// at all is an error, and its usual cause is JD's duplicate manager
			// silently dropping links already in JD's own lists; our leftovers
			// were swept before the handover, so the message points the user
			// at theirs.
			if len(links) == 0 {
				if len(pkgs) == 0 {
					return nil, fmt.Errorf(
						"jd accepted the container and produced no links within %s. "+
							"JDownloader silently drops any link it already has, so the usual cause is "+
							"that these files are already in JDownloader's own link grabber or download list: "+
							"remove them there, then upload the container again", timeout)
				}
				_ = b.c.RemoveCrawled(nil, pkgs)
				return nil, fmt.Errorf(
					"jd opened the container into an empty package within %s. "+
						"JDownloader silently drops any link it already has, so the usual cause is "+
						"that these files are already in JDownloader's own link grabber or download list: "+
						"remove them there, then upload the container again", timeout)
			}
			log.Printf("jd: container crawl had not settled after %s, keeping the %d link(s) it had by then", timeout, len(links))
			break
		}
	}

	// The crawl already knows name, size and availability, so they travel
	// with the links instead of needing a second crawl.
	out := make([]resolver.Result, 0, len(links))
	for _, l := range links {
		if l.URL != "" {
			out = append(out, resolver.Result{
				DirectURL: l.URL,
				Name:      l.Name,
				Size:      l.Size,
				Available: statedAvailability(l.Availability),
			})
		}
	}
	// Best effort: the links are already harvested.
	_ = b.c.RemoveCrawled(linkIDs(links), pkgs)
	return out, nil
}

// linkIDs is the grabber ids of links we have read, for handing to
// RemoveCrawled alongside their packages.
func linkIDs(links []CrawledLink) []int64 {
	out := make([]int64, 0, len(links))
	for _, l := range links {
		out = append(out, l.UUID)
	}
	return out
}

// impossibleJob is an addLinks job id that cannot exist, since JD's job ids
// only count up. A query for it tests the filter, not a crawl.
const impossibleJob = -1

// jobFilterProbe remembers, for one crawl, whether this JD honours queryLinks's
// jobUUIDs filter.
//
// JD ignores query keys it does not know and answers with the whole link
// grabber, as revision 48637 does for jobUUIDs. Taking that for the crawl's
// output would adopt the user's own links as the container's contents and
// then delete them, so the filter is tested before it is used.
type jobFilterProbe struct {
	decided bool
	honours bool
	// announce logs an ignored filter, at most once per backend.
	announce func()
}

// newJobFilterProbe makes a probe for one crawl. It is asked again for every
// crawl because JD can be updated while KnightLoader runs.
func (b *Backend) newJobFilterProbe() *jobFilterProbe {
	return &jobFilterProbe{announce: b.sayFilterIgnoredOnce}
}

func (b *Backend) sayFilterIgnoredOnce() {
	b.mu.Lock()
	said := b.filterSaid
	b.filterSaid = true
	b.mu.Unlock()
	if !said {
		log.Printf("jd: this build ignores queryLinks's jobUUIDs filter; following the crawl by its package name alone")
	}
}

// usable answers whether the jobUUIDs filter can be trusted on this JD, by
// querying impossibleJob while the grabber holds something: a working filter
// answers nothing. With an empty grabber, or on an error, the question stays
// open for the next round.
func (p *jobFilterProbe) usable(c *Client, packagesInGrabber int) bool {
	if p.decided {
		return p.honours
	}
	if packagesInGrabber == 0 {
		return false
	}
	links, err := c.CrawledLinksForJob(impossibleJob)
	if err != nil {
		return false
	}
	p.decided, p.honours = true, len(links) == 0
	if !p.honours && p.announce != nil {
		p.announce()
	}
	return p.honours
}

// crawlOutput reads back what one crawl job has produced so far: its links and
// the grabber packages they sit in. Packages found by job id and by marker
// name are unioned, and the links are read per package so a container opening
// into several is read whole. Nothing outside those anchors is read, since the
// grabber also holds the user's own links.
func (b *Backend) crawlOutput(job int64, marker string, probe *jobFilterProbe) ([]CrawledLink, []int64, error) {
	inGrabber, err := b.c.CrawledPackages()
	if err != nil {
		return nil, nil, err
	}
	seen := map[int64]bool{}
	var pkgs []int64
	add := func(id int64) {
		if id != 0 && !seen[id] {
			seen[id] = true
			pkgs = append(pkgs, id)
		}
	}

	var byJob []CrawledLink
	if probe.usable(b.c, len(inGrabber)) {
		if byJob, err = b.c.CrawledLinksForJob(job); err != nil {
			return nil, nil, err
		}
		for _, l := range byJob {
			add(l.PackageUUID)
		}
	}
	for _, p := range inGrabber {
		if p.Name == marker {
			add(p.UUID)
		}
	}

	if len(pkgs) == 0 {
		// Nothing yet, or job links without a package; those are harvested as
		// they are and removed by link id.
		return byJob, nil, nil
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i] < pkgs[j] })
	found, err := b.c.CrawledLinks(pkgs...)
	if err != nil {
		return nil, nil, err
	}
	return found, pkgs, nil
}

// collecting is JD's global "is the grabber crawling" flag, used only as a
// hint; an error counts as not collecting.
func (b *Backend) collecting() bool {
	busy, err := b.c.Collecting()
	return err == nil && busy
}

// CheckLinks asks JD's hoster plugins whether a batch of plain links is still
// online, without downloading anything: it stages them under a private marker
// package, waits for the crawl to settle, reads each availability and removes
// the package again.
//
// An anonymous HTTP probe cannot tell a premium hoster's missing file from a
// login page, while JD's plugin can, even without an account. ctx carries the
// caller's deadline for the whole batch.
func (b *Backend) CheckLinks(ctx context.Context, urls []string) ([]core.Availability, error) {
	marker := fmt.Sprintf("KL-check-%d", time.Now().UnixNano())
	b.holdGrabber(marker)
	defer b.releaseGrabber(marker)
	job, err := b.c.AddPlainLinks(strings.Join(urls, "\n"), marker)
	if err != nil {
		return nil, err
	}

	var pkgs []int64
	var links []CrawledLink
	settled := 0
	probe := b.newJobFilterProbe()
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = b.c.RemoveCrawled(linkIDs(links), pkgs)
			return nil, ctx.Err()
		case <-tick.C:
		}

		found, foundPkgs, err := b.crawlOutput(job, marker, probe)
		if err != nil {
			// Leftovers would poison the grabber (see sweepGrabber).
			_ = b.c.RemoveCrawled(linkIDs(links), pkgs)
			return nil, err
		}
		links, pkgs = found, foundPkgs

		// The batch has a known length, so every link being present is the
		// settle condition.
		if len(found) != len(urls) {
			settled = 0
		} else {
			settled++
		}
		need := settleReadings
		if settled >= need && b.collecting() {
			need++
		}
		if settled >= need {
			break
		}
	}
	_ = b.c.RemoveCrawled(linkIDs(links), pkgs)

	// Matched by the echoed URL so a missing link cannot shift the verdicts.
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

// jdAvailability maps JD's answer to a check. Anything but ONLINE and OFFLINE
// (TEMP_UNKNOWN, UNKNOWN, absent) is uncheckable.
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

// statedAvailability is jdAvailability for a crawl that was not a check:
// anything but ONLINE and OFFLINE stays empty, meaning nobody has looked.
func statedAvailability(jd string) core.Availability {
	switch jd {
	case "ONLINE":
		return core.AvailOnline
	case "OFFLINE":
		return core.AvailOffline
	default:
		return ""
	}
}

// Download hands the link to JD (auto-crawl + start) and polls its progress.
// The task's own leftover package from an earlier attempt is dropped first,
// or JD's duplicate manager would silently drop the new submission.
func (b *Backend) Download(taskID, url string, _ map[string]string, _ int) {
	pkg := b.pkgName(taskID)
	b.holdGrabber(pkg)
	go func() {
		b.dropGrabberPackage(pkg)
		if _, err := b.c.AddLinks(url, pkg, b.dirFor(taskID), true); err != nil {
			b.releaseGrabber(pkg)
			b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: " + err.Error()})
			return
		}
		defer b.releaseGrabber(pkg)
		b.poll(taskID)
	}()
}

// fatalPackageStatus reports whether JD's package status is a permanent
// refusal that no waiting fixes. JD reports these only on the package, while
// its links look healthy at 0 bytes.
//
// The match is a case-insensitive substring because the status is display
// text with appended detail. The list holds only certainly permanent
// conditions, since a false match fails a download that would have worked.
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

// captchaSkipped reports whether JD has given up on a captcha rather than
// working on one. "Captcha recognition (rapidgator.net)" is in progress;
// "Skipped - Captcha is required" is final and would otherwise wait out
// stallLimit.
func captchaSkipped(status string) bool {
	s := strings.ToLower(status)
	return strings.Contains(s, "skipped") && strings.Contains(s, "captcha")
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
	// Held while this task is watched, so the sweep leaves a running crawl
	// alone.
	b.holdGrabber(pkg)
	defer b.releaseGrabber(pkg)
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()

	// Two separate limits: appearBy bounds how long JD may take to accept the
	// link, and stallLimit how long a started download may stand still. One
	// wall-clock deadline would kill healthy multi-hour downloads.
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
				// A leftover would poison the grabber (see sweepGrabber).
				b.dropGrabberPackage(pkg)
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
			if fatalPackageStatus(p.Status) {
				b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: " + p.Status})
				return
			}
			if captchaSkipped(p.Status) {
				b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: " + p.Status, Note: p.Status})
				return
			}
			// Pinned once, when the package first appears: addLinks'
			// destinationFolder only names the parent of a folder named after
			// the package.
			if !pinned {
				pinned = true
				if dir := b.dirFor(taskID); dir != "" {
					// Best effort: finishing in JD's folder beats failing.
					_ = b.c.SetPackageDirectory(dir, []int64{puuid})
				}
			}
			links, err := b.c.QueryDownloads(puuid)
			if err != nil || len(links) == 0 {
				continue
			}
			seen = true
			// JD reports states like "Captcha recognition" on the package, not
			// on its links.
			note := strings.TrimSpace(p.Status)

			u := aggregate(links)
			u.Note = note
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
// given up on. It is generous because JD handles hoster cool-downs itself.
const stallLimit = 45 * time.Minute

// aggregate folds every file JD produced for one link into a single update, so
// size and progress cover the whole package and the task is not done while
// some files still download.
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

// Pause disables the links in JD and stops the poller, which would otherwise
// keep reporting "running" and turn the pause into a stall error after
// stallLimit. The paused task keeps its grabber name held so the sweep cannot
// take a crawl that is still in progress; Resume gives the hold back.
func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	if s, ok := b.stop[taskID]; ok {
		// Taken before the poller stops, so the name is never unheld.
		b.held[b.pkgName(taskID)]++
		close(s)
		delete(b.stop, taskID)
	}
	b.mu.Unlock()
	b.setEnabled(taskID, false)
}

// Resume re-enables the links in JD and starts a poller again. The dispatcher
// resumes an already started task through Resume rather than Download, so
// without a new poller the task would sit at "running" for ever.
func (b *Backend) Resume(taskID string) {
	b.setEnabled(taskID, true)
	b.mu.Lock()
	_, watched := b.stop[taskID]
	b.mu.Unlock()
	// A second poller on the same task would double every reported byte.
	if !watched {
		// Pause stopped the last poller, so its hold is given back here.
		b.releaseGrabber(b.pkgName(taskID))
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
	pkg := b.pkgName(taskID)
	b.mu.Lock()
	if s, ok := b.stop[taskID]; ok {
		close(s)
		delete(b.stop, taskID)
	}
	// Every hold goes, Pause's included.
	delete(b.held, pkg)
	b.mu.Unlock()
	if puuid, err := b.c.PackageUUID(pkg); err == nil && puuid != 0 {
		_ = b.c.RemoveLinks(nil, []int64{puuid})
	}
	// A link that never reached the download list is still staged in the
	// grabber and would poison it (see sweepGrabber).
	b.dropGrabberPackage(pkg)
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
