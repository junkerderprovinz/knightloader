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

// scriptsFile is its own JSON file beside settings.json, connections.json
// and instances.json - never a field inside settings.Settings. That struct
// is served whole on every GET and replaced whole on every PUT (see
// build-plan.md's own convention note on this exact hazard, and
// internal/rules' rule set store built for the identical reason): a browser
// that loaded the settings page before somebody saved a script would post
// its stale copy back and silently delete the one just added.
const scriptsFile = "scripts.json"

// store is the on-disk half of a Host - the exact shape
// internal/federation.Manager already uses (Load(dir), a name/id-keyed map
// guarded by one mutex, flush the whole list on every write), copied
// deliberately rather than reinvented.
type store struct {
	path string

	mu   sync.Mutex
	byID map[string]Script
}

// openStore reads scriptsFile from dir. A missing file is an empty store,
// not an error - the ordinary case for every install before the first
// script is ever saved.
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

// list returns every stored script, sorted by name for a stable, readable
// order - the same ordering federation.Manager.List and accounts use.
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

// save validates and persists s, assigning a fresh ID when s.ID is empty
// (a new script) and preserving the original CreatedAt when it is not (an
// edit). It refuses - and writes nothing - when s does not pass validate,
// which includes a real goja.Compile: a script that cannot parse is
// refused at save time rather than accepted and silently skipped every
// time it would otherwise fire (see Host.rebuildIndex, which would
// otherwise be the only place the mistake ever surfaced, and only in a
// log line nobody watching the editor would see).
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
		// A non-empty ID this store has never seen: treat it as a fresh
		// row under the caller's chosen ID rather than refusing it, the
		// same tolerance federation.Manager.Add shows for a name it has
		// not seen before.
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	st.byID[s.ID] = s
	if err := st.flushLocked(); err != nil {
		return Script{}, err
	}
	return s, nil
}

// delete removes a script by id. Refuses an unknown id rather than treating
// it as an already-satisfied no-op, so a caller's stale ID (a second
// browser tab, a double click) is told rather than left to wonder whether
// anything happened.
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

// validate normalises and checks one script before it is ever persisted or
// compiled a second time. Bounds are documented on the constants
// themselves (script.go); the goja.Compile call here is what makes "this
// script is saved" and "this script parses" the same fact, always.
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
	// strict mode: an undeclared assignment inside a script must raise a
	// ReferenceError rather than quietly create a global that could later
	// collide with a name this package adds - see the package doc
	// comment's sandbox enumeration for why the set of globals is meant to
	// be exact and closed.
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
