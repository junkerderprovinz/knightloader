package api

// Route-level tests for the SABnzbd-shaped download client, against a real app
// on a throwaway data directory - the same shape routes_torrents_test.go and
// links_test.go use, and for the same reason: this file's whole job is to be
// believed by a program on the other end of a socket, so a test that stubbed
// the app out would only prove the stub agrees with itself.
//
// Every fixture here uses a magnet link rather than an http one, deliberately.
// A magnet is resolved by internal/resolver/torrent's own local parse of the
// URI text, so staging one makes no network call at all; an http link goes to
// the direct resolver, which spawns a HEAD probe, and a test that depends on
// DNS is a test that fails on a train.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const testMagnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"

// downloadClientServer is an instance with the bridge switched on and one API
// token issued, plus the server the routes are attached to and that token's
// secret.
//
// The queue is halted on purpose. Staging a link through this bridge starts it
// (App.StartTasks), and a started magnet would be handed to the real engine and
// go looking for a swarm - so the halt is what keeps these tests off the
// network without changing anything the bridge itself does: StartTasks still
// moves the tasks to "queued", which is exactly the state the queue route is
// being asked about.
func downloadClientServer(t *testing.T, tune func(*settings.Settings)) (*app.App, *httptest.Server, string) {
	t.Helper()
	a := testApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	// On, because the bridge reports a finished download's folder to Sonarr and
	// that folder is only the release's own when per-package folders are on.
	s.SubfolderByPackage = true
	// Off, so a staged link is never fetched as a page.
	s.Crawl = false
	s.DownloadClientAPI = true
	if tune != nil {
		tune(&s)
	}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.SetHalted(true)

	_, secret, err := a.APITokens.Create("sonarr")
	if err != nil {
		t.Fatal(err)
	}

	reg := newRegistry()
	registerDownloadClient(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv, secret
}

// sabURL builds the address the way Sonarr's HttpRequestBuilder does: the mode
// and its arguments, with the api key appended last.
func sabURL(srv *httptest.Server, key string, params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	if key != "" {
		q.Set("apikey", key)
	}
	q.Set("output", "json")
	return srv.URL + sabnzbdPath + "?" + q.Encode()
}

func sabGet(t *testing.T, srv *httptest.Server, key string, params map[string]string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(sabURL(srv, key, params))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var doc map[string]any
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%v answered unparseable JSON: %v (%s)", params, err, raw)
		}
	}
	return resp.StatusCode, doc
}

// sabAddFile is Sonarr's own intake call: mode=addfile as a POST, the payload
// in a multipart field called "name", the category in the query string.
func sabAddFile(t *testing.T, srv *httptest.Server, key, filename, category string, payload []byte) (int, map[string]any) {
	t.Helper()
	u := sabURL(srv, key, map[string]string{"mode": "addfile", "cat": category, "priority": "-100"})
	code, raw := postMultipartFile(t, u, "name", filename, payload)
	var doc map[string]any
	if code == http.StatusOK {
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("addfile answered unparseable JSON: %v (%s)", err, raw)
		}
	}
	return code, doc
}

// slots digs the queue's or history's own slot list out of the answer, which is
// nested exactly the way Sonarr's JObject.SelectToken("queue") expects it.
func slots(t *testing.T, doc map[string]any, section string) []map[string]any {
	t.Helper()
	outer, ok := doc[section].(map[string]any)
	if !ok {
		t.Fatalf("the answer has no %q object: %+v", section, doc)
	}
	raw, ok := outer["slots"].([]any)
	if !ok {
		t.Fatalf("%q carries no slots list: %+v", section, outer)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, s := range raw {
		m, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("a %s slot is not an object: %+v", section, s)
		}
		out = append(out, m)
	}
	return out
}

func liveTasks(t *testing.T, a *app.App) int {
	t.Helper()
	all, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	return len(all)
}

// TestDownloadClientIsOffOnAFreshInstall is the default this whole feature
// hangs on. An interface that can create downloads and delete files must not be
// standing open because nobody chose otherwise, so a fresh install has to
// answer as if the endpoint did not exist at all.
func TestDownloadClientIsOffOnAFreshInstall(t *testing.T) {
	if settings.Defaults().DownloadClientAPI {
		t.Fatal("a fresh install ships with the download-client bridge switched on")
	}
}

