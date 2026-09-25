package api

// Torrent intake: uploading a .torrent file and staging it once its file
// selection is known. A magnet link is pasted like any other link and needs
// nothing here.
//
// Parsing and staging are separate routes so parsing has no side effects: a
// browser can show a large file tree and the user can change their mind
// without leaving a half-staged task behind.
//
// The torrent settings' own checks live here as well: the file rules and
// tracker lists a save is refused over, and how the public tracker list fared.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// maxStageBody bounds POST /api/torrents' body: the uri field carries the
// same bytes the upload capped at MaxTorrentBytes, plus base64 and JSON
// overhead, so it gets the same margin as the upload.
const maxStageBody = torrent.MaxTorrentBytes + 1<<20

func registerTorrents(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/torrents/parse",
		"validate an uploaded .torrent and return its file tree, with the files the torrent file selection chooses selected; stages nothing",
		func(w http.ResponseWriter, r *http.Request) {
			parseTorrentUpload(w, r, a)
		})

	reg.Add(http.MethodPost, "/api/torrents",
		"stage a magnet or an uploaded .torrent (from /api/torrents/parse) with a file selection",
		func(w http.ResponseWriter, r *http.Request) {
			stageTorrent(w, r, a)
		})

	reg.Add(http.MethodGet, "/api/torrents/trackers",
		"the public tracker list the torrent settings name: how many trackers it gave, when, and why the last fetch failed",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.TrackerListStatus())
		})
}

// torrentTree is what a parsed .torrent hands back: the file tree and the URI
// staging carries it forward as, in the API's camelCase, since
// torrent.Metadata has no JSON tags of its own.
type torrentTree struct {
	URI             string             `json:"uri"`
	InfoHash        string             `json:"infoHash"`
	Name            string             `json:"name"`
	Private         bool               `json:"private"`
	TotalSize       int64              `json:"totalSize"`
	PieceLength     int64              `json:"pieceLength"`
	Pieces          int                `json:"pieces"`
	Files           []core.TorrentFile `json:"files"`
	Trackers        []string           `json:"trackers"`
	DroppedTrackers int                `json:"droppedTrackers"`
}

// parseTorrentUpload handles untrusted input. torrent.ParseUpload checks size,
// geometry, file count and, above all, that no file path escapes the download
// folder; this handler adds the size cap that has to apply before the body is
// read (see torrent.MaxTorrentBytes), as uploadRestore does.
//
// The tree comes back with the files the file rules choose already selected,
// so the review shows what staging it untouched would fetch.
func parseTorrentUpload(w http.ResponseWriter, r *http.Request, a *app.App) {
	// The extra megabyte is for multipart boundaries and headers.
	r.Body = http.MaxBytesReader(w, r.Body, torrent.MaxTorrentBytes+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "send the .torrent as a multipart form field named \"file\"", http.StatusBadRequest)
		return
	}
	defer file.Close()
	// Capped at the reader, before the parser sees any bytes.
	data, err := io.ReadAll(io.LimitReader(file, torrent.MaxTorrentBytes+1))
	if err != nil {
		http.Error(w, "could not read the uploaded file", http.StatusBadRequest)
		return
	}
	if len(data) > torrent.MaxTorrentBytes {
		http.Error(w, fmt.Sprintf("a .torrent over %d bytes is refused", torrent.MaxTorrentBytes), http.StatusRequestEntityTooLarge)
		return
	}

	md, uri, err := torrent.ParseUpload(data)
	if err != nil {
		// torrent.Parse's errors are written to be read, so they go out as
		// they are.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if md.Files, err = a.PickTorrentFiles(md.Files); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, torrentTree{
		URI:             uri,
		InfoHash:        md.InfoHash,
		Name:            md.Name,
		Private:         md.Private,
		TotalSize:       md.TotalSize,
		PieceLength:     md.PieceLength,
		Pieces:          md.Pieces,
		Files:           md.Files,
		Trackers:        md.Trackers,
		DroppedTrackers: md.DroppedTrackers,
	})
}

