package notify

// Discord is the one far end this package recognises by its address.
//
// A Discord webhook takes nothing but its own JSON document, and the template
// model cannot write a good one: a body is one string per target, so it cannot
// colour a failure differently from a success, and it cannot keep a long file
// name under the embed limits, which Discord enforces by refusing the whole
// message with a 400. So a webhook address with no body of its own gets the
// document built here. A body the operator wrote still goes out as written,
// which keeps a custom layout possible and keeps this a default rather than a
// second kind of target.
//
// The address is also the credential: whoever has the token in its path can
// post to the channel. redact in http.go treats that path segment the way it
// treats an ntfy query token.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

// discordHosts are the hosts a webhook address is issued on. A var, so a test
// can add its own server.
var discordHosts = map[string]bool{
	"discord.com":           true,
	"discordapp.com":        true,
	"ptb.discord.com":       true,
	"canary.discord.com":    true,
	"ptb.discordapp.com":    true,
	"canary.discordapp.com": true,
}

// The colours of the three outcomes, Discord's own green, red and blurple, so a
// message reads like the rest of the client around it.
const (
	discordSuccess = 0x57F287
	discordFailure = 0xED4245
	discordInfo    = 0x5865F2
)

// The budgets each value is cut to. Discord's ceilings are 256 for the title,
// 4096 for the description, 1024 for a field value, 2048 for the footer and
// 6000 for the embed as a whole. The budgets sit below them so that the sum
// cannot pass 6000 however long every value is, which saves counting the
// whole embed again after it is built.
const (
	discordSubjectMax = 1024
	discordValueMax   = 256
	discordErrorMax   = 1024
	discordFooterMax  = 256
)

