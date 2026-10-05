package local

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// mapSuffix names the segment map beside a part file.
const mapSuffix = ".segments"

// segMap records which articles of a file are written, so a download that
// stopped asks only for the rest. The articles land anywhere in the part
// file, so its length says nothing about how far it got.
type segMap struct {
	// size is the file's size from its yEnc header, 0 until an article said.
	size int64
	done []bool
}

const mapMagic = "klsegments 1"

// loadSegMap reads the map at path for a file of n articles. A map that is
// missing, damaged or made for another article count starts empty.
func loadSegMap(path string, n int) *segMap {
	m := &segMap{done: make([]bool, n)}
	raw, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	head, body, ok := bytes.Cut(raw, []byte("\n"))
	fields := strings.Fields(string(head))
	if !ok || len(fields) != 4 || fields[0]+" "+fields[1] != mapMagic || len(body) != n {
		return m
	}
	size, err1 := strconv.ParseInt(fields[2], 10, 64)
	count, err2 := strconv.Atoi(fields[3])
	if err1 != nil || err2 != nil || count != n || size < 0 {
		return m
	}
	m.size = size
	for i, c := range body {
		m.done[i] = c == '1'
	}
	return m
}

func (m *segMap) save(path string) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %d %d\n", mapMagic, m.size, len(m.done))
	for _, d := range m.done {
		if d {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *segMap) count() int {
	n := 0
	for _, d := range m.done {
		if d {
			n++
		}
	}
	return n
}

func removeMap(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + ".tmp")
}
