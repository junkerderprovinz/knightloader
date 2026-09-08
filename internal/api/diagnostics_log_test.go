package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/logring"
)

// logServer is these four routes on a server of their own.
//
// A registry with one subsystem on it rather than buildRegistry's full table,
// for the reason TestAnUnknownApiPathIs404 builds its own too: what is being
// tested here is what the handlers answer, and a full table drags in every
// other subsystem's boot. That this subsystem is actually in registerAll is a
// different question, and TestEverySubsystemIsRegistered already asks it of
// every routes_*.go in the package - including this one.
func logServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg := newRegistry()
	registerDiagnosticsLog(reg, testApp(t))
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// get fetches one path and hands back the status and the body.
func get(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, err := readAll(resp) // closes resp.Body (federation_test.go)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

// TestNoneOfTheseRoutesAnswersWithoutASession. A log line can carry a feed URL
// with an indexer's API key in its query string (internal/feed's poller logs
// subscription addresses verbatim) and the crawler logs the pages it walks the
// same way. These four are the reason routes_test.go's open list is worth
// pinning, and none of them belongs on it.
func TestNoneOfTheseRoutesAnswersWithoutASession(t *testing.T) {
	reg := newRegistry()
	registerDiagnosticsLog(reg, testApp(t))
	for _, r := range reg.Routes() {
		if r.Open {
			t.Errorf("%s %s answers without a session; a log line can carry a feed URL with its key in it", r.Method, r.Path)
		}
		if !strings.HasPrefix(r.Path, "/api/diagnostics/") {
			t.Errorf("%s %s is outside /api/diagnostics, which is the one prefix neither the relay "+
				"nor the federation proxy forwards", r.Method, r.Path)
		}
	}
}

// TestTheTailAnswersWithACursor is what makes following possible at all: the
// second poll must return only what arrived since the first, or a follow view
// can do nothing but re-fetch five hundred lines and guess.
func TestTheTailAnswersWithACursor(t *testing.T) {
	srv := logServer(t)

	code, body := get(t, srv.URL+"/api/diagnostics/log")
	if code != http.StatusOK {
		t.Fatalf("GET /api/diagnostics/log answered %d: %s", code, body)
	}
	var first logTail
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatalf("decoding the tail: %v (%s)", err, body)
	}
	if first.Capacity != logring.Capacity {
		t.Errorf("capacity = %d, want the ring's own %d rather than a second, hand-copied number", first.Capacity, logring.Capacity)
	}
	if len(first.Sources) == 0 {
		t.Error("the source table is empty, so the picker would have nothing to offer")
	}

	marker := "diagnostics-log-cursor-marker-8xk2"
	log.Print(marker)

	code, body = get(t, fmt.Sprintf("%s/api/diagnostics/log?since=%d", srv.URL, first.Newest))
	if code != http.StatusOK {
		t.Fatalf("the follow-up poll answered %d: %s", code, body)
	}
	var second logTail
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range second.Entries {
		if strings.Contains(e.Line, marker) {
			found = true
		}
		if e.Seq <= first.Newest {
			t.Errorf("since=%d returned seq %d, which the caller had already seen", first.Newest, e.Seq)
		}
	}
	if !found {
		t.Errorf("the line logged between the two polls never arrived: %+v", second.Entries)
	}
	if second.Newest <= first.Newest {
		t.Errorf("newest went from %d to %d after a line was logged", first.Newest, second.Newest)
	}
}

// TestTheTailNamesTheSourceAndTheDownload. Both rules describe lines this tree
// writes, so they are decided on the server; a copy in the browser would drift
// the first time somebody reworded a log call, and the drift would show up as a
// filter that quietly matches nothing.
func TestTheTailNamesTheSourceAndTheDownload(t *testing.T) {
	srv := logServer(t)

	_, body := get(t, srv.URL+"/api/diagnostics/log")
	var before logTail
	if err := json.Unmarshal(body, &before); err != nil {
		t.Fatal(err)
	}

	const id = "00112233445566aa"
	log.Printf("task %s could not be moved out of the working folder: denied", id)
	log.Printf("checksum diag-marker.mkv: bad hash")

	_, body = get(t, fmt.Sprintf("%s/api/diagnostics/log?since=%d", srv.URL, before.Newest))
	var after logTail
	if err := json.Unmarshal(body, &after); err != nil {
		t.Fatal(err)
	}

	var task, sum *logLine
	for i := range after.Entries {
		e := &after.Entries[i]
		if strings.Contains(e.Line, "could not be moved out of the working folder") {
			task = e
		}
		if strings.Contains(e.Line, "diag-marker.mkv") {
			sum = e
		}
	}
	if task == nil || sum == nil {
		t.Fatalf("the two logged lines did not come back: %+v", after.Entries)
	}
	if task.Source != "task" || task.TaskID != id {
		t.Errorf("the task line came back as source %q, taskId %q; want \"task\" and %q", task.Source, task.TaskID, id)
	}
	if sum.Source != "checksum" {
		t.Errorf("the checksum line came back as source %q, want \"checksum\"", sum.Source)
	}
	if sum.TaskID != "" {
		t.Errorf("the checksum line claims to be about download %q; a file name is not an id", sum.TaskID)
	}
}

