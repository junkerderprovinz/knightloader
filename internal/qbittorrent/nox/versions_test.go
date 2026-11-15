package nox

import (
	"strings"
	"testing"
)

func TestTheTableNamesADefaultItHolds(t *testing.T) {
	table := Versions()
	v, err := table.Pick("")
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != table.Default || v.Note == "" {
		t.Fatalf("the default is %q with note %q, want %q with a note", v.Version, v.Note, table.Default)
	}
}

func TestEveryVersionHasABuildForEachPlatformKnightLoaderShipsOn(t *testing.T) {
	for _, v := range Versions().Versions {
		for _, p := range []string{"linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64", "darwin-arm64", "darwin-amd64"} {
			goos, goarch, _ := strings.Cut(p, "-")
			a, ok := v.AssetFor(goos, goarch)
			if !ok || a.Tag == "" || a.Name == "" {
				t.Errorf("%s has no build for %s", v.Version, p)
			}
		}
	}
}

// Windows on ARM runs the amd64 build under its x64 emulation, so it fetches
// the same zip, checked against the same digest.
func TestWindowsOnArmGetsTheAmd64Build(t *testing.T) {
	for _, v := range Versions().Versions {
		arm, ok := v.AssetFor("windows", "arm64")
		amd, _ := v.AssetFor("windows", "amd64")
		if !ok || arm != amd {
			t.Errorf("%s windows/arm64 gets %+v, want the windows/amd64 build %+v", v.Version, arm, amd)
		}
	}
}

func TestAPlatformWithoutABuildHasNone(t *testing.T) {
	if a, ok := Versions().Versions[0].AssetFor("linux", "riscv64"); ok {
		t.Errorf("linux/riscv64 gets %+v", a)
	}
}

func TestTheLinuxBuildsComeFromTheStaticReleaseOfTheirVersion(t *testing.T) {
	for _, v := range Versions().Versions {
		for _, arch := range []string{"amd64", "arm64"} {
			a, _ := v.AssetFor("linux", arch)
			if a.Repo != "userdocs/qbittorrent-nox-static" {
				t.Errorf("%s linux/%s comes from %q", v.Version, arch, a.Repo)
			}
			if want := "release-" + v.Version + "_v" + v.Libtorrent; a.Tag != want {
				t.Errorf("%s linux/%s is tagged %q, want %q", v.Version, arch, a.Tag, want)
			}
		}
	}
}

func TestOwnBuildsComeFromTheReleaseLocation(t *testing.T) {
	t.Setenv(releasesEnv, "")
	if got := ReleaseLocation(); got != defaultReleases {
		t.Fatalf("ReleaseLocation() = %q, want %q", got, defaultReleases)
	}
	t.Setenv(releasesEnv, " someone/builds ")
	if got := ReleaseLocation(); got != "someone/builds" {
		t.Fatalf("ReleaseLocation() = %q, want someone/builds", got)
	}
	for _, v := range Versions().Versions {
		a, _ := v.AssetFor("windows", "amd64")
		if a.Repo != "" {
			t.Errorf("%s windows/amd64 names a repository of its own, %q", v.Version, a.Repo)
		}
		if got := a.from(); got != "someone/builds" {
			t.Errorf("%s windows/amd64 is fetched from %q", v.Version, got)
		}
	}
}

func TestAVersionTheTableDoesNotKnowIsRefused(t *testing.T) {
	if _, err := Versions().Pick("9.9.9"); err == nil {
		t.Fatal("an unknown version was taken")
	}
}

func TestEveryPinnedDigestIsSHA256Hex(t *testing.T) {
	for _, v := range Versions().Versions {
		for p, a := range v.Assets {
			if a.SHA256 != "" && !isSHA256(a.SHA256) {
				t.Errorf("%s %s has digest %q", v.Version, p, a.SHA256)
			}
		}
	}
}
