package app

// Every task that settles in StatusError is classified here, so the interface
// can act on a typed reason instead of matching on backend- and locale-specific
// wording, and gets the code it words the failure by (core.ErrorCode). Anything
// not recognised is core.ReasonUnknown: a missing label costs the user nothing,
// while a wrong one ("the file is gone") gets a working link deleted.

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/extract"
)

// failure is what is known about one task when it settles. Every field is
// optional: the dispatcher has an error, a backend only a sentence, and the
// availability probe only a status code.
type failure struct {
	err error
	// text is the sentence, when that is all there is. Empty means err.Error().
	text string
	// status is the HTTP status the caller saw, or 0 when there was no response.
	status int
}

// classify names the cause of a failure, or returns core.ReasonUnknown.
func classify(f failure) core.Reason {
	// The error value first, since Windows reports errors in its install
	// language and the text matches below fail on a German system.
	if r := classifyErr(f.err); r != core.ReasonUnknown {
		return r
	}
	text := f.text
	if text == "" && f.err != nil {
		text = f.err.Error()
	}
	status := f.status
	if status == 0 {
		status = statusIn(text)
	}
	if r := reasonForStatus(status); r != core.ReasonUnknown {
		return r
	}
	return classifyText(text)
}

// classifyErr answers from the error value alone.
func classifyErr(err error) core.Reason {
	if err == nil {
		return core.ReasonUnknown
	}
	switch {
	case isDiskFull(err):
		return core.ReasonDiskFull
	case errors.Is(err, context.Canceled):
		return core.ReasonCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return core.ReasonNetwork
	}
	// *net.OpError, *net.DNSError and the *url.Error wrapping them all
	// implement net.Error, and all mean the transport gave up.
	var ne net.Error
	if errors.As(err, &ne) {
		return core.ReasonNetwork
	}
	return core.ReasonUnknown
}

// Windows error numbers for a full disk. Go's Windows syscall.ENOSPC is a
// synthetic value no call returns, so errors.Is(err, syscall.ENOSPC) never
// matches there. They are checked only on Windows because 112 is EHOSTDOWN on
// Linux.
const (
	winDiskFull       syscall.Errno = 112 // ERROR_DISK_FULL
	winHandleDiskFull syscall.Errno = 39  // ERROR_HANDLE_DISK_FULL
)

func isDiskFull(err error) bool {
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	return runtime.GOOS == "windows" &&
		(errors.Is(err, winDiskFull) || errors.Is(err, winHandleDiskFull))
}

// reasonForStatus maps the HTTP statuses that have one specific meaning; the
// rest fall through to the text.
func reasonForStatus(code int) core.Reason {
	switch code {
	case 404, 410:
		return core.ReasonGone
	case 401, 403, 407: // 407 is a proxy that wants credentials
		return core.ReasonAuth
	case 429, 509: // 509 is the bandwidth-limit code file hosters send
		return core.ReasonLimit
	case 408:
		return core.ReasonNetwork
	case 502, 503, 504:
		return core.ReasonUnavailable
	}
	return core.ReasonUnknown
}

// statusPattern finds an HTTP status inside a failure sentence, since backends
// report over the update channel as text. It covers "jd /downloads: HTTP 403",
// Gopeed's "http request fail, code:404" and "retries=3, status=503".
var statusPattern = regexp.MustCompile(`(?i)\b(?:http|code|status)[ :=/]+([1-5][0-9]{2})\b`)

