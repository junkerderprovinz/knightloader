package startupcheck

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// The fake binary these tests run is this test binary itself, re-entered with
// -test.run=TestHelperProcess and told by an environment variable what to
// pretend to be. It is the standard os/exec idiom and it is here rather than a
// compiled fixture for a reason that matters to what is being tested: what
// needs proving is how this package treats a process that prints a banner, one
// that exits non-zero, and one that never answers, and none of those needs a
// real ffmpeg. Building one per case would also add a `go build` to every run
// of the suite, including under -race.
const helperEnv = "KL_STARTUPCHECK_HELPER"

// helperTool is a ToolTarget wired to the helper above. Resolve is used rather
// than Bin because os.Args[0] is an absolute path that is deliberately not on
// PATH, which is precisely what Resolve exists for (java, whose lookup prefers
// JAVA_HOME).
func helperTool(t *testing.T, id, mode string) ToolTarget {
	t.Helper()
	t.Setenv(helperEnv, mode)
	exe := os.Args[0]
	return ToolTarget{
		ID:      id,
		Bin:     exe,
		Args:    []string{"-test.run=TestHelperProcess"},
		Resolve: func() (string, error) { return exe, nil },
	}
}

// TestHelperProcess is not a test. It is the fake binary: it exits immediately,
// before the testing framework can print anything of its own, so whatever it
// writes is the whole of the subprocess's output.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		return // an ordinary run of the suite; there is no subprocess here
	}
	switch mode {
	case "version":
		fmt.Println("yt-dlp 2026.09.01")
		os.Exit(0)
	case "banner":
		// What `ffmpeg -version` really does: a version line, then several
		// hundred bytes of configure flags. Both halves are the point - the
		// first line has to survive and the rest must not.
		fmt.Println("ffmpeg version 7.1 Copyright (c) 2000-2026 the FFmpeg developers")
		fmt.Println("built with gcc 14.2.0")
		fmt.Println("configuration: " + strings.Repeat("--enable-something ", 60))
		os.Exit(0)
	case "blankfirst":
		fmt.Println()
		fmt.Println("   ")
		fmt.Println("Python 3.12.7")
		os.Exit(0)
	case "broken":
		fmt.Fprintln(os.Stderr, "ModuleNotFoundError: No module named 'yt_dlp'")
		os.Exit(1)
	case "hang":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(2)
}

func TestToolReportsTheVersionItPrinted(t *testing.T) {
	c := Tool(context.Background(), helperTool(t, IDYtdlp, "version"), time.Minute)

	if c.Verdict != VerdictOK || c.Code != "" {
		t.Fatalf("verdict = %q code = %q err = %q, want a clean ok row", c.Verdict, c.Code, c.Err)
	}
	if c.Detail != "yt-dlp 2026.09.01" {
		t.Errorf("detail = %q, want the version line", c.Detail)
	}
	if c.Subject == "" {
		t.Error("subject is empty; the path the binary was found at is half the answer to 'which yt-dlp'")
	}
}

// TestToolKeepsOnlyTheFirstLineAndClampsIt. `ffmpeg -version` answers with
// several hundred bytes including the whole configure line; unclamped that goes
// into the container log, fills a fifth of the 500-line ring and rides in every
// downloaded bundle.
func TestToolKeepsOnlyTheFirstLineAndClampsIt(t *testing.T) {
	c := Tool(context.Background(), helperTool(t, IDFfmpeg, "banner"), time.Minute)

	if c.Verdict != VerdictOK {
		t.Fatalf("verdict = %q, want ok", c.Verdict)
	}
	if strings.ContainsAny(c.Detail, "\n\r") {
		t.Errorf("detail carries more than one line: %q", c.Detail)
	}
	if !strings.HasPrefix(c.Detail, "ffmpeg version 7.1") {
		t.Errorf("detail = %q, want the version line rather than whatever came after it", c.Detail)
	}
	if n := len([]rune(c.Detail)); n > maxDetail+1 {
		t.Errorf("detail is %d runes, want at most %d plus the ellipsis", n, maxDetail)
	}
}

// TestToolSkipsBlankLeadingOutput: several of these binaries put a warning, or
// nothing at all, on their first line and the version on the second. Taking line
// zero blind reports an empty version for a tool that answered perfectly.
func TestToolSkipsBlankLeadingOutput(t *testing.T) {
	c := Tool(context.Background(), helperTool(t, IDJava, "blankfirst"), time.Minute)
	if c.Detail != "Python 3.12.7" {
		t.Errorf("detail = %q, want the first line with anything on it", c.Detail)
	}
}

