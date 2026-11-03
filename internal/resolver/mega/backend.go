package mega

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/filemode"
	"github.com/junkerderprovinz/knightloader/internal/reclaim"
)

// progressEvery is how often a running transfer reports itself; every report
// takes the app's lock and reaches every open browser.
const progressEvery = 500 * time.Millisecond

// copyBuffer is the read size and the speed limiter's burst.
const copyBuffer = 256 << 10

// quotaWait is how long a spent transfer quota is waited out when MEGA does
// not say. It usually comes back within a few hours, a little at a time.
const quotaWait = 30 * time.Minute

// ErrMAC is a download whose content does not match the MAC in its key.
var ErrMAC = errors.New("mega: the downloaded file failed MEGA's integrity check, so it was deleted; the next try starts over")

// Backend downloads MEGA files, decrypting them on the way to disk and
// checking the result against the MAC in the file's key. A paused download
// resumes from what its part file holds.
type Backend struct {
	client   *Client
	transfer *http.Client
	dir      string
	onUpdate func(taskID string, u core.Update)

	// Dir returns the destination for one task; nil or "" falls back to the
	// backend's own directory.
	Dir func(taskID string) string
	// RateLimit returns the speed limit in force, in bytes per second, 0 for
	// none. These bytes bypass the engine's metering proxy, so it is read once
	// per buffer here.
	RateLimit func() int64

	mu    sync.Mutex
	runs  map[string]*runState
	link  map[string]string
	part  map[string]string
	saved map[string]string
}

// NewBackend asks client for download addresses and fetches the bytes
// through transfer, which must not put a deadline on the whole request.
func NewBackend(client *Client, transfer *http.Client, dir string, onUpdate func(string, core.Update)) *Backend {
	return &Backend{
		client: client, transfer: transfer, dir: dir, onUpdate: onUpdate,
		runs:  map[string]*runState{},
		link:  map[string]string{},
		part:  map[string]string{},
		saved: map[string]string{},
	}
}

// Client is the API client the backend and its resolver share, so a folder
// listed while staging is not listed again for every file.
func (b *Backend) Client() *Client { return b.client }

func (b *Backend) Download(taskID, link string, _ map[string]string, _ int) {
	b.mu.Lock()
	b.link[taskID] = link
	b.mu.Unlock()
	b.launch(taskID, link)
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	r := b.runs[taskID]
	b.mu.Unlock()
	if r != nil {
		r.cancel()
	}
}

func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	link := b.link[taskID]
	b.mu.Unlock()
	if link != "" {
		b.launch(taskID, link)
	}
}

// Halt stops the task's transfer and returns once its part file is closed,
// so the folder can be moved. It reports whether one was running.
func (b *Backend) Halt(taskID string) bool {
	b.mu.Lock()
	r := b.runs[taskID]
	b.mu.Unlock()
	if r == nil {
		return false
	}
	r.cancel()
	<-r.ended
	return true
}

func (b *Backend) Remove(taskID string, deleteFiles bool) {
	b.mu.Lock()
	if r := b.runs[taskID]; r != nil {
		r.cancel()
	}
	part, saved := b.part[taskID], b.saved[taskID]
	delete(b.link, taskID)
	delete(b.part, taskID)
	delete(b.saved, taskID)
	b.mu.Unlock()
	// A plain remove keeps the part file, so an undo resumes.
	if !deleteFiles {
		return
	}
	if part != "" {
		_ = os.Remove(part)
	}
	if saved != "" {
		_ = os.Remove(saved)
	}
}

type runState struct {
	cancel context.CancelFunc
	ended  chan struct{}
}

// launch starts run on a goroutine of its own, registered before it goes, so
// a Halt that comes straight after finds it.
func (b *Backend) launch(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &runState{cancel: cancel, ended: make(chan struct{})}
	b.mu.Lock()
	b.runs[taskID] = r
	b.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			b.mu.Lock()
			if b.runs[taskID] == r {
				delete(b.runs, taskID)
			}
			b.mu.Unlock()
			close(r.ended)
		}()
		if err := b.run(ctx, taskID, link); err != nil && ctx.Err() == nil {
			b.fail(taskID, err)
		}
	}()
}

