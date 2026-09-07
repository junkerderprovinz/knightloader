package hostheaders

import (
	"context"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// TestMatchClaimsByConfigurationAndNeverByShape. Nothing about a URL says
// "this host wants a Referer"; the only evidence is that somebody stored one.
// A resolver that guessed would take links away from every other backend in
// the tree the first time a hoster's URL looked right.
func TestMatchClaimsByConfigurationAndNeverByShape(t *testing.T) {
	store := NewStore(mustAccounts(t))
	set, _ := Normalize(Set{Origin: "https://forum.example.org", Headers: []Header{{Name: "X-A", Value: "v"}}})
	if err := store.Save("forum", set); err != nil {
		t.Fatal(err)
	}
	r := Resolver{Profiles: store}

	if !r.Match("https://forum.example.org/attachments/1/x.rar") {
		t.Error("a link on a configured origin was not claimed")
	}
	for _, off := range []string{
		"https://forum.example.net/attachments/1/x.rar",
		"http://forum.example.org/attachments/1/x.rar",
		"magnet:?xt=urn:btih:abc",
		"ftp://forum.example.org/x.rar",
	} {
		if r.Match(off) {
			t.Errorf("Match(%q) claimed a link on an origin nobody configured", off)
		}
	}
	// A resolver with nothing wired into it claims nothing at all, rather than
	// panicking on the first staged link.
	if (Resolver{}).Match("https://forum.example.org/x") {
		t.Error("a Resolver with no profiles claimed a link")
	}
}

// TestResolveAttachesTheProfileStoredForTheLinksOwnOrigin is the path that
// needs no rule and no field on the task: a profile exists for the origin, so
// a link on it is fetched with those headers.
func TestResolveAttachesTheProfileStoredForTheLinksOwnOrigin(t *testing.T) {
	site := newSite(t)
	site.serve(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("bytes")) })

	store := NewStore(mustAccounts(t))
	set, _ := Normalize(Set{Origin: site.URL, Headers: []Header{{Name: "X-Auth-Token", Value: secretToken}}})
	if err := store.Save("box", set); err != nil {
		t.Fatal(err)
	}

	got, err := Resolver{Profiles: store}.Resolve(context.Background(),
		resolver.Request{URL: site.URL + "/files/holiday.zip"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Headers["X-Auth-Token"] != secretToken {
		t.Errorf("Headers = %d entries, want the stored token", len(got.Headers))
	}
	if got.Name != "holiday.zip" {
		t.Errorf("Name = %q, want the name off the URL", got.Name)
	}
	// The probe reached the server, so the link is known online without a
	// second round trip - the same "resolving and checking happened together"
	// case remotefs reports off its own stat.
	if got.Available != core.AvailOnline {
		t.Errorf("Available = %q, want online", got.Available)
	}
	if sent := site.saw(); sent.Get("X-Auth-Token") != secretToken {
		t.Error("the probe itself did not carry the stored header")
	}
}

// TestAnExpiredCookieIsNotADeadLink. A 401 or a 403 is the host saying the
// credential did not work; filing it as offline would put a dead marker on a
// link whose only problem is a cookie that needs pasting again.
func TestAnExpiredCookieIsNotADeadLink(t *testing.T) {
	site := newSite(t)
	site.serve(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone.zip":
			http.NotFound(w, r)
		default:
			http.Error(w, "no", http.StatusForbidden)
		}
	})
	store := NewStore(mustAccounts(t))
	set, _ := Normalize(Set{Origin: site.URL, Headers: []Header{{Name: "Cookie", Value: "session=stale"}}})
	if err := store.Save("box", set); err != nil {
		t.Fatal(err)
	}
	r := Resolver{Profiles: store}

	refused, err := r.Resolve(context.Background(), resolver.Request{URL: site.URL + "/x.zip"})
	if err != nil {
		t.Fatal(err)
	}
	if refused.Available != core.AvailUncheckable {
		t.Errorf("a 403 was filed as %q, want uncheckable", refused.Available)
	}
	gone, err := r.Resolve(context.Background(), resolver.Request{URL: site.URL + "/gone.zip"})
	if err != nil {
		t.Fatal(err)
	}
	if gone.Available != core.AvailOffline {
		t.Errorf("a 404 was filed as %q, want offline", gone.Available)
	}
}

