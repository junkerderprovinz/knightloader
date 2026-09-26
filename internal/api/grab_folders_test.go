package api

// What Sonarr does once a grab has finished, through both doors: it imports
// from the path the door reports and then deletes that path, so the path must
// hold that grab's files and nothing else.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	anacrolix "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// filesUnder lists every file below dir, relative to it and with "/".
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	slices.Sort(out)
	return out
}

// sonarrOutputPath is the path Sonarr's SABnzbd client takes a history item's
// files to be at: its storage, or the highest folder above it that is named
// like the item (Sabnzbd.cs, GetHistory). Sonarr deletes this path, with
// everything in it, once it has imported from it.
func sonarrOutputPath(storage, title string) string {
	out := storage
	for p := filepath.Dir(storage); p != filepath.Dir(p); p = filepath.Dir(p) {
		if filepath.Base(p) == title {
			out = p
		}
	}
	return out
}

func TestSonarrsCleanupOfAGrabFromSABnzbdLeavesEverythingElse(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	t.Parallel()
	episodes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, filepath.Base(r.URL.Path), time.Time{}, bytes.NewReader([]byte("episode at "+r.URL.Path)))
	}))
	t.Cleanup(episodes.Close)
	shared := t.TempDir()

	for _, c := range []struct {
		name         string
		tune         func(*settings.Settings)
		first, other string
	}{
		// A fresh install, which has subfolders off.
		{"without subfolders", func(s *settings.Settings) { s.SubfolderByPackage = false },
			"Show.S01E01.1080p.WEB", "Show.S01E02.1080p.WEB"},
		// A rule folder has no package level, subfolders or not.
		{"under a Packagizer rule's folder", func(s *settings.Settings) {
			s.Packagizer = rules.Set{Rules: []rules.Rule{{
				Name:       "incoming",
				Conditions: []rules.Condition{{Field: rules.FieldURL, Op: rules.OpContains, Value: "/Show."}},
				Action:     rules.Action{DownloadDir: shared},
			}}}
		}, "Show.S02E01.1080p.WEB", "Show.S02E02.1080p.WEB"},
		// Sonarr grabs a release again after the first attempt failed.
		{"for the same release twice", nil, "Show.S03E01.1080p.WEB", "Show.S03E01.1080p.WEB"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, srv, key := downloadClientServer(t, c.tune)
			a.SetHalted(false)
			downloads := a.Settings.Get().DownloadDir
			// The owner's own files, beside where the grabs land.
			owned := []string{filepath.Join(downloads, "tv", "keep.txt"), filepath.Join(shared, "keep.txt")}
			for _, f := range owned {
				if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f, []byte("mine"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			grabs := map[string]string{}
			var order []string
			for i, release := range []string{c.first, c.other} {
				file := fmt.Sprintf("%s.%d.mkv", release, i)
				payload := []byte("<links><item>" + episodes.URL + "/" + file + "</item></links>")
				_, add := sabAddFile(t, srv, key, release+".nzb", "tv", payload)
				id := nzoIDOf(t, add)
				grabs[id] = file
				order = append(order, id)
			}

			rows := map[string]map[string]any{}
			waitUntil(t, "both grabs to finish", func() bool {
				_, doc := sabGet(t, srv, key, map[string]string{"mode": "history", "category": "tv"})
				for _, row := range slots(t, doc, "history") {
					if row["status"] == "Completed" {
						rows[row["nzo_id"].(string)] = row
					}
				}
				return len(rows) == 2
			})
			for id, file := range grabs {
				path := sonarrOutputPath(rows[id]["storage"].(string), rows[id]["name"].(string))
				if got := filesUnder(t, path); !slices.Equal(got, []string{file}) {
					t.Errorf("Sonarr imports %s from %s, which holds %v; want only that grab's file", id, path, got)
				}
			}

			// Sonarr's cleanup of the first: the history delete, then the path.
			first, other := order[0], order[1]
			if _, doc := sabGet(t, srv, key, map[string]string{
				"mode": "history", "name": "delete", "value": first, "del_files": "1",
			}); doc["status"] != true {
				t.Fatalf("the history delete answered %+v", doc)
			}
			if err := os.RemoveAll(sonarrOutputPath(rows[first]["storage"].(string), rows[first]["name"].(string))); err != nil {
				t.Fatal(err)
			}

			kept := filepath.Join(rows[other]["storage"].(string), grabs[other])
			if _, err := os.Stat(kept); err != nil {
				t.Errorf("the other grab's file is gone after Sonarr cleaned up the first: %v", err)
			}
			for _, f := range owned {
				if _, err := os.Stat(f); err != nil {
					t.Errorf("the owner's %s is gone after Sonarr cleaned up a grab: %v", f, err)
				}
			}
			_, doc := sabGet(t, srv, key, map[string]string{"mode": "history", "category": "tv"})
			if left := slots(t, doc, "history"); len(left) != 1 || left[0]["nzo_id"] != other {
				t.Errorf("the history after the cleanup holds %+v, want only the other grab", left)
			}
		})
	}
}

