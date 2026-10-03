package api

// A browser extension handing over a download it took from the browser: one
// link with the browser's Cookie, Referer and User-Agent, and the page it came
// from.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

const handedCookie = "session=s3cr3t-value"

func TestAddLinksTakesABrowsersHeadersForOneLink(t *testing.T) {
	t.Parallel()
	srv, _ := linkServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", map[string]any{
		"links":  "https://files.example/members/film.mkv",
		"origin": "cnl",
		"source": "https://files.example/thread/7",
		"headers": map[string]string{
			"Cookie":     handedCookie,
			"Referer":    "https://files.example/thread/7",
			"User-Agent": "Mozilla/5.0",
		},
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	if strings.Contains(string(raw), "s3cr3t") {
		t.Error("the answer echoes the cookie")
	}
	var created []core.Task
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0].Source != "https://files.example/thread/7" {
		t.Fatalf("created %+v, want the one link with the page it came from", created)
	}
}

func TestAddLinksRefusesHeadersItCannotScope(t *testing.T) {
	t.Parallel()
	srv, a := linkServer(t)

	for name, body := range map[string]map[string]any{
		"two links": {
			"links":   "https://files.example/a.zip\nhttps://other.example/b.zip",
			"headers": map[string]string{"Cookie": handedCookie},
		},
		"another header": {
			"links":   "https://files.example/a.zip",
			"headers": map[string]string{"Authorization": "Bearer " + handedCookie},
		},
		"a line break": {
			"links":   "https://files.example/a.zip",
			"headers": map[string]string{"Cookie": handedCookie + "\r\nX-Injected: 1"},
		},
		"a control character": {
			"links":   "https://files.example/a.zip",
			"headers": map[string]string{"Cookie": handedCookie + "\x00"},
		},
		"a name that is no token": {
			"links":   "https://files.example/a.zip",
			"headers": map[string]string{"Authorization:": "Bearer " + handedCookie},
		},
		"a file as two links": {
			"links": "https://files.example/a.zip\nhttps://files.example/b.zip",
			"file":  true,
			"name":  "a.zip",
		},
		"with passwords": {
			"links":     "https://files.example/a.zip",
			"passwords": []string{"pw"},
			"headers":   map[string]string{"Cookie": handedCookie},
		},
		"a source that is no web address": {
			"links":  "https://files.example/a.zip",
			"source": "javascript:alert(1)",
		},
	} {
		code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/links", body)
		if code != http.StatusBadRequest {
			t.Errorf("%s: answered %d, want 400: %s", name, code, raw)
		}
		if strings.Contains(string(raw), "s3cr3t") {
			t.Errorf("%s: the refusal quotes the value: %s", name, raw)
		}
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks staged from refused submissions", n)
	}
}
