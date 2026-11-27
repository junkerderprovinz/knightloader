package store

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestASubscriptionsSeenMemoryComesBack(t *testing.T) {
	s := open(t)
	if keys, known, err := s.FeedSeen("https://example.invalid/rss.xml"); err != nil || known || len(keys) != 0 {
		t.Fatalf("a subscription never polled came back as %v, %v, %v", keys, known, err)
	}
	if err := s.SetFeedSeen("https://example.invalid/rss.xml", []string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFeedSeen("https://example.invalid/empty.xml", nil); err != nil {
		t.Fatal(err)
	}
	keys, known, err := s.FeedSeen("https://example.invalid/rss.xml")
	if err != nil || !known || !slices.Equal(keys, []string{"b", "a"}) {
		t.Fatalf("got %v, %v, %v, want the keys in the order they were written", keys, known, err)
	}
	// Polled against an empty feed is not the same as never polled.
	if keys, known, err := s.FeedSeen("https://example.invalid/empty.xml"); err != nil || !known || len(keys) != 0 {
		t.Fatalf("an empty memory came back as %v, %v, %v", keys, known, err)
	}
}

func TestHundredsOfLargeMemoriesFit(t *testing.T) {
	s := open(t)
	keys := make([]string, 20000)
	for i := range keys {
		keys[i] = fmt.Sprintf("%016x", i)
	}
	for i := range 300 {
		if err := s.SetFeedSeen(fmt.Sprintf("https://example.invalid/%d.xml", i), keys); err != nil {
			t.Fatalf("subscription %d: %v", i, err)
		}
	}
	got, _, err := s.FeedSeen("https://example.invalid/299.xml")
	if err != nil || len(got) != len(keys) {
		t.Fatalf("got %d keys, %v", len(got), err)
	}
}

func TestForgettingKeepsOnlyTheNamedSubscriptions(t *testing.T) {
	s := open(t)
	for _, u := range []string{"a", "b", "c"} {
		if err := s.SetFeedSeen(u, []string{u}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.KeepFeedSeen([]string{"b"}); err != nil {
		t.Fatal(err)
	}
	for u, want := range map[string]bool{"a": false, "b": true, "c": false} {
		if _, known, _ := s.FeedSeen(u); known != want {
			t.Errorf("%s: known = %v, want %v", u, known, want)
		}
	}
}

// TestTheOldBucketIsTakenOverOnceAndLeftInPlace covers an upgrade: the
// memory lived in the interface-state bucket "feeds" before it had a table.
// The bucket stays, so an older build started again still finds it.
func TestTheOldBucketIsTakenOverOnceAndLeftInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	bucket := `{"https://example.invalid/rss.xml":["k2","k1"],"https://example.invalid/empty.xml":[]}`
	if err := s.SetUIState("feeds", bucket); err != nil {
		t.Fatal(err)
	}
	// What a database written by the older build looks like.
	if _, err := s.db.Exec(`DROP TABLE ` + feedSeenTable); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	keys, known, err := s.FeedSeen("https://example.invalid/rss.xml")
	if err != nil || !known || !slices.Equal(keys, []string{"k2", "k1"}) {
		t.Fatalf("the bucket's memory was not taken over: %v, %v, %v", keys, known, err)
	}
	if _, known, _ := s.FeedSeen("https://example.invalid/empty.xml"); !known {
		t.Error("a subscription that had seeded against an empty feed would seed again")
	}
	if got, _ := s.UIState("feeds"); got != bucket {
		t.Errorf("the bucket was changed to %q", got)
	}

	// Once only: what the table learns afterwards is not overwritten.
	if err := s.SetFeedSeen("https://example.invalid/rss.xml", []string{"k3", "k2", "k1"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if keys, _, _ := s.FeedSeen("https://example.invalid/rss.xml"); len(keys) != 3 {
		t.Fatalf("a restart imported the bucket over the table again: %v", keys)
	}
}

func TestABrokenBucketDoesNotKeepTheStoreFromOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetUIState("feeds", "{not json"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE ` + feedSeenTable); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("an unreadable bucket kept the downloads from starting: %v", err)
	}
	defer s.Close()
	if _, known, err := s.FeedSeen("https://example.invalid/rss.xml"); err != nil || known {
		t.Fatalf("got %v, %v", known, err)
	}
}

// TestTheSeenTableLeavesTheSchemaVersionAlone keeps the database open to the
// build before it, which refuses a schema version it does not know.
func TestTheSeenTableLeavesTheSchemaVersionAlone(t *testing.T) {
	s := open(t)
	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != SchemaVersion() {
		t.Fatalf("schema version %d, want %d", v, SchemaVersion())
	}
	var sql string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name = ?`, feedSeenTable).Scan(&sql); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "CREATE TABLE") {
		t.Fatalf("schema row %q", sql)
	}
}
