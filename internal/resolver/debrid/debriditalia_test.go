package debrid

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The bodies below are the plain-text answers the JD and pyLoad plugins parse,
// since DebridItalia documents none of them.

const (
	debriditaliaTestUser = "amy"
	debriditaliaTestPass = "s3cret&pw"
)

// debriditaliaServer answers every call with reply, which gets the query the
// client sent.
func debriditaliaServer(t *testing.T, reply func(w http.ResponseWriter, q url.Values)) *DebridItalia {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.php" {
			t.Errorf("request to %s, want /api.php", r.URL.Path)
		}
		reply(w, r.URL.Query())
	}))
	t.Cleanup(srv.Close)
	return debriditaliaAt(srv.URL + "/api.php")
}

// debriditaliaAt gives the client a pacer of its own, so one test cannot use
// up another's allowance.
func debriditaliaAt(base string) *DebridItalia {
	d := NewDebridItalia(debriditaliaTestUser, debriditaliaTestPass)
	d.base = base
	d.pace = &debriditaliaPacer{window: time.Minute, max: debriditaliaPerMinute}
	return d
}

func debriditaliaWantLogin(t *testing.T, q url.Values) {
	t.Helper()
	if q.Get("u") != debriditaliaTestUser || q.Get("p") != debriditaliaTestPass {
		t.Errorf("login sent as u=%q p=%q, want the configured username and password", q.Get("u"), q.Get("p"))
	}
}

