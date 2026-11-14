package browsercookies

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type family int

const (
	firefox family = iota
	chromium
)

// browser is one browser KnightLoader knows where to look for. The paths
// start with %APPDATA%, %LOCALAPPDATA%, ~ or $XDG_CONFIG_HOME.
type browser struct {
	name   string
	family family
	// win, mac and linux are where the browser keeps its profiles: a folder
	// with profiles.ini for Firefox, the user data folder for Chromium.
	win, mac, linux []string
	// macKey is the name Chromium files its key under in the macOS Keychain,
	// as "<macKey> Safe Storage", and linuxKey, lower-cased, the application
	// the Linux keyring knows it by.
	macKey, linuxKey string
	// single is a browser whose user data folder is its one profile.
	single bool
}

var browsers = []browser{
	{
		name: "Firefox", family: firefox,
		win: []string{"%APPDATA%/Mozilla/Firefox", "%LOCALAPPDATA%/Packages/Mozilla.Firefox_n80bbvh6b1yt2/LocalCache/Roaming/Mozilla/Firefox"},
		mac: []string{"~/Library/Application Support/Firefox"},
		linux: []string{"$XDG_CONFIG_HOME/mozilla/firefox", "~/.mozilla/firefox", "~/snap/firefox/common/.mozilla/firefox",
			"~/.var/app/org.mozilla.firefox/config/mozilla/firefox", "~/.var/app/org.mozilla.firefox/.mozilla/firefox"},
	},
	{
		name: "LibreWolf", family: firefox,
		win:   []string{"%APPDATA%/librewolf"},
		mac:   []string{"~/Library/Application Support/librewolf"},
		linux: []string{"~/.librewolf", "$XDG_CONFIG_HOME/librewolf", "~/.var/app/io.gitlab.librewolf-community/.librewolf"},
	},
	{
		name: "Waterfox", family: firefox,
		win:   []string{"%APPDATA%/Waterfox"},
		mac:   []string{"~/Library/Application Support/Waterfox"},
		linux: []string{"~/.waterfox", "~/.var/app/net.waterfox.waterfox/.waterfox"},
	},
	{
		name: "Zen", family: firefox,
		win:   []string{"%APPDATA%/zen"},
		mac:   []string{"~/Library/Application Support/zen"},
		linux: []string{"~/.zen", "$XDG_CONFIG_HOME/zen", "~/.var/app/app.zen_browser.zen/.zen"},
	},
	{
		name: "Edge", family: chromium, macKey: "Microsoft Edge", linuxKey: "Chromium",
		win:   []string{"%LOCALAPPDATA%/Microsoft/Edge/User Data"},
		mac:   []string{"~/Library/Application Support/Microsoft Edge"},
		linux: []string{"$XDG_CONFIG_HOME/microsoft-edge"},
	},
	{
		name: "Brave", family: chromium, macKey: "Brave", linuxKey: "Brave",
		win:   []string{"%LOCALAPPDATA%/BraveSoftware/Brave-Browser/User Data"},
		mac:   []string{"~/Library/Application Support/BraveSoftware/Brave-Browser"},
		linux: []string{"$XDG_CONFIG_HOME/BraveSoftware/Brave-Browser", "~/.var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser"},
	},
	{
		name: "Opera", family: chromium, macKey: "Opera", linuxKey: "Chromium", single: true,
		win:   []string{"%APPDATA%/Opera Software/Opera Stable"},
		mac:   []string{"~/Library/Application Support/com.operasoftware.Opera"},
		linux: []string{"$XDG_CONFIG_HOME/opera"},
	},
	{
		name: "Opera GX", family: chromium, single: true,
		win: []string{"%APPDATA%/Opera Software/Opera GX Stable"},
	},
	{
		name: "Vivaldi", family: chromium, macKey: "Vivaldi", linuxKey: "Chrome",
		win:   []string{"%LOCALAPPDATA%/Vivaldi/User Data"},
		mac:   []string{"~/Library/Application Support/Vivaldi"},
		linux: []string{"$XDG_CONFIG_HOME/vivaldi"},
	},
	{
		name: "Chromium", family: chromium, macKey: "Chromium", linuxKey: "Chromium",
		win:   []string{"%LOCALAPPDATA%/Chromium/User Data"},
		mac:   []string{"~/Library/Application Support/Chromium"},
		linux: []string{"$XDG_CONFIG_HOME/chromium", "~/snap/chromium/common/chromium", "~/.var/app/org.chromium.Chromium/config/chromium"},
	},
	{
		name: "Chrome", family: chromium, macKey: "Chrome", linuxKey: "Chrome",
		win:   []string{"%LOCALAPPDATA%/Google/Chrome/User Data"},
		mac:   []string{"~/Library/Application Support/Google/Chrome"},
		linux: []string{"$XDG_CONFIG_HOME/google-chrome", "~/.var/app/com.google.Chrome/config/google-chrome"},
	},
}

