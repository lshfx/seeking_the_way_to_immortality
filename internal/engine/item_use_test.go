package engine

import (
	"reflect"
	"testing"
)

func useTestEngine(t *testing.T) (*Engine, *MemoryStore) {
	t.Helper()
	state := readyState()
	state.Player.XP = 80 * SCALE
	state.Player.HP.Current = 20 * SCALE
	state.Player.Inventory.Stacks = []ItemStack{{ItemID: "qi", Quantity: 2}, {ItemID: "heal", Quantity: 1}, {ItemID: "foundation", Quantity: 1}}
	cat := testCatalogue()
	cat.Items = []ItemDefinition{
		{ID: "qi", Category: ItemConsumable, UseOutsideCombat: true,
			Effects: []GrantEffect{{Kind: GrantAdditive, Target: "xp", Amount: 50 * SCALE}}},
		{ID: "heal", Category: ItemConsumable, UseOutsideCombat: true,
			Effects: []GrantEffect{{Kind: GrantAdditive, Target: "hp.current", Amount: 40 * SCALE}}},
		{ID: "foundation", Category: ItemConsumable,
			Effects: []GrantEffect{{Kind: GrantAdditive, Target: "breakthrough.foundation_bonus", Amount: 10}}},
	}
	return newTestEngineWithCatalogue(t, state, cat)
}

func useCommand(e *Engine, actionID, itemID string, quantity int64) Command {
	c := cmd(e, actionID, KindUseItem)
	c.TargetID = itemID
	c.Payload = Payload{ItemID: itemID, Quantity: quantity}
	return c
}

func TestUseItemAppliesBoundedEffectAndPersistsOnce(t *testing.T) {
	e, store := useTestEngine(t)
	before := CloneGameState(e.State())
	c := useCommand(e, "qi-once", "qi", 1)
	r := e.Submit(c)
	if !r.OK || e.State().Player.XP != 100*SCALE || heldQuantity(&e.State().Player.Inventory, "qi") != 1 ||
		e.State().Counters.WorldMonth != 0 || !reflect.DeepEqual(before.RNG, e.State().RNG) {
		t.Fatalf("direct use did not atomically apply one bounded dose: %#v", r)
	}
	revision := e.State().Revision
	if repeat := e.Submit(c); !repeat.OK || e.State().Revision != revision {
		t.Fatal("duplicate item action consumed a second dose")
	}
	restarted := NewEngine(store, e.Catalogue, CloneGameState(store.Committed()))
	if repeat := restarted.Submit(c); !repeat.OK || restarted.State().Revision != revision {
		t.Fatal("restarted session consumed a duplicate dose")
	}
	if e.Submit(useCommand(e, "qi-full", "qi", 1)).OK || heldQuantity(&e.State().Player.Inventory, "qi") != 1 {
		t.Fatal("full cultivation threshold should leave the pill in the pack")
	}
	if r := e.Submit(useCommand(e, "heal-once", "heal", 1)); !r.OK || e.State().Player.HP.Current != 40*SCALE ||
		heldQuantity(&e.State().Player.Inventory, "heal") != 0 {
		t.Fatalf("healing pill was not applied: %#v", r)
	}
}

func TestUseItemRejectsMissingOrReservedItemsWithoutMutation(t *testing.T) {
	e, _ := useTestEngine(t)
	before := CloneGameState(e.State())
	for _, c := range []Command{
		useCommand(e, "too-many", "qi", 3),
		useCommand(e, "reserved", "foundation", 1),
		useCommand(e, "zero", "heal", 0),
	} {
		if r := e.Submit(c); r.OK || !reflect.DeepEqual(before, CloneGameState(e.State())) {
			t.Fatalf("invalid use changed state: %#v", r)
		}
	}
}

func TestFailedItemUseSaveLeavesInventoryAndVitalsUntouched(t *testing.T) {
	e, store := useTestEngine(t)
	before := CloneGameState(e.State())
	c := useCommand(e, "heal-disk-error", "heal", 1)
	store.FailNextCommit()
	if r := e.Submit(c); r.OK || r.SaveState != SaveFailed || !reflect.DeepEqual(before, CloneGameState(e.State())) ||
		!reflect.DeepEqual(before, store.Committed()) {
		t.Fatalf("failed save partially consumed an item: %#v", r)
	}
	if r := e.Submit(c); !r.OK || e.State().Player.HP.Current != 40*SCALE {
		t.Fatalf("item could not be retried after refused write: %#v", r)
	}
}
