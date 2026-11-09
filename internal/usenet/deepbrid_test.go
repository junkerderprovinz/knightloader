package usenet

import (
	"context"
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

// fakeDeepbrid answers the Usenet calls with the bodies Deepbrid's API docs
// print.
type fakeDeepbrid struct {
	t *testing.T

	mu sync.Mutex
	// uploads lists the account's uploads, add answers /usenet/add and info
	// /usenet/uploads/info, whose status is infoStatus when set.
	uploads    string
	add        string
	addStatus  int
	info       string
	infoStatus int
	sent       []byte
	sentName   string
	deleted    []string
	seen       []string
}

const deepbridInfo = `{"error":0,"id":1024,"title":"Show","has_password":false,"password":"","count":3,"files":[` +
	`{"name":"Show/show.mkv","size":9837421056,"size_human":"9.2 GB","video":true,"link":"https://dl.example/file/show.mkv"},` +
	`{"name":"Show/info.nfo","size":2048,"size_human":"2 KB","video":false,"link":"https://dl.example/file/info.nfo"},` +
	`{"name":"Show/movie.mkv","size":5704011001,"size_human":"5.3 GB","video":false,"link":"","unreachable":"truncated_missing_volumes"}]}`

func newFakeDeepbrid(t *testing.T) (*fakeDeepbrid, *httptest.Server) {
	f := &fakeDeepbrid{
		t:       t,
		uploads: `{"error":0,"count":0,"items":[]}`,
		add: `{"error":0,"id":1024,"files":[{"filename":"show.mkv","subject":"[1/2] - \"show.mkv\" yEnc (1/6420)",` +
			`"filesize":9837421056,"download_url":"https://dl.example/file/show.mkv"}],"has_password":false,"password":""}`,
		info: deepbridInfo,
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeDeepbrid) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := r.Method + " " + r.URL.Path
	f.seen = append(f.seen, call)
	if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
		f.t.Errorf("%s carried Authorization %q, want the key as bearer token", call, got)
	}
	switch {
	case call == "GET /usenet/uploads":
		fmt.Fprint(w, f.uploads)
	case call == "POST /usenet/add":
		file, head, err := r.FormFile("nzb_file")
		if err != nil {
			f.t.Errorf("the add carries no nzb_file: %v", err)
			return
		}
		f.sent, _ = io.ReadAll(file)
		f.sentName = head.Filename
		if f.addStatus != 0 {
			w.WriteHeader(f.addStatus)
		}
		fmt.Fprint(w, f.add)
	case call == "GET /usenet/uploads/info":
		if id := r.URL.Query().Get("id"); id != "1024" {
			f.t.Errorf("read upload %q", id)
		}
		if f.infoStatus != 0 {
			w.WriteHeader(f.infoStatus)
		}
		fmt.Fprint(w, f.info)
	case strings.HasPrefix(call, "DELETE /usenet/uploads/delete/"):
		f.deleted = append(f.deleted, strings.TrimPrefix(r.URL.Path, "/usenet/uploads/delete/"))
		fmt.Fprint(w, `{"error":0,"message":"OK","deleted":1,"ids":[1024],"not_found":[]}`)
	default:
		f.t.Errorf("unexpected %s", call)
		http.NotFound(w, r)
	}
}

func (f *fakeDeepbrid) deletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deleted)
}

func TestDeepbridHasTheFilesOfAnNZBAtOnce(t *testing.T) {
	fake, srv := newFakeDeepbrid(t)
	h := newHarness(t, NewDeepbrid(srv.URL, "deepbrid", testKey))
	j := h.add(t, "Show.S01E01")

	h.m.round(context.Background())

	j = h.job(t, j.ID)
	if j.State != StateStaged || j.Remote != "1024" || j.Label != "Deepbrid" {
		t.Fatalf("after one round the job is %+v, want it staged from Deepbrid's upload 1024", j)
	}
	if string(fake.sent) != sampleNZB || fake.sentName != "Show.S01E01.nzb" {
		t.Errorf("sent %q as %q, want the NZB under the release's name", fake.sent, fake.sentName)
	}
	files := h.staged[j.ID]
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	if !slices.Equal(names, []string{"show.mkv", "info.nfo"}) || files[0].Size != 9837421056 {
		t.Errorf("staged %+v, want the two files Deepbrid can serve", files)
	}
}

