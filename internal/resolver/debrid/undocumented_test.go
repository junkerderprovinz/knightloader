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
