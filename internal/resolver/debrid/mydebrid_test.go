package debrid

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The bodies are the ones MyDebrid's API page prints, and the refusals the
// live service sends with HTTP 400.

const (
	mydebridTestUser  = "knight"
	mydebridTestPass  = "s3cret-pass"
	mydebridTestToken = "OVQc5DYQ"
)

func mydebridAt(base string) *MyDebrid {
	m := NewMyDebrid(mydebridTestUser, mydebridTestPass)
	m.base = base
	return m
}

// mydebridServer logs in with the test login and hands every other path to
// answer, after checking that the call is a form POST carrying the token and
// nothing in its query.
func mydebridServer(t *testing.T, logins *atomic.Int32, answer func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("%s %s, want a POST", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("%s Content-Type = %q, want a form", r.URL.Path, got)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("%s carried a query %q; secrets belong in the body", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/login" {
			if logins != nil {
				logins.Add(1)
			}
			if r.PostFormValue("username") != mydebridTestUser || r.PostFormValue("password") != mydebridTestPass {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"success":false,"error":"INVALID_CREDENTIALS"}`)
				return
			}
			_, _ = io.WriteString(w, `{"success":true,"token":"`+mydebridTestToken+`"}`)
			return
		}
		if got := r.PostFormValue("token"); got != mydebridTestToken {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"success":false,"error":"INVALID_TOKEN"}`)
			return
		}
		answer(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMyDebridUnlockLogsInAndReturnsTheDownloadURL(t *testing.T) {
	const link = "https://1fichier.com/?abc123"
	var logins atomic.Int32
	srv := mydebridServer(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/get-download-url" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		if got := r.PostFormValue("fileUrl"); got != link {
			t.Errorf("fileUrl = %q, want the hoster link", got)
		}
		_, _ = io.WriteString(w, `{"success":true,"name":"607229.jpeg","size":5376.0,
			"downloadUrl":"https://dl2.mydebrid.com/dl/azrjmg41hbv4r59399ma"}`)
	})
	m := mydebridAt(srv.URL)

	for range 2 {
		got, err := m.Unlock(context.Background(), link)
		if err != nil {
			t.Fatalf("Unlock: %v", err)
		}
		want := Direct{URL: "https://dl2.mydebrid.com/dl/azrjmg41hbv4r59399ma", Name: "607229.jpeg", Size: 5376}
		if got != want {
			t.Errorf("Unlock = %+v, want %+v", got, want)
		}
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("logged in %d times for two unlocks, want once", n)
	}
}

func TestMyDebridLogsInAgainWhenTheTokenExpires(t *testing.T) {
	var logins, unlocks atomic.Int32
	srv := mydebridServer(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		if unlocks.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"success":false,"error":"TOKEN_EXPIRED"}`)
			return
		}
		_, _ = io.WriteString(w, `{"success":true,"name":"a.zip","size":1,"downloadUrl":"https://dl2.mydebrid.com/dl/x"}`)
	})
	m := mydebridAt(srv.URL)

	got, err := m.Unlock(context.Background(), "https://1fichier.com/?abc123")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl2.mydebrid.com/dl/x" {
		t.Errorf("Unlock = %+v, want the link from the retry", got)
	}
	if logins.Load() != 2 || unlocks.Load() != 2 {
		t.Errorf("%d logins and %d unlocks, want one fresh login and one retry", logins.Load(), unlocks.Load())
	}
}

func TestMyDebridUnlockErrorsSayWhatWentWrong(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"hoster quota", `{"success":false,"error":"LIMIT_EXCEEDED"}`, "daily limit for this hoster is used up"},
		{"hoster down", `{"success":false,"error":"HOST_UNAVAILABLE"}`, "cannot reach this hoster right now"},
		{"dead link", `{"success":false,"error":"FILE_NOT_FOUND"}`, "link is probably dead"},
		{"bad link", `{"success":false,"error":"INVALID_URL"}`, "does not take this as a valid link"},
		{"missing field", `{"success":false,"error":"MISSING_PARAMS","error_details":"fileUrl"}`, "the request lacked fileUrl"},
		{"unknown code", `{"success":false,"error":"Error processing input"}`, "refused with Error processing input"},
		{"no reason", `{"success":false}`, "refused without a reason"},
		{"no link", `{"success":true,"name":"a.zip","size":1}`, "no direct link returned"},
	}
	seen := map[string]string{}
	for _, c := range cases {
		srv := mydebridServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(c.body, `"success":true`) {
				w.WriteHeader(http.StatusBadRequest)
			}
			_, _ = io.WriteString(w, c.body)
		})
		_, err := mydebridAt(srv.URL).Unlock(context.Background(), "https://1fichier.com/?abc123")
		if err == nil {
			t.Errorf("%s: Unlock succeeded", c.name)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, c.want) {
			t.Errorf("%s: error = %q, want it to contain %q", c.name, msg, c.want)
		}
		if strings.Contains(msg, mydebridTestPass) || strings.Contains(msg, mydebridTestToken) {
			t.Errorf("%s: a secret leaked into %q", c.name, msg)
		}
		if other, dup := seen[msg]; dup {
			t.Errorf("%s reads the same as %s: %q", c.name, other, msg)
		}
		seen[msg] = c.name
	}
}

func TestMyDebridHostsSkipsUsedUpHostsAndKeepsTheirChunkCaps(t *testing.T) {
	srv := mydebridServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/get-hosts" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"success":true,"hosts":[
			{"name":"1fichier.com","dailyLimit":5368709120,"remaining":5368709120,"maxChunks":3,"maxDownloads":5,"resumable":true},
			{"name":"alfafile.net","dailyLimit":"unlimited","remaining":"unlimited","maxChunks":3,"maxDownloads":5,"resumable":true},
			{"name":"WWW.Rapidgator.net","dailyLimit":"unlimited","remaining":"unlimited","maxChunks":8,"maxDownloads":5,"resumable":false},
			{"name":"katfile.com","dailyLimit":1073741824,"remaining":0,"maxChunks":3,"maxDownloads":5,"resumable":true},
			{"name":"broken","remaining":"unlimited"},
			"not an object"]}`)
	})
	m := mydebridAt(srv.URL)

	hosts, err := m.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"1fichier.com", "alfafile.net", "rapidgator.net"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	for _, not := range []string{"katfile.com", "broken"} {
		if hosts[not] {
			t.Errorf("Hosts claimed %q", not)
		}
	}
	limits := map[string]int{"1fichier.com": 3, "alfafile.net": 3, "rapidgator.net": 1, "nitroflare.com": 1}
	for host, want := range limits {
		if got := m.HostLimit(host); got != want {
			t.Errorf("HostLimit(%s) = %d, want %d", host, got, want)
		}
	}
}

