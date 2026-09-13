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
	// held is the link-grabber packages this backend is presently using, by
	// name. Counted rather than flagged because one name is legitimately held
	// by two things at once: Download holds it across the handover and the
	// poller it starts holds it again for as long as it watches. A name in here
	// is live business; everything else in our own namespace is abandoned, and
	// that is the whole of what makes sweepGrabber safe.
	held map[string]int
	// filterSaid is whether the jobUUIDs verdict has already been logged. See
	// jobFilterProbe.announce.
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

// ourGrabberPackage matches the names KnightLoader gives its own link-grabber
// packages, and nothing else.
//
// Three shapes go in there and they are all of this form: "KL-" plus a task id
// (Backend.pkgName, sixteen lower-case hex digits from app.newID), "KL-" plus a
// nanosecond stamp (a container crawl's marker), and "KL-check-" plus one (a
// link check's marker). Deliberately tight: the grabber is shared with the
// user's own window, a package he named himself must never match, and the cost
// of being wrong here is deleting his links.
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

// sweepGrabber takes KnightLoader's own abandoned packages back out of JD's
// link grabber, and it is not housekeeping: it is the fix for a container that
// could never be opened a second time.
//
// JDownloader's duplicate manager drops a crawled link that is already in the
// grabber, and it does it silently - no package, not even an empty one, no
// error, no line in any log. KnightLoader stages every JD-routed download into
// that same grabber as "KL-<task id>" and, until this, never took one back out:
// Remove cleared the DOWNLOAD list only, and a link that never got that far
// (offline, a captcha JD gave up on, or its own leftover from a restart) stayed
// where it was. JD reloads the grabber at every start (GeneralSettings
// "savelinkgrabberlistenabled":true), so they accumulate for ever.
//
// Measured on the live instance, 2026-09-13: twenty-three such packages, and
// the Troja DLC whose nineteen links they held opened into NOTHING every single
// time - JD fetched it, decrypted it and produced no package at all. Deleting
// exactly one of the twenty-three and re-submitting the identical file produced
// a package holding exactly that one link. The leftovers were the bug.
//
// What is swept is only what nothing is using: a package in our own name shape
// (see ourGrabberPackage) that no crawl, check or poller currently holds. A
// download handed to JD moments ago and a second container being opened in
// parallel are both held, so both survive.
func (b *Backend) sweepGrabber() {
	pkgs, err := b.c.CrawledPackages()
	if err != nil {
		return // a grabber we cannot read is not one we may delete from
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

// dropGrabberPackage removes one named package from the link grabber, when it
// is there. The targeted counterpart to sweepGrabber, for the moments where the
// name is known: a task being removed, and a task being handed to JD that may
// still have its own leftover from a previous run sitting in the way.
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
	b.holdGrabber(marker)
	defer b.releaseGrabber(marker)
	// Before the container goes in, not after it has failed: JD silently drops
	// every crawled link its grabber already holds, and the links a container
	// carries are exactly the ones KnightLoader's own abandoned packages are
	// holding. See sweepGrabber - this is the difference between a container
	// that opens and one that opens into nothing for ever.
	b.sweepGrabber()
	job, err := b.c.AddContainerLinks(url, marker)
	if err != nil {
		return nil, err
	}
	return b.awaitContainerLinks(job, marker, timeout)
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
	b.holdGrabber(marker)
	defer b.releaseGrabber(marker)
	b.sweepGrabber() // see AddContainer: the same leftovers eat these links too
	job, err := b.c.AddContainerData("dlc", data, marker)
	if err != nil {
		return nil, err
	}
	return b.awaitContainerLinks(job, marker, timeout)
}

// settleReadings is how many identical readings in a row count as "the crawl
// has finished". A crawler that yields incrementally goes quiet between its own
// sub-crawls, so one unchanged reading means nothing. Measured on a real DLC: at
// one second the package held 1 of its 11 links, and harvesting there took that
// single link and threw the other ten away.
const settleReadings = 3

// awaitContainerLinks waits for one crawl job to finish, harvests every link it
// produced (URL, name, size and the crawl's own availability) and takes them
// back out of JD's grabber so JD does not start them itself. Shared by
// AddContainer and AddCryptedV1, whose only difference is how the container's
// bytes reach JD in the first place.
//
// The crawl is followed by its JOB and by the marker name, not by the name
// alone, because neither identifies a crawl on its own: the job filter does not
// care what JD named anything, and the marker name survives a JD whose jobUUIDs
// filter is useless. Both anchors are asked every round and their packages
// unioned (see Client.AddContainerLinks and CrawledLinksForJob).
//
// Note what this pair does NOT explain, because it was blamed for it once. A
// container that arrives here and produces nothing at all is not a container
// whose packages were named something we failed to look for: measured on the
// live instance on 2026-09-13, the Troja DLC that would not open created no
// package in JD's grabber under any name. Its links were already sitting there
// as KnightLoader's own abandoned packages, and JD's duplicate manager had
// dropped every one of them without a word. That is sweepGrabber's job, not
// this loop's.
//
// And the crawl is settled by its own link count standing still. JD's
// isCollecting is global to the instance: on one that is also chewing through
// Click'n'Load submissions it is true the whole time, so requiring it to go
// false is requiring something that is not about this crawl at all. It is kept
// only as a hint that buys one extra confirming reading - which is a delay, not
// a condition, and cannot hold a finished crawl up for ever.
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
			// Nothing yet, or the count just changed under us: the container is
			// not all here.
			settled = 0
		} else {
			settled++
		}
		links, pkgs = found, foundPkgs

		// Asked only when the count has already stood still, because that is the
		// only moment it could change anything: a JD that says it is collecting
		// buys one extra confirming reading, and a JD that says it is not cannot
		// shorten the wait, having been caught saying so between two sub-crawls
		// of the very container being waited for.
		need := settleReadings
		if settled >= need && b.collecting() {
			need++
		}
		if settled >= need {
			break
		}

		if time.Now().After(deadline) {
			// What is there at the deadline is kept, rather than thrown away
			// with an error (jdp, 2026-09-07: "Ich habe zwei Test-DLCs. einer
			// lädt rein und der andere wird abgebrochen nachdem der ladebalken
			// ewig gelaufen ist"). A crawl that never settles is not the same
			// as a crawl that found nothing: a container whose links point at a
			// hoster that answers slowly, or one link of forty that keeps the
			// count moving, left the other thirty-nine on the floor. Three
			// minutes of waiting followed by "nothing for you" is the worst of
			// both.
			//
			// Nothing found under either anchor is the genuinely empty case and
			// stays an error. What that error SAYS is the part that was worth
			// fixing: "jd did not open the container" is a sentence about the
			// symptom, and a user who has just watched a bar run for three
			// minutes is owed the one thing that helps instead (jdp,
			// 2026-09-13: "Heute laeuft ein Balken drei Minuten und danach
			// steht da nichts Brauchbares").
			//
			// There is one overwhelmingly likely reason, and it is now measured
			// rather than guessed: JD accepted the container, decrypted it, and
			// dropped every link in it because it already had them. Its
			// duplicate manager does that silently, so nothing in JD's own logs
			// mentions it either. KnightLoader has already cleared ITS share of
			// those (sweepGrabber, run before the handover), so what is left for
			// the user to clear is JD's own two lists.
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

	// Name and Size ride along rather than being dropped here and re-learned at
	// download time: this crawl already answered both (the same numbers JD's
	// own link-grabber window would show), and a caller that discards them
	// only forces JD to crawl the identical links a second time to say the
	// same thing again - which is exactly what left the collector showing the
	// bare URL and no size until the download itself started.
	//
	// The crawl's own availability rides along for the same reason: JD reports
	// it per link in this very answer, and dropping it left every row of a
	// freshly opened container grey until somebody pressed "Alle prüfen", which
	// then made JD crawl the identical links a second time to repeat what it
	// had already said (jdp, 2026-09-06: "bei dlc links funktioniert die status
	// anzeige immer noch nicht").
	out := make([]resolver.Result, 0, len(links))
	for _, l := range links {
		if l.URL != "" {
			out = append(out, resolver.Result{
				DirectURL: l.URL,
				Name:      l.Name,
				Size:      l.Size,
				// Only a stated verdict travels. jdAvailability answers
				// "uncheckable" for everything else, which is the right answer
				// to a CHECK somebody asked for and the wrong one here: a crawl
				// that simply did not mention this link has not looked at it,
				// and "nobody has looked" is the empty value.
				Available: statedAvailability(l.Availability),
			})
		}
	}
	// Every package the crawl opened into, and every link inside them - a
	// container that arrived as three packages and left two of them behind is
	// two packages JD is still free to start on its own.
	//
	// Best effort: we have the links, and failing to tidy JD's grabber is not a
	// reason to tell the user their container did not open.
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

// impossibleJob is an addLinks job id that cannot exist. JD hands those out
// from a counter of its own that only ever rises, so nothing is ever filed
// under a negative one - which makes a query for it a question about the
// filter rather than about any crawl. See jobFilterProbe.
const impossibleJob = -1

// jobFilterProbe remembers, for one crawl, whether this JD honours queryLinks's
// jobUUIDs filter.
//
// It has to be asked rather than assumed, and the reason is the damage if it is
// assumed wrongly. JD's API takes its query as a free-form map, so a build that
// does not know the key does not refuse it - it ignores it and answers with the
// WHOLE link grabber. A crawl that took that for "the links my container
// produced" would adopt every package in it, hand the user's own staged links
// back as the container's contents, and then delete them from the grabber on
// its way out. One measured build already treats the filter oddly (it answers
// nothing at all, see Client.AddContainerLinks), which is the cheap failure;
// this is the expensive one, and it costs one extra call per crawl to rule out.
//
// On the shipped JD (revision 48637, JDownloader2 r50639) the filter is ignored
// outright: queryLinks with jobUUIDs:[-1] answers with the entire link grabber,
// byte for byte the same as the unfiltered query. Measured 2026-09-13. So on
// that build the job anchor contributes nothing and the marker name carries the
// crawl alone. The probe is still what makes that safe rather than catastrophic,
// which is why it stays: delete the anchor and the guard goes with it.
type jobFilterProbe struct {
	decided bool
	honours bool
	// announce says the verdict, at most once per backend. The answer is a
	// property of the JD BUILD and does not change between two uploads, so
	// repeating it at every single one buries the upload that went differently
	// (jdp, 2026-09-13: "Im KL-Protokoll steht bei JEDEM Versuch").
	announce func()
}

// newJobFilterProbe makes a probe for one crawl whose verdict is announced at
// most once for the life of this backend. Asked fresh every crawl, because a JD
// can be updated under a running KnightLoader and the cheap direction of being
// wrong (falling back to the marker name) is the safe one.
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

// usable answers whether the jobUUIDs filter can be trusted on this JD.
//
// The probe is a query for a job that cannot exist while the grabber demonstrably
// holds something: a filter that is applied answers nothing, and an answer with
// links in it is the grabber being handed over wholesale. An empty grabber
// teaches nothing - both behaviours answer the same - so the question is simply
// left open and asked again on the next round.
func (p *jobFilterProbe) usable(c *Client, packagesInGrabber int) bool {
	if p.decided {
		return p.honours
	}
	if packagesInGrabber == 0 {
		return false
	}
	links, err := c.CrawledLinksForJob(impossibleJob)
	if err != nil {
		return false // no answer is no verdict; ask again next round
	}
	p.decided, p.honours = true, len(links) == 0
	if !p.honours && p.announce != nil {
		p.announce()
	}
	return p.honours
}

// crawlOutput reads back what one crawl job has produced so far: its links, and
// the grabber packages they sit in.
//
// The job id and the marker name are asked separately and unioned, because
// neither identifies a crawl on its own. The job filter does not care what JD
// named anything, which is what a container declaring its own package names
// needs; the marker name survives a JD whose jobUUIDs filter answers nothing or
// is ignored outright, both of which have been measured. Packages are the unit
// that is followed from there, so that a container opening into several is read
// whole rather than by whichever part happened to answer first.
//
// Nothing outside those two anchors is ever read, and nothing outside them is
// ever removed. The link grabber is shared with the user's own window, and a
// harvest that guessed at what is his would hand his links to somebody else's
// package and then clear them out of his.
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
		// Either nothing has been produced yet, or the job answered with links
		// while refusing to say which package they are in. In the second case
		// the links are still ours and still the whole of what the job made, so
		// they are harvested as they stand - and removed by their own ids
		// afterwards, which is why RemoveCrawled takes both lists.
		return byJob, nil, nil
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i] < pkgs[j] })
	found, err := b.c.CrawledLinks(pkgs...)
	if err != nil {
		return nil, nil, err
	}
	return found, pkgs, nil
}

