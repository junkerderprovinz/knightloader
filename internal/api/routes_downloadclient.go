package api

// The download-client door Sonarr and Radarr can be pointed at, speaking
// SABnzbd's API.
//
// WHY SABnzbd AND NOT ONE OF THE OTHERS. This was checked against the *arr
// source rather than assumed, because the obvious answer and the right answer
// are not the same one here.
//
//   - SABnzbd (Sonarr's NzbDrone.Core/Download/Clients/Sabnzbd/SabnzbdProxy.cs,
//     which Radarr ships a line-for-line copy of): ONE path, "<urlBase>/api",
//     with a "mode" query parameter selecting the operation, and the credential
//     is "apikey" as a plain query parameter. The whole set the two apps ever
//     call is version, get_config, queue, history, addfile, retry, plus
//     "queue"/"history" with name=delete. That is six shapes on one route.
//   - qBittorrent (QBittorrentProxyV2.cs) looks like the better fit for a link
//     downloader at first glance, because /api/v2/torrents/add takes a "urls"
//     form field and a URL is exactly what this app eats. It was rejected on
//     two counts. It authenticates by POSTing a username and a password to
//     /api/v2/auth/login and carrying the SID cookie it hands back, which is a
//     second credential mechanism next to the API tokens this app already has,
//     and this app has no username/password pair to give it. And it is not six
//     shapes but roughly fifteen: app/webapiVersion, app/version,
//     app/preferences, torrents/info, torrents/properties, torrents/files,
//     torrents/add, torrents/delete, torrents/categories, torrents/createCategory,
//     torrents/setCategory, torrents/setShareLimits, torrents/topPrio,
//     torrents/setForceStart.
//   - Transmission and Deluge are worse on the same axis: both open with a
//     session handshake of their own (Transmission's 409 plus
//     X-Transmission-Session-Id, Deluge's auth.login cookie), both are
//     torrent-only, and neither has any way to be handed a plain http link.
//   - Blackhole reports no state at all, so Sonarr can never learn that a
//     download finished and can never import it. It is not a download client,
//     it is a folder.
//
// So SABnzbd wins on size and it wins on the credential: "apikey" maps
// one-to-one onto internal/apitoken, and nothing new had to be invented to
// authenticate this.
//
// WHERE THE EMULATION IS HONESTLY INCOMPLETE, and it matters most at the
// intake. SABnzbd's own API has both addfile (upload an .nzb) and addurl (hand
// over a URL), but Sonarr and Radarr only ever call addfile: they fetch the
// .nzb from the indexer themselves and POST the bytes
// (SabnzbdProxy.DownloadNzb -> AddFormUpload("name", filename, nzbData,
// "application/x-nzb")). This app has no Usenet backend, so the bytes of a real
// .nzb are useless to it: there is no NNTP client here to fetch articles with.
//
// What addfile below therefore does is run the uploaded bytes through the same
// link scanner the paste box uses (internal/linkscan), which is what makes this
// bridge work with a DDL indexer whose "nzb" download is really a link list or
// a container, and refuse the upload with a stated reason when the payload
// holds no link this app can act on. It does NOT pretend to have accepted a
// real .nzb: a fake success would leave Sonarr waiting on a download that can
// never start, which is worse than a refusal it can log and move past. addurl
// is implemented as well, because it is the shape this app can genuinely serve
// and a custom indexer or script can use it, even though neither *arr app calls
// it.
//
// The other honest gaps are named at their own call sites below: mode=retry,
// mode=fullstatus, the download folder when it is a pathvars template, and what
// happens with subfolderByPackage off.
//
// WHERE IT LIVES. The path is "/api/sabnzbd/api" rather than SABnzbd's bare
// "/api", because "/api" is this app's own namespace and cannot be given away.
// Sonarr and Radarr both have a "URL Base" field in the SABnzbd client form and
// both build their request as <urlBase> + "/api", so setting URL Base to
// "api/sabnzbd" lands exactly here.

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

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/linkscan"
)

