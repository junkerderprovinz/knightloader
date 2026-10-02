package debrid

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Linksnappy and Offcloud publish no complete API reference, so besides the
// expected shapes these tests check that an unrecognised answer fails safely.

func newLinksnappyAt(base string) *Linksnappy {
	l := NewLinksnappy("u", "p")
	l.base = base
	return l
}

func newOffcloudAt(base string) *Offcloud {
	o := NewOffcloud("test-key")
	o.base = base
	return o
}

// The body is the one the live service answers, with Status as a string.
func TestLinksnappyHostsReadsTheMeasuredShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"OK","error":false,"return":{
			"filefactory.com":{"Status":"1","Quota":"unlimited","connlimit":"32"},
			"rapidgator.net":{"Status":"1","Quota":"unlimited"},
			"deadhost.example":{"Status":"0","Quota":"unlimited"}}}`)
	}))
	defer srv.Close()

	hosts, err := newLinksnappyAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["rapidgator.net"] || !hosts["filefactory.com"] {
		t.Errorf("Hosts is missing a live host: %v", hosts)
	}
	if hosts["deadhost.example"] {
		t.Error("a host the service reports as down was taken as up")
	}
}

// The live list names Mega as mega.co.nz and gives rapidgator.net its alias in
// a field of its own, while links use mega.nz, rg.to and k2s.cc.
func TestLinksnappyClaimsLinksUnderAHostersOtherDomains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"OK","error":false,"return":{
			"rapidgator.net":{"Status":"1","Quota":"unlimited","alias":["rg.to"]},
			"mega.co.nz":{"Status":"1","Quota":"unlimited"},
			"keep2share.cc":{"Status":"1","Quota":16106127360}}}`)
	}))
	defer srv.Close()

	hosts, err := newLinksnappyAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["rg.to"] {
		t.Errorf("Hosts left out the alias the service lists: %v", hosts)
	}
	r := Resolver{ServiceID: "linksnappy", Hosts: hosts}
	for _, link := range []string{
		"https://rg.to/file/0a1b2c/part1.rar.html",
		"https://mega.nz/file/AbCdEf#key",
		"https://k2s.cc/file/0a1b2c/part2.rar",
	} {
		if !r.Match(link) {
			t.Errorf("Linksnappy does not claim %s", link)
		}
	}
}

// JDownloader asks for the host list with the session AUTHENTICATE sets, and
// reads canDownload from it, a field the list without a session lacks.
func TestLinksnappyHostsAsksWithTheSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/AUTHENTICATE") {
			http.SetCookie(w, &http.Cookie{Name: "lslogin", Value: "s1", Path: "/"})
			_, _ = io.WriteString(w, `{"status":"OK","error":false,"return":"Logged in"}`)
			return
		}
		if c, err := r.Cookie("lslogin"); err != nil || c.Value != "s1" {
			_, _ = io.WriteString(w, `{"status":"OK","error":false,"return":{
				"rapidgator.net":{"Status":"1"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"OK","error":false,"return":{
			"rapidgator.net":{"Status":"1","canDownload":1,"Usage":0},
			"nitroflare.com":{"Status":"1","canDownload":1,"Usage":0},
			"filenext.com":{"Status":"1","canDownload":0,"Usage":0}}}`)
	}))
	defer srv.Close()

	hosts, err := newLinksnappyAt(srv.URL + "/api").Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["nitroflare.com"] {
		t.Errorf("Hosts = %v, want the list the logged-in session gets", hosts)
	}
	if hosts["filenext.com"] {
		t.Error("a host the account cannot download from was taken")
	}
}

// "error" is false on success and a sentence on failure.
func TestLinksnappyRefusalIsASentenceNotABool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ERROR","error":"Invalid Username"}`)
	}))
	defer srv.Close()

	err := newLinksnappyAt(srv.URL).Authenticate(context.Background())
	if err == nil {
		t.Fatal("Authenticate succeeded against an error answer")
	}
	if !strings.Contains(err.Error(), "Invalid Username") {
		t.Errorf("error = %q, want the service's own sentence", err)
	}
}

