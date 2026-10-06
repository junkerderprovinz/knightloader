package par2

import (
	"context"
	"crypto/md5"
	"hash/crc32"
	"io"
	"os"
	"runtime"
	"sync"
)

// Options tunes Verify and Repair.
type Options struct {
	// Workers is how many files Verify reads at once, and how many parts a
	// repair is split into. GOMAXPROCS when 0.
	Workers int
	// Memory caps the buffers a repair holds, 256 MiB when 0. Below about
	// two slices per damaged slice the repair reads every file more than
	// once.
	Memory int64
	// Trusted reports whether the file at path is known to have arrived
	// whole, as a download does whose every article matched its CRC. Such a
	// file is taken as good when its size and the hash of its first 16 KiB
	// match, without reading the rest.
	Trusted func(path string) bool
	// Progress hears how far the work has got, in bytes read or written of
	// the total it will read and write. It is called from one goroutine at
	// a time.
	Progress func(done, total int64)
}

func (o Options) workers() int {
	if o.Workers > 0 {
		return o.Workers
	}
	return runtime.GOMAXPROCS(0)
}

// FileReport is what Verify found of one file of the set.
type FileReport struct {
	// Path is where the file is, "" when it was found nowhere.
	Path string
	// Size is the size of the file at Path.
	Size int64
	// Bad lists the slices of the file that are damaged or missing, by their
	// place in the file.
	Bad []int
}

// Report is what Verify found, one FileReport per file of the set.
type Report struct {
	Files []FileReport
}

// Damaged is how many slices have to be rebuilt.
func (r *Report) Damaged() int {
	n := 0
	for _, f := range r.Files {
		n += len(f.Bad)
	}
	return n
}

// Whole reports whether every file is there with the right content and size.
func (r *Report) Whole(s *Set) bool {
	for i, f := range r.Files {
		if f.Path == "" || len(f.Bad) > 0 || f.Size != s.Files[i].Size {
			return false
		}
	}
	return true
}

// Verify checks the files at paths, one per file of the set as Match returns
// them, slice by slice.
func Verify(ctx context.Context, s *Set, paths []string, o Options) (*Report, error) {
	rep := &Report{Files: make([]FileReport, len(s.Files))}
	var todo []int
	var total int64
	for i, f := range s.Files {
		r := &rep.Files[i]
		r.Path = paths[i]
		fi, err := os.Stat(r.Path)
		if r.Path == "" || err != nil {
			r.Path = ""
			r.Bad = allSlices(f)
			continue
		}
		r.Size = fi.Size()
		if o.Trusted != nil && r.Size == f.Size && o.Trusted(r.Path) && head16k(r.Path) == f.Hash16k {
			continue
		}
		todo = append(todo, i)
		total += min(r.Size, f.Size)
	}

	m := newMeter(total, o.Progress)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	queue := make(chan int)
	for range min(o.workers(), len(todo)) {
		wg.Go(func() {
			for i := range queue {
				bad, err := scanFile(ctx, rep.Files[i].Path, s.Files[i], s.SliceSize, m)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = err
				}
				rep.Files[i].Bad = bad
				mu.Unlock()
			}
		})
	}
	for _, i := range todo {
		queue <- i
	}
	close(queue)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	m.finish()
	return rep, nil
}

func allSlices(f File) []int {
	out := make([]int, len(f.Checks))
	for i := range out {
		out[i] = i
	}
	return out
}

// chunk is how much of a file is read at a time.
const chunk = 1 << 20

var zeros [chunk]byte

// scanFile reads the file at path once, hashing it whole and taking the CRC
// of each slice. A file whose hash matches is whole. Otherwise a slice is bad
// when its CRC differs, and a slice whose CRC matches is read again for its
// MD5, the one check a slice of zeros where data was missing cannot pass by
// chance.
func scanFile(ctx context.Context, path string, f File, sliceSize int64, m *meter) ([]int, error) {
	r, err := os.Open(path)
	if err != nil {
		return allSlices(f), nil
	}
	defer r.Close()
	whole := md5.New()
	crcs := make([]uint32, len(f.Checks))
	buf := make([]byte, chunk)
	short := false
	for i := range f.Checks {
		want := min(sliceSize, f.Size-int64(i)*sliceSize)
		var got int64
		crc := uint32(0)
		for got < want && !short {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			n, err := io.ReadFull(r, buf[:min(chunk, want-got)])
			crc = crc32.Update(crc, crc32.IEEETable, buf[:n])
			whole.Write(buf[:n])
			got += int64(n)
			m.add(int64(n))
			if err != nil {
				short = true
			}
		}
		// Zeros stand in for what the file lacks and pad the last slice.
		for pad := sliceSize - got; pad > 0; {
			n := min(pad, chunk)
			crc = crc32.Update(crc, crc32.IEEETable, zeros[:n])
			pad -= n
		}
		crcs[i] = crc
	}
	if !short && [16]byte(whole.Sum(nil)) == f.Hash {
		return nil, nil
	}
	var bad []int
	slice := make([]byte, sliceSize)
	for i, c := range f.Checks {
		if crcs[i] != c.CRC {
			bad = append(bad, i)
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, _ := r.ReadAt(slice[:min(sliceSize, f.Size-int64(i)*sliceSize)], int64(i)*sliceSize)
		clear(slice[n:])
		if md5.Sum(slice) != c.MD5 {
			bad = append(bad, i)
		}
	}
	return bad, nil
}

// meter adds up progress from several goroutines and passes it on now and
// then, at most a few hundred times over the whole run.
type meter struct {
	mu          sync.Mutex
	done, total int64
	last        int64
	fn          func(done, total int64)
}

func newMeter(total int64, fn func(done, total int64)) *meter {
	return &meter{total: total, fn: fn}
}

func (m *meter) add(n int64) {
	if m.fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done += n
	if m.done-m.last >= max(m.total/256, chunk) {
		m.last = m.done
		m.fn(m.done, m.total)
	}
}

func (m *meter) finish() {
	if m.fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fn(m.total, m.total)
}
