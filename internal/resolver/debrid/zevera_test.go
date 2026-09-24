package debrid

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These tests answer with the bodies the Zevera spec documents and the
// refusals JDownloader's ZeveraCore matches.

// zeveraTestKey has the shape JDownloader checks a Zevera key against.
const zeveraTestKey = "0123456789abcdef"

func zeveraAt(base string) *Zevera {
	z := NewZevera(zeveraTestKey)
	z.base = base
	return z
}

// zeveraServe answers each path with its body and fails the test for a
// request that does not carry the key as the apikey query parameter.
func zeveraServe(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("apikey") != zeveraTestKey {
			t.Errorf("%s went out without the key in apikey", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestZeveraHostsTakesDirectDLAndItsAliases(t *testing.T) {
	srv := zeveraServe(t, map[string]string{
		"/services/list": `{"directdl":["rapidgator.net","uploaded.net","usenet"],
			"cache":["cloudonly.example"],
			"queue":["queued.example"],
			"fairusefactor":{"rapidgator.net":4},
			"aliases":{"uploaded.net":["uploaded.net","uploaded.to","www.UL.to"],"rapidgator.net":["rg.to"],
				"cloudonly.example":["cloudonly.example","co.example"]}}`,
	})
	hosts, err := zeveraAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"rapidgator.net", "rg.to", "uploaded.net", "uploaded.to", "ul.to"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	// Unlock cannot serve either list right away: cache only works for files
	// Zevera already holds, queue answers "deferred".
	for _, gone := range []string{"cloudonly.example", "co.example", "queued.example"} {
		if hosts[gone] {
			t.Errorf("Hosts claimed %q, which directdl does not list", gone)
		}
	}
	if hosts["usenet"] {
		t.Error("a bare service name was taken as a domain")
	}
}

func TestZeveraHostsSurviveAnAliasTableOfAnotherShape(t *testing.T) {
	srv := zeveraServe(t, map[string]string{
		"/services/list": `{"directdl":["rapidgator.net"],"cache":[],"aliases":[]}`,
	})
	hosts, err := zeveraAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["rapidgator.net"] {
		t.Errorf("Hosts = %v, want the directdl host despite the odd alias table", hosts)
	}
}

func TestZeveraUnlockPostsTheLinkAndReadsContent(t *testing.T) {
	const link = "https://rapidgator.net/file/x"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/transfer/directdl" || r.PostFormValue("src") != link {
			t.Errorf("got %s %s with src %q, want a POST to /transfer/directdl carrying the link",
				r.Method, r.URL.Path, r.PostFormValue("src"))
		}
		if r.URL.Query().Get("apikey") != zeveraTestKey {
			t.Error("the POST went out without the key in apikey")
		}
		// The legacy fields describe the same file; content is what the spec
		// says to read.
		_, _ = io.WriteString(w, `{"status":"success",
			"location":"https://legacy.example/one","filename":"legacy.bin","filesize":1,
			"content":[{"path":"Folder/Sub/video1.mkv","size":"123456789",
				"link":"https://dl.example/one","stream_link":"","transcode_status":"finished"}]}`)
	}))
	defer srv.Close()

	got, err := zeveraAt(srv.URL).Unlock(context.Background(), link)
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.example/one" || got.Size != 123456789 {
		t.Errorf("Unlock = %+v, want the link and size from content[0]", got)
	}
	if got.Name != "video1.mkv" {
		t.Errorf("Name = %q, want just the file name", got.Name)
	}
}

func TestZeveraUnlockFallsBackToTheSingleFileFields(t *testing.T) {
	srv := zeveraServe(t, map[string]string{
		"/transfer/directdl": `{"status":"success","location":"https://dl.example/one",
			"filename":"File.ext","filesize":125002,"content":[]}`,
	})
	got, err := zeveraAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.example/one" || got.Name != "File.ext" || got.Size != 125002 {
		t.Errorf("Unlock = %+v, want location, filename and filesize", got)
	}
}

func TestZeveraUnlockNamesTheKindOfRefusal(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		kind  string
		words string
	}{
		{"dead link", `{"status":"error","message":"Error: file not found"}`,
			"the hoster says this file is gone", "Error: file not found"},
		{"dead link by code", `{"status":"error","code":"not_found","message":"The file was removed"}`,
			"the hoster says this file is gone", "The file was removed"},
		{"dead link in the JSON-RPC shape", `{"jsonrpc":"2.0","id":1,"error":{"code":0,"message":"File not found or not your file"}}`,
			"the hoster says this file is gone", "File not found or not your file"},
		{"unsupported host", `{"status":"error","code":"service_unsupported","message":"Hoster not supported"}`,
			"Zevera does not support this hoster", "Hoster not supported"},
		{"host down", `{"status":"error","code":"service_down"}`,
			"this hoster is down at Zevera", "service_down"},
		{"fair use spent", `{"status":"error","code":"account_limit_reached","message":"Fair use limit reached"}`,
			"no fair-use allowance left", "Fair use limit reached"},
		{"no premium", `{"status":"error","error":"topup_required","message":"Please purchase premium membership or activate free mode."}`,
			"no fair-use allowance left", "Please purchase premium membership"},
		{"daily link limit", `{"status":"error","message":"Daily linklimit for this service reached."}`,
			"the daily link limit for this hoster is used up", "Daily linklimit for this service reached."},
		{"refused credential", `{"status":"error","message":"customer_id and pin parameter missing or not logged in"}`,
			"Zevera refused the API key", "customer_id and pin parameter missing"},
		{"account not allowed", `{"status":"error","code":"permission_denied","message":"Account not confirmed"}`,
			"Zevera does not let this account do that", "Account not confirmed"},
		{"rate limited", `{"status":"error","code":"rate_limit_reached"}`,
			"Zevera is rate limiting this account", "rate_limit_reached"},
		{"unknown refusal", `{"status":"error","message":"Something unexpected"}`,
			"zevera /transfer/directdl", "Something unexpected"},
		{"no reason", `{"status":"error"}`,
			"Zevera named no reason", ""},
		{"JSON-RPC error without a message", `{"jsonrpc":"2.0","id":1,"error":{"code":0}}`,
			"Zevera named no reason", ""},
		{"file still being fetched", `{"status":"deferred","delay":30}`,
			"has not finished fetching this file", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := zeveraServe(t, map[string]string{"/transfer/directdl": c.body})
			_, err := zeveraAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
			if err == nil {
				t.Fatal("Unlock succeeded against a refusal")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "zevera ") || !strings.Contains(msg, c.kind) || !strings.Contains(msg, c.words) {
				t.Errorf("error = %q, want it to name Zevera, say %q and carry %q", msg, c.kind, c.words)
			}
		})
	}
}