func TestLinksnappyUnlockReadsTheLinksArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/AUTHENTICATE") {
			_, _ = io.WriteString(w, `{"status":"OK","error":false,"return":{}}`)
			return
		}
		// linkgen answers a bare object without the envelope, with size as a
		// string.
		_, _ = io.WriteString(w, `{"links":[{"status":"OK","error":false,
			"generated":"https://dl.example/one","filename":"File.ext","filehost":"rapidgator.net","size":"125002"}]}`)
	}))
	defer srv.Close()

	got, err := newLinksnappyAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.example/one" || got.Name != "File.ext" || got.Size != 125002 {
		t.Errorf("Unlock = %+v, want the generated link, its name and 125002 bytes", got)
	}
}

// The call succeeds but the entry inside it carries the failure.
func TestLinksnappyUnlockReportsAPerLinkRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/AUTHENTICATE") {
			_, _ = io.WriteString(w, `{"status":"OK","error":false}`)
			return
		}
		_, _ = io.WriteString(w, `{"links":[{"status":"ERROR","error":"File not found"}]}`)
	}))
	defer srv.Close()

	if _, err := newLinksnappyAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x"); err == nil {
		t.Fatal("Unlock reported success for a link the service refused")
	} else if !strings.Contains(err.Error(), "File not found") {
		t.Errorf("error = %q, want the entry's own message", err)
	}
}

func TestOffcloudSitesAcceptsThreeShapesAndRefusesNonsense(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"flat array", `["rapidgator.net","uploaded.net"]`, "rapidgator.net"},
		{"array of objects", `[{"name":"rapidgator.net","id":"7"},{"name":"uploaded.net"}]`, "rapidgator.net"},
		{"grouped by category", `{"hosters":["rapidgator.net"],"video":["youtube.com"]}`, "rapidgator.net"},
	}
	for _, c := range cases {
		got := parseSites(json.RawMessage(c.body))
		if !got[c.want] {
			t.Errorf("%s: %q missing from %v", c.name, c.want, got)
		}
	}
	// Category labels and counts are not hosts.
	noise := parseSites(json.RawMessage(`{"categories":["video","hosters"],"count":["12"]}`))
	if len(noise) != 0 {
		t.Errorf("non-domain strings were taken as hosts: %v", noise)
	}
	if len(parseSites(json.RawMessage(`{"totally":{"unexpected":true}}`))) != 0 {
		t.Error("an unrecognised shape produced hosts out of nothing")
	}
}

func TestOffcloudReportsTheAddOnItIsMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"not_available":"premium"}`)
	}))
	defer srv.Close()

	_, err := newOffcloudAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded against a not_available answer")
	}
	if !strings.Contains(err.Error(), "premium add-on") {
		t.Errorf("error = %q, want it to name the missing add-on", err)
	}
}

func TestOffcloudUnlockReadsTheInstantAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("key"); got != "test-key" {
			t.Errorf("key parameter = %q, want the configured key", got)
		}
		_, _ = io.WriteString(w, `{"requestId":"abc","fileName":"File.ext",
			"url":"https://dl.example/one","site":"rapidgator","status":"created"}`)
	}))
	defer srv.Close()

	got, err := newOffcloudAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.example/one" || got.Name != "File.ext" {
		t.Errorf("Unlock = %+v, want the instant link and its file name", got)
	}
}

func TestOffcloudRefusedKeyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"NOAUTH"}`)
	}))
	defer srv.Close()

	if _, err := newOffcloudAt(srv.URL).Hosts(context.Background()); err == nil {
		t.Fatal("Hosts succeeded against a 401")
	}
}

// Both services take the credential in the query string, and a transport
// error quotes the URL. The error reaches the task row, account_health.json and
// the log, so the credential has to be gone from it.
func TestLinksnappyAndOffcloudTransportErrorsCarryNoCredential(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	l := NewLinksnappy("alice", "hunter2-secret")
	l.base = base
	if err := l.Authenticate(context.Background()); err == nil {
		t.Error("Linksnappy authenticated against a closed server")
	} else if strings.Contains(err.Error(), "hunter2-secret") {
		t.Errorf("Linksnappy error = %q, which gives the password away", err)
	}

	o := NewOffcloud("offcloud-secret-key")
	o.base = base
	if _, err := o.Hosts(context.Background()); err == nil {
		t.Error("Offcloud answered from a closed server")
	} else if strings.Contains(err.Error(), "offcloud-secret-key") {
		t.Errorf("Offcloud error = %q, which gives the key away", err)
	}
}
