package settings

import (
	"encoding/json"
	"fmt"
	"os"
)

// KeepAutoUpdateIn reads and writes the AutoUpdate switch in its own file at
// path. The desktop copy installed for all users on Windows is updated by a
// task that runs as the system rather than as any user, and reads the switch
// from there.
func (s *Store) KeepAutoUpdateIn(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoUpdatePath = path
	s.cur.AutoUpdate = ReadAutoUpdate(path)
}

type autoUpdateFile struct {
	AutoUpdate bool `json:"autoUpdate"`
}

// ReadAutoUpdate reads the switch from a file KeepAutoUpdateIn named. A
// missing, unreadable or corrupt file reads as on, like a fresh install.
func ReadAutoUpdate(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	f := autoUpdateFile{AutoUpdate: true}
	if json.Unmarshal(body, &f) != nil {
		return true
	}
	return f.AutoUpdate
}

// writeAutoUpdate rewrites the file in place. Its folder belongs to the
// administrators and only the file itself is open to every user, so there is
// no folder to put a temporary file in.
func writeAutoUpdate(path string, on bool) error {
	body, err := json.Marshal(autoUpdateFile{AutoUpdate: on})
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
