// Package captcha is the honest source of truth for one thing: a hoster (or,
// for an OAuth-style login gate, an account) demanding something a human has
// to answer before a download can continue. Nothing in this tree produced
// that fact before this package existed - see jdsource.go's package comment
// for how thin that made every plan to build a captcha UI, a solver or a
// skip button.
//
// This package owns the vocabulary (Challenge, Kind, AbortScope, Source) and
// one Source implementation backed by a headless JD. It does not poll, does
// not render, does not store, and does not touch settings - see
// build-plan.md section 9 package 16 and section 8's Wave 7 note for what
// those other pieces are and who builds them.
package captcha

import (
	"context"
	"time"
)

// Kind is how a Challenge must be rendered. A Source is expected to classify
// every challenge it produces into one of these; KindUnsupported is the
// honest fallback for a real challenge this app has no UI for, and it stays
// informative rather than a shrug - see UnsupportedPayload.
type Kind string

const (
	// KindImage is a picture plus a typed answer - the classic captcha shape.
	// Payload is *ImagePayload.
	KindImage Kind = "image"
	// KindClick is a picture plus one or more clicked points instead of typed
	// text. Payload is *ImagePayload too - see ClickPayload's doc comment for
	// why the same type stands in for both.
	KindClick Kind = "click"
	// KindWidget is a hosted third-party JS challenge (reCAPTCHA v2, hCaptcha)
	// that has to be embedded and solved in a browser context. Payload is
	// *WidgetPayload.
	KindWidget Kind = "widget"
	// KindUnsupported is a real challenge a Source produced but cannot
	// describe as one of the above. Payload is *UnsupportedPayload, which
	// names the real origin rather than leaving the UI to say only
	// "unsupported" - build-plan.md section 9 package 16 asks for this by
	// name.
	KindUnsupported Kind = "unsupported"
)

// ImagePayload is Challenge.Payload for KindImage: a complete, renderable
// image. DataURL is always a full "data:image/...;base64,..." string, ready
// for an <img src> with no further assembly required of the caller - see
// jdsource.go's normalizeImageDataURL for why that normalization has to
// happen somewhere, and why here rather than in every consumer.
type ImagePayload struct {
	DataURL string `json:"dataUrl"`
}

// ClickPayload is Challenge.Payload for KindClick. It is the identical type
// to ImagePayload, not a coincidence: the one Source this package ships
// verified that JD hands back exactly the same image data for a
// click-to-answer challenge as for a typed-text one (see jdsource.go's
// classify) and adds no separate click-region metadata over the wire. Kind
// alone is what tells a renderer to offer a click surface instead of a text
// box; a future Source with real click-region data would need a new,
// distinct payload type, not this one widened.
type ClickPayload = ImagePayload

// WidgetPayload is Challenge.Payload for KindWidget: the sitekey data a
// hosted reCAPTCHA v2 or hCaptcha JS widget needs to render and solve itself
// in a browser context - not a screenshot, not a proxied iframe. See
// jdsource.go's jdWidgetToken for exactly which JD call this is read from and
// why it is not the default captcha/get response.
type WidgetPayload struct {
	SiteKey    string `json:"siteKey"`
	SiteURL    string `json:"siteUrl"`
	ContextURL string `json:"contextUrl"`
	// Type is the widget variant ("normal"/"invisible" for reCAPTCHA v2;
	// hCaptcha's own API always reports "normal" - see jdsource.go). Rendered
	// as-is; this package does not interpret it.
	Type string `json:"type,omitempty"`
	// Enterprise and V3Action only ever apply to reCAPTCHA; hCaptcha's own
	// Storable has no such fields and leaves them at the zero value - see
	// jdsource.go's jdWidgetToken.
	Enterprise bool   `json:"enterprise,omitempty"`
	V3Action   string `json:"v3Action,omitempty"`
	// SecureToken is JD's own "stoken". Kept even though the one Source this
	// package ships never observed hCaptcha populate it (its Storable's
	// getStoken() is hardcoded to return nil - see jdsource.go): dropping a
	// field JD's wire format genuinely carries is a silent regression the day
	// a JD build starts sending it, not a simplification.
	SecureToken string `json:"secureToken,omitempty"`
}

// UnsupportedPayload is Challenge.Payload for KindUnsupported.
type UnsupportedPayload struct {
	// Vendor names the real origin of the challenge - for the JD-backed
	// Source, JD's own challenge class name (e.g. "AccountLoginOAuthChallenge"),
	// never a value this package invented or guessed at. See jdsource.go's
	// classify.
	Vendor string `json:"vendor"`
}

