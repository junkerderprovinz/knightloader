package app

import (
	"log"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
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
	// Category is the category every task in the batch is filed under. Like
	// Dir it is applied after staging, so it wins over a Packagizer rule. The
	// tasks are staged in it as well, so they start at its priority.
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
}

// AddLinksWithOptions stages a batch like AddLinksFrom and then applies the
// form's own destination, passwords and, with Overrule, priority, unpacking
// and comment. The destination is validated before anything is staged, so a
// bad folder refuses the whole batch with an error the user sees.
func (a *App) AddLinksWithOptions(urls []string, pkg string, origin core.Origin, opts LinkBatchOptions) ([]*core.Task, error) {
	dir := strings.TrimSpace(opts.Dir)
	if dir != "" {
		if err := settings.Validate("the folder for this batch", dir); err != nil {
			return nil, err
		}
	}

	created := a.addLinksFrom(urls, pkg, origin, opts)
	if len(created) == 0 {
		return a.detached(created), nil
	}
	// The form's values are for every row a yt-dlp link became, and so is the
	// confirm below.
	ids := a.withVariantFamilies(idsOf(created))

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
	if category := strings.TrimSpace(opts.Category); category != "" {
		a.SetCategory(ids, category)
	}
	// Last, so the batch's own folder and category are on the tasks before a
	// confirm can start one.
	if !opts.KeepCollected {
		a.autoConfirm(ids)
	}
	return a.detached(created), nil
}
