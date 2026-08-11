// Collector intake for a torrent whose file selection is already known: an
// uploaded .torrent that has been through the file-tree step, or (in principle)
// any other caller that already has a concrete TorrentFile list to stage with.
//
// This does not go through stage() (app_links.go), despite doing almost the
// same thing, and that is a deliberate choice worth spelling out because it
// looks like duplication at a glance:
//
//  1. TorrentFiles has to be on the Task struct literal BEFORE finishStaging's
//     put() ever runs. addLinksFrom (stage's only caller) can trigger
//     AutoConfirm - and therefore ConfirmTasks, and therefore the engine
//     starting the download - synchronously, before it returns. A selection
//     attached to the task AFTER staging would be racing the very engine job
//     it exists to shape: on an instance with AutoConfirm on, a multi-file
//     torrent could start fetching every file a heartbeat before the
//     selection landed. Building the struct literal with TorrentFiles already
//     set, the way this file does, makes that race structurally impossible
//     rather than merely unlikely.
//  2. A torrent's Size at STAGE time should already reflect the selection, not
//     the whole torrent - see torrentSize's own comment, which is the same
//     lesson internal/engine/torrent.go's torrentMeta already learned live
//     ("a resolve limited to one 1.5 KB subtitle still reported 129 MB").
//     stage()'s generic Resolve() call cannot know this, because the plain
//     Resolver interface's Result carries no file list to select over; this
//     file already has the list, from the same parse the file tree was drawn
//     from, so it computes the honest number from the start.
package app

