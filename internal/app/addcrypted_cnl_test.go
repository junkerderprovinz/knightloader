package app

// AddContainerCnL is Click'n'Load v1 ("addcrypted")'s entrance: the CnL
// listener hands over the raw "crypted" field, and only a backend that
// implements cryptedV1Adder (the shipped JD) can make anything of it. These
// tests pin the wiring — refusal with no backend, the bytes and package name
// the backend actually receives, and that a harvested link lands back in the
// list tagged OriginCnL, same as any other Click'n'Load submission.

import (
	"errors"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// stubCryptedV1Backend is a backend implementing addcrypted v1 support on top
// of the base download contract every backend needs.
type stubCryptedV1Backend struct {
	gotData []byte
	gotPkg  string
	links   []resolver.Result
	err     error
}

func (s *stubCryptedV1Backend) Download(string, string, map[string]string, int) {}
func (s *stubCryptedV1Backend) Pause(string)                                    {}
func (s *stubCryptedV1Backend) Resume(string)                                   {}
func (s *stubCryptedV1Backend) Remove(string, bool)                             {}

func (s *stubCryptedV1Backend) AddCryptedV1(data []byte, packageName string, _ time.Duration) ([]resolver.Result, error) {
	s.gotData, s.gotPkg = data, packageName
	return s.links, s.err
}

// TestAddContainerCnLWithoutBackendRefuses is the same refusal
// HandContainerToJD gives an uploaded .dlc when no JD is configured: an
// instance with no JD backend must say so plainly rather than claim success
// for a submission that goes nowhere.
func TestAddContainerCnLWithoutBackendRefuses(t *testing.T) {
	a := newCrawlApp(t, false)
	if a.CryptedV1BackendConfigured() {
		t.Fatal("CryptedV1BackendConfigured() = true with no backend wired at all")
	}
	err := a.AddContainerCnL([]byte("payload"), "pkg")
	if !errors.Is(err, ErrNoContainerBackend) {
		t.Fatalf("err = %v, want ErrNoContainerBackend", err)
	}
}

// TestAddContainerCnLRefusesEmptyContent guards against a site (or a bug
// upstream of this call) submitting nothing: a "crypted" field is meaningless
// empty, and going through to the backend anyway would report a confusing
// failure JD invents rather than the honest one this app already knows.
func TestAddContainerCnLRefusesEmptyContent(t *testing.T) {
	a := newCrawlApp(t, false)
	a.bmu.Lock()
	a.jd = &stubCryptedV1Backend{links: []resolver.Result{{DirectURL: "https://host.example/x"}}}
	a.bmu.Unlock()
	if err := a.AddContainerCnL(nil, "pkg"); err == nil {
		t.Fatal("AddContainerCnL(nil, ...) returned no error")
	}
}

// TestAddContainerCnLStagesHarvestedLinksAsCnLOrigin drives the success path:
// the backend receives exactly the bytes and package name the submission
// carried, and what it hands back is staged through the ordinary intake path
// (AddResolvedLinksFrom) tagged OriginCnL — the same entrance a plain
// /flash/add or /flash/addcrypted2 submission uses, because from the
// collector's point of view all three are "Click'n'Load", not three
// different sources.
//
// The harvested link's name and size are asserted here too, and the URL is
// deliberately extensionless so resolver.Direct (which would derive its own
// name from the path) does not claim it - jd.Resolver is registered and does
// instead, and jd's own Resolve answers with the URL as a placeholder Name
// and no Size (see its own doc comment). Without stage()'s guard against that
// placeholder overwriting a real hint, this test catches exactly the bug a
// DLC's crawled name and size were disappearing to: the harvest already knew
// both, and the ordinary intake path was throwing them away only to wait for
// JD to crawl the same link a second time, at download time, to learn them
// again.
func TestAddContainerCnLStagesHarvestedLinksAsCnLOrigin(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	const harvestedURL = "https://host.example/dl/harvested"
	stub := &stubCryptedV1Backend{links: []resolver.Result{
		{DirectURL: harvestedURL, Name: "Harvested File.bin", Size: 123456},
	}}
	a.bmu.Lock()
	a.jd = stub
	a.bmu.Unlock()

	if !a.CryptedV1BackendConfigured() {
		t.Fatal("CryptedV1BackendConfigured() = false with a backend that implements it")
	}
	payload := []byte("rsa-encrypted-stand-in")
	if err := a.AddContainerCnL(payload, "MyPackage"); err != nil {
		t.Fatalf("AddContainerCnL: %v", err)
	}

	waitFor(t, "the harvested link reaching the list", func() bool {
		return len(a.Tasks()) == 1
	})
	created := a.Tasks()
	if created[0].Origin != OriginCnL {
		t.Errorf("origin = %q, want %q", created[0].Origin, OriginCnL)
	}
	if created[0].URL != harvestedURL {
		t.Errorf("url = %q, want the harvested link", created[0].URL)
	}
	if created[0].Resolver != "jd" {
		t.Fatalf("test fixture broken: resolver = %q, want %q (jd's own placeholder Name is the case this test pins)", created[0].Resolver, "jd")
	}
	if created[0].Name != "Harvested File.bin" {
		t.Errorf("name = %q, want the name the harvest already knew, not jd's URL placeholder", created[0].Name)
	}
	if created[0].Size != 123456 {
		t.Errorf("size = %d, want the size the harvest already knew", created[0].Size)
	}

	if string(stub.gotData) != string(payload) {
		t.Errorf("backend received %q, want the original payload %q", stub.gotData, payload)
	}
	if stub.gotPkg != "MyPackage" {
		t.Errorf("backend received package %q, want MyPackage", stub.gotPkg)
	}
}

// TestAddContainerCnLRecordsABackendFailure pins the other branch: a backend
// that fails (JD could not make sense of the payload) must not vanish
// silently — it belongs in the same skipped trace an uploaded container's
// failure already uses, or the user is left staring at an unchanged list with
// nothing to explain it.
func TestAddContainerCnLRecordsABackendFailure(t *testing.T) {
	a := newCrawlApp(t, false)
	a.bmu.Lock()
	a.jd = &stubCryptedV1Backend{err: errors.New("jd opened the container but produced no links")}
	a.bmu.Unlock()

	if err := a.AddContainerCnL([]byte("payload"), "pkg"); err != nil {
		t.Fatalf("AddContainerCnL returned a synchronous error for an async failure: %v", err)
	}
	waitFor(t, "the failure reaching the skipped trace", func() bool {
		return len(a.SkippedLinks()) == 1
	})
	if got := a.SkippedLinks()[0].Reason; got == "" {
		t.Error("skipped reason is empty")
	}
	if len(a.Tasks()) != 0 {
		t.Errorf("tasks = %d, want 0 — a backend failure must not stage anything", len(a.Tasks()))
	}
}
