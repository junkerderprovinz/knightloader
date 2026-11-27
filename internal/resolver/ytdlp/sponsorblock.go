package ytdlp

import (
	"slices"
	"strings"
)

// SponsorBlock is what yt-dlp does with the segments SponsorBlock's community
// marked in a video: Remove cuts them out with ffmpeg, Mark turns them into
// chapters. Both hold names from SponsorBlockCategories. yt-dlp asks the
// SponsorBlock API on YouTube only and leaves every other site as it is.
type SponsorBlock struct {
	Remove []string `json:"remove"`
	Mark   []string `json:"mark"`
}

// SponsorBlockCategories lists the categories yt-dlp accepts, in the order its
// help gives them.
func SponsorBlockCategories() []string {
	return []string{"sponsor", "intro", "outro", "selfpromo", "preview", "filler",
		"interaction", "music_offtopic", "poi_highlight", "chapter", "hook"}
}

// markOnly are the categories that mark a point or a chapter rather than a
// stretch to skip, which yt-dlp refuses to remove.
var markOnly = []string{"poi_highlight", "chapter"}

// Sanitize keeps the known categories of each list once, in their order, and
// drops the mark-only ones from Remove. yt-dlp refuses the whole run over one
// name it does not know. An empty list stays a list, so the settings read
// [] rather than null.
func (s SponsorBlock) Sanitize() SponsorBlock {
	return SponsorBlock{
		Remove: knownCategories(s.Remove, markOnly),
		Mark:   knownCategories(s.Mark, nil),
	}
}

func knownCategories(in, refused []string) []string {
	out := []string{}
	for _, c := range in {
		c = strings.ToLower(strings.TrimSpace(c))
		if slices.Contains(SponsorBlockCategories(), c) && !slices.Contains(refused, c) && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// args is the SponsorBlock part of a media row's flags.
func (s SponsorBlock) args() []string {
	var args []string
	if len(s.Remove) > 0 {
		args = append(args, "--sponsorblock-remove", strings.Join(s.Remove, ","))
	}
	if len(s.Mark) > 0 {
		args = append(args, "--sponsorblock-mark", strings.Join(s.Mark, ","))
	}
	return args
}

// cutBySponsorBlock recognises the line yt-dlp prints before it cuts segments
// out of a finished file.
func cutBySponsorBlock(line string) bool {
	return strings.HasPrefix(line, "[ModifyChapters] Removing chapters from ")
}
