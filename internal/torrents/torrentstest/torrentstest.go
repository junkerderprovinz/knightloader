// Package torrentstest is a torrent client in memory, for the tests of the
// engine and the app. A swarm shared by the whole test binary holds the files
// of every torrent a test seeds (see Share), and a torrent added to a Client
// fetches them from there: on the client's next reading, unless the test
// stalls it, as if every byte arrived at once.
package torrentstest

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
)

// PieceLength is the piece size of every torrent this client holds.
const PieceLength = 16 << 10

// shared is the swarm: where the files of each seeded torrent are, by hash.
var shared = struct {
	sync.Mutex
	m map[string]share
}{m: map[string]share{}}

type share struct {
	dir   string
	files []torrents.File
}

// Share puts a torrent's files into the swarm. dir holds them at the paths of
// files, which are relative to it as torrents.File's are to Add.Dir.
func Share(hash, dir string, files []torrents.File) {
	shared.Lock()
	defer shared.Unlock()
	shared.m[hash] = share{dir: dir, files: slices.Clone(files)}
}

// Unshare takes a torrent out of the swarm again.
func Unshare(hash string) {
	shared.Lock()
	defer shared.Unlock()
	delete(shared.m, hash)
}

func lookup(hash string) (share, bool) {
	shared.Lock()
	defer shared.Unlock()
	s, ok := shared.m[hash]
	return s, ok
}

// Client is the client in memory. The zero value is not usable; see New.
type Client struct {
	mu       sync.Mutex
	held     map[string]*held
	prefs    []torrents.Prefs
	stalled  bool
	adds     []torrents.Add
	failNext error
	lag      int
}

type held struct {
	add      torrents.Add
	files    []torrents.File
	state    torrents.State
	err      string
	uploaded int64
	// seeded is how long the torrent has seeded, counted at each reading
	// from the one before.
	seeded    time.Duration
	read      time.Time
	streaming bool
	// hidden counts the calls for which the client still lacks the torrent
	// after taking it, as qBittorrent does while it adds in the background.
	hidden int
}

// New is an empty client.
func New() *Client { return &Client{held: map[string]*held{}} }

// Stall keeps every torrent from fetching until Flow.
func (c *Client) Stall() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stalled = true
}

// Flow lets the torrents fetch again.
func (c *Client) Flow() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stalled = false
}

// Has reports whether the client holds the torrent.
func (c *Client) Has(hash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.held[hash] != nil
}

// Added is every add so far.
func (c *Client) Added() []torrents.Add {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.adds)
}

// Prefs is every Configure so far.
func (c *Client) Prefs() []torrents.Prefs {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.prefs)
}

// State is where a torrent stands, and false when the client lacks it.
func (c *Client) State(hash string) (torrents.State, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.held[hash]
	if h == nil {
		return 0, false
	}
	return h.state, true
}

// Streaming reports whether the torrent fetches in order for a player.
func (c *Client) Streaming(hash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.held[hash]
	return h != nil && h.streaming
}

// Upload counts bytes the torrent gave the swarm and seconds it seeded, on top
// of the time it seeds between two readings.
func (c *Client) Upload(hash string, bytes, seconds int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h := c.held[hash]; h != nil {
		h.uploaded += bytes
		h.seeded += time.Duration(seconds) * time.Second
	}
}

// Fail has the client give up on the torrent, with why.
func (c *Client) Fail(hash, why string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h := c.held[hash]; h != nil {
		h.state, h.err = torrents.Failed, why
	}
}

// Lag has the client take every later add in the background, lacking the
// torrent for the next n calls about it.
func (c *Client) Lag(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lag = n
}

// FailNext has the next call fail with err.
func (c *Client) FailNext(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failNext = err
}

// failing returns the error FailNext left, once. Caller holds c.mu.
func (c *Client) failing() error {
	err := c.failNext
	c.failNext = nil
	return err
}

func (c *Client) Add(_ context.Context, a torrents.Add) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failing(); err != nil {
		return err
	}
	if c.held[a.Hash] != nil {
		return fmt.Errorf("the client has %s already", a.Hash)
	}
	c.adds = append(c.adds, a)
	h := &held{add: a, state: torrents.Stopped, hidden: c.lag}
	if a.File != nil {
		files, err := filesOf(a.File)
		if err != nil {
			return err
		}
		h.files = files
	} else if s, ok := lookup(a.Hash); ok {
		h.files = slices.Clone(s.files)
	} else {
		// Nobody shares it, so its file list never comes.
		h.state = torrents.Fetching
	}
	for i := range h.files {
		h.files[i].Picked, h.files[i].Done = true, 0
	}
	if a.Complete {
		for i := range h.files {
			h.files[i].Done = h.files[i].Size
		}
		h.state = torrents.Finished
	}
	c.held[a.Hash] = h
	return nil
}

// filesOf is the file list of a .torrent as the client names the files.
func filesOf(raw []byte) ([]torrents.File, error) {
	md, err := torrent.Parse(raw)
	if err != nil {
		return nil, err
	}
	out := make([]torrents.File, len(md.Files))
	for i, f := range md.Files {
		p := f.Path
		if len(md.Files) > 1 || f.Path != md.Name {
			p = path.Join(md.Name, f.Path)
		}
		out[i] = torrents.File{Path: p, Size: f.Size}
	}
	return out, nil
}

