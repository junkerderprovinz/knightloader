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

// Downloader is the byte-transfer backend a WebDAV link is handed on to, the
// embedded engine in production.
type Downloader interface {
	Download(taskID, url string, headers map[string]string, conns int)
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// partSuffix marks a file that is still arriving, so no media server or
// backup takes half a file for the whole one. The part file's length is also
// the offset a resume continues from.
const partSuffix = ".klpart"

// progressEvery is how often a running transfer reports itself; every report
// takes the app's lock and reaches every open browser.
const progressEvery = 500 * time.Millisecond

// copyBuffer is the read size and the speed limiter's burst: large enough for
// a fast NAS, small enough that a pause stops within a few hundred kilobytes.
const copyBuffer = 256 << 10

// Backend fetches what the engine cannot. WebDAV targets resolve to plain
// https URLs and go to the engine (see Resolver.Resolve); FTP, FTPS and SFTP
// are downloaded here as a single stream with resume.
type Backend struct {
	accounts Accounts
	dialer   Dialer
	eng      Downloader
	dir      string

	onUpdate func(taskID string, u core.Update)

	// Dir returns the destination for one task; nil or "" falls back to the
	// backend's own directory. It is asked per task so a settings change
	// applies to the next download.
	Dir func(taskID string) string
	// RateLimit returns the speed limit in force, in bytes per second, 0 for
	// none. It is read once per buffer so a scheduled limit applies to a
	// running transfer, and it is the only limit these non-HTTP transfers see.
	RateLimit func() int64

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	link   map[string]string
	part   map[string]string
	// engineTasks are the tasks handed to the engine (WebDAV), so Pause,
	// Resume and Remove reach whoever holds the transfer.
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
		// run measures the part file and continues from there.
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
	// The part file always goes, or a later attempt at the same link would
	// resume from it; the finished file only with deleteFiles.
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

	// Download sends every http(s) link to the engine, so the flag cannot
	// matter here.
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
	// collide.SafeName gives the same name the engine would write and keeps a
	// server-supplied "../../etc/passwd" inside the download directory.
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
	// A part file longer than the remote file means the server's copy changed
	// during a pause, so the download starts over.
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
				// Paused or removed; the app has already written the status.
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

	// Neither the FTP nor the SFTP library takes a context per read, so a
	// cancellation closes the stream under a blocked Read.
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
	// Closed before returning, so a resume measures a flushed part file.
	if err := f.Close(); err != nil && copyErr == nil {
		copyErr = fmt.Errorf("remotefs: %w", err)
	}
	if copyErr != nil {
		return copyErr
	}
	// A stream that ends early without an error must not pass as finished.
	if size > 0 && written != size {
		return fmt.Errorf("remotefs: %s ended after %d of %d bytes", remotePath, written, size)
	}
	return nil
}

// copy is io.Copy with periodic progress reports, the current speed limit and
// cancellation.
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
				// Rebuilt only when the limit changes. The burst is the buffer
				// size because WaitN fails for n above the burst.
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
		// Also checked between reads, for a read that returned nothing.
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
// it ended up with. collide.Handover reserves the name, so two downloads
// finishing at once cannot both pick "film (2).mkv". Delegated backends never
// receive the configured collision policy (see app.HonoursCollisionPolicy),
// and Rename neither destroys an existing file nor stalls the queue.
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
