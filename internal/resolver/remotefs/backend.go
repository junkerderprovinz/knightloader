package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Downloader is the byte-transfer backend a WebDAV link is handed on to - the
// embedded engine, in production. Declared here rather than imported so this
// package does not depend on internal/engine for one method set, which is the
// same trade internal/resolver/debrid's own Downloader already makes.
type Downloader interface {
	Download(taskID, url string, headers map[string]string, conns int)
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// partSuffix marks a file that is still arriving.
//
// A download in progress MUST NOT sit at its final name. Half a file called
// season1.mkv is indistinguishable from the whole thing to every other program
// on the machine - a media server will index it, a backup will copy it, a user
// will open it and conclude the download is broken. It also gives resume
// something unambiguous to measure: the length of the part file is the offset
// to continue from, and there is no way to confuse it with a file that was
// finished earlier.
const partSuffix = ".klpart"

// progressEvery is how often a running transfer reports itself. Every read
// would be thousands of updates a second, each one taking the app's lock and
// broadcasting to every open browser.
const progressEvery = 500 * time.Millisecond

// copyBuffer is the read size, and also the burst the speed limiter is built
// with. Large enough that a fast local NAS is not spending its time in system
// calls, small enough that a paused download stops within a few hundred
// kilobytes rather than a few megabytes.
const copyBuffer = 256 << 10

// Backend fetches what the engine cannot.
//
// IT DELIBERATELY DOES NOT FETCH EVERYTHING IT COULD. A WebDAV target resolves
// to an ordinary https URL (see Resolver.Resolve), and that one is handed
// straight to the engine, which already has chunked range requests, the
// outbound connection picker, the collision policy and the speed limiter. What
// is left here is FTP, FTPS and SFTP - three protocols nothing else in the
// tree speaks - and for those this file is a single-stream downloader with
// resume, which is what those protocols actually offer.
type Backend struct {
	accounts Accounts
	dialer   Dialer
	eng      Downloader
	dir      string

	onUpdate func(taskID string, u core.Update)

	// Dir returns the destination for one task; nil, or an empty answer, falls
	// back to the backend's own directory. The same per-task closure shape
	// internal/resolver/ytdlp's backend uses, and for the same reason: a
	// settings change has to take effect on the next download rather than at
	// the next restart.
	Dir func(taskID string) string
	// RateLimit returns the speed limit in force right now, in bytes per
	// second, 0 for none. Read once per buffer rather than captured, because
	// the limit is what a nightly schedule writes and a download that started
	// at six in the evening must slow down at midnight without being
	// restarted.
	//
	// These bytes do not pass through internal/netproxy's meter - they are not
	// HTTP and never touch the engine - so this is the ONLY thing that makes a
	// speed limit true for an FTP or SFTP transfer.
	RateLimit func() int64

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	link   map[string]string
	part   map[string]string
	// engineTasks are the ones handed to the engine (WebDAV). Pause, Resume
	// and Remove have to reach whoever is actually holding the transfer, and
	// guessing from the URL again on every call would be one more place for
	// the two halves to disagree.
	engineTasks map[string]bool
}

func NewBackend(accounts Accounts, dialer Dialer, eng Downloader, dir string, onUpdate func(string, core.Update)) *Backend {
	return &Backend{
		accounts: accounts, dialer: dialer, eng: eng, dir: dir, onUpdate: onUpdate,
		cancel:      map[string]context.CancelFunc{},
		link:        map[string]string{},
		part:        map[string]string{},
		engineTasks: map[string]bool{},
	}
}

func (b *Backend) Download(taskID, link string, headers map[string]string, conns int) {
	if isHTTP(link) {
		b.mu.Lock()
		b.engineTasks[taskID] = true
		b.mu.Unlock()
		b.eng.Download(taskID, link, headers, conns)
		return
	}
	b.mu.Lock()
	b.link[taskID] = link
	delete(b.engineTasks, taskID)
	b.mu.Unlock()
	go b.run(taskID, link)
}

func (b *Backend) Pause(taskID string) {
	if b.viaEngine(taskID) {
		b.eng.Pause(taskID)
		return
	}
	b.mu.Lock()
	c := b.cancel[taskID]
	b.mu.Unlock()
	if c != nil {
		c()
	}
}

func (b *Backend) Resume(taskID string) {
	if b.viaEngine(taskID) {
		b.eng.Resume(taskID)
		return
	}
	b.mu.Lock()
	link := b.link[taskID]
	b.mu.Unlock()
	if link != "" {
		// The part file is what carries the progress across the pause, so
		// there is nothing else to restore: run measures it and continues.
		go b.run(taskID, link)
	}
}

func (b *Backend) Remove(taskID string, deleteFiles bool) {
	if b.viaEngine(taskID) {
		b.eng.Remove(taskID, deleteFiles)
		b.mu.Lock()
		delete(b.engineTasks, taskID)
		b.mu.Unlock()
		return
	}
	b.mu.Lock()
	if c, ok := b.cancel[taskID]; ok {
		c()
	}
	part := b.part[taskID]
	delete(b.link, taskID)
	delete(b.part, taskID)
	b.mu.Unlock()
	// The part file goes whether or not deleteFiles was asked for, and the
	// finished file only when it was. A half-finished .klpart is this
	// backend's own scratch space and belongs to a task that no longer exists;
	// leaving it behind means the next attempt at the same link resumes from
	// bytes nobody asked to keep.
	if part != "" {
		_ = os.Remove(part)
		if deleteFiles {
			_ = os.Remove(strings.TrimSuffix(part, partSuffix))
		}
	}
}

func (b *Backend) viaEngine(taskID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.engineTasks[taskID]
}

func isHTTP(link string) bool {
	l := strings.ToLower(link)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

func (b *Backend) fail(taskID string, err error) {
	b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: err.Error(), Speed: 0})
}

