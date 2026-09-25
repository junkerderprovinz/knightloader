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
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
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
		// An undispatched task has no mode yet, but "free or premium" is asked
		// while it waits. It is derived on the copy only, since the JD hoster
		// list arrives after boot and changes; dispatch writes the real value.
		if c.Mode == core.ModeUnknown {
			c.Mode = a.modeForLocked(&c, c.Resolver)
		}
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// SetPackage moves tasks into a package (an empty name ungroups them). A
// variant family moves together even when only one of its ids is named, and
// files already on disk stay where they are (keepFoldersLocked), as they do
// when their package is renamed.
func (a *App) SetPackage(ids []string, pkg string) {
	pkg = strings.TrimSpace(pkg)
	a.mu.Lock()
	members := a.sharingLinksLocked(ids)
	a.keepFoldersLocked(members, pkg)
	copies := make([]core.Task, 0, len(members))
	for _, t := range members {
		t.Package = pkg
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
}

// sharingLinksLocked returns the tasks named by ids and every task that shares
// a link with one of them, oldest first. The rows of one yt-dlp link are one
// folder on disk, so they move together although staging hands back only the
// first. Caller holds a.mu.
func (a *App) sharingLinksLocked(ids []string) []*core.Task {
	links := map[string]bool{}
	for _, id := range ids {
		if t := a.tasks[id]; t != nil {
			links[t.URL] = true
		}
	}
	var members []*core.Task
	for _, t := range a.tasks {
		if links[t.URL] {
			members = append(members, t)
		}
	}
	sortByAge(members)
	return members
}

// keepFoldersLocked pins members to the folder each downloads to, by writing
// it into Dir, where that folder is built from the package name and pkg would
// change it. It does so for every member of a folder one of them has already
// written to: a file there would otherwise be looked for where it is not, and
// the volumes of an archive are only unpacked together while they share one
// folder. Members of a folder none of them has touched follow pkg. Caller
// holds a.mu.
func (a *App) keepFoldersLocked(members []*core.Task, pkg string) {
	folder := make(map[string]string, len(members))
	written := map[string]bool{}
	for _, t := range members {
		folder[t.ID] = a.dirFor(t)
		if a.hasFilesLocked(t) {
			written[folder[t.ID]] = true
		}
	}
	for _, t := range members {
		current := folder[t.ID]
		if t.Dir != "" || !written[current] {
			continue
		}
		moved := *t
		moved.Package = pkg
		if a.dirFor(&moved) != current {
			t.Dir = current
		}
	}
}

// setAvailability records what a check learned about a link. Availability
// belongs to the link, not to a download attempt, so a staged link can be known
// dead before anything starts. The caller passes the typed reason it already
// has rather than it being parsed back out of msg.
func (a *App) setAvailability(id string, avail core.Availability, msg string, reason core.Reason) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	t.Online = avail
	// A late probe must not overwrite why a settled task failed, such as a
	// filter rule or a taken destination.
	if t.Status != core.StatusError {
		t.Error = msg
		t.Reason = reason
	}
	c := *t
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// setTaskName records a name a probe found for a task still showing its URL as
// a placeholder (the convention stage and filename in app_links.go share).
//
// The guard is Name == URL rather than a status check: the task may be gone,
// or a download may have started and its progress stream supplied the real
// name, by the time a slow probe returns.
func (a *App) setTaskName(id, name string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil || t.Name != t.URL {
		a.mu.Unlock()
		return
	}
	// A package that is still the URL path's guess (every YouTube watch page
	// guesses "watch") is re-derived from the real name, unless a package
	// member that is not a variant sibling already has a real name, which marks
	// a resolved batch. Unrelated links sharing a guessed package split apart
	// one by one as their probes answer. The returned copies are not needed:
	// the sibling loop below broadcasts the whole family.
	newPackage := t.Package
	if reguessPackageLocked(a.tasks, t, name) != nil {
		newPackage = t.Package
	}
	t.Name = name
	c := *t

	// Variant siblings share this task's exact URL, and nothing else does. They
	// take the same name and package so the family stays in one folder.
	var siblings []core.Task
	for _, other := range a.tasks {
		if other == t || other.URL != t.URL {
			continue
		}
		if other.Name == other.URL {
			other.Name = name
		}
		other.Package = newPackage
		siblings = append(siblings, *other)
	}
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
	for i := range siblings {
		_ = a.Store.Save(&siblings[i])
		a.Hub.Broadcast("task", &siblings[i])
	}
}

// reguessPackageLocked replaces a package that is still the URL path's guess
// with one built from the real name, and returns the tasks it changed.
//
// regressGuessedPackages (app_links.go) calls it too: nameBucket writes its
// package after taking a snapshot, so a probe answering in between finds no
// package to replace, and the write then files the named link under the guess.
//
// Caller holds a.mu.
func reguessPackageLocked(tasks map[string]*core.Task, t *core.Task, name string) []core.Task {
	if !packageIsStillAGuess(t) || !noSiblingHasARealNameYet(tasks, t) {
		return nil
	}
	return setPackageLocked(tasks, t, sanitizeSegment(name))
}

// packageIsStillAGuess reports whether t's package was made up by the app
// rather than chosen, and may be replaced by a real name:
//
//   - the URL-path guess itself;
//   - no package at all, which is how nameBucket leaves a link still waiting
//     on a yt-dlp title probe (see awaitingMediaProbe, app_links.go).
//
// A package set by hand (ManualPackage), even an empty one, is never replaced.
func packageIsStillAGuess(t *core.Task) bool {
	if t.ManualPackage {
		return false
	}
	if strings.TrimSpace(t.Package) == "" {
		return true
	}
	guess := packageURLGuess(t)
	return guess != "" && t.Package == guess
}

// setPackageLocked files t in pkg, and with it every task sharing t's exact URL
// (its variant siblings), and returns every task it touched for the caller to
// save and broadcast.
//
// The five rows of one video must share one package to be one folder on disk.
// The siblings are created after a bucket is assembled, so they are in nobody's
// id list. A package picked by hand keeps the same rule through
// sharingLinksLocked. Caller holds a.mu.
func setPackageLocked(tasks map[string]*core.Task, t *core.Task, pkg string) []core.Task {
	t.Package = pkg
	out := []core.Task{*t}
	for _, other := range tasks {
		if other != t && other.URL == t.URL {
			other.Package = pkg
			out = append(out, *other)
		}
	}
	return out
}

// packageURLGuess returns the package fileStem's URL-path fallback
// (app_links.go) would have produced for t at staging time, or "" when the URL
// does not parse or the stem is shorter than derivePackage's three-character
// minimum. It reads only t.URL: the caller knows t.Name equalled the URL, the
// condition under which fileStem falls back to the path.
func packageURLGuess(t *core.Task) string {
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

// noSiblingHasARealNameYet tells a package worth splitting a member out of from
// a resolved batch: every other task in t's package must still show its URL as
// its name. A crawled batch gets real names at staging, so one named member is
// enough to leave the package alone. Caller holds a.mu.
func noSiblingHasARealNameYet(tasks map[string]*core.Task, t *core.Task) bool {
	for _, other := range tasks {
		if other == t || other.Package != t.Package {
			continue
		}
		// A row with t's exact URL is a variant sibling, not a member of a
		// resolved batch. Once a probe has named the whole family, counting
		// siblings would have every row veto every other's rename.
		if other.URL == t.URL {
			continue
		}
		if other.Name != other.URL {
			return false
		}
	}
	return true
}

// checkTimeout bounds one round of service checks. It is generous because a
// debrid provider asked about a hundred links does a hundred lookups itself.
const checkTimeout = 60 * time.Second

// RecheckTasks re-runs resolution and the availability check for collected
// tasks. An empty id list rechecks everything in the collector.
//
// Each backend is asked once for its whole group of links, which is why
// resolver.Checker takes a slice: these services rate-limit by account or
// address.
func (a *App) RecheckTasks(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0

	a.mu.Lock()
	var targets []core.Task
	for id, t := range a.tasks {
		// A link the filter holds is not probed, or a rule meant to keep this
		// box away from a host would call it on every recheck.
		if t.Status == core.StatusCollected && !t.Skipped && (all || want[id]) {
			targets = append(targets, *t)
		}
	}
	a.mu.Unlock()
	if len(targets) == 0 {
		return
	}
	// One linkcheck burst for the call, retired one target at a time: the
	// endActivity calls below and in settleCheck cover every exit exactly once.
	a.beginActivity(ActivityLinkCheck, len(targets))

	// Grouped by resolver id rather than by the resolver value, because a
	// resolver is a struct with a map in it and nothing says it is comparable.
	batches := map[string]*checkBatch{}
	var order []string
	for i := range targets {
		t := targets[i]
		// The same ranked lookup staging uses, since the answer is written back
		// onto the task below; plain registry order would move a task back to
		// "direct" and undo jd.PriorityFor's boost.
		res := a.stagingResolverFor(t.URL)
		if res == nil {
			a.setAvailability(t.ID, core.AvailOffline, "no backend handles this link", core.ReasonUnsupported)
			a.endActivity(ActivityLinkCheck, 1)
			continue
		}
		result, err := res.Resolve(context.Background(), resolver.Request{URL: t.URL})
		if err != nil {
			// Uncheckable, not offline: resolving happens on this side, so the host
			// was never asked.
			a.setAvailability(t.ID, core.AvailUncheckable, err.Error(), classify(failure{err: err}))
			a.endActivity(ActivityLinkCheck, 1)
			continue
		}
		a.mu.Lock()
		if live := a.tasks[t.ID]; live != nil {
			live.Resolver = res.Info().ID
			// Most resolvers answer with the URL as a placeholder name (see the
			// matching guard in stage), which must not replace a real name the task
			// already picked up.
			if result.Name != "" && result.Name != t.URL {
				live.Name = result.Name
			}
		}
		a.mu.Unlock()
		if res.Info().ID == "direct" {
			// Our own HEAD: no account to spend, nothing to batch, and it brings
			// back the size.
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
		// The resolved target, which is what would actually be fetched.
		b.urls = append(b.urls, result.DirectURL)
	}
	// settleCheck retires the batched targets, one endActivity per link.
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
	if !ok || a.resolverOff(b.res.Info().ID) {
		// Uncheckable rather than unknown: unknown means nobody has looked yet.
		a.settleCheck(b, nil)
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, checkTimeout)
	defer cancel()
	got, err := ck.Check(ctx, b.urls)
	if err != nil {
		// Uncheckable, never offline: a refused key must not make fifty live
		// links look deletable.
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
			// Names the service, so a hoster's verdict can be told from our own
			// HEAD.
			a.setAvailability(id, core.AvailOffline, "offline ("+b.res.Info().ID+")", core.ReasonGone)
		case core.AvailOnline:
			a.setAvailability(id, core.AvailOnline, "", core.ReasonUnknown)
		default:
			// No error text: uncheckable is not a failure.
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
	// a.Probe carries the shared client policy and can be replaced in tests.
	resp, err := a.Probe.Do(req)
	if err != nil {
		// A transport error says nothing about the file: the host was never
		// reached.
		a.setAvailability(id, core.AvailUncheckable, "", classify(failure{err: err}))
		return
	}
	resp.Body.Close()
	switch availabilityFor(resp.StatusCode) {
	case core.AvailOffline:
		a.setAvailability(id, core.AvailOffline,
			"offline (HTTP "+strconv.Itoa(resp.StatusCode)+")", classify(failure{status: resp.StatusCode}))
		return
	case core.AvailUncheckable:
		// No error text, as in the batch path; the typed reason is enough for
		// the availability cell.
		a.setAvailability(id, core.AvailUncheckable, "", classify(failure{status: resp.StatusCode}))
		return
	}
	a.setAvailability(id, core.AvailOnline, "", core.ReasonUnknown)
	if resp.ContentLength > 0 {
		a.onUpdate(id, core.Update{Size: resp.ContentLength})
	}
}

// probeYtdlpTitle asks the yt-dlp backend for a collected task's title and
// available formats without downloading anything, the yt-dlp counterpart to
// analyze. A failed probe is silent: the download's progress stream supplies
// the name later. A probe that answers also marks the link online (see
// applyProbeFormats).
func (a *App) probeYtdlpTitle(id, rawurl string) {
	tp, ok := a.ytdlpTitleProber()
	if !ok {
		// Filed like a probe that found nothing, or a link staged while
		// yt-dlp is switched off would never get a package.
		a.fileUnprobedMedia(id)
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, ytdlpProbeTimeout)
	defer cancel()
	res, err := tp.ProbeTitle(ctx, rawurl)
	if err != nil {
		a.fileUnprobedMedia(id)
		return
	}
	title := strings.TrimSpace(res.Title)
	if title == "" {
		a.fileUnprobedMedia(id)
	} else {
		a.setTaskName(id, title)
	}
	a.applyProbeFormats(rawurl, res.Formats)
}

// fileUnprobedMedia gives a media link a package after its title probe came
// back with nothing. The naming passes skip such a link while its probe runs
// (awaitingMediaProbe, app_links.go), so without this it would stay ungrouped.
// It applies the guess those passes would have: the URL path's last segment.
//
// A link that has meanwhile been named, filed or given a package by hand is
// left alone.
func (a *App) fileUnprobedMedia(id string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil || t.Name != t.URL || t.ManualPackage || strings.TrimSpace(t.Package) != "" {
		a.mu.Unlock()
		return
	}
	guess := packageURLGuess(t)
	if guess == "" {
		guess = catchAllPackage
	}
	changed := setPackageLocked(a.tasks, t, sanitizeSegment(guess))
	a.mu.Unlock()
	a.publishTasks(changed)
}

// availabilityFor reads a HEAD's status code as a statement about the link.
// Only 404 and 410 say the file is gone. Other errors (403, 405, 429, 503) are
// the host declining to answer, and the links are usually live.
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

// TriBool is a bool a request may also send as null. A *bool decodes absent
// and null alike, but here absent means "leave this alone", null means "inherit
// the global", and true or false is the override. A plain bool would turn
// "inherit" into false and stop unpacking for every task.
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

// MarshalJSON writes an override nobody set as null, never false.
func (t TriBool) MarshalJSON() ([]byte, error) { return json.Marshal(t.Value) }

// TaskOptions are the per-task overrides the UI can set. A nil field means
// "leave as it is", which keeps a partial edit from wiping the other values.
type TaskOptions struct {
	Dir      *string `json:"dir,omitempty"`
	Password *string `json:"password,omitempty"`
	// DownloadPassword is the password a hoster's page asks for before handing
	// over the file, separate from Password, the archive password extraction
	// tries first.
	DownloadPassword *string `json:"downloadPassword,omitempty"`
	// Name is a rename typed by hand. It has to be one path segment (see
	// checkName) and is applied according to what the task is doing (see
	// renameLocked), unlike Filename, which is written as given and acted on
	// once the bytes stop. With both, Name wins.
	//
	// It is refused for more than one task, since one name for many rows would
	// point them all at one destination.
	Name *string `json:"name,omitempty"`
	// Comment is the note on the row; nothing in the app acts on it.
	Comment *string `json:"comment,omitempty"`
	// Priority is the absolute value, not a step.
	Priority *int `json:"priority,omitempty"`
	// Filename is the name the finished file is put under. The backend
	// downloads under its own name and the file is renamed once the bytes stop,
	// since the engine keys its .part file on its own name. An empty string
	// removes the override.
	Filename *string `json:"filename,omitempty"`
	// Chunks is how many connections this download opens. Zero defers to the
	// resolver's answer and the built-in default.
	Chunks *int `json:"chunks,omitempty"`
	// AutoExtract is the per-task unpacking switch, read at extraction time, so
	// turning it on for a finished download unpacks it now.
	AutoExtract TriBool `json:"autoExtract"`
	// VariantQuality is the sub-value of a variant row (see variantEncode): a
	// height cap or a track for video, a format or a track for audio. The
	// row's kind is never edited. An empty string means "no opinion"; nil
	// means leave alone.
	VariantQuality *string `json:"variantQuality,omitempty"`
	// AudioBitrate is the audio row's bitrate beside a format in
	// VariantQuality: what a conversion encodes to, or, for a format the source
	// has, the bitrate whose nearest track the row takes. An empty string
	// means the best track or ffmpeg's default; nil means leave alone.
	AudioBitrate *string `json:"audioBitrate,omitempty"`
}

// SetTaskOptions applies per-task overrides. A folder change on a running task
// only affects a later restart, but a rename and the unpacking switch act at
// once on a finished download.
//
// A nil field is left alone: the properties panel edits a selection and sends
// only what changed.
func (a *App) SetTaskOptions(ids []string, o TaskOptions) error {
	// Everything is validated before any task is touched, so a refusal never
	// leaves a selection half edited.
	if o.Dir != nil && *o.Dir != "" {
		if err := settings.Validate("the folder for this download", *o.Dir); err != nil {
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
		name, err := checkName("download", *o.Name)
		if err != nil {
			return err
		}
		// What checkName lets through still passes the function a Packagizer
		// rename uses, so the two cannot write different names for one input.
		renameTo = rules.FileSegment(name)
	}
	if o.Chunks != nil && (*o.Chunks < 0 || *o.Chunks > rules.MaxChunks) {
		return fmt.Errorf("chunk count %d is outside 0..%d", *o.Chunks, rules.MaxChunks)
	}

	a.mu.Lock()
	var renameErr error
	var reprobe []string
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
		// The bitrate first, since resolving the new pick reads it.
		if o.AudioBitrate != nil {
			t.AudioBitrate = strings.TrimSpace(*o.AudioBitrate)
		}
		if o.VariantQuality != nil {
			kind, _ := variantDecode(t.Variant)
			if kind == "" {
				kind = ytdlp.VariantVideo
			}
			t.Variant = variantEncode(kind, strings.TrimSpace(*o.VariantQuality))
		}
		if (o.VariantQuality != nil || o.AudioBitrate != nil) && t.Status == core.StatusCollected {
			// A new pick is a different file: its extension and size follow.
			if !a.reapplyProbeLocked(t) && !slices.Contains(reprobe, t.URL) {
				reprobe = append(reprobe, t.URL)
			}
		}
		if o.Filename != nil {
			t.Filename = newName
			if t.Status == core.StatusDone {
				_ = a.renameFinishedLocked(t)
			}
		}
		if o.Name != nil {
			// Returned after the save and broadcast, since the reason belongs on
			// the row too.
			renameErr = a.renameLocked(t, renameTo)
		}
		if o.AutoExtract.Set {
			// Across the whole volume set: the first volume is the one opened, so
			// an override on part02 alone would do nothing.
			for _, part := range a.volumeSetLocked(t) {
				// A copy per task, so the rows do not share one pointer.
				part.AutoExtract = copyBool(o.AutoExtract.Value)
				touched[part.ID] = part
			}
		}
		touched[t.ID] = t
	}
	// Extraction is decided after every override has landed, so a multi-volume
	// selection is read as the user left it.
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
	for _, u := range reprobe {
		a.spawn(func() { a.reprobeYtdlp(u) })
	}
	// Through SetPriority, which clamps, re-sorts and dispatches; last because it
	// takes the lock itself.
	if o.Priority != nil {
		a.SetPriority(ids, *o.Priority)
	}
	return renameErr
}

// renameLocked applies a rename asked for by hand, according to the task's
// status:
//
//	done             the file moves on disk and the row follows, since
//	                 extraction and checksums build their path from the name.
//	running          the backend holds the file open under its own name, so the
//	                 name is only recorded and the settle path applies it;
//	                 renaming now would orphan the .part file.
//	extracting       refused: the unpacker has the file open, and nothing would
//	                 carry a recorded name out once it lets go.
//	everything else  the row takes the name at once. The backend reports the
//	                 name it used when the download starts, and the override
//	                 is applied at the end.
//
// A torrent, a JDownloader download and one part of a multi-volume archive are
// refused in every state, since none of them would ever take the name: the
// settle path skips the first two and refuses the third. A refusal leaves the
// task as it was. Caller holds a.mu.
func (a *App) renameLocked(t *core.Task, want string) error {
	switch {
	case t.InfoHash != "":
		// The swarm asks for the files by the names the torrent gives them.
		return refuseRename("torrent", t.Name, "%s is a torrent, and a torrent names its own files", t.Name)
	case !filesAreLocal(t):
		return refuseRename("remote", t.Name,
			"%s is downloaded by JDownloader on its own machine, so it cannot be renamed from here", t.Name)
	case t.Status == core.StatusExtracting:
		return refuseRename("unpacking", t.Name, "%s is being unpacked; it can be renamed once that is over", t.Name)
	case len(a.volumeSetLocked(t)) > 1:
		return refuseRename("volume", t.Name,
			"%s is one part of a multi-volume archive, and the parts keep their names so it can be unpacked", t.Name)
	}
	// The override is always set: no backend accepts a destination file name,
	// so a rename is applied to the finished download.
	t.Filename = want
	switch t.Status {
	case core.StatusDone:
		if err := a.renameFinishedLocked(t); err != nil {
			return err
		}
		if t.Name == want {
			return nil
		}
		return fmt.Errorf("%s was not renamed", t.Name)
	case core.StatusRunning:
		return nil
	default:
		t.Name = want
		return nil
	}
}

// checkName refuses a name typed for a download or a package that cannot
// stand as one path segment. A separator is refused rather than cut: cut, a
// name typed as "Season 1/Episode 2" would quietly become one nobody typed.
// Characters only some file systems refuse are left to rules.FileSegment and
// sanitizeSegment, where every other name goes through too.
func checkName(what, name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", refuseRename("empty", name, "a %s cannot be renamed to nothing", what)
	case strings.ContainsAny(name, `/\`):
		return "", refuseRename("separator", name, "%q cannot name a %s: / and \\ separate folders", name, what)
	case strings.Trim(name, ".") == "":
		return "", refuseRename("dots", name, "%q cannot name a %s: there is nothing in it but dots", name, what)
	}
	return name, nil
}

// RenameRefusal is a rename turned down for a reason the interface words in
// the reader's language. Code names the reason: torrent, remote, unpacking,
// volume, empty, separator, dots or exists. Name is the file or the name it is
// about, and Error says the same in English for every other client.
type RenameRefusal struct {
	Code string
	Name string
	text string
}

func (e *RenameRefusal) Error() string { return e.text }

func refuseRename(code, name, format string, args ...any) error {
	return &RenameRefusal{Code: code, Name: name, text: fmt.Sprintf(format, args...)}
}

// RenamePackage gives the named tasks, and every task sharing one of their
// links (sharingLinksLocked), a new package name. Where the download folder is
// built from the package name, it follows the new name only while nothing
// downloading into it has started (keepFoldersLocked).
func (a *App) RenamePackage(ids []string, name string) ([]string, error) {
	name, err := checkName("package", name)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	members := a.sharingLinksLocked(ids)
	a.keepFoldersLocked(members, name)
	copies := make([]core.Task, 0, len(members))
	for _, t := range members {
		t.Package = name
		// A probe that names the link later must not put the package back.
		t.ManualPackage = true
		copies = append(copies, *t)
	}
	a.mu.Unlock()
	a.saveAndBroadcast(copies)
	return idsOf(members), nil
}

// hasFilesLocked reports whether a task has put bytes on disk or been handed
// to a backend that is about to. Caller holds a.mu.
func (a *App) hasFilesLocked(t *core.Task) bool {
	switch t.Status {
	case core.StatusRunning, core.StatusExtracting, core.StatusDone:
		return true
	}
	return t.Loaded > 0 || a.started[t.ID]
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

// usableFilename reports whether a name is a single path segment, so it cannot
// escape the download folder.
func usableFilename(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

// renameFinishedLocked puts a finished download under the name a Packagizer
// rule or the user asked for. It only runs once the bytes have stopped, since
// the engine keys its .part file on its own name.
//
// A rename that cannot be done leaves the file alone and records why on the
// task, so the list never shows a name the disk does not have. The same reason
// is returned for a caller that is waiting for the answer. Caller holds a.mu.
func (a *App) renameFinishedLocked(t *core.Task) error {
	want := strings.TrimSpace(t.Filename)
	// A task whose name is still its URL has not been resolved, so there is no
	// file under the old name to move.
	if want == "" || want == t.Name || t.Name == "" || t.Name == t.URL || !filesAreLocal(t) {
		return nil
	}
	refuse := func(err error) error {
		t.Error = err.Error()
		return err
	}
	if !usableFilename(want) {
		return refuse(fmt.Errorf("not renamed: %s is not a single file name", strconv.Quote(want)))
	}
	if len(a.volumeSetLocked(t)) > 1 {
		// extract.SetKey groups volumes by name, and a fixed rule name would
		// give every part the same one and overwrite the set.
		return refuse(refuseRename("volume", t.Name, "not renamed: %s is one part of a multi-volume archive", t.Name))
	}
	from := t.File
	if from == "" {
		// Where the file is now: the working folder until the delivery has
		// moved it on, the destination after. The settle path renames before
		// that move.
		dir := a.workDirFor(t)
		if _, err := os.Lstat(filepath.Join(dir, t.Name)); err != nil {
			dir = a.dirFor(t)
		}
		from = filepath.Join(dir, t.Name)
	}
	to := filepath.Join(filepath.Dir(from), want)
	// Checked first, since Rename replaces an existing destination on most
	// platforms.
	if _, err := os.Stat(to); err == nil {
		return refuse(refuseRename("exists", want, "not renamed: %s already exists", to))
	}
	if err := os.Rename(from, to); err != nil {
		return refuse(fmt.Errorf("not renamed: %w", err))
	}
	t.Name = want
	if t.File != "" {
		t.File = to
	}
	return nil
}

// saveAndBroadcast persists task snapshots and pushes them to connected UIs.
func (a *App) saveAndBroadcast(copies []core.Task) {
	for i := range copies {
		c := copies[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
	}
}

// Remove drops a task from the list. deleteFiles also erases what was
// downloaded; it is never the default, as in JDownloader.
func (a *App) Remove(id string, deleteFiles bool) {
	if a.removeTask(id, deleteFiles) {
		// The link may have been the last one a countdown was waiting for.
		a.wakeAutoConfirm()
	}
}

// removeTask is Remove without waking the auto-confirm countdowns, so a caller
// removing many rows wakes them once. It reports whether the row was in the
// collector, the only place a countdown looks.
func (a *App) removeTask(id string, deleteFiles bool) (collected bool) {
	a.mu.Lock()
	t := a.tasks[id]
	collected = t != nil && t.Status == core.StatusCollected
	var own leftover
	if t != nil && deleteFiles {
		own = a.ownFileLocked(t)
	}
	// Unfiled first, or the removed link would keep blocking its own re-add.
	a.forgetLinkLocked(t)
	delete(a.tasks, id)
	delete(a.active, id)
	delete(a.started, id)
	delete(a.fellBack, id)
	a.dequeueLocked(id)
	a.dispatchLocked()
	a.mu.Unlock()
	if t != nil {
		a.backendFor(t.Resolver).Remove(id, deleteFiles)
		// The engine only deletes files of transfers it still knows, and it
		// forgets them all on a restart.
		own.drop(id)
	}
	_ = a.Store.Delete(id)
	a.Hub.Broadcast("removed", map[string]string{"id": id})
	return collected
}

// put stages a task: it enters the task map, the store and every connected
// browser.
//
// The mirror check and the insert are one critical section, so two pastes of
// the same file cannot both be told the link is new. It returns the entry that
// refused the link, so the caller can say which download it was folded into.
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
	// Fired here, where every link enters, so new staging paths get the event
	// too; outside the lock and after the broadcast.
	a.fireLinkAdded(c)
	return dedupe.Match{}, true
}

// linkEntry describes a task to the mirror set. An unresolved task's name is
// still its URL, which the set reads as "not known yet".
func linkEntry(t *core.Task) dedupe.Entry {
	return dedupe.Entry{ID: t.ID, URL: t.URL, Name: t.Name, Size: t.Size}
}

// forgetLinkLocked takes a task's link out of the mirror set, but only while
// the set still points at that task; a re-added link's successor must keep
// blocking copies. Caller holds a.mu.
func (a *App) forgetLinkLocked(t *core.Task) {
	if t == nil || a.dupes == nil {
		return
	}
	if m := a.dupes.Check(dedupe.Entry{URL: t.URL}); m.Verdict == dedupe.Duplicate && m.Of.ID == t.ID {
		a.dupes.Remove(t.URL)
	}
}

// verifyTask checks a finished file against a checksum when one is available:
// a hash in the file name, or a sums file downloaded alongside it. A download
// that cannot be verified is left unmarked rather than shown as passing.
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
		// Tagged with the task id so the per-download log card finds it (see
		// taskTag).
		log.Printf("checksum %s: %v%s", name, err, taskTag(id))
		return
	}
	if !ok {
		verdict = "failed"
		log.Printf("checksum mismatch for %s%s", name, taskTag(id))
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
	// Only a mismatch fires; an unreadable hash is unverified, not wrong (see
	// script.TriggerChecksumFailed).
	if !ok {
		a.fireChecksumFailed(c)
	}
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
			// Both parsers reject the whole file over one bad line, which would
			// otherwise look like no checksum file at all.
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
