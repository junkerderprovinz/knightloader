package hostheaders

// resolver.go: how a stored header block reaches an actual download.
//
// resolver.Result.Headers is the only thing in this tree that can fill
// engine.Job.Headers, and the only things that fill a Result are resolvers.
// So a header profile becomes reachable by being a resolver, and this is it:
// it claims a link on an origin the user configured headers for, resolves the
// redirect chain under the guard in redirect.go, and hands back the URL the
// chain ended at with the headers that are still valid there.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the routing id. Exported for the same reason
// remotefs.ResolverID is: the app's backend table and any log line about
// routing have to agree on the literal string.
const ResolverID = "hostheaders"

// prio places this resolver in the chain.
//
//   - Above resolver.Direct's 40, which claims a link because its path ends in
//     something that looks like a file extension and knows nothing at all
//     about the host. A host the user personally pasted a login for is the
//     more specific fact, and Direct would take the link and get a 401.
//   - Above jd's knownHostPrio of 41, which means no more than "JDownloader
//     ships a plugin for this host".
//   - BELOW the debrid band (44 for Offcloud up to 49 for AllDebrid), below
//     torrent's 50 and below remotefs' 51, and that direction is deliberate.
//     A debrid unlock or a stored WebDAV account is a way of FETCHING the
//     file; a header profile is an access detail for an ordinary GET. Where
//     both claim a link the capable backend should go first, and the
//     dispatcher's fallback chain still reaches this one when it fails.
const prio = 42

// Profiles is what the resolver asks about a link, and it is an interface
// rather than *Store for the reason remotefs.Accounts is one: Match is called
// by the dispatcher while the app's lock is held, so it must answer from
// memory, and this package has to be testable without an encrypted store on
// disk. *Store satisfies it and keeps the origin index that makes Covers
// cheap.
type Profiles interface {
	// Covers reports whether a profile is stored for rawurl's own origin.
	Covers(rawurl string) bool
	// ForURL returns the profile serving rawurl's origin and its id.
	ForURL(rawurl string) (string, Set)
	// Get returns one profile by id, or a zero Set.
	Get(id string) Set
}

// Resolver claims links on the origins a user stored headers for.
type Resolver struct {
	Profiles Profiles
	// Client is the outbound policy every preflight is made with. Nil builds
	// one on first use; the app hands in its own so that a proxy or a named
	// connection configured once applies here too.
	Client *http.Client
}

var _ resolver.Resolver = Resolver{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

// Match claims a link when a profile is stored for exactly its origin.
//
// BY CONFIGURATION AND NEVER BY SHAPE. Nothing about a URL says "this host
// wants a Referer"; the only evidence is that somebody stored one. A resolver
// that guessed would take links away from every other backend in the tree the
// first time a hoster's URL happened to look right, and it would take them on
// a guess - the same reasoning remotefs applies to its https:// branch.
func (r Resolver) Match(raw string) bool {
	if r.Profiles == nil {
		return false
	}
	return r.Profiles.Covers(raw)
}

// Resolve hands back the URL to fetch and the headers to fetch it with.
//
// The profile is chosen by the task's own Request.Headers when it names one,
// and otherwise by the link's origin. Both paths end at the same origin check
// in Attach, so the choice cannot widen what may be sent: a task pointed at
// the wrong profile gets no headers rather than another origin's credential.
func (r Resolver) Resolve(ctx context.Context, req resolver.Request) (resolver.Result, error) {
	if r.Profiles == nil {
		return resolver.Result{}, fmt.Errorf("hostheaders: no header profiles configured")
	}
	set := r.setFor(req)
	if set.Origin == "" {
		// Nothing stored for this link after all - the profile was removed
		// between Match and here, or the task names one that is gone. Handing
		// the URL back unchanged is the honest answer: the link is still a
		// plain http link and the fallback chain's HTTPFallback will fetch it
		// exactly as it would have before this package existed.
		return resolver.Result{Name: nameOf(req.URL), DirectURL: req.URL}, nil
	}

	probe, err := set.Preflight(ctx, r.client(), req.URL)
	if err != nil {
		// The link is NOT reported offline. A preflight that failed says the
		// probe did not get an answer, not that the file is gone, and
		// core.AvailUncheckable is the value that difference exists for.
		//
		// The headers still go out with the unresolved URL. A host that did
		// not answer this second says nothing about whether the credential is
		// right, and the download is the retry.
		return resolver.Result{
			Name:      nameOf(req.URL),
			DirectURL: req.URL,
			Headers:   set.Attach(req.URL),
			Available: core.AvailUncheckable,
		}, nil
	}
	return resolver.Result{
		Name:      nameOf(probe.URL),
		DirectURL: probe.URL,
		Headers:   probe.Headers,
		// The preflight IS the availability check for this link - the same
		// "resolving and checking happened together" case
		// resolver.Result.Available exists for, and the same call remotefs
		// makes off its own stat. Leaving it unknown would put a grey dot on
		// every row of a host we just successfully talked to.
		Available: availabilityOf(probe.Status),
	}, nil
}

// availabilityOf reads the probe's status line the way internal/core's own
// taxonomy means it.
//
// The one that is easy to get wrong is 401/403. That is the host saying the
// credential did not work, which is NOT the file being gone - filing it as
// offline would put a dead marker on a link whose only problem is an expired
// cookie, and the fix for an expired cookie is to paste a new one, not to
// delete the link.
//
// 416 counts as online: a server that answers "that range is not satisfiable"
// has looked the resource up to know it, which is exactly what was being
// asked.
func availabilityOf(status int) core.Availability {
	switch {
	case status >= 200 && status < 300, status == http.StatusRequestedRangeNotSatisfiable:
		return core.AvailOnline
	case status == http.StatusNotFound, status == http.StatusGone:
		return core.AvailOffline
	default:
		return core.AvailUncheckable
	}
}

// setFor picks the profile for one request: the one the task names, or the one
// stored for the link's origin.
//
// A named profile whose origin does not cover this link is NOT silently
// swapped for the origin's own. The name came from a rule or from a person,
// and quietly using a different credential than the one they wrote down is how
// a login ends up at a host nobody meant to send it to. Attach then returns
// nothing for it, which is the visible, safe failure.
func (r Resolver) setFor(req resolver.Request) Set {
	if id := ProfileID(req.Headers); id != "" {
		return r.Profiles.Get(id)
	}
	_, set := r.Profiles.ForURL(req.URL)
	return set
}

// client is the shared outbound client every preflight copies its transport
// from. Built once per Resolver value rather than per call, so a paste of
// several hundred links reuses one connection pool.
func (r Resolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return defaultClient
}

// defaultClient is the fallback for a Resolver nobody handed a client. It is
// package state, which is the thing internal/httpx's own package comment warns
// against - so it is deliberately only the DEFAULT: the app passes its own
// client in, and this exists so a zero-value Resolver in a test does not open
// a fresh transport per call.
var defaultClient = httpx.New(httpx.Options{Timeout: probeTimeout})

// nameOf is the file name to show before a download has started, taken from
// the URL's own path. The backend's progress stream supplies the real name
// once bytes move; this is the placeholder the collector shows until then, and
// it is derived exactly the way resolver.Direct derives its own so that a link
// looks the same in the list whichever of the two claimed it.
func nameOf(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "download"
	}
	if b := strings.TrimSpace(path.Base(u.Path)); b != "" && b != "/" && b != "." {
		return b
	}
	return "download"
}