// stageTorrent turns the parsed tree, with the user's selection, into a task.
//
// selectedPaths names the files to keep and is matched against a fresh
// re-parse of uri, never against paths or sizes from the request body. The
// body can only narrow which real files are fetched; it cannot add a file or
// change a path.
func stageTorrent(w http.ResponseWriter, r *http.Request, a *app.App) {
	// Read with a cap and checked before decoding, so an oversized body gets a
	// 413 rather than decodeJSON's generic 400.
	data, err := io.ReadAll(io.LimitReader(r.Body, maxStageBody+1))
	if err != nil {
		http.Error(w, "could not read the request body", http.StatusBadRequest)
		return
	}
	if len(data) > maxStageBody {
		http.Error(w, fmt.Sprintf("a stage request over %d bytes is refused", maxStageBody), http.StatusRequestEntityTooLarge)
		return
	}
	var body struct {
		URI     string `json:"uri"`
		Package string `json:"package"`
		// SelectedPaths is a pointer: absent leaves the choice to the file
		// rules of the category the task ends up in (see App.AddTorrent),
		// while an empty list means every box was unticked (see
		// core.SelectedTorrentIndices).
		SelectedPaths *[]string `json:"selectedPaths"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if !torrent.IsURI(body.URI) || torrent.IsMagnet(body.URI) {
		// A magnet has no file tree yet and is staged through POST
		// /api/links; accepting one here would silently ignore the selection.
		http.Error(w, "send the uri from POST /api/torrents/parse; a magnet link is staged through POST /api/links instead", http.StatusBadRequest)
		return
	}
	md, err := (torrent.Resolver{}).Describe(body.URI)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var files []core.TorrentFile
	if body.SelectedPaths != nil {
		want := make(map[string]bool, len(*body.SelectedPaths))
		for _, p := range *body.SelectedPaths {
			want[p] = true
		}
		files = make([]core.TorrentFile, len(md.Files))
		for i, f := range md.Files {
			f.Selected = want[f.Path]
			files[i] = f
		}
	}

	task, err := a.AddTorrent(body.URI, files, body.Package, app.OriginPaste)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, task) // null when the mirror set folded this into a task already in the list
}

// checkTorrentSettings refuses what sanitize would keep but nothing could use,
// naming the box it is in: a file pattern that does not compile, a line that
// is no tracker address, a list address that is not http or https, and a
// banned line that names no host. Blank lines are sanitize's to drop.
func checkTorrentSettings(t settings.Torrent) error {
	refuse := func(field string, err error) error {
		return &settings.FieldError{Field: "torrent." + field, Err: err}
	}
	if err := torrent.CheckPatterns(t.IncludeFiles); err != nil {
		return refuse("includeFiles", fmt.Errorf("only these files: %w", err))
	}
	if err := torrent.CheckPatterns(t.ExcludeFiles); err != nil {
		return refuse("excludeFiles", fmt.Errorf("never these files: %w", err))
	}
	for i, line := range t.ExtraTrackers {
		if strings.TrimSpace(line) != "" && !torrent.ValidTracker(line) {
			return refuse("extraTrackers", fmt.Errorf("extra trackers, line %d: %q is not a tracker address (udp, http, https, ws or wss)", i+1, line))
		}
	}
	if raw := strings.TrimSpace(t.TrackerListURL); raw != "" {
		// The address is left out of the message, in case it carries a login.
		if u, err := url.Parse(raw); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return refuse("trackerListUrl", errors.New("the tracker list address is not an http or https address"))
		}
	}
	for i, line := range t.BannedTrackers {
		if strings.TrimSpace(line) != "" && torrent.BannedHost(line) == "" {
			return refuse("bannedTrackers", fmt.Errorf("banned trackers, line %d: %q names no host", i+1, line))
		}
	}
	return nil
}

// checkCategoryFileRules refuses a category whose own torrent file selection
// has a pattern that does not compile, on that category's row.
func checkCategoryFileRules(cats []settings.Category) error {
	for i, c := range cats {
		r := c.TorrentFiles
		if r == nil {
			continue
		}
		if _, err := (torrent.FileRules{Include: r.IncludeFiles, Exclude: r.ExcludeFiles}).Compile(); err != nil {
			return &settings.FieldError{Field: fmt.Sprintf("categories.%d", i),
				Err: fmt.Errorf("category %d (%s): %w", i+1, categoryLabel(c, i), err)}
		}
	}
	return nil
}
