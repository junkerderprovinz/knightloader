package store

// The volume totals, driven against a database with known rows in it.
//
// Every query in volume.go buckets with 'localtime', so a test running in
// whatever zone the machine happens to be in would say nothing on a CI box set
// to UTC. time.Local is pinned for the length of each test instead.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// volumeStore opens an empty database and pins a zone that is nine hours ahead
// of UTC, so a local day and a UTC day are never the same day.
func volumeStore(t *testing.T) *Store {
	t.Helper()
	prev := time.Local
	t.Cleanup(func() { time.Local = prev })
	time.Local = time.FixedZone("VOLTEST", 9*3600)

	s, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// finished files one download in the history, finished at a given instant. The
// stamp is carried in rather than taken by the store, because every test here
// is about a window and needs to put rows on both sides of one.
func finished(t *testing.T, s *Store, id, host, resolver string, size int64, at time.Time) {
	t.Helper()
	task := core.Task{
		ID: id, URL: "https://" + host + "/" + id + ".bin", Name: id + ".bin",
		Host: host, Resolver: resolver, Size: size,
		Status: core.StatusDone, CreatedAt: at.Add(-time.Hour), FinishedAt: at,
	}
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
}

// bucketOf finds one bucket by key, or fails saying which keys there were.
func bucketOf(t *testing.T, got []VolumeBucket, key string) VolumeBucket {
	t.Helper()
	for _, b := range got {
		if b.Key == key {
			return b
		}
	}
	keys := []string{}
	for _, b := range got {
		keys = append(keys, b.Key)
	}
	t.Fatalf("no bucket %q in %v", key, keys)
	return VolumeBucket{}
}

// A day is a day on the server's own clock, because the monthly cap is
// enforced against that day and a chart bucketing in UTC would disagree with
// the number holding somebody's queue back.
//
// The instants are built from the local calendar rather than hardcoded in UTC.
// Local midday on two consecutive days falls on two different local days
// whatever the offset, even where SQLite's 'localtime' and Go's time.Local
// resolve an hour or two apart.
func TestVolumeBucketsCutTheDayOnTheServersOwnCalendar(t *testing.T) {
	s := volumeStore(t)
	first := time.Date(2026, 3, 14, 12, 0, 0, 0, time.Local)
	second := time.Date(2026, 3, 15, 12, 0, 0, 0, time.Local)
	finished(t, s, "early", "host.example", "direct", 2000, first)
	finished(t, s, "late", "host.example", "direct", 1000, second)

	got, err := s.VolumeBuckets(time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local), VolumeDay)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d buckets, want the two local days those instants fall on: %+v", len(got), got)
	}
	firstKey, secondKey := first.Format("2006-01-02"), second.Format("2006-01-02")
	if b := bucketOf(t, got, firstKey); b.Bytes != 2000 {
		t.Errorf("%s holds %d bytes, want the 2000 that finished at midday local", firstKey, b.Bytes)
	}
	if b := bucketOf(t, got, secondKey); b.Bytes != 1000 {
		t.Errorf("%s holds %d bytes, want the 1000 that finished at midday local", secondKey, b.Bytes)
	}
	// Oldest first, so a chart can draw them in the order they arrive.
	if got[0].Key != firstKey {
		t.Errorf("first bucket is %q, want the oldest day", got[0].Key)
	}
}

