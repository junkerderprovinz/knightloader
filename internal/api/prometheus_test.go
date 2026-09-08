package api

// The exposition encoder. Every test here is about a failure that costs the
// WHOLE scrape rather than one line, or about a number that would be wrong in
// the direction that wakes somebody up.

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// promFixture is a report with every awkward case in it at once: a Windows
// path, a quote inside a folder name, a volume nothing could measure, a part in
// each of the five states, and both breakdown maps populated.
//
// Every value in it is one the app can genuinely produce. The Windows path is
// what the desktop build reports; the quoted folder is a name a filesystem
// accepts on unix; the unmeasurable volume is what internal/diskspace answers on
// a kernel it has no call for.
func promFixture() app.HealthReport {
	return app.HealthReport{
		Status:        app.StateDegraded,
		Version:       "v1.2.3",
		Deployment:    "desktop",
		StartedAt:     time.Unix(1757260000, 0),
		UptimeSeconds: 86400,
		Subsystems: []app.Subsystem{
			{ID: app.SubsystemStore, State: app.StateOK},
			{ID: app.SubsystemQueue, State: app.StateDegraded},
			{ID: app.SubsystemDisk, State: app.StateUnknown},
			{ID: app.SubsystemJD, State: app.StateFailed, Detail: "dial tcp: connection refused"},
			{ID: app.SubsystemYtdlp, State: app.StateUnused},
		},
		Tasks: app.TaskCounts{
			Running: 3, Waiting: 16, Paused: 1, Extracting: 0, Collected: 4, Disabled: 2, Failed: 5,
			WaitingBy: map[string]int{"disk": 12, "slot": 4},
			FailedBy:  map[string]int{"gone": 4, "unknown": 1},
		},
		Volumes: []app.VolumeReport{
			{
				Dir: `C:\Users\x\Downloads`, Measured: `C:\Users\x\Downloads`, Exists: true,
				Known: true, Free: 128849018880, Used: 10, Total: 500107862016,
				Queued: 4096, Tasks: 7, Role: "downloads",
			},
			{
				// A folder name with a double quote in it, which unix
				// filesystems accept and which closes the label early if it is
				// not escaped.
				Dir: `/mnt/the "good" stuff`, Measured: `/mnt/the "good" stuff`, Exists: true,
				Known: true, Free: 1, Used: 2, Total: 3, Queued: 0, Tasks: 0, Role: "category",
			},
			{
				// Nothing could be asked. Its three byte counts are zero and
				// mean NOTHING.
				Dir: "/downloads", Measured: "/downloads", Exists: true,
				Known: false, Queued: 99, Tasks: 1, Role: "work",
			},
		},
		Halted:    true,
		Quiet:     false,
		SampledAt: time.Unix(1757260060, 0),
	}
}

// promLines splits a rendering into lines, dropping the trailing empty one.
func promLines(t *testing.T, body string) []string {
	t.Helper()
	if !strings.HasSuffix(body, "\n") {
		t.Fatalf("the exposition does not end in a newline; the last sample would be unterminated: %q", body)
	}
	return strings.Split(strings.TrimSuffix(body, "\n"), "\n")
}

// metricNameOf is the family a sample line belongs to.
func metricNameOf(line string) string {
	if i := strings.IndexAny(line, "{ "); i >= 0 {
		return line[:i]
	}
	return line
}

