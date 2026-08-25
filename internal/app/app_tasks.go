package app

// One task at a time: reading the list, editing a task, checking a link, taking
// a task away, and the two helpers everything else uses to persist a change and
// put it on screen.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
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
//
// The caller passes the typed reason rather than leaving it to be read back out
// of msg. Every caller here already knows the answer as a value — a status code,
// an error, or nothing at all — and re-deriving it from a sentence this code
// formatted itself is the round trip the typed reason exists to remove.
func (a *App) setAvailability(id string, avail core.Availability, msg string, reason core.Reason) {
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
		t.Reason = reason
	}
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// setTaskName records a name a probe found for a task that is still showing
// its own URL as a placeholder - see stage's own comment (app_links.go) on
// why every resolver that does not yet know a link's real name answers with
// the URL itself rather than leaving Name blank, and filename() (also
// app_links.go) which reads that exact convention back out.
//
// Same locked-read-modify-broadcast shape as setAvailability just above, and
// it guards against the same class of late answer: the task can be gone, or
// can already show a real name, by the time a backgrounded probe returns.
// The guard is Name == URL rather than a status check, because a task can
// leave StatusCollected (Start, then a real download begins) before a slow
// probe answers - at which point the backend's own progress stream
// (ytdlp/backend.go's "[download] Destination:" line, mirrored through
// onUpdate) has very likely already supplied the real name, and a probe
// fired before the download even started must not overwrite that.
func (a *App) setTaskName(id, name string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil || t.Name != t.URL {
		a.mu.Unlock()
		return
	}
	// A package that still says exactly what the URL's own path guessed at
	// staging time (fileStem/derivePackage's last resort, app_links.go) is
	// worth re-deriving now that a real name has arrived - jdp, 2026-08-25:
	// "bei einem Youtubelink heißt der Ordner nur watch. der soll den namen
	// anzeigen" (every YouTube watch page's path is /watch, so every bare-
	// pasted video landed in the identically-misnamed folder). Scoped tight
	// on purpose: only while this task is still the ONLY member of that
	// package (a real multi-link batch's own name comes from the shared
	// stem across all of them - one member's late answer must not rename
	// it out from under the others) and only while the package still
	// matches that guess exactly (a package the user renamed by hand,
	// coincidentally or not, is left alone).
	if guess := solePackageURLGuess(a.tasks, t); guess != "" && t.Package == guess {
		t.Package = sanitizeSegment(name)
	}
	t.Name = name
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// solePackageURLGuess returns what fileStem's own URL-path fallback
// (app_links.go) would have produced for t's package at staging time - or
// "" when there is nothing safe to compare against: t has no package,
// another task already shares it (a shared package belongs to the group,
// not to one member's own URL), or the guess itself would have been too
// short for derivePackage to have used in the first place (its own
// len(stem) >= 3 rule, mirrored here so this reports exactly what that
// function would have). t.Name is deliberately not read - the caller
// already knows it equalled t.URL a moment ago, which is exactly the
// condition under which fileStem takes this same path.Base fallback.
// Callers must already hold a.mu.
func solePackageURLGuess(tasks map[string]*core.Task, t *core.Task) string {
	if t.Package == "" {
		return ""
	}
	for _, other := range tasks {
		if other != t && other.Package == t.Package {
			return ""
		}
	}
	u, err := url.Parse(t.URL)
	if err != nil {
		return ""
	}
	stem := strings.Trim(path.Base(u.Path), ".-_ ")
	if len(stem) < 3 {
		return ""
	}
	return sanitizeSegment(stem)
}

// checkTimeout bounds one round of service checks. Generous compared with the
// staging HEAD, because a debrid provider asked about a hundred links is doing a
// hundred lookups of its own, and cutting that off mid-answer files links as
// uncheckable that the service was about to answer for.
const checkTimeout = 60 * time.Second

// RecheckTasks re-runs resolution and the availability check for collected
// tasks, so a link that was dead an hour ago can be tried again without
// re-pasting it. An empty id list rechecks everything in the collector.
//
// Links go out grouped by the backend that claims them, and a backend is asked
// once for its whole group. That is the reason resolver.Checker takes a slice:
// every service that answers this question meters by the account or by the
// address, and fifty separate questions about fifty links is how a key earns a
// "slow down" for the household.
func (a *App) RecheckTasks(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0

	a.mu.Lock()
	var targets []core.Task
	for id, t := range a.tasks {
		// A link the filter is holding is not probed. "Recheck the collector"
		// reaches every collected task, and a rule written to keep this box away
		// from a host would otherwise have it call that host once per recheck —
		// the same leak the staging-time pass exists to close, one button along.
		if t.Status == core.StatusCollected && !t.Skipped && (all || want[id]) {
			targets = append(targets, *t)
		}
	}
	a.mu.Unlock()
	if len(targets) == 0 {
		return
	}
	// One "linkcheck" burst for the whole call, retired one target at a time
	// as each settles below - see the three endActivity calls in this loop
	// and the one inside settleCheck, which between them cover every path a
	// target can leave by exactly once. Two overlapping calls (two browsers
	// both pressing "recheck all") add into the same shared counters rather
	// than each owning their own - see beginActivity's own doc comment.
	a.beginActivity(ActivityLinkCheck, len(targets))

	// Grouped by resolver id rather than by the resolver value, because a
	// resolver is a struct with a map in it and nothing says it is comparable.
	batches := map[string]*checkBatch{}
	var order []string
	for i := range targets {
		t := targets[i]
		res := a.Registry.For(t.URL)
		if res == nil {
			a.setAvailability(t.ID, core.AvailOffline, "no backend handles this link", core.ReasonUnsupported)
			a.endActivity(ActivityLinkCheck, 1)
			continue
		}
		result, err := res.Resolve(context.Background(), resolver.Request{URL: t.URL})
		if err != nil {
			// Uncheckable and not offline: resolving is this side of the wire, so a
			// failure here means the link was never put to the host at all. Filing
			// that as "the file is gone" is the same lie the HEAD probe used to tell.
			a.setAvailability(t.ID, core.AvailUncheckable, err.Error(), classify(failure{err: err}))
			a.endActivity(ActivityLinkCheck, 1)
			continue
		}
		a.mu.Lock()
		if live := a.tasks[t.ID]; live != nil {
			live.Resolver = res.Info().ID
			// result.Name != t.URL, not just != "": every resolver but "direct"
			// answers Resolve() with Name set to the URL itself as its "nothing
			// learned yet" placeholder (documented on stage()'s own matching
			// guard, app_links.go), and that string is never empty - so without
			// this half of the check, a routine Recheck (or the automatic one
			// RestoreFiltered fires) silently threw away any real name a task
			// had already picked up (the async yt-dlp title probe, a JD poll
			// update) and put the bare URL back, every time. jdp 2026-08-25:
			// "Die ganzen links im linksammler zeigen noch immer nicht ihre
			// namen richtig an, darauf habe ich dich jetzt schon mehrfach
			// angesprochen" - this klobber, parallel to the one round 35b fixed
			// in stage() but never mirrored here, is why a name that WAS
			// correct could still end up wrong on screen.
			if result.Name != "" && result.Name != t.URL {
				live.Name = result.Name
			}
		}
		a.mu.Unlock()
		if res.Info().ID == "direct" {
			// Our own HEAD, straight at the host: no account to spend, nothing to
			// batch, and it brings back the size as well.
			a.analyze(t.ID, result.DirectURL)
			a.endActivity(ActivityLinkCheck, 1)
			continue
		}
		id := res.Info().ID
		b := batches[id]
		if b == nil {
			b = &checkBatch{res: res}
			batches[id], order = b, append(order, id)
		}
		b.ids = append(b.ids, t.ID)
		// The resolved target, not the pasted URL: a resolver is entitled to have
		// rewritten it, and the service must be asked about the link that would
		// actually be fetched.
		b.urls = append(b.urls, result.DirectURL)
	}
	// The batched path retires the rest, one endActivity per link, inside
	// settleCheck - runCheck has exactly one caller, this loop, so that is
	// always precisely the targets that reached the batching branch above.
	for _, id := range order {
		a.runCheck(batches[id])
	}
}

// checkBatch is one backend's share of a recheck: the tasks and the links to ask
// about, held in the same order so a verdict lands on the link it is about.
type checkBatch struct {
	res  resolver.Resolver
	ids  []string
	urls []string
}

// runCheck asks one backend about its whole group and writes the verdicts back.
func (a *App) runCheck(b *checkBatch) {
	ck, ok := b.res.(resolver.Checker)
	if !ok {
		// A backend with no way to ask is not a backend whose links are unknown.
		// "Unknown" is what the list says about a link nobody has looked at, and
		// leaving a JD or yt-dlp link there after the user pressed Check is the
		// gap the fourth state was added to close.
		a.settleCheck(b, nil)
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, checkTimeout)
	defer cancel()
	got, err := ck.Check(ctx, b.urls)
	if err != nil {
		// The whole group is uncheckable and the log carries the why. A refused key
		// must never read as "these fifty files are gone" - that is the one wrong
		// answer here somebody acts on, by deleting them.
		log.Printf("%s could not check %d links: %v", b.res.Info().ID, len(b.urls), err)
		a.settleCheck(b, nil)
		return
	}
	a.settleCheck(b, resolver.Answers(got, len(b.ids)))
}

// settleCheck records a batch's verdicts; a nil slice files every task in it as
// uncheckable.
func (a *App) settleCheck(b *checkBatch, got []core.Availability) {
	for i, id := range b.ids {
		avail := core.AvailUncheckable
		if i < len(got) {
			avail = got[i]
		}
		switch avail {
		case core.AvailOffline:
			// Named, the way the HEAD path names the status it saw. A hoster's own
			// verdict and a HEAD off this box are different weights of evidence, and
			// the person deciding whether to delete the link should be able to see
			// which one this was without reading the code.
			a.setAvailability(id, core.AvailOffline, "offline ("+b.res.Info().ID+")", core.ReasonGone)
		case core.AvailOnline:
			a.setAvailability(id, core.AvailOnline, "", core.ReasonUnknown)
		default:
			// No sentence. Uncheckable is not a failure, and a red line of prose
			// under a link that is probably fine is how somebody is talked into
			// removing it. The row says "host would not say" and stops there.
			a.setAvailability(id, core.AvailUncheckable, "", core.ReasonUnknown)
		}
		a.endActivity(ActivityLinkCheck, 1)
	}
}

// analyze probes a plain file link with a HEAD request to fill in its size and
// record what the host said about it, updating the collected task in place.
func (a *App) analyze(id, rawurl string) {
	req, err := http.NewRequest(http.MethodHead, rawurl, nil)
	if err != nil {
		return
	}
	// a.Probe rather than a client built here: this is the one outbound call
	// that fires on a link the user has only just pasted, so it wants the shared
	// policy (proxy, user agent, and the redirect rule that stops a credential
	// following a hop off the host it was meant for) and it wants to be
	// replaceable, because every test that stages a link would otherwise be
	// racing a real DNS lookup.
	resp, err := a.Probe.Do(req)
	if err != nil {
		// A transport error is not a verdict about the file. The host was never
		// reached, so nothing was said about the link - and this branch used to
		// write "offline", which turns one flaky minute, one DNS hiccup, one
		// captive portal into a list of dead links the user then deletes.
		a.setAvailability(id, core.AvailUncheckable, "", classify(failure{err: err}))
		return
	}
	resp.Body.Close()
	// The status goes to the classifier as the number it is. This is the one probe
	// that holds a real response, and handing over the sentence instead would mean
	// parsing back out of "offline (HTTP 404)" what is sitting right here in a
	// field.
	switch availabilityFor(resp.StatusCode) {
	case core.AvailOffline:
		a.setAvailability(id, core.AvailOffline,
			"offline (HTTP "+strconv.Itoa(resp.StatusCode)+")", classify(failure{status: resp.StatusCode}))
		return
	case core.AvailUncheckable:
		// Silent, like the batch path: the typed reason is on the task for the
		// availability cell to show, and prose in the error column would put a
		// refusal to answer in the same red as a download that failed.
		a.setAvailability(id, core.AvailUncheckable, "", classify(failure{status: resp.StatusCode}))
		return
	}
	a.setAvailability(id, core.AvailOnline, "", core.ReasonUnknown)
	if resp.ContentLength > 0 {
		a.onUpdate(id, core.Update{Size: resp.ContentLength})
	}
}

// probeYtdlpTitle asks the yt-dlp backend for a collected task's real title
// without downloading anything, and applies it - the yt-dlp counterpart to
// analyze's HEAD probe just above for a plain file link, fired from the same
// stage() call site (app_links.go) and gated on the resolver id the same
// way analyze's own call is.
//
// Silent and non-fatal on failure or timeout, exactly like analyze's own
// probe leaves availability unset rather than guessing: yt-dlp's own
// progress stream still supplies the real name once a download actually
// starts (backend.go's "[download] Destination:" line, mirrored through
// onUpdate), so a probe that never answers costs the user nothing beyond the
// placeholder standing a little longer in the collector.
func (a *App) probeYtdlpTitle(id, rawurl string) {
	tp, ok := a.ytdlpTitleProber()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, ytdlpProbeTimeout)
	defer cancel()
	title, err := tp.ProbeTitle(ctx, rawurl)
	if err != nil {
		return
	}
	if title = strings.TrimSpace(title); title != "" {
		a.setTaskName(id, title)
	}
}

