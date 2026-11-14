package torrents

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/qbittest"
)

func serve(t *testing.T) (*qbittest.Server, Client) {
	t.Helper()
	q := qbittest.New("knightloader", "secret")
	srv := httptest.NewServer(q)
	t.Cleanup(srv.Close)
	c, err := qbittorrent.New(srv.URL, "knightloader", "secret")
	if err != nil {
		t.Fatal(err)
	}
	return q, QBit(func(context.Context) (*qbittorrent.Client, error) { return c, nil })
}

func TestAnUploadIsAddedStoppedWithTheTagAndItsTrackers(t *testing.T) {
	q, c := serve(t)
	ctx := context.Background()
	hash := bencode.InfoHash([]byte("d4:name1:ae"))
	err := c.Add(ctx, Add{Hash: hash, File: []byte("d4:infod4:name1:aee"), Dir: "/d", Trackers: []string{"udp://t:1/announce"}})
	if err != nil {
		t.Fatal(err)
	}
	f := q.Added()[0].Fields
	if f["paused"] != "true" || f["skip_checking"] != "false" || f["tags"] != Tag || f["savepath"] != "/d" || f["contentLayout"] != "Original" {
		t.Fatalf("fields %v", f)
	}
	if got := q.Get(hash).Trackers; !slices.Equal(got, []string{"udp://t:1/announce"}) {
		t.Fatalf("trackers %v", got)
	}
}

func TestAMagnetCarriesItsTrackersAndStopsAtItsMetadata(t *testing.T) {
	q, c := serve(t)
	err := c.Add(context.Background(), Add{Hash: "aa", Magnet: "magnet:?xt=urn:btih:aa", Dir: "/d", Trackers: []string{"udp://t:1/announce"}})
	if err != nil {
		t.Fatal(err)
	}
	f := q.Added()[0].Fields
	if f["paused"] != "false" || f["stopCondition"] != "MetadataReceived" {
		t.Fatalf("fields %v", f)
	}
	if f["urls"] != "magnet:?xt=urn:btih:aa&tr=udp%3A%2F%2Ft%3A1%2Fannounce" {
		t.Fatalf("urls %q", f["urls"])
	}
}

func TestACompleteTorrentSkipsTheCheck(t *testing.T) {
	q, c := serve(t)
	if err := c.Add(context.Background(), Add{Hash: "aa", File: []byte("d4:infod4:name1:aee"), Dir: "/d", Complete: true}); err != nil {
		t.Fatal(err)
	}
	if f := q.Added()[0].Fields; f["skip_checking"] != "true" {
		t.Fatalf("fields %v", f)
	}
}

func TestFilesArePickedByTheirPriority(t *testing.T) {
	q, c := serve(t)
	q.Put(qbittest.Torrent{Hash: "aa", Tags: Tag, Files: []qbittest.File{{Name: "x/a", Size: 10, Progress: 0.5}, {Name: "x/b", Size: 4}}})
	ctx := context.Background()
	if err := c.Pick(ctx, "aa", []bool{false, true}); err != nil {
		t.Fatal(err)
	}
	files, err := c.Files(ctx, "aa")
	if err != nil {
		t.Fatal(err)
	}
	want := []File{{Path: "x/a", Size: 10, Done: 5}, {Path: "x/b", Size: 4, Picked: true}}
	if !slices.Equal(files, want) {
		t.Fatalf("files %+v, want %+v", files, want)
	}
}

