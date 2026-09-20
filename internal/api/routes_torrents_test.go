package api

// Route-level tests for .torrent upload and file-tree staging. Fixtures are
// built with the same bencode library that reads them back.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

const testPieceLength = 32 << 10

func testPieces(total int64) []byte {
	n := int((total + testPieceLength - 1) / testPieceLength)
	return make([]byte, n*20)
}

// testTorrentBytes bencodes a valid .torrent from an info dict, like the
// unexported helper in internal/resolver/torrent's tests.
func testTorrentBytes(t *testing.T, info metainfo.Info) []byte {
	t.Helper()
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencoding info: %v", err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib, Announce: "udp://tracker.example.org:6969/announce"}
	b, err := bencode.Marshal(mi)
	if err != nil {
		t.Fatalf("bencoding torrent: %v", err)
	}
	return b
}

func testMultiFileTorrent(t *testing.T, folder string, files []metainfo.FileInfo) []byte {
	t.Helper()
	var total int64
	for _, f := range files {
		total += f.Length
	}
	return testTorrentBytes(t, metainfo.Info{Name: folder, Files: files, PieceLength: testPieceLength, Pieces: testPieces(total)})
}

func torrentsServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerTorrents(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// TestParseTorrentUploadReturnsTheFileTree checks that parsing returns the tree
// and the uri for staging, and stages nothing itself.
func TestParseTorrentUploadReturnsTheFileTree(t *testing.T) {
	a, srv := torrentsServer(t)
	data := testMultiFileTorrent(t, "Pack", []metainfo.FileInfo{
		{Length: 900, Path: []string{"one.mkv"}},
		{Length: 12, Path: []string{"two.srt"}},
	})

	code, body := postMultipartFile(t, srv.URL+"/api/torrents/parse", "file", "pack.torrent", data)
	if code != http.StatusOK {
		t.Fatalf("POST parse = %d: %s", code, body)
	}
	var tree torrentTree
	if err := json.Unmarshal(body, &tree); err != nil {
		t.Fatalf("decoding the parse response: %v (%s)", err, body)
	}
	if tree.Name != "Pack" {
		t.Errorf("name = %q, want Pack", tree.Name)
	}
	if !strings.HasPrefix(tree.URI, "data:application/x-bittorrent;base64,") {
		t.Errorf("uri = %q, want the data: URI shape", tree.URI)
	}
	if len(tree.Files) != 2 {
		t.Fatalf("files = %+v, want 2 entries", tree.Files)
	}
	for _, f := range tree.Files {
		if !f.Selected {
			t.Errorf("file %q came back unselected; Parse defaults every file to selected", f.Path)
		}
	}
	if tree.TotalSize != 912 {
		t.Errorf("totalSize = %d, want 912", tree.TotalSize)
	}

	live, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Errorf("the store holds %d tasks after a parse-only call, want 0", len(live))
	}
}

// TestParseTorrentUploadRejectsAnOversizedFile checks the cap enforced before
// the parser sees the bytes.
func TestParseTorrentUploadRejectsAnOversizedFile(t *testing.T) {
	_, srv := torrentsServer(t)
	// Over MaxTorrentBytes but under the request's outer cap, so the handler's
	// own 413 fires rather than MaxBytesReader.
	huge := bytes.Repeat([]byte("a"), torrent.MaxTorrentBytes+500_000)
	code, body := postMultipartFile(t, srv.URL+"/api/torrents/parse", "file", "huge.torrent", huge)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST parse with an oversized file = %d, want 413: %s", code, body)
	}
}

func TestParseTorrentUploadRejectsGarbage(t *testing.T) {
	_, srv := torrentsServer(t)
	code, body := postMultipartFile(t, srv.URL+"/api/torrents/parse", "file", "not-a.torrent", []byte("hello, this is not bencode"))
	if code != http.StatusBadRequest {
		t.Fatalf("POST parse with garbage = %d, want 400: %s", code, body)
	}
	if len(body) == 0 {
		t.Error("no reason given for the rejected upload")
	}
}

// TestParseTorrentUploadRejectsATraversalPath checks that a file path escaping
// the download folder is refused at parse time. bencode happily writes a ".."
// component, so the reading side has to refuse it.
func TestParseTorrentUploadRejectsATraversalPath(t *testing.T) {
	_, srv := torrentsServer(t)
	data := testMultiFileTorrent(t, "Evil", []metainfo.FileInfo{
		{Length: 10, Path: []string{"..", "..", "etc", "passwd"}},
	})
	code, body := postMultipartFile(t, srv.URL+"/api/torrents/parse", "file", "evil.torrent", data)
	if code != http.StatusBadRequest {
		t.Fatalf("POST parse with a traversal path = %d, want 400: %s", code, body)
	}
	if !bytes.Contains(body, []byte("outside the download folder")) {
		t.Errorf("refusal = %q, want it to name the actual reason", body)
	}
}

// TestParseTorrentUploadRequiresTheFileField checks that a missing "file"
// field is a 400, not a 500.
func TestParseTorrentUploadRequiresTheFileField(t *testing.T) {
	_, srv := torrentsServer(t)
	var buf bytes.Buffer
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/torrents/parse", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST parse with no file field = %d, want 400", resp.StatusCode)
	}
}

