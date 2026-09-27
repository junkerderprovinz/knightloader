package api

// What this instance has downloaded, added up: the curve, and the counter the
// volume cap is measured against.
//
// A route of its own rather than something a client aggregates from
// /api/history: the history is thousands of rows, and a client would pull the
// whole table on every page load to draw 42 numbers, bucketing them by its own
// calendar. The cap is enforced in this process, so the server's local day is
// the only day the chart and the number holding a queue back can both mean.
//
// Two routes, because the status bar asks for the counter on every page load
// and must not pull 42 buckets to draw one figure, while the chart asks for
// the buckets once and does not care what the cap is. Neither takes a ?limit.
// The curve's ?span has a ceiling in each unit, and a span past 92 days is cut
// into months, so no answer grows past 120 buckets.

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// How far back the two curves look without a ?span. Thirty days answers "what
// have I been doing lately" and twelve months "how does this month compare",
// and both are small enough to draw legibly on a phone.
const (
	volumeDays   = 30
	volumeMonths = 12
)

// The longest spans ?span accepts, in each unit. Two years of days is where
// counting in days stops being how anyone thinks about it, and ten years of
// months covers any history this instance can have kept.
const (
	volumeMaxDays   = 730
	volumeMaxMonths = 120
)

// volumeDailyUpTo is the longest span drawn one bar per day. Ninety-two is
// the longest three calendar months can be, so "90 days" and "3 months" are
// still days, and anything longer is months: a year of daily bars is thinner
// than a pixel on a phone.
const volumeDailyUpTo = 92

// volumeEdge is what both answers say about the record behind the buckets.
type volumeEdge struct {
	// TimeZone is the zone the buckets were cut in, so a chart can say whose
	// calendar it draws rather than leaving somebody to assume it is theirs.
	TimeZone string `json:"timeZone"`
	// Oldest is the earliest finish the history still holds, absent for an
	// empty history. Anything before it is not "nothing was downloaded", it is
	// "not recorded any more".
	Oldest *time.Time `json:"oldest,omitempty"`
	// Trimmed says the history is at its own ceiling, so the front of a long
	// curve is cut rather than empty.
	Trimmed bool `json:"trimmed"`
}

// volumeStats is GET /api/stats/volume without a ?span.
type volumeStats struct {
	// Days and Months are oldest first and gap-free: a day on which nothing
	// finished is a zero bucket rather than a missing one, so a chart can index
	// straight into them without working out which days are absent.
	Days   []store.VolumeBucket `json:"days"`
	Months []store.VolumeBucket `json:"months"`
	volumeEdge
}

// volumeCurve is GET /api/stats/volume?span=, one curve over the span asked
// for, oldest first and gap-free like the two above.
type volumeCurve struct {
	// Unit is store.VolumeDay or store.VolumeMonth, whichever the span's
	// length called for.
	Unit    string               `json:"unit"`
	Buckets []store.VolumeBucket `json:"buckets"`
	volumeEdge
}

func registerStats(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/stats/volume",
		"how much this instance has finished downloading, split by file host and by backend: by day for 30 days and by month for 12, or one curve over ?span= (7d, 18m, all), by day up to 92 days and by month beyond",
		func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			raw := r.URL.Query().Get("span")
			if raw == "" {
				out, err := volumeCurves(a, now)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				writeJSON(w, out)
				return
			}
			sp, err := parseVolumeSpan(raw)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			out, err := volumeSpanCurve(a, now, sp)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, out)
		})

	reg.Add(http.MethodGet, "/api/stats/volume/usage",
		"the volume counter alone: what has finished this period, the cap it is measured against, and what happens once it is reached",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.VolumeUsage())
		})
}

// volumeCurves builds both curves against one reading of the clock, so the two
// cannot straddle midnight between them.
func volumeCurves(a *app.App, now time.Time) (volumeStats, error) {
	dayKeys, dayFrom := dayWindow(now, volumeDays)
	days, err := a.VolumeBuckets(dayFrom, store.VolumeDay)
	if err != nil {
		return volumeStats{}, err
	}
	monthKeys, monthFrom := monthWindow(now, volumeMonths)
	months, err := a.VolumeBuckets(monthFrom, store.VolumeMonth)
	if err != nil {
		return volumeStats{}, err
	}
	edge, err := volumeEdgeAt(a, now)
	if err != nil {
		return volumeStats{}, err
	}
	return volumeStats{
		Days:       fillCurve(dayKeys, days),
		Months:     fillCurve(monthKeys, months),
		volumeEdge: edge,
	}, nil
}

func volumeEdgeAt(a *app.App, now time.Time) (volumeEdge, error) {
	edge, err := a.VolumeHistoryEdge()
	if err != nil {
		return volumeEdge{}, err
	}
	zone, _ := now.Zone()
	out := volumeEdge{TimeZone: zone, Trimmed: edge.Trimmed}
	if edge.Known {
		oldest := edge.Oldest
		out.Oldest = &oldest
	}
	return out, nil
}

// volumeSpan is a parsed ?span: the whole history, or the last n days or
// calendar months.
type volumeSpan struct {
	all  bool
	n    int
	unit string
}

var spanPattern = regexp.MustCompile(`^([0-9]{1,4})([dm])$`)

