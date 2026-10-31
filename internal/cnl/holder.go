package cnl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Identity is what a KnightLoader listening on the Click'n'Load port says about
// itself, so another one on the same computer can name it and ask it, through
// their group, to hand the port over.
type Identity struct {
	App string `json:"app"`
	// Name is the instance's display name, Instance its id in its group (empty
	// outside one) and Kind "desktop", "server" or "container".
	Name     string `json:"name"`
	Instance string `json:"instance,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

// identityPath answers who holds the port. It is not a Click'n'Load route and
// carries no CORS header, so a website cannot read it; only a program on this
// computer asks.
const identityPath = "/knightloader"

const appName = "KnightLoader"

// ErrNotKnightLoader is a port held by a program that is not KnightLoader,
// usually a running JDownloader, which cannot be asked to let go of it.
var ErrNotKnightLoader = errors.New("cnl: another program holds the port")

// holderTimeout bounds the question to whatever holds the port, which is a
// program on this computer and answers at once or not at all.
const holderTimeout = 2 * time.Second

// Holder asks what holds 127.0.0.1:port. It answers ErrNotKnightLoader for a
// program that is not KnightLoader, and the connection's error when nothing
// answers there at all.
func Holder(ctx context.Context, port int) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, holderTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, identityPath), nil)
	if err != nil {
		return Identity{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	var id Identity
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&id) != nil || id.App != appName {
		return Identity{}, ErrNotKnightLoader
	}
	return id, nil
}

// serveIdentity answers identityPath with who this is.
func (s *Server) serveIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || s.identity == nil {
		http.NotFound(w, r)
		return
	}
	id := s.identity()
	id.App = appName
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(id)
}