func TestDeepbridSaysWhichFilesItCannotServe(t *testing.T) {
	_, srv := newFakeDeepbrid(t)

	st, err := NewDeepbrid(srv.URL, "deepbrid", testKey).Status(context.Background(), []string{"1024"})

	if err != nil {
		t.Fatal(err)
	}
	got := st["1024"]
	if got.Phase != PhaseReady || len(got.Files) != 2 {
		t.Fatalf("status = %+v, want ready with the two files Deepbrid can serve", got)
	}
	if !strings.Contains(got.Note, "movie.mkv") || !strings.Contains(got.Note, "truncated_missing_volumes") {
		t.Errorf("note = %q, want the file left out and why", got.Note)
	}
}

func TestDeepbridUploadWithNothingToServeFails(t *testing.T) {
	fake, srv := newFakeDeepbrid(t)
	fake.info = `{"error":0,"id":1024,"files":[{"name":"movie.mkv","size":5704011001,"link":"","unreachable":"compressed_not_store"}]}`

	st, err := NewDeepbrid(srv.URL, "deepbrid", testKey).Status(context.Background(), []string{"1024"})

	if err != nil {
		t.Fatal(err)
	}
	if got := st["1024"]; got.Phase != PhaseFailed || !strings.Contains(got.Reason, "compressed_not_store") {
		t.Errorf("status = %+v, want failed with Deepbrid's reason", got)
	}
}

func TestDeepbridLinkReadsTheFileFromTheUpload(t *testing.T) {
	_, srv := newFakeDeepbrid(t)
	d := NewDeepbrid(srv.URL, "deepbrid", testKey)
	st, err := d.Status(context.Background(), []string{"1024"})
	if err != nil {
		t.Fatal(err)
	}

	link, err := d.Link(context.Background(), "1024", st["1024"].Files[1].ID)

	if err != nil || link != "https://dl.example/file/info.nfo" {
		t.Errorf("Link = %q, %v; want the address of info.nfo", link, err)
	}
}

func TestDeepbridUploadItDidNotKeepGoesOutUnderTheAnsweredLinks(t *testing.T) {
	fake, srv := newFakeDeepbrid(t)
	fake.add = `{"error":0,"id":0,"files":[{"filename":"show.mkv","filesize":9837421056,"download_url":"https://dl.example/file/show.mkv"},` +
		`{"filename":"show.par2","filesize":1024,"download_url":""}],"has_password":false,"password":""}`
	h := newHarness(t, NewDeepbrid(srv.URL, "deepbrid", testKey))
	j := h.add(t, "Show")

	h.m.round(context.Background())
	h.finished = func([]string) bool { return true }
	h.m.round(context.Background())

	j = h.job(t, j.ID)
	files := h.staged[j.ID]
	if j.State != StateStaged || len(files) != 1 || files[0].Link != "https://dl.example/file/show.mkv" {
		t.Fatalf("job %+v staged %+v, want show.mkv under the link the add answered with", j, files)
	}
	if !j.Cleared || len(fake.deletes()) != 0 {
		t.Errorf("cleared %v, deleted %v; an upload Deepbrid did not keep has nothing to delete", j.Cleared, fake.deletes())
	}
}

