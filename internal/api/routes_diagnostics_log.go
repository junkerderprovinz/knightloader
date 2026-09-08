package api

// The log, as four questions: what has been logged lately (with a cursor, so a
// follow view can ask only for what is new), whether any of it is being kept on
// disk, one of those files as a download, and what this instance has said about
// one particular download.
//
// ALL FOUR SIT UNDER /api/diagnostics, AND THAT IS A SECURITY DECISION RATHER
// THAN A FILING ONE.
//
// The obvious address for the last of them is /api/tasks/{id}/log, and it would
// be wrong. routes_relay.go allows `strings.HasPrefix(rest, "tasks/")` for any
// method, and routes_federation.go forwards the same prefix from a browser.
// Both allowlists rest on one argument, written out at routes_relay.go:473 -
// holding the relay phrase already means being able to drive this instance's
// queue and read its task list, so somebody with that is not learning anything
// new. Log lines are not the task list. internal/feed's poller logs
// subscription addresses verbatim, and a private indexer's feed URL carries its
// API key in the query string; internal/crawler logs the pages it walks the
// same way. Filing the per-download log under tasks/ would widen the relay and
// federation surface silently, past the reasoning those allowlists are built
// on. Under /api/diagnostics it is forwarded nowhere, which matches the
// deliberate decision that a peer's diagnostics are the peer's own.
//
// The download route is a second reason to be here: routes_federation.go sets
// Content-Type: application/json on everything it forwards, so a route that
// answers text/plain and later drifted into a forwardable prefix would be
// served to a browser as broken JSON.
//
// The price of all this is that the per-download log card draws nothing for a
// task running on a peer, and that is the correct answer rather than a gap: the
// log stays on the instance that wrote it.

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

// logLine is one line as the server read it.
//
// Source and TaskID are computed HERE and not in the browser. The prefixes and
// the "task <id>" rule both describe lines this tree writes, so a second copy on
// the other side of the wire would drift the first time somebody rewords a log
// call - and the drift would show up as a filter that quietly matches nothing.
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
	// Scanned is where the lines were looked for. "memory" today, always: the
	// alternative is reading up to a gigabyte of rotated files off an array
	// volume every time somebody double-clicks a row, and the panel that calls
	// this opens on a double-click. The field is here rather than added later
	// because the page prints it, and a page that started printing a new word
	// one day would be a shape change on the wire.
	Scanned string `json:"scanned"`
	// Partial is always true, and the page says so. Seven of this tree's log
	// call sites record which download they are about; the rest do not, so a
	// download can run into real trouble without a single line appearing here.
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
			// The real path, unredacted, unlike the copy the downloadable
			// bundle carries: this route is session-guarded and only ever
			// reaches somebody already looking at their own settings pages,
			// where the path is the single most useful thing on the card.
			st := logring.FileStatus()
			if st.Path == "" {
				// Nothing is armed, so the sink has no path of its own - and
				// this is exactly the moment somebody wants to know where the
				// file WOULD go, because they are deciding whether to switch it
				// on. Answered from the same function the arming itself uses,
				// so the card can never show a folder the sink would not pick.
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
			// Too late for http.Error once this starts, exactly as
			// routes_backup.go's own archive write is - and NOT logged here
			// either, because a copy that died halfway is the client's problem
			// and a log line per aborted download is how a log fills up.
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
// beginning" rather than as a refusal.
//
// A follow view sends whatever the last response gave it, and the one way to
// get a bad value here is a hand-typed URL or a client that has lost its place.
// Answering 400 to that would leave the page with an error instead of a log,
// when starting over is both harmless and exactly what the caller wanted.
func parseSeq(raw string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
