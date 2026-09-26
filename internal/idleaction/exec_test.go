package idleaction

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

// A command that sends a job to the background and then hangs would otherwise
// leave that job running past the limit.
func TestTheTimeLimitEndsWhatTheCommandStarted(t *testing.T) {
	program := execxtest.Program(t, execxtest.Spawn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, _ := ExecRunner(ctx, program)
	execxtest.AwaitGone(t, execxtest.Child(t, out))
}
