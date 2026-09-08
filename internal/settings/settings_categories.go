package settings

// Categories: named bundles of defaults, picked once instead of described five
// times.
//
// A category carries a destination folder, a priority, an unpacking switch, a
// speed limit and a collision rule under one word somebody chose. It is picked
// when links are thrown in, it is a facet the list can be filtered by, and a
// Packagizer rule can set it.
//
// It exists because "series here, films there, software over there" is one
// question about three drawers, and the only way to answer it before this table
// was a Packagizer rule per drawer keyed on something in the file name. That
// works right up until the name does not say - a release called "S01E03" is a
// series, a release called "1080p" is anything at all - and it buries the
// drawer's SETTINGS inside a condition about text, where they cannot be reused,
// cannot be filtered by, and cannot be offered as a menu at the moment the
// links are pasted.
//
// # The three questions this feature actually is
//
// 1. WHAT WINS WHEN THREE THINGS NAME A FOLDER. Most specific first: the task's
// own Dir, then the category, then DownloadDir. A Packagizer rule's downloadDir
// is already written onto the task by app.packagize, so a rule beats a category
// without any new precedence being invented here - it falls out of dirFor's
// existing first line, which takes a non-empty Task.Dir verbatim. That order is
// not arbitrary: a rule looked at THIS link (its name, its host, its size) and
// concluded something about it, while a category is a label somebody put on a
// whole batch. More evidence wins. A rule that sets both a category and a
// downloadDir therefore gets its downloadDir, and its category still decides
// the other four fields, because naming a folder is not an opinion about
// priority.
//
// The category replaces the BASE of the folder and not the whole answer:
// SubfolderByPackage still appends its per-package level on top. Anything else
// would mean that filing a batch under "Serien" silently switched off the
// per-package folders somebody had already asked for.
//
// 2. REFERENCE OR COPY. A reference, with exactly one copy at exactly one
// moment. Task.Category holds the ID and never the values, so renaming a
// category, editing its folder or retagging a task all reach every task that
// has not started yet. An ID naming nothing - a deleted category - resolves to
// the zero Category, which is "no opinion" in every field, so those downloads
// behave precisely like untagged ones; nothing breaks and nothing has to be
// migrated. The dead ID is KEPT on the task rather than cleared, because it is
// the record that somebody filed this download under "Serien", it still reads
// on the list, and a category re-created under the same ID picks its tasks back
// up. Cascading a delete into the tasks would destroy all of that with nothing
// to undo it from.
//
// The one thing that is copied is the folder, and only when the download
// starts. After the first byte the folder is not a preference any more, it is
// where the file is: extraction, checksum verification and the reclaim pass all
// build their path by joining dirFor's answer with the file name, so a category
// folder edited under a running download would send three separate readers
// looking in a folder that never held the file. See CategoryDir.
//
// A rule's reference is checked and a task's is not, and the difference is the
// point: a Packagizer rule naming a category that does not exist does nothing
// at all, silently, which is the failure this whole subsystem exists to refuse
// - so ValidateCategories turns it into a refused save. A task's reference is
// history, and history is allowed to name something that has been deleted.
//
// 3. ON THE TASK OR ON THE PACKAGE. On the task, and there was no second
// candidate once the tree was read. There IS no package entity in this build:
// Task.Package is a string, and a "package" is whatever set of tasks currently
// share it (app_script.go counts them by grouping). A field on the package
// would need a table, a lifecycle and an owner that nothing else has, purely to
// hold a value that has to be resolved per task anyway - every field a category
// presets (Dir, Priority, AutoExtract, Chunks' neighbours) is already a per-task
// field. And it would make the interesting case INEXPRESSIBLE: a package whose
// links belong in different drawers is normal (the sample beside the film, the
// subtitle beside the episode), and with the category on the package, dragging
// one link into a package would silently retag it. On the task, that package
// simply reads as mixed.

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/mediahook"
	"github.com/junkerderprovinz/knightloader/internal/pathvars"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// maxCategoryID and maxCategoryName bound the two strings a person types. The
