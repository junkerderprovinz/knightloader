package engine

// Reading a torrent's file while it downloads. The client writes each piece
// into the file at its place once the piece is whole and checked, so a read
// waits until the client counts the pieces under it as here, and then reads
// the file on disk. While anybody reads, the torrent fetches its pieces in
// order, the first and last of each file before the rest, since players read
// a container's index there.

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
)

// streamTorrent opens file, by its index in the torrent, of a run.
func (e *Engine) streamTorrent(run *torrentRun, file int) (*torrentReader, error) {
	bt := e.client()
	e.mu.Lock()
	count := len(run.c.files)
	e.mu.Unlock()
	if bt == nil || file < 0 || file >= count {
		return nil, ErrNotStreaming
	}
	start := e.fileStart(bt, run, file)
	e.mu.Lock()
	f := run.c.files[file]
	r := &torrentReader{
		e: e, run: run, bt: bt,
		path:  filepath.Join(filepath.Dir(run.root), filepath.FromSlash(run.c.landing()[file])),
		start: start, size: f.Size,
	}
	run.streams++
	first := run.streams == 1
	e.mu.Unlock()
	if first {
		ctx, cancel := context.WithTimeout(e.ctx, clientWait)
		defer cancel()
		if err := bt.Stream(ctx, run.hash, true); err != nil {
			log.Printf("task %s: the torrent could not be set to fetch in order: %v", run.job.TaskID, err)
		}
	}
	return r, nil
}

// fileStart is where file begins in the torrent's bytes, which is where the
// client's pieces place it. Padding files take room there but are left out of
// the client's file list, so the .torrent says where each file starts; a
// torrent whose .torrent cannot be had is taken to have no padding.
func (e *Engine) fileStart(bt torrents.Client, run *torrentRun, file int) int64 {
	e.mu.Lock()
	starts, files := run.starts, run.c.files
	e.mu.Unlock()
	if starts == nil {
		starts = e.readStarts(bt, run, len(files))
		if starts == nil {
			starts = make([]int64, len(files))
			for i := 1; i < len(files); i++ {
				starts[i] = starts[i-1] + files[i-1].Size
			}
		}
		e.mu.Lock()
		run.starts = starts
		e.mu.Unlock()
	}
	return starts[file]
}

// readStarts is where each of the count files of a run's torrent begins, by
// its .torrent: the upload's own, or the client's for a magnet. It is nil
// when there is no .torrent or it names other files.
func (e *Engine) readStarts(bt torrents.Client, run *torrentRun, count int) []int64 {
	var raw []byte
	var err error
	if torrent.IsMagnet(run.job.URL) {
		ctx, cancel := context.WithTimeout(e.ctx, clientWait)
		raw, err = bt.Metainfo(ctx, run.hash)
		cancel()
	} else {
		raw, err = torrent.DecodeBytes(run.job.URL)
	}
	if err != nil {
		return nil
	}
	info, _, err := torrent.ReadInfo(raw)
	if err != nil {
		return nil
	}
	if starts := info.Offsets(); len(starts) == count {
		return starts
	}
	return nil
}

// torrentReader is one file of a torrent, read as its pieces arrive.
type torrentReader struct {
	e   *Engine
	run *torrentRun
	bt  torrents.Client
	// path is the file on disk, start where it begins in the torrent's bytes
	// and size its length.
	path        string
	start, size int64

	mu     sync.Mutex
	pos    int64
	f      *os.File
	closed bool
}

func (r *torrentReader) Read(p []byte) (int, error) {
	return r.ReadContext(context.Background(), p)
}

// ReadContext reads at most up to the end of the piece under the position,
// once that piece is here.
func (r *torrentReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, os.ErrClosed
	}
	if r.pos >= r.size {
		return 0, io.EOF
	}
	length, err := r.await(ctx, r.start+r.pos)
	if err != nil {
		return 0, err
	}
	pieceEnd := ((r.start+r.pos)/length+1)*length - r.start
	n := min(int64(len(p)), r.size-r.pos, pieceEnd-r.pos)
	if r.f == nil {
		f, err := os.Open(r.path)
		if err != nil {
			return 0, err
		}
		r.f = f
	}
	got, err := r.f.ReadAt(p[:n], r.pos)
	r.pos += int64(got)
	if errors.Is(err, io.EOF) && got > 0 {
		err = nil
	}
	return got, err
}

// await waits until the piece holding byte at of the torrent is here, and
// returns the piece size.
func (r *torrentReader) await(ctx context.Context, at int64) (int64, error) {
	tick := time.NewTicker(filesPoll)
	defer tick.Stop()
	for {
		pc, err := r.bt.Pieces(ctx, r.run.hash)
		if err != nil {
			return 0, err
		}
		if pc.Length <= 0 {
			return 0, errors.New("the torrent client named no piece size")
		}
		if i := at / pc.Length; i < int64(len(pc.States)) && pc.States[i] == torrents.PieceHere {
			return pc.Length, nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-r.e.done:
			return 0, errShuttingDown
		}
	}
}

func (r *torrentReader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch whence {
	case io.SeekCurrent:
		offset += r.pos
	case io.SeekEnd:
		offset += r.size
	}
	if offset < 0 {
		return 0, errors.New("seek before the start of the file")
	}
	r.pos = offset
	return offset, nil
}

// Close lets the torrent fetch in its own order again once nobody reads it.
func (r *torrentReader) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	if r.f != nil {
		r.f.Close()
	}
	r.mu.Unlock()
	e := r.e
	e.mu.Lock()
	r.run.streams--
	last := r.run.streams == 0
	e.mu.Unlock()
	if last {
		ctx, cancel := context.WithTimeout(e.ctx, clientWait)
		defer cancel()
		_ = r.bt.Stream(ctx, r.run.hash, false)
	}
	return nil
}
