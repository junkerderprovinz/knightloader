package remotefs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id shared by the app's backend table, its
// directory expansion and the account catalogue; a mismatch would send these
// links to the HTTP engine.
const ResolverID = "remotefs"

// prio only matters for an https:// link on a host with a stored WebDAV
// account, the one case other resolvers also match. The user's own server is
// the more specific fact, so it sits above every debrid service (44 to 49) and
// resolver.Direct (40), with room for a debrid service added at 50.
const prio = 51

// The bounds on one directory expansion: an entry cap like the crawler's, and
// a depth cap because an FTP symlink to a parent directory makes a loop.
const (
	maxListingEntries = 2000
	maxListingDepth   = 8
)

// Resolver claims links on servers the user owns and answers what is behind
// them: the name, the size, and whether it is a folder to expand.
//
// It keeps no connection: every call dials, asks and hangs up, because an idle
// FTP control connection times out on the server's schedule. Staging a folder
// therefore costs one login per file; a connection cache would need exclusive
// ownership per transfer and idle eviction.
type Resolver struct {
	// Accounts is consulted by Match under the app's lock, so it must answer
	// from memory; the app wires a snapshot rebuilt on every account change.
	Accounts Accounts
	Dialer   Dialer
}

var _ resolver.Resolver = Resolver{}
var _ resolver.Checker = Resolver{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

// Match claims ftp, ftps, sftp, webdav and webdavs by scheme, and https only
// for a host with a stored account. The URL's shape alone would be a guess
// that takes links from other backends. http:// is never claimed, since an
// account does not say which scheme the host serves WebDAV on; webdav:// names
// the plaintext case explicitly.
func (r Resolver) Match(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "ftp", "ftps", "sftp", "webdav", "webdavs":
		return true
	case "https":
		return r.hasAccount(u.Hostname())
	}
	return false
}

func (r Resolver) hasAccount(host string) bool {
	if r.Accounts == nil {
		return false
	}
	_, ok := r.Accounts.Login(strings.ToLower(host))
	return ok
}

// Resolve confirms the file is there and says how to fetch it.
//
// For WebDAV it returns the plain http(s) URL with an Authorization header,
// which the engine fetches. For FTP, FTPS and SFTP it returns the
// credential-free link for Backend, which looks the login up again rather
// than receiving it through the dispatcher.
//
// A folder is an error: folder links are expanded by List before staging, so
// one arriving here was staged some other way.
func (r Resolver) Resolve(ctx context.Context, req resolver.Request) (resolver.Result, error) {
	t, login, fs, err := r.open(ctx, req.URL)
	if err != nil {
		return resolver.Result{}, err
	}
	defer fs.Close()

	e, err := fs.Stat(ctx, t.Path)
	if err != nil {
		return resolver.Result{}, err
	}
	if e.Dir {
		return resolver.Result{}, fmt.Errorf("remotefs: %s is a folder, not a file; paste it again to stage the files inside it", LinkOf(t))
	}

	name := e.Name
	if name == "" {
		name = Name(t)
	}
	res := resolver.Result{
		Name: name,
		Size: e.Size,
		// The successful stat is the availability check.
		Available: core.AvailOnline,
	}
	if t.Kind == KindWebDAV {
		res.DirectURL = t.HTTPURL()
		if login.Username != "" {
			res.Headers = map[string]string{
				"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(login.Username+":"+login.Password)),
			}
		}
		return res, nil
	}
	res.DirectURL = LinkOf(t)
	// One transfer per session is all FTP and SFTP offer, and more sessions
	// would mean more logins, which seedboxes often refuse as a bad password.
	res.Connections = 1
	return res, nil
}

// Listing is one file a folder link expands into: a canonical link of its own
// and what the server said about it.
type Listing struct {
	URL  string
	Name string
	Size int64
}

