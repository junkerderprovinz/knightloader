package par2

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"io"
	"os"
)

var magic = []byte("PAR2\x00PKT")

const headerSize = 64

// The packet types this package reads. Any other packet is skipped.
var (
	typeMain     = typeID("PAR 2.0\x00Main")
	typeFileDesc = typeID("PAR 2.0\x00FileDesc")
	typeIFSC     = typeID("PAR 2.0\x00IFSC")
	typeRecovery = typeID("PAR 2.0\x00RecvSlic")
	typeCreator  = typeID("PAR 2.0\x00Creator")
)

func typeID(s string) [16]byte {
	var t [16]byte
	copy(t[:], s)
	return t
}

// maxPacket caps a packet whose body is read into memory. The largest of them,
// a file's slice checksums, takes 20 bytes a slice, so this holds the 32768
// slices a set may have many times over. A recovery packet is not read into
// memory and has no cap.
const maxPacket = 4 << 20

// packet is one packet whose MD5 matched. Body is nil for a recovery slice,
// whose data stays in its file from dataAt on.
type packet struct {
	set    [16]byte
	typ    [16]byte
	body   []byte
	length int64
	// exponent and dataAt are set for a recovery slice.
	exponent uint32
	dataAt   int64
}

// IsPar2 reports whether head, the first bytes of a file, starts a par2
// packet, which is how a par2 file posted under a random name is found.
func IsPar2(head []byte) bool {
	return bytes.HasPrefix(head, magic)
}

// readPackets reads every intact packet in the file at path. A damaged packet
// is skipped and the search goes on from the byte after its magic, so a file
// with missing articles still yields the packets around the holes.
func readPackets(path string) ([]packet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := &scanner{f: f, r: bufio.NewReaderSize(f, 1<<20)}
	var out []packet
	for {
		at, ok, err := s.seekMagic()
		if err != nil {
			return out, err
		}
		if !ok {
			return out, nil
		}
		p, ok, err := s.read(at)
		if err != nil {
			return out, err
		}
		if !ok {
			if err := s.seek(at + 1); err != nil {
				return out, err
			}
			continue
		}
		out = append(out, p)
	}
}

type scanner struct {
	f   *os.File
	r   *bufio.Reader
	off int64
}

func (s *scanner) seek(off int64) error {
	if _, err := s.f.Seek(off, io.SeekStart); err != nil {
		return err
	}
	s.r.Reset(s.f)
	s.off = off
	return nil
}

func (s *scanner) discard(n int) error {
	k, err := s.r.Discard(n)
	s.off += int64(k)
	return err
}

// seekMagic moves to the next packet magic and reports its offset, or false
// at the end of the file.
func (s *scanner) seekMagic() (int64, bool, error) {
	for {
		buf, err := s.r.Peek(s.r.Size())
		if i := bytes.Index(buf, magic); i >= 0 {
			if err := s.discard(i); err != nil {
				return 0, false, err
			}
			return s.off, true, nil
		}
		if errors.Is(err, io.EOF) {
			return 0, false, nil
		}
		if err != nil {
			return 0, false, err
		}
		// The tail may hold the start of a magic cut off by the buffer's end.
		if err := s.discard(len(buf) - (len(magic) - 1)); err != nil {
			return 0, false, err
		}
	}
}

// read reads the packet at at, where the scanner stands, and reports false
// for one that is cut short or whose MD5 does not match.
func (s *scanner) read(at int64) (packet, bool, error) {
	var h [headerSize]byte
	if _, err := io.ReadFull(s.r, h[:]); err != nil {
		return packet{}, false, ignoreEOF(err)
	}
	s.off += headerSize
	p := packet{length: int64(binary.LittleEndian.Uint64(h[8:16]))}
	copy(p.set[:], h[32:48])
	copy(p.typ[:], h[48:64])
	if p.length < headerSize || p.length%4 != 0 {
		return packet{}, false, nil
	}
	sum := md5.New()
	sum.Write(h[32:64])
	n := p.length - headerSize
	if p.typ == typeRecovery {
		if n < 4 {
			return packet{}, false, nil
		}
		var e [4]byte
		if _, err := io.ReadFull(s.r, e[:]); err != nil {
			return packet{}, false, ignoreEOF(err)
		}
		s.off += 4
		sum.Write(e[:])
		p.exponent = binary.LittleEndian.Uint32(e[:])
		p.dataAt = s.off
		k, err := io.CopyN(sum, s.r, n-4)
		s.off += k
		if err != nil {
			return packet{}, false, ignoreEOF(err)
		}
	} else {
		if n > maxPacket {
			return packet{}, false, nil
		}
		p.body = make([]byte, n)
		k, err := io.ReadFull(s.r, p.body)
		s.off += int64(k)
		if err != nil {
			return packet{}, false, ignoreEOF(err)
		}
		sum.Write(p.body)
	}
	if !bytes.Equal(sum.Sum(nil), h[16:32]) {
		return packet{}, false, nil
	}
	return p, true, nil
}

func ignoreEOF(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return err
}
