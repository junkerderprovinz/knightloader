// Collector intake for a torrent whose file selection is already known, such as
// an uploaded .torrent that has been through the file-tree step.
//
// This does not go through stage() (app_links.go), for two reasons:
//
//  1. TorrentFiles must be on the task before finishStaging's put runs.
//     addLinksFrom can trigger AutoConfirm and start the engine synchronously,
//     so a selection attached after staging could arrive after every file had
//     started downloading.
//  2. The size at stage time should reflect the selection, not the whole
//     torrent (see torrentSize). stage's generic Resolve returns no file list
//     to select over.
package app

import (
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// AddTorrent stages one torrent with files as its selection and runs it through
// the same filter, dedupe, naming and Packagizer steps as every other entrance.
// uri is a magnet or the data: URI an uploaded .torrent was encoded as (see
// torrent.EncodeBytes).
//
// files is trusted: routes_torrents.go builds it from its own parse of uri,
// never from a client-supplied list.
//
// It returns the staged task, or the held task when the filter parked it
// (Task.Skipped), or nil when the mirror set folded it into one already listed.
// The error is always nil; it matches AddLinksWithOptions.
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
	// Not a.Registry.For(uri) and Resolve: resolver.Result carries no file list
	// to compute a selection-aware size from. The registry still holds
	// torrent.Resolver for pasted magnets and re-resolving after a restart.
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
	// The naming pass other entrances get: nameBucket only when the caller named
	// no package, then catchAll. A lone task is a bucket of one.
	if pkg == "" {
		a.nameBucket(&bucket{tasks: []*core.Task{staged}})
	}
	a.catchAll([]*core.Task{staged})

	if a.Settings.Get().AutoConfirm {
		a.ConfirmTasks([]string{staged.ID}, confirm.Config{}, confirm.TriggerAutoConfirm)
	}
	return a.detached([]*core.Task{staged})[0], nil
}

// torrentHost is a torrent task's Host bucket, used here and by stage for a
// pasted magnet.
//
// hostOf falls back to the raw URL when there is no hostname, and for a torrent
// that would put a magnet query string, or the base64 of a whole .torrent, into
// the Host column and the "hoster" path variable. Two torrents are not two hosts
// for MaxPerHost either, so each kind gets one fixed bucket.
func torrentHost(uri string) string {
	switch {
	case torrent.IsMagnet(uri):
		return "torrent-magnet"
	case torrent.IsURI(uri):
		return "torrent-upload"
	}
	return hostOf(uri)
}

// torrentSize is the byte count to show at stage time: the selected subset
// rather than md.TotalSize, so a row does not show the whole torrent's size and
// then shrink when the engine starts.
//
// With no files (a pasted magnet whose tree has not arrived) it falls back to
// md.TotalSize, which is 0 for a magnet.
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
