package feed

// What to do with a feed that is not UTF-8.
//
// Go's XML decoder refuses a document whose declaration names an encoding it
// cannot get a reader for, and it refuses the whole document rather than the one
// character at fault. Without an answer here, a publisher on a Windows box who
// writes <?xml version="1.0" encoding="ISO-8859-1"?> produces a subscription
// that silently stages nothing at all, forever, with the only evidence a parse
// error in the log.
//
// The full answer is golang.org/x/net/html/charset, which knows every label the
// HTML standard does. It is deliberately not used: it pulls in the whole
// golang.org/x/text encoding tree for the sake of a handful of feeds, and the
// encodings that actually appear in feed declarations are the two below plus
// UTF-8. Anything else is refused with its own name in the error, so the log
// says which encoding was asked for instead of leaving somebody to guess.

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// cp1252C1 is the block where Windows-1252 differs from ISO-8859-1: the
// typographic quotes, the dashes, the ellipsis and the euro sign. It is applied
// to both labels rather than only to the one that names it, because an editor
// that emits those bytes routinely declares the document as ISO-8859-1 anyway,
// and reading them as ISO-8859-1 says they are C1 control characters, which is
// never what a title meant.
//
// Written as code points rather than as the characters themselves. Five of the
// thirty-two positions are unassigned in Windows-1252 and stay control
// characters, and a source file carrying an invisible control character inside a
// literal is a source file somebody breaks by accident without ever seeing what
// they broke.
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
		// Already what the decoder wants. US-ASCII is a subset of UTF-8, so
		// passing it through is not a shortcut, it is the correct conversion.
		return r, nil
	case "iso-8859-1", "iso8859-1", "iso_8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		return &latin1Reader{r: bufio.NewReader(r)}, nil
	}
	return nil, fmt.Errorf("feed: %q is not an encoding this build reads", label)
}

// latin1Reader turns single-byte input into the UTF-8 the XML decoder reads.
type latin1Reader struct {
	r *bufio.Reader
	// buf holds the UTF-8 encoding of the byte last read, because one input byte
	// can become up to three output bytes and a caller is free to hand us a
	// one-byte p.
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
			// Bytes already converted are handed over before the error is. An
			// io.Reader is allowed to report a short read and an error together, but
			// it is not allowed to throw the bytes away, and the XML decoder would
			// lose the tail of the document if it did.
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
