package api

// A real .nzb through the SABnzbd bridge and the upload button, against a
// TorBox that answers from this process, and the category a grab is filed in.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
)

const realNZB = `<?xml version="1.0" encoding="iso-8859-1" ?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
 <file poster="someone" date="1700000000" subject="Show.S01E01 [1/2] - &#34;show.r00&#34; yEnc">
  <groups><group>alt.binaries.example</group></groups>
  <segments><segment bytes="739067" number="1">abcdef0123456789@news</segment></segments>
 </file>
</nzb>`

// largeNZB is an NZB of at least size bytes, as a large release has.
func largeNZB(size int) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="iso-8859-1" ?>` + "\n" +
		`<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">` + "\n" +
		` <file poster="someone" date="1700000000" subject="Film.2160p.Remux [1/1] yEnc">` + "\n" +
		`  <groups><group>alt.binaries.example</group></groups>` + "\n  <segments>\n")
	for n := 1; b.Len() < size; n++ {
		fmt.Fprintf(&b, `   <segment bytes="739067" number="%d">part%dof-a-large-release@news.example</segment>`+"\n", n, n)
	}
	b.WriteString("  </segments>\n </file>\n</nzb>\n")
	return b.Bytes()
}

// torboxUsenet is TorBox's Usenet API with one job, ready once ready is set,
// whose one file is served at fileURL.
type torboxUsenet struct {
	ready   atomic.Bool
	refuse  string
	fileURL string
	mu      sync.Mutex
	got     []byte
	deleted int
}

