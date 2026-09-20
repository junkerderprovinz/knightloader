package store

// How much this instance has fetched, added up: the curve a chart draws and
// the number a volume cap is measured against. It reads the history table
// like history.go does, but returns sums over a window rather than rows.
//
// What the numbers mean, since a volume cap is something people pay for:
//
//   - size is the size the hoster announced (core.Task.Size), not bytes that
//     crossed the line. A download nobody could size, such as a livestream,
//     adds zero, which is why every result carries unsized beside its total.
//   - A torrent's upload is not counted: core.Task.Uploaded is not persisted,
//     though for a seeder it is what the provider meters.
//   - There is one row per task, so a download fetched twice counts once, in
//     the bucket it last finished in.
//
// Buckets use the server's local calendar ('localtime'), because the cap is
// enforced in this process and the chart has to agree with it.

import (
	"database/sql"
	"fmt"
	"time"
)

// The two units VolumeBuckets groups by. They arrive as strings from a URL, so
// an unknown one can be refused.
const (
	VolumeDay   = "day"
	VolumeMonth = "month"
)

// VolumeBucket is one calendar day or one calendar month of finished downloads.
type VolumeBucket struct {
	// Key is the bucket in the server's calendar: "YYYY-MM-DD" for a day,
	// "YYYY-MM" for a month. It is a label, not an instant; parsing it as UTC
	// draws every bar a day early west of Greenwich.
	Key string `json:"key"`
	// Bytes is the announced size of everything that finished in this bucket,
	// and Count how many downloads that was.
	Bytes int64 `json:"bytes"`
	Count int   `json:"count"`
	// Unsized is how many of those had no size and so add nothing to Bytes.
	Unsized int `json:"unsized"`
	// ByHost and ByResolver split Bytes by file host and by backend. They are
	// never nil. A row without a recorded host or backend, mostly old entries
	// carried in by an upgrade, counts in Bytes but in neither map, so a split
	// may sum to less than the total.
	ByHost     map[string]int64 `json:"byHost"`
	ByResolver map[string]int64 `json:"byResolver"`
}

// NewVolumeBucket is the empty bucket with its maps, for filling gaps in the
// curve: a day with nothing finished is a zero, not a hole.
func NewVolumeBucket(key string) VolumeBucket {
	return VolumeBucket{Key: key, ByHost: map[string]int64{}, ByResolver: map[string]int64{}}
}

// volumeFormat is the strftime pattern one unit buckets by. An unknown unit is
// an error rather than a default, which would turn a typo into a plausible
// but wrong curve.
func volumeFormat(unit string) (string, error) {
	switch unit {
	case VolumeDay:
		return "%Y-%m-%d", nil
	case VolumeMonth:
		return "%Y-%m", nil
	}
	return "", fmt.Errorf("volume buckets: %q is not a unit (%q or %q)", unit, VolumeDay, VolumeMonth)
}

// VolumeBuckets adds up everything that finished at or after from, one entry
// per calendar day or month, oldest first. Only buckets with something in them
// are returned; the caller fills gaps with NewVolumeBucket.
//
// Bucket, host and backend are grouped in one pass so the splits cannot
// disagree with the total.
func (s *Store) VolumeBuckets(from time.Time, unit string) ([]VolumeBucket, error) {
	format, err := volumeFormat(unit)
	if err != nil {
		return nil, err
	}
	// A zero stamp means the row is not settled; as a date it would land in
	// January 1970.
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

// VolumeSince sums everything that finished in [from, to). The interval is
// half-open so adjacent periods never both count a download. A cap's period
// starts on a chosen day and does not line up with calendar buckets, hence a
// query of its own.
func (s *Store) VolumeSince(from, to time.Time) (bytes int64, count, unsized int, err error) {
	err = s.db.QueryRow(
		`SELECT COALESCE(SUM(size),0), COUNT(*), COALESCE(SUM(size = 0),0)
		   FROM history
		  WHERE finished_at > 0 AND finished_at >= ? AND finished_at < ?`,
		from.UnixMilli(), to.UnixMilli()).Scan(&bytes, &count, &unsized)
	return bytes, count, unsized, err
}

// OldestFinished is the earliest finish the history still holds, and false
// for an empty history. It lets a chart tell "nothing was downloaded" from
// "no longer recorded", since the table is trimmed and can be cleared.
func (s *Store) OldestFinished() (time.Time, bool, error) {
	var at sql.NullInt64
	err := s.db.QueryRow(`SELECT MIN(finished_at) FROM history WHERE finished_at > 0`).Scan(&at)
	if err == nil && !at.Valid {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return time.UnixMilli(at.Int64), true, nil
}

// HistoryFull reports whether the history sits at the ceiling retention trims
// it to, meaning the oldest months are missing rather than empty. max is
// settings.HistoryMax, passed in; zero or less keeps everything. TrimHistory
// leaves the table at exactly max, so reaching it is the test.
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
