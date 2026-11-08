package wetransfer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/wetransfer/wttest"
)

const (
	transferID = "5b2a0b4c7d5c9e6f1a2b3c4d5e6f708192a3b4c5d6e7f8"
	hash       = "a1b2c3"
)

func fake(t *testing.T) (*wttest.Server, Resolver) {
	srv := wttest.New(t)
	c := NewClient(srv.URL, srv.Client())
	c.short = srv.URL
	return srv, Resolver{Client: c}
}

func film() wttest.Transfer {
	return wttest.Transfer{Hash: hash, Files: []wttest.File{
		{ID: "f1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Isle of Dogs.mkv", Body: bytes.Repeat([]byte("mkv"), 100)},
		{ID: "f2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "Subs/Isle of Dogs.srt", Body: []byte("1\n")},
	}}
}

func TestEveryLinkFormIsClaimed(t *testing.T) {
	for _, raw := range []string{
		"https://wetransfer.com/downloads/" + transferID + "/" + hash,
		"https://wetransfer.com/downloads/" + transferID + "/0123456789abcdef0123456789abcdef0123456789abcd/" + hash,
		"https://acme.wetransfer.com/downloads/" + transferID + "/" + hash,
		"https://we.tl/t-ABCdef1234",
		"https://shorturls.wetransfer.com/ABCdef1234",
		"https://go.wetransfer.com/t-ABCdef1234",
	} {
		if !(Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = false", raw)
		}
	}
	for _, raw := range []string{
		"https://wetransfer.com/",
		"https://wetransfer.com/pricing",
		"https://wetransfer.com/downloads/nothex/" + hash,
		"https://we.tl/",
	} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func TestATransferOpensIntoOneRowPerFile(t *testing.T) {
	srv, r := fake(t)
	link, short := srv.Add(transferID, film())

	for _, in := range []string{link, short} {
		got, err := r.List(context.Background(), in, "")
		want := []resolver.Listing{
			{URL: link + "#file=f1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Isle of Dogs.mkv", Size: 300, Folder: "wetransfer_isle-of-dogs_2026-11-08_0712"},
			{URL: link + "#file=f2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "Isle of Dogs.srt", Size: 2, Folder: "wetransfer_isle-of-dogs_2026-11-08_0712/Subs"},
		}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("List(%s) = %+v, %v; want %+v", in, got, err, want)
		}
	}
}

func TestAFileOfATransferDownloadsFromItsDirectLink(t *testing.T) {
	srv, r := fake(t)
	link, _ := srv.Add(transferID, film())

	l, err := r.Client.Unlock(context.Background(), link+"#file=f1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "Isle of Dogs.mkv" || l.Size != 300 || l.Conns != maxConns || !strings.Contains(l.URL, "/storage/") {
		t.Errorf("Unlock = %+v", l)
	}
	resp, err := srv.Client().Get(l.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(body, bytes.Repeat([]byte("mkv"), 100)) {
		t.Error("the link fetched something else than the file")
	}

	zip, err := r.Client.Unlock(context.Background(), link, "")
	if err != nil || !strings.HasSuffix(zip.URL, "/zip") || zip.Name != "wetransfer_isle-of-dogs_2026-11-08_0712.zip" {
		t.Errorf("the transfer itself unlocked as %+v, %v; want its zip", zip, err)
	}
}

func TestAPasswordFromTheLinkOpensALockedTransfer(t *testing.T) {
	srv, r := fake(t)
	tr := film()
	tr.Password = "hunter2"
	link, _ := srv.Add(transferID, tr)

	var ref *hoster.Refusal
	if _, err := r.List(context.Background(), link, ""); !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
		t.Errorf("without a password List = %v, want a refusal asking for one", err)
	}
	if _, err := r.List(context.Background(), link, "wrong"); !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
		t.Errorf("with a wrong password List = %v, want a refusal", err)
	}
	rows, err := r.List(context.Background(), link, "hunter2")
	if err != nil || len(rows) != 2 {
		t.Fatalf("List = %+v, %v", rows, err)
	}
	if _, err := r.Client.Unlock(context.Background(), rows[1].URL, "hunter2"); err != nil {
		t.Errorf("a file of the locked transfer gave %v", err)
	}
}

func TestEveryWayATransferIsHeldBackIsTold(t *testing.T) {
	srv, r := fake(t)
	expired := film()
	expired.Expired = true
	old, _ := srv.Add(transferID, expired)
	verify := film()
	verify.Verify = true
	other, _ := srv.Add("9b2a0b4c7d5c9e6f1a2b3c4d5e6f708192a3b4c5d6e7f8", verify)

	if _, err := r.List(context.Background(), old, ""); !hoster.IsGone(err) {
		t.Errorf("an expired transfer gave %v, want gone", err)
	}
	if _, err := r.Client.Unlock(context.Background(), "https://wetransfer.com/downloads/0000000000000000000000000000000000000000000000/"+hash, ""); !hoster.IsGone(err) {
		t.Errorf("an unknown transfer gave %v, want gone", err)
	}
	_, err := r.Client.Unlock(context.Background(), other+"#file=f1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "")
	var ref *hoster.Refusal
	if !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
		t.Errorf("a transfer for a verified recipient gave %v, want a refusal", err)
	}
}

func TestInspectAsksOncePerTransfer(t *testing.T) {
	srv, r := fake(t)
	link, _ := srv.Add(transferID, film())

	got := r.Inspect(context.Background(), []string{
		link + "#file=f2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		link + "#file=f1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"https://wetransfer.com/downloads/0000000000000000000000000000000000000000000000/" + hash + "#file=f1",
	})
	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Isle of Dogs.srt", Size: 2},
		{Availability: core.AvailOnline, Name: "Isle of Dogs.mkv", Size: 300},
		{Availability: core.AvailOffline},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v, want %+v", got, want)
	}
	prepared := 0
	for _, a := range srv.Asked() {
		if strings.HasSuffix(a, "/prepare-download") {
			prepared++
		}
	}
	if prepared != 2 {
		t.Errorf("asked prepare-download %d times, want once per transfer", prepared)
	}
}
