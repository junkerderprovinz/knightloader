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

	"github.com/junkerderprovinz/knightloader/internal/torrents"
)

// streamTorrent opens file, by its index in the torrent, of a run.
func (e *Engine) streamTorrent(run *torrentRun, file int) (*torrentReader, error) {
	bt := e.client()
	e.mu.Lock()
	if bt == nil || file < 0 || file >= len(run.c.files) {
		e.mu.Unlock()
		return nil, ErrNotStreaming
	}
	var start int64
	for _, f := range run.c.files[:file] {
		start += f.Size
	}
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
