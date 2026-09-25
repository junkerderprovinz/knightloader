package eventprog

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// Runner starts one program with an argument list and an environment and
// reports its combined output. A function type, like idleaction.Runner, so a
// test can see what would have run without starting anything; unlike that one
// it takes the environment, which is half of what an event hands over.
type Runner func(ctx context.Context, program string, args, env []string) (string, error)

// keptOutput is how much of a program's output is held while it runs. The
// rest is read and thrown away rather than left in the pipe, where a program
// that writes a lot would block on a full buffer until it timed out.
const keptOutput = 4096

// waitDelay bounds how long a killed program's pipes are waited on. A program
// that started a child of its own can leave that child holding the pipe after
// the parent is gone, and Wait would otherwise sit there with it.
const waitDelay = 2 * time.Second

// ExecRunner is the default Runner. exec.CommandContext starts the program
// itself, never a shell, and runProgram makes the end of ctx end whatever the
// program started along with it.
//
// The output comes back uncut beyond keptOutput: the caller redacts it before
// it trims it, so a token cannot survive the cut as a prefix the redaction
// cannot match.
func ExecRunner(ctx context.Context, program string, args, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = env
	var out cappedBuffer
	// One writer for both streams, so os/exec calls it from one goroutine at
	// a time and the lines keep the order the program wrote them in.
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.WaitDelay = waitDelay
	err := runProgram(cmd)
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
