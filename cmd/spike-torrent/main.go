// Command spike-torrent is the Wave 11.5 verify-first spike.
//
// docs/torrent-support.md left one fact unconfirmed and named it as the first
// job of the wave: the exact path from a live gopeed download.Task to a
// populated bt.Stats (peers, seeds, ratio). This program answers it against a
// real magnet link and the real embedded engine, and prints the concrete Go
// type that comes back so nothing downstream is written against a guess.
//
// It also prints what a resolve returns before any bytes move (the file list a
// selection tree would be built from, the info hash, whether gopeed exposes the
// BEP 27 private flag at all), because those are the other three things the
// wave's build agents need to agree on.
//
// Run: go run ./cmd/spike-torrent   (magnet overridable via KL_SPIKE_MAGNET)
package main

import (
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	gbt "github.com/GopeedLab/gopeed/pkg/protocol/bt"
)

// Sintel, the Blender Foundation short. Public domain, permanently and heavily
// seeded, and the same torrent the anacrolix client's own test suite leans on -
// so a run that finds no peers is a network problem here, not a dead swarm.
const defaultMagnet = "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&dn=Sintel&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce&tr=udp%3A%2F%2Ftracker.openbittorrent.com%3A6969%2Fannounce&tr=udp%3A%2F%2Fexplodie.org%3A6969&tr=udp%3A%2F%2Ftracker.torrent.eu.org%3A451%2Fannounce&tr=wss%3A%2F%2Ftracker.btorrent.xyz&tr=wss%3A%2F%2Ftracker.openwebtorrent.com"

