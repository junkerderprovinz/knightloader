package idleaction

import (
	"context"
	"os"
	"strings"
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

func TestTheCommandIsNotGivenTheServiceKeys(t *testing.T) {
	t.Setenv("KL_TORBOX", "service-key-e41b")
	out, _ := ExecRunner(context.Background(), execxtest.Program(t, execxtest.ListKL))
	if !strings.Contains(out, "KL variables:") {
		t.Fatalf("the command did not list what it was given:\n%s", out)
	}
	if strings.Contains(out, "service-key-e41b") {
		t.Errorf("the command was given KL_TORBOX:\n%s", out)
	}
}