func TestDebridItaliaHostsReadsTheQuotedList(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"bare list", `"1fichier.com","4shared.com","www.AlfaFile.net","uploady.io"`},
		{"json array", `["1fichier.com", "4shared.com", "WWW.AlfaFile.net", "uploady.io"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
				if !q.Has("hosts") {
					t.Errorf("query %v does not ask for hosts", q)
				}
				if q.Has("u") || q.Has("p") {
					t.Error("the public host list was sent the login")
				}
				_, _ = io.WriteString(w, c.body+"\n")
			})
			hosts, err := d.Hosts(context.Background())
			if err != nil {
				t.Fatalf("Hosts: %v", err)
			}
			for _, want := range []string{"1fichier.com", "4shared.com", "alfafile.net", "uploady.io"} {
				if !hosts[want] {
					t.Errorf("Hosts is missing %q: %v", want, hosts)
				}
			}
			if len(hosts) != 4 {
				t.Errorf("Hosts = %v, want exactly the four listed domains", hosts)
			}
		})
	}
}

func TestDebridItaliaHostsLeavesOutWhatIsNotADomain(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		_, _ = io.WriteString(w, `"rapidgator.net","","NO_REQUEST","1.0","not a host.com","ddownload.com"`)
	})
	hosts, err := d.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if len(hosts) != 2 || !hosts["rapidgator.net"] || !hosts["ddownload.com"] {
		t.Errorf("Hosts = %v, want only rapidgator.net and ddownload.com", hosts)
	}
}

func TestDebridItaliaHostsFailsWhenTheAnswerNamesNoHost(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		_, _ = io.WriteString(w, "NO_REQUEST")
	})
	if hosts, err := d.Hosts(context.Background()); err == nil {
		t.Fatalf("Hosts = %v, want an error for an answer without domains", hosts)
	}
}

func TestDebridItaliaUnlockReturnsTheBareLink(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		if q.Get("generate") != "on" {
			t.Errorf("generate = %q, want on", q.Get("generate"))
		}
		debriditaliaWantLogin(t, q)
		if got := q.Get("link"); got != "http://1fichier.com/?abc123" {
			t.Errorf("link = %q, want the hoster link rewritten to http", got)
		}
		_, _ = io.WriteString(w, "https://s4.debriditalia.com/dl/1234567/File.ext\r\n")
	})
	got, err := d.Unlock(context.Background(), "https://1fichier.com/?abc123")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://s4.debriditalia.com/dl/1234567/File.ext" {
		t.Errorf("URL = %q, want the answer without its line break", got.URL)
	}
	if got.Name != "" || got.Size != 0 {
		t.Errorf("Unlock = %+v, want name and size left to the engine", got)
	}
}

func TestDebridItaliaUnlockEscapesAFileNameSentRaw(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		_, _ = io.WriteString(w, "https://s4.debriditalia.com/dl/1234567/My File [2024].mkv\n")
	})
	got, err := d.Unlock(context.Background(), "http://1fichier.com/?abc123")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if want := "https://s4.debriditalia.com/dl/1234567/My%20File%20%5B2024%5D.mkv"; got.URL != want {
		t.Errorf("URL = %q, want %q", got.URL, want)
	}
}

func TestDebridItaliaUnlockRefusesALinkSentWithAServerError(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "https://s4.debriditalia.com/dl/1234567/File.ext")
	})
	if got, err := d.Unlock(context.Background(), "http://1fichier.com/?abc123"); err == nil {
		t.Fatalf("Unlock = %+v from a 502, want an error", got)
	}
}

func TestDebridItaliaUnlockTellsTheRefusalsApart(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{"ERROR: not_available", "unavailable"},
		{"ERROR: not_supported", "not supported"},
		{"ERROR: bandwidth_limit", "traffic cap"},
		{"error:something_new", "something_new"},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
			_, _ = io.WriteString(w, c.body)
		})
		_, err := d.Unlock(context.Background(), "http://1fichier.com/?abc123")
		if err == nil {
			t.Fatalf("%s: Unlock succeeded", c.body)
		}
		msg := err.Error()
		if !strings.HasPrefix(msg, "debriditalia: ") || !strings.Contains(msg, c.want) {
			t.Errorf("%s: error = %q, want the service named and %q", c.body, msg, c.want)
		}
		if seen[msg] {
			t.Errorf("%s: error %q reads like another refusal", c.body, msg)
		}
		seen[msg] = true
	}
}

func TestDebridItaliaUnlockRefusesAnAnswerThatIsNotALink(t *testing.T) {
	for _, body := range []string{"NO_REQUEST", "<html><body>Access denied</body></html>", ""} {
		d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
			_, _ = io.WriteString(w, body)
		})
		if got, err := d.Unlock(context.Background(), "http://1fichier.com/?abc123"); err == nil {
			t.Errorf("answer %q: Unlock = %+v, want an error", body, got)
		}
	}
}

func TestDebridItaliaRefusedLoginNamesTheLoginNotThePassword(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err := d.Unlock(context.Background(), "http://1fichier.com/?abc123")
	if err == nil {
		t.Fatal("Unlock succeeded against a 401")
	}
	if !strings.Contains(err.Error(), "username or password") {
		t.Errorf("error = %q, want it to name the login", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error = %q carries the password", err)
	}
}

func TestDebridItaliaRateLimitReadsAsSuch(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "<html>Access denied</html>")
	})
	_, err := d.Unlock(context.Background(), "http://1fichier.com/?abc123")
	if err == nil || !strings.Contains(err.Error(), "too many requests") {
		t.Errorf("error = %v, want it to say the service rate-limited the call", err)
	}
}

// The API puts the password in the URL, and net/http quotes that URL in its
// transport errors.
func TestDebridItaliaTransportErrorCarriesNoCredentials(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL + "/api.php"
	srv.Close()

	_, err := debriditaliaAt(base).Unlock(context.Background(), "http://1fichier.com/?abc123")
	if err == nil {
		t.Fatal("Unlock succeeded against a closed server")
	}
	for _, secret := range []string{"s3cret", "u=amy", "p="} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error = %q carries %q", err, secret)
		}
	}
}

func TestDebridItaliaMissingLoginIsRefusedBeforeAnyRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a request went out without a username and password")
	}))
	defer srv.Close()
	d := debriditaliaAt(srv.URL + "/api.php")
	d.pass = ""

	if err := d.Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate succeeded without a password")
	}
}

func TestDebridItaliaAccountReadsStatusAndExpiry(t *testing.T) {
	cases := []struct {
		name       string
		expiration string
	}{
		{"bare", "1893456000"},
		{"quoted", `"1893456000"`},
		{"padded fraction", " 1893456000.0 "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
				if q.Get("check") != "on" {
					t.Errorf("check = %q, want on", q.Get("check"))
				}
				debriditaliaWantLogin(t, q)
				_, _ = io.WriteString(w, "<status>valid</status>\n<expiration>"+c.expiration+"</expiration>")
			})
			info, err := d.Account(context.Background())
			if err != nil {
				t.Fatalf("Account: %v", err)
			}
			if info.Tier != "premium" || !info.Traffic.Unlimited {
				t.Errorf("Account = %+v, want premium with unlimited traffic", info)
			}
			if want := time.Unix(1893456000, 0).UTC(); !info.ExpiresAt.Equal(want) {
				t.Errorf("ExpiresAt = %v, want %v", info.ExpiresAt, want)
			}
		})
	}
}

func TestDebridItaliaExpiredAccountLogsInAsFree(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		_, _ = io.WriteString(w, "<status>expired</status><expiration>1600000000</expiration>")
	})
	if err := d.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate refused an expired account whose login is right: %v", err)
	}
	info, err := d.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "free" || info.Traffic.Unlimited {
		t.Errorf("Account = %+v, want a free tier without unlimited traffic", info)
	}
	if want := time.Unix(1600000000, 0).UTC(); !info.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want the lapsed date %v", info.ExpiresAt, want)
	}
}

func TestDebridItaliaValidAccountWithoutExpiryHasNone(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		_, _ = io.WriteString(w, "<status>valid</status>")
	})
	info, err := d.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" || !info.ExpiresAt.IsZero() {
		t.Errorf("Account = %+v, want premium with the zero expiry", info)
	}
}

func TestDebridItaliaAuthenticateRejectsAWrongLogin(t *testing.T) {
	d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
		_, _ = io.WriteString(w, "<status>invalid</status>")
	})
	err := d.Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "username or password") {
		t.Errorf("Authenticate = %v, want the login refused", err)
	}
}

func TestDebridItaliaAccountRefusesAnUnknownStatus(t *testing.T) {
	for _, body := range []string{"<status>banned</status>", "NO_REQUEST"} {
		d := debriditaliaServer(t, func(w http.ResponseWriter, q url.Values) {
			_, _ = io.WriteString(w, body)
		})
		if info, err := d.Account(context.Background()); err == nil {
			t.Errorf("answer %q: Account = %+v, want an error", body, info)
		}
	}
}

func TestDebridItaliaPacerHoldsTheCallOverTheAllowance(t *testing.T) {
	p := &debriditaliaPacer{window: 150 * time.Millisecond, max: 2}
	start := time.Now()
	for range 3 {
		if err := p.wait(context.Background()); err != nil {
			t.Fatalf("wait: %v", err)
		}
	}
	if waited := time.Since(start); waited < 140*time.Millisecond {
		t.Errorf("the third call started after %v, want it held until the window moved on", waited)
	}
}

func TestDebridItaliaPacerGivesUpWithTheContext(t *testing.T) {
	p := &debriditaliaPacer{window: time.Hour, max: 1}
	if err := p.wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.wait(ctx); err == nil {
		t.Fatal("wait returned a slot the allowance does not have")
	}
}
