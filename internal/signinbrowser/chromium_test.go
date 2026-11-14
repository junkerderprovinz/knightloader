package signinbrowser

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

const testLogin = `<!doctype html><title>Sign in</title>
<body onload="document.getElementById('user').focus()">
<form method="post" action="/signin">
<input id="user" name="user"> <input id="pass" name="pass" type="password"> <button>Sign in</button>
</form>`

const testHome = `<!doctype html><title>Home</title>
<button style="position:fixed;inset:0;width:100%%;height:100%%" onclick="location.href='%s'">go</button>`

// A real Chromium signs in to a site served here, under a name that only this
// test puts on the site's list, and the session comes back as a cookies.txt.
func TestARealChromiumSignsInAndHandsTheSessionOver(t *testing.T) {
	bin := os.Getenv("KL_CHROMIUM")
	if bin == "" {
		t.Skip("KL_CHROMIUM names no Chromium to run")
	}
	away := make(chan string, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			fmt.Fprint(w, testLogin)
		case "/signin":
			if r.FormValue("user") != "knight" || r.FormValue("pass") != "Grüße" {
				http.Error(w, "wrong user "+r.FormValue("user")+" or password "+r.FormValue("pass"), http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session_id", Value: "S-REAL", Domain: "signin.test", Path: "/",
				Secure: true, HttpOnly: true, Expires: time.Now().Add(time.Hour)})
			http.Redirect(w, r, "/home", http.StatusFound)
		case "/home":
			fmt.Fprintf(w, testHome, r.URL.Query().Get("to"))
		case "/favicon.ico":
			w.WriteHeader(http.StatusNoContent)
		default:
			select {
			case away <- r.Host + r.URL.Path:
			default:
			}
		}
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(interface{ String() string }).String()
	port = port[strings.LastIndex(port, ":")+1:]

	s, _ := sitelogin.Lookup("patreon")
	s.Domains = append(s.Domains, "signin.test")
	s.LoginURL = "https://signin.test/login"

	handed := make(chan string, 8)
	m := NewManager(context.Background(), Config{
		Binary:  bin,
		Profile: t.TempDir(),
		Session: func(_ sitelogin.Site, jar string) any {
			handed <- jar
			return "ok"
		},
	})
	m.cfg.lookup = func(ctx context.Context, host string) httpx.Scope {
		if host == "signin.test" {
			return httpx.ScopePublic
		}
		return httpx.ScopeOfHost(ctx, host)
	}
	m.cfg.every = 500 * time.Millisecond
	m.cfg.args = []string{
		"--host-resolver-rules=MAP signin.test 127.0.0.1:" + port + ", MAP away.test 127.0.0.1:" + port,
		"--ignore-certificate-errors",
	}
	if err := m.Open(context.Background(), s, 800, 600, 1); err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())

	v := newViewer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Watch(ctx, v) }()
	waitHost(t, v, "signin.test")
	select {
	case f := <-v.frames:
		if !bytes.HasPrefix(f, []byte{0xFF, 0xD8}) {
			t.Errorf("a frame is not a JPEG: % x", f[:8])
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no frame came")
	}

	waitFor(t, m, "document.activeElement && document.activeElement.id === 'user'")
	typeText(m, "knight")
	m.Input(Input{Type: "keydown", Key: "Tab", Code: "Tab", KeyCode: 9})
	m.Input(Input{Type: "keyup", Key: "Tab", Code: "Tab", KeyCode: 9})
	typeText(m, "Grüße")
	m.Input(Input{Type: "keydown", Key: "Enter", Code: "Enter", KeyCode: 13})
	m.Input(Input{Type: "keyup", Key: "Enter", Code: "Enter", KeyCode: 13})
	select {
	case jar := <-handed:
		if !strings.Contains(jar, ".signin.test	TRUE	/	TRUE	") || !strings.Contains(jar, "session_id	S-REAL") {
			t.Errorf("handed on %q, want the test site's session", jar)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the sign-in never reached the handover")
	}

	// A page that sends the window into the LAN, off the list or onto the
	// disk stays where it is. Chromium refuses the disk to a page itself.
	for _, to := range []string{"http://192.168.20.1/", "https://away.test/elsewhere", "file:///C:/"} {
		navigate(m, "https://signin.test/home?to="+url.QueryEscape(to))
		waitHost(t, v, "signin.test")
		waitFor(t, m, "document.readyState === 'complete'")
		m.Input(Input{Type: "mousedown", X: 100, Y: 100, Button: 0, Buttons: 1, Clicks: 1})
		m.Input(Input{Type: "mouseup", X: 100, Y: 100, Button: 0, Clicks: 1})
		time.Sleep(2 * time.Second)
		if host := lastHost(m); host != "signin.test" {
			t.Errorf("a page sent the window to %s, and it shows %q", to, host)
		}
	}
	select {
	case got := <-away:
		t.Errorf("the window reached %s", got)
	default:
	}

	// So does a navigation from outside the page, which Chromium would
	// carry out: the LAN address never loads, and the disk sends the window
	// back to the login page.
	for _, to := range []string{"http://192.168.20.1/", "file:///C:/"} {
		drain(v)
		navigate(m, to)
		blocked := v.next(t, "blocked")
		time.Sleep(time.Second)
		if host := lastHost(m); host != "signin.test" {
			t.Errorf("the window went to %s and shows %q (told %v)", to, host, blocked)
		}
	}
}

func navigate(m *Manager, to string) {
	m.current().call(m.current().front().session, "Page.navigate", map[string]string{"url": to})
}

// waitFor waits until expr is true in the page in front.
func waitFor(t *testing.T, m *Manager, expr string) {
	t.Helper()
	for range 100 {
		var res struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		_ = m.current().c.call(context.Background(), m.current().front().session, "Runtime.evaluate",
			map[string]any{"expression": expr, "returnByValue": true}, &res)
		if res.Result.Value {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the page never got to %s", expr)
}

func drain(v *viewer) []map[string]any {
	var out []map[string]any
	for {
		select {
		case m := <-v.msgs:
			out = append(out, m)
		default:
			return out
		}
	}
}

func typeText(m *Manager, text string) {
	for _, r := range text {
		k := string(r)
		code := 0
		if r < 128 {
			code = int(strings.ToUpper(k)[0])
		}
		m.Input(Input{Type: "keydown", Key: k, KeyCode: code})
		m.Input(Input{Type: "keyup", Key: k, KeyCode: code})
	}
}

func waitHost(t *testing.T, v *viewer, host string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case m := <-v.msgs:
			if m["type"] == "page" && m["host"] == host {
				return
			}
		case <-deadline:
			t.Fatalf("the window never showed %s", host)
		}
	}
}

// lastHost asks the browser where the page in front is.
func lastHost(m *Manager) string {
	s := m.current()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var h struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			URL string `json:"url"`
		} `json:"entries"`
	}
	if s.c.call(ctx, s.front().session, "Page.getNavigationHistory", nil, &h) != nil {
		return "?"
	}
	u, _ := url.Parse(h.Entries[h.CurrentIndex].URL)
	return u.Hostname()
}
