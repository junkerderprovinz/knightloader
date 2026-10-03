package jdimport_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/jdimport"
	"github.com/junkerderprovinz/knightloader/internal/jdimport/jdimporttest"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

func readZip(t *testing.T, data []byte) *jdimport.Config {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jdimport.Read(zr)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestReadFindsTheCfgFolderInsideAZipOfTheWholeInstall(t *testing.T) {
	t.Parallel()
	data := jdimporttest.Zip(t, "JDownloader 2.0/cfg/", jdimporttest.Config{
		DownloadDir: "/output",
		Passwords:   []string{"secret", "other"},
	})
	cfg := readZip(t, data)
	if cfg.DownloadDir != "/output" {
		t.Errorf("DownloadDir = %q, want /output", cfg.DownloadDir)
	}
	if !reflect.DeepEqual(cfg.ArchivePasswords, []string{"secret", "other"}) {
		t.Errorf("ArchivePasswords = %q", cfg.ArchivePasswords)
	}
}

func TestReadRefusesAFolderWithoutJDownloadersFiles(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"cfg/notes.txt": {Data: []byte("hello")}}
	if _, err := jdimport.Read(fsys); !errors.Is(err, jdimport.ErrNoConfig) {
		t.Fatalf("err = %v, want ErrNoConfig", err)
	}
}

func TestAccountsOpenWithJDownloadersKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdimporttest.Config{Accounts: []jdimporttest.Account{
		{Host: "rapidgator.net", User: "alice", Password: "pw1", Enabled: true},
		{Host: "torbox.app", User: "alice@example.org", Password: "0123456789abcdef0123456789abcdef", Enabled: false},
	}})
	cfg, err := jdimport.Read(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Problems) > 0 {
		t.Fatalf("problems: %+v", cfg.Problems)
	}
	if len(cfg.Accounts) != 2 {
		t.Fatalf("got %d accounts, want 2", len(cfg.Accounts))
	}
	rg := cfg.Accounts[0]
	if rg.Host != "rapidgator.net" || rg.User != "alice" || rg.Password != "pw1" || !rg.Enabled {
		t.Errorf("rapidgator account = %+v", rg)
	}
	if cfg.Accounts[1].Enabled {
		t.Error("the TorBox account was off in JDownloader and reads as on")
	}
}

func TestAnAccountListThatDoesNotOpenIsAProblemNotAnError(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"org.jdownloader.settings.AccountSettings.accounts.ejs": {Data: bytes.Repeat([]byte{7}, 32)},
	}
	cfg, err := jdimport.Read(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Problems) != 1 || cfg.Problems[0].Code != "accountsLocked" {
		t.Fatalf("problems = %+v, want one accountsLocked", cfg.Problems)
	}
}

func TestReadingLeavesTheFolderAsItWas(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdimporttest.Config{
		Accounts:    []jdimporttest.Account{{Host: "rapidgator.net", User: "a", Password: "b", Enabled: true}},
		DownloadDir: "/output",
		Packages:    []jdimporttest.Package{{Name: "p", Links: []jdimporttest.Link{{URL: "https://example.org/a", Enabled: true}}}},
	})
	before := snapshot(t, dir)
	if _, err := jdimport.Read(os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("reading changed the cfg folder")
	}
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		info, _ := e.Info()
		out[e.Name()] = string(b) + info.ModTime().String()
	}
	return out
}

