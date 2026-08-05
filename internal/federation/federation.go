// Package federation lets one KnightLoader act as the dashboard for others:
// peer instances are stored locally, and their REST APIs are proxied so the UI
// can view and control every instance from one place (self-hosted, no relay).
package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Instance is a peer KnightLoader reachable over HTTP.
type Instance struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,31}$`)

// Manager persists the peer list and talks to peers.
type Manager struct {
	path string
	hc   *http.Client

	mu   sync.Mutex
	list map[string]Instance // by name
}

// Load reads instances.json from dir (missing file = empty list).
func Load(dir string) (*Manager, error) {
	m := &Manager{
		path: filepath.Join(dir, "instances.json"),
		hc:   &http.Client{Timeout: 15 * time.Second},
		list: map[string]Instance{},
	}
	if b, err := os.ReadFile(m.path); err == nil {
		var arr []Instance
		if json.Unmarshal(b, &arr) == nil {
			for _, in := range arr {
				m.list[in.Name] = in
			}
		}
	}
	return m, nil
}

// List returns the peers sorted by name.
func (m *Manager) List() []Instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Instance, 0, len(m.list))
	for _, in := range m.list {
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Add validates and stores a peer (overwrites the same name).
func (m *Manager) Add(in Instance) error {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if !nameRe.MatchString(in.Name) {
		return errors.New("federation: invalid instance name")
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("federation: instance URL must be http(s)")
	}
	m.mu.Lock()
	m.list[in.Name] = in
	err = m.flushLocked()
	m.mu.Unlock()
	return err
}

// Remove deletes a peer by name.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	delete(m.list, name)
	err := m.flushLocked()
	m.mu.Unlock()
	return err
}

func (m *Manager) flushLocked() error {
	arr := make([]Instance, 0, len(m.list))
	for _, in := range m.list {
		arr = append(arr, in)
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].Name < arr[j].Name })
	b, err := json.MarshalIndent(arr, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0o600)
}

// get returns the stored instance for a name.
func (m *Manager) get(name string) (Instance, error) {
	m.mu.Lock()
	in, ok := m.list[name]
	m.mu.Unlock()
	if !ok {
		return Instance{}, fmt.Errorf("federation: unknown instance %q", name)
	}
	return in, nil
}

// Proxy forwards an API call to a peer and returns its response body.
// method + path are the peer-local API route (e.g. GET /api/tasks).
func (m *Manager) Proxy(ctx context.Context, name, method, path string, body []byte) ([]byte, int, error) {
	in, err := m.get(name)
	if err != nil {
		return nil, http.StatusNotFound, err
	}
	var rd io.Reader
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, in.URL+path, rd)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.hc.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("federation: %s unreachable: %w", in.Name, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	return b, resp.StatusCode, nil
}

// Ping checks a peer by listing its tasks.
func (m *Manager) Ping(ctx context.Context, name string) error {
	_, code, err := m.Proxy(ctx, name, http.MethodGet, "/api/tasks", nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("federation: peer answered HTTP %d", code)
	}
	return nil
}