// fail reports err with the cause the backend knows: a spent quota waits
// until MEGA said it is back, and a removed file is gone.
func (b *Backend) fail(taskID string, err error) {
	u := core.Update{Status: core.StatusError, Err: err.Error()}
	var q *QuotaError
	switch {
	case errors.As(err, &q):
		u.Reason, u.Retry = core.ReasonLimit, q.Wait
		if u.Retry <= 0 {
			u.Retry = quotaWait
		}
	case Gone(err):
		u.Reason = core.ReasonGone
	}
	b.onUpdate(taskID, u)
}

func (b *Backend) run(ctx context.Context, taskID, link string) error {
	l, err := Parse(link)
	if err != nil {
		return err
	}
	dir := b.dir
	if b.Dir != nil {
		if d := b.Dir(taskID); d != "" {
			dir = d
		}
	}
	if err := os.MkdirAll(dir, filemode.Dir); err != nil {
		return fmt.Errorf("mega: %w", err)
	}
	part := partPath(dir, l, taskID)
	b.mu.Lock()
	b.part[taskID] = part
	b.mu.Unlock()

	f, err := b.client.file(ctx, l, true)
	if err != nil {
		return err
	}
	name := collide.SafeName(collide.BaseName(f.name))
	if name == "" {
		name = handleOf(l)
	}
	mac, err := newChunkMAC(f.key)
	if err != nil {
		return err
	}
	offset, err := resumeFrom(part, f.size, mac)
	if err != nil {
		return fmt.Errorf("mega: %w", err)
	}

	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: name, Size: f.size, Loaded: offset})
	if offset < f.size {
		if err := b.fetch(ctx, taskID, f, part, offset, mac); err != nil {
			return err
		}
	} else if err := touch(part); err != nil {
		return fmt.Errorf("mega: %w", err)
	}
	// MEGA keeps no MAC worth checking for an empty file.
	if f.size > 0 && !bytes.Equal(mac.Sum(), f.key.mac) {
		_ = os.Remove(part)
		return ErrMAC
	}

	final, err := finish(part, filepath.Join(dir, name))
	if err != nil {
		return err
	}
	b.mu.Lock()
	delete(b.part, taskID)
	b.saved[taskID] = final
	b.mu.Unlock()
	b.onUpdate(taskID, core.Update{Status: core.StatusDone, Name: filepath.Base(final), File: final, Size: f.size, Loaded: f.size})
	return nil
}

// partPath is the part file taskID writes the file of l into. It is named
// after the file's handle, which the link carries, because the file's name is
// only known once MEGA has been asked.
func partPath(dir string, l Link, taskID string) string {
	return reclaim.PartPath(dir, "mega-"+handleOf(l), taskID)
}

// PartFile is the part file the download taskID of link writes in dir until
// it finishes, or "" for something that is not a MEGA link.
func PartFile(dir, link, taskID string) string {
	l, err := Parse(link)
	if err != nil {
		return ""
	}
	return partPath(dir, l, taskID)
}

func handleOf(l Link) string {
	if l.Node != "" {
		return l.Node
	}
	return l.Handle
}

// resumeFrom is where the download carries on: the part file's length, cut
// back to a whole cipher block so the counter can pick up from there, or 0
// when the part file is longer than the file. What is already there goes
// through mac first.
func resumeFrom(part string, size int64, mac io.Writer) (int64, error) {
	fi, err := os.Stat(part)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	have := fi.Size() - fi.Size()%aes.BlockSize
	if fi.Size() > size {
		have = 0
	}
	if have != fi.Size() {
		if err := os.Truncate(part, have); err != nil {
			return 0, err
		}
	}
	if have == 0 {
		return 0, nil
	}
	in, err := os.Open(part)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if _, err := io.CopyN(mac, in, have); err != nil {
		return 0, err
	}
	return have, nil
}

