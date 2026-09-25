package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// An encrypted container nobody can open is refused with a code, so the
// upload's toast can say why in the reader's language.
func TestAnEncryptedContainerWithoutJDIsRefusedWithACode(t *testing.T) {
	a := testApp(t)
	reg := newRegistry()
	registerContainers(reg, a)
	h := http.NewServeMux()
	reg.attach(h, http.NotFoundHandler())

	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, err := mw.CreateFormFile("file", "links.ccf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("encrypted bytes")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/containers", &form)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var body struct{ Error, Code string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("answered %d with %q, want a JSON refusal: %v", rec.Code, rec.Body.String(), err)
	}
	if rec.Code != http.StatusServiceUnavailable || body.Code != "noJD" || body.Error == "" {
		t.Errorf("answered %d %+v, want 503 with the code noJD and the English sentence", rec.Code, body)
	}
}
