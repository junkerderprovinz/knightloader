package captcha

// JDSource against a fake jdCaptchaAPI - never a real one, matching
// internal/hosterauth/reconcile_test.go's own fakeJD pattern: the fake
// records what it was asked and answers exactly what the test seeds it with,
// so a test asserts on JDSource's own decisions rather than on a real
// sidecar's mood that day.

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// fakeJDCaptcha is jdCaptchaAPI without a network.
type fakeJDCaptcha struct {
	jobs    []jdCaptchaJob
	listErr error

	images   map[int64]string
	imageErr map[int64]error

	widgets   map[int64]jdWidgetToken
	widgetErr map[int64]error

	solveOK  map[int64]bool
	solveErr map[int64]error
	solvedAs map[int64]string // id -> text last passed to solve

	skipErr map[int64]error
	skipped map[int64]jdSkipRequest
}

func (f *fakeJDCaptcha) list(context.Context) ([]jdCaptchaJob, error) {
	return f.jobs, f.listErr
}

func (f *fakeJDCaptcha) image(_ context.Context, id int64) (string, error) {
	if f.imageErr != nil {
		if err := f.imageErr[id]; err != nil {
			return "", err
		}
	}
	return f.images[id], nil
}

func (f *fakeJDCaptcha) widgetToken(_ context.Context, id int64) (jdWidgetToken, error) {
	if f.widgetErr != nil {
		if err := f.widgetErr[id]; err != nil {
			return jdWidgetToken{}, err
		}
	}
	return f.widgets[id], nil
}

func (f *fakeJDCaptcha) solve(_ context.Context, id int64, text string) (bool, error) {
	if f.solvedAs == nil {
		f.solvedAs = map[int64]string{}
	}
	f.solvedAs[id] = text
	if f.solveErr != nil {
		if err := f.solveErr[id]; err != nil {
			return false, err
		}
	}
	return f.solveOK[id], nil
}

func (f *fakeJDCaptcha) skip(_ context.Context, id int64, scope jdSkipRequest) error {
	if f.skipped == nil {
		f.skipped = map[int64]jdSkipRequest{}
	}
	f.skipped[id] = scope
	if f.skipErr != nil {
		return f.skipErr[id]
	}
	return nil
}

// notAvailable builds the exact error jdClient.call produces for JD's own
// {"type":"NOT_AVAILABLE"} envelope (see jdclient_test.go's
// TestJDClientSolveDetectsNotAvailable, which pins the real one on the
// wire) - what a fake hands back to exercise the same path without a server.
func notAvailable(path string) error {
	return &jdAPIError{path: path, status: 404, typ: "NOT_AVAILABLE"}
}

func newTestSource(t *testing.T, fake jdCaptchaAPI) *JDSource {
	t.Helper()
	return newTestSourceWithResolver(t, fake, nil)
}

func newTestSourceWithResolver(t *testing.T, fake jdCaptchaAPI, resolveTask func(int64) (string, bool)) *JDSource {
	t.Helper()
	return &JDSource{
		jdBase:      func() string { return "http://127.0.0.1:0" }, // never dialled: newClient is overridden below
		newClient:   func(string) jdCaptchaAPI { return fake },
		resolveTask: resolveTask,
	}
}

// ---- List: the normal round-trip -----------------------------------------

// TestJDSourceListRoundTrip is the normal path: one image challenge, fully
// populated, task id resolved from JD's own link id.
func TestJDSourceListRoundTrip(t *testing.T) {
	fake := &fakeJDCaptcha{
		jobs: []jdCaptchaJob{{
			ID: 1, Hoster: "rapidgator.net", Link: 555,
			ChallengeType: "BasicCaptchaChallenge", Type: "BasicCaptchaChallenge",
			Explain: "type what you see", Remaining: 60000,
		}},
		images: map[int64]string{1: "image/png;base64,AAAA"},
	}
	resolveTask := func(link int64) (string, bool) {
		if link == 555 {
			return "task-42", true
		}
		return "", false
	}
	src := newTestSourceWithResolver(t, fake, resolveTask)

	before := time.Now()
	got, err := src.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(List()) = %d, want 1", len(got))
	}
	ch := got[0]

	if ch.ID != "1" {
		t.Errorf("ID = %q, want %q", ch.ID, "1")
	}
	if ch.Source != SourceJD {
		t.Errorf("Source = %q, want %q", ch.Source, SourceJD)
	}
	if ch.Host != "rapidgator.net" {
		t.Errorf("Host = %q, want %q", ch.Host, "rapidgator.net")
	}
	if ch.TaskID != "task-42" {
		t.Errorf("TaskID = %q, want %q - resolveTask(555) should have fired", ch.TaskID, "task-42")
	}
	if ch.Kind != KindImage {
		t.Errorf("Kind = %q, want %q", ch.Kind, KindImage)
	}
	if ch.Prompt != "type what you see" {
		t.Errorf("Prompt = %q, want JD's own explain text", ch.Prompt)
	}
	img, ok := ch.Payload.(*ImagePayload)
	if !ok {
		t.Fatalf("Payload = %T, want *ImagePayload", ch.Payload)
	}
	if img.DataURL != "data:image/png;base64,AAAA" {
		t.Errorf("DataURL = %q, want the missing \"data:\" scheme restored", img.DataURL)
	}
	wantExpiry := before.Add(60 * time.Second)
	if ch.ExpiresAt.Before(wantExpiry.Add(-2*time.Second)) || ch.ExpiresAt.After(wantExpiry.Add(2*time.Second)) {
		t.Errorf("ExpiresAt = %v, want ~%v (now + remaining)", ch.ExpiresAt, wantExpiry)
	}
}