// The same query at the month unit, which also pins that the two splits and
// the total come from one pass over one range.
func TestVolumeMonthsAreOneBucketPerCalendarMonth(t *testing.T) {
	s := volumeStore(t)
	feb := time.Date(2026, 2, 10, 12, 0, 0, 0, time.Local)
	mar := time.Date(2026, 3, 2, 12, 0, 0, 0, time.Local)
	finished(t, s, "f1", "one.example", "direct", 100, feb)
	finished(t, s, "f2", "two.example", "jd", 200, feb)
	finished(t, s, "m1", "one.example", "ytdlp", 400, mar)

	got, err := s.VolumeBuckets(time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local), VolumeMonth)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d month buckets, want February and March: %+v", len(got), got)
	}
	feb26 := bucketOf(t, got, "2026-02")
	if feb26.Bytes != 300 || feb26.Count != 2 {
		t.Errorf("February = %d bytes over %d downloads, want 300 over 2", feb26.Bytes, feb26.Count)
	}
	if feb26.ByHost["one.example"] != 100 || feb26.ByHost["two.example"] != 200 {
		t.Errorf("February by host = %v, want 100 and 200", feb26.ByHost)
	}
	if feb26.ByResolver["direct"] != 100 || feb26.ByResolver["jd"] != 200 {
		t.Errorf("February by backend = %v, want 100 and 200", feb26.ByResolver)
	}
	if mar26 := bucketOf(t, got, "2026-03"); mar26.ByResolver["ytdlp"] != 400 {
		t.Errorf("March by backend = %v, want 400 under ytdlp", mar26.ByResolver)
	}
}

// A livestream or a host that would not say leaves size 0 in the row, so the
// total is short and only the unsized count says how short.
func TestUnsizedDownloadsCountAsZeroAndAreCounted(t *testing.T) {
	s := volumeStore(t)
	day := time.Date(2026, 3, 14, 12, 0, 0, 0, time.Local)
	finished(t, s, "sized", "host.example", "direct", 5000, day)
	finished(t, s, "nosize1", "host.example", "ytdlp", 0, day)
	finished(t, s, "nosize2", "host.example", "ytdlp", 0, day)

	got, err := s.VolumeBuckets(day.Add(-24*time.Hour), VolumeDay)
	if err != nil {
		t.Fatal(err)
	}
	b := bucketOf(t, got, "2026-03-14")
	if b.Bytes != 5000 {
		t.Errorf("bytes = %d, want the 5000 that were announced", b.Bytes)
	}
	if b.Count != 3 {
		t.Errorf("count = %d, want all three downloads", b.Count)
	}
	if b.Unsized != 2 {
		t.Errorf("unsized = %d, want 2", b.Unsized)
	}
}

// Rows carried in by an upgrade have no host recorded. Filing them under an
// invented name would put a hoster in the legend that this instance never met;
// leaving them out of the total would make the chart disagree with the counter.
func TestARowWithNoHostIsInTheTotalAndInNoBand(t *testing.T) {
	s := volumeStore(t)
	day := time.Date(2026, 3, 14, 12, 0, 0, 0, time.Local)
	finished(t, s, "known", "host.example", "direct", 700, day)
	finished(t, s, "carried", "", "", 300, day)

	got, err := s.VolumeBuckets(day.Add(-24*time.Hour), VolumeDay)
	if err != nil {
		t.Fatal(err)
	}
	b := bucketOf(t, got, "2026-03-14")
	if b.Bytes != 1000 {
		t.Errorf("bytes = %d, want both downloads in the total", b.Bytes)
	}
	if len(b.ByHost) != 1 || b.ByHost["host.example"] != 700 {
		t.Errorf("by host = %v, want only the host that was recorded", b.ByHost)
	}
	if _, ok := b.ByHost[""]; ok {
		t.Error("a band with no name is in the split")
	}
}

// A nil map crosses the wire as JSON null, which every client drawing a gap in
// the curve would have to guard for.
func TestAnEmptyBucketCarriesMapsRatherThanNulls(t *testing.T) {
	b := NewVolumeBucket("2026-03-14")
	if b.ByHost == nil || b.ByResolver == nil {
		t.Fatal("an empty bucket carries nil maps, which cross the wire as null")
	}
}

// Two adjacent periods must never both claim the same download, and neither
// may drop one.
func TestVolumeSinceIsHalfOpen(t *testing.T) {
	s := volumeStore(t)
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 4, 1, 0, 0, 0, 0, time.Local)
	finished(t, s, "onTheEdge", "host.example", "direct", 111, from)
	finished(t, s, "inside", "host.example", "direct", 222, from.Add(72*time.Hour))
	finished(t, s, "nextPeriod", "host.example", "direct", 444, to)

	bytes, count, unsized, err := s.VolumeSince(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if bytes != 333 {
		t.Errorf("bytes = %d, want 333: the row at the start belongs to this period, the one at the end to the next", bytes)
	}
	if count != 2 || unsized != 0 {
		t.Errorf("count = %d, unsized = %d, want 2 and 0", count, unsized)
	}
}

