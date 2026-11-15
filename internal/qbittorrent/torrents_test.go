package qbittorrent

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/qbittest"
)

func TestATorrentToDownloadIsAddedStoppedWithItsTags(t *testing.T) {
	q, c := fake(t)
	err := c.Add(context.Background(), Torrent{
		File:     []byte("d4:infod4:name1:aee"),
		SavePath: "/downloads/x",
		Category: "knightloader",
		Tags:     []string{"knightloader", "task-1"},
		Download: true,
		Stopped:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	add := q.Added()
	if len(add) != 1 {
		t.Fatalf("added %v", add)
	}
	a := add[0]
	if a.Fields["skip_checking"] != "false" || a.Fields["paused"] != "true" || a.Fields["stopped"] != "true" {
		t.Errorf("fields %v", a.Fields)
	}
	if a.Fields["tags"] != "knightloader,task-1" || a.Fields["category"] != "knightloader" || a.Fields["savepath"] != "/downloads/x" {
		t.Errorf("fields %v", a.Fields)
	}
	if string(a.File) != "d4:infod4:name1:aee" {
		t.Errorf("file %q", a.File)
	}
}

func TestATorrentToSeedSkipsTheCheckAndStarts(t *testing.T) {
	q, c := fake(t)
	if err := c.Add(context.Background(), Torrent{Magnet: "magnet:?xt=urn:btih:abc", SavePath: "/d"}); err != nil {
		t.Fatal(err)
	}
	f := q.Added()[0].Fields
	if f["skip_checking"] != "true" || f["seedMode"] != "true" || f["paused"] != "false" || f["urls"] != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("fields %v", f)
	}
}

func TestTheTorrentsOfATagAreListedWithTheirNumbers(t *testing.T) {
	q, c := fake(t)
	q.Put(qbittest.Torrent{Hash: "aa", Name: "ours", Tags: "knightloader", Uploaded: 300, Ratio: 1.5, Seeds: 4, Leechs: 2, SeedingTime: 60, State: "uploading"})
	q.Put(qbittest.Torrent{Hash: "bb", Name: "someone else's", Tags: "other"})
	got, err := c.Torrents(context.Background(), Filter{Tag: "knightloader"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("listed %+v", got)
	}
	g := got[0]
	if g.Hash != "aa" || g.Uploaded != 300 || g.Ratio != 1.5 || g.Seeds != 4 || g.Leechs != 2 || g.SeedingTime != 60 || g.State != "uploading" {
		t.Fatalf("listed %+v", g)
	}
}

func TestATorrentsFilesAreReadAndPicked(t *testing.T) {
	q, c := fake(t)
	q.Put(qbittest.Torrent{Hash: "aa", Files: []qbittest.File{{Name: "x/a.mkv", Size: 10}, {Name: "x/b.nfo", Size: 1}, {Name: "x/c.srt", Size: 2}}})
	ctx := context.Background()
	if err := c.SetFilePriority(ctx, "aa", []int{1, 2}, 0); err != nil {
		t.Fatal(err)
	}
	files, err := c.Files(ctx, "aa")
	if err != nil {
		t.Fatal(err)
	}
	var prio []int
	for _, f := range files {
		prio = append(prio, f.Priority)
	}
	if !slices.Equal(prio, []int{1, 0, 0}) || files[0].Name != "x/a.mkv" || files[0].Index != 0 {
		t.Fatalf("files %+v", files)
	}
}

func TestStopAndStartSpeakBothGenerationsOfTheAPI(t *testing.T) {
	for _, modern := range []bool{false, true} {
		q, c := fake(t)
		q.Modern = modern
		q.Put(qbittest.Torrent{Hash: "aa", State: "downloading"})
		ctx := context.Background()
		if err := c.Stop(ctx, "aa"); err != nil {
			t.Fatalf("modern %v: %v", modern, err)
		}
		if s := q.Get("aa").State; s != "pausedDL" && s != "stoppedDL" {
			t.Fatalf("modern %v: state %s after Stop", modern, s)
		}
		if err := c.Start(ctx, "aa"); err != nil {
			t.Fatalf("modern %v: %v", modern, err)
		}
		if s := q.Get("aa").State; s != "downloading" {
			t.Fatalf("modern %v: state %s after Start", modern, s)
		}
	}
}

func TestDeleteRemovesTheTorrentAndSaysWhetherItsFilesGo(t *testing.T) {
	q, c := fake(t)
	q.Put(qbittest.Torrent{Hash: "aa"})
	if err := c.Delete(context.Background(), true, "aa"); err != nil {
		t.Fatal(err)
	}
	if q.Get("aa") != nil || !slices.Equal(q.DeletedWithFiles(), []string{"aa"}) {
		t.Fatalf("still %+v, deleted with files %v", q.Get("aa"), q.DeletedWithFiles())
	}
}

func TestATorrentIsMovedToAnotherFolder(t *testing.T) {
	q, c := fake(t)
	q.Put(qbittest.Torrent{Hash: "aa", SavePath: "/a"})
	if err := c.SetLocation(context.Background(), "/b", "aa"); err != nil {
		t.Fatal(err)
	}
	if q.Get("aa").SavePath != "/b" {
		t.Fatalf("save path %s", q.Get("aa").SavePath)
	}
}

func TestThePiecesOfATorrentAreRead(t *testing.T) {
	q, c := fake(t)
	q.Put(qbittest.Torrent{Hash: "aa", Pieces: []int{2, 2, 1, 0}, PieceSize: 16384})
	ctx := context.Background()
	states, err := c.PieceStates(ctx, "aa")
	if err != nil || !slices.Equal(states, []int{2, 2, 1, 0}) {
		t.Fatalf("PieceStates = %v, %v", states, err)
	}
	p, err := c.Properties(ctx, "aa")
	if err != nil || p.PieceSize != 16384 || p.Pieces != 4 {
		t.Fatalf("Properties = %+v, %v", p, err)
	}
}

func TestATorrentQBittorrentDoesNotHaveSaysSo(t *testing.T) {
	_, c := fake(t)
	if _, err := c.Files(context.Background(), "ff"); reason(err) != "qBittorrent has no torrent ff" {
		t.Fatalf("err = %v", err)
	}
}

func TestATorrentQBittorrentDoesNotHaveIsErrNoTorrent(t *testing.T) {
	_, c := fake(t)
	if _, err := c.Files(context.Background(), "ff"); !errors.Is(err, ErrNoTorrent) {
		t.Fatalf("err = %v", err)
	}
}

func TestAMagnetCanStopOnceItHasItsMetadata(t *testing.T) {
	q, c := fake(t)
	if err := c.Add(context.Background(), Torrent{Magnet: "magnet:?xt=urn:btih:abc", SavePath: "/d", Download: true, StopAtMetadata: true}); err != nil {
		t.Fatal(err)
	}
	f := q.Added()[0].Fields
	if f["stopCondition"] != "MetadataReceived" || f["paused"] != "false" {
		t.Fatalf("fields %v", f)
	}
}

func TestTrackersAreAddedToATorrent(t *testing.T) {
	q, c := fake(t)
	q.Put(qbittest.Torrent{Hash: "aa"})
	if err := c.AddTrackers(context.Background(), "aa", []string{"udp://a:1/announce", "http://b/announce"}); err != nil {
		t.Fatal(err)
	}
	if got := q.Get("aa").Trackers; !slices.Equal(got, []string{"udp://a:1/announce", "http://b/announce"}) {
		t.Fatalf("trackers %v", got)
	}
}

func TestSequentialDownloadAndFirstAndLastPiecesAreToggled(t *testing.T) {
	q, c := fake(t)
	q.Put(qbittest.Torrent{Hash: "aa"})
	ctx := context.Background()
	if err := c.ToggleSequential(ctx, "aa"); err != nil {
		t.Fatal(err)
	}
	if err := c.ToggleFirstLastPiece(ctx, "aa"); err != nil {
		t.Fatal(err)
	}
	got, err := c.Torrents(ctx, Filter{Hashes: []string{"aa"}})
	if err != nil || len(got) != 1 || !got[0].Sequential || !got[0].FirstLast {
		t.Fatalf("Torrents = %+v, %v", got, err)
	}
}

func TestTheNetworkInterfacesAreListedAsQBittorrentNamesThem(t *testing.T) {
	q, c := fake(t)
	q.Interfaces = []qbittest.Interface{
		{Name: "Ethernet", Value: "ethernet_32769", Addrs: []string{"192.168.1.5"}},
		{Name: "Loopback Pseudo-Interface 1", Value: "loopback_0", Addrs: []string{"::1", "127.0.0.1"}},
	}
	ctx := context.Background()
	got, err := c.NetworkInterfaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Interface{{Name: "Ethernet", Value: "ethernet_32769"}, {Name: "Loopback Pseudo-Interface 1", Value: "loopback_0"}}
	if !slices.Equal(got, want) {
		t.Fatalf("interfaces %+v", got)
	}
	addrs, err := c.InterfaceAddresses(ctx, "loopback_0")
	if err != nil || !slices.Equal(addrs, []string{"::1", "127.0.0.1"}) {
		t.Fatalf("addresses %v, %v", addrs, err)
	}
}

func TestATorrentIsHandedBackAsItsDotTorrent(t *testing.T) {
	_, c := fake(t)
	ctx := context.Background()
	raw := []byte("d4:infod4:name1:aee")
	if err := c.Add(ctx, Torrent{File: raw, SavePath: "/d"}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Export(ctx, "0b5aeea4cc4c4c56e2e28e1fe3d7d23cd76e1e9e")
	if !errors.Is(err, ErrNoTorrent) {
		t.Errorf("a torrent it does not hold: %v", err)
	}
	list, _ := c.Torrents(ctx, Filter{})
	if got, err = c.Export(ctx, list[0].Hash); err != nil || string(got) != string(raw) {
		t.Fatalf("export %q, %v", got, err)
	}
}
