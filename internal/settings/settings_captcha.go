package settings

// settings_captcha.go: the non-secret half of "solver order" - which
// automatic captcha solvers to try, and in what order, before a human ever
// sees the prompt modal (Settings.CaptchaSolverOrder, declared in
// settings.go beside every other field - see that file's own package
// comment on why the struct itself is not split even though its sanitize
// rules are). The credential each id names lives in internal/accounts
// instead (GroupCaptchaSolver, catalogue.go) - this file never touches a
// secret and does not import that package for one, unlike
// settings_network.go, whose own sanitize hook exists specifically to keep
// two real secrets (the router password, every proxy password) off the wire
// correctly.

// captchaSolverIDs are the only ids CaptchaSolverOrder may carry. Plain
// string literals rather than a shared Go constant imported from
// internal/captcha - the same convention this app already uses for
// "torbox"/"alldebrid"/"realdebrid" (see internal/accounts/catalogue.go's
// own doc comment: catalogue ids are coordinated across packages by
// convention, not by a shared type) - so that a settings field about ORDER
// never has to import a package about SOLVING. accounts/catalogue_test.go's
// TestCaptchaSolverEntries is the other half of this convention: a rename on
// either side without the matching edit here is a test failure, not a
// silently orphaned stored order.
var captchaSolverIDs = map[string]bool{"2captcha": true, "anticaptcha": true}

// sanitizeCaptcha keeps CaptchaSolverOrder a well-formed try-order: no
// unknown id (one a save from an older or newer build might carry, or a
// hand-edited settings.json), no repeat (a duplicate would make "try each
// configured solver in order" try the same one twice and never reach a
// second, distinct solver at all), and nil rather than an empty non-nil
// slice once everything is filtered out - so an empty order reads the same
// way on disk regardless of how it got there, and omitempty (settings.go)
// actually omits it.
//
// Deliberately does NOT check whether an id's credential is actually
// configured (internal/accounts) - this package has no dependency on that
// one and should not gain one only to duplicate a check the solver-order
// walk already has to make anyway (skip an id with no stored key, try the
// next). An order naming a solver nobody has configured yet is not
// malformed, only inert until a key is added - the same relationship
// GroupDebrid's routing order already has with whether a credential backs
// each entry.
func sanitizeCaptcha(n Settings) Settings {
	if len(n.CaptchaSolverOrder) == 0 {
		n.CaptchaSolverOrder = nil
		return n
	}
	seen := make(map[string]bool, len(n.CaptchaSolverOrder))
	out := make([]string, 0, len(n.CaptchaSolverOrder))
	for _, id := range n.CaptchaSolverOrder {
		if !captchaSolverIDs[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		out = nil
	}
	n.CaptchaSolverOrder = out
	return n
}
