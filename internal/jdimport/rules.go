package jdimport

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// regexFilter is JDownloader's text condition (RegexFilter). Without UseRegex,
// "*" and "?" are wildcards.
type regexFilter struct {
	Enabled   bool   `json:"enabled"`
	MatchType string `json:"matchType"`
	Regex     string `json:"regex"`
	UseRegex  bool   `json:"useRegex"`
}

// sizeFilter is JDownloader's file size condition (FilesizeFilter): bytes,
// both bounds inclusive.
type sizeFilter struct {
	Enabled   bool   `json:"enabled"`
	MatchType string `json:"matchType"`
	From      int64  `json:"from"`
	To        int64  `json:"to"`
}

// typeFilter is JDownloader's file type condition (FiletypeFilter). Customs is
// a comma-separated list of extensions, or one pattern with UseRegex.
type typeFilter struct {
	Enabled   bool   `json:"enabled"`
	MatchType string `json:"matchType"`
	Audio     bool   `json:"audioFilesEnabled"`
	Video     bool   `json:"videoFilesEnabled"`
	Archives  bool   `json:"archivesEnabled"`
	Images    bool   `json:"imagesEnabled"`
	Documents bool   `json:"docFilesEnabled"`
	Subtitles bool   `json:"subFilesEnabled"`
	Programs  bool   `json:"exeFilesEnabled"`
	Hashes    bool   `json:"hashEnabled"`
	Customs   string `json:"customs"`
	UseRegex  bool   `json:"useRegex"`
}

// switchFilter is any condition read only for whether it is switched on: the
// match-always switch, and the conditions KnightLoader has nothing to compare
// with.
type switchFilter struct {
	Enabled bool `json:"enabled"`
}

// JDRule is one Packagizer or link filter rule as JDownloader stores it
// (FilterRule, PackagizerRule, LinkgrabberFilterRule). A filter rule has no
// actions and a Packagizer rule no Accept.
type JDRule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Static marks a rule JDownloader ships and puts back when it is deleted.
	Static bool `json:"staticRule"`

	Filename    *regexFilter  `json:"filenameFilter"`
	HosterURL   *regexFilter  `json:"hosterURLFilter"`
	SourceURL   *regexFilter  `json:"sourceURLFilter"`
	PackageName *regexFilter  `json:"packagenameFilter"`
	Filesize    *sizeFilter   `json:"filesizeFilter"`
	Filetype    *typeFilter   `json:"filetypeFilter"`
	MatchAlways *switchFilter `json:"matchAlwaysFilter"`

	Origin          *switchFilter `json:"originFilter"`
	Online          *switchFilter `json:"onlineStatusFilter"`
	Plugin          *switchFilter `json:"pluginStatusFilter"`
	Condition       *switchFilter `json:"conditionFilter"`
	Comment         *switchFilter `json:"commentFilter"`
	LinkEnabled     *switchFilter `json:"linkEnabledFilter"`
	DownloadDupe    *switchFilter `json:"downloadListDupeFilter"`
	LinkgrabberDupe *switchFilter `json:"linkgrabberDupeFilter"`

	// Accept marks a link filter rule as an exception: JDownloader keeps a
	// link it matches whatever the other rules say.
	Accept bool `json:"accept"`

	DownloadDestination string `json:"downloadDestination"`
	PackageNameAction   string `json:"packageName"`
	FilenameAction      string `json:"filename"`
	CommentAction       string `json:"comment"`
	Priority            string `json:"priority"`
	Chunks              int    `json:"chunks"`
	AutoExtract         *bool  `json:"autoExtractionEnabled"`
	AutoAdd             *bool  `json:"autoAddEnabled"`
	AutoStart           *bool  `json:"autoStartEnabled"`
	ForcedStart         *bool  `json:"autoForcedStartEnabled"`
	LinkEnabledAction   *bool  `json:"linkEnabled"`
	Rename              string `json:"rename"`
	MoveTo              string `json:"moveto"`
	StopAfterThisRule   bool   `json:"stopAfterThisRule"`
}

