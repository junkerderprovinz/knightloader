package api

// The health report as Prometheus exposition text.
//
// A SECOND ENCODER, NEVER A SECOND COMPUTATION. This takes the same
// app.HealthReport the JSON route serves, so the two can differ in shape and
// can never differ in fact. Everything here is formatting, which is also why it
// is a pure function of a value rather than a handler: the awkward parts below
// are all provable without a server.
//
// FOUR RULES THE FORMAT ENFORCES AND THIS FILE HAS TO KEEP, because breaking
// any of them costs the WHOLE scrape rather than one line:
//
//  1. One # HELP and one # TYPE per family, and every sample of a family
//     contiguous. An interleaved family is a parse error.
//  2. Label values must escape backslash, double quote and newline. This is
//     not theoretical here: the disk rows carry the target folders' paths
//     straight out of the settings document, and the desktop build runs on
//     Windows, so `C:\downloads` lands in a label value. One unescaped
//     backslash makes the line unparseable and Prometheus drops the entire
//     scrape - so the test for it uses a real Windows path, not a POSIX one
//     with a backslash bolted on.
//  3. No duplicate series. Two samples with the same name and the same labels
//     in one exposition is rejected wholesale, which is why the test asserts
//     uniqueness over the whole body rather than eyeballing each family.
//  4. A number is written as a plain decimal. No thousands separators, no
//     locale, no units in the value.
//
// AND TWO RULES THAT ARE ABOUT BEING USEFUL RATHER THAN ABOUT BEING VALID:
//
//   - EVERY subsystem is emitted in ALL FIVE states, as 0 or 1, on every
//     scrape. Writing only the current one looks tidier and is a trap: when JD
//     recovers, the `state="failed"` series simply stops being written, and
//     Prometheus goes on serving its last value for the whole staleness window
//     (five minutes by default). An alert would then keep firing for five
//     minutes after the thing was fixed, which is the kind of false alarm that
//     teaches people to mute a rule.
//   - A volume that could not be measured emits knightloader_disk_measurable 0
//     and NO byte series at all. VolumeReport.Known says at length that the
//     three counts are zero and mean NOTHING when it is false; writing
//     `knightloader_disk_free_bytes ... 0` for such a volume hands an alert
//     rule a full disk that does not exist. A missing series is how Prometheus
//     already spells "no data", so the honest reading is the cheap one.
//
// WHY EVERY FAMILY IS A GAUGE, including the task counts. Nothing in this
// report is monotonic: the failed count falls when somebody clears a row or
// when retention trims the list, and an uptime resets on restart. A counter
// would make rate() and increase() available over figures that go backwards,
// which is worse than not offering them - see TaskCounts.Failed for the whole
// argument.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// promStates is every state a subsystem row can be in, in a fixed order so two
// scrapes of an unchanged instance produce byte-identical text. See the file
// comment for why all five are written every time.
var promStates = []app.SubsystemState{
	app.StateOK,
	app.StateDegraded,
	app.StateFailed,
	app.StateUnused,
	app.StateUnknown,
}

// promLabel is one label pair. A slice of these rather than a map, because the
// order of labels inside a series is part of the text and a map would shuffle
// it between scrapes - which parses fine and makes every diff of two scrapes
// useless.
type promLabel struct{ name, value string }

