package engine

// A finished torrent taken up again only to seed, after its files moved or
// after a restart (see Job.Seed). The library opens a torrent's files in the
// folder it was added in and keeps nothing across a restart, so such a torrent
// has to be added again; the library then checks the files it finds, calls the
// torrent done and seeds. That start and that finish belong to a download the
// app already has as done, so they go unreported, and the swarm readings carry
// on from where the earlier run left them.
//
// The library measures its seeding targets from the new start, so it would
// seed such a torrent to the full targets once more. The engine stops it
// itself when the totals reach them. A torrent started to seed by hand counts
// them from its mark instead, which is where the library counts from as well.
//
// A seed run never fetches. The library fetches a file it finds missing or of
// the wrong size, and takes one of the right size as whole without reading
// it, so a file that is not there at its full size ends the run before the
// library starts on the torrent. A run for files that did not come from the
// swarm (Job.Verify) also has every piece checked against the torrent first,
// where the link carries the pieces, which a magnet link does not.

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// seedRun is one torrent started only to seed: from is what it had uploaded
// before, mark where its targets count from, and seeding is whether the
// library has found its files complete.
type seedRun struct {
	from    core.TorrentStats
	mark    core.SeedMark
	seeding bool
}

// notInPlace is a seed run whose files are not as the torrent has them.
type notInPlace struct{ why string }

func (e *notInPlace) Error() string { return e.why }

// seedFiles checks that every chosen file of a resolved torrent is in dir at
// its full size. sel is the chosen files by index, nil for all of them. A file
// of no bytes is left out, since the library creates it as it resolves.
func seedFiles(dir string, res *base.Resource, sel []int) error {
	for i, f := range res.Files {
		if f == nil || f.Size == 0 || sel != nil && !slices.Contains(sel, i) {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(res.Name), filepath.FromSlash(f.Path), f.Name)
		if err := sizeIs(p, f.Size); err != nil {
			return err
		}
	}
	return nil
}

func sizeIs(p string, size int64) error {
	fi, err := os.Stat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &notInPlace{why: p + " is missing"}
	case err != nil:
		return &notInPlace{why: err.Error()}
	case fi.Size() != size:
		return &notInPlace{why: fmt.Sprintf("%s has %d bytes, the torrent says %d", p, fi.Size(), size)}
	}
	return nil
}

// CheckPieces checks every file and every piece of the .torrent in link against
// what is in dir, where the torrent lands. A magnet link carries no pieces and
// passes.
func CheckPieces(dir, link string) error {
	if torrent.IsMagnet(link) {
		return nil
	}
	raw, err := torrent.DecodeBytes(link)
	if err != nil {
		return err
	}
	mi, err := metainfo.Load(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return err
	}
	if !info.HasV1() {
		return nil
	}
	var files []*openLater
	var ends []int64
	var readers []io.Reader
	var total int64
	for _, f := range info.UpvertedFiles() {
		p := filepath.Join(dir, info.BestName(), filepath.Join(f.BestPath()...))
		total += f.Length
		if f.Length == 0 {
			continue
		}
		if err := sizeIs(p, f.Length); err != nil {
			return err
		}
		o := &openLater{path: p, size: f.Length}
		files, ends, readers = append(files, o), append(ends, total), append(readers, o)
	}
	defer func() {
		for _, o := range files {
			o.close()
		}
	}()
	data := io.MultiReader(readers...)
	buf := make([]byte, info.PieceLength)
	for i := range info.NumPieces() {
		n, err := io.ReadFull(data, buf)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		if sum := sha1.Sum(buf[:n]); !bytes.Equal(sum[:], info.Piece(i).V1Hash().Unwrap().Bytes()) {
			// A piece can span files, and any of them may hold the wrong bytes.
			from, to := int64(i)*info.PieceLength, int64(i)*info.PieceLength+int64(n)
			var in []string
			for f, end := range ends {
				if end > from && end-files[f].size < to {
					in = append(in, files[f].path)
				}
			}
			return &notInPlace{why: strings.Join(in, " or ") + " does not match the torrent"}
		}
	}
	return nil
}

