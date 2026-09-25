package torrent

import (
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func release() []core.TorrentFile {
	return []core.TorrentFile{
		{Path: "Movie.2024.1080p.mkv", Size: 4 << 30},
		{Path: "Sample/Movie.Sample.mkv", Size: 40 << 20},
		{Path: "Movie.2024.1080p.nfo", Size: 3 << 10},
		{Path: "Subs/English.srt", Size: 80 << 10},
		{Path: "setup.exe", Size: 2 << 20},
	}
}

func selectedPaths(files []core.TorrentFile) []string {
	var out []string
	for _, f := range files {
		if f.Selected {
			out = append(out, f.Path)
		}
	}
	return out
}

func samePaths(t *testing.T, got []core.TorrentFile, want ...string) {
	t.Helper()
	have := selectedPaths(got)
	if strings.Join(have, "|") != strings.Join(want, "|") {
		t.Fatalf("selected %q, want %q", have, want)
	}
}

func TestNoFileRulesChooseEveryFile(t *testing.T) {
	var r FileRules
	if !r.Empty() {
		t.Fatal("the zero FileRules claims to have rules")
	}
	got, err := r.Pick(release())
	if err != nil {
		t.Fatal(err)
	}
	samePaths(t, got, "Movie.2024.1080p.mkv", "Sample/Movie.Sample.mkv", "Movie.2024.1080p.nfo", "Subs/English.srt", "setup.exe")
}

func TestSamplesNfoAndExeFilesAreSkipped(t *testing.T) {
	r := FileRules{MinSize: 1 << 20, Exclude: []string{`(?i)sample`, `\.(nfo|exe)$`}}
	got, err := r.Pick(release())
	if err != nil {
		t.Fatal(err)
	}
	samePaths(t, got, "Movie.2024.1080p.mkv")
}

func TestAnIncludePatternKeepsOnlyTheFilesItMatches(t *testing.T) {
	r := FileRules{Include: []string{`\.mkv$`, `\.srt$`}, Exclude: []string{`(?i)^sample/`}}
	got, err := r.Pick(release())
	if err != nil {
		t.Fatal(err)
	}
	samePaths(t, got, "Movie.2024.1080p.mkv", "Subs/English.srt")
}

// The Packagizer's "matches" folds no case, so neither do these.
func TestCaseCountsUnlessThePatternSaysOtherwise(t *testing.T) {
	got, err := FileRules{Exclude: []string{`sample`}}.Pick(release())
	if err != nil {
		t.Fatal(err)
	}
	if !got[1].Selected {
		t.Error(`"sample" skipped "Sample/Movie.Sample.mkv", which spells it with a capital S`)
	}
	got, err = FileRules{Exclude: []string{`^Sample/`}}.Pick(release())
	if err != nil {
		t.Fatal(err)
	}
	if got[1].Selected {
		t.Error(`"^Sample/" did not match the folder the sample sits in`)
	}
}

func TestRulesThatWouldLeaveNothingKeepEveryFile(t *testing.T) {
	files := []core.TorrentFile{{Path: "installer.exe", Size: 10}, {Path: "readme.nfo", Size: 10}}
	got, err := FileRules{Exclude: []string{`\.(exe|nfo)$`}}.Pick(files)
	if err != nil {
		t.Fatal(err)
	}
	samePaths(t, got, "installer.exe", "readme.nfo")
}

func TestPickDecidesAfreshAndLeavesTheCallersListAlone(t *testing.T) {
	files := release()
	for i := range files {
		files[i].Selected = i == 4
	}
	got, err := FileRules{Exclude: []string{`\.exe$`}}.Pick(files)
	if err != nil {
		t.Fatal(err)
	}
	if got[4].Selected || !got[0].Selected {
		t.Fatalf("the earlier selection leaked into the rules' answer: %+v", got)
	}
	if !files[4].Selected || files[0].Selected {
		t.Fatal("Pick wrote into the list it was given")
	}
}

func TestABrokenPatternRefusesTheWholeSetAndSaysWhere(t *testing.T) {
	_, err := FileRules{Exclude: []string{`\.nfo$`, `(unclosed`}}.Pick(release())
	if err == nil {
		t.Fatal("a pattern that does not compile was accepted")
	}
	for _, want := range []string{"never these files", "line 2", "(unclosed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	if err := CheckPatterns([]string{`ok`, `[`}); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("CheckPatterns = %v, want the second line refused", err)
	}
}
