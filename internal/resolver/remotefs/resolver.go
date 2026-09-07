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

// ResolverID is the routing id, exported because three places outside this
// package have to agree on the literal string - the backend table
// (app.backendFor), the directory-expansion branch (app.crawl) and the account
// catalogue entry (internal/accounts). Every other resolver in this tree
// coordinates that by convention and a comment; this one has a constant,
// because a typo in any of those three silently routes remote-file links to
// the plain HTTP engine, which then fails on a scheme it has never heard of.
const ResolverID = "remotefs"

// prio sits above the whole debrid band (44 for Offcloud up to 49 for
// AllDebrid) and well above resolver.Direct's 40.
//
// For ftp://, ftps://, sftp:// and the two webdav:// schemes the number is
// decoration: nothing else in the tree matches those at all. It earns its keep
// for the ONE case that does overlap, the https:// link on a host the user has
// stored a WebDAV account for. There, a host somebody personally configured is
// a more specific fact than a debrid provider's public host list or Direct's
// guess from a file extension, and both of those would otherwise take the link
// and fail on a server they have no credential for.
//
// Two rather than one above AllDebrid, so that a later debrid service added at
// 50 - the next number anybody reaching for one would pick - does not silently
// end up level with this and have the tie settled by registration order.
//
// Torrent's own 50 is not a comparison this has to make: a magnet: or data:
// URI is not a URL with a host, and none of the five schemes here is either of
// those, so no link exists that both resolvers match and their relative order
// can never decide anything.
const prio = 51

// The two bounds on one directory expansion.
//
// The entry cap mirrors internal/crawler's own defaultMaxLinks and exists for
// the same reason: a link that quietly becomes ten thousand tasks is a paste
// nobody can undo. The depth cap is the more important of the two here,
// because an FTP server that publishes a symlink pointing at its own parent
// turns a walk into a loop, and no counter alone ends it in reasonable time.
const (
	maxListingEntries = 2000
	maxListingDepth   = 8
)

// Resolver claims links on servers the user owns and answers what is behind
// them - the name, the size, and whether it is a folder that should become
// several tasks instead of one.
//
// It holds no connection of its own. Every call dials, asks, and hangs up:
// an FTP control connection kept open between a paste and a download start is
// a connection the server times out on its own schedule, and a stale one fails
// at exactly the moment nothing is watching.
//
// THE COST OF THAT, STATED PLAINLY. Staging a folder of two hundred files is
// one List for the walk and then two hundred Resolves, one per staged link,
// each of them a full login. Against a seedbox on the other side of an ocean
// that is a slow paste. It is not a regression this resolver introduced -
// app.stage resolves every link it stages, whether it came from a page crawl,
// a container or a paste, and has since long before this package existed - but
// a login is a heavier Resolve than an http HEAD, so the arithmetic is worse
// here than anywhere else in the tree. Fixing it properly means a short-lived
// connection cache keyed by server, and a cache has to hand out EXCLUSIVE
// ownership (see ftpFS's own note: one connection is one in-flight transfer)
// and evict on the server's own idle timeout. That is a real piece of
// machinery, and it is deliberately not in this first version.
type Resolver struct {
	// Accounts is looked up by Match, which the dispatcher calls while holding
	// the app's lock - so an implementation must answer from memory. The app
	// wires a snapshot rebuilt on every account change (rewireBackends), never
	// the encrypted store itself.
	Accounts Accounts
	Dialer   Dialer
}

var _ resolver.Resolver = Resolver{}
var _ resolver.Checker = Resolver{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

// Match claims the four protocols by scheme, and https:// by ACCOUNT.
//
// The account gate is the whole safety of the https branch, and it is why
// Accounts is an interface rather than a flag: a resolver that claimed https
// on shape alone - a /remote.php/dav/ path, say - would take links away from
// every other backend in the tree the first time a hoster used a similar URL,
// and it would take them on a guess. "The user has stored a WebDAV login for
// exactly this hostname" is not a guess.
//
// http:// is deliberately NOT claimed the same way, even for a host with an
// account. The account is keyed to a host and says nothing about which scheme
// that host serves WebDAV on, and a plaintext link is far more often an
// ordinary download than a WebDAV share - so the plaintext case has to be
// named explicitly with webdav://, which is one keystroke and no ambiguity.
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
// WHAT IT HANDS BACK IS TWO DIFFERENT THINGS, on purpose:
//
//   - For WebDAV, the plain http(s) URL and an Authorization header. That link
//     goes straight to the embedded engine, which already fetches it with
//     several connections, byte ranges, the configured outbound route and the
//     speed limiter. Re-implementing any of that here would be a second, worse
//     HTTP downloader.
//   - For FTP, FTPS and SFTP, the link itself, unchanged and still without a
//     credential in it. Nothing in the engine speaks those, so Backend fetches
//     them - and it looks the credential up again from the account store
//     rather than being handed one here, because a Result travels through the
//     dispatcher and a password in it would be one copy of the secret too
//     many.
//
// A FOLDER IS AN ERROR HERE, and a deliberately worded one. A directory link
// is meant to be expanded into one task per file before it ever reaches this
// point (see List and its caller); a folder arriving anyway means it was
// staged before expansion existed, or restored from the store, and "this is a
// folder" is the only answer that tells somebody what to do about it.
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
		// The stat that just succeeded IS the availability check for this
		// link - the same "resolving and checking happened together" case
		// resolver.Result.Available was added for. Throwing it away would
		// leave every remote-server link grey in the collector until somebody
		// pressed Check, which would then make the identical call.
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
	// One connection, and it is a ceiling rather than a preference (see
	// app.connsFor). Both protocols really are one transfer per session: FTP's
	// own library says a connection supports one in-flight data connection,
	// and SFTP reads over one channel. Several would mean several LOGINS, and
	// a seedbox that caps concurrent logins per account - most of them do -
	// answers the second one with a refusal that looks like a bad password.
	res.Connections = 1
	return res, nil
}

