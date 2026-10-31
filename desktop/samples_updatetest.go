//go:build updatetest

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// sample is one row the test build starts with.
type sample struct {
	url    string
	pkg    string
	status core.Status
	size   int64
	err    string
}

// samples fill the collector and the download list of a fresh test install, so
// every kind of row can be looked at without finding links first. The files
// are small public test files, and the parts of the RAR set point at
// example.com, which has none. Nothing here starts by itself: collected rows
// wait for Start, and a queue found at boot comes up halted.
var samples = []sample{
	{url: "https://download.blender.org/peach/bigbuckbunny_movies/BigBuckBunny_320x180.mp4", pkg: "Sample media", status: core.StatusCollected, size: 64657027},
	{url: "https://download.samplelib.com/mp3/sample-9s.mp3", pkg: "Sample media", status: core.StatusCollected, size: 147418},
	{url: "https://upload.wikimedia.org/wikipedia/commons/a/a9/Example.jpg", pkg: "Sample media", status: core.StatusCollected, size: 27661},
	{url: "https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf", pkg: "Sample documents", status: core.StatusCollected, size: 13264},
	{url: "http://speedtest.tele2.net/1MB.zip", pkg: "Sample documents", status: core.StatusCollected, size: 1 << 20},
	{url: "https://www.rfc-editor.org/rfc/rfc2549.txt", pkg: "Sample documents", status: core.StatusCollected, size: 7782},

	{url: "http://speedtest.tele2.net/1KB.zip", pkg: "Test files", status: core.StatusDone, size: 1 << 10},
	{url: "http://speedtest.tele2.net/missing.zip", pkg: "Test files", status: core.StatusError, err: "HTTP 404 Not Found"},
	{url: "http://speedtest.tele2.net/10MB.zip", pkg: "Test files", status: core.StatusQueued, size: 10 << 20},
	{url: "https://example.com/samples/Sample.Set.part1.rar", pkg: "Sample.Set", status: core.StatusDone, size: 50 << 20},
	{url: "https://example.com/samples/Sample.Set.part2.rar", pkg: "Sample.Set", status: core.StatusDone, size: 50 << 20},
	{url: "https://example.com/samples/Sample.Set.part3.rar", pkg: "Sample.Set", status: core.StatusQueued, size: 23 << 20},
}

// seedSamples writes the samples into the database of a data folder that has
// none yet. Any other start leaves the list as it is.
func seedSamples(dataDir string, key *appkey.Key) {
	path := filepath.Join(dataDir, "knightloader.db")
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err := writeSamples(path, key, time.Now()); err != nil {
		log.Printf("could not add the sample links: %v", err)
	}
}

func writeSamples(path string, key *appkey.Key, now time.Time) error {
	st, err := store.OpenWithKey(path, key)
	if err != nil {
		return err
	}
	defer st.Close()
	for i, s := range samples {
		u, err := url.Parse(s.url)
		if err != nil {
			return err
		}
		t := &core.Task{
			ID:        fmt.Sprintf("sample%02d", i),
			URL:       s.url,
			Name:      filepath.Base(u.Path),
			Package:   s.pkg,
			Resolver:  "direct",
			Host:      u.Hostname(),
			Size:      s.size,
			Status:    s.status,
			Error:     s.err,
			Online:    core.AvailOnline,
			Enabled:   true,
			Position:  i,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		switch s.status {
		case core.StatusDone:
			t.Loaded = s.size
		case core.StatusError:
			t.Online = core.AvailOffline
		}
		if err := st.Save(t); err != nil {
			return err
		}
	}
	return nil
}
