package api

// The two volume routes over a real app with a real history behind it, because
// the shape of the answer is the whole contract: a chart that has to work out
// for itself which days are missing is a chart that will get it wrong on the
// month a curve is most interesting.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// TestTheCurveIsBoundedAndGapFree is the promise that lets the route do without
// a ?limit: both curves are always exactly as long as they say they are, on an
// instance that has downloaded nothing and on one that has downloaded for years.
//
// Gap-free is the other half. A quiet Sunday and a Sunday the record has lost
// look identical in a list that simply leaves both out, and telling those two
// apart is what the `oldest` field beside the curves is for.
func TestTheCurveIsBoundedAndGapFree(t *testing.T) {
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
	// Oldest first, one key each, and today at the end - which is what makes an
	// abscissa drawable straight from the order.
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

// TestAFinishedDownloadLandsInTodaysBucketAndInThisMonths is the route reading
// the history it claims to read, at both units, with the splits the legend is
// drawn from.
func TestAFinishedDownloadLandsInTodaysBucketAndInThisMonths(t *testing.T) {
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

// TestTheUsageRouteAnswersTheCounterAlone is why there are two routes. The
// status bar asks for this on every page load and must not pull 42 buckets to
// draw one number, and it needs the cap and the action with it or it cannot say
// why a queue is waiting.
func TestTheUsageRouteAnswersTheCounterAlone(t *testing.T) {
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
	// The window has to contain the moment it was asked for, or the number is a
	// sum over a period that has not started.
	now := time.Now()
	if now.Before(got.PeriodStart) || !now.Before(got.PeriodEnd) {
		t.Errorf("now is not inside [%v, %v)", got.PeriodStart, got.PeriodEnd)
	}
}
