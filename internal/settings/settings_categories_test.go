package settings

// The named drawers: that an empty table changes nothing, that a category is a
// reference and behaves like one when renamed or deleted, that a half-filled
// drawer changes only the fields it filled in, and that a rule pointing at a
// drawer that does not exist is caught where it can still be fixed.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/pathvars"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

func ptr[T any](v T) *T { return &v }

// absPath builds an absolute folder for whichever platform the tests run on.
// sanitizeCategories drops a relative folder the way sanitizePaths drops a
// relative DownloadDir, and a literal `C:\media` is relative on Linux while
// `/media` is relative on Windows, so either spelling written by hand would
// pass on one machine and fail on the other.
func absPath(segs ...string) string {
	abs, err := filepath.Abs(filepath.Join(segs...))
	if err != nil {
		panic(err)
	}
	return abs
}

// threeDrawers is the shape the feature is for: series, films, software. Each
// fills in a different subset, so "no opinion" is exercised on every field at
// least once.
func threeDrawers() []Category {
	return []Category{
		{ID: "serien", Name: "Serien", Dir: absPath("media", "serien"), Priority: ptr(2), Extract: ptr(true)},
		{ID: "filme", Name: "Filme", Dir: absPath("media", "filme"), Collision: string(collide.Skip)},
		{ID: "software", Name: "Software", Extract: ptr(false), SpeedLimit: 2 << 20},
	}
}

// An install that never opens the page behaves as it did before the table
// existed: every resolver answers with the instance's own setting, and the one
// that answers "where does this go" answers nothing rather than a folder.
func TestNoCategoriesIsExactlyTheOldBehaviour(t *testing.T) {
	s := Defaults()
	s.Extract, s.SpeedLimit, s.CollisionPolicy = true, 4242, string(collide.Overwrite)

	if got := s.CategoryFor("serien"); got.ID != "" || got.Dir != "" {
		t.Errorf("CategoryFor on an empty table = %+v, want the zero category", got)
	}
	if got := s.CategoryDir("serien", pathvars.Vars{}); got != "" {
		t.Errorf("CategoryDir = %q, want nothing at all so dirFor keeps its own answer", got)
	}
	if got := s.CollisionFor("serien"); got != string(collide.Overwrite) {
		t.Errorf("CollisionFor = %q, want the instance's own %q", got, collide.Overwrite)
	}
	if got := s.ExtractFor("serien"); got != s.Extract {
		t.Errorf("ExtractFor = %v, want the instance's own %v", got, s.Extract)
	}
	if got := s.SpeedLimitFor("serien"); got != s.SpeedLimit {
		t.Errorf("SpeedLimitFor = %d, want the instance's own %d", got, s.SpeedLimit)
	}
	if _, ok := s.PriorityFor("serien"); ok {
		t.Error("an empty table has an opinion about priority")
	}
}

// The "every zero means no opinion" convention, on the category most likely to
// be created and left as it is: a name, a folder, nothing else. Without it,
// filing a batch under a drawer would reset its priority to the middle and its
// unpacking to whatever the zero value happened to be.
func TestAHalfFilledDrawerChangesOnlyWhatItFilledIn(t *testing.T) {
	s := Defaults()
	s.Extract, s.SpeedLimit, s.CollisionPolicy = true, 4242, string(collide.Overwrite)
	s.Categories = []Category{{ID: "serien", Name: "Serien", Dir: absPath("media", "serien")}}

	if got := s.CategoryDir("serien", pathvars.Vars{}); got != absPath("media", "serien") {
		t.Errorf("CategoryDir = %q, want the folder the drawer names", got)
	}
	if _, ok := s.PriorityFor("serien"); ok {
		t.Error("a drawer that says nothing about priority handed one out anyway")
	}
	if got := s.ExtractFor("serien"); got != true {
		t.Error("a drawer that says nothing about unpacking overruled the global switch")
	}
	if got := s.CollisionFor("serien"); got != string(collide.Overwrite) {
		t.Errorf("CollisionFor = %q, want the instance's own %q", got, collide.Overwrite)
	}
	if got := s.SpeedLimitFor("serien"); got != 4242 {
		t.Errorf("SpeedLimitFor = %d, want the instance's own 4242", got)
	}
}

