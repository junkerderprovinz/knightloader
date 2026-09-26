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
	"os"
	"os/exec"
	"strings"
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

// Run starts program with args and waits for it to end. The program gets this
// process's environment without the instance's own KL_* variables, and env on
// top of it.
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
	cmd.Env = environ(os.Environ(), env)
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

// ownPrefix is what this instance's own configuration variables start with.
// KL_TORBOX and its neighbours are service keys, so none of them is handed to
// a program, and none can shadow a variable a caller hands over.
const ownPrefix = "KL_"

// environ is base without this instance's own variables, followed by extra.
//
// The prefix is compared without regard to case, because Windows looks up
// environment names that way and a leftover "kl_torbox" would reach the
// program there.
func environ(base, extra []string) []string {
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		if len(kv) >= len(ownPrefix) && strings.EqualFold(kv[:len(ownPrefix)], ownPrefix) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
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
