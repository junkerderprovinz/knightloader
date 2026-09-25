package debrid

// Each service's torrent API against a fake of it, driven through
// TorrentBackend from the add to the cleanup: the job is added, polled until
// the service has the files, the files are unlocked and handed to the engine,
// and the job is deleted. A refusal hands the task on.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// calls records the requests a fake service saw, as "METHOD /path".
type calls struct {
	mu  sync.Mutex
	got []string
}

func (c *calls) add(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, r.Method+" "+r.URL.Path)
}

func (c *calls) saw(call string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Contains(c.got, call)
}

// fetchTorrent runs one link through a backend for svc and returns what the
// engine was handed and how the task ended.
func fetchTorrent(t *testing.T, svc TorrentService, link string, sel []core.TorrentFile, settle core.Status) ([]Part, core.Update) {
	t.Helper()
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)
	b.Files = func(string) []core.TorrentFile { return sel }
	b.Download("t1", link, nil, 2)
	settled := up.until(t, settle)
	return parts.parts(), settled
}

func partURLs(ps []Part) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.URL + " -> " + p.Path
	}
	return out
}

func TestRealDebridFetchesAMagnetFromAddToCleanup(t *testing.T) {
	var (
		seen     calls
		mu       sync.Mutex
		reads    int
		selected string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("%s went out without the token", r.URL.Path)
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST /torrents/addMagnet":
			if r.FormValue("magnet") != testMagnet {
				t.Errorf("magnet = %q", r.FormValue("magnet"))
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":"RD1","uri":"https://api.real-debrid.com/rest/1.0/torrents/info/RD1"}`)
		case "GET /torrents/info/RD1":
			reads++
			files := `[{"id":1,"path":"/e01.mkv","bytes":700,"selected":%d},{"id":2,"path":"/extras/sample.mkv","bytes":30,"selected":%d}]`
			switch {
			case reads == 1:
				fmt.Fprint(w, `{"filename":"Show","status":"magnet_conversion","files":[],"links":[]}`)
			case selected == "":
				fmt.Fprintf(w, `{"filename":"Show","bytes":730,"status":"waiting_files_selection","files":`+files+`,"links":[]}`, 0, 0)
			case reads < 5:
				fmt.Fprintf(w, `{"filename":"Show","bytes":730,"status":"downloading","progress":50,"speed":1000,"seeders":7,"files":`+files+`,"links":[]}`, 1, 1)
			default:
				fmt.Fprintf(w, `{"filename":"Show","bytes":730,"status":"downloaded","progress":100,"files":`+files+
					`,"links":["https://real-debrid.com/d/A","https://real-debrid.com/d/B"]}`, 1, 1)
			}
		case "POST /torrents/selectFiles/RD1":
			selected = r.FormValue("files")
			w.WriteHeader(http.StatusNoContent)
		case "POST /unrestrict/link":
			l := r.FormValue("link")
			fmt.Fprintf(w, `{"download":"https://dl.example/%s","filename":"x","filesize":1}`, l[strings.LastIndex(l, "/")+1:])
		case "DELETE /torrents/delete/RD1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	rd := NewRealDebrid("tok")
	rd.base = srv.URL

	parts, done := fetchTorrent(t, rd, testMagnet, nil, core.StatusDone)

	want := []string{"https://dl.example/A -> Show/e01.mkv", "https://dl.example/B -> Show/extras/sample.mkv"}
	if got := partURLs(parts); !slices.Equal(got, want) {
		t.Errorf("the engine got %v, want %v", got, want)
	}
	if selected != "1,2" {
		t.Errorf("selectFiles got %q, want every file", selected)
	}
	if done.Loaded != 730 {
		t.Errorf("done with %d bytes", done.Loaded)
	}
	waitFor(t, func() bool { return seen.saw("DELETE /torrents/delete/RD1") }, "the torrent deleted on Real-Debrid")
}

func TestRealDebridGetsTheSelectionOfAnUploadedTorrent(t *testing.T) {
	var (
		mu       sync.Mutex
		body     []byte
		selected string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "PUT /torrents/addTorrent":
			body, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":"RD2"}`)
		case "GET /torrents/info/RD2":
			files := `[{"id":1,"path":"/Pack/a.mkv","bytes":64,"selected":%d},{"id":2,"path":"/Pack/b.mkv","bytes":64,"selected":%d}]`
			if selected == "" {
				fmt.Fprintf(w, `{"filename":"Pack","status":"waiting_files_selection","files":`+files+`}`, 0, 0)
				return
			}
			fmt.Fprintf(w, `{"filename":"Pack","status":"downloaded","files":`+files+`,"links":["https://real-debrid.com/d/B"]}`, 0, 1)
		case "POST /torrents/selectFiles/RD2":
			selected = r.FormValue("files")
			w.WriteHeader(http.StatusNoContent)
		case "POST /unrestrict/link":
			fmt.Fprint(w, `{"download":"https://dl.example/B"}`)
		case "DELETE /torrents/delete/RD2":
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	rd := NewRealDebrid("tok")
	rd.base = srv.URL
	upload, raw := builtTorrent(t, false)

	parts, _ := fetchTorrent(t, rd, upload, []core.TorrentFile{
		{Path: "a.mkv", Size: 64}, {Path: "b.mkv", Size: 64, Selected: true},
	}, core.StatusDone)

	if selected != "2" {
		t.Errorf("selectFiles got %q, want only the file picked in the tree", selected)
	}
	if string(body) != string(raw) {
		t.Error("the .torrent Real-Debrid got is not the one uploaded")
	}
	if got := partURLs(parts); !slices.Equal(got, []string{"https://dl.example/B -> Pack/b.mkv"}) {
		t.Errorf("the engine got %v", got)
	}
}

func TestRealDebridDecliningATorrentHandsItOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"too_many_active_downloads","error_code":21}`)
	}))
	defer srv.Close()
	rd := NewRealDebrid("tok")
	rd.base = srv.URL

	_, u := fetchTorrent(t, rd, testMagnet, nil, core.StatusError)
	if !u.Unsupported || u.Err != "realdebrid: too many active downloads" {
		t.Errorf("got %+v, want the task handed on with Real-Debrid's reason", u)
	}
}

func TestRealDebridRejectingTheTokenIsNoRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"bad_token","error_code":8}`)
	}))
	defer srv.Close()
	rd := NewRealDebrid("tok")
	rd.base = srv.URL

	_, u := fetchTorrent(t, rd, testMagnet, nil, core.StatusError)
	if u.Unsupported {
		t.Error("a rejected token handed the torrent on as if Real-Debrid had declined it; the account is at fault")
	}
}