// TestToolNotFoundAndNotRunnableAreDifferentAnswers. A yt-dlp whose Python
// environment broke is installed; answering "not found" for it sends somebody to
// install a thing they already have, and the tool's own first line is the only
// part of the failure worth quoting.
func TestToolNotFoundAndNotRunnableAreDifferentAnswers(t *testing.T) {
	missing := Tool(context.Background(), ToolTarget{
		ID:   IDYtdlp,
		Bin:  "kl-no-such-binary-8f2ca1",
		Args: []string{"--version"},
	}, time.Minute)
	if missing.Verdict != VerdictFail || missing.Code != CodeNotFound {
		t.Errorf("absent binary: verdict = %q code = %q, want fail/%s", missing.Verdict, missing.Code, CodeNotFound)
	}
	if missing.Subject != "kl-no-such-binary-8f2ca1" {
		t.Errorf("subject = %q, want the name that was looked for", missing.Subject)
	}

	broken := Tool(context.Background(), helperTool(t, IDYtdlp, "broken"), time.Minute)
	if broken.Verdict != VerdictFail || broken.Code != CodeNotRunnable {
		t.Errorf("broken binary: verdict = %q code = %q, want fail/%s", broken.Verdict, broken.Code, CodeNotRunnable)
	}
	if !strings.Contains(broken.Err, "ModuleNotFoundError") {
		t.Errorf("err = %q, want the tool's own first line rather than \"exit status 1\"", broken.Err)
	}
}

// TestToolTimeoutRatherThanAHang. The one version probe this tree already had
// runs with no context at all, which is a latent hang inside app.New; copying
// that shape for three more binaries would have multiplied it by four.
func TestToolTimeoutRatherThanAHang(t *testing.T) {
	start := time.Now()
	c := Tool(context.Background(), helperTool(t, IDFfprobe, "hang"), 150*time.Millisecond)
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("the probe took %s; the deadline did nothing", took)
	}
	if c.Code != CodeTimeout {
		t.Errorf("code = %q, want %q", c.Code, CodeTimeout)
	}
}

// TestToolOptionalDowngradesTheVerdict. ffprobe is read for the length check on
// a finished video and nothing else, so a red row for it would be a false alarm
// about something nobody depends on.
func TestToolOptionalDowngradesTheVerdict(t *testing.T) {
	c := Tool(context.Background(), ToolTarget{
		ID:       IDFfprobe,
		Bin:      "kl-no-such-binary-8f2ca1",
		Args:     []string{"-version"},
		Optional: true,
	}, time.Minute)
	if c.Verdict != VerdictWarn {
		t.Errorf("verdict = %q, want %q for an optional tool that is missing", c.Verdict, VerdictWarn)
	}
	if c.Code != CodeNotFound {
		t.Errorf("code = %q, want %q - the verdict softens, the diagnosis does not change", c.Code, CodeNotFound)
	}
}

// TestToolSkippedIsNotFailed. A box pointing KL_JD at a JDownloader running
// somewhere else starts no Java of its own, and one false alarm is enough for an
// operator to stop reading the report.
func TestToolSkippedIsNotFailed(t *testing.T) {
	c := Tool(context.Background(), ToolTarget{
		ID:       IDJava,
		Bin:      "kl-no-such-binary-8f2ca1",
		Args:     []string{"--version"},
		SkipCode: "javaNotNeeded",
	}, time.Minute)
	if c.Verdict != VerdictSkipped {
		t.Errorf("verdict = %q, want %q", c.Verdict, VerdictSkipped)
	}
	if c.Code != "javaNotNeeded" {
		t.Errorf("code = %q, want the reason it was skipped", c.Code)
	}
	if c.Subject != "" || c.Err != "" {
		t.Errorf("a skipped tool was still looked for: subject = %q err = %q", c.Subject, c.Err)
	}
}

// TestToolAppearedLate is the quiet one: the binary runs perfectly now and its
// resolver is not in the live routing table, so it was installed, fixed or
// mounted after this instance started. Everything looks right from a shell and
// nothing is being handed to it.
func TestToolAppearedLate(t *testing.T) {
	registered := false
	target := helperTool(t, IDYtdlp, "version")
	target.Registered = &registered

	c := Tool(context.Background(), target, time.Minute)
	if c.Verdict != VerdictWarn || c.Code != CodeAppearedLate {
		t.Errorf("verdict = %q code = %q, want warn/%s", c.Verdict, c.Code, CodeAppearedLate)
	}
	if c.Detail == "" {
		t.Error("detail is empty; the version it printed is what proves it really does run now")
	}

	registered = true
	if c := Tool(context.Background(), target, time.Minute); c.Verdict != VerdictOK || c.Code != "" {
		t.Errorf("with the resolver registered: verdict = %q code = %q, want a clean ok row", c.Verdict, c.Code)
	}
}
