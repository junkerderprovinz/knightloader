package settings

// The stored addresses a category drawer calls once a package filed in it has
// finished and its files are in place. The rows themselves live in
// internal/mediahook, which owns what one looks like and what may be in it; this
// file is only the settings document's half: where the list hangs, what a save
// refuses, and what the sanitiser does with a hand-edited file.
//
// The address, the method, the header name and the wait are configuration: they
// belong in settings.json, in a backup, and in the diagnostics bundle somebody
// helping with a bug report reads. The header value is a credential and is
// sealed in accounts.Store instead, see internal/mediahook's package comment
// for the places a string field here would have leaked it.
//
// The drawer points at the hook rather than the other way round, because a
// drawer already has an identity, a picker, a table and a settings home
// (settings_categories.go), and its id is stable by construction. Both other
// anchors fail on identity: a Packagizer rule's identity is its name, falling
// back to its position when it has none (rules.ruleName), and a resolved folder
// prefix changes when a template variable expands differently.

import (
	"fmt"
	"sort"

	"github.com/junkerderprovinz/knightloader/internal/mediahook"
)

// sanitizeMediaHooks bounds the table and drops what could never be called or
// pointed at. The work is mediahook.Sanitize's; this is the one line sanitize()
// calls, kept here so that the settings document's list of hooks is edited in
// one place and internal/mediahook never has to know what a Settings is.
func sanitizeMediaHooks(n Settings) Settings {
	n.MediaHooks = mediahook.Sanitize(n.MediaHooks)
	return n
}

// ValidateMediaHooks reports the first thing wrong with the table, in words
// meant for whoever is looking at the form.
//
// Refusal rather than sanitising, for the reason validateRows exists: a row
// that vanishes on save is a row the user goes on believing in, here while
// their library stops being scanned.
//
// It also checks the references into the table from the drawers, the asymmetry
// ValidateCategories draws for the Packagizer: a drawer pointing at an address
// that is not there calls nothing on every package filed in it. Nothing but a
// drawer ever names a hook, so there is no equivalent of a task's dead category
// id to leave alone.
func (s Settings) ValidateMediaHooks() error {
	if len(s.MediaHooks) > mediahook.MaxHooks {
		return fmt.Errorf("there are %d stored addresses; the limit is %d", len(s.MediaHooks), mediahook.MaxHooks)
	}
	known := make(map[string]bool, len(s.MediaHooks))
	for i, h := range s.MediaHooks {
		id := mediahook.HookID(h.ID)
		if id == "" {
			return fmt.Errorf("address %d has no usable name, so no drawer could ever point at it", i+1)
		}
		if known[id] {
			return fmt.Errorf("address %d repeats the name %q; two addresses with one key cannot be told apart", i+1, id)
		}
		known[id] = true
		if err := h.Validate(); err != nil {
			return err
		}
	}
	for _, c := range s.Categories {
		want := mediahook.HookID(c.Notify)
		if want == "" || known[want] {
			continue
		}
		return fmt.Errorf("the category %q calls the address %q after a package, and there is no address stored under that name",
			categoryKey(c), want)
	}
	return nil
}

// categoryKey names a drawer the way a message about it has to: its own id when
// it has one, otherwise the id its name would produce, which is what the save
// would have stored. It names the key rather than the label, because the key is
// what a dangling reference is about.
func categoryKey(c Category) string {
	if id := CategoryID(c.ID); id != "" {
		return id
	}
	return CategoryID(c.Name)
}

// NotifyHookFor is the address a drawer calls, or "" when it calls nothing,
// which is every drawer until somebody changes it and every id that names no
// category.
func (s Settings) NotifyHookFor(categoryID string) string {
	return mediahook.HookID(s.CategoryFor(categoryID).Notify)
}

// MediaHookUsers is every drawer pointing at one address, by category id,
// sorted. The listing counts the drawers so an address nothing calls reads as
// stored and idle, and a refused delete names them.
func (s Settings) MediaHookUsers(hookID string) []string {
	want := mediahook.HookID(hookID)
	if want == "" {
		return nil
	}
	var out []string
	for _, c := range s.Categories {
		if mediahook.HookID(c.Notify) != want {
			continue
		}
		if key := categoryKey(c); key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// MediaHookFor is one stored address by id, and false when nothing is stored
// under that name. The lookup every route and the runner make, in one place, so
// that an id naming nothing is answered the same way everywhere.
func (s Settings) MediaHookFor(id string) (mediahook.Hook, bool) {
	want := mediahook.HookID(id)
	if want == "" {
		return mediahook.Hook{}, false
	}
	for _, h := range s.MediaHooks {
		if mediahook.HookID(h.ID) == want {
			return h, true
		}
	}
	return mediahook.Hook{}, false
}
