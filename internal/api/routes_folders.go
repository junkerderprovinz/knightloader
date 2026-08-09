package api

// The folder chooser's one question: which directories are under this path.
//
// WHY IT IS A ROUTE AND NOT A DIALOG PER PAGE. The download folder, the
// extraction destination, the watch folder, the add-links destination and the
// per-task override are five fields holding one kind of value, and every one of
// them wants the same picker. Built per page it would be built five times and
// get the template rule below wrong in at least one of them.
//
// SECURITY, because this lists the host filesystem to whoever holds a session.
//
// The boundary is the filesystem this process can already see: in the shipped
// container that is the image plus whatever the operator mounted into it, which
// is exactly the set of folders a download can land in. It is deliberately not
// narrower by default - the download folder is wherever the user mounted their
// disk, and a chooser that cannot reach it is one they type around, which is the
// failure this feature exists to prevent. KL_BROWSE_ROOTS narrows it to a list
// of folders for an instance where the whole tree is too much, and resolving
// symlinks is what makes that narrowing real: a prefix check that never resolves
// is one `ln -s / /downloads/out` away from listing the entire disk.
//
// What the route never does is open a file. It answers with directory NAMES and
// nothing else - no file entries, no sizes, no contents - so the worst a session
// can learn from it is what somebody called their folders. It lives under /api/,
// which is the only place the session guard reaches: everything outside that
// prefix is open by construction (see reg.open in routes.go).

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// envBrowseRoots narrows the chooser to a list of folders, separated the way
// this platform separates a path list (":" on Unix). Unset means the whole
// filesystem this process can see, which is the boundary explained above.
const envBrowseRoots = "KL_BROWSE_ROOTS"

// maxFolderEntries caps one listing. A media library with ten thousand
// directories in it would otherwise be sent in full to a dialog that can show
// twelve of them at a time, on a route the user hits again with every click.
// The response says when it cut, so the interface can point at the path box
// instead of pretending it showed everything.
const maxFolderEntries = 2000

// folderEntry is one directory offered for the next click. Name is what is
// shown, Path is what to ask for next - assembled here rather than in the
// interface, which would have to know which separator this host uses.
type folderEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// folderListing is one place in the filesystem as the chooser needs to see it.
//
// Path and Listed are two different answers on purpose. A user typing a folder
// they are about to create must be told it is new, not shown an empty dialog
// with nothing in it and no explanation - so Path is what they asked for, Listed
// is the deepest existing folder above it, and Entries describes Listed. When
// the path exists the two are the same string.
type folderListing struct {
	// Path is the folder the chooser is pointed at, cleaned, with any template
	// tail removed. It is the value "use this folder" is built from.
	Path string `json:"path"`
	// Tail is the <jd:...> part that was cut off Path, leading separator
	// included, or "" when the caller sent a plain path. See splitTemplate: the
	// interface puts it back on, and the whole feature hangs on it doing so.
	Tail string `json:"tail"`
	// Exists reports whether Path is a directory today.
	Exists bool `json:"exists"`
	// Listed is the folder Entries actually describes.
	Listed string `json:"listed"`
	// Parent is one level above Listed, or "" at the top of the boundary.
	Parent string `json:"parent"`
	// Roots is the boundary itself, so the interface can offer a way back to it
	// rather than leaving somebody stuck below a mount they typed into.
	Roots []string `json:"roots"`
	// Entries are the sub-directories of Listed, sorted, folders only.
	Entries []folderEntry `json:"entries"`
	// Truncated says the list was cut at maxFolderEntries.
	Truncated bool `json:"truncated"`
}

// folderRefusal is a refusal that knows which status it deserves, so the reason
// the interface shows and the code the browser logs agree about what happened.
type folderRefusal struct {
	status int
	reason string
}

func (e folderRefusal) Error() string { return e.reason }