func TestATorrentTheClientLacksIsErrUnknown(t *testing.T) {
	_, c := serve(t)
	if _, err := c.Files(context.Background(), "ff"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadingsListOnlyTheTaggedTorrentsInTheEnginesTerms(t *testing.T) {
	q, c := serve(t)
	q.Put(qbittest.Torrent{Hash: "aa", Tags: Tag, State: "stalledUP", Progress: 1, Completed: 100, Size: 100, Uploaded: 50, Ratio: 0.5, Seeds: 2, Leechs: 3, SeedingTime: 9, SavePath: "/d"})
	q.Put(qbittest.Torrent{Hash: "bb", Tags: Tag, State: "missingFiles"})
	q.Put(qbittest.Torrent{Hash: "cc", Tags: "other", State: "downloading"})
	got, err := c.Readings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("readings %+v", got)
	}
	want := Reading{State: Seeding, Dir: "/d", Done: 100, Size: 100, Uploaded: 50, Ratio: 0.5, Peers: 5, Seeds: 2, SeedSeconds: 9}
	if got["aa"] != want {
		t.Errorf("aa = %+v, want %+v", got["aa"], want)
	}
	if got["bb"].State != Failed || got["bb"].Err == "" {
		t.Errorf("bb = %+v", got["bb"])
	}
}

func TestTheStatesOfQBittorrentMapOntoTheEngines(t *testing.T) {
	for raw, want := range map[string]State{
		"metaDL": Fetching, "forcedMetaDL": Fetching, "downloading": Fetching, "stalledDL": Fetching, "queuedDL": Fetching, "forcedDL": Fetching,
		"checkingDL": Checking, "checkingUP": Checking, "checkingResumeData": Checking, "moving": Checking, "allocating": Checking,
		"pausedDL": Stopped, "stoppedDL": Stopped,
		"uploading": Seeding, "stalledUP": Seeding, "queuedUP": Seeding, "forcedUP": Seeding,
		"pausedUP": Finished, "stoppedUP": Finished,
		"error": Failed, "missingFiles": Failed, "unknown": Fetching,
	} {
		if got, _ := stateOf(raw); got != want {
			t.Errorf("%s = %v, want %v", raw, got, want)
		}
	}
}

func TestStreamSetsSequentialOrderOnceWhicheverWayItStood(t *testing.T) {
	q, c := serve(t)
	q.Put(qbittest.Torrent{Hash: "aa", Tags: Tag, FirstLast: true})
	ctx := context.Background()
	for range 2 {
		if err := c.Stream(ctx, "aa", true); err != nil {
			t.Fatal(err)
		}
	}
	if got := q.Get("aa"); !got.Sequential || !got.FirstLast {
		t.Fatalf("on: %+v", got)
	}
	if err := c.Stream(ctx, "aa", false); err != nil {
		t.Fatal(err)
	}
	if got := q.Get("aa"); got.Sequential || got.FirstLast {
		t.Fatalf("off: %+v", got)
	}
}

func TestPiecesComeWithTheirLength(t *testing.T) {
	q, c := serve(t)
	q.Put(qbittest.Torrent{Hash: "aa", Tags: Tag, Pieces: []int{2, 1, 0}, PieceSize: 16384})
	p, err := c.Pieces(context.Background(), "aa")
	if err != nil || p.Length != 16384 || !slices.Equal(p.States, []int{2, 1, 0}) {
		t.Fatalf("Pieces = %+v, %v", p, err)
	}
}

func TestConfigureSetsTheSharedSettingsAndKeepsQBittorrentsOwnRulesOff(t *testing.T) {
	q, c := serve(t)
	err := c.Configure(context.Background(), Prefs{Port: 6881, UploadLimit: 1000, DHT: true, Interface: "wg0"})
	if err != nil {
		t.Fatal(err)
	}
	p := q.Preferences()
	for k, want := range map[string]any{
		"listen_port": 6881.0, "up_limit": 1000.0, "dht": true, "pex": false, "current_network_interface": "wg0",
		"queueing_enabled": false, "max_ratio_enabled": false, "max_seeding_time_enabled": false,
		"incomplete_files_ext": false, "temp_path_enabled": false, "auto_tmm_enabled": false, "add_trackers_enabled": false,
	} {
		if p[k] != want {
			t.Errorf("%s = %v, want %v", k, p[k], want)
		}
	}
}

func TestAnyPortLeavesQBittorrentsOwnPort(t *testing.T) {
	q, c := serve(t)
	if err := c.Configure(context.Background(), Prefs{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := q.Preferences()["listen_port"]; ok {
		t.Fatal("listen_port was set")
	}
}
