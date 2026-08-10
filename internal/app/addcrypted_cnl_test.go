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
)

// stubCryptedV1Backend is a backend implementing addcrypted v1 support on top
// of the base download contract every backend needs.
type stubCryptedV1Backend struct {
	gotData []byte
	gotPkg  string
	urls    []string
	err     error
}

func (s *stubCryptedV1Backend) Download(string, string, map[string]string, int) {}
func (s *stubCryptedV1Backend) Pause(string)                                    {}
func (s *stubCryptedV1Backend) Resume(string)                                   {}
func (s *stubCryptedV1Backend) Remove(string, bool)                             {}

func (s *stubCryptedV1Backend) AddCryptedV1(data []byte, packageName string, _ time.Duration) ([]string, error) {
	s.gotData, s.gotPkg = data, packageName
	return s.urls, s.err
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
	a.jd = &stubCryptedV1Backend{urls: []string{"https://host.example/x"}}
	if err := a.AddContainerCnL(nil, "pkg"); err == nil {
		t.Fatal("AddContainerCnL(nil, ...) returned no error")
	}
}

// TestAddContainerCnLStagesHarvestedLinksAsCnLOrigin drives the success path:
// the backend receives exactly the bytes and package name the submission
// carried, and what it hands back is staged through the ordinary intake path
// (AddLinksFrom) tagged OriginCnL — the same entrance a plain /flash/add or
// /flash/addcrypted2 submission uses, because from the collector's point of
// view all three are "Click'n'Load", not three different sources.
func TestAddContainerCnLStagesHarvestedLinksAsCnLOrigin(t *testing.T) {
	a := newCrawlApp(t, false)
	stub := &stubCryptedV1Backend{urls: []string{"https://host.example/harvested.bin"}}
	a.jd = stub

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
	if created[0].URL != "https://host.example/harvested.bin" {
		t.Errorf("url = %q, want the harvested link", created[0].URL)
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
	a.jd = &stubCryptedV1Backend{err: errors.New("jd opened the container but produced no links")}

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