func TestZeveraErrorsNeverCarryTheCredentials(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	_, err := zeveraAt(base).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded against a closed server")
	}
	if strings.Contains(err.Error(), zeveraTestKey) {
		t.Errorf("error = %q, which gives the key away", err)
	}
}

func TestZeveraAuthenticateRejectsARefusedKey(t *testing.T) {
	refusal := `{"status":"error","message":"customer_id and pin parameter missing or not logged in"}`
	srv := zeveraServe(t, map[string]string{"/account/info": refusal, "/services/list": refusal})
	z := zeveraAt(srv.URL)

	if err := z.Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate accepted a refused key")
	} else if !strings.Contains(err.Error(), "refused the API key") {
		t.Errorf("error = %q, want it to point at the API key", err)
	}
	if hosts, err := z.Hosts(context.Background()); err == nil {
		t.Errorf("Hosts made %v out of a refusal", hosts)
	}
}

// The JSON-RPC shape has no status, and its error object need not carry a
// message, so the object itself has to count as the refusal.
func TestZeveraAuthenticateRejectsAnErrorObjectWithoutAMessage(t *testing.T) {
	srv := zeveraServe(t, map[string]string{
		"/account/info": `{"jsonrpc":"2.0","id":1,"error":{"code":0}}`,
	})
	if err := zeveraAt(srv.URL).Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate accepted an answer that carried an error object")
	}
}

func TestZeveraAuthenticateAcceptsAWorkingKey(t *testing.T) {
	srv := zeveraServe(t, map[string]string{
		"/account/info": `{"status":"success","customer_id":1234567,"premium_until":false,"limit_used":0,"space_used":0}`,
	})
	if err := zeveraAt(srv.URL).Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestZeveraAuthenticateNeedsAKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Authenticate asked Zevera without a key")
	}))
	defer srv.Close()

	z := NewZevera("")
	z.base = srv.URL
	if err := z.Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate accepted an empty key")
	}
}

func TestZeveraAccountReadsPremiumAndTheFairUseFraction(t *testing.T) {
	until := time.Now().Add(30 * 24 * time.Hour).Unix()
	srv := zeveraServe(t, map[string]string{
		"/account/info": fmt.Sprintf(`{"status":"success","customer_id":1234567,
			"premium_until":%d,"limit_used":0.25,"space_used":1024}`, until),
	})
	info, err := zeveraAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" || info.ExpiresAt.Unix() != until {
		t.Errorf("Account = %+v, want premium until %d", info, until)
	}
	// limit_used is a fraction of the fair-use allowance, not bytes.
	if !info.Traffic.PercentKnown || info.Traffic.UsedPercent != 25 {
		t.Errorf("Traffic = %+v, want 25 percent used", info.Traffic)
	}
	if info.Traffic.Unlimited {
		t.Error("Traffic reads unlimited, which would hide the fair-use figure")
	}
}

func TestZeveraAccountAcceptsNumbersSentAsStrings(t *testing.T) {
	until := time.Now().Add(24 * time.Hour).Unix()
	srv := zeveraServe(t, map[string]string{
		"/account/info": fmt.Sprintf(`{"status":"success","premium_until":"%d","limit_used":"0.5"}`, until),
	})
	info, err := zeveraAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" || info.Traffic.UsedPercent != 50 {
		t.Errorf("Account = %+v, want premium with 50 percent used", info)
	}
}

func TestZeveraAccountWithoutRunningPremiumIsFree(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour).Unix()
	cases := []struct {
		name   string
		until  string
		expiry bool
	}{
		{"premium_until false", "false", false},
		{"premium_until null", "null", false},
		{"premium ran out", fmt.Sprint(past), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := zeveraServe(t, map[string]string{
				"/account/info": `{"status":"success","customer_id":1234567,"premium_until":` + c.until + `,"limit_used":0}`,
			})
			info, err := zeveraAt(srv.URL).Account(context.Background())
			if err != nil {
				t.Fatalf("Account: %v", err)
			}
			if info.Tier != "free" {
				t.Errorf("Tier = %q, want free", info.Tier)
			}
			if info.ExpiresAt.IsZero() == c.expiry {
				t.Errorf("ExpiresAt = %v, want it set only when premium ran out", info.ExpiresAt)
			}
		})
	}
}

// A body without a reason must not pass for a free account just because it
// decodes.
func TestZeveraAccountFailsOnAServerErrorWithoutAReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	if info, err := zeveraAt(srv.URL).Account(context.Background()); err == nil {
		t.Errorf("Account = %+v from a 502", info)
	}
}
