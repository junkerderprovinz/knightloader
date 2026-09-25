package usenet

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
)

// fakePremiumize is the part of Premiumize.me's API a transfer goes through:
// one transfer, tr1, which by default unpacked into a folder of its own with a
// subfolder. fileID makes it a transfer that left a single file in the
// destination folder instead.
type fakePremiumize struct {
	t *testing.T

	mu         sync.Mutex
	nzb        []byte
	status     string
	fileID     string
	folderName string
	deleted    []string
	rateOnce   bool
}

func (f *fakePremiumize) server() *httptest.Server {
	answer := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/transfer/create", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer pm-key" {
			f.t.Errorf("auth header = %q", got)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.rateOnce {
			f.rateOnce = false
			answer(w, `{"status":"error","message":"rate limit reached, slow down","code":"rate_limit_reached"}`)
			return
		}
		file, _, err := r.FormFile("src")
		if err != nil {
			f.t.Errorf("the NZB did not arrive as the multipart field src: %v", err)
			answer(w, `{"status":"error","message":"no src"}`)
			return
		}
		defer file.Close()
		f.nzb, _ = io.ReadAll(file)
		answer(w, `{"status":"success","id":"tr1","name":"Film.2024","type":"nzb"}`)
	})
	mux.HandleFunc("/transfer/list", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		status, folder, file := f.status, "fold1", "null"
		if f.fileID != "" {
			folder, file = "root", `"`+f.fileID+`"`
		}
		f.mu.Unlock()
		answer(w, `{"status":"success","transfers":[`+
			`{"id":"other","name":"Not ours","status":"running","progress":0.1},`+
			`{"id":"tr1","name":"Film.2024","status":"`+status+`","progress":0.5,"folder_id":"`+folder+`","file_id":`+file+`,"message":"par2 failed"}]}`)
	})
	mux.HandleFunc("/folder/list", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		name := f.folderName
		f.mu.Unlock()
		switch r.URL.Query().Get("id") {
		case "fold1":
			answer(w, `{"status":"success","name":"`+name+`","content":[`+
				`{"id":"f1","name":"film.2024.mkv","type":"file","size":7000,"link":"https://pm.example/f1"},`+
				`{"id":"sub","name":"Subs","type":"folder"}]}`)
		case "sub":
			answer(w, `{"status":"success","name":"Subs","content":[{"id":"f2","name":"film.2024.srt","type":"file","size":30,"link":"https://pm.example/f2"}]}`)
		case "root":
			answer(w, `{"status":"success","name":"root","content":[{"id":"f9","name":"film.2024.mkv","type":"file","size":7000}]}`)
		default:
			answer(w, `{"status":"error","message":"no such folder"}`)
		}
	})
	mux.HandleFunc("/item/details", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		answer(w, `{"id":"`+id+`","name":"film.2024.mkv","type":"file","size":7000,"link":"https://pm.example/`+id+`"}`)
	})
	del := func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.deleted = append(f.deleted, r.URL.Path+" "+r.FormValue("id"))
		f.mu.Unlock()
		answer(w, `{"status":"success"}`)
	}
	mux.HandleFunc("/transfer/delete", del)
	mux.HandleFunc("/folder/delete", del)
	mux.HandleFunc("/item/delete", del)
	srv := httptest.NewServer(mux)
	f.t.Cleanup(srv.Close)
	return srv
}

func (f *fakePremiumize) set(fn func(*fakePremiumize)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakePremiumize) deletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deleted)
}

