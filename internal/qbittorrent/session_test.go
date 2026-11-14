package qbittorrent

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/qbittest"
)

func fake(t *testing.T) (*qbittest.Server, *Client) {
	t.Helper()
	q := qbittest.New("knightloader", "pw")
	srv := httptest.NewServer(q)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "knightloader", "pw")
	if err != nil {
		t.Fatal(err)
	}
	return q, c
}

func TestASessionThatEndedIsOpenedAgainOnce(t *testing.T) {
	q, c := fake(t)
	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatal(err)
	}
	q.ForgetSessions()
	v, err := c.Version(ctx)
	if err != nil || v != "v4.5.5" {
		t.Fatalf("Version = %q, %v", v, err)
	}
	if q.Logins() != 2 {
		t.Fatalf("%d logins, want 2", q.Logins())
	}
}

func TestACallWithoutASessionLogsInFirst(t *testing.T) {
	q, c := fake(t)
	if _, err := c.Version(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.Logins() != 1 {
		t.Fatalf("%d logins", q.Logins())
	}
}

func TestAnAPIKeyNeedsNoLogin(t *testing.T) {
	q, c := fake(t)
	q.APIKey = "qbt_key"
	q.Password = "not this one"
	c.SetAPIKey("qbt_key")
	if _, err := c.Version(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.Logins() != 0 {
		t.Fatalf("%d logins", q.Logins())
	}
}

func TestPreferencesAreSetAsOneJSONObject(t *testing.T) {
	q, c := fake(t)
	err := c.SetPreferences(context.Background(), map[string]any{"rss_processing_enabled": false, "listen_port": 6881})
	if err != nil {
		t.Fatal(err)
	}
	p := q.Preferences()
	if p["rss_processing_enabled"] != false || p["listen_port"] != float64(6881) {
		t.Fatalf("preferences %v", p)
	}
}

func TestShutdownAsksQBittorrentToQuit(t *testing.T) {
	q, c := fake(t)
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !q.ShutDown() {
		t.Fatal("app/shutdown was not called")
	}
}

func TestAWrongPasswordIsNotTriedAgainAndAgain(t *testing.T) {
	q, c := fake(t)
	q.Password = "changed"
	if _, err := c.Version(context.Background()); reason(err) != "qBittorrent refused the username or password" {
		t.Fatalf("err = %v", err)
	}
}

func TestPreferencesReadBackWhatWasSet(t *testing.T) {
	_, c := fake(t)
	ctx := context.Background()
	if err := c.SetPreferences(ctx, map[string]any{"dht": false}); err != nil {
		t.Fatal(err)
	}
	p, err := c.Preferences(ctx)
	if err != nil || p["dht"] != false {
		t.Fatalf("Preferences = %v, %v", p, err)
	}
}
