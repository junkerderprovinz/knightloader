package feed

// Go's XML decoder refuses a whole document whose declared encoding it has no
// reader for, so a feed declaring ISO-8859-1 would stage nothing. The full
// answer, golang.org/x/net/html/charset, pulls in the whole x/text encoding
// tree; feeds in practice use UTF-8 or the Latin-1 family below, and anything
// else is refused with its name in the error.

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// cp1252C1 is where Windows-1252 differs from ISO-8859-1: typographic quotes,
// dashes, the ellipsis and the euro sign. It applies to both labels, since
// editors that emit these bytes often declare ISO-8859-1, and as C1 control
// characters they are never what a title meant. The entries are code points
// because five of them stay invisible control characters.
var cp1252C1 = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// charsetReader is the decoder's hook for a non-UTF-8 document.
func charsetReader(label string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		// US-ASCII is a subset of UTF-8.
		return r, nil
	case "iso-8859-1", "iso8859-1", "iso_8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		return &latin1Reader{r: bufio.NewReader(r)}, nil
	}
	return nil, fmt.Errorf("feed: %q is not an encoding this build reads", label)
}

// latin1Reader turns single-byte input into the UTF-8 the XML decoder reads.
type latin1Reader struct {
	r *bufio.Reader
	// buf holds the UTF-8 encoding of the last byte read, since one input
	// byte can become up to three output bytes and p may be one byte long.
	buf  [utf8.UTFMax]byte
	n, i int
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if l.i < l.n {
			p[n] = l.buf[l.i]
			l.i++
			n++
			continue
		}
		b, err := l.r.ReadByte()
		if err != nil {
			// Hand over what was already converted before the error.
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
		c := rune(b)
		if b >= 0x80 && b <= 0x9F {
			c = cp1252C1[b-0x80]
		}
		l.n, l.i = utf8.EncodeRune(l.buf[:], c), 0
	}
	return n, nil
}
