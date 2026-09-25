package settings

import (
	"strconv"
	"sync"
	"testing"
)

func TestEnsureCategoryFindsADrawerByNameWhateverTheCase(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cur := s.Get()
	cur.Categories = []Category{{ID: "serien", Name: "TV-Sonarr", Dir: absPath("media", "tv")}}
	if _, err := s.Set(cur); err != nil {
		t.Fatal(err)
	}

	id, created, err := s.EnsureCategory("tv-sonarr", absPath("elsewhere"))
	if err != nil {
		t.Fatal(err)
	}
	if id != "serien" || created {
		t.Errorf("EnsureCategory = %q, created %v; want the existing drawer named TV-Sonarr", id, created)
	}
	if n := len(s.Get().Categories); n != 1 {
		t.Errorf("the table holds %d categories, want the one it had", n)
	}
}

func TestEnsureCategoryMatchesTheIdANameFoldsTo(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cur := s.Get()
	cur.Categories = []Category{{ID: "radarr", Name: "Filme"}}
	if _, err := s.Set(cur); err != nil {
		t.Fatal(err)
	}
	id, created, err := s.EnsureCategory("Radarr", absPath("x"))
	if err != nil || id != "radarr" || created {
		t.Errorf("EnsureCategory = %q, %v, %v; want the drawer whose id Radarr folds to", id, created, err)
	}
}

func TestEnsureCategoryFilesANewDrawerOnce(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	folder := absPath("downloads", "tv-sonarr")

	var wg sync.WaitGroup
	createdCount := 0
	var mu sync.Mutex
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, created, err := s.EnsureCategory("tv-sonarr", folder)
			if err != nil || id != "tv-sonarr" {
				t.Errorf("EnsureCategory = %q, %v", id, err)
			}
			if created {
				mu.Lock()
				createdCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if createdCount != 1 {
		t.Errorf("%d calls reported creating the drawer, want exactly one", createdCount)
	}
	cats := s.Get().Categories
	if len(cats) != 1 || cats[0].Name != "tv-sonarr" || cats[0].Dir != folder {
		t.Fatalf("categories = %+v, want one named tv-sonarr in its own folder", cats)
	}

	// Saved, not only held in memory.
	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Get().Categories; len(got) != 1 || got[0].ID != "tv-sonarr" {
		t.Errorf("after a reload the categories are %+v", got)
	}
}

func TestEnsureCategoryRefusesPastTheLimit(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cur := s.Get()
	for i := range MaxCategories {
		cur.Categories = append(cur.Categories, Category{Name: "drawer " + strconv.Itoa(i)})
	}
	if _, err := s.Set(cur); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EnsureCategory("one-too-many", absPath("x")); err == nil {
		t.Error("a category past the limit was filed")
	}
}
