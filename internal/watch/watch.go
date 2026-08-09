// Package watch picks up links dropped into a folder. Dropping a file into a
// share is the intake method that costs a self-hoster nothing: no browser
// extension, no API client, no port to open. It is also the format
// JDownloader's folder watch uses (*.crawljob), so the tooling people already
// have keeps working when it is pointed at KnightLoader instead.
//
// The package is split by subject so that the file format and the polling can be
// read apart: this file is what a dropped file says (Job, Parse), poller.go is
// one directory being watched, and watcher.go is the set of directories the app
// has configured, which is more than one.
package watch

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// maxIntakeSize caps what we are willing to read into memory. An intake file is
// a link list; anything larger is somebody's ISO that happened to be named
// .txt, and reading it would be a self-inflicted denial of service.
const maxIntakeSize = 8 << 20

// Job is one entry of an intake file: the links, and everything else that entry
// asked for which this app can carry out.
//
// A zero field means "the file said nothing about it" and the app's own setting
// decides. Two fields are shaped against that rule on purpose and say why on
// themselves: Disabled, because false has to mean "on", and Priority, because
// zero is a priority somebody may have meant.
type Job struct {
	URLs    []string
	Package string
	// Dir is the destination override (crawljob downloadFolder), empty for none.
	Dir string
	// Password is the first of Passwords, and it is a separate field because a
	// task carries exactly one: this is the one written onto the tasks this job
	// creates, and the rest are only worth keeping for a later archive.
	Password string
	// Passwords is every archive password the entry listed, in the order it
	// listed them.
	Passwords []string
	// AutoStart starts the links instead of leaving them staged.
	AutoStart bool
	// Forced starts them ahead of the configured limits (crawljob forcedStart).
	// A forced start is still a start, so it implies AutoStart rather than
	// meaning something a caller has to combine by hand.
	Forced bool
	// Disabled parks the links: they are added, they keep their place, and
	// everything that starts downloads passes over them.
	//
	// It is inverted from the crawljob key it comes from, which is `enabled`. A
	// bool's zero value is false, so spelled the same way round, a Job built
	// anywhere else in the tree - and every entry of a file that never mentions
	// the key - would arrive parked. A queue that silently refuses to run is the
	// bug nobody traces back to the file they dropped.
	Disabled bool
	// Priority is one of the seven values the queue orders by, and nil when the
	// entry named none. A plain int cannot say that: zero is the default
	// priority, so an entry that is silent about it would overwrite a priority a
	// Packagizer rule had just set.
	Priority *int
	// Chunks is how many connections one of these downloads opens. Zero is "no
	// opinion", which is not "no connections".
	Chunks int
	// Comment is the note carried onto the row. Nothing acts on it; it is for the
	// person who comes back to the list next month.
	Comment string
	// Filename is the name the finished file is put under. It describes one file,
	// so it is only usable on an entry that carries one link - see the caller.
	Filename string
	// Extract overrides the global unpacking switch for these links, nil when the
	// entry said nothing. It is a pointer for the same reason the task field is:
	// a rule that deliberately switches unpacking off has to survive a global
	// that is on, and with a plain bool the two are the same value.
	Extract *bool
}

// schemeURL matches anything carrying a scheme. The intake stays deliberately
// permissive: the resolvers know what they can take, this package does not, and
// silently dropping a link the user explicitly handed us is the worse failure.
var schemeURL = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*://\S+$`)