// TestJDSourceListEmptyIsNotAnError pins Source.List's own contract: nothing
// pending is a nil error and an empty (not nil-vs-empty-ambiguous) slice.
func TestJDSourceListEmptyIsNotAnError(t *testing.T) {
	src := newTestSource(t, &fakeJDCaptcha{})
	got, err := src.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(List()) = %d, want 0", len(got))
	}
}

// TestJDSourceListClassifiesEveryVerifiedChallengeFamily is classify's own
// table, exercised through List so a mistake in either direction (a real
// family classified as unsupported, or an unverified name given a kind it
// was never confirmed to have) shows up the same way a consumer would see it.
func TestJDSourceListClassifiesEveryVerifiedChallengeFamily(t *testing.T) {
	cases := []struct {
		challengeType string
		want          Kind
	}{
		{"ImageCaptchaChallenge", KindImage},
		{"BasicCaptchaChallenge", KindImage},
		{"SolveMediaCaptchaChallenge", KindImage},
		{"RecaptchaV1CaptchaChallenge", KindImage},
		{"ClickCaptchaChallenge", KindClick},
		{"MultiClickCaptchaChallenge", KindClick},
		{"RecaptchaV2Challenge", KindWidget},
		{"HCaptchaChallenge", KindWidget},
		{"AccountLoginOAuthChallenge", KindUnsupported},
		{"SomeFutureChallengeTypeNobodyHasSeenYet", KindUnsupported},
	}
	for _, c := range cases {
		t.Run(c.challengeType, func(t *testing.T) {
			fake := &fakeJDCaptcha{
				jobs:    []jdCaptchaJob{{ID: 1, ChallengeType: c.challengeType, Type: c.challengeType}},
				images:  map[int64]string{1: "image/png;base64,AAAA"},
				widgets: map[int64]jdWidgetToken{1: {SiteKey: "k"}},
			}
			got, err := newTestSource(t, fake).List(context.Background())
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != 1 || got[0].Kind != c.want {
				t.Fatalf("classify(%q) via List = %v, want %v", c.challengeType, got, c.want)
			}
		})
	}
}

// TestJDSourceListPreservesUnsupportedVendorName is build-plan.md section 9
// package 16's own requirement, pinned directly: a challenge this app cannot
// render must still say which one it is, not just "unsupported".
func TestJDSourceListPreservesUnsupportedVendorName(t *testing.T) {
	fake := &fakeJDCaptcha{
		jobs: []jdCaptchaJob{{
			ID: 9, Hoster: "mega.nz",
			ChallengeType: "AccountLoginOAuthChallenge", Type: "AccountLoginOAuthChallenge",
		}},
	}
	got, err := newTestSource(t, fake).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(List()) = %d, want 1", len(got))
	}
	if got[0].Kind != KindUnsupported {
		t.Fatalf("Kind = %q, want %q", got[0].Kind, KindUnsupported)
	}
	p, ok := got[0].Payload.(*UnsupportedPayload)
	if !ok {
		t.Fatalf("Payload = %T, want *UnsupportedPayload", got[0].Payload)
	}
	if p.Vendor != "AccountLoginOAuthChallenge" {
		t.Errorf("Vendor = %q, want the real JD challenge class name, not a generic label", p.Vendor)
	}
}

