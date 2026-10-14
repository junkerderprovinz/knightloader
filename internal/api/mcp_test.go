package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

const mcpInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
	`"capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`

// mcpClientToken sets a client up the way the settings card does and returns
// the token's secret.
func mcpClientToken(t *testing.T, a *app.App, scopes ...apitoken.Scope) string {
	t.Helper()
	tok, secret, err := a.APITokens.CreateScoped("assistant", scopes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.MCPClients.Add("claude-code", tok.ID); err != nil {
		t.Fatal(err)
	}
	return secret
}

func postMCP(t *testing.T, srv *httptest.Server, body string, header func(*http.Request)) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if header != nil {
		header(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func bearerHeader(secret string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }
}

// tokenTransport puts the token on every request the SDK client sends, as a
// client configured with a header does.
type tokenTransport struct{ secret string }

func (t tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.secret)
	return http.DefaultTransport.RoundTrip(r)
}

// mcpSession connects the official client to a test server as the client
// holding secret.
func mcpSession(t *testing.T, a *app.App, secret string) *mcp.ClientSession {
	t.Helper()
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "knightloader-tests", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             srv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: tokenTransport{secret: secret}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// callTool calls name and returns the structured answer, failing the test on
// a tool error.
func callTool(t *testing.T, s *mcp.ClientSession, name string, args any) map[string]any {
	t.Helper()
	res := callToolRaw(t, s, name, args)
	if res.IsError {
		t.Fatalf("%s answered an error: %s", name, toolText(res))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(toolText(res)), &out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

func callToolRaw(t *testing.T, s *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	return res
}

func toolText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	text, _ := res.Content[0].(*mcp.TextContent)
	if text == nil {
		return ""
	}
	return text.Text
}

func TestTheMCPEndpointIsClosedUntilAClientIsSetUp(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	_, secret, err := a.APITokens.CreateScoped("script", apitoken.AllScopes())
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := postMCP(t, srv, mcpInitialize, bearerHeader(secret)); code != http.StatusNotFound {
		t.Errorf("/mcp answered %d with no client set up, want 404", code)
	}
}

func TestOnlyATokenMadeForAClientReachesMCP(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	client := mcpClientToken(t, a, apitoken.ScopeRead)
	_, sonarr, err := a.APITokens.CreateScoped("sonarr", apitoken.AllScopes())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		header func(*http.Request)
		want   int
	}{
		{"no token", nil, http.StatusUnauthorized},
		{"a token of no client", bearerHeader(sonarr), http.StatusUnauthorized},
		{"a wrong token", bearerHeader("kl_nothing"), http.StatusUnauthorized},
		{"the client's token", bearerHeader(client), http.StatusOK},
		{"the client's token in X-API-Key", func(r *http.Request) { r.Header.Set("X-API-Key", client) }, http.StatusOK},
		{"two different tokens", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+client)
			r.Header.Set("X-API-Key", sonarr)
		}, http.StatusUnauthorized},
	}
	for _, c := range cases {
		if code, body := postMCP(t, srv, mcpInitialize, c.header); code != c.want {
			t.Errorf("%s: /mcp answered %d (%s), want %d", c.name, code, body, c.want)
		}
	}
}

func TestMCPRefusesABatch(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	secret := mcpClientToken(t, a, apitoken.ScopeRead)
	code, body := postMCP(t, srv, "  ["+mcpInitialize+"]", bearerHeader(secret))
	if code != http.StatusBadRequest || !strings.Contains(body, "batch") {
		t.Errorf("a batch got %d %s, want 400 naming the batch", code, body)
	}
}

