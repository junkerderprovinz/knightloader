package api

// What this instance has downloaded, added up: the curve, and the counter the
// volume cap is measured against.
//
// A route of its own rather than a flag on /api/history, and not something a
// client works out for itself from that route either. The history is thousands
// of rows and a client that aggregated them would be pulling the whole table to
// draw 42 numbers, on every page load, over a link that may be somebody's phone.
// Worse, it would be bucketing by ITS calendar: the cap is enforced in this
// process, so the server's local day is the only day the chart and the number
// holding a queue back can both mean.
//
// TWO ROUTES AND NOT ONE. The status bar asks for the counter on every page
// load and must not pull 42 buckets to draw one figure, and the chart asks for
// the buckets once and does not care what the cap is. Neither takes a ?limit:
// both are bounded by construction, which is a promise a limit parameter would
// quietly take away.

import (
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// How far back the two curves look.
//
// Thirty days and twelve months, because those are the two questions people
// actually have: "what have I been doing lately" and "how does this month
// compare". Both are also small enough to draw legibly on a phone, which a
// ninety day curve is not.
const (
	volumeDays   = 30
	volumeMonths = 12
)

// volumeStats is GET /api/stats/volume.
type volumeStats struct {
	// Days and Months are oldest first and gap-free: a day on which nothing
	// finished is a zero bucket rather than a missing one, so a chart can index
	// straight into them without working out which days are absent.
	Days   []store.VolumeBucket `json:"days"`
	Months []store.VolumeBucket `json:"months"`
	// TimeZone is the zone the buckets were cut in, so a chart can say whose
	// calendar it is drawing rather than leaving somebody to assume it is theirs.
	TimeZone string `json:"timeZone"`
	// Oldest is the earliest finish the history still holds, and it is absent
	// for a history with nothing in it. Anything before it is not "nothing was
	// downloaded", it is "no longer recorded", and those are different sentences.
	Oldest *time.Time `json:"oldest,omitempty"`
	// Trimmed says the history is at its own ceiling, so the front of the twelve
	// month curve is cut rather than empty.
	Trimmed bool `json:"trimmed"`
}

func registerStats(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/stats/volume",
		"how much this instance has finished downloading, by day for 30 days and by month for 12, split by file host and by backend",
		func(w http.ResponseWriter, r *http.Request) {
			out, err := volumeCurves(a, time.Now())
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
	edge, err := a.VolumeHistoryEdge()
	if err != nil {
		return volumeStats{}, err
	}
	zone, _ := now.Zone()
	out := volumeStats{
		Days:     fillCurve(dayKeys, days),
		Months:   fillCurve(monthKeys, months),
		TimeZone: zone,
		Trimmed:  edge.Trimmed,
	}
	if edge.Known {
		oldest := edge.Oldest
		out.Oldest = &oldest
	}
	return out, nil
}

// dayWindow is the last n calendar days in the server's own zone, oldest first,
// and the instant the earliest of them begins.
//
// ANCHORED AT NOON and stepped by whole days. Midnight is the tempting anchor
// and it is the wrong one: there are zones and dates where local midnight does
// not exist at all (daylight saving starting at 00:00, which Brazil used to do),
// and Go normalises the missing hour forward, so stepping back day by day from
// midnight can produce the same date twice and skip its neighbour. Noon is never
// the hour a zone jumps over.
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

// fillCurve puts the buckets the store found onto the calendar the window asked
// for, and fills what is left with zeroes.
//
// The zeroes are the point. A quiet Sunday and a Sunday the record has lost look
// identical in a list that simply omits both, and a chart drawn from gaps has no
// abscissa: the reason there is an `oldest` beside these curves is so the two can
// be told apart, and that only works if the empty days are actually drawn.
//
// A bucket the window does not name is dropped, which can only be a row stamped
// in the future by a clock that was wrong. Drawing it would stretch the axis
// past today for one bad row.
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
