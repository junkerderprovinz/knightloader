package app

// AddContainerCnL is the entrance for Click'n'Load v1 ("addcrypted"): only a
// backend implementing cryptedV1Adder (JD) can decode the "crypted" field.
// These tests cover the refusal without a backend, what the backend receives,
// and that harvested links are staged as OriginCnL.

import (
	"errors"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// stubCryptedV1Backend is a backend with addcrypted v1 support.
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

// Without a JD backend the submission is refused, as HandContainerToJD refuses
// an uploaded .dlc.
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

// An empty "crypted" field is refused here rather than passed to JD.
func TestAddContainerCnLRefusesEmptyContent(t *testing.T) {
	a := newCrawlApp(t, false)
	a.bmu.Lock()
	a.jd = &stubCryptedV1Backend{links: []resolver.Result{{DirectURL: "https://host.example/x"}}}
	a.bmu.Unlock()
	if err := a.AddContainerCnL(nil, "pkg"); err == nil {
		t.Fatal("AddContainerCnL(nil, ...) returned no error")
	}
}

// The backend receives the submitted bytes and package name, and what it hands
// back is staged through AddResolvedLinksFrom as OriginCnL, like any other
// Click'n'Load submission.
//
// The URL has no extension so jd.Resolver claims it rather than
// resolver.Direct. jd answers with the URL as a placeholder name and no size,
// which must not replace the name and size the harvest already knew.
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

// A backend failure is recorded in the skipped trace, as for an uploaded
// container, rather than leaving the list unchanged without a word.
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
		t.Errorf("tasks = %d, want 0; a backend failure must not stage anything", len(a.Tasks()))
	}
}
