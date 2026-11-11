package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/fortressmcp"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

func fortressRun(t *testing.T, a fortress.Action, body string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := a.Run(ctx, fortress.Caller{ID: "bv1", App: "bombvault", Device: "Tower"}, []byte(body))
	if err != nil {
		t.Fatalf("%s: %v", body, err)
	}
	return out
}

func TestTheFortressCallsOnlyTheToolsThatAddAndSteer(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	act := fortressToolsAction(a)
	if act.Name != fortressmcp.ActAction {
		t.Fatalf("the action is %q, want %q", act.Name, fortressmcp.ActAction)
	}
	var list struct{ Tools []mcp.Tool }
	if err := json.Unmarshal(fortressRun(t, act, ""), &list); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := []string{"add_links", "pause_downloads", "resume_downloads", "set_queue", "start_collector"}
	if !slices.Equal(names, want) {
		t.Fatalf("the fortress sees %v, want %v", names, want)
	}

	var res mcp.CallToolResult
	body := `{"tool":"add_links","arguments":{"links":["https://host.example/a.mkv"],"package":"Show"}}`
	if err := json.Unmarshal(fortressRun(t, act, body), &res); err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("add_links through the fortress: %s", toolText(&res))
	}
	if n := collected(a); n != 1 {
		t.Fatalf("the collector holds %d links after add_links through the fortress, want 1", n)
	}
}

func TestOnlyAClientThatMayControlCallsTheToolsOfOtherApps(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	cfg := fortressMCP(a)
	read := withMCPCaller(context.Background(), mcpCaller{token: apitoken.Token{Scopes: []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdd}}})
	control := withMCPCaller(context.Background(), mcpCaller{token: apitoken.Token{Scopes: []apitoken.Scope{apitoken.ScopeControl}}})
	if cfg.MayAct(read) {
		t.Error("a client without control may call the tools of other apps")
	}
	if !cfg.MayAct(control) {
		t.Error("a client with control may not call the tools of other apps")
	}
	if cfg.Fortress() != nil {
		t.Error("a fortress shows up before KnightLoader joined one")
	}
}

// backupIn opens an app of the fortress secret makes that offers the tool
// start_backup to the others, and returns its fortress.
func backupIn(t *testing.T, relayURL string, secret []byte) *fortress.Fortress {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "bombvault", Version: "1.0.0"}, nil)
	srv.AddTool(&mcp.Tool{Name: "start_backup", Description: "Backs up one item now.", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"started":true}`}}}, nil
		})
	seed := sha256.Sum256([]byte("bombvault"))
	id, err := member.FromSeed(member.KindInstance, seed[:])
	if err != nil {
		t.Fatal(err)
	}
	f, err := fortress.Open(fortress.Config{
		App:      fortress.App{ID: "bombvault", Name: "BombVault", Version: "1.0.0"},
		Secret:   secret,
		Identity: id,
		Self:     group.Self{Name: "Tower", Deployment: "container"},
		Relay:    group.RelayOwn,
		RelayURL: relayURL,
		Store:    &memoryStore{},
		Actions: []fortress.Action{fortressmcp.Offer(srv, []string{"start_backup"},
			func(ctx context.Context, _ fortress.Caller) (context.Context, error) { return ctx, nil })},
		Uses: []string{fortressmcp.ActAction},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestTheMCPServerReachesTheOtherAppsOfTheFortress(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	bv := backupIn(t, relayURL, secret)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	waitFor(t, "each to see the other offer its tools", func() bool {
		return offersTools(bv.Members(), "knightloader") && offersTools(fortressMembers(a), "bombvault")
	})

	s := mcpSession(t, a, mcpClientToken(t, a, apitoken.ScopeRead, apitoken.ScopeControl))
	members := callTool(t, s, "fortress_members", nil)
	if list, _ := members["members"].([]any); len(list) != 1 || list[0].(map[string]any)["app"] != "bombvault" {
		t.Fatalf("fortress_members reads %v", members)
	}
	if started := callTool(t, s, "bombvault_start_backup", nil); started["started"] != true {
		t.Fatalf("bombvault_start_backup answered %v", started)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var kl string
	for _, m := range bv.Members() {
		if m.App.ID == "knightloader" {
			kl = m.ID
		}
	}
	body := `{"tool":"add_links","arguments":{"links":["https://host.example/b.mkv"]}}`
	if _, err := bv.Act(ctx, kl, fortressmcp.ActAction, []byte(body)); err != nil {
		t.Fatalf("the other app calls add_links here: %v", err)
	}
	if n := collected(a); n != 1 {
		t.Fatalf("the collector holds %d links, want the one the other app added", n)
	}
}

func offersTools(ms []fortress.Member, app string) bool {
	return slices.ContainsFunc(ms, func(m fortress.Member) bool {
		return m.App.ID == app && m.Online() && slices.Contains(m.Actions, fortressmcp.ActAction)
	})
}

func fortressMembers(a *app.App) []fortress.Member {
	f, _ := a.Fortress().(fortressmcp.Fortress)
	if f == nil {
		return nil
	}
	return f.Members()
}

func collected(a *app.App) int {
	n := 0
	for _, t := range a.Tasks() {
		if t.Status == core.StatusCollected {
			n++
		}
	}
	return n
}
