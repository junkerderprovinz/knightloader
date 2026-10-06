// Package yenc decodes and encodes yEnc, the encoding nearly every binary on
// Usenet is posted in. One article carries one part of a file: a =ybegin line,
// a =ypart line naming the byte range, the encoded bytes and a =yend line with
// the part's CRC32.
//
// The format is described at http://www.yenc.org/yenc-draft.1.3.txt.
package yenc

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
)

// ErrCRC is a part whose bytes do not match the checksum posted with it. The
// article was damaged on its way through Usenet, and another server's copy
// may be whole.
var ErrCRC = errors.New("yenc: checksum mismatch")

// ErrFormat is an article that is not yEnc or is cut short.
var ErrFormat = errors.New("yenc: not a complete yEnc part")

// Part is one decoded article.
type Part struct {
	// Name is the file name the poster gave.
	Name string
	// FileSize is the size of the whole file, which every part repeats.
	FileSize int64
	// Number and Total count the parts from 1, and are 1 for a file posted in
	// one article.
	Number int
	Total  int
	// Begin is the offset of Data in the file, counted from 0.
	Begin int64
	Data  []byte
}

// End is the offset just past the part's bytes.
func (p Part) End() int64 { return p.Begin + int64(len(p.Data)) }

// Decode reads one article body. Text before the =ybegin line is skipped, as
// some posters put a line of their own there. The part's size and CRC32 are
// checked against its =yend line; a mismatch of the CRC is ErrCRC.
func Decode(body []byte) (Part, error) {
	var p Part
	begin, rest, ok := findLine(body, "=ybegin ")
	if !ok {
		return p, fmt.Errorf("%w: no =ybegin line", ErrFormat)
	}
	head := params(begin)
	p.Name = head["name"]
	var err error
	if p.FileSize, err = strconv.ParseInt(head["size"], 10, 64); err != nil || p.FileSize < 0 {
		return p, fmt.Errorf("%w: =ybegin has no size", ErrFormat)
	}
	p.Number, p.Total = 1, 1
	if v, ok := head["part"]; ok {
		if p.Number, err = strconv.Atoi(v); err != nil || p.Number < 1 {
			return p, fmt.Errorf("%w: bad part number %q", ErrFormat, v)
		}
		if t, err := strconv.Atoi(head["total"]); err == nil && t > 0 {
			p.Total = t
		} else {
			p.Total = p.Number
		}
	}

	wantLen := p.FileSize
	if _, multi := head["part"]; multi {
		line, after, ok := nextLine(rest)
		if !ok || !bytes.HasPrefix(line, []byte("=ypart ")) {
			return p, fmt.Errorf("%w: part %d has no =ypart line", ErrFormat, p.Number)
		}
		rest = after
		rng := params(line)
		from, err1 := strconv.ParseInt(rng["begin"], 10, 64)
		to, err2 := strconv.ParseInt(rng["end"], 10, 64)
		if err1 != nil || err2 != nil || from < 1 || to < from-1 || to > p.FileSize {
			return p, fmt.Errorf("%w: bad =ypart range %q", ErrFormat, line)
		}
		p.Begin = from - 1
		wantLen = to - from + 1
	}

	end := bytes.Index(rest, []byte("\n=yend"))
	if end < 0 {
		if !bytes.HasPrefix(rest, []byte("=yend")) {
			return p, fmt.Errorf("%w: no =yend line", ErrFormat)
		}
		end = -1
	}
	// A forged header can name any size, so the encoded lines bound the
	// buffer: they never decode to more bytes than they hold.
	src := rest[:max(end, 0)]
	p.Data = decodeData(src, make([]byte, 0, min(wantLen, int64(len(src)))))
	trailer, _, _ := nextLine(rest[end+1:])
	tail := params(trailer)

	if n, err := strconv.ParseInt(tail["size"], 10, 64); err == nil && n != int64(len(p.Data)) {
		return p, fmt.Errorf("%w: part %d decodes to %d bytes, =yend says %d", ErrCRC, p.Number, len(p.Data), n)
	}
	if int64(len(p.Data)) != wantLen {
		return p, fmt.Errorf("%w: part %d decodes to %d bytes, its header says %d", ErrCRC, p.Number, len(p.Data), wantLen)
	}
	sum := tail["pcrc32"]
	if sum == "" && p.Total == 1 {
		sum = tail["crc32"]
	}
	if sum != "" {
		want, err := strconv.ParseUint(sum, 16, 32)
		if err != nil {
			return p, fmt.Errorf("%w: bad checksum %q", ErrFormat, sum)
		}
		if got := crc32.ChecksumIEEE(p.Data); got != uint32(want) {
			return p, fmt.Errorf("%w: part %d has %08x, =yend says %08x", ErrCRC, p.Number, got, want)
		}
	}
	return p, nil
}

