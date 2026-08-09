package engine

import (
	"testing"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
)

// TestAnUnroutedDownloadFollowsTheGlobalProxy is the trap this mapping exists to
// avoid, and it is worth a test rather than a comment because both answers look
// correct.
//
// nil means "follow the global config", which is the loopback proxy the speed
// limit lives in. The mode that reads like the right one for an unproxied
// download - RequestProxyModeNone - means no proxy handler at all, so it would
// take the download off the meter as well. The bug that follows is a speed limit
// that silently does nothing, on the downloads somebody was most deliberate
// about.
func TestAnUnroutedDownloadFollowsTheGlobalProxy(t *testing.T) {
	for _, name := range []string{"no route at all", "the direct gateway"} {
		r := proxycfg.Route{}
		if name == "the direct gateway" {
			var err error
			r, err = proxycfg.Direct().Route()
			if err != nil {
				t.Fatalf("the direct gateway has no route: %v", err)
			}
		}
		if got := requestProxy(r); got != nil {
			t.Fatalf("%s produced %+v, want nil so the loopback meter still applies", name, got)
		}
	}
}

// TestARoutedDownloadNamesItsOwnProxy. Custom is the only mode gopeed resolves
// in favour of the request; follow and none both hand the download back to the
// global config, which would mean the connection the user picked was ignored
// with nothing to say so.
func TestARoutedDownloadNamesItsOwnProxy(t *testing.T) {
	e := proxycfg.Entry{ID: "3", Kind: proxycfg.KindSOCKS5, Host: "proxy.lan", Port: 1080, Username: "alice", Password: "s3cret", Enabled: true}
	r, err := e.Route()
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	got := requestProxy(r)
	if got == nil {
		t.Fatal("a routed download produced no request proxy")
	}
	want := base.RequestProxy{
		Mode:   base.RequestProxyModeCustom,
		Scheme: "socks5",
		Host:   "proxy.lan:1080",
		Usr:    "alice",
		Pwd:    "s3cret",
	}
	if *got != want {
		t.Fatalf("requestProxy = %+v, want %+v", *got, want)
	}
	// The handler is what gopeed actually calls; a mode or a field it does not
	// like produces a nil one and the download quietly goes out unproxied.
	if got.ToHandler() == nil {
		t.Fatal("gopeed built no proxy handler from this route, so the download would go out unproxied")
	}
}