// TestDownloadClientOffAcceptsNothing is the off-state test the wave asks for by
// name: with the module switched off the bridge must not merely hide, it must
// refuse to take anything, including from a caller holding a perfectly good API
// token.
func TestDownloadClientOffAcceptsNothing(t *testing.T) {
	a, srv, key := downloadClientServer(t, func(s *settings.Settings) {
		s.DownloadClientAPI = false
	})

	// Reading is refused, and refused as a 404 rather than as an error
	// document: a closed door does not tell anybody whether their key fits.
	code, _ := sabGet(t, srv, key, map[string]string{"mode": "version"})
	if code != http.StatusNotFound {
		t.Errorf("mode=version with the module off answered %d, want 404", code)
	}

	// And writing is refused, which is the half that matters.
	code, _ = sabAddFile(t, srv, key, "Show.S01E01.nzb", "tv-sonarr", []byte(testMagnet))
	// Errorf and not Fatalf on purpose: the status is the smaller half of this
	// claim, and stopping here would leave the half that actually matters -
	// that nothing was staged and nothing was recorded - unchecked in exactly
	// the run where it is most worth knowing.
	if code != http.StatusNotFound {
		t.Errorf("mode=addfile with the module off answered %d, want 404", code)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Errorf("the store holds %d tasks after an add to a switched-off bridge, want 0", n)
	}
	value, err := a.UIState(downloadClientBucket)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(value) != "" {
		t.Errorf("a switched-off bridge recorded a grab: %q", value)
	}
}

// TestDownloadClientRefusesWithoutAValidKey pins both halves of the credential,
// including the exact wording. Sonarr's TestAuthentication matches on these two
// strings to tell the person which field to fix; anything else surfaces as an
// unattributed connection failure and sends them looking at their network.
func TestDownloadClientRefusesWithoutAValidKey(t *testing.T) {
	a, srv, key := downloadClientServer(t, nil)

	for _, c := range []struct {
		name, key, want string
	}{
		{"no key at all", "", "API Key Required"},
		{"a key that is not ours", "not-a-real-token", "API Key Incorrect"},
	} {
		code, doc := sabGet(t, srv, c.key, map[string]string{"mode": "queue"})
		if code != http.StatusOK {
			t.Fatalf("%s answered %d; the refusal is a SABnzbd error document, not an HTTP status", c.name, code)
		}
		if ok, _ := doc["status"].(bool); ok {
			t.Errorf("%s was accepted: %+v", c.name, doc)
		}
		if got, _ := doc["error"].(string); got != c.want {
			t.Errorf("%s answered error %q, want %q - Sonarr matches on this text", c.name, got, c.want)
		}
	}

	// The bad keys must not have been able to stage anything either.
	code, doc := sabAddFile(t, srv, "not-a-real-token", "Show.S01E01.nzb", "tv-sonarr", []byte(testMagnet))
	if code != http.StatusOK {
		t.Fatalf("addfile with a bad key answered %d", code)
	}
	if ok, _ := doc["status"].(bool); ok {
		t.Errorf("addfile with a bad key was accepted: %+v", doc)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Errorf("the store holds %d tasks after an add with a bad key, want 0", n)
	}

	// And the real key still works, or the test above would pass on a bridge
	// that refuses everybody.
	if code, doc := sabGet(t, srv, key, map[string]string{"mode": "queue"}); code != http.StatusOK || doc["queue"] == nil {
		t.Errorf("the issued token was refused: %d %+v", code, doc)
	}
}

