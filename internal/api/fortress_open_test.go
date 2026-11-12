package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

const (
	openerPassword = "the opener's own password"
	memberPassword = "the member's own password"
)

// openingPair is two KnightLoaders in one fortress on a relay of their own:
// the opener, where the browser signs in with the password, and the member
// it opens, which asks for a password of its own when withPassword is set.
type openingPair struct {
	opener, member *httptest.Server
	a, b           *app.App
	browser        *http.Client
	memberID       string
}

func openingTwo(t *testing.T, withPassword bool) *openingPair {
	t.Helper()
	p := &openingPair{}
	p.opener, p.a = testServer(t)
	t.Cleanup(p.opener.Close)
	p.member, p.b = testServer(t)
	t.Cleanup(p.member.Close)
	relayURL := ownRelay(t, p.a, p.opener.URL)
	if code, cfg := putRelayConfig(t, p.member.URL, `{"relayUrl":"`+relayURL+`","mode":"own"}`); code != http.StatusOK {
		t.Fatalf("PUT /api/relay/config = %d %+v", code, cfg)
	}
	_, words := newWords(t)
	for _, base := range []string{p.opener.URL, p.member.URL} {
		if code, raw := postJSON(t, http.MethodPost, base+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
			t.Fatalf("join answered %d: %s", code, raw)
		}
	}
	p.memberID = memberKey(p.b).ID()
	waitFor(t, "the member offering its pages", func() bool {
		for _, m := range fortressViewOf(p.a).Members {
			if m.ID == p.memberID && m.Open && m.Online {
				return true
			}
		}
		return false
	})
	if err := p.a.Auth.SetPassword("", openerPassword); err != nil {
		t.Fatal(err)
	}
	if withPassword {
		if err := p.b.Auth.SetPassword("", memberPassword); err != nil {
			t.Fatal(err)
		}
	}
	p.browser = &http.Client{Jar: newJar(t), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if code := login(t, p.browser, p.opener.URL, openerPassword); code != http.StatusOK {
		t.Fatalf("signing in at the opener answered %d", code)
	}
	return p
}

// open asks the opener for a tab on the member and returns its status and
// what it answered.
func (p *openingPair) open(t *testing.T, c *http.Client) (int, openAnswer) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, p.opener.URL+buildinfo.BasePath+"/api/fortress/open/"+p.memberID, nil)
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out openAnswer
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

type openAnswer struct {
	Path string `json:"path"`
	Code string `json:"code"`
}

// tab opens a tab on the member and returns the address its pages live under.
func (p *openingPair) tab(t *testing.T) string {
	t.Helper()
	code, out := p.open(t, p.browser)
	if code != http.StatusOK || !strings.HasPrefix(out.Path, buildinfo.BasePath+"/fortress/open/") {
		t.Fatalf("opening answered %d %+v", code, out)
	}
	return p.opener.URL + out.Path
}

// inTab sends a request of the sandboxed page in the tab, as a browser does:
// from an origin of its own and without the opener's cookies.
func inTab(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Origin", "null")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}

func TestTheMembersInterfaceOpensBehindItsOwnSignIn(t *testing.T) {
	t.Parallel()
	p := openingTwo(t, true)
	tab := p.tab(t)

	res, err := p.browser.Get(tab)
	if err != nil {
		t.Fatal(err)
	}
	shell, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(shell), "<iframe") {
		t.Fatalf("the tab answered %d %s", res.StatusCode, shell)
	}

	code, page := inTab(t, http.MethodGet, tab+"/", "")
	prefix := strings.TrimPrefix(tab, p.opener.URL) + "/"
	if code != http.StatusOK || !strings.Contains(page, baseElement(prefix)) {
		t.Fatalf("the member's page answered %d without the base %s: %.300s", code, prefix, page)
	}
	if code, _ := inTab(t, http.MethodGet, tab+"/api/tasks", ""); code != http.StatusUnauthorized {
		t.Errorf("the member's list answered %d before its own sign-in, want 401", code)
	}
	if code, raw := inTab(t, http.MethodPost, tab+"/api/auth/login", `{"password":"`+memberPassword+`"}`); code != http.StatusOK {
		t.Fatalf("signing in at the member answered %d %s", code, raw)
	}
	if code, raw := inTab(t, http.MethodGet, tab+"/api/tasks", ""); code != http.StatusOK {
		t.Errorf("after its own sign-in the member's list answered %d %s", code, raw)
	}

	entries := p.b.FortressLog.Page(status.LogQuery{Limit: 50, Lang: "en"}).Entries
	if !slices.ContainsFunc(entries, func(e status.LogEntry) bool {
		return e.Code == "ui.opened" && strings.HasPrefix(e.Text, "Opened from KnightLoader on ")
	}) {
		t.Errorf("the member's log does not say it was opened: %+v", entries)
	}
}

