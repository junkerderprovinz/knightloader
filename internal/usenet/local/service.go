// Package local fetches an .nzb from the user's own Usenet servers. It is one
// more account for internal/usenet's queue, ranked with TorBox and
// Premiumize.me, but its files are not fetched by a service first: every file
// of the .nzb becomes a task at once, whose articles this package's backend
// downloads over NNTP and writes into place (nntp://<job>/<file>/<name>).
package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/nzb"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
)

// ResolverID is the resolver and backend id of the tasks, and the slot the
// queue knows these servers by. It is also their row on the accounts page's
// priority card, which orders them against the debrid services.
const ResolverID = "nntp"

// ErrNotStored is a job whose .nzb is not here any more.
var ErrNotStored = errors.New("the .nzb of this download is no longer stored")

// Service keeps the .nzb of each job it took, which the backend reads the
// articles from and the queue takes back when the job goes to a debrid
// service after all.
type Service struct {
	dir string

	mu     sync.Mutex
	parsed map[string]*nzb.NZB
}

// NewService keeps its .nzb files in dir.
func NewService(dir string) *Service {
	return &Service{dir: dir, parsed: map[string]*nzb.NZB{}}
}

func (s *Service) Slot() string        { return ResolverID }
func (s *Service) Label() string       { return "your Usenet servers" }
func (s *Service) SubmitsPerHour() int { return 0 }

// Submit stores the .nzb. One that lists nothing to download is refused, which
// passes it to the next account.
func (s *Service) Submit(_ context.Context, _ string, data []byte) (string, error) {
	n, err := nzb.Parse(data)
	if err != nil {
		return "", err
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(s.path(id), data, 0o600); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.parsed[id] = n
	s.mu.Unlock()
	return id, nil
}

// Status reports every stored job as ready, its files being the .nzb's. The
// recovery volumes of a par2 set are held: a download that arrives whole
// does not need them.
func (s *Service) Status(_ context.Context, ids []string) (map[string]usenet.Status, error) {
	out := map[string]usenet.Status{}
	for _, id := range ids {
		n, err := s.load(id)
		if err != nil {
			continue
		}
		st := usenet.Status{Phase: usenet.PhaseReady, Progress: 1}
		for i, f := range n.Files {
			st.Files = append(st.Files, usenet.File{
				ID:   strconv.Itoa(i),
				Name: f.Name,
				Size: f.Bytes(),
				Link: FileLink(id, i, f.Name),
				Held: nzb.IsRecoveryVolume(f.Name),
			})
			st.Size += f.Bytes()
		}
		out[id] = st
	}
	return out, nil
}

// Link is never asked for: the files are not fetched over HTTP.
func (s *Service) Link(context.Context, string, string) (string, error) {
	return "", errors.New("a file from your Usenet servers has no download address")
}

// Delete forgets the job's .nzb.
func (s *Service) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	delete(s.parsed, id)
	s.mu.Unlock()
	if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// NZB returns the job's .nzb as it came in.
func (s *Service) NZB(id string) ([]byte, error) {
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotStored
	}
	return data, err
}

// File returns one file of a job.
func (s *Service) File(job string, index int) (nzb.File, error) {
	n, err := s.load(job)
	if err != nil {
		return nzb.File{}, err
	}
	if index < 0 || index >= len(n.Files) {
		return nzb.File{}, fmt.Errorf("the .nzb of this download has no file %d", index)
	}
	return n.Files[index], nil
}

func (s *Service) load(id string) (*nzb.NZB, error) {
	s.mu.Lock()
	n := s.parsed[id]
	s.mu.Unlock()
	if n != nil {
		return n, nil
	}
	data, err := s.NZB(id)
	if err != nil {
		return nil, err
	}
	if n, err = nzb.Parse(data); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.parsed[id] = n
	s.mu.Unlock()
	return n, nil
}

// path is where a job's .nzb is kept. The id is checked first, since it comes
// back in through a task's link.
func (s *Service) path(id string) string {
	if _, err := hex.DecodeString(id); err != nil || len(id) != 16 {
		id = "invalid"
	}
	return filepath.Join(s.dir, id+".nzb")
}
