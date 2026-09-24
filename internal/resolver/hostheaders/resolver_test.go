package hostheaders

import (
	"context"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

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
	if (Resolver{}).Match("https://forum.example.org/x") {
		t.Error("a Resolver with no profiles claimed a link")
	}
}

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
	// The probe reached the server, so the link is known to be online.
	if got.Available != core.AvailOnline {
		t.Errorf("Available = %q, want online", got.Available)
	}
	if sent := site.saw(); sent.Get("X-Auth-Token") != secretToken {
		t.Error("the probe itself did not carry the stored header")
	}
}

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

func TestResolveDegradesToAPlainLinkWhenTheProbeFails(t *testing.T) {
	store := NewStore(mustAccounts(t))
	// Nothing listens on port 1, so the dial fails at once.
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
	if got.Headers["X-A"] != "v" {
		t.Error("a failed probe dropped the headers for the profile's own origin")
	}
}

func TestResolveGoesFirstForAConfiguredHost(t *testing.T) {
	reg := resolver.NewRegistry()
	reg.Register(resolver.Direct{})
	reg.Register(Resolver{Profiles: fixed{"https://box.lan:443": "box"}})

	// The URL needs a file extension, or Direct would not claim it.
	ids := []string{}
	for _, res := range reg.All("https://box.lan/file.zip") {
		ids = append(ids, res.Info().ID)
	}
	if len(ids) != 2 || ids[0] != ResolverID || ids[1] != "direct" {
		t.Fatalf("chain for a configured host = %v, want [%s direct]", ids, ResolverID)
	}
	// A profile is the user's own setup for its origin, often a premium
	// cookie, and the priority card does not list it, so nothing else could
	// move it above a debrid service that carries the same host.
	if got := (Resolver{}).Info().Prio; got <= 49 {
		t.Errorf("Prio = %d, want it above the debrid band (43 to 49)", got)
	}
}

// fixed is the Profiles a test hands in when it only needs Match answered.
type fixed map[string]string

func (f fixed) Covers(rawurl string) bool { _, ok := f[OriginOf(rawurl)]; return ok }

func (f fixed) ForURL(rawurl string) (string, Set) { return f[OriginOf(rawurl)], Set{} }

func (f fixed) Get(string) Set { return Set{} }
