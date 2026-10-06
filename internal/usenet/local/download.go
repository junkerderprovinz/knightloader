package local

import (
	"fmt"
	"os"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/filemode"
	"github.com/junkerderprovinz/knightloader/internal/nntp"
	"github.com/junkerderprovinz/knightloader/internal/nzb"
	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

// download is one file being written: its part file, its segment map and how
// many bytes have landed.
type download struct {
	file    nzb.File
	part    string
	mapPath string
	f       *os.File

	mu    sync.Mutex
	m     *segMap
	bytes int64
}

// openDownload opens the part file and its map. A map without its part file
// is stale and starts over.
func openDownload(part string, file nzb.File) (*download, error) {
	d := &download{file: file, part: part, mapPath: part + mapSuffix}
	if _, err := os.Stat(part); err == nil {
		d.m = loadSegMap(d.mapPath, len(file.Segments))
	} else {
		d.m = &segMap{done: make([]bool, len(file.Segments))}
	}
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, filemode.File)
	if err != nil {
		return nil, err
	}
	d.f = f
	if d.m.size > 0 {
		if err := d.allocate(d.m.size); err != nil {
			f.Close()
			return nil, err
		}
	}
	// What the map already counts, at the ratio of the file's size to its
	// articles', since the decoded sizes were not kept.
	for i, done := range d.m.done {
		if done {
			d.bytes += file.Segments[i].Bytes
		}
	}
	if d.m.size > 0 && file.Bytes() > 0 {
		// In float64, as the product of two sizes overflows int64 past 3 GB.
		d.bytes = int64(float64(d.bytes) / float64(file.Bytes()) * float64(d.m.size))
	}
	return d, nil
}

// allocate grows the part file to size without writing it, which leaves a
// sparse file where the file system has them.
func (d *download) allocate(size int64) error {
	fi, err := d.f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < size {
		return d.f.Truncate(size)
	}
	return nil
}

func (d *download) pending() []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []int
	for i, done := range d.m.done {
		if !done {
			out = append(out, i)
		}
	}
	return out
}

// write puts article i at its offset. An article whose range does not fit the
// file its header names is treated as damaged, and so is one naming a file
// far larger than the .nzb lists, since that size becomes the part file's.
func (d *download) write(i int, p yenc.Part) error {
	if listed := d.file.Bytes(); listed > 0 && p.FileSize > 2*listed {
		return fmt.Errorf("%w: article %d names a file of %d bytes, the .nzb lists %d", nntp.ErrDamaged, i+1, p.FileSize, listed)
	}
	d.mu.Lock()
	if d.m.size == 0 && p.FileSize > 0 {
		if err := d.allocate(p.FileSize); err != nil {
			d.mu.Unlock()
			return err
		}
		d.m.size = p.FileSize
	}
	size := d.m.size
	d.mu.Unlock()
	if p.End() > size || p.FileSize != size {
		return fmt.Errorf("%w: article %d does not fit a file of %d bytes", nntp.ErrDamaged, i+1, size)
	}
	if _, err := d.f.WriteAt(p.Data, p.Begin); err != nil {
		return err
	}
	d.mu.Lock()
	d.m.done[i] = true
	d.bytes += int64(len(p.Data))
	d.mu.Unlock()
	return nil
}

// size is the file's size once an article has named it, and the size of its
// articles until then.
func (d *download) size() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m.size > 0 {
		return d.m.size
	}
	return d.file.Bytes()
}

func (d *download) loaded() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bytes
}

// save writes the map after the data it describes has reached the disk, so a
// crash never leaves the map claiming an article the file lacks.
func (d *download) save() {
	// Taken before the sync, as an article written during it may not be on
	// the disk yet.
	d.mu.Lock()
	snap := segMap{size: d.m.size, done: append([]bool(nil), d.m.done...)}
	d.mu.Unlock()
	if err := d.f.Sync(); err != nil {
		return
	}
	_ = snap.save(d.mapPath)
}

// close saves the map and closes the part file.
func (d *download) close() error {
	d.save()
	return d.f.Close()
}

// finish moves the whole part file onto target, or onto a counted name next
// to it when target exists, and drops the map.
func (d *download) finish(target string) (string, error) {
	res, err := collide.Handover(target, collide.Rename)
	if err != nil {
		return "", err
	}
	if err := os.Rename(d.part, res.Path); err != nil {
		return "", err
	}
	removeMap(d.mapPath)
	return res.Path, nil
}
