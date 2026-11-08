package mega

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/mega/megatest"
)

func TestEveryLinkFormMEGAHandsOutIsRead(t *testing.T) {
	fileKey := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	folderKey := "AAAAAAAAAAAAAAAAAAAAAA"
	for _, tc := range []struct {
		link   string
		folder bool
		handle string
		node   string
		sub    bool
	}{
		{link: "https://mega.nz/file/AbCdEfGh#" + fileKey, handle: "AbCdEfGh"},
		{link: "https://www.mega.nz/embed/AbCdEfGh#" + fileKey, handle: "AbCdEfGh"},
		{link: "https://mega.nz/#!AbCdEfGh!" + fileKey, handle: "AbCdEfGh"},
		{link: "https://mega.co.nz/#!AbCdEfGh!" + fileKey, handle: "AbCdEfGh"},
		{link: "https://mega.nz/folder/FoLdEr12#" + folderKey, folder: true, handle: "FoLdEr12"},
		{link: "https://mega.nz/folder/FoLdEr12#" + folderKey + "/file/NoDe1234", folder: true, handle: "FoLdEr12", node: "NoDe1234"},
		{link: "https://mega.nz/folder/FoLdEr12#" + folderKey + "/folder/NoDe1234", folder: true, handle: "FoLdEr12", node: "NoDe1234", sub: true},
		{link: "https://mega.nz/#F!FoLdEr12!" + folderKey, folder: true, handle: "FoLdEr12"},
		{link: "https://mega.co.nz/#F!FoLdEr12!" + folderKey + "!NoDe1234", folder: true, handle: "FoLdEr12", node: "NoDe1234", sub: true},
	} {
		l, err := Parse(tc.link)
		if err != nil {
			t.Errorf("%s: %v", tc.link, err)
			continue
		}
		if l.Folder != tc.folder || l.Handle != tc.handle || l.Node != tc.node || l.SubFolder != tc.sub {
			t.Errorf("%s read as %+v", tc.link, l)
		}
		again, err := Parse(l.String())
		if err != nil || again.Handle != l.Handle || again.Node != l.Node || !bytes.Equal(again.Key, l.Key) {
			t.Errorf("%s does not read back as itself from %s", tc.link, l.String())
		}
	}
}

func TestLinksThatAreNotMEGAFilesAreLeftAlone(t *testing.T) {
	r := Resolver{}
	for _, link := range []string{
		"https://mega.nz/",
		"https://mega.nz/login",
		"https://example.com/file/AbCdEfGh#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"https://notmega.nz/file/AbCdEfGh#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	} {
		if r.Match(link) {
			t.Errorf("%s was claimed", link)
		}
	}
}

func TestAKeyThatIsMissingOrCutShortIsClaimedAndSaysSo(t *testing.T) {
	for link, says := range map[string]string{
		"https://mega.nz/file/AbCdEfGh#AAAAAAAAAA": "cut short",
		"https://mega.nz/file/AbCdEfGh":            "no key",
	} {
		if !(Resolver{}).Match(link) {
			t.Errorf("%s was left to another backend", link)
			continue
		}
		_, err := (Resolver{}).Resolve(context.Background(), resolverRequest(link))
		if err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("Resolve(%s) = %v, want it to say %q", link, err, says)
		}
	}
}

func TestStreamedMACMatchesTheWholeFileMAC(t *testing.T) {
	k, _ := splitFileKey(megatest.Random(32))
	for _, size := range []int{1, 15, 16, 17, 128 << 10, 128<<10 + 1, 3 << 20, 5<<20 + 7} {
		plain := megatest.Random(size)
		m, _ := newChunkMAC(k)
		for rest := plain; len(rest) > 0; {
			n := min(len(rest), 1+rand.IntN(70000))
			_, _ = m.Write(rest[:n])
			rest = rest[n:]
		}
		if got, want := m.Sum(), megatest.MAC(k.aes, k.nonce, plain); !bytes.Equal(got, want) {
			t.Errorf("%d bytes: streamed MAC %x, whole-file MAC %x", size, got, want)
		}
	}
}

// sampleFolder shares Holiday/{beach.jpg, Clips/{wave.mp4, Raw/{wave.raw}}}.
func sampleFolder(f *megatest.Server) string {
	return f.ShareFolder("HoLiDay1",
		megatest.Folder("root0001", "owner", "Holiday"),
		megatest.FileNode("beach001", "root0001", megatest.NewFile("beach.jpg", megatest.Random(3000))),
		megatest.Folder("clips001", "root0001", "Clips"),
		megatest.FileNode("wave0001", "clips001", megatest.NewFile("wave.mp4", megatest.Random(200_000))),
		megatest.Folder("raw00001", "clips001", "Raw"),
		megatest.FileNode("wave0002", "raw00001", megatest.NewFile("wave.raw", megatest.Random(17))),
	)
}