func touch(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, filemode.File)
	if err != nil {
		return err
	}
	return f.Close()
}

// fetch downloads the file from offset on into the part file, decrypting it
// and feeding the plaintext to mac.
func (b *Backend) fetch(ctx context.Context, taskID string, f remoteFile, part string, offset int64, mac io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%d-%d", f.url, offset, f.size-1), nil)
	if err != nil {
		return fmt.Errorf("mega: the download address does not work: %w", err)
	}
	resp, err := b.transfer.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch code := resp.StatusCode; {
	case code == http.StatusOK || code == http.StatusPartialContent:
	case code == 509:
		left, _ := strconv.ParseInt(strings.TrimSpace(resp.Header.Get("X-MEGA-Time-Left")), 10, 64)
		return &QuotaError{Wait: time.Duration(left) * time.Second}
	case code >= 500:
		return errors.New("mega: the download server is temporarily unavailable")
	default:
		// Not the file's fault: the address MEGA handed out has expired or was
		// refused, and the next try asks for a new one.
		return fmt.Errorf("mega: the download server turned the request away (%d)", code)
	}
	stream, err := ctrAt(f.key, offset)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filemode.File)
	if err != nil {
		return fmt.Errorf("mega: %w", err)
	}
	total, copyErr := b.copy(ctx, taskID, out, mac, stream, resp.Body, offset, f.size)
	// Closed before returning, so a resume measures a flushed part file.
	if err := out.Close(); err != nil && copyErr == nil {
		copyErr = fmt.Errorf("mega: %w", err)
	}
	if copyErr != nil {
		return copyErr
	}
	if total != f.size {
		return fmt.Errorf("mega: the download ended after %d of %d bytes", total, f.size)
	}
	return nil
}

// copy decrypts src into dst and mac with periodic progress reports, the
// current speed limit and cancellation. It returns how far into the file it
// got.
func (b *Backend) copy(ctx context.Context, taskID string, dst, mac io.Writer, stream cipher.Stream, src io.Reader, start, size int64) (int64, error) {
	buf := make([]byte, copyBuffer)
	total := start
	var lim *rate.Limiter
	var limAt int64
	last, lastBytes := time.Now(), total
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			// MEGA sends no more than was asked for, and a server that did
			// would be decrypted into garbage past the end.
			if total+int64(n) > size {
				n = int(size - total)
			}
			chunk := buf[:n]
			stream.XORKeyStream(chunk, chunk)
			if _, err := dst.Write(chunk); err != nil {
				return total, fmt.Errorf("mega: %w", err)
			}
			_, _ = mac.Write(chunk)
			total += int64(n)
			if want := b.limit(); want > 0 {
				// WaitN fails for n above the burst, which is the buffer size.
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
			if errors.Is(rerr, io.EOF) || total == size {
				return total, nil
			}
			if ctx.Err() != nil {
				return total, ctx.Err()
			}
			return total, fmt.Errorf("mega: %w", rerr)
		}
		if total == size {
			return total, nil
		}
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

// finish moves the completed part file onto its real name and reports the
// name it ended up with. The collision policy never reaches this backend (see
// app.HonoursCollisionPolicy), and Rename neither destroys an existing file
// nor stalls the queue.
func finish(part, target string) (string, error) {
	res, err := collide.Reserve(target, collide.Rename)
	if err != nil {
		return "", fmt.Errorf("mega: %w", err)
	}
	// Windows does not replace a file that is still open.
	_ = res.File.Close()
	if err := os.Rename(part, res.Path); err != nil {
		_ = os.Remove(res.Path)
		return "", fmt.Errorf("mega: %w", err)
	}
	return res.Path, nil
}
