package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// A folder that is not an absolute path is refused with its field named, and
// the stored folder stays, so a half-typed "C:" does not empty the field.
func TestARelativeFolderIsRefusedAndTheStoredOneKept(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	watch := filepath.Join(t.TempDir(), "watch")
	if code, _, msg := patchSettings(t, srv.URL, fmt.Sprintf(`{"watchDir":%q}`, watch)); code != http.StatusOK {
		t.Fatalf("an absolute watch folder answered %d: %s", code, msg)
	}

	for _, field := range []string{"watchDir", "downloadDir", "workDir", "extractTo", "extractMoveTo"} {
		code, _, msg := patchSettings(t, srv.URL, fmt.Sprintf(`{%q:"C:"}`, field))
		if code != http.StatusBadRequest {
			t.Errorf("%s = \"C:\" answered %d, want it refused", field, code)
			continue
		}
		var body struct {
			Field  string            `json:"field"`
			Code   string            `json:"code"`
			Params map[string]string `json:"params"`
		}
		if err := json.Unmarshal([]byte(msg), &body); err != nil {
			t.Fatalf("%s: refusal is not JSON: %q", field, msg)
		}
		if body.Field != field || body.Code != "pathProblem.notAbsolute" || body.Params["dir"] != "C:" {
			t.Errorf("%s: refusal = %+v, want field %s, code pathProblem.notAbsolute, dir C:", field, body, field)
		}
	}
	if got := a.Settings.Get().WatchDir; got != watch {
		t.Errorf("stored watch folder = %q after the refusals, want %q kept", got, watch)
	}
}

// A template counts as absolute by its fixed head, so a folder per package is
// kept while a template with no fixed part is refused.
func TestAnExtractionTemplateNeedsAnAbsoluteHead(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	good := filepath.Join(t.TempDir(), "<jd:packagename>")
	if code, _, msg := patchSettings(t, srv.URL, fmt.Sprintf(`{"extractTo":%q}`, good)); code != http.StatusOK {
		t.Fatalf("an absolute template answered %d: %s", code, msg)
	}
	if code, _, _ := patchSettings(t, srv.URL, `{"extractTo":"<jd:packagename>/unpacked"}`); code != http.StatusBadRequest {
		t.Errorf("a template with no fixed part answered %d, want it refused", code)
	}
	if got := a.Settings.Get().ExtractTo; got != good {
		t.Errorf("stored extraction folder = %q, want %q", got, good)
	}
}

// A patch is checked for the folders it names, so a stored download folder
// that cannot be created does not refuse an edit to something else.
func TestAPatchIsNotRefusedForAFolderItDoesNotName(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s := a.Settings.Get()
	s.DownloadDir = filepath.Join(blocker, "downloads")
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	if code, _, msg := patchSettings(t, srv.URL, `{"maxConcurrent":2}`); code != http.StatusOK {
		t.Errorf("a patch of maxConcurrent answered %d: %s", code, msg)
	}
	if code, _, _ := patchSettings(t, srv.URL, fmt.Sprintf(`{"downloadDir":%q}`, s.DownloadDir)); code != http.StatusBadRequest {
		t.Errorf("a patch naming the unusable download folder answered %d, want it refused", code)
	}
}

// A category's folder is checked when a save sends the categories, and only
// then, so a stored drawer whose share is offline does not refuse a patch that
// never mentions it.
func TestAPatchIsNotRefusedForACategoryFolderItDoesNotName(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s := a.Settings.Get()
	s.Categories = []settings.Category{{ID: "filme", Name: "Filme", Dir: filepath.Join(blocker, "filme")}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	if code, _, msg := patchSettings(t, srv.URL, `{"maxConcurrent":2}`); code != http.StatusOK {
		t.Errorf("a patch of maxConcurrent answered %d: %s", code, msg)
	}
	cats, _ := json.Marshal(a.Settings.Get().Categories)
	if code, _, _ := patchSettings(t, srv.URL, fmt.Sprintf(`{"categories":%s}`, cats)); code != http.StatusBadRequest {
		t.Errorf("a patch sending the category with the unusable folder answered %d, want it refused", code)
	}
}