func registerFolders(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/folders",
		"the sub-folders of one directory, for the folder chooser; directory names only, never file contents",
		func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Query().Get("path")
			// Opened with no path at all, the chooser starts where downloads
			// already go. Anywhere else is a dialog that opens on a folder the
			// user has to navigate away from before it is of any use.
			if strings.TrimSpace(path) == "" {
				path = a.Settings.Get().DownloadDir
			}
			out, err := listFolders(path)
			if err != nil {
				var ref folderRefusal
				switch {
				case errors.As(err, &ref):
					http.Error(w, ref.reason, ref.status)
				case errors.Is(err, fs.ErrPermission):
					// The message names the folder, which is the one thing that
					// makes this actionable: a bare "forbidden" beside a path the
					// user can plainly see in another window teaches nobody which
					// side the problem is on.
					http.Error(w, err.Error(), http.StatusForbidden)
				default:
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}
			writeJSON(w, out)
		})
}

// listFolders is the whole route, kept apart from the handler so the rules can
// be tested without a socket.
func listFolders(raw string) (folderListing, error) {
	fixed, tail := splitTemplate(raw)
	if strings.TrimSpace(fixed) == "" {
		fixed = defaultStart()
	}
	if !filepath.IsAbs(fixed) {
		// The same rule as settings.Validate, and for the same reason: a relative
		// path is resolved against whatever the process's working directory
		// happens to be, which is not something a user can reason about.
		return folderListing{}, folderRefusal{http.StatusBadRequest, "the folder must be an absolute path"}
	}
	fixed = filepath.Clean(fixed)

	roots, err := browseRoots(fixed)
	if err != nil {
		return folderListing{}, err
	}

	listed := deepestExisting(fixed)
	if fi, err := os.Stat(listed); err != nil || !fi.IsDir() {
		return folderListing{}, folderRefusal{http.StatusNotFound,
			"there is no folder at or above " + fixed + " that this instance can read"}
	}
	real, ok := resolveWithin(listed, roots)
	if !ok {
		return folderListing{}, folderRefusal{http.StatusForbidden,
			"this instance may not list " + listed + "; it is outside " + strings.Join(roots, ", ")}
	}

	entries, truncated, err := readFolders(real, listed, roots)
	if err != nil {
		return folderListing{}, err
	}

	out := folderListing{
		Path:      fixed,
		Tail:      tail,
		Exists:    listed == fixed,
		Listed:    listed,
		Roots:     roots,
		Entries:   entries,
		Truncated: truncated,
	}
	// A parent that leaves the boundary is not offered rather than offered and
	// then refused: a disabled-looking button that answers 403 when pressed is
	// the interface lying about what it can do.
	if parent := filepath.Dir(listed); parent != listed {
		if _, ok := resolveWithin(parent, roots); ok {
			out.Parent = parent
		}
	}
	return out, nil
}

// splitTemplate cuts a download folder into the part that is a real path and the
// part that is a pathvars template, e.g. "/downloads/<jd:date>/<jd:hoster>" into
// "/downloads" and "/<jd:date>/<jd:hoster>".
//
// This is the whole point of the route existing rather than the interface
// stat-ing a path. Browsing may only ever replace the fixed part: a chooser that
// wrote back the folder it landed on would silently delete the user's naming
// scheme, and they would not find out until every file in six months of
// downloads had landed in one flat directory.
//
// The rule - cut at the first segment containing "<" - is a deliberate twin of
// the unexported fixedPrefix in internal/settings/settings_paths.go, which is
// what decides the directory the app actually creates and writes to. The two
// must agree: splitting one segment later offers a folder the app will never
// make, splitting earlier drops a fixed segment out of the user's path.
// TestTheSplitMatchesTheFolderThatGetsCreated is what says so out loud.
func splitTemplate(dir string) (fixed, tail string) {
	if !strings.Contains(dir, "<") {
		return dir, ""
	}
	sep := string(filepath.Separator)
	parts := strings.Split(strings.ReplaceAll(dir, "/", sep), sep)
	for i, p := range parts {
		if !strings.Contains(p, "<") {
			continue
		}
		fixed = strings.Join(parts[:i], sep)
		if fixed == "" {
			// Everything below the root is a placeholder; the root is the fixed
			// part. The tail keeps its leading separator either way, so the
			// caller re-assembles by concatenation and never has to guess which
			// separator this host uses.
			fixed = sep
		}
		return fixed, sep + strings.Join(parts[i:], sep)
	}
	return dir, ""
}

