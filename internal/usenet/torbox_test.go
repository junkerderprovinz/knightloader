package usenet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

const testKey = "tb-secret-key"

func writeTorBox(w http.ResponseWriter, data string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"success":true,"error":null,"detail":"ok","data":`+data+`}`)
}

func refuseTorBox(w http.ResponseWriter, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"success":false,"error":"`+code+`","detail":"`+detail+`","data":null}`)
}

// fakeTorBox is TorBox's Usenet API with one job that is ready once it has
// been asked about fetchRounds times.
type fakeTorBox struct {
	t           *testing.T
	fetchRounds int

	mu       sync.Mutex
	nzb      []byte
	name     string
	creates  int
	lists    int
	deleted  []int64
	busyNext int
}

func (f *fakeTorBox) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/usenet/createusenetdownload", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			f.t.Errorf("auth header = %q", got)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.creates++
		if f.busyNext > 0 {
			f.busyNext--
			w.WriteHeader(http.StatusTooManyRequests)
			refuseTorBox(w, "RATE_LIMITED", "too many requests")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			f.t.Errorf("the NZB did not arrive as the multipart field file: %v", err)
			refuseTorBox(w, "MISSING_REQUIRED_OPTION", "file")
			return
		}
		defer file.Close()
		if ct := header.Header.Get("Content-Type"); ct != "application/x-nzb" {
			f.t.Errorf("the NZB went out as %q, which TorBox refuses", ct)
		}
		f.nzb, _ = io.ReadAll(file)
		f.name = r.FormValue("name")
		writeTorBox(w, `{"usenetdownload_id":77,"hash":"abc","auth_id":"u"}`)
	})
	mux.HandleFunc("/api/usenet/mylist", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("id") {
			f.t.Errorf("mylist asked about one job, want the whole list")
		}
		f.mu.Lock()
		f.lists++
		ready := f.lists > f.fetchRounds
		f.mu.Unlock()
		// Another download on the account, which is nobody's business here.
		other := `{"id":12,"name":"Other","size":5,"progress":0.5,"download_state":"downloading","files":[]}`
		if !ready {
			writeTorBox(w, `[`+other+`,{"id":77,"name":"Show.S01E01","size":1000,"progress":0.25,"download_speed":500,`+
				`"download_state":"downloading","download_present":false,"files":[]}]`)
			return
		}
		writeTorBox(w, `[`+other+`,{"id":77,"name":"Show.S01E01","size":1000,"progress":1,"download_state":"completed",`+
			`"download_present":true,"files":[`+
			`{"id":1,"name":"Show.S01E01/show.s01e01.mkv","short_name":"show.s01e01.mkv","size":900},`+
			`{"id":2,"name":"Show.S01E01/show.s01e01.nfo","short_name":"show.s01e01.nfo","size":100}]}]`)
	})
	mux.HandleFunc("/api/usenet/requestdl", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("token") != testKey || q.Get("usenet_id") != "77" {
			f.t.Errorf("requestdl params = %v", q)
		}
		writeTorBox(w, `"https://store.torbox.example/dl/`+q.Get("file_id")+`"`)
	})
	mux.HandleFunc("/api/usenet/controlusenetdownload", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID        int64  `json:"usenet_id"`
			Operation string `json:"operation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Operation != "delete" {
			f.t.Errorf("controlusenetdownload body = %+v (%v), want a delete", body, err)
		}
		f.mu.Lock()
		f.deleted = append(f.deleted, body.ID)
		f.mu.Unlock()
		writeTorBox(w, `null`)
	})
	srv := httptest.NewServer(mux)
	f.t.Cleanup(srv.Close)
	return srv
}

func TestTorBoxTakesTheNZBAsAFileAndHandsBackItsFiles(t *testing.T) {
	fake := &fakeTorBox{t: t, fetchRounds: 1}
	srv := fake.server()
	tb := NewTorBox(srv.URL, "torbox", testKey)
	ctx := context.Background()

	id, err := tb.Submit(ctx, "Show.S01E01", []byte(sampleNZB))
	if err != nil {
		t.Fatal(err)
	}
	if id != "77" {
		t.Errorf("job id = %q, want TorBox's usenetdownload_id", id)
	}
	if string(fake.nzb) != sampleNZB || fake.name != "Show.S01E01" {
		t.Errorf("TorBox got %q named %q", fake.nzb, fake.name)
	}

	st := statusOf(t, tb, id)
	if st.Phase != PhaseFetching || st.Progress != 0.25 || st.Size != 1000 || st.Speed != 500 {
		t.Errorf("first reading = %+v, want a quarter of 1000 bytes fetched at 500 B/s", st)
	}
	st = statusOf(t, tb, id)
	if st.Phase != PhaseReady || len(st.Files) != 2 {
		t.Fatalf("second reading = %+v, want the job ready with two files", st)
	}
	if f := st.Files[0]; f.Name != "show.s01e01.mkv" || f.Dir != "" || f.ID != "1" || f.Size != 900 {
		t.Errorf("first file = %+v, want it at the top, without the job's folder", f)
	}

	link, err := tb.Link(ctx, id, "1")
	if err != nil || link != "https://store.torbox.example/dl/1" {
		t.Errorf("link = %q, %v", link, err)
	}
	if err := tb.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != 77 {
		t.Errorf("deleted %v, want job 77", fake.deleted)
	}
}

// statusOf reads one job and fails the test when the answer leaves it out.
func statusOf(t *testing.T, s Service, id string) Status {
	t.Helper()
	all, err := s.Status(context.Background(), []string{id})
	if err != nil {
		t.Fatal(err)
	}
	st, ok := all[id]
	if !ok {
		t.Fatalf("the answer leaves out job %s: %+v", id, all)
	}
	return st
}

func TestTorBoxReadsEveryJobInOneCall(t *testing.T) {
	fake := &fakeTorBox{t: t, fetchRounds: 5}
	tb := NewTorBox(fake.server().URL, "torbox", testKey)
	all, err := tb.Status(context.Background(), []string{"77", "12", "99"})
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	lists := fake.lists
	fake.mu.Unlock()
	if lists != 1 {
		t.Errorf("mylist was called %d times for three jobs, want once", lists)
	}
	if _, ok := all["77"]; !ok {
		t.Error("job 77 is missing from the answer")
	}
	if _, ok := all["12"]; !ok {
		t.Error("job 12 is missing from the answer")
	}
	if _, ok := all["99"]; ok {
		t.Error("a job the account does not hold was reported")
	}
}

func TestTorBoxFollowsADownloadItQueuedUntilItStarts(t *testing.T) {
	var (
		mu      sync.Mutex
		started bool
		deletes []string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/usenet/createusenetdownload", func(w http.ResponseWriter, r *http.Request) {
		writeTorBox(w, `{"queued_id":"5","hash":"ABC123"}`)
	})
	mux.HandleFunc("/api/queued/getqueued", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "usenet" {
			t.Errorf("getqueued type = %q, want usenet", r.URL.Query().Get("type"))
		}
		mu.Lock()
		defer mu.Unlock()
		if started {
			writeTorBox(w, `[]`)
			return
		}
		writeTorBox(w, `[{"id":5,"hash":"abc123","type":"usenet"}]`)
	})
	mux.HandleFunc("/api/usenet/mylist", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !started {
			writeTorBox(w, `[]`)
			return
		}
		writeTorBox(w, `[{"id":88,"hash":"abc123","name":"Show","size":50,"progress":0.5,"download_state":"downloading","files":[]}]`)
	})
	mux.HandleFunc("/api/queued/controlqueued", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		deletes = append(deletes, "queued "+fmt.Sprint(body["queued_id"])+" "+fmt.Sprint(body["operation"]))
		mu.Unlock()
		refuseTorBox(w, "ITEM_NOT_FOUND", "no such queued item")
	})
	mux.HandleFunc("/api/usenet/controlusenetdownload", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		deletes = append(deletes, "usenet "+fmt.Sprint(body["usenet_id"]))
		mu.Unlock()
		writeTorBox(w, `null`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	tb := NewTorBox(srv.URL, "torbox", testKey)
	ctx := context.Background()

	id, err := tb.Submit(ctx, "Show", []byte(sampleNZB))
	if err != nil {
		t.Fatalf("a queued download was refused: %v", err)
	}
	if st := statusOf(t, tb, id); st.Phase != PhaseFetching || st.ID != "" {
		t.Errorf("while queued = %+v, want it fetching with no id of its own yet", st)
	}

	mu.Lock()
	started = true
	mu.Unlock()
	st := statusOf(t, tb, id)
	if st.ID != "88" || st.Phase != PhaseFetching || st.Progress != 0.5 {
		t.Errorf("once started = %+v, want job 88 half fetched", st)
	}

	if err := tb.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(deletes, []string{"queued 5 delete", "usenet 88"}) {
		t.Errorf("deletes = %v, want the queue entry tried, then the download it became", deletes)
	}
}

func TestTorBoxAnsweringAnEmptyListAsNotFoundLeavesTheJobsOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refuseTorBox(w, "ITEM_NOT_FOUND", "no usenet downloads found")
	}))
	defer srv.Close()
	all, err := NewTorBox(srv.URL, "torbox", testKey).Status(context.Background(), []string{"77", queuedID("5", "abc")})
	if err != nil {
		t.Fatalf("an empty account answered as a failed call: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("reported %+v, want both jobs left out", all)
	}
}

func TestTorBoxTakesAJobIDSentAsAString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTorBox(w, `{"usenetdownload_id":"77","hash":"abc"}`)
	}))
	defer srv.Close()
	id, err := NewTorBox(srv.URL, "torbox", testKey).Submit(context.Background(), "x", []byte(sampleNZB))
	if err != nil || id != "77" {
		t.Errorf("id = %q, %v; want 77", id, err)
	}
}

func TestTorBoxPlanWithoutUsenetIsSaidSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refuseTorBox(w, "PLAN_RESTRICTED_FEATURE", "This feature is restricted to users of higher plans.")
	}))
	defer srv.Close()
	_, err := NewTorBox(srv.URL, "torbox", testKey).Submit(context.Background(), "x", []byte(sampleNZB))
	if !errors.Is(err, ErrNoUsenet) {
		t.Errorf("err = %v, want ErrNoUsenet", err)
	}
}

func TestTorBoxKeepsTheFoldersInsideADownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTorBox(w, `[{"id":9,"name":"Season","size":30,"download_state":"completed","download_present":true,"files":[`+
			`{"id":1,"name":"Season/Show.S01E01.mkv","short_name":"Show.S01E01.mkv","size":10},`+
			`{"id":2,"name":"Season/Subs/Show.S01E01/2_English.srt","short_name":"2_English.srt","size":10},`+
			`{"id":3,"name":"Season/Subs/Show.S01E02/2_English.srt","short_name":"2_English.srt","size":10}]}]`)
	}))
	defer srv.Close()
	st := statusOf(t, NewTorBox(srv.URL, "torbox", testKey), "9")
	var got []string
	for _, f := range st.Files {
		got = append(got, f.Dir+"|"+f.Name)
	}
	want := []string{"|Show.S01E01.mkv", "Subs/Show.S01E01|2_English.srt", "Subs/Show.S01E02|2_English.srt"}
	if !slices.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestTorBoxLimitsReadAsBusyNotAsARefusal(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer func(http.ResponseWriter)
	}{
		{"HTTP 429", func(w http.ResponseWriter) { w.WriteHeader(http.StatusTooManyRequests) }},
		{"ACTIVE_LIMIT", func(w http.ResponseWriter) { refuseTorBox(w, "ACTIVE_LIMIT", "too many active downloads") }},
		{"COOLDOWN_LIMIT", func(w http.ResponseWriter) { refuseTorBox(w, "COOLDOWN_LIMIT", "wait a moment") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { c.answer(w) }))
			defer srv.Close()
			_, err := NewTorBox(srv.URL, "torbox", testKey).Submit(context.Background(), "x", []byte(sampleNZB))
			if !errors.Is(err, ErrBusy) {
				t.Errorf("err = %v, want ErrBusy", err)
			}
		})
	}
}

func TestTorBoxRefusalIsNeitherBusyNorTemporary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refuseTorBox(w, "BOZO_NZB", "this nzb is invalid")
	}))
	defer srv.Close()
	_, err := NewTorBox(srv.URL, "torbox", testKey).Submit(context.Background(), "x", []byte(sampleNZB))
	if err == nil || errors.Is(err, ErrBusy) || temporary(err) {
		t.Fatalf("err = %v, want a plain refusal", err)
	}
	if !strings.Contains(err.Error(), "this nzb is invalid") {
		t.Errorf("err = %v, want TorBox's own sentence", err)
	}
}

func TestTorBoxErrorsNeverCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	_, err := NewTorBox(srv.URL, "torbox", testKey).Link(context.Background(), "77", "1")
	if err == nil {
		t.Fatal("a closed server answered")
	}
	if !temporary(err) {
		t.Errorf("err = %v, want an unreachable service to count as temporary", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("the error quotes the API key: %v", err)
	}
}

func TestTorBoxReportsAFailedJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTorBox(w, `[{"id":5,"name":"Broken","size":10,"download_state":"failed (missing articles)","download_present":false,"files":[]}]`)
	}))
	defer srv.Close()
	st := statusOf(t, NewTorBox(srv.URL, "torbox", testKey), "5")
	if st.Phase != PhaseFailed || !strings.Contains(st.Reason, "missing articles") {
		t.Errorf("status = %+v, want a failure naming TorBox's state", st)
	}
}
