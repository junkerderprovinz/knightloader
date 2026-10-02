package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/jdimport/jdimporttest"
)

func jdImportServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerJDImport(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

func decodeAnswer(t *testing.T, resp *http.Response, want int, v any) {
	t.Helper()
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("status %d, want %d: %s", resp.StatusCode, want, raw)
	}
	if v != nil {
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
	}
}

func TestJDImportReadsAnUploadedZipAndAppliesIt(t *testing.T) {
	t.Parallel()
	a, srv := jdImportServer(t)
	data := jdimporttest.Zip(t, "cfg/", jdimporttest.Config{Passwords: []string{"from-jd"}})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "cfg.zip")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	mw.Close()
	resp, err := http.Post(srv.URL+"/api/jdimport/read", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	var preview app.JDImportPreview
	decodeAnswer(t, resp, http.StatusOK, &preview)
	if len(preview.Items) != 1 || preview.Items[0].ID != "passwords" {
		t.Fatalf("items = %+v", preview.Items)
	}

	req, _ := json.Marshal(map[string]any{"token": preview.Token, "ids": []string{"passwords"}})
	resp, err = http.Post(srv.URL+"/api/jdimport/apply", "application/json", bytes.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	var rep app.JDImportReport
	decodeAnswer(t, resp, http.StatusOK, &rep)
	if len(rep.Imported) != 1 {
		t.Errorf("report = %+v", rep)
	}
	got := a.Settings.Get().ArchivePasswords
	if len(got) == 0 || got[len(got)-1] != "from-jd" {
		t.Errorf("archive passwords = %q", got)
	}

	// The token is spent.
	resp, err = http.Post(srv.URL+"/api/jdimport/apply", "application/json", bytes.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	var refusal map[string]any
	decodeAnswer(t, resp, http.StatusGone, &refusal)
	if refusal["code"] != "jdimport.expired" {
		t.Errorf("refusal = %v", refusal)
	}
}

func TestJDImportReadsAFolderByItsPath(t *testing.T) {
	t.Parallel()
	_, srv := jdImportServer(t)
	install := t.TempDir()
	jdimporttest.Write(t, filepath.Join(install, "cfg"), jdimporttest.Config{DownloadDir: "/jd/output"})

	post := func(path string) *http.Response {
		b, _ := json.Marshal(map[string]string{"path": path})
		resp, err := http.Post(srv.URL+"/api/jdimport/read", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	var preview app.JDImportPreview
	decodeAnswer(t, post(install), http.StatusOK, &preview)
	if len(preview.Items) != 1 || preview.Items[0].Name != "/jd/output" {
		t.Fatalf("items = %+v", preview.Items)
	}

	var refusal map[string]any
	decodeAnswer(t, post(filepath.Join(install, "nothing-here")), http.StatusBadRequest, &refusal)
	if refusal["code"] != "jdimport.pathMissing" {
		t.Errorf("missing path: %v", refusal)
	}

	empty := t.TempDir()
	decodeAnswer(t, post(empty), http.StatusBadRequest, &refusal)
	if refusal["code"] != "jdimport.noConfig" {
		t.Errorf("empty folder: %v", refusal)
	}

	notZip := filepath.Join(empty, "cfg.zip")
	if err := os.WriteFile(notZip, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	decodeAnswer(t, post(notZip), http.StatusBadRequest, &refusal)
	if refusal["code"] != "jdimport.notZip" {
		t.Errorf("not a zip: %v", refusal)
	}
}
