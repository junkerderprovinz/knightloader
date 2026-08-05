// Spike: drive the TorBox Web-Downloads/Debrid API live with the real client.
// Verifies the response shapes end-to-end: mylist (status/files) -> requestdl
// (CDN URL) -> download -> sha256, then deletes the job. Needs KL_TORBOX.
//
//	KL_TORBOX=<key> go run ./cmd/spike-torbox <webdownload_id>|<link>
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
)

func main() {
	key := os.Getenv("KL_TORBOX")
	if key == "" || len(os.Args) < 2 {
		log.Fatal("usage: KL_TORBOX=<key> spike-torbox <webdownload_id>|<link>")
	}
	c := torbox.NewClient(key)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var id int64
	if n, err := strconv.ParseInt(os.Args[1], 10, 64); err == nil {
		id = n
		log.Printf("using existing web download %d", id)
	} else {
		var err error
		id, err = c.CreateWebDownload(ctx, os.Args[1])
		if err != nil {
			log.Fatalf("createwebdownload: %v", err)
		}
		log.Printf("created web download %d", id)
	}

	var wd *torbox.WebDownload
	for {
		var err error
		wd, err = c.Get(ctx, id)
		if err != nil {
			log.Fatalf("mylist: %v", err)
		}
		log.Printf("state=%s present=%v progress=%.0f%% files=%d name=%q",
			wd.DownloadState, wd.DownloadPresent, wd.Progress*100, len(wd.Files), wd.Name)
		if wd.DownloadPresent && len(wd.Files) > 0 {
			break
		}
		time.Sleep(2 * time.Second)
	}

	f := wd.Files[0]
	link, err := c.RequestDL(ctx, id, f.ID)
	if err != nil {
		log.Fatalf("requestdl: %v", err)
	}
	host := link
	if i := strings.Index(link, "//"); i >= 0 {
		host = link[i+2:]
		if j := strings.IndexByte(host, '/'); j >= 0 {
			host = host[:j]
		}
	}
	log.Printf("requestdl ok: file=%q size=%d cdn-host=%s", f.Name, f.Size, host)

	resp, err := http.Get(link)
	if err != nil {
		log.Fatalf("cdn get: %v", err)
	}
	defer resp.Body.Close()
	h := sha256.New()
	n, err := io.Copy(h, resp.Body)
	if err != nil {
		log.Fatalf("cdn read: %v", err)
	}
	fmt.Printf("downloaded %d bytes from CDN, sha256=%x\n", n, h.Sum(nil))

	if err := c.Delete(ctx, id); err != nil {
		log.Printf("cleanup delete failed: %v", err)
	} else {
		log.Printf("web download %d deleted (account clean)", id)
	}
}
