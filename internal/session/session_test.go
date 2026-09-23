package session

import (
	"os"
	"strings"
	"testing"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/storage"
)

func TestCreationCultivationExitAndRestoreShortLoop(t *testing.T) {
	layout := storage.LayoutFor(t.TempDir())
	app, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	first := app.Model()
	if first.Creation == nil || first.Creation.Author != "作者：雾见川" || len(first.Options) < 3 {
		t.Fatalf("first screen does not present the quick-start creation flow: %#v", first)
	}
	if _, err := app.HandleKey("1"); err != nil {
		t.Fatal(err)
	}
	if app.State().Pending.Creation.Step != engine.CreationStepDetails {
		t.Fatal("choosing a preset must advance to the confirmation step")
	}
	for _, field := range app.Model().Creation.Fields {
		switch field.Value {
		case "human", "true", "ordinary", "unspecified":
			t.Errorf("creation screen leaked an internal id %q for %s", field.Value, field.Label)
		}
	}
	if _, err := app.HandleKey("c"); err != nil {
		t.Fatal(err)
	}
	created := app.State()
	if created.Phase != engine.PhaseReady || created.Player == nil || created.Counters.WorldMonth != 0 {
		t.Fatalf("creation produced the wrong state: %#v", created)
	}
	if _, err := app.HandleKey("s"); err != nil {
		t.Fatal(err)
	}
	revision, month := app.State().Revision, app.State().Counters.WorldMonth
	if _, err := app.HandleKey("c"); err != nil {
		t.Fatal(err)
	}
	if app.State().Revision != revision || app.State().Counters.WorldMonth != month {
		t.Fatal("a presentation setting must not touch game state or advance time")
	}
	if app.Settings().ColorMode != "basic" {
		t.Fatalf("color preference did not change: %#v", app.Settings())
	}
	if _, err := app.HandleKey("b"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.HandleKey("1"); err != nil {
		t.Fatal(err)
	}
	progress := app.State()
	if progress.Player.XP != 195000 || progress.Counters.WorldMonth != 1 || progress.Player.AgeMonths != 253 {
		t.Fatalf("ordinary cultivation did not settle exactly one saved month: xp=%d month=%d age=%d",
			progress.Player.XP, progress.Counters.WorldMonth, progress.Player.AgeMonths)
	}
	if exit, err := app.HandleKey("q"); err != nil || !exit {
		t.Fatalf("q should request a normal exit: exit=%t err=%v", exit, err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	restored, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	state := restored.State()
	if state.Player.XP != 195000 || state.Counters.WorldMonth != 1 || state.Player.AgeMonths != 253 {
		t.Fatalf("restart re-ran or lost the last action: xp=%d month=%d age=%d",
			state.Player.XP, state.Counters.WorldMonth, state.Player.AgeMonths)
	}
	model := restored.Model()
	if model.Notice == nil || !strings.Contains(model.Notice.Text, "没有重新执行") {
		t.Fatalf("restore screen must explain that settlement was not replayed: %#v", model.Notice)
	}
	found := false
	for _, change := range model.Changes {
		if strings.Contains(change.Text, "修炼修为 +19.5") {
			found = true
		}
	}
	if !found {
		t.Fatalf("last saved action summary was not restored: %#v", model.Changes)
	}
}

func TestEmptyInputAndLocalPagesDoNotSubmitCommands(t *testing.T) {
	app, err := OpenAt(storage.LayoutFor(t.TempDir()), DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err := app.HandleKey(""); err != nil {
		t.Fatal(err)
	}
	before := app.State()
	for _, key := range []string{"d", "n", "b", "i", "b", "h", "b", "s", "b"} {
		if _, err := app.HandleKey(key); err != nil {
			t.Fatalf("local key %q: %v", key, err)
		}
	}
	after := app.State()
	if after.Revision != before.Revision || after.Counters.WorldMonth != before.Counters.WorldMonth || after.Counters.InteractionSeq != before.Counters.InteractionSeq {
		t.Fatalf("local navigation changed game state: before=%#v after=%#v", before.Counters, after.Counters)
	}
}

func TestCreationTextEditingTreatsQAsTextAndPersistsDaoName(t *testing.T) {
	layout := storage.LayoutFor(t.TempDir())
	app, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.HandleKey("1"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.HandleKey("e"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.HandleKey("1"); err != nil {
		t.Fatal(err)
	}
	if !app.TextInputActive() {
		t.Fatal("selecting a creation field should start line input")
	}
	if exit, err := app.HandleKey("q"); err != nil || exit {
		t.Fatalf("q in text input must not exit: exit=%t err=%v", exit, err)
	}
	if err := app.HandleText("q\r\n"); err != nil {
		t.Fatal(err)
	}
	if app.State().Pending.Creation.Identity.Surname != "q" || app.State().Counters.WorldMonth != 0 {
		t.Fatalf("q was not stored as a name or editing advanced time: %#v", app.State().Pending.Creation.Identity)
	}
	if _, err := app.HandleKey("3"); err != nil {
		t.Fatal(err)
	}
	if err := app.HandleText("青玄"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.HandleKey("b"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.HandleKey("c"); err != nil {
		t.Fatal(err)
	}
	player := app.State().Player
	if player == nil || player.Identity.Surname != "q" || player.Identity.DaoName != "青玄" {
		t.Fatalf("creation confirmation lost edited identity fields: %#v", player)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	restored, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got := restored.State().Player.Identity; got.Surname != "q" || got.DaoName != "青玄" {
		t.Fatalf("edited identity did not survive reload: %#v", got)
	}
}

func TestCreationAgeTextValidationKeepsTheEditorOpen(t *testing.T) {
	app, err := OpenAt(storage.LayoutFor(t.TempDir()), DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err := app.HandleKey("1"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.HandleKey("e"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.HandleKey("6"); err != nil {
		t.Fatal(err)
	}
	if err := app.HandleText("q"); err != nil {
		t.Fatal(err)
	}
	if !app.TextInputActive() || app.State().Pending.Creation.AgeYears != engine.CreationDefaultAgeYears {
		t.Fatal("invalid age input should preserve both the editor and saved draft")
	}
	if err := app.HandleText("31"); err != nil {
		t.Fatal(err)
	}
	if app.TextInputActive() || app.State().Pending.Creation.AgeYears != 31 {
		t.Fatal("valid age input should commit and close the text editor")
	}
}

func TestFailedSettingsWriteLeavesPreferencesAndGameStateUntouched(t *testing.T) {
	root := t.TempDir()
	layout := storage.LayoutFor(root)
	app, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	before := app.State()
	// Make the settings target an existing directory so its atomic replacement
	// fails without touching the independent character snapshot.
	if err := os.Mkdir(layout.SettingsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = app.HandleKey("s")
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.HandleKey("c")
	if err != nil {
		t.Fatal(err)
	}
	after := app.State()
	if after.Revision != before.Revision || after.Counters.WorldMonth != before.Counters.WorldMonth || app.Settings().ColorMode != "auto" {
		t.Fatalf("failed settings save changed live game or preference state: revision=%d mode=%q", after.Revision, app.Settings().ColorMode)
	}
}
