package torrent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// FileRules choose the files of a torrent that nobody chose by hand: skip the
// sample, the .nfo and the .exe that ride along with what was wanted. The zero
// value chooses every file.
//
// The patterns are regular expressions matched against a file's path inside
// the torrent, such as "Sample/sample.mkv", unanchored and with case counting,
// which is how a Packagizer "matches" condition reads a file name.
type FileRules struct {
	// MinSize skips a file smaller than this many bytes; 0 keeps every size.
	MinSize int64
	// Include, when it has any pattern, keeps only a file that matches one.
	Include []string
	// Exclude skips a file that matches any of its patterns.
	Exclude []string
}

// Empty reports whether r would choose every file of any torrent.
func (r FileRules) Empty() bool {
	return r.MinSize <= 0 && len(r.Include) == 0 && len(r.Exclude) == 0
}

// Pick returns a copy of files with Selected set by the rules. It is Compile
// and Picker.Pick in one call, for a caller with one file list.
func (r FileRules) Pick(files []core.TorrentFile) ([]core.TorrentFile, error) {
	p, err := r.Compile()
	if err != nil {
		return nil, err
	}
	return p.Pick(files), nil
}

// Compile checks every pattern. A pattern that does not compile refuses the
// whole set rather than being left out, since a skip rule that silently is not
// there fetches the files somebody asked to be spared.
func (r FileRules) Compile() (Picker, error) {
	include, err := compilePatterns(r.Include)
	if err != nil {
		return Picker{}, fmt.Errorf("torrent file selection, only these files: %w", err)
	}
	exclude, err := compilePatterns(r.Exclude)
	if err != nil {
		return Picker{}, fmt.Errorf("torrent file selection, never these files: %w", err)
	}
	return Picker{min: r.MinSize, include: include, exclude: exclude}, nil
}

// CheckPatterns refuses the first pattern in list that does not compile, for a
// settings form that shows the refusal beside the box it came from.
func CheckPatterns(list []string) error {
	_, err := compilePatterns(list)
	return err
}

// compilePatterns skips blank lines, as the crawl filters do: an empty
// pattern matches every path, which as a skip rule would skip everything.
func compilePatterns(list []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(list))
	for i, p := range list {
		if strings.TrimSpace(p) == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("line %d is not a regular expression: %v", i+1, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// Picker is FileRules compiled.
type Picker struct {
	min              int64
	include, exclude []*regexp.Regexp
}

// Wants reports whether the rules keep the file at path, size bytes long.
func (p Picker) Wants(path string, size int64) bool {
	if size < p.min {
		return false
	}
	for _, re := range p.exclude {
		if re.MatchString(path) {
			return false
		}
	}
	if len(p.include) == 0 {
		return true
	}
	for _, re := range p.include {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

// Pick returns a copy of files with Selected set by the rules, whatever it was
// before.
//
// When the rules would leave nothing, every file is selected instead. A torrent
// whose every file is filtered away is far more often a rule that does not fit
// it than a torrent nobody wants, and an empty selection cannot be started.
func (p Picker) Pick(files []core.TorrentFile) []core.TorrentFile {
	out := make([]core.TorrentFile, len(files))
	kept := false
	for i, f := range files {
		f.Selected = p.Wants(f.Path, f.Size)
		kept = kept || f.Selected
		out[i] = f
	}
	if !kept {
		for i := range out {
			out[i].Selected = true
		}
	}
	return out
}