// seedTorrent writes files into a folder named name, builds the torrent for it
// and seeds it from a client that listens on loopback only. The magnet it
// returns names that client as a peer, so a download needs neither a tracker
// nor the DHT.
func seedTorrent(t *testing.T, name string, files map[string]int) (hash, magnet string) {
	t.Helper()
	root, err := os.MkdirTemp("", "kl-qbit-seeder-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	for p, size := range files {
		full := filepath.Join(root, name, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, bytes.Repeat([]byte(p), size/len(p)+1)[:size], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib}

	cfg := anacrolix.NewDefaultClientConfig()
	cfg.DataDir = root
	cfg.Seed = true
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true
	cfg.ListenHost = func(string) string { return "127.0.0.1" }
	cl, err := anacrolix.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	tor, err := cl.AddTorrent(&mi)
	if err != nil {
		t.Fatal(err)
	}
	if err := tor.VerifyData(); err != nil {
		t.Fatal(err)
	}
	m := mi.Magnet(nil, &info)
	m.Params.Set("x.pe", fmt.Sprintf("127.0.0.1:%d", cl.LocalPort()))
	return mi.HashInfoBytes().HexString(), m.String()
}

func TestSonarrsDeleteOfATorrentLeavesTheOneBesideIt(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this starts a torrent client")
	}
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race once a torrent runs")
	}
	t.Parallel()
	a, srv, secret, _ := qbitServer(t, func(s *settings.Settings) { s.SubfolderByPackage = false })
	a.SetHalted(false)
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)
	owned := filepath.Join(a.Settings.Get().DownloadDir, "tv-sonarr", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(owned), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(owned, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two releases of one name from two trackers: the same folder name, other
	// files, another info hash.
	first, firstMagnet := seedTorrent(t, "Show.S01", map[string]int{"Show.S01E01.mkv": 40 << 10})
	other, otherMagnet := seedTorrent(t, "Show.S01", map[string]int{"Show.S01E02.mkv": 50 << 10})
	for _, magnet := range []string{firstMagnet, otherMagnet} {
		if code, body := qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {magnet}, "category": {"tv-sonarr"}}); string(body) != "Ok." {
			t.Fatalf("torrents/add answered %d %q", code, body)
		}
	}
	infos := map[string]qbitInfo{}
	waitUntil(t, "both torrents to finish", func() bool {
		for _, i := range qbitInfos(t, c, srv, url.Values{"category": {"tv-sonarr"}}) {
			if sonarrStatus(i.State) == "Completed" {
				infos[i.Hash] = i
			}
		}
		return len(infos) == 2
	})
	for hash, want := range map[string]string{first: "Show.S01/Show.S01E01.mkv", other: "Show.S01/Show.S01E02.mkv"} {
		i := infos[hash]
		if i.ContentPath == i.SavePath {
			t.Errorf("%s has content_path equal to save_path %q, which Sonarr refuses", hash, i.SavePath)
		}
		if got := filesUnder(t, i.ContentPath); !slices.Equal(got, []string{want}) {
			t.Errorf("Sonarr imports %s from %s, which holds %v; want only %s", hash, i.ContentPath, got, want)
		}
	}

	if code, body := qbitPost(t, c, srv, "torrents/delete", url.Values{"hashes": {first}, "deleteFiles": {"true"}}); code != http.StatusOK {
		t.Fatalf("torrents/delete answered %d %s", code, body)
	}
	if _, err := os.Stat(infos[first].ContentPath); !os.IsNotExist(err) {
		t.Errorf("the deleted torrent's folder %s is still there (%v)", infos[first].ContentPath, err)
	}
	if got := filesUnder(t, infos[other].ContentPath); !slices.Equal(got, []string{"Show.S01/Show.S01E02.mkv"}) {
		t.Errorf("after the first torrent was deleted with its files, the other holds %v", got)
	}
	if _, err := os.Stat(owned); err != nil {
		t.Errorf("the owner's file beside the torrents is gone: %v", err)
	}
	left := qbitInfos(t, c, srv, nil)
	if len(left) != 1 || left[0].Hash != other || sonarrStatus(left[0].State) != "Completed" {
		raw, _ := json.Marshal(left)
		t.Errorf("after the delete torrents/info lists %s, want the other torrent, still completed", raw)
	}
}
