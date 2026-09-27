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

func press(t *testing.T, app *Session, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := app.HandleKey(key); err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
	}
}

func TestMarketBuySellPersistsAndLocalMenusDoNotRestock(t *testing.T) {
	layout := storage.LayoutFor(t.TempDir())
	app, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	press(t, app, "1", "c") // default preset, then confirm character
	before := app.State()
	press(t, app, "2", "1") // cave -> market, zero months
	if app.State().World.CurrentLocation != "market" || app.State().Counters.WorldMonth != 0 {
		t.Fatal("zero-month travel did not reach the market")
	}
	press(t, app, "3")
	stock := app.State().World.MarketStock["qi_gathering_pill"]
	for _, key := range []string{"n", "p", "b", "3"} {
		press(t, app, key)
	}
	if app.State().World.MarketStock["qi_gathering_pill"] != stock || app.State().Counters.WorldMonth != 0 {
		t.Fatal("opening or paging the market changed stock or time")
	}
	press(t, app, "1")
	quote := app.Model().Confirmation
	if quote == nil || !strings.Contains(quote.CostText, "20") || !strings.Contains(quote.Summary, "库存") {
		t.Fatalf("purchase confirmation hid its price or stock: %#v", quote)
	}
	if app.State().Player.Resources[engine.ResSpiritStones] != before.Player.Resources[engine.ResSpiritStones] {
		t.Fatal("preview charged money")
	}
	press(t, app, "+")
	if quote := app.Model().Confirmation; quote == nil || !strings.Contains(quote.CostText, "40") {
		t.Fatalf("quantity adjustment did not update total: %#v", quote)
	}
	press(t, app, "c")
	bought := app.State()
	if bought.Player.Resources[engine.ResSpiritStones] != 60 ||
		bought.World.MarketStock["qi_gathering_pill"] != stock-2 ||
		len(bought.Player.Inventory.Stacks) != 1 || bought.Player.Inventory.Stacks[0].Quantity != 2 ||
		bought.Counters.WorldMonth != 0 || bought.Player.Debt != 0 {
		t.Fatalf("buy did not atomically update all three ledgers: %#v", bought)
	}
	press(t, app, "t", "1") // switch to sale, quote held pill
	if app.Model().Confirmation == nil {
		t.Fatal("sale did not open a confirmation page")
	}
	press(t, app, "b")
	if app.State().Revision != bought.Revision {
		t.Fatal("cancelling a sale changed the save")
	}
	press(t, app, "1", "c")
	sold := app.State()
	if sold.Player.Resources[engine.ResSpiritStones] != 70 ||
		sold.World.MarketStock["qi_gathering_pill"] != stock-1 ||
		sold.Player.Inventory.Stacks[0].Quantity != 1 || sold.Counters.WorldMonth != 0 {
		t.Fatalf("sale or the 50%% recycle spread is wrong: %#v", sold)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.State().World.MarketStock["qi_gathering_pill"] != stock-1 ||
		restored.State().Player.Resources[engine.ResSpiritStones] != 70 {
		t.Fatal("reload lost money or market stock")
	}
}

func TestBoughtPillCanBeUsedFromInventoryAndRestored(t *testing.T) {
	layout := storage.LayoutFor(t.TempDir())
	app, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	press(t, app, "1", "c", "2", "1", "3", "1", "c")
	bought := app.State()
	if bought.Player.XP >= 50*engine.SCALE {
		t.Fatal("test starts too close to the cultivation threshold")
	}
	press(t, app, "b", "i", "u")
	if app.Model().Title != "使用随身丹药" || len(app.Model().Options) == 0 {
		t.Fatal("inventory has no direct-use entry")
	}
	press(t, app, "1")
	used := app.State()
	if used.Player.XP != bought.Player.XP+50*engine.SCALE || len(used.Player.Inventory.Stacks) != 0 ||
		used.Counters.WorldMonth != bought.Counters.WorldMonth {
		t.Fatalf("using the purchased pill failed: %#v", used)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenAt(layout, DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.State().Player.XP != used.Player.XP || len(restored.State().Player.Inventory.Stacks) != 0 {
		t.Fatal("reopening lost the consumed pill or its effect")
	}
}

func TestTask12SaveUpgradePreservesOriginalAndExistingItems(t *testing.T) {
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
	oldState.SchemaVersion, oldState.RulesVersion, oldState.ContentVersion = 4, 5, 5
	oldState.World.MarketStock = nil
	oldState.Player.Inventory.Stacks = []engine.ItemStack{{ItemID: "spirit_herb", Quantity: 3}}
	oldEnvelope := envelopeFor(oldState, []string{"旧进度"})
	oldEnvelope.ComputeIntegrity()
	data, err := json.Marshal(oldEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.SavePath(DefaultGameID), data, 0o600); err != nil {
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
		state.Counters.WorldMonth != oldState.Counters.WorldMonth ||
		len(state.Player.Inventory.Stacks) != 1 || state.Player.Inventory.Stacks[0].Quantity != 3 ||
		state.World.MarketStock["foundation_pill"] != 8 {
		t.Fatalf("TASK-12 upgrade discarded progress or stock: %#v", state)
	}
	if notice := upgraded.Model().Notice; notice == nil || !strings.Contains(notice.Text, "升级") {
		t.Fatalf("upgrade was not disclosed on resume: %#v", notice)
	}
	copies, err := filepath.Glob(filepath.Join(layout.PreservedDir(), "*pre-task13*"))
	if err != nil || len(copies) != 1 {
		t.Fatalf("old save was not preserved exactly once: copies=%v err=%v", copies, err)
	}
	copyBytes, err := os.ReadFile(copies[0])
	if err != nil || !bytes.Equal(copyBytes, data) {
		t.Fatalf("preserved save differs from original: %v", err)
	}
}
