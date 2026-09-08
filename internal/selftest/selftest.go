// Package selftest is the vocabulary one instance uses to describe itself to
// its own operator: seven checks, each answered with a typed status, a stable
// code, the substitutions that code's sentence needs, and - where somebody
// else did the talking - that other side's own untranslated words.
//
// WHY A PACKAGE AND NOT A STRUCT INSIDE internal/app. Two of the three things
// here are pure functions over strings and clocks (the yt-dlp version parser,
// the zone reading), and pure is exactly what makes them testable: a version
// parser that lives on *App can only be exercised by building an App, opening
// a store and mocking a process, which is three moving parts guarding one
// regexp. The third thing, the Result/Run vocabulary, is shared by the runner
// (internal/app) and the route (internal/api), and a type owned by one of
// those two would make the other import it for a reason that has nothing to do
// with what that package is for.
//
// WHAT IS DELIBERATELY NOT HERE: the checks themselves. Every one of them
// needs the App - the credentials, the folders, the JD address, the relay's
// live socket - so the runner lives in internal/app/app_selftest.go and this
// package never learns what an account is. That split is the same one
// internal/portmap and internal/proxycfg already keep: the vocabulary and the
// pure logic here, the wiring where the state is.
//
// THE INTERFACE PICKS THE WORDS. Nothing in this package produces a sentence
// for a person to read. It produces a Code, and the browser looks that code's
// sentence up in whichever of the forty-two locales is loaded - the same rule
// reconnect.ConfigProblem.Code and portmap's Reason constants already state,
// and for the same reason: the server has no idea which language is in front
// of the person who pressed the button. The one exception is Detail, which is
// somebody ELSE's sentence (a router's fault string, a provider's refusal, a
// Go transport error) and is passed through verbatim rather than being
// paraphrased into a code that would lose what it said.
package selftest

import "time"

// Status is how one check came out.
//
// FIVE VALUES, AND THE LAST TWO ARE THE POINT. "skipped" and "unknown" are
// different answers and collapsing them is the single easiest mistake in this
// whole feature - the same distinction internal/diskspace's second return
// value exists to keep, where "this platform cannot be asked" had to stay
// apart from "zero bytes free" or every guard in the app would stop a healthy
// machine.
//
//   - skipped: there is nothing configured here to check. The relay is
//     switched off; no debrid account exists; JD provisioning was opted out
//     of. Nothing is wrong and nothing needs doing.
//   - unknown: it IS configured, and this build cannot find out. The torrent
//     port is set and only a machine outside this network could say whether it
//     is reachable; the platform has no way to measure a volume. Something
//     might well be wrong and this instance is not the one that can say.
//
// Neither is ever rendered as the word "off". A relay somebody deliberately
// switched off is described as their own choice ("you have switched the relay
// off"), not labelled with a status word, because a status word invites the
// reading that the app decided something.
type Status string

const (
	// StatusPass is the check finding what it hoped to find.
	StatusPass Status = "pass"
	// StatusWarn is a real finding that is not stopping anything today: a
	// yt-dlp two months old, a download folder that does not exist yet, a
	// relay that is retrying. Worth a look, not worth an alarm.
	StatusWarn Status = "warn"
	// StatusFail is something that is not working now: a sidecar that will
	// not answer, a folder this process cannot write into, a login the
	// provider refused.
	StatusFail Status = "fail"
	// StatusSkipped is "nothing is configured here" - see the type's own
	// comment for why this must never merge with StatusUnknown.
	StatusSkipped Status = "skipped"
	// StatusUnknown is "configured, and this build cannot find out".
	StatusUnknown Status = "unknown"
)

// The seven check ids. They are stable strings rather than an enum because
// they cross the wire into a browser and back into a translation catalogue,
// and because Run.Planned is a list of them that the page draws as pending
// rows before a single result has landed.
const (
	// CheckJD is the headless JDownloader sidecar: configured, reachable,
	// which revision.
	CheckJD = "jd"
	// CheckYtdlp is the yt-dlp binary: present, which version, how old.
	CheckYtdlp = "ytdlp"
	// CheckFolders is the download and working folders: there, writable, how
	// much room, and whether the figures describe the folder that was asked
	// about or a parent of it.
	CheckFolders = "folders"
	// CheckAccounts is every configured debrid login, asked whether it still
	// works. The ONLY check that leaves this machine, and it only ever
	// reaches providers the operator set up themselves.
	CheckAccounts = "accounts"
	// CheckRelay is the relay connection as the relay client already knows
	// it. It dials nothing of its own - see the runner.
	CheckRelay = "relay"
	// CheckClock is this machine's clock and, far more usefully, its time
	// zone: a container with no TZ runs every timetable in UTC and nothing
	// has ever said so.
	CheckClock = "clock"
	// CheckTorrentPort is the torrent listen port. It reports what is
	// configured and says plainly that whether the port is open from outside
	// is not a question this instance can answer.
	CheckTorrentPort = "torrentPort"
)

