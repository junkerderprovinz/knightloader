package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The page never holds a stored indexer key, so a test sends the mask and
// the server asks with the key it stores, but only at the address the key
// was stored for.
func TestAnIndexerTestAsksWithTheStoredKey(t *testing.T) {
	t.Parallel()
	ix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "idx-key" {
			fmt.Fprint(w, `<error code="100" description="Incorrect user credentials"/>`)
			return
		}
		fmt.Fprint(w, `<caps><searching/></caps>`)
	}))
	t.Cleanup(ix.Close)
	a, srv := torrentsServer(t)
	s := a.Settings.Get()
	s.Torrent.CrossSeed.Indexers = []settings.TorznabIndexer{{Name: "Prowlarr", URL: ix.URL + "/1/api", APIKey: "idx-key"}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	route := srv.URL + "/api/torrents/crossseed/test"
	for _, c := range []struct{ url, want string }{
		{ix.URL + "/1/api", `{"ok":true}`},
		{strings.Replace(ix.URL, "127.0.0.1", "localhost", 1) + "/1/api", "Incorrect user credentials"},
	} {
		body := map[string]any{"what": "indexer", "index": 0, "crossSeed": map[string]any{
			"indexers": []map[string]string{{"name": "Prowlarr", "url": c.url, "apiKey": "********"}},
		}}
		code, got := postJSON(t, http.MethodPost, route, body)
		if code != http.StatusOK || !strings.Contains(string(got), c.want) {
			t.Errorf("testing %s = %d %s, want %s", c.url, code, got, c.want)
		}
	}
}

func TestTheCrossSeedLogComesNewestFirst(t *testing.T) {
	t.Parallel()
	a, srv := torrentsServer(t)
	route := srv.URL + "/api/torrents/crossseed/log?limit=2"
	if got := strings.TrimSpace(getBody(t, route)); got != `{"entries":[]}` {
		t.Fatalf("an empty log reads %s", got)
	}
	lines := `{"time":"2026-05-01T12:00:00Z","tracker":"OPS","name":"one","infoHash":"aa","result":"injected"}
{"time":"2026-05-01T12:01:00Z","tracker":"OPS","name":"two","infoHash":"bb","result":"refused","reason":"no"}
{"time":"2026-05-01T12:02:00Z","tracker":"RED","name":"three","infoHash":"cc","result":"injected"}
`
	if err := os.WriteFile(filepath.Join(a.DataDir, "cross-seed.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Entries []struct {
			Name, Result string
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(getBody(t, route)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 2 || out.Entries[0].Name != "three" || out.Entries[1].Name != "two" {
		t.Fatalf("the log reads %+v", out.Entries)
	}
}