// TestStageTorrentAppliesTheSelection runs parse then stage and checks the task
// carries exactly the selection.
func TestStageTorrentAppliesTheSelection(t *testing.T) {
	a, srv := torrentsServer(t)
	data := testMultiFileTorrent(t, "Pack", []metainfo.FileInfo{
		{Length: 900, Path: []string{"one.mkv"}},
		{Length: 12, Path: []string{"two.srt"}},
	})
	code, body := postMultipartFile(t, srv.URL+"/api/torrents/parse", "file", "pack.torrent", data)
	if code != http.StatusOK {
		t.Fatalf("POST parse = %d: %s", code, body)
	}
	var tree torrentTree
	if err := json.Unmarshal(body, &tree); err != nil {
		t.Fatal(err)
	}

	stageBody, _ := json.Marshal(map[string]any{
		"uri":           tree.URI,
		"package":       "StagedPack",
		"selectedPaths": []string{"one.mkv"},
	})
	resp, err := http.Post(srv.URL+"/api/torrents", "application/json", bytes.NewReader(stageBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST stage = %d: %s", resp.StatusCode, respBody)
	}
	var task core.Task
	if err := json.Unmarshal(respBody, &task); err != nil {
		t.Fatalf("decoding the staged task: %v (%s)", err, respBody)
	}
	if task.ID == "" {
		t.Fatal("no task id in the stage response")
	}
	if len(task.TorrentFiles) != 2 {
		t.Fatalf("torrent files = %+v, want 2", task.TorrentFiles)
	}
	for _, f := range task.TorrentFiles {
		want := f.Path == "one.mkv"
		if f.Selected != want {
			t.Errorf("file %q selected=%v, want %v", f.Path, f.Selected, want)
		}
	}
	if task.Size != 900 {
		t.Errorf("size = %d, want 900 (the selected file only)", task.Size)
	}

	live, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Fatalf("the store holds %d tasks after staging, want 1", len(live))
	}
}

// TestStageTorrentIgnoresAPathThatIsNotReallyInTheTorrent checks that a
// selected path missing from the real torrent matches nothing, since the file
// list comes from the server's own re-parse.
func TestStageTorrentIgnoresAPathThatIsNotReallyInTheTorrent(t *testing.T) {
	_, srv := torrentsServer(t)
	data := testMultiFileTorrent(t, "Pack2", []metainfo.FileInfo{{Length: 10, Path: []string{"real.bin"}}})
	code, body := postMultipartFile(t, srv.URL+"/api/torrents/parse", "file", "pack2.torrent", data)
	if code != http.StatusOK {
		t.Fatalf("POST parse = %d: %s", code, body)
	}
	var tree torrentTree
	if err := json.Unmarshal(body, &tree); err != nil {
		t.Fatal(err)
	}

	stageBody, _ := json.Marshal(map[string]any{
		"uri":           tree.URI,
		"package":       "P2",
		"selectedPaths": []string{"real.bin", "../../etc/passwd", "made-up-file-nobody-uploaded"},
	})
	resp, err := http.Post(srv.URL+"/api/torrents", "application/json", bytes.NewReader(stageBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST stage = %d: %s", resp.StatusCode, respBody)
	}
	var task core.Task
	if err := json.Unmarshal(respBody, &task); err != nil {
		t.Fatal(err)
	}
	if len(task.TorrentFiles) != 1 {
		t.Fatalf("torrent files = %+v, want exactly the one real file; fabricated paths must not appear", task.TorrentFiles)
	}
	if task.TorrentFiles[0].Path != "real.bin" || !task.TorrentFiles[0].Selected {
		t.Errorf("torrent file = %+v, want real.bin selected", task.TorrentFiles[0])
	}
}

// TestStageTorrentRefusesAMagnet checks that a magnet, which has no file tree
// yet, is refused rather than staged with its selection ignored.
func TestStageTorrentRefusesAMagnet(t *testing.T) {
	_, srv := torrentsServer(t)
	stageBody, _ := json.Marshal(map[string]any{
		"uri": "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
	})
	resp, err := http.Post(srv.URL+"/api/torrents", "application/json", bytes.NewReader(stageBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST stage with a magnet = %d, want 400", resp.StatusCode)
	}
}

// TestStageTorrentRefusesAHandCraftedURI checks that a forged data: URI with
// the right prefix is refused by the re-parse.
func TestStageTorrentRefusesAHandCraftedURI(t *testing.T) {
	_, srv := torrentsServer(t)
	stageBody, _ := json.Marshal(map[string]any{
		"uri": "data:application/x-bittorrent;base64,dGhpcyBpcyBub3QgYSB0b3JyZW50",
	})
	resp, err := http.Post(srv.URL+"/api/torrents", "application/json", bytes.NewReader(stageBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST stage with a forged uri = %d, want 400", resp.StatusCode)
	}
}

// TestStageTorrentBoundsAnOversizedBody checks that the stage route caps its
// body before decoding, as the upload route does.
func TestStageTorrentBoundsAnOversizedBody(t *testing.T) {
	_, srv := torrentsServer(t)
	huge := strings.Repeat("a", torrent.MaxTorrentBytes+2<<20)
	stageBody, err := json.Marshal(map[string]any{"uri": "data:application/x-bittorrent;base64," + huge})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/api/torrents", "application/json", bytes.NewReader(stageBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("POST stage with an oversized body = %d, want 413: %s", resp.StatusCode, mustRead(t, resp))
	}
}

func mustRead(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
