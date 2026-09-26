package reconnect

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

func TestTheProgramIsNotGivenTheServiceKeys(t *testing.T) {
	t.Setenv("KL_TORBOX", "service-key-e41b")
	err := execRunner(context.Background(), execxtest.Program(t, execxtest.ListKL))
	if err == nil || !strings.Contains(err.Error(), "KL variables:") {
		t.Fatalf("the program's listing is not in the error: %v", err)
	}
	if strings.Contains(err.Error(), "service-key-e41b") {
		t.Errorf("the program was given KL_TORBOX: %v", err)
	}
}