func (f *torboxUsenet) start(t *testing.T) *httptest.Server {
	t.Helper()
	answer := func(w http.ResponseWriter, data string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"error":null,"detail":"ok","data":`+data+`}`)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/usenet/createusenetdownload", func(w http.ResponseWriter, r *http.Request) {
		if f.refuse != "" {
			_, _ = io.WriteString(w, `{"success":false,"error":"BOZO_NZB","detail":"`+f.refuse+`","data":null}`)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("no NZB in the upload: %v", err)
			return
		}
		defer file.Close()
		f.mu.Lock()
		f.got, _ = io.ReadAll(file)
		f.mu.Unlock()
		answer(w, `{"usenetdownload_id":77}`)
	})
	mux.HandleFunc("/api/usenet/mylist", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		gone := f.deleted > 0
		f.mu.Unlock()
		switch {
		case gone:
			answer(w, `[]`)
		case !f.ready.Load():
			answer(w, `[{"id":77,"name":"Show","size":2000,"progress":0.5,"download_speed":100,"download_state":"downloading","files":[]}]`)
		default:
			answer(w, `[{"id":77,"name":"Show","size":2000,"progress":1,"download_state":"completed","download_present":true,`+
				`"files":[{"id":1,"name":"Show/show.s01e01.mkv","short_name":"show.s01e01.mkv","size":2000}]}]`)
		}
	})
	mux.HandleFunc("/api/usenet/requestdl", func(w http.ResponseWriter, r *http.Request) {
		answer(w, `"`+f.fileURL+`"`)
	})
	mux.HandleFunc("/api/usenet/controlusenetdownload", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.deleted++
		f.mu.Unlock()
		answer(w, `null`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (f *torboxUsenet) deletes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleted
}

// usenetClientServer is downloadClientServer with a TorBox account behind it.
func usenetClientServer(t *testing.T, fake *torboxUsenet) (*app.App, *httptest.Server, string) {
	t.Helper()
	a, srv, key := downloadClientServer(t, nil)
	a.SetUsenetServices(usenet.NewTorBox(fake.start(t).URL, "torbox", "tb-key"))
	return a, srv, key
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestARealNZBFromSonarrIsFollowedThroughTorBox(t *testing.T) {
	t.Parallel()
	fake := &torboxUsenet{}
	a, srv, key := usenetClientServer(t, fake)
	downloads := a.Settings.Get().DownloadDir

	code, add := sabAddFile(t, srv, key, "Show.S01E01.1080p.WEB.nzb", "tv-sonarr", []byte(realNZB))
	if code != http.StatusOK {
		t.Fatalf("addfile answered %d", code)
	}
	nzoID := nzoIDOf(t, add)

	// While TorBox fetches, Sonarr sees the job downloading, with TorBox's
	// own progress.
	var row map[string]any
	waitUntil(t, "the job to show TorBox's progress", func() bool {
		_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
		rows := slots(t, doc, "queue")
		if len(rows) == 1 && rows[0]["percentage"] == float64(50) {
			row = rows[0]
			return true
		}
		return false
	})
	if row["nzo_id"] != nzoID || row["status"] != "Downloading" {
		t.Errorf("queue slot = %+v, want the grab downloading at TorBox", row)
	}
	fake.mu.Lock()
	sent := string(fake.got)
	fake.mu.Unlock()
	if sent != realNZB {
		t.Errorf("TorBox got %q, want the .nzb Sonarr uploaded", sent)
	}

	// Once TorBox has the file, the grab follows the task it became, which is
	// queued like every grab's.
	fake.ready.Store(true)
	waitUntil(t, "the grab to follow its file", func() bool {
		_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
		rows := slots(t, doc, "queue")
		return len(rows) == 1 && rows[0]["nzo_id"] == nzoID && rows[0]["status"] == "Queued"
	})
	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want TorBox's one file", len(tasks))
	}
	task := tasks[0]
	if task.Resolver != usenet.ResolverID || task.Name != "show.s01e01.mkv" || task.Size != 2000 {
		t.Errorf("task = %s via %s, %d bytes; want TorBox's file on the usenet backend", task.Name, task.Resolver, task.Size)
	}
	if strings.Contains(task.URL, "tb-key") {
		t.Errorf("the task's link carries the API key: %s", task.URL)
	}
	if task.Package != "Show.S01E01.1080p.WEB" || task.Category != "tv-sonarr" {
		t.Errorf("package %q, category %q; want the release in Sonarr's category", task.Package, task.Category)
	}
	if task.Status != core.StatusQueued {
		t.Errorf("status = %s, want it queued", task.Status)
	}

	// The folder Sonarr imports from is the category's, created on first use.
	want := filepath.Join(downloads, "tv-sonarr", "Show.S01E01.1080p.WEB")
	if got := a.TaskFolder(task.ID); got != want {
		t.Errorf("the file lands in %s, want %s", got, want)
	}
}

func TestAGrabFromSonarrDownloadsAndIsImportableFromItsHistory(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	t.Parallel()
	payload := bytes.Repeat([]byte("episode "), 250)
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "show.s01e01.mkv", time.Time{}, bytes.NewReader(payload))
	}))
	t.Cleanup(files.Close)
	fake := &torboxUsenet{fileURL: files.URL + "/show.s01e01.mkv"}
	fake.ready.Store(true)
	a, srv, key := usenetClientServer(t, fake)
	a.SetHalted(false)
	downloads := a.Settings.Get().DownloadDir

	_, add := sabAddFile(t, srv, key, "Show.S01E01.1080p.WEB.nzb", "tv-sonarr", []byte(realNZB))
	nzoID := nzoIDOf(t, add)

	var row map[string]any
	waitUntil(t, "the grab to reach the history", func() bool {
		_, doc := sabGet(t, srv, key, map[string]string{"mode": "history", "category": "tv-sonarr"})
		rows := slots(t, doc, "history")
		if len(rows) == 1 {
			row = rows[0]
			return true
		}
		return false
	})
	folder := filepath.Join(downloads, "tv-sonarr", "Show.S01E01.1080p.WEB")
	if row["nzo_id"] != nzoID || row["status"] != "Completed" || row["storage"] != folder {
		t.Fatalf("history slot = %+v, want it completed in %s", row, folder)
	}
	got, err := os.ReadFile(filepath.Join(folder, "show.s01e01.mkv"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Errorf("the file Sonarr would import holds %d bytes (%v), want %d", len(got), err, len(payload))
	}
	waitUntil(t, "TorBox's copy to be deleted", func() bool { return fake.deletes() == 1 })
}

func TestAnNZBTorBoxRefusesLandsInSonarrsHistoryAsFailed(t *testing.T) {
	t.Parallel()
	fake := &torboxUsenet{refuse: "this nzb has no files"}
	a, srv, key := usenetClientServer(t, fake)

	if _, add := sabAddFile(t, srv, key, "Broken.nzb", "tv-sonarr", []byte(realNZB)); add["status"] != true {
		t.Fatalf("addfile refused at the door: %+v", add)
	}
	var rows []map[string]any
	waitUntil(t, "the refusal to reach the history", func() bool {
		_, doc := sabGet(t, srv, key, map[string]string{"mode": "history", "category": "tv-sonarr"})
		rows = slots(t, doc, "history")
		return len(rows) == 1
	})
	if rows[0]["status"] != "Failed" || !strings.Contains(rows[0]["fail_message"].(string), "this nzb has no files") {
		t.Errorf("history slot = %+v, want Failed with TorBox's reason", rows[0])
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks exist for a refused .nzb", n)
	}
}

func TestDeletingAGrabStillAtTorBoxDeletesItThere(t *testing.T) {
	t.Parallel()
	fake := &torboxUsenet{}
	a, srv, key := usenetClientServer(t, fake)
	folder := filepath.Join(a.Settings.Get().DownloadDir, "tv-sonarr", "Unwanted")

	_, add := sabAddFile(t, srv, key, "Unwanted.nzb", "tv-sonarr", []byte(realNZB))
	nzoID := nzoIDOf(t, add)
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("the grab has no folder of its own at %s: %v", folder, err)
	}
	waitUntil(t, "TorBox to take the job", func() bool {
		_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
		rows := slots(t, doc, "queue")
		return len(rows) == 1 && rows[0]["status"] == "Downloading"
	})

	if _, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "name": "delete", "value": nzoID, "del_files": "1"}); doc["status"] != true {
		t.Fatalf("the delete answered %+v", doc)
	}
	waitUntil(t, "TorBox to be told", func() bool { return fake.deletes() == 1 })
	_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
	if rows := slots(t, doc, "queue"); len(rows) != 0 {
		t.Errorf("the deleted grab is still queued: %+v", rows)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Errorf("the deleted grab's empty folder is still there (%v)", err)
	}
}

func TestDeletingAGrabWhoseFilesJustArrivedRemovesThem(t *testing.T) {
	t.Parallel()
	fake := &torboxUsenet{}
	fake.ready.Store(true)
	a, srv, key := usenetClientServer(t, fake)

	_, add := sabAddFile(t, srv, key, "Unwanted.nzb", "tv-sonarr", []byte(realNZB))
	nzoID := nzoIDOf(t, add)
	// No queue poll in between, so the grab has not taken the job's tasks.
	waitUntil(t, "the file to be staged", func() bool { return len(a.Tasks()) == 1 })

	if _, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "name": "delete", "value": nzoID, "del_files": "1"}); doc["status"] != true {
		t.Fatalf("the delete answered %+v", doc)
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks are left after Sonarr deleted the grab", n)
	}
	waitUntil(t, "TorBox's copy to go with the tasks", func() bool { return fake.deletes() == 1 })
}

func TestAFailedTaskWithARetryComingStaysInTheQueue(t *testing.T) {
	t.Parallel()
	dc := &downloadClient{a: testApp(t)}
	g := sabGrab{ID: "SABnzbd_nzo_x", Name: "Show", Category: "tv", TaskIDs: []string{"t1"}}
	task := &core.Task{ID: "t1", Status: core.StatusError, Error: "503 Service Unavailable", Enabled: true,
		NextTry: time.Now().Add(time.Minute)}
	live := map[string]*core.Task{"t1": task}

	if v, ok := dc.view(g, live, nil); !ok || v.finished || v.status != "Queued" {
		t.Errorf("with a retry coming the grab is %q (finished %v), want it queued", v.status, v.finished)
	}
	task.NextTry = time.Time{}
	if v, _ := dc.view(g, live, nil); !v.finished || v.status != "Failed" {
		t.Errorf("with no retry left the grab is %q, want it failed", v.status)
	}
}

func TestAnNZBForALargeReleaseIsTaken(t *testing.T) {
	t.Parallel()
	fake := &torboxUsenet{}
	_, srv, sonarr := usenetClientServer(t, fake)
	nzb := largeNZB(12 << 20)

	_, add := sabAddFile(t, srv, sonarr, "Film.2160p.Remux.nzb", "movies", nzb)
	nzoIDOf(t, add)
	waitUntil(t, "TorBox to receive the whole .nzb", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return len(fake.got) == len(nzb)
	})
}

func TestAGrabIsFiledInTheCategoryOfTheSameName(t *testing.T) {
	t.Parallel()
	shows := t.TempDir()
	a, srv, key := downloadClientServer(t, func(s *settings.Settings) {
		s.Categories = []settings.Category{{ID: "serien", Name: "TV-Sonarr", Dir: shows}}
	})

	if _, add := sabAddFile(t, srv, key, "Show.S02E01.nzb", "tv-sonarr", []byte(testMagnet)); add["status"] != true {
		t.Fatalf("addfile refused: %+v", add)
	}
	tasks := a.Tasks()
	if len(tasks) != 1 || tasks[0].Category != "serien" {
		t.Fatalf("tasks = %+v, want the grab in the category named TV-Sonarr", tasks)
	}
	if n := len(a.Settings.Get().Categories); n != 1 {
		t.Errorf("the table holds %d categories, want no new one", n)
	}
	if got := a.TaskFolder(tasks[0].ID); !strings.HasPrefix(got, shows) {
		t.Errorf("the grab lands in %s, want the category's folder %s", got, shows)
	}
}

func TestAMissingCategoryIsCreatedOnceInTheDownloadFolder(t *testing.T) {
	t.Parallel()
	a, srv, key := downloadClientServer(t, nil)
	downloads := a.Settings.Get().DownloadDir

	for release, hash := range map[string]string{"Film.2024.nzb": "aa", "Other.Film.2023.nzb": "bb"} {
		magnet := "magnet:?xt=urn:btih:" + strings.Repeat(hash, 20)
		if _, add := sabAddFile(t, srv, key, release, "movies", []byte(magnet)); add["status"] != true {
			t.Fatalf("addfile refused %s: %+v", release, add)
		}
	}
	cats := a.Settings.Get().Categories
	if len(cats) != 1 || cats[0].ID != "movies" || cats[0].Dir != filepath.Join(downloads, "movies") {
		t.Fatalf("categories = %+v, want one movies drawer in the download folder", cats)
	}
	for _, task := range a.Tasks() {
		if task.Category != "movies" {
			t.Errorf("%s is filed in %q, want movies", task.Package, task.Category)
		}
	}
}

// offeredCategories reads get_config's categories as Sonarr does, by name.
func offeredCategories(t *testing.T, srv *httptest.Server, key string) map[string]string {
	t.Helper()
	_, doc := sabGet(t, srv, key, map[string]string{"mode": "get_config"})
	raw, _ := json.Marshal(doc["config"].(map[string]any)["categories"])
	var cats []struct{ Name, Dir string }
	if err := json.Unmarshal(raw, &cats); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range cats {
		if _, dup := got[c.Name]; dup {
			t.Errorf("category %q is offered twice", c.Name)
		}
		got[c.Name] = c.Dir
	}
	return got
}

func TestSonarrIsOfferedTheInstancesCategories(t *testing.T) {
	t.Parallel()
	shows := t.TempDir()
	_, srv, key := downloadClientServer(t, func(s *settings.Settings) {
		s.Categories = []settings.Category{{ID: "tv", Name: "Serien", Dir: shows}, {ID: "musik", Name: "Musik"}}
	})
	got := offeredCategories(t, srv, key)

	if got["Serien"] != shows {
		t.Errorf("Serien's dir = %q, want the category's own folder %s", got["Serien"], shows)
	}
	// Sonarr compares names exactly, and "tv" files a grab in Serien.
	if got["tv"] != shows {
		t.Errorf("tv = %q, want it offered with Serien's folder", got["tv"])
	}
	if _, ok := got["Musik"]; !ok {
		t.Error("a category of this instance is not offered")
	}
	// The defaults nothing answers to yet are offered inside complete_dir,
	// since their first grab creates them.
	for _, name := range []string{"movies", "tv-sonarr", "radarr"} {
		if got[name] != name {
			t.Errorf("%s = %q, want it offered inside complete_dir", name, got[name])
		}
	}
}

func TestAnUploadedNZBWithoutAnAccountIsRefusedWithACode(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	srv := containerServer(t, a)

	code, body := postMultipartFile(t, srv.URL+"/api/containers", "file", "Show.nzb", []byte(realNZB))
	var got struct{ Error, Code string }
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("answered %d %s, want a JSON refusal", code, body)
	}
	if code != http.StatusServiceUnavailable || got.Code != "noUsenet" {
		t.Errorf("answered %d %+v, want 503 with the code noUsenet", code, got)
	}
	// The namespace address inside it is not a download.
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks were staged from a refused .nzb", n)
	}
}

// containerServer serves the upload routes of a.
func containerServer(t *testing.T, a *app.App) *httptest.Server {
	t.Helper()
	reg := newRegistry()
	registerContainers(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestAnUploadedNZBGoesToTorBox(t *testing.T) {
	t.Parallel()
	fake := &torboxUsenet{}
	a := testApp(t)
	a.SetUsenetServices(usenet.NewTorBox(fake.start(t).URL, "torbox", "tb-key"))
	srv := containerServer(t, a)

	code, body := postMultipartFile(t, srv.URL+"/api/containers", "file", "Show.S01E01.nzb", []byte(realNZB))
	if code != http.StatusAccepted {
		t.Fatalf("answered %d %s, want 202", code, body)
	}
	var got struct{ Kind, HandedTo, Service string }
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.HandedTo != "usenet" || got.Service != "TorBox" {
		t.Errorf("answer = %+v, want it handed to TorBox", got)
	}
	waitUntil(t, "TorBox to receive the .nzb", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return string(fake.got) == realNZB
	})
}

func TestAnUploadedNZBForALargeReleaseIsTakenAndALargeListIsNot(t *testing.T) {
	t.Parallel()
	fake := &torboxUsenet{}
	a := testApp(t)
	a.SetUsenetServices(usenet.NewTorBox(fake.start(t).URL, "torbox", "tb-key"))
	srv := containerServer(t, a)
	big := largeNZB(12 << 20)

	if code, body := postMultipartFile(t, srv.URL+"/api/containers", "file", "Film.2160p.Remux.nzb", big); code != http.StatusAccepted {
		t.Errorf("a 12 MB .nzb answered %d %s, want 202", code, body)
	}
	if code, _ := postMultipartFile(t, srv.URL+"/api/containers", "file", "links.txt", big); code != http.StatusRequestEntityTooLarge {
		t.Errorf("a 12 MB link list answered %d, want 413", code)
	}
}

// An add and read token, the preset for Sonarr, may clear a grab TorBox gave
// up on, but not drop one TorBox is still fetching, which would leave that
// job running with nobody tracking it.
func TestAnAddAndReadTokenOnlyForgetsAnNZBThatFailed(t *testing.T) {
	t.Parallel()
	fake := &torboxUsenet{}
	a, srv, _ := usenetClientServer(t, fake)
	_, key, err := a.APITokens.CreateScoped("sonarr", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}

	_, add := sabAddFile(t, srv, key, "Wanted.nzb", "tv-sonarr", []byte(realNZB))
	nzoID := nzoIDOf(t, add)
	waitUntil(t, "TorBox to take the job", func() bool {
		_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
		rows := slots(t, doc, "queue")
		return len(rows) == 1 && rows[0]["status"] == "Downloading"
	})
	_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "name": "delete", "value": nzoID, "del_files": "1"})
	if msg, _ := doc["error"].(string); !strings.Contains(msg, `"control"`) {
		t.Errorf("dropping a job TorBox is fetching answered %+v, want a refusal naming the control right", doc)
	}
	if n := fake.deletes(); n != 0 {
		t.Errorf("TorBox was told to delete the job %d times after a refused delete", n)
	}

	fake.refuse = "this nzb has no files"
	_, add = sabAddFile(t, srv, key, "Broken.nzb", "tv-sonarr", []byte(realNZB))
	failedID := nzoIDOf(t, add)
	waitUntil(t, "the refusal to reach the history", func() bool {
		_, doc := sabGet(t, srv, key, map[string]string{"mode": "history", "category": "tv-sonarr"})
		return len(slots(t, doc, "history")) == 1
	})
	_, doc = sabGet(t, srv, key, map[string]string{"mode": "history", "name": "delete", "value": failedID, "del_files": "1"})
	if doc["status"] != true {
		t.Fatalf("Sonarr's cleanup of the failed grab answered %+v", doc)
	}
	_, doc = sabGet(t, srv, key, map[string]string{"mode": "history", "category": "tv-sonarr"})
	if rows := slots(t, doc, "history"); len(rows) != 0 {
		t.Errorf("the failed grab is still reported after Sonarr cleared it: %+v", rows)
	}
}
