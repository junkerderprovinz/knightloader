package usenet

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// ResolverID is the resolver and backend id of the files a finished job
// becomes.
const ResolverID = "usenet"

// scheme is the scheme of their links.
const scheme = "usenet"

// linkTries is how often a file's address is asked for before a busy or
// unreachable service fails the task, and linkWait the first pause between
// two tries. Together they wait out about a quarter of an hour.
const (
	linkTries = 6
	linkWait  = 30 * time.Second
)

// FileLink is the link one file of a finished job is staged under. It names
// the service, the job and the file, carries a named account in its query,
// and ends in the file name, so the list shows what the row is. It holds no
// secret: the address the service hands out is asked for when the download
// starts.
func FileLink(slot, job string, f File) string {
	service, account := resolver.SplitSlot(slot)
	u := url.URL{Scheme: scheme, Host: service, Path: "/" + job + "/" + f.ID + "/" + f.Name}
	if account != "" {
		u.RawQuery = url.Values{"account": {account}}.Encode()
	}
	return u.String()
}

// fileRef is a FileLink read back.
type fileRef struct {
	slot string
	job  string
	file string
	name string
}

func parseFileLink(raw string) (fileRef, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != scheme || u.Host == "" {
		return fileRef{}, fmt.Errorf("%q is not the link of a file fetched from Usenet", raw)
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return fileRef{}, fmt.Errorf("%q names no job and file", raw)
	}
	return fileRef{
		slot: resolver.SlotID(u.Host, u.Query().Get("account")),
		job:  parts[0],
		file: parts[1],
		name: parts[2],
	}, nil
}

// Resolver claims the file links and nothing else. It never calls out: the
// name is in the link, and the size was known when the row was staged.
type Resolver struct{}

// Info ranks it with the debrid services, though nothing else claims these
// links.
func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: 50} }

func (Resolver) Match(raw string) bool {
	_, err := parseFileLink(raw)
	return err == nil
}

func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	ref, err := parseFileLink(req.URL)
	if err != nil {
		return resolver.Result{}, err
	}
	return resolver.Result{Name: ref.name, DirectURL: req.URL}, nil
}

// Downloader is the engine a file's address is handed to.
type Downloader interface {
	// Handover starts the transfer of url. relink asks the service for a
	// fresh address when this one stops working part way.
	Handover(taskID, url string, conns int, relink func(context.Context) (string, error))
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// Files is the backend for the file links: it asks the account the job went
// to for an address and hands it to the engine, whose progress then drives
// the task.
type Files struct {
	eng      Downloader
	service  func(slot string) Service
	onUpdate func(taskID string, u core.Update)
	// wait is linkWait outside tests.
	wait time.Duration

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	link   map[string]string
	handed map[string]bool
	conns  map[string]int
}

// NewFiles builds the backend. service finds the account a link names, nil
// when that account has been removed.
func NewFiles(eng Downloader, service func(slot string) Service, onUpdate func(string, core.Update)) *Files {
	return &Files{
		eng: eng, service: service, onUpdate: onUpdate, wait: linkWait,
		cancel: map[string]context.CancelFunc{},
		link:   map[string]string{},
		handed: map[string]bool{},
		conns:  map[string]int{},
	}
}

func (b *Files) Download(taskID, link string, _ map[string]string, conns int) {
	b.mu.Lock()
	b.link[taskID] = link
	b.handed[taskID] = false
	b.conns[taskID] = conns
	b.mu.Unlock()
	b.start(taskID, link)
}

func (b *Files) start(taskID, link string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel[taskID] = cancel
	b.mu.Unlock()
	go b.run(ctx, taskID, link)
}

func (b *Files) run(ctx context.Context, taskID, link string) {
	ref, err := parseFileLink(link)
	if err != nil {
		b.fail(ctx, taskID, err)
		return
	}
	svc := b.service(ref.slot)
	if svc == nil {
		b.fail(ctx, taskID, fmt.Errorf("the account this file was fetched with (%s) is no longer set up", ref.slot))
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: "asking " + svc.Label() + " for the file"})
	direct, err := b.address(ctx, svc, ref, func() {
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Note: svc.Label() + " is busy, asking again shortly"})
	})
	if err != nil {
		b.fail(ctx, taskID, err)
		return
	}
	// Pause and Remove cancel under b.mu, so one that came while the service
	// answered is seen here and the address is not handed on.
	b.mu.Lock()
	if ctx.Err() != nil {
		b.mu.Unlock()
		return
	}
	b.handed[taskID] = true
	conns := b.conns[taskID]
	b.mu.Unlock()
	b.eng.Handover(taskID, direct, conns, func(ctx context.Context) (string, error) {
		return b.address(ctx, svc, ref, nil)
	})
}

// address asks svc for the file's address. A busy or unreachable service is
// asked again a few times first, with the wait doubling each time, since a
// failed task reads as a failed release to Sonarr. waiting, when set, runs
// before each wait.
func (b *Files) address(ctx context.Context, svc Service, ref fileRef, waiting func()) (string, error) {
	wait := b.wait
	for try := 1; ; try++ {
		direct, err := svc.Link(ctx, ref.job, ref.file)
		if err == nil || try == linkTries || !(errors.Is(err, ErrBusy) || temporary(err)) {
			return direct, err
		}
		if waiting != nil {
			waiting()
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
}

// fail reports err on the task, unless the run was cancelled by Pause or
// Remove, which set the task's state themselves.
func (b *Files) fail(ctx context.Context, taskID string, err error) {
	if ctx.Err() != nil {
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: err.Error()})
}

func (b *Files) Pause(taskID string) {
	b.mu.Lock()
	handed := b.handed[taskID]
	if cancel := b.cancel[taskID]; cancel != nil && !handed {
		cancel()
	}
	b.mu.Unlock()
	if handed {
		b.eng.Pause(taskID)
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusPaused})
}

func (b *Files) Resume(taskID string) {
	b.mu.Lock()
	handed := b.handed[taskID]
	link := b.link[taskID]
	b.mu.Unlock()
	if handed {
		b.eng.Resume(taskID)
		return
	}
	if link != "" {
		b.start(taskID, link)
	}
}

func (b *Files) Remove(taskID string, deleteFiles bool) {
	b.mu.Lock()
	if cancel := b.cancel[taskID]; cancel != nil {
		cancel()
	}
	handed := b.handed[taskID]
	delete(b.cancel, taskID)
	delete(b.link, taskID)
	delete(b.handed, taskID)
	delete(b.conns, taskID)
	b.mu.Unlock()
	if handed {
		b.eng.Remove(taskID, deleteFiles)
	}
}
