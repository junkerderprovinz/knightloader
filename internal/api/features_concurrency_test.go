package api

import (
	"strconv"
	"sync"
	"testing"
)

// TestASwitchKeepsWhatAnotherWriterSavedMeanwhile: Sonarr files a new category
// while somebody flips a module. The switch must not write back the settings
// it read before the category existed.
func TestASwitchKeepsWhatAnotherWriterSavedMeanwhile(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	const n = 40

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 4 * n {
			if err := setFeature(a, "metrics", i%2 == 0); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range n {
			if _, _, err := a.Settings.EnsureCategory("grabs-"+strconv.Itoa(i), ""); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()

	cfg := a.Settings.Get()
	for i := range n {
		if _, ok := cfg.CategoryByName("grabs-" + strconv.Itoa(i)); !ok {
			t.Errorf("category grabs-%d was filed and is gone after the module switches", i)
		}
	}
}
