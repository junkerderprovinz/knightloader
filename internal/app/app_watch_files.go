package app

// Dropped files that are not link lists: a .torrent, an encrypted container
// and an .nzb. Each goes where an upload of the same file would.

import (
	"errors"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/container"
	"github.com/junkerderprovinz/knightloader/internal/linkscan"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// errNothingInNZB is a file named .nzb that is neither an NZB nor a link list.
var errNothingInNZB = errors.New("this file is neither an .nzb nor a list of links")

// containerFileAdder is a backend that opens a container whose bytes are at
// hand rather than behind an address.
type containerFileAdder interface {
	AddContainerFile(ext string, data []byte, packageName string, timeout time.Duration) ([]resolver.Result, error)
}

// checkWatchJob refuses a dropped file this instance cannot open before the
// file is retired, so it stays in the folder and the log says why.
func (a *App) checkWatchJob(j watch.Job) error {
	f := j.File
	if f == nil {
		return nil
	}
	switch strings.ToLower(filepath.Ext(f.Name)) {
	case ".torrent":
		_, _, err := torrent.ParseUpload(f.Data)
		return err
	case ".nzb":
		if usenet.IsNZB(f.Data) {
			if _, ok := a.UsenetService(); !ok {
				return ErrNZBNeedsUsenet
			}
			return nil
		}
		if len(linkscan.Extract(string(f.Data))) == 0 {
			return errNothingInNZB
		}
		return nil
	}
	_, err := container.Links(f.Name, f.Data)
	if !errors.Is(err, container.ErrNeedsBackend) {
		return err
	}
	switch {
	case !a.ContainerBackendConfigured():
		return ErrNoContainerBackend
	case a.ModuleOff("jd"):
		return ErrJDOff
	}
	return nil
}

// stageWatchFile hands a dropped file to whatever opens it. checkWatchJob has
// already said yes, so a failure here is one that could not be foreseen, and
// it is logged beside the other links that did not make it.
func (a *App) stageWatchFile(f *watch.File, pkg string) {
	switch strings.ToLower(filepath.Ext(f.Name)) {
	case ".torrent":
		md, uri, err := torrent.ParseUpload(f.Data)
		if err != nil {
			a.watchFileFailed(f, "torrent", err)
			return
		}
		// All of its files, since nobody is at the collector to pick. AddTorrent
		// asks auto-confirm itself.
		_, _ = a.AddTorrent(uri, md.Files, pkg, OriginWatch)
	case ".nzb":
		if usenet.IsNZB(f.Data) {
			if _, err := a.AddNZB(NZB{Name: pkg, Data: f.Data, Origin: OriginWatch}); err != nil {
				a.watchFileFailed(f, "nzb", err)
			}
			return
		}
		a.stageWatchJob(watch.Job{URLs: linkscan.Extract(string(f.Data)), Package: pkg})
	default:
		links, err := container.Links(f.Name, f.Data)
		switch {
		case err == nil:
			a.stageWatchJob(watch.Job{URLs: links, Package: pkg})
		case errors.Is(err, container.ErrNeedsBackend):
			a.openWatchContainer(f, pkg)
		default:
			a.watchFileFailed(f, "container", err)
		}
	}
}

// openWatchContainer has JD open an encrypted container. The bytes go to JD
// inline, since there is no upload whose address JD could fetch.
func (a *App) openWatchContainer(f *watch.File, pkg string) {
	a.bmu.RLock()
	be := a.jd
	a.bmu.RUnlock()
	adder, ok := be.(containerFileAdder)
	switch {
	case !ok:
		a.watchFileFailed(f, "container", ErrNoContainerBackend)
		return
	case a.ModuleOff("jd"):
		a.watchFileFailed(f, "container", ErrJDOff)
		return
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Name)), ".")
	links, err := adder.AddContainerFile(ext, f.Data, pkg, containerCrawlLimit)
	if err != nil {
		a.watchFileFailed(f, "container", err)
		return
	}
	created := a.AddResolvedLinksFrom(links, pkg, OriginWatch)
	log.Printf("dropped container %s: %d links, %d staged", f.Name, len(links), len(created))
}

func (a *App) watchFileFailed(f *watch.File, kind string, err error) {
	log.Printf("dropped file %s: %v", f.Name, err)
	a.recordSkippedReason(f.Name, kind, err.Error())
}
