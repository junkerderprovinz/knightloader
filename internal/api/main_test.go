package api

import (
	"os"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// A test that touches process-wide state stays serial: an environment
// variable, a buildinfo value, the relay TTL, or the log ring the diagnostics
// read. Every other test calls t.Parallel, and Go starts those only once the
// serial ones have finished.
func TestMain(m *testing.M) {
	testenv.VolatileSQLite()
	testenv.NoDNS()
	testenv.NoYtdlp()
	os.Exit(m.Run())
}
