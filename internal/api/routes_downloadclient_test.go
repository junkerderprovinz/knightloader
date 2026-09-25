package api

// Route-level tests for the SABnzbd-shaped download client against a real app.
// The fixtures use magnet links, which the torrent resolver parses locally,
// while an http link would make the direct resolver send a HEAD probe.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const testMagnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"

// downloadClientServer is an instance with the bridge switched on and one API
// token issued, plus the server and the token's secret. The queue is halted so
// a started magnet never goes looking for a swarm; StartTasks still moves the
// tasks to "queued".
func downloadClientServer(t *testing.T, tune func(*settings.Settings)) (*app.App, *httptest.Server, string) {
	t.Helper()
	a := testApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
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

// slots digs the queue's or history's slot list out of the answer, nested the
// way Sonarr's JObject.SelectToken("queue") expects it.
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

// TestDownloadClientIsOffOnAFreshInstall checks that a route able to create
// downloads and delete files is not open unless somebody chose it.
func TestDownloadClientIsOffOnAFreshInstall(t *testing.T) {
	t.Parallel()
	if settings.Defaults().DownloadClientAPI {
		t.Fatal("a fresh install ships with the download-client bridge switched on")
	}
}

// TestDownloadClientOffAcceptsNothing checks that the switched-off bridge takes
// nothing, even from a caller holding a valid API token.
func TestDownloadClientOffAcceptsNothing(t *testing.T) {
	t.Parallel()
	a, srv, key := downloadClientServer(t, func(s *settings.Settings) {
		s.DownloadClientAPI = false
	})

	// A 404 rather than an error document, so a closed door does not say
	// whether the key fits.
	code, _ := sabGet(t, srv, key, map[string]string{"mode": "version"})
	if code != http.StatusNotFound {
		t.Errorf("mode=version with the module off answered %d, want 404", code)
	}

	code, _ = sabAddFile(t, srv, key, "Show.S01E01.nzb", "tv-sonarr", []byte(testMagnet))
	// Errorf, so the checks that nothing was staged still run.
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

// TestDownloadClientRefusesWithoutAValidKey pins the exact wording, which
// Sonarr's TestAuthentication matches on.
func TestDownloadClientRefusesWithoutAValidKey(t *testing.T) {
	t.Parallel()
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
			t.Errorf("%s answered error %q, want %q; Sonarr matches on this text", c.name, got, c.want)
		}
	}

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

	// The real key still works, or the test above would pass on a bridge that
	// refuses everybody.
	if code, doc := sabGet(t, srv, key, map[string]string{"mode": "queue"}); code != http.StatusOK || doc["queue"] == nil {
		t.Errorf("the issued token was refused: %d %+v", code, doc)
	}
}

