package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/storage"
)

func TestQuestMenuAcceptRunClaimAndReload(t *testing.T) {
	layout := storage.LayoutFor(t.TempDir())
	app, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	// Keep this session test focused on the quest page. The engine tests cover
	// month-end event selection separately, so disable the optional event roll.
	app.catalogue.EventBaseChancePermille.Value = 0
	press(t, app, "1", "c", "2", "1", "4")
	if app.Model().Title != "委托与收入" || len(app.Model().Options) == 0 {
		t.Fatalf("quest menu did not open: %#v", app.Model())
	}
	press(t, app, "1")
	detail := app.Model()
	if detail.Title != "坊市杂务" || !strings.Contains(detail.Scene, "三十灵石") {
		t.Fatalf("quest detail did not expose the configured commission: %#v", detail)
	}
	month := app.State().Counters.WorldMonth
	press(t, app, "a")
	accepted := app.State()
	if accepted.World.Quests["quest_safe_chore"].Status != engine.QuestAccepted || accepted.Counters.WorldMonth != month {
		t.Fatalf("accepting a quest changed the wrong state: %#v", accepted.World.Quests["quest_safe_chore"])
	}
	press(t, app, "4", "1", "x")
	if app.Model().Confirmation == nil || !strings.Contains(app.Model().Confirmation.CostText, "耗时1游戏月") {
		t.Fatalf("quest execution did not show a confirmation: %#v", app.Model())
	}
	press(t, app, "c")
	completed := app.State()
	if completed.World.Quests["quest_safe_chore"].Status != engine.QuestComplete || completed.Counters.WorldMonth != month+1 {
		t.Fatalf("quest execution did not settle one month: %#v", completed)
	}
	press(t, app, "4", "1", "c")
	claimed := app.State()
	if claimed.World.Quests["quest_safe_chore"].Status != engine.QuestClaimed ||
		claimed.Player.Resources[engine.ResSpiritStones] != 130 {
		t.Fatalf("quest claim did not pay exactly once: %#v", claimed)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got := restored.State()
	if got.World.Quests["quest_safe_chore"].Status != engine.QuestClaimed ||
		got.Player.Resources[engine.ResSpiritStones] != 130 || got.Counters.WorldMonth != month+1 {
		t.Fatalf("quest progress did not survive reload: %#v", got)
	}
}

func TestTask13SaveUpgradePreservesProgressAndStartsEmptyQuests(t *testing.T) {
	layout := storage.LayoutFor(t.TempDir())
	app, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	press(t, app, "1", "c")
	oldState := app.State()
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	oldState.SchemaVersion, oldState.RulesVersion, oldState.ContentVersion = 5, 6, 6
	oldState.World.Quests = nil
	old := envelopeFor(oldState, []string{"TASK-13旧进度"})
	old.ComputeIntegrity()
	original, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.SavePath(DefaultGameID), original, 0o600); err != nil {
		t.Fatal(err)
	}

	upgraded, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	state := upgraded.State()
	if state.SchemaVersion != engine.SchemaVersion || state.RulesVersion != engine.RulesVersion ||
		state.ContentVersion != engine.ContentVersion || state.Revision != oldState.Revision+1 ||
		state.World.Quests == nil || len(state.World.Quests) != 0 || state.Counters.WorldMonth != oldState.Counters.WorldMonth {
		t.Fatalf("TASK-13 upgrade changed progress or quest baseline: %#v", state)
	}
	copies, err := filepath.Glob(filepath.Join(layout.PreservedDir(), "*pre-task14*"))
	if err != nil || len(copies) != 1 {
		t.Fatalf("TASK-13 save was not preserved exactly once: copies=%v err=%v", copies, err)
	}
	copyBytes, err := os.ReadFile(copies[0])
	if err != nil || !bytes.Equal(copyBytes, original) {
		t.Fatalf("preserved TASK-13 save differs from original: %v", err)
	}
}
