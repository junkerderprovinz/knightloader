package rules

// What the rule editor may offer, described by the engine that enforces it:
// fields, the operators each takes, actions and placeholders. A second copy in
// the interface would drift, and an operator the form offers that Compile
// refuses becomes a rule that never fires.
//
// Only ids and shapes are described, no text a person reads; the interface
// keys its own translated strings off these ids and falls back to the id.

// Grammar is the whole description.
type Grammar struct {
	Fields    []FieldGrammar  `json:"fields"`
	Operators []OpGrammar     `json:"operators"`
	Actions   []ActionGrammar `json:"actions"`
	Variables []Variable      `json:"variables"`
	// Categories are the file-type shorthands the editor offers on a filetype
	// condition, with the exact pattern the engine will run.
	Categories []Category `json:"categories"`
	Limits     Limits     `json:"limits"`
}

// FieldGrammar is one thing a condition can look at, and what it can be asked.
type FieldGrammar struct {
	ID Field `json:"id"`
	// Ops is exactly the set compileCondition accepts for this field, in the
	// order the form should list them.
	Ops []Op `json:"ops"`
	// Numeric marks a field whose values are byte counts, so the form offers a
	// size box and does the unit parsing itself.
	Numeric bool `json:"numeric,omitempty"`
	// Groups marks a field a capture group can be read from.
	Groups bool `json:"groups,omitempty"`
}

// OpGrammar says what a form has to collect for one operator.
type OpGrammar struct {
	ID Op `json:"id"`
	// Value is set when the operator needs the single Value box filled.
	Value bool `json:"value,omitempty"`
	// Range is set when it needs Min and Max instead. Both are never set at once.
	Range bool `json:"range,omitempty"`
	// Regex marks the operator whose value is a pattern.
	Regex bool `json:"regex,omitempty"`
}

// ActionGrammar is one thing a matching rule can do.
type ActionGrammar struct {
	// ID is the JSON field name on Action, so the form addresses the key the
	// engine reads.
	ID string `json:"id"`
	// Kind is how it is edited: template, int, bool, category or reject. The
	// editor falls through to the reject control for a kind it does not know,
	// so a new kind needs a branch in web/src/components/RuleEditor.tsx
	// (TestEveryActionKindHasAControl checks this).
	Kind string `json:"kind"`
	// Flavour is which engine honours it: "packagizer", "filter", or empty for
	// both.
	Flavour string `json:"flavour,omitempty"`
	// Min and Max bound an int action to the numbers actionProblems accepts.
	// For a category, Max is the longest id that can address a stored one.
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

// Variable is one placeholder a template field can carry.
type Variable struct {
	// Tag is inserted verbatim, with N and FIELD left as the parts the user
	// replaces.
	Tag string `json:"tag"`
	// ID keys the description the interface shows beside it.
	ID string `json:"id"`
	// Params names the placeholders inside Tag, so the form can prompt for
	// them.
	Params []string `json:"params,omitempty"`
}

// Limits are the numbers a form should stop the user at, from the same
// constants that refuse a rule.
type Limits struct {
	PriorityMin int `json:"priorityMin"`
	PriorityMax int `json:"priorityMax"`
	MaxChunks   int `json:"maxChunks"`
	MaxPattern  int `json:"maxPattern"`
}

// textOps is what compileCondition accepts on everything except a file size.
var textOps = []Op{OpContains, OpContainsNot, OpEquals, OpEqualsNot, OpMatches}

// sizeOps is what it accepts on a file size.
var sizeOps = []Op{OpBetween, OpEquals, OpEqualsNot}

func intPtr(v int) *int { return &v }

// Describe is the grammar as it stands, built from the same constants the
// engine uses.
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
			// Listed beside the folder because a rule that sets both lands in
			// the folder: the category's own folder only applies when nothing
			// else names one, while its priority, unpacking and collision rule
			// still do. It is a pick from settings.Categories, not from
			// Grammar.Categories, which are file-type shorthands, and never a
			// template (see categoryProblem).
			{ID: "category", Kind: "category", Flavour: "packagizer", Max: intPtr(MaxCategoryRef)},
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
	// "headers" is left out of Actions until RuleEditor.tsx has a "profile"
	// branch offering the stored profiles; until then the editor would fall
	// through to a reject switch wired to Action.Reject. The entry to add is
	//
	//	{ID: "headers", Kind: "profile", Flavour: "packagizer", Max: intPtr(MaxHeaderProfile)},
	//
	// "filename" is left out because nothing downstream honours it yet: the
	// download engine is handed a folder and names the file itself. Apply
	// still fills it and the dry run reports it, so an imported JDownloader
	// set that renames is visible.
	for _, f := range []Field{
		FieldFilename, FieldURL, FieldHoster, FieldSource, FieldFiletype, FieldPackage,
	} {
		g.Fields = append(g.Fields, FieldGrammar{ID: f, Ops: textOps, Groups: true})
	}
	g.Fields = append(g.Fields, FieldGrammar{ID: FieldFilesize, Ops: sizeOps, Numeric: true})
	return g
}

// variables is every placeholder a template resolves, in menu order: the
// link's own values, the derived ones, the two with a parameter, then the
// counter. The date family is listed by its common shapes, since the
// simpledate pattern language is Java's.
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
