package crossseed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// multiFile is a .torrent with a folder of two files.
func multiFile(name string, sizes ...int64) []byte {
	var files strings.Builder
	for i, s := range sizes {
		p := fmt.Sprintf("f%d.mkv", i)
		fmt.Fprintf(&files, "d6:lengthi%de4:pathl%d:%see", s, len(p), p)
	}
	info := fmt.Sprintf("d5:filesl%se4:name%d:%s12:piece lengthi16384e6:pieces20:%s7:privatei1ee", files.String(), len(name), name, pieces20)
	return []byte("d8:announce20:http://tracker.x/ann4:info" + info + "e")
}

func TestATorrentsFilesAreReadWithTheirFolder(t *testing.T) {
	name, files, err := FilesOf(multiFile("Show.S01", 100, 200))
	if err != nil {
		t.Fatal(err)
	}
	if name != "Show.S01" || len(files) != 2 || files[0] != (FileEntry{"Show.S01/f0.mkv", 100}) || files[1] != (FileEntry{"Show.S01/f1.mkv", 200}) {
		t.Fatalf("FilesOf = %q, %+v", name, files)
	}
}

func TestOnlyTheSameFilesAtTheSameSizesMatch(t *testing.T) {
	_, local, _ := FilesOf(multiFile("Show.S01", 100, 200))
	for _, c := range []struct {
		torrent []byte
		want    bool
	}{
		{multiFile("Show.S01", 100, 200), true},
		{multiFile("Show.S01", 100, 201), false},
		{multiFile("Show.S01", 100), false},
		{multiFile("Show.S01.REPACK", 100, 200), false},
	} {
		got, err := SameFiles(local, c.torrent)
		if err != nil || got != c.want {
			t.Errorf("SameFiles = %v, %v, want %v", got, err, c.want)
		}
	}
}

// fakeIndexer answers a Torznab search with the items it is given and serves
// their .torrent files.
func fakeIndexer(t *testing.T, items []string, files map[string][]byte, fetched *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "idx-key" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `<error code="100" description="Incorrect user credentials"/>`)
			return
		}
		fmt.Fprint(w, `<?xml version="1.0"?><rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>`)
		for _, it := range items {
			fmt.Fprint(w, strings.ReplaceAll(it, "BASE", srv.URL))
		}
		fmt.Fprint(w, `</channel></rss>`)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		fetched.Add(1)
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/dl/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func item(title string, size int64, file string) string {
	return fmt.Sprintf(`<item><title>%s</title><link>BASE/dl/%s</link><size>%d</size><torznab:attr name="size" value="%d"/></item>`, title, file, size, size)
}

func TestAnIndexerResultWithTheSameFilesIsFound(t *testing.T) {
	var fetched atomic.Int32
	srv := fakeIndexer(t, []string{
		item("Other.Show.S01", 300, "other"),
		item("Show.S01", 999999, "wrongsize"),
		item("Show.S01", 300, "repack"),
		item("Show.S01", 300, "good"),
	}, map[string][]byte{
		"repack": multiFile("Show.S01", 150, 150),
		"good":   multiFile("Show.S01", 100, 200),
	}, &fetched)
	_, local, _ := FilesOf(multiFile("Show.S01", 100, 200))
	ix := NewTorznab("tracker", srv.URL+"/api", "idx-key")
	m, err := ix.Find(context.Background(), "Show.S01", local)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || !strings.HasSuffix(m.Link, "/dl/good") {
		t.Fatalf("match %+v", m)
	}
	if fetched.Load() != 2 {
		t.Fatalf("fetched %d .torrent files, the name and size should have ruled out the rest", fetched.Load())
	}
}

func TestAnIndexerThatRefusesTheKeySaysSo(t *testing.T) {
	var fetched atomic.Int32
	srv := fakeIndexer(t, nil, nil, &fetched)
	_, err := NewTorznab("tracker", srv.URL+"/api", "wrong").Find(context.Background(), "Show.S01", nil)
	if err == nil || !strings.Contains(err.Error(), "Incorrect user credentials") {
		t.Fatalf("err = %v", err)
	}
}

func TestNamesMatchWhateverTheirSeparators(t *testing.T) {
	for a, b := range map[string]string{"Show.S01.1080p": "Show S01 1080p", "Show_S01": "show.s01"} {
		if !sameName(a, b) {
			t.Errorf("%q and %q do not match", a, b)
		}
	}
	if sameName("Show.S01", "Show.S02") {
		t.Error("two seasons match")
	}
}
