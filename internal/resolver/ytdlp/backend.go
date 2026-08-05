// Package ytdlp is the media-extraction backend: it delegates the download to
// the yt-dlp binary (which handles ~1800 sites incl. HLS/DASH muxing) and
// mirrors yt-dlp's progress into KnightLoader tasks.
package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

type Backend struct {
	bin string
	dir string

	onUpdate func(taskID string, u core.Update)

	// RateLimit, when set, returns the current download limit in bytes/s
	// (0 = unlimited); applied per spawn via --limit-rate.
	RateLimit func() int64

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	url    map[string]string // for resume
}

func NewBackend(bin, dir string, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		bin: bin, dir: dir, onUpdate: onUpdate,
		cancel: map[string]context.CancelFunc{},
		url:    map[string]string{},
	}
}

// Available reports whether the yt-dlp binary runs.
func (b *Backend) Available() bool {
	return exec.Command(b.bin, "--version").Run() == nil
}

func (b *Backend) Download(taskID, url string, _ map[string]string, _ int) {
	b.mu.Lock()
	b.url[taskID] = url
	b.mu.Unlock()
	go b.run(taskID, url)
}

func (b *Backend) run(taskID, url string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel[taskID] = cancel
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.cancel, taskID)
		b.mu.Unlock()
	}()

	args := []string{
		"--newline", "--no-warnings", "--no-color", "--no-playlist",
		"--progress-template", "KLP:%(progress)j",
		"-o", filepath.Join(b.dir, "%(title)s.%(ext)s"),
	}
	if b.RateLimit != nil {
		if lim := b.RateLimit(); lim > 0 {
			// --limit-rate is per fragment connection; fragments default to 1.
			args = append(args, "--limit-rate", fmt.Sprint(lim))
		}
	}
	cmd := exec.CommandContext(ctx, b.bin, append(args, url)...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: err.Error()})
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "yt-dlp: " + err.Error()})
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning})

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // progress JSON lines can be long
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "KLP:"):
			var p struct {
				Status     string  `json:"status"`
				Downloaded int64   `json:"downloaded_bytes"`
				Total      int64   `json:"total_bytes"`
				TotalEst   float64 `json:"total_bytes_estimate"`
				Speed      float64 `json:"speed"`
				Filename   string  `json:"filename"`
			}
			if json.Unmarshal([]byte(line[4:]), &p) != nil {
				continue
			}
			size := p.Total
			if size == 0 {
				size = int64(p.TotalEst)
			}
			u := core.Update{Status: core.StatusRunning, Loaded: p.Downloaded, Size: size, Speed: int64(p.Speed)}
			if p.Filename != "" {
				u.Name = filepath.Base(p.Filename)
			}
			b.onUpdate(taskID, u)
		case strings.Contains(line, "[download] Destination:"):
			name := strings.TrimSpace(line[strings.Index(line, "Destination:")+len("Destination:"):])
			b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: filepath.Base(name)})
		}
	}
	scanErr := sc.Err()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return // cancelled by Pause/Remove
	}
	if err == nil && scanErr != nil {
		err = scanErr
	}
	if err != nil {
		msg := tail(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "yt-dlp: " + msg})
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusDone, Speed: 0})
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	c := b.cancel[taskID]
	b.mu.Unlock()
	if c != nil {
		c()
		b.onUpdate(taskID, core.Update{Status: core.StatusPaused, Speed: 0})
	}
}

func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	url := b.url[taskID]
	b.mu.Unlock()
	if url != "" {
		go b.run(taskID, url) // yt-dlp continues the .part by default
	}
}

func (b *Backend) Remove(taskID string) {
	b.mu.Lock()
	if c, ok := b.cancel[taskID]; ok {
		c()
	}
	delete(b.url, taskID)
	b.mu.Unlock()
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 200 {
		return s[len(s)-200:]
	}
	return s
}