func TestMyDebridHostsWithNothingListedIsAnError(t *testing.T) {
	srv := mydebridServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"hosts":[]}`)
	})
	if _, err := mydebridAt(srv.URL).Hosts(context.Background()); err == nil {
		t.Fatal("Hosts succeeded without a single host")
	}
}

func TestMyDebridAccountReadsPremiumAndItsExpiry(t *testing.T) {
	srv := mydebridServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/account-status" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"success":true,"userId":3,"accountType":"premium","remainingTraffic":"unlimited","expiryDate":"07-03-2020"}`)
	})

	info, err := mydebridAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" || !info.Traffic.Unlimited {
		t.Errorf("Account = %+v, want premium with unlimited traffic", info)
	}
	if want := time.Date(2020, 7, 3, 0, 0, 0, 0, time.UTC); !info.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v (month first)", info.ExpiresAt, want)
	}
}

func TestMyDebridAccountWithoutPremiumReadsFree(t *testing.T) {
	for _, body := range []string{
		`{"success":true,"accountType":"free","remainingTraffic":"0","expiryDate":""}`,
		`{"success":true,"accountType":"premium","remainingTraffic":"unlimited","expiryDate":"expired"}`,
	} {
		srv := mydebridServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		})
		info, err := mydebridAt(srv.URL).Account(context.Background())
		if err != nil {
			t.Fatalf("Account: %v", err)
		}
		if info.Tier != "free" || !info.ExpiresAt.IsZero() || info.Traffic.Unlimited {
			t.Errorf("Account from %s = %+v, want free without expiry or traffic", body, info)
		}
	}
}

func TestMyDebridAuthenticateReportsARefusedLogin(t *testing.T) {
	srv := mydebridServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a call went out after a refused login: %s", r.URL.Path)
	})
	m := NewMyDebrid(mydebridTestUser, "wrong-password")
	m.base = srv.URL

	err := m.Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "username or password was refused") {
		t.Fatalf("Authenticate = %v, want the refused login", err)
	}
	if strings.Contains(err.Error(), "wrong-password") {
		t.Error("the password leaked into the error")
	}
	if _, err := m.Unlock(context.Background(), "https://1fichier.com/?abc123"); err == nil {
		t.Error("Unlock succeeded with a refused login")
	}
}

func TestMyDebridAuthenticateWithoutALoginAsksForOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request went out without a login: %s", r.URL.Path)
	}))
	defer srv.Close()
	m := NewMyDebrid(mydebridTestUser, "")
	m.base = srv.URL

	if err := m.Authenticate(context.Background()); err == nil || !strings.Contains(err.Error(), "username and a password are required") {
		t.Fatalf("Authenticate = %v, want it to ask for both", err)
	}
}

// Cloudflare sits in front of the API, and its challenge page must not pass
// as a verified login.
func TestMyDebridRefusesAnAnswerThatIsNotJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "<html><title>Attention Required! | Cloudflare</title></html>")
	}))
	defer srv.Close()

	err := mydebridAt(srv.URL).Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unreadable answer (403 Forbidden)") {
		t.Fatalf("Authenticate = %v, want an unreadable answer", err)
	}
}

func TestMyDebridTransportErrorsCarryNoSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := srv.URL
	srv.Close()

	err := mydebridAt(base).Authenticate(context.Background())
	if err == nil {
		t.Fatal("Authenticate succeeded against a closed server")
	}
	if strings.Contains(err.Error(), mydebridTestPass) || strings.Contains(err.Error(), mydebridTestUser) {
		t.Errorf("a credential leaked into %q", err)
	}
}

func TestMyDebridBytesReadsNumbersFractionsAndStrings(t *testing.T) {
	cases := map[string]struct {
		n     int64
		sized bool
	}{
		`5376.0`:      {5376, true},
		`5368709120`:  {5368709120, true},
		`"1024"`:      {1024, true},
		`0`:           {0, true},
		`"unlimited"`: {0, false},
		`null`:        {0, false},
	}
	for raw, want := range cases {
		n, sized := mydebridBytes([]byte(raw))
		if n != want.n || sized != want.sized {
			t.Errorf("mydebridBytes(%s) = %d, %v, want %d, %v", raw, n, sized, want.n, want.sized)
		}
	}
}
