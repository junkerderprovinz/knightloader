package api

// Torrent intake: uploading a .torrent file and staging it once its file
// selection is known. A magnet link is pasted like any other link and needs
// nothing here.
//
// Parsing and staging are separate routes so parsing has no side effects: a
// browser can show a large file tree and the user can change their mind
// without leaving a half-staged task behind.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// maxStageBody bounds POST /api/torrents' body: the uri field carries the
// same bytes the upload capped at MaxTorrentBytes, plus base64 and JSON
// overhead, so it gets the same margin as the upload.
const maxStageBody = torrent.MaxTorrentBytes + 1<<20

func registerTorrents(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/torrents/parse",
		"validate an uploaded .torrent and return its file tree; stages nothing",
		func(w http.ResponseWriter, r *http.Request) {
			parseTorrentUpload(w, r)
		})

	reg.Add(http.MethodPost, "/api/torrents",
		"stage a magnet or an uploaded .torrent (from /api/torrents/parse) with a file selection",
		func(w http.ResponseWriter, r *http.Request) {
			stageTorrent(w, r, a)
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
func parseTorrentUpload(w http.ResponseWriter, r *http.Request) {
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
		// SelectedPaths is a pointer: absent keeps every file selected, while
		// an empty list means every box was unticked (see
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

	files := make([]core.TorrentFile, len(md.Files))
	copy(files, md.Files)
	if body.SelectedPaths != nil {
		want := make(map[string]bool, len(*body.SelectedPaths))
		for _, p := range *body.SelectedPaths {
			want[p] = true
		}
		for i := range files {
			files[i].Selected = want[files[i].Path]
		}
	}

	task, err := a.AddTorrent(body.URI, files, body.Package, app.OriginPaste)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, task) // null when the mirror set folded this into a task already in the list
}