// Parse reads one intake file into the jobs it holds. Two formats are accepted:
//
//	*.crawljob - JDownloader's key=value format, one entry per blank-line-
//	             separated block
//	*.txt      - one URL per line, always a single job
//
// The package name defaults to the file's base name, so dropping "Season 3.txt"
// produces a package called "Season 3" without the user configuring anything.
func Parse(name string, r io.Reader) ([]Job, error) {
	base := filepath.Base(name)
	var (
		jobs []Job
		err  error
	)
	switch strings.ToLower(filepath.Ext(base)) {
	case ".crawljob":
		jobs, err = parseCrawljob(r)
	case ".txt":
		jobs, err = parseText(r)
	default:
		return nil, fmt.Errorf("watch: %s: not an intake file (want .crawljob or .txt)", base)
	}
	if err != nil {
		return nil, fmt.Errorf("watch: %s: %w", base, err)
	}
	fallback := strings.TrimSuffix(base, filepath.Ext(base))
	out := jobs[:0]
	for _, j := range jobs {
		// An entry that set a package or a folder but carries no link is dropped
		// rather than failing the file: the other entries are still somebody's
		// link list, and refusing all of them over one stray block would retire
		// nothing and report nothing usable.
		if len(j.URLs) == 0 {
			continue
		}
		if j.Package == "" {
			j.Package = fallback
		}
		out = append(out, j)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("watch: %s: no links found", base)
	}
	return out, nil
}

// parseCrawljob reads JD's key=value format.
//
// A blank line ends an entry. JD's folder watch takes several jobs in one file
// that way, and folding them into one is a real loss now that an entry carries
// more than links: every link in the file would get the last block's package,
// folder and password.
//
// WHAT IS READ AND WHAT IS NOT. Everything below the switch is a key JD writes
// that this app has nothing to do with, listed rather than left to be
// rediscovered by whoever next wonders why their file had no effect:
//
//	downloadPassword            the hoster's own password for the link, not the
//	                            archive's. No resolver here takes a per-link
//	                            secret, so there is nowhere to put it.
//	deepAnalyseEnabled          crawling a page for the files it links to is one
//	                            global switch (Settings.Crawl), not a per-job one.
//	overwritePackagizerEnabled  which of the file and the Packagizer wins. Here
//	setBeforePackagizerEnabled  the order is fixed and the file always wins: the
//	                            Packagizer names the package as the link is
//	                            staged and the file's values are written over it
//	                            afterwards.
//	autoConfirm                 JD moves links from the LinkGrabber into the
//	autoConfirmDelay            download list. There is one list here, so a
//	                            staged link is already confirmed and autoStart is
//	                            the only distinction left.
//	addOfflineLink              availability is decided by the checker after
//	                            staging and nothing is dropped for being offline,
//	                            so there is no decision to take at intake.
//
// Any other key is ignored in the same spirit as those: a file we cannot fully
// understand is still a file whose links we can take.
func parseCrawljob(r io.Reader) ([]Job, error) {
	var (
		jobs []Job
		cur  Job
		open bool
	)
	flush := func() {
		if open {
			jobs = append(jobs, cur)
		}
		cur, open = Job{}, false
	}
	err := scanLines(r, func(line string) {
		if line == "" {
			flush()
			return
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return
		}
		open = true
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "text":
			cur.URLs = append(cur.URLs, splitLinks(value)...)
		case "packagename":
			cur.Package = value
		case "downloadfolder":
			cur.Dir = value
		case "autostart":
			if v, ok := boolValue(value); ok {
				cur.AutoStart = v
			}
		case "forcedstart":
			if v, ok := boolValue(value); ok {
				cur.Forced = v
			}
		case "enabled":
			// Only an explicit no parks the links. UNSET, and any spelling we do
			// not recognise, has to leave them runnable.
			if v, ok := boolValue(value); ok && !v {
				cur.Disabled = true
			}
		case "extractpasswords":
			cur.Passwords = passwordList(value)
			if len(cur.Passwords) > 0 {
				cur.Password = cur.Passwords[0]
			}
		case "extractafterdownload":
			if v, ok := boolValue(value); ok {
				cur.Extract = &v
			}
		case "priority":
			if v, ok := priorityValue(value); ok {
				cur.Priority = &v
			}
		case "chunks":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				cur.Chunks = n
			}
		case "comment":
			cur.Comment = value
		case "filename":
			cur.Filename = value
		}
	})
	flush()
	return jobs, err
}

