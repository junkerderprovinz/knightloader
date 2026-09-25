package api

// A download client Sonarr and Radarr can be pointed at, speaking SABnzbd's
// API (Sonarr's SabnzbdProxy.cs, which Radarr copies). SABnzbd needs one route
// with a mode parameter and authenticates with an "apikey" query parameter,
// which maps onto internal/apitoken. qBittorrent, Transmission and Deluge each
// need a session login and many more endpoints, and Blackhole reports no
// state at all.
//
// Sonarr and Radarr only call addfile, uploading an .nzb they fetched
// themselves. Without a Usenet backend the bytes are scanned for links
// instead, which suits a DDL indexer whose "nzb" is really a link list or a
// container; a payload without links is refused rather than faked. addurl is
// served too, for scripts.
//
// The path is /api/sabnzbd/api because /api belongs to this app; setting the
// client's URL Base to "api/sabnzbd" lands here.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/linkscan"
)

const sabnzbdPath = "/api/sabnzbd/api"

// sabnzbdVersion is what mode=version answers: the SABnzbd API release this
// file speaks, not KnightLoader's version. Sonarr gates behaviour on it and
// fails the connection test on anything that is not major.minor.patch.
const sabnzbdVersion = "4.3.3"

// maxUploadBytes bounds an addfile body before it is held in memory. A link
// list is kilobytes; the margin is for a real .nzb sent by mistake.
const maxUploadBytes = 8 << 20

// downloadClientBucket is the interface-state bucket the bridge keeps its
// grabs in. Which tasks form a grab, and the category it arrived under, cannot
// be derived from the tasks, and the category has to round-trip because
// Sonarr drops every item whose category is not its own.
const downloadClientBucket = "downloadclient"

// sabGrab is one thing Sonarr handed over: the release, the category it came
// under, and the tasks it became.
type sabGrab struct {
	// ID is the nzo_id Sonarr passes back for delete. It is generated rather
	// than the package name, which a Packagizer rule may change.
	ID string `json:"id"`
	// Name is the release name, which Sonarr matches its history against.
	Name     string    `json:"name"`
	Category string    `json:"category"`
	TaskIDs  []string  `json:"taskIds"`
	AddedAt  time.Time `json:"addedAt"`
}

// downloadClient holds what the routes share. It is not package state, so two
// apps in one test process never share a lock or a store.
type downloadClient struct {
	a *app.App
	// mu serialises the read-modify-write of the grab document, so two
	// concurrent addfile calls cannot drop each other's grab.
	mu sync.Mutex
}

func registerDownloadClient(reg *Registry, a *app.App) {
	dc := &downloadClient{a: a}

	// Open because Sonarr sends its key as ?apikey=, which the session guard
	// does not know. serve checks it against the API tokens on every request,
	// even without a password, and answers 404 while the module is off.
	reg.AddOpen(http.MethodGet, sabnzbdPath,
		"SABnzbd-shaped download client for Sonarr and Radarr (set their URL Base to \"api/sabnzbd\"); "+
			"off unless \"Download client for Sonarr and Radarr\" is switched on (Remote access page or Modules page), and the ?apikey= is an API token of this instance that can add and read",
		dc.serve)
	reg.AddOpen(http.MethodPost, sabnzbdPath,
		"the same door for mode=addfile, which is the only call Sonarr and Radarr make as a POST",
		dc.serve)
}

