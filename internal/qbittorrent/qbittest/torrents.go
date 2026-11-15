package qbittest

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
)

// Torrent is one torrent the server holds, by the fields torrents/info
// answers with, and its files and pieces.
type Torrent struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress"`
	Size        int64   `json:"size"`
	TotalSize   int64   `json:"total_size"`
	Downloaded  int64   `json:"downloaded"`
	Uploaded    int64   `json:"uploaded"`
	Ratio       float64 `json:"ratio"`
	Completed   int64   `json:"completed"`
	DLSpeed     int64   `json:"dlspeed"`
	UPSpeed     int64   `json:"upspeed"`
	Seeds       int     `json:"num_seeds"`
	Leechs      int     `json:"num_leechs"`
	SeedingTime int64   `json:"seeding_time"`
	AmountLeft  int64   `json:"amount_left"`
	SavePath    string  `json:"save_path"`
	Category    string  `json:"category"`
	Tags        string  `json:"tags"`
	Sequential  bool    `json:"seq_dl"`
	FirstLast   bool    `json:"f_l_piece_prio"`

	Files     []File   `json:"-"`
	Trackers  []string `json:"-"`
	Pieces    []int    `json:"-"`
	PieceSize int64    `json:"-"`
	// Metainfo is the .torrent it was added with.
	Metainfo []byte `json:"-"`
}

// File is one file of a torrent, as torrents/files answers.
type File struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

// AddCall is one torrents/add as it arrived: its form fields and the
// .torrent file, if one came.
type AddCall struct {
	Fields map[string]string
	File   []byte
}

// Put holds t, every file of it to be fetched.
func (s *Server) Put(t Torrent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.torrents == nil {
		s.torrents = map[string]*Torrent{}
	}
	for i := range t.Files {
		t.Files[i].Index = i
		t.Files[i].Priority = 1
	}
	s.torrents[t.Hash] = &t
}

// Get is a copy of the torrent with hash, or nil.
func (s *Server) Get(hash string) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.torrents[hash]
	if !ok {
		return nil
	}
	c := *t
	c.Files = slices.Clone(t.Files)
	c.Trackers = slices.Clone(t.Trackers)
	return &c
}

// Added is every torrents/add so far.
func (s *Server) Added() []AddCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.added)
}

// DeletedWithFiles is every torrent deleted with its files.
func (s *Server) DeletedWithFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.deleted)
}

func (s *Server) serveTorrents(w http.ResponseWriter, r *http.Request) bool {
	call, ok := strings.CutPrefix(r.URL.Path, "/api/v2/torrents/")
	if !ok {
		return false
	}
	pause, resume, paused := "pause", "resume", "pausedDL"
	if s.Modern {
		pause, resume, paused = "stop", "start", "stoppedDL"
	}
	switch call {
	case "add":
		s.add(w, r)
	case "info":
		s.info(w, r)
	case "files":
		s.withTorrent(w, r, func(t *Torrent) any { return t.Files })
	case "export":
		s.mu.Lock()
		t, ok := s.torrents[r.FormValue("hash")]
		var b []byte
		if ok {
			b = t.Metainfo
		}
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return true
		}
		w.Header().Set("Content-Type", "application/x-bittorrent")
		w.Write(b)
	case "pieceStates":
		s.withTorrent(w, r, func(t *Torrent) any { return t.Pieces })
	case "properties":
		s.withTorrent(w, r, func(t *Torrent) any {
			return map[string]any{"piece_size": t.PieceSize, "pieces_num": len(t.Pieces), "save_path": t.SavePath}
		})
	case "filePrio":
		prio, _ := strconv.Atoi(r.FormValue("priority"))
		s.change(w, r, func(t *Torrent) {
			for _, id := range strings.Split(r.FormValue("id"), "|") {
				if i, err := strconv.Atoi(id); err == nil && i >= 0 && i < len(t.Files) {
					t.Files[i].Priority = prio
				}
			}
		})
	case pause:
		s.noteSwitch(call, r)
		s.change(w, r, func(t *Torrent) { t.State = paused })
	case resume:
		s.noteSwitch(call, r)
		s.change(w, r, func(t *Torrent) { t.State = "downloading" })
	case "addTrackers":
		s.change(w, r, func(t *Torrent) {
			t.Trackers = append(t.Trackers, strings.Split(r.FormValue("urls"), "\n")...)
		})
	case "toggleSequentialDownload":
		s.change(w, r, func(t *Torrent) { t.Sequential = !t.Sequential })
	case "toggleFirstLastPiecePrio":
		s.change(w, r, func(t *Torrent) { t.FirstLast = !t.FirstLast })
	case "setLocation":
		s.change(w, r, func(t *Torrent) { t.SavePath = r.FormValue("location") })
	case "delete":
		s.mu.Lock()
		for _, h := range strings.Split(r.FormValue("hashes"), "|") {
			if _, ok := s.torrents[h]; ok && r.FormValue("deleteFiles") == "true" {
				s.deleted = append(s.deleted, h)
			}
			delete(s.torrents, h)
		}
		s.mu.Unlock()
	default:
		return false
	}
	return true
}

