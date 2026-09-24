package debrid

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// megadebridServe points a client with the credential amy / secret-pass at a
// fake api.php that hands each request to answer by its action.
func megadebridServe(t *testing.T, answer func(w http.ResponseWriter, r *http.Request, action string)) *MegaDebrid {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.php" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		answer(w, r, r.URL.Query().Get("action"))
	}))
	t.Cleanup(srv.Close)
	m := NewMegaDebrid("amy", "secret-pass")
	m.base = srv.URL + "/api.php"
	return m
}

// megadebridLogin answers connectUser with the documented fields, vip_end as a
// string as JDownloader expects it.
func megadebridLogin(w io.Writer, token string, vipEnd int64) {
	_, _ = fmt.Fprintf(w, `{"response_code":"ok","response_text":"User logged","token":%q,"vip_end":"%d","email":"amy@example.com"}`, token, vipEnd)
}

var megadebridFuture = time.Now().Add(30 * 24 * time.Hour).Unix()

// The entries follow the live answer, which adds url and type to the
// documented name, status, img, domains and regexps.
func TestMegaDebridHostsKeepsLiveFileHostersAndTheirAliases(t *testing.T) {
	m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
		if action != "getHostersList" {
			t.Errorf("Hosts called %q", action)
		}
		if r.URL.Query().Has("token") || r.URL.Query().Has("login") {
			t.Error("Hosts sent account data to a public endpoint")
		}
		_, _ = io.WriteString(w, `{"response_code":"ok","response_text":"","hosters":[
			{"name":"rapidgator","url":"Rapidgator","img":"https://cdn.example/rapidgator.png",
			 "domains":["rapidgator.net","rg.to"],"status":"up",
			 "regexps":["#http[s]*?://[www\\.]*?rapidgator\\.net/(.*?)$#msi"],"type":"hoster"},
			{"name":"alfafile","url":"Alfafile","domains":["www.Alfafile.net"],"status":"up","type":"hoster"},
			{"name":"deadhost","domains":["deadhost.example"],"status":"down","type":"hoster"},
			{"name":"archive","domains":["archive.org"],"status":"up","type":"stream"},
			{"name":"nostatus","domains":["nostatus.example"]},
			{"name":"broken","domains":"broken.example","status":"up"}]}`)
	})

	hosts, err := m.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"rapidgator.net", "rg.to", "alfafile.net", "nostatus.example"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	for _, unwanted := range []string{"deadhost.example", "archive.org", "rapidgator", "broken.example"} {
		if hosts[unwanted] {
			t.Errorf("Hosts claimed %q: %v", unwanted, hosts)
		}
	}
}

func TestMegaDebridHostsFailsOnAnEmptyList(t *testing.T) {
	m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
		_, _ = io.WriteString(w, `{"response_code":"ok","response_text":"","hosters":[]}`)
	})
	if _, err := m.Hosts(context.Background()); err == nil {
		t.Fatal("Hosts succeeded with no hosters, which would empty the routing table")
	}
}

func TestMegaDebridUnlockLogsInOnceAndPostsTheLink(t *testing.T) {
	var logins atomic.Int32
	m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
		switch action {
		case "connectUser":
			logins.Add(1)
			q := r.URL.Query()
			if q.Get("login") != "amy" || q.Get("password") != "secret-pass" {
				t.Errorf("connectUser got login %q, want the stored credential", q.Get("login"))
			}
			megadebridLogin(w, "tok-1", megadebridFuture)
		case "getLink":
			if r.Method != http.MethodPost {
				t.Errorf("getLink went out as %s, want POST", r.Method)
			}
			if got := r.URL.Query().Get("token"); got != "tok-1" {
				t.Errorf("getLink token = %q, want the one connectUser handed out", got)
			}
			if got := r.PostFormValue("link"); got != "https://rapidgator.net/file/x" {
				t.Errorf("getLink link = %q, want the hoster link in the form body", got)
			}
			_, _ = io.WriteString(w, `{"response_code":"ok","response_text":"",
				"debridLink":"https:\/\/www12.mega-debrid.eu\/download\/file\/abc","filename":"File.ext"}`)
		default:
			t.Errorf("unexpected action %q", action)
		}
	})

	for range 2 {
		got, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
		if err != nil {
			t.Fatalf("Unlock: %v", err)
		}
		if got.URL != "https://www12.mega-debrid.eu/download/file/abc" || got.Name != "File.ext" {
			t.Errorf("Unlock = %+v, want debridLink and filename", got)
		}
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("logged in %d times for two unlocks, want the token reused", n)
	}
}

func TestMegaDebridUnlockLogsInAgainWhenTheTokenIsRefused(t *testing.T) {
	var logins atomic.Int32
	m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
		switch action {
		case "connectUser":
			megadebridLogin(w, fmt.Sprintf("tok-%d", logins.Add(1)), megadebridFuture)
		case "getLink":
			if r.URL.Query().Get("token") == "tok-1" {
				_, _ = io.WriteString(w, `{"response_code":"TOKEN_ERROR","response_text":"Token error, please log-in"}`)
				return
			}
			_, _ = io.WriteString(w, `{"response_code":"ok","debridLink":"https://dl.example/one"}`)
		}
	})

	got, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.example/one" {
		t.Errorf("Unlock = %+v, want the link unlocked with the fresh token", got)
	}
	if n := logins.Load(); n != 2 {
		t.Errorf("logged in %d times, want one login and one after the refusal", n)
	}
}

