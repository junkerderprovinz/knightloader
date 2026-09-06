package debrid

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The two services added on 2026-09-06, tested against a local server that
// answers exactly the bodies their vendors document. That is what these tests
// are for: not that Go can parse JSON, but that the field names, the envelope
// and the "which value means what" decisions match the published contract,
// since neither service can be reached from CI with a real key.

func serve(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("%s carried Authorization %q, want a bearer token", r.URL.Path, got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newDebridLinkAt(base string) *DebridLink {
	d := NewDebridLink("test-key")
	d.base = base
	return d
}

func newPremiumizeAt(base string) *Premiumize {
	p := NewPremiumize("test-key")
	p.base = base
	return p
}

// ---- Debrid-Link ----------------------------------------------------------

func TestDebridLinkHostsTakesOnlineFileHostsOnly(t *testing.T) {
	// "Host is online when the 'status' >= 1" - debrid-link.com/api_doc/v2.
	srv := serve(t, map[string]string{
		"/downloader/hosts": `{"success":true,"value":[
			{"name":"uploaded","type":"host","status":1,"domains":["uploaded.net","www.UL.to"]},
			{"name":"deadhost","type":"host","status":0,"domains":["deadhost.example"]},
			{"name":"rapidgator","type":"host","status":2,"domains":["rapidgator.net"]}]}`,
	})
	hosts, err := newDebridLinkAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"uploaded.net", "ul.to", "rapidgator.net"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	if hosts["deadhost.example"] {
		t.Error("a host with status 0 was taken as online")
	}
}

func TestDebridLinkUnlockReadsTheLinkObject(t *testing.T) {
	srv := serve(t, map[string]string{
		"/downloader/add": `{"success":true,"value":{"id":"ab03","name":"File.ext",
			"url":"http://host.ext/file/x","downloadUrl":"https://dl6.debrid.link/dl/abc","size":125002}}`,
	})
	got, err := newDebridLinkAt(srv.URL).Unlock(context.Background(), "http://host.ext/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl6.debrid.link/dl/abc" || got.Name != "File.ext" || got.Size != 125002 {
		t.Errorf("Unlock = %+v, want the downloadUrl/name/size from the answer", got)
	}
}

// TestDebridLinkUnlockReadsAFolderAnswer covers the one shape the documentation
// explicitly warns about: "it can be an sequential array when you send a folder
// that have multiple links". Decoding only the object would fail on exactly the
// links a folder-aware hoster produces.
func TestDebridLinkUnlockReadsAFolderAnswer(t *testing.T) {
	srv := serve(t, map[string]string{
		"/downloader/add": `{"success":true,"value":[
			{"name":"First.ext","downloadUrl":"https://dl6.debrid.link/dl/one","size":10},
			{"name":"Second.ext","downloadUrl":"https://dl6.debrid.link/dl/two","size":20}]}`,
	})
	got, err := newDebridLinkAt(srv.URL).Unlock(context.Background(), "http://host.ext/folder/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.Name != "First.ext" {
		t.Errorf("Unlock took %q from a folder answer, want the first entry", got.Name)
	}
}

// TestDebridLinkTranslatesItsErrorCodes: the API answers a CODE, never a
// sentence, and an interface that prints "badToken" at somebody is telling
// them nothing they can act on.
func TestDebridLinkTranslatesItsErrorCodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"success":false,"error":"badToken"}`)
	}))
	defer srv.Close()

	_, err := newDebridLinkAt(srv.URL).Unlock(context.Background(), "http://host.ext/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded against an error answer")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("error = %q, want it to name the API key rather than the raw code", err)
	}
}

func TestDebridLinkAccountReadsPremiumLeftAndUsage(t *testing.T) {
	srv := serve(t, map[string]string{
		"/account/infos":     `{"success":true,"value":{"username":"Amy","accountType":1,"premiumLeft":3628800,"pts":500}}`,
		"/downloader/limits": `{"success":true,"value":{"usagePercent":{"current":25,"value":100},"nextResetSeconds":{"current":-1,"value":3600}}}`,
	})
	info, err := newDebridLinkAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium - premiumLeft is 42 days", info.Tier)
	}
	// premiumLeft is a DURATION in seconds, not a timestamp. Reading it as one
	// would put the expiry in 1970 and the row would say "expired" about a
	// paid-up account.
	if d := time.Until(info.ExpiresAt); d < 41*24*time.Hour || d > 43*24*time.Hour {
		t.Errorf("ExpiresAt is %v away, want about 42 days", d)
	}
	if info.Traffic.UsedPercent != 25 {
		t.Errorf("UsedPercent = %v, want 25", info.Traffic.UsedPercent)
	}
	// Bytes stay untouched: the service quotes no byte ceiling, and inventing
	// one would put a figure on screen its own dashboard never shows.
	if info.Traffic.LimitBytes != 0 || info.Traffic.UsedBytes != 0 {
		t.Errorf("traffic bytes = %+v, want them left at zero", info.Traffic)
	}
}

// TestDebridLinkAccountSurvivesALimitsFailure: the plan is the answer, the
// allowance is the extra. A limits call that fails must not lose the tier and
// the expiry that were already read.
func TestDebridLinkAccountSurvivesALimitsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/downloader/limits" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"success":false,"error":"internalError"}`)
			return
		}
		_, _ = io.WriteString(w, `{"success":true,"value":{"premiumLeft":86400}}`)
	}))
	defer srv.Close()

	info, err := newDebridLinkAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium despite the limits call failing", info.Tier)
	}
}