// serve is the whole protocol: one address, one mode parameter.
func (dc *downloadClient) serve(w http.ResponseWriter, r *http.Request) {
	// The switch comes before the credential, so a closed door does not reveal
	// whether a key would have worked. The wording matches the /api/ catch-all.
	if !dc.a.Settings.Get().DownloadClientAPI {
		http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		return
	}
	tok, ok := dc.authorized(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	mode := strings.ToLower(strings.TrimSpace(q.Get("mode")))
	op := mode
	// SABnzbd overloads mode=queue and mode=history: with name=delete they
	// remove.
	if (mode == "queue" || mode == "history") && strings.EqualFold(q.Get("name"), "delete") {
		op = "delete"
	}
	need, known := sabnzbdScopes[op]
	if !known {
		// Sonarr never calls retry, and only asks for fullstatus when
		// complete_dir is relative, which serveConfig never answers.
		sabError(w, fmt.Sprintf("mode %q is not implemented by this download client", mode))
		return
	}
	// Sonarr and Radarr clear what they have imported through delete, and
	// their token can add and read but not control. Such a token may still
	// forget a finished grab it handed over; serveDelete leaves the downloads
	// alone.
	if !tok.Has(need) && !(op == "delete" && tok.Has(apitoken.ScopeAdd)) {
		sabError(w, scopeRefusal(need))
		return
	}

	switch op {
	case "version":
		writeJSON(w, map[string]string{"version": sabnzbdVersion})
	case "get_config":
		dc.serveConfig(w)
	case "delete":
		dc.serveDelete(w, r, tok.Has(apitoken.ScopeControl))
	case "queue":
		dc.serveQueue(w, r)
	case "history":
		dc.serveHistory(w, r)
	case "addfile", "addurl":
		dc.serveAdd(w, r, mode)
	}
}

// authorized checks the credential and returns the token when the caller may
// proceed.
//
// Sonarr's TestAuthentication matches on "API Key Incorrect" and "API Key
// Required". The refusal is an HTTP 200 with an error document, as in real
// SABnzbd, because Sonarr raises a 401 as "unable to connect" before reading
// the document.
func (dc *downloadClient) authorized(w http.ResponseWriter, r *http.Request) (apitoken.Token, bool) {
	key := strings.TrimSpace(r.URL.Query().Get("apikey"))
	if key == "" {
		// Sonarr never sends a Bearer header, but curl users do.
		key = bearerToken(r)
	}
	if key == "" {
		sabError(w, "API Key Required")
		return apitoken.Token{}, false
	}
	// The instance's own tokens, so there is only one place to revoke from.
	tok, ok := dc.a.APITokens.Check(key)
	if !ok {
		sabError(w, "API Key Incorrect")
		return apitoken.Token{}, false
	}
	return tok, true
}

// sabError is SABnzbd's failure document; Sonarr shows the sentence to the
// user.
func sabError(w http.ResponseWriter, msg string) {
	writeJSON(w, map[string]any{"status": false, "error": msg})
}

// serveConfig answers mode=get_config, which Sonarr's connection test and
// category validation read. Each field is required by something specific:
//
//   - complete_dir has to be absolute, or Sonarr asks for mode=fullstatus.
//   - categories must contain the one configured in Sonarr, and no dir may end
//     in "*", which Sonarr reads as "no job folders".
//   - sorters must be a list; Sonarr does not check it for null.
//   - the sorting switches are false, since this app does not rename files.
//   - history_retention_option "all" tells Sonarr to remove finished items
//     itself after import.
func (dc *downloadClient) serveConfig(w http.ResponseWriter) {
	// The download folder. A pathvars template is reported verbatim: it is
	// still absolute, and trimming it would need a second copy of
	// settings.fixedPrefix.
	complete := dc.a.TaskFolder("")
	writeJSON(w, map[string]any{
		"config": map[string]any{
			"misc": map[string]any{
				"complete_dir":             complete,
				"pre_check":                false,
				"enable_tv_sorting":        false,
				"enable_movie_sorting":     false,
				"enable_date_sorting":      false,
				"tv_categories":            []string{},
				"movie_categories":         []string{},
				"date_categories":          []string{},
				"history_retention":        "0",
				"history_retention_option": "all",
				"history_retention_number": 0,
			},
			"categories": sabCategories(),
			"sorters":    []any{},
		},
	})
}

// sabCategories is the list Sonarr validates its configured category against:
// SABnzbd's catch-all plus Sonarr's and Radarr's defaults. serveAdd accepts
// any category, so the list only has to satisfy the connection test on a
// fresh install.
func sabCategories() []map[string]string {
	return []map[string]string{
		{"name": "*", "dir": ""},
		{"name": "tv-sonarr", "dir": "tv-sonarr"},
		{"name": "radarr", "dir": "radarr"},
	}
}

// serveAdd is the intake: addfile, which Sonarr and Radarr send, and addurl,
// which a script can send.
func (dc *downloadClient) serveAdd(w http.ResponseWriter, r *http.Request, mode string) {
	category := strings.TrimSpace(r.URL.Query().Get("cat"))
	name := strings.TrimSpace(r.URL.Query().Get("nzbname"))

	var blob string
	if mode == "addurl" {
		// SABnzbd's addurl carries the URL in "name".
		blob = strings.TrimSpace(r.URL.Query().Get("name"))
		if blob == "" {
			sabError(w, "addurl needs the link in the name parameter")
			return
		}
	} else {
		// Capped at the reader, before the body is in memory.
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)
		file, header, err := r.FormFile("name")
		if err != nil {
			// SABnzbd also accepts the field as "nzbfile".
			file, header, err = r.FormFile("nzbfile")
			if err != nil {
				// A payload over the cap and a wrong field name look alike
				// here but need different fixes.
				var tooBig *http.MaxBytesError
				if errors.As(err, &tooBig) {
					sabError(w, fmt.Sprintf("a payload over %d bytes is refused", maxUploadBytes))
					return
				}
				sabError(w, "send the payload as a multipart form field named \"name\"")
				return
			}
		}
		defer file.Close()
		// One byte over the cap, so a payload that is too large is refused
		// rather than truncated.
		data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
		if err != nil {
			sabError(w, "could not read the uploaded payload")
			return
		}
		if len(data) > maxUploadBytes {
			sabError(w, fmt.Sprintf("a payload over %d bytes is refused", maxUploadBytes))
			return
		}
		blob = string(data)
		if name == "" && header != nil {
			name = releaseName(header.Filename)
		}
	}

	// Always scanned, regardless of preParserEnabled: the links are buried in
	// the indexer's markup, and reading line by line would find nothing.
	urls := linkscan.Extract(blob)
	if len(urls) == 0 {
		// This is what a real .nzb produces, so the reason is spelled out.
		sabError(w, "nothing in this payload is a link this instance can download; "+
			"a real .nzb needs a Usenet backend, which this instance does not have")
		return
	}

	if name == "" {
		name = "download-" + time.Now().Format("20060102-150405")
	}

	// A package of its own, named after the release, so with
	// subfolderByPackage on the grab gets its own folder. Filed as a paste,
	// the closest of the known entrances; a new one would change what every
	// rule keyed on the entrance sees.
	created := dc.a.AddLinksFrom(urls, name, app.OriginPaste)
	ids := make([]string, 0, len(created))
	for _, t := range created {
		if t != nil {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		// Every link was folded into one already in the list. Sonarr would
		// read an empty nzo_ids as an unexplained rejection.
		sabError(w, "every link in this payload is already in the list")
		return
	}
	// StartTasks, not StartTasksByHand: a queue halted by hand stays halted
	// when Sonarr finds an episode.
	dc.a.StartTasks(ids)

	grab := sabGrab{ID: newGrabID(), Name: name, Category: category, TaskIDs: ids, AddedAt: time.Now()}
	if err := dc.record(grab); err != nil {
		// The tasks are running; only the bookkeeping failed.
		sabError(w, "the download was staged but this instance could not record it: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": true, "nzo_ids": []string{grab.ID}})
}

// serveQueue answers mode=queue with what this bridge staged that has not
// finished or failed. Only its own grabs, so Sonarr never imports or deletes
// the owner's other downloads. ?start= and ?limit= are ignored; the list is
// bounded by task retention.
func (dc *downloadClient) serveQueue(w http.ResponseWriter, r *http.Request) {
	views := dc.views(r, false)
	slots := make([]map[string]any, 0, len(views))
	for i, v := range views {
		slots = append(slots, map[string]any{
			"status": v.status,
			"index":  i,
			// Sonarr splits "H:MM:SS" and parses the pieces; anything else
			// throws on its side.
			"timeleft": v.timeleft(),
			// Numbers rather than SABnzbd's quoted decimals: Newtonsoft reads
			// both, System.Text.Json only numbers.
			"mb":     megabytes(v.size),
			"mbleft": megabytes(v.size - v.loaded),
			// The release name as handed over, not the package name a rule
			// may have changed.
			"filename": v.grab.Name,
			// Must be a real SabnzbdPriority name. Fixed at Normal: the
			// bridge does not let an external program reorder the queue.
			"priority":   "Normal",
			"cat":        v.grab.Category,
			"percentage": v.percentage(),
			"nzo_id":     v.grab.ID,
		})
	}
	writeJSON(w, map[string]any{
		"queue": map[string]any{
			// Sonarr then shows every queued item as paused rather than
			// stuck.
			"paused":    dc.a.Queue().Halted,
			"slots":     slots,
			"noofslots": len(slots),
		},
	})
}

// serveHistory answers mode=history with what has finished or failed.
func (dc *downloadClient) serveHistory(w http.ResponseWriter, r *http.Request) {
	views := dc.views(r, true)
	slots := make([]map[string]any, 0, len(views))
	for _, v := range views {
		slots = append(slots, map[string]any{
			"fail_message":  v.failMsg,
			"bytes":         v.size,
			"category":      v.grab.Category,
			"nzb_name":      v.grab.Name,
			"name":          v.grab.Name,
			"download_time": int(time.Since(v.grab.AddedAt).Seconds()),
			// The folder Sonarr imports from.
			"storage": v.storage,
			"status":  v.status,
			"nzo_id":  v.grab.ID,
		})
	}
	writeJSON(w, map[string]any{
		"history": map[string]any{
			"paused":    dc.a.Queue().Halted,
			"slots":     slots,
			"noofslots": len(slots),
		},
	})
}

// serveDelete removes a grab and, with removeTasks, its tasks through
// RemoveTasks, like every other delete. del_files is honoured exactly as sent,
// so files are only deleted when asked for. Without removeTasks a finished
// grab is only forgotten: Sonarr stops tracking it, and its downloads and
// files stay in the list. One still in the queue is refused instead, since
// forgetting it would leave a download running that nobody tracks.
func (dc *downloadClient) serveDelete(w http.ResponseWriter, r *http.Request, removeTasks bool) {
	q := r.URL.Query()
	value := strings.TrimSpace(q.Get("value"))
	if value == "" {
		sabError(w, "delete needs the nzo_id in the value parameter")
		return
	}
	delFiles := q.Get("del_files") == "1"

	// SABnzbd accepts a comma-separated list.
	wanted := map[string]bool{}
	for _, id := range strings.Split(value, ",") {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}

	live := dc.liveTasks()
	dc.mu.Lock()
	defer dc.mu.Unlock()
	grabs, err := dc.load()
	if err != nil {
		sabError(w, err.Error())
		return
	}
	if !removeTasks {
		for id := range wanted {
			if g, ok := grabs[id]; ok {
				if v, ok := dc.view(g, live); ok && !v.finished {
					sabError(w, scopeRefusal(apitoken.ScopeControl))
					return
				}
			}
		}
	}
	var taskIDs []string
	for id := range wanted {
		g, ok := grabs[id]
		if !ok {
			continue
		}
		taskIDs = append(taskIDs, g.TaskIDs...)
		delete(grabs, id)
	}
	if removeTasks {
		dc.a.RemoveTasks(taskIDs, delFiles)
	}
	if err := dc.store(grabs); err != nil {
		sabError(w, err.Error())
		return
	}
	// Success even for an unknown id, as in SABnzbd, or Sonarr would log a
	// repeating failure for an item that is already gone.
	writeJSON(w, map[string]any{"status": true})
}

// grabView is one grab rendered against the live task list.
type grabView struct {
	grab   sabGrab
	status string
	// finished is whether this belongs in the history rather than the queue.
	finished bool
	failMsg  string
	size     int64
	loaded   int64
	speed    int64
	storage  string
}

func (v grabView) percentage() int {
	if v.size <= 0 {
		return 0
	}
	p := int(v.loaded * 100 / v.size)
	if p > 100 {
		return 100
	}
	return p
}

// timeleft is SABnzbd's "H:MM:SS" estimate, "0:00:00" for a job that is not
// moving.
func (v grabView) timeleft() string {
	left := v.size - v.loaded
	if v.speed <= 0 || left <= 0 {
		return "0:00:00"
	}
	secs := left / v.speed
	// Capped so a crawling link cannot overflow the format.
	if secs > 99*3600 {
		secs = 99 * 3600
	}
	return fmt.Sprintf("%d:%02d:%02d", secs/3600, (secs%3600)/60, secs%60)
}

// views builds the grabs on one side of the queue/history split, filtered by
// the requested category, and prunes grabs whose tasks are all gone. This is
// the only reader of the document, so the prune needs no timer.
func (dc *downloadClient) views(r *http.Request, finished bool) []grabView {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	live := dc.liveTasks()

	dc.mu.Lock()
	grabs, err := dc.load()
	if err != nil {
		dc.mu.Unlock()
		return nil
	}
	var out []grabView
	changed := false
	for id, g := range grabs {
		v, ok := dc.view(g, live)
		if !ok {
			delete(grabs, id)
			changed = true
			continue
		}
		if v.finished != finished {
			continue
		}
		// An empty category means everything; view reports a grab without one
		// as "*", which Sonarr keeps.
		if category != "" && !strings.EqualFold(v.grab.Category, category) {
			continue
		}
		out = append(out, v)
	}
	if changed {
		// A failed write is retried by the next call's prune.
		_ = dc.store(grabs)
	}
	dc.mu.Unlock()

	// Oldest first, so the order does not shuffle between two polls of a map.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].grab.AddedAt.Equal(out[j].grab.AddedAt) {
			return out[i].grab.AddedAt.Before(out[j].grab.AddedAt)
		}
		return out[i].grab.ID < out[j].grab.ID
	})
	return out
}