func statusIn(text string) int {
	m := statusPattern.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// textReasons are the phrases a failure sentence is matched against, in order.
// Each is wording this build actually receives. "not found" is left out
// because it also matches the local "no such file or directory", and a write
// failure filed as a dead link gets a good link deleted.
var textReasons = []struct {
	phrase string
	reason core.Reason
}{
	// Disk full comes first: its sentence often also contains a phrase further
	// down ("write ...: no space left on device").
	{"no space left on device", core.ReasonDiskFull},
	{"not enough space on the disk", core.ReasonDiskFull},
	{"disk full", core.ReasonDiskFull},
	{"captcha", core.ReasonCaptcha},
	{"unsupported url", core.ReasonUnsupported},      // yt-dlp
	{"unsupported protocol", core.ReasonUnsupported}, // the download library
	{"context canceled", core.ReasonCancelled},
	{"no such host", core.ReasonNetwork},
	{"connection refused", core.ReasonNetwork},
	{"connection reset", core.ReasonNetwork},
	{"network is unreachable", core.ReasonNetwork},
	{"deadline exceeded", core.ReasonNetwork},
	{"timeout", core.ReasonNetwork},
	{"too many requests", core.ReasonLimit},
	{"traffic limit", core.ReasonLimit},
	{"quota exceeded", core.ReasonLimit},
	{"unauthorized", core.ReasonAuth},
	{"forbidden", core.ReasonAuth},
	{"temporarily unavailable", core.ReasonUnavailable},
	{"service unavailable", core.ReasonUnavailable},
}

// addressMayHelp reports whether a new public address could change the
// outcome. It only ever holds a reconnect back, so an unclassified failure
// answers yes. A bot check is vetoed too: the flag is on the address, but a
// reconnect takes the whole household offline for a gamble, while a stored
// cookie jar works from the current address.
func addressMayHelp(r core.Reason) bool {
	switch r {
	case core.ReasonGone, core.ReasonAuth, core.ReasonDiskFull,
		core.ReasonUnsupported, core.ReasonCaptcha, core.ReasonCancelled,
		core.ReasonBotCheck, core.ReasonMembersOnly, core.ReasonGeoBlocked,
		core.ReasonDRM, core.ReasonExtractorBroken, core.ReasonUnsupportedPlayer:
		return false
	}
	return true
}

// retryCannotHelp reports whether another attempt is wasted, for the causes a
// backend names itself (core.Update.Reason). It lists the hopeless reasons so
// that a new reason keeps its retries by default.
func retryCannotHelp(r core.Reason) bool {
	switch r {
	case core.ReasonBotCheck, core.ReasonMembersOnly, core.ReasonGeoBlocked,
		core.ReasonDRM, core.ReasonExtractorBroken, core.ReasonUnsupportedPlayer:
		return true
	}
	return false
}

func classifyText(text string) core.Reason {
	if text == "" {
		return core.ReasonUnknown
	}
	low := strings.ToLower(text)
	for _, c := range textReasons {
		if strings.Contains(low, c.phrase) {
			return c.reason
		}
	}
	return core.ReasonUnknown
}

// recordFailure settles a classified failure on t: its sentence, its typed
// cause and the code the interface words it by.
func recordFailure(t *core.Task, f failure) {
	t.Reason = classify(f)
	t.SetError(f.sentence(), codeFor(f, t.Reason), nil)
}

func (f failure) sentence() string {
	if f.text == "" && f.err != nil {
		return f.err.Error()
	}
	return f.text
}

// codeFor is the code a failure of reason r reads as. Within a reason it can
// be finer, a timeout rather than just the network, but never a code of
// another reason, so the badge and the sentence agree. A failure with no
// reason gets a code only where its sentence is unmistakable, and "" else.
func codeFor(f failure, r core.Reason) core.ErrorCode {
	low := strings.ToLower(f.sentence())
	switch r {
	case core.ReasonNetwork:
		if f.status == 408 || timedOut(f.err, low) {
			return core.CodeTimeout
		}
	case core.ReasonAuth:
		if containsAny(low, premiumPhrases) {
			return core.CodePremiumNeeded
		}
	case core.ReasonUnknown:
		switch {
		case containsAny(low, premiumPhrases):
			return core.CodePremiumNeeded
		case writeRefused(f.err, low):
			return core.CodeNoPermission
		}
	}
	return r.Code()
}

// premiumPhrases are how hosters, JDownloader and the debrid services say a
// file needs a paid account. Not the bare word: "premiumize" is a service, and
// its every error starts with its name.
var premiumPhrases = []string{
	"premium account", "premium plan", "premium membership", "premium user",
	"premium only", "only for premium", "not premium", "premium add-on", "premium required",
	"requires premium",
}

func timedOut(err error, low string) bool {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return true
	}
	return containsAny(low, []string{"timeout", "timed out", "deadline exceeded"})
}

// writeRefused recognises the file system turning a write down. The sentence
// is checked too because a backend reports over the update channel as text;
// "access is denied" is Windows' wording, and only read when no HTTP status
// already made the failure an auth one.
func writeRefused(err error, low string) bool {
	return errors.Is(err, fs.ErrPermission) ||
		containsAny(low, []string{"permission denied", "access is denied", "read-only file system"})
}

func containsAny(low string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// diskCode is the code of a file the disk would not take, or "" for any other
// failure. It is for the steps after a download, renaming, moving and
// unpacking, whose failures carry no Reason: the download itself finished.
func diskCode(err error) core.ErrorCode {
	switch {
	case isDiskFull(err):
		return core.CodeDiskFull
	case errors.Is(err, fs.ErrPermission):
		return core.CodeNoPermission
	}
	return ""
}

// unpackCode is the code an unpacking failure reads as, and the values its
// wording needs.
func unpackCode(err error) (core.ErrorCode, map[string]string) {
	code := diskCode(err)
	if code == "" {
		code = archiveCode(err)
	}
	if part := extract.PartOf(err); code != "" && part != "" {
		return code, map[string]string{"part": part}
	}
	return code, nil
}

func archiveCode(err error) core.ErrorCode {
	switch {
	case errors.Is(err, extract.ErrPasswordRequired):
		return core.CodeArchivePassword
	case errors.Is(err, extract.ErrDamaged):
		return core.CodeArchiveDamaged
	case errors.Is(err, extract.ErrPartMissing):
		return core.CodeArchivePartMissing
	case errors.Is(err, extract.ErrUnsupported):
		return core.CodeArchiveUnsupported
	}
	return ""
}