// discordToken is the token in a Discord webhook address, or "" when addr is
// not one.
//
// The path has to be exactly a webhook's, with or without an API version: the
// same address with /slack or /github after it takes those services' payloads,
// and an embed sent there is refused.
func discordToken(addr string) string {
	u, err := url.Parse(addr)
	if err != nil || !discordHosts[strings.ToLower(u.Hostname())] {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 5 && strings.HasPrefix(parts[1], "v") {
		parts = append(parts[:1], parts[2:]...)
	}
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "webhooks" || parts[2] == "" || parts[3] == "" {
		return ""
	}
	return parts[3]
}

type discordMessage struct {
	Embeds []discordEmbed `json:"embeds"`
	// AllowedMentions with an empty list switches every ping off, so a file
	// called "@everyone.mkv" stays a file name.
	AllowedMentions struct {
		Parse []string `json:"parse"`
	} `json:"allowed_mentions"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
	Footer      discordFooter  `json:"footer"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordFooter struct {
	Text string `json:"text"`
}

// discordBody is the message a Discord webhook is sent for one firing: one
// embed titled after the event, naming what it is about, coloured by how it
// went.
//
// The words are English. The server has no reader to ask for a language, and
// a channel is read by everyone in it.
func discordBody(f script.Firing, instanceName string) string {
	e := discordEmbedFor(f)
	footer := strings.TrimSpace(instanceName)
	if footer == "" {
		footer = "KnightLoader"
	}
	e.Footer.Text = discordText(footer, discordFooterMax)
	if !f.At.IsZero() {
		e.Timestamp = f.At.UTC().Format(time.RFC3339)
	}
	msg := discordMessage{Embeds: []discordEmbed{e}}
	msg.AllowedMentions.Parse = []string{}
	b, _ := json.Marshal(msg)
	return string(b)
}

// discordEmbedFor fills in what differs between events. A payload the trigger
// should carry and does not leaves an embed with its title alone, which still
// says what happened.
func discordEmbedFor(f script.Firing) discordEmbed {
	var e discordEmbed
	var fields fieldList
	task := func(title string, colour int) {
		e.Title, e.Color = title, colour
		if v := f.Task; v != nil {
			e.Description = discordText(v.Name, discordSubjectMax)
			fields.add("Package", v.Package)
			fields.add("Size", sizeText(v.Size))
			fields.add("Category", v.Category)
			fields.add("Host", v.Host)
			if f.Trigger == script.TriggerTaskFailed {
				fields.addLong("Error", firstOf(v.Error, v.Reason))
			}
		}
	}
	switch f.Trigger {
	case script.TriggerTaskDone:
		task("Download finished", discordSuccess)
	case script.TriggerTaskFailed:
		task("Download failed", discordFailure)
	case script.TriggerLinkAdded:
		task("Link added", discordInfo)
	case script.TriggerChecksumFailed:
		task("Checksum mismatch", discordFailure)
	case script.TriggerPackageDone:
		e.Title, e.Color = "Package finished", discordSuccess
		if v := f.Package; v != nil {
			if v.Failed > 0 {
				e.Title, e.Color = "Package finished with failed files", discordFailure
			}
			e.Description = discordText(v.Name, discordSubjectMax)
			fields.add("Files", fmt.Sprintf("%d of %d done", v.Done, v.Files))
			if v.Failed > 0 {
				fields.add("Failed", strconv.Itoa(v.Failed))
			}
			fields.add("Size", sizeText(v.Bytes))
		}
	case script.TriggerExtractDone:
		e.Title, e.Color = "Unpacked", discordSuccess
		if v := f.Extract; v != nil {
			if !v.OK {
				e.Title, e.Color = "Unpacking failed", discordFailure
			}
			e.Description = discordText(v.Name, discordSubjectMax)
			fields.add("Package", v.Package)
			if v.Files > 0 {
				fields.add("Files", strconv.Itoa(v.Files))
			}
			fields.add("Size", sizeText(v.Bytes))
			fields.add("Folder", v.Dir)
			if !v.OK {
				fields.addLong("Error", v.Error)
			}
		}
	case script.TriggerReconnectDone:
		e.Title, e.Color = "Reconnect failed", discordFailure
		if v := f.Reconnect; v != nil {
			switch {
			case v.OK && v.Changed:
				e.Title, e.Color = "New IP address", discordSuccess
			case v.OK:
				e.Title, e.Color = "Reconnect kept the same IP address", discordInfo
			}
			fields.add("From", v.From)
			fields.add("To", v.To)
			fields.addLong("Error", v.Error)
		}
	case script.TriggerAccountExpired:
		e.Title, e.Color = "Account expired", discordFailure
		if v := f.Account; v != nil {
			e.Description = discordText(firstOf(v.Label, v.Service), discordSubjectMax)
			fields.add("Service", v.Service)
			fields.add("Tier", v.Tier)
			fields.add("Expired", v.Expiry)
		}
	case script.TriggerCaptchaPending:
		e.Title, e.Color = "Captcha waiting", discordInfo
		if v := f.Captcha; v != nil {
			e.Description = discordText(v.Host, discordSubjectMax)
			fields.add("Kind", v.Kind)
			fields.add("Prompt", v.Prompt)
			fields.add("Expires", v.ExpiresAt)
		}
	case script.TriggerQueueIdle:
		e.Title, e.Color = "Queue is idle", discordInfo
	default:
		e.Title, e.Color = string(f.Trigger), discordInfo
	}
	e.Fields = fields
	return e
}

// fieldList leaves out a field with no value, since Discord refuses an embed
// with an empty one.
type fieldList []discordField

func (l *fieldList) add(name, value string) {
	if v := discordText(value, discordValueMax); v != "" {
		*l = append(*l, discordField{Name: name, Value: v, Inline: true})
	}
}

// addLong is for an error, which is a sentence rather than a figure and gets
// a line of its own.
func (l *fieldList) addLong(name, value string) {
	if v := discordText(value, discordErrorMax); v != "" {
		*l = append(*l, discordField{Name: name, Value: v})
	}
}

func firstOf(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// discordMarkup is what Discord reads as formatting anywhere in a line. A
// backslash in front of any of them shows the character itself.
const discordMarkup = "\\*_~`|<>[]#-"

// discordText is v as one line of Discord markdown that shows exactly v, cut
// to at most limit characters with an ellipsis where it was cut.
//
// Without the escaping "__init__.py" arrives as "init.py" in bold, and
// "01. Intro.mp3" as a list item numbered 1. A line break becomes a space,
// since a name that starts a second line can start a heading there.
//
// Characters are counted in UTF-16 units, the stricter of the two ways a limit
// can be read, so a name full of emoji cannot pass it. The cut falls between
// two escaped characters, never inside one, or the backslash left behind
// would swallow the ellipsis.
func discordText(v string, limit int) string {
	v = strings.TrimSpace(strings.Map(func(r rune) rune {
		if isControl(r) {
			return ' '
		}
		return r
	}, v))
	if v == "" {
		return ""
	}
	runes := []rune(v)
	pieces := make([]string, len(runes))
	total := 0
	for i, r := range runes {
		pieces[i] = string(r)
		if strings.ContainsRune(discordMarkup, r) || (r == '.' && listNumber(runes, i)) {
			pieces[i] = `\` + pieces[i]
		}
		total += utf16Len(pieces[i])
	}
	if total <= limit {
		return strings.Join(pieces, "")
	}
	var b strings.Builder
	used := 0
	for _, piece := range pieces {
		n := utf16Len(piece)
		if used+n > limit-1 {
			break
		}
		b.WriteString(piece)
		used += n
	}
	b.WriteString("…")
	return b.String()
}

// listNumber reports whether the full stop at i ends the number of an ordered
// list item, "1. " at the start of the line.
func listNumber(runes []rune, i int) bool {
	if i == 0 || i+1 >= len(runes) || runes[i+1] != ' ' {
		return false
	}
	for _, r := range runes[:i] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// sizeText writes a byte count the way the web interface does (decimal units,
// one decimal below 10, as app's fmtBytes), or "" for an unknown size.
func sizeText(n int64) string {
	if n <= 0 {
		return ""
	}
	units := [...]string{"B", "kB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	if v < 10 && i > 0 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}