// Order is the order the seven are reported in, and it is the order the page
// draws them: the two resolvers that a download actually depends on first,
// then the disk, then the credentials, then the three that are configuration
// rather than machinery. Run.Planned is filled from this, so a check added
// later appears in every pending list without the browser being taught about
// it.
var Order = []string{
	CheckJD,
	CheckYtdlp,
	CheckFolders,
	CheckAccounts,
	CheckRelay,
	CheckClock,
	CheckTorrentPort,
}

// Result is one check, or one row inside one check.
type Result struct {
	// ID is a check id from Order, or - inside Rows - the identifier of the
	// thing that row describes (a folder path, an account's metaKey). It is
	// never shown as-is: the page has the name in its own language.
	ID string `json:"id"`
	// Status is the verdict. See Status.
	Status Status `json:"status"`
	// Code is the stable name of the sentence to render, e.g. "ytdlp.old".
	// Prefixed with the check id so two checks can both have an "ok" without
	// the browser needing to know which check a code came from.
	Code string `json:"code"`
	// Params are that sentence's substitutions: {version}, {days}, {dir},
	// {measured}, {free}, {port}, {label}. Byte counts travel as decimal
	// strings of BYTES and are formatted by the browser's own fmtBytes, never
	// pre-formatted here - a server that writes "4,2 GB" has decided the
	// reader's language and their thousands separator.
	Params map[string]string `json:"params,omitempty"`
	// Detail is the OTHER side's own words: a Go error, a provider's refusal,
	// a router's fault string. English and untranslated on purpose, exactly
	// as portmap.Result.Detail is - the words did not come from this app, and
	// paraphrasing them into a code would throw away the only part of the
	// answer that names the actual problem.
	Detail string `json:"detail,omitempty"`
	// Rows is one level of nesting and no more: one row per debrid account,
	// one per folder. A tree would need a tree renderer, and nothing here has
	// ever wanted one - the parent carries the worst of its children's
	// statuses and the summary sentence, the children carry the specifics.
	Rows []Result `json:"rows,omitempty"`
	// At is when this result landed.
	At time.Time `json:"at"`
}

// Run is one sweep.
type Run struct {
	// ID identifies this sweep. The page holds it so that a result arriving
	// for a run it did not start - a second tab pressed the button - is
	// recognisable as such rather than being drawn as its own.
	ID string `json:"id"`
	// StartedAt is when the sweep began.
	StartedAt time.Time `json:"startedAt"`
	// FinishedAt is zero while the sweep is still going, which is exactly what
	// the page polls on: it asks again every second until this is set.
	// omitzero rather than omitempty, because time.Time is a struct and
	// omitempty has never done anything to one.
	FinishedAt time.Time `json:"finishedAt,omitzero"`
	// Planned is every check id this sweep will report, in Order. It is sent
	// with the very first answer so the page can draw all seven rows as
	// pending immediately - without it the list grows from nothing and the
	// reader cannot tell a check that has not run yet from one that is not
	// part of this build.
	Planned []string `json:"planned"`
	// Results are the checks that have landed, in Order. Never nil: a nil
	// slice encodes as JSON null and the page that walks it throws instead of
	// drawing an empty list, the same trap DiskReport.Volumes documents.
	Results []Result `json:"results"`
}

// Worst is the most severe status in a set, for a parent row that summarises
// its children.
//
// The ordering is by how much attention each deserves rather than by any
// natural order of the words: fail beats warn beats unknown beats skipped
// beats pass. unknown sits ABOVE skipped deliberately - a parent whose rows
// are half "not configured" and half "cannot find out" should read as the
// second, because the second is the one with an open question in it.
//
// An empty set is StatusSkipped: there was nothing to check, which is what a
// parent with no rows means every time it happens here (no debrid account, no
// measurable folder).
func Worst(statuses ...Status) Status {
	rank := map[Status]int{
		StatusPass:    0,
		StatusSkipped: 1,
		StatusUnknown: 2,
		StatusWarn:    3,
		StatusFail:    4,
	}
	worst := StatusSkipped
	best := -1
	for _, s := range statuses {
		r, ok := rank[s]
		if !ok {
			// A status this function has not been taught about. Left alone
			// rather than treated as the worst or the best: a value that is
			// not one of the five is a bug in whatever produced it, and
			// silently promoting it to "fail" would put an alarm in front of
			// somebody for a typo in a code path.
			continue
		}
		if r > best {
			best, worst = r, s
		}
	}
	return worst
}
