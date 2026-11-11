package afterdownload

import (
	"context"
	"slices"
	"testing"
)

func towerWithTargets(f *fakeFortress) *fakeEngine {
	e := f.addEngine("m-tower", "Tower", photosAndInbox()...)
	e.destinations = []Destination{
		{ID: "nas", Name: "NAS", Kind: "share", Where: "share /archive"},
		{ID: "usb", Name: "Backup disk", Kind: "disk", Where: "/mnt/disks/backup"},
	}
	e.mounted = []string{"/mnt/user/media"}
	return e
}

func TestDestinationsAreTheTargetsArrowLoopKnows(t *testing.T) {
	f := newFakeFortress()
	towerWithTargets(f)

	got, err := Destinations(context.Background(), f, "m-tower")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Destination{ID: "nas", Name: "NAS", Kind: "share", Where: "share /archive"}) || got[1].ID != "usb" {
		t.Errorf("Destinations = %+v", got)
	}
}

func TestAnEngineWithoutTargetsHasAnEmptyList(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower")

	got, err := Destinations(context.Background(), f, "m-tower")
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("Destinations = %+v, %v", got, err)
	}
}

func TestCreateMakesTheJobInArrowLoop(t *testing.T) {
	f := newFakeFortress()
	e := towerWithTargets(f)

	req := CreateRequest{Name: "Series to the archive", Source: "/mnt/user/media/series", Target: "nas", Mode: "mirror"}
	job, err := Create(context.Background(), f, "m-tower", req)
	if err != nil || job != "Series to the archive" {
		t.Fatalf("Create = %q, %v", job, err)
	}
	if !slices.Equal(e.created, []CreateRequest{req}) {
		t.Errorf("ArrowLoop was asked %+v", e.created)
	}
	engines := Engines(context.Background(), f)
	if !slices.ContainsFunc(engines[0].Jobs, func(j Job) bool { return j.Name == req.Name && j.Mode == "mirror" }) {
		t.Errorf("the new job is not listed: %+v", engines[0].Jobs)
	}
}

func TestCreateSaysWhyArrowLoopRefused(t *testing.T) {
	f := newFakeFortress()
	towerWithTargets(f)

	for _, c := range []struct {
		name string
		req  CreateRequest
		want string
	}{
		{"name taken", CreateRequest{Name: "photos", Source: "/mnt/user/media/series", Target: "nas", Mode: "sync"}, CodeNameTaken},
		{"unknown target", CreateRequest{Name: "new", Source: "/mnt/user/media/series", Target: "cloud", Mode: "sync"}, CodeNoSuchTarget},
		{"folder not mounted", CreateRequest{Name: "new", Source: "/srv/elsewhere", Target: "nas", Mode: "sync"}, CodeSourceNotMounted},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Create(context.Background(), f, "m-tower", c.req); Code(err) != c.want {
				t.Errorf("Create: %v (%s), want %s", err, Code(err), c.want)
			}
		})
	}
	if _, err := Create(context.Background(), nil, "m-tower", CreateRequest{}); Code(err) != CodeNoFortress {
		t.Errorf("Create without a fortress: %v", err)
	}
	if _, err := Create(context.Background(), f, "m-gone", CreateRequest{Name: "x"}); Code(err) != CodeGone {
		t.Errorf("Create on a member that left: %v", err)
	}
}

func TestAModeOutsideTheThreeIsRefusedBeforeAsking(t *testing.T) {
	f := newFakeFortress()
	e := towerWithTargets(f)

	_, err := Create(context.Background(), f, "m-tower", CreateRequest{Name: "new", Source: "/mnt/user/media", Target: "nas", Mode: "wipe"})
	if Code(err) != CodeBadRequest || len(e.created) != 0 {
		t.Errorf("Create with mode wipe: %v (%s), asked %+v", err, Code(err), e.created)
	}
}
