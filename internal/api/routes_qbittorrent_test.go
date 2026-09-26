package api

// Route-level tests for the qBittorrent-shaped download client against a real
// app. The calls are made the way Sonarr's QBittorrentProxyV2 makes them: a
// form login, GETs with query parameters for reading, form POSTs for changes.

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

const (
	qbitTestHash  = "0123456789abcdef0123456789abcdef01234567"
	qbitOtherHash = "fedcba9876543210fedcba9876543210fedcba98"
)

// qbitServer is an instance with the bridge switched on and one API token
// issued, plus the server, the token's secret and the bridge itself, whose
// clock a test can move. The queue is halted, as in downloadClientServer, so
// a started magnet never goes looking for a swarm.
func qbitServer(t *testing.T, tune func(*settings.Settings)) (*app.App, *httptest.Server, string, *qbitClient) {
	t.Helper()
	return qbitServerOn(t, testApp(t), tune)
}

// qbitServerOn is qbitServer on an app the test built, such as one that found
// finished downloads in its store when it started.
func qbitServerOn(t *testing.T, a *app.App, tune func(*settings.Settings)) (*app.App, *httptest.Server, string, *qbitClient) {
	t.Helper()
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.SubfolderByPackage = true
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

	qb := newQbitClient(a)
	reg := newRegistry()
	qb.register(reg)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv, secret, qb
}

// sonarrClient keeps the session cookie between calls, as Sonarr's cookie
// cache does.
func sonarrClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

