package buildinfo

import (
	"reflect"
	"runtime/debug"
	"testing"
)

func TestDescribeNamesVersionAndCommit(t *testing.T) {
	defer func(v, c string) { Version, Commit = v, c }(Version, Commit)
	Version, Commit = "v1.2.3", "0123abcd"
	if got, want := Describe("knightloader-relay"), "knightloader-relay v1.2.3 (commit 0123abcd)"; got != want {
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