func TestTheOfficialClientListsAndCallsTheTools(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	s := mcpSession(t, a, mcpClientToken(t, a, apitoken.ScopeRead, apitoken.ScopeAdd, apitoken.ScopeControl))

	if got := s.InitializeResult().ServerInfo.Name; got != "knightloader" {
		t.Errorf("server name = %q", got)
	}
	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if want := len(mcpToolDefs(a)); len(tools.Tools) != want {
		t.Fatalf("the client sees %d tools, want %d", len(tools.Tools), want)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil {
			t.Errorf("%s says nothing about whether it destroys anything", tool.Name)
		}
	}

	status := callTool(t, s, "get_status", nil)
	token, _ := status["token"].(map[string]any)
	if token["name"] != "assistant" {
		t.Errorf("get_status names the token %v", token["name"])
	}

	added := callTool(t, s, "add_links", map[string]any{
		"links": []string{"see https://host.example/a.mkv and https://host.example/b.mkv"}, "package": "Show",
	})
	if added["added"] != float64(2) {
		t.Fatalf("add_links added %v, want the two links in the text", added["added"])
	}
	collector := callTool(t, s, "list_collector", map[string]any{"search": "show"})
	shown, _ := collector["shown"].([]any)
	if len(shown) != 1 || shown[0].(map[string]any)["count"] != float64(2) {
		t.Fatalf("list_collector = %v, want the package with both links", collector)
	}
	if downloads := callTool(t, s, "list_downloads", nil); downloads["total"] != float64(0) {
		t.Errorf("list_downloads counts collected links: %v", downloads)
	}
}

