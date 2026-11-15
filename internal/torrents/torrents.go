// Package torrents is the seam between the engine and the torrent client it
// runs every torrent in. KnightLoader keeps the decisions: where a torrent
// lands, which of its files it fetches, which trackers it may use and when its
// seeding is over. The client moves the bytes. QBit is the client outside the
// tests, and torrentstest keeps one in memory.
package torrents

import (
	"context"
	"errors"
)

// Client is a torrent client as the engine drives it. Every torrent is known
// by its info hash in lower-case hex: the v1 hash, or the first 40 digits of
// the v2 one for a torrent without a v1 hash, as qBittorrent names them.
type Client interface {
	// Add hands a torrent over stopped. A magnet still fetches its file list,
	// and stops once it has it. Adding a torrent the client already has fails.
	Add(ctx context.Context, t Add) error
	// Files lists the torrent's files in the torrent's own order. The list is
	// empty while a magnet waits for it.
	Files(ctx context.Context, hash string) ([]File, error)
	// Pick fetches the files whose entry in picked is true and leaves the
	// others out. picked has one entry for each file.
	Pick(ctx context.Context, hash string, picked []bool) error
	Start(ctx context.Context, hash string) error
	Stop(ctx context.Context, hash string) error
	// Delete takes the torrent out of the client, and with files what it
	// wrote as well.
	Delete(ctx context.Context, hash string, files bool) error
	// Readings is every torrent the engine handed over, by hash, read in one
	// go.
	Readings(ctx context.Context) (map[string]Reading, error)
	// Pieces is where each piece of the torrent stands.
	Pieces(ctx context.Context, hash string) (Pieces, error)
	// Stream has the torrent fetch its pieces in order, its files' first and
	// last pieces before the rest, for a player reading it as it arrives; off
	// lets it fetch in its own order again.
	Stream(ctx context.Context, hash string, on bool) error
	// Configure applies the settings every torrent shares.
	Configure(ctx context.Context, p Prefs) error
}

var (
	// ErrUnknown is a call about a torrent the client does not have.
	ErrUnknown = errors.New("the torrent client does not have this torrent")
	// ErrOff is a client that does not run at all, rather than one still
	// starting.
	ErrOff = errors.New("the torrent client is off")
)

// Add is a torrent to hand over: the .torrent's bytes in File, or Magnet.
type Add struct {
	Hash   string
	File   []byte
	Magnet string
	// Dir is the folder the torrent lands in: its file, or its folder of
	// several files, goes there.
	Dir string
	// Trackers are announce addresses to add to the torrent's own.
	Trackers []string
	// Complete takes the files already in Dir as the torrent's, unread. The
	// caller has checked them; otherwise the client checks what it finds.
	Complete bool
	// Tags are the client's tags for the torrent besides Tag, and Category
	// files it in the client.
	Tags     []string
	Category string
}

// File is one file of a torrent. Path is where it lands below Add.Dir, with
// "/": inside the torrent's folder when it has several files, and the file's
// own name when it has one.
type File struct {
	Path   string
	Size   int64
	Done   int64
	Picked bool
}

// State is what a torrent is doing, in the client's words reduced to what
// the engine acts on.
type State int

const (
	// Fetching is a torrent fetching its file list or its files, or waiting
	// for peers to do so.
	Fetching State = iota
	// Checking is a torrent reading what it has, or moving it.
	Checking
	// Stopped is a torrent stopped before it had everything.
	Stopped
	// Seeding is a torrent with every picked file, giving it to the swarm.
	Seeding
	// Finished is a torrent with every picked file, stopped.
	Finished
	// Failed is a torrent the client gave up on; Reading.Err says why.
	Failed
)

// Complete reports whether the torrent has every file it fetches.
func (s State) Complete() bool { return s == Seeding || s == Finished }

// Reading is one torrent's numbers at one moment.
type Reading struct {
	State State
	Err   string
	// Dir is the folder the torrent lands in.
	Dir string
	// Done is how many bytes of the picked files are here, of Size.
	Done, Size int64
	Speed      int64
	Uploaded   int64
	Ratio      float64
	// Peers is every peer connected, seeds among them.
	Peers, Seeds int
	SeedSeconds  int64
}

// Piece states.
const (
	PieceMissing = 0
	PieceComing  = 1
	PieceHere    = 2
)

// Pieces is each piece's state, in the torrent's order, and the piece size.
type Pieces struct {
	Length int64
	States []int
}

// Prefs are the settings every torrent shares.
type Prefs struct {
	// Port is the port the client listens on, 0 for its own choice.
	Port int
	// UploadLimit is in bytes per second, 0 for none.
	UploadLimit int64
	DHT, PEX    bool
	// Interface is the network interface every torrent's traffic is tied
	// to, by the name the system gives it, empty for any.
	Interface string
	// Proxy carries every connection of the client, nil for none. While it
	// is set the client is tied to loopback, and whatever would go around a
	// proxy is off: the DHT, which looks up its first nodes on the machine's
	// own name servers, local peer discovery, port mapping and uTP.
	Proxy *Proxy
}

// Proxy is a SOCKS5 proxy with a login.
type Proxy struct {
	Host       string
	Port       int
	User, Pass string
}

// NoInterface is the interface the client is tied to when a proxy could not
// be set: there is none of that name, so nothing goes out at all.
const NoInterface = "knightloader-vpn"
