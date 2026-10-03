package app

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/jdimport/jdimporttest"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// jdFixture is a JDownloader install with one of everything the import reads.
func jdFixture() jdimporttest.Config {
	return jdimporttest.Config{
		Accounts: []jdimporttest.Account{
			{Host: "rapidgator.net", User: "alice", Password: "rg-secret", Enabled: true},
			{Host: "rapidgator.net", User: "alice2", Password: "rg-second", Enabled: true},
			{Host: "torbox.app", User: "alice@example.org", Password: "0123456789abcdef0123456789abcdef", Enabled: true},
			{Host: "real-debrid.com", User: "alice", Password: "rd-website", Enabled: true},
			{Host: "simply-debrid.com", User: "alice", Password: "sd", Enabled: true},
			{Host: "nitroflare.com", User: "alice", Password: "nf-secret", Enabled: false},
		},
		Passwords:   []string{"known", "fresh"},
		DownloadDir: "/jd/output",
		Packagizer: []map[string]any{
			jdimporttest.Rule("mkv to films", map[string]any{
				"filetypeFilter": map[string]any{"enabled": true, "matchType": "IS", "customs": "mkv"},
				"packageName":    "Films",
			}),
			jdimporttest.Rule("offline view", map[string]any{
				"onlineStatusFilter": map[string]any{"enabled": true, "matchType": "IS", "onlineStatus": "OFFLINE"},
				"packageName":        "x",
			}),
		},
		LinkFilter: []map[string]any{
			jdimporttest.Rule("no samples", map[string]any{"filenameFilter": jdimporttest.Text("CONTAINS", "sample", false)}),
			jdimporttest.Rule("but keep mine", map[string]any{"accept": true, "filenameFilter": jdimporttest.Text("CONTAINS", "mine", false)}),
		},
		Packages: []jdimporttest.Package{
			{Name: "Show S01", Comment: "from the forum", Links: []jdimporttest.Link{
				{URL: "https://host.example/ep1.mkv", Name: "ep1.mkv", Enabled: true, Properties: map[string]any{"PWLIST": "pkg-pw"}},
				{URL: "https://host.example/ep2.mkv", Name: "ep2.mkv", Enabled: true, State: "FINISHED"},
			}},
			{Name: "Done", Links: []jdimporttest.Link{
				{URL: "https://host.example/old.bin", Enabled: true, State: "FINISHED"},
			}},
		},
		ListNumber: 3,
	}
}

func itemByName(t *testing.T, p JDImportPreview, group, name string) JDImportItem {
	t.Helper()
	for _, it := range p.Items {
		if it.Group == group && it.Name == name {
			return it
		}
	}
	t.Fatalf("no %s item named %q in %+v", group, name, p.Items)
	return JDImportItem{}
}

func TestJDImportPreviewSaysWhatComesOverAndWhatStays(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {
		s.ArchivePasswords = []string{"known"}
	})
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdFixture())

	p, err := a.ReadJDImport(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	for _, secret := range []string{"rg-secret", "rg-second", "0123456789abcdef", "rd-website", "nf-secret"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the preview carries the password %q", secret)
		}
	}

	if it := itemByName(t, p, JDGroupAccounts, "TorBox"); it.Blocked != nil || !it.Ticked || it.Slot != "" {
		t.Errorf("TorBox = %+v, want the default account, ticked", it)
	}
	if it := itemByName(t, p, JDGroupAccounts, "Real-Debrid"); it.Blocked == nil || it.Blocked.Code != "noApiKey" {
		t.Errorf("Real-Debrid = %+v, want noApiKey", it)
	}
	if it := itemByName(t, p, JDGroupAccounts, "simply-debrid.com"); it.Blocked == nil || it.Blocked.Code != "noClient" {
		t.Errorf("simply-debrid = %+v, want noClient", it)
	}
	if it := itemByName(t, p, JDGroupAccounts, "nitroflare.com"); !it.Off || it.Ticked {
		t.Errorf("nitroflare = %+v, want off and not ticked", it)
	}
	var rapidgator []JDImportItem
	for _, it := range p.Items {
		if it.Name == "rapidgator.net" {
			rapidgator = append(rapidgator, it)
		}
	}
	if len(rapidgator) != 2 || rapidgator[0].Blocked != nil || rapidgator[1].Blocked == nil || rapidgator[1].Blocked.Code != "secondLogin" {
		t.Errorf("rapidgator = %+v, want the first account and the second refused", rapidgator)
	}

	pw := itemByName(t, p, JDGroupSettings, "")
	if pw.Kind != "passwords" || pw.Count != 1 || pw.Total != 2 {
		t.Errorf("passwords = %+v, want 1 new of 2", pw)
	}
	if it := itemByName(t, p, JDGroupSettings, "/jd/output"); it.Ticked || !it.Replaces {
		t.Errorf("folder = %+v, want offered unticked as a replacement", it)
	}
	if it := itemByName(t, p, JDGroupPackagizer, "offline view"); it.Blocked == nil {
		t.Errorf("offline view = %+v, want blocked", it)
	}
	if it := itemByName(t, p, JDGroupDownloads, "Done"); it.Blocked == nil || it.Blocked.Code != "packageEmpty" {
		t.Errorf("Done = %+v, want packageEmpty", it)
	}
}

