package par2

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/filemode"
)

// NotEnoughError is a set with more damaged slices than recovery slices to
// rebuild them from.
type NotEnoughError struct {
	Damaged, Recovery int
}

func (e *NotEnoughError) Error() string {
	return fmt.Sprintf("par2: %d slices are damaged or missing, and the recovery files hold %d", e.Damaged, e.Recovery)
}

// ErrMismatch is a rebuilt file whose hash is not the one the set gives it.
// It happens when a file taken as whole was not.
var ErrMismatch = errors.New("par2: a repaired file does not match its hash")

const defaultMemory = 256 << 20

// Repair rebuilds every slice rep names as bad, writing it into its file in
// place, creates a missing file in dir, cuts a file to its size, and then
// checks each file it wrote against the hash the set gives it.
//
// Each damaged slice is the solution of a linear system over GF(2^16) with
// one equation per recovery slice used. The work goes through the slices a
// stretch of bytes at a time, as wide as the memory budget allows: the
// recovery slices' stretches are read, every intact slice's stretch is
// multiplied in, and the damaged slices' stretches come out of the inverted
// system. The rows are split over the workers.
func Repair(ctx context.Context, s *Set, rep *Report, dir string, o Options) error {
	type target struct {
		file  int
		slice int
	}
	var lost []target
	var globals []int
	isLost := map[int]bool{}
	for i, f := range rep.Files {
		for _, b := range f.Bad {
			g := s.Files[i].first + b
			lost = append(lost, target{i, b})
			globals = append(globals, g)
			isLost[g] = true
		}
	}
	k := len(lost)
	if k > len(s.Recovery) {
		return &NotEnoughError{Damaged: k, Recovery: len(s.Recovery)}
	}

	consts := inputConstants(s.Slices())
	var rows []Recovery
	var inv [][]uint16
	if k > 0 {
		var ok bool
		rows, inv, ok = solve(s, consts, globals)
		if !ok {
			return fmt.Errorf("par2: the recovery slices cannot rebuild these %d slices", k)
		}
	}

	// Every file that is written is opened for writing, a missing one is
	// created, and each is given its size, so a short file reads as zeros
	// where it ends and the rebuilt slices fit.
	files := make([]*os.File, len(s.Files))
	defer func() {
		for _, f := range files {
			if f != nil {
				f.Close()
			}
		}
	}()
	var written []int
	for i, f := range s.Files {
		r := &rep.Files[i]
		fix := r.Path == "" || len(r.Bad) > 0 || r.Size != f.Size
		var err error
		switch {
		case r.Path == "":
			r.Path = filepath.Join(dir, f.BaseName())
			files[i], err = os.OpenFile(r.Path, os.O_RDWR|os.O_CREATE, filemode.File)
		case fix:
			files[i], err = os.OpenFile(r.Path, os.O_RDWR, 0)
		default:
			files[i], err = os.Open(r.Path)
		}
		if err != nil {
			return err
		}
		if fix {
			if err := files[i].Truncate(f.Size); err != nil {
				return err
			}
			r.Size = f.Size
			written = append(written, i)
		}
	}

	var check int64
	for _, i := range written {
		check += s.Files[i].Size
	}
	present := s.Slices() - k
	m := newMeter(int64(present+2*k)*s.SliceSize+check, o.Progress)

	if k > 0 {
		workers := o.workers()
		budget := o.Memory
		if budget <= 0 {
			budget = defaultMemory
		}
		const reads = 3
		width := budget / int64(k+workers+reads)
		width = max(8, min(width, s.SliceSize)) &^ 7

		// The constant of each intact slice raised to each row's exponent,
		// worked out once rather than in every pass.
		type input struct {
			file  int
			slice int
			coef  []uint16
		}
		inputs := make([]input, 0, present)
		for i, f := range s.Files {
			for b := range f.Checks {
				g := f.first + b
				if isLost[g] {
					continue
				}
				coef := make([]uint16, k)
				for r, row := range rows {
					coef[r] = gfPow(consts[g], row.Exponent)
				}
				inputs = append(inputs, input{i, b, coef})
			}
		}
		recovery := map[string]*os.File{}
		defer func() {
			for _, f := range recovery {
				f.Close()
			}
		}()
		for _, row := range rows {
			if recovery[row.Path] == nil {
				f, err := os.Open(row.Path)
				if err != nil {
					return err
				}
				recovery[row.Path] = f
			}
		}

		acc := make([][]byte, k)
		for r := range acc {
			acc[r] = make([]byte, width)
		}
		parts := split(k, workers)
		for off := int64(0); off < s.SliceSize; off += width {
			n := min(width, s.SliceSize-off)
			for r, row := range rows {
				if _, err := recovery[row.Path].ReadAt(acc[r][:n], row.Offset+off); err != nil {
					return fmt.Errorf("par2: reading a recovery slice: %w", err)
				}
			}
			m.add(int64(k) * n)

			// One goroutine reads the intact slices ahead while the workers
			// multiply in the one before.
			type piece struct {
				in  int
				buf []byte
				err error
			}
			pool := make(chan []byte, reads)
			for range reads {
				pool <- make([]byte, width)
			}
			pieces := make(chan piece, reads)
			rctx, stop := context.WithCancel(ctx)
			go func() {
				defer close(pieces)
				for in, x := range inputs {
					var buf []byte
					select {
					case buf = <-pool:
					case <-rctx.Done():
						return
					}
					err := readSlice(files[x.file], s.Files[x.file], s.SliceSize, x.slice, off, buf[:n])
					select {
					case pieces <- piece{in, buf, err}:
					case <-rctx.Done():
						return
					}
				}
			}()
			var failed error
			for p := range pieces {
				if failed == nil {
					failed = p.err
				}
				if failed == nil {
					failed = ctx.Err()
				}
				if failed != nil {
					stop()
					continue
				}
				coef := inputs[p.in].coef
				var wg sync.WaitGroup
				for _, part := range parts {
					wg.Go(func() {
						var t mulTable
						for r := part[0]; r < part[1]; r++ {
							t.set(coef[r])
							t.mulAdd(acc[r][:n], p.buf[:n])
						}
					})
				}
				wg.Wait()
				pool <- p.buf
				m.add(n)
			}
			stop()
			if failed != nil {
				return failed
			}

			var (
				wg   sync.WaitGroup
				mu   sync.Mutex
				werr error
			)
			for _, part := range split(k, workers) {
				wg.Go(func() {
					var t mulTable
					out := make([]byte, n)
					for c := part[0]; c < part[1]; c++ {
						clear(out)
						for r := range k {
							t.set(inv[c][r])
							t.mulAdd(out, acc[r][:n])
						}
						tg := lost[c]
						f := s.Files[tg.file]
						start := int64(tg.slice) * s.SliceSize
						valid := min(s.SliceSize, f.Size-start)
						if off < valid {
							if _, err := files[tg.file].WriteAt(out[:min(n, valid-off)], start+off); err != nil {
								mu.Lock()
								if werr == nil {
									werr = err
								}
								mu.Unlock()
							}
						}
					}
				})
			}
			wg.Wait()
			if werr != nil {
				return werr
			}
			m.add(int64(k) * n)
		}
	}

	for _, i := range written {
		if err := files[i].Sync(); err != nil {
			return err
		}
		h := md5.New()
		if _, err := io.Copy(h, io.NewSectionReader(files[i], 0, s.Files[i].Size)); err != nil {
			return err
		}
		m.add(s.Files[i].Size)
		if [16]byte(h.Sum(nil)) != s.Files[i].Hash {
			return fmt.Errorf("%w: %s", ErrMismatch, s.Files[i].Name)
		}
		rep.Files[i].Bad = nil
	}
	m.finish()
	return nil
}