// Why Category.Extract is a pointer: with a plain bool, "this drawer does not
// unpack" and "this drawer has no opinion" are the same value, so the drawer
// created to stop unpacking would be the one that unpacks.
func TestExtractOffInADrawerSurvivesAGlobalThatIsOn(t *testing.T) {
	s := Defaults()
	s.Extract = true
	s.Categories = []Category{{ID: "software", Extract: ptr(false)}}
	if s.ExtractFor("software") {
		t.Error("a drawer that switches unpacking off was overruled by the global switch")
	}
	if !s.ExtractFor("") {
		t.Error("an untagged task stopped following the global switch")
	}
}

// The same argument for the other pointer: zero is the middle priority, so a
// category asking for it has to be distinguishable from one saying nothing.
func TestPriorityZeroIsAnAnswerAndNotSilence(t *testing.T) {
	s := Settings{Categories: []Category{
		{ID: "mitte", Priority: ptr(0)},
		{ID: "still"},
	}}
	if p, ok := s.PriorityFor("mitte"); !ok || p != 0 {
		t.Errorf("PriorityFor(mitte) = %d, %v; want 0, true", p, ok)
	}
	if _, ok := s.PriorityFor("still"); ok {
		t.Error("a drawer with no priority handed one out")
	}
}

// A task keeps the id of a drawer that has been thrown away, and every resolver
// then answers what it answers for a task that was never filed anywhere, so
// nothing about the download changes and nothing needs migrating.
func TestADeletedCategoryLeavesItsDownloadsAlone(t *testing.T) {
	s := Defaults()
	s.Extract, s.SpeedLimit, s.CollisionPolicy = false, 999, string(collide.Skip)
	s.Categories = threeDrawers()

	before := s.CategoryDir("serien", pathvars.Vars{})
	if before == "" {
		t.Fatal("the fixture drawer has no folder, so the deletion below proves nothing")
	}
	// The drawer is deleted. The task still says "serien", since nothing
	// rewrites a task.
	s.Categories = s.Categories[1:]

	if got := s.CategoryDir("serien", pathvars.Vars{}); got != "" {
		t.Errorf("CategoryDir after the delete = %q, want nothing so the global folder applies", got)
	}
	if got := s.CollisionFor("serien"); got != string(collide.Skip) {
		t.Errorf("CollisionFor after the delete = %q, want the instance's own %q", got, collide.Skip)
	}
	if got := s.ExtractFor("serien"); got != false {
		t.Error("a task filed in a deleted drawer stopped following the global unpacking switch")
	}
	if got := s.SpeedLimitFor("serien"); got != 999 {
		t.Errorf("SpeedLimitFor after the delete = %d, want the instance's own 999", got)
	}
	if _, ok := s.PriorityFor("serien"); ok {
		t.Error("a deleted drawer is still handing out a priority")
	}
}

// The id is the key and the name is not, so a rename reaches every task without
// touching one. With Name as the key the downloads would stop being in the
// drawer, silently.
func TestRenamingADrawerKeepsItsDownloads(t *testing.T) {
	s := Settings{Categories: []Category{
		{ID: "serien", Name: "Serien", Dir: absPath("media", "serien")},
	}}
	s.Categories[0].Name = "TV-Serien und Doku"
	if got := s.CategoryDir("serien", pathvars.Vars{}); got != absPath("media", "serien") {
		t.Errorf("after the rename CategoryDir = %q, want the drawer to still answer", got)
	}
}

// What a reference rather than a copy buys: the task holds the id, so editing
// the folder in one place is the whole edit. Copied onto the tasks at staging
// time, this would be a hundred rows to fix by hand.
func TestARetargetedDrawerMovesEverythingNotYetStarted(t *testing.T) {
	s := Settings{Categories: []Category{{ID: "serien", Dir: absPath("media", "serien")}}}
	s.Categories[0].Dir = absPath("archive", "serien")
	if got := s.CategoryDir("serien", pathvars.Vars{}); got != absPath("archive", "serien") {
		t.Errorf("CategoryDir = %q, want the drawer's new folder", got)
	}
}

