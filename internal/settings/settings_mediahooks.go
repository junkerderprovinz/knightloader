package settings

// The stored addresses a category drawer calls once a package filed in it has
// finished and its files are in place. The rows themselves live in
// internal/mediahook, which owns what one looks like and what may be in it; this
// file is only the settings document's half: where the list hangs, what a save
// refuses, and what the sanitiser does with a hand-edited file.
//
// WHY THE LIST IS HERE AND THE VALUE IS NOT. The address, the method, the header
// NAME and the wait are configuration: they belong in settings.json, they belong
// in a backup, and they belong in the diagnostics bundle where somebody helping
// with a bug report can see them. The header VALUE is a credential and is sealed
// in accounts.Store instead - see internal/mediahook's package comment for the
// three separate places a string field on this struct would have leaked it.
//
// WHY THE DRAWER POINTS AT THE HOOK AND NOT THE OTHER WAY ROUND. A drawer is
// already the thing with an identity, a picker, a table and a settings home
// (settings_categories.go), and its id is stable by construction. The two
// rejected anchors both fail on identity: a Packagizer rule's identity is its
// NAME, which falls back to its POSITION when it has none (rules.ruleName), so a
// hook keyed on a rule would break the moment somebody renamed or reordered
// their rules; and a resolved folder prefix is not an identity at all, it is a
// string that changes when a template variable expands differently.

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
// Refusal rather than sanitising, for the reason validateRows exists at all: a
// row that vanishes on save is a row the user goes on believing in, and this one
// would go on believing in it while their library quietly stopped being scanned.
//
// It also checks the references INTO the table from the drawers, and that is the
// same asymmetry ValidateCategories draws for the Packagizer: a drawer pointing
// at an address that is not there is a drawer that calls nothing, silently, on
// every package ever filed in it - which is the exact failure this feature
// exists to end. A TASK's own dead category id, by contrast, is history and is
// left alone; there is no equivalent of that here, because nothing but a drawer
// ever names a hook.
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
// would have stored. It is not categoryLabel's job (that one is for humans and
// prefers the name); this one has to name the KEY, because the key is what the
// dangling reference is about.
func categoryKey(c Category) string {
	if id := CategoryID(c.ID); id != "" {
		return id
	}
	return CategoryID(c.Name)
}

// NotifyHookFor is the address a drawer calls, or "" when it calls nothing -
// which is every drawer until somebody changes it, and every drawer whose id
// names no category at all.
func (s Settings) NotifyHookFor(categoryID string) string {
	return mediahook.HookID(s.CategoryFor(categoryID).Notify)
}

// MediaHookUsers is every drawer pointing at one address, by category id,
// sorted.
//
// Two callers, and they are the reason it is here rather than in either route:
// the listing says "picked on N drawers" so that an address nothing calls can be
// recognised as stored-and-idle, and the delete refuses with the drawers named
// so that "it will not delete" is answerable rather than mysterious.
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
// that "an id names nothing" is answered the same way everywhere.
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