// TestAWindowsPathSurvivesTheLabelEscaping is the one that drops an entire
// scrape when it is wrong.
//
// The disk rows carry the folder paths straight out of the settings document
// and the desktop build runs on Windows, so `C:\Users\x\Downloads` lands in a
// label value. One unescaped backslash makes the line unparseable and Prometheus
// discards the WHOLE scrape, not just that series - so a monitoring system would
// report the instance as down because of a folder name.
func TestAWindowsPathSurvivesTheLabelEscaping(t *testing.T) {
	body := prometheusText(promFixture())

	if !strings.Contains(body, `dir="C:\\Users\\x\\Downloads"`) {
		t.Errorf("the Windows path is not escaped in the output:\n%s", body)
	}
	if strings.Contains(body, `dir="C:\U`) {
		t.Error(`a bare backslash reached a label value; the line is unparseable and the scrape is dropped`)
	}
	if !strings.Contains(body, `dir="/mnt/the \"good\" stuff"`) {
		t.Errorf("a quote in a folder name is not escaped, so the label closes early:\n%s", body)
	}

	// The order of the two replacements is the classic way this is written
	// wrong: escaping the quote first turns `"` into `\"` and the backslash pass
	// then doubles the backslash it had just written.
	if got := escapeLabelValue(`a"b\c` + "\nd"); got != `a\"b\\c\nd` {
		t.Errorf("escapeLabelValue = %q, want %q", got, `a\"b\\c\nd`)
	}
	if got := escapeLabelValue(`\`); got != `\\` {
		t.Errorf("a lone backslash escaped to %q", got)
	}
}

// TestAnUnmeasurableVolumeEmitsNoByteSeries is the difference between a report
// and a false alarm at three in the morning.
//
// Known false means Free, Used and Total are 0 and mean NOTHING, which
// VolumeReport says at length and useDiskSpace.ts repeats for the frontend.
// Writing knightloader_disk_free_bytes ... 0 for such a volume hands an alert
// rule a full disk that does not exist, on the exact platform where every disk
// guard in the app is already holding nothing back.
func TestAnUnmeasurableVolumeEmitsNoByteSeries(t *testing.T) {
	body := prometheusText(promFixture())

	if !strings.Contains(body, `knightloader_disk_measurable{role="work",dir="/downloads"} 0`) {
		t.Errorf("the unmeasurable volume does not say so:\n%s", body)
	}
	for _, family := range []string{"knightloader_disk_free_bytes", "knightloader_disk_used_bytes", "knightloader_disk_total_bytes"} {
		for _, line := range promLines(t, body) {
			if strings.HasPrefix(line, family) && strings.Contains(line, `dir="/downloads"`) {
				t.Errorf("%s was written for a volume nothing could measure: %q", family, line)
			}
		}
	}
	// What it DOES keep are the two figures that come from the queue rather
	// than from the filesystem: nothing about those is unknown.
	if !strings.Contains(body, `knightloader_disk_queued_bytes{role="work",dir="/downloads"} 99`) {
		t.Errorf("the queued bytes went missing with the byte series; they come from the queue, not the disk:\n%s", body)
	}
	if !strings.Contains(body, `knightloader_disk_tasks{role="work",dir="/downloads"} 1`) {
		t.Errorf("the task count went missing with the byte series:\n%s", body)
	}
	// And a measurable volume keeps all three, including the one that is
	// genuinely zero.
	if !strings.Contains(body, `knightloader_disk_free_bytes{role="downloads",dir="C:\\Users\\x\\Downloads"} 128849018880`) {
		t.Errorf("a measured volume lost its free bytes:\n%s", body)
	}
}

// TestEveryPartIsEmittedInAllFiveStates. Writing only the state a part is in
// looks tidier and is a trap: when JD recovers, the state="failed" series stops
// being written and Prometheus goes on serving its last value for the whole
// staleness window, so an alert keeps firing for five minutes after the fault
// was fixed.
func TestEveryPartIsEmittedInAllFiveStates(t *testing.T) {
	rep := promFixture()
	body := prometheusText(rep)

	for _, s := range rep.Subsystems {
		ones := 0
		for _, state := range promStates {
			want := `knightloader_subsystem_state{subsystem="` + s.ID + `",state="` + string(state) + `"} `
			var found string
			for _, line := range promLines(t, body) {
				if strings.HasPrefix(line, want) {
					found = strings.TrimPrefix(line, want)
				}
			}
			switch found {
			case "":
				t.Errorf("%s has no series for state %q; an alert on it would go stale rather than clear", s.ID, state)
			case "1":
				ones++
			case "0":
			default:
				t.Errorf("%s/%s = %q, want 0 or 1", s.ID, state, found)
			}
		}
		if ones != 1 {
			t.Errorf("%s reports %d states as 1, want exactly one", s.ID, ones)
		}
	}
	if len(promStates) != 5 {
		t.Errorf("promStates has %d entries; the report answers five", len(promStates))
	}
}

// TestEachFamilyIsDeclaredOnceAndItsSamplesAreContiguous is the format's own
// hard rule. An interleaved family or a second # HELP for one that already has
// it is a parse error, and a parse error is the whole scrape.
func TestEachFamilyIsDeclaredOnceAndItsSamplesAreContiguous(t *testing.T) {
	lines := promLines(t, prometheusText(promFixture()))

	helps, types := map[string]int{}, map[string]int{}
	// closed is a family whose run of samples has already ended: seeing another
	// of its samples afterwards is the interleaving this test exists for.
	closed := map[string]bool{}
	current := ""
	declared := map[string]bool{}

	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "# HELP "):
			name := metricNameOf(strings.TrimPrefix(line, "# HELP "))
			helps[name]++
			if current != "" && current != name {
				closed[current] = true
			}
			current = name
			declared[name] = true
		case strings.HasPrefix(line, "# TYPE "):
			rest := strings.TrimPrefix(line, "# TYPE ")
			name := metricNameOf(rest)
			types[name]++
			if !strings.HasSuffix(rest, " gauge") {
				t.Errorf("line %d declares %s as something other than a gauge: %q - nothing in this report is monotonic", i+1, name, line)
			}
		case strings.TrimSpace(line) == "":
			t.Errorf("line %d is blank; a blank line inside an exposition is not part of the format", i+1)
		default:
			name := metricNameOf(line)
			if !declared[name] {
				t.Errorf("line %d is a sample of %s, which has no # HELP above it: %q", i+1, name, line)
			}
			if closed[name] {
				t.Errorf("line %d puts %s back after its run ended: %q - an interleaved family is a parse error", i+1, name, line)
			}
			if name != current {
				closed[current] = true
				current = name
			}
		}
	}
	for name, n := range helps {
		if n != 1 {
			t.Errorf("%s has %d # HELP lines, want 1", name, n)
		}
		if types[name] != 1 {
			t.Errorf("%s has %d # TYPE lines, want 1", name, types[name])
		}
	}
	if len(helps) < 10 {
		t.Errorf("only %d families were written; the parser is wrong, not the code", len(helps))
	}
}

// TestNoSeriesIsEmittedTwice. A duplicate series - the same name with the same
// labels - is rejected wholesale, so it costs everything else in the scrape as
// well. The one realistic way to write it is a family being emitted from two
// places, which is why this looks at the whole body rather than at one family.
func TestNoSeriesIsEmittedTwice(t *testing.T) {
	seen := map[string]int{}
	for _, line := range promLines(t, prometheusText(promFixture())) {
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Everything up to the last space is the series; what follows is the
		// value, which is allowed to repeat.
		series := line
		if i := strings.LastIndex(line, " "); i >= 0 {
			series = line[:i]
		}
		seen[series]++
	}
	var dupes []string
	for series, n := range seen {
		if n > 1 {
			dupes = append(dupes, series)
		}
	}
	sort.Strings(dupes)
	if len(dupes) > 0 {
		t.Errorf("%d series are written more than once, which makes Prometheus drop the whole scrape: %v", len(dupes), dupes)
	}
}

// TestTwoRendersOfOneReportAreIdentical. Go randomises map iteration, so an
// unsorted walk over the two breakdown maps reorders the samples of a family on
// every scrape. That parses correctly and makes two scrapes impossible to diff
// by eye, which is the first thing anybody does when a number looks wrong.
func TestTwoRendersOfOneReportAreIdentical(t *testing.T) {
	rep := promFixture()
	first := prometheusText(rep)
	for i := 0; i < 20; i++ {
		if got := prometheusText(rep); got != first {
			t.Fatalf("render %d differs from the first:\n%s\n---\n%s", i+2, first, got)
		}
	}
	// And the breakdown really is in key order rather than in whatever order it
	// happened to land in.
	waiting := strings.Index(first, `knightloader_tasks_waiting{reason="disk"} 12`)
	slot := strings.Index(first, `knightloader_tasks_waiting{reason="slot"} 4`)
	if waiting < 0 || slot < 0 || waiting > slot {
		t.Errorf("the waiting breakdown is not in key order:\n%s", first)
	}
}

// TestTheTaskBucketsAndTheFlagsAreWrittenAsPlainNumbers is the small stuff that
// is only ever wrong once: a flag written as true/false, a count written with a
// separator, a family that quietly stopped being written at all.
func TestTheTaskBucketsAndTheFlagsAreWrittenAsPlainNumbers(t *testing.T) {
	body := prometheusText(promFixture())
	for _, want := range []string{
		`knightloader_build_info{version="v1.2.3",deployment="desktop"} 1`,
		"knightloader_start_time_seconds 1757260000",
		"knightloader_uptime_seconds 86400",
		"knightloader_queue_halted 1",
		"knightloader_queue_quiet 0",
		`knightloader_tasks{state="running"} 3`,
		`knightloader_tasks{state="waiting"} 16`,
		`knightloader_tasks{state="extracting"} 0`,
		`knightloader_tasks{state="disabled"} 2`,
		`knightloader_tasks{state="failed"} 5`,
		`knightloader_tasks_failed{reason="unknown"} 1`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing or misspelt: %q\n%s", want, body)
		}
	}
	// An empty report still renders: every family is declared, no sample lies,
	// and nothing panics on nil maps and nil slices.
	empty := prometheusText(app.HealthReport{})
	if !strings.Contains(empty, "# TYPE knightloader_disk_free_bytes gauge") {
		t.Errorf("an instance with no volumes drops the family declaration:\n%s", empty)
	}
	if strings.Contains(empty, "knightloader_disk_free_bytes{") {
		t.Errorf("an instance with no volumes invented a sample:\n%s", empty)
	}
}