// availabilityFor reads a HEAD's status code as a statement about the link.
//
// Only 404 and 410 are the host saying the file is not there. Everything else
// above 399 is the host declining to answer the question that was asked, and
// every one of them used to be filed as offline: a 403 is usually a hoster that
// will not be probed, a 405 is one that does not implement HEAD at all, a 429 is
// one that has heard enough for now, a 503 is one having a bad afternoon. Four
// perfectly live links, marked dead, on a list with a "remove offline" button on
// it.
func availabilityFor(status int) core.Availability {
	switch {
	case status == http.StatusNotFound || status == http.StatusGone:
		return core.AvailOffline
	case status >= 400:
		return core.AvailUncheckable
	}
	return core.AvailOnline
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
	// DownloadPassword is the password a hoster's own page asks for before it
	// hands over the file. It is NOT Password above, which is the archive
	// password extraction tries first - two secrets asked by two different
	// parties, and one field for both is how the wrong one gets typed into the
	// wrong prompt. See core.Task.DownloadPassword's own comment.
	DownloadPassword *string `json:"downloadPassword,omitempty"`
	// Name is a rename asked for by a person - the properties panel's name box.
	// It is cut to one path segment and then applied according to what the task
	// is doing right now (see renameLocked), which is the whole of the difference
	// from Filename below: that one is the raw override, written as given and
	// acted on only once the bytes have stopped moving. A request carrying both
	// is not refused; Name is applied second and wins.
	//
	// It is refused over a selection of more than one, because a name is an
	// identity and not a property. Forty rows given one name is forty downloads
	// pointed at one destination, of which renameFinishedLocked would carry out
	// the first and refuse the other thirty-nine one at a time.
	Name *string `json:"name,omitempty"`
	// Comment is the note on the row. Nothing in the app acts on it, which is why
	// it is editable here at all: it is the one field whose only reader is the
	// person who comes back to this list next month.
	Comment *string `json:"comment,omitempty"`
	// Priority is the absolute value, not a step. The panel shows the five the
	// queue accepts and writes the one that was chosen; the two arrows in the
	// toolbar are the relative reading of the same field.
	Priority *int `json:"priority,omitempty"`
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
//
// A nil field is left alone, and that is the whole contract the properties panel
// rests on: it edits a selection, so a box the user never touched must not carry
// its own emptiness onto forty rows. The panel sends what was changed and
// nothing else - see renameLocked for the one field whose answer depends on the
// task rather than on the request.
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
	var renameTo string
	if o.Name != nil {
		if len(ids) > 1 {
			return fmt.Errorf("a name belongs to one file, and %d are selected", len(ids))
		}
		if strings.TrimSpace(*o.Name) == "" {
			return errors.New("a download cannot be renamed to nothing")
		}
		// Cut rather than refused, and cut by the rule engine's own function: a
		// name is one path segment here for exactly the reason it is one in a
		// Packagizer rename, and a second cut written next to this one is a second
		// answer to "is this a name" that will eventually differ from the first.
		renameTo = rules.FileSegment(*o.Name)
	}
	if o.Chunks != nil && (*o.Chunks < 0 || *o.Chunks > rules.MaxChunks) {
		return fmt.Errorf("chunk count %d is outside 0..%d", *o.Chunks, rules.MaxChunks)
	}

	a.mu.Lock()
	var renameErr error
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
		if o.DownloadPassword != nil {
			t.DownloadPassword = strings.TrimSpace(*o.DownloadPassword)
		}
		if o.Comment != nil {
			t.Comment = strings.TrimSpace(*o.Comment)
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
		if o.Name != nil {
			// Kept rather than returned on the spot: the reason belongs on the row as
			// well as in the reply, and leaving the lock here would skip the save and
			// the broadcast that put it there.
			renameErr = a.renameLocked(t, renameTo)
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
	// SetPriority rather than a second assignment here, and last because it takes
	// the lock itself. The queue's range, its re-sort and the dispatch that acts on
	// the new order all live in that one call; a properties panel that wrote
	// t.Priority directly would be a second copy of all three, and the copy that
	// forgets to dispatch is the one where a raised task sits still.
	if o.Priority != nil {
		a.SetPriority(ids, *o.Priority)
	}
	return renameErr
}

// renameLocked applies a rename a person asked for, and what it does depends
// entirely on what the task is doing at that moment. The rule per status:
//
//	done                the file is closed, so it moves on disk and the row
//	                    follows it. A row renamed on its own would promise a name
//	                    the folder does not have, and extraction and checksum
//	                    verification both build their path from that name.
//	running, extracting the backend holds the file open under the name it chose
//	                    itself. The new name is only recorded; the settle path
//	                    carries it out. Writing to that handle now is how a
//	                    transfer stops finding its own .part file after a restart.
//	everything else     nothing final has been written yet, so the row takes the
//	                    name at once. The backend reports the name it really used
//	                    when the download starts, which puts the two apart again
//	                    and leaves the override something to do at the end.
//
// Caller holds a.mu.
func (a *App) renameLocked(t *core.Task, want string) error {
	// The override is set in every case, because it is the only thing that reaches
	// the file: no backend accepts a destination file name, so a rename is always
	// something done to the finished download rather than asked of the transfer.
	t.Filename = want
	switch t.Status {
	case core.StatusDone:
		if !filesAreLocal(t) {
			return fmt.Errorf("%s was downloaded on another machine, so it cannot be renamed from here", t.Name)
		}
		before := t.Error
		a.renameFinishedLocked(t)
		if t.Name == want {
			return nil
		}
		// renameFinishedLocked records a refusal on the task rather than returning
		// it, because its other caller is the settle path, where there is nobody
		// left to answer. Here somebody is waiting on a reply, and a rename that
		// quietly did not happen is the silence this panel exists to break.
		if t.Error != before && t.Error != "" {
			return errors.New(t.Error)
		}
		return fmt.Errorf("%s was not renamed", t.Name)
	case core.StatusRunning, core.StatusExtracting:
		return nil
	default:
		t.Name = want
		return nil
	}
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