func TestTheNewestDownloadListThatOpensIsRead(t *testing.T) {
	t.Parallel()
	good := jdimporttest.DownloadList(t, []jdimporttest.Package{{
		Name: "Show S01", Comment: "from the forum",
		Links: []jdimporttest.Link{
			{URL: "https://rapidgator.net/file/1", Enabled: true, Properties: map[string]any{"PWLIST": []any{"pw-a", "pw-b"}, "pass": "dl"}},
			{URL: "directhttp://https://cdn.example.org/2.bin", Enabled: true},
			{URL: "httpsviajd://cdn.example.org/3.bin", Enabled: true},
			{URL: "https://rapidgator.net/file/4", Enabled: true, State: "FINISHED"},
			{URL: "https://rapidgator.net/file/5", Enabled: false},
			{URL: "youtubev2://abc", Enabled: true},
			{URL: "youtubev2://def", Enabled: true, Properties: map[string]any{"URL_CONTENT": "https://www.youtube.com/watch?v=def"}},
			{Enabled: true, Protected: true},
		},
	}})
	fsys := fstest.MapFS{
		"cfg/downloadList7.zip": {Data: []byte("not a zip")},
		"cfg/downloadList6.zip": {Data: good},
		"cfg/downloadList.zip":  {Data: jdimporttest.DownloadList(t, nil)},
	}
	cfg, err := jdimport.Read(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Packages) != 1 {
		t.Fatalf("got %d packages, want the one from downloadList6.zip", len(cfg.Packages))
	}
	p := cfg.Packages[0]
	if p.Name != "Show S01" || p.Comment != "from the forum" || p.DownloadPassword != "dl" {
		t.Errorf("package = %+v", p)
	}
	if !reflect.DeepEqual(p.Passwords, []string{"pw-a", "pw-b"}) {
		t.Errorf("passwords = %q", p.Passwords)
	}
	links := jdimport.PackageLinks(p)
	want := []string{
		"https://rapidgator.net/file/1",
		"https://cdn.example.org/2.bin",
		"https://cdn.example.org/3.bin",
		"https://www.youtube.com/watch?v=def",
	}
	if !reflect.DeepEqual(links.URLs, want) {
		t.Errorf("urls = %q, want %q", links.URLs, want)
	}
	codes := map[string]string{}
	for _, n := range links.Notes {
		codes[n.Code] = n.Params["n"]
	}
	wantCodes := map[string]string{"linksFinished": "1", "linksOff": "1", "linksProtected": "1", "linksInternal": "1"}
	if !reflect.DeepEqual(codes, wantCodes) {
		t.Errorf("notes = %v, want %v", codes, wantCodes)
	}
	if len(cfg.Problems) != 1 || cfg.Problems[0].Params["file"] != "downloadList7.zip" {
		t.Errorf("problems = %+v, want downloadList7.zip named", cfg.Problems)
	}
}

func TestEveryDebridServiceKnightLoaderHasIsRecognised(t *testing.T) {
	t.Parallel()
	mapped := map[string]bool{}
	for _, host := range []string{
		"torbox.app", "alldebrid.com", "real-debrid.com", "debrid-link.com", "debrid-link.fr",
		"premiumize.me", "linksnappy.com", "offcloud.com", "bestdebrid.com", "cocoleech.com",
		"cooldebrid.com", "debriditalia.com", "deepbrid.com", "fakirdebrid.net", "mega-debrid.eu",
		"multiup.io", "mydebrid.com", "neodebrid.com", "proleech.link", "premium.rpnet.biz", "zevera.com",
	} {
		id, ok := jdimport.DebridService(host)
		if !ok {
			t.Errorf("%s is not recognised", host)
			continue
		}
		if _, ok := accounts.Lookup(id); !ok {
			t.Errorf("%s maps to %q, which the catalogue does not have", host, id)
		}
		mapped[id] = true
	}
	for _, svc := range accounts.Catalogue {
		if svc.Group == accounts.GroupDebrid && !mapped[svc.ID] {
			t.Errorf("no JDownloader host maps to the debrid service %s", svc.ID)
		}
	}
}

