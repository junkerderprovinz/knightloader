package buildinfo

import (
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

func TestDescribeNamesVersionAndCommit(t *testing.T) {
	defer func(v, c string) { Version, Commit = v, c }(Version, Commit)
	Version, Commit = "v1.2.3", "0123abcd"
	if got, want := Describe("knightloader"), "knightloader v1.2.3 (commit 0123abcd)"; got != want {
		t.Fatalf("Describe = %q, want %q", got, want)
	}
}

func TestModulesKeepsTheAskedOrderAndReadsAForksVersion(t *testing.T) {
	deps := []*debug.Module{
		{Path: "example.com/zip", Version: "v1.2.0"},
		{Path: "example.com/engine", Version: "v1.9.3", Replace: &debug.Module{Path: "example.com/fork/engine", Version: "v0.0.0-20261005140626-5111a4c57695"}},
	}
	got := modulesIn(deps, []string{"example.com/engine", "example.com/absent", "example.com/zip"})
	want := []Module{
		{Path: "example.com/engine", Version: "v0.0.0-20261005140626-5111a4c57695", Fork: true},
		{Path: "example.com/zip", Version: "v1.2.0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("modulesIn = %+v, want %+v", got, want)
	}
}

func TestTheContainerIDComesFromTheMountedHostnameFile(t *testing.T) {
	const id = "e8fc22d4db7b5d0c1f3a9e6b2c4d8f0a1b3c5d7e9f0a2b4c6d8e0f1a3b5c7d9e"
	for name, c := range map[string]struct{ mountinfo, host, want string }{
		"docker": {
			"1045 1027 0:52 /containers/" + id + "/hostname /etc/hostname rw,relatime - btrfs /dev/loop2 rw\n",
			"knightloader", id,
		},
		"podman": {
			"812 790 0:44 /var/lib/containers/storage/overlay-containers/" + id + "/userdata/hostname /etc/hostname rw - tmpfs tmpfs rw\n",
			"", id,
		},
		"short hostname": {"36 25 0:30 / /data rw - ext4 /dev/sda1 rw\n", "e8fc22d4db7b", "e8fc22d4db7b"},
		"named hostname": {"36 25 0:30 / /data rw - ext4 /dev/sda1 rw\n", "tower", ""},
	} {
		if got := containerIDIn(strings.NewReader(c.mountinfo), c.host); got != c.want {
			t.Errorf("%s: containerIDIn = %q, want %q", name, got, c.want)
		}
	}
}
