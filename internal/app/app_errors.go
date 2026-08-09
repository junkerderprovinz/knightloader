package app

// Turning a failure into a typed reason. Every place a task settles in
// StatusError comes through here, so the interface can act on the cause instead
// of matching on a sentence that changes with the backend, the hoster, and the
// language the operating system was installed in.
//
// One rule holds the whole file up: a failure nothing recognises is
// core.ReasonUnknown. The reason is what the interface turns into advice, and
// advice is acted on - "the file is gone" makes people delete a link that was
// only ever throttled. A missing label costs the user nothing; a confident wrong
// one costs them the download.

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

// failure is everything known about one settled task at the moment it settles,
// and every field is optional because the callers genuinely differ: the
// dispatcher holds a real error, a download backend hands the update channel a
// sentence and nothing else, and the availability probe has a status code and no
// error at all.
type failure struct {
	err error
	// text is the sentence, when that is all there is. Left empty it is taken
	// from err.
	text string
	// status is the HTTP status the caller saw, or 0 when there was no response.
	status int
}

// classify names the cause of a failure, or answers core.ReasonUnknown.
func classify(f failure) core.Reason {
	// The error value first: it is the only part of this that no wording can
	// spoil. Windows reports its errors in the language it was installed in, so
	// every match further down is one a German box fails.
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
	// Then the status, because it is the one thing in a sentence that nobody
	// paraphrases: 429 means the same whoever wrote the words around it.
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
	// net.Error covers the lot - *net.OpError, *net.DNSError and the *url.Error
	// an HTTP client wraps them in all implement it - and every one of them means
	// the transport gave up before the host had said anything.
	var ne net.Error
	if errors.As(err, &ne) {
		return core.ReasonNetwork
	}
	return core.ReasonUnknown
}

// The Windows numbers for a full disk, written out for the same reason the
// Winsock numbers in internal/proxycfg are: Go's Windows syscall package defines
// the POSIX names as synthetic APPLICATION_ERROR values that no call ever
// returns, so errors.Is(err, syscall.ENOSPC) is false for precisely the error it
// names. They are only consulted on Windows, because 112 is EHOSTDOWN on Linux
// and a host that is down is not a full disk - which is the sort of mix-up this
// whole file exists to avoid.
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

// reasonForStatus is the taxonomy for an HTTP status. Only the codes that carry
// one specific meaning are named; the rest fall through, because "some 4xx" is
// not something to give a user instructions about.
func reasonForStatus(code int) core.Reason {
	switch code {
	case 404, 410:
		return core.ReasonGone
	case 401, 403, 407: // 407 is a proxy in the way, and it wants credentials too
		return core.ReasonAuth
	case 429, 509: // 509 is the bandwidth-limit code file hosters actually send
		return core.ReasonLimit
	case 408:
		return core.ReasonNetwork
	case 502, 503, 504:
		return core.ReasonUnavailable
	}
	return core.ReasonUnknown
}

// statusPattern finds the HTTP status inside a sentence, because for most
// failures the sentence is all that survives: a backend reports over the update
// channel as a string, not as the response it came from. The three shapes are
// the ones this build actually produces - "jd /downloads: HTTP 403", Gopeed's
// "http request fail, code:404" and its per-connection "retries=3, status=503".
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
// Every entry is wording this build can actually receive - Go's own transport
// errors, the words yt-dlp and JDownloader print, a hoster API's message - and
// nothing is in here on the strength of "a host might phrase it like that".
//
// "not found" is the phrase deliberately left out. It is the wording of a dead
// link and of the local disk's "no such file or directory" alike, and filing a
// write failure as a dead link tells somebody to delete a link that is fine.
var textReasons = []struct {
	phrase string
	reason core.Reason
}{
	// A full disk leads, because its sentence usually carries a second clause
	// that matches something below it ("write ...: no space left on device" ends
	// up as a write error, an i/o error, a broken pipe), and it is the one cause
	// here whose remedy is not "wait and try again".
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

// addressMayHelp reports whether a new public address could plausibly change
// the outcome. It only ever holds a reconnect back, and never causes one: an
// unclassified failure answers yes, so nothing that reconnects today stops.
//
// Rebooting the router for a file that is gone, a password that is not
// accepted, or a disk with no room on it takes the connection away from
// everyone in the house and fixes none of them.
func addressMayHelp(r core.Reason) bool {
	switch r {
	case core.ReasonGone, core.ReasonAuth, core.ReasonDiskFull,
		core.ReasonUnsupported, core.ReasonCaptcha, core.ReasonCancelled:
		return false
	}
	return true
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
