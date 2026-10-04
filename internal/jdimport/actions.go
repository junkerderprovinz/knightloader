package jdimport

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// jdPriority is JDownloader's priority scale by name, onto KnightLoader's
// -3..3.
var jdPriority = map[string]int{
	"LOWEST":  -3,
	"LOWER":   -2,
	"LOW":     -1,
	"DEFAULT": 0,
	"HIGH":    1,
	"HIGHER":  2,
	"HIGHEST": 3,
}

// jdTag is JDownloader's own placeholder grammar: a name, an optional argument
// after the first colon, and an optional closing slash.
var jdTag = regexp.MustCompile(`(?i)<jd:([^<>:]+)(?::([^<>]*?))?\s*/?\s*>`)

// sameMeaning are the placeholders KnightLoader resolves as JDownloader does
// when they carry no argument.
var sameMeaning = map[string]bool{
	"packagename":           true,
	"orgfilename":           true,
	"orgfilenamewithoutext": true,
	"orgfiletype":           true,
	"hoster":                true,
	"filename":              true,
	"date":                  true,
	"year":                  true,
	"month":                 true,
	"day":                   true,
}

// groupFields are JDownloader's capture-group placeholders and the condition
// each reads its groups from, as KnightLoader names that field.
var groupFields = map[string]rules.Field{
	"source":         rules.FieldSource,
	"hoster":         rules.FieldURL,
	"orgfilename":    rules.FieldFilename,
	"orgpackagename": rules.FieldPackage,
	"orgfiletype":    rules.FieldFiletype,
}

var digits = regexp.MustCompile(`^-?[0-9]{1,2}$`)

// translateTemplate rewrites a JDownloader template into KnightLoader's. The
// second result is the first placeholder that has no counterpart here, or "".
func translateTemplate(s string) (string, string) {
	var unknown string
	out := jdTag.ReplaceAllStringFunc(s, func(tag string) string {
		m := jdTag.FindStringSubmatch(tag)
		name, arg := strings.ToLower(strings.TrimSpace(m[1])), strings.TrimSpace(m[2])
		hasArg := strings.Contains(tag[len("<jd:")+len(m[1]):], ":")
		switch {
		case !hasArg && sameMeaning[name]:
			return "<jd:" + name + ">"
		case !hasArg && name == "orgpackagename":
			return "<jd:packagename>"
		case name == "simpledate" && arg != "":
			return "<jd:simpledate:" + arg + ">"
		case name == "hoster" && arg == "-1":
			return "<jd:hoster>"
		case hasArg && digits.MatchString(arg) && !strings.HasPrefix(arg, "-") && groupFields[name] != "":
			return "<jd:match:" + string(groupFields[name]) + ":" + arg + ">"
		}
		if unknown == "" {
			unknown = tag
		}
		return tag
	})
	return out, unknown
}

// Codes of what a Packagizer rule can do in JDownloader that has no rule
// action here.
const (
	DroppedAutoAdd     = "autoAdd"
	DroppedAutoStart   = "autoStart"
	DroppedForcedStart = "forcedStart"
	DroppedLinkEnabled = "linkEnabled"
	DroppedRename      = "renameAfter"
	DroppedMoveTo      = "moveAfter"
	DroppedStop        = "stopAfterRule"
)

// mapActions maps what a Packagizer rule sets. Whatever has no counterpart is
// left out with a note; a rule left with nothing to do is blocked.
func mapActions(r JDRule) (rules.Action, []Reason, *Reason) {
	var a rules.Action
	var notes []Reason
	set := false

	template := func(raw, field string, dst *string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		out, unknown := translateTemplate(raw)
		if unknown != "" {
			notes = append(notes, Reason{
				Code:   "rulePlaceholder",
				Params: map[string]string{"tag": unknown, "field": field},
				Text:   fmt.Sprintf("The %s uses %s, which KnightLoader does not have, so it is left out.", field, unknown),
			})
			return
		}
		*dst = out
		set = true
	}
	template(r.PackageNameAction, "packageName", &a.PackageName)
	template(r.DownloadDestination, "downloadDir", &a.DownloadDir)
	template(r.CommentAction, "comment", &a.Comment)
	template(r.FilenameAction, "filename", &a.Filename)
	if a.Filename != "" {
		notes = append(notes, Reason{
			Code: "ruleRenameKept",
			Text: "The new file name stays in the rule and shows in the rule test, but downloads do not use it yet.",
		})
	}

	if v, ok := jdPriority[strings.ToUpper(strings.TrimSpace(r.Priority))]; ok {
		a.Priority = &v
		set = true
	}
	// Zero is JDownloader's "leave it".
	if r.Chunks > 0 {
		n := min(r.Chunks, rules.MaxChunks)
		a.Chunks = &n
		set = true
	}
	if r.AutoExtract != nil {
		v := *r.AutoExtract
		a.AutoExtract = &v
		set = true
	}

	var dropped []string
	for _, d := range []struct {
		on   bool
		code string
	}{
		{r.AutoAdd != nil, DroppedAutoAdd},
		{r.AutoStart != nil, DroppedAutoStart},
		{r.ForcedStart != nil, DroppedForcedStart},
		{r.LinkEnabledAction != nil, DroppedLinkEnabled},
		{strings.TrimSpace(r.Rename) != "", DroppedRename},
		{strings.TrimSpace(r.MoveTo) != "", DroppedMoveTo},
		{r.StopAfterThisRule, DroppedStop},
	} {
		if d.on {
			dropped = append(dropped, d.code)
		}
	}
	if len(dropped) > 0 {
		notes = append(notes, Reason{
			Code:   "ruleDropped",
			Params: map[string]string{"actions": strings.Join(dropped, ",")},
			Text:   "Left out, since KnightLoader rules cannot do this: " + strings.Join(dropped, ", "),
		})
	}
	if !set {
		return a, notes, reason("ruleNoAction", "Nothing this rule sets exists in KnightLoader.", nil)
	}
	return a, notes, nil
}
