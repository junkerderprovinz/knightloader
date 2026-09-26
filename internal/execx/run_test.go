package execx

import (
	"bytes"
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