// TestTheTailHonoursALimitAndABadCursor. A cursor that cannot be read is a
// hand-typed URL or a client that has lost its place, and starting over is both
// harmless and what the caller wanted - an error would leave the page showing a
// refusal instead of a log.
func TestTheTailHonoursALimitAndABadCursor(t *testing.T) {
	srv := logServer(t)
	for i := 0; i < 4; i++ {
		log.Printf("diagnostics-log-limit-marker %d", i)
	}

	code, body := get(t, srv.URL+"/api/diagnostics/log?limit=2&since=not-a-number")
	if code != http.StatusOK {
		t.Fatalf("an unreadable cursor answered %d: %s", code, body)
	}
	var tail logTail
	if err := json.Unmarshal(body, &tail); err != nil {
		t.Fatal(err)
	}
	if len(tail.Entries) != 2 {
		t.Errorf("limit=2 returned %d entries", len(tail.Entries))
	}
	if tail.Entries[0].Seq >= tail.Entries[1].Seq {
		t.Error("a limited answer came back newest first; the cursor could then never advance past the gap")
	}
}

// TestTheTailNeverAnswersNull. `entries` is fed straight into a .map on the
// page, and JSON null there blanks the whole card - the same class of bug
// routes_features.go documents for archivePasswords.
func TestTheTailNeverAnswersNull(t *testing.T) {
	srv := logServer(t)
	_, body := get(t, srv.URL+"/api/diagnostics/log?since=999999999")
	if strings.Contains(string(body), `"entries":null`) {
		t.Errorf("entries came back as null: %s", body)
	}
	if strings.Contains(string(body), `"sources":null`) {
		t.Errorf("sources came back as null: %s", body)
	}
}

// TestTheFileStateAnswersOnAnInstanceThatHasNeverArmedIt, which is every
// install on the day this ships.
func TestTheFileStateAnswersOnAnInstanceThatHasNeverArmedIt(t *testing.T) {
	srv := logServer(t)
	code, body := get(t, srv.URL+"/api/diagnostics/logfile")
	if code != http.StatusOK {
		t.Fatalf("GET /api/diagnostics/logfile answered %d: %s", code, body)
	}
	var st logring.FileState
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("decoding the file state: %v (%s)", err, body)
	}
	if st.Enabled {
		t.Error("the file state says a log is being written on an instance that never armed one")
	}
	// The path is answered even with nothing armed, because this is exactly
	// when somebody wants to know where the file would go - they are deciding
	// whether to switch it on, and a blank cell answers nothing.
	if st.Path == "" {
		t.Error("no path was reported, so the card cannot say where the file would be written")
	}
	if strings.Contains(string(body), `"generations":null`) {
		t.Errorf("generations came back as null, which throws on the card's own .map: %s", body)
	}
}