// collecting is JD's global "is the grabber crawling" flag, reduced to what it
// is worth: a hint. An instance that will not answer it is not an instance whose
// crawl cannot be read, so an error here is no news rather than a failure - see
// Client.Collecting for why the flag is unreliable in both directions even when
// it does answer.
func (b *Backend) collecting() bool {
	busy, err := b.c.Collecting()
	return err == nil && busy
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
			// What has been staged so far still comes out of the grabber. A
			// check that gave up on a reading error used to leave its own
			// package behind, and a package left behind is a link JD will drop
			// out of the next container that carries it (see sweepGrabber).
			_ = b.c.RemoveCrawled(linkIDs(links), pkgs)
			return nil, err
		}
		links, pkgs = found, foundPkgs

		// A batch of plain links has a known length, so "all of them are here"
		// is the settle condition, not merely "the count stopped moving".
		// isCollecting is the same hint it is for a container and no more: this
		// crawl sharing an instance with somebody else's is not a reason for
		// every check on it to run into its deadline.
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

// statedAvailability is jdAvailability for a crawl that was not a check: only
// the two verdicts JD actually states travel, and anything else stays empty -
// "nobody has looked" - rather than becoming "the host would not say".
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
//
// The grabber package is held for the whole life of the task and its own
// leftover is cleared before the link goes in. Both halves are about JD's
// duplicate manager: a "KL-<task id>" package left over from an earlier attempt
// at the SAME task makes JD drop the link this attempt is submitting, silently,
// so the task would sit there until appearLimit and report that the link never
// reached the download list - having been eaten by its own predecessor. See
// sweepGrabber.
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

