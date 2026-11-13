package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// failingYtdlp is a yt-dlp that answers --version and fails every other run
// with line on stderr, as the real one does for a link it cannot fetch.
func failingYtdlp(t *testing.T, line string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ytdlp-stub")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 2026.10.26; exit 0; fi\necho '" + line + "' >&2\nexit 1\n"
	if runtime.GOOS == "windows" {
		bin += ".bat"
		script = "@if \"%1\"==\"--version\" (echo 2026.10.26& exit /b 0)\r\n@echo " + line + " 1>&2\r\n@exit /b 1\r\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if exec.Command(bin, "--version").Run() != nil {
		t.Skip("the yt-dlp stub is not executable here")
	}
	return bin
}

// Another attempt meets the same paywall, so the row settles at once with the
// reason the cookie advice is offered for.
func TestAPaidPostFailsAtOnceAsMembersOnly(t *testing.T) {
	t.Setenv("KL_YTDLP", failingYtdlp(t, "ERROR: [patreon] 110207086: You do not have access to this post"))
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.AutoConfirm = true })
	const url = "https://www.patreon.com/posts/a-paid-post-110207086"
	a.AddLinksFrom([]string{url}, "", OriginPaste)

	// Every row of the link runs yt-dlp, and each meets the same answer.
	waitFor(t, "every row of the link failing", func() bool {
		rows := tasksSharingURL(a, url)
		for _, x := range rows {
			if x.Status != core.StatusError {
				return false
			}
		}
		return len(rows) > 0
	})
	for _, x := range tasksSharingURL(a, url) {
		if x.Reason != core.ReasonMembersOnly || x.ErrorCode != core.CodeMembersOnly {
			t.Errorf("the %s row failed with reason %q and code %q, want members-only for both", x.Variant, x.Reason, x.ErrorCode)
		}
		if !x.GaveUp || x.Retries != 0 || !x.NextTry.IsZero() {
			t.Errorf("the %s row has given up %v after %d retries with one due at %v, want it given up with none", x.Variant, x.GaveUp, x.Retries, x.NextTry)
		}
	}
}
