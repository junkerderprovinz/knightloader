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
//
// A file that is still downloading is served all the same, at its full size.
// Its bytes come from the engine, a read waits for the ones that have not
// arrived, and the download fetches what is read first, so a player can start
// and seek before the download is done (see app.OpenTaskFile).

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// streamWait is how long one read of a file that is still downloading waits
// for its bytes. Then the response ends short, and a player asks again from
// where it stopped.
const streamWait = 90 * time.Second

// playTicketTTL is how long a link from the play route opens the file. It has
// to outlast a long film paused halfway, and it opens that one file only.
const playTicketTTL = 12 * time.Hour

// maxPlayRange is the most one call to the range route answers with, well
// inside what a relay answer may hold.
const maxPlayRange = 4 << 20

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
		"stream a task's own file, or with ?file= one file of its torrent; inline for an allowlisted type, a download prompt for everything else. "+
			"A file still downloading is served at its full size, and a read waits for its bytes, which the download then fetches first",
		func(w http.ResponseWriter, r *http.Request) {
			serveTaskFile(w, r, a, r.PathValue("id"))
		})
	reg.Add(http.MethodPost, "/api/tasks/{id}/play",
		"a link to the task's file, or to the file of its torrent that plays, that a media player can open without the session or token, for twelve hours or until the token that asked for it is revoked or every session is signed out",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			// Opened once to learn which file plays and to refuse what will
			// not, before a player is handed a link it cannot use.
			of, err := a.OpenTaskFile(id, -1)
			if err != nil {
				http.Error(w, err.Error(), taskFileStatus(err))
				return
			}
			size, err := of.File.Seek(0, io.SeekEnd)
			_ = of.File.Close()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			file := ""
			if of.Index >= 0 {
				file = strconv.Itoa(of.Index)
			}
			by := ""
			if tok, ok := tokenOf(r); ok {
				by = tok.ID
			}
			if c, ok := memberOf(r); ok {
				by = memberTicket + c.ID
			}
			path := "/api/tasks/" + id + "/file?ticket=" + playTicket(id, file, by, a.Auth.Epoch(), time.Now().Add(playTicketTTL))
			if file != "" {
				path += "&file=" + file
			}
			ctype, _ := inlineType(of.Name)
			writeJSON(w, map[string]any{"path": path, "size": size, "type": ctype, "file": file})
		})
	reg.Add(http.MethodGet, "/api/tasks/{id}/range",
		"length bytes of a task's file from from on, or with ?file= of one file of its torrent, as a 200 that is never longer than a relay answer may be. "+
			"The phone app plays a file through it, over the relay and as a stream on the local network, neither of which carries a Range header",
		func(w http.ResponseWriter, r *http.Request) {
			serveTaskRange(w, r, a, r.PathValue("id"))
		})
}