func TestAllDebridFetchesAMagnetFromAddToCleanup(t *testing.T) {
	var (
		seen  calls
		mu    sync.Mutex
		reads int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		mu.Lock()
		defer mu.Unlock()
		ok := func(data string) { fmt.Fprintf(w, `{"status":"success","data":%s}`, data) }
		switch r.URL.Path {
		case "/v4/magnet/upload":
			if r.FormValue("magnets[]") != testMagnet {
				t.Errorf("magnets[] = %q", r.FormValue("magnets[]"))
			}
			ok(`{"magnets":[{"magnet":"x","id":55,"ready":false}]}`)
		case "/v4.1/magnet/status":
			reads++
			code := 1
			if reads > 2 {
				code = 4
			}
			ok(fmt.Sprintf(`{"magnets":{"id":55,"filename":"Show","size":730,"status":"x","statusCode":%d,"downloaded":365,"downloadSpeed":10,"seeders":2}}`, code))
		case "/v4/magnet/files":
			ok(`{"magnets":[{"id":"55","files":[{"n":"Show","e":[{"n":"e01.mkv","s":700,"l":"https://alldebrid.com/f/A"},` +
				`{"n":"extras","e":[{"n":"sample.mkv","s":30,"l":"https://alldebrid.com/f/B"}]}]}]}]}`)
		case "/v4/link/unlock":
			l := r.URL.Query().Get("link")
			ok(fmt.Sprintf(`{"link":"https://dl.example/%s","filename":"x"}`, l[strings.LastIndex(l, "/")+1:]))
		case "/v4/magnet/delete":
			if r.FormValue("id") != "55" {
				t.Errorf("deleted magnet %q", r.FormValue("id"))
			}
			ok(`{"message":"Magnet was successfully deleted"}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	ad := NewAllDebrid("key")
	ad.base = srv.URL + "/v4"

	parts, _ := fetchTorrent(t, ad, testMagnet, nil, core.StatusDone)

	want := []string{"https://dl.example/A -> Show/e01.mkv", "https://dl.example/B -> Show/extras/sample.mkv"}
	if got := partURLs(parts); !slices.Equal(got, want) {
		t.Errorf("the engine got %v, want %v", got, want)
	}
	waitFor(t, func() bool { return seen.saw("POST /v4/magnet/delete") }, "the magnet deleted on AllDebrid")
}

func TestAllDebridDecliningAMagnetHandsItOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"magnet":"x","error":{"code":"MAGNET_TOO_MANY_ACTIVE","message":"Already have maximum allowed active magnets (30)."}}]}}`)
	}))
	defer srv.Close()
	ad := NewAllDebrid("key")
	ad.base = srv.URL + "/v4"

	_, u := fetchTorrent(t, ad, testMagnet, nil, core.StatusError)
	if !u.Unsupported || !strings.Contains(u.Err, "MAGNET_TOO_MANY_ACTIVE") {
		t.Errorf("got %+v, want the task handed on with AllDebrid's reason", u)
	}
}