// sabnzbdPath is the one address this bridge answers on. See the file comment
// for why it is not SABnzbd's own bare "/api".
const sabnzbdPath = "/api/sabnzbd/api"

// sabnzbdVersion is what mode=version answers, and it is a SABnzbd version on
// purpose rather than KnightLoader's own.
//
// Sonarr gates behaviour on it: TestConnectionAndVersion refuses anything below
// 0.7.0, GetCategories asks for mode=fullstatus instead of reading the queue
// when it is 2.0 or newer, and TestGlobalConfig only complains about pre_check
// below 1.1. A number that did not parse as major.minor.patch fails the
// connection test outright. This is the release of the API this file was
// written against; it is a claim about the protocol spoken here, not about the
// program speaking it.
const sabnzbdVersion = "4.3.3"

// maxUploadBytes bounds an addfile body before any of it is held in memory, the
// same read-then-check discipline routes_torrents.go and routes_backup.go use.
// A link list or a container that has to be scanned for links is kilobytes; the
// margin is for a large .nzb somebody points at this by mistake, which should
// be refused for the right reason rather than by running the process out of
// memory first.
const maxUploadBytes = 8 << 20

// downloadClientBucket is the interface-state bucket this bridge keeps its
// grabs in.
//
// Stored rather than derived, which is the exception to the rule the module
// registry works by (routes_features.go's file comment). Two facts have to
// survive that nothing else in this app records: which tasks belong to one
// grab, and which *arr category that grab arrived under. Neither is derivable
// from a task - a task knows its package and its links, not that Sonarr asked
// for it under "tv-sonarr" - and the category has to round-trip, because
// Sonarr's own GetItems() drops every item whose category is not the one it
// configured. Without it a Sonarr and a Radarr pointed at the same instance
// would each see the other's downloads as theirs and fight over them.
//
// It reuses the UI-state store for the same reason the module registry's park
// bucket does: this is not configuration, nothing but this file reads it, and a
// settings field would be a schema change for a map that is pure bookkeeping.
const downloadClientBucket = "downloadclient"

// sabGrab is one thing Sonarr handed over: the release, the category it came
// under, and the tasks it became.
type sabGrab struct {
	// ID is the nzo_id Sonarr is told and passes back for delete. Opaque and
	// generated, never the package name: a Packagizer rule may rename a package
	// the moment it is staged, and an id that renamed itself is an item that
	// vanishes from Sonarr's queue and reads to it as "removed from client".
	ID string `json:"id"`
	// Name is the release name, which is what Sonarr matches its own history
	// against and what it expects back as the slot's filename.
	Name     string    `json:"name"`
	Category string    `json:"category"`
	TaskIDs  []string  `json:"taskIds"`
	AddedAt  time.Time `json:"addedAt"`
}

// downloadClient holds what the routes share. It is built in
// registerDownloadClient rather than being package state, the same shape
// containerRelay in routes_containers.go has, so two apps in one test process
// never share a lock or a store.
type downloadClient struct {
	a *app.App
	// mu serialises the read-modify-write of the grab document. Two addfile
	// calls arriving together would otherwise each read the map, add their own
	// grab and write it back, and the second write would drop the first grab -
	// which reads to Sonarr as a release it handed over that the client then
	// denied ever seeing.
	mu sync.Mutex
}