// TestDownloadClientVersionParsesTheWaySonarrParsesIt. Sonarr reads the answer
// with a major.minor.patch regex and refuses anything below 0.7.0, so a version
// string that does not parse fails the connection test with no useful reason
// attached.
func TestDownloadClientVersionParsesTheWaySonarrParsesIt(t *testing.T) {
	_, srv, key := downloadClientServer(t, nil)
	code, doc := sabGet(t, srv, key, map[string]string{"mode": "version"})
	if code != http.StatusOK {
		t.Fatalf("mode=version answered %d", code)
	}
	got, _ := doc["version"].(string)
	// Sonarr's own expression, copied rather than approximated.
	if !regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+|x)$`).MatchString(got) {
		t.Fatalf("version = %q, which Sonarr's version regex does not match", got)
	}
	if !strings.HasPrefix(got, "4.") {
		t.Errorf("version = %q; the emulation was written against SABnzbd 4.x and the number is a claim about the protocol", got)
	}
}

// TestDownloadClientConfigSatisfiesSonarrsChecks covers the three things
// mode=get_config has to carry or the client cannot be saved in Sonarr at all:
// the category it was configured with, a category dir that does not end in "*",
// and a rooted complete_dir. The sorters list is checked because Sonarr calls
// .Any() on it without a nil check, so an absent key is an exception on its
// side rather than a missing feature.
func TestDownloadClientConfigSatisfiesSonarrsChecks(t *testing.T) {
	_, srv, key := downloadClientServer(t, nil)
	code, doc := sabGet(t, srv, key, map[string]string{"mode": "get_config"})
	if code != http.StatusOK {
		t.Fatalf("mode=get_config answered %d", code)
	}
	cfg, ok := doc["config"].(map[string]any)
	if !ok {
		t.Fatalf("no config object: %+v", doc)
	}
	if _, ok := cfg["sorters"].([]any); !ok {
		t.Errorf("sorters is missing or not a list; Sonarr calls config.Sorters.Any() on it unguarded")
	}
	misc, ok := cfg["misc"].(map[string]any)
	if !ok {
		t.Fatalf("no misc object: %+v", cfg)
	}
	complete, _ := misc["complete_dir"].(string)
	if !strings.ContainsAny(complete, `/\`) || strings.HasPrefix(complete, ".") {
		t.Errorf("complete_dir = %q, which is not a rooted path; Sonarr then asks for mode=fullstatus, which this bridge does not answer", complete)
	}
	if pre, _ := misc["pre_check"].(bool); pre {
		t.Error("pre_check is true, which makes Sonarr's global config test fail on any version below 1.1")
	}

	cats, ok := cfg["categories"].([]any)
	if !ok || len(cats) == 0 {
		t.Fatalf("no categories: %+v", cfg)
	}
	seen := map[string]bool{}
	for _, raw := range cats {
		c, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("a category is not an object: %+v", raw)
		}
		name, _ := c["name"].(string)
		dir, _ := c["dir"].(string)
		seen[name] = true
		if strings.HasSuffix(dir, "*") {
			t.Errorf("category %q has dir %q; a trailing * is what Sonarr reads as \"job folders off\" and warns about", name, dir)
		}
	}
	// The two an untouched Sonarr and an untouched Radarr are configured with.
	for _, want := range []string{"tv-sonarr", "radarr", "*"} {
		if !seen[want] {
			t.Errorf("category %q is not advertised, so Sonarr's category validation fails for a default install", want)
		}
	}
}

// TestDownloadClientAddFileStagesTheLinksItFinds is the intake end to end: the
// payload is scanned for links the way a paste is, the release name becomes the
// package (which is what gives the grab its own folder), and the answer carries
// the nzo_id Sonarr needs to follow the download afterwards.
func TestDownloadClientAddFileStagesTheLinksItFinds(t *testing.T) {
	a, srv, key := downloadClientServer(t, nil)
	// A payload with the link buried in markup, which is the realistic case
	// this bridge exists for: a DDL indexer wrapping links in something that is
	// not a link list.
	payload := []byte("<links>\n  <item>" + testMagnet + "</item>\n</links>\n")

	code, doc := sabAddFile(t, srv, key, "Show.S01E01.1080p.WEB.nzb", "tv-sonarr", payload)
	if code != http.StatusOK {
		t.Fatalf("addfile answered %d", code)
	}
	if ok, _ := doc["status"].(bool); !ok {
		t.Fatalf("addfile refused a payload holding a link: %+v", doc)
	}
	ids, _ := doc["nzo_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("nzo_ids = %+v, want exactly one id - Sonarr treats an empty list as a rejected release", ids)
	}
	if id, _ := ids[0].(string); strings.TrimSpace(id) == "" {
		t.Errorf("the nzo_id is blank: %+v", ids)
	}

	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(tasks))
	}
	// The extension is stripped and the dots inside the release are not: the
	// package name is what Sonarr sees back as the slot's filename and what
	// becomes the folder on disk.
	if tasks[0].Package != "Show.S01E01.1080p.WEB" {
		t.Errorf("package = %q, want the release name from the uploaded file", tasks[0].Package)
	}
	if tasks[0].Origin != app.OriginPaste {
		t.Errorf("origin = %q; the bridge files a grab as a paste, see serveAdd", tasks[0].Origin)
	}
}

// TestDownloadClientAddFileRefusesAPayloadWithNoLinks is the honesty test. A
// real .nzb describes Usenet articles and holds nothing this app can fetch, so
// the bridge has to say so rather than answer success and leave Sonarr waiting
// out its timeout on a download that was never going to start.
func TestDownloadClientAddFileRefusesAPayloadWithNoLinks(t *testing.T) {
	a, srv, key := downloadClientServer(t, nil)
	nzb := []byte(`<?xml version="1.0" encoding="iso-8859-1" ?>
