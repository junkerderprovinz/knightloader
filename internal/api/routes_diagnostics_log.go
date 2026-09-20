package api

// The log, as four questions: what has been logged lately (with a cursor, so a
// follow view can ask only for what is new), whether any of it is being kept on
// disk, one of those files as a download, and what this instance has said about
// one particular download.
//
// All four sit under /api/diagnostics for security, not filing. The obvious
// address for the last of them, /api/tasks/{id}/log, falls inside the "tasks/"
// prefix that routes_relay.go and routes_federation.go both forward. Those
// allowlists rest on the relay phrase already granting the queue and the task
// list; log lines are neither. internal/feed's poller logs subscription
// addresses verbatim, and a private indexer's feed URL carries its API key in
// the query string, the same way internal/crawler logs the pages it walks.
//
// routes_federation.go also sets Content-Type: application/json on everything
// it forwards, so the download route would be served to a browser as broken
// JSON if it ever drifted into a forwardable prefix.
//
// The per-download log card therefore draws nothing for a task running on a
// peer: the log stays on the instance that wrote it.

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/logring"
)

// logLine is one line as the server read it. Source and TaskID are computed
// here rather than in the browser: the prefixes and the "task <id>" rule
// describe lines this tree writes, and a second copy across the wire would
// drift into a filter that matches nothing the first time a log call is
// reworded.
type logLine struct {
	Seq    uint64 `json:"seq"`
	Line   string `json:"line"`
	Source string `json:"source,omitempty"`
	TaskID string `json:"taskId,omitempty"`
}

// logTail is what a follow view asks for every couple of seconds.
type logTail struct {
	Entries []logLine `json:"entries"`
	// Dropped is how many lines the memory buffer threw away between this poll
	// and the last one. Non-zero means the view has a hole in it and has to say
	// so out loud rather than joining the two halves silently.
	Dropped  int      `json:"dropped"`
	Newest   uint64   `json:"newest"`
	Capacity int      `json:"capacity"`
	Sources  []string `json:"sources"`
}

// taskLog is every line this instance has logged that names one download.
type taskLog struct {
	Lines []logLine `json:"lines"`
	// Scanned is where the lines were looked for, always "memory": the
	// alternative is reading up to a gigabyte of rotated files off an array
	// volume every time somebody double-clicks a row, which is what opens the
	// panel that calls this.
	Scanned string `json:"scanned"`
	// Partial is true, and the page says so. Only a few of this tree's log call
	// sites record which download they are about, so a download can run into
	// real trouble without a single line appearing here.
	Partial bool `json:"partial"`
}

func registerDiagnosticsLog(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/diagnostics/log",
		"the process's own recent log lines with a cursor, so a follow view can ask only for what is new",
		func(w http.ResponseWriter, r *http.Request) {
			since := parseSeq(r.URL.Query().Get("since"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			entries, dropped, newest := logring.Since(since, limit)
			out := logTail{
				Entries:  make([]logLine, 0, len(entries)),
				Dropped:  dropped,
				Newest:   newest,
				Capacity: logring.Capacity,
				Sources:  logring.Sources(),
			}
			for _, e := range entries {
				out.Entries = append(out.Entries, describeLine(e))
			}
			writeJSON(w, out)
		})

	reg.Add(http.MethodGet, "/api/diagnostics/logfile",
		"whether the log is being written to disk, where, how big it has grown, and what to try when it is not",
		func(w http.ResponseWriter, r *http.Request) {
			// The real path, unlike the redacted copy in the downloadable
			// bundle: this route is session-guarded and reaches somebody
			// already looking at their own settings pages.
			st := logring.FileStatus()
			if st.Path == "" {
				// With nothing armed the sink has no path, and this is when
				// somebody wants to know where the file would go. Taken from
				// the function the arming itself uses, so the card cannot show
				// a folder the sink would not pick.
				st.Path = filepath.Join(a.LogDir(), logring.Name)
			}
			writeJSON(w, st)
		})

	reg.Add(http.MethodGet, "/api/diagnostics/logfile/{gen}",
		"download one log file; 0 is the one being written, 1 the newest renamed one",
		func(w http.ResponseWriter, r *http.Request) {
			raw := r.PathValue("gen")
			gen, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "the generation has to be a number: 0 for the file being written, 1 for the newest renamed one", http.StatusBadRequest)
				return
			}
			// Range-checked against what the sink actually keeps, and never
			// joined into a path here. The number arrives from a URL, and the
			// one place that turns it into a file name is the sink that owns
			// the names.
			path, ok := logring.GenerationPath(gen)
			if !ok {
				http.Error(w, "there is no log file "+raw+" on this instance", http.StatusNotFound)
				return
			}
			f, err := os.Open(path)
			if err != nil {
				http.Error(w, "there is no log file "+raw+" on this instance", http.StatusNotFound)
				return
			}
			defer f.Close()

			name := "knightloader.log"
			if gen > 0 {
				name = "knightloader." + raw + ".log"
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
			// Too late for http.Error once this starts, as in
			// routes_backup.go, and not logged either: a line per aborted
			// download is how a log fills up.
			_, _ = io.Copy(w, f)
		})

	reg.Add(http.MethodGet, "/api/diagnostics/task/{id}",
		"the log lines that name this download by its id; most lines name none, so this is never the whole story",
		func(w http.ResponseWriter, r *http.Request) {
			id := strings.TrimSpace(r.PathValue("id"))
			out := taskLog{Lines: []logLine{}, Scanned: "memory", Partial: true}
			if id == "" {
				writeJSON(w, out)
				return
			}
			entries, _, _ := logring.Since(0, 0)
			for _, e := range entries {
				if logring.TaskIDOf(e.Line) != id {
					continue
				}
				out.Lines = append(out.Lines, describeLine(e))
			}
			writeJSON(w, out)
		})
}

// describeLine is the one place a line is turned into what the page draws, so
// the tail and the per-download view can never disagree about which download a
// line names.
func describeLine(e logring.Entry) logLine {
	return logLine{
		Seq:    e.Seq,
		Line:   e.Line,
		Source: logring.SourceOf(e.Line),
		TaskID: logring.TaskIDOf(e.Line),
	}
}

// parseSeq reads a cursor, treating anything unreadable as "start from the
// beginning" rather than as a refusal. A follow view sends back whatever the
// last response gave it, so a bad value means a hand-typed URL or a client
// that lost its place, and starting over is what either wants.
func parseSeq(raw string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