// List expands a folder link into the files under it, one task per file. It
// returns (nil, nil) for a link that is not a folder, meaning "stage it as
// itself".
//
// It recurses within maxListingDepth and maxListingEntries, so subfolders like
// "Subs" and "Sample" are not left behind. Equal names from different
// subfolders are kept apart by the collision policy.
func (r Resolver) List(ctx context.Context, raw string) ([]Listing, error) {
	t, _, fs, err := r.open(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer fs.Close()

	e, err := fs.Stat(ctx, t.Path)
	if err != nil {
		return nil, err
	}
	if !e.Dir {
		return nil, nil
	}
	var out []Listing
	if err := walk(ctx, fs, t, t.Path, 0, &out); err != nil {
		return nil, err
	}
	// Servers need not order a listing; sorting keeps the collector stable.
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out, nil
}

func walk(ctx context.Context, fs FS, t Target, dir string, depth int, out *[]Listing) error {
	if depth > maxListingDepth || len(*out) >= maxListingEntries {
		return nil
	}
	entries, err := fs.List(ctx, dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if len(*out) >= maxListingEntries {
			return nil
		}
		// A server-supplied name must not leave the listed folder, here or
		// later on disk.
		if e.Name == "" || e.Name == "." || e.Name == ".." || strings.ContainsAny(e.Name, `/\`) {
			continue
		}
		child := Join(dir, e.Name)
		if e.Dir {
			if err := walk(ctx, fs, t, child, depth+1, out); err != nil {
				return err
			}
			continue
		}
		ct := t
		ct.Path = child
		*out = append(*out, Listing{URL: LinkOf(ct), Name: e.Name, Size: e.Size})
	}
	return nil
}

// Check answers whether these links are still there with one connection per
// server, since a seedbox's connection limit would turn many parallel logins
// into refusals. Only a definite ErrNotFound is offline; a refused login, a
// server that is down or any other failure leaves the link uncheckable.
func (r Resolver) Check(ctx context.Context, urls []string) ([]core.Availability, error) {
	out := make([]core.Availability, len(urls))
	for i := range out {
		out[i] = core.AvailUncheckable
	}
	// One host over ftp and over sftp is two servers.
	groups := map[string][]int{}
	targets := map[string]Target{}
	for i, raw := range urls {
		t, err := r.target(raw)
		if err != nil {
			continue
		}
		key := string(t.Kind) + "|" + t.Addr()
		groups[key] = append(groups[key], i)
		targets[key] = t
	}
	for key, idx := range groups {
		t := targets[key]
		login, err := r.loginFor(t)
		if err != nil {
			continue
		}
		fs, err := r.Dialer.Dial(ctx, t, login)
		if err != nil {
			continue
		}
		for _, i := range idx {
			one, err := r.target(urls[i])
			if err != nil {
				continue
			}
			switch _, err := fs.Stat(ctx, one.Path); {
			case err == nil:
				out[i] = core.AvailOnline
			case errors.Is(err, ErrNotFound):
				out[i] = core.AvailOffline
			}
		}
		_ = fs.Close()
	}
	return out, nil
}

// open parses a link, finds its credential and connects. The caller closes
// the FS.
func (r Resolver) open(ctx context.Context, raw string) (Target, Login, FS, error) {
	t, err := r.target(raw)
	if err != nil {
		return Target{}, Login{}, nil, err
	}
	login, err := r.loginFor(t)
	if err != nil {
		return Target{}, Login{}, nil, err
	}
	fs, err := r.Dialer.Dial(ctx, t, login)
	if err != nil {
		return Target{}, Login{}, nil, err
	}
	return t, login, fs, nil
}

func (r Resolver) target(raw string) (Target, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Target{}, fmt.Errorf("remotefs: %q is not a URL: %w", raw, err)
	}
	return Parse(raw, r.hasAccount(u.Hostname()))
}

// loginFor picks the credential for a target. A stored account wins over the
// link. Without one, FTP uses the link's username or anonymous, WebDAV sends
// none (a public share needs none), and SFTP reports ErrNoAccount rather than
// failing later as an authentication error.
func (r Resolver) loginFor(t Target) (Login, error) {
	if r.Accounts != nil {
		if l, ok := r.Accounts.Login(t.Host); ok {
			return l, nil
		}
	}
	switch t.Kind {
	case KindFTP, KindFTPS:
		return Login{Username: t.User}, nil
	case KindWebDAV:
		return Login{Username: t.User}, nil
	}
	return Login{}, fmt.Errorf("remotefs: %s: %w", t.Host, ErrNoAccount)
}

// PackageName is the package a batch expanded from one directory link is
// filed under: the directory's name, or the host for the server's root. The
// app's own guess works from file names, which in a folder of unrelated files
// share no stem.
func PackageName(raw string) string {
	t, err := Parse(raw, true)
	if err != nil {
		return ""
	}
	if b := path.Base(t.Path); b != "" && b != "/" && b != "." {
		return b
	}
	return t.Host
}