// TestDownloadingALogFile covers the one route that answers something other
// than JSON, and the three ways of asking for a file that is not there.
func TestDownloadingALogFile(t *testing.T) {
	// The server FIRST and the sink second, and the order is load-bearing:
	// logServer builds an app.App, app.New applies the log-file setting, and
	// the shipped setting is off - so an app built after the sink was armed
	// disarms it again. That is the boot half of applyLogFile's two call sites
	// doing exactly what it is there for, and it costs this test nothing but
	// the order of two lines.
	srv := logServer(t)

	dir := t.TempDir()
	if err := logring.OpenFile(logring.FileOptions{Dir: dir, MaxBytes: 1 << 20, Keep: 2}); err != nil {
		t.Fatal(err)
	}
	// Disarmed again whatever happens, or every later test in this package
	// would go on writing into a directory the test framework has removed.
	t.Cleanup(func() { _ = logring.CloseFile() })

	marker := "diagnostics-log-download-marker-5j1p"
	log.Print(marker)

	code, body := get(t, srv.URL+"/api/diagnostics/logfile/0")
	if code != http.StatusOK {
		t.Fatalf("downloading generation 0 answered %d: %s", code, body)
	}
	if !strings.Contains(string(body), marker) {
		t.Errorf("the downloaded file does not contain the line that was logged:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(dir, logring.Name)); err != nil {
		t.Errorf("the file the route served is not where the sink says it is: %v", err)
	}

	// Everything that is not a generation this sink keeps. None of these may
	// become part of a path: the range check happens in the sink that owns the
	// names, and this route never joins anything itself.
	if code, _ = get(t, srv.URL+"/api/diagnostics/logfile/-1"); code != http.StatusNotFound {
		t.Errorf("a negative generation answered %d, want 404", code)
	}
	if code, _ = get(t, srv.URL+"/api/diagnostics/logfile/seven"); code != http.StatusBadRequest {
		t.Errorf("a non-numeric generation answered %d, want 400", code)
	}
	if code, _ = get(t, srv.URL+"/api/diagnostics/logfile/9"); code != http.StatusNotFound {
		t.Errorf("a generation past Keep answered %d, want 404", code)
	}
	// Kept but never written: the sink keeps two, and nothing has rotated yet.
	if code, _ = get(t, srv.URL+"/api/diagnostics/logfile/2"); code != http.StatusNotFound {
		t.Errorf("a generation that is kept but does not exist yet answered %d, want 404", code)
	}
}

// TestTheDiagnosticsBundleCarriesNoLogPath is TestDiagnosticsShipsNoPaths with
// the sink actually armed, which is the one state that test cannot reach: it
// runs against an instance that never switched the log file on, so the path
// field it would have to catch is empty either way.
//
// The bundle is a file people attach to PUBLIC bug reports and a desktop data
// directory is C:\Users\<their real name>\AppData\..., so the log file's state
// travels in it with the path stripped (logring.FileState.Redacted) while the
// unredacted path goes out on the session-guarded /api/diagnostics/logfile
// route. This test is what stops that Redacted() from being dropped in a later
// edit as a piece of ceremony nobody could see the point of.
//
// It reads the RAW body rather than a field, deliberately: it has to keep
// working whatever shape the log-file section of the bundle takes, and a
// missing redaction is a substring problem rather than a typed one.
func TestTheDiagnosticsBundleCarriesNoLogPath(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	// After the server, because app.New applies the log-file setting and the
	// shipped setting is off - see TestDownloadingALogFile for the same order
	// and the same reason.
	dir := a.LogDir()
	if err := logring.OpenFile(logring.FileOptions{Dir: dir, MaxBytes: 1 << 20, Keep: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logring.CloseFile() })
	if st := logring.FileStatus(); !st.Enabled || st.Path == "" {
		t.Fatalf("the sink is not armed, so this test would pass without checking anything: %+v", st)
	}

	_, _, raw := getDiagnostics(t, srv.URL)
	for _, secret := range []string{dir, filepath.Join(dir, logring.Name)} {
		for _, spelling := range spellingsInJSON(t, secret) {
			if strings.Contains(string(raw), spelling) {
				t.Errorf("the diagnostics bundle shipped the log file's path %q (as %q)", secret, spelling)
			}
		}
	}
}

// TestThePerDownloadLogFindsItsLinesAndAdmitsWhatItMisses.
//
// The "partial" flag is the point of this test as much as the lines are. Seven
// of this tree's log call sites record which download they are about and the
// other 149 do not, so a card that showed an empty list without saying why is a
// card people report as broken.
func TestThePerDownloadLogFindsItsLinesAndAdmitsWhatItMisses(t *testing.T) {
	srv := logServer(t)

	const id = "aabbccddeeff0011"
	log.Printf("task %s stood still for 2m0s and was started again (restart 1)", id)
	log.Printf("checksum %s: this is a hash and not a download", id)

	code, body := get(t, srv.URL+"/api/diagnostics/task/"+id)
	if code != http.StatusOK {
		t.Fatalf("GET /api/diagnostics/task/{id} answered %d: %s", code, body)
	}
	var got taskLog
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decoding the per-download log: %v (%s)", err, body)
	}
	if !got.Partial {
		t.Error("partial is false; most log lines record no download at all and the card has to say so")
	}
	if got.Scanned != "memory" {
		t.Errorf("scanned = %q, want \"memory\"", got.Scanned)
	}
	if len(got.Lines) != 1 {
		t.Fatalf("got %d line(s) for one download, want exactly the one that names it: %+v", len(got.Lines), got.Lines)
	}
	if !strings.Contains(got.Lines[0].Line, "stood still") {
		t.Errorf("the wrong line came back: %q", got.Lines[0].Line)
	}

	// A download nothing has ever logged about, which is the usual case.
	code, body = get(t, srv.URL+"/api/diagnostics/task/0000000000000000")
	if code != http.StatusOK {
		t.Fatalf("a download with no lines answered %d: %s", code, body)
	}
	if strings.Contains(string(body), `"lines":null`) {
		t.Errorf("lines came back as null, which throws on the card's own .map: %s", body)
	}
}