// liveTasks is the task list by id, which view reads a grab against.
func (dc *downloadClient) liveTasks() map[string]*core.Task {
	live := map[string]*core.Task{}
	for _, t := range dc.a.Tasks() {
		if t != nil {
			live[t.ID] = t
		}
	}
	return live
}

// view maps one grab's tasks onto the state SABnzbd would report, and reports
// whether the grab still exists.
//
// A grab takes the state of its least finished task. A task left in the
// collector for a person to look at (held by the link filter, offline at the
// host, or collected with an error) is reported as failed, because it will
// never start on its own and Sonarr should try another release; the task
// itself is left alone. Extracting stays in the queue, since the job is not
// finished.
func (dc *downloadClient) view(g sabGrab, live map[string]*core.Task) (grabView, bool) {
	v := grabView{grab: g}
	if strings.TrimSpace(v.grab.Category) == "" {
		// SABnzbd's catch-all, which Sonarr looks for when it has no category.
		v.grab.Category = "*"
	}
	var seen, failed, done, running, extracting, paused int
	for _, id := range g.TaskIDs {
		t := live[id]
		if t == nil {
			continue
		}
		seen++
		v.size += t.Size
		v.loaded += t.Loaded
		v.speed += t.Speed
		if v.storage == "" {
			// With subfolderByPackage off this is the shared download folder,
			// a limitation the module row's detail line points out.
			v.storage = dc.a.TaskFolder(t.ID)
		}
		switch {
		case t.Skipped:
			failed++
			if v.failMsg == "" {
				v.failMsg = t.SkipReason
			}
		case t.Status == core.StatusError:
			failed++
			if v.failMsg == "" {
				v.failMsg = t.Error
			}
		case t.Online == core.AvailOffline:
			failed++
			if v.failMsg == "" {
				v.failMsg = firstNonEmpty(t.Error, "the host says this file is gone")
			}
		case t.Status == core.StatusCollected && t.Error != "":
			// Failed while being resolved, so it never became StatusError.
			failed++
			if v.failMsg == "" {
				v.failMsg = t.Error
			}
		case t.Status == core.StatusDone:
			done++
		case t.Status == core.StatusRunning:
			running++
		case t.Status == core.StatusExtracting:
			extracting++
		case t.Status == core.StatusPaused || !t.Enabled:
			paused++
		}
	}
	if seen == 0 {
		return grabView{}, false
	}
	switch {
	case failed > 0:
		v.status, v.finished = "Failed", true
		if v.failMsg == "" {
			v.failMsg = "this download failed"
		}
	case done == seen:
		v.status, v.finished = "Completed", true
	case running > 0:
		v.status = "Downloading"
	case extracting > 0:
		v.status = "Extracting"
	case paused > 0:
		v.status = "Paused"
	default:
		v.status = "Queued"
	}
	return v, true
}

