package engine

import (
	"reflect"
	"testing"
)

func marketTestEngine(t *testing.T) (*Engine, *MemoryStore) {
	t.Helper()
	state := readyState()
	state.World.CurrentLocation = "market"
	state.World.VisitedLocations = []string{"market"}
	state.World.MarketStock = map[string]int64{"pill": 3}
	state.Player.Resources[ResSpiritStones] = 100
	cat := testCatalogue()
	cat.Items = []ItemDefinition{{ID: "pill", NameZH: "测试丹", Category: ItemConsumable,
		BuyPrice: ConfigValue{Value: 20}, SellPrice: ConfigValue{Value: 10}, StackLimit: 9}}
	cat.MarketOffers = []MarketOfferDefinition{{ItemID: "pill", InitialStock: ConfigValue{Value: 3}}}
	return newTestEngineWithCatalogue(t, state, cat)
}

func tradeCommand(e *Engine, actionID string, quote TradeQuote) Command {
	c := cmd(e, actionID, KindTrade)
	c.TargetID = quote.ItemID
	c.Payload = Payload{TradeSide: quote.Side, ItemID: quote.ItemID, Quantity: quote.Quantity}
	c.ConfirmationToken = quote.Token
	return c
}

func TestTradeBuySellAndIdempotency(t *testing.T) {
	e, store := marketTestEngine(t)
	before := CloneGameState(e.State())
	quote, err := e.PreviewTrade(TradeBuy, "pill", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, CloneGameState(e.State())) {
		t.Fatal("preview changed mechanical state")
	}
	if quote.Total != 40 || quote.StockAfter != 1 || quote.BalanceAfter != 60 {
		t.Fatalf("wrong purchase preview: %#v", quote)
	}
	c := tradeCommand(e, "buy-1", quote)
	r := e.Submit(c)
	if !r.OK || r.MonthCostApplied != 0 || len(r.Delta.Entries) != 3 {
		t.Fatalf("purchase did not commit one complete ledger: %#v", r)
	}
	state := e.State()
	if state.Player.Resources[ResSpiritStones] != 60 || heldQuantity(&state.Player.Inventory, "pill") != 2 ||
		state.World.MarketStock["pill"] != 1 || state.Counters.WorldMonth != 0 || state.Player.Debt != 0 {
		t.Fatalf("purchase committed inconsistent money, items or stock: %#v", state)
	}
	if !reflect.DeepEqual(before.RNG, state.RNG) {
		t.Fatal("a zero-month trade consumed randomness")
	}
	revision := state.Revision
	again := e.Submit(c)
	if !again.OK || e.State().Revision != revision || !reflect.DeepEqual(again.Delta, r.Delta) {
		t.Fatal("resending one action id charged the trade twice")
	}
	if !reflect.DeepEqual(store.Committed(), CloneGameState(e.State())) {
		t.Fatal("committed snapshot differs from the live state")
	}
	restarted := NewEngine(store, e.Catalogue, CloneGameState(store.Committed()))
	if replay := restarted.Submit(c); !replay.OK || replay.RevisionAfter != r.RevisionAfter ||
		restarted.State().Revision != revision {
		t.Fatal("restarting from the durable snapshot charged an action id again")
	}
	conflict := c
	conflict.Payload.TradeSide = TradeSell
	if got := e.Submit(conflict); got.Code != ErrActionIdConflict {
		t.Fatalf("reusing the action id for the opposite side returned %s", got.Code)
	}

	sellQuote, err := e.PreviewTrade(TradeSell, "pill", 1)
	if err != nil {
		t.Fatal(err)
	}
	sell := e.Submit(tradeCommand(e, "sell-1", sellQuote))
	if !sell.OK || e.State().Player.Resources[ResSpiritStones] != 70 ||
		heldQuantity(&e.State().Player.Inventory, "pill") != 1 || e.State().World.MarketStock["pill"] != 2 {
		t.Fatalf("sale or buy-sell spread is wrong: %#v", sell)
	}
}

