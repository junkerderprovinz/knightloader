package engine

// A finished torrent taken up again only to seed, after its files moved or
// after a restart (see Job.Seed). Its start and its finish belong to a
// download the app already has as done, so they go unreported, and the swarm
// readings carry on from where the earlier run left them.
//
// A seed run never fetches. The client takes an uploaded .torrent's files as
// complete once they are in place at their full size, so a file that is not
// ends the run before the client gets the torrent. A run for files that did
// not come from the swarm (Job.Verify) also has every piece checked against
// the torrent first. A magnet carries no pieces, and the client checks what it
// finds.

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// notInPlace is a seed run whose files are not as the torrent has them.
type notInPlace struct{ why string }

func (e *notInPlace) Error() string { return e.why }

// seedFiles checks that every chosen file of a torrent is in dir at its full
// size. sel is the chosen files by index, nil for all of them. A file of no
// bytes is left out, since the client creates it itself.
func seedFiles(dir string, c content, sel []int) error {
	for i, f := range c.files {
		if f.Size == 0 || sel != nil && !slices.Contains(sel, i) {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(c.name), filepath.FromSlash(f.Path))
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
// passes, as does a torrent of BitTorrent v2 alone. It stops with ctx, so a
// hash of a large download does not hold up whoever gave up on it.
func CheckPieces(ctx context.Context, dir, link string) error {
	if torrent.IsMagnet(link) {
		return nil
	}
	raw, err := torrent.DecodeBytes(link)
	if err != nil {
		return err
	}
	info, _, err := torrent.ReadInfo(raw)
	if err != nil {
		return err
	}
	if len(info.Pieces) == 0 {
		return nil
	}
	if info.PieceLength <= 0 || len(info.Pieces)%sha1.Size != 0 {
		return errors.New("the torrent's pieces cannot be read")
	}
	var files []*openLater
	var ends []int64
	var readers []io.Reader
	var total int64
	for _, f := range info.Files {
		p := filepath.Join(dir, info.Name, filepath.Join(f.Path...))
		total += f.Length
		if f.Length == 0 {
			continue
		}
		if f.Pad {
			readers = append(readers, io.LimitReader(zeros{}, f.Length))
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
	for i := range len(info.Pieces) / sha1.Size {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := io.ReadFull(data, buf)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		want := info.Pieces[i*sha1.Size : (i+1)*sha1.Size]
		if sum := sha1.Sum(buf[:n]); !bytes.Equal(sum[:], want) {
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

type zeros struct{}

func (zeros) Read(b []byte) (int, error) {
	clear(b)
	return len(b), nil
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

// failStart reports a torrent start that failed: as an error on the task, or
// for a start only to seed, as the end of its seeding with the totals it had.
func (e *Engine) failStart(j Job, err error) {
	if !j.Seed {
		e.emit(j.TaskID, core.Update{Status: core.StatusError, Err: err.Error()})
		return
	}
	e.endRun(&torrentRun{job: j, from: j.SeedFrom}, err)
}
