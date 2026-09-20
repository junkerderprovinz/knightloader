package app

// Every task that settles in StatusError is classified here, so the interface
// can act on a typed reason instead of matching on backend- and locale-specific
// wording. Anything not recognised is core.ReasonUnknown: a missing label costs
// the user nothing, while a wrong one ("the file is gone") gets a working link
// deleted.

import (
	"context"
	"errors"
	"net"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/junkerderprovinz/knightloader/internal/core"
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
		core.ReasonDRM, core.ReasonExtractorBroken:
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
		core.ReasonDRM, core.ReasonExtractorBroken:
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