func main() {
	dir, err := os.MkdirTemp("", "kl-spike-bt-*")
	must(err)
	defer os.RemoveAll(dir)

	magnet := env("KL_SPIKE_MAGNET", defaultMagnet)
	fmt.Println("KnightLoader Wave 11.5 - torrent spike")
	fmt.Println("download dir:", dir)
	fmt.Println("magnet:", magnet[:min(len(magnet), 90)], "...")

	// Exactly what internal/engine.New builds: no FetchManagers override, so
	// gopeed's own Init defaults them to http + bt + ed2k.
	cfg := (&download.DownloaderConfig{
		RefreshInterval: 500,
		DownloaderStoreConfig: &base.DownloaderStoreConfig{
			DownloadDir: dir,
			MaxRunning:  5,
		},
	}).Init()
	fmt.Printf("\n[0] FetchManagers gopeed defaulted to: ")
	for _, fm := range cfg.FetchManagers {
		fmt.Printf("%s ", fm.Name())
	}
	fmt.Println()

	d := download.NewDownloader(cfg)
	must(d.Setup())
	defer d.Close()

	d.Listener(func(e *download.Event) {
		if e.Key == download.EventKeyProgress {
			return
		}
		fmt.Printf("    event %-9s task=%s err=%v\n", e.Key, taskID(e), e.Err)
	})

	fmt.Println("\n[1] Resolve(magnet) - blocks until the swarm hands over metadata")
	start := time.Now()
	rr, err := d.Resolve(&base.Request{URL: magnet}, &base.Options{Path: dir})
	must(err)
	fmt.Printf("    resolved in %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("    resolve id   : %s\n", rr.ID)
	fmt.Printf("    res.Name     : %q  (non-empty means multi-file/folder)\n", rr.Res.Name)
	fmt.Printf("    res.Size     : %d\n", rr.Res.Size)
	fmt.Printf("    res.Hash     : %q\n", rr.Res.Hash)
	fmt.Printf("    res.Files    : %d\n", len(rr.Res.Files))
	for i, f := range rr.Res.Files {
		if i >= 8 {
			fmt.Printf("      ... and %d more\n", len(rr.Res.Files)-8)
			break
		}
		fmt.Printf("      [%d] path=%q name=%q size=%d\n", i, f.Path, f.Name, f.Size)
	}

	fmt.Println("\n[2] Create -> a real download.Task")
	gid, err := d.Create(rr.ID)
	must(err)
	fmt.Println("    gopeed task id:", gid)

	fmt.Println("\n[3] Downloader.Stats(taskID) - the unconfirmed path")
	for i := 0; i < 20; i++ {
		time.Sleep(2 * time.Second)
		sr, err := d.Stats(gid)
		if err != nil {
			fmt.Println("    Stats error:", err)
			continue
		}
		t := d.GetTask(gid)
		fmt.Printf("    t=%2ds concrete type %-40s ", (i+1)*2, reflect.TypeOf(sr))
		s, ok := sr.(*gbt.Stats)
		if !ok {
			fmt.Println("NOT *bt.Stats")
			continue
		}
		fmt.Printf("peers=%d active=%d seeders=%d seedBytes=%d ratio=%.4f seedTime=%d",
			s.TotalPeers, s.ActivePeers, s.ConnectedSeeders, s.SeedBytes, s.SeedRatio, s.SeedTime)
		if t != nil {
			fmt.Printf(" | task.Status=%s task.Uploading=%v", t.Status, t.Uploading)
			if t.Progress != nil {
				fmt.Printf(" downloaded=%d speed=%d", t.Progress.Downloaded, t.Progress.Speed)
			}
		}
		fmt.Println()
		if s.TotalPeers > 0 && s.ConnectedSeeders > 0 {
			fmt.Println("    -> populated. Path confirmed.")
			break
		}
	}

	_ = d.Delete(&download.TaskFilter{IDs: []string{gid}}, false)

	fmt.Println("\n[4] selective download of one tiny file, to reach done + seeding for real")
	rr2, err := d.Resolve(&base.Request{URL: magnet}, &base.Options{Path: dir, SelectFiles: []int{smallest(rr.Res.Files)}})
	must(err)
	fmt.Printf("    selected [%d] %q (%d bytes) of %d files; res.Size now %d\n",
		smallest(rr.Res.Files), rr2.Res.Files[smallest(rr.Res.Files)].Name,
		rr2.Res.Files[smallest(rr.Res.Files)].Size, len(rr2.Res.Files), rr2.Res.Size)
	gid2, err := d.Create(rr2.ID)
	must(err)
	for i := 0; i < 60; i++ {
		time.Sleep(2 * time.Second)
		t := d.GetTask(gid2)
		if t == nil {
			fmt.Println("    task vanished")
			break
		}
		sr, _ := d.Stats(gid2)
		s, _ := sr.(*gbt.Stats)
		var loaded int64
		if t.Progress != nil {
			loaded = t.Progress.Downloaded
		}
		fmt.Printf("    t=%2ds status=%-8s uploading=%-5v loaded=%-10d", (i+1)*2, t.Status, t.Uploading, loaded)
		if s != nil {
			fmt.Printf(" peers=%d seeders=%d seedBytes=%d ratio=%.4f seedTime=%d", s.TotalPeers, s.ConnectedSeeders, s.SeedBytes, s.SeedRatio, s.SeedTime)
		}
		fmt.Println()
		if t.Status == base.DownloadStatusDone {
			fmt.Printf("    -> DONE with Uploading=%v  (this is the pair the Seeding flag is derived from)\n", t.Uploading)
			// A few more ticks to watch SeedTime/SeedBytes move after done.
			for j := 0; j < 3; j++ {
				time.Sleep(3 * time.Second)
				sr, _ := d.Stats(gid2)
				if s, ok := sr.(*gbt.Stats); ok {
					t := d.GetTask(gid2)
					up := false
					if t != nil {
						up = t.Uploading
					}
					fmt.Printf("       post-done uploading=%v seedBytes=%d ratio=%.4f seedTime=%d\n", up, s.SeedBytes, s.SeedRatio, s.SeedTime)
				}
			}
			break
		}
	}

	fmt.Println("\n[5] cleanup")
	_ = d.Delete(&download.TaskFilter{IDs: []string{gid2}}, false)
	fmt.Println("done")
}

func smallest(files []*base.FileInfo) int {
	best := 0
	for i, f := range files {
		if f.Size < files[best].Size {
			best = i
		}
	}
	return best
}

func taskID(e *download.Event) string {
	if e.Task == nil {
		return "-"
	}
	return e.Task.ID
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "spike failed:", err)
		os.Exit(1)
	}
}
