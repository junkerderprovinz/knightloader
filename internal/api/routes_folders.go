package api

// The folder chooser's one question: which directories are under this path.
// Every folder field in the interface uses the same picker, so the template
// rule in splitTemplate lives in one place.
//
// The boundary is the filesystem this process can see, which in the container
// is the image plus the operator's mounts, exactly where downloads can land.
// KL_BROWSE_ROOTS narrows it, and symlinks are resolved so a link to / cannot
// widen it again. The route answers with directory names only, never files,
// sizes or contents.

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

// envBrowseRoots narrows the chooser to a list of folders, separated like a
// path list on this platform. Unset means the whole visible filesystem.
const envBrowseRoots = "KL_BROWSE_ROOTS"

// maxFolderEntries caps one listing; the response says when it cut, so the
// interface can point at the path box instead.
const maxFolderEntries = 2000

// folderEntry is one directory offered for the next click. Path is built here
// because the interface does not know this host's separator.
type folderEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// folderListing is one place in the filesystem as the chooser needs it. Path
// is what was asked for and Listed the deepest existing folder above it, so a
// folder that is about to be created shows as new rather than as an empty
// dialog.
type folderListing struct {
	// Path is the folder the chooser points at, cleaned, without any template
	// tail.
	Path string `json:"path"`
	// Tail is the <jd:...> part cut off Path, leading separator included; the
	// interface puts it back on (see splitTemplate).
	Tail string `json:"tail"`
	// Exists reports whether Path is a directory today.
	Exists bool `json:"exists"`
	// Listed is the folder Entries actually describes.
	Listed string `json:"listed"`
	// Parent is one level above Listed, or "" at the top of the boundary.
	Parent string `json:"parent"`
	// Roots is the boundary, so the interface can offer a way back to it.
	Roots []string `json:"roots"`
	// Entries are the sub-directories of Listed, sorted, folders only.
	Entries []folderEntry `json:"entries"`
	// Truncated says the list was cut at maxFolderEntries.
	Truncated bool `json:"truncated"`
}

// folderRefusal is a refusal that carries its HTTP status.
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
			// Without a path the chooser starts where downloads go.
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
					// The error names the folder, which a bare "forbidden"
					// would not.
					http.Error(w, err.Error(), http.StatusForbidden)
				default:
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}
			writeJSON(w, out)
		})
}

// listFolders is the whole route, apart from the handler so the rules can be
// tested without a socket.
func listFolders(raw string) (folderListing, error) {
	fixed, tail := splitTemplate(raw)
	if strings.TrimSpace(fixed) == "" {
		fixed = defaultStart()
	}
	if !filepath.IsAbs(fixed) {
		// As in settings.Validate: a relative path would depend on the
		// process's working directory.
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
	// A parent outside the boundary is not offered at all.
	if parent := filepath.Dir(listed); parent != listed {
		if _, ok := resolveWithin(parent, roots); ok {
			out.Parent = parent
		}
	}
	return out, nil
}

// splitTemplate cuts a download folder into the real path and the pathvars
// template, so "/downloads/<jd:date>/<jd:hoster>" becomes "/downloads" and
// "/<jd:date>/<jd:hoster>". Browsing only ever replaces the fixed part, so the
// user's naming scheme survives.
//
// Cutting at the first segment containing "<" must match settings.fixedPrefix,
// which decides the folder the app creates;
// TestTheSplitMatchesTheFolderThatGetsCreated keeps the two in step.
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
			// Everything below the root is a placeholder. The tail keeps its
			// leading separator, so the caller re-assembles by concatenation.
			fixed = sep
		}
		return fixed, sep + strings.Join(parts[i:], sep)
	}
	return dir, ""
}

// browseRoots is the boundary for one request.
func browseRoots(p string) ([]string, error) {
	set := strings.TrimSpace(os.Getenv(envBrowseRoots))
	if set == "" {
		return []string{volumeRoot(p)}, nil
	}
	var out []string
	for _, part := range filepath.SplitList(set) {
		part = strings.TrimSpace(part)
		if part == "" || !filepath.IsAbs(part) {
			continue
		}
		// Resolved, so a symlinked root still contains its resolved children.
		// A root that does not exist yet is kept as written, which gives an
		// empty chooser rather than a wider one.
		if real, err := filepath.EvalSymlinks(part); err == nil {
			out = append(out, filepath.Clean(real))
			continue
		}
		out = append(out, filepath.Clean(part))
	}
	if len(out) == 0 {
		// A typo in the variable must not widen the chooser back to
		// everything.
		return nil, folderRefusal{http.StatusInternalServerError,
			envBrowseRoots + " is set but names no absolute folder, so nothing may be listed"}
	}
	return out, nil
}

// volumeRoot is the top of the filesystem p lives on: "/", or the drive on
// Windows.
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

// deepestExisting walks up until it finds a directory that exists.
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

// within reports whether p is root or sits below it. filepath.Rel knows the
// platform's rules, such as case-insensitive comparison on Windows.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	// Not HasPrefix(rel, ".."), which would also match a folder named "..old".
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// readFolders lists the sub-directories of an already-resolved directory.
// real is what is read and display the path the caller asked for; entries are
// named after display, so a symlink does not rewrite the user's setting.
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
			// ReadDir reports a link as a link, so a symlinked folder is
			// offered only once it resolves to a directory inside the
			// boundary.
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
	// Case-insensitive, as people expect folder lists to be sorted.
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
