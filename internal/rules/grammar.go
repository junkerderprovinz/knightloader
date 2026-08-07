package rules

// What the editor is allowed to offer, described by the engine that enforces it.
//
// A rule editor has to know which fields exist, which operators each one takes,
// which actions do something, and which placeholders resolve. Every one of those
// is a fact about this package. Written out a second time in the interface they
// drift the moment a field is added here, and the drift is invisible in both
// directions: an operator the form offers that Compile refuses becomes a rule
// that silently never fires, and a field added here with no entry in the form is
// a feature nobody can reach.
//
// Only ids and shapes are described. Not one word of what a human reads is in
// here, because the server does not know which of the 38 locales a given browser
// is showing, and two clients of one instance can differ. The interface keys its
// own strings off these ids, and an id it has no string for falls back to the id
// itself rather than to a blank row.

// Grammar is the whole of it.
type Grammar struct {
	Fields    []FieldGrammar  `json:"fields"`
	Operators []OpGrammar     `json:"operators"`
	Actions   []ActionGrammar `json:"actions"`
	Variables []Variable      `json:"variables"`
	// Categories are the file-type shorthands the editor offers on a filetype
	// condition. They expand into ordinary conditions and are described here for
	// the same reason everything else in this file is: the pattern a category
	// stands for has to be the one the engine will actually run.
	Categories []Category `json:"categories"`
	Limits     Limits     `json:"limits"`
}

// FieldGrammar is one thing a condition can look at, and what it can be asked.
type FieldGrammar struct {
	ID Field `json:"id"`
	// Ops is exactly the set compileCondition accepts for this field, in the
	// order the form should list them.
	Ops []Op `json:"ops"`
	// Numeric marks the field whose values are byte counts rather than text, so
	// the form knows to offer a size box instead of a text box. Turning "700 MB"
	// into a number is the interface's job: a parser down here would disagree
	// with the one up there sooner or later.
	Numeric bool `json:"numeric,omitempty"`
	// Groups marks a field a capture group can be read from, which is every field
	// the "matches" operator applies to.
	Groups bool `json:"groups,omitempty"`
}

// OpGrammar says what a form has to collect for one operator.
type OpGrammar struct {
	ID Op `json:"id"`
	// Value is set when the operator needs the single Value box filled.
	Value bool `json:"value,omitempty"`
	// Range is set when it needs Min and Max instead. Both are never set at once.
	Range bool `json:"range,omitempty"`
	// Regex marks the operator whose value is a pattern, so the form can offer a
	// pattern box and say what a bad one costs.
	Regex bool `json:"regex,omitempty"`
}

