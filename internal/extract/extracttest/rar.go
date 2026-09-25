// Package extracttest builds archives for tests in a format nothing in the
// standard library writes. It exists only for tests; nothing in the built
// binaries imports it.
package extracttest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// File is one entry of an archive.
type File struct {
	Name string
	Body []byte
}

// rarSignature opens every RAR 5 volume.
const rarSignature = "Rar!\x1a\x07\x01\x00"

// piece is the part of one entry that lands in one volume.
type piece struct {
	name        string
	data        []byte
	whole       []byte
	first, last bool
}

// RarSet lays files out the way rar -v cuts a set: stored, in order, with at
// most per bytes of file data in each volume, a file that does not fit
// carrying on in the next one. It returns each volume's bytes, and a single
// volume that is not marked as part of a set when everything fits in one.
//
// The format is the one in RARLAB's technical note for RAR 5.0, store method
// only, which is enough to exercise everything a reader does with volumes.
// 7-Zip reads and tests what it writes.
func RarSet(per int, files ...File) [][]byte {
	var vols [][]piece
	var cur []piece
	room := per
	for _, f := range files {
		data := f.Body
		first := true
		for {
			if room == 0 {
				vols = append(vols, cur)
				cur, room = nil, per
			}
			n := min(len(data), room)
			last := n == len(data)
			cur = append(cur, piece{name: f.Name, data: data[:n], whole: f.Body, first: first, last: last})
			data, room, first = data[n:], room-n, false
			if last {
				break
			}
		}
	}
	vols = append(vols, cur)

	out := make([][]byte, len(vols))
	for i, pieces := range vols {
		var b bytes.Buffer
		b.WriteString(rarSignature)
		b.Write(mainHeader(i, len(vols) > 1))
		for _, p := range pieces {
			b.Write(fileHeader(p))
			b.Write(p.data)
		}
		b.Write(endHeader(i < len(vols)-1))
		out[i] = b.Bytes()
	}
	return out
}

// WriteRarSet writes volumes as stem.part1.rar, stem.part2.rar and so on into
// dir, and returns the paths in order.
func WriteRarSet(t testing.TB, dir, stem string, vols [][]byte) []string {
	t.Helper()
	var paths []string
	for i, v := range vols {
		p := filepath.Join(dir, fmt.Sprintf("%s.part%d.rar", stem, i+1))
		if err := os.WriteFile(p, v, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

func vint(b *bytes.Buffer, v uint64) {
	for v >= 0x80 {
		b.WriteByte(byte(v) | 0x80)
		v >>= 7
	}
	b.WriteByte(byte(v))
}

// block frames a header: its CRC32, then its size, then the fields. The CRC
// covers the size and the fields.
func block(fields []byte) []byte {
	var sized bytes.Buffer
	vint(&sized, uint64(len(fields)))
	sized.Write(fields)
	out := binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(sized.Bytes()))
	return append(out, sized.Bytes()...)
}

func mainHeader(volume int, multi bool) []byte {
	var f bytes.Buffer
	vint(&f, 1) // main archive header
	vint(&f, 0) // no extra area, no data
	var flags uint64
	if multi {
		flags |= 0x0001 // a volume
	}
	if volume > 0 {
		flags |= 0x0002 // with its number, which the first one leaves out
	}
	vint(&f, flags)
	if volume > 0 {
		vint(&f, uint64(volume))
	}
	return block(f.Bytes())
}

func fileHeader(p piece) []byte {
	var f bytes.Buffer
	vint(&f, 2) // file header
	flags := uint64(0x0002)
	if !p.first {
		flags |= 0x0008 // data carried over from the previous volume
	}
	if !p.last {
		flags |= 0x0010 // data carrying on in the next volume
	}
	vint(&f, flags)
	vint(&f, uint64(len(p.data)))
	vint(&f, 0x0004) // a CRC32 follows
	vint(&f, uint64(len(p.whole)))
	vint(&f, 0o644)
	// Every piece but the last carries the CRC of its own data, the last one
	// the CRC of the whole file.
	sum := crc32.ChecksumIEEE(p.data)
	if p.last {
		sum = crc32.ChecksumIEEE(p.whole)
	}
	f.Write(binary.LittleEndian.AppendUint32(nil, sum))
	vint(&f, 0) // stored
	vint(&f, 1) // written on Unix
	vint(&f, uint64(len(p.name)))
	f.WriteString(p.name)
	return block(f.Bytes())
}

func endHeader(more bool) []byte {
	var f bytes.Buffer
	vint(&f, 5) // end of archive
	vint(&f, 0)
	var flags uint64
	if more {
		flags = 0x0001 // not the last volume
	}
	vint(&f, flags)
	return block(f.Bytes())
}
