package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
)

// TestANarrowTokenDoesNotLearnTheIdleProgram: the settings hide the
// end-of-queue program from everybody but an administrator, so the last run
// must not name it to a token that may only read, neither over GET nor over
// the live stream.
func TestANarrowTokenDoesNotLearnTheIdleProgram(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, read, err := a.APITokens.CreateScoped("dashboard", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	_, admin, err := a.APITokens.CreateScoped("owner", []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdmin})
	if err != nil {
		t.Fatal(err)
	}
	spec := idleaction.CommandSpec{Program: notARealProgram, TimeoutSeconds: 30}
	a.RunIdleCommandNow(spec)

	get := func(token string) string {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/idle-action", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if body := get(read); strings.Contains(body, notARealProgram) || !strings.Contains(body, idleaction.RedactedCommand) {
		t.Errorf("GET /api/idle-action with a read token = %s, want the program redacted", body)
	}
	if body := get(admin); !strings.Contains(body, notARealProgram) {
		t.Errorf("GET /api/idle-action with an admin token = %s, want the program named", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv.URL+"/api/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + read}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	// The snapshot arrives once the stream is registered; a run broadcast
	// after it reaches this connection.
	readOneOfType(t, c, "snapshot")
	a.RunIdleCommandNow(spec)
	msg := readOneOfType(t, c, "idleAction")
	run, _ := msg["data"].(map[string]any)["lastRun"].(map[string]any)
	if run["program"] != idleaction.RedactedCommand {
		t.Errorf("the stream told a read token program = %v, want %q", run["program"], idleaction.RedactedCommand)
	}
}
