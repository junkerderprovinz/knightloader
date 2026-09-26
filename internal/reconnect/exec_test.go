package reconnect

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/execx/execxtest"
)

func TestMain(m *testing.M) {
	execxtest.Main()
	os.Exit(m.Run())
}

// The command and the script's interpreter both run through execRunner, and a
// shutdown cancels its context. What the program started must end with it, or
// the shutdown waits for it.
func TestACancelledRunEndsWhatTheProgramStarted(t *testing.T) {
	program := execxtest.Program(t, execxtest.Spawn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := execRunner(ctx, program)
	if err == nil {
		t.Fatal("a program killed at the end of its context was reported as a success")
	}
	execxtest.AwaitGone(t, execxtest.Child(t, err.Error()))
}
