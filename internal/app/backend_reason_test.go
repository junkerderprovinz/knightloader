package app

// core.Update.Reason: a backend that read its tool's full output knows the
// cause better than a regex over the truncated sentence that reaches this
// package. These tests cover that contract and the policies reading it; the
// causes come from yt-dlp (internal/resolver/ytdlp/diagnose.go), but any
// backend may set one.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// botCheckErr is a bot check's error sentence. classify reads the "503" in its
// URL as a service outage before looking at any phrase, which is why the
// backend's verdict has to win.
const botCheckErr = "yt-dlp: ERROR: [youtube] dQw4w9WgXcQ: Sign in to confirm you're not a bot. " +
	"See  https://example.invalid/wiki/FAQ#status/503  for how to manually pass cookies."

// The first check proves the classifier disagrees, or the test could pass
// without the verdict winning.
func TestABackendsOwnVerdictBeatsTheClassifier(t *testing.T) {
	if got := classify(failure{text: botCheckErr}); got == core.ReasonBotCheck {
		t.Fatalf("the classifier already answers %q for this sentence, so nothing below can show a verdict winning over it", got)
	}
	a := retryApp(t, func(*settings.Settings) {})
	runningOn(a, "bot1", plainHost, simpleResolverID)

	a.onUpdate("bot1", core.Update{Status: core.StatusError, Err: botCheckErr, Reason: core.ReasonBotCheck})

	if got := liveTask(a, "bot1").Reason; got != core.ReasonBotCheck {
		t.Errorf("Reason = %q, want %q; the backend's own verdict was thrown away and the sentence re-read", got, core.ReasonBotCheck)
	}
}

// An empty Reason means no opinion, and the sentence is classified as before.
func TestAnUpdateWithNoVerdictStillGoesThroughTheClassifier(t *testing.T) {
	a := retryApp(t, func(*settings.Settings) {})
	runningOn(a, "plain1", plainHost, simpleResolverID)

	a.onUpdate("plain1", core.Update{Status: core.StatusError, Err: "rapidgator: HTTP 429 too many requests"})

	if got := liveTask(a, "plain1").Reason; got != core.ReasonLimit {
		t.Errorf("Reason = %q, want %q; an update carrying no verdict stopped being classified", got, core.ReasonLimit)
	}
}

// These causes are not retried. Retrying a bot check makes it worse: every
// request from a flagged address confirms the flag.
func TestTheBackendNamedCausesSettleAsGivenUp(t *testing.T) {
	for _, reason := range []core.Reason{
		core.ReasonBotCheck, core.ReasonMembersOnly, core.ReasonGeoBlocked,
		core.ReasonDRM, core.ReasonExtractorBroken,
	} {
		t.Run(string(reason), func(t *testing.T) {
			a := retryApp(t, func(*settings.Settings) {})
			runningOn(a, "t1", plainHost, simpleResolverID)

			a.onUpdate("t1", core.Update{Status: core.StatusError, Err: "yt-dlp: ERROR: something", Reason: reason})

			got := liveTask(a, "t1")
			if got.Reason != reason {
				t.Fatalf("Reason = %q, want %q; this test cannot reach the branch it is about", got.Reason, reason)
			}
			if !got.GaveUp {
				t.Error("settled as an ordinary failure, so the retry count on the Advanced page can buy more attempts against it")
			}
			if !got.NextTry.IsZero() {
				t.Errorf("an automatic retry is pending at %s", got.NextTry)
			}
			if got.Retries != 0 {
				t.Errorf("Retries = %d, want an attempt that was never made not to be counted", got.Retries)
			}
		})
	}
}

// None of these causes triggers a reconnect, not even the bot check, whose flag
// is on the address (see addressMayHelp).
func TestTheBackendNamedCausesNeverRebootTheRouter(t *testing.T) {
	for _, reason := range []core.Reason{
		core.ReasonBotCheck, core.ReasonMembersOnly, core.ReasonGeoBlocked,
		core.ReasonDRM, core.ReasonExtractorBroken,
	} {
		if addressMayHelp(reason) {
			t.Errorf("%q would take the whole house off the internet, and a new address does not mend it", reason)
		}
	}
}

// Unlike retries and reconnects, a mirror is still tried: it is a different
// site, and none of these causes is a fact about the file.
func TestAMirrorIsStillWorthTryingForTheBackendNamedCauses(t *testing.T) {
	for _, reason := range []core.Reason{
		core.ReasonBotCheck, core.ReasonMembersOnly, core.ReasonGeoBlocked,
		core.ReasonDRM, core.ReasonExtractorBroken,
	} {
		if !mirrorCanHelp(reason) {
			t.Errorf("%q blocks the handover to a second source, which may well not have this problem at all", reason)
		}
	}
}