// Challenge is one captcha instance blocking a download (or a login) until a
// human answers it, dismisses it, or it expires on its own. It is the shape
// every Source produces and every consumer - the prompt modal, the widget
// route, the skip action - is built against, so changing it after those
// start costs all of them a rewrite; see build-plan.md section 9 package 16.
type Challenge struct {
	// ID identifies this challenge to the Source that produced it. Opaque:
	// callers pass it back to Answer/Abort unchanged and must not parse it -
	// the JD-backed Source's ID happens to be a stringified int64, but
	// nothing here promises another Source's will be.
	ID string `json:"id"`
	// Source names which Source produced this challenge (SourceJD today).
	// Carried on the value itself, not only implied by which Source a caller
	// happened to ask, because a consumer holding challenges from more than
	// one Source needs to tell them apart without threading that context
	// through separately.
	Source string `json:"source"`
	// Host is the hoster the challenge is guarding, e.g. "rapidgator.net".
	Host string `json:"host"`
	// TaskID is the KnightLoader task this challenge blocks, when whoever
	// built this Source could work that out. Empty is a real, expected
	// answer, not a bug - see jdsource.go's NewJDSource on why this package
	// cannot resolve it unaided.
	TaskID string `json:"taskId,omitempty"`
	// Kind is how this challenge must be rendered - see Kind's own doc
	// comment.
	Kind Kind `json:"kind"`
	// Prompt is the instructions a human reads, in whatever language the
	// hoster wrote them. Can be empty - not every challenge populates one.
	Prompt string `json:"prompt,omitempty"`
	// Payload is the kind-specific data a solver needs: *ImagePayload for
	// KindImage/KindClick, *WidgetPayload for KindWidget, *UnsupportedPayload
	// for KindUnsupported. A concrete Go type rather than raw JSON so a
	// same-process consumer (this wave's own tests, and 7A's app-level code)
	// can switch on Kind and type-assert without a redundant decode; it still
	// marshals to plain JSON for the eventual HTTP route exactly the same way.
	Payload any `json:"payload,omitempty"`
	// ExpiresAt is when this challenge stops being answerable, computed from
	// the Source's own live countdown at the moment it was listed rather than
	// a deadline fixed at creation - see jdsource.go's doc comment on why
	// that is the honest field to trust. Calling List again can move this
	// later without the challenge having changed identity, exactly as it
	// should when the challenge is still alive. Zero means the Source could
	// not say.
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// AbortScope is how far a skipped challenge's effect reaches, named for what
// a KnightLoader user is choosing rather than for JD's own constant spelling
// - see the mapping table in jdsource.go's jdSkipRequestFor, the way
// internal/hosterauth documents its own JD-facing mapping decisions rather
// than assuming a reader can reconstruct them.
type AbortScope string

const (
	// AbortSkipOnce leaves this one challenge unanswered and moves on to
	// whatever happens next for that single link.
	AbortSkipOnce AbortScope = "skip-once"
	// AbortBlacklistHoster stops this hoster's captchas from being shown
	// again for the rest of the session.
	AbortBlacklistHoster AbortScope = "blacklist-hoster"
	// AbortBlacklistEverywhere stops every hoster's captchas from being shown
	// again for the rest of the session.
	AbortBlacklistEverywhere AbortScope = "blacklist-everywhere"
)

// Source is a producer of captcha challenges and the only way to answer or
// dismiss one. 7A's poll loop calls List on a schedule; the prompt modal and
// the skip action call Answer/Abort with an id List already handed them -
// see build-plan.md section 8's Wave 7 note ("7A - additionally: render from
// 7F's typed descriptor").
//
// One List, not a stream and not a per-challenge subscription: the only
// backend this package wraps today answers "everything pending" in a single
// call (see jdsource.go), and a push-based shape would be speculative for a
// second Source that does not exist yet.
type Source interface {
	// List returns every challenge currently waiting for an answer. An empty
	// slice with a nil error is the ordinary "nothing waiting" case, not a
	// failure - a poll loop must not treat every quiet tick as one.
	List(ctx context.Context) ([]Challenge, error)

	// Answer submits text as the solution to challenge id. stillValid reports
	// whether id was still live when the Source received it - the direct,
	// authoritative answer to "did this arrive too late", read from the
	// backend rather than guessed at from a client-side countdown (see
	// build-plan.md section 9 package 16: "solving an expired id silently
	// does nothing while the user sees sent"). false with a nil error is not
	// itself a failure: it means the challenge expired, or was already
	// answered elsewhere, between List and this call, and the caller should
	// treat that as "refresh and show whatever replaced it", not as
	// something to report to the user as an error.
	//
	// err is reserved for everything else going wrong: a transport failure,
	// an id the Source never issued, an answer shape the challenge rejected
	// outright.
	Answer(ctx context.Context, id string, text string) (stillValid bool, err error)

	// Abort tells the Source the user chose not to answer id, at the given
	// scope - see AbortScope. Aborting a challenge that has already expired
	// or been answered elsewhere is not an error: the end state Abort exists
	// to reach (id no longer pending) already holds, so a Source should
	// return nil for that case rather than making every caller special-case
	// a race it did not cause.
	Abort(ctx context.Context, id string, scope AbortScope) error
}