// ID travels in Task.Category, into a filter query and into the JSON of every
// task in a list of ten thousand, so it is kept short on purpose; the name is
// only ever read.
const (
	maxCategoryID   = 64
	maxCategoryName = 120
)

// MaxCategories caps the table. It is a menu somebody picks from, and a menu of
// four hundred entries is not a menu - it is the condition list this feature
// exists to replace, spelled differently.
const MaxCategories = 64

// Category is one named drawer.
//
// Every field except ID and Name is an override of something that already has
// an answer a level up, and every zero value means "no opinion, use the level
// above" - the same convention HostRule and Task.Chunks already carry. That is
// what makes a category somebody created and left half-filled harmless: it
// changes exactly the fields they filled in.
type Category struct {
	// ID is the stable key Task.Category refers to and the only field that may
	// not change: renaming a category has to keep the tasks that are in it, and
	// the only way to do that is for the name not to be the key.
	//
	// A client may leave it empty when it creates one, in which case
	// sanitizeCategories derives it from Name once and stores it - so
	// `{"name":"Serien"}` is a complete category through the API. It is never
	// re-derived afterwards, because a client that sends a renamed category with
	// no ID is not renaming anything, it is replacing the list with one that has
	// a different member.
	ID string `json:"id"`
	// Name is what a picker shows. It may change freely.
	Name string `json:"name,omitempty"`
	// Dir is the destination folder for a task in this category, and it may be a
	// pathvars template exactly as DownloadDir may. Empty means the category has
	// no opinion and the global folder applies. See CategoryDir for when it is
	// read and when it deliberately is not.
	Dir string `json:"dir,omitempty"`
	// Priority is the queue position every task filed here starts at. Nil rather
	// than 0 because 0 is a real priority - the middle one - and a category that
	// merely says nothing about priority must not push tasks a rule had lifted
	// back down to the middle.
	Priority *int `json:"priority,omitempty"`
	// Extract is this drawer's own unpacking switch. Nil is "no opinion"; false
	// is a category that deliberately does NOT unpack (a music drawer where the
	// archive is the delivery) and has to survive a global that says otherwise,
	// which a plain bool cannot express.
	Extract *bool `json:"extract,omitempty"`
	// SpeedLimit is what a task in this drawer is allowed to pull, in bytes per
	// second, 0 meaning "no opinion".
	//
	// NOTHING ENFORCES THIS YET, and saying so here is better than a settings
	// page that quietly lies. This build has ONE limiter (internal/throttle) for
	// the whole app, shared out between three resolver families by app.applyBudget
	// - there is no per-task allowance for a category's number to be written
	// into, and inventing one is a bandwidth-scheduler-sized job rather than a
	// line of wiring. The field is here because the value is what a person
	// configures and it round-trips correctly today, and because the alternative
	// (leave it out, add it later) means a settings key that changes shape after
	// people have files on disk. SpeedLimitFor resolves it, so whoever builds
	// the per-task limiter has one call to make and no precedence to re-derive.
	SpeedLimit int64 `json:"speedLimit,omitempty"`
	// Collision is what happens when this drawer's destination file already
	// exists, as collide.Policy's own string form. Empty takes the global
	// CollisionPolicy.
	//
	// collide.Ask is refused here, not merely left out of a menu: it means "park
	// the task until a human answers", there is no status for that and no way to
	// answer, so a task set to it sits in the queue forever with nothing saying
	// why. The API already withholds it from the global menu for that reason
	// (see options() in routes_settings.go); a category is the second door into
	// the same field and would otherwise be the way round the first.
	Collision string `json:"collision,omitempty"`
	// Notify is the stored address called once a package filed in this drawer
	// has finished AND its files have been moved into place - a media library
	// told to rescan, in practice. Empty means this drawer calls nothing, which
	// is what every drawer does until somebody changes it.
	//
	// It is the id of a mediahook.Hook (see settings_mediahooks.go), so it is a
	// REFERENCE and not a copy, exactly like every other field on this struct: an
	// address edited on the Downloads page reaches every drawer pointing at it
	// with nothing to migrate. Unlike the other fields, a dangling one is REFUSED
	// rather than read as "no opinion" - see ValidateMediaHooks for why this
	// reference is treated the way a Packagizer rule's category reference is and
	// not the way a task's own dead category id is.
	//
	// THE HOOK HANGS HERE AND NOT ON A PACKAGIZER RULE OR A FOLDER PREFIX. A
	// drawer is the one anchor in this build that already has a stable identity,
	// a picker and a settings home; a rule's identity is its name, which falls
	// back to its POSITION when it has none (rules.ruleName), so a hook keyed on
	// one would re-aim itself the first time somebody reordered their rules. A
	// resolved folder prefix is not an identity at all - it is the output of a
	// template.
	//
	// A package whose links sit in two drawers calls both, once each. That is not
	// an edge case to be tidied away: mixed packages are normal here and this
	// file argues for them at length above (the sample beside the film, the
	// subtitle beside the episode).
	Notify string `json:"notify,omitempty"`
}

