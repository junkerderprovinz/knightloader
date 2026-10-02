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
	t.Parallel()
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

// sampleRSDF holds https://host.example/one.bin and two.bin, encrypted under
// the format's fixed key.
const sampleRSDF = "6F59654632575A57447757717573666A61427539393349506F77326A345143357762773348513D3D0D0A2B647A44787A30762F396E756B36666A583679312F3164314A6D616F717463587576796F67673D3D"

func TestAnRSDFIsOpenedHereWithoutJD(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()

	code, body := uploadContainer(t, srv.URL, "release.rsdf", []byte(sampleRSDF))
	if code != http.StatusOK {
		t.Fatalf("uploading an RSDF with no JD = %d, want it opened here: %s", code, body)
	}
	var got struct {
		Kind  string
		Links int
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "rsdf" || got.Links != 2 || len(a.Tasks()) != 2 {
		t.Errorf("answered %+v with %d tasks, want both links of the rsdf staged", got, len(a.Tasks()))
	}
}