func TestAMemberWithoutAPasswordDoesNotOpen(t *testing.T) {
	t.Parallel()
	p := openingTwo(t, false)
	if code, out := p.open(t, p.browser); code != http.StatusForbidden || out.Code != "ui_no_login" {
		t.Errorf("opening a member without a password answered %d %+v", code, out)
	}
}

func TestOnlyAPasswordSessionOpensAMember(t *testing.T) {
	t.Parallel()
	p := openingTwo(t, true)
	_, secret, err := p.a.APITokens.Create("script")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, p.opener.URL+"/api/fortress/open/"+p.memberID, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("an API token opened a member with %d", res.StatusCode)
	}
}

func TestATunnelledPageFindsTheDoorsOfOtherProgramsShut(t *testing.T) {
	t.Parallel()
	p := openingTwo(t, true)
	tab := p.tab(t)
	if code, raw := inTab(t, http.MethodPost, tab+"/api/auth/login", `{"password":"`+memberPassword+`"}`); code != http.StatusOK {
		t.Fatalf("signing in at the member answered %d %s", code, raw)
	}
	for _, door := range []struct{ method, path string }{
		{http.MethodPost, "/api/auth/reset"},
		{http.MethodPost, "/api/auth/reset/finish"},
		{http.MethodGet, "/api/tokens"},
		{http.MethodPost, "/api/tokens"},
		{http.MethodGet, "/api/mcp"},
		{http.MethodPost, "/mcp"},
		{http.MethodGet, "/api/metrics"},
		{http.MethodGet, "/api/fortress/v1/status"},
		{http.MethodGet, "/api/qbittorrent/api/v2/app/version"},
		{http.MethodGet, "/api/sabnzbd/api?mode=version"},
		{http.MethodGet, "/dav/"},
		{http.MethodGet, "/api/system/backup"},
		{http.MethodPost, "/api/fortress/open/" + memberKey(p.a).ID()},
		{http.MethodGet, "/fortress/open/0123456789abcdef0123456789abcdef/"},
	} {
		code, raw := inTab(t, door.method, tab+door.path, "{}")
		if code != http.StatusForbidden || !strings.Contains(raw, `"code":"notThroughFortress"`) {
			t.Errorf("%s %s answered %d %.200s through the tunnel", door.method, door.path, code, raw)
		}
	}
}

func TestTheLiveStreamRunsThroughTheTunnel(t *testing.T) {
	t.Parallel()
	p := openingTwo(t, true)
	tab := p.tab(t)
	if code, raw := inTab(t, http.MethodPost, tab+"/api/auth/login", `{"password":"`+memberPassword+`"}`); code != http.StatusOK {
		t.Fatalf("signing in at the member answered %d %s", code, raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(tab, "http")+"/api/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"null"}},
	})
	if err != nil {
		t.Fatalf("the member's stream did not open through the tunnel: %v", err)
	}
	defer c.CloseNow()
	_, data, err := c.Read(ctx)
	if err != nil || !strings.Contains(string(data), "snapshot") {
		t.Errorf("the stream's first message is %q, %v", data, err)
	}
}

func TestASandboxedPageStillCannotWriteToTheOpener(t *testing.T) {
	t.Parallel()
	p := openingTwo(t, true)
	req, _ := http.NewRequest(http.MethodPost, p.opener.URL+"/api/links", strings.NewReader(`{"links":"https://example.com/x.bin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "null")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := p.browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a cross-site write with the opener's session answered %d, want 403", res.StatusCode)
	}
}

func TestAMembersStatusShowsForItsCard(t *testing.T) {
	t.Parallel()
	p := openingTwo(t, true)
	res, err := p.browser.Get(p.opener.URL + "/api/fortress/members/" + p.memberID + "/status?lang=en")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Report status.Report     `json:"report"`
		Log    []status.LogEntry `json:"log"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("the status answered %d, %v", res.StatusCode, err)
	}
	if out.Report.App.ID != appID || out.Report.State == "" || out.Log == nil {
		t.Errorf("the status reads %+v", out)
	}
}

// TestAnOpenedPageLivesUnderTheTabsPrefixWhateverTheBasePath stays serial: it
// sets KL_BASE_PATH for the whole package.
func TestAnOpenedPageLivesUnderTheTabsPrefixWhateverTheBasePath(t *testing.T) {
	servedUnder(t, "/kl")
	p := openingTwo(t, true)
	tab := p.tab(t)
	code, page := inTab(t, http.MethodGet, tab+"/", "")
	prefix := strings.TrimPrefix(tab, p.opener.URL+"/kl") + "/"
	if code != http.StatusOK || !strings.Contains(page, baseElement(prefix)) {
		t.Errorf("under a base path the member's page answered %d without the base %s: %.300s", code, prefix, page)
	}
}