func TestDeepbridDeletesItsUploadOnceTheFilesAreHere(t *testing.T) {
	fake, srv := newFakeDeepbrid(t)
	h := newHarness(t, NewDeepbrid(srv.URL, "deepbrid", testKey))
	h.add(t, "Show")

	h.m.round(context.Background())
	h.finished = func([]string) bool { return true }
	h.m.round(context.Background())

	if got := fake.deletes(); !slices.Equal(got, []string{"1024"}) {
		t.Errorf("deleted %v, want upload 1024", got)
	}
}

func TestDeepbridNeverDeletesAnUploadTheAccountHad(t *testing.T) {
	fake, srv := newFakeDeepbrid(t)
	fake.uploads = `{"error":0,"count":1,"items":[{"id":1024,"title":"Show.nzb","source":"file","source_url":null,"hash":"3f1a9ce9","added_at":"2026-06-05 18:22:10"}]}`
	h := newHarness(t, NewDeepbrid(srv.URL, "deepbrid", testKey))
	j := h.add(t, "Show")

	h.m.round(context.Background())
	h.finished = func([]string) bool { return true }
	h.m.round(context.Background())

	if j = h.job(t, j.ID); j.State != StateStaged || !j.Cleared {
		t.Fatalf("job = %+v, want it staged and done with", j)
	}
	if got := fake.deletes(); len(got) != 0 {
		t.Errorf("deleted %v, which the account had before", got)
	}
}

func TestDeepbridSubmitErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		is     error
		says   string
		retry  bool
	}{
		{name: "not premium", body: `{"error":2,"message":"Only premium users can use this feature."}`, is: ErrNoUsenet},
		{name: "too many requests", body: `{"error":11,"message":"Too many requests"}`, is: ErrBusy},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":429,"message":"Rate limit exceeded"}`, is: ErrBusy},
		{name: "proxy or VPN", body: `{"error":15,"message":"Proxy or VPN detected"}`, says: "VPN"},
		{name: "invalid NZB", body: `{"error":10,"message":"Invalid NZB"}`, says: "Invalid NZB"},
		{name: "not processed", body: `{"error":12,"message":"Could not process NZB"}`, says: "Could not process NZB"},
		{name: "key refused", status: http.StatusUnauthorized, body: `{"error":401,"message":"Authentication required."}`, says: "key"},
		{name: "maintenance", status: http.StatusServiceUnavailable, body: `{"error":503,"message":"Service under maintenance"}`, retry: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, srv := newFakeDeepbrid(t)
			fake.add, fake.addStatus = tt.body, tt.status

			_, err := NewDeepbrid(srv.URL, "deepbrid", testKey).Submit(context.Background(), "Show", []byte(sampleNZB))

			switch {
			case err == nil:
				t.Fatal("the submit passed")
			case tt.is != nil && !errors.Is(err, tt.is):
				t.Errorf("err = %v, want %v", err, tt.is)
			case !strings.Contains(err.Error(), tt.says):
				t.Errorf("err = %v, want it to say %q", err, tt.says)
			case temporary(err) != tt.retry:
				t.Errorf("err = %v, temporary %v, want %v", err, temporary(err), tt.retry)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Errorf("err = %v carries the key", err)
			}
		})
	}
}

func TestDeepbridUploadItNoLongerHasIsLeftOut(t *testing.T) {
	fake, srv := newFakeDeepbrid(t)
	fake.info = `{"error":1,"message":"Nothing matched"}`

	st, err := NewDeepbrid(srv.URL, "deepbrid", testKey).Status(context.Background(), []string{"1024"})

	if _, found := st["1024"]; err != nil || found {
		t.Errorf("Status = %+v, %v; want the upload left out of the answer", st, err)
	}
}

func TestDeepbridBusyStatusIsBusy(t *testing.T) {
	fake, srv := newFakeDeepbrid(t)
	fake.info, fake.infoStatus = `{"error":429,"message":"Rate limit exceeded"}`, http.StatusTooManyRequests

	_, err := NewDeepbrid(srv.URL, "deepbrid", testKey).Status(context.Background(), []string{"1024"})

	if !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v, want ErrBusy", err)
	}
}
