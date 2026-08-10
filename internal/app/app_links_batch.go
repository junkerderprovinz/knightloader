package app

// The add-links form's own batch options (build-plan.md §8A): a destination
// for this batch alone, a priority and an unpacking switch that apply to
// every task the batch creates, a comment, and the two passwords a hoster and
// an archive ask for - two different secrets asked by two different parties,
// see core.Task.Password vs DownloadPassword.

import (
	"log"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// LinkBatchOptions are the values the add-links form attaches to a whole
// batch rather than to one task. They are distinct from TaskOptions, which
// edits tasks that already exist: this is what a batch is STAGED with, and
// AddLinksWithOptions is the only thing that builds a Task from one.
type LinkBatchOptions struct {
	// Dir overrides the destination folder for every task this batch creates.
	// Empty leaves the ordinary Packagizer-then-settings folder in place.
	//
	// It always wins over whatever the Packagizer decides for the same reason
	// any task's own Dir does: dirFor takes a non-empty Task.Dir verbatim, so
	// whichever write lands last is the one dirFor ever sees. AddLinksWithOptions
	// applies Dir once staging (and the Packagizer inside it) has finished,
	// which is what makes that write the last one - never conditional on
	// Overrule below, because a hand-picked destination is not a property a
	// rule and the form are contending over, it is the one field a person
	// pointed at directly.
	Dir string
	// Password is the archive password tried first when a task from this batch
	// is unpacked. NOT DownloadPassword below - two different secrets asked by
	// two different parties (see core.Task.Password's own comment). No
	// Packagizer action can set it either, so there is nothing for Overrule to
	// invert here: the form's answer is simply the only one there ever is.
	Password string
	// DownloadPassword is what a hoster's own page asks for before it hands
	// over the file. Same reasoning as Password above - a rule cannot set it,
	// so it is applied unconditionally.
	DownloadPassword string
	// Comment is the note attached to every task in the batch.
	Comment string
	// Priority and AutoExtract are nil when the form has no opinion, exactly
	// like a Packagizer rule's own Action - the zero value would otherwise be
	// indistinguishable from "priority 0" and "switch extraction off".
	Priority    *int
	AutoExtract *bool
	// Overrule makes Priority, AutoExtract and Comment win over a Packagizer
	// rule that would otherwise set the same property on the same task,
	// inverting the default.
	//
	// THE DEFAULT, with Overrule left off: a matching rule wins. addLinksFrom
	// seeds these three fields onto each task through stage's intake BEFORE
	// finishStaging runs the Packagizer, and packagize() already overwrites a
	// field unconditionally whenever a rule has an opinion about it - the same
	// code path that lets a rule answer "what priority" for a plain paste with
	// no form involved at all. Seeding first and letting the rule write over it
	// is what makes that the default here too, with no change to packagize
	// itself: build-plan.md §4 conflict 5 and §8's Wave 8 amendment ask for
	// exactly this, decided here rather than left as a guess.
	//
	// With Overrule on, AddLinksWithOptions applies the three fields a SECOND
	// time, once staging has finished - the only way to give the form the
	// LAST word instead of the first, now that the rule has already had its
	// say.
	Overrule bool
}

// AddLinksWithOptions stages a batch with the add-links form's own answers
// for destination, priority, unpacking, comment and the two passwords.
// Everything else about staging - the filter, the crawl, naming, the
// Packagizer - runs exactly as it does for a plain AddLinks; this only
// layers what the form itself decided on top, and an empty LinkBatchOptions{}
// behaves identically to AddLinksFrom.
//
// The destination is validated BEFORE anything is staged, and the whole batch
// is refused together when it cannot be used, rather than staging every link
// to the wrong folder and reporting the mistake only in the server log the
// way a dropped watch-folder job does (see applyWatchJobOptions, app_watch.go): a path a person just
// typed into a form deserves an answer they can see on the spot, and a batch
// half-applied under an error nobody read is worse than one refused outright.
func (a *App) AddLinksWithOptions(urls []string, pkg string, origin core.Origin, opts LinkBatchOptions) ([]*core.Task, error) {
	dir := strings.TrimSpace(opts.Dir)
	if dir != "" {
		if err := settings.Validate(dir); err != nil {
			return nil, err
		}
	}

	created := a.addLinksFrom(urls, pkg, origin, opts)
	if len(created) == 0 {
		return a.detached(created), nil
	}
	ids := make([]string, 0, len(created))
	for _, t := range created {
		ids = append(ids, t.ID)
	}

	// Everything from here on is applied AFTER staging, through the same
	// per-task route the properties panel uses - which is what makes each of
	// these win over whatever finishStaging's Packagizer pass just decided.
	// AddLinksWithPasswords already relies on that same ordering for the
	// archive password a Click'n'Load submission supplies.
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
	// Priority, AutoExtract and Comment were already seeded before the
	// Packagizer ran (see intake and stage), which is what makes a matching
	// rule win BY DEFAULT. Overrule is the form asking for the last word
	// instead of the first, and the only way to give it that is to set these
	// again now, after the rule has already had its say.
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
			// The batch already exists and is already on screen; refusing the
			// whole request at this point would tell the user their links had
			// vanished when they had not. Logged rather than swallowed, the
			// same choice stageWatchJob makes for the same reason - the
			// destination was already validated above, so what can still fail
			// here is a rename-shaped edge case the panel itself would surface
			// the same way.
			log.Printf("add-links: could not apply the batch's own options: %v", err)
		}
	}
	// A password the form supplied is worth keeping for the NEXT archive from
	// a source nobody names in advance, exactly like a Click'n'Load
	// submission's passwords already are - see rememberPasswords.
	if password != "" {
		a.rememberPasswords([]string{password})
	}
	return a.detached(created), nil
}