// serveTaskRange answers the range route.
func serveTaskRange(w http.ResponseWriter, r *http.Request, a *app.App, id string) {
	q := r.URL.Query()
	from, err1 := strconv.ParseInt(q.Get("from"), 10, 64)
	length, err2 := strconv.ParseInt(q.Get("length"), 10, 64)
	if err1 != nil || err2 != nil || from < 0 || length <= 0 {
		http.Error(w, "from and length must say which bytes to send", http.StatusBadRequest)
		return
	}
	index := -1
	if v := q.Get("file"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, "file must be the index of a file of the torrent", http.StatusBadRequest)
			return
		}
		index = n
	}
	of, err := a.OpenTaskFile(id, index)
	if err != nil {
		http.Error(w, err.Error(), taskFileStatus(err))
		return
	}
	defer of.File.Close()
	size, err := of.File.Seek(0, io.SeekEnd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if from >= size {
		http.Error(w, "the file ends before from", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	n := min(length, size-from, maxPlayRange)
	if _, err := of.File.Seek(from, io.SeekStart); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var content io.Reader = of.File
	if of.Live {
		content = waitingReader{ctx: r.Context(), f: of.File, wait: streamWait}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	rc := http.NewResponseController(w)
	defer context.AfterFunc(r.Context(), func() { _ = rc.SetWriteDeadline(time.Now()) })()
	_, _ = io.CopyN(w, content, n)
}

func serveTaskFile(w http.ResponseWriter, r *http.Request, a *app.App, id string) {
	index := -1
	if v := r.URL.Query().Get("file"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, "file must be the index of a file of the torrent", http.StatusBadRequest)
			return
		}
		index = n
	}
	of, err := a.OpenTaskFile(id, index)
	if err != nil {
		http.Error(w, err.Error(), taskFileStatus(err))
		return
	}
	defer of.File.Close()

	ctype, inline := inlineType(of.Name)
	// Both set before ServeContent, which sniffs only when Content-Type is
	// still empty, so the file's own bytes never get to say what they are.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", ctype)
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename="+quoteFilename(of.Name))
	// A zero modtime skips Last-Modified and If-Modified-Since. A running
	// task's file is still changing, and a 304 built from a modtime taken
	// minutes ago would answer "unchanged" about a file that has gained
	// another gigabyte. ServeContent measures Content-Length by seeking, which
	// gives the full size for a file the engine streams.
	var content io.ReadSeeker = of.File
	if of.Live {
		content = waitingReader{ctx: r.Context(), f: of.File, wait: streamWait}
	}
	// A paused player stops reading and leaves the connection open, so a
	// write can block until the player goes on. Once the request is over,
	// which a shutdown also makes it, that write gives up.
	rc := http.NewResponseController(w)
	defer context.AfterFunc(r.Context(), func() { _ = rc.SetWriteDeadline(time.Now()) })()
	http.ServeContent(w, r, "", time.Time{}, content)
}

// waitingReader gives each read of a file that is still downloading wait to
// find its bytes.
type waitingReader struct {
	ctx  context.Context
	f    app.StreamFile
	wait time.Duration
}

func (r waitingReader) Read(p []byte) (int, error) {
	ctx, cancel := context.WithTimeout(r.ctx, r.wait)
	defer cancel()
	return r.f.ReadContext(ctx, p)
}

func (r waitingReader) Seek(offset int64, whence int) (int64, error) {
	return r.f.Seek(offset, whence)
}

// playKey signs the play links of this process; a restart ends them all.
var playKey = sync.OnceValue(func() []byte {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return key
})

// memberTicket opens the "by" of a play link a member of the group asked for,
// before its member id. An API token's id never starts with it.
const memberTicket = "member-"

// playTicket is the credential in a play link: when it runs out, who asked for
// it (an API token, a member of the group, or empty for a session), and a MAC
// over both, the task, the link's ?file (empty for the task's own file) and
// the session epoch. A lost phone's links then end with its token or with its
// removal from the group, or when every session is signed out.
func playTicket(id, file, by string, epoch uint64, until time.Time) string {
	exp := strconv.FormatInt(until.Unix(), 36)
	mac := hmac.New(sha256.New, playKey())
	mac.Write([]byte(id + "\x00" + file + "\x00" + by + "\x00" + strconv.FormatUint(epoch, 10) + "\x00" + exp))
	return exp + "." + by + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// playTicketOpens reports whether r reads a task's file with a play link for
// that task and that file that has not run out, from a token that still
// exists. It opens that route and nothing else.
func playTicketOpens(a *app.App, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	id, ok := strings.CutPrefix(r.URL.Path, "/api/tasks/")
	if !ok {
		return false
	}
	id, ok = strings.CutSuffix(id, "/file")
	if !ok || id == "" || strings.Contains(id, "/") {
		return false
	}
	q := r.URL.Query()
	ticket := q.Get("ticket")
	exp, rest, ok := strings.Cut(ticket, ".")
	if !ok {
		return false
	}
	by, _, ok := strings.Cut(rest, ".")
	if !ok {
		return false
	}
	until, err := strconv.ParseInt(exp, 36, 64)
	if err != nil || time.Now().Unix() > until {
		return false
	}
	if member, ok := strings.CutPrefix(by, memberTicket); ok {
		if !a.Federation.StillMember(member) {
			return false
		}
	} else if by != "" && !slices.ContainsFunc(a.APITokens.List(), func(t apitoken.Token) bool { return t.ID == by }) {
		return false
	}
	return hmac.Equal([]byte(ticket), []byte(playTicket(id, q.Get("file"), by, a.Auth.Epoch(), time.Unix(until, 0))))
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
// on: 404 for "there is nothing here yet", 409 for a download that has to run
// again first, 503 for one that plays once it has fetched a missing part, 400
// for "not this app's file to serve", 403 for the one refusal that means
// somebody's stored path tried to leave its own folder.
func taskFileStatus(err error) int {
	switch {
	case errors.Is(err, app.ErrTaskFileNotFound), errors.Is(err, app.ErrTaskFileNoBytes), errors.Is(err, app.ErrTaskFileNoSuchFile),
		errors.Is(err, app.ErrTaskFileNoMedia):
		return http.StatusNotFound
	case errors.Is(err, app.ErrTaskFileIncomplete):
		return http.StatusConflict
	case errors.Is(err, app.ErrTaskFileMending):
		return http.StatusServiceUnavailable
	case errors.Is(err, app.ErrTaskFileNotLocal):
		return http.StatusBadRequest
	case errors.Is(err, app.ErrTaskFileEscape):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
