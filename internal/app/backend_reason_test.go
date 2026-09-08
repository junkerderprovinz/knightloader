package app

// core.Update.Reason: a backend that read the whole of its own tool's output
// beating a regex over the one truncated sentence that reached this package,
// and the two policies that then read the verdict.
//
// The failures behind it are yt-dlp's (internal/resolver/ytdlp/diagnose.go),
// but nothing here reaches into that package: what is under test is the
// contract, which is open to any backend that can tell its own failures apart.

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// botCheckErr is the sentence a bot check arrives with, and the URL in it is
// the point. classify() pulls an HTTP status out of any text it is handed,
// before it looks at a single phrase, so this sentence classifies as a service
// outage - which is why the verdict cannot be left to it.
const botCheckErr = "yt-dlp: ERROR: [youtube] dQw4w9WgXcQ: Sign in to confirm you're not a bot. " +
	"See  https://example.invalid/wiki/FAQ#status/503  for how to manually pass cookies."

// TestABackendsOwnVerdictBeatsTheClassifier is the whole contract in one
// assertion, and it opens by proving the branch it is about: without the first
// check this test would keep passing if the classifier happened to agree.
func TestABackendsOwnVerdictBeatsTheClassifier(t *testing.T) {
	if got := classify(failure{text: botCheckErr}); got == core.ReasonBotCheck {
		t.Fatalf("the classifier already answers %q for this sentence, so nothing below can show a verdict winning over it", got)
	}
	a := retryApp(t, func(*settings.Settings) {})
	runningOn(a, "bot1", plainHost, simpleResolverID)

	a.onUpdate("bot1", core.Update{Status: core.StatusError, Err: botCheckErr, Reason: core.ReasonBotCheck})

	if got := liveTask(a, "bot1").Reason; got != core.ReasonBotCheck {
		t.Errorf("Reason = %q, want %q - the backend's own verdict was thrown away and the sentence re-read", got, core.ReasonBotCheck)
	}
}

// TestAnUpdateWithNoVerdictStillGoesThroughTheClassifier is the other half of
// "empty means no opinion": every backend but one sets nothing here, and their
// failures must be named exactly as they always were.
func TestAnUpdateWithNoVerdictStillGoesThroughTheClassifier(t *testing.T) {
	a := retryApp(t, func(*settings.Settings) {})
	runningOn(a, "plain1", plainHost, simpleResolverID)

	a.onUpdate("plain1", core.Update{Status: core.StatusError, Err: "rapidgator: HTTP 429 too many requests"})

	if got := liveTask(a, "plain1").Reason; got != core.ReasonLimit {
		t.Errorf("Reason = %q, want %q - an update carrying no verdict stopped being classified", got, core.ReasonLimit)
	}
}

// TestTheBackendNamedCausesSettleAsGivenUp is the retry policy. A bot check is
// the sharp one: every further request from an address a site has already
// flagged is more evidence for the flag, so an armed retry here is not merely
// wasted, it works against the person who is waiting for the download.
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
				t.Fatalf("Reason = %q, want %q - this test cannot reach the branch it is about", got.Reason, reason)
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

// TestTheBackendNamedCausesNeverRebootTheRouter is the reconnect veto. The bot
// check is the one somebody will want to argue about, since its flag really is
// on the address - see addressMayHelp for why the answer is still no.
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

// TestAMirrorIsStillWorthTryingForTheBackendNamedCauses pins the deliberate
// asymmetry beside the two vetoes above: a mirror is a different SITE, and not
// one of these five is a fact about the file. Written as a test because it is
// the sort of decision that otherwise gets "tidied up" into consistency with
// its neighbours.
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