import (
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// AddTorrent stages one torrent - uri is the magnet URI or the data: URI an
// uploaded .torrent was re-expressed as (see torrent.EncodeBytes) - with files
// as its selection, and runs it through the same filter, dedupe, naming and
// Packagizer pipeline every other entrance uses (see app_links.go's stage).
//
// files is trusted to already be the RIGHT list: the caller (routes_torrents.go)
// builds it by taking its OWN fresh parse of uri - never a client-supplied file
// list - and turning on Selected for whichever paths the caller asked to keep.
// This function does not re-derive it, so a caller that hands it a list from
// somewhere untrusted has skipped the one check that matters; see
// routes_torrents.go's own comment for where that check actually lives.
//
// It returns (task, nil) for an ordinary stage, (held, nil) for a link the
// filter parked instead (Task.Skipped is how a caller tells the two apart, same
// as everywhere else in this app), and (nil, nil) for a link the mirror set
// folded into one already in the list. The only non-nil error is a batch
// destination that failed validation before this ran - there is none of that
// here, so today this never returns an error; it is on the signature because
// every other staging entry point (AddLinksWithOptions) has one and a second
// convention for "this one cannot fail" is not worth it for one caller.
func (a *App) AddTorrent(uri string, files []core.TorrentFile, pkg string, origin core.Origin) (*core.Task, error) {
	now := time.Now()
	cand := rules.Candidate{URL: uri, Package: pkg, Added: now}
	if v := a.filter(cand); v.Rejected {
		return a.hold(cand, v, origin, now), nil
	}
	if m := a.mirror(dedupe.Entry{URL: uri}); m.Seen() && !a.keepsAsSibling(m) {
		a.recordSkipped(uri, m)
		return nil, nil
	}

	t := &core.Task{
		URL:          uri,
		Name:         uri,
		Package:      pkg,
		Status:       core.StatusCollected,
		Enabled:      true,
		Origin:       origin,
		Host:         torrentHost(uri),
		CreatedAt:    now,
		TorrentFiles: files,
	}
	// Deliberately NOT a.Registry.For(uri)+Resolve(): this function is already
	// torrent-specific by construction (its caller has just parsed a .torrent
	// or described a magnet to get `files` in the first place), and the plain
	// resolver.Result the registry path returns has no file list to compute an
	// honest, selection-aware size from - see the package comment above.
	// a.Registry still has to hold torrent.Resolver{} for anything to route a
	// PASTED magnet or re-resolve this task after a restart (see app.go's own
	// boot-time Register call); this call sites around it rather than through
	// it only for the one response this staging moment needs.
	res := torrent.Resolver{}
	t.Resolver = res.Info().ID
	if md, err := res.Describe(uri); err != nil {
		t.Error = err.Error()
		t.Reason = classify(failure{err: err})
	} else {
		if md.Name != "" {
			t.Name = md.Name
		}
		t.Size = torrentSize(md, files)
		t.InfoHash = md.InfoHash
		t.Trackers = md.Trackers
	}

	cand.Filename, cand.Filesize = filename(t), t.Size
	if v := a.filter(cand); v.Rejected {
		return a.hold(cand, v, origin, now), nil
	}

	staged := a.finishStaging(t, cand)
	if staged == nil {
		return nil, nil
	}
	// The naming pass every other entrance gets before finishStaging - see
	// addLinksFrom's own two-pass comment for why nameBucket only runs when the
	// caller named no package, and catchAll for whatever is left over either
	// way. A lone task stands in for the batch-of-one it is; nameBucket has
	// judged a single-task bucket since the day it was written, for exactly a
	// plain paste with nothing to name it.
	if pkg == "" {
		a.nameBucket(&bucket{tasks: []*core.Task{staged}})
	}
	a.catchAll([]*core.Task{staged})

	if a.Settings.Get().AutoConfirm {
		a.ConfirmTasks([]string{staged.ID}, confirm.Config{}, confirm.TriggerAutoConfirm)
	}
	return a.detached([]*core.Task{staged})[0], nil
}

// torrentHost is a torrent task's Host bucket, in place of the generic
// hostOf(t.URL) every other intake path uses. Called from here for an
// uploaded .torrent, and from app_links.go's stage for a pasted magnet -
// neither is a link with a single host to bucket by, so both go through this
// instead.
//
// hostOf falls back to the RAW URL string whenever url.Parse finds no
// hostname, which is true of both a magnet and an uploaded .torrent's data:
// URI - neither names a single host at all. That fallback is wrong for both,
// not just the upload case a size argument alone would flag: whatever lands
// in Task.Host also becomes the "hoster" path variable a naming template can
// put straight into a folder name (internal/pathvars) and the value the
// frontend's Host column shows for the row, and neither is well served by a
// magnet's own query string, however short it happens to be. An uploaded
// .torrent's data: URI is the sharper version of the same problem - the
// base64 of the whole file, up to torrent.MaxTorrentBytes - so both get a
// short fixed bucket instead of hostOf's answer, one bucket per torrent kind
// rather than per torrent, since two magnets are not two hosts in any sense
// cfg.MaxPerHost or a "hoster" folder name means it.
func torrentHost(uri string) string {
	switch {
	case torrent.IsMagnet(uri):
		return "torrent-magnet"
	case torrent.IsURI(uri):
		return "torrent-upload"
	}
	return hostOf(uri)
}

// torrentSize is the byte count to show at STAGE time, before a single
// connection has opened.
//
// It is the selected subset, not md.TotalSize - the whole-torrent figure a
// plain Describe/Resolve reports regardless of what will actually be fetched.
// internal/engine/torrent.go's torrentMeta already had to learn this lesson
// live, at START time, past the point this function runs: "a resolve limited
// to one 1.5 KB subtitle still reported 129 MB... a row that reads 129 GB for
// a minute and then 4 MB is a row nobody believes the second time." This
// function exists so the collector never shows the misleading number in the
// first place - files is already the caller's resolved selection, computed
// from the same parse md came from, so there is nothing left to wait for.
//
// Empty files (a freshly pasted magnet, whose tree has not arrived from the
// swarm yet) falls back to md.TotalSize, which is 0 for a magnet Describe
// answers today - an honest "not yet known" rather than a guess.
func torrentSize(md torrent.Metadata, files []core.TorrentFile) int64 {
	if len(files) == 0 {
		return md.TotalSize
	}
	var size int64
	for _, f := range files {
		if f.Selected {
			size += f.Size
		}
	}
	return size
}
