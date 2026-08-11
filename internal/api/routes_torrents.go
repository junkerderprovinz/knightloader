package api

// Torrent intake: uploading a .torrent file and staging it once its file
// selection is known.
//
// Magnet-link paste needs nothing here - it is already a string on the
// collector's existing paste path, and internal/resolver/torrent.Resolver.Match
// already recognises the scheme (see app.go's boot-time Register call). What a
// magnet or an upload BOTH still need before staging continues, for a torrent
// with more than one file, is the file tree: parse first shows it, stage then
// creates the task with whatever the tree ended up checked.
//
// The two routes are deliberately separate rather than one upload-and-stage
// call. Parsing is free of side effects - nothing is created, nothing is
// staged - so a browser can let somebody look at a hundred-file tree, change
// their mind, or navigate away, without a half-staged task left behind either
// way.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// maxStageBody bounds POST /api/torrents' own body: the uri field wraps the
// same bytes parseTorrentUpload already caps at MaxTorrentBytes, plus base64
// and JSON-escaping overhead on top of them - the same +1MiB margin that
// route's own MaxBytesReader gives itself, not a tighter one, so an
// ordinary large-but-legitimate torrent that parsed correctly at the upload
// step is never rejected only here for being (correctly) a little larger
// encoded.
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

// torrentTree is what a parsed .torrent hands back: enough to draw the file
// tree, and the URI staging carries it forward as. core.TorrentFile already
// has the json tags the tree itself needs (path/size/selected); the rest of
// torrent.Metadata does not - it carries no tags at all, being Go-side data
// for a resolver's own use rather than a wire shape - so this is the
// translation to the camelCase the rest of the API uses, done here rather
// than by adding tags to a type internal/resolver/torrent owns.
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

// parseTorrentUpload is the untrusted half. An uploaded .torrent is exactly
// the class of input package 20's file route and Wave 10's restore upload
// already were: size-limited BEFORE the whole body is read into memory, and
// validated before anything else happens - see routes_backup.go's
// uploadRestore, the pattern this copies.
//
// Every check that matters - size, geometry, file count, and above all
// whether any file's own path would escape the download folder - runs inside
// torrent.Parse (called via ParseUpload), which is 11.5A's answer to the
// wave's own "do not invent a fresh, unreviewed path-safety check" warning.
// This handler adds exactly one check ParseUpload cannot: the request-level
// size cap, enforced before the bytes are even fully read, which is the one
// gate that has to live at the door rather than inside the parser - see
// torrent.MaxTorrentBytes's own doc comment, which names this handler by
// description.
func parseTorrentUpload(w http.ResponseWriter, r *http.Request) {
	// The +1<<20 slack is for multipart's own boundary and header overhead
	// around the file part, not for the file itself - the same margin
	// uploadRestore and the container route both give their own MaxBytes.
	r.Body = http.MaxBytesReader(w, r.Body, torrent.MaxTorrentBytes+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "send the .torrent as a multipart form field named \"file\"", http.StatusBadRequest)
		return
	}
	defer file.Close()
	// Capped at the reader, before ParseUpload ever sees a byte: by the time a
	// parser can refuse something for being too large, the too-large bytes are
	// already in memory. This is the door the doc comment above is about.
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
		// Verbatim: torrent.Parse's errors are written to be read - "this
		// .torrent's piece layout does not match the data it describes" is an
		// explanation, and a generic "invalid file" is what sends somebody
		// re-uploading the same broken one.
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

// stageTorrent is the confirm step: the file tree parseTorrentUpload returned
// (checked and unchecked by a person, or left untouched for a single-file
// torrent that never showed one) becomes a task.
//
// selectedPaths NAMES THE FILES TO KEEP, not the ones to drop, and it is read
// against THIS HANDLER'S OWN FRESH RE-PARSE of uri - never against whatever
// the request body claims a file's path or size is. This is the wave's own
// containment lesson applied to the other direction: parseTorrentUpload
// already proved uri decodes to a torrent with only safe paths in it, but a
// browser could still send back a hand-edited files list with a Path that was
// never in that torrent at all, or a Selected the parse never set. Re-parsing
// here (Describe, which is what a.AddTorrent's own Resolver.Info().ID branch
// does too) means the request body can only ever narrow which of the REAL
// files get fetched - it has no way to introduce one that is not real, and no
// way to change what a real one's path is.
func stageTorrent(w http.ResponseWriter, r *http.Request, a *app.App) {
	// Read-then-check, the identical shape parseTorrentUpload uses above and
	// for the identical reason: by the time a JSON decoder can refuse a body
	// for being too large, the too-large bytes are already in memory.
	// decodeJSON's shared, generic error handling turns any decode failure
	// into one plain 400 "bad json" - reached for here deliberately instead
	// of a bare http.MaxBytesReader assignment, which would still route
	// through that same generic 400 and lose the specific, honest 413 an
	// oversized body deserves (reproduced before this shape: a body this
	// size answered 400 "bad json", not 413, with no MaxBytesReader
	// involved at all - the whole body had already been read by then).
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
		// SelectedPaths is a pointer so the three JSON shapes stay distinguishable:
		// the field absent (nil) keeps every file selected, the way Parse leaves a
		// tree nobody has looked at; "[]" (a non-nil, empty slice) is a person
		// unticking every box, which is a real answer and not the same as absent -
		// see core.SelectedTorrentIndices' own doc comment for why that distinction
		// matters all the way down to what the engine is told to fetch.
		SelectedPaths *[]string `json:"selectedPaths"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if !torrent.IsURI(body.URI) || torrent.IsMagnet(body.URI) {
		// A magnet has no file tree to select from at this point in the flow
		// (see torrent.Resolver.Describe's own comment: "nobody knows them yet") -
		// it stages through the ordinary /api/links paste path, which is already
		// wired and needs nothing from this route. Keeping that split explicit
		// here, rather than silently accepting a magnet and ignoring the
		// selection field, is what stops a caller from believing a selection was
		// applied when it was not.
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
