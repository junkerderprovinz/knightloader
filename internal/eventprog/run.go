package eventprog

import "context"

// Runner starts one program with an argument list and an environment and
// reports its combined output. A function type, like idleaction.Runner, so a
// test can see what would have run without starting anything; unlike that one
// it takes the environment, which is half of what an event hands over.
//
// The default is execx.Run, whose output comes back uncut but for its cap: the
// dispatcher redacts it before it trims it, so a token cannot survive the cut
// as a prefix the redaction cannot match.
type Runner func(ctx context.Context, program string, args, env []string) (string, error)
