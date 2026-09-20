package script

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// scriptsFile is its own JSON file rather than a field of settings.Settings,
// which is replaced whole on every PUT: a settings page loaded before a
// script was saved would otherwise post its stale copy back and delete it.
const scriptsFile = "scripts.json"

// store is the on-disk half of a Host, shaped like
// internal/federation.Manager: an id-keyed map under one mutex, with the
// whole list written on every change.
type store struct {
	path string

	mu   sync.Mutex
	byID map[string]Script
}

// openStore reads scriptsFile from dir. A missing file is an empty store.
func openStore(dataDir string) (*store, error) {
	st := &store{path: filepath.Join(dataDir, scriptsFile), byID: map[string]Script{}}
	b, err := os.ReadFile(st.path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, fmt.Errorf("script: reading %s: %w", scriptsFile, err)
	}
	var arr []Script
	if err := json.Unmarshal(b, &arr); err != nil {
		return nil, fmt.Errorf("script: %s is not valid JSON: %w", scriptsFile, err)
	}
	for _, s := range arr {
		st.byID[s.ID] = s
	}
	return st, nil
}

// list returns every stored script, sorted by name.
func (st *store) list() []Script {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]Script, 0, len(st.byID))
	for _, s := range st.byID {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// get returns one script by id.
func (st *store) get(id string) (Script, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.byID[id]
	return s, ok
}

// save validates and persists s, assigning a fresh ID to a new script and
// keeping CreatedAt on an edit. A script that does not compile is refused
// here, where the editor shows the error, rather than skipped with a log line
// every time it would fire.
func (st *store) save(s Script) (Script, error) {
	if err := validate(&s); err != nil {
		return Script{}, err
	}
	now := time.Now()
	st.mu.Lock()
	defer st.mu.Unlock()
	switch existing, ok := st.byID[s.ID]; {
	case s.ID == "":
		s.ID = newID()
		s.CreatedAt = now
	case ok:
		s.CreatedAt = existing.CreatedAt
	default:
		// An unknown non-empty ID becomes a new row under that ID, as in
		// federation.Manager.Add.
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	st.byID[s.ID] = s
	if err := st.flushLocked(); err != nil {
		return Script{}, err
	}
	return s, nil
}

// delete removes a script by id. An unknown id is an error, so a stale ID
// from a second tab or a double click is reported.
func (st *store) delete(id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.byID[id]; !ok {
		return fmt.Errorf("script: %q not found", id)
	}
	delete(st.byID, id)
	return st.flushLocked()
}

func (st *store) flushLocked() error {
	arr := make([]Script, 0, len(st.byID))
	for _, s := range st.byID {
		arr = append(arr, s)
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].Name < arr[j].Name })
	b, err := json.MarshalIndent(arr, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(st.path, b, 0o600)
}

// validate normalises and checks one script before it is persisted, including
// compiling it, so every saved script parses.
func validate(s *Script) error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return errors.New("script: name is required")
	}
	if len(s.Name) > MaxNameBytes {
		return fmt.Errorf("script: name longer than %d bytes", MaxNameBytes)
	}
	if !s.Trigger.Valid() {
		return fmt.Errorf("script: %q is not a trigger this build knows", s.Trigger)
	}
	if s.Code == "" {
		return errors.New("script: code is required")
	}
	if len(s.Code) > MaxCodeBytes {
		return fmt.Errorf("script: source longer than %d bytes", MaxCodeBytes)
	}
	// Strict mode, so an undeclared assignment raises a ReferenceError
	// instead of creating a global that could collide with one this package
	// adds.
	if _, err := goja.Compile(s.ID, s.Code, true); err != nil {
		return fmt.Errorf("script: does not compile: %w", err)
	}
	if s.TimeoutMS != 0 {
		d := time.Duration(s.TimeoutMS) * time.Millisecond
		if d < MinTimeout || d > MaxTimeout {
			return fmt.Errorf("script: timeoutMs must be between %d and %d",
				MinTimeout.Milliseconds(), MaxTimeout.Milliseconds())
		}
	}
	return nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
