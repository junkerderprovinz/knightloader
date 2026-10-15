package privdrop

// makePlan carries every rule, so these run without root and on every
// platform. The switch itself is proved in CI, by starting the image as root
// (the image job in .github/workflows/ci.yml).

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// looks answers from a table: what a stat and a directory listing would have
// found.
func looks(table map[string]folder) func(string) (folder, bool) {
	return func(dir string) (folder, bool) {
		f, ok := table[dir]
		return f, ok
	}
}

func path(p string) string { return filepath.Clean(p) }

func TestAFreshEmptyRootOwnedFolderIsHandedToTheDefaultAccount(t *testing.T) {
	p, err := makePlan(0, noEnv, []string{"/data"}, looks(map[string]folder{path("/data"): {uid: 0, empty: true}}))
	if err != nil {
		t.Fatal(err)
	}
	if !p.drop || p.uid != DefaultUID || p.gid != DefaultGID {
		t.Errorf("a root start without PUID or PGID planned drop=%v as %d:%d, want %d:%d", p.drop, p.uid, p.gid, DefaultUID, DefaultGID)
	}
	if !slices.Equal(p.handOver, []string{path("/data")}) {
		t.Errorf("the empty folder Docker just created was not handed over: %v", p.handOver)
	}
}

func TestAFolderWithSomethingInItKeepsItsOwner(t *testing.T) {
	p, err := makePlan(0, noEnv, []string{"/data"}, looks(map[string]folder{path("/data"): {uid: 0, empty: false}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.handOver) != 0 {
		t.Errorf("a root-owned folder with files in it was planned for a chown: %v; that rewrites the ownership of somebody's data", p.handOver)
	}
	if !p.drop {
		t.Error("the process would stay root because a folder could not be handed over")
	}
}

func TestAnEmptyFolderSomebodyElseOwnsIsLeftAlone(t *testing.T) {
	p, err := makePlan(0, noEnv, []string{"/watch"}, looks(map[string]folder{path("/watch"): {uid: 99, empty: true}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.handOver) != 0 {
		t.Errorf("an empty folder owned by uid 99 was planned for a chown: %v", p.handOver)
	}
}

func TestAMissingFolderIsSkipped(t *testing.T) {
	p, err := makePlan(0, noEnv, []string{"/watch", "", " "}, looks(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.handOver) != 0 {
		t.Errorf("folders that are not there were planned for a chown: %v", p.handOver)
	}
}

func TestPUIDAndPGIDNameTheAccount(t *testing.T) {
	p, err := makePlan(0, env(map[string]string{"PUID": "500", "PGID": " 501 "}), []string{"/data"},
		looks(map[string]folder{path("/data"): {uid: 0, empty: true}}))
	if err != nil {
		t.Fatal(err)
	}
	if p.uid != 500 || p.gid != 501 {
		t.Errorf("PUID=500 PGID=501 planned %d:%d", p.uid, p.gid)
	}
}

// TestAStartAsAnotherAccountChangesNothing is --user 99:100 on Unraid: PUID
// and PGID are not read, and no folder is touched, however it looks.
func TestAStartAsAnotherAccountChangesNothing(t *testing.T) {
	p, err := makePlan(99, env(map[string]string{"PUID": "500", "PGID": "0"}), []string{"/data"},
		looks(map[string]folder{path("/data"): {uid: 0, empty: true}}))
	if err != nil {
		t.Errorf("a start as uid 99 read PGID=0 and refused it: %v", err)
	}
	if p.drop || len(p.handOver) != 0 {
		t.Errorf("a start as uid 99 planned drop=%v and a chown of %v", p.drop, p.handOver)
	}
}

func TestAnIdThatKeepsRootOrDoesNotParseStopsTheStart(t *testing.T) {
	for _, vars := range []map[string]string{
		{"PUID": "0"},
		{"PGID": "0"},
		{"PUID": "knight"},
		{"PGID": "-1"},
	} {
		_, err := makePlan(0, env(vars), nil, looks(nil))
		if err == nil {
			t.Errorf("%v was accepted", vars)
			continue
		}
		for name := range vars {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the refusal of %v does not name %s: %v", vars, name, err)
			}
		}
	}
}

func TestAFolderNamedTwiceIsPlannedOnce(t *testing.T) {
	p, err := makePlan(0, noEnv, []string{"/data", "/data/"}, looks(map[string]folder{path("/data"): {uid: 0, empty: true}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.handOver) != 1 {
		t.Errorf("one folder named twice was planned %d times", len(p.handOver))
	}
}

// TestAMountPointInsideTheFolderDoesNotCountAsContent is the data directory of
// `-v appdata:/data -v downloads:/data/downloads`, where Docker has put the
// second volume's mount point into the first.
func TestAMountPointInsideTheFolderDoesNotCountAsContent(t *testing.T) {
	data := t.TempDir()
	downloads := filepath.Join(data, "downloads")
	if err := os.Mkdir(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	mounts := map[string]bool{downloads: true}

	empty, err := emptyBesides(data, mounts)
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Error("a data directory holding only another volume's mount point counted as in use")
	}

	if err := os.WriteFile(filepath.Join(data, "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if empty, _ := emptyBesides(data, mounts); empty {
		t.Error("a data directory with a settings file in it counted as empty")
	}
	if empty, _ := emptyBesides(downloads, mounts); !empty {
		t.Error("an empty folder counted as in use")
	}
}

func TestAFolderThatIsNotAMountPointCountsAsContent(t *testing.T) {
	data := t.TempDir()
	if err := os.Mkdir(filepath.Join(data, "jd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if empty, _ := emptyBesides(data, map[string]bool{}); empty {
		t.Error("a data directory holding a plain sub-folder counted as empty")
	}
}

func TestMountPointsAreReadFromTheFifthField(t *testing.T) {
	info := strings.Join([]string{
		`36 35 98:0 /mnt1 /mnt/parent rw,noatime master:1 - ext3 /dev/root rw,errors=continue`,
		`600 598 0:52 /appdata/knightloader /data rw,relatime - btrfs /dev/sdb1 rw`,
		`601 600 0:53 /user/My\040Downloads /data/downloads rw,relatime - shfs shfs rw`,
		``,
	}, "\n")
	got := mountPoints(info)
	for _, want := range []string{"/mnt/parent", "/data", "/data/downloads"} {
		if !got[path(want)] {
			t.Errorf("%s is missing from %v", want, got)
		}
	}
	if got[path("/user/My Downloads")] {
		t.Error("the mount's source was read as a mount point")
	}
	if len(got) != 3 {
		t.Errorf("read %d mount points from three lines: %v", len(got), got)
	}
}

func TestAnEscapedMountPointIsReadAsTheKernelMeantIt(t *testing.T) {
	got := mountPoints(`601 600 0:53 / /data/My\040Downloads rw - shfs shfs rw`)
	if !got[path("/data/My Downloads")] {
		t.Errorf("the escaped space was not undone: %v", got)
	}
}
