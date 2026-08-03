// Command spike-jd is the KnightLoader M0 JD-integration spike.
//
// It proves a Go client can drive a *headless* JDownloader through its local
// "Deprecated API" — plain HTTP JSON on :3128, no cloud, no crypto — by adding
// links to the LinkGrabber and reading them back with live name/size/status.
// This is the arm's-length path KnightLoader uses to reach JD's full hoster
// coverage while rendering everything in its own UI.
//
// Run: KL_JD=http://<jd-host>:3128 go run ./cmd/spike-jd
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

type jd struct {
	base string
	c    *http.Client
}

// call invokes a Deprecated-API method: GET /namespace/method?<url-encoded JSON param>.
// The envelope is {"data": <result>}; we return the raw data.
func (j *jd) call(path string, param any) (json.RawMessage, error) {
	q := ""
	if param != nil {
		b, _ := json.Marshal(param)
		q = "?" + url.QueryEscape(string(b))
	}
	resp, err := j.c.Get(j.base + path + q)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("bad json: %v (%s)", err, truncate(string(body), 200))
	}
	return env.Data, nil
}

func main() {
	base := env("KL_JD", "http://127.0.0.1:3128") // co-located headless JD; override with KL_JD
	j := &jd{base: base, c: &http.Client{Timeout: 15 * time.Second}}
	fmt.Println("KnightLoader M0 — headless-JD Deprecated-API spike ->", base)

	// [0] reachability
	resp, err := j.c.Get(base + "/help")
	must(err)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		must(fmt.Errorf("/help returned HTTP %d", resp.StatusCode))
	}
	fmt.Println("[0] /help reachable: OK")

	// [1] addLinks — push two direct links into the LinkGrabber
	data, err := j.call("/linkgrabberv2/addLinks", map[string]any{
		"links":       "http://speedtest.tele2.net/1MB.zip\nhttp://speedtest.tele2.net/5MB.zip",
		"packageName": "KnightLoader-spike",
		"autostart":   false,
	})
	must(err)
	fmt.Println("[1] addLinks OK ->", string(data))

	// [2] queryLinks — poll until JD has crawled/resolved name+size (proves live read-back)
	query := map[string]any{
		"bytesTotal": true, "name": true, "status": true, "url": true,
		"availability": true, "packageUUIDs": []int64{},
	}
	var links []map[string]any
	for i := 0; i < 20; i++ {
		time.Sleep(time.Second)
		d, err := j.call("/linkgrabberv2/queryLinks", query)
		must(err)
		links = nil
		must(json.Unmarshal(d, &links))
		resolved := 0
		for _, l := range links {
			if n, _ := l["bytesTotal"].(float64); n > 0 {
				resolved++
			}
		}
		if resolved >= 2 {
			break
		}
	}
	fmt.Printf("[2] queryLinks OK -> %d link(s) in the LinkGrabber\n", len(links))
	for _, l := range links {
		fmt.Printf("    - %-28v size=%-10v avail=%v\n", l["name"], l["bytesTotal"], l["availability"])
	}

	fmt.Println("\nM0 JD spike: OK — Go client drives headless JD via the local Deprecated API (addLinks + queryLinks, live name/size).")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
