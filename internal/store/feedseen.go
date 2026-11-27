package store

// What each feed subscription has already handed over (internal/feed). The
// table sits outside the numbered migrations, like SignInTable, so the build
// before it still opens a database that has it.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
)

const feedSeenTable = "feed_seen"

// feedSeenBucket is the interface-state bucket the memory was kept in before
// it had a table. It is read once and left in place, since the build before
// still reads it.
const feedSeenBucket = "feeds"

// openFeedSeen creates the table on first use and takes over the bucket in
// the same transaction, so the import happens exactly once.
func openFeedSeen(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: feed memory: %w", err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, feedSeenTable).Scan(&n); err != nil {
		return fmt.Errorf("store: feed memory: %w", err)
	}
	if n > 0 {
		return nil
	}
	if _, err := tx.Exec(`CREATE TABLE ` + feedSeenTable + ` (
	   url        TEXT PRIMARY KEY,
	   keys       TEXT NOT NULL,
	   changed_at INTEGER NOT NULL DEFAULT 0
	 )`); err != nil {
		return fmt.Errorf("store: feed memory: %w", err)
	}
	var bucket string
	err = tx.QueryRow(`SELECT value FROM uistate WHERE key = ?`, feedSeenBucket).Scan(&bucket)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("store: feed memory: %w", err)
	}
	if bucket != "" {
		doc := map[string][]string{}
		if err := json.Unmarshal([]byte(bucket), &doc); err != nil {
			// The downloads matter more than the memory; those feeds seed
			// again rather than keep the store shut.
			log.Printf("the feeds' record of what they had already added could not be read, so they start afresh: %v", err)
			doc = nil
		}
		now := time.Now().UnixMilli()
		for u, keys := range doc {
			b, err := json.Marshal(nonNil(keys))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO `+feedSeenTable+` (url,keys,changed_at) VALUES (?,?,?)`, u, string(b), now); err != nil {
				return fmt.Errorf("store: feed memory: %w", err)
			}
		}
	}
	return tx.Commit()
}

func nonNil(keys []string) []string {
	if keys == nil {
		return []string{}
	}
	return keys
}

// FeedSeen returns the keys stored for one subscription, newest first. known
// says whether anything was ever stored for it, so a subscription that ran
// against an empty feed is not taken for one that never ran.
func (s *Store) FeedSeen(url string) (keys []string, known bool, err error) {
	var raw string
	err = s.db.QueryRow(`SELECT keys FROM `+feedSeenTable+` WHERE url = ?`, url).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("store: feed memory: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil, false, fmt.Errorf("store: feed memory of %s: %w", url, err)
	}
	return keys, true, nil
}

// SetFeedSeen replaces what is stored for one subscription.
func (s *Store) SetFeedSeen(url string, keys []string) error {
	b, err := json.Marshal(nonNil(keys))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO `+feedSeenTable+` (url,keys,changed_at) VALUES (?,?,?)`,
		url, string(b), time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("store: feed memory: %w", err)
	}
	return nil
}

// KeepFeedSeen forgets every subscription not in urls, so one removed and
// added back seeds again.
func (s *Store) KeepFeedSeen(urls []string) error {
	keep := make(map[string]bool, len(urls))
	for _, u := range urls {
		keep[u] = true
	}
	rows, err := s.db.Query(`SELECT url FROM ` + feedSeenTable)
	if err != nil {
		return fmt.Errorf("store: feed memory: %w", err)
	}
	var gone []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return fmt.Errorf("store: feed memory: %w", err)
		}
		if !keep[u] {
			gone = append(gone, u)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: feed memory: %w", err)
	}
	for _, u := range gone {
		if _, err := s.db.Exec(`DELETE FROM `+feedSeenTable+` WHERE url = ?`, u); err != nil {
			return fmt.Errorf("store: feed memory: %w", err)
		}
	}
	return nil
}
