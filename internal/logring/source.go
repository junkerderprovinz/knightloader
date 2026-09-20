package logring

// Which part of the app a line came from, and which download it names.
//
// A source filter and not a level picker, because there are no levels in this
// tree: nothing imports log/slog, every call site is a bare log.Printf against
// the standard logger, and log.SetFlags is never called outside two test
// files. A picker offering Info, Warn and Error would have to guess a level
// out of the wording, and a guess dressed as a level hides lines from whoever
// trusts it. What the lines do carry is a prefix naming the subsystem, so that
// is what the page filters on, and the table below is read off the tree.
//
// Matched by literal prefix rather than parsed. Splitting on ": " lands in the
// middle of a value more often than not: "checksum foo.mkv: bad" and "crawl
// https://x: refused" both carry a colon-space where a parser would take the
// head. A prefix table is auditable against the source and wrong in a visible
// way.
//
// The table lives on the server because the lines do. A copy in the frontend
// would drift the first time somebody renamed a prefix, and show up as a
// filter that matches nothing.

import "strings"

// source is one bucket: the name the page shows, and every line prefix that
// belongs to it.
type source struct {
	name     string
	prefixes []string
}

// sources is the fixed, ordered table. The order is what the dropdown shows,
// roughly what somebody is most likely to be chasing rather than alphabetical,
// which would put "account health" above "task".
var sources = []source{
	{"task", []string{"task "}},
	{"feed", []string{"feed ", "feed subscription", "following ", "no feed could be polled"}},
	{"crawl", []string{"crawl "}},
	{"checksum", []string{"checksum "}},
	{"extraction", []string{"extraction "}},
	{"container", []string{"container "}},
	{"add-links", []string{"add-links"}},
	{"addcrypted", []string{"addcrypted "}},
	{"Click'n'Load", []string{"Click'n'Load "}},
	{"captcha", []string{"captcha"}},
	{"hosterauth", []string{"hosterauth"}},
	{"account health", []string{"account health"}},
	{"connection", []string{"connection "}},
	{"torrent", []string{"torrent "}},
	{"relay", []string{"relay"}},
	{"bridge", []string{"bridge"}},
	{"browsertools", []string{"browsertools"}},
	{"JD", []string{"JD ", "jd: ", "KL_JD "}},
	{"script", []string{"script:"}},
	{"backup", []string{"backup"}},
	{"reclaim", []string{"reclaim:"}},
	{"retention", []string{"retention:"}},
	{"idle action", []string{"idle action"}},
	{"speed history", []string{"speed history"}},
	{"desktop", []string{"desktop:"}},
	{"shutdown", []string{"shutdown", "shutting down "}},
}

// Sources is the bucket names in the order the picker offers them.
//
// It is a copy, because it is JSON-encoded straight into a response and the
// table itself must not be reachable through one.
func Sources() []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.name)
	}
	return out
}

// SourceOf is the bucket a line belongs to, or "" for one that names no part
// of the app. An empty answer is a real one: the page shows those lines under
// "Everything else" rather than filing them under the first bucket.
func SourceOf(line string) string {
	body := TrimStamp(line)
	for _, s := range sources {
		for _, p := range s.prefixes {
			if strings.HasPrefix(body, p) {
				return s.name
			}
		}
	}
	return ""
}

// TrimStamp removes the standard library's own date and time from the front of
// a line.
//
// This is what makes the prefixes in the table above match at all. The ring is
// fed by log.SetOutput, which hands it the formatted record, so every line in
// the buffer begins "2026/09/08 14:18:22 " and a prefix table applied to the
// raw line would match nothing.
//
// Written as an exact shape check rather than a regexp or a cut at the second
// space, which would take two words off a line that carries no stamp.
// Microseconds are tolerated because log.Lmicroseconds is one flag away and a
// test file in this tree changes the flags.
func TrimStamp(line string) string {
	const stamp = "2006/01/02 15:04:05"
	if len(line) < len(stamp)+1 {
		return line
	}
	for i, c := range []byte(stamp) {
		got := line[i]
		switch c {
		case '/', ':', ' ':
			if got != c {
				return line
			}
		default:
			if got < '0' || got > '9' {
				return line
			}
		}
	}
	rest := line[len(stamp):]
	if strings.HasPrefix(rest, ".") {
		i := 1
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		rest = rest[i:]
	}
	if !strings.HasPrefix(rest, " ") {
		return line
	}
	return strings.TrimLeft(rest, " ")
}

// TaskIDOf is the download a line names, or "" for one that names none.
//
// Anchored to the word, never to the shape of the id. A task id is sixteen hex
// characters (internal/app.newID), and so is half of what this app logs: a
// truncated checksum, a torrent infohash, a JD package uuid's first block. A
// bare hex match would file all of them under whichever download had the same
// digits. The literal "task " in front is what every call site that records an
// id writes.
//
// It matches mid-line as well as at the start, because "reconnect after task
// <id> hit a limit" is about a task without being filed under one.
//
// Most lines name no task at all, and the per-download panel says so rather
// than letting an empty card read as a broken one.
func TaskIDOf(line string) string {
	const word = "task "
	rest := line
	for {
		i := strings.Index(rest, word)
		if i < 0 {
			return ""
		}
		// Not preceded by a letter, so that "subtask 0011..." is not read as a
		// task named by a line that says nothing of the sort.
		if i == 0 || !isWordByte(rest[i-1]) {
			if id, ok := hexID(rest[i+len(word):]); ok {
				return id
			}
		}
		rest = rest[i+len(word):]
	}
}

// taskIDLen is the length of an id from internal/app.newID: eight random bytes
// as hex.
const taskIDLen = 16

// hexID reads exactly taskIDLen lowercase hex characters off the front of s,
// and requires that whatever follows is not a seventeenth. A longer run is a
// checksum, not an id.
func hexID(s string) (string, bool) {
	if len(s) < taskIDLen {
		return "", false
	}
	for i := 0; i < taskIDLen; i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	if len(s) > taskIDLen && isWordByte(s[taskIDLen]) {
		return "", false
	}
	return s[:taskIDLen], true
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