func (c *Client) torrent(hash string) (*held, error) {
	if err := c.failing(); err != nil {
		return nil, err
	}
	h := c.held[hash]
	if h == nil {
		return nil, torrents.ErrUnknown
	}
	if h.hidden > 0 {
		h.hidden--
		return nil, torrents.ErrUnknown
	}
	return h, nil
}

func (c *Client) Files(_ context.Context, hash string) ([]torrents.File, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, err := c.torrent(hash)
	if err != nil {
		return nil, err
	}
	return slices.Clone(h.files), nil
}

func (c *Client) Pick(_ context.Context, hash string, picked []bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, err := c.torrent(hash)
	if err != nil {
		return err
	}
	if len(picked) != len(h.files) {
		return fmt.Errorf("%d files picked of %d", len(picked), len(h.files))
	}
	for i := range h.files {
		h.files[i].Picked = picked[i]
	}
	if h.state.Complete() && !h.complete() {
		h.state = torrents.Fetching
	}
	return nil
}

func (c *Client) Start(_ context.Context, hash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, err := c.torrent(hash)
	if err != nil {
		return err
	}
	switch {
	case h.state == torrents.Failed:
	case h.complete():
		h.state = torrents.Seeding
	default:
		h.state = torrents.Fetching
	}
	return nil
}

func (c *Client) Stop(_ context.Context, hash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, err := c.torrent(hash)
	if err != nil {
		return err
	}
	switch {
	case h.state == torrents.Failed:
	case h.complete():
		h.state = torrents.Finished
	default:
		h.state = torrents.Stopped
	}
	return nil
}

func (c *Client) Delete(_ context.Context, hash string, files bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, err := c.torrent(hash)
	if err != nil {
		return err
	}
	delete(c.held, hash)
	if files {
		for _, f := range h.files {
			_ = os.Remove(filepath.Join(h.add.Dir, filepath.FromSlash(f.Path)))
		}
	}
	return nil
}

// Readings first lets every running torrent fetch what the swarm has of it,
// and finds the files of a seed run where they should be.
func (c *Client) Readings(_ context.Context) (map[string]torrents.Reading, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failing(); err != nil {
		return nil, err
	}
	out := make(map[string]torrents.Reading, len(c.held))
	now := time.Now()
	for hash, h := range c.held {
		if h.state == torrents.Fetching && !c.stalled {
			h.fetch(hash)
		}
		if h.state == torrents.Seeding && !h.read.IsZero() {
			h.seeded += now.Sub(h.read)
		}
		h.read = now
		r := torrents.Reading{State: h.state, Err: h.err, Dir: h.add.Dir, Uploaded: h.uploaded, SeedSeconds: int64(h.seeded / time.Second)}
		for _, f := range h.files {
			if f.Picked {
				r.Done += f.Done
				r.Size += f.Size
			}
		}
		if r.Size > 0 {
			r.Ratio = float64(h.uploaded) / float64(r.Size)
		}
		out[hash] = r
	}
	return out, nil
}

// fetch copies every picked file from the swarm, or takes a file already in
// place at its full size as it is.
func (h *held) fetch(hash string) {
	s, shared := lookup(hash)
	for i, f := range h.files {
		if !f.Picked || f.Done == f.Size {
			continue
		}
		dst := filepath.Join(h.add.Dir, filepath.FromSlash(f.Path))
		if fi, err := os.Stat(dst); err == nil && fi.Size() == f.Size {
			h.files[i].Done = f.Size
			continue
		}
		if !shared {
			continue
		}
		if err := copyFile(filepath.Join(s.dir, filepath.FromSlash(s.files[i].Path)), dst); err != nil {
			h.state, h.err = torrents.Failed, err.Error()
			return
		}
		h.files[i].Done = f.Size
	}
	if h.complete() {
		h.state = torrents.Seeding
	}
}

func (h *held) complete() bool {
	if len(h.files) == 0 {
		return false
	}
	for _, f := range h.files {
		if f.Picked && f.Done < f.Size {
			return false
		}
	}
	return true
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Pieces counts a piece here when every byte of it is in a file that has
// arrived whole, or that is empty.
func (c *Client) Pieces(_ context.Context, hash string) (torrents.Pieces, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, err := c.torrent(hash)
	if err != nil {
		return torrents.Pieces{}, err
	}
	var total int64
	for _, f := range h.files {
		total += f.Size
	}
	states := make([]int, (total+PieceLength-1)/PieceLength)
	for i := range states {
		states[i] = torrents.PieceHere
	}
	var at int64
	for _, f := range h.files {
		if f.Done < f.Size {
			for p := at / PieceLength; p <= (at+f.Size-1)/PieceLength; p++ {
				states[p] = torrents.PieceMissing
			}
		}
		at += f.Size
	}
	return torrents.Pieces{Length: PieceLength, States: states}, nil
}

// Metainfo is the .torrent a torrent was added with. One added as a magnet
// has none here.
func (c *Client) Metainfo(_ context.Context, hash string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, err := c.torrent(hash)
	if err != nil {
		return nil, err
	}
	if h.add.File == nil {
		return nil, torrents.ErrUnknown
	}
	return h.add.File, nil
}

func (c *Client) Stream(_ context.Context, hash string, on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, err := c.torrent(hash)
	if err != nil {
		return err
	}
	h.streaming = on
	return nil
}

func (c *Client) Configure(_ context.Context, p torrents.Prefs) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failing(); err != nil {
		return err
	}
	c.prefs = append(c.prefs, p)
	return nil
}

var _ torrents.Client = (*Client)(nil)