// TestJDSourceListWidgetPayload pins that a widget challenge's payload
// actually carries the sitekey data JDSource.build fetched with the
// rawtoken format, not the image fallback the default call would have
// returned - see jdsource.go's widgetToken and this file's own fake, which
// only ever returns rawtoken-shaped data from widgetToken, never from image.
func TestJDSourceListWidgetPayload(t *testing.T) {
	fake := &fakeJDCaptcha{
		jobs: []jdCaptchaJob{{ID: 3, Hoster: "host.example", ChallengeType: "RecaptchaV2Challenge", Type: "RecaptchaV2Challenge"}},
		widgets: map[int64]jdWidgetToken{3: {
			SiteKey: "6Lc-key", SiteURL: "https://host.example/dl", ContextURL: "https://host.example",
			Type: "normal", Enterprise: true, V3Action: "download", SecureToken: "tok",
		}},
	}
	got, err := newTestSource(t, fake).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	p, ok := got[0].Payload.(*WidgetPayload)
	if !ok {
		t.Fatalf("Payload = %T, want *WidgetPayload", got[0].Payload)
	}
	if p.SiteKey != "6Lc-key" || p.SiteURL != "https://host.example/dl" || p.ContextURL != "https://host.example" ||
		p.Type != "normal" || !p.Enterprise || p.V3Action != "download" || p.SecureToken != "tok" {
		t.Errorf("WidgetPayload = %+v, want every field carried through from jdWidgetToken", p)
	}
}

// TestJDSourceListNoTimeoutLeavesExpiresAtZero pins that a non-positive
// Remaining (JD's own "no timeout configured") is left as the zero time
// rather than turned into a fabricated deadline - see jdCaptchaJob's doc
// comment.
func TestJDSourceListNoTimeoutLeavesExpiresAtZero(t *testing.T) {
	fake := &fakeJDCaptcha{
		jobs:   []jdCaptchaJob{{ID: 1, ChallengeType: "BasicCaptchaChallenge", Remaining: -1}},
		images: map[int64]string{1: "image/png;base64,AAAA"},
	}
	got, err := newTestSource(t, fake).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !got[0].ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want the zero time for Remaining=-1", got[0].ExpiresAt)
	}
}

// TestJDSourceListFailsWholeCallRatherThanDroppingAChallenge pins the
// documented choice in JDSource.List: a challenge whose payload fetch fails
// must not simply be missing from an otherwise-200-looking result, because a
// relay that silently drops a challenge is worse than one that visibly
// fails and gets retried next poll tick (build-plan.md section 9 package 16).
func TestJDSourceListFailsWholeCallRatherThanDroppingAChallenge(t *testing.T) {
	fake := &fakeJDCaptcha{
		jobs: []jdCaptchaJob{
			{ID: 1, ChallengeType: "BasicCaptchaChallenge"},
			{ID: 2, ChallengeType: "BasicCaptchaChallenge"},
		},
		images:   map[int64]string{1: "image/png;base64,AAAA"},
		imageErr: map[int64]error{2: errors.New("jd: transient hiccup")},
	}
	_, err := newTestSource(t, fake).List(context.Background())
	if err == nil {
		t.Fatal("List with one failing payload fetch returned no error")
	}
}

// TestJDSourceListWithoutResolverLeavesTaskIDEmpty pins NewJDSource's
// documented contract: a nil resolveTask is valid, and every Challenge it
// produces has an empty TaskID rather than a guessed one.
func TestJDSourceListWithoutResolverLeavesTaskIDEmpty(t *testing.T) {
	fake := &fakeJDCaptcha{
		jobs:   []jdCaptchaJob{{ID: 1, Link: 555, ChallengeType: "BasicCaptchaChallenge"}},
		images: map[int64]string{1: "image/png;base64,AAAA"},
	}
	got, err := newTestSource(t, fake).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got[0].TaskID != "" {
		t.Errorf("TaskID = %q, want empty with no resolver wired in", got[0].TaskID)
	}
}

// ---- Answer: stillValid is the direct signal, not a guess -----------------

// TestJDSourceAnswerStillValidFalseWhenGone is one of this package's three
// required tests: solve reporting stillValid=false for an id JD says is gone.
func TestJDSourceAnswerStillValidFalseWhenGone(t *testing.T) {
	fake := &fakeJDCaptcha{solveErr: map[int64]error{7: notAvailable("/captcha/solve")}}
	stillValid, err := newTestSource(t, fake).Answer(context.Background(), "7", "too-late")
	if err != nil {
		t.Fatalf("Answer: %v, want nil error - a gone id is not an application failure", err)
	}
	if stillValid {
		t.Error("stillValid = true, want false for an id JD reports as gone")
	}
}

