package settings

// Categories: named bundles of defaults, picked once instead of described five
// times.
//
// A category carries a destination folder, a priority, an unpacking switch, a
// speed limit and a collision rule under one word somebody chose. It is picked
// when links are thrown in, it is a facet the list can be filtered by, and a
// Packagizer rule can set it. The alternative was a Packagizer rule per drawer
// keyed on something in the file name, which works only while the name says:
// "S01E03" is a series, "1080p" is anything at all.
//
// Folder precedence, most specific first: the task's own Dir, then the
// category, then DownloadDir. A Packagizer rule's downloadDir is already
// written onto the task by app.packagize, so a rule beats a category out of
// dirFor's first line, which takes a non-empty Task.Dir verbatim. The category
// replaces the base of the folder and not the whole answer, so
// SubfolderByPackage keeps appending its per-package level.
//
// A reference, not a copy. Task.Category holds the id and never the values, so
// renaming a category or editing its folder reaches every task that has not
// started, and an id naming nothing resolves to the zero Category, which is "no
// opinion" in every field. The dead id stays on the task, because a category
// re-created under the same id picks its tasks back up. The folder alone is
// copied, when the download starts, because after the first byte extraction,
// checksum verification and the reclaim pass all join the file name onto
// dirFor's answer. See CategoryDir.
//
// The field hangs off the task, not the package: Task.Package is a string, and
// a package is whatever set of tasks share it. On the package it would also
// make the normal case inexpressible, since a package whose links belong in
// different drawers is ordinary (the sample beside the film).

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
// four hundred entries is the condition list this feature replaces, spelled
// differently.
const MaxCategories = 64

// Category is one named drawer.
//
// Every field except ID and Name overrides something that already has an answer
// a level up, and every zero value means "no opinion, use the level above", the
// convention HostRule and Task.Chunks carry. A category somebody created and
// left half-filled therefore changes exactly the fields they filled in.
type Category struct {
	// ID is the stable key Task.Category refers to and the one field that may
	// not change: renaming a category has to keep the tasks in it, which means
	// the name cannot be the key.
	//
	// A client may leave it empty when it creates one, and sanitizeCategories
	// then derives it from Name once, so `{"name":"Serien"}` is a complete
	// category through the API. It is never re-derived afterwards: a client that
	// sends a renamed category with no id is not renaming anything, it is
	// replacing the list with one that has a different member.
	ID string `json:"id"`
	// Name is what a picker shows. It may change freely.
	Name string `json:"name,omitempty"`
	// Dir is the destination folder for a task in this category, and it may be a
	// pathvars template as DownloadDir may. Empty means the category has no
	// opinion and the global folder applies. See CategoryDir for when it is read
	// and when it is not.
	Dir string `json:"dir,omitempty"`
	// Priority is the queue position every task filed here starts at. Nil rather
	// than 0 because 0 is a real priority, the middle one, and a category that
	// says nothing about priority must not push tasks a rule had lifted back
	// down to the middle.
	Priority *int `json:"priority,omitempty"`
	// Extract is this drawer's own unpacking switch. Nil is "no opinion"; false
	// is a category that does not unpack, a music drawer where the archive is
	// the delivery, and it has to survive a global that says otherwise, which a
	// plain bool cannot express.
	Extract *bool `json:"extract,omitempty"`
	// SpeedLimit is what a task in this drawer may pull, in bytes per second, 0
	// meaning "no opinion".
	//
	// Nothing enforces it yet. The build has one limiter (internal/throttle) for
	// the whole app, shared out between three resolver families by
	// app.applyBudget, and there is no per-task allowance for a category's
	// number to be written into. The field is here because leaving it out would
	// mean a settings key that changes shape after people have files on disk,
	// and SpeedLimitFor already resolves it, so a per-task limiter has one call
	// to make and no precedence to re-derive.
	SpeedLimit int64 `json:"speedLimit,omitempty"`
	// Collision is what happens when this drawer's destination file already
	// exists, as collide.Policy's string form. Empty takes the global
	// CollisionPolicy.
	//
	// collide.Ask is refused here rather than only left out of a menu: it parks
	// the task until a human answers, and with no status for that and no way to
	// answer, a task set to it sits in the queue forever with nothing saying
	// why. The API withholds it from the global menu for the same reason (see
	// options() in routes_settings.go), and a category is the second door into
	// the same field.
	Collision string `json:"collision,omitempty"`
	// Notify is the stored address called once a package filed in this drawer
	// has finished and its files have been moved into place, a media library
	// told to rescan in practice. Empty means this drawer calls nothing.
	//
	// It is the id of a mediahook.Hook (settings_mediahooks.go), so a reference
	// rather than a copy, like every other field here: an address edited on the
	// Automation page reaches every drawer pointing at it. Unlike the other
	// fields, a dangling one is refused rather than read as "no opinion", see
	// ValidateMediaHooks.
	//
	// The hook hangs on the drawer and not on a Packagizer rule or a folder
	// prefix, because a drawer is the one anchor here with a stable identity, a
	// picker and a settings home. A rule's identity falls back to its position
	// when it has no name (rules.ruleName), so a hook keyed on one would re-aim
	// itself the first time somebody reordered their rules, and a resolved
	// folder prefix is the output of a template rather than an identity.
	//
	// A package whose links sit in two drawers calls both, once each.
	Notify string `json:"notify,omitempty"`
}