func TestJDImportAppliesOnlyTheTickedItems(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, base string) {
		s.ArchivePasswords = []string{"known"}
		s.LinkFilter = rules.Set{Rules: []rules.Rule{{
			Name:       "existing",
			Conditions: []rules.Condition{{Field: rules.FieldFilename, Op: rules.OpContains, Value: "trailer"}},
			Action:     rules.Action{Reject: true},
		}}}
	})
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdFixture())
	p, err := a.ReadJDImport(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range p.Items {
		if it.Ticked {
			ids = append(ids, it.ID)
		}
	}
	rep, err := a.ApplyJDImport(p.Token, ids, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Failed) > 0 {
		t.Fatalf("failed: %+v", rep.Failed)
	}
	if len(rep.Imported) != len(ids) {
		t.Errorf("imported %d of %d ticked items", len(rep.Imported), len(ids))
	}

	if got, _ := hosterauth.NewStore(a.Accounts).Get("rapidgator.net"); got != (accounts.Credential{Username: "alice", Password: "rg-secret"}) {
		t.Errorf("rapidgator login = %+v", got)
	}
	if got, _ := hosterauth.NewStore(a.Accounts).Get("nitroflare.com"); !got.IsZero() {
		t.Error("the unticked nitroflare login was stored")
	}
	if got, _ := a.Accounts.Get("torbox"); got != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Errorf("TorBox key = %q", got)
	}

	s := a.Settings.Get()
	if strings.Join(s.ArchivePasswords, ",") != "known,fresh,pkg-pw" {
		t.Errorf("archive passwords = %q", s.ArchivePasswords)
	}
	if s.DownloadDir != base {
		t.Errorf("download folder = %q; it was not ticked", s.DownloadDir)
	}
	if n := len(s.Packagizer.Rules); n != 1 || s.Packagizer.Rules[0].Name != "mkv to films" {
		t.Errorf("packagizer = %+v", s.Packagizer.Rules)
	}
	var names []string
	for _, r := range s.LinkFilter.Rules {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "but keep mine,existing,no samples" {
		t.Errorf("filter order = %q, want the exception first and the new filter last", names)
	}
	if !s.LinkFilter.StopAfterMatch || !rep.FilterStops {
		t.Error("an imported exception needs the filter to stop at its first match")
	}

	var staged []*core.Task
	for _, task := range a.Tasks() {
		if task.Origin == OriginJDownloader {
			staged = append(staged, task)
		}
	}
	if len(staged) != 1 || rep.Links != 1 {
		t.Fatalf("staged %d links (report %d), want the one unfinished link", len(staged), rep.Links)
	}
	task := staged[0]
	if task.Status != core.StatusCollected {
		t.Errorf("status = %s; an imported link must wait in the collector", task.Status)
	}
	if task.Package != "Films" {
		t.Errorf("package = %q; the imported Packagizer rule should have named it", task.Package)
	}
	if task.Password != "pkg-pw" || task.Comment != "from the forum" {
		t.Errorf("password %q, comment %q", task.Password, task.Comment)
	}
}

