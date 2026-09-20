package settings

// The rule lists and the timetable, and the reason nothing here touches them.

// sanitizeRules changes nothing. Sanitising a rule list or a timetable at load
// means deleting the row somebody got wrong instead of showing it to them, and
// a filter rule that vanishes on save is a filter they go on believing in: for
// a link filter, links they think are blocked and are not. rules.Compile and
// schedule.Entry.Validate report what cannot be used and the API hands that
// back; nothing edits the list behind them.
//
// It is a hook rather than a comment because the next person to add a
// rule-shaped setting will look for one, and finding nothing is how the rule
// above gets broken.
func sanitizeRules(n Settings) Settings { return n }