func (b *Backend) run(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel[taskID] = cancel
	b.mu.Unlock()
	defer func() {
		cancel()
		b.mu.Lock()
		delete(b.cancel, taskID)
		b.mu.Unlock()
	}()

	// httpsIsWebDAV is true here without consulting an account, and it costs
	// nothing: Download above has already sent every http(s) link to the
	// engine, so nothing that reaches this function can be one. The flag only
	// decides whether Parse refuses an https scheme outright, and refusing it
	// here would be refusing a link that cannot arrive.
	t, err := Parse(link, true)
	if err != nil {
		b.fail(taskID, err)
		return
	}
	login := Login{Username: t.User}
	if b.accounts != nil {
		if l, ok := b.accounts.Login(t.Host); ok {
			login = l
		}
	}

	dir := b.dir
	if b.Dir != nil {
		if d := b.Dir(taskID); d != "" {
			dir = d
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.fail(taskID, fmt.Errorf("remotefs: %w", err))
		return
	}
	// collide.SafeName rather than the server's name as given: it is the
	// download library's own rewrite, so a name this backend writes and a name
	// the engine would have written for the same task are the same name - and
	// a server that answers a listing with "../../etc/passwd" cannot name a
	// file outside the download directory.
	name := collide.SafeName(Name(t))
	part := filepath.Join(dir, name+partSuffix)
	b.mu.Lock()
	b.part[taskID] = part
	b.mu.Unlock()

	fs, err := b.dialer.Dial(ctx, t, login)
	if err != nil {
		b.fail(taskID, err)
		return
	}
	defer fs.Close()

	remote, err := fs.Stat(ctx, t.Path)
	if err != nil {
		b.fail(taskID, err)
		return
	}
	if remote.Dir {
		b.fail(taskID, fmt.Errorf("remotefs: %s is a folder, not a file", link))
		return
	}

	offset, err := partSize(part)
	if err != nil {
		b.fail(taskID, fmt.Errorf("remotefs: %w", err))
		return
	}
	// A part file longer than the file on the server means the server's copy
	// changed while this download was paused. Continuing from an offset past
	// the end would append nothing and then declare a truncated file finished,
	// so the part is thrown away and the download starts again - which loses
	// bytes that were, by then, bytes of a different file.
	if remote.Size > 0 && offset > remote.Size {
		if err := os.Remove(part); err != nil && !errors.Is(err, os.ErrNotExist) {
			b.fail(taskID, fmt.Errorf("remotefs: %w", err))
			return
		}
		offset = 0
	}

	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: name, Size: remote.Size, Loaded: offset})

	if remote.Size == 0 || offset < remote.Size {
		if err := b.transfer(ctx, taskID, fs, t.Path, part, offset, remote.Size); err != nil {
			if ctx.Err() != nil {
				// Paused or removed. The status is the app's to write (see
				// app.stop, which writes it before telling the backend), and a
				// second one from here would race it.
				return
			}
			b.fail(taskID, err)
			return
		}
	}

	final, err := b.finish(part, filepath.Join(dir, name))
	if err != nil {
		b.fail(taskID, err)
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusDone, Name: filepath.Base(final), Size: remote.Size, Loaded: remote.Size, Speed: 0})
}

