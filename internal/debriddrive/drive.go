// Package debriddrive serves what is on the debrid accounts as a read-only
// WebDAV share, so a media server can stream it through an rclone mount
// without anything being downloaded first.
//
// The share has one folder per account, one folder per download on it, and
// the download's files below that. A listing is read from the service and kept
// for the refresh interval. A file is unlocked when somebody reads it, and each
// read asks the service's download server for the range it needs, so a player
// can seek.
package debriddrive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"
	"golang.org/x/time/rate"

	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

// Source is one debrid account as the drive reads it. Every client the import
// from an account can list implements it.
type Source interface {
	debrid.Lister
	TorrentStatus(ctx context.Context, id string) (debrid.TorrentJob, error)
	FileURL(ctx context.Context, id string, f debrid.TorrentFile) (debrid.Direct, error)
}

// Account is one folder at the top of the drive.
type Account struct {
	// Slot is the account's resolver slot, which keys what the drive keeps
	// of it.
	Slot   string
	Name   string
	Source Source
}

// linkLifetime is how long an unlocked link is used before the service is
// asked for a new one. The services keep theirs valid for hours; a link that
// dies sooner is noticed by the download server's refusal and unlocked again.
const linkLifetime = time.Hour

// The pace of the calls to one account's API. Real-Debrid allows 250 calls a
// minute, the strictest of the services, and a mount that lists a library
// folder by folder should never come near it, downloads running beside it.
const (
	callSpacing = 300 * time.Millisecond
	callBurst   = 5
)

// Drive is the share. Build it with New.
type Drive struct {
	accounts func() []Account
	refresh  func() time.Duration
	client   *http.Client
	locks    webdav.LockSystem
	born     time.Time
	now      func() time.Time
	logf     func(format string, args ...any)

	mu       sync.Mutex
	lists    map[string]*cached[listing]
	jobs     map[string]*cached[*node]
	links    map[string]*cached[string]
	limiters map[string]*rate.Limiter
}

// New builds a drive over the accounts accounts returns, which it asks on
// every request. refresh is how long a listing is served before the service
// is asked again, and client fetches from the download servers.
func New(accounts func() []Account, refresh func() time.Duration, client *http.Client) *Drive {
	return &Drive{
		accounts: accounts,
		refresh:  refresh,
		client:   client,
		locks:    webdav.NewMemLS(),
		born:     time.Now(),
		now:      time.Now,
		logf:     log.Printf,
		lists:    map[string]*cached[listing]{},
		jobs:     map[string]*cached[*node]{},
		links:    map[string]*cached[string]{},
		limiters: map[string]*rate.Limiter{},
	}
}

// cached is one value read from a service. Its lock is held across the read,
// so requests that want the same value wait for one call instead of making
// their own.
type cached[T any] struct {
	mu  sync.Mutex
	val T
	// at is when val was read, zero before the first read that worked.
	at time.Time
}

// get returns the value, reading it with fetch unless fresh says the one held
// will do. A failed read falls back to the value held when stale is set,
// since a listing a few minutes old beats a mount that shows nothing.
func (c *cached[T]) get(now time.Time, fresh func(T, time.Time) bool, stale bool, fetch func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && fresh(c.val, c.at) {
		return c.val, nil
	}
	v, err := fetch()
	if err != nil {
		if stale && !c.at.IsZero() {
			return c.val, nil
		}
		return v, err
	}
	c.val, c.at = v, now
	return v, nil
}

func entry[T any](d *Drive, m map[string]*cached[T], key string) *cached[T] {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := m[key]
	if c == nil {
		c = &cached[T]{}
		m[key] = c
	}
	return c
}

// call waits for the account's next turn at its API.
func (d *Drive) call(ctx context.Context, slot string) error {
	d.mu.Lock()
	l := d.limiters[slot]
	if l == nil {
		l = rate.NewLimiter(rate.Every(callSpacing), callBurst)
		d.limiters[slot] = l
	}
	d.mu.Unlock()
	return l.Wait(ctx)
}

// node is one folder or file of the drive.
type node struct {
	name string
	dir  bool
	size int64
	mod  time.Time
	// kids is a folder's content in name order.
	kids []*node
	// job and file name what a file is on its account.
	job  string
	file debrid.TorrentFile
	// complete marks the root of a download the service has finished.
	complete bool
}

func (n *node) child(name string) *node {
	i := sort.Search(len(n.kids), func(i int) bool { return n.kids[i].name >= name })
	if i < len(n.kids) && n.kids[i].name == name {
		return n.kids[i]
	}
	return nil
}

// listing is an account's downloads as folders, each named once.
type listing struct {
	downloads []*node
	byName    map[string]*node
}

// list reads an account's downloads, or takes them from memory.
func (d *Drive) list(ctx context.Context, acct Account) (listing, error) {
	fresh := func(_ listing, at time.Time) bool { return d.now().Sub(at) < d.refresh() }
	return entry(d, d.lists, acct.Slot).get(d.now(), fresh, true, func() (listing, error) {
		if err := d.call(ctx, acct.Slot); err != nil {
			return listing{}, err
		}
		all, _, err := acct.Source.List(ctx)
		if err != nil {
			return listing{}, err
		}
		l := d.name(all)
		d.forgetGone(acct.Slot, l)
		return l, nil
	})
}