// decodeData appends the decoded bytes of the lines in src to dst.
func decodeData(src, dst []byte) []byte {
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case '\r', '\n':
			continue
		case '=':
			i++
			if i == len(src) {
				return dst
			}
			c = src[i] - 64
		}
		dst = append(dst, c-42)
	}
	return dst
}

// findLine returns the first line starting with prefix, without its line end,
// and what follows it.
func findLine(b []byte, prefix string) (line, rest []byte, ok bool) {
	for len(b) > 0 {
		l, after, _ := nextLine(b)
		if bytes.HasPrefix(l, []byte(prefix)) {
			return l, after, true
		}
		b = after
	}
	return nil, nil, false
}

// nextLine splits b after its first line and drops that line's CR LF.
func nextLine(b []byte) (line, rest []byte, ok bool) {
	if len(b) == 0 {
		return nil, nil, false
	}
	i := bytes.IndexByte(b, '\n')
	if i < 0 {
		return bytes.TrimRight(b, "\r"), nil, true
	}
	return bytes.TrimRight(b[:i], "\r"), b[i+1:], true
}

// params reads the key=value pairs of a header line. name takes the rest of
// the line, since a file name may hold spaces and equals signs.
func params(line []byte) map[string]string {
	out := map[string]string{}
	fields := line
	if i := bytes.Index(line, []byte(" name=")); i >= 0 {
		out["name"] = string(bytes.TrimSpace(line[i+len(" name="):]))
		fields = line[:i]
	}
	for _, f := range bytes.Fields(fields) {
		if k, v, ok := bytes.Cut(f, []byte("=")); ok && len(k) > 0 {
			out[string(k)] = string(v)
		}
	}
	return out
}

// Encode writes p as one article body, its lines lineLen encoded characters
// long (128 when 0). The bytes NNTP or the format cannot carry are escaped:
// NUL, CR, LF and '=' everywhere, a tab or space at either end of a line and a
// dot at its start.
func Encode(w io.Writer, p Part, lineLen int) error {
	if lineLen <= 0 {
		lineLen = 128
	}
	var b bytes.Buffer
	if p.Total > 1 {
		fmt.Fprintf(&b, "=ybegin part=%d total=%d line=%d size=%d name=%s\r\n", p.Number, p.Total, lineLen, p.FileSize, p.Name)
		fmt.Fprintf(&b, "=ypart begin=%d end=%d\r\n", p.Begin+1, p.End())
	} else {
		fmt.Fprintf(&b, "=ybegin line=%d size=%d name=%s\r\n", lineLen, p.FileSize, p.Name)
	}
	col := 0
	for i, raw := range p.Data {
		c := raw + 42
		last := i == len(p.Data)-1 || col >= lineLen-1
		escape := c == 0 || c == '\n' || c == '\r' || c == '='
		if (c == '\t' || c == ' ') && (col == 0 || last) {
			escape = true
		}
		if c == '.' && col == 0 {
			escape = true
		}
		if escape {
			b.WriteByte('=')
			c += 64
			col++
		}
		b.WriteByte(c)
		col++
		if col >= lineLen {
			b.WriteString("\r\n")
			col = 0
		}
	}
	if col > 0 {
		b.WriteString("\r\n")
	}
	sum := crc32.ChecksumIEEE(p.Data)
	if p.Total > 1 {
		fmt.Fprintf(&b, "=yend size=%d part=%d pcrc32=%08x\r\n", len(p.Data), p.Number, sum)
	} else {
		fmt.Fprintf(&b, "=yend size=%d crc32=%08x\r\n", len(p.Data), sum)
	}
	_, err := w.Write(b.Bytes())
	return err
}
