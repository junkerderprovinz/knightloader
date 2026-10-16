package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

const plexToken = "SECRET-plex-9qk2-no-response-may-carry-this"

func playbackServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerPlayback(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

type playbackListingBody struct {
	Servers []struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Host     string `json:"host"`
		HasToken bool   `json:"hasToken"`
	} `json:"servers"`
	Kinds []string `json:"kinds"`
}

func getPlayback(t *testing.T, srv *httptest.Server) (playbackListingBody, string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/api/playback")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out playbackListingBody
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("GET /api/playback answered %d with %s", resp.StatusCode, raw)
	}
	return out, string(raw)
}

func TestAMediaServerIsStoredWithItsTokenSealed(t *testing.T) {
	t.Parallel()
	a, srv := playbackServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/playback/servers",
		map[string]any{"kind": "plex", "url": "http://192.168.1.10:32400", "token": plexToken})
	if code != http.StatusOK {
		t.Fatalf("POST answered %d: %s", code, raw)
	}
	if strings.Contains(string(raw), plexToken) {
		t.Fatalf("the token came back in the answer: %s", raw)
	}
	list, body := getPlayback(t, srv)
	if strings.Contains(body, plexToken) {
		t.Fatalf("the token is in the listing: %s", body)
	}
	if len(list.Servers) != 1 || list.Servers[0].ID != "plex" || !list.Servers[0].HasToken || list.Servers[0].Host != "192.168.1.10:32400" {
		t.Fatalf("listing = %+v", list.Servers)
	}
	if got := a.PlaybackStore().Token("plex"); got != plexToken {
		t.Errorf("sealed token = %q", got)
	}

	// An edit with the placeholder keeps the token.
	code, raw = postJSON(t, http.MethodPost, srv.URL+"/api/playback/servers",
		map[string]any{"id": "plex", "name": "Living room", "kind": "plex", "url": "http://192.168.1.10:32400", "token": accounts.Redacted})
	if code != http.StatusOK {
		t.Fatalf("edit answered %d: %s", code, raw)
	}
	if got := a.PlaybackStore().Token("plex"); got != plexToken {
		t.Errorf("the placeholder replaced the token with %q", got)
	}

	code, raw = postJSON(t, http.MethodDelete, srv.URL+"/api/playback/servers/plex", nil)
	if code != http.StatusNoContent {
		t.Fatalf("DELETE answered %d: %s", code, raw)
	}
	if a.PlaybackStore().Has("plex") {
		t.Error("the token outlived its server")
	}
	if list, _ := getPlayback(t, srv); len(list.Servers) != 0 {
		t.Errorf("the server is still listed: %+v", list.Servers)
	}
}

func TestASecondServerOfOneKindGetsItsOwnID(t *testing.T) {
	t.Parallel()
	_, srv := playbackServer(t)
	for range 2 {
		if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/playback/servers",
			map[string]any{"kind": "jellyfin", "url": "http://10.0.0.5:8096"}); code != http.StatusOK {
			t.Fatalf("POST answered %d: %s", code, raw)
		}
	}
	list, _ := getPlayback(t, srv)
	if len(list.Servers) != 2 || list.Servers[0].ID != "jellyfin" || list.Servers[1].ID != "jellyfin-2" {
		t.Fatalf("listing = %+v", list.Servers)
	}
}

func TestAnAddressWithATokenInItsQueryIsRefused(t *testing.T) {
	t.Parallel()
	a, srv := playbackServer(t)
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/playback/servers",
		map[string]any{"kind": "plex", "url": "http://10.0.0.5:32400/?X-Plex-Token=" + plexToken})
	if code != http.StatusBadRequest {
		t.Fatalf("POST answered %d: %s", code, raw)
	}
	if strings.Contains(string(raw), plexToken) {
		t.Fatalf("the refusal quotes the token: %s", raw)
	}
	if len(a.Settings.Get().PlaybackServers) != 0 {
		t.Error("the refused row was stored")
	}
}

func TestTheTestQuestionReportsWhatPlays(t *testing.T) {
	t.Parallel()
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != plexToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[{"Id":"s","UserName":"anna","NowPlayingItem":{"Name":"A Film"},"PlayState":{}}]`))
	}))
	defer media.Close()
	_, srv := playbackServer(t)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/playback/servers",
		map[string]any{"kind": "emby", "url": media.URL, "token": plexToken}); code != http.StatusOK {
		t.Fatalf("POST answered %d: %s", code, raw)
	}
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/playback/servers/emby/test", nil)
	if code != http.StatusOK {
		t.Fatalf("test answered %d: %s", code, raw)
	}
	var out struct {
		OK      bool `json:"ok"`
		Streams []struct {
			Title string `json:"title"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Streams) != 1 || out.Streams[0].Title != "A Film" {
		t.Fatalf("test answer = %s", raw)
	}
}
