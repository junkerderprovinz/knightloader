package crossseed

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/fsretry"
)

// The results of asking a tracker for a torrent, in the history and the log.
const (
	ResultInjected = "injected"
	ResultRefused  = "refused"
	ResultNone     = "none"
	ResultError    = "error"
	// ResultHanded is a torrent handed to the cross-seed daemon.
	ResultHanded = "handed"
)

// Answer is what one tracker said about one torrent. InfoHash is the twin's,
// for a twin that was injected.
type Answer struct {
	Result   string    `json:"result"`
	InfoHash string    `json:"infoHash,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	At       time.Time `json:"at"`
}

// History remembers which tracker was asked for which torrent, so none is
// asked twice, across restarts.
type History struct {
	path string
	// saveMu keeps two saves from writing the same file at once, the older
	// answers last.
	saveMu sync.Mutex

	mu      sync.Mutex
	answers map[string]Answer
	dirty   bool
}

// OpenHistory reads the history at path. A missing or unreadable file starts
// an empty one, which costs at most a second round of questions.
func OpenHistory(path string) *History {
	h := &History{path: path, answers: map[string]Answer{}}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		log.Printf("cross-seed: %s could not be read and starts empty: %v", path, err)
	default:
		if err := json.Unmarshal(b, &h.answers); err != nil {
			log.Printf("cross-seed: %s could not be read and starts empty: %v", path, err)
			h.answers = map[string]Answer{}
		}
	}
	return h
}

func historyKey(tracker, hash string) string { return tracker + " " + strings.ToLower(hash) }

// Asked reports whether tracker has answered for the torrent with hash.
func (h *History) Asked(tracker, hash string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.answers[historyKey(tracker, hash)]
	return ok
}

// Note records tracker's answer for the torrent with hash.
func (h *History) Note(tracker, hash string, a Answer) {
	if a.At.IsZero() {
		a.At = time.Now().UTC()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.answers[historyKey(tracker, hash)] = a
	h.dirty = true
}

// ForgetErrors drops the errors tracker gave, so a changed key or address
// asks again where only the old one failed.
func (h *History) ForgetErrors(tracker string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, a := range h.answers {
		if a.Result == ResultError && strings.HasPrefix(k, tracker+" ") {
			delete(h.answers, k)
			h.dirty = true
		}
	}
}

// Forget drops every answer about the torrent with hash, so each tracker is
// asked again should it come back.
func (h *History) Forget(hash string) {
	suffix := " " + strings.ToLower(hash)
	h.mu.Lock()
	defer h.mu.Unlock()
	for k := range h.answers {
		if strings.HasSuffix(k, suffix) {
			delete(h.answers, k)
			h.dirty = true
		}
	}
}

// Injected is every twin injected for the torrent with hash.
func (h *History) Injected(hash string) []string {
	suffix := " " + strings.ToLower(hash)
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for k, a := range h.answers {
		if a.Result == ResultInjected && a.InfoHash != "" && strings.HasSuffix(k, suffix) {
			out = append(out, a.InfoHash)
		}
	}
	return out
}

// Save writes the history when it changed.
func (h *History) Save() error {
	h.saveMu.Lock()
	defer h.saveMu.Unlock()
	h.mu.Lock()
	if !h.dirty {
		h.mu.Unlock()
		return nil
	}
	b, err := json.Marshal(h.answers)
	h.dirty = false
	h.mu.Unlock()
	if err == nil {
		err = writeAtomic(h.path, b)
	}
	if err != nil {
		h.mu.Lock()
		h.dirty = true
		h.mu.Unlock()
	}
	return err
}

func writeAtomic(path string, b []byte) error { return writeFileAtomic(path, b, 0o600) }

func writeFileAtomic(path string, b []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, perm); err != nil {
		return err
	}
	if err := fsretry.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