// parseText reads a plain list, one URL per line, as a single job. The format
// carries nothing but links, so everything else on the Job stays at the value
// that means "the app's own settings decide".
func parseText(r io.Reader) ([]Job, error) {
	var job Job
	err := scanLines(r, func(line string) {
		// Split on whitespace rather than taking the whole line: a list pasted
		// onto one line is the normal shape of a copied link block, and the
		// crawljob parser already treats its value that way. Two parsers in one
		// package disagreeing about what a list looks like is how a drop file
		// ends up permanently "unusable" with nothing to explain it.
		job.URLs = append(job.URLs, splitLinks(line)...)
	})
	return []Job{job}, err
}

// bom is what Windows editors put at the front of a UTF-8 file. Left in place
// it becomes part of the first line, so the first link silently fails to parse
// while the rest succeed — and the file is then retired as consumed, so nothing
// ever reports the loss. Written as the escape, because the literal character
// is invisible in an editor and a source file that carries one is a source file
// somebody deletes by accident.
const bom = "\ufeff"

// scanLines feeds every line to fn, trimmed, with the BOM taken off the first
// one and comment lines dropped.
//
// A blank line is passed through rather than skipped, because for a crawljob it
// is the boundary between two entries and only the caller knows whether that
// matters. parseText throws them away, which is what "one URL per line" means.
func scanLines(r io.Reader, fn func(string)) error {
	first := true
	sc := bufio.NewScanner(io.LimitReader(r, maxIntakeSize))
	// A single crawljob text= value can hold hundreds of links on one line,
	// which blows straight past Scanner's 64 KiB default.
	sc.Buffer(make([]byte, 0, 64*1024), maxIntakeSize)
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, bom)
			first = false
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		fn(line)
	}
	return sc.Err()
}

// splitLinks pulls the URLs out of one crawljob text= value. JD writes multiple
// links into that single line as the literal two-character sequence \n, so the
// escape has to be undone before splitting on real whitespace.
func splitLinks(s string) []string {
	s = strings.NewReplacer(`\r\n`, "\n", `\n`, "\n", `\r`, "\n").Replace(s)
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ' ' || r == '\t'
	}) {
		if looksLikeURL(f) {
			out = append(out, f)
		}
	}
	return out
}

func looksLikeURL(s string) bool {
	// Magnet links carry no "//" after the colon, so they need their own case.
	return schemeURL.MatchString(s) || strings.HasPrefix(strings.ToLower(s), "magnet:?")
}

// boolValue reads JD's tri-state booleans, which are TRUE / FALSE / UNSET. The
// second result is whether the value said anything at all: UNSET and a spelling
// we do not know are both "no opinion", and a caller that cannot tell those from
// an explicit FALSE turns every silent key into a switched-off one.
func boolValue(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}

// priorityValue maps what a crawljob may put in priority= onto the seven values
// the queue orders by. JD writes the names; a hand-written file may well carry
// the number, and refusing that would be pedantry.
//
// The bounds come from the rules package rather than being spelled again here:
// it is the same seven values a Packagizer rule may set, and a second copy of
// the range is the copy that is forgotten when the enum grows.
func priorityValue(v string) (int, bool) {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "HIGHEST":
		return rules.PriorityMax, true
	case "HIGHER":
		return 2, true
	case "HIGH":
		return 1, true
	case "DEFAULT":
		return 0, true
	case "LOW":
		return -1, true
	case "LOWER":
		return -2, true
	case "LOWEST":
		return rules.PriorityMin, true
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, false
	}
	// Clamped rather than refused: a file asking for more urgency than the queue
	// has is asking for the most it has, and dropping the key instead would leave
	// the links at a priority the file plainly did not want.
	if n < rules.PriorityMin {
		n = rules.PriorityMin
	}
	if n > rules.PriorityMax {
		n = rules.PriorityMax
	}
	return n, true
}

// passwordList reads extractPasswords, which JD writes either bare or as a
// JSON-ish list. A bare value is taken whole, commas and all: only the bracketed
// form is a list, and splitting the bare one would cut a perfectly good password
// in half at the first comma.
func passwordList(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if !strings.HasPrefix(v, "[") {
		return []string{unquote(v)}
	}
	v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := unquote(strings.TrimSpace(part)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func unquote(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"'`)
}