func TestDebridCredentialsComeFromWhereEachPluginKeepsThem(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		service string
		acc     jdimport.Account
		want    accounts.Credential
		ok      bool
	}{
		{"TorBox gets its dashes back", "torbox",
			jdimport.Account{Password: "0123456789abcdef0123456789abcdef"},
			accounts.Credential{APIKey: "01234567-89ab-cdef-0123-456789abcdef"}, true},
		{"AllDebrid key typed as the password", "alldebrid",
			jdimport.Account{Password: "AbCdEfGhIjKlMnOpQrSt"},
			accounts.Credential{APIKey: "AbCdEfGhIjKlMnOpQrSt"}, true},
		{"AllDebrid key from the PIN login", "alldebrid",
			jdimport.Account{Properties: map[string]any{"apiv4_apikey": "fromthepinlogin12345"}},
			accounts.Credential{APIKey: "fromthepinlogin12345"}, true},
		{"Real-Debrid keeps only an OAuth token", "realdebrid",
			jdimport.Account{User: "bob", Password: "website", Properties: map[string]any{"TOKEN": "{}"}},
			accounts.Credential{}, false},
		{"Offcloud from before its key login", "offcloud",
			jdimport.Account{User: "bob@example.org", Password: "a website password"},
			accounts.Credential{}, false},
		{"Linksnappy logs in as typed", "linksnappy",
			jdimport.Account{User: "bob", Password: "pw"},
			accounts.Credential{Username: "bob", Password: "pw"}, true},
		{"ProLeech API pair in the fields", "proleech",
			jdimport.Account{User: "apiuser", Password: "abcdefghij0123456789k"},
			accounts.Credential{Username: "apiuser", Password: "abcdefghij0123456789k"}, true},
		{"ProLeech website login with the pair in the properties", "proleech",
			jdimport.Account{User: "bob", Password: "website", Properties: map[string]any{"apiuser": "u1", "apikey": "k1"}},
			accounts.Credential{Username: "u1", Password: "k1"}, true},
	}
	for _, c := range cases {
		got, ok := jdimport.DebridCredential(c.service, c.acc)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func mapOne(t *testing.T, filter bool, rule map[string]any) jdimport.MappedRule {
	t.Helper()
	fsys := fstest.MapFS{}
	files := jdimporttest.Files(t, jdimporttest.Config{
		Packagizer: []map[string]any{rule},
		LinkFilter: []map[string]any{rule},
	})
	for name, b := range files {
		fsys[name] = &fstest.MapFile{Data: b}
	}
	cfg, err := jdimport.Read(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if filter {
		return jdimport.MapLinkFilter(cfg.LinkFilter)[0]
	}
	return jdimport.MapPackagizer(cfg.Packagizer)[0]
}

func TestTextConditionsKeepJDownloadersMatching(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		cond  map[string]any
		want  rules.Condition
		match []string
		miss  []string
	}{
		{"plain contains folds case", jdimporttest.Text("CONTAINS", "Sample", false),
			rules.Condition{Field: rules.FieldFilename, Op: rules.OpContains, Value: "Sample"},
			[]string{"movie.sample.mkv"}, []string{"movie.mkv"}},
		{"plain contains-not stays plain", jdimporttest.Text("CONTAINS_NOT", "sample", false),
			rules.Condition{Field: rules.FieldFilename, Op: rules.OpContainsNot, Value: "sample"},
			[]string{"movie.mkv"}, []string{"a.sample.mkv"}},
		{"wildcards", jdimporttest.Text("EQUALS", "*.part?.rar", false),
			rules.Condition{Field: rules.FieldFilename, Op: rules.OpMatches, Value: `(?is)^(?:.*\.part.\.rar)$`},
			[]string{"Show.PART1.rar"}, []string{"show.part10.rar", "show.part1.rar.txt"}},
		{"a pattern is case-insensitive", jdimporttest.Text("CONTAINS", `s(\d+)e(\d+)`, true),
			rules.Condition{Field: rules.FieldFilename, Op: rules.OpMatches, Value: `(?is)s(\d+)e(\d+)`},
			[]string{"Show.S01E02.mkv"}, []string{"show.mkv"}},
	}
	for _, c := range cases {
		m := mapOne(t, false, jdimporttest.Rule(c.name, map[string]any{
			"filenameFilter": c.cond, "packageName": "x",
		}))
		if m.Blocked != nil {
			t.Errorf("%s: blocked: %s", c.name, m.Blocked.Text)
			continue
		}
		if !reflect.DeepEqual(m.Rule.Conditions, []rules.Condition{c.want}) {
			t.Errorf("%s: conditions = %+v, want %+v", c.name, m.Rule.Conditions, c.want)
			continue
		}
		matcher, problems := rules.Compile(rules.Set{Rules: []rules.Rule{m.Rule}})
		if len(problems) > 0 {
			t.Fatalf("%s: %v", c.name, problems)
		}
		for _, name := range c.match {
			if e := matcher.Apply(rules.Candidate{Filename: name}); len(e.Matched) == 0 {
				t.Errorf("%s: %q should match", c.name, name)
			}
		}
		for _, name := range c.miss {
			if e := matcher.Apply(rules.Candidate{Filename: name}); len(e.Matched) != 0 {
				t.Errorf("%s: %q should not match", c.name, name)
			}
		}
	}
}

func TestRulesKnightLoaderCannotTestAreBlockedNotWidened(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		over map[string]any
		code string
	}{
		{"negated pattern", map[string]any{"filenameFilter": jdimporttest.Text("CONTAINS_NOT", "sample.*", true)}, "ruleNegated"},
		{"origin", map[string]any{"matchAlwaysFilter": map[string]any{"enabled": true},
			"originFilter": map[string]any{"enabled": true, "matchType": "IS", "origins": []string{"CNL"}}}, "ruleOrigin"},
		{"online status", map[string]any{"onlineStatusFilter": map[string]any{"enabled": true, "matchType": "IS", "onlineStatus": "OFFLINE"}}, "ruleStatus"},
		{"size outside", map[string]any{"filesizeFilter": map[string]any{"enabled": true, "from": 1, "to": 100, "matchType": "NOT_BETWEEN"}}, "ruleSizeOutside"},
		{"type is not", map[string]any{"filetypeFilter": map[string]any{"enabled": true, "matchType": "IS_NOT", "videoFilesEnabled": true}}, "ruleTypeNot"},
		{"java pattern", map[string]any{"filenameFilter": jdimporttest.Text("CONTAINS", `(?<=a)b`, true)}, "rulePattern"},
		{"no condition", map[string]any{}, "ruleNoCondition"},
		{"built in", map[string]any{"staticRule": true, "id": "OfflineView"}, "ruleBuiltin"},
		{"folder per package", map[string]any{"staticRule": true, "id": "SubFolderByPackageRule"}, "ruleBuiltinPackageFolder"},
	}
	for _, c := range cases {
		over := map[string]any{"packageName": "x"}
		for k, v := range c.over {
			over[k] = v
		}
		for _, filter := range []bool{false, true} {
			m := mapOne(t, filter, jdimporttest.Rule(c.name, over))
			if m.Blocked == nil || m.Blocked.Code != c.code {
				t.Errorf("%s (filter %v): blocked = %+v, want %s", c.name, filter, m.Blocked, c.code)
			}
		}
	}
}