// CategoryFor is the category an ID names, or the zero Category when it names
// nothing - which is every ID on an install with no categories, and every ID
// left over on a task whose category was deleted.
//
// The zero Category is a complete answer and not an error: every field on it is
// the "no opinion" value, so a task pointing at a category that is gone behaves
// exactly like a task that was never filed anywhere. That is what lets a
// deletion be a deletion rather than a migration.
func (s Settings) CategoryFor(id string) Category {
	id = CategoryID(id)
	if id == "" {
		return Category{}
	}
	for _, c := range s.Categories {
		if CategoryID(c.ID) == id {
			return c
		}
	}
	return Category{}
}

// CategoryDir is the folder a category asks for, expanded against one task's
// own values, or "" when the category has nothing to say - including for an ID
// that names nothing.
//
// IT IS ONLY CORRECT FOR A TASK THAT HAS NOT STARTED. The category is a
// reference, so this is a late-bound read: edit the folder and every task that
// has not begun follows it. A task whose bytes are already on disk must not
// follow it, because its folder has stopped being a preference and become a
// fact - and dirFor's answer is what extraction, checksum verification and the
// reclaim pass all join the file name onto. The pin is Task.Dir, which dirFor
// already takes verbatim ahead of everything else: whoever starts a download
// writes the resolved folder there, and this function is never consulted for
// that task again.
//
// A result that is not absolute after expansion is dropped rather than
// returned, exactly as dirFor drops a non-absolute expansion of DownloadDir: a
// relative download folder is resolved against whatever the process's working
// directory happens to be, which is not a place anybody can reason about.
func (s Settings) CategoryDir(id string, v pathvars.Vars) string {
	dir := strings.TrimSpace(s.CategoryFor(id).Dir)
	if dir == "" {
		return ""
	}
	if !pathvars.HasVars(dir) {
		return dir
	}
	expanded := pathvars.Expand(dir, v)
	if !filepath.IsAbs(expanded) {
		return ""
	}
	return expanded
}

// CollisionFor is the collision policy in force for a task in this category:
// the category's own, or the instance's when it has none. It answers in
// collide.Policy's string form so the caller parses it exactly once, the way
// app_dispatch.go already parses CollisionPolicy.
func (s Settings) CollisionFor(id string) string {
	if p := strings.TrimSpace(s.CategoryFor(id).Collision); p != "" {
		return p
	}
	return s.CollisionPolicy
}

// ExtractFor is whether a task in this category unpacks, when the task itself
// has no switch of its own. The task's own AutoExtract still wins over this -
// see app.extractWanted - because that is a rule or a person having spoken
// about one download, and this is a default for a drawer.
func (s Settings) ExtractFor(id string) bool {
	if e := s.CategoryFor(id).Extract; e != nil {
		return *e
	}
	return s.Extract
}

// SpeedLimitFor is the allowance for a task in this category, in bytes per
// second, falling back to the instance-wide limit. See Category.SpeedLimit for
// what does and does not honour it today.
func (s Settings) SpeedLimitFor(id string) int64 {
	if l := s.CategoryFor(id).SpeedLimit; l > 0 {
		return l
	}
	return s.SpeedLimit
}

