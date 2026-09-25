package api

// A second download client for Sonarr, Radarr and Prowlarr: the part of
// qBittorrent's Web API v2 they call (Sonarr's QBittorrentProxyV2.cs), so a
// torrent indexer can hand its grabs to this instance. It sits beside the
// SABnzbd bridge in routes_downloadclient.go and opens with the same switch.
//
// qBittorrent logs in with a form and a session cookie. The password is one of
// this instance's API tokens and the username is not checked; the session
// lasts an hour, belongs to the token that opened it and ends when that token
// is revoked. A Bearer token is taken as well, which is what Sonarr sends once
// its API Key field is filled in. Each call needs the right qbittorrentScopes
// gives it in scopes.go, whichever way the token came.
//
// What is added goes through the ordinary intake, like a pasted magnet, and
// each torrent is reported under its info hash, the id Sonarr tracks it by.
// Only the bridge's own torrents are listed, so Sonarr never imports, moves or
// deletes the owner's other downloads.
//
// The calls live under /api/qbittorrent because /api belongs to this app;
// setting the client's URL Base to "api/qbittorrent" lands them here.

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/pathvars"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const qbitPrefix = "/api/qbittorrent/api/v2/"

// qbitVersion and qbitAPIVersion are the qBittorrent release this file
// speaks. 4.6 is new enough for content_path and share limits on add, and old
// enough that Sonarr sends "paused" and reads "pausedUP", the names every
// Sonarr and Radarr release knows; the 5.x names are accepted as well.
const (
	qbitVersion    = "v4.6.7"
	qbitAPIVersion = "2.9.3"
)

// qbitSessionTTL is qBittorrent's own default session timeout. Sonarr logs in
// again whenever a call is refused, so a short session costs one request.
const qbitSessionTTL = time.Hour

// qbitMaxSessions bounds the session table, since every login adds a row.
const qbitMaxSessions = 64

// qbitCookie is qBittorrent's default session cookie name.
const qbitCookie = "SID"

// qbitBucket is the interface-state bucket the bridge keeps its torrents in,
// for the same reasons as downloadClientBucket.
const qbitBucket = "qbittorrent"

// qbitNoETA is what qBittorrent sends for an ETA it cannot give, and what
// Sonarr reads as none.
const qbitNoETA = 8640000

// qbitMaxForm bounds the body of every call but torrents/add, which carries
// .torrent files and gets room for a few of them.
const (
	qbitMaxForm = 1 << 20
	qbitMaxAdd  = 4*torrent.MaxTorrentBytes + 1<<20
)

// qbitFetchTimeout bounds fetching a .torrent from a link an indexer handed
// over, which Sonarr waits for.
const qbitFetchTimeout = 30 * time.Second

// qbitPostOnly are the calls qBittorrent refuses as a GET, every one that
// changes something.
var qbitPostOnly = map[string]bool{
	"auth/login": true, "auth/logout": true,
	"torrents/add": true, "torrents/delete": true,
	"torrents/pause": true, "torrents/resume": true, "torrents/stop": true, "torrents/start": true,
	"torrents/setCategory": true, "torrents/createCategory": true, "torrents/setShareLimits": true,
	"torrents/topPrio": true, "torrents/setForceStart": true,
}

// qbitStateRank orders the states from least to most finished. A torrent made
// of several tasks takes the least finished one, so nothing is imported while
// any part of it is missing.
var qbitStateRank = map[string]int{
	"error": 0, "downloading": 1, "metaDL": 2, "stalledDL": 3, "queuedDL": 4, "pausedDL": 5,
	"moving": 6, "uploading": 7, "stalledUP": 8, "pausedUP": 9,
}

// qbitTorrent is one thing handed over: the hash it is known by, the category
// it came under, the tasks it became and the share limits it was given.
type qbitTorrent struct {
	Hash     string    `json:"hash"`
	Category string    `json:"category"`
	TaskIDs  []string  `json:"taskIds"`
	AddedAt  time.Time `json:"addedAt"`
	// RatioLimit and SeedingTimeLimit are in qBittorrent's terms, the time in
	// minutes and -1 for no limit. Nil leaves the torrent to the instance's
	// seeding targets.
	RatioLimit       *float64 `json:"ratioLimit,omitempty"`
	SeedingTimeLimit *int64   `json:"seedingTimeLimit,omitempty"`
}

// qbitMaxRatio and qbitMaxSeedingMinutes are where qBittorrent caps a
// torrent's share limits.
const (
	qbitMaxRatio          = 9998
	qbitMaxSeedingMinutes = 525600
)

// qbitRatioLimit reads a ratioLimit parameter as qBittorrent does: -2, or
// nothing a number can be read from, leaves the limit to the instance, and
// anything else below zero means none.
func qbitRatioLimit(raw string) *float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(v) || v == -2 {
		return nil
	}
	if v < 0 {
		v = -1
	}
	v = min(v, qbitMaxRatio)
	return &v
}

// qbitSeedingTimeLimit is qbitRatioLimit for seedingTimeLimit, in minutes.
func qbitSeedingTimeLimit(raw string) *int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v == -2 {
		return nil
	}
	if v < 0 {
		v = -1
	}
	v = min(v, qbitMaxSeedingMinutes)
	return &v
}

type qbitClient struct {
	a        *app.App
	sessions *qbitSessions
	fetcher  *http.Client
	// mu serialises the read-modify-write of the torrent document and of the
	// category table.
	mu sync.Mutex
	// rid numbers sync/maindata answers; every one is a full update.
	rid atomic.Int64
}

func newQbitClient(a *app.App) *qbitClient {
	lookup := func(tokenID string) (apitoken.Token, bool) {
		for _, t := range a.APITokens.List() {
			if t.ID == tokenID {
				return t, true
			}
		}
		return apitoken.Token{}, false
	}
	return &qbitClient{a: a, sessions: newQbitSessions(lookup), fetcher: newQbitFetcher()}
}

func registerQBittorrent(reg *Registry, a *app.App) {
	newQbitClient(a).register(reg)
}