func TestMegaDebridUnlockGivesUpAfterOneFreshLogin(t *testing.T) {
	var logins, unlocks atomic.Int32
	m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
		switch action {
		case "connectUser":
			megadebridLogin(w, fmt.Sprintf("tok-%d", logins.Add(1)), megadebridFuture)
		case "getLink":
			unlocks.Add(1)
			_, _ = io.WriteString(w, `{"response_code":"TOKEN_ERROR","response_text":"Token error, please log-in"}`)
		}
	})

	_, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded although every token was refused")
	}
	if !strings.Contains(err.Error(), "Token error, please log-in") {
		t.Errorf("error = %q, want the service's own text", err)
	}
	if logins.Load() != 2 || unlocks.Load() != 2 {
		t.Errorf("%d logins and %d unlocks, want two of each and no loop", logins.Load(), unlocks.Load())
	}
}

// Only TOKEN_ERROR and UNALLOWED_IP are known codes. The other texts are the
// ones JDownloader matches, sent here under a neutral code because the real
// one is not documented; the quota and ban wordings are assumed.
func TestMegaDebridUnlockRefusalsReadDifferently(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
		text string
	}{
		{"dead link", `{"response_code":"error","response_text":"Unable to load file"}`, "probably dead", "Unable to load file"},
		{"unsupported host", `{"response_code":"error","response_text":"Erreur : Lien incorrect"}`, "probably not supported", "Erreur : Lien incorrect"},
		{"quota", `{"response_code":"error","response_text":"Limite journalière atteinte pour cet hébergeur"}`, "daily limit", "Limite journalière"},
		{"cannot unlock", `{"response_code":"ok","response_text":"","debridLink":"cantDebridLink"}`, "could not unlock this link", ""},
		{"address ban", `{"response_code":"UNALLOWED_IP","response_text":"Request limit exceeded"}`, "IP address is blocked", "Request limit exceeded"},
		{"vpn", `{"response_code":"error","response_text":"VPN, proxy ou serveur détecté."}`, "VPNs, proxies and servers", "VPN, proxy ou serveur détecté."},
		{"unlocker fault", `{"response_code":"error","response_text":"Erreur : Problème Débrideur"}`, "try again later", "Problème Débrideur"},
		{"unknown text", `{"response_code":"error","response_text":"Something else went wrong"}`, "getLink", "Something else went wrong"},
		{"no link", `{"response_code":"ok","response_text":"Hoster answered nothing"}`, "no direct link returned", "Hoster answered nothing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
				if action == "connectUser" {
					megadebridLogin(w, "tok-1", megadebridFuture)
					return
				}
				_, _ = io.WriteString(w, c.body)
			})
			_, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
			if err == nil {
				t.Fatal("Unlock succeeded against a refusal")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "mega-debrid") || !strings.Contains(msg, c.want) || !strings.Contains(msg, c.text) {
				t.Errorf("error = %q, want the service named, %q and the service's text %q", msg, c.want, c.text)
			}
		})
	}
}

