package api

// The folder chooser's two requests: which directories are under this path,
// and make a new one here. Every folder field in the interface uses the same
// picker, so the template rule in splitTemplate lives in one place.
//
// The boundary is the filesystem this process can see, which in the container
// is the image plus the operator's mounts, exactly where downloads can land.
// KL_BROWSE_ROOTS narrows it, and links are resolved, Windows junctions and
// mounted folders included, so a link to / cannot widen it again. Listing
// answers with directory names only, never files, sizes or contents, and
// creating makes one empty folder inside a folder that listing would have
// shown.

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/realpath"
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

// createRefusal is a folder that was not created. Code names the reason for
// the interface to translate, and text says the same for everybody else.
type createRefusal struct {
	status int
	code   string
	text   string
}

func (e createRefusal) Error() string { return e.text }

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

	reg.Add(http.MethodPost, "/api/folders",
		"create one empty folder, named by a single plain name, inside a folder the chooser may list",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Parent string `json:"parent"`
				Name   string `json:"name"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			path, err := createFolder(body.Parent, body.Name)
			if err != nil {
				ref := createRefusal{status: http.StatusInternalServerError, text: err.Error()}
				var bounds folderRefusal
				if !errors.As(err, &ref) && errors.As(err, &bounds) {
					ref = createRefusal{status: bounds.status, text: bounds.reason}
				}
				writeJSONStatus(w, ref.status, map[string]string{"error": ref.text, "code": ref.code})
				return
			}
			writeJSONStatus(w, http.StatusCreated, map[string]string{"path": path})
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

	b, err := browseRoots(fixed)
	if err != nil {
		return folderListing{}, err
	}

	listed := deepestExisting(fixed)
	if fi, err := os.Stat(listed); err != nil || !fi.IsDir() {
		return folderListing{}, folderRefusal{http.StatusNotFound,
			"there is no folder at or above " + fixed + " that this instance can read"}
	}
	real, ok := b.resolve(listed)
	if !ok {
		return folderListing{}, folderRefusal{http.StatusForbidden,
			"this instance may not list " + listed + "; it is outside " + strings.Join(b.roots, ", ")}
	}

	entries, truncated, err := readFolders(real, listed, b)
	if err != nil {
		return folderListing{}, err
	}

	out := folderListing{
		Path:      fixed,
		Tail:      tail,
		Exists:    listed == fixed,
		Listed:    listed,
		Roots:     b.roots,
		Entries:   entries,
		Truncated: truncated,
	}
	// A parent outside the boundary is not offered at all.
	if parent := filepath.Dir(listed); parent != listed {
		if _, ok := b.resolve(parent); ok {
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

// boundary is the part of the filesystem one request may see.
type boundary struct {
	// roots are the tops the interface offers a way back to.
	roots []string
	// open is set when KL_BROWSE_ROOTS is unset. roots then only names the
	// volume the request started on, and a folder on another volume that a
	// link or junction leads to is still inside.
	open bool
}

// browseRoots is the boundary for one request.
func browseRoots(p string) (boundary, error) {
	set := strings.TrimSpace(os.Getenv(envBrowseRoots))
	if set == "" {
		return boundary{roots: []string{volumeRoot(p)}, open: true}, nil
	}
	var out []string
	for _, part := range filepath.SplitList(set) {
		part = strings.TrimSpace(part)
		if part == "" || !filepath.IsAbs(part) {
			continue
		}
		// Resolved, so a linked root still contains its resolved children. A
		// root that does not exist yet is kept as written, which gives an empty
		// chooser rather than a wider one.
		if real, err := realpath.Resolve(part); err == nil {
			out = append(out, filepath.Clean(real))
			continue
		}
		out = append(out, filepath.Clean(part))
	}
	if len(out) == 0 {
		// A typo in the variable must not widen the chooser back to
		// everything.
		return boundary{}, folderRefusal{http.StatusInternalServerError,
			envBrowseRoots + " is set but names no absolute folder, so nothing may be listed"}
	}
	return boundary{roots: out}, nil
}

// resolve resolves p and reports whether what it really points at is inside
// the boundary. Everything that reads or creates a directory goes through here.
func (b boundary) resolve(p string) (string, bool) {
	real, err := realpath.Resolve(p)
	if err != nil {
		return "", false
	}
	if b.open {
		return real, true
	}
	for _, root := range b.roots {
		if within(root, real) {
			return real, true
		}
	}
	return "", false
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
func readFolders(real, display string, b boundary) ([]folderEntry, bool, error) {
	items, err := os.ReadDir(real)
	if err != nil {
		return nil, false, err
	}
	out := make([]folderEntry, 0, len(items))
	for _, it := range items {
		name := it.Name()
		switch {
		case it.IsDir():
		case it.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0:
			// ReadDir reports a link as a link, and a Windows junction as
			// irregular, so either is offered only once it resolves to a
			// directory inside the boundary.
			target := filepath.Join(real, name)
			if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
				continue
			}
			if _, ok := b.resolve(target); !ok {
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

// createFolder makes name inside parent, which must be a folder listFolders
// would list, and returns the new path spelled through parent like the
// entries of a listing. The folder is made inside what parent resolves to,
// and a single checked name cannot lead anywhere else.
func createFolder(parent, name string) (string, error) {
	if !filepath.IsAbs(parent) {
		return "", createRefusal{http.StatusBadRequest, "parent", "the folder to create it in must be an absolute path"}
	}
	parent = filepath.Clean(parent)
	if err := checkFolderName(name); err != nil {
		return "", err
	}
	b, err := browseRoots(parent)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
		return "", createRefusal{http.StatusNotFound, "missing", "there is no folder at " + parent + " that this instance can see"}
	}
	real, ok := b.resolve(parent)
	if !ok {
		return "", createRefusal{http.StatusForbidden, "outside",
			"this instance may not create folders in " + parent + "; it is outside " + strings.Join(b.roots, ", ")}
	}
	err = os.Mkdir(filepath.Join(real, name), 0o755)
	switch {
	case err == nil:
		return filepath.Join(parent, name), nil
	case errors.Is(err, fs.ErrExist):
		return "", createRefusal{http.StatusConflict, "exists", "there is already something named " + name + " in " + parent}
	case errors.Is(err, fs.ErrPermission):
		return "", createRefusal{http.StatusForbidden, "denied",
			"this instance has no permission to create a folder in " + parent}
	}
	return "", err
}

// maxFolderNameBytes is the longest name ext4, XFS and Btrfs accept.
const maxFolderNameBytes = 255

// windowsDeviceNames are names Windows reads as a device whatever follows the
// first dot, so "con" and "con.old" both address the console.
var windowsDeviceNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// checkFolderName accepts a single plain folder name. The Windows rules apply
// on every platform, since a download folder on a Linux server is often
// opened over SMB from Windows, which cannot open such a name. The angle
// brackets are refused anywhere, because the download folder reads them as
// the start of a variable.
func checkFolderName(name string) error {
	refuse := func(code, text string) error {
		return createRefusal{http.StatusBadRequest, code, text}
	}
	switch {
	case strings.TrimSpace(name) == "":
		return refuse("empty", "the new folder needs a name")
	case name == "." || name == "..":
		return refuse("dots", `"." and ".." are not folder names`)
	case strings.ContainsAny(name, `/\`):
		return refuse("separator", `a folder name cannot contain / or \; create one level at a time`)
	case len(name) > maxFolderNameBytes:
		return refuse("tooLong", "a folder name can be at most 255 bytes long")
	case strings.ContainsAny(name, `<>:"|?*`):
		return refuse("character", `a folder name cannot contain < > : " | ? or *`)
	case strings.HasSuffix(name, ".") || strings.HasSuffix(name, " "):
		return refuse("trailing", "a folder name cannot end in a dot or a space")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return refuse("character", "a folder name cannot contain control characters")
		}
	}
	base, _, _ := strings.Cut(name, ".")
	if windowsDeviceNames[strings.ToLower(strings.TrimRight(base, " "))] {
		return refuse("reserved", name+" is a device name on Windows, not a folder name")
	}
	return nil
}
