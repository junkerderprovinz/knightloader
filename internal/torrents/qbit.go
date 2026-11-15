package torrents

import (
	"context"
	"errors"
	"math"
	"net/url"
	"slices"

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
	before, err := c.Preferences(ctx)
	if err != nil {
		return err
	}
	// Up to 4.5 the proxy's type is a number, with a type of its own for a
	// proxy that takes a login; later versions name it.
	_, legacy := before["proxy_type"].(float64)
	m := prefsOf(p, legacy)
	if p.Proxy != nil {
		m["current_network_interface"] = loopback(ctx, c)
	} else {
		m["current_network_interface"] = interfaceValue(ctx, c, p.Interface)
	}
	if err := c.SetPreferences(ctx, m); err != nil {
		return err
	}
	if p.Proxy == nil {
		return nil
	}
	after, err := c.Preferences(ctx)
	if err != nil || !proxyHolds(after, p.Proxy) {
		if err := c.SetPreferences(ctx, map[string]any{"current_network_interface": NoInterface}); err != nil {
			return err
		}
		return errors.New("qBittorrent did not take the proxy, so its torrents are tied to no interface and wait")
	}
	if proxyHolds(before, p.Proxy) {
		return nil
	}
	// A connection opened before stays where it was, so every torrent that
	// runs drops its own and connects again through the proxy.
	return restart(ctx, c)
}

// prefsOf is p in qBittorrent's keys, with the rules qBittorrent would apply
// on its own switched off: KnightLoader queues the torrents, decides when
// their seeding ends, adds extra trackers only where a torrent is not private,
// and reads a file at its final name while it arrives.
func prefsOf(p Prefs, legacy bool) map[string]any {
	m := map[string]any{
		"up_limit":                  p.UploadLimit,
		"dht":                       p.DHT,
		"pex":                       p.PEX,
		"current_interface_address": "",
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
		"upnp":                      true,
		"lsd":                       true,
		"anonymous_mode":            false,
		"bittorrent_protocol":       protocolBoth,
		"proxy_type":                "None",
		"proxy_auth_enabled":        false,
	}
	if legacy {
		m["proxy_type"] = 0
	}
	// qBittorrent picked a port of its own on its first start, and keeps it.
	if p.Port > 0 {
		m["listen_port"] = p.Port
	}
	if px := p.Proxy; px != nil {
		m["proxy_type"] = "SOCKS5"
		m["proxy_auth_enabled"] = true
		if legacy {
			m["proxy_type"] = legacySOCKS5Login
		}
		m["proxy_ip"] = px.Host
		m["proxy_port"] = px.Port
		m["proxy_username"] = px.User
		m["proxy_password"] = px.Pass
		m["proxy_peer_connections"] = true
		m["proxy_torrents_only"] = false
		m["proxy_hostname_lookup"] = true
		// Anonymous mode keeps the machine's own addresses out of what goes
		// to trackers and peers.
		m["anonymous_mode"] = true
		m["dht"] = false
		m["lsd"] = false
		m["upnp"] = false
		m["bittorrent_protocol"] = protocolTCP
	}
	return m
}

// qBittorrent's numbers for the proxy type and the peer protocols.
const (
	legacySOCKS5Login = 4
	protocolBoth      = 0
	protocolTCP       = 1
)

// proxyHolds reports whether the settings m have qBittorrent connect through
// px.
func proxyHolds(m map[string]any, px *Proxy) bool {
	typ := m["proxy_type"]
	return (typ == float64(legacySOCKS5Login) || typ == "SOCKS5") &&
		m["proxy_auth_enabled"] == true &&
		m["proxy_peer_connections"] == true &&
		m["proxy_ip"] == px.Host &&
		m["proxy_port"] == float64(px.Port) &&
		m["proxy_username"] == px.User
}

// interfaceValue is the name qBittorrent takes for the interface the system
// calls name. On Windows qBittorrent names an adapter by its internal name,
// such as ethernet_32769, and shows the system's beside it; elsewhere the
// two are one. A name qBittorrent does not list is passed on as it is, so
// torrents wait for that interface rather than go out on another.
func interfaceValue(ctx context.Context, c *qbittorrent.Client, name string) string {
	if name == "" {
		return ""
	}
	list, err := c.NetworkInterfaces(ctx)
	if err != nil {
		return name
	}
	for _, i := range list {
		if i.Value == name {
			return i.Value
		}
	}
	for _, i := range list {
		if i.Name == name {
			return i.Value
		}
	}
	return name
}

// loopback is qBittorrent's name for the interface of 127.0.0.1, so that with
// a proxy on loopback nothing can leave the machine but through it. Without
// one the proxy alone carries the torrents.
func loopback(ctx context.Context, c *qbittorrent.Client) string {
	list, err := c.NetworkInterfaces(ctx)
	if err != nil {
		return ""
	}
	for _, i := range list {
		addrs, err := c.InterfaceAddresses(ctx, i.Value)
		if err == nil && slices.Contains(addrs, "127.0.0.1") {
			return i.Value
		}
	}
	return ""
}

// restart stops and starts every torrent that runs.
func restart(ctx context.Context, c *qbittorrent.Client) error {
	list, err := c.Torrents(ctx, qbittorrent.Filter{})
	if err != nil {
		return err
	}
	var running []string
	for _, t := range list {
		switch st, _ := stateOf(t.State); st {
		case Stopped, Finished, Failed:
		default:
			running = append(running, t.Hash)
		}
	}
	if len(running) == 0 {
		return nil
	}
	if err := c.Stop(ctx, running...); err != nil {
		return err
	}
	return c.Start(ctx, running...)
}