func TestPremiumizeFetchesAnNZBIntoTheCloudAndListsWhatItLeft(t *testing.T) {
	fake := &fakePremiumize{t: t, status: "running", folderName: "Film.2024"}
	pm := NewPremiumize(fake.server().URL, "premiumize", "pm-key")
	ctx := context.Background()

	id, err := pm.Submit(ctx, "Film.2024", []byte(sampleNZB))
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	uploaded := string(fake.nzb)
	fake.mu.Unlock()
	if id != "tr1" || uploaded != sampleNZB {
		t.Fatalf("id = %q, uploaded %q", id, uploaded)
	}

	if st := statusOf(t, pm, id); st.Phase != PhaseFetching || st.Progress != 0.5 {
		t.Fatalf("status = %+v, want it still fetching, half way", st)
	}

	fake.set(func(f *fakePremiumize) { f.status = "finished" })
	st := statusOf(t, pm, id)
	if st.Phase != PhaseReady || len(st.Files) != 2 {
		t.Fatalf("status = %+v, want both files, the one in the subfolder included", st)
	}
	if st.Size != 7030 {
		t.Errorf("size = %d, want the files' sum", st.Size)
	}
	if f := st.Files[1]; f.Dir != "Subs" || f.Name != "film.2024.srt" {
		t.Errorf("the subtitle is %+v, want it in Subs", f)
	}

	link, err := pm.Link(ctx, id, "f1")
	if err != nil || link != "https://pm.example/f1" {
		t.Errorf("link = %q, %v", link, err)
	}

	if err := pm.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got, want := fake.deletes(), []string{"/transfer/delete tr1", "/folder/delete fold1"}; !slices.Equal(got, want) {
		t.Errorf("deleted %v, want the transfer and the folder it left", got)
	}
}

func TestPremiumizeDeletesASingleFileAndLeavesTheFolderItWasPutIn(t *testing.T) {
	fake := &fakePremiumize{t: t, status: "finished", fileID: "f9"}
	pm := NewPremiumize(fake.server().URL, "premiumize", "pm-key")

	st := statusOf(t, pm, "tr1")
	if len(st.Files) != 1 || st.Files[0].ID != "f9" {
		t.Fatalf("files = %+v, want the one file the transfer left", st.Files)
	}
	if err := pm.Delete(context.Background(), "tr1"); err != nil {
		t.Fatal(err)
	}
	if got, want := fake.deletes(), []string{"/transfer/delete tr1", "/item/delete f9"}; !slices.Equal(got, want) {
		t.Errorf("deleted %v, want the transfer and its file, never the folder around it", got)
	}
}

func TestPremiumizeLeavesAFolderNotNamedAfterTheTransfer(t *testing.T) {
	fake := &fakePremiumize{t: t, status: "finished", folderName: "My Films"}
	if err := NewPremiumize(fake.server().URL, "premiumize", "pm-key").Delete(context.Background(), "tr1"); err != nil {
		t.Fatal(err)
	}
	if got, want := fake.deletes(), []string{"/transfer/delete tr1"}; !slices.Equal(got, want) {
		t.Errorf("deleted %v, want only the transfer", got)
	}
}

func TestPremiumizeReportsAFailedTransferWithItsMessage(t *testing.T) {
	fake := &fakePremiumize{t: t, status: "error"}
	st := statusOf(t, NewPremiumize(fake.server().URL, "premiumize", "pm-key"), "tr1")
	if st.Phase != PhaseFailed || st.Reason != "Premiumize.me: par2 failed" {
		t.Errorf("status = %+v, want the transfer's own message", st)
	}
}

func TestPremiumizeRateLimitReadsAsBusy(t *testing.T) {
	fake := &fakePremiumize{t: t, rateOnce: true}
	_, err := NewPremiumize(fake.server().URL, "premiumize", "pm-key").Submit(context.Background(), "x", []byte(sampleNZB))
	if !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v, want ErrBusy", err)
	}
}

func TestPremiumizeLeavesOutATransferItNoLongerHas(t *testing.T) {
	fake := &fakePremiumize{t: t, status: "running"}
	all, err := NewPremiumize(fake.server().URL, "premiumize", "pm-key").Status(context.Background(), []string{"nope", "tr1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := all["nope"]; ok {
		t.Error("a transfer the account does not hold was reported")
	}
	if _, ok := all["tr1"]; !ok {
		t.Error("the transfer the account holds is missing")
	}
}
