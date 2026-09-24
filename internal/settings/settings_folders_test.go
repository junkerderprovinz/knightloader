package settings

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// A save refuses exactly the folders sanitize would clear from the settings
// file, so nothing a save accepts is dropped on its way to disk and nothing it
// refuses would have been kept.
func TestCheckFoldersRefusesWhatSanitizeClears(t *testing.T) {
	abs := t.TempDir()
	values := []string{"", "watch", "C:", "<jd:packagename>/unpacked", abs, filepath.Join(abs, "<jd:packagename>")}
	for _, f := range folderFields {
		for _, dir := range values {
			raw, _ := json.Marshal(dir)
			s, err := ApplyPatch(Settings{}, map[string]json.RawMessage{f.key: raw})
			if err != nil {
				t.Fatal(err)
			}
			refused := CheckFolders(s, func(key string) bool { return key == f.key }) != nil
			kept := f.get(sanitize(s)) == dir
			if refused == kept {
				t.Errorf("%s = %q: refused %v, kept by sanitize %v", f.key, dir, refused, kept)
			}
		}
	}
}

func TestCheckFoldersNamesTheFieldThatFailed(t *testing.T) {
	err := CheckFolders(Settings{WatchDir: "C:"}, nil)
	var p *PathProblem
	if !errors.As(err, &p) {
		t.Fatalf("CheckFolders = %v, want a *PathProblem", err)
	}
	if p.Field != "watchDir" || p.Code != "notAbsolute" || p.Dir != "C:" {
		t.Errorf("problem = %+v, want watchDir, notAbsolute, C:", p)
	}
	if err.Error() != "the watch folder must be an absolute path" {
		t.Errorf("message = %q", err)
	}
}
