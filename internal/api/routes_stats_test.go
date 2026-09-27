package api

// The two volume routes over a real app with a real history behind it, where
// the shape of the answer is the contract: a chart that has to work out for
// itself which days are missing gets it wrong on the month a curve is most
// interesting.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func statsServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerStats(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// statsFetched files one finished download, dated now, straight into the
// history the routes read.
func statsFetched(t *testing.T, a *app.App, id, host, resolver string, size int64) {
	t.Helper()
	now := time.Now()
	task := core.Task{
		ID: id, URL: "https://" + host + "/" + id + ".bin", Name: id + ".bin",
		Host: host, Resolver: resolver, Size: size,
		Status: core.StatusDone, CreatedAt: now.Add(-time.Minute), FinishedAt: now,
	}
	if err := a.Store.Save(&task); err != nil {
		t.Fatal(err)
	}
}

// TestTheCurveIsBoundedAndGapFree is the promise that lets the route do
// without a ?limit: both curves are as long as they say they are, on an
// instance that has downloaded nothing and on one that has downloaded for
// years.
//
// Gap-free is the other half. A quiet Sunday and a Sunday the record has lost
// look identical in a list that leaves both out, and the oldest field beside
// the curves is what tells them apart.
func TestTheCurveIsBoundedAndGapFree(t *testing.T) {
	t.Parallel()
	_, srv := statsServer(t)

	var got volumeStats
	if code := getJSON(t, srv.URL+"/api/stats/volume", &got); code != http.StatusOK {
		t.Fatalf("GET /api/stats/volume: %d", code)
	}
	if len(got.Days) != volumeDays || len(got.Months) != volumeMonths {
		t.Fatalf("%d days and %d months over an empty history, want %d and %d",
			len(got.Days), len(got.Months), volumeDays, volumeMonths)
	}
	if got.Oldest != nil {
		t.Errorf("an empty history reports an oldest entry of %v", got.Oldest)
	}
	if got.TimeZone == "" {
		t.Error("no zone on the answer, so nothing can say whose calendar the buckets were cut in")
	}
	// Oldest first, one key each, and today at the end, so an abscissa is
	// drawable straight from the order.
	seen := map[string]bool{}
	for i, b := range got.Days {
		if seen[b.Key] {
			t.Fatalf("day %q appears twice", b.Key)
		}
		seen[b.Key] = true
		if i > 0 && got.Days[i-1].Key >= b.Key {
			t.Fatalf("days are not oldest first: %q then %q", got.Days[i-1].Key, b.Key)
		}
		if b.ByHost == nil || b.ByResolver == nil {
			t.Fatalf("day %q carries nil splits, which cross the wire as null", b.Key)
		}
	}
	if last, want := got.Days[len(got.Days)-1].Key, time.Now().Format("2006-01-02"); last != want {
		t.Errorf("the last day is %q, want today (%q)", last, want)
	}
	if last, want := got.Months[len(got.Months)-1].Key, time.Now().Format("2006-01"); last != want {
		t.Errorf("the last month is %q, want this month (%q)", last, want)
	}
}

// TestAFinishedDownloadLandsInTodaysBucketAndInThisMonths: the route reads the
// history it claims to read, at both units, with the splits the legend is
// drawn from.
func TestAFinishedDownloadLandsInTodaysBucketAndInThisMonths(t *testing.T) {
	t.Parallel()
	a, srv := statsServer(t)
	statsFetched(t, a, "one", "host.example", "jd", 4096)

	var got volumeStats
	if code := getJSON(t, srv.URL+"/api/stats/volume", &got); code != http.StatusOK {
		t.Fatalf("GET /api/stats/volume: %d", code)
	}
	today := got.Days[len(got.Days)-1]
	if today.Bytes != 4096 || today.Count != 1 {
		t.Errorf("today = %d bytes over %d downloads, want 4096 over 1", today.Bytes, today.Count)
	}
	if today.ByHost["host.example"] != 4096 || today.ByResolver["jd"] != 4096 {
		t.Errorf("today's splits are %v and %v, want 4096 under host.example and under jd", today.ByHost, today.ByResolver)
	}
	if month := got.Months[len(got.Months)-1]; month.Bytes != 4096 {
		t.Errorf("this month = %d bytes, want the same download's 4096", month.Bytes)
	}
	if got.Oldest == nil {
		t.Error("no oldest entry although the history has one; a chart cannot then say where the record runs out")
	}
}