// name turns the service's list into folders. Two downloads can share a name,
// so the older one keeps it and the other has its id added, which keeps a
// path from changing when something new arrives.
func (d *Drive) name(all []debrid.Listed) listing {
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Added.Equal(all[j].Added) {
			return all[i].Added.Before(all[j].Added)
		}
		return all[i].ID < all[j].ID
	})
	l := listing{byName: map[string]*node{}}
	for _, it := range all {
		name := segment(it.Name)
		if name == "" {
			name = segment(it.ID)
		}
		if l.byName[name] != nil {
			name += " [" + segment(it.ID) + "]"
		}
		if name == "" || l.byName[name] != nil {
			continue
		}
		mod := it.Added
		if mod.IsZero() {
			mod = d.born
		}
		n := &node{name: name, dir: true, size: it.Size, mod: mod, job: it.ID}
		l.byName[name] = n
		l.downloads = append(l.downloads, n)
	}
	sort.Slice(l.downloads, func(i, j int) bool { return l.downloads[i].name < l.downloads[j].name })
	return l
}

// forgetGone drops what the drive holds for downloads no longer on the
// account, so memory follows the account rather than everything it ever held.
func (d *Drive) forgetGone(slot string, l listing) {
	listed := map[string]bool{}
	for _, n := range l.downloads {
		listed[n.job] = true
	}
	gone := func(key string) bool {
		s, rest, _ := strings.Cut(key, "\x00")
		job, _, _ := strings.Cut(rest, "\x00")
		return s == slot && !listed[job]
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for key := range d.jobs {
		if gone(key) {
			delete(d.jobs, key)
		}
	}
	for key := range d.links {
		if gone(key) {
			delete(d.links, key)
		}
	}
}

// files reads one download's files, or takes them from memory. A download
// that is complete on the service is not read again while it stays listed,
// since its files do not change; one still on its way is read again after the
// refresh interval.
func (d *Drive) files(ctx context.Context, acct Account, dl *node) (*node, error) {
	fresh := func(root *node, at time.Time) bool {
		return root.complete || d.now().Sub(at) < d.refresh()
	}
	return entry(d, d.jobs, acct.Slot+"\x00"+dl.job).get(d.now(), fresh, true, func() (*node, error) {
		if err := d.call(ctx, acct.Slot); err != nil {
			return nil, err
		}
		job, err := acct.Source.TorrentStatus(ctx, dl.job)
		if err != nil {
			return nil, err
		}
		return tree(dl, job), nil
	})
}

// tree builds the folders and files of one download from what the service
// holds of it. A path a service states starts with the torrent's own folder
// or without it, so that folder comes off where it is in front, the download
// already being a folder of that name. A file the service cannot hand out yet
// is left out, and so is one it names no path for, since only the unlock would
// tell its name.
func tree(dl *node, job debrid.TorrentJob) *node {
	root := &node{name: dl.name, dir: true, mod: dl.mod, complete: job.State == debrid.TorrentReady}
	for _, f := range job.Files {
		if !f.Held {
			continue
		}
		var parts []string
		for _, p := range strings.Split(strings.ReplaceAll(f.Path, `\`, "/"), "/") {
			if s := segment(p); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 1 && (parts[0] == segment(job.Name) || parts[0] == dl.name) {
			parts = parts[1:]
		}
		if len(parts) == 0 {
			continue
		}
		at := root
		for _, p := range parts[:len(parts)-1] {
			next := at.child(p)
			if next == nil {
				next = &node{name: p, dir: true, mod: dl.mod}
				at.insert(next)
			}
			if !next.dir {
				at = nil
				break
			}
			at = next
		}
		name := parts[len(parts)-1]
		if at == nil || at.child(name) != nil {
			continue
		}
		at.insert(&node{name: name, size: f.Size, mod: dl.mod, job: dl.job, file: f})
	}
	return root
}

func (n *node) insert(kid *node) {
	i := sort.Search(len(n.kids), func(i int) bool { return n.kids[i].name >= kid.name })
	n.kids = append(n.kids, nil)
	copy(n.kids[i+1:], n.kids[i:])
	n.kids[i] = kid
}

// segment makes one name usable as a path segment: no slash, no control
// character, no space at either end. A name that would step out of its folder
// comes back empty.
func segment(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\':
			return '_'
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, s))
	if s == "." || s == ".." {
		return ""
	}
	return s
}

// level is how deep in the drive a path is.
type level int

const (
	atRoot level = iota
	atAccount
	atDownload
	inDownload
)

// spot is a path of the drive found: its node, how deep it is, and the
// account it is on below the root.
type spot struct {
	node  *node
	level level
	acct  Account
}

// find walks a path. Only what the path passes through is read: the root and
// an account's folder need nothing, a download's folder the account's
// listing, and anything inside a download that download's files.
func (d *Drive) find(ctx context.Context, name string) (spot, error) {
	parts := strings.Split(strings.Trim(path.Clean("/"+name), "/"), "/")
	if parts[0] == "" {
		return spot{node: &node{name: "/", dir: true, mod: d.born}}, nil
	}
	var acct Account
	found := false
	for _, a := range d.folders() {
		if a.Name == parts[0] {
			acct, found = a, true
			break
		}
	}
	if !found {
		return spot{}, os.ErrNotExist
	}
	if len(parts) == 1 {
		return spot{node: d.accountNode(acct), level: atAccount, acct: acct}, nil
	}
	l, err := d.list(ctx, acct)
	if err != nil {
		return spot{}, err
	}
	dl := l.byName[parts[1]]
	if dl == nil {
		return spot{}, os.ErrNotExist
	}
	if len(parts) == 2 {
		return spot{node: dl, level: atDownload, acct: acct}, nil
	}
	at, err := d.files(ctx, acct, dl)
	if err != nil {
		return spot{}, err
	}
	for _, p := range parts[2:] {
		if at = at.child(p); at == nil {
			return spot{}, os.ErrNotExist
		}
	}
	return spot{node: at, level: inDownload, acct: acct}, nil
}

// folders is every account in name order, named as its folder. The name goes
// through segment like any other, since an account id may hold a slash. A
// name segment leaves alone is handed out first, so an account is found under
// the name it has, and one that is taken gets a number added.
func (d *Drive) folders() []Account {
	all := d.accounts()
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Name == segment(all[i].Name) && all[j].Name != segment(all[j].Name)
	})
	var out []Account
	taken := map[string]bool{}
	for _, a := range all {
		base := segment(a.Name)
		if base == "" {
			continue
		}
		a.Name = base
		for n := 2; taken[a.Name]; n++ {
			a.Name = base + " " + strconv.Itoa(n)
		}
		taken[a.Name] = true
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Folders names the account folders at the top of the drive.
func (d *Drive) Folders() []string {
	var out []string
	for _, a := range d.folders() {
		out = append(out, a.Name)
	}
	return out
}

func (d *Drive) accountNode(a Account) *node {
	return &node{name: a.Name, dir: true, mod: d.born}
}

// kids lists a folder.
func (d *Drive) kids(ctx context.Context, s spot) ([]*node, error) {
	switch s.level {
	case atRoot:
		var out []*node
		for _, a := range d.folders() {
			out = append(out, d.accountNode(a))
		}
		return out, nil
	case atAccount:
		l, err := d.list(ctx, s.acct)
		return l.downloads, err
	case atDownload:
		root, err := d.files(ctx, s.acct, s.node)
		if err != nil {
			return nil, err
		}
		return root.kids, nil
	}
	return s.node.kids, nil
}

// key names one file's unlocked link.
func key(slot string, n *node) string {
	return slot + "\x00" + n.job + "\x00" + n.file.ID
}

// link unlocks a file, or takes the link unlocked for it within the hour.
func (d *Drive) link(ctx context.Context, acct Account, n *node) (string, error) {
	fresh := func(_ string, at time.Time) bool { return d.now().Sub(at) < linkLifetime }
	return entry(d, d.links, key(acct.Slot, n)).get(d.now(), fresh, false, func() (string, error) {
		if err := d.call(ctx, acct.Slot); err != nil {
			return "", err
		}
		direct, err := acct.Source.FileURL(ctx, n.job, n.file)
		if err != nil {
			return "", err
		}
		if direct.URL == "" {
			return "", errors.New("the service gave no link for the file")
		}
		return direct.URL, nil
	})
}

// forgetLink drops a link the download server refused, unless another read
// has already put a new one in its place.
func (d *Drive) forgetLink(acct Account, n *node, dead string) {
	c := entry(d, d.links, key(acct.Slot, n))
	c.mu.Lock()
	if c.val == dead {
		c.at = time.Time{}
	}
	c.mu.Unlock()
}

// open asks the download server for a file from offset on. A refusal that
// means the link has run out unlocks the file again, once.
func (d *Drive) open(ctx context.Context, acct Account, n *node, offset int64) (io.ReadCloser, error) {
	for again := false; ; again = true {
		u, err := d.link(ctx, acct, n)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, errors.New("the service gave a link that is not an address")
		}
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
		resp, err := d.client.Do(req)
		if err != nil {
			// The link is a credential in itself, and a url.Error repeats it.
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			return nil, fmt.Errorf("the download server could not be reached: %w", err)
		}
		if resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusOK && offset == 0 {
			return resp.Body, nil
		}
		resp.Body.Close()
		if expired(resp.StatusCode) && !again {
			d.forgetLink(acct, n, u)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return nil, errors.New("the download server cannot start a file part way")
		}
		return nil, fmt.Errorf("the download server answered %s", resp.Status)
	}
}

// expired reports whether a download server's answer means the link is no
// longer good, as opposed to the file or the server being in trouble.
func expired(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return true
	}
	return false
}