func registerDownloadClient(reg *Registry, a *app.App) {
	dc := &downloadClient{a: a}

	// Open, and it is the second kind of open route this app has: not a way in
	// (that is /api/auth/login), but a route whose own credential is in the
	// request. Sonarr sends the key as ?apikey=, which the session guard knows
	// nothing about, so a guarded route would answer 401 to every call from a
	// password-protected instance.
	//
	// Open here does NOT mean unauthenticated. serve refuses every request that
	// does not carry a valid API token, on every instance, including one with no
	// password set at all - which makes this route stricter than the rest of the
	// app on such an instance, not looser. And while the module is switched off
	// it answers 404, so the door does not exist until somebody opens it.
	//
	// GET and POST are registered separately rather than as AnyMethod: everything
	// except addfile is a GET, addfile is a POST, and naming both is what stops a
	// third method from ever reaching this dispatcher by accident.
	reg.AddOpen(http.MethodGet, sabnzbdPath,
		"SABnzbd-shaped download client for Sonarr and Radarr (set their URL Base to \"api/sabnzbd\"); "+
			"off unless the downloadclient module is switched on, and the ?apikey= is an API token of this instance",
		dc.serve)
	reg.AddOpen(http.MethodPost, sabnzbdPath,
		"the same door for mode=addfile, which is the only call Sonarr and Radarr make as a POST",
		dc.serve)
}

// serve is the whole protocol: one address, one mode parameter.
func (dc *downloadClient) serve(w http.ResponseWriter, r *http.Request) {
	// The switch first, before the credential is even looked at. A door that is
	// closed should not be able to tell somebody whether a key would have
	// worked, and 404 is what "this endpoint is not here" means everywhere else
	// in this app - see the /api/ catch-all in routes.go, whose wording this
	// matches on purpose.
	if !dc.a.Settings.Get().DownloadClientAPI {
		http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		return
	}
	if !dc.authorized(w, r) {
		return
	}

	q := r.URL.Query()
	switch mode := strings.ToLower(strings.TrimSpace(q.Get("mode"))); mode {
	case "version":
		writeJSON(w, map[string]string{"version": sabnzbdVersion})
	case "get_config":
		dc.serveConfig(w)
	case "queue":
		// SABnzbd overloads mode=queue: without a name it lists, with
		// name=delete it removes. Sonarr uses both (GetQueue and
		// RemoveFromQueue), so the name has to be read before the listing
		// branch or a delete would silently answer with the queue and Sonarr
		// would go on reporting a download it thinks it removed.
		if strings.EqualFold(q.Get("name"), "delete") {
			dc.serveDelete(w, r)
			return
		}
		dc.serveQueue(w, r)
	case "history":
		if strings.EqualFold(q.Get("name"), "delete") {
			dc.serveDelete(w, r)
			return
		}
		dc.serveHistory(w, r)
	case "addfile", "addurl":
		dc.serveAdd(w, r, mode)
	default:
		// Named rather than answered with an empty success. mode=retry and
		// mode=fullstatus are the two Sonarr can reach that land here:
		//
		//   - retry is on Sonarr's proxy interface but nothing in Sonarr's
		//     SABnzbd client calls it, and "retry" here would mean
		//     App.RestartTasks, which the app already offers on its own route.
		//   - fullstatus is only asked for when complete_dir came back relative
		//     (Sabnzbd.GetCategories), and serveConfig below always answers an
		//     absolute one, because settings.sanitizePaths blanks a relative
		//     download folder and the built-in fallback is absolute. So this
		//     branch being reachable at all would mean an assumption above has
		//     stopped being true, which is exactly when a stated error beats a
		//     shrug.
		sabError(w, fmt.Sprintf("mode %q is not implemented by this download client", mode))
	}
}