// prometheusText renders one report. It never fails: there is no input a report
// can carry that this cannot write, because every string that reaches a label
// goes through escapeLabelValue first.
func prometheusText(rep app.HealthReport) string {
	var b strings.Builder

	promFamily(&b, "knightloader_build_info",
		"The running build, as a constant 1 with the version and deployment as labels.")
	promSample(&b, "knightloader_build_info", []promLabel{
		{"version", rep.Version},
		{"deployment", rep.Deployment},
	}, "1")

	promFamily(&b, "knightloader_start_time_seconds",
		"When this process started, in seconds since the epoch.")
	promSample(&b, "knightloader_start_time_seconds", nil, strconv.FormatInt(rep.StartedAt.Unix(), 10))

	promFamily(&b, "knightloader_uptime_seconds",
		"How long this process has been running. It is the process, not the machine: a container that restarted an hour ago starts again at zero here.")
	promSample(&b, "knightloader_uptime_seconds", nil, strconv.FormatInt(rep.UptimeSeconds, 10))

	promFamily(&b, "knightloader_subsystem_state",
		"1 for the state each part of this instance is in, 0 for the four it is not. unused means not set up here and unknown means it cannot be asked; neither is a fault.")
	for _, s := range rep.Subsystems {
		for _, state := range promStates {
			promSample(&b, "knightloader_subsystem_state", []promLabel{
				{"subsystem", s.ID},
				{"state", string(state)},
			}, promBool(s.State == state))
		}
	}

	promFamily(&b, "knightloader_queue_halted",
		"1 while the queue is stopped, by the button or by a timetable window. A stopped queue is a choice, not a fault.")
	promSample(&b, "knightloader_queue_halted", nil, promBool(rep.Halted))

	promFamily(&b, "knightloader_queue_quiet",
		"1 while quiet mode is in force, so a low speed and a low slot count read as the mode they are rather than as a fault.")
	promSample(&b, "knightloader_queue_quiet", nil, promBool(rep.Quiet))

	// The task buckets, one family with a state label rather than seven
	// families. They are counts of one list split by one dimension, which is
	// exactly what a label is for, and it lets a dashboard sum or stack them
	// without naming each one.
	promFamily(&b, "knightloader_tasks",
		"How many downloads are in each state RIGHT NOW. A gauge and not a tally: failed counts the rows sitting in the list with an error on them, so it falls when they are cleared and when retention trims the list. disabled is counted across the other states, not instead of them.")
	for _, row := range []struct {
		state string
		n     int
	}{
		{"running", rep.Tasks.Running},
		{"waiting", rep.Tasks.Waiting},
		{"paused", rep.Tasks.Paused},
		{"extracting", rep.Tasks.Extracting},
		{"collected", rep.Tasks.Collected},
		{"disabled", rep.Tasks.Disabled},
		{"failed", rep.Tasks.Failed},
	} {
		promSample(&b, "knightloader_tasks", []promLabel{{"state", row.state}}, strconv.Itoa(row.n))
	}

	promFamily(&b, "knightloader_tasks_waiting",
		"How many queued downloads are being held back for each reason. A reason with nothing behind it is absent rather than zero.")
	promCounts(&b, "knightloader_tasks_waiting", "reason", rep.Tasks.WaitingBy)

	promFamily(&b, "knightloader_tasks_failed",
		"How many downloads are sitting in the list failed, by what went wrong. Same gauge caveat as knightloader_tasks above.")
	promCounts(&b, "knightloader_tasks_failed", "reason", rep.Tasks.FailedBy)

	// The disk families. Each is written in its own pass over the volumes, so
	// the samples of one family stay contiguous - the whole reason this is not
	// one loop emitting six lines per volume.
	promFamily(&b, "knightloader_disk_measurable",
		"1 when this platform could measure the folder at all. While it is 0 the byte series below are absent for that folder, because zero bytes free and no answer are different facts.")
	for _, v := range rep.Volumes {
		promSample(&b, "knightloader_disk_measurable", diskLabels(v), promBool(v.Known))
	}

	// free, used and total are three separate series and none may be derived
	// from the other two: a filesystem keeps blocks back for root and a
	// per-user quota can leave this process less than the volume has, so
	// free + used is routinely LESS than total. Dropping one of them and
	// letting a dashboard subtract would print a confident wrong number.
	promDiskBytes(&b, "knightloader_disk_free_bytes",
		"Bytes this process may still write into the folder. On unix that excludes the root reserve; on Windows it honours a per-user quota.",
		rep.Volumes, func(v app.VolumeReport) uint64 { return v.Free })
	promDiskBytes(&b, "knightloader_disk_used_bytes",
		"Bytes somebody's files occupy on the folder's volume.",
		rep.Volumes, func(v app.VolumeReport) uint64 { return v.Used })
	promDiskBytes(&b, "knightloader_disk_total_bytes",
		"The size of the volume the folder sits on. Free plus used can be less than this; see the free series.",
		rep.Volumes, func(v app.VolumeReport) uint64 { return v.Total })

	// queued and tasks are written for every folder, measurable or not: they
	// come from the queue rather than from the filesystem, so nothing about
	// them is unknown when the disk cannot be asked.
	promFamily(&b, "knightloader_disk_queued_bytes",
		"What the downloads still owed would add to this folder. A FLOOR and not a forecast: a download whose size nobody knows contributes nothing. Never subtract it from the free bytes, which already exclude the room a running transfer has claimed.")
	for _, v := range rep.Volumes {
		promSample(&b, "knightloader_disk_queued_bytes", diskLabels(v), strconv.FormatInt(v.Queued, 10))
	}

	promFamily(&b, "knightloader_disk_tasks",
		"How many downloads are aimed at this folder, including the ones whose size nobody knows and which therefore add nothing to the queued bytes.")
	for _, v := range rep.Volumes {
		promSample(&b, "knightloader_disk_tasks", diskLabels(v), strconv.Itoa(v.Tasks))
	}

	return b.String()
}

