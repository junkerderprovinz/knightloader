package store

// The media archive: every video a yt-dlp row has finished, by the identity
// yt-dlp's --download-archive uses, so the same video is known again under
// another link, playlist or subscription. It is the history's counterpart for
// media and outlives a trim of the history, since a channel lists videos for
// years and a trim by count would otherwise bring the oldest back. Clearing
// the history empties it, and a download removed by hand leaves it, as both
// do in the history.
//
// A row is written as soon as a task knows its video, with no finish time,
// so the identity survives a restart before the download finishes. Only rows
// with a finish time count as downloaded.

import (
	"database/sql"
	"errors"
	"path/filepath"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// mediaTable sits outside the numbered migrations, so a build from before it
// still opens a database that has it.
const mediaTable = "media_archive"

var mediaSchema = []string{
	`CREATE TABLE IF NOT EXISTS ` + mediaTable + ` (
	   task_id     TEXT PRIMARY KEY,
	   key         TEXT NOT NULL,
	   url         TEXT NOT NULL DEFAULT '',
	   name        TEXT NOT NULL DEFAULT '',
	   folder      TEXT NOT NULL DEFAULT '',
	   finished_at INTEGER NOT NULL DEFAULT 0,
	   removed     INTEGER NOT NULL DEFAULT 0
	 )`,
	`CREATE INDEX IF NOT EXISTS ` + mediaTable + `_key ON ` + mediaTable + `(key, finished_at DESC)`,
}

// MediaRecord is one finished download of a video.
type MediaRecord struct {
	Key    string
	TaskID string
	URL    string
	// Name is the file the video was saved as, and Folder the folder it was
	// saved in, empty where the download did not say.
	Name       string
	Folder     string
	FinishedAt time.Time
}

// recordMedia files the video a task downloads. A task that is not done
// keeps the finish time a done one left, as the history keeps its row when a
// finished download is started again.
func (s *Store) recordMedia(t *core.Task) error {
	if t.Media == "" {
		return nil
	}
	if t.Status != core.StatusDone {
		_, err := s.db.Exec(`INSERT INTO `+mediaTable+` (task_id,key,url) VALUES (?,?,?)
		   ON CONFLICT(task_id) DO UPDATE SET key=excluded.key, url=excluded.url`,
			t.ID, t.Media, t.URL)
		return err
	}
	name, folder := t.Name, t.Dir
	if t.File != "" {
		name, folder = filepath.Base(t.File), filepath.Dir(t.File)
	}
	_, err := s.db.Exec(`INSERT INTO `+mediaTable+` (task_id,key,url,name,folder,finished_at) VALUES (?,?,?,?,?,?)
	   ON CONFLICT(task_id) DO UPDATE SET key=excluded.key, url=excluded.url,
	     name=excluded.name, folder=excluded.folder, finished_at=excluded.finished_at`,
		t.ID, t.Media, t.URL, name, folder, t.FinishedAt.UnixMilli())
	return err
}

// DownloadedMedia reports the latest finished download of the video key, and
// whether there is one.
func (s *Store) DownloadedMedia(key string) (MediaRecord, bool, error) {
	r := MediaRecord{Key: key}
	var at int64
	err := s.db.QueryRow(`SELECT task_id,url,name,folder,finished_at FROM `+mediaTable+`
	   WHERE key=? AND finished_at>0 AND removed=0 ORDER BY finished_at DESC, rowid DESC LIMIT 1`, key).
		Scan(&r.TaskID, &r.URL, &r.Name, &r.Folder, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return MediaRecord{}, false, nil
	}
	if err != nil {
		return MediaRecord{}, false, err
	}
	r.FinishedAt = time.UnixMilli(at)
	return r, true, nil
}

// MediaFolder reports the folder the video of a finished task was saved in,
// "" where the archive has none. The history keeps no folders, so this is
// where a repeat the history found gets one.
func (s *Store) MediaFolder(taskID string) (string, error) {
	var folder string
	err := s.db.QueryRow(`SELECT folder FROM `+mediaTable+` WHERE task_id=? AND finished_at>0`, taskID).Scan(&folder)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return folder, err
}

// taskMedia is the video of every task in the list that knows one.
func (s *Store) taskMedia() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT m.task_id, m.key FROM ` + mediaTable + ` m JOIN tasks t ON t.id = m.task_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, err
		}
		out[id] = key
	}
	return out, rows.Err()
}
