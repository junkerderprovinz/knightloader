package fortress

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
)

// expiryNotice is how long before its end an account is worth a reminder.
const expiryNotice = 7 * 24 * time.Hour

// retryGrace is how long a failed download whose retry time has passed still
// counts as about to be retried, as the overview's countdown has it.
const retryGrace = time.Minute

// Inputs is what the needs are read from, taken at one moment by the caller.
type Inputs struct {
	Now      time.Time
	Tasks    []*core.Task
	Captchas []captcha.Challenge
	Accounts []Account
	Logins   []hosterauth.LoginState
	Parts    []Part
	Volumes  []Volume
	// DiskLow and DiskStop are the two free-space floors in bytes, 0 for none.
	DiskLow, DiskStop int64
	Cap               Cap
	// Done is what the person set aside on the overview, by the keys it
	// stores there.
	Done map[string]bool
}

// Account is a debrid or other service account as the overview reads it.
type Account struct {
	ID, Name            string
	Enabled, Configured bool
	OK                  bool
	// Detail is the service's complaint, empty for an account nobody checked.
	Detail string
	// Expiry is RFC 3339 or empty.
	Expiry string
}

// Part is one row of the health report.
type Part struct {
	ID, State, Remedy string
	Since             time.Time
}

// Volume is one download folder and the room left on it.
type Volume struct {
	Dir   string
	Free  uint64
	Known bool
}

// Cap is the volume cap of the running period.
type Cap struct {
	Reached     bool
	PeriodStart time.Time
}

// need is one thing that waits for the person, before it is put into words.
// Its id is the key the overview uses for the same thing, so both agree on
// what is one item.
type need struct {
	id       string
	severity status.Severity
	code     string
	key      string
	args     map[string]string
	since    time.Time
	link     string
	// parts are the health part and state a health need names, which are
	// texts of their own.
	part, state string
}

func (n need) render(lang string) status.Need {
	text := say(lang, n.key, n.args)
	if n.part != "" {
		text = say(lang, "health.part."+n.part, nil) + " · " + say(lang, "health.state."+n.state, nil)
	}
	return status.Need{ID: n.id, Severity: n.severity, Code: n.code, Text: text, Since: n.since, Link: n.link}
}

// needs lists what waits for the person with the overview's rules: a captcha
// first, since it runs out, then what has failed, then what only warns. The
// web interface's needsOf in web/src/lib/needs.ts is the same list.
func needs(in Inputs) []need {
	var captchas, fails, warns []need
	add := func(n need) {
		if n.severity == status.SeverityError {
			fails = append(fails, n)
		} else {
			warns = append(warns, n)
		}
	}
	open := func(key string) bool { return !in.Done[key] }

	for _, c := range in.Captchas {
		if c.Test {
			continue
		}
		host := c.Host
		if host == "" {
			host = "?"
		}
		captchas = append(captchas, need{
			id: "captcha:" + c.ID, severity: status.SeverityWarn, code: "captcha.waiting",
			key: "overview.needs.captcha", args: map[string]string{"host": host}, link: "/",
		})
	}

	for _, n := range diskNeeds(in) {
		add(n)
	}

	var failed, unpacked []*core.Task
	for _, t := range in.Tasks {
		if failedForGood(t, in.Now) && open("task:"+t.ID) {
			failed = append(failed, t)
		}
		if unpackFailed(t) && open("unpack:"+t.ID) {
			unpacked = append(unpacked, t)
		}
	}
	for _, group := range byPackage(failed) {
		n := need{id: "failed:" + group[0].ID, severity: status.SeverityError, code: "download.failed", link: "/downloads", since: group[0].ChangedAt}
		if len(group) > 1 {
			n.key, n.args = "fortress.need.failedMany", map[string]string{"name": titleOf(group), "n": strconv.Itoa(len(group))}
		} else {
			n.key, n.args = "fortress.need.failed", map[string]string{"name": titleOf(group)}
		}
		add(n)
	}
	for _, group := range byPackage(unpacked) {
		n := need{id: "unpack:" + group[0].ID, severity: status.SeverityError, code: "unpack.failed",
			key: "fortress.need.unpack", args: map[string]string{"name": titleOf(group)}, link: "/downloads", since: group[0].ChangedAt}
		for _, t := range group {
			if t.Unpack == core.UnpackPassword {
				n.code, n.key = "unpack.password", "fortress.need.unpackPassword"
			}
		}
		add(n)
	}

	refused := 0
	for _, a := range in.Accounts {
		if !a.Enabled || !a.Configured {
			continue
		}
		if a.Detail != "" && !a.OK {
			refused++
			if key := "account:" + a.ID; open(key) {
				add(need{id: key, severity: status.SeverityError, code: "account.failing",
					key: "overview.needs.accountFailing", args: map[string]string{"name": a.Name}, link: "/accounts"})
			}
		} else if endsSoon(a.Expiry, in.Now) {
			if key := "expiry:" + a.ID + ":" + a.Expiry; open(key) {
				add(expiryNeed(key, a.Name, a.Expiry))
			}
		}
	}
	for _, l := range in.Logins {
		if !l.Enabled {
			continue
		}
		if l.Status == hosterauth.StatusRejected {
			refused++
			if key := "login:" + l.Host; open(key) {
				add(need{id: key, severity: status.SeverityError, code: "login.rejected",
					key: "overview.needs.loginRejected", args: map[string]string{"name": l.Host}, link: "/accounts"})
			}
		} else if endsSoon(l.Expiry, in.Now) {
			if key := "expiry:login:" + l.Host + ":" + l.Expiry; open(key) {
				add(expiryNeed(key, l.Host, l.Expiry))
			}
		}
	}

	if in.Cap.Reached {
		// The period is in the key, so "Done" lasts until the counter starts over.
		if key := "volumeCap:" + in.Cap.PeriodStart.Format(time.RFC3339Nano); open(key) {
			add(need{id: key, severity: status.SeverityWarn, code: "volume.cap",
				key: "fortress.need.volumeCap", since: in.Cap.PeriodStart, link: "/settings/downloads"})
		}
	}

	for _, p := range in.Parts {
		if (p.State != "failed" && p.State != "degraded") || p.Remedy == "" {
			continue
		}
		// The captchas and the folders are listed above by name, and a refused
		// account has its own item.
		if p.ID == "captcha" || p.Remedy == "disk.low" || (p.Remedy == "accounts.invalid" && refused > 0) {
			continue
		}
		key := "health:" + p.ID + ":" + p.Remedy
		if !open(key) {
			continue
		}
		severity := status.SeverityWarn
		if p.State == "failed" {
			severity = status.SeverityError
		}
		add(need{id: key, severity: severity, code: snake(p.Remedy), part: p.ID, state: p.State, since: p.Since, link: "/settings/health"})
	}

	return append(append(captchas, fails...), warns...)
}