func (qb *qbitClient) register(reg *Registry) {
	// Open because the session cookie is qBittorrent's, which the guard does not
	// know. serve checks the cookie or a token on every call but the login, even
	// without a password, and answers 404 while the module is off.
	reg.AddOpen(http.MethodGet, qbitPrefix+"{call...}",
		"qBittorrent-shaped download client for Sonarr, Radarr and Prowlarr (set their URL Base to \"api/qbittorrent\"); "+
			"off unless \"Download client for Sonarr and Radarr\" is switched on, and the login password is an API token of this instance that can add and read",
		qb.serve)
	reg.AddOpen(http.MethodPost, qbitPrefix+"{call...}",
		"the same door for the login and every call that changes something, which qBittorrent only takes as a POST",
		qb.serve)
}

func (qb *qbitClient) serve(w http.ResponseWriter, r *http.Request) {
	// The switch comes before the credential, so a closed door does not reveal
	// whether a token would have worked. The wording matches the /api/ catch-all.
	if !qb.a.Settings.Get().DownloadClientAPI {
		http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		return
	}
	call := r.PathValue("call")
	if call == "auth/login" {
		qb.login(w, r)
		return
	}
	// qBittorrent refuses before it looks the call up, so a caller without a
	// session learns nothing about which calls exist.
	tok, ok := qb.authorized(r)
	if !ok {
		qbitText(w, http.StatusForbidden, "Forbidden")
		return
	}
	if qbitPostOnly[call] && r.Method != http.MethodPost {
		qbitText(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	if call == "auth/logout" {
		if c, err := r.Cookie(qbitCookie); err == nil {
			qb.sessions.end(c.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: qbitCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
		return
	}
	need, known := qbittorrentScopes[call]
	if !known {
		qbitText(w, http.StatusNotFound, "Not Found")
		return
	}
	if !tok.Has(need) && !(qbittorrentAddMay[call] && tok.Has(apitoken.ScopeAdd)) {
		qbitRefuse(w, need)
		return
	}
	control := tok.Has(apitoken.ScopeControl)
	if call == "torrents/add" {
		qb.add(w, r, tok.Has(apitoken.ScopeAdmin))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, qbitMaxForm)
	if err := r.ParseForm(); err != nil {
		qbitText(w, http.StatusBadRequest, "Bad Request")
		return
	}

	switch call {
	case "app/version":
		qbitText(w, http.StatusOK, qbitVersion)
	case "app/webapiVersion":
		qbitText(w, http.StatusOK, qbitAPIVersion)
	case "app/buildInfo":
		// None of qBittorrent's libraries are in this build, so their versions
		// are left empty rather than made up.
		writeJSON(w, map[string]any{
			"qt": "", "libtorrent": "", "boost": "", "openssl": "", "zlib": "",
			"bitness": strconv.IntSize, "platform": runtime.GOOS,
		})
	case "app/preferences":
		writeJSON(w, qb.preferences())
	case "torrents/info":
		qb.info(w, r)
	case "torrents/properties":
		qb.properties(w, r)
	case "torrents/files":
		qb.files(w, r)
	case "torrents/delete":
		qb.remove(w, r, control)
	case "torrents/pause", "torrents/stop":
		if ids, ok := qb.pick(w, r, nil); ok {
			qb.a.PauseTasks(ids)
		}
	case "torrents/resume", "torrents/start":
		if ids, ok := qb.pick(w, r, nil); ok && len(ids) > 0 {
			if !control && qb.holdsPaused(ids) {
				qbitRefuse(w, apitoken.ScopeControl)
				return
			}
			// Staged tasks leave the collector and paused ones rejoin the
			// queue; each call leaves the other kind alone.
			qb.a.StartTasks(ids)
			qb.a.ResumeTasks(ids)
		}
	case "torrents/topPrio":
		if ids, ok := qb.pick(w, r, nil); ok {
			qb.a.MoveTasks(ids, app.MoveTop)
		}
	case "torrents/setForceStart":
		if ids, ok := qb.pick(w, r, nil); ok && len(ids) > 0 {
			if qbitBool(r.Form.Get("value")) {
				qb.a.ForceDownload(app.Selection{Ids: ids})
			} else {
				qb.a.SetForced(ids, false)
			}
		}
	case "torrents/setShareLimits":
		qb.setShareLimits(w, r)
	case "torrents/setCategory":
		qb.setCategory(w, r)
	case "torrents/categories":
		writeJSON(w, qb.categories())
	case "torrents/createCategory":
		qb.createCategory(w, r)
	case "sync/maindata":
		qb.maindata(w)
	case "transfer/info":
		writeJSON(w, qb.transferInfo())
	}
}

// login answers "Ok." and sets the session cookie for a password that is one
// of this instance's API tokens, and "Fails." with a 200 for anything else, as
// qBittorrent does. The password is read from the body only, so it never
// travels in a URL a proxy writes to its log.
func (qb *qbitClient) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		qbitText(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, qbitMaxForm)
	if err := r.ParseForm(); err != nil {
		qbitText(w, http.StatusBadRequest, "Bad Request")
		return
	}
	tok, ok := qb.a.APITokens.Check(strings.TrimSpace(r.PostForm.Get("password")))
	if !ok {
		qbitText(w, http.StatusOK, "Fails.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     qbitCookie,
		Value:    qb.sessions.open(tok.ID),
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	qbitText(w, http.StatusOK, "Ok.")
}

// authorized returns the token behind a Bearer header or a live session. A
// Bearer header that does not check out refuses the call even when a session
// cookie came along.
func (qb *qbitClient) authorized(r *http.Request) (apitoken.Token, bool) {
	if secret := bearerToken(r); secret != "" {
		return qb.a.APITokens.Check(secret)
	}
	c, err := r.Cookie(qbitCookie)
	if err != nil {
		return apitoken.Token{}, false
	}
	return qb.sessions.token(c.Value)
}

// qbitRefuse answers a call the token has no right to. qBittorrent's own 403
// means "log in first", so Sonarr logs in once more and tries again; the body
// names the right for whoever reads the answer.
func qbitRefuse(w http.ResponseWriter, s apitoken.Scope) {
	qbitText(w, http.StatusForbidden, scopeRefusal(s))
}

func qbitText(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

// qbitBool reads a boolean parameter the way qBittorrent's parseBool does.
func qbitBool(s string) bool {
	s = strings.TrimSpace(s)
	return strings.EqualFold(s, "true") || s == "1"
}

// preferences carries what Sonarr and Radarr read: the download folder, whether
// a torrent is removed at its seeding limit (never here, since they refuse a
// client that does), queueing for their priority setting, and DHT for a magnet
// without trackers.
func (qb *qbitClient) preferences() map[string]any {
	s := qb.a.Settings.Get()
	maxRatio, maxMinutes := -1.0, -1
	if s.Torrent.SeedRatioTarget > 0 {
		maxRatio = s.Torrent.SeedRatioTarget
	}
	if s.Torrent.SeedDurationSeconds > 0 {
		maxMinutes = s.Torrent.SeedDurationSeconds / 60
	}
	return map[string]any{
		"save_path":                         qb.a.TaskFolder(""),
		"temp_path_enabled":                 false,
		"auto_tmm_enabled":                  false,
		"queueing_enabled":                  true,
		"max_active_downloads":              s.MaxConcurrent,
		"max_ratio_enabled":                 maxRatio >= 0,
		"max_ratio":                         maxRatio,
		"max_seeding_time_enabled":          maxMinutes >= 0,
		"max_seeding_time":                  maxMinutes,
		"max_inactive_seeding_time_enabled": false,
		"max_inactive_seeding_time":         -1,
		// Stop seeding, which is what the engine does at its targets.
		"max_ratio_act": 0,
		// The engine's DHT and PEX are always on; see settings.Torrent.
		"dht":                    true,
		"pex":                    true,
		"listen_port":            s.Torrent.Port,
		"web_ui_session_timeout": int(qbitSessionTTL / time.Second),
	}
}

// qbitItem is one thing torrents/add was handed, reduced to what the intake
// takes.
type qbitItem struct {
	// uri is a magnet or a .torrent as a data: URI.
	uri string
	// hash is the info hash, empty for a magnet the parser refused.
	hash string
	// name is the package the torrent lands in, empty to let the intake name it.
	name string
}

// add stages what torrents/add was handed. A save path is where files land on
// the host, which is configuration, so only a token with admin may send one,
// as for POST /api/links.
func (qb *qbitClient) add(w http.ResponseWriter, r *http.Request, admin bool) {
	r.Body = http.MaxBytesReader(w, r.Body, qbitMaxAdd)
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		err = r.ParseMultipartForm(qbitMaxAdd)
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			qbitText(w, http.StatusRequestEntityTooLarge, "Request Entity Too Large")
			return
		}
		qbitText(w, http.StatusBadRequest, "Bad Request")
		return
	}
	dir := strings.TrimSpace(r.FormValue("savepath"))
	if dir != "" && !admin {
		qbitRefuse(w, apitoken.ScopeAdmin)
		return
	}

	// Every file is read before anything is staged, so one bad file refuses the
	// request without leaving the files before it behind.
	var items []qbitItem
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["torrents"] {
			item, err := torrentFileItem(fh)
			if err != nil {
				qbitText(w, http.StatusUnsupportedMediaType, fmt.Sprintf("Error: '%s' is not a valid torrent file.", fh.Filename))
				return
			}
			items = append(items, item)
		}
	}
	for _, line := range strings.Split(r.FormValue("urls"), "\n") {
		u := strings.TrimSpace(line)
		if u == "" {
			continue
		}
		// A link that leads to no torrent is left out, as qBittorrent leaves
		// out one it cannot load.
		if item, ok := qb.linkItem(r.Context(), u); ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		qbitText(w, http.StatusOK, "Fails.")
		return
	}

	category := strings.TrimSpace(r.FormValue("category"))
	stopped := qbitBool(r.FormValue("paused")) || qbitBool(r.FormValue("stopped"))
	opts := app.LinkBatchOptions{Dir: dir, KeepCollected: stopped}
	// qBittorrent creates a category it is handed. When this instance cannot,
	// the torrent still arrives and keeps the name for Sonarr's filter.
	if category != "" {
		if id, err := qb.ensureCategory(category); err == nil {
			opts.Category = id
		}
	}
	rename := strings.TrimSpace(r.FormValue("rename"))
	ratioLimit := qbitRatioLimit(r.FormValue("ratioLimit"))
	seedingTimeLimit := qbitSeedingTimeLimit(r.FormValue("seedingTimeLimit"))

	added := 0
	for _, item := range items {
		pkg := item.name
		if rename != "" && len(items) == 1 {
			pkg = rename
		}
		// Filed as a paste, as the SABnzbd bridge files its grabs.
		created, err := qb.a.AddLinksWithOptions([]string{item.uri}, pkg, app.OriginPaste, opts)
		if err != nil {
			// Only the save path can be refused, which happens on the first
			// item, before anything is staged.
			qbitText(w, http.StatusBadRequest, err.Error())
			return
		}
		// Nothing is created for a torrent the list already has. qBittorrent
		// refuses such a torrent and leaves it as it was; taking it over would
		// hand Sonarr one of the owner's downloads to move and delete.
		if len(created) == 0 {
			continue
		}
		ids := make([]string, 0, len(created))
		for _, t := range created {
			ids = append(ids, t.ID)
		}
		if !stopped {
			// StartTasks, not StartTasksByHand: a queue halted by hand stays
			// halted when Sonarr finds an episode.
			qb.a.StartTasks(ids)
		}
		hash := item.hash
		if hash == "" {
			hash = syntheticHash(ids[0])
		}
		t := qbitTorrent{
			Hash: hash, Category: category, TaskIDs: ids, AddedAt: time.Now(),
			RatioLimit: ratioLimit, SeedingTimeLimit: seedingTimeLimit,
		}
		if err := qb.record(t); err != nil {
			qbitText(w, http.StatusInternalServerError, "the torrent was staged but this instance could not record it")
			return
		}
		added++
	}
	if added == 0 {
		qbitText(w, http.StatusOK, "Fails.")
		return
	}
	qbitText(w, http.StatusOK, "Ok.")
}

// torrentFileItem reads one uploaded .torrent, checked by the parser the
// upload route uses.
func torrentFileItem(fh *multipart.FileHeader) (qbitItem, error) {
	f, err := fh.Open()
	if err != nil {
		return qbitItem{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, torrent.MaxTorrentBytes+1))
	if err != nil {
		return qbitItem{}, err
	}
	md, uri, err := torrent.ParseUpload(b)
	if err != nil {
		return qbitItem{}, err
	}
	return qbitItem{uri: uri, hash: strings.ToLower(md.InfoHash), name: md.Name}, nil
}

// linkItem turns a line of urls into an item: a magnet as it is, and a web
// link by the .torrent it serves or the magnet it redirects to. It reports
// false for anything else, such as an indexer's error page, which would
// otherwise be downloaded as a file.
func (qb *qbitClient) linkItem(ctx context.Context, u string) (qbitItem, bool) {
	if torrent.IsMagnet(u) {
		return magnetItem(u), true
	}
	lower := strings.ToLower(u)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return qbitItem{}, false
	}
	magnet, body := qb.fetch(ctx, u)
	if magnet != "" {
		return magnetItem(magnet), true
	}
	md, uri, err := torrent.ParseUpload(body)
	if err != nil {
		return qbitItem{}, false
	}
	return qbitItem{uri: uri, hash: strings.ToLower(md.InfoHash), name: md.Name}, true
}

func magnetItem(u string) qbitItem {
	item := qbitItem{uri: u}
	// A magnet the parser refuses is still staged, where it fails with the
	// reason on the row.
	if md, err := (torrent.Resolver{}).Describe(u); err == nil {
		item.hash, item.name = strings.ToLower(md.InfoHash), md.Name
	}
	return item
}

// fetch reads a link as far as a .torrent can reach, or returns the magnet it
// redirects to. A failure returns nothing.
func (qb *qbitClient) fetch(ctx context.Context, u string) (magnet string, body []byte) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", nil
	}
	resp, err := qb.fetcher.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); torrent.IsMagnet(loc) {
		return loc, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, torrent.MaxTorrentBytes+1))
	if err != nil || len(body) > torrent.MaxTorrentBytes {
		return "", nil
	}
	return "", body
}

