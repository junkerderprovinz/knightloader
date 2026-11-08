package swisstransfer

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/swisstransfer/sttest"
)

const linkID = "01a1a57e-cc7d-7249-ac30-4c3063a223f5"

func fake(t *testing.T) (*sttest.Server, Resolver) {
	srv := sttest.New(t)
	return srv, Resolver{Client: NewClient(srv.URL, srv.Client())}
}

func course() sttest.Transfer {
	return sttest.Transfer{
		ID: "01a1a57e-cc7a-71b1-a50a-e6e5e4365b5e", Title: "RVK-Kurs", ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
		Files: []sttest.File{
			{ID: "01a1a57e-cc80-7325-b632-ec859a727292", Path: "RVK-Selbstlernkurs.docx", Body: make([]byte, 16)},
			{ID: "01a1a57e-cc80-7325-b632-ec859ab73531", Path: "Export/RVK-Selbstlernkurs.pdf", Body: []byte("%PDF-1.6")},
		},
	}
}

func TestEveryLinkFormIsClaimed(t *testing.T) {
	for _, raw := range []string{
		"https://www.swisstransfer.com/dl/" + linkID,
		"https://swisstransfer.com/dl/" + linkID + "#file=01a1a57e-cc80-7325-b632-ec859a727292",
		"https://www.swisstransfer.com/d/6edf5a5b-55e5-4d73-83bb-5bee140e47cb",
	} {
		if !(Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = false", raw)
		}
	}
	for _, raw := range []string{
		"https://www.swisstransfer.com/",
		"https://www.swisstransfer.com/dl/not-a-uuid",
		"https://www.swisstransfer.com/en-gb/faq",
	} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func TestATransferOpensIntoOneRowPerFile(t *testing.T) {
	srv, r := fake(t)
	link := srv.Add(linkID, course())

	got, err := r.List(context.Background(), link, "")
	want := []resolver.Listing{
		{URL: link + "#file=01a1a57e-cc80-7325-b632-ec859a727292", Name: "RVK-Selbstlernkurs.docx", Size: 16, Folder: "RVK-Kurs"},
		{URL: link + "#file=01a1a57e-cc80-7325-b632-ec859ab73531", Name: "RVK-Selbstlernkurs.pdf", Size: 8, Folder: "RVK-Kurs/Export"},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("List = %+v, %v; want %+v", got, err, want)
	}
}

func TestAFileDownloadsFromTheSignedLinkTheAPIHandsOut(t *testing.T) {
	srv, r := fake(t)
	link := srv.Add(linkID, course())

	l, err := r.Client.Unlock(context.Background(), link+"#file=01a1a57e-cc80-7325-b632-ec859ab73531", "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "RVK-Selbstlernkurs.pdf" || l.Size != 8 || !strings.Contains(l.URL, "X-Amz-Signature=") {
		t.Errorf("Unlock = %+v", l)
	}
	resp, err := srv.Client().Get(l.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "%PDF-1.6" {
		t.Errorf("the link fetched %q", body)
	}
}

func TestAPasswordFromTheLinkOpensALockedTransfer(t *testing.T) {
	srv, r := fake(t)
	tr := course()
	tr.Password = "hunter2"
	link := srv.Add(linkID, tr)

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
	if l, err := r.Client.Unlock(context.Background(), rows[0].URL, "hunter2"); err != nil || l.URL == "" {
		t.Errorf("a file of the locked transfer gave %+v, %v", l, err)
	}
}

func TestEveryWayATransferIsGoneIsTold(t *testing.T) {
	srv, r := fake(t)
	old := course()
	old.ExpiresAt = time.Now().Add(-time.Hour).Unix()
	expired := srv.Add("01a1a57e-0000-7000-8000-000000000001", old)
	used := course()
	used.Exceeded = true
	exceeded := srv.Add("01a1a57e-0000-7000-8000-000000000002", used)

	if _, err := r.List(context.Background(), expired, ""); !hoster.IsGone(err) {
		t.Errorf("an expired transfer gave %v, want gone", err)
	}
	if _, err := r.List(context.Background(), "https://www.swisstransfer.com/dl/0b7c3d2e-1111-4222-8333-944455556666", ""); !hoster.IsGone(err) {
		t.Errorf("an unknown transfer gave %v, want gone", err)
	}
	if _, err := r.Client.Unlock(context.Background(), exceeded+"#file=01a1a57e-cc80-7325-b632-ec859a727292", ""); !hoster.IsGone(err) {
		t.Errorf("a transfer downloaded as often as allowed gave %v, want gone", err)
	}
}

func TestInspectReadsEachTransferOnce(t *testing.T) {
	srv, r := fake(t)
	link := srv.Add(linkID, course())

	got := r.Inspect(context.Background(), []string{
		link + "#file=01a1a57e-cc80-7325-b632-ec859ab73531",
		link + "#file=01a1a57e-cc80-7325-b632-ec859a727292",
		"https://www.swisstransfer.com/dl/0b7c3d2e-1111-4222-8333-944455556666#file=x",
	})
	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "RVK-Selbstlernkurs.pdf", Size: 8},
		{Availability: core.AvailOnline, Name: "RVK-Selbstlernkurs.docx", Size: 16},
		{Availability: core.AvailOffline},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v, want %+v", got, want)
	}
	pages := 0
	for _, a := range srv.Asked() {
		if strings.HasPrefix(a, "GET /dl/") {
			pages++
		}
	}
	if pages != 2 {
		t.Errorf("read %d transfer pages, want one per transfer", pages)
	}
}
