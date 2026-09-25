// Package usenet fetches an .nzb through a debrid service that has Usenet
// access, TorBox or Premiumize.me. The service downloads the articles, repairs
// and unpacks them in its own cloud, and hands back ordinary files, which the
// app then downloads with its own engine like any other link.
//
// This package owns the part in between: the queue of NZBs waiting for a
// service that will take them, the per-account submit limit, the polling until
// the service is done, and the resolver and backend for the file links that
// come out of it (usenet://torbox/<job>/<file>/<name>).
package usenet

import (
	"context"
	"errors"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// MaxNZBBytes caps an .nzb at every entrance. An NZB lists each article of a
// release at about 100 bytes per 700 KB, so this covers a release of about
// 450 GB, a 4K season pack included.
const MaxNZBBytes = 64 << 20

// Service is one account at a service that takes NZBs.
type Service interface {
	// Slot is the account's resolver slot, "torbox" or "torbox#work" (see
	// resolver.SlotID), which the file links carry so they reach the account
	// the job was sent to.
	Slot() string
	// Label is the service's name as a person reads it.
	Label() string
	// SubmitsPerHour is how many NZBs the account may send in an hour, 0 when
	// the service names no limit.
	SubmitsPerHour() int
	// Submit hands the service an NZB and returns its id for the job.
	Submit(ctx context.Context, name string, nzb []byte) (string, error)
	// Status reports how far the service has got with each of the jobs ids,
	// in as few calls as it allows: one call per job and round would use up
	// the account's request limit. A job missing from the answer is one the
	// service no longer has.
	Status(ctx context.Context, ids []string) (map[string]Status, error)
	// Link returns a download address for one file of a finished job. It may
	// expire, so it is asked for when the download starts and again when the
	// address stops working.
	Link(ctx context.Context, id, fileID string) (string, error)
	// Delete removes the job and its files from the account.
	Delete(ctx context.Context, id string) error
}

// Phase is where a job stands at the service.
type Phase string

const (
	// PhaseFetching is a job the service is still downloading or unpacking.
	PhaseFetching Phase = "fetching"
	// PhaseReady is a job whose files can be downloaded.
	PhaseReady Phase = "ready"
	// PhaseFailed is a job the service gave up on.
	PhaseFailed Phase = "failed"
)

// Status is one reading of a job at the service.
type Status struct {
	Phase Phase
	// ID is the job's id at the service when it has changed since the job was
	// sent: TorBox gives a download it queued for a free slot an id of its own
	// once it starts.
	ID   string
	Name string
	// Size is the service's reading of the download in bytes, 0 when it names
	// none. Progress runs from 0 to 1, and Speed is in bytes per second.
	Size     int64
	Progress float64
	Speed    int64
	// Files is set once Phase is PhaseReady.
	Files []File
	// Reason is the service's explanation for PhaseFailed.
	Reason string
}

// File is one file of a finished job.
type File struct {
	ID   string
	Name string
	// Dir is the folder the file sits in inside the download, slash-separated
	// and empty for a file at the top.
	Dir  string
	Size int64
}

// ErrBusy is a service declining to take an NZB for a while, because the
// account hit its submit limit or its number of running jobs. The NZB waits
// and is offered again later.
var ErrBusy = errors.New("the service is not taking new jobs at the moment")

// ErrGone is a job the service has dropped: deleted on its website or expired
// there.
var ErrGone = errors.New("the service no longer has this job")

// ErrNoUsenet is an account whose plan does not include Usenet. It is passed
// over for a while, and no NZB waits for it.
var ErrNoUsenet = errors.New("this account's plan does not include Usenet")

// unreachable is a call that got no usable answer: the connection failed or
// the service answered with a server error. Trying again later may work,
// where a refusal of the NZB itself will not.
type unreachable struct{ err error }

func (u unreachable) Error() string { return u.err.Error() }
func (u unreachable) Unwrap() error { return u.err }

func temporary(err error) bool {
	var u unreachable
	return errors.As(err, &u)
}

// maxSniff is how much of a file IsNZB reads. The root element and the first
// file entry sit at the top of every NZB.
const maxSniff = 64 << 10

// IsNZB reports whether data is a real NZB: an XML document whose root is
// <nzb> and which lists at least one file. A DDL indexer's "nzb" that is
// really a list of links is not one.
func IsNZB(data []byte) bool {
	head := data
	if len(head) > maxSniff {
		head = head[:maxSniff]
	}
	s := strings.ToLower(string(head))
	i := strings.Index(s, "<nzb")
	if i < 0 || i+len("<nzb") >= len(s) {
		return false
	}
	// "<nzb" must be the whole element name, not the start of another one.
	switch s[i+len("<nzb")] {
	case '>', ' ', '\t', '\r', '\n':
	default:
		return false
	}
	return strings.Contains(s[i:], "<file")
}

var segmentBytes = regexp.MustCompile(`<segment\s[^>]*?\bbytes="(\d+)"`)

// NZBSize adds up the articles an NZB lists. That is the release's size before
// the service unpacks it, within a few percent, and stands in for the size a
// service does not report.
func NZBSize(data []byte) int64 {
	var total int64
	for _, m := range segmentBytes.FindAllSubmatch(data, -1) {
		n, err := strconv.ParseInt(string(m[1]), 10, 64)
		if err == nil {
			total += n
		}
	}
	return total
}

// withoutCommonDir drops the folders every file sits in. A release that
// unpacks into a folder of its own would otherwise land one folder deeper than
// its package's.
func withoutCommonDir(files []File) []File {
	if len(files) == 0 {
		return files
	}
	common := strings.Split(files[0].Dir, "/")
	for _, f := range files[1:] {
		parts := strings.Split(f.Dir, "/")
		n := 0
		for n < len(common) && n < len(parts) && common[n] == parts[n] {
			n++
		}
		common = common[:n]
	}
	if len(common) == 0 || common[0] == "" {
		return files
	}
	prefix := strings.Join(common, "/")
	out := make([]File, len(files))
	for i, f := range files {
		f.Dir = strings.TrimPrefix(strings.TrimPrefix(f.Dir, prefix), "/")
		out[i] = f
	}
	return out
}

// splitPath turns a slash-separated path inside a download into its folder and
// file name.
func splitPath(p string) (dir, name string) {
	p = strings.Trim(strings.ReplaceAll(p, `\`, "/"), "/")
	dir, name = path.Split(p)
	return strings.TrimSuffix(dir, "/"), name
}
