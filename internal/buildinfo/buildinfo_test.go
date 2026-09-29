package buildinfo

import "testing"

func TestDescribeNamesVersionAndCommit(t *testing.T) {
	defer func(v, c string) { Version, Commit = v, c }(Version, Commit)
	Version, Commit = "v1.2.3", "0123abcd"
	if got, want := Describe("knightloader-relay"), "knightloader-relay v1.2.3 (commit 0123abcd)"; got != want {
		t.Fatalf("Describe = %q, want %q", got, want)
	}
}