// qbitGet reads the way Sonarr reads: a GET with the parameters in the query.
func qbitGet(t *testing.T, c *http.Client, srv *httptest.Server, call string, params url.Values) (int, []byte) {
	t.Helper()
	u := srv.URL + qbitPrefix + call
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// qbitPost changes things the way Sonarr does: a form POST.
func qbitPost(t *testing.T, c *http.Client, srv *httptest.Server, call string, form url.Values) (int, []byte) {
	t.Helper()
	resp, err := c.PostForm(srv.URL+qbitPrefix+call, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func qbitLogin(t *testing.T, c *http.Client, srv *httptest.Server, password string) string {
	t.Helper()
	code, body := qbitPost(t, c, srv, "auth/login", url.Values{"username": {"sonarr"}, "password": {password}})
	if code != http.StatusOK {
		t.Fatalf("auth/login answered %d: %s", code, body)
	}
	return string(body)
}

func qbitInfos(t *testing.T, c *http.Client, srv *httptest.Server, params url.Values) []qbitInfo {
	t.Helper()
	code, body := qbitGet(t, c, srv, "torrents/info", params)
	if code != http.StatusOK {
		t.Fatalf("torrents/info answered %d: %s", code, body)
	}
	var out []qbitInfo
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("torrents/info answered unparseable JSON: %v (%s)", err, body)
	}
	return out
}

// TestSonarrGrabsAMagnetAndRemovesItAfterImport walks the calls Sonarr makes
// from the connection test to the cleanup after an import, and pins the fields
// it reads on the way.
func TestSonarrGrabsAMagnetAndRemovesItAfterImport(t *testing.T) {
	t.Parallel()
	a, srv, secret, _ := qbitServer(t, nil)
	c := sonarrClient(t)

	if got := qbitLogin(t, c, srv, secret); got != "Ok." {
		t.Fatalf("login with a token answered %q, want Ok.", got)
	}

	_, version := qbitGet(t, c, srv, "app/webapiVersion", nil)
	if string(version) != qbitAPIVersion {
		t.Errorf("app/webapiVersion = %q, want %q", version, qbitAPIVersion)
	}
	if _, v := qbitGet(t, c, srv, "app/version", nil); !strings.HasPrefix(string(v), "v4.") {
		t.Errorf("app/version = %q; Sonarr strips a leading v and parses the rest", v)
	}

	code, body := qbitGet(t, c, srv, "app/preferences", nil)
	if code != http.StatusOK {
		t.Fatalf("app/preferences answered %d", code)
	}
	var prefs map[string]any
	if err := json.Unmarshal(body, &prefs); err != nil {
		t.Fatal(err)
	}
	if prefs["save_path"] == "" {
		t.Error("preferences carry no save_path; Sonarr checks it as the output root")
	}
	if act, _ := prefs["max_ratio_act"].(float64); act != 0 {
		t.Errorf("max_ratio_act = %v; anything but 0 makes Sonarr refuse the client as one that deletes finished torrents", act)
	}
	if dht, _ := prefs["dht"].(bool); !dht {
		t.Error("dht is off, so Sonarr refuses every magnet without trackers")
	}

	// Sonarr's category test: missing, so it creates it and reads again.
	if code, body := qbitPost(t, c, srv, "torrents/createCategory", url.Values{"category": {"tv-sonarr"}}); code != http.StatusOK {
		t.Fatalf("createCategory answered %d: %s", code, body)
	}
	_, body = qbitGet(t, c, srv, "torrents/categories", nil)
	var cats map[string]map[string]string
	if err := json.Unmarshal(body, &cats); err != nil {
		t.Fatal(err)
	}
	if _, ok := cats["tv-sonarr"]; !ok {
		t.Fatalf("categories after createCategory = %v; Sonarr fails its test without tv-sonarr", cats)
	}
	if a.Settings.Get().CategoryFor("tv-sonarr").ID == "" {
		t.Error("the qBittorrent category did not become a category of this instance")
	}

	magnet := "magnet:?xt=urn:btih:" + strings.ToUpper(qbitTestHash) + "&dn=Show.S01E01.1080p.WEB"
	code, body = qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {magnet}, "category": {"tv-sonarr"}, "paused": {"false"}})
	if code != http.StatusOK || string(body) != "Ok." {
		t.Fatalf("torrents/add answered %d %q, want Ok.", code, body)
	}

	infos := qbitInfos(t, c, srv, url.Values{"category": {"tv-sonarr"}})
	if len(infos) != 1 {
		t.Fatalf("torrents/info holds %d torrents, want the one added: %+v", len(infos), infos)
	}
	info := infos[0]
	if info.Hash != qbitTestHash {
		t.Errorf("hash = %q, want %q; Sonarr tracks the grab by the magnet's info hash", info.Hash, qbitTestHash)
	}
	if info.Category != "tv-sonarr" {
		t.Errorf("category = %q; Sonarr drops every item whose category is not its own", info.Category)
	}
	if info.State != "queuedDL" {
		t.Errorf("state = %q, want queuedDL for a started magnet in a halted queue", info.State)
	}
	if info.ContentPath == info.SavePath || filepath.Dir(info.ContentPath) != info.SavePath {
		t.Errorf("content_path %q is not a folder inside save_path %q; Sonarr refuses a finished torrent whose two paths are equal",
			info.ContentPath, info.SavePath)
	}
	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(tasks))
	}
	if tasks[0].Category != "tv-sonarr" {
		t.Errorf("the task is filed under %q, want the category Sonarr sent", tasks[0].Category)
	}
	if got := qbitInfos(t, c, srv, url.Values{"category": {"radarr"}}); len(got) != 0 {
		t.Errorf("Radarr's category sees Sonarr's torrent: %+v", got)
	}

	code, body = qbitGet(t, c, srv, "torrents/properties", url.Values{"hash": {qbitTestHash}})
	if code != http.StatusOK {
		t.Fatalf("torrents/properties answered %d; Sonarr reads that as the torrent not being loaded", code)
	}
	var props map[string]any
	if err := json.Unmarshal(body, &props); err != nil {
		t.Fatal(err)
	}
	if props["save_path"] != info.SavePath {
		t.Errorf("properties save_path = %v, want %q", props["save_path"], info.SavePath)
	}

	code, body = qbitGet(t, c, srv, "torrents/files", url.Values{"hash": {qbitTestHash}})
	if code != http.StatusOK {
		t.Fatalf("torrents/files answered %d", code)
	}
	var files []qbitFile
	if err := json.Unmarshal(body, &files); err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 || strings.Contains(files[0].Name, `\`) {
		t.Errorf("files = %+v; Sonarr splits the first name on / to find the folder", files)
	}

	code, _ = qbitPost(t, c, srv, "torrents/delete", url.Values{"hashes": {qbitTestHash}, "deleteFiles": {"true"}})
	if code != http.StatusOK {
		t.Fatalf("torrents/delete answered %d", code)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Errorf("the store still holds %d tasks after the delete", n)
	}
	if got := qbitInfos(t, c, srv, nil); len(got) != 0 {
		t.Errorf("the deleted torrent is still listed: %+v", got)
	}
	if code, _ := qbitGet(t, c, srv, "torrents/properties", url.Values{"hash": {qbitTestHash}}); code != http.StatusNotFound {
		t.Errorf("properties of a deleted torrent answered %d, want 404", code)
	}
}

// TestAWrongPasswordFails pins qBittorrent's answer to a bad login, which
// Sonarr matches on, and checks that it opens nothing.
func TestAWrongPasswordFails(t *testing.T) {
	t.Parallel()
	a, srv, secret, _ := qbitServer(t, nil)
	c := sonarrClient(t)

	for _, password := range []string{"", "not-a-real-token"} {
		code, body := qbitPost(t, c, srv, "auth/login", url.Values{"username": {"admin"}, "password": {password}})
		if code != http.StatusOK || string(body) != "Fails." {
			t.Errorf("login with password %q answered %d %q, want 200 Fails.", password, code, body)
		}
	}
	u, _ := url.Parse(srv.URL)
	if cookies := c.Jar.Cookies(u); len(cookies) != 0 {
		t.Errorf("a failed login set cookies: %v", cookies)
	}
	if code, _ := qbitGet(t, c, srv, "torrents/info", nil); code != http.StatusForbidden {
		t.Errorf("torrents/info without a session answered %d, want 403, which makes Sonarr log in", code)
	}
	code, _ := qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {"magnet:?xt=urn:btih:" + qbitTestHash}})
	if code != http.StatusForbidden {
		t.Errorf("torrents/add without a session answered %d, want 403", code)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Errorf("the store holds %d tasks after an add without a session", n)
	}
	// A good password in the query string is not taken either: it would sit
	// in every proxy's access log.
	resp, err := c.Post(srv.URL+qbitPrefix+"auth/login?password="+url.QueryEscape(secret), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "Fails." {
		t.Errorf("a password in the query string answered %q, want Fails.", body)
	}
}

// TestASessionLastsAnHour checks that a session stops working once it is an
// hour old, and that logging in again brings it back, which is what Sonarr
// does on a 403.
func TestASessionLastsAnHour(t *testing.T) {
	t.Parallel()
	_, srv, secret, qb := qbitServer(t, nil)
	var clock atomic.Int64
	clock.Store(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC).UnixNano())
	qb.sessions.now = func() time.Time { return time.Unix(0, clock.Load()) }
	c := sonarrClient(t)

	qbitLogin(t, c, srv, secret)
	if code, _ := qbitGet(t, c, srv, "torrents/info", nil); code != http.StatusOK {
		t.Fatalf("a fresh session was refused with %d", code)
	}
	clock.Add(int64(qbitSessionTTL - time.Minute))
	if code, _ := qbitGet(t, c, srv, "torrents/info", nil); code != http.StatusOK {
		t.Fatalf("a session 59 minutes old was refused with %d", code)
	}
	clock.Add(int64(2 * time.Minute))
	if code, _ := qbitGet(t, c, srv, "torrents/info", nil); code != http.StatusForbidden {
		t.Errorf("a session past its hour answered %d, want 403", code)
	}
	qbitLogin(t, c, srv, secret)
	if code, _ := qbitGet(t, c, srv, "torrents/info", nil); code != http.StatusOK {
		t.Errorf("logging in again did not bring the session back: %d", code)
	}
}

// TestASessionEndsWithItsToken checks that revoking the token a session was
// opened with shuts the session while another token lives on, and that
// logging out ends it too.
func TestASessionEndsWithItsToken(t *testing.T) {
	t.Parallel()
	a, srv, secret, _ := qbitServer(t, nil)
	sonarr := a.APITokens.List()[0]
	radarr, radarrSecret, err := a.APITokens.Create("radarr")
	if err != nil {
		t.Fatal(err)
	}
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)
	if code, _ := qbitGet(t, c, srv, "torrents/info", nil); code != http.StatusOK {
		t.Fatalf("a fresh session was refused with %d", code)
	}
	if err := a.APITokens.Revoke(sonarr.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := qbitGet(t, c, srv, "torrents/info", nil); code != http.StatusForbidden {
		t.Errorf("a session of a revoked token answered %d while %s's token is still valid, want 403", code, radarr.Name)
	}

	qbitLogin(t, c, srv, radarrSecret)
	if code, _ := qbitPost(t, c, srv, "auth/logout", nil); code != http.StatusOK {
		t.Fatalf("auth/logout answered %d", code)
	}
	if code, _ := qbitGet(t, c, srv, "torrents/info", nil); code != http.StatusForbidden {
		t.Errorf("a call after logging out answered %d, want 403", code)
	}
}

// TestABearerTokenNeedsNoLogin covers Sonarr's API Key field, which sends the
// token as a Bearer header instead of logging in.
func TestABearerTokenNeedsNoLogin(t *testing.T) {
	t.Parallel()
	_, srv, secret, _ := qbitServer(t, nil)
	for _, c := range []struct {
		token string
		want  int
	}{{secret, http.StatusOK}, {"not-a-real-token", http.StatusForbidden}} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+qbitPrefix+"torrents/info", nil)
		req.Header.Set("Authorization", "Bearer "+c.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("Bearer %q answered %d, want %d", c.token, resp.StatusCode, c.want)
		}
	}
}

// TestTheQbittorrentDoorFollowsTheSwitch checks that the switched-off bridge
// answers 404 to everybody, which Sonarr reads as no qBittorrent at this
// address, and how the open door answers what it does not serve.
func TestTheQbittorrentDoorFollowsTheSwitch(t *testing.T) {
	t.Parallel()
	a, srv, secret, _ := qbitServer(t, nil)
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)

	if code, _ := qbitGet(t, c, srv, "torrents/nonsense", nil); code != http.StatusNotFound {
		t.Errorf("an unknown call answered %d, want 404", code)
	}
	if code, _ := qbitGet(t, c, srv, "torrents/delete", url.Values{"hashes": {qbitTestHash}, "deleteFiles": {"true"}}); code != http.StatusMethodNotAllowed {
		t.Errorf("a delete sent as a GET answered %d, want 405", code)
	}
	if code, _ := qbitGet(t, sonarrClient(t), srv, "torrents/nonsense", nil); code != http.StatusForbidden {
		t.Errorf("an unknown call without a session answered %d, want 403 before anything is looked up", code)
	}

	s := a.Settings.Get()
	s.DownloadClientAPI = false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{"app/webapiVersion", "torrents/info"} {
		if code, _ := qbitGet(t, c, srv, call, nil); code != http.StatusNotFound {
			t.Errorf("%s with the module off answered %d, want 404", call, code)
		}
	}
	if code, _ := qbitPost(t, c, srv, "auth/login", url.Values{"password": {secret}}); code != http.StatusNotFound {
		t.Errorf("auth/login with the module off answered %d, want 404", code)
	}
}

// TestAnUploadedTorrentIsTrackedByItsInfoHash covers Sonarr's other way in: it
// fetches the .torrent itself and uploads it as a multipart file.
func TestAnUploadedTorrentIsTrackedByItsInfoHash(t *testing.T) {
	t.Parallel()
	a, srv, secret, _ := qbitServer(t, nil)
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)

	data := testMultiFileTorrent(t, "Show.S01E02", []metainfo.FileInfo{
		{Length: 900, Path: []string{"show.s01e02.mkv"}},
		{Length: 12, Path: []string{"show.s01e02.srt"}},
	})
	md, err := torrent.Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	code, body := qbitUpload(t, c, srv, "Show.S01E02.torrent", data, url.Values{"category": {"tv-sonarr"}, "paused": {"true"}})
	if code != http.StatusOK || string(body) != "Ok." {
		t.Fatalf("torrents/add with a file answered %d %q", code, body)
	}
	infos := qbitInfos(t, c, srv, url.Values{"hashes": {md.InfoHash}})
	if len(infos) != 1 {
		t.Fatalf("torrents/info by the torrent's info hash holds %d entries, want 1", len(infos))
	}
	if infos[0].State != "pausedDL" {
		t.Errorf("state = %q, want pausedDL for a torrent added paused", infos[0].State)
	}
	if infos[0].Name != "Show.S01E02" {
		t.Errorf("name = %q, want the torrent's own", infos[0].Name)
	}
	if tasks := a.Tasks(); len(tasks) != 1 || tasks[0].Status != core.StatusCollected {
		t.Errorf("a torrent added paused left the collector: %+v", tasks)
	}

	// Started, it joins the queue; stopped, it pauses there.
	qbitPost(t, c, srv, "torrents/start", url.Values{"hashes": {md.InfoHash}})
	if got := qbitInfos(t, c, srv, nil)[0].State; got != "queuedDL" {
		t.Errorf("state after torrents/start = %q, want queuedDL", got)
	}
	qbitPost(t, c, srv, "torrents/pause", url.Values{"hashes": {md.InfoHash}})
	if got := qbitInfos(t, c, srv, nil)[0].State; got != "pausedDL" {
		t.Errorf("state after torrents/pause = %q, want pausedDL", got)
	}

	code, body = qbitUpload(t, c, srv, "broken.torrent", []byte("this is not bencode"), nil)
	if code != http.StatusUnsupportedMediaType {
		t.Errorf("an invalid .torrent answered %d %q, want 415", code, body)
	}
	if n := liveTasks(t, a); n != 1 {
		t.Errorf("the store holds %d tasks after an invalid upload, want the one from before", n)
	}
}

// TestALinkIsFetchedForTheTorrentBehindIt covers Prowlarr, which hands over
// its own download link: one serves the .torrent, another redirects to a
// magnet.
func TestALinkIsFetchedForTheTorrentBehindIt(t *testing.T) {
	t.Parallel()
	_, srv, secret, _ := qbitServer(t, nil)
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)

	data := testMultiFileTorrent(t, "Film.2026", []metainfo.FileInfo{{Length: 900, Path: []string{"film.mkv"}}})
	md, err := torrent.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/magnet" {
			http.Redirect(w, r, "magnet:?xt=urn:btih:"+qbitTestHash, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(data)
	}))
	t.Cleanup(indexer.Close)

	urls := indexer.URL + "/file?id=1\n" + indexer.URL + "/magnet"
	if code, body := qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {urls}}); string(body) != "Ok." {
		t.Fatalf("torrents/add with two links answered %d %q", code, body)
	}
	got := map[string]bool{}
	for _, i := range qbitInfos(t, c, srv, nil) {
		got[i.Hash] = true
	}
	if !got[md.InfoHash] || !got[qbitTestHash] {
		t.Errorf("hashes listed = %v, want the served torrent's %s and the magnet's %s", got, md.InfoHash, qbitTestHash)
	}
}

func qbitUpload(t *testing.T, c *http.Client, srv *httptest.Server, filename string, data []byte, fields url.Values) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("torrents", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	for k, vs := range fields {
		for _, v := range vs {
			_ = mw.WriteField(k, v)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+qbitPrefix+"torrents/add", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// sonarrStatus is the switch in Sonarr's QBittorrent.GetItems, reduced to the
// status it gives each state.
func sonarrStatus(state string) string {
	switch state {
	case "error", "stalledDL", "missingFiles":
		return "Warning"
	case "pausedDL", "stoppedDL":
		return "Paused"
	case "queuedDL", "checkingDL", "checkingUP", "checkingResumeData", "metaDL", "forcedMetaDL":
		return "Queued"
	case "pausedUP", "stoppedUP", "uploading", "stalledUP", "queuedUP", "forcedUP":
		return "Completed"
	}
	return "Downloading"
}

// TestTaskStatesMapOntoQbittorrentStates pins the mapping and what Sonarr
// makes of it: only a download whose bytes are all on disk and unpacked reads
// as completed, and a paused one reads as paused.
func TestTaskStatesMapOntoQbittorrentStates(t *testing.T) {
	t.Parallel()
	magnet := "magnet:?xt=urn:btih:" + qbitTestHash
	cases := []struct {
		name   string
		task   core.Task
		state  string
		sonarr string
	}{
		{"held by the link filter", core.Task{Status: core.StatusCollected, Skipped: true, Enabled: true}, "error", "Warning"},
		{"failed", core.Task{Status: core.StatusError, Enabled: true}, "error", "Warning"},
		{"refused while staging", core.Task{Status: core.StatusCollected, Error: "this magnet link is not usable", Enabled: true}, "error", "Warning"},
		{"gone at the host", core.Task{Status: core.StatusQueued, Online: core.AvailOffline, Enabled: true}, "error", "Warning"},
		{"left in the collector", core.Task{Status: core.StatusCollected, Enabled: true}, "pausedDL", "Paused"},
		{"counting down to auto-confirm", core.Task{Status: core.StatusCollected, Enabled: true, ConfirmDue: time.Now()}, "queuedDL", "Queued"},
		{"waiting in the queue", core.Task{Status: core.StatusQueued, Enabled: true}, "queuedDL", "Queued"},
		{"switched off in the queue", core.Task{Status: core.StatusQueued}, "pausedDL", "Paused"},
		{"held by hand", core.Task{Status: core.StatusQueued, Enabled: true, Hold: true}, "pausedDL", "Paused"},
		{"paused", core.Task{Status: core.StatusPaused, Enabled: true}, "pausedDL", "Paused"},
		{"fetching a magnet's metadata", core.Task{Status: core.StatusRunning, URL: magnet, Enabled: true}, "metaDL", "Queued"},
		{"downloading", core.Task{Status: core.StatusRunning, Size: 100, Loaded: 40, Speed: 10, Enabled: true}, "downloading", "Downloading"},
		{"stalled", core.Task{Status: core.StatusRunning, Size: 100, StalledSince: time.Now(), Enabled: true}, "stalledDL", "Warning"},
		{"unpacking", core.Task{Status: core.StatusExtracting, Enabled: true}, "moving", "Downloading"},
		{"finished", core.Task{Status: core.StatusDone, Enabled: true}, "pausedUP", "Completed"},
		{"seeding to nobody", core.Task{Status: core.StatusDone, Seeding: true, Enabled: true}, "stalledUP", "Completed"},
		{"seeding", core.Task{Status: core.StatusDone, Seeding: true, Peers: 3, Enabled: true}, "uploading", "Completed"},
	}
	for _, c := range cases {
		got := qbitState(&c.task)
		if got != c.state {
			t.Errorf("%s: state = %q, want %q", c.name, got, c.state)
		}
		if s := sonarrStatus(got); s != c.sonarr {
			t.Errorf("%s: Sonarr reads %q as %s, want %s", c.name, got, s, c.sonarr)
		}
		if _, ranked := qbitStateRank[got]; !ranked {
			t.Errorf("%s: %q has no place in qbitStateRank", c.name, got)
		}
	}
}

// TestATorrentOfSeveralTasksIsAsFarAsItsSlowestPart checks that a torrent is
// only completed once every task behind it is, and that a finished one whose
// folder has gone reads as missing rather than completed.
func TestATorrentOfSeveralTasksIsAsFarAsItsSlowestPart(t *testing.T) {
	t.Parallel()
	_, _, _, qb := qbitServer(t, nil)
	folder := filepath.Join(t.TempDir(), "Show")
	file := filepath.Join(folder, "Show.S01E03.mkv")
	live := map[string]*core.Task{
		"done":    {ID: "done", Status: core.StatusDone, Package: "Show", Size: 10, Loaded: 10, File: file},
		"running": {ID: "running", Status: core.StatusRunning, Package: "Show", Size: 10, Loaded: 5, Speed: 1},
	}
	both := qbitTorrent{Hash: syntheticHash("done"), TaskIDs: []string{"done", "running"}, Folder: folder}
	if v := qb.view(both, live); v.state != "downloading" || v.complete() {
		t.Errorf("a torrent with a part still downloading reads %q", v.state)
	}

	finished := qbitTorrent{Hash: syntheticHash("done"), TaskIDs: []string{"done"}, Folder: folder}
	if v := qb.view(finished, live); v.state != "missingFiles" {
		t.Errorf("a finished torrent whose folder %q is gone reads %q, want missingFiles", v.contentPath, v.state)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := qb.view(finished, live)
	if v.state != "pausedUP" || v.contentPath != folder || v.savePath != filepath.Dir(folder) {
		t.Errorf("a finished torrent reads %q with content_path %q and save_path %q, want pausedUP at %q",
			v.state, v.contentPath, v.savePath, folder)
	}
	// Sonarr removes a finished torrent after the import only once its ratio
	// limit is reached.
	if got := v.info().RatioLimit; got != 0 {
		t.Errorf("ratio_limit of a torrent nothing seeds any more = %v, want 0", got)
	}
}

// TestATorrentAlreadyInTheListIsNotTakenOver adds a magnet the owner pasted
// earlier. qBittorrent answers Fails. for a torrent it already has and leaves
// it as it was, and so does the bridge: Sonarr must never be shown one of the
// owner's downloads it could move or delete.
func TestATorrentAlreadyInTheListIsNotTakenOver(t *testing.T) {
	t.Parallel()
	a, srv, secret, _ := qbitServer(t, nil)
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)

	magnet := "magnet:?xt=urn:btih:" + qbitTestHash + "&dn=Home.Video"
	if mine := a.AddLinksFrom([]string{magnet}, "mine", app.OriginPaste); len(mine) != 1 {
		t.Fatalf("the owner's paste staged %d tasks, want 1", len(mine))
	}

	code, body := qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {magnet}, "category": {"tv-sonarr"}})
	if code != http.StatusOK || string(body) != "Fails." {
		t.Errorf("adding a torrent the list already has answered %d %q, want 200 Fails.", code, body)
	}
	if got := qbitInfos(t, c, srv, nil); len(got) != 0 {
		t.Errorf("Sonarr is shown the owner's download: %+v", got)
	}
	qbitPost(t, c, srv, "torrents/delete", url.Values{"hashes": {qbitTestHash}, "deleteFiles": {"true"}})
	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("the list holds %d tasks after Sonarr's delete, want the owner's one", len(tasks))
	}
	if tasks[0].Package != "mine" || tasks[0].Category != "" {
		t.Errorf("the owner's download was refiled: package %q, category %q", tasks[0].Package, tasks[0].Category)
	}
}

// TestATorrentAddedStoppedStaysStoppedUnderAutoConfirm covers Sonarr's and
// Prowlarr's Initial State "Stopped", which sends paused (stopped from
// qBittorrent 5 on) and must not be undone by auto-confirm.
func TestATorrentAddedStoppedStaysStoppedUnderAutoConfirm(t *testing.T) {
	t.Parallel()
	a, srv, secret, _ := qbitServer(t, func(s *settings.Settings) { s.AutoConfirm, s.AutoConfirmDelay = true, 0 })
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)

	for param, hash := range map[string]string{"paused": qbitTestHash, "stopped": qbitOtherHash} {
		code, body := qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {"magnet:?xt=urn:btih:" + hash}, param: {"true"}})
		if code != http.StatusOK || string(body) != "Ok." {
			t.Fatalf("torrents/add with %s answered %d %q", param, code, body)
		}
		infos := qbitInfos(t, c, srv, url.Values{"hashes": {hash}})
		if len(infos) != 1 || infos[0].State != "pausedDL" {
			t.Errorf("a torrent added with %s=true reads %+v, want one in pausedDL", param, infos)
		}
	}
	for _, task := range a.Tasks() {
		if task.Status != core.StatusCollected {
			t.Errorf("%s is %s, want it left in the collector", task.Name, task.Status)
		}
	}
}

// TestACategoryIsFoundByTheNameItIsListedUnder has a category whose name is
// Sonarr's and whose id is not. Everything Sonarr does with the name it saw in
// the listing has to reach that category, and none of it may make a second
// one.
func TestACategoryIsFoundByTheNameItIsListedUnder(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	high := 3
	a, srv, secret, _ := qbitServer(t, func(s *settings.Settings) {
		s.Categories = []settings.Category{{ID: "serien", Name: "tv-sonarr", Dir: folder, Priority: &high}}
	})
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)

	_, body := qbitGet(t, c, srv, "torrents/categories", nil)
	var cats map[string]map[string]string
	if err := json.Unmarshal(body, &cats); err != nil {
		t.Fatal(err)
	}
	if cats["tv-sonarr"]["savePath"] != folder {
		t.Fatalf("categories = %v, want tv-sonarr listed with its folder %q", cats, folder)
	}
	if code, body := qbitPost(t, c, srv, "torrents/setCategory", url.Values{"hashes": {"all"}, "category": {"tv-sonarr"}}); code != http.StatusOK {
		t.Errorf("setCategory to a listed category answered %d %q", code, body)
	}
	if code, body := qbitPost(t, c, srv, "torrents/createCategory", url.Values{"category": {"tv-sonarr"}}); code != http.StatusOK {
		t.Errorf("createCategory of a listed category answered %d %q", code, body)
	}

	magnet := "magnet:?xt=urn:btih:" + qbitTestHash + "&dn=Show.S01E01"
	if code, body := qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {magnet}, "category": {"tv-sonarr"}}); string(body) != "Ok." {
		t.Fatalf("torrents/add answered %d %q", code, body)
	}
	if got := a.Settings.Get().Categories; len(got) != 1 {
		t.Errorf("categories after the add = %+v, want the one there was", got)
	}
	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(tasks))
	}
	if tasks[0].Category != "serien" {
		t.Errorf("the task is filed under %q, want serien", tasks[0].Category)
	}
	if got := a.TaskFolder(tasks[0].ID); filepath.Dir(got) != folder {
		t.Errorf("the task downloads to %q, want a folder inside the category's %q", got, folder)
	}
	if tasks[0].Priority != high {
		t.Errorf("the task starts at priority %d, want the category's %d", tasks[0].Priority, high)
	}
	if infos := qbitInfos(t, c, srv, url.Values{"category": {"tv-sonarr"}}); len(infos) != 1 {
		t.Errorf("Sonarr's category lists %d torrents, want the one it added", len(infos))
	}
}

// TestALinkThatLeadsToNoTorrentIsRefused covers an indexer link that answers
// with an error or a login page instead of a torrent. It is left out rather
// than downloaded as a file, and a request with nothing else in it fails.
func TestALinkThatLeadsToNoTorrentIsRefused(t *testing.T) {
	t.Parallel()
	a, srv, secret, _ := qbitServer(t, nil)
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)

	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/busy" {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>Please log in</body></html>")
	}))
	t.Cleanup(indexer.Close)

	urls := indexer.URL + "/busy\n" + indexer.URL + "/download?id=1\nftp://indexer.example/x.torrent"
	if code, body := qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {urls}}); code != http.StatusOK || string(body) != "Fails." {
		t.Errorf("links that lead to no torrent answered %d %q, want 200 Fails.", code, body)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Errorf("the store holds %d tasks after links that lead to no torrent", n)
	}

	urls = indexer.URL + "/busy\nmagnet:?xt=urn:btih:" + qbitTestHash
	if code, body := qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {urls}}); string(body) != "Ok." {
		t.Errorf("a magnet beside a dead link answered %d %q, want Ok.", code, body)
	}
	if n := liveTasks(t, a); n != 1 {
		t.Errorf("the store holds %d tasks, want only the magnet's", n)
	}
}

// TestShareLimitsAreKeptAndReportedBack covers the seed goal Sonarr sends
// with an add, from an indexer's Seed Ratio and Seed Time, and changes with
// setShareLimits.
func TestShareLimitsAreKeptAndReportedBack(t *testing.T) {
	t.Parallel()
	_, srv, secret, _ := qbitServer(t, nil)
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)

	qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {"magnet:?xt=urn:btih:" + qbitOtherHash}})
	qbitPost(t, c, srv, "torrents/add", url.Values{
		"urls": {"magnet:?xt=urn:btih:" + qbitTestHash}, "ratioLimit": {"1.5"}, "seedingTimeLimit": {"4320"},
	})
	type limits struct {
		ratio   float64
		minutes int64
	}
	read := func() map[string]limits {
		out := map[string]limits{}
		for _, i := range qbitInfos(t, c, srv, nil) {
			out[i.Hash] = limits{i.RatioLimit, i.SeedingTimeLimit}
		}
		return out
	}
	got := read()
	if got[qbitOtherHash] != (limits{-2, -2}) {
		t.Errorf("a torrent added without limits reports %+v, want -2 for both, the instance's", got[qbitOtherHash])
	}
	if got[qbitTestHash] != (limits{1.5, 4320}) {
		t.Errorf("a torrent added with ratio 1.5 and 4320 minutes reports %+v", got[qbitTestHash])
	}

	code, _ := qbitPost(t, c, srv, "torrents/setShareLimits", url.Values{
		"hashes": {qbitTestHash}, "ratioLimit": {"-2"}, "seedingTimeLimit": {"-1"}, "inactiveSeedingTimeLimit": {"-2"},
	})
	if code != http.StatusOK {
		t.Fatalf("setShareLimits answered %d", code)
	}
	if got := read()[qbitTestHash]; got != (limits{-2, -1}) {
		t.Errorf("after setShareLimits the torrent reports %+v, want the instance's ratio and no time limit", got)
	}
	if code, _ := qbitPost(t, c, srv, "torrents/setShareLimits", url.Values{"hashes": {qbitTestHash}, "ratioLimit": {"1"}}); code != http.StatusBadRequest {
		t.Errorf("setShareLimits without seedingTimeLimit answered %d, want 400", code)
	}
}

// sonarrPrefs is the part of app/preferences Sonarr's seed limit check reads.
type sonarrPrefs struct {
	MaxRatioEnabled       bool    `json:"max_ratio_enabled"`
	MaxRatio              float64 `json:"max_ratio"`
	MaxSeedingTimeEnabled bool    `json:"max_seeding_time_enabled"`
	MaxSeedingTime        int64   `json:"max_seeding_time"`
}

// sonarrMayRemove is Sonarr's CanBeRemoved for a qBittorrent torrent: stopped
// after finishing, and past a seed limit, its own or the client's.
func sonarrMayRemove(i qbitInfo, p sonarrPrefs) bool {
	if i.State != "pausedUP" && i.State != "stoppedUP" {
		return false
	}
	if i.RatioLimit >= 0 && i.Ratio >= i.RatioLimit {
		return true
	}
	if i.RatioLimit == -2 && p.MaxRatioEnabled && math.Round(i.Ratio*100)/100 >= p.MaxRatio {
		return true
	}
	switch {
	case i.SeedingTimeLimit >= 0:
		return i.SeedingTime >= i.SeedingTimeLimit*60
	case i.SeedingTimeLimit == -2 && p.MaxSeedingTimeEnabled:
		return i.SeedingTime >= p.MaxSeedingTime*60
	}
	return false
}

// appWithTasks is an app that finds tasks in its store when it starts, the
// way it finds a finished download after a restart.
func appWithTasks(t *testing.T, tasks ...core.Task) *app.App {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range tasks {
		if err := st.Save(&tasks[i]); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// TestAFinishedTorrentReadsTheWaySonarrImportsAndRemovesIt reads two finished
// torrents through torrents/info, the call Sonarr's import and its Remove
// Completed act on. Both import from their folder. The one without limits of
// its own may be removed; the one whose indexer asked for a ratio it has not
// reached may not.
func TestAFinishedTorrentReadsTheWaySonarrImportsAndRemovesIt(t *testing.T) {
	t.Parallel()
	content := filepath.Join(t.TempDir(), "Show.S01E05")
	if err := os.MkdirAll(content, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(content, "show.s01e05.mkv"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	finished := func(id, hash string) core.Task {
		return core.Task{
			ID: id, URL: "magnet:?xt=urn:btih:" + hash, Name: "Show.S01E05", Package: "Show.S01E05",
			InfoHash: hash, Dir: content, Size: 10, Loaded: 10,
			Status: core.StatusDone, Enabled: true, CreatedAt: now, FinishedAt: now,
		}
	}
	a := appWithTasks(t, finished("open", qbitTestHash), finished("held", qbitOtherHash))
	_, srv, secret, qb := qbitServerOn(t, a, nil)
	one := 1.0
	for _, rec := range []qbitTorrent{
		{Hash: qbitTestHash, Category: "tv-sonarr", TaskIDs: []string{"open"}, Folder: content, AddedAt: now},
		{Hash: qbitOtherHash, Category: "tv-sonarr", TaskIDs: []string{"held"}, Folder: content, AddedAt: now, RatioLimit: &one},
	} {
		if err := qb.record(rec); err != nil {
			t.Fatal(err)
		}
	}
	c := sonarrClient(t)
	qbitLogin(t, c, srv, secret)

	_, body := qbitGet(t, c, srv, "app/preferences", nil)
	var prefs sonarrPrefs
	if err := json.Unmarshal(body, &prefs); err != nil {
		t.Fatal(err)
	}
	byHash := map[string]qbitInfo{}
	for _, i := range qbitInfos(t, c, srv, url.Values{"category": {"tv-sonarr"}}) {
		byHash[i.Hash] = i
	}
	for _, hash := range []string{qbitTestHash, qbitOtherHash} {
		i, ok := byHash[hash]
		if !ok {
			t.Fatalf("torrent %s is not listed: %v", hash, byHash)
		}
		if i.State != "pausedUP" || i.Progress != 1 || i.AmountLeft != 0 {
			t.Errorf("%s reads %s at %v with %d bytes left, want pausedUP and done", hash, i.State, i.Progress, i.AmountLeft)
		}
		if i.ContentPath != content || i.SavePath != filepath.Dir(content) {
			t.Errorf("%s has content_path %q and save_path %q, want %q inside %q",
				hash, i.ContentPath, i.SavePath, content, filepath.Dir(content))
		}
	}
	if open := byHash[qbitTestHash]; !sonarrMayRemove(open, prefs) {
		t.Errorf("Sonarr would never remove a finished torrent nothing seeds and nobody set limits for: %+v", open)
	}
	if held := byHash[qbitOtherHash]; held.RatioLimit != 1 || sonarrMayRemove(held, prefs) {
		t.Errorf("a torrent short of the ratio its indexer asked for reads ratio_limit %v and removable %v, want 1 and false",
			held.RatioLimit, sonarrMayRemove(held, prefs))
	}
}

// TestAListingDuringAnAddKeepsTheNewTorrent lets a listing wait on the torrent
// document while an add stages a magnet and records it. The listing prunes
// torrents whose tasks are gone, and must not take the new one for such.
func TestAListingDuringAnAddKeepsTheNewTorrent(t *testing.T) {
	t.Parallel()
	a, _, _, qb := qbitServer(t, nil)

	qb.mu.Lock()
	listed := make(chan struct{})
	go func() {
		qb.views()
		close(listed)
	}()
	// Long enough for a listing that reads the task list before it takes the
	// lock to have read it.
	time.Sleep(100 * time.Millisecond)
	created := a.AddLinksFrom([]string{"magnet:?xt=urn:btih:" + qbitTestHash}, "", app.OriginPaste)
	var err error
	if len(created) == 1 {
		var torrents map[string]qbitTorrent
		if torrents, err = qb.load(); err == nil {
			torrents[qbitTestHash] = qbitTorrent{Hash: qbitTestHash, TaskIDs: []string{created[0].ID}, AddedAt: time.Now()}
			err = qb.store(torrents)
		}
	}
	qb.mu.Unlock()
	<-listed
	if len(created) != 1 || err != nil {
		t.Fatalf("staged %d tasks, recorded with %v", len(created), err)
	}

	qb.mu.Lock()
	torrents, err := qb.load()
	qb.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := torrents[qbitTestHash]; !ok {
		t.Error("the listing pruned the torrent the add had just recorded, so Sonarr never sees its grab again")
	}
}

// qbitBearer makes one call with the token as a Bearer header, the way
// Sonarr's API Key field sends it: a POST with a form for the calls that
// change something, a GET for the rest.
func qbitBearer(t *testing.T, srv *httptest.Server, secret, call string, form url.Values) (int, string) {
	t.Helper()
	method, body := http.MethodGet, io.Reader(nil)
	target := srv.URL + qbitPrefix + call
	if qbitPostOnly[call] {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	} else if len(form) > 0 {
		target += "?" + form.Encode()
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestSonarrsRoundWorksWithAnAddAndReadToken walks Sonarr's calls with the
// preset the Access page offers for it. Its cleanup of a finished torrent
// forgets the torrent, and the download stays, since the token cannot control;
// a torrent still on its way is not forgotten.
func TestSonarrsRoundWorksWithAnAddAndReadToken(t *testing.T) {
	t.Parallel()
	content := filepath.Join(t.TempDir(), "Show.S01E07")
	if err := os.MkdirAll(content, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a := appWithTasks(t, core.Task{
		ID: "done", URL: "magnet:?xt=urn:btih:" + qbitOtherHash, Name: "Show.S01E07", Package: "Show.S01E07",
		InfoHash: qbitOtherHash, Dir: content, Size: 10, Loaded: 10,
		Status: core.StatusDone, Enabled: true, CreatedAt: now, FinishedAt: now,
	})
	_, srv, _, qb := qbitServerOn(t, a, nil)
	if err := qb.record(qbitTorrent{Hash: qbitOtherHash, Category: "tv-sonarr", TaskIDs: []string{"done"}, Folder: content, AddedAt: now}); err != nil {
		t.Fatal(err)
	}
	_, secret, err := a.APITokens.CreateScoped("sonarr-add-read", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	c := sonarrClient(t)
	if got := qbitLogin(t, c, srv, secret); got != "Ok." {
		t.Fatalf("login with an add and read token answered %q", got)
	}
	for _, call := range []string{"app/version", "app/webapiVersion", "app/preferences", "torrents/categories"} {
		if code, body := qbitGet(t, c, srv, call, nil); code != http.StatusOK {
			t.Errorf("%s answered %d %s to an add and read token", call, code, body)
		}
	}
	if code, body := qbitPost(t, c, srv, "torrents/createCategory", url.Values{"category": {"tv-sonarr"}}); code != http.StatusOK {
		t.Errorf("createCategory answered %d %s to an add and read token", code, body)
	}
	magnet := "magnet:?xt=urn:btih:" + qbitTestHash + "&dn=Show.S01E08"
	if code, body := qbitPost(t, c, srv, "torrents/add", url.Values{"urls": {magnet}, "category": {"tv-sonarr"}}); string(body) != "Ok." {
		t.Fatalf("torrents/add answered %d %q to an add and read token", code, body)
	}
	// What Sonarr sends right after an add: the initial state and the
	// indexer's seed limits.
	hashes := url.Values{"hashes": {qbitTestHash}}
	if code, body := qbitPost(t, c, srv, "torrents/resume", hashes); code != http.StatusOK {
		t.Errorf("resuming the torrent just added answered %d %s", code, body)
	}
	limits := url.Values{"hashes": {qbitTestHash}, "ratioLimit": {"1"}, "seedingTimeLimit": {"-2"}}
	if code, body := qbitPost(t, c, srv, "torrents/setShareLimits", limits); code != http.StatusOK {
		t.Errorf("setShareLimits answered %d %s", code, body)
	}
	if got := qbitInfos(t, c, srv, url.Values{"category": {"tv-sonarr"}}); len(got) != 2 {
		t.Fatalf("torrents/info lists %d torrents, want the finished one and the one added", len(got))
	}

	gone := url.Values{"hashes": {qbitTestHash}, "deleteFiles": {"true"}}
	if code, body := qbitPost(t, c, srv, "torrents/delete", gone); code != http.StatusForbidden || !strings.Contains(string(body), `"control"`) {
		t.Errorf("deleting a torrent on its way answered %d %q, want 403 naming the control right", code, body)
	}
	done := url.Values{"hashes": {qbitOtherHash}, "deleteFiles": {"true"}}
	if code, body := qbitPost(t, c, srv, "torrents/delete", done); code != http.StatusOK {
		t.Fatalf("Sonarr's cleanup of a finished torrent answered %d %s", code, body)
	}
	infos := qbitInfos(t, c, srv, nil)
	if len(infos) != 1 || infos[0].Hash != qbitTestHash {
		t.Errorf("after the cleanup torrents/info lists %+v, want only the torrent on its way", infos)
	}
	if n := liveTasks(t, a); n != 2 {
		t.Errorf("the store holds %d tasks after the cleanup, want both, since the token cannot control", n)
	}
	if _, err := os.Stat(content); err != nil {
		t.Errorf("the finished download's folder is gone after a cleanup without control: %v", err)
	}

	a.PauseTasks(qb.taskIDs(t, qbitTestHash))
	if code, body := qbitPost(t, c, srv, "torrents/resume", hashes); code != http.StatusForbidden || !strings.Contains(string(body), `"control"`) {
		t.Errorf("resuming a paused download answered %d %q, want 403 naming the control right", code, body)
	}
}

// taskIDs is the tasks the bridge recorded for hash.
func (qb *qbitClient) taskIDs(t *testing.T, hash string) []string {
	t.Helper()
	qb.mu.Lock()
	defer qb.mu.Unlock()
	torrents, err := qb.load()
	if err != nil {
		t.Fatal(err)
	}
	return torrents[hash].TaskIDs
}

// TestEveryQbittorrentCallNeedsTheRightItsTableNames goes through
// qbittorrentScopes: a token with only that right gets past the check, and
// one with every other right is refused with the right named.
func TestEveryQbittorrentCallNeedsTheRightItsTableNames(t *testing.T) {
	t.Parallel()
	a, srv, _, _ := qbitServer(t, nil)
	token := func(scopes []apitoken.Scope) string {
		t.Helper()
		_, secret, err := a.APITokens.CreateScoped("probe", scopes)
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	for call, need := range qbittorrentScopes {
		if code, body := qbitBearer(t, srv, token([]apitoken.Scope{need}), call, nil); strings.Contains(body, "does not have the") {
			t.Errorf("%s with only the %q right answered %d %q", call, need, code, body)
		}
		var others []apitoken.Scope
		for _, s := range apitoken.AllScopes() {
			if s != need && !(qbittorrentAddMay[call] && s == apitoken.ScopeAdd) {
				others = append(others, s)
			}
		}
		if code, body := qbitBearer(t, srv, token(others), call, nil); code != http.StatusForbidden || !strings.Contains(body, `"`+string(need)+`"`) {
			t.Errorf("%s with %v answered %d %q, want 403 naming the %q right", call, others, code, body, need)
		}
	}
}

// TestASavePathNeedsAdmin holds torrents/add to the rule for POST /api/links:
// where the files land on the host is configuration.
func TestASavePathNeedsAdmin(t *testing.T) {
	t.Parallel()
	a, srv, full, _ := qbitServer(t, nil)
	_, adder, err := a.APITokens.CreateScoped("sonarr", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(a.Settings.Get().DownloadDir, "elsewhere")
	form := url.Values{"urls": {"magnet:?xt=urn:btih:" + qbitTestHash}, "savepath": {dir}}
	if code, body := qbitBearer(t, srv, adder, "torrents/add", form); code != http.StatusForbidden || !strings.Contains(body, `"admin"`) {
		t.Errorf("a save path from an add token answered %d %q, want 403 naming the admin right", code, body)
	}
	if n := liveTasks(t, a); n != 0 {
		t.Fatalf("the refused add staged %d tasks", n)
	}
	if code, body := qbitBearer(t, srv, full, "torrents/add", form); body != "Ok." {
		t.Errorf("a save path from a full token answered %d %q, want Ok.", code, body)
	}
}

// A category Sonarr names that this instance does not have is filed the same
// way whichever door it comes through: under that name, with a folder of its
// own in the download folder.
func TestBothDoorsFileAMissingCategoryAlike(t *testing.T) {
	t.Parallel()
	const name = "TV Shows"
	magnet := "magnet:?xt=urn:btih:" + qbitTestHash
	filed := func(a *app.App) settings.Category {
		t.Helper()
		s := a.Settings.Get()
		if len(s.Categories) != 1 {
			t.Fatalf("categories = %+v, want the one Sonarr named", s.Categories)
		}
		c := s.Categories[0]
		rel, err := filepath.Rel(s.DownloadDir, c.Dir)
		if err != nil || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
			t.Fatalf("the category's folder %q is not inside the download folder %q", c.Dir, s.DownloadDir)
		}
		c.Dir = rel
		return c
	}

	sab, sabSrv, key := downloadClientServer(t, nil)
	if _, add := sabAddFile(t, sabSrv, key, "Show.S01E01.nzb", name, []byte(magnet)); add["status"] != true {
		t.Fatalf("addfile answered %+v", add)
	}
	want := filed(sab)

	for call, form := range map[string]url.Values{
		"torrents/createCategory": {"category": {name}},
		"torrents/add":            {"urls": {magnet}, "category": {name}},
	} {
		a, srv, secret, _ := qbitServer(t, nil)
		c := sonarrClient(t)
		qbitLogin(t, c, srv, secret)
		if code, body := qbitPost(t, c, srv, call, form); code != http.StatusOK {
			t.Fatalf("%s answered %d %q", call, code, body)
		}
		if got := filed(a); got.ID != want.ID || got.Name != want.Name || got.Dir != want.Dir {
			t.Errorf("%s filed %s %q in %q, the SABnzbd door %s %q in %q", call, got.ID, got.Name, got.Dir, want.ID, want.Name, want.Dir)
		}
	}
}

// A save path is a folder on the host, so it takes a token with admin. It
// picks the folder of a category that is new; one that exists keeps its own.
func TestACategorySavePathNeedsAdminAndOnlyShapesANewCategory(t *testing.T) {
	t.Parallel()
	a, srv, full, _ := qbitServer(t, nil)
	_, sonarr, err := a.APITokens.CreateScoped("sonarr", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"category": {"films"}, "savePath": {"Filme"}}

	if code, body := qbitBearer(t, srv, sonarr, "torrents/createCategory", form); code != http.StatusForbidden || !strings.Contains(body, `"admin"`) {
		t.Errorf("a save path from an add token answered %d %q, want 403 naming the admin right", code, body)
	}
	if got := a.Settings.Get().Categories; len(got) != 0 {
		t.Fatalf("the refused call filed %+v", got)
	}

	if code, body := qbitBearer(t, srv, full, "torrents/createCategory", form); code != http.StatusOK {
		t.Fatalf("a save path from a full token answered %d %q", code, body)
	}
	want := filepath.Join(a.Settings.Get().DownloadDir, "Filme")
	if got := a.Settings.Get().CategoryFor("films").Dir; got != want {
		t.Errorf("the new category's folder is %q, want the save path %q inside the download folder", got, want)
	}

	elsewhere := url.Values{"category": {"films"}, "savePath": {t.TempDir()}}
	if code, body := qbitBearer(t, srv, full, "torrents/createCategory", elsewhere); code != http.StatusOK {
		t.Fatalf("creating a category that exists answered %d %q", code, body)
	}
	if got := a.Settings.Get().CategoryFor("films").Dir; got != want {
		t.Errorf("a save path moved an existing category to %q, want it kept in %q", got, want)
	}
}