func newQbitFetcher() *http.Client {
	c := httpx.New(httpx.Options{Timeout: qbitFetchTimeout})
	follow := c.CheckRedirect
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// net/http cannot follow a redirect to a magnet, and the magnet is
		// what the caller wants.
		if torrent.IsMagnet(req.URL.String()) {
			return http.ErrUseLastResponse
		}
		return follow(req, via)
	}
	return c
}

// syntheticHash stands in for the info hash of a magnet the parser refused,
// which is staged to fail with the reason on its row. It is stable for the
// task and shaped like a hash, so a client that checks the field accepts it.
func syntheticHash(taskID string) string {
	sum := sha1.Sum([]byte(taskID))
	return hex.EncodeToString(sum[:])
}

// pick returns the tasks behind the torrents the hashes parameter names. With
// an edit it runs it on each of them under the document lock and writes the
// document back. Unknown hashes are skipped, as in qBittorrent. It answers the
// request itself when it cannot.
func (qb *qbitClient) pick(w http.ResponseWriter, r *http.Request, edit func(torrents map[string]qbitTorrent, hash string)) ([]string, bool) {
	if !r.Form.Has("hashes") {
		qbitText(w, http.StatusBadRequest, "Bad Request")
		return nil, false
	}
	all, wanted := qbitHashes(r.Form.Get("hashes"))
	qb.mu.Lock()
	defer qb.mu.Unlock()
	torrents, err := qb.load()
	if err != nil {
		qbitText(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	var ids []string
	for hash, t := range torrents {
		if !all && !wanted[hash] {
			continue
		}
		ids = append(ids, t.TaskIDs...)
		if edit != nil {
			edit(torrents, hash)
		}
	}
	if edit != nil {
		if err := qb.store(torrents); err != nil {
			qbitText(w, http.StatusInternalServerError, err.Error())
			return nil, false
		}
	}
	return ids, true
}

// holdsPaused reports whether one of ids is a paused download, which only a
// token with control may resume.
func (qb *qbitClient) holdsPaused(ids []string) bool {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	return slices.ContainsFunc(qb.a.Tasks(), func(t *core.Task) bool {
		return want[t.ID] && t.Status == core.StatusPaused
	})
}

// qbitHashes reads a hashes parameter: "all", or hashes separated by "|".
func qbitHashes(raw string) (all bool, wanted map[string]bool) {
	wanted = map[string]bool{}
	for _, h := range strings.Split(raw, "|") {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "all" {
			return true, nil
		}
		if h != "" {
			wanted[h] = true
		}
	}
	return false, wanted
}

// remove deletes torrents and, with removeTasks, their tasks through
// RemoveTasks, like every other delete. deleteFiles is honoured exactly as
// sent. Without removeTasks a torrent that is done or failed is only
// forgotten, and its downloads and files stay in the list. One still on its
// way is refused instead, since forgetting it would leave a download running
// that nobody tracks.
func (qb *qbitClient) remove(w http.ResponseWriter, r *http.Request, removeTasks bool) {
	if !r.Form.Has("deleteFiles") {
		qbitText(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if !removeTasks {
		all, wanted := qbitHashes(r.Form.Get("hashes"))
		for _, v := range qb.views() {
			if (all || wanted[v.torrent.Hash]) && !v.settled() {
				qbitRefuse(w, apitoken.ScopeControl)
				return
			}
		}
	}
	ids, ok := qb.pick(w, r, func(torrents map[string]qbitTorrent, hash string) { delete(torrents, hash) })
	if ok && removeTasks {
		// Outside the document lock, since deleting files can take a while.
		qb.a.RemoveTasks(ids, qbitBool(r.Form.Get("deleteFiles")))
	}
}

// setCategory refiles torrents, and their tasks under the matching category of
// this instance. An empty category takes them out of any.
func (qb *qbitClient) setCategory(w http.ResponseWriter, r *http.Request) {
	if !r.Form.Has("category") {
		qbitText(w, http.StatusBadRequest, "Bad Request")
		return
	}
	category := strings.TrimSpace(r.Form.Get("category"))
	id := ""
	if category != "" {
		if id = qbitCategoryID(qb.a.Settings.Get(), category); id == "" {
			qbitText(w, http.StatusConflict, "Incorrect category name")
			return
		}
	}
	ids, ok := qb.pick(w, r, func(torrents map[string]qbitTorrent, hash string) {
		t := torrents[hash]
		t.Category = category
		torrents[hash] = t
	})
	if ok {
		qb.a.SetCategory(ids, id)
	}
}

// setShareLimits records the limits a client sets on torrents, which
// torrents/info then reports back.
func (qb *qbitClient) setShareLimits(w http.ResponseWriter, r *http.Request) {
	if !r.Form.Has("ratioLimit") || !r.Form.Has("seedingTimeLimit") {
		qbitText(w, http.StatusBadRequest, "Bad Request")
		return
	}
	ratio := qbitRatioLimit(r.Form.Get("ratioLimit"))
	minutes := qbitSeedingTimeLimit(r.Form.Get("seedingTimeLimit"))
	qb.pick(w, r, func(torrents map[string]qbitTorrent, hash string) {
		t := torrents[hash]
		t.RatioLimit, t.SeedingTimeLimit = ratio, minutes
		torrents[hash] = t
	})
}

func (qb *qbitClient) createCategory(w http.ResponseWriter, r *http.Request) {
	if !r.Form.Has("category") {
		qbitText(w, http.StatusBadRequest, "Bad Request")
		return
	}
	name := strings.TrimSpace(r.Form.Get("category"))
	if name == "" {
		qbitText(w, http.StatusBadRequest, "Category cannot be empty")
		return
	}
	if _, err := qb.ensureCategory(name); errors.Is(err, errBadCategory) {
		qbitText(w, http.StatusConflict, "Incorrect category name")
	} else if err != nil {
		qbitText(w, http.StatusConflict, "Unable to create category")
	}
}

// errBadCategory is a category name with nothing an id can be made of.
var errBadCategory = errors.New("a category name needs a letter or a digit")

// ensureCategory answers the id of the category a qBittorrent client names,
// making it one of this instance's own first when there is none, so Sonarr's
// category shows up in the list and can be given a folder. A save path sent
// with it is ignored; where files go is decided here.
func (qb *qbitClient) ensureCategory(name string) (string, error) {
	if settings.CategoryID(name) == "" {
		return "", errBadCategory
	}
	qb.mu.Lock()
	defer qb.mu.Unlock()
	s := qb.a.Settings.Get()
	if id := qbitCategoryID(s, name); id != "" {
		return id, nil
	}
	// Clipped, so the append cannot write into the array the stored settings
	// still share.
	s.Categories = append(slices.Clip(s.Categories), settings.Category{Name: name})
	saved, err := qb.a.ApplySettings(s)
	if err != nil {
		return "", err
	}
	return qbitCategoryID(saved, name), nil
}

// qbitCategoryKeys lists this instance's categories under their names and
// under their ids, so Sonarr finds its category whichever spelling it was set
// up with. A key two categories share belongs to the first.
func qbitCategoryKeys(categories []settings.Category) map[string]settings.Category {
	out := map[string]settings.Category{}
	for _, c := range categories {
		for _, key := range []string{c.Name, c.ID} {
			if _, seen := out[key]; key != "" && !seen {
				out[key] = c
			}
		}
	}
	return out
}

// qbitCategoryID is the id of the category a client names, found the way
// torrents/categories lists it, or "" when this instance has none by that
// name.
func qbitCategoryID(s settings.Settings, name string) string {
	if c, ok := qbitCategoryKeys(s.Categories)[name]; ok {
		return c.ID
	}
	return s.CategoryFor(name).ID
}

func (qb *qbitClient) categories() map[string]map[string]string {
	out := map[string]map[string]string{}
	for key, c := range qbitCategoryKeys(qb.a.Settings.Get().Categories) {
		// A folder with variables in it has no single answer for all tasks.
		dir := c.Dir
		if pathvars.HasVars(dir) {
			dir = ""
		}
		out[key] = map[string]string{"name": key, "savePath": dir}
	}
	return out
}

// qbitInfo is one entry of torrents/info. Only the fields a client acts on are
// filled in.
type qbitInfo struct {
	Hash                 string  `json:"hash"`
	InfoHashV1           string  `json:"infohash_v1"`
	Name                 string  `json:"name"`
	Size                 int64   `json:"size"`
	TotalSize            int64   `json:"total_size"`
	Progress             float64 `json:"progress"`
	DLSpeed              int64   `json:"dlspeed"`
	UPSpeed              int64   `json:"upspeed"`
	ETA                  int64   `json:"eta"`
	State                string  `json:"state"`
	Category             string  `json:"category"`
	Tags                 string  `json:"tags"`
	SavePath             string  `json:"save_path"`
	ContentPath          string  `json:"content_path"`
	Downloaded           int64   `json:"downloaded"`
	Uploaded             int64   `json:"uploaded"`
	Completed            int64   `json:"completed"`
	AmountLeft           int64   `json:"amount_left"`
	Ratio                float64 `json:"ratio"`
	RatioLimit           float64 `json:"ratio_limit"`
	SeedingTimeLimit     int64   `json:"seeding_time_limit"`
	InactiveSeedingLimit int64   `json:"inactive_seeding_time_limit"`
	SeedingTime          int64   `json:"seeding_time"`
	NumSeeds             int     `json:"num_seeds"`
	NumLeechs            int     `json:"num_leechs"`
	AddedOn              int64   `json:"added_on"`
	CompletionOn         int64   `json:"completion_on"`
	LastActivity         int64   `json:"last_activity"`
	ForceStart           bool    `json:"force_start"`
	AutoTMM              bool    `json:"auto_tmm"`
	MagnetURI            string  `json:"magnet_uri"`
}

func (qb *qbitClient) info(w http.ResponseWriter, r *http.Request) {
	filter := strings.ToLower(strings.TrimSpace(r.Form.Get("filter")))
	_, byCategory := r.Form["category"]
	category := r.Form.Get("category")
	all, wanted := qbitHashes(r.Form.Get("hashes"))
	if len(wanted) == 0 {
		all = true
	}
	out := []qbitInfo{}
	for _, v := range qb.views() {
		// An empty category asks for the torrents without one, as in
		// qBittorrent.
		if byCategory && v.torrent.Category != category {
			continue
		}
		if !all && !wanted[v.torrent.Hash] {
			continue
		}
		if !qbitFilterMatches(filter, v) {
			continue
		}
		out = append(out, v.info())
	}
	writeJSON(w, out)
}

// qbitFilterMatches is qBittorrent's TorrentFilter over the states this file
// reports. An unknown filter matches everything, as it does there.
func qbitFilterMatches(filter string, v qbitView) bool {
	switch filter {
	case "downloading":
		return strings.HasSuffix(v.state, "DL") || v.state == "downloading"
	case "seeding":
		return v.state == "uploading" || v.state == "stalledUP"
	case "completed":
		return v.complete()
	case "paused", "stopped":
		return v.state == "pausedDL" || v.state == "pausedUP"
	case "resumed", "running":
		return v.state != "pausedDL" && v.state != "pausedUP"
	case "active":
		return v.active()
	case "inactive":
		return !v.active()
	case "stalled":
		return v.state == "stalledDL" || v.state == "stalledUP"
	case "stalled_uploading":
		return v.state == "stalledUP"
	case "stalled_downloading":
		return v.state == "stalledDL"
	case "moving":
		return v.state == "moving"
	case "errored":
		return v.state == "error" || v.state == "missingFiles"
	}
	return true
}

func (qb *qbitClient) properties(w http.ResponseWriter, r *http.Request) {
	v, ok := qb.one(w, r)
	if !ok {
		return
	}
	i := v.info()
	writeJSON(w, map[string]any{
		"hash":             i.Hash,
		"infohash_v1":      i.InfoHashV1,
		"name":             i.Name,
		"save_path":        i.SavePath,
		"addition_date":    i.AddedOn,
		"completion_date":  i.CompletionOn,
		"eta":              i.ETA,
		"dl_speed":         i.DLSpeed,
		"up_speed":         i.UPSpeed,
		"seeds":            i.NumSeeds,
		"peers":            i.NumLeechs,
		"share_ratio":      i.Ratio,
		"seeding_time":     i.SeedingTime,
		"time_elapsed":     time.Now().Unix() - i.AddedOn,
		"total_downloaded": i.Downloaded,
		"total_uploaded":   i.Uploaded,
		"total_size":       i.TotalSize,
		"comment":          "",
	})
}

func (qb *qbitClient) files(w http.ResponseWriter, r *http.Request) {
	v, ok := qb.one(w, r)
	if !ok {
		return
	}
	writeJSON(w, v.files())
}

// one is the torrent the hash parameter names, answering 400 or 404 itself
// when there is none.
func (qb *qbitClient) one(w http.ResponseWriter, r *http.Request) (qbitView, bool) {
	hash := strings.ToLower(strings.TrimSpace(r.Form.Get("hash")))
	if hash == "" {
		qbitText(w, http.StatusBadRequest, "Bad Request")
		return qbitView{}, false
	}
	for _, v := range qb.views() {
		if v.torrent.Hash == hash {
			return v, true
		}
	}
	qbitText(w, http.StatusNotFound, "Not Found")
	return qbitView{}, false
}

// maindata answers every sync/maindata with a full update, which a client has
// to be able to take at any time.
func (qb *qbitClient) maindata(w http.ResponseWriter) {
	torrents := map[string]qbitInfo{}
	for _, v := range qb.views() {
		torrents[v.torrent.Hash] = v.info()
	}
	writeJSON(w, map[string]any{
		"rid":          qb.rid.Add(1),
		"full_update":  true,
		"torrents":     torrents,
		"categories":   qb.categories(),
		"tags":         []string{},
		"server_state": qb.transferInfo(),
	})
}

// transferInfo covers the whole instance, as qBittorrent's does. Upload is
// not measured across torrents, so it is left at zero, and so is the upload
// limit, which the engine does not enforce.
func (qb *qbitClient) transferInfo() map[string]any {
	return map[string]any{
		"dl_info_speed":     qb.a.Counters().Speed,
		"up_info_speed":     0,
		"dl_rate_limit":     qb.a.Settings.Get().SpeedLimit,
		"up_rate_limit":     0,
		"dht_nodes":         0,
		"connection_status": "connected",
	}
}

// qbitView is one torrent rendered against the live task list.
type qbitView struct {
	torrent qbitTorrent
	tasks   []*core.Task
	name    string
	state   string
	size    int64
	loaded  int64
	speed   int64
	sent    int64
	seeds   int
	peers   int
	forced  bool
	// folder says content_path is a folder holding the tasks' files rather
	// than a file itself.
	folder      bool
	savePath    string
	contentPath string
	finishedAt  time.Time
	changedAt   time.Time
}

// views renders every torrent this bridge staged, oldest first. It prunes
// torrents whose tasks are all gone; every listing goes through here, so the
// prune needs no timer.
func (qb *qbitClient) views() []qbitView {
	qb.mu.Lock()
	// Read under the lock, so a torrent recorded after the read cannot be
	// pruned as one whose tasks are gone.
	live := map[string]*core.Task{}
	for _, t := range qb.a.Tasks() {
		live[t.ID] = t
	}
	torrents, err := qb.load()
	if err != nil {
		qb.mu.Unlock()
		return nil
	}
	kept := make([]qbitTorrent, 0, len(torrents))
	changed := false
	for hash, t := range torrents {
		if !slices.ContainsFunc(t.TaskIDs, func(id string) bool { return live[id] != nil }) {
			delete(torrents, hash)
			changed = true
			continue
		}
		kept = append(kept, t)
	}
	if changed {
		// A failed write is retried by the next call's prune.
		_ = qb.store(torrents)
	}
	qb.mu.Unlock()

	sort.Slice(kept, func(i, j int) bool {
		if !kept[i].AddedAt.Equal(kept[j].AddedAt) {
			return kept[i].AddedAt.Before(kept[j].AddedAt)
		}
		return kept[i].Hash < kept[j].Hash
	})
	// Rendered outside the lock, since it asks the app for folders and the
	// disk whether the content is still there.
	perPackage := qb.a.Settings.Get().SubfolderByPackage
	out := make([]qbitView, 0, len(kept))
	for _, t := range kept {
		out = append(out, qb.view(t, live, perPackage))
	}
	return out
}

func (qb *qbitClient) view(t qbitTorrent, live map[string]*core.Task, perPackage bool) qbitView {
	v := qbitView{torrent: t, state: "pausedUP"}
	for _, id := range t.TaskIDs {
		task := live[id]
		if task == nil {
			continue
		}
		v.tasks = append(v.tasks, task)
		v.size += task.Size
		v.loaded += task.Loaded
		v.speed += task.Speed
		v.sent += task.Uploaded
		v.seeds += task.Seeds
		v.peers += task.Peers
		v.forced = v.forced || task.Forced
		if task.FinishedAt.After(v.finishedAt) {
			v.finishedAt = task.FinishedAt
		}
		if task.ChangedAt.After(v.changedAt) {
			v.changedAt = task.ChangedAt
		}
		if s := qbitState(task); qbitStateRank[s] < qbitStateRank[v.state] {
			v.state = s
		}
	}
	v.name = qbitName(v.tasks, t.Hash)
	v.savePath, v.contentPath, v.folder = qb.paths(v.tasks, perPackage)
	if v.complete() {
		if _, err := os.Stat(v.contentPath); errors.Is(err, fs.ErrNotExist) {
			v.state = "missingFiles"
		}
	}
	return v
}

// qbitState maps one task onto the qBittorrent state that makes Sonarr do the
// right thing with it: import only once every byte is on disk and unpacked,
// report a download that will never start on its own as an error, and treat a
// paused download as paused rather than finished.
//
// A task left in the collector is stopped, like a torrent added paused, unless
// the auto-confirm countdown is about to send it on.
func qbitState(t *core.Task) string {
	switch {
	case t.Skipped, t.Status == core.StatusError:
		return "error"
	case t.Status == core.StatusDone && t.Seeding && t.Peers > 0:
		return "uploading"
	case t.Status == core.StatusDone && t.Seeding:
		return "stalledUP"
	case t.Status == core.StatusDone:
		return "pausedUP"
	case t.Online == core.AvailOffline, t.Status == core.StatusCollected && t.Error != "":
		return "error"
	case t.Status == core.StatusExtracting:
		// Not finished for Sonarr, which would otherwise import the archive.
		return "moving"
	case t.Status == core.StatusRunning && !t.StalledSince.IsZero():
		return "stalledDL"
	case t.Status == core.StatusRunning && torrent.IsMagnet(t.URL) && t.Size == 0:
		return "metaDL"
	case t.Status == core.StatusRunning:
		return "downloading"
	case t.Status == core.StatusQueued && t.Enabled && !t.Hold:
		return "queuedDL"
	case t.Status == core.StatusCollected && !t.ConfirmDue.IsZero():
		return "queuedDL"
	}
	return "pausedDL"
}

// qbitName is the name Sonarr shows: a lone task's own name, or the package
// several tasks share.
func qbitName(tasks []*core.Task, hash string) string {
	if len(tasks) == 1 && tasks[0].Name != tasks[0].URL && tasks[0].Name != "" {
		return tasks[0].Name
	}
	if len(tasks) > 0 && tasks[0].Package != "" {
		return tasks[0].Package
	}
	return hash
}

// paths answers save_path and content_path, and whether the content is a
// folder. Sonarr imports from content_path and refuses one equal to
// save_path, so the content is the package's own folder when every package
// has one, and otherwise the file itself. Several files loose in the shared
// folder are the limitation the module row warns about, and Sonarr says so
// too.
func (qb *qbitClient) paths(tasks []*core.Task, perPackage bool) (save, content string, folder bool) {
	dir := qb.a.TaskFolder(tasks[0].ID)
	if perPackage {
		return filepath.Dir(dir), dir, true
	}
	if len(tasks) == 1 {
		if t := tasks[0]; t.File != "" {
			return dir, t.File, false
		} else if name := qbitFileName(t); name != "" {
			return dir, filepath.Join(dir, name), false
		}
	}
	return dir, dir, true
}

// qbitFileName is the name a task's bytes are under on disk, or "" while that
// is not known yet.
func qbitFileName(t *core.Task) string {
	switch {
	case t.File != "":
		return filepath.Base(t.File)
	case t.Filename != "":
		return t.Filename
	case t.Name != t.URL:
		return t.Name
	}
	return ""
}

func (v qbitView) complete() bool {
	return v.state == "uploading" || v.state == "stalledUP" || v.state == "pausedUP"
}

// settled reports whether Sonarr is done waiting for the torrent: it is
// complete, or failed in a way Sonarr hands to its failed-download handling.
func (v qbitView) settled() bool {
	return v.complete() || v.state == "error" || v.state == "missingFiles"
}

func (v qbitView) active() bool {
	switch v.state {
	case "downloading", "metaDL", "uploading", "moving":
		return true
	}
	return false
}

func (v qbitView) progress() float64 {
	if v.complete() {
		return 1
	}
	if v.size <= 0 {
		return 0
	}
	return min(float64(v.loaded)/float64(v.size), 1)
}

func (v qbitView) info() qbitInfo {
	i := qbitInfo{
		Hash:         v.torrent.Hash,
		Name:         v.name,
		Size:         v.size,
		TotalSize:    v.size,
		Progress:     v.progress(),
		DLSpeed:      v.speed,
		ETA:          qbitNoETA,
		State:        v.state,
		Category:     v.torrent.Category,
		SavePath:     v.savePath,
		ContentPath:  v.contentPath,
		Downloaded:   v.loaded,
		Uploaded:     v.sent,
		Completed:    v.loaded,
		AmountLeft:   max(v.size-v.loaded, 0),
		NumSeeds:     v.seeds,
		NumLeechs:    v.peers,
		AddedOn:      v.torrent.AddedAt.Unix(),
		CompletionOn: -1,
		ForceStart:   v.forced,
		// -2 is "the global limit", which preferences reports.
		RatioLimit:           -2,
		SeedingTimeLimit:     -2,
		InactiveSeedingLimit: -2,
	}
	if l := v.torrent.RatioLimit; l != nil {
		i.RatioLimit = *l
	}
	if l := v.torrent.SeedingTimeLimit; l != nil {
		i.SeedingTimeLimit = *l
	}
	if len(v.tasks) == 1 && v.tasks[0].InfoHash != "" {
		i.InfoHashV1 = v.tasks[0].InfoHash
		if torrent.IsMagnet(v.tasks[0].URL) {
			i.MagnetURI = v.tasks[0].URL
		}
	}
	if !v.changedAt.IsZero() {
		i.LastActivity = v.changedAt.Unix()
	}
	if v.loaded > 0 {
		i.Ratio = float64(v.sent) / float64(v.loaded)
	}
	if v.speed > 0 && v.size > v.loaded {
		i.ETA = (v.size - v.loaded) / v.speed
	}
	if v.complete() {
		i.Completed, i.AmountLeft = v.size, 0
		if !v.finishedAt.IsZero() {
			i.CompletionOn = v.finishedAt.Unix()
		}
	}
	switch v.state {
	case "uploading", "stalledUP":
		if !v.finishedAt.IsZero() {
			i.SeedingTime = int64(time.Since(v.finishedAt).Seconds())
		}
	case "pausedUP":
		// Nothing seeds it any more, so a limit left to the instance reads as
		// reached, which is when Sonarr removes a finished torrent. A limit of
		// its own is reported as it is, since the engine seeds to the
		// instance's targets and can stop short of it.
		if v.torrent.RatioLimit == nil && v.torrent.SeedingTimeLimit == nil {
			i.RatioLimit = 0
		}
	}
	return i
}

// qbitFile is one entry of torrents/files. Names are relative to save_path and
// use "/", as qBittorrent's do on every platform.
type qbitFile struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
	IsSeed   bool    `json:"is_seed"`
}

func (v qbitView) files() []qbitFile {
	prefix := ""
	if v.folder && v.contentPath != v.savePath {
		prefix = filepath.Base(v.contentPath)
	}
	out := []qbitFile{}
	add := func(name string, size int64, progress float64, selected bool) {
		priority := 0
		if selected {
			priority = 1
		}
		out = append(out, qbitFile{
			Index: len(out), Name: path.Join(prefix, name), Size: size,
			Progress: progress, Priority: priority, IsSeed: v.complete(),
		})
	}
	for _, t := range v.tasks {
		progress := 0.0
		switch {
		case t.Status == core.StatusDone:
			progress = 1
		case t.Size > 0:
			progress = min(float64(t.Loaded)/float64(t.Size), 1)
		}
		switch {
		case len(t.TorrentFiles) == 1 && t.TorrentFiles[0].Path == t.Name:
			// A single-file torrent's file is its name.
			f := t.TorrentFiles[0]
			add(f.Path, f.Size, progress, f.Selected)
		case len(t.TorrentFiles) > 0:
			// A multi-file torrent lands in a folder named after it.
			for _, f := range t.TorrentFiles {
				add(path.Join(t.Name, f.Path), f.Size, progress, f.Selected)
			}
		default:
			name := qbitFileName(t)
			if name == "" {
				name = v.name
			}
			add(name, t.Size, progress, true)
		}
	}
	return out
}

// record adds one torrent to the document, merged into an entry with the same
// hash when the torrent was handed over before.
func (qb *qbitClient) record(t qbitTorrent) error {
	qb.mu.Lock()
	defer qb.mu.Unlock()
	torrents, err := qb.load()
	if err != nil {
		return err
	}
	if old, ok := torrents[t.Hash]; ok {
		for _, id := range old.TaskIDs {
			if !slices.Contains(t.TaskIDs, id) {
				t.TaskIDs = append(t.TaskIDs, id)
			}
		}
		t.AddedAt = old.AddedAt
		if t.Category == "" {
			t.Category = old.Category
		}
	}
	torrents[t.Hash] = t
	return qb.store(torrents)
}

// load reads the torrent document. Callers hold mu. An unreadable document
// starts over rather than making the bridge unusable.
func (qb *qbitClient) load() (map[string]qbitTorrent, error) {
	value, err := qb.a.UIState(qbitBucket)
	if err != nil {
		return nil, err
	}
	torrents := map[string]qbitTorrent{}
	if strings.TrimSpace(value) == "" {
		return torrents, nil
	}
	if json.Unmarshal([]byte(value), &torrents) != nil {
		return map[string]qbitTorrent{}, nil
	}
	return torrents, nil
}

// store writes the torrent document back. Callers hold mu.
func (qb *qbitClient) store(torrents map[string]qbitTorrent) error {
	b, err := json.Marshal(torrents)
	if err != nil {
		return err
	}
	return qb.a.SetUIState(qbitBucket, string(b))
}

// qbitSessions are the logins handed out. A session is only as good as the
// token it was opened with: lookup finds that token again on every call, so
// revoking the token ends the session with it, and the session can do what
// the token can.
type qbitSessions struct {
	now    func() time.Time
	lookup func(tokenID string) (apitoken.Token, bool)

	mu   sync.Mutex
	byID map[string]qbitSession
}

type qbitSession struct {
	tokenID string
	expires time.Time
}

func newQbitSessions(lookup func(string) (apitoken.Token, bool)) *qbitSessions {
	return &qbitSessions{now: time.Now, lookup: lookup, byID: map[string]qbitSession{}}
}

// open starts a session for a token and returns its id. At the cap the session
// closest to expiring makes room.
func (s *qbitSessions) open(tokenID string) string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	sid := base64.RawURLEncoding.EncodeToString(b[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for id, sess := range s.byID {
		if !now.Before(sess.expires) {
			delete(s.byID, id)
		}
	}
	if len(s.byID) >= qbitMaxSessions {
		oldest := ""
		for id, sess := range s.byID {
			if oldest == "" || sess.expires.Before(s.byID[oldest].expires) {
				oldest = id
			}
		}
		delete(s.byID, oldest)
	}
	s.byID[sid] = qbitSession{tokenID: tokenID, expires: now.Add(qbitSessionTTL)}
	return sid
}

// token is the token a live session was opened with.
func (s *qbitSessions) token(sid string) (apitoken.Token, bool) {
	s.mu.Lock()
	sess, ok := s.byID[sid]
	if ok && !s.now().Before(sess.expires) {
		delete(s.byID, sid)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return apitoken.Token{}, false
	}
	tok, ok := s.lookup(sess.tokenID)
	if !ok {
		s.end(sid)
	}
	return tok, ok
}

func (s *qbitSessions) end(sid string) {
	s.mu.Lock()
	delete(s.byID, sid)
	s.mu.Unlock()
}