// MappedRule is one JDownloader rule and what it became.
type MappedRule struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Accept is set on a link filter exception.
	Accept bool `json:"accept,omitempty"`
	// Rule is the KnightLoader rule, valid when Blocked is nil.
	Rule rules.Rule `json:"rule"`
	// Blocked says why the rule cannot come over at all.
	Blocked *Reason `json:"blocked,omitempty"`
	// Notes name what the rule did in JDownloader that it does not do here,
	// for a rule that comes over otherwise.
	Notes []Reason `json:"notes,omitempty"`
}

// readRules reads a rule list file. switchKey in settingsFile switches the
// whole list in JDownloader, and a list switched off there arrives with every
// rule switched off.
func (c *Config) readRules(fsys fs.FS, dir, name, settingsFile, switchKey string) []JDRule {
	b, ok := c.readFile(fsys, dir, name)
	if !ok {
		return nil
	}
	var out []JDRule
	if err := json.Unmarshal(b, &out); err != nil {
		c.problem(name, err)
		return nil
	}
	if s, ok := c.readFile(fsys, dir, settingsFile); ok {
		var m map[string]any
		if json.Unmarshal(s, &m) == nil {
			if on, ok := m[switchKey].(bool); ok && !on {
				for i := range out {
					out[i].Enabled = false
				}
			}
		}
	}
	return out
}

// MapPackagizer maps JDownloader's Packagizer rules.
func MapPackagizer(in []JDRule) []MappedRule {
	out := make([]MappedRule, 0, len(in))
	for _, r := range in {
		out = append(out, mapRule(r, false))
	}
	return out
}

// MapLinkFilter maps JDownloader's link filter rules.
func MapLinkFilter(in []JDRule) []MappedRule {
	out := make([]MappedRule, 0, len(in))
	for _, r := range in {
		out = append(out, mapRule(r, true))
	}
	return out
}

// builtinPackageFolder is the id of JDownloader's own "a folder per package"
// rule, which KnightLoader has as a setting.
const builtinPackageFolder = "SubFolderByPackageRule"

func mapRule(r JDRule, filter bool) MappedRule {
	m := MappedRule{Name: strings.TrimSpace(r.Name), Enabled: r.Enabled, Accept: filter && r.Accept}
	if m.Name == "" && r.ID != "" {
		m.Name = r.ID
	}
	if r.Static {
		if r.ID == builtinPackageFolder {
			m.Blocked = reason("ruleBuiltinPackageFolder",
				"JDownloader's own rule for a folder per package. In KnightLoader this is the switch Subfolder per package under Settings, Downloads.", nil)
		} else {
			m.Blocked = reason("ruleBuiltin",
				"A rule JDownloader ships itself. KnightLoader either has no use for it or does the same on its own.", nil)
		}
		return m
	}
	conds, blocked := mapConditions(r)
	if blocked != nil {
		m.Blocked = blocked
		return m
	}
	rule := rules.Rule{Name: m.Name, Disabled: !r.Enabled, Conditions: conds}
	if filter {
		rule.Action = rules.Action{Reject: !r.Accept}
	} else {
		act, notes, blocked := mapActions(r)
		if blocked != nil {
			m.Blocked = blocked
			m.Notes = notes
			return m
		}
		rule.Action = act
		m.Notes = notes
	}
	// Compiled on its own and switched on, so a rule that is off in
	// JDownloader is still checked before it is stored.
	check := rule
	check.Disabled = false
	if _, problems := rules.Compile(rules.Set{Rules: []rules.Rule{check}}); len(problems) > 0 {
		m.Blocked = reason("ruleInvalid", "KnightLoader cannot use this rule: "+problems[0].Message,
			map[string]string{"error": problems[0].Message})
		return m
	}
	m.Rule = rule
	return m
}

