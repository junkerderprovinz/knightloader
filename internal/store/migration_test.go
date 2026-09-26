package store

import (
	"database/sql"
	"fmt"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// beforeTheWideningMigration is how many migrations had shipped before the
// column batch this file covers, so the database it builds is what an
// installed copy of the previous build holds.
const beforeTheWideningMigration = 13

// openAtOldSchema builds a database as the previous build left it: the
// migrations that had shipped and the version stamp they set. Reopening it
// through Open is then the real upgrade path rather than a simulation.
func openAtOldSchema(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < beforeTheWideningMigration; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatalf("old migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, beforeTheWideningMigration)); err != nil {
		t.Fatal(err)
	}
}

// Enabled defaults to true, so a column added with a bool's zero value would
// write 0 into every existing row and leave the whole queue stopped after an
// upgrade, with nothing on screen to explain it.
func TestUpgradeLeavesExistingTasksEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	openAtOldSchema(t, path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Every column the old schema had. The original CREATE TABLE declares no
	// defaults, so a partial insert would leave NULLs no released build could
	// have produced.
	if _, err := db.Exec(
		`INSERT INTO tasks (id,url,name,package,resolver,size,loaded,speed,status,error,created_at,
		   dir,password,online,retries,next_try,priority,position,checksum,
		   comment,chunks,auto_extract,matched_rules)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"old", "https://host.example/from-before.bin", "from-before.bin", "Batch", "direct",
		1024, 512, 0, string(core.StatusPaused), "", time.Now().UnixMilli(),
		"", "", "", 0, 0, 0, 0, "", "", 0, nil, ""); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("upgrading an existing database failed: %v", err)
	}
	defer s.Close()
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want the one that was already there", len(all))
	}
	if !all[0].Enabled {
		t.Fatal("a task stored by the previous build came back disabled")
	}
}

// A column that is written but never read back leaves a feature working until
// the process restarts and then silently stopping.
func TestWidenedFieldsSurviveARestart(t *testing.T) {
	yes := true
	finished := time.Now().Add(-time.Hour).Round(time.Millisecond)
	changed := time.Now().Round(time.Millisecond)
	want := core.Task{
		ID: "wide", URL: "https://host.example/part01.rar", Name: "part01.rar",
		CreatedAt: time.Now(),
		// Done, because stampFinish only lets a finished task carry a finish
		// time, so a fixture in any other state could not hold one.
		Status:           core.StatusDone,
		FinishedAt:       finished,
		Enabled:          true,
		Skipped:          true,
		SkipReason:       "the destination is full",
		Hold:             true,
		Forced:           true,
		DownloadPassword: "hoster-side",
		ExpectedHash:     "sha256:abc",
		Connection:       "conn-2",
		Host:             "host.example",
		Source:           "https://host.example/gallery",
		MirrorOf:         "other",
		Resumable:        &yes,
		Filename:         "renamed.rar",
		Variant:          "1080p",
		ManualPackage:    true,
		Reason:           core.Reason("ip_blocked"),
		Origin:           core.Origin("watch"),
		ChangedAt:        changed,
		ArchivePart:      1,
	}

	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	task := want
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	s.Close()

	again, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want 1", len(all))
	}
	got := *all[0]

	checks := []struct {
		field     string
		got, want any
	}{
		{"finishedAt", got.FinishedAt.UTC(), want.FinishedAt.UTC()},
		{"enabled", got.Enabled, want.Enabled},
		{"skipped", got.Skipped, want.Skipped},
		{"skipReason", got.SkipReason, want.SkipReason},
		{"hold", got.Hold, want.Hold},
		{"forced", got.Forced, want.Forced},
		{"downloadPassword", got.DownloadPassword, want.DownloadPassword},
		{"expectedHash", got.ExpectedHash, want.ExpectedHash},
		{"connection", got.Connection, want.Connection},
		{"host", got.Host, want.Host},
		{"source", got.Source, want.Source},
		{"mirrorOf", got.MirrorOf, want.MirrorOf},
		{"filename", got.Filename, want.Filename},
		{"variant", got.Variant, want.Variant},
		{"manualPackage", got.ManualPackage, want.ManualPackage},
		{"reason", got.Reason, want.Reason},
		{"origin", got.Origin, want.Origin},
		{"changedAt", got.ChangedAt.UTC(), want.ChangedAt.UTC()},
		{"archivePart", got.ArchivePart, want.ArchivePart},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}
	if got.Resumable == nil || *got.Resumable != yes {
		t.Errorf("resumable = %v, want %v", got.Resumable, yes)
	}
}

// A rejected link is worded from its code in the reader's language, at intake
// and when it was about to start alike, so both codes and their values have to
// come back from a restart as they were written.
func TestARejectionsCodeSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	tracker := map[string]string{"host": "tracker.example.org"}
	rule := map[string]string{"rule": "no samples"}
	tasks := []core.Task{
		{
			ID: "at-intake", URL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", CreatedAt: time.Now(),
			Status: core.StatusCollected, Skipped: true,
			SkipReason: "announces tracker.example.org, which is on the banned trackers list",
			SkipCode:   "bannedTracker", SkipParams: tracker,
		},
		{
			ID: "at-start", URL: "https://host.example/sample.mkv", CreatedAt: time.Now(),
			Status:     core.StatusError,
			Error:      `rejected by link filter rule "no samples"`,
			RejectCode: "filterRule", RejectParams: rule,
		},
	}
	for i := range tasks {
		if err := s.Save(&tasks[i]); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	again, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*core.Task{}
	for _, task := range all {
		got[task.ID] = task
	}
	if in := got["at-intake"]; in == nil || in.SkipCode != "bannedTracker" || !maps.Equal(in.SkipParams, tracker) {
		t.Errorf("the link rejected at intake came back as %+v, want its code and host", in)
	}
	if st := got["at-start"]; st == nil || st.RejectCode != "filterRule" || !maps.Equal(st.RejectParams, rule) {
		t.Errorf("the link rejected at the start came back as %+v, want its code and rule", st)
	}
}

// Resumable is tri-state like auto_extract: "nobody has asked whether this
// resumes" must not come back as "it does not", or the interface warns about
// losing bytes that would be picked up where they stopped.
func TestResumableKeepsItsThirdAnswer(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	task := core.Task{ID: "unasked", URL: "https://host.example/x.bin", CreatedAt: time.Now()}
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	s.Close()

	again, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want 1", len(all))
	}
	if all[0].Resumable != nil {
		t.Errorf("resumable came back as %v, want no answer at all", *all[0].Resumable)
	}
}

// schemaOf is every table and index in the database with the statement that
// defines it, which ALTER TABLE keeps current, so two files can be compared by
// shape.
func schemaOf(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT type, name, COALESCE(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kind, name, def string
		if err := rows.Scan(&kind, &name, &def); err != nil {
			t.Fatal(err)
		}
		out = append(out, kind+" "+name+": "+def)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func versionOf(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// A fresh store gets the whole schema and the stamp of the last migration, and
// it is the same schema an older install reaches by upgrading.
func TestAFreshStoreHasTheSchemaAnUpgradeReaches(t *testing.T) {
	dir := t.TempDir()
	fresh, err := Open(filepath.Join(dir, "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	oldPath := filepath.Join(dir, "old.db")
	openAtOldSchema(t, oldPath)
	upgraded, err := Open(oldPath)
	if err != nil {
		t.Fatalf("upgrading an existing database failed: %v", err)
	}
	defer upgraded.Close()

	for name, s := range map[string]*Store{"fresh": fresh, "upgraded": upgraded} {
		if v := versionOf(t, s.db); v != len(migrations) {
			t.Errorf("the %s store is at schema version %d, want %d", name, v, len(migrations))
		}
	}
	want := schemaOf(t, fresh.db)
	got := schemaOf(t, upgraded.db)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the upgraded schema differs from a fresh one\n got  %q\n want %q", got, want)
	}
}

// A fresh database is migrated in one transaction, so a step that fails leaves
// neither a table nor a version stamp behind, and the next start begins again
// from nothing instead of from half a schema.
func TestAFailedMigrationLeavesAFreshDatabaseUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	db := openRaw(t, path)

	broken := append([]string{}, migrations[:beforeTheWideningMigration]...)
	broken = append(broken, `ALTER TABLE no_such_table ADD COLUMN x TEXT`)
	broken = append(broken, migrations[beforeTheWideningMigration:]...)
	if err := migrate(db, broken); err == nil {
		t.Fatal("a migration list with a broken step went through")
	}
	if v := versionOf(t, db); v != 0 {
		t.Errorf("schema version is %d after the failure, want 0", v)
	}
	if got := schemaOf(t, db); len(got) != 0 {
		t.Errorf("the failed migration left %q behind", got)
	}

	if err := migrate(db, migrations); err != nil {
		t.Fatalf("migrating again after the failure: %v", err)
	}
	if v := versionOf(t, db); v != len(migrations) {
		t.Errorf("schema version is %d after the second run, want %d", v, len(migrations))
	}
}

// On an upgrade each step lands with its version stamp or not at all. A step
// that fails halfway keeps nothing of itself, and the steps before it stay
// done, so the next start does not run them twice.
func TestAFailedUpgradeStepKeepsNothingOfItself(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	openAtOldSchema(t, path)
	db := openRaw(t, path)

	steps := append([]string{}, migrations[:beforeTheWideningMigration]...)
	steps = append(steps,
		`ALTER TABLE tasks ADD COLUMN first TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN second TEXT NOT NULL DEFAULT '';
		 ALTER TABLE no_such_table ADD COLUMN x TEXT`,
	)
	if err := migrate(db, steps); err == nil {
		t.Fatal("an upgrade with a broken step went through")
	}
	if v := versionOf(t, db); v != beforeTheWideningMigration+1 {
		t.Errorf("schema version is %d, want %d: the step before the broken one stays done", v, beforeTheWideningMigration+1)
	}
	columns := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM pragma_table_info('tasks')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	rows.Close()
	if !columns["first"] {
		t.Error("the step that went through lost its column")
	}
	if columns["second"] {
		t.Error("the broken step kept the column it added before failing")
	}
}

// beforeTheConfirmDueColumn is how many migrations there were before the
// column that keeps a pending auto-confirm countdown.
const beforeTheConfirmDueColumn = 49

// A row stored before the column existed has no countdown pending, so an
// upgrade confirms nothing by itself.
func TestAnUpgradeLeavesNoCountdownPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < beforeTheConfirmDueColumn; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatalf("old migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, beforeTheConfirmDueColumn)); err != nil {
		t.Fatal(err)
	}
	// Every column after the first eleven has a default.
	if _, err := db.Exec(
		`INSERT INTO tasks (id,url,name,package,resolver,size,loaded,speed,status,error,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		"old", "https://host.example/waiting.bin", "waiting.bin", "Batch", "direct",
		0, 0, 0, string(core.StatusCollected), "", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("upgrading an existing database failed: %v", err)
	}
	defer s.Close()
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want the one that was already there", len(all))
	}
	if !all[0].ConfirmDue.IsZero() {
		t.Errorf("a row from before the upgrade has a countdown due at %v, want none", all[0].ConfirmDue)
	}
}

// beforeTheUnpackColumn is how many migrations there were before the column
// that keeps how a task's last unpacking ended.
const beforeTheUnpackColumn = 52

// A finished download stored before the column existed was never seen being
// unpacked by anything that could say so, so it comes back without a result
// rather than with one the upgrade made up.
func TestAnUpgradeGivesNoFileAnUnpackResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < beforeTheUnpackColumn; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatalf("old migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, beforeTheUnpackColumn)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO tasks (id,url,name,package,resolver,size,loaded,speed,status,error,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		"old", "https://host.example/film.rar", "film.rar", "Film", "direct",
		10, 10, 0, string(core.StatusDone), "", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("upgrading an existing database failed: %v", err)
	}
	defer s.Close()
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want the one that was already there", len(all))
	}
	if all[0].Unpack != core.UnpackNone {
		t.Errorf("a row from before the upgrade reads unpack %q, want none", all[0].Unpack)
	}
}

