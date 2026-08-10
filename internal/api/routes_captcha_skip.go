package api

// Giving up on one captcha challenge - POST /api/captcha/{id}/skip, body
// {"scope": "skip-once" | "blacklist-hoster" | "blacklist-everywhere"}, the
// exact three names captcha.AbortScope already exports
// (internal/captcha/challenge.go) so this route does not invent a second
// vocabulary for the choice 7F's Source.Abort already takes a value from.
//
// DOES THIS NEED ITS OWN BLACKLIST, OR DOES JD ALREADY KEEP ONE? Answered
// from JD's own source rather than assumed, because build-plan.md section 8's
// Wave 7 note is explicit that the answer decides whether this file is a
// thin pass-through or a second store:
//
// Yes - JD keeps its own, and calling Abort with the right scope is enough.
// Traced past where jdsource.go's own citations stop (its package comment
// covers CaptchaAPISolver.skip(id, SkipRequest) being what /captcha/skip
// actually calls; this continues into what skip() itself triggers):
//
//   - CaptchaAPISolver.skip() calls
//     ChallengeResponseController.getInstance().setSkipRequest(type, this,
//     challenge) (org/jdownloader/api/captcha/CaptchaAPISolver.java), which
//     records the SkipRequest on the matching SolverJob(s)
//     (org/jdownloader/captcha/v2/ChallengeResponseController.java).
//   - ChallengeResponseController.handle(), the method every plugin's captcha
//     call blocks in, reads that back and throws
//     SkipException(challenge, skipRequest) once the job resolves with one
//     set (same file).
//   - The per-challenge-type helper that catches it - verified in two
//     independent, byte-identical implementations,
//     org/jdownloader/captcha/v2/challenge/recaptcha/v2/CaptchaHelperHostPluginRecaptchaV2.java
//     and .../hcaptcha/CaptchaHelperHostPluginHCaptcha.java, both
//     `catch (SkipException e) { switch (e.getSkipRequest()) { case
//     BLOCK_HOSTER: CaptchaBlackList.getInstance().add(new
//     BlockDownloadCaptchasByHost(link.getHost())); ... case
//     BLOCK_ALL_CAPTCHAS: CaptchaBlackList.getInstance().add(new
//     BlockAllDownloadCaptchasEntry()); ... }` - is what actually files the
//     block, in org/jdownloader/captcha/blacklist.
//   - That filing is what the NEXT captcha on that plugin checks BEFORE
//     creating a new challenge at all: the same two helpers open with
//     `CaptchaBlackList.getInstance().matches(challenge); if (blackListEntry
//     != null) throw new CaptchaException(...)`, ahead of
//     ChallengeResponseController.handle() and therefore ahead of the
//     challenge ever reaching CaptchaAPISolver.list() /captcha/list.
//     A blacklisted host's next captcha-gated link fails fast with a
//     CaptchaException; it never becomes a new prompt this app would have to
//     relay in the first place.
//
// So a KL-side blacklist store here would duplicate state JD already keeps,
// exactly what build-plan.md section 8 said to avoid building. This file
// stays a thin translation: parse the scope, call Abort, report what Abort
// reports.
//
// Two limits worth recording rather than working around:
//
//   - BlockDownloadCaptchasByHost and BlockAllDownloadCaptchasEntry both
//     `implements SessionBlackListEntry` (org/jdownloader/captcha/blacklist),
//     and CaptchaBlackList purges every SessionBlackListEntry the moment
//     DownloadWatchDog reports the queue stopped (idle) - in-memory only,
//     inside the JD process, for JD's idea of "session" (until the queue
//     next fully drains), not persisted to JD's own settings and not visible
//     to KnightLoader at all. A recreated or restarted JD sidecar forgets
//     silently, same risk build-plan.md package 15 already names for hoster
//     account logins. That is accepted here, not compensated for: a KL-side
//     shadow copy would face the identical "is the JD side still honouring
//     this" doubt, with two stores to keep in sync instead of one.
//   - BlockAllDownloadCaptchasEntry.matches() returns false for a
//     PluginForDecrypt challenge (org/jdownloader/captcha/blacklist/
//     BlockAllDownloadCaptchasEntry.java) - "blacklist-everywhere" silences
//     every hoster download captcha, not a captcha hit while crawling/
//     decrypting a page. Worth keeping in mind if 7A's UI copy for this
//     scope ever says "everywhere" without qualification.
//
// What this does NOT cover: internal/resolver/jd/resolver.go's
// SetHostActive/PriorityFor (Wave 6) is a different signal for a different
// question - whether a host has a confirmed-*working* native login, used to
// rank JD above the Direct resolver for it - and is deliberately left alone
// here. Reusing it for "the user is tired of this host's captchas" would
// conflate two unrelated facts and could wrongly cancel a login that is
// still working, for a reason (captcha fatigue) that has nothing to do with
// whether that login is valid. JD itself already turns a blacklisted host's
// next attempt into a fast, ordinary download error (via the CaptchaException
// path above) rather than a hang, so KnightLoader's own resolver routing
// degrades the way any other per-host failure does - nothing here is stuck
// silently, which is what would have justified a parallel signal.
//
// Not independently reachable end-to-end from this environment: no KL_JD is
// set here and no local sidecar answered on :3128, so this is source-verified
// only (two independent challenge-type implementations, not a live probe),
// unlike jdsource.go's own three-way check. The source itself leaves nothing
// ambiguous about the mechanism above; what a live check would add is
// confirmation that this build of the sidecar runs unmodified stock code,
// which is worth doing the next time this environment can reach one.
//
// THE CONTRACT THIS FILE ASSUMES OF *app.App: an AbortCaptcha(ctx, id,
// scope) method, landing in app_captcha.go, which 7A owns this wave and had
// not yet landed as this file was written (7A/7B/7C/7D all run in parallel
// per build-plan.md section 8's Wave 7 note). Named and shaped after the two
// closest precedents already in this package: app_extract.go's
// AbortExtraction(jobID string) error for the Abort<Subject>(id) shape, and
// app_hosterauth.go's HosterHosts(ctx context.Context) []hosterauth.Host for
// threading the request's context through a call that reaches the JD
// sidecar over HTTP, the same way captcha.Source.Abort does. If
// app_captcha.go lands a different shape, this call site is the one line to
// update - the JSON contract above it (path, body, status codes) does not
// need to change for that.
import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
)