// Four failed logins ban the address, so a refused credential is not tried
// again by later calls on the same client.
func TestMegaDebridRefusedLoginIsNotRetried(t *testing.T) {
	cases := []struct {
		name   string
		answer func(w http.ResponseWriter)
	}{
		{"unknown user", func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"response_code":"UNKNOWN_USER","response_text":""}`)
		}},
		{"http 401", func(w http.ResponseWriter) { w.WriteHeader(http.StatusUnauthorized) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var logins, unlocks atomic.Int32
			m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
				switch action {
				case "connectUser":
					logins.Add(1)
					c.answer(w)
				case "getLink":
					unlocks.Add(1)
				}
			})
			err := m.Authenticate(context.Background())
			if err == nil {
				t.Fatal("Authenticate accepted a refused credential")
			}
			if !strings.Contains(err.Error(), "user name or password was refused") {
				t.Errorf("error = %q, want it to name the credential", err)
			}
			if strings.Contains(err.Error(), "secret-pass") {
				t.Errorf("error = %q carries the password", err)
			}
			if _, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x"); err == nil {
				t.Error("Unlock succeeded after the login was refused")
			}
			if err := m.Authenticate(context.Background()); err == nil {
				t.Error("a second Authenticate succeeded after the login was refused")
			}
			if logins.Load() != 1 || unlocks.Load() != 0 {
				t.Errorf("%d logins and %d unlocks, want the one refused login and nothing after it", logins.Load(), unlocks.Load())
			}
		})
	}
}

func TestMegaDebridBannedAddressIsReportedOnLogin(t *testing.T) {
	m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	err := m.Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "IP address is blocked") {
		t.Errorf("Authenticate = %v, want the address ban named", err)
	}
}

// An incomplete credential would be one more failed login towards the ban, so
// it has to stop before the request.
func TestMegaDebridAuthenticateSendsNothingWithoutBothFields(t *testing.T) {
	for _, c := range []struct{ name, user, pass string }{
		{"no password", "amy", ""},
		{"no user name", "", "secret-pass"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
				calls.Add(1)
				megadebridLogin(w, "tok-1", megadebridFuture)
			})
			m.user, m.pass = c.user, c.pass
			err := m.Authenticate(context.Background())
			if err == nil || !strings.Contains(err.Error(), "user name and a password are required") {
				t.Errorf("Authenticate = %v, want the missing field named", err)
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("%d requests went out, want none", n)
			}
		})
	}
}

func TestMegaDebridLoginWithoutATokenIsNotASession(t *testing.T) {
	var logins atomic.Int32
	m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
		if action != "connectUser" {
			t.Errorf("%s went out without a session", action)
			return
		}
		logins.Add(1)
		_, _ = io.WriteString(w, `{"response_code":"ok","response_text":"User logged","vip_end":"0"}`)
	})
	if err := m.Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate accepted a login that handed out no token")
	}
	if _, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x"); err == nil {
		t.Fatal("Unlock succeeded without a token")
	}
	if n := logins.Load(); n != 2 {
		t.Errorf("logged in %d times, want the tokenless answer not kept as a refusal", n)
	}
}

// The password and the token travel in the query string, and a transport
// error from net/http prints the whole URL.
func TestMegaDebridTransportErrorsCarryNoCredential(t *testing.T) {
	t.Run("login", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		m := NewMegaDebrid("amy", "secret-pass")
		m.base = srv.URL + "/api.php"
		srv.Close()

		err := m.Authenticate(context.Background())
		if err == nil {
			t.Fatal("Authenticate succeeded against a closed server")
		}
		if strings.Contains(err.Error(), "secret-pass") || strings.Contains(err.Error(), "amy") {
			t.Errorf("error = %q carries the credential", err)
		}
	})
	t.Run("unlock", func(t *testing.T) {
		m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
			if action == "connectUser" {
				megadebridLogin(w, "tok-secret", megadebridFuture)
				return
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
		})
		_, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x")
		if err == nil {
			t.Fatal("Unlock succeeded against a dropped connection")
		}
		if strings.Contains(err.Error(), "tok-secret") {
			t.Errorf("error = %q carries the session token", err)
		}
	})
}

func TestMegaDebridAccountReadsVipEnd(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour).Unix()
	cases := []struct {
		name      string
		vipEnd    string
		tier      string
		unlimited bool
		expires   int64
	}{
		{"premium as a string", fmt.Sprintf("%q", fmt.Sprint(megadebridFuture)), "premium", true, megadebridFuture},
		{"premium as a number", fmt.Sprint(megadebridFuture), "premium", true, megadebridFuture},
		{"expired", fmt.Sprint(past), "free", false, past},
		{"never premium", `"0"`, "free", false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
				_, _ = fmt.Fprintf(w, `{"response_code":"ok","response_text":"User logged","token":"tok-1","vip_end":%s}`, c.vipEnd)
			})
			info, err := m.Account(context.Background())
			if err != nil {
				t.Fatalf("Account: %v", err)
			}
			if info.Tier != c.tier || info.Traffic.Unlimited != c.unlimited {
				t.Errorf("Account = %+v, want tier %q and Unlimited %v", info, c.tier, c.unlimited)
			}
			switch {
			case c.expires == 0 && !info.ExpiresAt.IsZero():
				t.Errorf("ExpiresAt = %v, want the zero time", info.ExpiresAt)
			case c.expires != 0 && info.ExpiresAt.Unix() != c.expires:
				t.Errorf("ExpiresAt = %v, want the vip_end timestamp %d", info.ExpiresAt, c.expires)
			}
		})
	}
}

// Every login voids the token before it, so an account check and an unlock
// on the same client have to share one.
func TestMegaDebridAccountAndUnlockShareOneToken(t *testing.T) {
	var logins atomic.Int32
	m := megadebridServe(t, func(w http.ResponseWriter, r *http.Request, action string) {
		switch action {
		case "connectUser":
			megadebridLogin(w, fmt.Sprintf("tok-%d", logins.Add(1)), megadebridFuture)
		case "getLink":
			if got := r.URL.Query().Get("token"); got != "tok-1" {
				t.Errorf("getLink token = %q, want the one Account logged in with", got)
			}
			_, _ = io.WriteString(w, `{"response_code":"ok","debridLink":"https://dl.example/one"}`)
		}
	})
	if _, err := m.Account(context.Background()); err != nil {
		t.Fatalf("Account: %v", err)
	}
	if _, err := m.Unlock(context.Background(), "https://rapidgator.net/file/x"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("logged in %d times, want Unlock to reuse the account check's token", n)
	}
}