// CategoryFor is the category an id names, or the zero Category when it names
// nothing: every id on an install with no categories, and every id left over on
// a task whose category was deleted.
//
// The zero Category is a complete answer rather than an error, since every
// field on it is the "no opinion" value, so a task pointing at a category that
// is gone behaves like a task that was never filed anywhere. That is what lets
// a deletion be a deletion rather than a migration.
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
// own values, or "" when the category has nothing to say, including for an id
// that names nothing.
//
// It is only correct for a task that has not started. The category is a
// reference, so this is a late-bound read: edit the folder and every task that
// has not begun follows it. A task whose bytes are on disk must not, because
// its folder has stopped being a preference, and dirFor's answer is what
// extraction, checksum verification and the reclaim pass join the file name
// onto. The pin is Task.Dir, which dirFor takes verbatim ahead of everything
// else: whoever starts a download writes the resolved folder there, and this
// function is never consulted for that task again.
//
// A result that is not absolute after expansion is dropped, as dirFor drops a
// non-absolute expansion of DownloadDir: a relative download folder resolves
// against whatever the process's working directory happens to be.
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
// has no switch of its own. Task.AutoExtract still wins (see
// app.extractWanted): that is a rule or a person speaking about one download,
// this is a default for a drawer.
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

// CategoryID folds the spellings of one id into one, and is the only place that
// decides what an id may contain.
//
// Case and stray whitespace go, letters and digits stay including non-ASCII
// ones ("hörspiele" is a good key, and mangling it would make the derived id
// unrecognisable beside the name it came from), and every other character
// becomes a single dash. Runs collapse and the edges are trimmed, so
// "  Serien & Filme  " and "serien-filme" are one key rather than two drawers
// with one name.
//
// It runs on both sides of every comparison rather than only at save time,
// because Task.Category is written by clients, by imported rule sets and by a
// hand-edited settings file, and a lookup matching only the stored spelling
// would drop a task out of its own category over a capital letter.
func CategoryID(raw string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			dash = false
			continue
		}
		// One dash for any run of separators, and none at the start: an id
		// beginning or ending in a dash names the same drawer as one that does
		// not, and a key cannot have two spellings.
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

// sanitizeCategories bounds the table and drops the rows nothing could refer
// to.
//
// The slice is rebuilt rather than edited in place, matching sanitizeHostRules:
// what the caller handed in still holds the same backing array, and a settings
// document that changes underneath whoever submitted it is a bug people find
// months later.
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
			// Derived from the name once, on a category that has never had an
			// id, see Category.ID. A row with neither is dropped: it can never
			// be picked and never be referred to, the reason sanitizeHostRules
			// drops a blank host pattern.
			id = CategoryID(c.Name)
		}
		if id == "" || seen[id] {
			// ValidateCategories refuses a duplicate long before this, so it
			// only fires on a hand-edited settings.json. The first one is kept,
			// because it is the one a picker built from this slice shows first.
			continue
		}
		seen[id] = true
		c.ID = id
		c.Dir = strings.TrimSpace(c.Dir)
		if c.Dir != "" && !filepath.IsAbs(c.Dir) {
			// The rule sanitizePaths applies to DownloadDir: nobody can say
			// where a relative folder is. A template counts as absolute when it
			// starts with a real path level, which is how
			// "/media/serien/<jd:packagename>" survives.
			c.Dir = ""
		}
		c.Priority = clampCategoryPriority(c.Priority)
		if c.SpeedLimit < 0 {
			c.SpeedLimit = 0
		}
		c.Collision = normalizeCategoryCollision(c.Collision)
		// Folded, never cleared. An id that folds to something usable is kept
		// even when no address is stored under it, because clearing it would
		// undo a drawer's setting the moment somebody hand-edited their hooks
		// list. ValidateMediaHooks refuses that state at the door.
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
// It does not go through collide.ParsePolicy, which maps anything unknown onto
// rename. Here unknown and unset have to stay distinguishable from rename: a
// category with no opinion falls through to the instance's CollisionPolicy, and
// running the empty string through ParsePolicy would give every category an
// explicit rename, overruling an instance configured to skip.
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
// Refusal rather than sanitising, for the reason validateRows exists: a row
// that vanishes on save is a row the user goes on believing in. sanitize drops
// a duplicate id silently, leaving two drawers on screen, one save and one
// drawer.
//
// It also checks the references into the table from the Packagizer. A rule
// pointing at a category that does not exist is an action that does nothing on
// every link it matches; a task pointing at one is a download filed in a drawer
// somebody has since thrown away, which is a fact about the past. Only the link
// filter is exempt, because a rejected link never gets a category and the
// filter flavour ignores the field.
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

// ruleLabel names a rule the way rules.Compile's problems do, so both messages
// call one rule the same thing. An unnamed rule still has to be findable, and
// its position is the only handle on it.
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