// parseVolumeSpan reads "all", or a count and a unit such as "45d" or "6m".
func parseVolumeSpan(s string) (volumeSpan, error) {
	if s == "all" {
		return volumeSpan{all: true}, nil
	}
	m := spanPattern.FindStringSubmatch(s)
	if m == nil {
		return volumeSpan{}, fmt.Errorf("span %q is not \"all\" or a count of days or months such as \"45d\" or \"6m\"", s)
	}
	n, _ := strconv.Atoi(m[1])
	if m[2] == "d" {
		if n < 1 || n > volumeMaxDays {
			return volumeSpan{}, fmt.Errorf("span %q: between 1 and %d days", s, volumeMaxDays)
		}
		return volumeSpan{n: n, unit: store.VolumeDay}, nil
	}
	if n < 1 || n > volumeMaxMonths {
		return volumeSpan{}, fmt.Errorf("span %q: between 1 and %d months", s, volumeMaxMonths)
	}
	return volumeSpan{n: n, unit: store.VolumeMonth}, nil
}

// volumeSpanCurve builds the one curve a span asks for. Its bars are days
// while the span is at most volumeDailyUpTo days long, and months beyond. A
// span of days that ends up in months starts partway into its first month, and
// that bar holds only the part inside the span.
func volumeSpanCurve(a *app.App, now time.Time, sp volumeSpan) (volumeCurve, error) {
	edge, err := volumeEdgeAt(a, now)
	if err != nil {
		return volumeCurve{}, err
	}
	var oldest time.Time
	if edge.Oldest != nil {
		oldest = *edge.Oldest
	}
	from := spanStart(now, sp, oldest)

	unit := store.VolumeMonth
	var keys []string
	if n := calendarDays(from, now); n <= volumeDailyUpTo {
		unit = store.VolumeDay
		keys, _ = dayWindow(now, n)
	} else {
		keys, _ = monthWindow(now, calendarMonths(from, now))
	}
	got, err := a.VolumeBuckets(from, unit)
	if err != nil {
		return volumeCurve{}, err
	}
	return volumeCurve{Unit: unit, Buckets: fillCurve(keys, got), volumeEdge: edge}, nil
}

// spanStart is the midnight a span begins at, in the server's zone. The whole
// history begins on the day of its oldest entry, today for an empty one, and
// no further back than volumeMaxMonths.
func spanStart(now time.Time, sp volumeSpan, oldest time.Time) time.Time {
	if !sp.all {
		if sp.unit == store.VolumeDay {
			_, from := dayWindow(now, sp.n)
			return from
		}
		_, from := monthWindow(now, sp.n)
		return from
	}
	_, today := dayWindow(now, 1)
	if oldest.IsZero() {
		return today
	}
	o := oldest.In(now.Location())
	from := time.Date(o.Year(), o.Month(), o.Day(), 0, 0, 0, 0, now.Location())
	// A row stamped in the future by a wrong clock would start the curve after
	// its own end.
	if from.After(today) {
		return today
	}
	if _, floor := monthWindow(now, volumeMaxMonths); from.Before(floor) {
		return floor
	}
	return from
}

// calendarDays counts the calendar days from from's date to now's, both
// included. Taken between noons, for the reason dayWindow gives, and rounded,
// since a day with a clock change is 23 or 25 hours long.
func calendarDays(from, now time.Time) int {
	loc := now.Location()
	a := time.Date(from.Year(), from.Month(), from.Day(), 12, 0, 0, 0, loc)
	b := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)
	return int(b.Sub(a).Round(24*time.Hour)/(24*time.Hour)) + 1
}

// calendarMonths counts the calendar months from from's to now's, both
// included.
func calendarMonths(from, now time.Time) int {
	return (now.Year()-from.Year())*12 + int(now.Month()) - int(from.Month()) + 1
}

// dayWindow is the last n calendar days in the server's own zone, oldest
// first, and the instant the earliest of them begins.
//
// Anchored at noon and stepped by whole days. In zones where daylight saving
// starts at 00:00 local midnight does not exist on that date, and Go
// normalises the missing hour forward, so stepping back from midnight can
// produce the same date twice and skip its neighbour. Noon is never the hour a
// zone jumps over.
func dayWindow(now time.Time, n int) ([]string, time.Time) {
	loc := now.Location()
	first := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc).AddDate(0, 0, -(n - 1))
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		keys = append(keys, first.AddDate(0, 0, i).Format("2006-01-02"))
	}
	return keys, time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc)
}

// monthWindow is the last n calendar months, oldest first, and the instant the
// earliest of them begins. Noon on the first for the reason dayWindow says.
func monthWindow(now time.Time, n int) ([]string, time.Time) {
	loc := now.Location()
	first := time.Date(now.Year(), now.Month(), 1, 12, 0, 0, 0, loc).AddDate(0, -(n - 1), 0)
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		keys = append(keys, first.AddDate(0, i, 0).Format("2006-01"))
	}
	return keys, time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, loc)
}

// fillCurve puts the buckets the store found onto the calendar the window
// asked for, and fills what is left with zeroes. A quiet Sunday and a Sunday
// the record has lost look identical in a list that omits both, and the
// oldest field beside these curves only tells them apart if the empty days are
// drawn.
//
// A bucket the window does not name is dropped. It can only be a row stamped
// in the future by a wrong clock, and drawing it would stretch the axis past
// today.
func fillCurve(keys []string, got []store.VolumeBucket) []store.VolumeBucket {
	by := make(map[string]store.VolumeBucket, len(got))
	for _, b := range got {
		by[b.Key] = b
	}
	out := make([]store.VolumeBucket, 0, len(keys))
	for _, k := range keys {
		if b, ok := by[k]; ok {
			out = append(out, b)
			continue
		}
		out = append(out, store.NewVolumeBucket(k))
	}
	return out
}