func TestTradeRefusalsLeaveAllLedgersUntouched(t *testing.T) {
	tests := []struct {
		name string
		prep func(*Engine)
		side TradeSide
		qty  int64
		code ErrorCode
	}{
		{"insufficient funds", func(e *Engine) { e.State().Player.Resources[ResSpiritStones] = 19 }, TradeBuy, 1, ErrInsufficientFunds},
		{"out of stock", func(e *Engine) {}, TradeBuy, 4, ErrPreconditionUnmet},
		{"item not held", func(e *Engine) {}, TradeSell, 1, ErrQuantityNotHeld},
		{"stack limit", func(e *Engine) { e.State().Player.Inventory.Stacks = []ItemStack{{ItemID: "pill", Quantity: 9}} }, TradeBuy, 1, ErrPreconditionUnmet},
		{"full inventory", func(e *Engine) {
			e.State().Player.Inventory.Capacity = 1
			e.State().Player.Inventory.Stacks = []ItemStack{{ItemID: "other", Quantity: 1}}
		}, TradeBuy, 1, ErrPreconditionUnmet},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := marketTestEngine(t)
			tc.prep(e)
			before := CloneGameState(e.State())
			_, err := e.PreviewTrade(tc.side, "pill", tc.qty)
			tradeErr, ok := err.(TradeError)
			if !ok || tradeErr.Code != tc.code {
				t.Fatalf("refusal = %v, want %s", err, tc.code)
			}
			if !reflect.DeepEqual(before, CloneGameState(e.State())) {
				t.Fatal("refused quote changed the world")
			}
		})
	}
}

func TestTradeNeedsMatchingPreviewAndCancelIsFree(t *testing.T) {
	e, _ := marketTestEngine(t)
	before := CloneGameState(e.State())
	unquoted := cmd(e, "trade-0", KindTrade)
	unquoted.TargetID = "pill"
	unquoted.Payload = Payload{TradeSide: TradeBuy, ItemID: "pill", Quantity: 1}
	if got := e.Submit(unquoted); got.Code != ErrConfirmationNeeded {
		t.Fatalf("unquoted trade returned %s", got.Code)
	}
	quote, err := e.PreviewTrade(TradeBuy, "pill", 1)
	if err != nil {
		t.Fatal(err)
	}
	wrong := tradeCommand(e, "trade-1", quote)
	wrong.Payload.Quantity = 2
	if got := e.Submit(wrong); got.Code != ErrConfirmationExpired {
		t.Fatalf("changed quantity passed a one-unit quote: %s", got.Code)
	}
	e.CancelTradeQuote()
	if got := e.Submit(tradeCommand(e, "trade-2", quote)); got.Code != ErrConfirmationExpired {
		t.Fatalf("cancelled quote returned %s", got.Code)
	}
	if !reflect.DeepEqual(before, CloneGameState(e.State())) {
		t.Fatal("unquoted, altered or cancelled trade changed state")
	}
}

func TestFailedTradeSaveRollsBackAllLedgersAndCanRetry(t *testing.T) {
	e, store := marketTestEngine(t)
	quote, err := e.PreviewTrade(TradeBuy, "pill", 1)
	if err != nil {
		t.Fatal(err)
	}
	command := tradeCommand(e, "buy-after-disk-error", quote)
	before := CloneGameState(e.State())
	store.FailNextCommit()
	failed := e.Submit(command)
	if failed.OK || failed.SaveState != SaveFailed || !reflect.DeepEqual(before, CloneGameState(e.State())) ||
		!reflect.DeepEqual(before, store.Committed()) {
		t.Fatalf("failed disk write left a partial purchase: %#v", failed)
	}
	if retry := e.Submit(command); !retry.OK || e.State().World.MarketStock["pill"] != 2 {
		t.Fatalf("a refused write could not be safely retried: %#v", retry)
	}
}

func TestTradeStockAndInventoryAreCoveredByNewSaveDigest(t *testing.T) {
	e, _ := marketTestEngine(t)
	e.State().Player.Inventory.Stacks = []ItemStack{{ItemID: "pill", Quantity: 1}}
	env := &SaveEnvelope{EnvelopeVersion: EnvelopeVersion, SchemaVersion: SchemaVersion,
		RulesVersion: RulesVersion, ContentVersion: ContentVersion, GameID: e.State().GameID,
		BranchID: e.State().BranchID, Revision: e.State().Revision, State: *CloneGameState(e.State())}
	env.ComputeIntegrity()
	if !env.VerifyIntegrity() {
		t.Fatal("sealed save failed verification")
	}
	env.State.Player.Inventory.Stacks[0].Quantity++
	if env.VerifyIntegrity() {
		t.Fatal("edited inventory was not detected by the digest")
	}
	env.State.Player.Inventory.Stacks[0].Quantity--
	env.State.World.MarketStock["pill"]++
	if env.VerifyIntegrity() {
		t.Fatal("edited market stock was not detected by the digest")
	}
}
