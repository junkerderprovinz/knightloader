package torrents

import (
	"context"
	"errors"
	"math"
	"net/url"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
)

// Tag marks every torrent KnightLoader adds to its qBittorrent, so the
// readings and the qBittorrent door see only those.
const Tag = "knightloader"

// QBit is the qBittorrent KnightLoader runs, reached through client, which
// waits for it to be running.
func QBit(client func(ctx context.Context) (*qbittorrent.Client, error)) Client {
	return qbit{client}
}

type qbit struct {
	client func(ctx context.Context) (*qbittorrent.Client, error)
}

// unknown is err as ErrUnknown when qBittorrent does not hold the torrent.
func unknown(err error) error {
	if errors.Is(err, qbittorrent.ErrNoTorrent) {
		return ErrUnknown
	}
	return err
}

func (q qbit) Add(ctx context.Context, t Add) error {
	c, err := q.client(ctx)
	if err != nil {
		return err
	}
	add := qbittorrent.Torrent{
		File:     t.File,
		Magnet:   t.Magnet,
		SavePath: t.Dir,
		Category: t.Category,
		Tags:     append([]string{Tag}, t.Tags...),
		Download: !t.Complete,
		Folder:   true,
	}
	if t.File == nil {
		// A magnet's trackers are what it starts from, so they go in the
		// link rather than after the add.
		add.Magnet = withTrackers(t.Magnet, t.Trackers)
		add.StopAtMetadata = true
	} else {
		add.Stopped = true
	}
	if err := c.Add(ctx, add); err != nil {
		return err
	}
	if t.File == nil || len(t.Trackers) == 0 {
		return nil
	}
	return unknown(c.AddTrackers(ctx, t.Hash, t.Trackers))
}

func withTrackers(magnet string, trackers []string) string {
	for _, tr := range trackers {
		magnet += "&tr=" + url.QueryEscape(tr)
	}
	return magnet
}

func (q qbit) Files(ctx context.Context, hash string) ([]File, error) {
	c, err := q.client(ctx)
	if err != nil {
		return nil, err
	}
	files, err := c.Files(ctx, hash)
	if err != nil {
		return nil, unknown(err)
	}
	out := make([]File, len(files))
	for i, f := range files {
		done := min(f.Size, int64(math.Round(f.Progress*float64(f.Size))))
		out[i] = File{Path: f.Name, Size: f.Size, Done: done, Picked: f.Priority > 0}
	}
	return out, nil
}

func (q qbit) Pick(ctx context.Context, hash string, picked []bool) error {
	c, err := q.client(ctx)
	if err != nil {
		return err
	}
	var on, off []int
	for i, p := range picked {
		if p {
			on = append(on, i)
		} else {
			off = append(off, i)
		}
	}
	// Those to fetch first, so the torrent is never left with none.
	if len(on) > 0 {
		if err := c.SetFilePriority(ctx, hash, on, 1); err != nil {
			return unknown(err)
		}
	}
	if len(off) > 0 {
		return unknown(c.SetFilePriority(ctx, hash, off, 0))
	}
	return nil
}

func (q qbit) Start(ctx context.Context, hash string) error {
	c, err := q.client(ctx)
	if err != nil {
		return err
	}
	return unknown(c.Start(ctx, hash))
}

func (q qbit) Stop(ctx context.Context, hash string) error {
	c, err := q.client(ctx)
	if err != nil {
		return err
	}
	return unknown(c.Stop(ctx, hash))
}

func (q qbit) Delete(ctx context.Context, hash string, files bool) error {
	c, err := q.client(ctx)
	if err != nil {
		return err
	}
	return unknown(c.Delete(ctx, files, hash))
}

func (q qbit) Readings(ctx context.Context) (map[string]Reading, error) {
	c, err := q.client(ctx)
	if err != nil {
		return nil, err
	}
	list, err := c.Torrents(ctx, qbittorrent.Filter{Tag: Tag})
	if err != nil {
		return nil, err
	}
	out := make(map[string]Reading, len(list))
	for _, t := range list {
		st, why := stateOf(t.State)
		out[t.Hash] = Reading{
			State:       st,
			Err:         why,
			Dir:         t.SavePath,
			Done:        t.Completed,
			Size:        t.Size,
			Speed:       t.DLSpeed,
			Uploaded:    t.Uploaded,
			Ratio:       t.Ratio,
			Peers:       t.Seeds + t.Leechs,
			Seeds:       t.Seeds,
			SeedSeconds: t.SeedingTime,
		}
	}
	return out, nil
}

// stateOf reduces qBittorrent's state of a torrent to the engine's, with the
// reason for a failure. A state this code does not know counts as fetching.
func stateOf(s string) (State, string) {
	switch s {
	case "checkingDL", "checkingUP", "checkingResumeData", "moving", "allocating":
		return Checking, ""
	case "pausedDL", "stoppedDL":
		return Stopped, ""
	case "uploading", "stalledUP", "queuedUP", "forcedUP":
		return Seeding, ""
	case "pausedUP", "stoppedUP":
		return Finished, ""
	case "missingFiles":
		return Failed, "qBittorrent cannot find the torrent's files"
	case "error":
		return Failed, "qBittorrent stopped the torrent with an error, such as a disk that is full or cannot be written"
	}
	return Fetching, ""
}

func (q qbit) Pieces(ctx context.Context, hash string) (Pieces, error) {
	c, err := q.client(ctx)
	if err != nil {
		return Pieces{}, err
	}
	p, err := c.Properties(ctx, hash)
	if err != nil {
		return Pieces{}, unknown(err)
	}
	states, err := c.PieceStates(ctx, hash)
	if err != nil {
		return Pieces{}, unknown(err)
	}
	return Pieces{Length: p.PieceSize, States: states}, nil
}

func (q qbit) Stream(ctx context.Context, hash string, on bool) error {
	c, err := q.client(ctx)
	if err != nil {
		return err
	}
	list, err := c.Torrents(ctx, qbittorrent.Filter{Hashes: []string{hash}})
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return ErrUnknown
	}
	// qBittorrent only offers to flip either switch.
	if list[0].Sequential != on {
		if err := c.ToggleSequential(ctx, hash); err != nil {
			return unknown(err)
		}
	}
	if list[0].FirstLast != on {
		return unknown(c.ToggleFirstLastPiece(ctx, hash))
	}
	return nil
}

func (q qbit) Configure(ctx context.Context, p Prefs) error {
	c, err := q.client(ctx)
	if err != nil {
		return err
	}
	return c.SetPreferences(ctx, prefsOf(p))
}

// prefsOf is p in qBittorrent's keys, with the rules qBittorrent would apply
// on its own switched off: KnightLoader queues the torrents, decides when
// their seeding ends, adds extra trackers only where a torrent is not private,
// and reads a file at its final name while it arrives.
func prefsOf(p Prefs) map[string]any {
	m := map[string]any{
		"up_limit":                  p.UploadLimit,
		"dht":                       p.DHT,
		"pex":                       p.PEX,
		"current_network_interface": p.Interface,
		"queueing_enabled":          false,
		"max_ratio_enabled":         false,
		"max_seeding_time_enabled":  false,
		"auto_tmm_enabled":          false,
		"torrent_content_layout":    "Original",
		"create_subfolder_enabled":  true,
		"start_paused_enabled":      false,
		"incomplete_files_ext":      false,
		"temp_path_enabled":         false,
		"add_trackers_enabled":      false,
	}
	// qBittorrent picked a port of its own on its first start, and keeps it.
	if p.Port > 0 {
		m["listen_port"] = p.Port
	}
	return m
}
