package app

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"unicode"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// LinkBatchOptions are the values the add-links form attaches to a whole batch
// rather than to one task. TaskOptions edits tasks that already exist; this is
// what a batch is staged with.
type LinkBatchOptions struct {
	// Dir overrides the destination folder for every task in the batch. It is
	// applied after staging, so it always wins over the Packagizer, whatever
	// Overrule says.
	Dir string
	// Password is the archive password tried first when a task from this batch
	// is unpacked. No Packagizer action can set it, so it is applied
	// unconditionally.
	Password string
	// DownloadPassword is what a hoster's page asks for before it hands over
	// the file. Like Password, it is applied unconditionally.
	DownloadPassword string
	// Comment is the note attached to every task in the batch.
	Comment string
	// Category is the id of the category every task in the batch is filed
	// in. It is set before the Packagizer runs, so a rule that names a
	// category wins, whatever Overrule says, and the tasks start at the
	// category's priority.
	Category string
	// KeepCollected leaves the batch in the collector whatever AutoConfirm
	// says, for a caller that was asked to add it stopped.
	KeepCollected bool
	// Priority and AutoExtract are nil when the form has no opinion, so that
	// "priority 0" and "extraction off" stay distinguishable from no answer.
	Priority    *int
	AutoExtract *bool
	// Overrule makes Priority, AutoExtract and Comment win over a matching
	// Packagizer rule. By default the rule wins, because these fields are seeded
	// before the Packagizer runs and packagize overwrites whatever it has an
	// opinion on.
	Overrule bool
	// Within caps how far inside the network a crawl of these links may reach.
	// A feed sets it to the scope of its own address, since its entries are
	// links the feed chose, not the user. Nil leaves each crawl at the scope
	// of the link it starts from.
	Within *httpx.Scope
	// Source is the page the links were found on, as a crawl records it.
	Source string
	// Headers are a browser's own request headers for a link it was
	// downloading (see BrowserHeaders). Such a link is staged as it is, with
	// no crawl and no playlist listing, and the headers go with its download
	// alone.
	Headers hostheaders.Set
	// File says the link is a file a browser was downloading, and FileName
	// is the name the browser gave it, one path segment, which the file is
	// written under. Such a link is staged as it is and fetched as a plain
	// file on every start, never by yt-dlp, which takes an address with no
	// file extension for a page and saves whatever it finds there.
	File     bool
	FileName string
	// Choice sets the host's preset aside for the yt-dlp links of this batch
	// (see ytdlp.HosterPreset.ForChoice), and Format is its short form for a
	// single row (see ytdlp.FormatChoice). With neither, they stage with the
	// preset.
	Choice *ytdlp.LinkChoice
	Format string
	// Start confirms and starts the batch at once, whatever AutoConfirm says,
	// for a caller that asked for a download rather than a list entry.
	Start bool

	// listed collects the rows already in the list that links of the batch
	// were folded into (see LinkBatch.Listed).
	listed *listedRows
}

// LinkBatch is what a batch of links came to: the rows it created, and the
// rows already in the list that some of its links were folded into, which a
// caller can tell apart from links that were refused.
type LinkBatch struct {
	Created []*core.Task
	Listed  []*core.Task
}

// listedRows gathers the ids of the rows a batch's links were folded into.
// Nil gathers nothing, for the entrances that do not answer with them.
type listedRows []string

func (l *listedRows) add(m dedupe.Match) {
	if l != nil && !slices.Contains(*l, m.Of.ID) {
		*l = append(*l, m.Of.ID)
	}
}

// AddLinksWithOptions is AddLinkBatch for the callers that only want the rows
// it created.
func (a *App) AddLinksWithOptions(urls []string, pkg string, origin core.Origin, opts LinkBatchOptions) ([]*core.Task, error) {
	b, err := a.AddLinkBatch(urls, pkg, origin, opts)
	return b.Created, err
}

