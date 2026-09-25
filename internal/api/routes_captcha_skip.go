package api

// Giving up on one captcha challenge: POST /api/captcha/{id}/skip with a body
// of {"scope": "skip-once" | "blacklist-hoster" | "blacklist-everywhere"}, the
// names captcha.AbortScope exports.
//
// JD keeps its own blacklist, so this route only translates the scope and
// calls Abort. CaptchaAPISolver.skip records the skip request on the solver
// job, the per-type captcha helpers (CaptchaHelperHostPluginRecaptchaV2,
// CaptchaHelperHostPluginHCaptcha) file it in CaptchaBlackList, and the next
// captcha on a blocked host fails fast with a CaptchaException before it ever
// reaches /captcha/list.
//
// Two limits are accepted rather than worked around. JD's blacklist entries
// are session entries, purged when the download queue goes idle and lost when
// the sidecar restarts; a shadow copy here would have the same doubt with two
// stores to keep in sync. And blacklist-everywhere does not cover captchas hit
// while decrypting a page (BlockAllDownloadCaptchasEntry.matches), which UI
// text should not promise.
//
// The JD resolver's SetHostActive/PriorityFor is left alone: it tracks whether
// a host's login works, which captcha fatigue says nothing about.

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
			// The JD client narrows an unknown scope to skip-once, but a typo
			// here should be a 400, not a 204 for something that did not
			// happen.
			switch body.Scope {
			case captcha.AbortSkipOnce, captcha.AbortBlacklistHoster, captcha.AbortBlacklistEverywhere:
			default:
				http.Error(w, "scope has to be one of skip-once, blacklist-hoster, blacklist-everywhere", http.StatusBadRequest)
				return
			}

			// The JD client's own timeout, so the transport error is the one
			// that surfaces.
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()

			err := a.AbortCaptcha(ctx, r.PathValue("id"), body.Scope)
			if err == nil {
				// Also covers a challenge that was already gone.
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if errors.Is(err, captcha.ErrJDNotConfigured) {
				writeRefusal(w, http.StatusServiceUnavailable, "noJD", err.Error(), nil)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
		})
}
