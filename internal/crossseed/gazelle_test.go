package crossseed

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
)

const pieces20 = "xxxxxxxxxxxxxxxxxxxx"

// redTorrent is a .torrent as RED hands it out: a private one with RED's
// source tag and a passkey in its announce address.
func redTorrent(source string) []byte {
	info := "d6:lengthi5e4:name5:album12:piece lengthi16384e6:pieces20:" + pieces20 + "7:privatei1e6:source" + strconv.Itoa(len(source)) + ":" + source + "e"
	return []byte("d8:announce39:https://flacsfor.me/abcdef0123/announce4:info" + info + "e")
}

func hashWithSource(t *testing.T, source string) string {
	t.Helper()
	raw, err := bencode.Raw(redTorrent(source), "info")
	if err != nil {
		t.Fatal(err)
	}
	return bencode.InfoHash(raw)
}

func TestTheOtherSitesSourceTagsGiveTheHashesToLookFor(t *testing.T) {
	cands, err := Candidates(redTorrent("RED"), OPS)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 || cands[0].Source != "OPS" || cands[1].Source != "APL" {
		t.Fatalf("candidates %+v", cands)
	}
	for _, c := range cands {
		if want := hashWithSource(t, c.Source); c.InfoHash != want {
			t.Errorf("%s: hash %s, want %s", c.Source, c.InfoHash, want)
		}
		sum := sha1.Sum(c.Info)
		if hex.EncodeToString(sum[:]) != c.InfoHash {
			t.Errorf("%s: the hash is not of the info it carries", c.Source)
		}
	}
}

func TestATorrentFromElsewhereIsNotACandidate(t *testing.T) {
	public := []byte("d8:announce20:http://tracker.x/ann4:infod6:lengthi5e4:name1:a12:piece lengthi16384e6:pieces20:" + pieces20 + "ee")
	if _, err := Candidates(public, OPS); err == nil {
		t.Fatal("a public torrent was offered for cross-seeding")
	}
}

func TestATorrentAlreadyFromTheTargetIsNotACandidate(t *testing.T) {
	if _, err := Candidates(redTorrent("OPS"), OPS); err == nil {
		t.Fatal("an OPS torrent was offered to OPS")
	}
}

// fakeGazelle answers ajax.php?action=torrent like RED and OPS, for the
// hashes it knows, and counts the calls with their key.
type fakeGazelle struct {
	mu    sync.Mutex
	known map[string]int
	keys  []string
}

func (g *fakeGazelle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.keys = append(g.keys, r.Header.Get("Authorization"))
	id, ok := g.known[strings.ToUpper(r.URL.Query().Get("hash"))]
	g.mu.Unlock()
	if r.URL.Path != "/ajax.php" || r.URL.Query().Get("action") != "torrent" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Authorization") != "key-1" {
		json.NewEncoder(w).Encode(map[string]any{"status": "failure", "error": "bad credentials"})
		return
	}
	if !ok {
		json.NewEncoder(w).Encode(map[string]any{"status": "failure", "error": "bad hash parameter"})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"status": "success", "response": map[string]any{
		"group":   map[string]any{"id": 7, "name": "Album"},
		"torrent": map[string]any{"id": id, "infoHash": "ignored"},
	}})
}

func TestTheOtherSiteIsAskedForEachHashUntilOneIsThere(t *testing.T) {
	g := &fakeGazelle{known: map[string]int{strings.ToUpper(hashWithSource(t, "APL")): 4242}}
	srv := httptest.NewServer(g)
	defer srv.Close()
	site := OPS
	site.API = srv.URL
	c := NewGazelle(site, "key-1")
	m, err := c.Find(context.Background(), redTorrent("RED"))
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || m.TorrentID != 4242 || m.Candidate.Source != "APL" {
		t.Fatalf("match %+v", m)
	}
	if len(g.keys) != 2 || g.keys[0] != "key-1" {
		t.Fatalf("asked with %v", g.keys)
	}
}

func TestNoMatchIsNoError(t *testing.T) {
	srv := httptest.NewServer(&fakeGazelle{})
	defer srv.Close()
	site := OPS
	site.API = srv.URL
	m, err := NewGazelle(site, "key-1").Find(context.Background(), redTorrent("RED"))
	if err != nil || m != nil {
		t.Fatalf("Find = %+v, %v", m, err)
	}
}

func TestARefusedKeySaysSo(t *testing.T) {
	srv := httptest.NewServer(&fakeGazelle{})
	defer srv.Close()
	site := OPS
	site.API = srv.URL
	_, err := NewGazelle(site, "wrong").Find(context.Background(), redTorrent("RED"))
	if err == nil || !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("err = %v", err)
	}
}

func TestTheCrossSeedTorrentCarriesTheUsersAnnounceAndTheNewSource(t *testing.T) {
	cands, err := Candidates(redTorrent("RED"), OPS)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Build(cands[0], "https://home.opsfet.ch/secret/announce")
	if err != nil {
		t.Fatal(err)
	}
	v, err := bencode.Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	d := v.(map[string]any)
	if string(d["announce"].([]byte)) != "https://home.opsfet.ch/secret/announce" {
		t.Fatalf("announce %q", d["announce"])
	}
	raw, _ := bencode.Raw(out, "info")
	if bencode.InfoHash(raw) != cands[0].InfoHash {
		t.Fatal("the written torrent does not have the hash the site knows")
	}
}

func TestTheLimiterLetsFiveThroughInTenSeconds(t *testing.T) {
	now := time.Unix(1000, 0)
	var slept []time.Duration
	l := newLimiter(5, 10*time.Second)
	l.now = func() time.Time { return now }
	l.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		now = now.Add(d)
		return nil
	}
	for range 5 {
		if err := l.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	if len(slept) != 0 {
		t.Fatalf("waited %v for the first five", slept)
	}
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != 5*time.Second {
		t.Fatalf("the sixth waited %v, want 5s", slept)
	}
}

func TestTheIndexCallTestsTheKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ajax.php", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("action") != "index" || r.Header.Get("Authorization") != "key-1" {
			json.NewEncoder(w).Encode(map[string]any{"status": "failure", "error": "bad credentials"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "response": map[string]any{"username": "listener", "id": 3}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	site := RED
	site.API = srv.URL
	g := NewGazelle(site, "key-1")
	if who, err := g.Index(context.Background()); err != nil || who != "listener" {
		t.Fatalf("Index = %q, %v", who, err)
	}
	g.SetKey("wrong")
	if _, err := g.Index(context.Background()); err == nil || !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("a wrong key gave %v", err)
	}
}
