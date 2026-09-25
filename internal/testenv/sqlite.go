package testenv

import (
	"context"
	"database/sql/driver"
	"sync"

	"modernc.org/sqlite"
)

var volatileOnce sync.Once

// VolatileSQLite makes every SQLite connection the test binary opens from here
// on skip fsync and keep its rollback journal in memory. Call it from TestMain,
// before anything opens a store.
//
// A fresh store runs one committed statement per migration, and on Windows each
// commit waits for the disk twice, for the database and for the journal file it
// creates and deletes. That adds about two seconds to every app.New, which most
// tests in internal/app and internal/api call. What the two settings give up
// is only surviving a power cut or a killed process, which no test does:
// transactions, rollback and reopening a store in the same process behave as
// they do in the shipped binary.
func VolatileSQLite() {
	volatileOnce.Do(func() {
		sqlite.RegisterConnectionHook(func(c sqlite.ExecQuerierContext, _ string) error {
			// Errors are dropped so a test that opens a damaged file on purpose
			// fails where the shipped binary fails, not in this hook.
			for _, pragma := range []string{`PRAGMA synchronous = OFF`, `PRAGMA journal_mode = MEMORY`} {
				_, _ = c.ExecContext(context.Background(), pragma, []driver.NamedValue{})
			}
			return nil
		})
	})
}