// authorized checks the credential and, when it fails, answers in SABnzbd's own
// words. It reports whether the caller may proceed.
//
// The two error strings are load-bearing rather than decorative: Sonarr's
// TestAuthentication matches on "API Key Incorrect" and "API Key Required" to
// tell the person which field to go and fix, and anything else surfaces as an
// unattributed connection failure that sends them looking at their network.
//
// This deliberately answers HTTP 200 with an error document, which real SABnzbd
// also does. It is the one place where protocol fidelity beats HTTP hygiene:
// Sonarr's own error handling (SabnzbdProxy.CheckForError) reads the document,
// and a 401 is raised by its HTTP layer before that ever runs, turning "your
// API key is wrong" into "unable to connect to SABnzbd". Nothing is granted
// either way; only the sentence the person reads changes.
func (dc *downloadClient) authorized(w http.ResponseWriter, r *http.Request) bool {
	key := strings.TrimSpace(r.URL.Query().Get("apikey"))
	if key == "" {
		// Also accepted as a Bearer header, which is how every other client of
		// this app authenticates. Sonarr never sends one; a person testing this
		// route with curl always will, and making them move the token into a
		// query string to do it would be a second habit to learn for no reason.
		key = bearerToken(r)
	}
	if key == "" {
		sabError(w, "API Key Required")
		return false
	}
	// The instance's own named tokens, not a credential of this bridge's own.
	// A second store of secrets is a second place to revoke from, and the one
	// somebody forgets is the one still working after the phone was lost.
	if _, ok := dc.a.APITokens.Check(key); !ok {
		sabError(w, "API Key Incorrect")
		return false
	}
	return true
}

// sabError is SABnzbd's failure document. Sonarr reads status and error out of
// it and puts the sentence in front of the person, so the text is written to be
// read by one.
func sabError(w http.ResponseWriter, msg string) {
	writeJSON(w, map[string]any{"status": false, "error": msg})
}

