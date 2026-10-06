package app

import (
	"slices"
	"testing"
)

func TestTheVolumesFetchedForARepairCoverItWithLittleToSpare(t *testing.T) {
	// The doubling sizes a post usually has.
	spare := []spareVolume{{"v1", 1}, {"v2", 2}, {"v4", 4}, {"v8", 8}, {"v16", 16}}
	for need, want := range map[int][]string{
		1:  {"v1"},
		3:  {"v4"},
		5:  {"v8"},
		20: {"v16", "v4"},
		31: {"v16", "v8", "v4", "v2", "v1"},
	} {
		var got []string
		for _, v := range pickVolumes(spare, need) {
			got = append(got, v.id)
		}
		if !slices.Equal(got, want) {
			t.Errorf("for %d blocks picked %v, want %v", need, got, want)
		}
	}
}
