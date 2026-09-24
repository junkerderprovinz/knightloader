// Package captcha describes a hoster, or for an OAuth-style login gate an
// account, demanding something a human has to answer before a download can
// continue.
//
// It owns the vocabulary (Challenge, Kind, AbortScope, Source) and one Source
// backed by a headless JDownloader. It does not poll, render, store, or read
// settings.
package captcha

import (
	"context"
	"time"
)

// Kind is how a Challenge has to be rendered. A Source classifies every
// challenge it produces as one of these.
type Kind string

const (
	// KindImage is a picture plus a typed answer. Payload is *ImagePayload.
	KindImage Kind = "image"
	// KindClick is a picture answered by clicking points rather than typing.
	// Payload is *ImagePayload as well; see ClickPayload.
	KindClick Kind = "click"
	// KindWidget is a hosted third-party JS challenge (reCAPTCHA v2, hCaptcha)
	// that has to be embedded and solved in a browser context. Payload is
	// *WidgetPayload.
	KindWidget Kind = "widget"
	// KindUnsupported is a real challenge a Source cannot describe as one of
	// the above. Payload is *UnsupportedPayload, which names the origin so the
	// interface can say more than "unsupported".
	KindUnsupported Kind = "unsupported"
)

// ImagePayload is Challenge.Payload for KindImage. DataURL is always a complete
// "data:image/...;base64,..." string, ready for an <img src> with no assembly
// left to the caller; jdsource.go's normalizeImageDataURL builds it.
type ImagePayload struct {
	DataURL string `json:"dataUrl"`
}

// ClickPayload is Challenge.Payload for KindClick. It is an alias of
// ImagePayload because JD hands back the same image data for a click-to-answer
// challenge and sends no click-region metadata with it, so Kind alone tells a
// renderer to offer a click surface. A Source that does carry click regions
// would need a payload type of its own.
type ClickPayload = ImagePayload

// The vendors a KindWidget challenge can come from, as WidgetPayload.Vendor
// names them.
const (
	VendorRecaptcha = "recaptcha"
	VendorHCaptcha  = "hcaptcha"
)

// WidgetPayload is Challenge.Payload for KindWidget: the sitekey data a hosted
// reCAPTCHA v2 or hCaptcha widget needs to render and solve itself in a
// browser. See jdsource.go's jdWidgetToken for which JD call it is read from.
type WidgetPayload struct {
	// Vendor is VendorRecaptcha or VendorHCaptcha. The two load different
	// scripts from different origins, and nothing else in the payload tells
	// them apart for certain.
	Vendor     string `json:"vendor"`
	SiteKey    string `json:"siteKey"`
	SiteURL    string `json:"siteUrl"`
	ContextURL string `json:"contextUrl"`
	// Type is the widget variant JD found on the hoster's page, "NORMAL" or
	// "INVISIBLE" for either vendor. It is passed on as it arrives.
	Type string `json:"type,omitempty"`
	// Enterprise and V3Action apply to reCAPTCHA; hCaptcha leaves them at the
	// zero value.
	Enterprise bool   `json:"enterprise,omitempty"`
	V3Action   string `json:"v3Action,omitempty"`
	// SecureToken is JD's "stoken". hCaptcha's Storable hardcodes it to nil,
	// but JD's wire format carries it, so dropping the field would regress the
	// day a JD build starts sending one.
	SecureToken string `json:"secureToken,omitempty"`
}

// UnsupportedPayload is Challenge.Payload for KindUnsupported.
type UnsupportedPayload struct {
	// Vendor is the challenge's real origin, for the JD-backed Source its own
	// class name such as "AccountLoginOAuthChallenge".
	Vendor string `json:"vendor"`
}

// Challenge is one captcha instance blocking a download or a login until a
// human answers it, dismisses it, or it expires on its own.
type Challenge struct {
	// ID identifies this challenge to the Source that produced it. It is
	// opaque: callers hand it back to Answer and Abort unchanged.
	ID string `json:"id"`
	// Source names which Source produced this challenge, so a consumer holding
	// challenges from more than one can tell them apart.
	Source string `json:"source"`
	// Host is the hoster the challenge is guarding, e.g. "rapidgator.net".
	Host string `json:"host"`
	// TaskID is the task this challenge blocks where the Source could work it
	// out. Empty is an expected answer; see NewJDSource.
	TaskID string `json:"taskId,omitempty"`
	// Kind is how this challenge has to be rendered.
	Kind Kind `json:"kind"`
	// Prompt is the instructions a human reads, in whatever language the hoster
	// wrote them. It can be empty.
	Prompt string `json:"prompt,omitempty"`
	// Payload is the kind-specific data a solver needs: *ImagePayload for
	// KindImage and KindClick, *WidgetPayload for KindWidget,
	// *UnsupportedPayload for KindUnsupported. A concrete type rather than raw
	// JSON, so a same-process consumer can switch on Kind without a second
	// decode; it marshals the same either way.
	Payload any `json:"payload,omitempty"`
	// ExpiresAt is when this challenge stops being answerable, read from the
	// Source's live countdown at the moment it was listed rather than fixed at
	// creation, so a later List can move it further out. Zero means the Source
	// could not say.
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// AbortScope is how far a skipped challenge reaches, named for what the user is
// choosing rather than for JD's own spelling. jdSkipRequestFor holds the
// mapping.
type AbortScope string

const (
	// AbortSkipOnce leaves this one challenge unanswered and moves on to
	// whatever happens next for that single link.
	AbortSkipOnce AbortScope = "skip-once"
	// AbortBlacklistHoster stops this hoster's captchas from being shown again
	// for the rest of the session.
	AbortBlacklistHoster AbortScope = "blacklist-hoster"
	// AbortBlacklistEverywhere stops every hoster's captchas from being shown
	// again for the rest of the session.
	AbortBlacklistEverywhere AbortScope = "blacklist-everywhere"
)

// Source produces captcha challenges and is the only way to answer or dismiss
// one. It offers a single List rather than a stream, because the backend this
// package wraps answers "everything pending" in one call.
type Source interface {
	// List returns every challenge waiting for an answer. An empty slice with a
	// nil error is the ordinary quiet case, not a failure.
	List(ctx context.Context) ([]Challenge, error)

	// Answer submits text as the solution to challenge id. stillValid reports
	// whether id was still live when the Source received it, read from the
	// backend rather than guessed from a client-side countdown. false with a
	// nil error means the challenge expired or was answered elsewhere between
	// List and this call, which a caller should treat as a reason to refresh
	// rather than as an error to report.
	//
	// err covers everything else: a transport failure, an id the Source never
	// issued, an answer shape the challenge rejected outright.
	Answer(ctx context.Context, id string, text string) (stillValid bool, err error)

	// Abort tells the Source the user chose not to answer id, at the given
	// scope. Aborting a challenge that has already expired or been answered
	// elsewhere is not an error, since the state Abort exists to reach already
	// holds.
	Abort(ctx context.Context, id string, scope AbortScope) error
}