// ActionGrammar is one thing a matching rule can do.
type ActionGrammar struct {
	// ID is the JSON field name on Action, so the form addresses the same key the
	// engine reads and a typo is a compile error over here rather than a control
	// that quietly does nothing.
	ID string `json:"id"`
	// Kind is how it is edited: template, int, bool or reject.
	Kind string `json:"kind"`
	// Flavour is which of the two engines honours it: "packagizer", "filter", or
	// empty for both.
	Flavour string `json:"flavour,omitempty"`
	// Min and Max bound an int action, so the form clamps to the same numbers
	// actionProblems refuses outside of.
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

// Variable is one placeholder a template field can carry.
type Variable struct {
	// Tag is inserted verbatim, with N and FIELD left standing as the parts the
	// user replaces.
	Tag string `json:"tag"`
	// ID keys the description the interface shows beside it.
	ID string `json:"id"`
	// Params names the placeholders inside Tag, so the form can prompt for them
	// instead of leaving the user to notice a literal N in their folder name.
	Params []string `json:"params,omitempty"`
}

// Limits are the numbers a form should stop the user at, taken from the same
// constants that refuse a rule.
type Limits struct {
	PriorityMin int `json:"priorityMin"`
	PriorityMax int `json:"priorityMax"`
	MaxChunks   int `json:"maxChunks"`
	MaxPattern  int `json:"maxPattern"`
}

// textOps is what compileCondition accepts on everything except a file size.
var textOps = []Op{OpContains, OpContainsNot, OpEquals, OpEqualsNot, OpMatches}

// sizeOps is what it accepts on a file size: the text operators are refused
// there, because "filesize contains 100" would match 1000, 2100 and 100000.
var sizeOps = []Op{OpBetween, OpEquals, OpEqualsNot}

func intPtr(v int) *int { return &v }

// Describe is the grammar as it stands. It is built rather than stored so that
// adding a field or an operator above shows up here by the same edit.
func Describe() Grammar {
	g := Grammar{
		Operators: []OpGrammar{
			{ID: OpContains, Value: true},
			{ID: OpContainsNot, Value: true},
			{ID: OpEquals, Value: true},
			{ID: OpEqualsNot, Value: true},
			{ID: OpMatches, Value: true, Regex: true},
			{ID: OpBetween, Range: true},
		},
		Actions: []ActionGrammar{
			{ID: "packageName", Kind: "template", Flavour: "packagizer"},
			{ID: "downloadDir", Kind: "template", Flavour: "packagizer"},
			{ID: "comment", Kind: "template", Flavour: "packagizer"},
			{ID: "priority", Kind: "int", Flavour: "packagizer",
				Min: intPtr(PriorityMin), Max: intPtr(PriorityMax)},
			{ID: "autoExtract", Kind: "bool", Flavour: "packagizer"},
			{ID: "chunks", Kind: "int", Flavour: "packagizer", Min: intPtr(1), Max: intPtr(MaxChunks)},
			{ID: "reject", Kind: "reject", Flavour: "filter"},
			{ID: "reason", Kind: "template", Flavour: "filter"},
		},
		Variables:  variables(),
		Categories: Categories(),
		Limits: Limits{
			PriorityMin: PriorityMin,
			PriorityMax: PriorityMax,
			MaxChunks:   MaxChunks,
			MaxPattern:  maxPattern,
		},
	}
	// "filename" is deliberately absent from Actions. Action.Filename exists and
	// Apply fills it in, but nothing downstream can honour it — the engine is
	// handed a folder and names the file itself — so a rename offered in the form
	// would be a control that changes the list and not the disk. It stays in the
	// engine because the wave that widens the download call will want it, and the
	// dry run still reports it so an imported JDownloader set that renames is
	// visible rather than silently ignored.
	for _, f := range []Field{
		FieldFilename, FieldURL, FieldHoster, FieldSource, FieldFiletype, FieldPackage,
	} {
		g.Fields = append(g.Fields, FieldGrammar{ID: f, Ops: textOps, Groups: true})
	}
	g.Fields = append(g.Fields, FieldGrammar{ID: FieldFilesize, Ops: sizeOps, Numeric: true})
	return g
}

// variables is every placeholder a template resolves, in the order a menu should
// list them: the link's own values first, then the derived ones, then the two
// that need a parameter, then the counter.
//
// The date family is listed by its common shapes rather than as one
// <jd:simpledate:...> entry. The pattern language is Java's, which is not
// something to make somebody guess at from a tag name.
func variables() []Variable {
	return []Variable{
		{Tag: "<jd:packagename>", ID: "packagename"},
		{Tag: "<jd:hoster>", ID: "hoster"},
		{Tag: "<jd:filename>", ID: "filename"},
		{Tag: "<jd:orgfilename>", ID: "orgfilename"},
		{Tag: "<jd:orgfilenamewithoutext>", ID: "orgfilenamewithoutext"},
		{Tag: "<jd:orgfiletype>", ID: "orgfiletype"},
		{Tag: "<jd:date>", ID: "date"},
		{Tag: "<jd:year>", ID: "year"},
		{Tag: "<jd:month>", ID: "month"},
		{Tag: "<jd:day>", ID: "day"},
		{Tag: "<jd:simpledate:yyyy-MM>", ID: "simpledate", Params: []string{"pattern"}},
		{Tag: "<jd:source:N>", ID: "source", Params: []string{"n"}},
		{Tag: "<jd:match:FIELD:N>", ID: "match", Params: []string{"field", "n"}},
		{Tag: "<jd:append>", ID: "append"},
	}
}