// promDiskBytes writes one byte family over every MEASURABLE volume.
//
// The skip is the whole point of the helper existing rather than the three
// calls being written out: a volume with Known false must produce no sample in
// any of the three, and three separate loops are three places to forget it.
func promDiskBytes(b *strings.Builder, name, help string, vols []app.VolumeReport, pick func(app.VolumeReport) uint64) {
	promFamily(b, name, help)
	for _, v := range vols {
		if !v.Known {
			continue
		}
		promSample(b, name, diskLabels(v), strconv.FormatUint(pick(v), 10))
	}
}

// diskLabels identifies one folder's series.
//
// Both the role and the path, because neither alone is enough: the role is not
// unique (there is one row per category folder) and the path is not readable
// (an alert that names /mnt/user/downloads/tv says more with "category" beside
// it). The path is the identity; a folder that is renamed becomes a new series,
// which is correct, because it is a different folder.
func diskLabels(v app.VolumeReport) []promLabel {
	return []promLabel{{"role", v.Role}, {"dir", v.Dir}}
}

// promCounts writes a map as one family's samples, in key order.
//
// SORTED, and that is not cosmetic. Go randomises map iteration, so an unsorted
// walk reorders the samples of one family on every scrape: still valid, still
// parsed correctly, and it makes two scrapes of an unchanged instance
// impossible to diff by eye - which is the first thing anybody does when a
// number looks wrong.
//
// A zero count is written when the key is present, because a key that reached
// this map was put there by something counting at least one; the omission
// happens upstairs, where a reason nothing is waiting on never becomes a key.
func promCounts(b *strings.Builder, name, label string, counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		promSample(b, name, []promLabel{{label, k}}, strconv.Itoa(counts[k]))
	}
}

// promFamily writes the two comment lines that open a family.
//
// Every family here is a gauge; see the file comment for why there is no
// counter in this exposition at all. The help text is escaped the way the
// format wants a comment escaped (backslash and newline only, and NOT the
// double quote, which is legal in a HELP line and illegal unescaped in a label
// value) - the help strings in this file contain neither today, and going
// through the escaper anyway is what keeps that from becoming a rule somebody
// has to remember when they edit one.
func promFamily(b *strings.Builder, name, help string) {
	b.WriteString("# HELP ")
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(escapeHelp(help))
	b.WriteString("\n# TYPE ")
	b.WriteString(name)
	b.WriteString(" gauge\n")
}

// promSample writes one sample line.
func promSample(b *strings.Builder, name string, labels []promLabel, value string) {
	b.WriteString(name)
	if len(labels) > 0 {
		b.WriteByte('{')
		for i, l := range labels {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(l.name)
			b.WriteString(`="`)
			b.WriteString(escapeLabelValue(l.value))
			b.WriteByte('"')
		}
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(value)
	b.WriteByte('\n')
}

// promBool is a flag as the format spells one.
func promBool(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// escapeLabelValue makes any string safe inside a label's quotes.
//
// THE BACKSLASH IS FIRST AND HAS TO BE. Escaping the quote first would turn
// `"` into `\"` and the second pass would then double the backslash it had just
// written, producing `\\"` - an escaped backslash followed by a bare quote,
// which closes the label early and breaks the line. That is the classic way
// this function is written wrong, and it is the reason the three replacements
// are not a loop over a map.
//
// Carriage return is deliberately left alone: the exposition format names
// backslash, double quote and line feed as the three that must be escaped, and
// a lone CR inside a value is legal. Escaping more than the format asks would
// silently change a path.
func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// escapeHelp is the same for a HELP line, which escapes two of the three: a
// double quote needs no escaping in a comment, and escaping it there would put
// a visible backslash in front of every quote somebody reads.
func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