func TestPremiumizeFetchesAMagnetFromAddToCleanup(t *testing.T) {
	var (
		seen  calls
		mu    sync.Mutex
		reads int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST /api/transfer/create":
			if r.FormValue("src") != testMagnet {
				t.Errorf("src = %q", r.FormValue("src"))
			}
			fmt.Fprint(w, `{"status":"success","id":"T1","name":"Show","type":"torrent"}`)
		case "GET /api/transfer/list":
			reads++
			if reads < 3 {
				fmt.Fprint(w, `{"status":"success","transfers":[{"id":"T1","name":"Show","status":"running","progress":0.5}]}`)
				return
			}
			fmt.Fprint(w, `{"status":"success","transfers":[{"id":"T1","name":"Show","status":"finished","progress":1,"folder_id":"F1"}]}`)
		case "GET /api/folder/list":
			switch r.URL.Query().Get("id") {
			case "F1":
				fmt.Fprint(w, `{"status":"success","name":"Show","content":[{"id":"a","name":"e01.mkv","type":"file","size":700,"link":"https://pm.example/a"},{"id":"S","name":"extras","type":"folder"}]}`)
			case "S":
				fmt.Fprint(w, `{"status":"success","name":"extras","content":[{"id":"b","name":"sample.mkv","type":"file","size":30,"link":"https://pm.example/b"}]}`)
			}
		case "POST /api/transfer/delete", "POST /api/folder/delete":
			fmt.Fprint(w, `{"status":"success"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	pm := NewPremiumize("key")
	pm.base = srv.URL + "/api"

	parts, _ := fetchTorrent(t, pm, testMagnet, nil, core.StatusDone)

	want := []string{"https://pm.example/a -> Show/e01.mkv", "https://pm.example/b -> Show/extras/sample.mkv"}
	if got := partURLs(parts); !slices.Equal(got, want) {
		t.Errorf("the engine got %v, want %v", got, want)
	}
	waitFor(t, func() bool { return seen.saw("POST /api/folder/delete") }, "the transfer's folder deleted from the cloud")
	if !seen.saw("POST /api/transfer/delete") {
		t.Error("the transfer itself was left on Premiumize")
	}
}

func TestPremiumizeLeavesAFolderThatIsNotTheTransfersOwn(t *testing.T) {
	var seen calls
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		switch r.URL.Path {
		case "/api/transfer/list":
			fmt.Fprint(w, `{"status":"success","transfers":[{"id":"T1","name":"Show","status":"finished","folder_id":"ROOT"}]}`)
		case "/api/folder/list":
			fmt.Fprint(w, `{"status":"success","name":"My Files","content":[]}`)
		default:
			fmt.Fprint(w, `{"status":"success"}`)
		}
	}))
	defer srv.Close()
	pm := NewPremiumize("key")
	pm.base = srv.URL + "/api"

	if err := pm.DeleteTorrent(t.Context(), "T1"); err != nil {
		t.Fatal(err)
	}
	if seen.saw("POST /api/folder/delete") {
		t.Error("deleted a folder that is not named after the transfer, and everything in it")
	}
}

func TestPremiumizeDecliningATransferHandsItOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"error","message":"You already added this job."}`)
	}))
	defer srv.Close()
	pm := NewPremiumize("key")
	pm.base = srv.URL + "/api"

	_, u := fetchTorrent(t, pm, testMagnet, nil, core.StatusError)
	if !u.Unsupported || u.Err != "premiumize: You already added this job." {
		t.Errorf("got %+v, want the task handed on with Premiumize's reason", u)
	}
}