// partSize is how many bytes of this download are already on disk. A missing
// part file is 0, which is the ordinary first attempt and not an error.
func partSize(part string) (int64, error) {
	fi, err := os.Stat(part)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// transfer streams the remote file into the part file from offset onwards.
func (b *Backend) transfer(ctx context.Context, taskID string, fs FS, remotePath, part string, offset, size int64) error {
	rc, err := fs.Open(ctx, remotePath, offset)
	if err != nil {
		return err
	}
	defer rc.Close()

	// A cancelled context has to break a Read that is already blocked on a
	// socket, and neither the FTP library nor SFTP takes a context per call -
	// so the stream is closed out from under the read, which is what makes
	// Pause take effect in milliseconds instead of at the next TCP timeout.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = rc.Close()
		case <-done:
		}
	}()

	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("remotefs: %w", err)
	}
	written, copyErr := b.copy(ctx, taskID, f, rc, offset, size)
	// Closed before the error is reported, not after: the bytes have to be on
	// disk before anything else measures the part file, and a resume that
	// starts from a length the operating system had not flushed yet is a hole
	// in the middle of the file.
	if err := f.Close(); err != nil && copyErr == nil {
		copyErr = fmt.Errorf("remotefs: %w", err)
	}
	if copyErr != nil {
		return copyErr
	}
	// A transfer that ended early with no error at all is the one failure a
	// downloader must never call success: the file would be renamed to its
	// final name, the task would go green, and the truncation would surface
	// weeks later in whatever tried to open it.
	if size > 0 && written != size {
		return fmt.Errorf("remotefs: %s ended after %d of %d bytes", remotePath, written, size)
	}
	return nil
}

// copy is io.Copy with three things bolted on that io.Copy cannot have: a
// progress report on a timer, the speed limit in force right now, and a
// cancellation that does not wait for the next read to return.
func (b *Backend) copy(ctx context.Context, taskID string, dst io.Writer, src io.Reader, start, size int64) (int64, error) {
	buf := make([]byte, copyBuffer)
	total := start
	var lim *rate.Limiter
	var limAt int64

	last := time.Now()
	lastBytes := total
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, fmt.Errorf("remotefs: %w", werr)
			}
			total += int64(n)
			if want := b.limit(); want > 0 {
				// Rebuilt only when the number actually changed, so the common
				// case (a constant limit, or none) allocates once per download
				// rather than once per buffer. The burst is the buffer size
				// because WaitN refuses outright to wait for more than the
				// burst it was built with.
				if lim == nil || limAt != want {
					limAt = want
					lim = rate.NewLimiter(rate.Limit(want), copyBuffer)
				}
				if err := lim.WaitN(ctx, n); err != nil {
					return total, err
				}
			} else {
				lim, limAt = nil, 0
			}
			if since := time.Since(last); since >= progressEvery {
				b.onUpdate(taskID, core.Update{
					Status: core.StatusRunning, Loaded: total, Size: size,
					Speed: int64(float64(total-lastBytes) / since.Seconds()),
				})
				last, lastBytes = time.Now(), total
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return total, nil
			}
			if ctx.Err() != nil {
				return total, ctx.Err()
			}
			return total, fmt.Errorf("remotefs: %w", rerr)
		}
		// Checked between reads as well, so a stalled server whose socket was
		// closed by the watcher above still ends promptly even if the read
		// returned nothing rather than an error.
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

func (b *Backend) limit() int64 {
	if b.RateLimit == nil {
		return 0
	}
	return b.RateLimit()
}

// finish moves the completed part file onto its real name and reports the name
// it ended up with.
//
// collide.Handover picks that name against a real reservation rather than a
// "does it exist" test, which is what keeps two downloads finishing in the
// same instant from both choosing "film (2).mkv". The policy is Rename and not
// the configured one, deliberately: a delegated backend never receives the
// collision policy (see app.HonoursCollisionPolicy, which answers false for
// every one of them), and Rename is the only policy that neither destroys a
// file somebody already had nor stalls a queue nobody is watching.
func (b *Backend) finish(part, target string) (string, error) {
	res, err := collide.Handover(target, collide.Rename)
	if err != nil {
		return "", fmt.Errorf("remotefs: %w", err)
	}
	if err := os.Rename(part, res.Path); err != nil {
		return "", fmt.Errorf("remotefs: %w", err)
	}
	return res.Path, nil
}
