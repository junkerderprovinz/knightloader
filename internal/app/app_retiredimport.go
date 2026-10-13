package app

// What the import from a debrid account left behind. Versions 1.7 to 1.9
// staged a download added on the account as a debrid:// link, which nothing
// here fetches any more, kept what each account had shown in
// debrid_imports.json, and noted in debrid_drops.json the downloads to delete
// on the account once their task was removed.

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

const (
	importRecordsFile = "debrid_imports.json"
	importDropsFile   = "debrid_drops.json"
)

// importedLinkPrefix starts the link of every download the import staged.
const importedLinkPrefix = "debrid://"

// failImported fails a stored task the import staged and reports whether it
// changed. A finished one keeps its file and stays as it is.
func failImported(t *core.Task) bool {
	if !strings.HasPrefix(t.URL, importedLinkPrefix) || t.Status == core.StatusDone || t.ErrorCode == core.CodeDebridImport {
		return false
	}
	t.Status = core.StatusError
	t.Speed = 0
	t.NextTry = time.Time{}
	t.SetError("imported from the debrid account, which this version no longer does", core.CodeDebridImport, nil)
	return true
}

// importDrop is a download the import noted to delete on the account once its
// task was gone.
type importDrop struct {
	Task string `json:"task"`
	Slot string `json:"slot"`
	Job  string `json:"job"`
}

// settleRetiredImport deletes on the account what the import still meant to
// delete, for every task that is gone, and removes both of its files. It runs
// once the task list is whole and the accounts are wired. A delete is tried
// once: the file goes before any of them, so a refusal leaves the download on
// the account rather than being tried at every start.
func (a *App) settleRetiredImport() {
	dir := filepath.Dir(a.dlDir)
	if err := os.Remove(filepath.Join(dir, importRecordsFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("could not remove what the debrid account import remembered: %v", err)
	}
	path := filepath.Join(dir, importDropsFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	var notes map[string]importDrop
	if err == nil {
		err = json.Unmarshal(b, &notes)
	}
	if err != nil {
		log.Printf("could not read which imported downloads were to be deleted on the account, so they stay there: %v", err)
	}
	if err := os.Remove(path); err != nil {
		log.Printf("could not remove the list of imported downloads to delete: %v", err)
		return
	}
	var due []importDrop
	a.mu.Lock()
	for _, d := range notes {
		if a.tasks[d.Task] == nil && d.Slot != "" && d.Job != "" {
			due = append(due, d)
		}
	}
	a.mu.Unlock()
	if len(due) == 0 {
		return
	}
	a.spawn(func() {
		for _, d := range due {
			svc := a.torrentServiceOf(d.Slot)
			if svc == nil {
				log.Printf("%s: left %s on the account, which is not set up here", slotLabel(d.Slot), d.Job)
				continue
			}
			ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
			err := svc.DeleteTorrent(ctx, d.Job)
			cancel()
			if err != nil {
				log.Printf("%s: left %s on the account, the delete failed: %v", slotLabel(d.Slot), d.Job, err)
			}
		}
	})
}

// slotLabel names an account in a log line: the service, and the account id
// for a further one.
func slotLabel(slot string) string {
	service, account := resolver.SplitSlot(slot)
	if svc, ok := accounts.Lookup(service); ok {
		service = svc.Label
	}
	return service + accountSuffix(account)
}

// torrentServiceOf is the service behind an account's torrent backend, or nil
// when the account takes no torrents or is not wired.
func (a *App) torrentServiceOf(slot string) debrid.TorrentService {
	a.bmu.RLock()
	defer a.bmu.RUnlock()
	if tb, ok := a.debrid[slot].(*debrid.TorrentBackend); ok {
		return tb.Service()
	}
	return nil
}