func TestJDSourceAnswerStillValidTrueOnSuccess(t *testing.T) {
	fake := &fakeJDCaptcha{solveOK: map[int64]bool{7: true}}
	stillValid, err := newTestSource(t, fake).Answer(context.Background(), "7", "abcd")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if !stillValid {
		t.Error("stillValid = false, want true for a fresh id JD accepted")
	}
	if fake.solvedAs[7] != "abcd" {
		t.Errorf("solve was called with %q, want the exact text Answer was given", fake.solvedAs[7])
	}
}

// TestJDSourceAnswerPropagatesRealErrors pins that a failure other than
// "gone" is a real error, not folded into stillValid=false - a caller must be
// able to tell "this expired" apart from "the network broke".
func TestJDSourceAnswerPropagatesRealErrors(t *testing.T) {
	fake := &fakeJDCaptcha{solveErr: map[int64]error{7: errors.New("jd: connection reset")}}
	_, err := newTestSource(t, fake).Answer(context.Background(), "7", "abcd")
	if err == nil {
		t.Fatal("Answer with a real transport failure returned no error")
	}
	if isNotAvailable(err) {
		t.Error("isNotAvailable(err) = true, want false - this was never JD's NOT_AVAILABLE envelope")
	}
}

func TestJDSourceAnswerRejectsANonJDID(t *testing.T) {
	_, err := newTestSource(t, &fakeJDCaptcha{}).Answer(context.Background(), "not-a-number", "x")
	if err == nil {
		t.Fatal("Answer with a non-numeric id returned no error")
	}
}

// ---- Abort: gone-already is success, not an error -------------------------

func TestJDSourceAbortSendsTheMappedScope(t *testing.T) {
	fake := &fakeJDCaptcha{}
	src := newTestSource(t, fake)

	cases := []struct {
		scope AbortScope
		want  jdSkipRequest
	}{
		{AbortSkipOnce, jdSkipSingle},
		{AbortBlacklistHoster, jdSkipBlockHoster},
		{AbortBlacklistEverywhere, jdSkipBlockAllCaptchas},
	}
	for i, c := range cases {
		id := int64(i + 1)
		if err := src.Abort(context.Background(), strconv.FormatInt(id, 10), c.scope); err != nil {
			t.Fatalf("Abort(%v): %v", c.scope, err)
		}
		if got := fake.skipped[id]; got != c.want {
			t.Errorf("Abort(%v) sent skip type %q, want %q", c.scope, got, c.want)
		}
	}
}

// TestJDSourceAbortIsIdempotentOnGone pins Source.Abort's documented
// contract: a challenge that is already gone must not surface as an error -
// the end state Abort exists to reach already holds.
func TestJDSourceAbortIsIdempotentOnGone(t *testing.T) {
	fake := &fakeJDCaptcha{skipErr: map[int64]error{7: notAvailable("/captcha/skip")}}
	err := newTestSource(t, fake).Abort(context.Background(), "7", AbortSkipOnce)
	if err != nil {
		t.Errorf("Abort on an already-gone id = %v, want nil", err)
	}
}

func TestJDSourceAbortPropagatesRealErrors(t *testing.T) {
	fake := &fakeJDCaptcha{skipErr: map[int64]error{7: errors.New("jd: connection reset")}}
	err := newTestSource(t, fake).Abort(context.Background(), "7", AbortSkipOnce)
	if err == nil {
		t.Fatal("Abort with a real transport failure returned no error")
	}
}

// ---- Not configured: quiet, not a special case callers must detect first -

func TestJDSourceMethodsReportNotConfigured(t *testing.T) {
	src := &JDSource{
		jdBase:    func() string { return "" },
		newClient: func(string) jdCaptchaAPI { return &fakeJDCaptcha{} },
	}
	if _, err := src.List(context.Background()); !errors.Is(err, ErrJDNotConfigured) {
		t.Errorf("List() err = %v, want ErrJDNotConfigured", err)
	}
	if _, err := src.Answer(context.Background(), "1", "x"); !errors.Is(err, ErrJDNotConfigured) {
		t.Errorf("Answer() err = %v, want ErrJDNotConfigured", err)
	}
	if err := src.Abort(context.Background(), "1", AbortSkipOnce); !errors.Is(err, ErrJDNotConfigured) {
		t.Errorf("Abort() err = %v, want ErrJDNotConfigured", err)
	}
}
