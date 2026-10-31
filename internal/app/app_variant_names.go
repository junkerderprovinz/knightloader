package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// The side rows of a yt-dlp link, its thumbnail, subtitles and description,
// take the name of the video row's file, quality included, so Plex, Jellyfin
// and Kodi pair them with it: "Clip (1080p).mkv" beside "Clip (1080p).de.srt".
// Each row writes its own file, under a name marked with its own ID until the
// video's name is known (see ytdlp.SideMark), and the names are matched
// whenever a row of the family finishes, in whichever order they do. A video
// that will not come leaves its side rows the plain title.

// quality is the bracket the video template puts after the title.
var quality = regexp.MustCompile(` \([^()]*\)$`)

// familyLocked is the rows staged from one paste of a link: the same URL and
// the same creation time, which every sibling copies from the video row.
// Caller holds a.mu.
func (a *App) familyLocked(t *core.Task) []*core.Task {
	var out []*core.Task
	for _, x := range a.tasks {
		if x.Variant != "" && x.URL == t.URL && x.CreatedAt.Equal(t.CreatedAt) {
			out = append(out, x)
		}
	}
	return out
}

// sideStem is the name the side rows of family take: the stem of the video
// row's file, or "" for their plain title when the video carries no quality
// or will not be written. It is false while the video may still come.
func sideStem(family []*core.Task) (string, bool) {
	for _, x := range family {
		if kind, _ := variantDecode(x.Variant); kind != ytdlp.VariantVideo {
			continue
		}
		switch {
		case x.Status == core.StatusDone && x.File != "":
			stem := strings.TrimSuffix(filepath.Base(x.File), filepath.Ext(x.File))
			if quality.MatchString(stem) {
				return stem, true
			}
			// A typed template, or a video kept as separate streams.
			return "", true
		case x.Status == core.StatusDone, x.Status == core.StatusCollected:
			return "", true
		case x.Status == core.StatusError:
			return "", x.NextTry.IsZero()
		}
		return "", false
	}
	return "", true
}

// matchVariantNamesLocked names the finished side rows of t's family after
// its video row once that is settled, and returns copies of the rows it
// renamed. A row somebody named by hand keeps that name. Caller holds a.mu.
func (a *App) matchVariantNamesLocked(t *core.Task) []taskCopy {
	if t.Variant == "" {
		return nil
	}
	family := a.familyLocked(t)
	stem, settled := sideStem(family)
	if !settled {
		return nil
	}
	var out []taskCopy
	for _, x := range family {
		if kind, _ := variantDecode(x.Variant); kind == ytdlp.VariantVideo || kind == ytdlp.VariantAudio {
			continue
		}
		if x.Status != core.StatusDone || x.File == "" || strings.TrimSpace(x.Filename) != "" {
			continue
		}
		if !nameSide(x, stem) {
			continue
		}
		a.noteMovedLocked(x.ID)
		out = append(out, a.copyLocked(x))
	}
	return out
}

// settleRemovedVariantLocked names what removing t leaves behind: its own
// marked file, which no video renames once the row is gone, and the side
// files of a family whose video t was. Caller holds a.mu, and t is no longer
// in a.tasks.
func (a *App) settleRemovedVariantLocked(t *core.Task, deleteFiles bool) []taskCopy {
	if t.Variant == "" {
		return nil
	}
	if !deleteFiles && t.Status == core.StatusDone && t.File != "" && strings.TrimSpace(t.Filename) == "" {
		stem, _ := sideStem(a.familyLocked(t))
		nameSide(t, stem)
	}
	return a.matchVariantNamesLocked(t)
}

// nameSide moves the files of side row x from their marked names to stem, or
// to the plain title for "", and reports whether its main file moved.
func nameSide(x *core.Task, stem string) bool {
	mark := ytdlp.SideMark(x.ID)
	file, moved := renameSide(x.File, mark, stem)
	if !moved {
		return false
	}
	x.File = file
	x.Name = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	// A subtitle row's further languages.
	for i, f := range x.WorkFiles {
		if to, ok := renameSide(f, mark, stem); ok {
			x.WorkFiles[i] = to
		}
	}
	return true
}

// renameSide moves file from its name with mark to stem, keeping what follows
// the mark, or to the title before the mark when stem is "". A name already
// taken gets a counter, as collide.Rename would give it.
func renameSide(file, mark, stem string) (string, bool) {
	title, rest, ok := strings.Cut(filepath.Base(file), mark)
	if !ok {
		return file, false
	}
	if stem == "" {
		stem = title
	}
	dir := filepath.Dir(file)
	to := filepath.Join(dir, stem+rest)
	for n := 2; ; n++ {
		if _, err := os.Lstat(to); err != nil {
			break
		}
		if n > collide.DefaultMaxAttempts {
			log.Printf("did not name %q after its video: no free name", file)
			return file, false
		}
		to = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, rest))
	}
	if err := os.Rename(file, to); err != nil {
		log.Printf("did not name %q after its video: %v", file, err)
		return file, false
	}
	return to, true
}