// AddLinkBatch stages a batch like AddLinksFrom and then applies the form's
// own destination, passwords and, with Overrule, priority, unpacking and
// comment. The destination is validated before anything is staged, so a bad
// folder refuses the whole batch with an error the user sees.
func (a *App) AddLinkBatch(urls []string, pkg string, origin core.Origin, opts LinkBatchOptions) (LinkBatch, error) {
	dir := strings.TrimSpace(opts.Dir)
	if dir != "" {
		if err := settings.Validate(a.DataDir, "the folder for this batch", dir); err != nil {
			return LinkBatch{}, err
		}
	}
	if opts.FileName != "" {
		if !opts.File {
			return LinkBatch{}, errors.New("a file name comes with a file the browser was downloading")
		}
		// A control character, which only a script sends, becomes a space as
		// in a rename, and a name too long for a file is cut the way the
		// download library cuts it, so the row shows the name the file gets.
		name, err := checkName("file", strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, opts.FileName))
		if err != nil {
			return LinkBatch{}, err
		}
		opts.FileName = collide.SafeName(name)
	}
	switch {
	case opts.Format != "" && opts.Choice != nil:
		return LinkBatch{}, errors.New("a batch comes with a format or a choice of rows, not both")
	case opts.Format != "":
		c, ok := ytdlp.FormatChoice(opts.Format)
		if !ok {
			return LinkBatch{}, fmt.Errorf("unknown format %q", opts.Format)
		}
		opts.Choice = &c
	case opts.Choice != nil && !opts.Choice.Valid():
		return LinkBatch{}, errors.New("the choice names no row, or a row, quality or audio format yt-dlp has not got")
	}

	var listed listedRows
	opts.listed = &listed
	created := a.addLinksFrom(urls, pkg, origin, opts)
	if len(created) == 0 {
		return LinkBatch{Listed: a.rowsOf(listed)}, nil
	}
	// The form's values are for every row a yt-dlp link became, and so is the
	// confirm below.
	ids := a.withVariantFamilies(idsOf(created))
	a.keepBrowserHeaders(opts.Headers, ids)

	// Applied after staging, through the same route as the properties panel, so
	// these win over whatever the Packagizer pass in finishStaging decided.
	to := TaskOptions{}
	touched := false
	if dir != "" {
		to.Dir = &dir
		touched = true
	}
	password := strings.TrimSpace(opts.Password)
	if password != "" {
		to.Password = &password
		touched = true
	}
	if dlPassword := strings.TrimSpace(opts.DownloadPassword); dlPassword != "" {
		to.DownloadPassword = &dlPassword
		touched = true
	}
	if opts.Overrule {
		if opts.Priority != nil {
			to.Priority = opts.Priority
			touched = true
		}
		if comment := strings.TrimSpace(opts.Comment); comment != "" {
			to.Comment = &comment
			touched = true
		}
		if opts.AutoExtract != nil {
			v := *opts.AutoExtract
			to.AutoExtract = TriBool{Set: true, Value: &v}
			touched = true
		}
	}
	if touched {
		if err := a.SetTaskOptions(ids, to); err != nil {
			// The batch already exists and is on screen, so failing the request
			// here would suggest the links were lost.
			log.Printf("add-links: could not apply the batch's own options: %v", err)
		}
	}
	if password != "" {
		a.rememberPasswords([]string{password})
	}
	// Last, so the batch's own folder is on the tasks before a confirm can
	// start one. Nobody is there to answer a question the confirm policy asks.
	switch {
	case opts.Start:
		a.ConfirmTasks(ids, confirm.Config{}, confirm.TriggerCnL)
	case !opts.KeepCollected:
		a.autoConfirm(ids)
	}
	return LinkBatch{Created: a.detached(created), Listed: a.rowsOf(listed)}, nil
}

// rowsOf copies the rows among ids that are still in the list.
func (a *App) rowsOf(ids []string) []*core.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*core.Task
	for _, id := range ids {
		if t := a.tasks[id]; t != nil {
			c := *t
			out = append(out, &c)
		}
	}
	return out
}
