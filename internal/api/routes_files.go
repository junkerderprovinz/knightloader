package api

// Streaming a task's own file. See internal/app/app_files.go for the part
// that matters: this handler never joins a path itself, it only asks
// SafeTaskFile for one and serves exactly what comes back.
//
// THE ALLOWLIST IS THE OTHER HALF OF THE SECURITY CHECK. This route serves
// bytes a hoster chose, not bytes this app wrote, so the Content-Type it
// answers with can never come from the file, the resolver, or the request:
// all three are somebody else's word. inlineTypes is the one list this route
// trusts, keyed on the extension already stored on the task, and it excludes
// every type a browser can execute rather than merely display - most pointedly
// HTML, SVG and XML, any one of which served inline at this app's own origin
// would run with this app's own session live in the tab. Anything not on the
// list is offered as attachment, never as inline with a guessed type.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// inlineTypes is what this route will show in the tab rather than hand the
// browser a save dialog for: media a browser only ever displays or plays,
// plus plain text, and nothing a browser can execute. Extending it needs the
// same question asked again - "can this run as active content at this app's
// origin" - not just a missing extension filled in.
var inlineTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".avif": "image/avif",

	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".wav":  "audio/wav",
	".flac": "audio/flac",
	".ogg":  "audio/ogg",

	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mkv":  "video/x-matroska",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".m4v":  "video/mp4",

	".pdf": "application/pdf",

	// text/plain rather than each format's own registered type (text/csv,
	// application/json): the point of inline here is "show it as text", and a
	// browser's own handler for those more specific types is exactly what a
	// download manager's users are trying to get past when they open an .nfo
	// instead of downloading it.
	".txt": "text/plain; charset=utf-8",
	".nfo": "text/plain; charset=utf-8",
	".log": "text/plain; charset=utf-8",
	".csv": "text/plain; charset=utf-8",
	".srt": "text/plain; charset=utf-8",
	".vtt": "text/vtt; charset=utf-8",
}

func registerFiles(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/tasks/{id}/file",
		"stream a task's own file from disk; inline for an allowlisted type, a download prompt for everything else",
		func(w http.ResponseWriter, r *http.Request) {
			serveTaskFile(w, r, a, r.PathValue("id"))
		})
}

func serveTaskFile(w http.ResponseWriter, r *http.Request, a *app.App, id string) {
	tf, err := a.SafeTaskFile(id)
	if err != nil {
		http.Error(w, err.Error(), taskFileStatus(err))
		return
	}
	// Opened by the path SafeTaskFile already resolved and confirmed, and
	// never re-joined here: a second filepath.Join at this layer is a second
	// chance to get the one check that matters wrong.
	f, err := os.Open(tf.Path)
	if err != nil {
		http.Error(w, "could not open the file", http.StatusNotFound)
		return
	}
	defer f.Close()

	ctype, inline := inlineType(tf.Name)
	// Both set before ServeContent, which only sniffs when Content-Type is
	// still empty - setting it ourselves first is what keeps this route from
	// ever trusting the file's own bytes to say what they are.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", ctype)
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename="+quoteFilename(tf.Name))
	// A zero modtime skips Last-Modified/If-Modified-Since entirely, which
	// matters for a running task: its file is still growing, and a 304 built
	// from a modtime taken minutes ago would answer "unchanged" about a file
	// that has gained another gigabyte since. Range requests still work -
	// ServeContent measures Content-Length itself by seeking this same
	// handle, so a partial download reports exactly the bytes it has right
	// now rather than the task's own expected total.
	http.ServeContent(w, r, "", time.Time{}, f)
}

// inlineType is the Content-Type this route answers with and whether it goes
// out as inline: an allowlisted extension's own type, or attachment/
// octet-stream for everything else. The extension comes from the task's own
// stored name, never from the request or from sniffing the file.
func inlineType(name string) (contentType string, inline bool) {
	ext := strings.ToLower(filepath.Ext(name))
	if ct, ok := inlineTypes[ext]; ok {
		return ct, true
	}
	return "application/octet-stream", false
}

// taskFileStatus maps a SafeTaskFile refusal to the status a client can act
// on: 404 for "there is nothing here yet", 400 for "not this app's file to
// serve", 403 for the one refusal that means somebody's stored path tried to
// leave its own folder.
func taskFileStatus(err error) int {
	switch {
	case errors.Is(err, app.ErrTaskFileNotFound), errors.Is(err, app.ErrTaskFileNoBytes):
		return http.StatusNotFound
	case errors.Is(err, app.ErrTaskFileNotLocal):
		return http.StatusBadRequest
	case errors.Is(err, app.ErrTaskFileEscape):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
