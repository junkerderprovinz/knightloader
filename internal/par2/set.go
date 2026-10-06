// Package par2 checks a download against its PAR 2.0 files and rebuilds
// damaged or missing slices from the recovery slices in them, by the
// Reed-Solomon code over GF(2^16) the specification defines. It streams: no
// more than a slice of each file is held at a time while checking, and a
// repair keeps to a memory budget however many slices it rebuilds.
package par2

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Set is a recovery set as its par2 files describe it.
type Set struct {
	ID        [16]byte
	SliceSize int64
	// Files are the files the recovery slices protect, in the order their
	// slices are numbered.
	Files []File
	// Recovery holds one intact recovery slice per exponent, in the order of
	// the exponents.
	Recovery []Recovery
	// Creator names the program that made the set, as it says.
	Creator string
}

// File is one protected file.
type File struct {
	ID [16]byte
	// Name is the file's name as the set gives it, which can carry folders
	// separated by slashes.
	Name    string
	Size    int64
	Hash    [16]byte
	Hash16k [16]byte
	// Checks holds the MD5 and CRC32 of each slice, the last one padded with
	// zeros to a full slice.
	Checks []Check
	// first is the number of the file's first slice in the set.
	first int
}

// Check is what a slice must hash to.
type Check struct {
	MD5 [16]byte
	CRC uint32
}

// Recovery is one recovery slice, which stays in its file.
type Recovery struct {
	Exponent uint32
	Path     string
	// Offset is where the slice's data starts in Path.
	Offset int64
}

// BaseName is the name the file goes by in a folder of its own, without the
// folders the set may put it in.
func (f File) BaseName() string {
	return path.Base(strings.ReplaceAll(f.Name, `\`, "/"))
}

// Slices is how many slices the set has.
func (s *Set) Slices() int {
	n := 0
	for _, f := range s.Files {
		n += len(f.Checks)
	}
	return n
}

// ErrNoSet is a group of files with no intact main packet among them, which
// says what a set protects.
var ErrNoSet = errors.New("par2: no intact main packet")

// Load reads a recovery set from its par2 files: the index file, the volumes,
// or both. Damaged packets are passed over, so the set is whole as long as
// every packet it needs is intact in one of the files, which is why each
// volume repeats the description of the files.
func Load(paths ...string) (*Set, error) {
	type found struct {
		main     []byte
		desc     map[[16]byte][]byte
		ifsc     map[[16]byte][]byte
		recovery []packet
		paths    []string
		creator  string
	}
	sets := map[[16]byte]*found{}
	var order [][16]byte
	for _, p := range paths {
		packets, err := readPackets(p)
		if err != nil {
			return nil, fmt.Errorf("par2: reading %s: %w", p, err)
		}
		for _, pk := range packets {
			g := sets[pk.set]
			if g == nil {
				g = &found{desc: map[[16]byte][]byte{}, ifsc: map[[16]byte][]byte{}}
				sets[pk.set] = g
				order = append(order, pk.set)
			}
			switch pk.typ {
			case typeMain:
				g.main = pk.body
			case typeFileDesc:
				if len(pk.body) >= 56 {
					g.desc[[16]byte(pk.body[:16])] = pk.body
				}
			case typeIFSC:
				if len(pk.body) >= 16 {
					g.ifsc[[16]byte(pk.body[:16])] = pk.body
				}
			case typeRecovery:
				g.recovery = append(g.recovery, pk)
				g.paths = append(g.paths, p)
			case typeCreator:
				g.creator = strings.TrimRight(string(pk.body), "\x00")
			}
		}
	}
	var g *found
	var id [16]byte
	for _, k := range order {
		if c := sets[k]; c.main != nil && md5.Sum(c.main) == k {
			g, id = c, k
			break
		}
	}
	if g == nil {
		return nil, ErrNoSet
	}

	s := &Set{ID: id, Creator: g.creator}
	main := g.main
	if len(main) < 12 || (len(main)-12)%16 != 0 {
		return nil, fmt.Errorf("par2: the main packet is malformed")
	}
	s.SliceSize = int64(binary.LittleEndian.Uint64(main[:8]))
	count := int(binary.LittleEndian.Uint32(main[8:12]))
	if s.SliceSize <= 0 || s.SliceSize%4 != 0 || s.SliceSize > 1<<31 || count > (len(main)-12)/16 {
		return nil, fmt.Errorf("par2: the main packet is malformed")
	}
	ids := make([][16]byte, count)
	for i := range ids {
		ids[i] = [16]byte(main[12+16*i:])
	}
	// The slices are numbered in the order the main packet lists the files.
	// It sorts the ids, but as par2cmdline compares them, from the last
	// byte, so sorting them again here would number the slices wrong.

	var lacking int
	next := 0
	for _, fid := range ids {
		desc, checks := g.desc[fid], g.ifsc[fid]
		if desc == nil || checks == nil {
			lacking++
			continue
		}
		f := File{ID: fid, first: next}
		copy(f.Hash[:], desc[16:32])
		copy(f.Hash16k[:], desc[32:48])
		f.Size = int64(binary.LittleEndian.Uint64(desc[48:56]))
		f.Name = strings.TrimRight(string(desc[56:]), "\x00")
		n := (f.Size + s.SliceSize - 1) / s.SliceSize
		if f.Size < 0 || int64(len(checks)-16) != n*20 {
			return nil, fmt.Errorf("par2: the checksums of %s do not fit its size", f.Name)
		}
		for i := range int(n) {
			e := checks[16+20*i:]
			f.Checks = append(f.Checks, Check{MD5: [16]byte(e[:16]), CRC: binary.LittleEndian.Uint32(e[16:20])})
		}
		next += len(f.Checks)
		s.Files = append(s.Files, f)
	}
	if lacking > 0 {
		return nil, fmt.Errorf("par2: the description of %d of the %d files is missing", lacking, count)
	}
	if next > maxSlices {
		return nil, fmt.Errorf("par2: %d slices are more than a set can have", next)
	}

	seen := map[uint32]bool{}
	for i, pk := range g.recovery {
		if pk.length != headerSize+4+s.SliceSize || pk.exponent >= fieldMax || seen[pk.exponent] {
			continue
		}
		seen[pk.exponent] = true
		s.Recovery = append(s.Recovery, Recovery{Exponent: pk.exponent, Path: g.paths[i], Offset: pk.dataAt})
	}
	slices.SortFunc(s.Recovery, func(a, b Recovery) int { return int(a.Exponent) - int(b.Exponent) })
	return s, nil
}

var volumeName = regexp.MustCompile(`(?i)\.vol\d+[+-](\d+)\.par2$`)

// VolumeBlocks is how many recovery slices a volume holds by its name,
// "name.vol07+08.par2" holding 8, and 0 for a name that is not a volume's.
func VolumeBlocks(name string) int {
	m := volumeName.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}
