// Command spike-download checks the two things the engine layer relies on in
// the embedded Gopeed library: that custom request headers are sent, and that
// progress events fire during a multi-connection download.
//
// Run: go run ./cmd/spike-download   (URLs overridable via KL_SPIKE_ECHO / KL_SPIKE_FILE)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/GopeedLab/gopeed/pkg/download"
	fhttp "github.com/GopeedLab/gopeed/pkg/protocol/http"
)

func main() {
	dir, err := os.MkdirTemp("", "kl-spike-*")
	must(err)
	defer os.RemoveAll(dir)
	fmt.Println("KnightLoader engine spike (embedded Gopeed", gopeedVersion(), ")")
	fmt.Println("download dir:", dir)

	cfg := (&download.DownloaderConfig{
		RefreshInterval: 300, // ms between progress events
		DownloaderStoreConfig: &base.DownloaderStoreConfig{
			DownloadDir: dir,
			MaxRunning:  3,
		},
	}).Init()
	d := download.NewDownloader(cfg)
	must(d.Setup())
	defer d.Close()

	events := make(chan *download.Event, 512)
	d.Listener(func(e *download.Event) {
		select {
		case events <- e:
		default: // a spike does not need every tick
		}
	})

	token := fmt.Sprintf("kl-proof-%d", time.Now().UnixNano())
	echoURL := env("KL_SPIKE_ECHO", "https://httpbin.org/headers")
	fileURL := env("KL_SPIKE_FILE", "https://httpbin.org/bytes/2097152")

	fmt.Println("\n[1] custom-header injection ->", echoURL)
	p1 := run(d, dir, events, echoURL, map[string]string{
		"User-Agent": "KnightLoader/0.0-spike",
		"X-KL-Spike": token,
	}, 1)
	body, _ := os.ReadFile(p1)
	if strings.Contains(string(body), token) {
		fmt.Println("  PASS: echo server reflected our custom header token:", token)
	} else {
		fmt.Println("  FAIL: token not found in response body:")
		fmt.Println(indent(string(body)))
		os.Exit(1)
	}

	fmt.Println("\n[2] progress + multi-connection ->", fileURL)
	p2 := run(d, dir, events, fileURL, map[string]string{"User-Agent": "KnightLoader/0.0-spike"}, 4)
	fi, err := os.Stat(p2)
	must(err)
	fmt.Printf("  PASS: downloaded %s (%d bytes)\n", filepath.Base(p2), fi.Size())

	fmt.Println("\nengine spike: OK (Gopeed embeds, custom headers work, progress streams)")
}

// run creates one direct download, prints its progress and returns the file path.
func run(d *download.Downloader, dir string, events chan *download.Event, url string, header map[string]string, conns int) string {
	req := &base.Request{
		URL:   url,
		Extra: &fhttp.ReqExtra{Method: "GET", Header: header},
	}
	opts := &base.Options{
		Path:  dir,
		Extra: &fhttp.OptsExtra{Connections: conns},
	}
	id, err := d.CreateDirect(req, opts)
	must(err)
	deadline := time.After(90 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Task == nil || e.Task.ID != id {
				continue
			}
			switch e.Key {
			case download.EventKeyProgress:
				if pr := e.Task.Progress; pr != nil {
					fmt.Printf("  ... %8.1f KiB  @ %8.1f KiB/s\n", float64(pr.Downloaded)/1024, float64(pr.Speed)/1024)
				}
			case download.EventKeyDone:
				return filepath.Join(dir, d.GetTask(id).Name())
			case download.EventKeyError:
				fmt.Println("  ERROR:", e.Err)
				os.Exit(1)
			}
		case <-deadline:
			fmt.Println("  TIMEOUT after 90s")
			os.Exit(1)
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n    ")
}

func gopeedVersion() string { return "v1.9.3" }

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
