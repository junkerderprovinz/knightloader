package store

// How much this instance has fetched, added up: the curve a chart draws and the
// one number a volume cap is measured against.
//
// A file of its own rather than three more queries in history.go, because the
// two read the same table for opposite purposes. history.go hands out ROWS,
// newest first, and is bounded by a limit somebody typed; everything here hands
// out SUMS over a window and is bounded by the window itself. The row reader has
// no use for a GROUP BY and the totals have no use for a URL.
//
// WHAT THE NUMBERS ARE, which matters more here than anywhere else in the tree,
// because a volume cap is a figure somebody pays for:
//
//   - `size` is the size the HOSTER ANNOUNCED, copied by recordFinished from
//     core.Task.Size. It is not the bytes that crossed the line, and this table
//     has no column that is. A download nobody could size - a livestream, a
//     direct host that would not say - contributes exactly zero however many
//     gigabytes it took. That is why every result below carries `unsized`
//     beside its total: a sum that is silently short is worse than one that
//     says how short it might be.
//   - A torrent's UPLOAD is not counted and cannot be. core.Task.Uploaded is
//     deliberately not persisted, and for anyone seeding that is the traffic
//     their provider actually meters.
//   - One row per task (history.go's own rule), so a download fetched twice
//     counts once, in the bucket it LAST finished in. A re-download therefore
//     moves bytes out of a month that was already closed, which is a fact about
//     the record and not a defect to repair here.
//
// AND THE CALENDAR IS THE SERVER'S. Every statement below buckets with
// 'localtime', so a day is a local day in this process's own zone. It has to be
// the server's: the cap is enforced in this process, so the server's day is the
// only day the chart and the counter can both mean, and a browser two zones away
// re-bucketing the same rows would draw a curve that disagrees with the number
// holding its queue back.

import (
	"database/sql"
	"fmt"
	"time"
)

// The two units VolumeBuckets groups by. Strings rather than an enum because
// they arrive from a caller that read them off a URL, and a value it does not
// recognise has to be refusable rather than silently one of the two.
const (
	VolumeDay   = "day"
	VolumeMonth = "month"
)

// VolumeBucket is one calendar day or one calendar month of finished downloads.
type VolumeBucket struct {
	// Key is the bucket in the server's own calendar: "YYYY-MM-DD" for a day,
	// "YYYY-MM" for a month. It is a label and not an instant - handing it to a
	// date parser that assumes UTC draws every bar a day early west of
	// Greenwich, which is the one thing the localtime bucketing above is for.
	Key string `json:"key"`
	// Bytes is the announced size of everything that finished in this bucket,
	// and Count how many downloads that was.
	Bytes int64 `json:"bytes"`
	Count int   `json:"count"`
	// Unsized is how many of those had no size at all. They add nothing to
	// Bytes, so this is the one number that says whether the total is
	// understating itself.
	Unsized int `json:"unsized"`
	// ByHost and ByResolver split Bytes by file host and by backend. Never nil,
	// because a nil map is JSON null and every reader would have to guard it.
	//
	// A row whose host or backend was never recorded - the oldest entries an
	// upgrade carried in, mostly - is in Bytes and in Count and in NEITHER map,
	// so a split can legitimately add up to less than the total. Filing it under
	// an invented name would be worse: the chart can show the difference as
	// "everything else", which is true, and no legend gains a host that does not
	// exist.
	ByHost     map[string]int64 `json:"byHost"`
	ByResolver map[string]int64 `json:"byResolver"`
}

// NewVolumeBucket is the empty bucket, maps and all. Whoever fills a gap in the
// curve wants exactly this: a day on which nothing finished is a zero, not a
// hole, and it has to carry the same shape as a day that has something in it.
//
// Exported for that one reason. Building the empty bucket at the call site
// instead means the "these maps are never nil" rule is written down in two
// places, and the second one is a route file that has no reason to know it.
func NewVolumeBucket(key string) VolumeBucket {
	return VolumeBucket{Key: key, ByHost: map[string]int64{}, ByResolver: map[string]int64{}}
}

// volumeFormat is the strftime pattern one unit buckets by.
//
// It REFUSES an unknown unit rather than falling back to days, because the two
// callers of this are a route reading a fixed word and this package's own tests.
// A default would turn a typo into a curve that looks plausible and is a
// different question's answer.
func volumeFormat(unit string) (string, error) {
	switch unit {
	case VolumeDay:
		return "%Y-%m-%d", nil
	case VolumeMonth:
		return "%Y-%m", nil
	}
	return "", fmt.Errorf("volume buckets: %q is not a unit (%q or %q)", unit, VolumeDay, VolumeMonth)
}

