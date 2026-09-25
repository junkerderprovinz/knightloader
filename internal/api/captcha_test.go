package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
