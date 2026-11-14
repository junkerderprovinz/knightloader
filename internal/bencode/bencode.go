// Package bencode reads and writes the encoding .torrent files use (BEP 3).
//
// Decoding is strict: keys in order and no duplicates, no leading zeros, no
// data after the end. A torrent is hashed over its info dictionary as it
// stands in the file, and a lenient reader would accept a file it cannot
// write back the same way.
package bencode

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// maxDepth bounds nesting, which a .torrent from a stranger could otherwise
// push deep enough to exhaust the stack.
const maxDepth = 64

// Decode reads one value: an int64, a []byte, a []any or a map[string]any.
func Decode(b []byte) (any, error) {
	d := decoder{b: b}
	v, err := d.value(0)
	if err != nil {
		return nil, err
	}
	if d.pos != len(b) {
		return nil, fmt.Errorf("bencode: %d bytes after the end", len(b)-d.pos)
	}
	return v, nil
}

// Raw is the value of key in the dictionary b holds, as it stands in b.
func Raw(b []byte, key string) ([]byte, error) {
	d := decoder{b: b}
	if d.peek() != 'd' {
		return nil, errors.New("bencode: not a dictionary")
	}
	d.pos++
	var last []byte
	for d.peek() != 'e' {
		k, err := d.str()
		if err != nil {
			return nil, err
		}
		if last != nil && bytes.Compare(k, last) <= 0 {
			return nil, errors.New("bencode: dictionary keys out of order")
		}
		last = k
		start := d.pos
		if _, err := d.value(1); err != nil {
			return nil, err
		}
		if string(k) == key {
			return b[start:d.pos], nil
		}
	}
	return nil, fmt.Errorf("bencode: no %q in the dictionary", key)
}

// InfoHash is the BitTorrent v1 info hash of a raw info dictionary.
func InfoHash(rawInfo []byte) string {
	sum := sha1.Sum(rawInfo)
	return hex.EncodeToString(sum[:])
}

type decoder struct {
	b   []byte
	pos int
}

var errShort = errors.New("bencode: the data ends too soon")

func (d *decoder) peek() byte {
	if d.pos >= len(d.b) {
		return 0
	}
	return d.b[d.pos]
}

func (d *decoder) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, errors.New("bencode: nested too deep")
	}
	switch c := d.peek(); {
	case c == 'i':
		return d.int()
	case c == 'l':
		d.pos++
		l := []any{}
		for d.peek() != 'e' {
			if d.pos >= len(d.b) {
				return nil, errShort
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			l = append(l, v)
		}
		d.pos++
		return l, nil
	case c == 'd':
		d.pos++
		m := map[string]any{}
		var last []byte
		for d.peek() != 'e' {
			if d.pos >= len(d.b) {
				return nil, errShort
			}
			k, err := d.str()
			if err != nil {
				return nil, err
			}
			if last != nil && bytes.Compare(k, last) <= 0 {
				return nil, errors.New("bencode: dictionary keys out of order or repeated")
			}
			last = k
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			m[string(k)] = v
		}
		d.pos++
		return m, nil
	case c >= '0' && c <= '9':
		return d.str()
	case c == 0:
		return nil, errShort
	default:
		return nil, fmt.Errorf("bencode: unexpected %q at %d", c, d.pos)
	}
}

func (d *decoder) int() (int64, error) {
	end := bytes.IndexByte(d.b[d.pos:], 'e')
	if end < 0 {
		return 0, errShort
	}
	digits := string(d.b[d.pos+1 : d.pos+end])
	abs := strings.TrimPrefix(digits, "-")
	if !plainDigits(abs) || abs == "" || (abs != "0" && abs[0] == '0') || digits == "-0" {
		return 0, fmt.Errorf("bencode: %q is not a canonical integer", digits)
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bencode: %q is not an integer", digits)
	}
	d.pos += end + 1
	return n, nil
}

func (d *decoder) str() ([]byte, error) {
	colon := bytes.IndexByte(d.b[d.pos:], ':')
	if colon < 0 {
		return nil, errShort
	}
	digits := string(d.b[d.pos : d.pos+colon])
	n, err := strconv.Atoi(digits)
	if err != nil || !plainDigits(digits) || (len(digits) > 1 && digits[0] == '0') {
		return nil, fmt.Errorf("bencode: %q is not a string length", digits)
	}
	start := d.pos + colon + 1
	if n > len(d.b)-start {
		return nil, errShort
	}
	d.pos = start + n
	return d.b[start:d.pos], nil
}

// plainDigits reports s made of ASCII digits only; strconv takes a sign too.
func plainDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Encode writes v, which may hold int, int64, string, []byte, []any and
// map[string]any, with a dictionary's keys sorted as BEP 3 asks.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encode(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case int:
		fmt.Fprintf(buf, "i%de", x)
	case int64:
		fmt.Fprintf(buf, "i%de", x)
	case string:
		fmt.Fprintf(buf, "%d:%s", len(x), x)
	case []byte:
		fmt.Fprintf(buf, "%d:", len(x))
		buf.Write(x)
	case []any:
		buf.WriteByte('l')
		for _, e := range x {
			if err := encode(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	case map[string]any:
		buf.WriteByte('d')
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			fmt.Fprintf(buf, "%d:%s", len(k), k)
			if err := encode(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	default:
		return fmt.Errorf("bencode: cannot encode a %T", v)
	}
	return nil
}
