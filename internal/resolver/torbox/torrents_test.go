package torbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

const tbMagnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Show"

// engineStub finishes every file at once and records what it was handed.
type engineStub struct {
	mu  sync.Mutex
	got []string
}

func (e *engineStub) FetchPart(_ context.Context, p debrid.Part) (string, error) {
	e.mu.Lock()
	e.got = append(e.got, p.URL+" -> "+p.Path)
	e.mu.Unlock()
	return "", nil
}
func (e *engineStub) Pause(string)        {}
func (e *engineStub) Resume(string)       {}
func (e *engineStub) Remove(string, bool) {}

func (e *engineStub) parts() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.got)
}

// runTorBox sends one magnet through a torrent backend for c and waits for the
// task to settle.
func runTorBox(t *testing.T, c *Client) (*engineStub, core.Update, []core.Update) {
	t.Helper()
	eng := &engineStub{}
	var mu sync.Mutex
	var all []core.Update
	settled := make(chan core.Update, 1)
	b := debrid.NewTorrentBackend(NewTorrents(c), NewBackend(c, nil, nil), eng, debrid.NewRuns("torbox"), func(_ string, u core.Update) {
		mu.Lock()
		all = append(all, u)
		mu.Unlock()
		if u.Status == core.StatusDone || u.Status == core.StatusError {
			settled <- u
		}
	})
	b.Download("t1", tbMagnet, nil, 1)
	select {
	case u := <-settled:
		mu.Lock()
		defer mu.Unlock()
		return eng, u, slices.Clone(all)
	case <-time.After(30 * time.Second):
		t.Fatal("the torrent never settled")
	}
	return nil, core.Update{}, nil
}

func TestTorBoxFetchesAMagnetFromAddToCleanup(t *testing.T) {
	var (
		mu      sync.Mutex
		reads   int
		deleted = make(chan map[string]any, 1)
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/torrents/createtorrent":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("createtorrent: %v", err)
			}
			if r.FormValue("magnet") != tbMagnet || r.FormValue("allow_zip") != "false" {
				t.Errorf("createtorrent got magnet %q, allow_zip %q", r.FormValue("magnet"), r.FormValue("allow_zip"))
			}
			fmt.Fprint(w, `{"success":true,"data":{"torrent_id":7,"hash":"0123"}}`)
		case "/api/torrents/mylist":
			id := r.URL.Query().Get("id")
			if id == "" {
				fmt.Fprint(w, `{"success":true,"data":[]}`)
				return
			}
			if id != "7" {
				t.Errorf("mylist asked for %q", r.URL.RawQuery)
			}
			reads++
			if reads == 1 {
				fmt.Fprint(w, `{"success":true,"data":{"id":7,"name":"Show","size":730,"download_state":"downloading","progress":0.3,"download_speed":2000,"seeds":9,"files":[]}}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"data":{"id":7,"name":"Show","size":730,"download_state":"cached","download_present":true,"files":[`+
				`{"id":0,"name":"Show/e01.mkv","size":700},{"id":1,"name":"Show/extras/sample.mkv","size":30}]}}`)
		case "/api/torrents/requestdl":
			q := r.URL.Query()
			if q.Get("token") != "k" || q.Get("torrent_id") != "7" {
				t.Errorf("requestdl got %q", r.URL.RawQuery)
			}
			fmt.Fprintf(w, `{"success":true,"data":"https://cdn.torbox.example/%s"}`, q.Get("file_id"))
		case "/api/torrents/controltorrent":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			deleted <- body
			fmt.Fprint(w, `{"success":true,"data":null}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := NewClient("k")
	c.base = srv.URL

	eng, done, all := runTorBox(t, c)

	if done.Status != core.StatusDone {
		t.Fatalf("settled as %+v", done)
	}
	want := []string{"https://cdn.torbox.example/0 -> Show/e01.mkv", "https://cdn.torbox.example/1 -> Show/extras/sample.mkv"}
	if got := eng.parts(); !slices.Equal(got, want) {
		t.Errorf("the engine got %v, want %v", got, want)
	}
	if !slices.ContainsFunc(all, func(u core.Update) bool { return u.Remote != nil && u.Remote.Progress == 0.3 && u.Remote.Seeds == 9 }) {
		t.Error("TorBox's own progress never reached the task")
	}
	select {
	case body := <-deleted:
		if body["operation"] != "delete" || body["torrent_id"] != float64(7) {
			t.Errorf("controltorrent got %v", body)
		}
	case <-time.After(5 * time.Second):
		t.Error("the finished torrent was not deleted on TorBox")
	}
}

func TestTorBoxDecliningATorrentHandsItOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/torrents/mylist" {
			fmt.Fprint(w, `{"success":true,"data":[]}`)
			return
		}
		fmt.Fprint(w, `{"success":false,"error":"DOWNLOAD_TOO_LARGE","detail":"This download is larger than your plan allows."}`)
	}))
	defer srv.Close()
	c := NewClient("k")
	c.base = srv.URL

	_, u, _ := runTorBox(t, c)
	if !u.Unsupported || !strings.Contains(u.Err, "DOWNLOAD_TOO_LARGE") {
		t.Errorf("got %+v, want the task handed on with TorBox's reason", u)
	}
}

func TestTorBoxRejectingTheKeyIsNoRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":false,"error":"BAD_TOKEN","detail":"Invalid API key."}`)
	}))
	defer srv.Close()
	c := NewClient("k")
	c.base = srv.URL

	_, u, _ := runTorBox(t, c)
	if u.Unsupported {
		t.Error("a rejected key handed the torrent on as if TorBox had declined it; the account is at fault")
	}
}