// VolumeBuckets adds up everything that finished at or after `from`, one entry
// per calendar day or month, oldest first.
//
// It returns only the buckets that have something in them: the window is the
// caller's decision, not this table's, and a query that invented 30 rows here
// would have to be told how many to invent. Whoever draws the curve fills the
// gaps with NewVolumeBucket.
//
// The grouping is by bucket AND host AND backend in one pass rather than three
// queries, because the alternative reads the same range three times and the two
// splits would then be free to disagree with the total.
func (s *Store) VolumeBuckets(from time.Time, unit string) ([]VolumeBucket, error) {
	format, err := volumeFormat(unit)
	if err != nil {
		return nil, err
	}
	// finished_at > 0 as well as the window: a zero stamp means the row is not
	// settled, and dividing it into a date would file a live download under
	// January 1970 - a bucket nobody asked for that the caller cannot even see
	// to discard.
	rows, err := s.db.Query(
		`SELECT strftime(?, finished_at/1000, 'unixepoch', 'localtime') AS bucket,
		        host, resolver, SUM(size), COUNT(*), SUM(size = 0)
		   FROM history
		  WHERE finished_at > 0 AND finished_at >= ?
		  GROUP BY bucket, host, resolver
		  ORDER BY bucket`,
		format, from.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Never nil, so a client reads an empty curve rather than null.
	out := []VolumeBucket{}
	at := map[string]int{}
	for rows.Next() {
		var key, host, resolver string
		var bytes int64
		var count, unsized int
		if err := rows.Scan(&key, &host, &resolver, &bytes, &count, &unsized); err != nil {
			return nil, err
		}
		i, ok := at[key]
		if !ok {
			i = len(out)
			at[key] = i
			out = append(out, NewVolumeBucket(key))
		}
		out[i].Bytes += bytes
		out[i].Count += count
		out[i].Unsized += unsized
		if host != "" {
			out[i].ByHost[host] += bytes
		}
		if resolver != "" {
			out[i].ByResolver[resolver] += bytes
		}
	}
	return out, rows.Err()
}

// VolumeSince is the counter's own sum: everything that finished in
// [from, to), in one row.
//
// Half-open at the top so two adjacent periods can never both claim the same
// download, which is the whole property a cap needs from a boundary. A
// separate query from VolumeBuckets rather than a fold over its result,
// because the period a cap runs over starts on a day somebody chose and does
// not line up with any calendar bucket.
func (s *Store) VolumeSince(from, to time.Time) (bytes int64, count, unsized int, err error) {
	err = s.db.QueryRow(
		`SELECT COALESCE(SUM(size),0), COUNT(*), COALESCE(SUM(size = 0),0)
		   FROM history
		  WHERE finished_at > 0 AND finished_at >= ? AND finished_at < ?`,
		from.UnixMilli(), to.UnixMilli()).Scan(&bytes, &count, &unsized)
	if err == sql.ErrNoRows {
		return 0, 0, 0, nil
	}
	return bytes, count, unsized, err
}

// OldestFinished is the earliest finish the history still holds, and false for
// a history with nothing in it.
//
// It is what lets a chart say the difference between "nothing was downloaded
// then" and "that is no longer recorded". The table is trimmed by row count and
// can be emptied by hand, so a curve that runs off the front of it is drawing
// zeroes for months this instance may well have been busy in.
func (s *Store) OldestFinished() (time.Time, bool, error) {
	var at sql.NullInt64
	err := s.db.QueryRow(`SELECT MIN(finished_at) FROM history WHERE finished_at > 0`).Scan(&at)
	if err == sql.ErrNoRows || (err == nil && !at.Valid) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return time.UnixMilli(at.Int64), true, nil
}

// HistoryFull reports whether the history is sitting at the ceiling retention
// trims it to, which is the honest way to say "the oldest months are missing"
// rather than "you downloaded nothing then".
//
// max is settings.HistoryMax, passed in rather than read here: this package has
// no opinion about settings and is not about to grow one. Zero or less is "keep
// everything", so nothing has been cut.
//
// It reuses historyRows, which until now only the trim and the tests had a use
// for. At the ceiling rather than over it: TrimHistory runs on the upkeep tick
// and leaves the table at exactly max, so "over" is a state nobody would ever
// observe.
func (s *Store) HistoryFull(max int) (bool, error) {
	if max <= 0 {
		return false, nil
	}
	n, err := s.historyRows()
	if err != nil {
		return false, err
	}
	return n >= max, nil
}