// Listing is one file a folder link expands into: a link of its own, already
// canonical, with what the server said about it.
type Listing struct {
	URL  string
	Name string
	Size int64
}

// List expands a folder link into the files under it, so that a pasted
// directory becomes one task per file - the same thing the torrent resolver's
// file list does for the files inside a torrent.
//
// It returns (nil, nil) for a link that is not a folder, which is the answer
// the caller reads as "stage this one as itself". That is deliberately not an
// error: the common paste is a single file, and a caller that had to tell an
// error apart from a real answer on the ordinary path would get it wrong.
//
// IT RECURSES, bounded by maxListingDepth and maxListingEntries. A release
// folder on a seedbox has "Subs" and "Sample" in it, and a listing that
// stopped at the top level would silently leave those behind - silently,
// because there would be nothing on screen to say a folder had been skipped.
// Two files with the same name in different subfolders become two tasks with
// the same name, which the collision policy then keeps apart on disk exactly
// as it does for any other two downloads that agree on a name.
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
	// Sorted by path so a folder lists the same way twice running. Servers are
	// under no obligation to order a listing, and an unstable order turns the
	// collector's own row order into noise between two pastes of the same
	// folder.
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
		// A name a server invents must never be able to leave the folder that
		// was listed. "..", an absolute path or a separator in a name would
		// otherwise walk this loop straight up the tree, and on the download
		// side would name a file outside the download directory.
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

// Check answers whether these links are still there, one connection per
// server rather than one per link.
//
// This is the shape resolver.Checker was made batched for, and the one place
// in this tree where the batching is not about a rate limit but about a
// LOGIN: asking a seedbox about forty files by opening forty FTP sessions
// would be turned away by its own concurrent-connection limit long before the
// fortieth, and the refusals would read as forty dead links. Grouped by
// server, forty files cost one login.
//
// Anything that is not a definite "gone" is uncheckable, never offline. A
// refused credential, a server that is down, a folder that could not be
// listed - none of those is evidence about the file, and calling them offline
// would strike out a whole seedbox's worth of working links the first time it
// rebooted.
func (r Resolver) Check(ctx context.Context, urls []string) ([]core.Availability, error) {
	out := make([]core.Availability, len(urls))
	for i := range out {
		out[i] = core.AvailUncheckable
	}
	// Grouped by server AND protocol: the same host reached over ftp and over
	// sftp is two different servers as far as a connection is concerned.
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

// open is the three steps every entry point above starts with: parse, find the
// credential, connect. The caller closes the FS.
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

// loginFor is where a link and a stored account meet, and the order matters:
// the account store wins over anything the link says, because the link is a
// string somebody pasted and the account is a decision they made.
//
// FTP is the one protocol with a real anonymous mode, so a public archive with
// no account stored still works - with the username the link named, if it
// named one, which is what "ftp://anonymous@..." means. The other two have no
// credential-free form, so a missing account is reported as exactly that
// rather than left to fail as an authentication error three round trips
// later. WebDAV sits in between: a public share genuinely needs no
// credential, so no account means "send none" rather than a refusal.
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

// PackageName is the folder name a batch expanded out of one directory link
// should be filed under: the directory's own name, or the host for a link that
// named the server's root.
//
// It exists because the app's own guesser (derivePackage) works from the file
// names in a batch, and a folder full of unrelated files shares no stem - so a
// perfectly well-named release folder would land in the catch-all package
// while its name sat unused in the very link that produced it.
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