// TestTheUsageRouteAnswersTheCounterAlone is why there are two routes: the
// status bar asks for this on every page load and must not pull 42 buckets to
// draw one number, and it needs the cap and the action with it to say why a
// queue is waiting.
func TestTheUsageRouteAnswersTheCounterAlone(t *testing.T) {
	t.Parallel()
	a, srv := statsServer(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.VolumeCap = 10_000
	s.VolumeCapAction = settings.VolumeCapPause
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	statsFetched(t, a, "one", "host.example", "direct", 4096)

	var got app.VolumeUsage
	if code := getJSON(t, srv.URL+"/api/stats/volume/usage", &got); code != http.StatusOK {
		t.Fatalf("GET /api/stats/volume/usage: %d", code)
	}
	if got.Used != 4096 || got.Cap != 10_000 {
		t.Errorf("used %d of %d, want 4096 of 10000", got.Used, got.Cap)
	}
	if got.Action != settings.VolumeCapPause {
		t.Errorf("action = %q, want %q", got.Action, settings.VolumeCapPause)
	}
	if got.Reached {
		t.Error("4096 of 10000 reads as reached")
	}
	// The window has to contain the moment it was asked for, or the number
	// sums a period that has not started.
	now := time.Now()
	if now.Before(got.PeriodStart) || !now.Before(got.PeriodEnd) {
		t.Errorf("now is not inside [%v, %v)", got.PeriodStart, got.PeriodEnd)
	}
}

// statsFinished files one finished download of 1000 bytes, finished at the
// given moment.
func statsFinished(t *testing.T, a *app.App, id string, at time.Time) {
	t.Helper()
	task := core.Task{
		ID: id, URL: "https://host.example/" + id + ".bin", Name: id + ".bin",
		Host: "host.example", Resolver: "direct", Size: 1000,
		Status: core.StatusDone, CreatedAt: at.Add(-time.Minute), FinishedAt: at,
	}
	if err := a.Store.Save(&task); err != nil {
		t.Fatal(err)
	}
}

func getCurve(t *testing.T, srv *httptest.Server, span string) volumeCurve {
	t.Helper()
	var got volumeCurve
	if code := getJSON(t, srv.URL+"/api/stats/volume?span="+span, &got); code != http.StatusOK {
		t.Fatalf("GET ?span=%s: %d", span, code)
	}
	return got
}

func curveBytes(c volumeCurve) int64 {
	var sum int64
	for _, b := range c.Buckets {
		sum += b.Bytes
	}
	return sum
}

// dayKey is the calendar day n days before today, as the buckets name it.
func dayKey(n int) string {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location()).AddDate(0, 0, -n).Format("2006-01-02")
}

func TestASpanOfUpToNinetyTwoDaysIsDrawnByTheDay(t *testing.T) {
	t.Parallel()
	_, srv := statsServer(t)

	for _, tc := range []struct {
		span string
		n    int
	}{{"7d", 7}, {"90d", 90}, {"92d", 92}} {
		got := getCurve(t, srv, tc.span)
		if got.Unit != "day" || len(got.Buckets) != tc.n {
			t.Errorf("?span=%s: %d buckets by %s, want %d by day", tc.span, len(got.Buckets), got.Unit, tc.n)
			continue
		}
		if first, last := got.Buckets[0].Key, got.Buckets[tc.n-1].Key; first != dayKey(tc.n-1) || last != dayKey(0) {
			t.Errorf("?span=%s runs %s to %s, want %s to today (%s)", tc.span, first, last, dayKey(tc.n-1), dayKey(0))
		}
	}
}

func TestALongerSpanIsDrawnByTheMonth(t *testing.T) {
	t.Parallel()
	_, srv := statsServer(t)
	thisMonth := time.Now().Format("2006-01")

	got := getCurve(t, srv, "12m")
	if got.Unit != "month" || len(got.Buckets) != 12 {
		t.Fatalf("?span=12m: %d buckets by %s, want 12 by month", len(got.Buckets), got.Unit)
	}
	if last := got.Buckets[11].Key; last != thisMonth {
		t.Errorf("the last month is %s, want this month (%s)", last, thisMonth)
	}

	got = getCurve(t, srv, "93d")
	if got.Unit != "month" {
		t.Fatalf("?span=93d is drawn by %s, want month", got.Unit)
	}
	if first := got.Buckets[0].Key; first != dayKey(92)[:7] {
		t.Errorf("?span=93d starts in %s, want the month of its first day (%s)", first, dayKey(92)[:7])
	}
}

// Three calendar months are at most 92 days, so they stay a daily curve that
// begins on the first of the month.
func TestThreeMonthsAreDrawnByTheDayFromTheFirst(t *testing.T) {
	t.Parallel()
	_, srv := statsServer(t)
	now := time.Now()
	first := time.Date(now.Year(), now.Month(), 1, 12, 0, 0, 0, now.Location()).AddDate(0, -2, 0).Format("2006-01-02")

	got := getCurve(t, srv, "3m")
	if got.Unit != "day" {
		t.Fatalf("?span=3m is drawn by %s, want day", got.Unit)
	}
	if got.Buckets[0].Key != first || got.Buckets[len(got.Buckets)-1].Key != dayKey(0) {
		t.Errorf("?span=3m runs %s to %s, want %s to today", got.Buckets[0].Key, got.Buckets[len(got.Buckets)-1].Key, first)
	}
}