func TestPackagizerActionsMapAndSayWhatIsLeftOut(t *testing.T) {
	t.Parallel()
	m := mapOne(t, false, jdimporttest.Rule("Series", map[string]any{
		"sourceURLFilter":       jdimporttest.Text("CONTAINS", `forum\.example\.org/(\w+)/`, true),
		"downloadDestination":   "/downloads/<jd:source:1>/<jd:packagename>",
		"packageName":           "<jd:orgpackagename> <jd:simpledate:yyyy-MM-dd/>",
		"priority":              "HIGHER",
		"chunks":                40,
		"autoExtractionEnabled": false,
		"autoStartEnabled":      true,
		"comment":               "<jd:env:HOME>",
	}))
	if m.Blocked != nil {
		t.Fatalf("blocked: %s", m.Blocked.Text)
	}
	a := m.Rule.Action
	if a.DownloadDir != "/downloads/<jd:match:source:1>/<jd:packagename>" {
		t.Errorf("DownloadDir = %q", a.DownloadDir)
	}
	if a.PackageName != "<jd:packagename> <jd:simpledate:yyyy-MM-dd>" {
		t.Errorf("PackageName = %q", a.PackageName)
	}
	if a.Priority == nil || *a.Priority != 2 {
		t.Errorf("Priority = %v, want 2", a.Priority)
	}
	if a.Chunks == nil || *a.Chunks != rules.MaxChunks {
		t.Errorf("Chunks = %v, want the cap %d", a.Chunks, rules.MaxChunks)
	}
	if a.AutoExtract == nil || *a.AutoExtract {
		t.Errorf("AutoExtract = %v, want false", a.AutoExtract)
	}
	if a.Comment != "" {
		t.Errorf("Comment = %q; a placeholder KnightLoader lacks must leave the field out", a.Comment)
	}
	codes := map[string]map[string]string{}
	for _, n := range m.Notes {
		codes[n.Code] = n.Params
	}
	if codes["rulePlaceholder"]["tag"] != "<jd:env:HOME>" {
		t.Errorf("notes = %+v, want the env placeholder named", m.Notes)
	}
	if codes["ruleDropped"]["actions"] != jdimport.DroppedAutoStart {
		t.Errorf("notes = %+v, want autoStart left out", m.Notes)
	}
}

func TestARuleThatOnlyDoesWhatKnightLoaderCannotIsBlocked(t *testing.T) {
	t.Parallel()
	m := mapOne(t, false, jdimporttest.Rule("rev off", map[string]any{
		"filetypeFilter": map[string]any{"enabled": true, "matchType": "IS", "customs": "rev"},
		"linkEnabled":    false,
	}))
	if m.Blocked == nil || m.Blocked.Code != "ruleNoAction" {
		t.Fatalf("blocked = %+v, want ruleNoAction", m.Blocked)
	}
}

