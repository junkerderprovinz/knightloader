package api

// The health report as Prometheus exposition text. It encodes the same
// app.HealthReport the JSON route serves, so the two can differ in shape but
// never in fact, and it is a pure function so the format can be tested without
// a server.
//
// Any of these mistakes costs the whole scrape, not one line: a family whose
// samples are not contiguous or that has a second # HELP, a label value with an
// unescaped backslash, quote or newline (Windows folder paths land in labels),
// a duplicate series, or a number that is not a plain decimal.
//
// Every subsystem is written in all five states on every scrape. Writing only
// the current one would let Prometheus keep serving a stale state="failed"
// sample for the staleness window after a recovery. A volume that could not be
// measured writes knightloader_disk_measurable 0 and no byte series, since its
// zeros mean nothing (VolumeReport.Known) and a missing series is how
// Prometheus spells "no data".
//
// Every family is a gauge: nothing here is monotonic, since the failed count
// falls when rows are cleared and uptime resets on restart (see
// TaskCounts.Failed).

import (
	"sort"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// promStates is every state a subsystem row can be in, in a fixed order so
// two scrapes of an unchanged instance produce identical text.
var promStates = []app.SubsystemState{
	app.StateOK,
	app.StateDegraded,
	app.StateFailed,
	app.StateUnused,
	app.StateUnknown,
}

// promLabel is one label pair, kept in a slice so the label order is stable
// between scrapes.
type promLabel struct{ name, value string }

// prometheusText renders one report. It cannot fail, since every label value
// goes through escapeLabelValue.
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

	// One family with a state label, so a dashboard can sum or stack them.
	promFamily(&b, "knightloader_tasks",
		"How many downloads are in each state right now. A gauge and not a tally: failed counts the rows sitting in the list with an error on them, so it falls when they are cleared and when retention trims the list. disabled is counted across the other states, not instead of them.")
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

	// One pass over the volumes per family, so each family stays contiguous.
	promFamily(&b, "knightloader_disk_measurable",
		"1 when this platform could measure the folder at all. While it is 0 the byte series below are absent for that folder, because zero bytes free and no answer are different facts.")
	for _, v := range rep.Volumes {
		promSample(&b, "knightloader_disk_measurable", diskLabels(v), promBool(v.Known))
	}

	// Three separate series, since the root reserve and quotas make free plus
	// used less than total.
	promDiskBytes(&b, "knightloader_disk_free_bytes",
		"Bytes this process may still write into the folder. On unix that excludes the root reserve; on Windows it honours a per-user quota.",
		rep.Volumes, func(v app.VolumeReport) uint64 { return v.Free })
	promDiskBytes(&b, "knightloader_disk_used_bytes",
		"Bytes somebody's files occupy on the folder's volume.",
		rep.Volumes, func(v app.VolumeReport) uint64 { return v.Used })
	promDiskBytes(&b, "knightloader_disk_total_bytes",
		"The size of the volume the folder sits on. Free plus used can be less than this; see the free series.",
		rep.Volumes, func(v app.VolumeReport) uint64 { return v.Total })

	// Written for every folder, measurable or not, since they come from the
	// queue rather than the filesystem.
	promFamily(&b, "knightloader_disk_queued_bytes",
		"What the downloads still owed would add to this folder. A floor and not a forecast: a download whose size nobody knows contributes nothing. Never subtract it from the free bytes, which already exclude the room a running transfer has claimed.")
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

// promDiskBytes writes one byte family over every measurable volume, so the
// skip lives in one place.
func promDiskBytes(b *strings.Builder, name, help string, vols []app.VolumeReport, pick func(app.VolumeReport) uint64) {
	promFamily(b, name, help)
	for _, v := range vols {
		if !v.Known {
			continue
		}
		promSample(b, name, diskLabels(v), strconv.FormatUint(pick(v), 10))
	}
}

// diskLabels identifies one folder's series. The path is the identity; the
// role is not unique across category folders but makes an alert readable.
func diskLabels(v app.VolumeReport) []promLabel {
	return []promLabel{{"role", v.Role}, {"dir", v.Dir}}
}

// promCounts writes a map as one family's samples, sorted by key so scrapes
// of an unchanged instance can be diffed.
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

// promFamily writes the # HELP and # TYPE lines that open a gauge family.
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

// escapeLabelValue makes any string safe inside a label's quotes. The
// backslash has to be escaped first, or the backslashes added for quotes would
// be doubled. A carriage return is legal in a value and left alone.
func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// escapeHelp escapes a HELP line, where a double quote needs no escaping.
func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