// dirs are the user's folders the browsers' paths start from.
type dirs struct {
	home, appData, localAppData, configHome string
}

func userDirs() dirs {
	home, _ := os.UserHomeDir()
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" && home != "" {
		config = filepath.Join(home, ".config")
	}
	return dirs{home: home, appData: os.Getenv("APPDATA"), localAppData: os.Getenv("LOCALAPPDATA"), configHome: config}
}

// expand resolves a table path, or returns "" when its base is not known.
func (d dirs) expand(p string) string {
	for prefix, base := range map[string]string{"%APPDATA%/": d.appData, "%LOCALAPPDATA%/": d.localAppData, "~/": d.home, "$XDG_CONFIG_HOME/": d.configHome} {
		if rest, ok := strings.CutPrefix(p, prefix); ok {
			if base == "" {
				return ""
			}
			return filepath.Join(base, filepath.FromSlash(rest))
		}
	}
	return ""
}

// findProfiles lists the profiles with a cookie database of every known
// browser, as goos lays them out.
func findProfiles(goos string, d dirs) []Profile {
	var out []Profile
	seen := map[string]bool{}
	for i := range browsers {
		b := &browsers[i]
		roots := b.linux
		switch goos {
		case "windows":
			roots = b.win
		case "darwin":
			roots = b.mac
		}
		for _, r := range roots {
			root := d.expand(r)
			if root == "" {
				continue
			}
			var found []Profile
			if b.family == firefox {
				found = firefoxProfiles(b, root)
			} else {
				found = chromiumProfiles(b, root)
			}
			for _, p := range found {
				// Two of the paths may lead to one folder through a link.
				key := p.db
				if real, err := filepath.EvalSymlinks(p.db); err == nil {
					key = real
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				p.goos = goos
				out = append(out, p)
			}
		}
	}
	return out
}

// firefoxProfiles reads profiles.ini, or without one takes every folder that
// holds a cookies.sqlite.
func firefoxProfiles(b *browser, root string) []Profile {
	var out []Profile
	add := func(name, dir string) {
		db := filepath.Join(dir, "cookies.sqlite")
		if fileExists(db) {
			out = append(out, Profile{Browser: b.name, Name: name, kind: b, root: root, db: db})
		}
	}
	sections := readINI(filepath.Join(root, "profiles.ini"))
	if sections == nil {
		for _, pattern := range []string{"*", "Profiles/*"} {
			matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
			for _, dir := range matches {
				add(filepath.Base(dir), dir)
			}
		}
		return out
	}
	for _, s := range sections {
		if !strings.HasPrefix(s.name, "Profile") || s.keys["Path"] == "" {
			continue
		}
		dir := filepath.FromSlash(s.keys["Path"])
		if s.keys["IsRelative"] != "0" {
			dir = filepath.Join(root, dir)
		}
		name := s.keys["Name"]
		if name == "" {
			name = filepath.Base(dir)
		}
		add(name, dir)
	}
	return out
}

type iniSection struct {
	name string
	keys map[string]string
}

// readINI reads the sections of an ini file in order, or nil without one.
func readINI(path string) []iniSection {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := []iniSection{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if name, ok := strings.CutPrefix(line, "["); ok {
			out = append(out, iniSection{name: strings.TrimSuffix(name, "]"), keys: map[string]string{}})
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && len(out) > 0 {
			out[len(out)-1].keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// chromiumProfiles takes the profiles Local State lists, under the names the
// browser gives them.
func chromiumProfiles(b *browser, root string) []Profile {
	if b.single {
		if db := chromiumDB(root); db != "" {
			return []Profile{{Browser: b.name, Name: "Default", kind: b, root: root, db: db}}
		}
		return nil
	}
	var state struct {
		Profile struct {
			InfoCache map[string]struct {
				Name string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	raw, err := os.ReadFile(filepath.Join(root, "Local State"))
	if err != nil {
		return nil
	}
	_ = json.Unmarshal(raw, &state)
	dirs := make([]string, 0, len(state.Profile.InfoCache))
	for dir := range state.Profile.InfoCache {
		dirs = append(dirs, dir)
	}
	if len(dirs) == 0 {
		dirs = append(dirs, "Default")
	}
	slices.Sort(dirs)
	var out []Profile
	for _, dir := range dirs {
		// The key comes from Local State, so it must not lead out of the
		// browser's folder.
		if !filepath.IsLocal(dir) {
			continue
		}
		db := chromiumDB(filepath.Join(root, dir))
		if db == "" {
			continue
		}
		name := state.Profile.InfoCache[dir].Name
		if name == "" {
			name = dir
		}
		out = append(out, Profile{Browser: b.name, Name: name, kind: b, root: root, db: db})
	}
	return out
}

// chromiumDB is a profile folder's cookie database, which newer versions keep
// under Network, or "".
func chromiumDB(dir string) string {
	for _, p := range []string{filepath.Join(dir, "Network", "Cookies"), filepath.Join(dir, "Cookies")} {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