// SUM over no rows is NULL, so a fresh install has to read zero rather than an
// error.
func TestVolumeSinceOverAnEmptyHistoryIsZeroAndNotAnError(t *testing.T) {
	s := volumeStore(t)
	bytes, count, _, err := s.VolumeSince(time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("summing an empty history failed: %v", err)
	}
	if bytes != 0 || count != 0 {
		t.Errorf("empty history summed to %d bytes over %d downloads", bytes, count)
	}
}

// The history keeps one row per task, so a second fetch of the same link moves
// its bytes into the newer month instead of adding them. A month can therefore
// shrink, and the running counter re-queries rather than only adding.
func TestARefetchMovesItsBytesRatherThanAddingThem(t *testing.T) {
	s := volumeStore(t)
	march := time.Date(2026, 3, 14, 12, 0, 0, 0, time.Local)
	april := time.Date(2026, 4, 2, 12, 0, 0, 0, time.Local)
	finished(t, s, "again", "host.example", "direct", 900, march)
	finished(t, s, "again", "host.example", "direct", 900, april)

	got, err := s.VolumeBuckets(time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local), VolumeMonth)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "2026-04" {
		t.Fatalf("buckets = %+v, want the bytes moved to April rather than counted twice", got)
	}
	if got[0].Bytes != 900 {
		t.Errorf("April = %d bytes, want the one download's 900", got[0].Bytes)
	}
}

// A chart has to tell "nothing was downloaded then" from "that is no longer
// recorded"; on a curve of zeroes the two look the same.
func TestOldestFinishedSaysWhereTheRecordRunsOut(t *testing.T) {
	s := volumeStore(t)
	if _, known, err := s.OldestFinished(); err != nil || known {
		t.Errorf("an empty history reports known=%v (err %v), want no answer at all", known, err)
	}
	oldest := time.Date(2026, 1, 4, 9, 0, 0, 0, time.Local)
	finished(t, s, "first", "host.example", "direct", 10, oldest)
	finished(t, s, "later", "host.example", "direct", 10, oldest.Add(48*time.Hour))

	got, known, err := s.OldestFinished()
	if err != nil {
		t.Fatal(err)
	}
	if !known || !got.Equal(oldest) {
		t.Errorf("oldest = %v (known %v), want %v", got, known, oldest)
	}
}

// A history at its limit is missing its oldest months; a history with no limit
// is not, and a client draws the two differently.
func TestHistoryFullReportsTheCeilingAndNotTheAbsenceOfOne(t *testing.T) {
	s := volumeStore(t)
	day := time.Date(2026, 3, 14, 12, 0, 0, 0, time.Local)
	for i := 0; i < 3; i++ {
		finished(t, s, string(rune('a'+i)), "host.example", "direct", 10, day.Add(time.Duration(i)*time.Hour))
	}
	if full, err := s.HistoryFull(0); err != nil || full {
		t.Errorf("HistoryFull(0) = %v (err %v), want false: zero keeps everything", full, err)
	}
	if full, err := s.HistoryFull(10); err != nil || full {
		t.Errorf("HistoryFull(10) = %v (err %v) with three rows, want false", full, err)
	}
	if full, err := s.HistoryFull(3); err != nil || !full {
		t.Errorf("HistoryFull(3) = %v (err %v) with three rows, want true", full, err)
	}
}

// An unknown unit is refused rather than defaulted, so a typo cannot draw a
// plausible curve that answers a different question.
func TestAnUnknownUnitIsRefused(t *testing.T) {
	s := volumeStore(t)
	if _, err := s.VolumeBuckets(time.Now().Add(-time.Hour), "week"); err == nil {
		t.Error("a unit nothing supports was accepted")
	}
}