func TestDebridLinkFetchesAMagnetFromAddToCleanup(t *testing.T) {
	var (
		seen  calls
		mu    sync.Mutex
		reads int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST /seedbox/add":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["url"] != testMagnet || body["wait"] != false {
				t.Errorf("add got %v", body)
			}
			fmt.Fprint(w, `{"success":true,"value":{"id":"D1","name":"Show","status":1,"files":[]}}`)
		case "GET /seedbox/list":
			if r.URL.Query().Get("ids") != "D1" {
				t.Errorf("listed %q", r.URL.RawQuery)
			}
			reads++
			if reads < 3 {
				fmt.Fprint(w, `{"success":true,"value":[{"id":"D1","name":"Show","status":4,"totalSize":730,"downloadPercent":40,"downloadSpeed":900,"files":[]}]}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"value":[{"id":"D1","name":"Show","status":100,"totalSize":730,"downloadPercent":100,"files":[`+
				`{"id":"D1-1","name":"e01.mkv","size":700,"downloadUrl":"https://seed.example/1","downloadPercent":100},`+
				`{"id":"D1-2","name":"extras/sample.mkv","size":30,"downloadUrl":"https://seed.example/2","downloadPercent":100}]}]}`)
		case "DELETE /seedbox/D1/remove":
			fmt.Fprint(w, `{"success":true,"value":["D1"]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	dl := NewDebridLink("key")
	dl.base = srv.URL

	parts, _ := fetchTorrent(t, dl, testMagnet, nil, core.StatusDone)

	want := []string{"https://seed.example/1 -> Show/e01.mkv", "https://seed.example/2 -> Show/extras/sample.mkv"}
	if got := partURLs(parts); !slices.Equal(got, want) {
		t.Errorf("the engine got %v, want %v", got, want)
	}
	waitFor(t, func() bool { return seen.saw("DELETE /seedbox/D1/remove") }, "the torrent removed from the seedbox")
}

func TestDebridLinkGetsTheFilesToLeaveOut(t *testing.T) {
	var (
		mu       sync.Mutex
		unwanted []any
		configed bool
		wait     any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		files := `[{"id":"D2-1","name":"a.mkv","size":64,"downloadUrl":"https://seed.example/a","downloadPercent":%d},` +
			`{"id":"D2-2","name":"b.mkv","size":64,"downloadUrl":"https://seed.example/b","downloadPercent":%d}]`
		switch r.Method + " " + r.URL.Path {
		case "POST /seedbox/add":
			_ = r.ParseMultipartForm(1 << 20)
			wait = r.FormValue("wait")
			fmt.Fprint(w, `{"success":true,"value":{"id":"D2","name":"Pack","wait":true}}`)
		case "GET /seedbox/list":
			if !configed {
				fmt.Fprintf(w, `{"success":true,"value":[{"id":"D2","name":"Pack","wait":true,"status":0,"files":`+files+`}]}`, 0, 0)
				return
			}
			fmt.Fprintf(w, `{"success":true,"value":[{"id":"D2","name":"Pack","status":100,"downloadPercent":100,"files":`+files+`}]}`, 0, 100)
		case "POST /seedbox/D2/config":
			var body map[string][]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			unwanted, configed = body["files-unwanted"], true
			fmt.Fprint(w, `{"success":true,"value":[]}`)
		case "DELETE /seedbox/D2/remove":
			fmt.Fprint(w, `{"success":true,"value":["D2"]}`)
		}
	}))
	defer srv.Close()
	dl := NewDebridLink("key")
	dl.base = srv.URL
	upload, _ := builtTorrent(t, false)

	parts, _ := fetchTorrent(t, dl, upload, []core.TorrentFile{
		{Path: "a.mkv", Size: 64}, {Path: "b.mkv", Size: 64, Selected: true},
	}, core.StatusDone)

	if wait != "true" {
		t.Errorf("the upload went in with wait=%v; Debrid-Link would start before the selection", wait)
	}
	if !slices.Equal(unwanted, []any{"D2-1"}) {
		t.Errorf("files-unwanted = %v, want the file left out of the tree", unwanted)
	}
	if got := partURLs(parts); !slices.Equal(got, []string{"https://seed.example/b -> Pack/b.mkv"}) {
		t.Errorf("the engine got %v", got)
	}
}

func TestDebridLinkDecliningATorrentHandsItOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"success":false,"error":"maxTorrent"}`)
	}))
	defer srv.Close()
	dl := NewDebridLink("key")
	dl.base = srv.URL

	_, u := fetchTorrent(t, dl, testMagnet, nil, core.StatusError)
	if !u.Unsupported || u.Err != "debridlink: the daily torrent limit for this account is used up" {
		t.Errorf("got %+v, want the task handed on with the limit named", u)
	}
}

// builtTorrent is an uploaded .torrent as the collector carries it, and its
// raw bytes. A public one is a folder "Pack" of a.mkv and b.mkv.
func builtTorrent(t *testing.T, private bool) (string, []byte) {
	t.Helper()
	const piece = 32 << 10
	info := metainfo.Info{
		Name:        "Pack",
		PieceLength: piece,
		Files:       []metainfo.FileInfo{{Length: 64, Path: []string{"a.mkv"}}, {Length: 64, Path: []string{"b.mkv"}}},
		Pieces:      make([]byte, 20),
	}
	if private {
		p := true
		info.Private = &p
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := bencode.Marshal(metainfo.MetaInfo{InfoBytes: ib, Announce: "https://tracker.example/announce"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := torrent.Parse(raw); err != nil {
		t.Fatalf("fixture broken: %v", err)
	}
	return torrent.EncodeBytes(raw), raw
}