// PriorityFor is the starting priority a category asks for. The second result
// is false when it asks for none, which is not the same as asking for 0: 0 is
// the middle priority and a real answer.
func (s Settings) PriorityFor(id string) (int, bool) {
	if p := s.CategoryFor(id).Priority; p != nil {
		return *p, true
	}
	return 0, false
}

// CategoryID folds the spellings of one ID into one, and is the only place that
// decides what an ID may contain.
//
// Case and stray whitespace go, letters and digits stay (including non-ASCII
// ones: "hörspiele" is a perfectly good key and mangling it would make the
// derived ID unrecognisable next to the name it came from), and every other
// character becomes a single dash. Runs collapse and the edges are trimmed, so
// "  Serien & Filme  " and "serien-filme" are the same key rather than two
// drawers with one name.
//
// It is called on both sides of every comparison rather than only at save time,
// because Task.Category is written by clients, by imported rule sets and by a
// hand-edited settings file, and a lookup that only matched the exact stored
// spelling would drop a task out of its own category over a capital letter.
func CategoryID(raw string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			dash = false
			continue
		}
		// One dash for any run of separators, and none yet at the start: an ID
		// beginning or ending in a dash is the same drawer as one that does not,
		// and two spellings of one drawer is exactly what a key must not have.
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > maxCategoryID {
		// Cut on a rune boundary, then trim again: a cut that lands mid-dash
		// would put the trailing dash back that the rule above just removed.
		out = strings.TrimRight(strings.ToValidUTF8(out[:maxCategoryID], ""), "-")
	}
	return out
}

// sanitizeCategories bounds the table and drops the rows that could never be
// referred to.
//
// The slice is rebuilt rather than edited in place, matching sanitizeHostRules:
// what the caller handed in is still holding the same backing array, and a
// settings document that keeps changing underneath whoever submitted it is a
// bug people find months later.
func sanitizeCategories(n Settings) Settings {
	if len(n.Categories) == 0 {
		return n
	}
	out := make([]Category, 0, len(n.Categories))
	seen := make(map[string]bool, len(n.Categories))
	for _, c := range n.Categories {
		c.Name = trimTo(c.Name, maxCategoryName)
		id := CategoryID(c.ID)
		if id == "" {
			// Derived from the name exactly once, on a category that has never
			// had an ID - see Category.ID. A row with neither is dropped: it can
			// never be picked, never be referred to and never be explained, which
			// is the same reason sanitizeHostRules drops a blank host pattern.
			id = CategoryID(c.Name)
		}
		if id == "" || seen[id] {
			// A duplicate is refused loudly by ValidateCategories long before it
			// gets here, so this only ever fires on a hand-edited settings.json.
			// The FIRST one is kept because it is the one a picker built from this
			// slice shows first, which makes the surviving row the one the person
			// looking at the page would have expected.
			continue
		}
		seen[id] = true
		c.ID = id
		c.Dir = strings.TrimSpace(c.Dir)
		if c.Dir != "" && !filepath.IsAbs(c.Dir) {
			// Same rule sanitizePaths applies to DownloadDir, and for the same
			// reason: nobody can say where a relative folder actually is. A
			// template counts as absolute when it starts with a real path level,
			// which is how "/media/serien/<jd:packagename>" survives.
			c.Dir = ""
		}
		c.Priority = clampCategoryPriority(c.Priority)
		if c.SpeedLimit < 0 {
			c.SpeedLimit = 0
		}
		c.Collision = normalizeCategoryCollision(c.Collision)
		// Folded, never cleared. HookID answers "" for a spelling no address
		// could ever be stored under, and that empty is the honest reading of it
		// - but an id that folds to something usable is KEPT even when no address
		// is stored under it today, because clearing it here would silently undo
		// a drawer's setting the moment somebody hand-edited their hooks list.
		// ValidateMediaHooks is what refuses that state at the door.
		c.Notify = mediahook.HookID(c.Notify)
		out = append(out, c)
		if len(out) == MaxCategories {
			break
		}
	}
	n.Categories = out
	return n
}