func expiryNeed(key, name, expiry string) need {
	date := expiry
	if t, err := time.Parse(time.RFC3339, expiry); err == nil {
		date = t.Format(time.DateOnly)
	}
	return need{id: key, severity: status.SeverityWarn, code: "account.expiring",
		key: "overview.needs.accountExpires", args: map[string]string{"name": name, "date": date}, link: "/accounts"}
}

// diskNeeds holds each folder to the two floors. A folder listed under two
// roles is one folder.
func diskNeeds(in Inputs) []need {
	var out []need
	seen := map[string]bool{}
	for _, v := range in.Volumes {
		if !v.Known || seen[v.Dir] {
			continue
		}
		seen[v.Dir] = true
		stopped := in.DiskStop > 0 && v.Free < uint64(in.DiskStop)
		if !stopped && !(in.DiskLow > 0 && v.Free < uint64(in.DiskLow)) {
			continue
		}
		n := need{id: "disk:low:" + v.Dir, severity: status.SeverityWarn, code: "disk.low",
			key: "overview.needs.diskLow", args: map[string]string{"folder": v.Dir}, link: "/settings/downloads"}
		if stopped {
			n.id, n.severity, n.code = "disk:stop:"+v.Dir, status.SeverityError, "disk.stopped"
		}
		if !in.Done[n.id] {
			out = append(out, n)
		}
	}
	return out
}

// failedForGood is a failed download no retry is coming for.
func failedForGood(t *core.Task, now time.Time) bool {
	if t.Status != core.StatusError || !t.Enabled {
		return false
	}
	if t.GaveUp || t.NextTry.IsZero() {
		return true
	}
	return now.Sub(t.NextTry) > retryGrace
}

func unpackFailed(t *core.Task) bool {
	return t.Status == core.StatusDone && t.Enabled && (t.Unpack == core.UnpackFailed || t.Unpack == core.UnpackPassword)
}

// byPackage groups tasks as the lists do, in the order they come. A loose link
// is a group of its own.
func byPackage(tasks []*core.Task) [][]*core.Task {
	var groups [][]*core.Task
	at := map[string]int{}
	for _, t := range tasks {
		key := "t:" + t.ID
		if t.Package != "" {
			key = "p:" + t.Package
		}
		if i, ok := at[key]; ok {
			groups[i] = append(groups[i], t)
			continue
		}
		at[key] = len(groups)
		groups = append(groups, []*core.Task{t})
	}
	return groups
}

func titleOf(group []*core.Task) string {
	t := group[0]
	switch {
	case t.Package != "":
		return t.Package
	case t.Name != "":
		return t.Name
	}
	return t.URL
}

// snake writes a remedy id such as "jd.notWired" the way the contract spells
// codes, "jd.not_wired".
func snake(id string) string {
	var b strings.Builder
	for _, r := range id {
		if unicode.IsUpper(r) {
			b.WriteByte('_')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func endsSoon(expiry string, now time.Time) bool {
	at, err := time.Parse(time.RFC3339, expiry)
	return err == nil && at.After(now) && at.Sub(now) <= expiryNotice
}
