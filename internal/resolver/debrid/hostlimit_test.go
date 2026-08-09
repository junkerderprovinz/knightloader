package debrid

// The per-host chunk ceiling: what RealDebrid.Unlock learns from a live
// response, and how it survives to answer HostLimit / Resolver.HostCap
// afterwards. See HostLimiter's doc comment in debrid.go for why this is
// learned opportunistically rather than fetched from a per-host table - none
// exists anywhere in Real-Debrid's documented API.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestRealDebridUnlockLearnsTheHostChunkCap drives Unlock against a response
// shaped like the live API (verified: "chunks" is documented as "Max Chunks
// allowed" on /unrestrict/link, api.real-debrid.com) and checks the number
// becomes visible through HostLimit afterwards, keyed by the ORIGINAL link's
// host - not Real-Debrid's own CDN host the direct URL resolves to, which
// would make every provider's cache key collide on "cdn.real-debrid.com".
func TestRealDebridUnlockLearnsTheHostChunkCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"filename":"f.bin","filesize":99,
			"download":"https://cdn.real-debrid.com/d/ABC/f.bin","chunks":4}`))
	}))
	defer srv.Close()

	rd := NewRealDebrid("T")
	rd.base = srv.URL

	if got := rd.HostLimit("rapidgator.net"); got != 0 {
		t.Fatalf("HostLimit before any unlock = %d, want 0 (no opinion)", got)
	}

	if _, err := rd.Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
		t.Fatal(err)
	}

	if got := rd.HostLimit("rapidgator.net"); got != 4 {
		t.Errorf("HostLimit(rapidgator.net) = %d, want 4 (the CDN host's own domain must not be what got remembered)", got)
	}
	if got := rd.HostLimit("cdn.real-debrid.com"); got != 0 {
		t.Errorf("HostLimit(cdn.real-debrid.com) = %d, want 0 - the direct URL's host has no business in this cache", got)
	}
	if got := rd.HostLimit("www.rapidgator.net"); got != 4 {
		t.Errorf("HostLimit(www.rapidgator.net) = %d, want 4 - a www. prefix must not miss the cache NormalizeHost already strips it in", got)
	}
}

// TestRealDebridHostLimitKeepsTheSmallest pins the merge rule: two unlocks
// against the same host that report different chunk ceilings must leave the
// smaller one in force, because that is the one no request against the host
// has ever been refused for.
func TestRealDebridHostLimitKeepsTheSmallest(t *testing.T) {
	chunks := 16
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"filename":"f.bin","filesize":1,"download":"https://cdn.real-debrid.com/d/1/f.bin","chunks":` +
			strconv.Itoa(chunks) + `}`))
	}))
	defer srv.Close()

	rd := NewRealDebrid("T")
	rd.base = srv.URL

	if _, err := rd.Unlock(context.Background(), "https://slowhost.example/a"); err != nil {
		t.Fatal(err)
	}
	if got := rd.HostLimit("slowhost.example"); got != 16 {
		t.Fatalf("after the first unlock, HostLimit = %d, want 16", got)
	}

	chunks = 2
	if _, err := rd.Unlock(context.Background(), "https://slowhost.example/b"); err != nil {
		t.Fatal(err)
	}
	if got := rd.HostLimit("slowhost.example"); got != 2 {
		t.Errorf("after a smaller second unlock, HostLimit = %d, want 2 (the smaller of the two seen)", got)
	}

	chunks = 12
	if _, err := rd.Unlock(context.Background(), "https://slowhost.example/c"); err != nil {
		t.Fatal(err)
	}
	if got := rd.HostLimit("slowhost.example"); got != 2 {
		t.Errorf("after a LARGER third unlock, HostLimit = %d, want the 2 already on record - a larger report must never raise the cap back up", got)
	}
}

// TestRealDebridHostLimitIgnoresAZeroChunks pins the other edge: a response
// that omits "chunks" (the ordinary shape for most links, per the docs) must
// not be read as "zero chunks allowed" - that would hand connsFor a hard stop
// on every future download to a host Real-Debrid simply said nothing about.
func TestRealDebridHostLimitIgnoresAZeroChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"filename":"f.bin","filesize":1,"download":"https://cdn.real-debrid.com/d/1/f.bin"}`))
	}))
	defer srv.Close()

	rd := NewRealDebrid("T")
	rd.base = srv.URL

	if _, err := rd.Unlock(context.Background(), "https://nolimit.example/a"); err != nil {
		t.Fatal(err)
	}
	if got := rd.HostLimit("nolimit.example"); got != 0 {
		t.Errorf("HostLimit(nolimit.example) = %d, want 0 - an absent chunks field is not a report of zero", got)
	}
}

// TestDebridResolverHostCapDelegatesToTheService pins the seam between the
// registry-facing Resolver and whatever Service backs it: HostCap must read
// straight through to a HostLimiter-implementing service, and answer 0 for
// one that does not implement it at all - AllDebrid today, since neither
// /hosts nor /user/hosts carries anything comparable in AllDebrid's own API.
func TestDebridResolverHostCapDelegatesToTheService(t *testing.T) {
	rd := NewRealDebrid("T")
	res := Resolver{ServiceID: "realdebrid", Svc: rd}
	if got := res.HostCap("anyhost.example"); got != 0 {
		t.Fatalf("HostCap before any unlock = %d, want 0", got)
	}

	ad := NewAllDebrid("K")
	adRes := Resolver{ServiceID: "alldebrid", Svc: ad}
	if got := adRes.HostCap("anyhost.example"); got != 0 {
		t.Errorf("HostCap for a service with no opinion at all = %d, want 0, never an invented ceiling", got)
	}

	noSvc := Resolver{ServiceID: "unwired"}
	if got := noSvc.HostCap("anyhost.example"); got != 0 {
		t.Errorf("HostCap with Svc unset = %d, want 0", got)
	}
}