// TestANamedProfileIsNeverSwappedForAnotherOrigins. The name comes from a rule
// or from a person; quietly using a different credential than the one they
// wrote down is how a login ends up at a host nobody meant to send it to.
// Sending nothing is the visible, safe failure.
func TestANamedProfileIsNeverSwappedForAnotherOrigins(t *testing.T) {
	site := newSite(t)
	site.serve(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("bytes")) })

	store := NewStore(mustAccounts(t))
	mine, _ := Normalize(Set{Origin: site.URL, Headers: []Header{{Name: "X-Auth-Token", Value: secretToken}}})
	if err := store.Save("thisbox", mine); err != nil {
		t.Fatal(err)
	}
	elsewhere, _ := Normalize(Set{Origin: "https://other.example.net", Headers: []Header{{Name: "X-Auth-Token", Value: "other"}}})
	if err := store.Save("otherbox", elsewhere); err != nil {
		t.Fatal(err)
	}

	got, err := Resolver{Profiles: store}.Resolve(context.Background(),
		resolver.Request{URL: site.URL + "/files/x.zip", Headers: "otherbox"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Headers) != 0 {
		t.Errorf("Headers = %v, want none: the named profile is for another origin", got.Headers)
	}
	if sent := site.saw(); sent.Get("X-Auth-Token") != "" {
		t.Error("the probe carried a header from a profile scoped to a different origin")
	}
}

// TestResolveDegradesToAPlainLinkWhenTheProbeFails. A host that is down is not
// a file that is gone, and a resolver that errored here would take the link
// out of the chain instead of letting the ordinary HTTP fallback have it.
func TestResolveDegradesToAPlainLinkWhenTheProbeFails(t *testing.T) {
	store := NewStore(mustAccounts(t))
	// Port 1 on loopback: nothing listens there, and the dial fails at once
	// rather than after a timeout.
	set, _ := Normalize(Set{Origin: "http://127.0.0.1:1", Headers: []Header{{Name: "X-A", Value: "v"}}})
	if err := store.Save("dead", set); err != nil {
		t.Fatal(err)
	}
	got, err := Resolver{Profiles: store}.Resolve(context.Background(),
		resolver.Request{URL: "http://127.0.0.1:1/x.zip"})
	if err != nil {
		t.Fatalf("Resolve returned an error for an unreachable host: %v", err)
	}
	if got.DirectURL != "http://127.0.0.1:1/x.zip" {
		t.Errorf("DirectURL = %q, want the link unchanged", got.DirectURL)
	}
	if got.Available != core.AvailUncheckable {
		t.Errorf("Available = %q, want uncheckable: nobody answered, which is not the same as gone", got.Available)
	}
	// The headers are still attached: the link is on the profile's own origin,
	// and the probe failing says nothing about whether the credential is right.
	if got.Headers["X-A"] != "v" {
		t.Error("a failed probe dropped the headers for the profile's own origin")
	}
}

// TestResolveOutranksDirectAndFallsBelowTheDebridBand pins the number rather
// than the comment: the whole point of the priority is which backend gets a
// link both of them claim.
func TestResolveOutranksDirectAndFallsBelowTheDebridBand(t *testing.T) {
	reg := resolver.NewRegistry()
	reg.Register(resolver.Direct{})
	reg.Register(Resolver{Profiles: fixed{"https://box.lan:443": "box"}})

	// A URL with a file extension in it, because that is the only kind
	// resolver.Direct claims at all - and a link neither of them claimed would
	// make this test pass by proving nothing.
	ids := []string{}
	for _, res := range reg.All("https://box.lan/file.zip") {
		ids = append(ids, res.Info().ID)
	}
	if len(ids) != 2 || ids[0] != ResolverID || ids[1] != "direct" {
		t.Fatalf("chain for a configured host = %v, want [%s direct]", ids, ResolverID)
	}
	if got := (Resolver{}).Info().Prio; got >= 44 {
		t.Errorf("Prio = %d, want it below the debrid band's 44 so a paid unlock goes first", got)
	}
}

// fixed is the Profiles a test hands in when it only needs Match answered.
type fixed map[string]string

func (f fixed) Covers(rawurl string) bool { _, ok := f[OriginOf(rawurl)]; return ok }

func (f fixed) ForURL(rawurl string) (string, Set) { return f[OriginOf(rawurl)], Set{} }

func (f fixed) Get(string) Set { return Set{} }
