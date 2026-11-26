package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func TestAMemberReadsTheFoldersItMayPick(t *testing.T) {
	t.Parallel()
	a, downloads, category, member, owner := groupServer(t)
	extract, watch := t.TempDir(), t.TempDir()
	s := a.Settings.Get()
	s.ExtractTo = extract
	s.WatchDir = watch
	s.Categories = append(s.Categories,
		settings.Category{ID: "musik", Name: "Musik"},
		settings.Category{ID: "again", Name: "Again", Dir: downloads},
	)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	status, body := member(http.MethodGet, "/api/download-folders", "")
	var got struct {
		Folders []string `json:"folders"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &got) != nil {
		t.Fatalf("a member reading the download folders = %d (%s), want 200 and a list", status, body)
	}
	if want := []string{downloads, category}; !slices.Equal(got.Folders, want) {
		t.Errorf("folders = %v, want the download folder and the category's, each once: %v", got.Folders, want)
	}
	for _, other := range []string{extract, watch} {
		if slices.Contains(got.Folders, other) {
			t.Errorf("the folders a member reads name %s, which no download goes to: %s", other, body)
		}
	}

	id := stage(t, a, "https://host.example/picked.bin")[0].ID
	for _, dir := range got.Folders {
		b, _ := json.Marshal(map[string]any{"ids": []string{id}, "dir": dir})
		if status, body := member(http.MethodPost, "/api/tasks/options", string(b)); status != http.StatusNoContent {
			t.Errorf("a member picking %s, which it was offered = %d (%s), want 204", dir, status, body)
		}
	}

	if status, _ := owner(http.MethodGet, "/api/download-folders", ""); status != http.StatusOK {
		t.Errorf("the owner reading the download folders = %d, want 200", status)
	}
}

func TestAMemberReadsTheCategoriesByNameAndNothingElseOfThem(t *testing.T) {
	t.Parallel()
	a, _, category, member, _ := groupServer(t)
	s := a.Settings.Get()
	extract, prio := t.TempDir(), 2
	s.Categories[0].ExtractTo = extract
	s.Categories[0].Priority = &prio
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	status, body := member(http.MethodGet, "/api/categories", "")
	var got struct {
		Categories []map[string]any `json:"categories"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &got) != nil {
		t.Fatalf("a member reading the categories = %d (%s), want 200 and a list", status, body)
	}
	if len(got.Categories) != 1 {
		t.Fatalf("categories = %v, want the one the instance has", got.Categories)
	}
	keys := make([]string, 0, 2)
	for k := range got.Categories[0] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"id", "name"}) {
		t.Errorf("a category carries %v, want only its id and its name", keys)
	}
	if got.Categories[0]["id"] != "serien" || got.Categories[0]["name"] != "Serien" {
		t.Errorf("category = %v, want serien named Serien", got.Categories[0])
	}
	for _, dir := range []string{category, extract} {
		if strings.Contains(string(body), filepath.Base(dir)) {
			t.Errorf("the categories a member reads name the folder %s: %s", dir, body)
		}
	}

	id := stage(t, a, "https://host.example/filed.bin")[0].ID
	if status, body := member(http.MethodPost, "/api/tasks/category", `{"ids":["`+id+`"],"category":"serien"}`); status != http.StatusOK {
		t.Errorf("a member filing a link under the category it read = %d (%s), want 200", status, body)
	}
}

func TestTheSettingsStayClosedToAMemberBesideTheTwoLists(t *testing.T) {
	t.Parallel()
	_, _, _, member, _ := groupServer(t)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/settings", ""},
		{http.MethodGet, "/api/settings/defaults", ""},
		{http.MethodPatch, "/api/settings", `{"downloadDir":"/elsewhere"}`},
		{http.MethodGet, "/api/folders", ""},
		{http.MethodGet, "/api/options", ""},
	} {
		if status, body := member(c.method, c.path, c.body); !refusedForGroup(status, body) {
			t.Errorf("%s %s answered a member %d (%s), want ownLoginNeeded", c.method, c.path, status, body)
		}
	}
	if status, _ := member(http.MethodPost, "/api/download-folders", "{}"); status == http.StatusOK {
		t.Error("the folder list takes a write from a member")
	}
}