func TestEveryToolRefusesATokenWithoutItsRight(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	for _, d := range mcpToolDefs(a) {
		var others []apitoken.Scope
		for _, s := range apitoken.AllScopes() {
			if s != d.scope {
				others = append(others, s)
			}
		}
		s := mcpSession(t, a, mcpClientToken(t, a, others...))
		res := callToolRaw(t, s, d.tool.Name, nil)
		if !res.IsError || !strings.HasPrefix(toolText(res), "not_permitted") ||
			!strings.Contains(toolText(res), string(d.scope)) {
			t.Errorf("%s without %q answered %q, want not_permitted naming the right", d.tool.Name, d.scope, toolText(res))
		}

		s = mcpSession(t, a, mcpClientToken(t, a, d.scope))
		if res := callToolRaw(t, s, d.tool.Name, nil); strings.HasPrefix(toolText(res), "not_permitted") {
			t.Errorf("%s refused a token that holds %q: %s", d.tool.Name, d.scope, toolText(res))
		}
		for _, c := range a.MCPClients.List() {
			if _, err := a.MCPClients.Remove(c.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestTheMCPToolsActOnTheQueue(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	s := mcpSession(t, a, mcpClientToken(t, a, apitoken.ScopeRead, apitoken.ScopeAdd, apitoken.ScopeControl))
	callTool(t, s, "set_queue", map[string]any{"running": false})
	if !a.Queue().Halted {
		t.Fatal("set_queue running:false left the queue running")
	}

	a.AddLinks([]string{"https://host.example/one.mkv", "https://host.example/two.mkv"}, "Pack")
	started := callTool(t, s, "start_collector", map[string]any{"packages": []string{"Pack"}})
	if started["started"] != float64(2) {
		t.Fatalf("start_collector = %v, want both links of the package", started)
	}
	if started["queue"].(map[string]any)["running"] != false {
		t.Error("start_collector released a halted queue")
	}

	var ids []string
	for _, task := range a.Tasks() {
		if task.Status != core.StatusQueued {
			t.Fatalf("%s is %s after the start", task.Name, task.Status)
		}
		ids = append(ids, task.ID)
	}
	if got := callTool(t, s, "pause_downloads", map[string]any{"ids": ids}); got["changed"] != float64(2) {
		t.Errorf("pause_downloads = %v", got)
	}
	if got := callTool(t, s, "list_downloads", map[string]any{"state": "paused"}); got["total"] != float64(2) {
		t.Errorf("list_downloads state paused = %v", got)
	}
	if got := callTool(t, s, "resume_downloads", map[string]any{"ids": ids}); got["changed"] != float64(2) {
		t.Errorf("resume_downloads = %v", got)
	}
	if got := callTool(t, s, "remove_downloads", map[string]any{"ids": ids[:1]}); got["removed"] != float64(1) {
		t.Errorf("remove_downloads = %v", got)
	}
	if n := len(a.Tasks()); n != 1 {
		t.Errorf("%d tasks left after removing one of two", n)
	}

	res := callToolRaw(t, s, "remove_downloads", map[string]any{"ids": ids, "files": true})
	if !res.IsError || !strings.HasPrefix(toolText(res), "invalid_arguments") {
		t.Errorf("remove_downloads took a field it does not have: %s", toolText(res))
	}
}

func TestTheMCPToolsKeepCredentialsFromTheAssistant(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	s := mcpSession(t, a, mcpClientToken(t, a, apitoken.AllScopes()...))
	callTool(t, s, "add_links", map[string]any{"links": []string{"https://jane:hunter2@host.example/file.bin?apikey=s3cr3t"}})
	for _, tool := range []string{"list_collector", "list_downloads"} {
		res := callToolRaw(t, s, tool, nil)
		if text := toolText(res); strings.Contains(text, "hunter2") || strings.Contains(text, "s3cr3t") {
			t.Errorf("%s hands a credential to the assistant, even on a token with admin: %s", tool, text)
		}
	}
}

func TestAddContainerOpensATextListHere(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	s := mcpSession(t, a, mcpClientToken(t, a, apitoken.ScopeAdd))
	list := "https://host.example/one.rar\nhttps://host.example/two.rar\n"
	got := callTool(t, s, "add_container", map[string]any{
		"fileName": "links.txt", "content": base64.StdEncoding.EncodeToString([]byte(list)), "package": "Box",
	})
	if got["added"] != float64(2) {
		t.Fatalf("add_container = %v, want both links", got)
	}
	for _, task := range a.Tasks() {
		if task.Package != "Box" || task.Status != core.StatusCollected {
			t.Errorf("%s landed in %q as %s", task.Name, task.Package, task.Status)
		}
	}
}

func TestSettingUpAClientIssuesItsTokenAndRemovingItRevokesIt(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/mcp/clients", "application/json",
		strings.NewReader(`{"kind":"cursor","name":"Cursor","scopes":["read","add"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var made struct {
		Client mcpClientView `json:"client"`
		Secret string        `json:"secret"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&made)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || made.Secret == "" || made.Client.TokenName != "Cursor" ||
		!slices.Equal(made.Client.Scopes, []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdd}) {
		t.Fatalf("setting up a client answered %d %+v", resp.StatusCode, made)
	}
	if code, _ := postMCP(t, srv, mcpInitialize, bearerHeader(made.Secret)); code != http.StatusOK {
		t.Fatalf("the new client's token got %d at /mcp", code)
	}

	overview := mcpOverview(a)
	if len(overview.Clients) != 1 || overview.Clients[0].LastCall == nil {
		t.Errorf("the overview does not show the client's call: %+v", overview.Clients)
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/mcp/clients/"+made.Client.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("removing the client answered %d", resp.StatusCode)
	}
	if _, ok := a.APITokens.Check(made.Secret); ok {
		t.Error("the client's token still works after the client was removed")
	}
	if code, _ := postMCP(t, srv, mcpInitialize, bearerHeader(made.Secret)); code != http.StatusNotFound {
		t.Errorf("/mcp answered %d after the last client went, want 404", code)
	}
}

func TestAClientWhoseTokenWasRevokedIsShownLockedOut(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	mcpClientToken(t, a, apitoken.ScopeRead)
	if err := a.APITokens.RevokeAll(); err != nil {
		t.Fatal(err)
	}
	if c := mcpOverview(a).Clients; len(c) != 1 || !c[0].TokenGone {
		t.Errorf("overview after a password change = %+v, want the client marked as locked out", c)
	}
}