func TestAJDImportPreviewCanBeAppliedOnce(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {})
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdimporttest.Config{Passwords: []string{"pw"}})
	p, err := a.ReadJDImport(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyJDImport(p.Token, []string{"passwords"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyJDImport(p.Token, []string{"passwords"}, nil); !errors.Is(err, ErrJDImportExpired) {
		t.Fatalf("second apply: err = %v, want ErrJDImportExpired", err)
	}
}

func TestADebridKeyAlreadyHereIsNotOverwritten(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {})
	if err := a.Accounts.Set("torbox", "11111111-2222-3333-4444-555555555555"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdimporttest.Config{Accounts: []jdimporttest.Account{
		{Host: "torbox.app", User: "alice@example.org", Password: "0123456789abcdef0123456789abcdef", Enabled: true},
	}})
	p, err := a.ReadJDImport(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	it := itemByName(t, p, JDGroupAccounts, "TorBox")
	if it.Slot != "alice@example.org" {
		t.Fatalf("TorBox goes to %q, want a named account beside the stored one", it.Detail)
	}
	if _, err := a.ApplyJDImport(p.Token, []string{it.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.Accounts.Get("torbox"); got != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("the stored key became %q", got)
	}
	if got, _ := a.Accounts.GetCredential("torbox", "alice@example.org"); got.APIKey != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Errorf("named account = %+v", got)
	}
}

func TestAnEnabledAccountWinsTheHosterOverADisabledOneAboveIt(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {})
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdimporttest.Config{Accounts: []jdimporttest.Account{
		{Host: "katfile.com", User: "old", Password: "old-pw", Enabled: false},
		{Host: "katfile.com", User: "new", Password: "new-pw", Enabled: true},
	}})
	p, err := a.ReadJDImport(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 {
		t.Fatalf("items = %+v", p.Items)
	}
	old, cur := p.Items[0], p.Items[1]
	if cur.Blocked != nil || !cur.Ticked {
		t.Errorf("enabled account = %+v, want it ticked", cur)
	}
	if old.Blocked == nil || old.Blocked.Code != "secondLogin" {
		t.Errorf("disabled account = %+v, want secondLogin", old)
	}
	if _, err := a.ApplyJDImport(p.Token, []string{cur.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := hosterauth.NewStore(a.Accounts).Get("katfile.com"); got != (accounts.Credential{Username: "new", Password: "new-pw"}) {
		t.Errorf("katfile login = %+v, want the enabled one", got)
	}
}

func TestEveryArchivePasswordOfAPackageComesOver(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {})
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdimporttest.Config{Packages: []jdimporttest.Package{
		{Name: "Pack", Links: []jdimporttest.Link{
			{URL: "https://host.example/a.rar", Name: "a.rar", Enabled: true, Properties: map[string]any{"PWLIST": []string{"first", "second"}}},
		}},
	}})
	p, err := a.ReadJDImport(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	it := itemByName(t, p, JDGroupDownloads, "Pack")
	if _, err := a.ApplyJDImport(p.Token, []string{it.ID}, nil); err != nil {
		t.Fatal(err)
	}
	var task *core.Task
	for _, x := range a.Tasks() {
		if x.Origin == OriginJDownloader {
			task = x
		}
	}
	if task == nil || task.Password != "first" {
		t.Fatalf("task = %+v, want the first password on it", task)
	}
	if got := strings.Join(a.Settings.Get().ArchivePasswords, ","); got != "first,second" {
		t.Errorf("archive passwords = %q, want both of the package's", got)
	}
}

func TestAFolderThatDoesNotFitHereFailsOnlyItself(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, base string) {})
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdimporttest.Config{
		Passwords:   []string{"pw"},
		DownloadDir: "jdimp/Downloads",
		Packagizer: []map[string]any{jdimporttest.Rule("mkv to films", map[string]any{
			"filetypeFilter": map[string]any{"enabled": true, "matchType": "IS", "customs": "mkv"},
			"packageName":    "Films",
		})},
	})
	p, err := a.ReadJDImport(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range p.Items {
		ids = append(ids, it.ID)
	}
	check := func(preview settings.Settings, patch map[string]json.RawMessage) error {
		return settings.CheckFolders(preview, func(key string) bool { _, ok := patch[key]; return ok })
	}
	rep, err := a.ApplyJDImport(p.Token, ids, check)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Failed) != 1 || rep.Failed[0].ID != "folder" {
		t.Errorf("failed = %+v, want only the folder", rep.Failed)
	}
	if strings.Join(rep.Imported, ",") != "passwords,packagizer:0" {
		t.Errorf("imported = %q, want the passwords and the rule", rep.Imported)
	}
	s := a.Settings.Get()
	if s.DownloadDir != base || len(s.ArchivePasswords) != 1 || len(s.Packagizer.Rules) != 1 {
		t.Errorf("settings: folder %q, passwords %q, rules %d", s.DownloadDir, s.ArchivePasswords, len(s.Packagizer.Rules))
	}
}

func TestAPreviewPushedOutByNewerReadsSaysSoOnEveryApply(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {})
	dir := t.TempDir()
	jdimporttest.Write(t, dir, jdimporttest.Config{Passwords: []string{"pw"}})
	first, err := a.ReadJDImport(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	for range jdPendingMax {
		if _, err := a.ReadJDImport(os.DirFS(dir)); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if _, err := a.ApplyJDImport(first.Token, []string{"passwords"}, nil); !errors.Is(err, ErrJDImportReplaced) {
			t.Fatalf("err = %v, want ErrJDImportReplaced", err)
		}
	}
}

func TestRulesSwitchedOffInJDownloaderComeUnticked(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {})
	dir := t.TempDir()
	off := jdimporttest.Rule("mkv to films", map[string]any{
		"filetypeFilter": map[string]any{"enabled": true, "matchType": "IS", "customs": "mkv"},
		"packageName":    "Films",
	})
	off["enabled"] = false
	jdimporttest.Write(t, dir, jdimporttest.Config{Packagizer: []map[string]any{off}})
	p, err := a.ReadJDImport(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	it := itemByName(t, p, JDGroupPackagizer, "mkv to films")
	if !it.Off || it.Ticked || it.Blocked != nil {
		t.Errorf("rule = %+v, want offered off and unticked", it)
	}
}