func TestFilterExceptionsAndTheListSwitch(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{}
	for name, b := range jdimporttest.Files(t, jdimporttest.Config{
		PackagizerOff: true,
		Packagizer: []map[string]any{jdimporttest.Rule("p", map[string]any{
			"matchAlwaysFilter": map[string]any{"enabled": true}, "packageName": "all",
		})},
		LinkFilter: []map[string]any{
			jdimporttest.Rule("keep", map[string]any{"accept": true, "hosterURLFilter": jdimporttest.Text("CONTAINS", "example.org", false)}),
			jdimporttest.Rule("drop", map[string]any{"filesizeFilter": map[string]any{"enabled": true, "from": 0, "to": 1000, "matchType": "BETWEEN"}}),
		},
	}) {
		fsys[name] = &fstest.MapFile{Data: b}
	}
	cfg, err := jdimport.Read(fsys)
	if err != nil {
		t.Fatal(err)
	}
	pk := jdimport.MapPackagizer(cfg.Packagizer)[0]
	if pk.Enabled || !pk.Rule.Disabled {
		t.Error("the Packagizer was off in JDownloader, so its rules must arrive switched off")
	}
	f := jdimport.MapLinkFilter(cfg.LinkFilter)
	if !f[0].Accept || f[0].Rule.Action.Reject {
		t.Errorf("exception = %+v, want an accept rule", f[0])
	}
	if f[1].Accept || !f[1].Rule.Action.Reject {
		t.Errorf("filter rule = %+v, want a reject", f[1])
	}
	want := rules.Condition{Field: rules.FieldFilesize, Op: rules.OpBetween, Min: 1, Max: 1000}
	if !reflect.DeepEqual(f[1].Rule.Conditions, []rules.Condition{want}) {
		t.Errorf("size condition = %+v, want %+v", f[1].Rule.Conditions, want)
	}
}

func TestASizeRangeFromZeroLeavesLinksOfUnknownSizeAlone(t *testing.T) {
	t.Parallel()
	m := mapOne(t, true, jdimporttest.Rule("samples", map[string]any{
		"filesizeFilter": map[string]any{"enabled": true, "from": 0, "to": 1024, "matchType": "BETWEEN"},
	}))
	if m.Blocked != nil {
		t.Fatalf("blocked: %s", m.Blocked.Text)
	}
	matcher, problems := rules.Compile(rules.Set{Rules: []rules.Rule{m.Rule}})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if e := matcher.Apply(rules.Candidate{URL: "https://host.example/disc.iso"}); len(e.Matched) != 0 {
		t.Error("a link whose size is not known yet was rejected; JDownloader tests the size only once it is known")
	}
	if e := matcher.Apply(rules.Candidate{URL: "https://host.example/sample.mkv", Filesize: 900}); len(e.Matched) == 0 {
		t.Error("a 900 byte file should still match 0 to 1024 bytes")
	}

	exact := mapOne(t, true, jdimporttest.Rule("empty files", map[string]any{
		"filesizeFilter": map[string]any{"enabled": true, "from": 0, "to": 0, "matchType": "BETWEEN"},
	}))
	if exact.Blocked == nil || exact.Blocked.Code != "ruleInvalid" {
		t.Errorf("0 to 0 bytes: blocked = %+v; here it would match every link of unknown size", exact.Blocked)
	}
}

func TestRuleListsWithAByteOrderMarkAreRead(t *testing.T) {
	t.Parallel()
	const bom = "\xef\xbb\xbf"
	fsys := fstest.MapFS{}
	for name, b := range jdimporttest.Files(t, jdimporttest.Config{
		PackagizerOff: true,
		Packagizer: []map[string]any{jdimporttest.Rule("p", map[string]any{
			"matchAlwaysFilter": map[string]any{"enabled": true}, "packageName": "all",
		})},
	}) {
		fsys[name] = &fstest.MapFile{Data: append([]byte(bom), b...)}
	}
	cfg, err := jdimport.Read(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Problems) > 0 {
		t.Errorf("problems = %+v", cfg.Problems)
	}
	if len(cfg.Packagizer) != 1 {
		t.Fatalf("packagizer = %+v, want the one rule", cfg.Packagizer)
	}
	if cfg.Packagizer[0].Enabled {
		t.Error("the Packagizer switch file was off, so the rule must arrive switched off")
	}
}

func TestAZipWithWindowsSeparatorsIsRead(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, b := range jdimporttest.Files(t, jdimporttest.Config{Passwords: []string{"secret"}}) {
		w, err := zw.Create(`JDownloader 2.0\cfg\` + name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jdimport.Read(jdimport.ZipFS(zr))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.ArchivePasswords, []string{"secret"}) {
		t.Errorf("ArchivePasswords = %q", cfg.ArchivePasswords)
	}
}
