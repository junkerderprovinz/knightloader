// Package update checks whether a newer KnightLoader release exists, for the
// desktop build only - a container has no update mechanism of its own to
// drive (see routes_features.go's updaterReason: the deployment that runs a
// container already detects and performs its own update).
//
// This package only CHECKS and reports. It does not download, verify or
// apply anything: a desktop auto-updater that silently replaces its own
// binary is a real attack surface (an unverified download executed with the
// user's own privileges), and building that safely - code-signature
// verification, an atomic swap with rollback, per-platform install
// mechanics - is its own careful piece of work, not something to bolt onto
// a UI change. What ships here is the honest first slice: tell the user a
// newer version exists and hand them the release page to fetch it
// themselves, the same tier of "auto-update" a great many desktop apps ship
// before ever attempting a silent one.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// releasesAPI is GitHub's own "latest release" endpoint - not the releases
// list, which would need pagination and a definition of "latest" this
// package would then have to reinvent. Unauthenticated: fine for a public
// repo, and returns 404 for a private one (github.com/junkerderprovinz/
// knightloader is private until the v1 release - see the project's own
// vault note) rather than anything Check needs to treat specially, since a
// 404 already reads as "not available" through the same status check every
// other non-200 response gets.
const releasesAPI = "https://api.github.com/repos/junkerderprovinz/knightloader/releases/latest"

// Info is what the settings page shows. Checked is false whenever Check
// could not complete (network error, private repo, rate limit) - distinct
// from Available=false, which means the check succeeded and the answer was
// "no, you already have the latest".
type Info struct {
	Checked   bool   `json:"checked"`
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	URL       string `json:"url,omitempty"`
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// Check compares `current` (buildinfo.Version) against the latest GitHub
// release tag. "dev" - every untagged local/main build - can never be
// meaningfully compared to a released version, so it is reported as
// checked-but-not-available rather than a version parse this package would
// have to guess at.
func Check(ctx context.Context, current string) Info {
	info := Info{Current: current}
	if current == "" || current == "dev" {
		info.Checked = true
		return info
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return info
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return info
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 404 (private repo, no releases yet), rate-limited, or any other
		// non-200 - all read the same way to a caller: could not check.
		return info
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return info
	}

	info.Checked = true
	latest := strings.TrimPrefix(rel.TagName, "v")
	currentTrimmed := strings.TrimPrefix(current, "v")
	newer, ok := isNewer(latest, currentTrimmed)
	if !ok {
		// A tag that does not parse as X.Y.Z is not something this package
		// can order against the running version - reported as checked, not
		// available, rather than guessing.
		return info
	}
	info.Available = newer
	if newer {
		info.Latest = rel.TagName
		info.URL = rel.HTMLURL
	}
	return info
}

// isNewer reports whether `latest` sorts after `current` under plain X.Y.Z
// comparison - the project's own versioning convention (patch/minor/major,
// no pre-release suffixes to weigh) has no use for a general semver library
// here. ok is false when either string does not parse as three numeric
// parts.
func isNewer(latest, current string) (newer bool, ok bool) {
	lp, lok := parts(latest)
	cp, cok := parts(current)
	if !lok || !cok {
		return false, false
	}
	for i := 0; i < 3; i++ {
		if lp[i] != cp[i] {
			return lp[i] > cp[i], true
		}
	}
	return false, true
}

func parts(v string) ([3]int, bool) {
	var out [3]int
	seg := strings.SplitN(v, ".", 3)
	if len(seg) != 3 {
		return out, false
	}
	for i, s := range seg {
		n, err := strconv.Atoi(s)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// String is a small debug/log helper, not used by the API response itself.
func (i Info) String() string {
	if !i.Checked {
		return "update check: failed"
	}
	if !i.Available {
		return fmt.Sprintf("update check: %s is current", i.Current)
	}
	return fmt.Sprintf("update check: %s available (running %s)", i.Latest, i.Current)
}
