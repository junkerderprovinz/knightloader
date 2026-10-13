package relay_test

import (
	"os"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

func TestMain(m *testing.M) {
	os.Exit(testenv.RunWithoutProjectRelay(m))
}