// openLater is a file opened on its first read, so a torrent of thousands of
// files is read with one of them open at a time.
type openLater struct {
	path string
	size int64
	f    *os.File
}

func (o *openLater) Read(b []byte) (int, error) {
	if o.f == nil {
		f, err := os.Open(o.path)
		if err != nil {
			return 0, err
		}
		o.f = f
	}
	n, err := o.f.Read(b)
	if err == io.EOF {
		o.close()
	}
	return n, err
}

func (o *openLater) close() {
	if o.f != nil {
		o.f.Close()
		o.f = nil
	}
}

// seedTargets are the seeding targets as the settings last set them, zero
// meaning no target.
type seedTargets struct {
	ratio   float64
	seconds int64
}

// reached reports whether a torrent's totals meet one of the targets.
func (t seedTargets) reached(s core.TorrentStats) bool {
	return (t.ratio > 0 && s.Ratio >= t.ratio) || (t.seconds > 0 && s.SeedSeconds >= t.seconds)
}

// seedOf is a copy of a task's seed run, and false for any other task.
func (e *Engine) seedOf(taskID string) (seedRun, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if sd := e.seeds[taskID]; sd != nil {
		return *sd, true
	}
	return seedRun{}, false
}

// seedEvent is onEvent for a torrent started only to seed. Done is the files
// found complete, from when on the torrent seeds; an error ends the seeding.
// The rest is the download's and goes unreported.
func (e *Engine) seedEvent(taskID string, ev *download.Event) {
	switch ev.Key {
	case download.EventKeyDone:
		e.mu.Lock()
		if sd := e.seeds[taskID]; sd != nil {
			sd.seeding = true
		}
		e.mu.Unlock()
		if s, _, ok := e.readTorrentStats(ev.Task.ID); ok {
			e.emit(taskID, core.Update{Torrent: e.seedStats(taskID, s)})
		}
	case download.EventKeyError:
		e.endSeed(taskID, ev.Err)
	}
}

// seedStats adds what a seed run had uploaded before to a reading of the
// library's, which counts from the new start.
func (e *Engine) seedStats(taskID string, s core.TorrentStats) *core.TorrentStats {
	if sd, ok := e.seedOf(taskID); ok {
		s.Uploaded += sd.from.Uploaded
		s.Ratio += sd.from.Ratio
		s.SeedSeconds += sd.from.SeedSeconds
	}
	return &s
}

// seedDone reports whether a seed run's totals have reached a target since its
// mark, and if so takes the torrent out of the library, which closes its
// upload, and out of the poll. The files stay, and so does the rest of what
// the engine knows of the task, for a later removal with its files.
func (e *Engine) seedDone(taskID, gid string, s core.TorrentStats) bool {
	e.mu.Lock()
	var mark core.SeedMark
	if sd := e.seeds[taskID]; sd != nil {
		mark = sd.mark
	}
	reached := e.targets.reached(mark.Since(s))
	e.mu.Unlock()
	if !reached {
		return false
	}
	e.forgetTorrent(taskID)
	e.dropTorrent(gid)
	e.dropUnpicked(taskID, true)
	return true
}

// endSeed reports a seed run that could not start or broke off as seeding no
// more, with the totals it had, and leaves the task done.
func (e *Engine) endSeed(taskID string, err error) {
	e.mu.Lock()
	sd := e.seeds[taskID]
	delete(e.seeds, taskID)
	e.mu.Unlock()
	if sd == nil {
		return
	}
	log.Printf("task %s could not go on seeding: %v", taskID, err)
	st := &core.TorrentStats{Uploaded: sd.from.Uploaded, Ratio: sd.from.Ratio, SeedSeconds: sd.from.SeedSeconds}
	var nip *notInPlace
	if errors.As(err, &nip) {
		st.NotSeeded = nip.why
	}
	e.emit(taskID, core.Update{Torrent: st})
}

// failStart reports a torrent start that failed: as an error on the task, or
// for a start only to seed, as the end of its seeding.
func (e *Engine) failStart(j Job, err error) {
	if j.Seed {
		e.endSeed(j.TaskID, err)
		return
	}
	e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
}
