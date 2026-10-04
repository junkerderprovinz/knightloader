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
	"os"
	"path/filepath"
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
	// A file-system error's sentence is mostly its path, and a file name can
	// hold any of the words below.
	if _, ok := localPath(f.err); ok {
		return core.ReasonUnknown
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
	// A syscall.Errno implements net.Error as well, so a file the system
	// refused would otherwise read as a server that could not be reached.
	if _, ok := localPath(err); ok {
		return core.ReasonUnknown
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
	{"closed the connection", core.ReasonNetwork}, // the download engine
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
	code, params := codeFor(f, t.Reason)
	t.SetError(f.sentence(), code, params)
}

func (f failure) sentence() string {
	if f.text == "" && f.err != nil {
		return f.err.Error()
	}
	return f.text
}

// codeFor is the code a failure of reason r reads as, and the values its
// wording needs. Within a reason it can be finer, a timeout rather than just
// the network, but never a code of another reason, so the badge and the
// sentence agree. A failure with no reason gets a code only where its error
// value or its sentence is unmistakable, and "" else.
func codeFor(f failure, r core.Reason) (core.ErrorCode, map[string]string) {
	low := strings.ToLower(f.sentence())
	switch r {
	case core.ReasonNetwork:
		if f.status == 408 || timedOut(f.err, low) {
			return core.CodeTimeout, nil
		}
		if strings.Contains(low, "closed the connection") {
			return core.CodeConnectionClosed, nil
		}
	case core.ReasonAuth:
		if containsAny(low, premiumPhrases) {
			return core.CodePremiumNeeded, nil
		}
	case core.ReasonUnknown:
		if code, params := fileCode(f.err); code != "" {
			return code, params
		}
		switch {
		case containsAny(low, premiumPhrases):
			return core.CodePremiumNeeded, nil
		case containsAny(low, refusedPhrases):
			return core.CodeNoPermission, nil
		}
	}
	return r.Code(), nil
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

// refusedPhrases are the file system turning KnightLoader down, in the words a
// backend reports over the update channel. "access is denied" is Windows'
// wording, and only read when no HTTP status already made the failure an auth
// one.
var refusedPhrases = []string{"permission denied", "access is denied", "read-only file system"}

// localPath is the file or folder a file-system error is about, and whether
// err is one at all.
func localPath(err error) (string, bool) {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Path, true
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.New, true
	}
	return "", false
}

// fileCode is the code of a failure the file system reported, with the path
// it names, or "" for any other failure.
func fileCode(err error) (core.ErrorCode, map[string]string) {
	path, local := localPath(err)
	var params map[string]string
	if path != "" {
		params = map[string]string{"path": path}
	}
	switch {
	case errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS):
		return core.CodeNoPermission, params
	case local:
		return core.CodeLocalFile, params
	}
	return "", nil
}

func containsAny(low string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// diskCode is the code of a failure the disk or the file system reported, with
// the values its wording needs, or "" for any other failure. It is for the
// steps after a download, renaming, moving and unpacking, whose failures carry
// no Reason: the download itself finished.
func diskCode(err error) (core.ErrorCode, map[string]string) {
	if isDiskFull(err) {
		return core.CodeDiskFull, nil
	}
	return fileCode(err)
}

// unpackCode is the code an unpacking failure reads as, and the values its
// wording needs. The archive's own problems go first: a missing volume is a
// file-system error too, and naming it as a part says more.
func unpackCode(err error) (core.ErrorCode, map[string]string) {
	code, params := archiveCode(err)
	if code == "" {
		code, params = diskCode(err)
	}
	if part := extract.PartOf(err); code != "" && part != "" {
		if params == nil {
			params = map[string]string{}
		}
		params["part"] = part
	}
	return code, params
}

func archiveCode(err error) (core.ErrorCode, map[string]string) {
	var taken *extract.DestinationTakenError
	switch {
	case errors.Is(err, extract.ErrPasswordRequired):
		return core.CodeArchivePassword, nil
	case errors.Is(err, extract.ErrDamaged):
		return core.CodeArchiveDamaged, nil
	case errors.Is(err, extract.ErrPartMissing):
		return core.CodeArchivePartMissing, nil
	case errors.Is(err, extract.ErrUnsupported):
		return core.CodeArchiveUnsupported, nil
	case errors.As(err, &taken):
		return core.CodeArchiveFolderExists, map[string]string{"folder": filepath.Base(taken.Dir)}
	}
	return "", nil
}