// A span of days drawn by the month counts only the days inside it, so the
// first bar is part of a month and the total is the span's.
func TestALongSpanOfDaysCountsOnlyItsOwnDays(t *testing.T) {
	t.Parallel()
	a, srv := statsServer(t)
	now := time.Now()
	statsFinished(t, a, "inside", now.AddDate(0, 0, -150))
	statsFinished(t, a, "outside", now.AddDate(0, 0, -250))

	if got := curveBytes(getCurve(t, srv, "200d")); got != 1000 {
		t.Errorf("?span=200d adds up to %d bytes, want the 1000 finished inside it", got)
	}
	if got := curveBytes(getCurve(t, srv, "90d")); got != 0 {
		t.Errorf("?span=90d adds up to %d bytes, want nothing: both downloads are older", got)
	}
}

func TestTheWholeHistoryStartsAtItsOldestEntry(t *testing.T) {
	t.Parallel()

	t.Run("a young history is drawn by the day", func(t *testing.T) {
		t.Parallel()
		a, srv := statsServer(t)
		statsFinished(t, a, "old", time.Now().AddDate(0, 0, -10))
		got := getCurve(t, srv, "all")
		if got.Unit != "day" || len(got.Buckets) != 11 || got.Buckets[0].Key != dayKey(10) {
			t.Errorf("%d buckets by %s from %s, want 11 by day from %s", len(got.Buckets), got.Unit, got.Buckets[0].Key, dayKey(10))
		}
		if curveBytes(got) != 1000 {
			t.Errorf("the whole history adds up to %d bytes, want 1000", curveBytes(got))
		}
	})

	t.Run("an old history is drawn by the month", func(t *testing.T) {
		t.Parallel()
		a, srv := statsServer(t)
		old := time.Now().AddDate(0, -20, 0)
		statsFinished(t, a, "old", old)
		got := getCurve(t, srv, "all")
		if got.Unit != "month" || got.Buckets[0].Key != old.Format("2006-01") {
			t.Errorf("drawn by %s from %s, want by month from %s", got.Unit, got.Buckets[0].Key, old.Format("2006-01"))
		}
	})

	t.Run("an ancient entry stops at ten years", func(t *testing.T) {
		t.Parallel()
		a, srv := statsServer(t)
		statsFinished(t, a, "ancient", time.Now().AddDate(-15, 0, 0))
		if got := getCurve(t, srv, "all"); len(got.Buckets) != volumeMaxMonths {
			t.Errorf("%d buckets, want no more than %d", len(got.Buckets), volumeMaxMonths)
		}
	})

	t.Run("an empty history is today alone", func(t *testing.T) {
		t.Parallel()
		_, srv := statsServer(t)
		got := getCurve(t, srv, "all")
		if len(got.Buckets) != 1 || got.Buckets[0].Key != dayKey(0) {
			t.Errorf("%d buckets, want today alone", len(got.Buckets))
		}
	})
}

func TestASpanThatIsNoSpanIsRefused(t *testing.T) {
	t.Parallel()
	_, srv := statsServer(t)
	for _, span := range []string{"0d", "731d", "0m", "121m", "7w", "-3d", "7", "d", "ALL", "12345d", "3.5m"} {
		if code := getJSON(t, srv.URL+"/api/stats/volume?span="+span, nil); code != http.StatusBadRequest {
			t.Errorf("?span=%s answered %d, want 400", span, code)
		}
	}
	for _, span := range []string{"1d", "730d", "1m", "120m"} {
		if code := getJSON(t, srv.URL+"/api/stats/volume?span="+span, nil); code != http.StatusOK {
			t.Errorf("?span=%s answered %d, want 200", span, code)
		}
	}
}

// Across the spring change in Vienna a day is 23 hours long, and counting it
// as a fraction would lose a bar.
func TestCalendarDaysCountsAShortDayAsADay(t *testing.T) {
	t.Parallel()
	vienna, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 3, 28, 0, 0, 0, 0, vienna)
	now := time.Date(2026, 3, 30, 1, 0, 0, 0, vienna)
	if got := calendarDays(from, now); got != 3 {
		t.Errorf("28 to 30 March is %d days, want 3", got)
	}
	if got := calendarMonths(time.Date(2025, 11, 20, 0, 0, 0, 0, vienna), now); got != 5 {
		t.Errorf("November to March is %d months, want 5", got)
	}
}
