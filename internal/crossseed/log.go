package crossseed

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/fsretry"
)

// logMax is how large the log grows before it starts a fresh file. The one
// before is kept beside it, so the log never takes more than twice this.
const logMax = 1 << 20

// Entry is one line of the log: a twin injected, or refused or failed with
// the reason.
type Entry struct {
	Time     time.Time `json:"time"`
	Tracker  string    `json:"tracker"`
	Name     string    `json:"name"`
	InfoHash string    `json:"infoHash"`
	Result   string    `json:"result"`
	Reason   string    `json:"reason,omitempty"`
}

// Log is cross-seed.jsonl, one JSON object per line.
type Log struct {
	path string
	max  int64
	mu   sync.Mutex
}

// OpenLog is the log at path.
func OpenLog(path string) *Log { return &Log{path: path, max: logMax} }

// Add appends e. A log that cannot be written costs only the line.
func (l *Log) Add(e Entry) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if fi, err := os.Stat(l.path); err == nil && fi.Size()+int64(len(b)) > l.max {
		if err := fsretry.Rename(l.path, l.path+".1"); err != nil {
			log.Printf("cross-seed: the log could not start a fresh file: %v", err)
		}
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("cross-seed: the log could not be written: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		log.Printf("cross-seed: the log could not be written: %v", err)
	}
}

// Last is the newest n entries, newest first. A line that cannot be read is
// passed over.
func (l *Log) Last(n int) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Entry
	for _, p := range []string{l.path, l.path + ".1"} {
		b, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var file []Entry
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(nil, int(logMax))
		for sc.Scan() {
			var e Entry
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				file = append(file, e)
			}
		}
		slices.Reverse(file)
		out = append(out, file...)
		if len(out) >= n {
			return out[:n], nil
		}
	}
	return out, nil
}