// record adds one grab to the document under the lock that also read it.
func (dc *downloadClient) record(g sabGrab) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	grabs, err := dc.load()
	if err != nil {
		return err
	}
	grabs[g.ID] = g
	return dc.store(grabs)
}

// load reads the grab document. Callers hold mu. An unreadable document starts
// over rather than making the bridge unusable.
func (dc *downloadClient) load() (map[string]sabGrab, error) {
	value, err := dc.a.UIState(downloadClientBucket)
	if err != nil {
		return nil, err
	}
	grabs := map[string]sabGrab{}
	if strings.TrimSpace(value) == "" {
		return grabs, nil
	}
	if json.Unmarshal([]byte(value), &grabs) != nil {
		return map[string]sabGrab{}, nil
	}
	return grabs, nil
}

// store writes the grab document back. Callers hold mu.
func (dc *downloadClient) store(grabs map[string]sabGrab) error {
	b, err := json.Marshal(grabs)
	if err != nil {
		return err
	}
	return dc.a.SetUIState(downloadClientBucket, string(b))
}

// newGrabID is the nzo_id Sonarr carries around, shaped like SABnzbd's own and
// random so one id cannot be guessed from another.
func newGrabID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "SABnzbd_nzo_" + fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return "SABnzbd_nzo_" + hex.EncodeToString(b[:])
}

// releaseName is the uploaded file's name without its last extension, so
// "Show.S01E01.1080p.WEB.nzb" becomes "Show.S01E01.1080p.WEB".
func releaseName(filename string) string {
	name := strings.TrimSpace(filename)
	// The base name only, so Sonarr sees the release rather than a path.
	// app.sanitizeSegment is still what makes the package name safe.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// megabytes is a byte count in MB, the unit of SABnzbd's whole API.
func megabytes(b int64) float64 {
	if b <= 0 {
		return 0
	}
	return float64(b) / (1 << 20)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