// After a restart the task is the only place that remembers an archive was
// unpacked, or failed to be, so every result has to come back as written.
func TestHowAnUnpackingEndedSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	results := []core.UnpackResult{core.UnpackDone, core.UnpackFailed, core.UnpackPassword, core.UnpackNone}
	for i, r := range results {
		task := core.Task{
			ID: fmt.Sprintf("u%d", i), URL: fmt.Sprintf("https://host.example/set%d.rar", i),
			Name: fmt.Sprintf("set%d.rar", i), CreatedAt: time.Now(), Status: core.StatusDone, Unpack: r,
		}
		if err := s.Save(&task); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	again, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]core.UnpackResult{}
	for _, task := range all {
		got[task.ID] = task.Unpack
	}
	for i, want := range results {
		if id := fmt.Sprintf("u%d", i); got[id] != want {
			t.Errorf("%s reads unpack %q after a restart, want %q", id, got[id], want)
		}
	}
}

// The due time is what a restart counts down to, so it has to come back as it
// was written.
func TestAPendingCountdownsDueTimeSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	due := time.UnixMilli(time.Now().Add(time.Hour).UnixMilli())
	task := core.Task{
		ID: "w", URL: "https://host.example/waiting.bin", Name: "waiting.bin", CreatedAt: time.Now(),
		Status: core.StatusCollected, ConfirmDue: due,
	}
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	s.Close()

	again, err := Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("reloaded %d tasks, want 1", len(all))
	}
	if !all[0].ConfirmDue.Equal(due) {
		t.Errorf("countdown due at %v, want %v", all[0].ConfirmDue, due)
	}
}