func registerCaptchaSkip(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/captcha/{id}/skip",
		"give up on one captcha challenge; scope decides whether JD keeps asking for this hoster, or at all",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Scope captcha.AbortScope `json:"scope"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			// Rejected here rather than left to jdSkipRequestFor's own
			// defensive default (internal/captcha/jdsource.go, unexported -
			// narrows an unrecognised scope to skip-once rather than
			// over-blocking): that default protects JD's side of the call
			// from a future caller's bug, it is not a reason for this HTTP
			// layer to answer 204 to a typo'd scope the user thinks did
			// something it did not.
			switch body.Scope {
			case captcha.AbortSkipOnce, captcha.AbortBlacklistHoster, captcha.AbortBlacklistEverywhere:
				// recognised
			default:
				http.Error(w, "scope has to be one of skip-once, blacklist-hoster, blacklist-everywhere", http.StatusBadRequest)
				return
			}

			// 15s to match captcha.newJDClient's own httpx timeout
			// (internal/captcha/jdsource.go) - bounding the wait here shorter
			// would just cut the request off before that client would have,
			// and trade a real transport error for a less informative
			// "context deadline exceeded".
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()

			err := a.AbortCaptcha(ctx, r.PathValue("id"), body.Scope)
			if err == nil {
				// Covers "already gone" too - captcha.Source.Abort's own
				// contract treats an id that expired or was answered
				// elsewhere between list and this call as the end state
				// Abort exists to reach, not an error to report.
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if errors.Is(err, captcha.ErrJDNotConfigured) {
				// Matches routes_containers.go's ErrNoContainerBackend
				// precedent: no JD sidecar configured is "this backend is
				// unavailable", not a malformed request.
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
		})
}