func (s *Server) add(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	call := AddCall{Fields: map[string]string{}}
	for k, v := range r.MultipartForm.Value {
		call.Fields[k] = v[0]
	}
	if fh := r.MultipartForm.File["torrents"]; len(fh) > 0 {
		if f, err := fh[0].Open(); err == nil {
			call.File, _ = io.ReadAll(f)
			f.Close()
		}
	}
	hash := hashOf(call)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.added = append(s.added, call)
	if s.torrents == nil {
		s.torrents = map[string]*Torrent{}
	}
	if _, ok := s.torrents[hash]; ok {
		io.WriteString(w, "Fails.")
		return
	}
	state := "downloading"
	if call.Fields["paused"] == "true" || call.Fields["stopped"] == "true" {
		state = "pausedDL"
	}
	s.torrents[hash] = &Torrent{Hash: hash, State: state, SavePath: call.Fields["savepath"], Category: call.Fields["category"], Tags: call.Fields["tags"], Metainfo: call.File}
	io.WriteString(w, "Ok.")
}

// hashOf is the info hash of an added torrent as qBittorrent names it: from
// the .torrent's info dictionary or the magnet's btih, and for anything else
// a digest of what came, so that each add still has a name.
func hashOf(call AddCall) string {
	if raw, err := bencode.Raw(call.File, "info"); err == nil {
		return bencode.InfoHash(raw)
	}
	src := call.File
	if src == nil {
		urls := call.Fields["urls"]
		if _, rest, ok := strings.Cut(urls, "urn:btih:"); ok {
			h, _, _ := strings.Cut(rest, "&")
			return strings.ToLower(h)
		}
		src = []byte(urls)
	}
	sum := sha1.Sum(src)
	return hex.EncodeToString(sum[:])
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	tag, category := r.FormValue("tag"), r.FormValue("category")
	var hashes []string
	if h := r.FormValue("hashes"); h != "" {
		hashes = strings.Split(h, "|")
	}
	s.mu.Lock()
	out := []Torrent{}
	for _, t := range s.torrents {
		if tag != "" && !slices.Contains(strings.Split(t.Tags, ","), tag) {
			continue
		}
		if category != "" && t.Category != category {
			continue
		}
		if hashes != nil && !slices.Contains(hashes, t.Hash) {
			continue
		}
		out = append(out, *t)
	}
	s.mu.Unlock()
	slices.SortFunc(out, func(a, b Torrent) int { return strings.Compare(a.Hash, b.Hash) })
	writeJSON(w, out)
}

// withTorrent answers with what f makes of the torrent the hash names, and
// 404 for a hash qBittorrent does not hold, as qBittorrent does.
func (s *Server) withTorrent(w http.ResponseWriter, r *http.Request, f func(t *Torrent) any) {
	s.mu.Lock()
	t, ok := s.torrents[r.FormValue("hash")]
	var v any
	if ok {
		v = f(t)
	}
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, v)
}

func (s *Server) change(w http.ResponseWriter, r *http.Request, f func(t *Torrent)) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	keys := r.FormValue("hashes")
	if keys == "" {
		keys = r.FormValue("hash")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, h := range strings.Split(keys, "|") {
		if t, ok := s.torrents[h]; ok {
			f(t)
			found = true
		}
	}
	if !found && r.FormValue("hash") != "" {
		http.NotFound(w, r)
	}
}

func (s *Server) noteSwitch(call string, r *http.Request) {
	s.mu.Lock()
	s.switches = append(s.switches, call+" "+r.FormValue("hashes"))
	s.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
