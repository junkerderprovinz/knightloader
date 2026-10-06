package par2

import (
	"crypto/md5"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

// Match finds each file of the set among paths. A file under the set's name
// for it is taken as it is, damaged or not. Every other file is recognised by
// its content, the way a file posted under a random or a wrong name has to
// be: the size and the hash of its first 16 KiB, or failing that, for a file
// whose start is damaged, the checksum of one of its slices. The result holds
// one path per file of the set, "" for a file found nowhere.
func (s *Set) Match(paths []string) []string {
	out := make([]string, len(s.Files))
	taken := make([]bool, len(paths))
	for i, f := range s.Files {
		for j, p := range paths {
			if !taken[j] && filepath.Base(p) == f.BaseName() {
				out[i], taken[j] = p, true
				break
			}
		}
	}
	sizes := make([]int64, len(paths))
	for j, p := range paths {
		sizes[j] = -1
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			sizes[j] = fi.Size()
		}
	}
	for _, byHead := range []bool{true, false} {
		for i, f := range s.Files {
			if out[i] != "" {
				continue
			}
			for j, p := range paths {
				if taken[j] || sizes[j] != f.Size {
					continue
				}
				if byHead && head16k(p) == f.Hash16k || !byHead && anySliceMatches(p, f, s.SliceSize) {
					out[i], taken[j] = p, true
					break
				}
			}
		}
	}
	return out
}

// head16k is the MD5 of a file's first 16 KiB, or of all of it when it is
// shorter, as a par2 file description records it.
func head16k(path string) [16]byte {
	f, err := os.Open(path)
	if err != nil {
		return [16]byte{}
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.CopyN(h, f, 16<<10); err != nil && err != io.EOF {
		return [16]byte{}
	}
	return [16]byte(h.Sum(nil))
}

// anySliceMatches reports whether one slice of the file at path has the
// checksums f gives the slice at its place.
func anySliceMatches(path string, f File, sliceSize int64) bool {
	r, err := os.Open(path)
	if err != nil {
		return false
	}
	defer r.Close()
	buf := make([]byte, sliceSize)
	for i, c := range f.Checks {
		n, err := r.ReadAt(buf, int64(i)*sliceSize)
		if n == 0 {
			return false
		}
		clear(buf[n:])
		if crc32.ChecksumIEEE(buf) == c.CRC && md5.Sum(buf) == c.MD5 {
			return true
		}
		if err != nil {
			return false
		}
	}
	return false
}