// captchaSkipped reports whether JD has GIVEN UP on a challenge rather than
// merely being busy with one.
//
// "Captcha recognition (rapidgator.net)" is work in progress and must be left
// alone. "Skipped - Captcha is required" is JD's own full stop: the link is not
// coming, and nothing about waiting changes that. Told apart because the wrong
// answer either way is expensive - treating the first as terminal kills a
// download JD was about to finish, and treating the second as running is what
// made a dead task sit at "running, 0 bytes" for forty-five minutes and then
// report "no progress for 45m0s", a sentence about the symptom that names
// neither the cause nor anything to do about it.
//
// Measured on the live instance, 2026-09-03: both strings above came back from
// a real free-mode rapidgator download while KnightLoader's own row said
// nothing but "running".
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
	// Held for as long as anything is watching this task, so the sweep cannot
	// take a package out from under a crawl that is still running. Counted, not
	// flagged, because Download holds the same name around the handover and
	// Resume reaches this with nothing held at all.
	b.holdGrabber(pkg)
	defer b.releaseGrabber(pkg)
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
				// Whatever is still sitting in the grabber under our name goes
				// with it. A link that never made it to the download list is a
				// link JD is done with, and leaving it there makes JD drop the
				// same URL out of every container that carries it from now on
				// (see sweepGrabber).
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
			// Said out loud the moment JD says it, instead of being waited out.
			if fatalPackageStatus(p.Status) {
				b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: " + p.Status})
				return
			}
			// A captcha JD has given up on settles NOW, with JD's own sentence.
			// Waiting out stallLimit would replace the one useful fact with a
			// meaningless one three quarters of an hour later.
			if captchaSkipped(p.Status) {
				b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "jd: " + p.Status, Note: p.Status})
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
			// JD's own word for what it is doing, carried out of the backend.
			//
			// This is the half that was missing while a free-mode download sat on
			// "Captcha recognition (rapidgator.net)" and the row said "running"
			// with no bytes: aggregate() below reads the LINKS, and JD reports
			// this on the PACKAGE. Everything the poller could see looked healthy.
			note := strings.TrimSpace(p.Status)

			// JD may have crawled one link into several files. Reporting only
			// the first would show a fraction of the real size and call the
			// task done while the rest is still downloading, so the whole
			// package is summed instead.
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
// A paused task keeps its grabber name held, which is the third thing this has
// to do now. The sweep's whole safety argument is "a package nothing is
// watching is abandoned", and pausing is the one way a task stays alive with
// nobody watching it: pause a download in the seconds while JD is still
// crawling it, open a container in that same window, and without the hold the
// sweep would take the paused crawl away. Resume gives the hold back.
func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	if s, ok := b.stop[taskID]; ok {
		// Taken before the poller is told to stop, so the name is never
		// unheld for even an instant: the poller's own release brings the
		// count back to this one, not to zero.
		b.held[b.pkgName(taskID)]++
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
		// Nobody watching means Pause is what stopped the last poller, so its
		// grabber hold is the one to give back. The new poller takes its own.
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
	// Every hold on this name goes, Pause's included: the task is gone, so
	// nothing about it is live business any more and the sweep may have it.
	delete(b.held, pkg)
	b.mu.Unlock()
	if puuid, err := b.c.PackageUUID(pkg); err == nil && puuid != 0 {
		_ = b.c.RemoveLinks(nil, []int64{puuid})
	}
	// The link grabber half is not tidiness. A task whose link never reached the
	// download list still has its "KL-<task id>" package staged there, JD
	// reloads the grabber at every start, and JD then silently drops that URL
	// out of every container a user opens from then on: the crawl produces no
	// package at all and nothing anywhere says why. Twenty-three of those were
	// sitting on the live instance, and they were the reason one particular DLC
	// had stopped opening. See sweepGrabber.
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
