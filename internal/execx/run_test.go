package execx

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/execx/execxtest"
)

func TestMain(m *testing.M) {
	execxtest.Main()
	os.Exit(m.Run())
}

func TestTheEndOfTheContextEndsWhatTheProgramStarted(t *testing.T) {
	program := execxtest.Program(t, execxtest.Spawn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, _ := Run(ctx, program, nil, nil)
	execxtest.AwaitGone(t, execxtest.Child(t, out))
}

func TestACleanExitIsNotHeldUpByWhatItLeftInTheBackground(t *testing.T) {
	program := execxtest.Program(t, execxtest.Detach)
	started := time.Now()
	out, err := Run(context.Background(), program, nil, nil)
	took := time.Since(started)
	pid := execxtest.Child(t, out)

	if took > 20*time.Second {
		t.Errorf("Run took %s; it waited for the child that still holds the output", took)
	}
	if err != nil {
		t.Errorf("Run = %v, want the program's own clean exit", err)
	}
	if execxtest.Gone(pid) {
		t.Error("the child the program left running in the background was killed")
	}
}

func TestTheInstancesOwnKLVariablesAreNotHandedDown(t *testing.T) {
	base := []string{"PATH=/usr/bin", "KL_TORBOX=secret-key", "kl_alldebrid=other-secret", "KL_NAME=stale", "HOME=/home/x"}
	env := environ(base, []string{"KL_NAME=a.mkv"})
	joined := strings.Join(env, "\n")
	for _, leak := range []string{"secret-key", "other-secret", "stale"} {
		if strings.Contains(joined, leak) {
			t.Errorf("the program's environment carries %q:\n%s", leak, joined)
		}
	}
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/x", "KL_NAME=a.mkv"} {
		if !slices.Contains(env, want) {
			t.Errorf("the environment lacks %s:\n%s", want, joined)
		}
	}
}

func TestAProgramIsNotGivenTheServiceKeys(t *testing.T) {
	t.Setenv("KL_TORBOX", "service-key-e41b")
	program := execxtest.Program(t, execxtest.ListKL)
	out, _ := Run(context.Background(), program, nil, []string{"KL_EVENT=task.done"})
	if !strings.Contains(out, "KL variables:") || !strings.Contains(out, "KL_EVENT=task.done") {
		t.Fatalf("the program did not list what it was given:\n%s", out)
	}
	if strings.Contains(out, "service-key-e41b") {
		t.Errorf("the program was given KL_TORBOX:\n%s", out)
	}
}

func TestOutputBeyondTheCapIsReadAndDropped(t *testing.T) {
	var c cappedBuffer
	chunk := bytes.Repeat([]byte("x"), keptOutput-10)
	for range 3 {
		if n, err := c.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v; a short write would stop the program's output", n, err)
		}
	}
	if c.buf.Len() != keptOutput {
		t.Errorf("kept %d bytes, want %d", c.buf.Len(), keptOutput)
	}
}
