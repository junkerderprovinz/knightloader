package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// Without a JDownloader backend, answering or skipping a captcha is refused
// with a code the interface can word, and the English names the module the
// way every page does.
func TestACaptchaWithoutJDIsRefusedWithACode(t *testing.T) {
	t.Setenv("KL_JD", "")
	a := testApp(t)
	reg := newRegistry()
	registerCaptcha(reg, a)
	registerCaptchaSkip(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())

	for path, body := range map[string]string{
		"/api/captcha/c1/answer": `{"text":"abcd"}`,
		"/api/captcha/c1/skip":   `{"scope":"skip-once"}`,
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		var got struct{ Error, Code string }
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Errorf("%s answered %d with %q, want a JSON refusal", path, rec.Code, rec.Body.String())
			continue
		}
		if rec.Code != http.StatusServiceUnavailable || got.Code != "noJD" {
			t.Errorf("%s answered %d %+v, want 503 noJD", path, rec.Code, got)
		}
		if !strings.Contains(got.Error, "JDownloader backend") {
			t.Errorf("%s says %q, which does not name the JDownloader backend", path, got.Error)
		}
	}
}

// A window and the phone app each say on a path of their own that they cannot
// load a captcha, and take it back there, so the phone's report never takes
// the windows out of the count or the other way round. A viewer without a
// path of its own records nothing.
func TestEachViewerSaysItCannotLoadACaptchaOnItsOwnPath(t *testing.T) {
	jd, _ := fakeJDWithHCaptcha(t)
	t.Setenv("KL_JD", jd.URL)
	a := testApp(t)
	reg := newRegistry()
	registerCaptcha(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())

	pending := a.RefreshCaptchas(context.Background())
	if len(pending) != 1 {
		t.Fatalf("pending = %+v, want the one hCaptcha", pending)
	}
	id := pending[0].ID
	marked := func() string {
		var by []string
		for _, v := range []app.CaptchaViewer{app.CaptchaWindow, app.CaptchaPhone} {
			if a.CaptchaUnanswerable(id, v) {
				by = append(by, string(v))
			}
		}
		return strings.Join(by, " ")
	}

	path := "/api/captcha/" + id + "/unanswerable"
	for _, c := range []struct {
		method, path string
		code         int
		marked       string
	}{
		{http.MethodPost, path, http.StatusNoContent, "window"},
		{http.MethodPost, path + "/phone", http.StatusNoContent, "window phone"},
		{http.MethodDelete, path, http.StatusNoContent, "phone"},
		{http.MethodPost, path + "/tablet", http.StatusNotFound, "phone"},
		{http.MethodDelete, path + "/phone", http.StatusNoContent, ""},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if got := marked(); rec.Code != c.code || got != c.marked {
			t.Errorf("%s %s answered %d and left %q marked, want %d and %q", c.method, c.path, rec.Code, got, c.code, c.marked)
		}
	}
}

// An app that polls the list instead of holding a socket counts as watching,
// for the kinds it lists when it lists them; the web interface reads with
// watch=0, since its socket reports that already.
func TestReadingTheCaptchaListCountsAsWatchingUnlessAskedNotTo(t *testing.T) {
	t.Setenv("KL_JD", "")
	read := func(path string) *app.App {
		a := testApp(t)
		reg := newRegistry()
		registerCaptcha(reg, a)
		mux := http.NewServeMux()
		reg.attach(mux, http.NotFoundHandler())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s answered %d", path, rec.Code)
		}
		return a
	}
	watched := func(a *app.App, typ string) bool { return a.Hub.Watched(typ, time.Minute) }

	if !watched(read("/api/captcha"), "captcha") {
		t.Error("a poll of the list did not count as watching")
	}
	if a := read("/api/captcha?watch=0"); watched(a, "captcha") || watched(a, "captcha:image") {
		t.Error("a read with watch=0 counted as watching")
	}
	a := read("/api/captcha?watch=image,click,nonsense")
	if watched(a, "captcha") || watched(a, "captcha:widget") || watched(a, "captcha:nonsense") {
		t.Error("a read listing pictures counted as watching for more than pictures")
	}
	if !watched(a, "captcha:image") || !watched(a, "captcha:click") {
		t.Error("a read listing pictures did not count as watching for them")
	}
}
