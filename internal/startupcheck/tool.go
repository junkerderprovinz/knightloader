package startupcheck

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Tool finds one binary and runs it once for its version.
//
// FOUR ANSWERS AND NOT TWO. "Not on PATH" and "on PATH and will not run" are
// different problems with different remedies: a yt-dlp whose Python environment
// broke prints a traceback and exits non-zero, and answering CodeNotFound for
// that sends somebody to install a thing that is already installed. The third
// is "needed by nobody here" (SkipCode), and the fourth is the quiet one -
// CodeAppearedLate, a binary that runs perfectly now and was not there when
// this instance started, so nothing is routed to it and nothing says so.
//
// ALWAYS UNDER A DEADLINE. The one version probe this tree already had
// (ytdlp.Backend.Available) runs exec.Command(...).Run() with no context at
// all, which is a latent hang: a `yt-dlp` that is a wrapper script waiting on
// something takes app.New down with it. Copying that shape for three more
// binaries would have multiplied it by four.
func Tool(ctx context.Context, t ToolTarget, timeout time.Duration) Check {
	c := Check{ID: t.ID}

	if t.SkipCode != "" {
		// Skipped, never failed. A red "Java missing" row on a box that points
		// KL_JD at a JDownloader running somewhere else is a false alarm about
		// a deliberate configuration, and one false alarm is enough for an
		// operator to stop reading the report at all.
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

	// CombinedOutput and not Output, because `java -version` prints to stderr -
	// it is the two-dash `--version` (JDK 9+) that goes to stdout, and reading
	// stdout alone would report "found, version unknown" for every healthy JVM
	// in the shipped image. Reading both costs nothing and is right for all
	// four binaries.
	out, runErr := exec.CommandContext(runCtx, path, t.Args...).CombinedOutput()
	line := firstLine(string(out))

	if runCtx.Err() != nil {
		// Checked BEFORE runErr and without asking which of the two deadlines
		// did it. A killed process comes back as "signal: killed" or "exit
		// status 1" depending on the platform, and reporting that as
		// CodeNotRunnable would tell somebody their perfectly good ffmpeg is
		// broken when the truth is that it was not given time to answer.
		// Whether it was this tool's own five seconds or the whole pass's
		// budget that ran out, the finding is the same: it did not answer.
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

	// FIRST LINE ONLY, CLAMPED. `ffmpeg -version` answers with several hundred
	// bytes including the whole configure line; unclamped it goes into the
	// container log, fills a fifth of the 500-line ring, and rides in every
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