// ---- Premiumize -----------------------------------------------------------

func TestPremiumizeHostsUsesDirectDLAndItsAliases(t *testing.T) {
	// cache is deliberately NOT taken: it is what Premiumize can answer from
	// its cloud, and Unlock speaks /transfer/directdl. Claiming a link this
	// backend cannot hand over would stop the lower-priority backends from
	// ever getting their turn at it.
	srv := serve(t, map[string]string{
		"/services/list": `{"status":"success","cache":["cloudonly.example"],
			"directdl":["uploaded","rapidgator.net"],
			"aliases":{"uploaded":["uploaded.net","ul.to"],"rapidgator.net":["rg.to"]}}`,
	})
	hosts, err := newPremiumizeAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"rapidgator.net", "rg.to", "uploaded.net", "ul.to"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	if hosts["cloudonly.example"] {
		t.Error("a cache-only service was claimed, but directdl is what Unlock uses")
	}
	// "uploaded" is a service NAME, not a domain, and must not become one - a
	// hostname with no dot in it can never match a real link.
	if hosts["uploaded"] {
		t.Error("the bare service name was taken as a domain")
	}
}

func TestPremiumizeUnlockTakesTheFirstFileAndItsBareName(t *testing.T) {
	srv := serve(t, map[string]string{
		"/transfer/directdl": `{"status":"success","content":[
			{"path":"Folder/Sub/video1.mkv","size":123456789,"link":"https://dl.example/one"}]}`,
	})
	got, err := newPremiumizeAt(srv.URL).Unlock(context.Background(), "http://host.ext/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.example/one" || got.Size != 123456789 {
		t.Errorf("Unlock = %+v, want the link and size from content[0]", got)
	}
	// path is slash-joined inside the source; a task named
	// "Folder/Sub/video1.mkv" becomes a folder tree when it reaches the disk.
	if got.Name != "video1.mkv" {
		t.Errorf("Name = %q, want just the file name", got.Name)
	}
}

// TestPremiumizeReadsStatusNotTheHTTPCode is the trap this API sets: "HTTP
// status is 200 for business-logic errors". Testing the status line would
// report every refusal as a success.
func TestPremiumizeReadsStatusNotTheHTTPCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"error","message":"not a premium account","code":"not_premium"}`)
	}))
	defer srv.Close()

	if _, err := newPremiumizeAt(srv.URL).Unlock(context.Background(), "http://host.ext/x"); err == nil {
		t.Fatal("Unlock reported success on a 200 that said status:error")
	} else if !strings.Contains(err.Error(), "not a premium account") {
		t.Errorf("error = %q, want the service's own message", err)
	}
}

func TestPremiumizeAccountReadsTheFairUseFraction(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour).Unix()
	body, _ := json.Marshal(map[string]any{
		"status": "success", "customer_id": "1234567",
		"premium_until": future, "limit_used": 0.42,
	})
	srv := serve(t, map[string]string{"/account/info": string(body)})

	info, err := newPremiumizeAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium", info.Tier)
	}
	// limit_used is a fraction in [0,1]; a reader that forgot to scale it would
	// report a 42%-used account as 0% and the column would read "100 % left".
	if info.Traffic.UsedPercent != 42 {
		t.Errorf("UsedPercent = %v, want 42", info.Traffic.UsedPercent)
	}
}

// TestPremiumizeFreeAccountHasNoExpiry: premium_until is null for a free
// account, and a plain int64 would read that as "expired in 1970" - a row
// claiming an expiry date about an account that has none.
func TestPremiumizeFreeAccountHasNoExpiry(t *testing.T) {
	srv := serve(t, map[string]string{
		"/account/info": `{"status":"success","premium_until":null,"limit_used":0}`,
	})
	info, err := newPremiumizeAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "free" {
		t.Errorf("Tier = %q, want free", info.Tier)
	}
	if !info.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want the zero time for an account with nothing to expire", info.ExpiresAt)
	}
}