// serveConfig answers mode=get_config, which is what Sonarr's connection test,
// its category validation and its "where does this client put things" status
// all read.
//
// Every field below is here because leaving it out breaks something specific,
// not for completeness:
//
//   - misc.complete_dir has to be an ABSOLUTE path or Sonarr goes looking for a
//     root folder through mode=fullstatus, which this bridge does not answer.
//   - categories must exist and must contain the category configured in Sonarr,
//     or its TestCategory fails with "category missing" and the client cannot be
//     saved. A category dir must not end in "*", which Sonarr reads as SABnzbd's
//     "no job folders" setting and warns about.
//   - sorters must be present as a list. Sonarr calls config.Sorters.Any(...)
//     without a nil check, so an absent key is a NullReferenceException on its
//     side rather than a missing feature.
//   - the three sorting switches are false because this app has no equivalent of
//     SABnzbd's own renaming, and claiming otherwise makes Sonarr warn that the
//     client will rename files out from under it.
//   - history_retention_option "all" tells Sonarr this client never drops
//     finished items by itself, so Sonarr removes them after import. That is
//     true of this bridge: keepFinishedDays trims the task list after a month,
//     which is long past the import, and nothing else removes anything.
func (dc *downloadClient) serveConfig(w http.ResponseWriter) {
	// The empty id is "where would a task with no folder of its own go", which
	// is this instance's download folder. When that folder is a pathvars
	// template ("/downloads/<jd:packagename>") this reports the template
	// verbatim: it is still absolute, so the connection test passes, but the
	// root folder Sonarr shows in its own status page is then a path with a
	// placeholder in it. Reported honestly rather than guessed at, because
	// trimming the template back to its fixed prefix would need a second copy of
	// settings.fixedPrefix here and a second copy is a second thing to get wrong.
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

// sabCategories is the list Sonarr validates its configured category against.
//
// It is a fixed list and not a set this app stores, because the bridge does not
// actually restrict anything: serveAdd files a grab under whatever category
// arrived, so any name works. The list exists purely so Sonarr's TestCategory
// finds the one it was configured with, and it holds SABnzbd's own catch-all
// plus the two names Sonarr and Radarr ship as their defaults - which is what
// somebody who has not thought about categories will have configured.
//
// Deriving it from the categories seen so far was the alternative and is worse:
// a fresh install would then advertise nothing, Sonarr's connection test would
// fail on the very first attempt, and the only way out would be to grab
// something first through a client that does not save.
func sabCategories() []map[string]string {
	return []map[string]string{
		{"name": "*", "dir": ""},
		{"name": "tv-sonarr", "dir": "tv-sonarr"},
		{"name": "radarr", "dir": "radarr"},
	}
}

// serveAdd is the intake: addfile (what Sonarr and Radarr send) and addurl
// (what a script can send). See the file comment for why an uploaded .nzb is
// scanned for links rather than parsed as Usenet.
func (dc *downloadClient) serveAdd(w http.ResponseWriter, r *http.Request, mode string) {
	category := strings.TrimSpace(r.URL.Query().Get("cat"))
	name := strings.TrimSpace(r.URL.Query().Get("nzbname"))

	var blob string
	if mode == "addurl" {
		// SABnzbd's addurl carries the URL in "name", which is the same
		// parameter addfile uses for the uploaded file's name. Kept as SABnzbd
		// has it rather than renamed to something clearer, because a caller
		// writing against SABnzbd's own documentation has to work here unchanged
		// or this is not an emulation, it is a lookalike.
		blob = strings.TrimSpace(r.URL.Query().Get("name"))
		if blob == "" {
			sabError(w, "addurl needs the link in the name parameter")
			return
		}
	} else {
		// Capped at the reader, before the body is fully in memory. By the time
		// a parser can refuse something for being too large the too-large bytes
		// are already held, which is the lesson routes_torrents.go and
		// routes_backup.go both carry at their own upload doors.
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)
		file, header, err := r.FormFile("name")
		if err != nil {
			// SABnzbd itself also accepts the field under the name "nzbfile", and
			// a hand-rolled caller is likelier to have read that from SABnzbd's
			// documentation than to have read Sonarr's source.
			file, header, err = r.FormFile("nzbfile")
			if err != nil {
				// Told apart, because the two look identical from here and mean
				// opposite things to whoever has to fix it: one is a client
				// sending the wrong field, the other is a payload the cap
				// refused before the multipart parser ever saw a complete form.
				// Reported as one generic "send it as a form field" this route
				// would send somebody rewriting a request that was already
				// correct.
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
		// One byte over the cap is read on purpose, so that "exactly at the
		// limit" and "too large" can be told apart at all - a LimitReader that
		// stops at the cap hands back a truncated payload that looks like a
		// valid short one, and the link scanner would then happily find the
		// links in the first eight megabytes of something that should have been
		// refused.
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

	// The same scanner the paste box uses, and unconditionally rather than
	// behind preParserEnabled. That setting exists so somebody whose PASTE the
	// scanner misreads can fall back to one-link-per-line; this payload is a
	// file that arrived over the wire with links buried in whatever markup the
	// indexer wrapped them in, and reading it line by line would find nothing at
	// all.
	urls := linkscan.Extract(blob)
	if len(urls) == 0 {
		// The refusal names the actual reason, because this is the failure a
		// real .nzb produces, and "rejected for an unknown reason" would send
		// somebody debugging their indexer instead of learning that this app has
		// no Usenet backend.
		sabError(w, "nothing in this payload is a link this instance can download; "+
			"a real .nzb needs a Usenet backend, which this instance does not have")
		return
	}

	if name == "" {
		name = "download-" + time.Now().Format("20060102-150405")
	}

	// Staged as a package of its own, named after the release. That is what
	// gives the grab its own folder when subfolderByPackage is on, which is what
	// lets Sonarr's importer tell one release from another - see grabView's
	// storage note for what happens when it is off.
	//
	// OriginPaste, and that is the one mapping here with no honest counterpart:
	// the five entrances (app.KnownOrigin) are paste, crawl, cnl, watch and
	// container, and this is none of them. It is filed as a paste because a
	// program pasting links is closer to the truth than a crawl or a watch
	// folder, and because adding a sixth entrance changes what KnownOrigin
	// accepts and what every rule keyed on the entrance sees - a wider change
	// than one route should make on its own.
	created := dc.a.AddLinksFrom(urls, name, app.OriginPaste)
	ids := make([]string, 0, len(created))
	for _, t := range created {
		if t != nil {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		// Nothing was staged, which at this point means the mirror set folded
		// every link into one already in the list. Refused rather than answered
		// with an empty nzo_ids, because Sonarr treats an empty id list as
		// "rejected for an unknown reason" and this reason is worth stating.
		sabError(w, "every link in this payload is already in the list")
		return
	}
	// The automatic start, not the one a person presses: StartTasks leaves a
	// halt somebody set by hand exactly where it was, which is what stops a
	// stopped queue from starting itself because Sonarr found an episode. See
	// App.StartTasks against StartTasksByHand.
	dc.a.StartTasks(ids)

	grab := sabGrab{ID: newGrabID(), Name: name, Category: category, TaskIDs: ids, AddedAt: time.Now()}
	if err := dc.record(grab); err != nil {
		// The tasks exist and are running; only the bookkeeping failed. Said
		// plainly, because Sonarr would otherwise be handed an id it can never
		// look up again and would report the download as vanished.
		sabError(w, "the download was staged but this instance could not record it: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": true, "nzo_ids": []string{grab.ID}})
}

// serveQueue answers mode=queue with everything this bridge staged that has not
// finished or failed.
//
// Only what this bridge staged. A queue that reported the whole task list would
// hand Sonarr the instance owner's own downloads to import and delete, and
// Sonarr's category filter is no protection at all for a task that never had a
// category.
//
// ?start= and ?limit= are read and ignored, which is a deviation and a small
// one. Sonarr asks the queue for start=0&limit=0, which in SABnzbd means "all
// of it" and is what this answers; it asks the history for a limit of its own
// (30 by default) and gets everything instead, which costs it a slightly longer
// list to walk and nothing else. Paging would be worth adding the day this list
// is long enough to matter, and it is bounded by the task list, which is
// trimmed by retention.
func (dc *downloadClient) serveQueue(w http.ResponseWriter, r *http.Request) {
	views := dc.views(r, false)
	slots := make([]map[string]any, 0, len(views))
	for i, v := range views {
		slots = append(slots, map[string]any{
			"status": v.status,
			"index":  i,
			// A string in "H:MM:SS", because Sonarr parses this one with a
			// converter that splits on ":" and calls int.Parse on the pieces.
			// A number or a null here is an exception on its side, not a
			// missing estimate.
			"timeleft": v.timeleft(),
			// Megabytes as JSON numbers rather than the quoted decimals real
			// SABnzbd writes. Both are read correctly by Newtonsoft, which is
			// what Sonarr and Radarr deserialize with today, and a number is
			// also read correctly by System.Text.Json, which a quoted decimal
			// is not. Where the two shapes are equally faithful, the one that
			// survives the migration is the better one to write.
			"mb":     megabytes(v.size),
			"mbleft": megabytes(v.size - v.loaded),
			// The release name, which is what Sonarr shows and logs. It is the
			// name this bridge was handed, kept on the grab, not the package
			// name read back from a task: a Packagizer rule is free to rename
			// the package, and Sonarr matching its own history against a
			// renamed title finds nothing.
			"filename": v.grab.Name,
			// Parsed by Sonarr with Enum.TryParse, which quietly yields the
			// zero value for anything it does not know, so this must be a real
			// SabnzbdPriority name. Fixed at Normal because this bridge does
			// not act on the priority Sonarr sends: task priority here is the
			// user's own ordering of their queue, and letting an external
			// program reorder it silently is not something a download client
			// should do uninvited.
			"priority":   "Normal",
			"cat":        v.grab.Category,
			"percentage": v.percentage(),
			"nzo_id":     v.grab.ID,
		})
	}
	writeJSON(w, map[string]any{
		"queue": map[string]any{
			// The master switch, reported honestly: Sonarr marks every queued
			// item Paused when this is true, which is exactly right for a halted
			// queue and is the difference between "nothing is moving" and
			// "nothing is moving and Sonarr thinks it is stuck".
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
			// The folder the bytes are in, which is what Sonarr imports from.
			// See grabView.storage for the one case where this is a folder
			// shared with other grabs and what that costs.
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

// serveDelete removes a grab, and the tasks behind it, through the same
// RemoveTasks every other delete in this app goes through - which is the one
// path that unfiles the link from the mirror set and frees the dispatch slot.
//
// del_files is honoured exactly as sent. It is the difference between taking a
// row off a list and deleting somebody's file, and this app has never conflated
// the two; a bridge that quietly deleted files because a remote program asked
// to tidy its queue would be the worst possible place to start.
func (dc *downloadClient) serveDelete(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	value := strings.TrimSpace(q.Get("value"))
	if value == "" {
		sabError(w, "delete needs the nzo_id in the value parameter")
		return
	}
	delFiles := q.Get("del_files") == "1"

	// SABnzbd accepts a comma-separated list here and so does this. Sonarr sends
	// one id at a time, but a caller that batches must not have half its request
	// silently ignored.
	wanted := map[string]bool{}
	for _, id := range strings.Split(value, ",") {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}

	dc.mu.Lock()
	defer dc.mu.Unlock()
	grabs, err := dc.load()
	if err != nil {
		sabError(w, err.Error())
		return
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
	dc.a.RemoveTasks(taskIDs, delFiles)
	if err := dc.store(grabs); err != nil {
		sabError(w, err.Error())
		return
	}
	// SABnzbd answers a bare success here whether or not the id was known, and
	// so does this: Sonarr calls delete for an item it has decided to forget,
	// and an error for an id that is already gone would turn a completed cleanup
	// into a permanent, repeating failure in its log.
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

// timeleft is SABnzbd's "H:MM:SS" estimate. Zero speed answers "0:00:00", which
// is what SABnzbd itself reports for a job that is not moving, and Sonarr only
// ever shows it.
func (v grabView) timeleft() string {
	left := v.size - v.loaded
	if v.speed <= 0 || left <= 0 {
		return "0:00:00"
	}
	secs := left / v.speed
	// Capped so a link crawling at a few bytes a second cannot report a number
	// of hours the format has no room for and the converter then mis-parses.
	if secs > 99*3600 {
		secs = 99 * 3600
	}
	return fmt.Sprintf("%d:%02d:%02d", secs/3600, (secs%3600)/60, secs%60)
}

// views builds the grabs that belong on one side of the queue/history split,
// filtered by the category the caller asked for, and prunes the ones whose
// tasks are all gone.
//
// The prune is here rather than on a timer because this is the only code that
// ever reads the document, and a grab whose tasks have all been removed - by
// hand, or by the retention sweep - is a row nothing can be said about any
// more. SABnzbd's own history is finite for the same reason.
func (dc *downloadClient) views(r *http.Request, finished bool) []grabView {
	category := strings.TrimSpace(r.URL.Query().Get("category"))

	live := map[string]*core.Task{}
	for _, t := range dc.a.Tasks() {
		if t != nil {
			live[t.ID] = t
		}
	}

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
		// An empty category on the request means "everything", which is what
		// Sonarr sends when its own category field is blank. Its GetItems then
		// keeps only items whose category is "*", so a grab that arrived with no
		// category at all is reported as "*" by view below rather than as an
		// empty string it would silently drop.
		if category != "" && !strings.EqualFold(v.grab.Category, category) {
			continue
		}
		out = append(out, v)
	}
	if changed {
		// A failed write is not worth failing the request over: the stale grabs
		// are pruned again on the next call, and the queue Sonarr is waiting for
		// is the thing it actually asked for.
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

// view maps one grab's tasks onto the state SABnzbd would report, and reports
// whether the grab still exists at all.
//
// THE STATE MAPPING, which is where an emulation is honest or is not. SABnzbd
// has one status per job; this app has a status per task and a grab can be
// several tasks. So the grab takes the state of its least finished part, with
// three deliberate exceptions, and all three are the same judgement: a task
// this app keeps in the collector so a PERSON can look at it is a task Sonarr
// has to be told to give up on, because Sonarr cannot look at anything.
//
//   - A link the link filter is holding (Skipped) is reported FAILED, not
//     queued. It is a task that will never start on its own, and reporting it
//     as queued leaves Sonarr waiting out its whole timeout before trying
//     another release. The skip reason travels as fail_message, so the person
//     reading Sonarr's log learns which rule caught it.
//   - A link the host has answered "gone" for (AvailOffline) is reported
//     FAILED even though its status is still "collected".
//   - A collected link carrying an error is reported FAILED for the same
//     reason: it is one nothing could resolve, and its status never becomes
//     StatusError because it was never started.
//
// None of the three touches the task. Telling Sonarr to move on is not the same
// as throwing the link away, and the holding area still holds what it held.
//
// Everything else maps straight across: error is Failed, all-done is Completed,
// running is Downloading, extracting is Extracting, paused or switched off is
// Paused, and anything still waiting is Queued. "Extracting" is left in the
// QUEUE rather than moved to the history the way real SABnzbd does its
// post-processing, because the job genuinely is not finished and Sonarr reads
// an unknown queue status as Downloading, which is the truth.
func (dc *downloadClient) view(g sabGrab, live map[string]*core.Task) (grabView, bool) {
	v := grabView{grab: g}
	if strings.TrimSpace(v.grab.Category) == "" {
		// SABnzbd's own catch-all, and what Sonarr's GetItems looks for when no
		// category is configured on its side.
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
			// The folder the app itself would write this task into, asked of the
			// app rather than rebuilt here. With subfolderByPackage on this is
			// the grab's own folder and Sonarr imports exactly this release;
			// with it off it is the shared download folder, and Sonarr's
			// importer then sees every other download beside this one. That is a
			// real limitation of running this bridge with per-package folders
			// switched off, and the module registry's own detail line says so
			// rather than leaving it to be discovered.
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
			// A collected task carrying an error is one that failed while it
			// was being resolved: nothing handled the link, or the resolver
			// refused it. Its status never becomes StatusError, because it was
			// never started, so the two cases above do not catch it - and left
			// as Queued it is a slot Sonarr waits on forever for a link that
			// has already been decided against.
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

// load reads the grab document. Callers hold mu.
//
// An unreadable document starts a fresh one rather than failing every call,
// the same choice routes_features.go's parkDoc makes: what is lost is the
// bookkeeping for grabs already in flight, and what is gained is that a single
// bad write cannot make the bridge permanently unusable.
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

// newGrabID is the nzo_id Sonarr carries around. It is shaped like SABnzbd's
// own ("SABnzbd_nzo_xxxxxx") because a client that pattern-matches the id would
// otherwise be surprised by ours, and it is random rather than sequential so
// that an id cannot be guessed from another one and used to delete a grab
// somebody else made.
func newGrabID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Never observed in practice; a time-based id is still unique enough to
		// address a grab, and refusing an intake because the random source
		// hiccuped would be the worse failure.
		return "SABnzbd_nzo_" + fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return "SABnzbd_nzo_" + hex.EncodeToString(b[:])
}

// releaseName is the uploaded file's name without its extension, which is the
// release name Sonarr expects to see back on the queue and history slots. It
// strips only the last extension: "Show.S01E01.1080p.WEB.nzb" becomes
// "Show.S01E01.1080p.WEB", and the dots inside the release are left alone.
func releaseName(filename string) string {
	name := strings.TrimSpace(filename)
	// The base name only. A multipart part header carries whatever the client
	// put in it, path separators included, and a name with a directory in it
	// would end up as a package name and therefore as a folder. This is the
	// outer of two guards and not the binding one: app.sanitizeSegment is what
	// every package name goes through before it becomes a path segment, and it
	// already maps separators to dashes and trims a name down to nothing (then
	// to "package") if dots are all that is left. Stripping here as well means
	// the name a person reads in Sonarr's queue is the release rather than a
	// mangled path, which the sanitizer alone would not give them.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// megabytes is a byte count as SABnzbd reports sizes. Its whole API is in MB.
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
