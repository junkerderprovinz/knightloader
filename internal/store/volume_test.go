package store

// The volume totals, driven against a database with known rows in it and a
// known zone around it.
//
// The zone is not decoration. Every query in volume.go buckets with
// 'localtime', and a test that ran in whatever zone the machine happens to be
// in would say nothing at all on a CI box set to UTC - which is exactly the
// machine on which "just drop the localtime bit" would look like a tidy-up.
// time.Local is set for the length of the test instead, so the assertion has
// teeth everywhere.

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

// TestVolumeBucketsCutTheDayOnTheServersOwnCalendar is the guard on the one
// word that makes the curve mean anything: 'localtime'. A day here is a day on
// the server's own clock, because the monthly cap is enforced against that day,
// and a chart bucketing in UTC would disagree with the number holding somebody's
// queue back.
//
// THE INSTANTS ARE BUILT FROM THE LOCAL CALENDAR, and that is the fix rather
// than the setup. The first version of this test hardcoded two UTC instants and
// asserted they fall on two different local days, which is only true in some
// zones: it wanted an offset of about +09, passed on this machine for an
// unrelated reason (SQLite's 'localtime' and Go's time.Local do not resolve
// identically on Windows), and failed on CI, where both are UTC and the two
// instants are simply the same day. A test whose answer depends on where the
// machine is standing proves nothing about the code.
//
// Local midday on two consecutive local days is the portable form: whatever the
// offset, and even if SQLite and Go disagree about it by an hour or two, noon
// cannot fall over a midnight. What is left is exactly the claim - two instants
// on two different local days come back as two buckets, keyed by those days.
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

// TestVolumeMonthsAreOneBucketPerCalendarMonth is the same query at the other
// unit, and it also pins that the two splits and the total are one pass over
// one range rather than three that can disagree.
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

// TestUnsizedDownloadsCountAsZeroAndAreCounted is the honesty of the total. A
// livestream or a host that would not say leaves size 0 in the row, so the
// bytes are silently short; the only defence is a number that says how short
// they might be, and a chart that never gets it cannot draw the caveat.
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
		t.Errorf("unsized = %d, want 2; without it the total is short and says nothing about it", b.Unsized)
	}
}

// TestARowWithNoHostIsInTheTotalAndInNoBand pins what a split does with the
// oldest rows an upgrade carried in, which have no host recorded at all.
// Filing them under an invented name would put a hoster in the legend that
// this instance has never met; leaving them out of the total would make the
// chart disagree with the counter.
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
		t.Errorf("by host = %v, want the one host that was recorded and no band for the blank", b.ByHost)
	}
	if _, ok := b.ByHost[""]; ok {
		t.Error("a band with no name is in the split; a legend cannot draw it and nobody can read it")
	}
}

// TestAnEmptyBucketCarriesMapsRatherThanNulls is the shape a gap in the curve
// has to have. A nil map is JSON null, and a client that has to guard every
// bucket for it will forget on one of the two curves.
func TestAnEmptyBucketCarriesMapsRatherThanNulls(t *testing.T) {
	b := NewVolumeBucket("2026-03-14")
	if b.ByHost == nil || b.ByResolver == nil {
		t.Fatal("an empty bucket carries nil maps, which cross the wire as null")
	}
}

// TestVolumeSinceIsHalfOpen is the property a period boundary rests on: two
// adjacent periods must never both claim the same download, and neither may
// drop one.
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
		t.Errorf("bytes = %d, want 333: the row exactly at the start belongs to this period and the one exactly at the end to the next", bytes)
	}
	if count != 2 || unsized != 0 {
		t.Errorf("count = %d, unsized = %d, want 2 and 0", count, unsized)
	}
}

// TestVolumeSinceOverAnEmptyHistoryIsZeroAndNotAnError keeps a fresh install
// out of the error path: SUM over no rows is NULL, and a scan that did not
// expect it would make the counter unreadable on the one instance that has
// nothing to hide.
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

// TestARefetchMovesItsBytesRatherThanAddingThem writes down what the history's
// one-row-per-task rule does to a curve, so that nobody reads a shrinking month
// as a bug. It is also the reason the running counter re-queries instead of
// only ever adding: a second fetch of the same link adds nothing to the total.
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
		t.Fatalf("buckets = %+v, want the bytes to have MOVED to April rather than being counted twice", got)
	}
	if got[0].Bytes != 900 {
		t.Errorf("April = %d bytes, want the one download's 900", got[0].Bytes)
	}
}

// TestOldestFinishedSaysWhereTheRecordRunsOut is what lets a chart tell "you
// downloaded nothing then" apart from "that is no longer recorded". The two
// look identical on a curve of zeroes.
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

// TestHistoryFullReportsTheCeilingAndNotTheAbsenceOfOne separates the two
// answers a client draws differently: a history at its limit is missing its
// oldest months, and a history with no limit at all is not.
func TestHistoryFullReportsTheCeilingAndNotTheAbsenceOfOne(t *testing.T) {
	s := volumeStore(t)
	day := time.Date(2026, 3, 14, 12, 0, 0, 0, time.Local)
	for i := 0; i < 3; i++ {
		finished(t, s, string(rune('a'+i)), "host.example", "direct", 10, day.Add(time.Duration(i)*time.Hour))
	}
	if full, err := s.HistoryFull(0); err != nil || full {
		t.Errorf("HistoryFull(0) = %v (err %v), want false: zero is keep everything, so nothing has been cut", full, err)
	}
	if full, err := s.HistoryFull(10); err != nil || full {
		t.Errorf("HistoryFull(10) = %v (err %v) with three rows, want false", full, err)
	}
	if full, err := s.HistoryFull(3); err != nil || !full {
		t.Errorf("HistoryFull(3) = %v (err %v) with three rows, want true", full, err)
	}
}

// TestAnUnknownUnitIsRefused keeps a typo from drawing a plausible curve of a
// different question's answer.
func TestAnUnknownUnitIsRefused(t *testing.T) {
	s := volumeStore(t)
	if _, err := s.VolumeBuckets(time.Now().Add(-time.Hour), "week"); err == nil {
		t.Error("a unit nothing supports was accepted; a curve drawn from it would look right and mean nothing")
	}
}