// browseRoots is the boundary for one request. See the file header for what it
// is and why it is that wide by default.
func browseRoots(p string) ([]string, error) {
	set := strings.TrimSpace(os.Getenv(envBrowseRoots))
	if set == "" {
		return []string{volumeRoot(p)}, nil
	}
	// Read per request rather than once at startup: this costs one map lookup on
	// a route a human drives at human speed, and it keeps the setting knowable
	// from the environment the process is actually running in.
	var out []string
	for _, part := range filepath.SplitList(set) {
		part = strings.TrimSpace(part)
		if part == "" || !filepath.IsAbs(part) {
			continue
		}
		// Resolved, so a root that is itself a symlink still contains the paths
		// below it once those are resolved too. A root that does not exist yet is
		// kept as written: an operator naming a mount that is not up must get an
		// empty chooser, not a silently wider one.
		if real, err := filepath.EvalSymlinks(part); err == nil {
			out = append(out, filepath.Clean(real))
			continue
		}
		out = append(out, filepath.Clean(part))
	}
	if len(out) == 0 {
		// Loudly, not by falling back to the whole filesystem. Somebody set this
		// variable to narrow what the chooser may see; a typo in it must never be
		// the thing that widens it back to everything.
		return nil, folderRefusal{http.StatusInternalServerError,
			envBrowseRoots + " is set but names no absolute folder, so nothing may be listed"}
	}
	return out, nil
}

// volumeRoot is the top of the filesystem the given path lives on: "/" on the
// platforms this ships to, and the drive on Windows, where the app is only ever
// run by somebody developing it.
func volumeRoot(p string) string {
	if v := filepath.VolumeName(p); v != "" {
		return v + string(filepath.Separator)
	}
	return string(filepath.Separator)
}

// defaultStart is where the chooser opens when nothing was asked for and no
// download folder has been configured yet.
func defaultStart() string {
	if wd, err := os.Getwd(); err == nil {
		if v := filepath.VolumeName(wd); v != "" {
			return v + string(filepath.Separator)
		}
	}
	return string(filepath.Separator)
}

// deepestExisting walks up until it finds a directory that is really there, so a
// folder the user is about to create can be reported as new while still showing
// them where it would go.
func deepestExisting(p string) string {
	for {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

// resolveWithin resolves p and reports whether what it really points at is
// inside the boundary. Everything that reads a directory goes through here.
func resolveWithin(p string, roots []string) (string, bool) {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	for _, root := range roots {
		if within(root, real) {
			return real, true
		}
	}
	return "", false
}

// within reports whether p is root or sits below it. filepath.Rel does the
// comparison because it is the one that knows this platform's rules - on Windows
// it compares case-insensitively, which a strings.HasPrefix here would not.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	// Not HasPrefix(rel, ".."): a folder named "..old" is a perfectly ordinary
	// folder and starts with the same two characters as the way out.
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// readFolders lists the sub-directories of an already-resolved directory. real
// is what is read, display is the path the caller asked in - the two differ
// through a symlink, and the entries are named after display so the user keeps
// the spelling they typed instead of having their setting silently rewritten to
// wherever the link pointed.
func readFolders(real, display string, roots []string) ([]folderEntry, bool, error) {
	items, err := os.ReadDir(real)
	if err != nil {
		return nil, false, err
	}
	out := make([]folderEntry, 0, len(items))
	for _, it := range items {
		name := it.Name()
		switch {
		case it.IsDir():
		case it.Type()&fs.ModeSymlink != 0:
			// A symlinked folder is still a folder somebody may want to download
			// into, so it is offered - but only after resolving it, and only when
			// what it points at is inside the boundary. ReadDir reports a link as
			// a link, never as a directory, so skipping this case would hide half
			// the folders on a machine where /downloads is a link.
			target := filepath.Join(real, name)
			if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
				continue
			}
			if _, ok := resolveWithin(target, roots); !ok {
				continue
			}
		default:
			continue // a file; the chooser picks folders and opens neither
		}
		out = append(out, folderEntry{Name: name, Path: filepath.Join(display, name)})
	}
	// Case-insensitive, because a list where "Movies" sorts before "archive"
	// reads as unsorted to everybody who is not a byte comparator.
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > maxFolderEntries {
		return out[:maxFolderEntries], true, nil
	}
	return out, false, nil
}
