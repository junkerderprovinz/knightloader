package hostheaders

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

// ResolverID is the routing id the app's backend table and logs share.
const ResolverID = "hostheaders"

// prio puts this resolver first for every link it claims, above the debrid
// services and above a hand-arranged order (orderBase in internal/app, 1000).
// It claims only links on an origin the user stored headers for, usually their
// own premium cookie, which is as specific as a login of their own. The
// fallback chain still reaches the others when it fails.
const prio = 1001

// Profiles is what the resolver asks about a link. Match runs under the app's
// lock, so it must answer from memory; *Store satisfies it.
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
	// Client is the outbound client every preflight copies. Nil uses a
	// default; the app passes its own so a configured proxy applies here too.
	Client *http.Client
}

var _ resolver.Resolver = Resolver{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: prio} }

// Match claims a link only when a profile is stored for exactly its origin.
// Nothing about a URL's shape says it needs headers.
func (r Resolver) Match(raw string) bool {
	if r.Profiles == nil {
		return false
	}
	return r.Profiles.Covers(raw)
}

// Resolve hands back the URL to fetch and the headers to fetch it with. The
// profile is the one the task names, else the one for the link's origin; both
// go through the origin check in Attach, so a wrong name yields no headers.
func (r Resolver) Resolve(ctx context.Context, req resolver.Request) (resolver.Result, error) {
	if r.Profiles == nil {
		return resolver.Result{}, fmt.Errorf("hostheaders: no header profiles configured")
	}
	set := r.setFor(req)
	if set.Origin == "" {
		// The profile was removed after Match, or the task names one that is
		// gone; the link is still an ordinary http link.
		return resolver.Result{Name: nameOf(req.URL), DirectURL: req.URL}, nil
	}

	probe, err := set.Preflight(ctx, r.client(), req.URL)
	if err != nil {
		// A failed probe says nothing about the file, so the link is
		// uncheckable rather than offline, and the download itself is the
		// retry.
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
		Available: availabilityOf(probe.Status),
	}, nil
}

// availabilityOf reads the probe's status. 401 and 403 mean the credential
// failed, not that the file is gone, so they stay uncheckable. 416 counts as
// online because the server had to find the resource to refuse the range.
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

// setFor picks the profile the task names, or else the one stored for the
// link's origin. A named profile for another origin is not swapped for the
// matching one; Attach then sends nothing, which is the safe failure.
func (r Resolver) setFor(req resolver.Request) Set {
	if id := ProfileID(req.Headers); id != "" {
		return r.Profiles.Get(id)
	}
	_, set := r.Profiles.ForURL(req.URL)
	return set
}

func (r Resolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return defaultClient
}

// defaultClient serves a Resolver without a client, so a zero value in tests
// does not open a transport per call.
var defaultClient = httpx.New(httpx.Options{Timeout: probeTimeout})

// nameOf is the placeholder file name shown before a download starts, derived
// from the URL path the same way resolver.Direct does.
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