// The torrent the account holds is the user's, or another app's, and stays
// there when its files are here.
func TestTorBoxReusesATorrentTheAccountAlreadyHoldsAndLeavesItThere(t *testing.T) {
	deleted := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/torrents/controltorrent":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			deleted <- fmt.Sprint(body["torrent_id"])
			fmt.Fprint(w, `{"success":true,"data":null}`)
		case r.URL.Path == "/api/torrents/createtorrent":
			fmt.Fprint(w, `{"success":false,"error":"DUPLICATE_ITEM","detail":"You already have this torrent."}`)
		case r.URL.Path == "/api/torrents/mylist" && r.URL.Query().Get("id") == "":
			fmt.Fprint(w, `{"success":true,"data":[{"id":3,"hash":"ffff"},{"id":12,"hash":"0123456789ABCDEF0123456789ABCDEF01234567"}]}`)
		case r.URL.Path == "/api/torrents/mylist":
			if r.URL.Query().Get("id") != "12" {
				t.Errorf("polled torrent %s, want the one the account holds", r.URL.Query().Get("id"))
			}
			fmt.Fprint(w, `{"success":true,"data":{"id":12,"name":"Show","download_present":true,"files":[{"id":0,"name":"Show.mkv","size":5}]}}`)
		case r.URL.Path == "/api/torrents/requestdl":
			fmt.Fprint(w, `{"success":true,"data":"https://cdn.torbox.example/x"}`)
		default:
			fmt.Fprint(w, `{"success":true,"data":null}`)
		}
	}))
	defer srv.Close()
	c := NewClient("k")
	c.base = srv.URL

	eng, u, _ := runTorBox(t, c)
	if u.Status != core.StatusDone {
		t.Fatalf("settled as %+v", u)
	}
	if got := eng.parts(); !slices.Equal(got, []string{"https://cdn.torbox.example/x -> Show.mkv"}) {
		t.Errorf("the engine got %v", got)
	}
	select {
	case id := <-deleted:
		t.Errorf("deleted torrent %s, which was on the account before this task asked for it", id)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestTheTorBoxResolverClaimsTorrentsOnlyWhenAsked(t *testing.T) {
	if (Resolver{}).Match(tbMagnet) {
		t.Error("a TorBox entry claimed a magnet without being asked to take torrents")
	}
	if !(Resolver{Torrents: true}).Match(tbMagnet) {
		t.Error("a TorBox entry that takes torrents does not claim a magnet")
	}
}