// clampCategoryPriority holds a category to the same range a Packagizer rule is
// held to, so a category cannot hand a task a priority the interface has no
// control able to undo. Nil stays nil: "no opinion" is not a number to clamp.
func clampCategoryPriority(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	if v < rules.PriorityMin {
		v = rules.PriorityMin
	}
	if v > rules.PriorityMax {
		v = rules.PriorityMax
	}
	// A fresh pointer, never the caller's: the value came off a struct the
	// caller still holds, and writing through it would edit their copy.
	return &v
}

// normalizeCategoryCollision folds a stored policy onto one this build honours,
// and answers "" for everything it does not recognise.
//
// It deliberately does NOT go through collide.ParsePolicy, which maps anything
// unknown onto the default (rename). Here "unknown" and "unset" have to stay
// distinguishable from "rename": a category with no opinion must fall through
// to the instance's own CollisionPolicy, and running the empty string through
// ParsePolicy would give every category an explicit rename instead - silently
// overruling an instance configured to skip.
func normalizeCategoryCollision(raw string) string {
	p := collide.Policy(strings.ToLower(strings.TrimSpace(raw)))
	switch p {
	case collide.Rename, collide.Skip, collide.Overwrite:
		return string(p)
	}
	// collide.Ask lands here with the typos. See Category.Collision.
	return ""
}

// ValidateCategories reports the first thing wrong with the table, in words
// meant for whoever is looking at the form.
//
// It is refusal rather than sanitising, for the reason validateRows exists at
// all: a row that vanishes on save is a row the user goes on believing in.
// sanitize would drop a duplicate ID silently and the person would be left with
// two drawers on screen, one save, and one drawer.
//
// It also checks the references INTO the table from the Packagizer, and that is
// the asymmetry worth reading twice: a rule pointing at a category that does
// not exist is an action that does nothing, silently, on every link it matches.
// A task pointing at one is merely a download that was filed in a drawer
// somebody has since thrown away, which is a fact about the past and stays
// exactly as it is. Only the link filter is exempt, because a rejected link
// never gets a category and the filter flavour ignores the field.
func (s Settings) ValidateCategories() error {
	if len(s.Categories) > MaxCategories {
		return fmt.Errorf("there are %d categories; the limit is %d", len(s.Categories), MaxCategories)
	}
	known := make(map[string]bool, len(s.Categories))
	for i, c := range s.Categories {
		id := CategoryID(c.ID)
		if id == "" {
			id = CategoryID(c.Name)
		}
		if id == "" {
			return fmt.Errorf("category %d has neither an id nor a name, so nothing could ever be filed in it", i+1)
		}
		if known[id] {
			return fmt.Errorf("category %d repeats the id %q; two drawers with one key cannot be told apart", i+1, id)
		}
		known[id] = true
		if p := c.Priority; p != nil && (*p < rules.PriorityMin || *p > rules.PriorityMax) {
			return fmt.Errorf("category %q: priority %d is outside %d..%d", id, *p, rules.PriorityMin, rules.PriorityMax)
		}
		if c.SpeedLimit < 0 {
			return fmt.Errorf("category %q: a speed limit of %d bytes per second is not a limit", id, c.SpeedLimit)
		}
		if raw := strings.TrimSpace(c.Collision); raw != "" && normalizeCategoryCollision(raw) == "" {
			return fmt.Errorf("category %q: %q is not a collision rule this build can apply; use rename, skip or overwrite", id, raw)
		}
	}
	for i, r := range s.Packagizer.Rules {
		want := CategoryID(r.Action.Category)
		if want == "" || known[want] {
			continue
		}
		return fmt.Errorf("packagizer rule %d (%s) files links in the category %q, which does not exist",
			i+1, ruleLabel(r, i), want)
	}
	return nil
}

// ruleLabel names a rule the way rules.Compile's own problems do, so one rule
// is called the same thing by both messages. An unnamed rule still has to be
// findable and its position is the only handle on it.
func ruleLabel(r rules.Rule, index int) string {
	if n := strings.TrimSpace(r.Name); n != "" {
		return n
	}
	return fmt.Sprintf("rule %d", index+1)
}

// trimTo trims the whitespace and then the length, on a rune boundary so a
// multi-byte name cannot be cut into invalid UTF-8 and land in the JSON as a
// replacement character.
func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "")
}
