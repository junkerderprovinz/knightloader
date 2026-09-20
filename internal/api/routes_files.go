package api

// Streaming a task's own file. See internal/app/app_files.go for the part
// that matters: this handler never joins a path itself, it only asks
// SafeTaskFile for one and serves exactly what comes back.
//
// The allowlist is the other half of that check. This route serves bytes a
// hoster chose, so the Content-Type can never come from the file, the resolver
// or the request. inlineTypes is keyed on the extension stored on the task and
// excludes every type a browser executes rather than displays: HTML, SVG and
// XML served inline at this app's origin would run with this app's session
// live in the tab. Anything off the list goes out as an attachment, never as
// inline with a guessed type.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// inlineTypes is what this route shows in the tab rather than handing the
// browser a save dialog: media a browser displays or plays, plus plain text.
// Extending it means answering again whether the type can run as active
// content at this app's origin.
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

	// text/plain rather than each format's registered type (text/csv and so
	// on): inline here means "show it as text", and the browser's handler for
	// the more specific types is what somebody opening an .nfo wants to skip.
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
	// Both set before ServeContent, which sniffs only when Content-Type is
	// still empty, so the file's own bytes never get to say what they are.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", ctype)
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename="+quoteFilename(tf.Name))
	// A zero modtime skips Last-Modified and If-Modified-Since. A running
	// task's file is still growing, and a 304 built from a modtime taken
	// minutes ago would answer "unchanged" about a file that has gained
	// another gigabyte. Range requests still work: ServeContent measures
	// Content-Length by seeking this handle, so a partial download reports the
	// bytes it has rather than the task's expected total.
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
