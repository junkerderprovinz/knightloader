package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// reconnectServer is the reconnect routes on a throwaway app.
func reconnectServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerReconnect(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

func getRaw(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// TestReconnectStateNamesTheMissingField is why the state route carries a
// reason at all. "Not configured" beside a method that is plainly selected sends
// people to the on/off switch, which is already on; the field that is empty is
// three rows further down and nothing on the page points at it.
func TestReconnectStateNamesTheMissingField(t *testing.T) {
	a, srv := reconnectServer(t)

	// A half-filled command method: the method is chosen, the program is not.
	s := a.Settings.Get()
	s.Reconnect = reconnect.Config{Method: reconnect.MethodCommand, CheckURL: "https://example.org/ip"}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	code, raw := getRaw(t, srv.URL+"/api/reconnect")
	if code != http.StatusOK {
		t.Fatalf("GET /api/reconnect answered %d: %s", code, raw)
	}
	var got struct {
		Busy       bool   `json:"busy"`
		Configured bool   `json:"configured"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Configured {
		t.Error("a command method with no program came back as configured")
	}
	// The exact words are Validate's, not this route's - that is the whole point
	// of taking the message from there - so the assertion is on the field it has
	// to name rather than on the sentence around it.
	if !strings.Contains(got.Reason, "command") {
		t.Errorf("the reason %q never mentions the empty field", got.Reason)
	}
}

// TestReconnectStateIsQuietWhenItIsFine checks the other direction: a working
// configuration must not hand the page a sentence to display, or the settings
// form grows a permanent complaint about nothing.
func TestReconnectStateIsQuietWhenItIsFine(t *testing.T) {
	a, srv := reconnectServer(t)
	s := a.Settings.Get()
	s.Reconnect = reconnect.Config{
		Method:   reconnect.MethodCommand,
		Command:  "/bin/true",
		CheckURL: "https://example.org/ip",
	}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	_, raw := getRaw(t, srv.URL+"/api/reconnect")
	var got struct {
		Configured bool   `json:"configured"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Configured || got.Reason != "" {
		t.Errorf("a complete configuration came back configured=%v reason=%q", got.Configured, got.Reason)
	}
}

// TestReconnectImportShowsBothHalves is the contract the import route exists
// for. A script that is nine tenths right has to come back as nine tenths of a
// request list plus the line that was refused, with its number: the alternative
// is an error under a forty-line paste and somebody counting rows by hand.
func TestReconnectImportShowsBothHalves(t *testing.T) {
	_, srv := reconnectServer(t)

	const script = "[[[HSRC]]]\n" +
		"GET /login.cgi HTTP/1.1\n" +
		"Host: %%%routerip%%%\n" +
		"[[[/HSRC]]]\n" +
		"this line is not in a block\n"

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/reconnect/import", map[string]string{"text": script})
	if code != http.StatusOK {
		t.Fatalf("POST /api/reconnect/import answered %d: %s", code, raw)
	}
	var got struct {
		Requests []reconnect.Request `json:"requests"`
		Problems []reconnect.Problem `json:"problems"`
		Error    string              `json:"error"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Requests) != 1 {
		t.Fatalf("the block that maps came back as %d requests, want 1", len(got.Requests))
	}
	if len(got.Problems) != 1 {
		t.Fatalf("the line that does not map came back as %d problems, want 1", len(got.Problems))
	}
	if got.Problems[0].Line != 5 {
		t.Errorf("the refused line is reported as line %d, want 5", got.Problems[0].Line)
	}
	if got.Problems[0].Text == "" {
		t.Error("the refused line came back without its text, so the page cannot show which one it was")
	}
	// A script with a refused line must not be storable, and the only thing that
	// says so is this field - the two lists on their own read as a successful
	// import with a note attached.
	if got.Error == "" {
		t.Error("a refused script came back with no error, which reads as a clean import")
	}
}

// TestReconnectImportRefusesAnEmptyPasteOutLoud pins the failure that produces
// no per-line problem at all. A form that cleared itself and said nothing here
// looks exactly like a successful import of nothing.
func TestReconnectImportRefusesAnEmptyPasteOutLoud(t *testing.T) {
	_, srv := reconnectServer(t)
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/reconnect/import", map[string]string{"text": "   \n\n"})
	if code != http.StatusOK {
		t.Fatalf("POST /api/reconnect/import answered %d: %s", code, raw)
	}
	var got struct {
		Requests []reconnect.Request `json:"requests"`
		Error    string              `json:"error"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Requests) != 0 || got.Error == "" {
		t.Errorf("an empty paste came back with %d requests and error %q", len(got.Requests), got.Error)
	}
}

// TestReconnectRouterAnswersOrSaysWhy is deliberately platform-agnostic: the
// gateway is only readable on Linux, and the point of the route is that the two
// outcomes are told apart. What it must never do is answer 200 with nothing in
// it, which is the shape that leaves the field silently blank.
func TestReconnectRouterAnswersOrSaysWhy(t *testing.T) {
	_, srv := reconnectServer(t)
	code, raw := getRaw(t, srv.URL+"/api/reconnect/router")
	switch code {
	case http.StatusOK:
		var got reconnect.RouterAddress
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if !got.Address.IsValid() {
			t.Error("the router route answered 200 with no address in it")
		}
	case http.StatusNotFound:
		if strings.TrimSpace(string(raw)) == "" {
			t.Error("the router route refused with no reason, so the field has nothing to show")
		}
	default:
		t.Fatalf("GET /api/reconnect/router answered %d: %s", code, raw)
	}
}