// mapConditions turns the switched-on conditions into KnightLoader's. A
// condition KnightLoader cannot test blocks the whole rule, since leaving it
// out would make the rule match links it never matched in JDownloader.
func mapConditions(r JDRule) ([]rules.Condition, *Reason) {
	on := func(f *switchFilter) bool { return f != nil && f.Enabled }
	if on(r.Origin) {
		return nil, reason("ruleOrigin", "KnightLoader rules cannot test where a link came from.", nil)
	}
	for _, f := range []*switchFilter{r.Online, r.Plugin, r.Condition, r.Comment, r.LinkEnabled, r.DownloadDupe, r.LinkgrabberDupe} {
		if on(f) {
			return nil, reason("ruleStatus",
				"KnightLoader rules cannot test a link's status, its comment or whether it is a duplicate.", nil)
		}
	}
	var out []rules.Condition
	for _, t := range []struct {
		f     *regexFilter
		field rules.Field
	}{
		{r.Filename, rules.FieldFilename},
		{r.HosterURL, rules.FieldURL},
		{r.SourceURL, rules.FieldSource},
		{r.PackageName, rules.FieldPackage},
	} {
		if t.f == nil || !t.f.Enabled {
			continue
		}
		c, why := textCondition(*t.f, t.field)
		if why != nil {
			return nil, why
		}
		out = append(out, c)
	}
	if r.Filesize != nil && r.Filesize.Enabled {
		c, why := sizeCondition(*r.Filesize)
		if why != nil {
			return nil, why
		}
		out = append(out, c)
	}
	if r.Filetype != nil && r.Filetype.Enabled {
		c, why := typeCondition(*r.Filetype)
		if why != nil {
			return nil, why
		}
		out = append(out, c)
	}
	if len(out) == 0 && !on(r.MatchAlways) {
		return nil, noCondition()
	}
	return out, nil
}

func noCondition() *Reason {
	return reason("ruleNoCondition", "This rule tests nothing, so here it would match every link.", nil)
}

// textCondition maps a text condition. A plain value keeps KnightLoader's
// plain operators, which fold case as JDownloader does; a wildcard or a
// pattern becomes a regular expression, case-insensitive and with "." taking
// line breaks unless the pattern says otherwise, and has no negated form here.
func textCondition(f regexFilter, field rules.Field) (rules.Condition, *Reason) {
	value := f.Regex
	plain := !f.UseRegex && !strings.ContainsAny(value, "*?")
	if plain {
		ops := map[string]rules.Op{
			"CONTAINS":     rules.OpContains,
			"EQUALS":       rules.OpEquals,
			"CONTAINS_NOT": rules.OpContainsNot,
			"EQUALS_NOT":   rules.OpEqualsNot,
		}
		op, ok := ops[f.MatchType]
		if !ok {
			return rules.Condition{}, unknownMatch(f.MatchType)
		}
		return rules.Condition{Field: field, Op: op, Value: value}, nil
	}
	pattern := value
	if !f.UseRegex {
		pattern = wildcard(value)
	}
	switch f.MatchType {
	case "CONTAINS":
	case "EQUALS":
		pattern = "^(?:" + pattern + ")$"
	case "CONTAINS_NOT", "EQUALS_NOT":
		return rules.Condition{}, reason("ruleNegated",
			"KnightLoader rules have no “does not contain” for a pattern or a wildcard.", nil)
	default:
		return rules.Condition{}, unknownMatch(f.MatchType)
	}
	pattern = javaFlags(value) + pattern
	if _, err := regexp.Compile(pattern); err != nil {
		return rules.Condition{}, patternProblem(value, err)
	}
	return rules.Condition{Field: field, Op: rules.OpMatches, Value: pattern}, nil
}

// javaFlags is the flag group JDownloader compiles a pattern with: case
// folding and "." over line breaks, each unless the pattern sets that flag
// itself.
func javaFlags(pattern string) string {
	flags := ""
	if !strings.Contains(pattern, "(?i)") && !strings.Contains(pattern, "(?-i)") {
		flags += "i"
	}
	if !strings.Contains(pattern, "(?s)") && !strings.Contains(pattern, "(?-s)") {
		flags += "s"
	}
	if flags == "" {
		return ""
	}
	return "(?" + flags + ")"
}

