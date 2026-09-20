package settings

// The non-secret half of the solver order: which automatic captcha solvers to
// try, and in what order, before a human sees the prompt modal
// (Settings.CaptchaSolverOrder). The credential each id names lives in
// internal/accounts instead (GroupCaptchaSolver, catalogue.go), so this file
// never touches a secret and does not import that package for one.

// captchaSolverIDs are the only ids CaptchaSolverOrder may carry. Plain string
// literals rather than a constant imported from internal/captcha, the
// convention catalogue ids already follow (see internal/accounts/catalogue.go),
// so that a settings field about order does not import a package about solving.
// accounts/catalogue_test.go's TestCaptchaSolverEntries is the other half of
// that convention: a rename on either side without the matching edit here fails
// a test rather than orphaning a stored order.
var captchaSolverIDs = map[string]bool{"2captcha": true, "anticaptcha": true}

// sanitizeCaptcha keeps CaptchaSolverOrder a well-formed try-order: no unknown
// id, no repeat, since trying the same solver twice never reaches a second one,
// and nil rather than an empty slice once everything is filtered out, so that
// an empty order reads the same on disk however it got there.
//
// It does not check whether an id's credential is configured. This package has
// no dependency on internal/accounts and should not gain one to duplicate a
// check the solver-order walk makes anyway: skip an id with no stored key, try
// the next. An order naming a solver nobody has configured is inert rather than
// malformed, the relationship GroupDebrid's routing order already has with its
// credentials.
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
