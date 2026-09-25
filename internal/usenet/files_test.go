package usenet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

type fakeEngine struct {
	got    chan string
	relink func(context.Context) (string, error)
}

func (f *fakeEngine) Handover(_, url string, _ int, relink func(context.Context) (string, error)) {
	f.relink = relink
	f.got <- url
}
func (f *fakeEngine) Pause(string)        {}
func (f *fakeEngine) Resume(string)       {}
func (f *fakeEngine) Remove(string, bool) {}

func TestAFileLinkNamesTheAccountTheJobAndTheFile(t *testing.T) {
	link := FileLink(resolver.SlotID("torbox", "work"), "77", File{ID: "3", Name: "Show S01E01 #1.mkv"})
	if !strings.HasPrefix(link, "usenet://torbox/77/3/") {
		t.Errorf("link = %q", link)
	}
	ref, err := parseFileLink(link)
	if err != nil {
		t.Fatal(err)
	}
	want := fileRef{slot: "torbox#work", job: "77", file: "3", name: "Show S01E01 #1.mkv"}
	if ref != want {
		t.Errorf("read back %+v, want %+v", ref, want)
	}

	res, err := Resolver{}.Resolve(context.Background(), resolver.Request{URL: link})
	if err != nil || res.Name != "Show S01E01 #1.mkv" {
		t.Errorf("resolved %+v, %v; want the file name from the link", res, err)
	}
	if (Resolver{}).Match("https://torbox.app/77/3/x.mkv") {
		t.Error("the resolver claims an ordinary link")
	}
}

func TestTheFilesBackendAsksForTheAddressWhenTheDownloadStarts(t *testing.T) {
	svc := &fakeService{slot: "torbox"}
	eng := &fakeEngine{got: make(chan string, 1)}
	b := NewFiles(eng, func(slot string) Service {
		if slot == "torbox" {
			return svc
		}
		return nil
	}, func(string, core.Update) {})

	b.Download("t1", FileLink("torbox", "77", File{ID: "3", Name: "a.mkv"}), nil, 4)
	select {
	case url := <-eng.got:
		if url != "https://cdn.example/77/3" {
			t.Fatalf("the engine got %q, want the address the service handed out", url)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was handed to the engine")
	}
	fresh, err := eng.relink(context.Background())
	if err != nil || fresh != "https://cdn.example/77/3" {
		t.Errorf("relink = %q, %v; want the service asked again", fresh, err)
	}
}

func TestAFileAddressIsAskedForAgainWhileTheServiceIsBusy(t *testing.T) {
	svc := &fakeService{slot: "torbox", linkErr: []error{
		fmt.Errorf("torbox usenet/requestdl: %w", ErrBusy),
		unreachable{errors.New("torbox usenet/requestdl: 502 Bad Gateway")},
	}}
	eng := &fakeEngine{got: make(chan string, 1)}
	var mu sync.Mutex
	var updates []core.Update
	b := NewFiles(eng, func(string) Service { return svc }, func(_ string, u core.Update) {
		mu.Lock()
		updates = append(updates, u)
		mu.Unlock()
	})
	b.wait = time.Millisecond

	b.Download("t1", FileLink("torbox", "77", File{ID: "3", Name: "a.mkv"}), nil, 4)
	select {
	case url := <-eng.got:
		if url != "https://cdn.example/77/3" {
			t.Errorf("the engine got %q", url)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was handed to the engine")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, u := range updates {
		if u.Status == core.StatusError {
			t.Errorf("the task failed on the way (%s), want it to wait for the service", u.Err)
		}
	}
}

func TestAFileWhoseAccountIsGoneFails(t *testing.T) {
	failed := make(chan string, 1)
	b := NewFiles(&fakeEngine{got: make(chan string, 1)}, func(string) Service { return nil }, func(_ string, u core.Update) {
		if u.Status == core.StatusError {
			failed <- u.Err
		}
	})
	b.Download("t1", FileLink("premiumize", "tr1", File{ID: "f1", Name: "a.mkv"}), nil, 0)
	select {
	case msg := <-failed:
		if !strings.Contains(msg, "no longer set up") {
			t.Errorf("error = %q, want it to say the account is gone", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the task never failed")
	}
}
