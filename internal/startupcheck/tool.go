package startupcheck

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Tool finds one binary and runs it once for its version.
//
// Four answers and not two. "Not on PATH" and "on PATH and will not run" are
// different problems with different remedies: a yt-dlp whose Python
// environment broke prints a traceback and exits non-zero, and CodeNotFound
// for that sends somebody to install what is already installed. The third is
// "needed by nobody here" (SkipCode), and the fourth is CodeAppearedLate, a
// binary that runs now and was not there when this instance started, so
// nothing is routed to it and nothing says so.
//
// Always under a deadline. ytdlp.Backend.Available runs
// exec.Command(...).Run() with no context at all, which is a latent hang: a
// `yt-dlp` that is a wrapper script waiting on something takes app.New down
// with it.
func Tool(ctx context.Context, t ToolTarget, timeout time.Duration) Check {
	c := Check{ID: t.ID}

	if t.SkipCode != "" {
		// Skipped and never failed. A red "Java missing" row on a box that
		// points KL_JD at a JDownloader running somewhere else is a false
		// alarm about a chosen configuration, and one false alarm is enough
		// for an operator to stop reading the report.
		c.Verdict = VerdictSkipped
		c.Code = t.SkipCode
		return c
	}

	bad := VerdictFail
	if t.Optional {
		bad = VerdictWarn
	}

	path, err := resolveTool(t)
	if err != nil {
		c.Verdict = bad
		c.Code = CodeNotFound
		c.Subject = t.Bin
		c.Err = clamp(err.Error())
		return c
	}
	c.Subject = path

	if ctx.Err() != nil {
		c.Verdict = VerdictWarn
		c.Code = CodeTimeout
		return c
	}
	if timeout <= 0 {
		timeout = DefaultToolTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// CombinedOutput and not Output, because `java -version` prints to
	// stderr: it is the two-dash `--version` (JDK 9+) that goes to stdout,
	// and reading stdout alone would report "found, version unknown" for
	// every healthy JVM in the shipped image.
	out, runErr := exec.CommandContext(runCtx, path, t.Args...).CombinedOutput()
	line := firstLine(string(out))

	if runCtx.Err() != nil {
		// Checked before runErr and without asking which of the two
		// deadlines did it. A killed process comes back as "signal: killed"
		// or "exit status 1" depending on the platform, and CodeNotRunnable
		// would tell somebody their working ffmpeg is broken when it was
		// only not given time to answer.
		c.Verdict = bad
		c.Code = CodeTimeout
		c.Err = clamp(line)
		return c
	}
	if runErr != nil {
		c.Verdict = bad
		c.Code = CodeNotRunnable
		// The tool's own first line, which for a broken Python install is the
		// headline of the traceback and the only part worth quoting. The exec
		// error ("exit status 1") says nothing at all on its own, so it is the
		// fallback and not the first choice.
		if line == "" {
			line = runErr.Error()
		}
		c.Err = clamp(line)
		return c
	}

	// The first line only, clamped. `ffmpeg -version` answers with several
	// hundred bytes including the whole configure line, and unclamped it goes
	// into the container log, fills a fifth of the ring and rides in every
	// downloaded bundle.
	c.Detail = clamp(line)
	c.Verdict = VerdictOK

	if t.Registered != nil && !*t.Registered {
		// It runs, and the routing table does not have it: it was installed,
		// fixed or mounted after this instance started. Everything looks right
		// from a shell and nothing is being handed to it, which is precisely
		// the state nobody would ever think to check for.
		c.Verdict = VerdictWarn
		c.Code = CodeAppearedLate
	}
	return c
}

// resolveTool is the tool's own lookup where it has one, and PATH otherwise.
func resolveTool(t ToolTarget) (string, error) {
	if t.Resolve != nil {
		return t.Resolve()
	}
	return exec.LookPath(strings.TrimSpace(t.Bin))
}