// readSlice reads the stretch of slice b of file f that starts off bytes into
// the slice, with zeros past the end of the file's data.
func readSlice(r *os.File, f File, sliceSize int64, b int, off int64, buf []byte) error {
	start := int64(b)*sliceSize + off
	valid := max(0, min(int64(len(buf)), f.Size-start))
	n, err := r.ReadAt(buf[:valid], start)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	clear(buf[n:])
	return nil
}

// split cuts n rows into at most parts runs of about the same length.
func split(n, parts int) [][2]int {
	parts = max(1, min(parts, n))
	out := make([][2]int, 0, parts)
	for p := range parts {
		out = append(out, [2]int{p * n / parts, (p + 1) * n / parts})
	}
	return out
}

// solve picks k recovery slices whose equations can be solved for the lost
// slices and returns them with the inverse of their matrix. The first k are
// tried first, as nearly always works; when their matrix is singular the
// others are taken in until k independent rows are found.
func solve(s *Set, consts []uint16, lost []int) ([]Recovery, [][]uint16, bool) {
	k := len(lost)
	row := func(r Recovery) []uint16 {
		out := make([]uint16, k)
		for c, g := range lost {
			out[c] = gfPow(consts[g], r.Exponent)
		}
		return out
	}
	matrix := func(rows []Recovery) [][]uint16 {
		m := make([][]uint16, len(rows))
		for i, r := range rows {
			m[i] = row(r)
		}
		return m
	}
	rows := s.Recovery[:k]
	if inv, ok := invert(matrix(rows)); ok {
		return rows, inv, true
	}
	// Gaussian elimination over the candidates, keeping each row that is not
	// a combination of the ones kept before it.
	var basis [][]uint16
	var lead []int
	rows = nil
	for _, r := range s.Recovery {
		v := row(r)
		for i, b := range basis {
			if f := v[lead[i]]; f != 0 {
				for j := range v {
					v[j] ^= gfMul(f, b[j])
				}
			}
		}
		p := -1
		for j, x := range v {
			if x != 0 {
				p = j
				break
			}
		}
		if p < 0 {
			continue
		}
		d := v[p]
		for j := range v {
			v[j] = gfDiv(v[j], d)
		}
		basis = append(basis, v)
		lead = append(lead, p)
		rows = append(rows, r)
		if len(rows) == k {
			inv, ok := invert(matrix(rows))
			return rows, inv, ok
		}
	}
	return nil, nil, false
}
