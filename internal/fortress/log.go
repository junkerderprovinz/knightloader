package fortress

import (
	"crypto/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

// Log keeps the latest events for other members of the fortress to read. It
// lives in memory: what a reader missed while the process was down is not
// news by the time it comes back.
type Log struct {
	// run starts every cursor, so a cursor from before a restart reads as
	// unknown instead of skipping the first entries of this run.
	run string

	mu      sync.Mutex
	seq     uint64
	entries []logEntry
	start   int
	size    int
}

type logEntry struct {
	seq   uint64
	at    time.Time
	level status.Severity
	code  string
	key   string
	args  map[string]string
	link  string
}

// NewLog returns a log that keeps the newest size entries.
func NewLog(size int) *Log {
	return &Log{run: rand.Text()[:8], size: max(size, 1)}
}

// On takes one event from the bus. It only appends, so it returns at once.
func (l *Log) On(f script.Firing) {
	e, ok := describe(f)
	if !ok {
		return
	}
	e.at = f.At
	if e.at.IsZero() {
		e.at = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	e.seq = l.seq
	if len(l.entries) < l.size {
		l.entries = append(l.entries, e)
		return
	}
	l.entries[l.start] = e
	l.start = (l.start + 1) % l.size
}

// Page returns the entries after q.After, oldest first, in q.Lang.
func (l *Log) Page(q status.LogQuery) status.LogPage {
	after := l.position(q.After)
	l.mu.Lock()
	var picked []logEntry
	for i := range l.entries {
		e := l.entries[(l.start+i)%len(l.entries)]
		if e.seq <= after {
			continue
		}
		if len(picked) == q.Limit {
			break
		}
		picked = append(picked, e)
	}
	l.mu.Unlock()

	page := status.LogPage{Entries: make([]status.LogEntry, len(picked)), Next: q.After}
	for i, e := range picked {
		id := l.run + "-" + strconv.FormatUint(e.seq, 10)
		page.Entries[i] = status.LogEntry{ID: id, At: e.at.UTC(), Level: e.level, Code: e.code, Text: say(q.Lang, e.key, e.args), Link: e.link}
		page.Next = id
	}
	return page
}

// position is the sequence number a cursor stands at, 0 for the start.
func (l *Log) position(cursor string) uint64 {
	run, seq, ok := strings.Cut(cursor, "-")
	if !ok || run != l.run {
		return 0
	}
	n, err := strconv.ParseUint(seq, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// describe puts an event into the log's terms. An added link is left out: one
// pasted container would push everything else out of the log.
func describe(f script.Firing) (logEntry, bool) {
	e := logEntry{level: status.SeverityInfo, link: "/downloads"}
	named := func(name string) map[string]string { return map[string]string{"name": name} }
	switch {
	case f.Trigger == script.TriggerTaskDone && f.Task != nil:
		e.code, e.key, e.args = "download.done", "fortress.log.taskDone", named(taskName(f.Task))
	case f.Trigger == script.TriggerTaskFailed && f.Task != nil:
		e.level, e.code, e.key, e.args = status.SeverityError, "download.failed", "fortress.log.taskFailed", named(taskName(f.Task))
	case f.Trigger == script.TriggerChecksumFailed && f.Task != nil:
		e.level, e.code, e.key, e.args = status.SeverityError, "checksum.failed", "fortress.log.checksumFailed", named(taskName(f.Task))
	case f.Trigger == script.TriggerQueueIdle:
		e.code, e.key = "queue.idle", "fortress.log.queueIdle"
	case f.Trigger == script.TriggerPackageDone && f.Package != nil:
		e.code, e.key, e.args = "package.done", "fortress.log.packageDone", named(f.Package.Name)
		if f.Package.Failed > 0 {
			e.level, e.code, e.key = status.SeverityWarn, "package.failed", "fortress.log.packageFailed"
			e.args["n"] = strconv.Itoa(f.Package.Failed)
		}
	case f.Trigger == script.TriggerExtractDone && f.Extract != nil:
		name := f.Extract.Name
		if f.Extract.Package != "" {
			name = f.Extract.Package
		}
		e.code, e.key, e.args = "unpack.done", "fortress.log.extractDone", named(name)
		if !f.Extract.OK {
			e.level, e.code, e.key = status.SeverityError, "unpack.failed", "fortress.log.extractFailed"
		}
	case f.Trigger == script.TriggerReconnectDone && f.Reconnect != nil:
		e.code, e.link = "reconnect.done", "/settings/network"
		switch {
		case !f.Reconnect.OK:
			e.level, e.code, e.key = status.SeverityWarn, "reconnect.failed", "fortress.log.reconnectFailed"
		case f.Reconnect.Changed:
			e.key = "fortress.log.reconnectChanged"
		default:
			e.key = "fortress.log.reconnectSame"
		}
	case f.Trigger == script.TriggerAccountExpired && f.Account != nil:
		name := f.Account.Label
		if name == "" {
			name = f.Account.Service
		}
		e.level, e.code, e.key, e.args, e.link = status.SeverityWarn, "account.expired", "fortress.log.accountExpired", named(name), "/accounts"
	case f.Trigger == script.TriggerCaptchaPending && f.Captcha != nil:
		e.level, e.code, e.key, e.args, e.link = status.SeverityWarn, "captcha.waiting", "fortress.log.captchaPending", map[string]string{"host": f.Captcha.Host}, "/"
	default:
		return e, false
	}
	return e, true
}

func taskName(t *script.TaskView) string {
	if t.Name != "" {
		return t.Name
	}
	return t.URL
}
