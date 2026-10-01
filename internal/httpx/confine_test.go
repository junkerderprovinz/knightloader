package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestScopeOf(t *testing.T) {
	cases := map[string]Scope{
		"93.184.216.34":    ScopePublic,
		"2606:4700::1111":  ScopePublic,
		"127.0.0.1":        ScopeLocal,
		"::1":              ScopeLocal,
		"::ffff:127.0.0.1": ScopeLocal,
		"0.0.0.0":          ScopeLocal,
		"10.1.2.3":         ScopeLocal,
		"192.168.1.1":      ScopeLocal,
		"fd12:3456::1":     ScopeLocal,
		"100.100.1.1":      ScopeLocal,
		"169.254.169.254":  ScopeInternal,
		"fe80::1":          ScopeInternal,
		"fd00:ec2::254":    ScopeInternal,
		"100.100.100.200":  ScopeInternal,
		"224.0.0.1":        ScopeInternal,
	}
	for addr, want := range cases {
		if got := ScopeOf(netip.MustParseAddr(addr)); got != want {
			t.Errorf("ScopeOf(%s) = %d, want %d", addr, got, want)
		}
	}
}

func TestAConfinedRequestStopsAtItsScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	c := New(Options{Proxy: NoProxy})

	get := func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := c.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	ctx := context.Background()
	if err := get(Confine(ctx, ScopePublic)); !errors.Is(err, ErrRefused) {
		t.Errorf("a request confined to public addresses reached loopback: %v", err)
	}
	if err := get(Confine(Confine(ctx, ScopePublic), ScopeLocal)); !errors.Is(err, ErrRefused) {
		t.Errorf("a second confinement widened the first: %v", err)
	}
	if err := get(Confine(ctx, ScopeLocal)); err != nil {
		t.Errorf("a request confined to the LAN could not reach loopback: %v", err)
	}
	if err := get(ctx); err != nil {
		t.Errorf("an unconfined request failed: %v", err)
	}
}
