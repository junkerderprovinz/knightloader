package core

// ErrorCode is Task.Error as a value, for an interface that says in the
// reader's language what went wrong and what to do about it.
//
// A code is finer than a Reason: the retry policy and the badges key on the
// Reason, the sentence on the code. A code set beside a Reason is always one of
// that Reason's own, so the two cannot contradict each other. Empty means
// nothing recognised the failure, and the interface shows a general sentence
// beside Error.
type ErrorCode string

// The interface has words for every code here (web/src/lib/taskError.ts,
// mobile/src/api/taskError.ts), and its tests read this list to hold it to that.
const (
	// CodeGone is a file the host says is not there any more. ReasonGone.
	CodeGone ErrorCode = "gone"
	// CodeAccessDenied is a host refusing the request: a 401, a 403, a proxy's
	// 407. ReasonAuth.
	CodeAccessDenied ErrorCode = "accessDenied"
	// CodePremiumNeeded is a host or service that only hands the file to a
	// paid account. ReasonAuth when the classifier saw a refusal, no Reason
	// when only the sentence says so.
	CodePremiumNeeded ErrorCode = "premiumNeeded"
	// CodeLimit is a used-up allowance. ReasonLimit.
	CodeLimit ErrorCode = "limit"
	// CodeUnavailable is a host that is up but says "not now". ReasonUnavailable.
	CodeUnavailable ErrorCode = "unavailable"
	// CodeUnreachable is a host or server that could not be reached at all.
	// ReasonNetwork.
	CodeUnreachable ErrorCode = "unreachable"
	// CodeTimeout is a host that stopped answering. ReasonNetwork.
	CodeTimeout ErrorCode = "timeout"
	// CodeDiskFull is the destination out of space, while downloading
	// (ReasonDiskFull) or while unpacking.
	CodeDiskFull ErrorCode = "diskFull"
	// CodeNoPermission is the file system refusing KnightLoader a file or
	// folder, while downloading, unpacking or running what a backend needs.
	// Params: "path", where the failure names it. No Reason.
	CodeNoPermission ErrorCode = "noPermission"
	// CodeLocalFile is any other failure the file system reports about a file
	// or folder on this machine, such as one that is not there. Params:
	// "path". No Reason.
	CodeLocalFile ErrorCode = "localFile"
	// CodeUnsupported is a link no backend here fetches, yt-dlp's
	// "Unsupported URL" included. ReasonUnsupported.
	CodeUnsupported ErrorCode = "unsupported"
	// CodeHostExcluded is a link some backend fetches, but the host rule for
	// its host switches every such backend off. Params: "host".
	// ReasonUnsupported.
	CodeHostExcluded ErrorCode = "hostExcluded"
	// CodeDebridRefused is a debrid service turning the link down with an
	// answer nothing more specific recognised. Params: "service". No Reason.
	CodeDebridRefused ErrorCode = "debridRefused"
	// CodePinned is a task pinned to a backend that cannot fetch it right now.
	// ReasonAuth or ReasonUnsupported, whichever the pin ran into.
	CodePinned ErrorCode = "pinned"
	// CodeFileExists is a file of the same name already in the folder while
	// the collision policy says to skip. Params: "file". No Reason.
	CodeFileExists ErrorCode = "fileExists"
	CodeCaptcha    ErrorCode = "captcha"   // ReasonCaptcha
	CodeCancelled  ErrorCode = "cancelled" // ReasonCancelled

	// The causes a backend names itself, one code each, as their Reasons.
	CodeBotCheck        ErrorCode = "botCheck"
	CodeMembersOnly     ErrorCode = "membersOnly"
	CodeGeoBlocked      ErrorCode = "geoBlocked"
	CodeDRM             ErrorCode = "drm"
	CodeExtractorBroken ErrorCode = "extractorBroken"

	// The unpacking codes carry "part", the file of the archive the failure
	// happened in, where it is known. None has a Reason: the download itself
	// finished.

	// CodeArchiveFolderExists is a folder of the archive's name already there
	// while the archive settings say to skip it. The archive is fine. Params:
	// "folder".
	CodeArchiveFolderExists ErrorCode = "archiveFolderExists"

	// CodeArchiveDamaged is an archive whose bytes are not what its own
	// headers and checksums say: a bad block header, a CRC error, a volume
	// that ends early.
	CodeArchiveDamaged ErrorCode = "archiveDamaged"
	// CodeArchivePassword is an encrypted archive none of the passwords open.
	CodeArchivePassword ErrorCode = "archivePassword"
	// CodeArchivePartMissing is a part of the set that is not where the
	// archive looks for it.
	CodeArchivePartMissing ErrorCode = "archivePartMissing"
	// CodeArchiveUnsupported is an archive this build cannot read.
	CodeArchiveUnsupported ErrorCode = "archiveUnsupported"
)

// Code is the code a failure of this reason reads as when nothing finer is
// known, and empty for ReasonUnknown.
func (r Reason) Code() ErrorCode {
	switch r {
	case ReasonGone:
		return CodeGone
	case ReasonAuth:
		return CodeAccessDenied
	case ReasonLimit:
		return CodeLimit
	case ReasonUnavailable:
		return CodeUnavailable
	case ReasonNetwork:
		return CodeUnreachable
	case ReasonDiskFull:
		return CodeDiskFull
	case ReasonUnsupported:
		return CodeUnsupported
	case ReasonCaptcha:
		return CodeCaptcha
	case ReasonCancelled:
		return CodeCancelled
	case ReasonBotCheck:
		return CodeBotCheck
	case ReasonMembersOnly:
		return CodeMembersOnly
	case ReasonGeoBlocked:
		return CodeGeoBlocked
	case ReasonDRM:
		return CodeDRM
	case ReasonExtractorBroken:
		return CodeExtractorBroken
	}
	return ""
}