<nzb>
 <file poster="someone" date="1700000000" subject="Show.S01E01 [1/2] - &#34;show.r00&#34; yEnc">
  <groups><group>alt.binaries.example</group></groups>
  <segments><segment bytes="739067" number="1">abcdef0123456789@news</segment></segments>
 </file>
</nzb>`)

	code, doc := sabAddFile(t, srv, key, "Show.S01E01.nzb", "tv-sonarr", nzb)
	if code != http.StatusOK {
		t.Fatalf("addfile answered %d", code)
	}
	if ok, _ := doc["status"].(bool); ok {
		t.Fatalf("a real .nzb was accepted; this instance has no Usenet backend to fetch it with: %+v", doc)
	}
	if msg, _ := doc["error"].(string); !strings.Contains(msg, "Usenet") {
		t.Errorf("the refusal is %q, which does not say why; the reason is the whole point of refusing", msg)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Errorf("the store holds %d tasks after a refused payload, want 0", n)
	}
}

// TestDownloadClientQueueReportsWhatItStaged walks the queue through the two
// states these tests can reach without a real transfer, and pins the fields
// Sonarr actually reads off a slot.
func TestDownloadClientQueueReportsWhatItStaged(t *testing.T) {
	a, srv, key := downloadClientServer(t, nil)
	_, add := sabAddFile(t, srv, key, "Show.S01E02.nzb", "tv-sonarr", []byte(testMagnet))
	ids, _ := add["nzo_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("addfile did not stage anything: %+v", add)
	}
	nzoID, _ := ids[0].(string)

	code, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
	if code != http.StatusOK {
		t.Fatalf("mode=queue answered %d", code)
	}
	rows := slots(t, doc, "queue")
	if len(rows) != 1 {
		t.Fatalf("queue holds %d slots, want 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if got, _ := row["nzo_id"].(string); got != nzoID {
		t.Errorf("nzo_id = %q, want %q - Sonarr matches queue and history on this id", got, nzoID)
	}
	if got, _ := row["cat"].(string); got != "tv-sonarr" {
		t.Errorf("cat = %q, want tv-sonarr; Sonarr drops every item whose category is not its own", got)
	}
	if got, _ := row["filename"].(string); got != "Show.S01E02" {
		t.Errorf("filename = %q, want the release name", got)
	}
	if got, _ := row["status"].(string); got != "Queued" {
		t.Errorf("status = %q, want Queued for a staged, not yet running task", got)
	}
	// Parsed by a converter that splits on ":" and int.Parses the pieces, so an
	// absent or numeric value is an exception on Sonarr's side.
	if got, _ := row["timeleft"].(string); strings.Count(got, ":") != 2 {
		t.Errorf("timeleft = %q, want H:MM:SS", got)
	}
	// Read through Enum.TryParse against SabnzbdPriority, which silently yields
	// the zero value for a name it does not know.
	if got, _ := row["priority"].(string); got != "Normal" {
		t.Errorf("priority = %q, want a real SabnzbdPriority name", got)
	}
	// The queue-level flag, which is what makes a halted instance legible to
	// Sonarr rather than looking stuck.
	if outer, _ := doc["queue"].(map[string]any); outer != nil {
		if paused, _ := outer["paused"].(bool); !paused {
			t.Error("the queue is halted and the answer does not say so")
		}
	}

	// Pausing the task itself is the other state that reaches Sonarr as Paused.
	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(tasks))
	}
	a.Pause(tasks[0].ID)
	_, doc = sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
	rows = slots(t, doc, "queue")
	if len(rows) != 1 {
		t.Fatalf("the paused task left the queue: %+v", rows)
	}
	if got, _ := rows[0]["status"].(string); got != "Paused" {
		t.Errorf("status after pausing = %q, want Paused", got)
	}
}

// TestDownloadClientQueueKeepsTheTwoAppsApart. Sonarr and Radarr pointed at one
// instance must not see each other's grabs, and neither may see the downloads
// the person who owns the instance added themselves - Sonarr would otherwise
// try to import them and then delete them.
func TestDownloadClientQueueKeepsTheTwoAppsApart(t *testing.T) {
	a, srv, key := downloadClientServer(t, nil)
	if _, doc := sabAddFile(t, srv, key, "Show.S01E03.nzb", "tv-sonarr", []byte(testMagnet)); doc["nzo_ids"] == nil {
		t.Fatalf("addfile did not stage anything: %+v", doc)
	}
	// The instance owner's own paste, through the ordinary path, with a
	// different info hash so the mirror set does not fold it into the grab.
	own := a.AddLinks([]string{"magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98"}, "My Own Download")
	if len(own) != 1 {
		t.Fatalf("the owner's own paste staged %d tasks", len(own))
	}

	_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "radarr"})
	if rows := slots(t, doc, "queue"); len(rows) != 0 {
		t.Errorf("Radarr sees %d of Sonarr's grabs: %+v", len(rows), rows)
	}

	_, doc = sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
	rows := slots(t, doc, "queue")
	if len(rows) != 1 {
		t.Fatalf("Sonarr sees %d slots, want only its own grab: %+v", len(rows), rows)
	}
	if got, _ := rows[0]["filename"].(string); got != "Show.S01E03" {
		t.Errorf("the slot is %q; the owner's own download has leaked into Sonarr's queue", got)
	}
}

// TestDownloadClientReportsAHeldLinkAsFailed is the state mapping's most
// consequential choice. A link the link filter is holding will never start on
// its own, and reporting it as queued would leave Sonarr waiting out its whole
// timeout before it tried another release - so it is reported as a failed item
// in the history, carrying the rule's own reason.
func TestDownloadClientReportsAHeldLinkAsFailed(t *testing.T) {
	a, srv, key := downloadClientServer(t, func(s *settings.Settings) {
		s.LinkFilter = rules.Set{Rules: []rules.Rule{{
			Name:       "no samples",
			Conditions: []rules.Condition{{Field: rules.FieldURL, Op: rules.OpContains, Value: "sample"}},
			Action:     rules.Action{Reject: true, Reason: "sample files are not wanted here"},
		}}}
	})

	held := testMagnet + "&dn=sample.mkv"
	code, add := sabAddFile(t, srv, key, "Show.S01E04.nzb", "tv-sonarr", []byte(held))
	if code != http.StatusOK {
		t.Fatalf("addfile answered %d", code)
	}
	if ok, _ := add["status"].(bool); !ok {
		t.Fatalf("addfile refused a link the filter merely holds: %+v", add)
	}

	// It must not be sitting in the queue, where Sonarr would wait for it.
	_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
	if rows := slots(t, doc, "queue"); len(rows) != 0 {
		t.Errorf("a held link is in the queue, so Sonarr will wait for a download that cannot start: %+v", rows)
	}

	_, doc = sabGet(t, srv, key, map[string]string{"mode": "history", "category": "tv-sonarr"})
	rows := slots(t, doc, "history")
	if len(rows) != 1 {
		t.Fatalf("history holds %d slots, want the held link reported as one: %+v", len(rows), rows)
	}
	if got, _ := rows[0]["status"].(string); got != "Failed" {
		t.Errorf("status = %q, want Failed", got)
	}
	if msg, _ := rows[0]["fail_message"].(string); !strings.Contains(msg, "sample files are not wanted here") {
		t.Errorf("fail_message = %q, want the filter's own reason so the log says which rule caught it", msg)
	}
	if got, _ := rows[0]["storage"].(string); strings.TrimSpace(got) == "" {
		t.Error("the history slot names no folder; Sonarr imports from storage")
	}
	// The task itself is still in the holding area, untouched: telling Sonarr to
	// move on is not the same as throwing the link away.
	if len(a.FilteredLinks()) != 1 {
		t.Errorf("the holding area holds %d links, want the one the filter caught", len(a.FilteredLinks()))
	}
}

// TestDownloadClientDeleteRemovesTheTasks covers Sonarr's cleanup after an
// import, including that del_files is honoured as sent rather than assumed:
// taking a row off a list and deleting somebody's file have never been the same
// action in this app.
func TestDownloadClientDeleteRemovesTheTasks(t *testing.T) {
	a, srv, key := downloadClientServer(t, nil)
	_, add := sabAddFile(t, srv, key, "Show.S01E05.nzb", "tv-sonarr", []byte(testMagnet))
	ids, _ := add["nzo_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("addfile did not stage anything: %+v", add)
	}
	nzoID, _ := ids[0].(string)
	if n := liveTasks(t, a); n != 1 {
		t.Fatalf("the store holds %d tasks before the delete, want 1", n)
	}

	code, doc := sabGet(t, srv, key, map[string]string{
		"mode": "queue", "name": "delete", "value": nzoID, "del_files": "0",
	})
	if code != http.StatusOK {
		t.Fatalf("the delete answered %d", code)
	}
	if ok, _ := doc["status"].(bool); !ok {
		t.Fatalf("the delete reported failure: %+v", doc)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Errorf("the store still holds %d tasks after the delete", n)
	}

	_, doc = sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
	if rows := slots(t, doc, "queue"); len(rows) != 0 {
		t.Errorf("the deleted grab is still in the queue: %+v", rows)
	}

	// Deleting an id that is already gone answers success, because Sonarr calls
	// delete for an item it has decided to forget and an error here would be a
	// permanent, repeating failure in its log.
	if _, doc := sabGet(t, srv, key, map[string]string{
		"mode": "queue", "name": "delete", "value": nzoID,
	}); doc["status"] != true {
		t.Errorf("deleting an already-deleted grab reported failure: %+v", doc)
	}
}

// TestDownloadClientNamesAnUnimplementedMode. mode=retry and mode=fullstatus
// are both reachable from Sonarr, and answering an empty success to either
// would leave it believing something happened.
func TestDownloadClientNamesAnUnimplementedMode(t *testing.T) {
	_, srv, key := downloadClientServer(t, nil)
	for _, mode := range []string{"retry", "fullstatus", "nonsense"} {
		code, doc := sabGet(t, srv, key, map[string]string{"mode": mode})
		if code != http.StatusOK {
			t.Fatalf("mode=%s answered %d", mode, code)
		}
		if ok, _ := doc["status"].(bool); ok {
			t.Errorf("mode=%s reported success for something this bridge does not do: %+v", mode, doc)
		}
		if msg, _ := doc["error"].(string); !strings.Contains(msg, mode) {
			t.Errorf("mode=%s answered %q, which does not name the mode", mode, msg)
		}
	}
}

// TestDownloadClientAddUrlTakesALinkDirectly covers the half of SABnzbd's own
// intake that actually fits this app. Neither Sonarr nor Radarr calls it, which
// is exactly why it needs a test of its own: nothing else would notice it
// breaking.
func TestDownloadClientAddUrlTakesALinkDirectly(t *testing.T) {
	a, srv, key := downloadClientServer(t, nil)
	code, doc := sabGet(t, srv, key, map[string]string{
		"mode": "addurl", "name": testMagnet, "nzbname": "Handed.Over.By.Script", "cat": "radarr",
	})
	if code != http.StatusOK {
		t.Fatalf("mode=addurl answered %d", code)
	}
	if ok, _ := doc["status"].(bool); !ok {
		t.Fatalf("addurl refused a plain link: %+v", doc)
	}
	tasks := a.Tasks()
	if len(tasks) != 1 || tasks[0].Package != "Handed.Over.By.Script" {
		t.Fatalf("addurl staged %+v, want one task under the nzbname it was given", tasks)
	}
	_, doc = sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "radarr"})
	if rows := slots(t, doc, "queue"); len(rows) != 1 {
		t.Errorf("the link added by addurl is not in the radarr queue: %+v", rows)
	}
}

// TestDownloadClientModuleRowTracksTheSetting is the module registry's own
// invariant applied to this row: Enabled is derived from the live setting, so a
// change made anywhere else - the advanced key table, a script, another browser
// - moves the row with it.
func TestDownloadClientModuleRowTracksTheSetting(t *testing.T) {
	a := testApp(t)
	find := func() Feature {
		t.Helper()
		for _, m := range featureList(a) {
			if m.ID == "downloadclient" {
				return m
			}
		}
		t.Fatal("the module registry has no downloadclient row")
		return Feature{}
	}
	if row := find(); row.Enabled {
		t.Error("a fresh instance reports the download-client bridge as on")
	}
	if err := setFeature(a, "downloadclient", true); err != nil {
		t.Fatal(err)
	}
	if !a.Settings.Get().DownloadClientAPI {
		t.Fatal("the switch did not reach the flag the route reads")
	}
	row := find()
	if !row.Enabled {
		t.Error("the flag is set and the module row still reports off")
	}
	if row.Switch != SwitchSetting {
		t.Errorf("switch = %q, want %q", row.Switch, SwitchSetting)
	}
	if err := setFeature(a, "downloadclient", false); err != nil {
		t.Fatal(err)
	}
	if a.Settings.Get().DownloadClientAPI {
		t.Error("switching the module off left the door open")
	}
}