// What counts as one drawer. An id is written by clients, by imported rule sets
// and by a hand-edited settings file, and a lookup matching only the stored
// spelling would drop a task out of its own drawer over a capital letter.
func TestCategoryIDFoldsTheSpellings(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Serien", "serien"},
		{"  SERIEN  ", "serien"},
		{"Serien & Filme", "serien-filme"},
		{"serien---filme", "serien-filme"},
		{"-serien-", "serien"},
		{"Hörspiele", "hörspiele"},
		{"###", ""},
		{"", ""},
	} {
		if got := CategoryID(c.in); got != c.want {
			t.Errorf("CategoryID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The half of the rule above that is easy to build only on the save path: the
// stored id and the id being looked up are normalised together, so a task
// carrying "Serien" finds the drawer stored as "serien".
func TestALookupFoldsBothSides(t *testing.T) {
	s := Settings{Categories: []Category{{ID: "serien", Dir: absPath("x")}}}
	for _, id := range []string{"Serien", " serien ", "SERIEN"} {
		if got := s.CategoryFor(id).Dir; got != absPath("x") {
			t.Errorf("CategoryFor(%q) found nothing; a capital letter dropped a task out of its own drawer", id)
		}
	}
}

// The case a fixed folder cannot cover: "/media/serien/<jd:packagename>"
// resolves against the task it is being resolved for.
func TestADrawerFolderMayBeATemplate(t *testing.T) {
	s := Settings{Categories: []Category{{ID: "serien", Dir: filepath.Join(absPath("media", "serien"), "<jd:packagename>")}}}
	got := s.CategoryDir("serien", pathvars.Vars{Package: "Doctor Who S01"})
	if want := filepath.Join(absPath("media", "serien"), "Doctor Who S01"); got != want {
		t.Errorf("CategoryDir = %q, want %q", got, want)
	}
}

// What dirFor does to DownloadDir: a relative folder resolves against whatever
// the process's working directory happens to be, so answering with one would
// scatter a drawer's downloads somewhere nobody can name.
func TestATemplateThatExpandsToARelativeFolderIsDropped(t *testing.T) {
	s := Settings{Categories: []Category{{ID: "serien", Dir: filepath.Join("<jd:packagename>", "x")}}}
	if got := s.CategoryDir("serien", pathvars.Vars{Package: "Doctor Who"}); got != "" {
		t.Errorf("CategoryDir = %q, want nothing rather than a folder nobody can locate", got)
	}
}

// What makes {"name":"Serien"} a complete category through the API. The derived
// id is then stored, so a later rename does not re-derive it and orphan every
// download already filed there.
func TestSanitizeDerivesAnIDFromTheNameOnce(t *testing.T) {
	n := sanitizeCategories(Settings{Categories: []Category{{Name: "Serien & Doku"}}})
	if len(n.Categories) != 1 {
		t.Fatalf("sanitize kept %d categories, want the one it could name", len(n.Categories))
	}
	if got := n.Categories[0].ID; got != "serien-doku" {
		t.Errorf("derived id = %q, want %q", got, "serien-doku")
	}
	n.Categories[0].Name = "Fernsehen"
	n = sanitizeCategories(n)
	if got := n.Categories[0].ID; got != "serien-doku" {
		t.Errorf("id after the rename = %q, want the stored %q so the downloads stay put", got, "serien-doku")
	}
}

// The reasoning sanitizeHostRules applies to a blank host pattern: a row that
// can never be picked and never be referred to is a row on the page that can
// never be explained.
func TestSanitizeDropsADrawerNothingCouldEverPointAt(t *testing.T) {
	n := sanitizeCategories(Settings{Categories: []Category{
		{Name: "###"},
		{ID: "filme", Name: "Filme"},
	}})
	if len(n.Categories) != 1 || n.Categories[0].ID != "filme" {
		t.Errorf("sanitize kept %+v, want only the drawer that can be pointed at", n.Categories)
	}
}

// Which one survives a hand-edited file. ValidateCategories refuses the pair
// before this, and the first is the one a picker built from the slice shows
// first.
func TestSanitizeKeepsTheFirstOfTwoDrawersSharingAnID(t *testing.T) {
	n := sanitizeCategories(Settings{Categories: []Category{
		{ID: "serien", Dir: absPath("first")},
		{ID: "SERIEN", Dir: absPath("second")},
	}})
	if len(n.Categories) != 1 {
		t.Fatalf("sanitize kept %d drawers sharing one key", len(n.Categories))
	}
	if got := n.Categories[0].Dir; got != absPath("first") {
		t.Errorf("the surviving drawer points at %q, want the first one's %q", got, absPath("first"))
	}
}

// collide.Ask parks a task until a human answers, with no status for that and
// no way to answer, so a task set to it sits in the queue forever. The API
// withholds Ask from the global menu, and a category is the second door into
// the same field.
func TestSanitizeRefusesTheOneCollisionRuleNothingCanAnswer(t *testing.T) {
	n := sanitizeCategories(Settings{Categories: []Category{{ID: "filme", Collision: string(collide.Ask)}}})
	if got := n.Categories[0].Collision; got != "" {
		t.Errorf("collision = %q, want it dropped so the instance's own rule applies", got)
	}
	s := Settings{CollisionPolicy: string(collide.Rename), Categories: n.Categories}
	if got := s.CollisionFor("filme"); got != string(collide.Rename) {
		t.Errorf("CollisionFor = %q, want the instance's own %q", got, collide.Rename)
	}
}

// What normalizeCategoryCollision exists to avoid: collide.ParsePolicy maps
// everything it does not recognise onto rename, so running an empty string
// through it would give every drawer an explicit rename and overrule an
// instance configured to skip.
func TestAnUnsetCollisionIsNotRename(t *testing.T) {
	s := Settings{CollisionPolicy: string(collide.Skip), Categories: []Category{{ID: "filme"}}}
	if got := s.CollisionFor("filme"); got != string(collide.Skip) {
		t.Errorf("CollisionFor = %q, want the instance's own %q rather than the package default", got, collide.Skip)
	}
}

// A category is held to the range a Packagizer rule is held to. Outside it, a
// drawer could hand a task a priority no control in the interface can take
// back.
func TestSanitizeClampsAPriorityToWhatTheInterfaceCanUndo(t *testing.T) {
	n := sanitizeCategories(Settings{Categories: []Category{
		{ID: "hoch", Priority: ptr(99)},
		{ID: "tief", Priority: ptr(-99)},
	}})
	if got := *n.Categories[0].Priority; got != rules.PriorityMax {
		t.Errorf("priority = %d, want the ceiling %d", got, rules.PriorityMax)
	}
	if got := *n.Categories[1].Priority; got != rules.PriorityMin {
		t.Errorf("priority = %d, want the floor %d", got, rules.PriorityMin)
	}
}

// Why clampCategoryPriority allocates: the caller still holds the struct it
// handed in, and a sanitiser that edited their copy is a settings document that
// changes underneath whoever submitted it.
func TestSanitizeDoesNotWriteThroughTheCallersPointer(t *testing.T) {
	mine := ptr(99)
	sanitizeCategories(Settings{Categories: []Category{{ID: "hoch", Priority: mine}}})
	if *mine != 99 {
		t.Errorf("the caller's own priority is now %d; sanitize wrote through their pointer", *mine)
	}
}

// Refusal rather than sanitising, for the reason validateRows exists: dropped
// quietly, the person is left with two drawers on screen, one save and one
// drawer.
func TestValidateRefusesTwoDrawersSharingAKey(t *testing.T) {
	s := Settings{Categories: []Category{{ID: "serien"}, {Name: "Serien"}}}
	err := s.ValidateCategories()
	if err == nil {
		t.Fatal("two drawers with one key were accepted")
	}
	if !strings.Contains(err.Error(), "serien") {
		t.Errorf("the message %q does not name the key that is repeated", err)
	}
}

// The asymmetry at the centre of the reference model: a rule naming a category
// that does not exist does nothing, silently, on every link it matches, while a
// task naming one is history and stays as it is.
func TestValidateRefusesARuleFilingLinksInADrawerThatIsGone(t *testing.T) {
	s := Settings{
		Categories: []Category{{ID: "filme", Name: "Filme"}},
		Packagizer: rules.Set{Rules: []rules.Rule{
			{Name: "Serien nach Serien", Action: rules.Action{Category: "serien"}},
		}},
	}
	err := s.ValidateCategories()
	if err == nil {
		t.Fatal("a rule filing links in a drawer that does not exist was accepted")
	}
	for _, want := range []string{"Serien nach Serien", "serien"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message %q does not name %q", err, want)
		}
	}
	// The same rule against the drawer that does exist has to pass, or the check
	// refuses the feature rather than the mistake.
	s.Packagizer.Rules[0].Action.Category = "Filme"
	if err := s.ValidateCategories(); err != nil {
		t.Errorf("a rule naming an existing drawer was refused: %v", err)
	}
}

// The filter flavour ignores the packagizer's action fields, so a category on a
// reject rule is inert rather than broken, and refusing the save over it would
// refuse an imported JDownloader set for a field it never uses.
func TestTheLinkFilterMayNotNameADrawer(t *testing.T) {
	s := Settings{
		LinkFilter: rules.Set{Rules: []rules.Rule{
			{Name: "kein sample", Action: rules.Action{Reject: true, Category: "gibtsnicht"}},
		}},
	}
	if err := s.ValidateCategories(); err != nil {
		t.Errorf("a filter rule carrying a category was refused: %v", err)
	}
}

// The file on disk is the copy the form is reopened against tomorrow. The
// pointer fields are what this is about: an Extract that came back nil would
// switch a drawer's "do not unpack" into "no opinion" on the next boot.
func TestTheTableSurvivesTheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.Categories = threeDrawers()
	if _, err := st.Set(n); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	back := reloaded.Get()
	if len(back.Categories) != 3 {
		t.Fatalf("%d drawers came back, want 3", len(back.Categories))
	}
	if got := back.Categories[0].ID; got != "serien" {
		t.Errorf("the first drawer came back as %q; the order a menu is offered in was not kept", got)
	}
	soft := back.CategoryFor("software")
	if soft.Extract == nil || *soft.Extract {
		t.Errorf("software.extract came back %v, want a deliberate false", soft.Extract)
	}
	if soft.SpeedLimit != 2<<20 {
		t.Errorf("software.speedLimit came back %d, want %d", soft.SpeedLimit, 2<<20)
	}
	if got, ok := back.PriorityFor("serien"); !ok || got != 2 {
		t.Errorf("serien.priority came back %d, %v; want 2, true", got, ok)
	}
}

// Why Categories carries no omitempty: a nil slice with omitempty is dropped
// from the document, and the frontend has no way to type a field that is
// sometimes absent.
func TestTheKeyIsAlwaysInTheJSON(t *testing.T) {
	b, err := json.Marshal(Defaults())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	raw, ok := doc["categories"]
	if !ok {
		t.Fatal(`a fresh install's JSON has no "categories" key at all`)
	}
	if string(raw) != "null" {
		t.Errorf("categories = %s, want null on a fresh install", raw)
	}
}

// Categories are creatable and editable through the settings API, and a patch
// naming one key disturbs nothing else. ApplyPatch is the function PATCH
// /api/settings builds its merge from.
func TestADrawerCanBeCreatedByPatchingOneKey(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := Defaults()
	base.SpeedLimit = 1234
	if _, err := st.Set(base); err != nil {
		t.Fatal(err)
	}
	patch := map[string]json.RawMessage{
		"categories": json.RawMessage(`[{"name":"Serien","priority":2}]`),
	}
	applied, err := st.SetPartial(patch)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Categories) != 1 {
		t.Fatalf("the patch created %d drawers, want 1", len(applied.Categories))
	}
	if got := applied.Categories[0].ID; got != "serien" {
		t.Errorf("the created drawer's id = %q, want it derived from the name", got)
	}
	if applied.SpeedLimit != 1234 {
		t.Errorf("the unrelated speed limit is now %d; a one-key patch disturbed another field", applied.SpeedLimit)
	}

	// Editable the same way: the drawer gains a folder without losing the
	// priority already on it. The folder is marshalled rather than written into
	// the literal, because a Windows path in a JSON string needs its separators
	// doubling and a hand-escaped one passes for the wrong reason on the other
	// platform.
	want := absPath("media", "serien")
	edited, err := json.Marshal([]Category{{ID: "serien", Name: "Serien", Priority: ptr(2), Dir: want}})
	if err != nil {
		t.Fatal(err)
	}
	patch = map[string]json.RawMessage{"categories": edited}
	applied, err = st.SetPartial(patch)
	if err != nil {
		t.Fatal(err)
	}
	if got := applied.CategoryDir("serien", pathvars.Vars{}); got != want {
		t.Errorf("after the edit CategoryDir = %q, want the folder %q that was patched in", got, want)
	}
	if p, ok := applied.PriorityFor("serien"); !ok || p != 2 {
		t.Errorf("priority after the edit = %d, %v; want 2, true", p, ok)
	}
}
