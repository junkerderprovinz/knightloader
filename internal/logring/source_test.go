package logring

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTrimStampFindsTheBodyOfTheLine. Every line in the ring arrives already
// formatted by the standard logger, so a prefix table applied to the raw line
// would match nothing at all - forever, and while looking perfectly reasonable
// in review.
func TestTrimStampFindsTheBodyOfTheLine(t *testing.T) {
	cases := []struct{ line, want string }{
		{"2026/09/08 14:18:22 task 00112233445566aa moved", "task 00112233445566aa moved"},
		{"2026/09/08 14:18:22.123456 feed https://x: refused", "feed https://x: refused"},
		// Not a stamp, so not touched: a line whose own text opens with two
		// words must keep both of them.
		{"relay reconnected after 3 tries", "relay reconnected after 3 tries"},
		{"2026/09/08 not a time at all", "2026/09/08 not a time at all"},
		{"", ""},
		{"short", "short"},
	}
	for _, c := range cases {
		if got := TrimStamp(c.line); got != c.want {
			t.Errorf("TrimStamp(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// TestSourceOfReadsTheBeginningOfTheLine, stamp and all.
func TestSourceOfReadsTheBeginningOfTheLine(t *testing.T) {
	cases := []struct{ line, want string }{
		{"2026/09/08 14:18:22 task 00112233445566aa could not be moved", "task"},
		{"2026/09/08 14:18:22 checksum big.mkv: bad hash", "checksum"},
		{"2026/09/08 14:18:22 crawl https://x: refused", "crawl"},
		{"2026/09/08 14:18:22 following https://a, https://b", "feed"},
		{"2026/09/08 14:18:22 jd: container crawl had not settled", "JD"},
		{"2026/09/08 14:18:22 KL_JD set but JD unreachable", "JD"},
		// A line about a task that does not START with the word is honestly
		// filed under nothing, because the source is read off the beginning of
		// the line and this one begins somewhere else.
		{"2026/09/08 14:18:22 reconnect after task 00112233445566aa hit a limit", ""},
		{"2026/09/08 14:18:22 could not read the task list returned by /api/links", ""},
		{"2026/09/08 14:18:22 the volume cap is reached", ""},
	}
	for _, c := range cases {
		if got := SourceOf(c.line); got != c.want {
			t.Errorf("SourceOf(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// TestSourcesIsACopy. The table is handed out over HTTP, and a caller that
// could reach the package's own slice through the response could reorder the
// picker for every request after it.
func TestSourcesIsACopy(t *testing.T) {
	got := Sources()
	if len(got) == 0 {
		t.Fatal("Sources() is empty")
	}
	first := got[0]
	got[0] = "tampered"
	if again := Sources(); again[0] != first {
		t.Errorf("mutating the returned slice changed the table: %q", again[0])
	}
}

// TestTaskIDIsAnchoredToTheWord is trap 12 of this feature written down as a
// test. A task id is sixteen hex characters and so is half of what this app
// logs - a truncated checksum, a torrent infohash, the first block of a JD
// package uuid. Matching the shape alone would file all of them under whichever
// download happened to share the digits, which is worse than finding nothing.
func TestTaskIDIsAnchoredToTheWord(t *testing.T) {
	const id = "00112233445566aa"
	found := []string{
		"2026/09/08 14:18:22 task " + id + " could not be moved out of the working folder: denied",
		"2026/09/08 14:18:22 reconnect after task " + id + " hit a limit: none left",
		"2026/09/08 14:18:22 task " + id + ": the account behind ddl is unavailable",
		"task " + id,
	}
	for _, line := range found {
		if got := TaskIDOf(line); got != id {
			t.Errorf("TaskIDOf(%q) = %q, want %q", line, got, id)
		}
	}

	none := []string{
		// A checksum in the leading position, which app_tasks.go really logs.
		"2026/09/08 14:18:22 checksum " + id + ": bad hash",
		// A resolver id in the leading position, which app_tasks.go also logs.
		"2026/09/08 14:18:22 " + id + " could not check 4 links",
		// Longer than an id: a full sha1, not a task.
		"2026/09/08 14:18:22 task " + id + "deadbeef is not an id",
		// Shorter than an id.
		"2026/09/08 14:18:22 task 00112233 is not an id",
		// Not hex.
		"2026/09/08 14:18:22 task zzzzzzzzzzzzzzzz",
		// The phrase this app uses about the LIST rather than about one task.
		"2026/09/08 14:18:22 could not read the task list returned by /api/links",
		// The word inside another word.
		"2026/09/08 14:18:22 subtask " + id + " finished",
	}
	for _, line := range none {
		if got := TaskIDOf(line); got != "" {
			t.Errorf("TaskIDOf(%q) = %q, want no match at all", line, got)
		}
	}
}

// logCall matches a logging call whose format string is a literal, which is
// every one of them in this tree.
var logCall = regexp.MustCompile(`\blog\.(?:Printf|Println|Print|Fatalf|Fatalln|Fatal)\("((?:[^"\\]|\\.)*)"`)

// TestEveryBucketMatchesSomethingTheTreeActuallyLogs is what keeps the table
// from becoming decoration.
//
// The prefixes were read OFF this tree, and a table read off a tree drifts the
// moment somebody rewords a line: the picker then offers a bucket that silently
// matches nothing, which looks exactly like a subsystem that had a quiet day.
// So the source of every log call in the repository is scanned for a literal
// format string, and each bucket has to claim at least one of them.
//
// It scans the format strings and not the ring, deliberately: half of these
// lines are only ever logged when something has gone wrong on somebody else's
// machine, and a test that waited for them would test nothing.
func TestEveryBucketMatchesSomethingTheTreeActuallyLogs(t *testing.T) {
	root := filepath.Join("..", "..")
	claimed := map[string]int{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "web", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, m := range logCall.FindAllStringSubmatch(string(b), -1) {
			if name := SourceOf(m[1]); name != "" {
				claimed[name]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A scan that reads nothing is a check that passes for the wrong reason -
	// the same guard the settings-search script makes on its own fixtures.
	total := 0
	for _, n := range claimed {
		total += n
	}
	if total < 40 {
		t.Fatalf("only %d log call sites were matched in the whole tree; the scan is wrong, not the table", total)
	}

	for _, s := range sources {
		if claimed[s.name] == 0 {
			t.Errorf("the source picker offers %q but nothing in this tree logs a line starting with any of %v - "+
				"a bucket that matches nothing looks exactly like a subsystem that had a quiet day", s.name, s.prefixes)
		}
	}
}
