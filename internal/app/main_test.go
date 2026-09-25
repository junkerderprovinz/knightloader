package app

import (
	"os"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// A test whose time goes into waiting out a timer calls t.Parallel, so those
// waits overlap. Such a test builds its App with newApp, and it must not swap
// a package variable such as autoConfirmSecond or idleActionPoll, or the
// other parallel tests would run with the swapped value.
func TestMain(m *testing.M) {
	testenv.VolatileSQLite()
	testenv.NoDNS()
	testenv.NoYtdlp()
	os.Exit(m.Run())
}

// appBuild lets one test at a time build an App. New starts the download
// engine, whose constructor sets a variable of its logging library, and the
// race detector reports two parallel tests doing that at once.
var appBuild sync.Mutex

// newApp is New for a test that may run in parallel with others.
func newApp(dataDir string) (*App, error) {
	appBuild.Lock()
	defer appBuild.Unlock()
	return New(dataDir)
}
