package app

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// The side rows of a yt-dlp link, its thumbnail, subtitles and description,
// take the name of the video row's file, quality included, so Plex, Jellyfin
// and Kodi pair them with it: "Clip (1080p).mkv" beside "Clip (1080p).de.srt".
// Each row writes its own file, and the video's quality is known only once it
// is written, so the names are matched whenever a row of the family finishes,
// in whichever order they do.

// quality is the bracket the video template puts after the title.
var quality = regexp.MustCompile(` \([^()]*\)$`)

// counter is the number collide.Counted puts before the extension when a side
// file met another family's file of the same name.
var counter = regexp.MustCompile(` \(\d+\)(\.[^.]*)$`)

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

// matchVariantNamesLocked renames the finished side rows of t's family after
// its finished video row, and returns copies of the rows it renamed. A row
// somebody named by hand keeps that name. Caller holds a.mu.
func (a *App) matchVariantNamesLocked(t *core.Task) []taskCopy {
	if t.Variant == "" {
		return nil
	}
	family := a.familyLocked(t)
	var video *core.Task
	for _, x := range family {
		if kind, _ := variantDecode(x.Variant); kind == ytdlp.VariantVideo && x.Status == core.StatusDone && x.File != "" {
			video = x
		}
	}
	if video == nil {
		return nil
	}
	stem := strings.TrimSuffix(filepath.Base(video.File), filepath.Ext(video.File))
	title := quality.ReplaceAllString(stem, "")
	// A typed template, or a video kept as separate streams, carries no
	// quality to take over.
	if title == stem {
		return nil
	}
	var out []taskCopy
	for _, x := range family {
		kind, _ := variantDecode(x.Variant)
		side := kind == ytdlp.VariantThumbnail || kind == ytdlp.VariantSubtitle || kind == ytdlp.VariantDescription
		if !side || x.Status != core.StatusDone || x.File == "" || strings.TrimSpace(x.Filename) != "" {
			continue
		}
		file, moved := renameSide(x.File, title, stem)
		if !moved {
			continue
		}
		x.File = file
		x.Name = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		// A subtitle row's further languages.
		for i, f := range x.WorkFiles {
			if to, ok := renameSide(f, title, stem); ok {
				x.WorkFiles[i] = to
			}
		}
		a.noteMovedLocked(x.ID)
		out = append(out, a.copyLocked(x))
	}
	return out
}

// renameSide moves a side file named after title to stem, keeping what
// follows the title, and drops the counter a collision added on the way. It
// leaves alone a file named otherwise and a name already taken.
func renameSide(file, title, stem string) (string, bool) {
	rest, ok := strings.CutPrefix(filepath.Base(file), title)
	if !ok || !strings.HasPrefix(rest, ".") {
		return file, false
	}
	rest = counter.ReplaceAllString(rest, "$1")
	to := filepath.Join(filepath.Dir(file), stem+rest)
	if _, err := os.Lstat(to); err == nil {
		return file, false
	}
	if err := os.Rename(file, to); err != nil {
		log.Printf("did not name %q after its video: %v", file, err)
		return file, false
	}
	return to, true
}