// TestDownloadClientVersionParsesTheWaySonarrParsesIt checks the version
// against Sonarr's own major.minor.patch expression.
func TestDownloadClientVersionParsesTheWaySonarrParsesIt(t *testing.T) {
	t.Parallel()
	_, srv, key := downloadClientServer(t, nil)
	code, doc := sabGet(t, srv, key, map[string]string{"mode": "version"})
	if code != http.StatusOK {
		t.Fatalf("mode=version answered %d", code)
	}
	got, _ := doc["version"].(string)
	if !regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+|x)$`).MatchString(got) {
		t.Fatalf("version = %q, which Sonarr's version regex does not match", got)
	}
	if !strings.HasPrefix(got, "4.") {
		t.Errorf("version = %q; the emulation was written against SABnzbd 4.x and the number is a claim about the protocol", got)
	}
}

// TestDownloadClientConfigSatisfiesSonarrsChecks covers what mode=get_config
// needs for the client to be saved in Sonarr: the configured category, no dir
// ending in "*", a rooted complete_dir, and a sorters list.
func TestDownloadClientConfigSatisfiesSonarrsChecks(t *testing.T) {
	t.Parallel()
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

// TestDownloadClientAddFileStagesTheLinksItFinds covers the intake end to end:
// the payload is scanned for links, the release name becomes the package, and
// the answer carries the nzo_id.
func TestDownloadClientAddFileStagesTheLinksItFinds(t *testing.T) {
	t.Parallel()
	a, srv, key := downloadClientServer(t, nil)
	// The link is buried in markup, as a DDL indexer would wrap it.
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
		t.Fatalf("nzo_ids = %+v, want exactly one id; Sonarr treats an empty list as a rejected release", ids)
	}
	if id, _ := ids[0].(string); strings.TrimSpace(id) == "" {
		t.Errorf("the nzo_id is blank: %+v", ids)
	}

	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(tasks))
	}
	// Only the extension is stripped, not the dots inside the release.
	if tasks[0].Package != "Show.S01E01.1080p.WEB" {
		t.Errorf("package = %q, want the release name from the uploaded file", tasks[0].Package)
	}
	if tasks[0].Origin != app.OriginPaste {
		t.Errorf("origin = %q; the bridge files a grab as a paste, see serveAdd", tasks[0].Origin)
	}
}

// TestDownloadClientAddFileRefusesAPayloadWithNoLinks checks that a real .nzb,
// which holds nothing this app can fetch, is refused with the reason instead
// of accepted.
func TestDownloadClientAddFileRefusesAPayloadWithNoLinks(t *testing.T) {
	t.Parallel()
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
		t.Errorf("the refusal is %q, which does not say why", msg)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Errorf("the store holds %d tasks after a refused payload, want 0", n)
	}
}

// TestDownloadClientQueueReportsWhatItStaged walks the queue through the two
// states reachable without a real transfer and pins the fields Sonarr reads.
func TestDownloadClientQueueReportsWhatItStaged(t *testing.T) {
	t.Parallel()
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
		t.Errorf("nzo_id = %q, want %q; Sonarr matches queue and history on this id", got, nzoID)
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
	if got, _ := row["timeleft"].(string); strings.Count(got, ":") != 2 {
		t.Errorf("timeleft = %q, want H:MM:SS", got)
	}
	if got, _ := row["priority"].(string); got != "Normal" {
		t.Errorf("priority = %q, want a real SabnzbdPriority name", got)
	}
	if outer, _ := doc["queue"].(map[string]any); outer != nil {
		if paused, _ := outer["paused"].(bool); !paused {
			t.Error("the queue is halted and the answer does not say so")
		}
	}

	// A paused task also reaches Sonarr as Paused.
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

// TestDownloadClientQueueKeepsTheTwoAppsApart checks that Sonarr and Radarr on
// one instance see neither each other's grabs nor the owner's own downloads,
// which Sonarr would otherwise import and delete.
func TestDownloadClientQueueKeepsTheTwoAppsApart(t *testing.T) {
	t.Parallel()
	a, srv, key := downloadClientServer(t, nil)
	if _, doc := sabAddFile(t, srv, key, "Show.S01E03.nzb", "tv-sonarr", []byte(testMagnet)); doc["nzo_ids"] == nil {
		t.Fatalf("addfile did not stage anything: %+v", doc)
	}
	// The owner's own paste, with a different info hash so the mirror set
	// does not fold it into the grab.
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

// TestDownloadClientReportsAHeldLinkAsFailed checks that a link the filter
// holds appears as a failed history item with the rule's reason, so Sonarr
// tries another release instead of waiting.
func TestDownloadClientReportsAHeldLinkAsFailed(t *testing.T) {
	t.Parallel()
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
	// The link itself stays in the holding area.
	if len(a.FilteredLinks()) != 1 {
		t.Errorf("the holding area holds %d links, want the one the filter caught", len(a.FilteredLinks()))
	}
}

// TestDownloadClientDeleteRemovesTheTasks covers Sonarr's cleanup after an
// import, with del_files honoured as sent.
func TestDownloadClientDeleteRemovesTheTasks(t *testing.T) {
	t.Parallel()
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

	// An id that is already gone still answers success.
	if _, doc := sabGet(t, srv, key, map[string]string{
		"mode": "queue", "name": "delete", "value": nzoID,
	}); doc["status"] != true {
		t.Errorf("deleting an already-deleted grab reported failure: %+v", doc)
	}
}

// holdSamples is a link filter that holds every link with "sample" in it,
// which is the one way a test reaches a finished grab without a transfer.
func holdSamples(s *settings.Settings) {
	s.LinkFilter = rules.Set{Rules: []rules.Rule{{
		Name:       "no samples",
		Conditions: []rules.Condition{{Field: rules.FieldURL, Op: rules.OpContains, Value: "sample"}},
		Action:     rules.Action{Reject: true, Reason: "sample files are not wanted here"},
	}}}
}

// otherMagnet has another info hash than testMagnet, so the mirror set does
// not fold the two into one task.
const otherMagnet = "magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98"

func nzoIDOf(t *testing.T, add map[string]any) string {
	t.Helper()
	ids, _ := add["nzo_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("addfile staged nothing: %+v", add)
	}
	id, _ := ids[0].(string)
	return id
}

// TestDownloadClientWorksWithAnAddAndReadToken walks Sonarr's whole round with
// the preset the Access page offers for it. Its cleanup of a finished grab
// forgets the grab, and the downloads stay, since the token cannot control.
func TestDownloadClientWorksWithAnAddAndReadToken(t *testing.T) {
	t.Parallel()
	a, srv, _ := downloadClientServer(t, holdSamples)
	_, key, err := a.APITokens.CreateScoped("sonarr", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{"version", "get_config"} {
		if _, doc := sabGet(t, srv, key, map[string]string{"mode": mode}); doc["error"] != nil {
			t.Errorf("mode=%s was refused to an add and read token: %+v", mode, doc)
		}
	}
	_, add := sabAddFile(t, srv, key, "Show.S02E01.nzb", "tv-sonarr", []byte(testMagnet))
	nzoIDOf(t, add)
	_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
	if rows := slots(t, doc, "queue"); len(rows) != 1 {
		t.Fatalf("the queue shows %d slots to an add and read token, want the one grab", len(rows))
	}

	// A held link is finished as far as Sonarr knows: it fails in the
	// history, and Sonarr clears it to try another release.
	_, add = sabAddFile(t, srv, key, "Show.S02E01.Sample.nzb", "tv-sonarr", []byte(otherMagnet+"&dn=sample.mkv"))
	heldID := nzoIDOf(t, add)
	_, doc = sabGet(t, srv, key, map[string]string{
		"mode": "history", "name": "delete", "value": heldID, "del_files": "1",
	})
	if ok, _ := doc["status"].(bool); !ok {
		t.Fatalf("Sonarr's cleanup was refused: %+v", doc)
	}
	_, doc = sabGet(t, srv, key, map[string]string{"mode": "history", "category": "tv-sonarr"})
	if rows := slots(t, doc, "history"); len(rows) != 0 {
		t.Errorf("the grab is still reported after Sonarr cleared it: %+v", rows)
	}
	if n := len(a.FilteredLinks()); n != 1 {
		t.Errorf("the holding area has %d links after the cleanup, want the held link kept, since the token cannot control", n)
	}
}

// TestDownloadClientKeepsARunningDownloadWithoutControl covers "Remove from
// download client" on a download Sonarr still has in its queue. The token
// cannot stop it, so the answer says so, rather than the grab being forgotten
// while the download carries on with nobody tracking it.
func TestDownloadClientKeepsARunningDownloadWithoutControl(t *testing.T) {
	t.Parallel()
	a, srv, _ := downloadClientServer(t, nil)
	_, key, err := a.APITokens.CreateScoped("sonarr", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	_, add := sabAddFile(t, srv, key, "Show.S02E04.nzb", "tv-sonarr", []byte(testMagnet))
	nzoID := nzoIDOf(t, add)

	_, doc := sabGet(t, srv, key, map[string]string{
		"mode": "queue", "name": "delete", "value": nzoID, "del_files": "1",
	})
	if msg, _ := doc["error"].(string); !strings.Contains(msg, `"control"`) {
		t.Errorf("removing a running download without control answered %+v, want a refusal naming the control right", doc)
	}
	_, doc = sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
	if rows := slots(t, doc, "queue"); len(rows) != 1 {
		t.Errorf("the queue shows %d slots after the refused removal, want the grab still tracked", len(rows))
	}
	if n := liveTasks(t, a); n != 1 {
		t.Errorf("the store holds %d tasks after the refused removal, want 1", n)
	}
}

// TestEveryBridgeOperationNeedsTheRightItsTableNames goes through
// sabnzbdScopes: a token with only that right gets through, and one with
// every other right is refused with the right named.
func TestEveryBridgeOperationNeedsTheRightItsTableNames(t *testing.T) {
	t.Parallel()
	a, srv, _ := downloadClientServer(t, nil)
	magnets := []string{
		testMagnet, otherMagnet,
		"magnet:?xt=urn:btih:00112233445566778899aabbccddeeff00112233",
		"magnet:?xt=urn:btih:ffeeddccbbaa99887766554433221100ffeeddcc",
	}
	call := func(op, key string) map[string]any {
		t.Helper()
		var doc map[string]any
		switch op {
		case "version", "get_config", "queue", "history":
			_, doc = sabGet(t, srv, key, map[string]string{"mode": op})
		case "addurl":
			_, doc = sabGet(t, srv, key, map[string]string{"mode": op, "name": magnets[0]})
			magnets = magnets[1:]
		case "addfile":
			_, doc = sabAddFile(t, srv, key, "Show.S03E01.nzb", "tv-sonarr", []byte(magnets[0]))
			magnets = magnets[1:]
		case "delete":
			_, doc = sabGet(t, srv, key, map[string]string{"mode": "queue", "name": "delete", "value": "SABnzbd_nzo_gone"})
		default:
			t.Fatalf("the rights table names %q, which this test has no call for", op)
		}
		return doc
	}
	token := func(scopes []apitoken.Scope) string {
		t.Helper()
		_, secret, err := a.APITokens.CreateScoped("probe", scopes)
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}

	for op, need := range sabnzbdScopes {
		if msg, _ := call(op, token([]apitoken.Scope{need}))["error"].(string); strings.Contains(msg, "does not have the") {
			t.Errorf("%s with only the %q right was refused: %s", op, need, msg)
		}
		var others []apitoken.Scope
		for _, s := range apitoken.AllScopes() {
			// Add may forget a finished grab, so it is left out for delete.
			if s != need && !(op == "delete" && s == apitoken.ScopeAdd) {
				others = append(others, s)
			}
		}
		if msg, _ := call(op, token(others))["error"].(string); !strings.Contains(msg, `"`+string(need)+`"`) {
			t.Errorf("%s with %v answered %q, want a refusal naming the %q right", op, others, msg, need)
		}
	}
}

// TestDownloadClientRefusesWhatTheTokenMayNotDo checks that each operation
// names the right it is missing, in the error document Sonarr shows.
func TestDownloadClientRefusesWhatTheTokenMayNotDo(t *testing.T) {
	t.Parallel()
	a, srv, full := downloadClientServer(t, nil)
	_, reader, err := a.APITokens.CreateScoped("dashboard", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	_, adder, err := a.APITokens.CreateScoped("script", []apitoken.Scope{apitoken.ScopeAdd})
	if err != nil {
		t.Fatal(err)
	}

	_, doc := sabAddFile(t, srv, reader, "Show.S02E02.nzb", "tv-sonarr", []byte(testMagnet))
	if msg, _ := doc["error"].(string); !strings.Contains(msg, `"add"`) {
		t.Errorf("addfile with a read token answered %+v, want a refusal naming the add right", doc)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Fatalf("a read token staged %d tasks", n)
	}
	_, doc = sabGet(t, srv, adder, map[string]string{"mode": "queue"})
	if msg, _ := doc["error"].(string); !strings.Contains(msg, `"read"`) {
		t.Errorf("mode=queue with an add token answered %+v, want a refusal naming the read right", doc)
	}

	_, add := sabAddFile(t, srv, full, "Show.S02E03.nzb", "tv-sonarr", []byte(testMagnet))
	ids, _ := add["nzo_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("addfile did not stage anything: %+v", add)
	}
	nzoID, _ := ids[0].(string)
	_, doc = sabGet(t, srv, reader, map[string]string{"mode": "queue", "name": "delete", "value": nzoID})
	if msg, _ := doc["error"].(string); !strings.Contains(msg, `"control"`) {
		t.Errorf("a delete with a read token answered %+v, want a refusal naming the control right", doc)
	}
	if n := liveTasks(t, a); n != 1 {
		t.Errorf("the store holds %d tasks after a refused delete, want 1", n)
	}
}

// TestDownloadClientNamesAnUnimplementedMode checks that an unimplemented mode
// is an error naming the mode, not an empty success, and the same one for
// every token, so a narrowed key is not sent to fetch a right that would not
// help.
func TestDownloadClientNamesAnUnimplementedMode(t *testing.T) {
	t.Parallel()
	a, srv, full := downloadClientServer(t, nil)
	_, reader, err := a.APITokens.CreateScoped("dashboard", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{full, reader} {
		for _, mode := range []string{"retry", "fullstatus", "nonsense"} {
			code, doc := sabGet(t, srv, key, map[string]string{"mode": mode})
			if code != http.StatusOK {
				t.Fatalf("mode=%s answered %d", mode, code)
			}
			if ok, _ := doc["status"].(bool); ok {
				t.Errorf("mode=%s reported success for something this bridge does not do: %+v", mode, doc)
			}
			if msg, _ := doc["error"].(string); !strings.Contains(msg, mode) || strings.Contains(msg, "right") {
				t.Errorf("mode=%s answered %q, which does not name the mode or talks about rights", mode, msg)
			}
		}
	}
}

// TestDownloadClientAddUrlTakesALinkDirectly covers addurl, which neither
// Sonarr nor Radarr calls.
func TestDownloadClientAddUrlTakesALinkDirectly(t *testing.T) {
	t.Parallel()
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

// TestDownloadClientModuleRowTracksTheSetting checks that the module row's
// Enabled follows the live setting.
func TestDownloadClientModuleRowTracksTheSetting(t *testing.T) {
	t.Parallel()
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
