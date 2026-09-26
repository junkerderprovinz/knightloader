// Package execx starts the programs an operator configures: the end-of-queue
// command, the event programs and the reconnect command or script. They all
// run the same way, without a shell, as a tree that the end of the context
// kills as a whole, and on Windows with a batch file's arguments quoted for
// cmd.exe.
package execx

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// keptOutput is how much of a program's output is held while it runs. The
// rest is read and thrown away rather than left in the pipe, where a program
// that writes a lot would block on a full buffer until it timed out.
const keptOutput = 4096

// waitDelay bounds how long Run waits for the output to close once the program
// has exited or been killed. Something the program left running in the
// background can hold the pipe open for as long as it lives.
const waitDelay = 2 * time.Second

// Run starts program with args and waits for it to end. A nil env passes on
// this process's environment, as os/exec does.
//
// The end of ctx kills the program and everything it started. A program that
// exits on its own is left to it: what it put in the background keeps running,
// and a clean exit is reported as clean even when that background work still
// holds the output.
//
// The output is stdout and stderr in the order they were written, cut at
// keptOutput and otherwise untouched, so a caller can redact it before it
// trims it further.
func Run(ctx context.Context, program string, args, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = env
	var out cappedBuffer
	// One writer for both streams, so os/exec calls it from one goroutine at
	// a time and the lines keep the order the program wrote them in.
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.WaitDelay = waitDelay
	err := run(cmd)
	if errors.Is(err, exec.ErrWaitDelay) {
		// os/exec reports this only for a program that exited with status 0
		// and was never cancelled.
		err = nil
	}
	return out.buf.String(), err
}

// cappedBuffer keeps the first keptOutput bytes and reports every write as
// taken in full.
type cappedBuffer struct {
	buf bytes.Buffer
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := keptOutput - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}