func patternProblem(pattern string, err error) *Reason {
	return reason("rulePattern",
		fmt.Sprintf("The pattern %q is written for Java and cannot be read here: %v", pattern, err),
		map[string]string{"pattern": pattern, "error": err.Error()})
}

// wildcard turns JDownloader's wildcard text, "*" for any run of characters
// and "?" for one, into a pattern.
func wildcard(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return b.String()
}

func unknownMatch(t string) *Reason {
	msg := fmt.Sprintf("unknown comparison %q", t)
	return reason("ruleInvalid", "KnightLoader cannot use this rule: "+msg, map[string]string{"error": msg})
}

func sizeCondition(f sizeFilter) (rules.Condition, *Reason) {
	switch f.MatchType {
	case "BETWEEN":
	case "NOT_BETWEEN":
		return rules.Condition{}, reason("ruleSizeOutside",
			"KnightLoader rules can test a size inside a range, not outside one.", nil)
	default:
		return rules.Condition{}, unknownMatch(f.MatchType)
	}
	if f.From < 0 || f.To < f.From {
		msg := fmt.Sprintf("the size range %d to %d bytes is empty", f.From, f.To)
		return rules.Condition{}, reason("ruleInvalid", "KnightLoader cannot use this rule: "+msg, map[string]string{"error": msg})
	}
	// Max zero means no upper bound here, so JDownloader's 0..0 is an exact
	// size.
	if f.To == 0 {
		return rules.Condition{Field: rules.FieldFilesize, Op: rules.OpEquals, Value: "0"}, nil
	}
	return rules.Condition{Field: rules.FieldFilesize, Op: rules.OpBetween, Min: f.From, Max: f.To}, nil
}

// hashExtensions are the checksum files behind JDownloader's hash box, which
// no KnightLoader category lists.
var hashExtensions = []string{"sfv", "md5", "sha1", "sha256", "sha512", "par2"}

func typeCondition(f typeFilter) (rules.Condition, *Reason) {
	switch f.MatchType {
	case "IS":
	case "IS_NOT":
		return rules.Condition{}, reason("ruleTypeNot", "KnightLoader rules have no “is not of this type”.", nil)
	default:
		return rules.Condition{}, unknownMatch(f.MatchType)
	}
	var exts []string
	add := func(id string) {
		for _, c := range rules.Categories() {
			if c.ID == id {
				exts = append(exts, c.Extensions...)
			}
		}
	}
	for _, box := range []struct {
		on bool
		id string
	}{
		{f.Audio, "audio"}, {f.Video, "video"}, {f.Archives, "archive"}, {f.Images, "image"},
		{f.Documents, "document"}, {f.Subtitles, "subtitle"}, {f.Programs, "program"},
	} {
		if box.on {
			add(box.id)
		}
	}
	if f.Hashes {
		exts = append(exts, hashExtensions...)
	}
	var alts []string
	for _, e := range exts {
		alts = append(alts, regexp.QuoteMeta(e))
	}
	if custom := strings.TrimSpace(f.Customs); custom != "" {
		if f.UseRegex {
			alts = append(alts, custom)
		} else {
			for _, e := range strings.Split(custom, ",") {
				if e = strings.TrimPrefix(strings.TrimSpace(e), "."); e != "" {
					alts = append(alts, wildcard(e))
				}
			}
		}
	}
	if len(alts) == 0 {
		return rules.Condition{}, noCondition()
	}
	pattern := "(?i)^(?:" + strings.Join(alts, "|") + ")$"
	if _, err := regexp.Compile(pattern); err != nil {
		return rules.Condition{}, patternProblem(f.Customs, err)
	}
	return rules.Condition{Field: rules.FieldFiletype, Op: rules.OpMatches, Value: pattern}, nil
}