func TestAFolderOpensIntoItsFilesWithTheirFolders(t *testing.T) {
	f := megatest.New(t)
	link := sampleFolder(f)
	got, err := Resolver{Client: client(f)}.List(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []Listing{
		{Name: "beach.jpg", Size: 3000, Folder: "Holiday"},
		{Name: "wave.mp4", Size: 200_000, Folder: "Holiday/Clips"},
		{Name: "wave.raw", Size: 17, Folder: "Holiday/Clips/Raw"},
	}
	if len(got) != len(want) {
		t.Fatalf("listed %+v, want %d files", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.Name || g.Size != w.Size || g.Folder != w.Folder {
			t.Errorf("file %d = %+v, want %+v", i, g, w)
		}
		l, err := Parse(g.URL)
		if err != nil || !l.Folder || l.Handle != "HoLiDay1" || l.Node == "" || l.SubFolder {
			t.Errorf("%s is not a link to a file in the folder: %+v, %v", g.URL, l, err)
		}
	}
}

func TestASubfolderLinkOpensOnlyThatSubfolder(t *testing.T) {
	f := megatest.New(t)
	link := sampleFolder(f) + "/folder/clips001"
	got, err := Resolver{Client: client(f)}.List(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Folder != "Clips" || got[1].Folder != "Clips/Raw" {
		t.Errorf("listed %+v, want the two files under Clips", got)
	}
}

func TestAFileInAFolderIsItsOwnRow(t *testing.T) {
	f := megatest.New(t)
	link := sampleFolder(f) + "/file/beach001"
	got, err := Resolver{Client: client(f)}.List(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "beach.jpg" || got[0].Folder != "Holiday" {
		t.Errorf("listed %+v, want the one file", got)
	}
}

func TestNamesAndSizesComeDecryptedAndADeadLinkIsOffline(t *testing.T) {
	f := megatest.New(t)
	link := f.Share("SoLo0001", megatest.NewFile("Grüße aus Wien.pdf", megatest.Random(4096)))
	inFolder := sampleFolder(f) + "/file/wave0001"
	dead := "https://mega.nz/file/GoNe0001#" + strings.Repeat("A", 43)

	got := Resolver{Client: client(f)}.Inspect(context.Background(), []string{link, inFolder, dead})
	want := []Info{
		{Availability: core.AvailOnline, Name: "Grüße aus Wien.pdf", Size: 4096},
		{Availability: core.AvailOnline, Name: "wave.mp4", Size: 200_000},
		{Availability: core.AvailOffline},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAWrongKeyNamesNothing(t *testing.T) {
	f := megatest.New(t)
	link := f.Share("SoLo0001", megatest.NewFile("a.bin", megatest.Random(64)))
	l, _ := Parse(link)
	l.Key = megatest.Random(32)
	got := Resolver{Client: client(f)}.Inspect(context.Background(), []string{l.String()})
	if got[0].Name != "" || got[0].Availability == core.AvailOffline {
		t.Errorf("a wrong key gave %+v, want no opinion", got[0])
	}
}

func TestAProofOfWorkIsAnswered(t *testing.T) {
	f := megatest.New(t)
	link := f.Share("SoLo0001", megatest.NewFile("a.bin", megatest.Random(64)))
	// Easiness 255 puts the threshold at half the range, so a test solves it
	// in a couple of tries.
	f.Demand("1:255:1700000000:" + strings.Repeat("Q", 64))
	got := Resolver{Client: client(f)}.Inspect(context.Background(), []string{link})
	if got[0].Name != "a.bin" {
		t.Errorf("after the challenge: %+v", got[0])
	}
}

// recorder collects a backend's updates for one task.
type recorder struct {
	updates chan core.Update
}

func newRecorder() *recorder { return &recorder{updates: make(chan core.Update, 256)} }

func (r *recorder) on(_ string, u core.Update) { r.updates <- u }

// settled waits for the task's last update, done or failed.
func (r *recorder) settled(t *testing.T) core.Update {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case u := <-r.updates:
			if u.Status == core.StatusDone || u.Status == core.StatusError {
				return u
			}
		case <-deadline:
			t.Fatal("the download never settled")
		}
	}
}

func newTestBackend(t *testing.T, f *megatest.Server) (*Backend, *recorder, string) {
	dir := t.TempDir()
	rec := newRecorder()
	return NewBackend(client(f), f.Client(), dir, rec.on), rec, dir
}

func TestAFileIsDownloadedAndDecrypted(t *testing.T) {
	f := megatest.New(t)
	ff := megatest.NewFile("film.mkv", megatest.Random(2<<20+123))
	link := f.Share("SoLo0001", ff)
	b, rec, dir := newTestBackend(t, f)

	b.Download("t1", link, nil, 1)
	u := rec.settled(t)
	if u.Status != core.StatusDone {
		t.Fatalf("download failed: %s", u.Err)
	}
	if u.File != filepath.Join(dir, "film.mkv") {
		t.Errorf("saved as %s", u.File)
	}
	got, err := os.ReadFile(u.File)
	if err != nil || !bytes.Equal(got, ff.Plain) {
		t.Errorf("the saved file is not the decrypted content (%v)", err)
	}
	if _, err := os.Stat(PartFile(dir, link, "t1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the part file is still there: %v", err)
	}
}

func TestAFileFromAFolderIsDownloaded(t *testing.T) {
	f := megatest.New(t)
	link := sampleFolder(f) + "/file/wave0001"
	b, rec, _ := newTestBackend(t, f)

	b.Download("t1", link, nil, 1)
	u := rec.settled(t)
	if u.Status != core.StatusDone || filepath.Base(u.File) != "wave.mp4" || u.Size != 200_000 {
		t.Fatalf("got %+v", u)
	}
}

func TestContentThatFailsItsMACIsThrownAway(t *testing.T) {
	f := megatest.New(t)
	ff := megatest.NewFile("film.mkv", megatest.Random(300_000))
	ff.Cipher[150_000] ^= 1
	link := f.Share("SoLo0001", ff)
	b, rec, dir := newTestBackend(t, f)

	b.Download("t1", link, nil, 1)
	u := rec.settled(t)
	if u.Status != core.StatusError || u.Err != ErrMAC.Error() {
		t.Fatalf("got %+v, want the MAC failure", u)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("left behind %v", entries)
	}
}

func TestASpentQuotaWaitsAsLongAsMEGASays(t *testing.T) {
	f := megatest.New(t)
	link := f.Share("SoLo0001", megatest.NewFile("film.mkv", megatest.Random(1000)))
	f.SpendQuota(3600)
	b, rec, _ := newTestBackend(t, f)

	b.Download("t1", link, nil, 1)
	u := rec.settled(t)
	if u.Status != core.StatusError || u.Reason != core.ReasonLimit || u.Retry != time.Hour {
		t.Fatalf("got %+v, want a limit that is retried in an hour", u)
	}
	if !strings.Contains(u.Err, "transfer quota") {
		t.Errorf("the message %q does not say what ran out", u.Err)
	}
}

func TestADownloadServerOutOfBandwidthWaitsToo(t *testing.T) {
	f := megatest.New(t)
	link := f.Share("SoLo0001", megatest.NewFile("film.mkv", megatest.Random(1000)))
	f.SpendBandwidth()
	b, rec, _ := newTestBackend(t, f)

	b.Download("t1", link, nil, 1)
	u := rec.settled(t)
	if u.Reason != core.ReasonLimit || u.Retry != 2*time.Minute {
		t.Fatalf("got %+v, want a limit that is retried in two minutes", u)
	}
}

func TestAPartFileIsCarriedOnFrom(t *testing.T) {
	f := megatest.New(t)
	ff := megatest.NewFile("film.mkv", megatest.Random(400_000))
	link := f.Share("SoLo0001", ff)
	b, rec, dir := newTestBackend(t, f)
	// Not on a block boundary, as a transfer cut off mid-read leaves it.
	if err := os.WriteFile(PartFile(dir, link, "t1"), ff.Plain[:250_007], 0o644); err != nil {
		t.Fatal(err)
	}

	b.Download("t1", link, nil, 1)
	u := rec.settled(t)
	if u.Status != core.StatusDone {
		t.Fatalf("resume failed: %s", u.Err)
	}
	got, _ := os.ReadFile(u.File)
	if !bytes.Equal(got, ff.Plain) {
		t.Error("the resumed file is not the content")
	}
	if r := f.Ranges(); len(r) != 1 || r[0] != "250000-399999" {
		t.Errorf("asked for %v, want the rest from the last whole block", r)
	}
}

func resolverRequest(link string) resolver.Request { return resolver.Request{URL: link} }

func client(f *megatest.Server) *Client { return NewClient(f.URL, f.Client()) }
