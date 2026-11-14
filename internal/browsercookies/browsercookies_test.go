package browsercookies

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var (
	used   = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	future = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
)

func names(cookies []Cookie) []string {
	var out []string
	for _, c := range cookies {
		out = append(out, c.Host+" "+c.Name+"="+c.Value)
	}
	slices.Sort(out)
	return out
}

func labels(profiles []Profile) []string {
	var out []string
	for _, p := range profiles {
		out = append(out, p.String())
	}
	return out
}

// firefoxHome lays out a Firefox root with a profiles.ini naming two
// profiles, one of which has a cookie database.
func firefoxHome(t *testing.T, root string) string {
	t.Helper()
	ini := "[General]\nStartWithLastProfile=1\n\n" +
		"[Profile1]\nName=default\nIsRelative=1\nPath=Profiles/aaaa.default\n\n" +
		"[Profile0]\nName=default-release\nIsRelative=1\nPath=Profiles/bbbb.default-release\nDefault=1\n"
	if err := os.MkdirAll(filepath.Join(root, "Profiles", "aaaa.default"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "Profiles", "bbbb.default-release")
}

func TestFirefoxProfilesWithACookieDatabaseAreFound(t *testing.T) {
	home := t.TempDir()
	d := dirs{appData: filepath.Join(home, "Roaming")}
	profile := firefoxHome(t, filepath.Join(d.appData, "Mozilla", "Firefox"))
	writeFirefoxDB(t, filepath.Join(profile, "cookies.sqlite"), 16, false, nil)

	got := labels(findProfiles("windows", d))
	if !slices.Equal(got, []string{"Firefox (default-release)"}) {
		t.Errorf("profiles = %q, want the one with a cookie database", got)
	}
}

func TestFirefoxProfilesAreFoundWhereEachSystemKeepsThem(t *testing.T) {
	for _, tc := range []struct {
		goos string
		root func(home string) string
		want string
	}{
		{"darwin", func(h string) string { return filepath.Join(h, "Library", "Application Support", "Firefox") }, "Firefox (default-release)"},
		{"linux", func(h string) string { return filepath.Join(h, ".mozilla", "firefox") }, "Firefox (default-release)"},
		{"linux", func(h string) string { return filepath.Join(h, "snap", "firefox", "common", ".mozilla", "firefox") }, "Firefox (default-release)"},
		{"linux", func(h string) string { return filepath.Join(h, ".librewolf") }, "LibreWolf (default-release)"},
		{"darwin", func(h string) string { return filepath.Join(h, "Library", "Application Support", "zen") }, "Zen (default-release)"},
	} {
		home := t.TempDir()
		profile := firefoxHome(t, tc.root(home))
		writeFirefoxDB(t, filepath.Join(profile, "cookies.sqlite"), 16, false, nil)
		got := labels(findProfiles(tc.goos, dirs{home: home, configHome: filepath.Join(home, ".config")}))
		if !slices.Equal(got, []string{tc.want}) {
			t.Errorf("%s under %s: profiles = %q, want %q", tc.goos, tc.root(home), got, tc.want)
		}
	}
}

func TestFirefoxCookiesOfTheDomainsAndBelowAreRead(t *testing.T) {
	home := t.TempDir()
	d := dirs{appData: home}
	profile := firefoxHome(t, filepath.Join(home, "Mozilla", "Firefox"))
	writeFirefoxDB(t, filepath.Join(profile, "cookies.sqlite"), 16, false, []firefoxRow{
		{host: ".patreon.com", name: "session_id", value: "S1", expiry: future.UnixMilli(), secure: true, lastAccessed: used},
		{host: "www.patreon.com", name: "seen", value: "1", expiry: future.UnixMilli(), lastAccessed: used},
		{host: ".evilpatreon.com", name: "session_id", value: "EVIL", expiry: future.UnixMilli()},
		{host: ".example.com", name: "sid", value: "OTHER", expiry: future.UnixMilli()},
		{origin: "^userContextId=2", host: ".patreon.com", name: "session_id", value: "CONTAINER", expiry: future.UnixMilli()},
		{origin: "^partitionKey=%28https%2Cexample.com%29", host: ".patreon.com", name: "embedded", value: "PART", expiry: future.UnixMilli()},
	})

	profiles := findProfiles("windows", d)
	if len(profiles) != 1 {
		t.Fatalf("profiles = %q", labels(profiles))
	}
	cookies, err := profiles[0].Cookies([]string{"patreon.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(cookies), []string{".patreon.com session_id=S1", "www.patreon.com seen=1"}; !slices.Equal(got, want) {
		t.Fatalf("cookies = %q, want %q", got, want)
	}
	for _, c := range cookies {
		if c.Name == "session_id" && (!c.Expires.Equal(future) || !c.Secure || !c.LastUsed.Equal(used)) {
			t.Errorf("session_id = %+v, want it to expire %v, be secure and last used %v", c, future, used)
		}
	}
}

func TestAnOlderFirefoxCountsExpiryInSeconds(t *testing.T) {
	home := t.TempDir()
	profile := firefoxHome(t, filepath.Join(home, "Mozilla", "Firefox"))
	writeFirefoxDB(t, filepath.Join(profile, "cookies.sqlite"), 15, false, []firefoxRow{
		{host: ".patreon.com", name: "session_id", value: "S1", expiry: future.Unix()},
	})
	cookies, err := findProfiles("windows", dirs{appData: home})[0].Cookies([]string{"patreon.com"})
	if err != nil || len(cookies) != 1 {
		t.Fatalf("cookies = %v, %v", cookies, err)
	}
	if !cookies[0].Expires.Equal(future) {
		t.Errorf("expires %v, want %v", cookies[0].Expires, future)
	}
}

func TestACookieFirefoxHasNotWrittenBackYetIsRead(t *testing.T) {
	home := t.TempDir()
	profile := firefoxHome(t, filepath.Join(home, "Mozilla", "Firefox"))
	db := writeFirefoxDB(t, filepath.Join(profile, "cookies.sqlite"), 16, true, nil)
	// The database stays open, as in a running Firefox, so the row is only in
	// the write-ahead log.
	if _, err := db.Exec(`INSERT INTO moz_cookies (name, value, host, path, expiry, lastAccessed, isSecure, isHttpOnly) VALUES ('session_id', 'FRESH', '.patreon.com', '/', ?, 0, 1, 1)`, future.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profile, "cookies.sqlite-wal")); err != nil {
		t.Fatalf("the fixture has no write-ahead log: %v", err)
	}
	cookies, err := findProfiles("windows", dirs{appData: home})[0].Cookies([]string{"patreon.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(cookies); !slices.Equal(got, []string{".patreon.com session_id=FRESH"}) {
		t.Errorf("cookies = %q, want the one in the write-ahead log", got)
	}
}

func TestReadingLeavesNoCopyBehind(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("TMPDIR", tmp)
	home := t.TempDir()
	profile := firefoxHome(t, filepath.Join(home, "Mozilla", "Firefox"))
	writeFirefoxDB(t, filepath.Join(profile, "cookies.sqlite"), 16, false, []firefoxRow{
		{host: ".patreon.com", name: "session_id", value: "S1", expiry: future.UnixMilli()},
	})
	if _, err := findProfiles("windows", dirs{appData: home})[0].Cookies([]string{"patreon.com"}); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadDir(tmp)
	if len(left) != 0 {
		t.Errorf("the temp folder still holds %d entries after the read", len(left))
	}
}

// chromiumHome lays out a Chromium user data folder with the given profiles
// and their names in Local State.
func chromiumHome(t *testing.T, root string, profiles map[string]string, state map[string]any) {
	t.Helper()
	cache := map[string]any{}
	for dir, name := range profiles {
		cache[dir] = map[string]any{"name": name}
	}
	if state == nil {
		state = map[string]any{}
	}
	state["profile"] = map[string]any{"info_cache": cache}
	writeLocalState(t, root, state)
}

func TestChromiumProfilesAreNamedAsTheBrowserNamesThem(t *testing.T) {
	home := t.TempDir()
	d := dirs{home: home, configHome: filepath.Join(home, ".config")}
	root := filepath.Join(d.configHome, "BraveSoftware", "Brave-Browser")
	chromiumHome(t, root, map[string]string{"Default": "Personal", "Profile 2": "Work"}, nil)
	writeChromiumDB(t, filepath.Join(root, "Default", "Network", "Cookies"), 24, nil)
	writeChromiumDB(t, filepath.Join(root, "Profile 2", "Cookies"), 24, nil)

	got := labels(findProfiles("linux", d))
	slices.Sort(got)
	if want := []string{"Brave (Personal)", "Brave (Work)"}; !slices.Equal(got, want) {
		t.Errorf("profiles = %q, want %q", got, want)
	}
}

func TestOperaIsItsOwnProfile(t *testing.T) {
	home := t.TempDir()
	d := dirs{home: home, configHome: filepath.Join(home, ".config")}
	root := filepath.Join(d.configHome, "opera")
	writeLocalState(t, root, map[string]any{})
	writeChromiumDB(t, filepath.Join(root, "Network", "Cookies"), 24, nil)
	if got := labels(findProfiles("linux", d)); !slices.Equal(got, []string{"Opera (Default)"}) {
		t.Errorf("profiles = %q", got)
	}
}

func linuxChromium(t *testing.T, rows []chromiumRow, metaVersion int) Profile {
	t.Helper()
	home := t.TempDir()
	d := dirs{home: home, configHome: filepath.Join(home, ".config")}
	root := filepath.Join(d.configHome, "chromium")
	chromiumHome(t, root, map[string]string{"Default": "Person 1"}, nil)
	writeChromiumDB(t, filepath.Join(root, "Default", "Network", "Cookies"), metaVersion, rows)
	profiles := findProfiles("linux", d)
	if len(profiles) != 1 {
		t.Fatalf("profiles = %q", labels(profiles))
	}
	return profiles[0]
}

func TestChromiumOnLinuxDecryptsWithItsBuiltInPassword(t *testing.T) {
	p := linuxChromium(t, []chromiumRow{
		{host: ".youtube.com", name: "SID", encrypted: sealCBC(t, "v10", "peanuts", 1, ".youtube.com", "SID-VALUE", true), expires: future, lastAccess: used, secure: true},
		{host: "www.youtube.com", name: "PREF", plain: "f6=8"},
		{host: ".google.com", name: "SID", encrypted: sealCBC(t, "v10", "peanuts", 1, ".google.com", "GOOGLE", true), expires: future},
	}, 24)
	cookies, err := p.Cookies([]string{"youtube.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(cookies), []string{".youtube.com SID=SID-VALUE", "www.youtube.com PREF=f6=8"}; !slices.Equal(got, want) {
		t.Fatalf("cookies = %q, want %q", got, want)
	}
	for _, c := range cookies {
		if c.Name == "SID" && (!c.Expires.Equal(future) || !c.LastUsed.Equal(used) || !c.Secure) {
			t.Errorf("SID = %+v, want it to expire %v and be last used %v", c, future, used)
		}
		if c.Name == "PREF" && !c.Expires.IsZero() {
			t.Errorf("a cookie without an expiry got %v", c.Expires)
		}
	}
}

func TestAnOlderChromiumDatabaseHasNoHostHashInFront(t *testing.T) {
	p := linuxChromium(t, []chromiumRow{
		{host: ".youtube.com", name: "SID", encrypted: sealCBC(t, "v10", "peanuts", 1, ".youtube.com", "SID-VALUE", false), expires: future},
	}, 23)
	cookies, err := p.Cookies([]string{"youtube.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(cookies); !slices.Equal(got, []string{".youtube.com SID=SID-VALUE"}) {
		t.Errorf("cookies = %q", got)
	}
}

func TestChromiumOnLinuxTakesTheKeyringPasswordForV11(t *testing.T) {
	prev := secretServicePassword
	var asked string
	secretServicePassword = func(app string) ([]byte, error) { asked = app; return []byte("keyring-secret"), nil }
	t.Cleanup(func() { secretServicePassword = prev })

	p := linuxChromium(t, []chromiumRow{
		{host: ".youtube.com", name: "SID", encrypted: sealCBC(t, "v11", "keyring-secret", 1, ".youtube.com", "SID-VALUE", true), expires: future},
	}, 24)
	cookies, err := p.Cookies([]string{"youtube.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(cookies); !slices.Equal(got, []string{".youtube.com SID=SID-VALUE"}) {
		t.Errorf("cookies = %q", got)
	}
	if asked != "chromium" {
		t.Errorf("the keyring was asked for %q, want chromium", asked)
	}
}

func TestV11CookiesWithoutAKeyringAreUnreadable(t *testing.T) {
	prev := secretServicePassword
	secretServicePassword = func(string) ([]byte, error) { return nil, errors.New("no secret service") }
	t.Cleanup(func() { secretServicePassword = prev })

	p := linuxChromium(t, []chromiumRow{
		{host: ".youtube.com", name: "SID", encrypted: sealCBC(t, "v11", "keyring-secret", 1, ".youtube.com", "SID-VALUE", true), expires: future},
	}, 24)
	if _, err := p.Cookies([]string{"youtube.com"}); !errors.Is(err, ErrNoKey) {
		t.Errorf("err = %v, want ErrNoKey", err)
	}
}

func TestChromiumOnMacTakesTheKeychainPassword(t *testing.T) {
	prev := keychainPassword
	var service, account string
	keychainPassword = func(s, a string) ([]byte, error) { service, account = s, a; return []byte("mac-secret"), nil }
	t.Cleanup(func() { keychainPassword = prev })

	home := t.TempDir()
	root := filepath.Join(home, "Library", "Application Support", "Microsoft Edge")
	chromiumHome(t, root, map[string]string{"Default": "Profile 1"}, nil)
	writeChromiumDB(t, filepath.Join(root, "Default", "Cookies"), 24, []chromiumRow{
		{host: ".patreon.com", name: "session_id", encrypted: sealCBC(t, "v10", "mac-secret", 1003, ".patreon.com", "S1", true), expires: future},
	})
	profiles := findProfiles("darwin", dirs{home: home})
	if len(profiles) != 1 {
		t.Fatalf("profiles = %q", labels(profiles))
	}
	cookies, err := profiles[0].Cookies([]string{"patreon.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(cookies); !slices.Equal(got, []string{".patreon.com session_id=S1"}) {
		t.Errorf("cookies = %q", got)
	}
	if service != "Microsoft Edge Safe Storage" || account != "Microsoft Edge" {
		t.Errorf("the keychain was asked for %q / %q", service, account)
	}
}

func TestAppBoundCookiesOfTheSiteAreReportedAsUnreadable(t *testing.T) {
	p := linuxChromium(t, []chromiumRow{
		{host: ".youtube.com", name: "SID", encrypted: append([]byte("v20"), make([]byte, 40)...), expires: future},
	}, 24)
	if _, err := p.Cookies([]string{"youtube.com"}); !errors.Is(err, ErrAppBound) {
		t.Errorf("err = %v, want ErrAppBound", err)
	}
}

func TestAppBoundCookiesOfOtherSitesDoNotMatter(t *testing.T) {
	p := linuxChromium(t, []chromiumRow{
		{host: ".google.com", name: "SID", encrypted: append([]byte("v20"), make([]byte, 40)...), expires: future},
		{host: ".youtube.com", name: "PREF", plain: "f6=8"},
	}, 24)
	cookies, err := p.Cookies([]string{"youtube.com"})
	if err != nil || len(cookies) != 1 {
		t.Errorf("cookies = %q, %v; want YouTube's one plain cookie", names(cookies), err)
	}
}

func TestPartitionedChromiumCookiesAreLeftOut(t *testing.T) {
	p := linuxChromium(t, []chromiumRow{
		{host: ".youtube.com", name: "SID", plain: "TOP"},
		{host: ".youtube.com", name: "SID", plain: "EMBEDDED", topFrame: "https://example.com"},
	}, 24)
	cookies, err := p.Cookies([]string{"youtube.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(cookies); !slices.Equal(got, []string{".youtube.com SID=TOP"}) {
		t.Errorf("cookies = %q", got)
	}
}

func TestReadNamesTheBrowsersItCouldNotRead(t *testing.T) {
	good := linuxChromium(t, []chromiumRow{{host: ".youtube.com", name: "PREF", plain: "f6=8"}}, 24)
	bound := linuxChromium(t, []chromiumRow{{host: ".youtube.com", name: "SID", encrypted: append([]byte("v20"), make([]byte, 40)...)}}, 24)
	bound.Browser = "Chrome"
	empty := linuxChromium(t, nil, 24)

	found, unreadable := read([]Profile{good, bound, empty}, []string{"youtube.com"})
	if len(found) != 1 || found[0].Profile.String() != good.String() || len(found[0].Cookies) != 1 {
		t.Errorf("found = %+v, want the one readable profile with cookies", found)
	}
	if len(unreadable) != 1 || unreadable[0].Profile.Browser != "Chrome" || !errors.Is(unreadable[0].Err, ErrAppBound) {
		t.Errorf("unreadable = %+v, want Chrome for its app-bound key", unreadable)
	}
}

func TestDomainMatching(t *testing.T) {
	for _, tc := range []struct {
		host string
		want bool
	}{
		{"patreon.com", true},
		{".patreon.com", true},
		{"www.patreon.com", true},
		{".c10.patreonusercontent.com", false},
		{"evilpatreon.com", false},
		{"patreon.com.example", false},
		{"PATREON.COM", true},
	} {
		if got := matches(tc.host, []string{"patreon.com"}); got != tc.want {
			t.Errorf("matches(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

// A process that ends in the middle of a read leaves its copy behind, and it
// holds the cookies' values.
func TestACopyLeftBehindIsSweptAway(t *testing.T) {
	tmp := t.TempDir()
	old := filepath.Join(tmp, "kl-browsercookies-1")
	fresh := filepath.Join(tmp, "kl-browsercookies-2")
	other := filepath.Join(tmp, "someone-elses")
	for _, d := range []string{old, fresh, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "cookies.db"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	long := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, long, long); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, long, long); err != nil {
		t.Fatal(err)
	}
	sweep(tmp, time.Now())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("an hour-old copy is still there (stat err = %v)", err)
	}
	for _, d := range []string{fresh, other} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s went too: %v", d, err)
		}
	}
}
