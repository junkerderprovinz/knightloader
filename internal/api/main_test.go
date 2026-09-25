package api

import (
	"os"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// The tests here stay serial. Handler sets package variables such as
// discoveryRefresh, so two test servers built side by side would race, and a
// settings save on one would call the other's closure.
func TestMain(m *testing.M) {
	testenv.VolatileSQLite()
	testenv.NoDNS()
	testenv.NoYtdlp()
	os.Exit(m.Run())
}
