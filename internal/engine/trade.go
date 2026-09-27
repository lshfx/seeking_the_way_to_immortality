package engine

import "fmt"

// TradeQuote is a read-only preview of a single market transaction. It is
// bound to one revision and view token; leaving the screen spends nothing.
type TradeQuote struct {
	Token         string
	Revision      uint64
	ViewToken     string
	Side          TradeSide
	ItemID        string
	Quantity      int64
	UnitPrice     int64
	Total         int64
	StockBefore   int64
	StockAfter    int64
	HeldBefore    int64
	HeldAfter     int64
	BalanceBefore int64
	BalanceAfter  int64
}

// TradeError keeps the engine's machine-readable refusal code available to a
// UI while still giving a useful diagnostic to tests and local tools.
type TradeError struct {
	Code   ErrorCode
	Detail string
}

func (e TradeError) Error() string { return e.Detail }

// InitialMarketStock creates a fresh stock map from validated catalogue data.
// No menu render calls this function: stock is created only with a new game or
// an explicit save migration.
func InitialMarketStock(cat *Catalogue) map[string]int64 {
	stock := map[string]int64{}
	if cat == nil {
		return stock
	}
	for _, offer := range cat.MarketOffers {
		stock[offer.ItemID] = int64(offer.InitialStock.Value)
	}
	return stock
}

// PreviewTrade checks a proposed trade without changing mechanical state.
// It remembers only the current quote in process memory; reopening the game
// cancels the proposal rather than automatically spending money.
func (e *Engine) PreviewTrade(side TradeSide, itemID string, quantity int64) (TradeQuote, error) {
	if e == nil || e.state == nil || e.state.Phase != PhaseReady {
		return TradeQuote{}, TradeError{ErrBadPhase, "trading is unavailable in this phase"}
	}
	quote, code, detail := e.calculateTrade(e.state, side, itemID, quantity)
	if code != ErrNone {
		return TradeQuote{}, TradeError{code, detail}
	}
	e.tradeSeq++
	quote.Token = fmt.Sprintf("trade:%d:%d:%d", e.state.Revision, e.viewTokenSeq, e.tradeSeq)
	quote.Revision = e.state.Revision
	quote.ViewToken = e.viewToken
	e.tradeQuote = &quote
	return quote, nil
}

// CancelTradeQuote discards an unconfirmed local proposal without touching the
// save, clocks, balances or random streams.
func (e *Engine) CancelTradeQuote() {
	if e != nil {
		e.tradeQuote = nil
	}
}

func (e *Engine) calculateTrade(s *GameState, side TradeSide, itemID string, quantity int64) (TradeQuote, ErrorCode, string) {
	if s == nil || s.Player == nil || s.World == nil || e.Catalogue == nil {
		return TradeQuote{}, ErrPreconditionUnmet, "trade needs a player, world and catalogue"
	}
	if s.World.CurrentLocation != "market" {
		return TradeQuote{}, ErrPreconditionUnmet, "travel to the market before trading"
	}
	if !side.Valid() || quantity <= 0 {
		return TradeQuote{}, ErrPreconditionUnmet, "trade side and positive quantity are required"
	}
	item := findItem(e.Catalogue, itemID)
	if item == nil || !marketOffersItem(e.Catalogue, itemID) {
		return TradeQuote{}, ErrUnknownItem, "item is not listed by this market"
	}
	stock, ok := s.World.MarketStock[itemID]
	if !ok || stock < 0 {
		return TradeQuote{}, ErrPreconditionUnmet, "market stock is unavailable"
	}
	held := heldQuantity(&s.Player.Inventory, itemID)
	balance := s.Player.Resources[ResSpiritStones]
	unit := int64(item.BuyPrice.Value)
	if side == TradeSell {
		unit = int64(item.SellPrice.Value)
	}
	if unit < 0 {
		return TradeQuote{}, ErrInvariantBroken, "item has a negative price"
	}
	total, err := mulChecked(unit, quantity)
	if err != nil {
		return TradeQuote{}, ErrPreconditionUnmet, "trade total overflows"
	}
	q := TradeQuote{Side: side, ItemID: itemID, Quantity: quantity, UnitPrice: unit,
		Total: total, StockBefore: stock, HeldBefore: held, BalanceBefore: balance}
	if side == TradeBuy {
		if stock < quantity {
			return TradeQuote{}, ErrPreconditionUnmet, "market stock is insufficient"
		}
		if balance < total {
			return TradeQuote{}, ErrInsufficientFunds, "spirit stones are insufficient; no debt is created"
		}
		afterHeld, err := addChecked(held, quantity)
		if err != nil || (item.StackLimit > 0 && afterHeld > int64(item.StackLimit)) {
			return TradeQuote{}, ErrPreconditionUnmet, "item stack limit would be exceeded"
		}
		if held == 0 && s.Player.Inventory.Capacity > 0 && len(s.Player.Inventory.Stacks) >= s.Player.Inventory.Capacity {
			return TradeQuote{}, ErrPreconditionUnmet, "inventory has no free stack slot"
		}
		q.StockAfter, q.HeldAfter, q.BalanceAfter = stock-quantity, afterHeld, balance-total
	} else {
		if held < quantity {
			return TradeQuote{}, ErrQuantityNotHeld, "cannot sell an item not held in this quantity"
		}
		q.StockAfter, err = addChecked(stock, quantity)
		if err != nil {
			return TradeQuote{}, ErrPreconditionUnmet, "market stock would overflow"
		}
		q.BalanceAfter, err = addChecked(balance, total)
		if err != nil {
			return TradeQuote{}, ErrPreconditionUnmet, "spirit stone balance would overflow"
		}
		q.HeldAfter = held - quantity
	}
	return q, ErrNone, ""
}

func marketOffersItem(cat *Catalogue, itemID string) bool {
	if cat == nil {
		return false
	}
	for _, offer := range cat.MarketOffers {
		if offer.ItemID == itemID {
			return true
		}
	}
	return false
}

func (e *Engine) applyTrade(s *GameState, c Command, result CommandResult) CommandResult {
	q, code, detail := e.calculateTrade(s, c.Payload.TradeSide, c.Payload.ItemID, c.Payload.Quantity)
	if code != ErrNone {
		return reject(c, code, detail)
	}
	// Price, inventory and stock settle together on the pipeline's clone. A
	// failed disk commit discards all three changes, and a duplicate action id
	// returns the original result before this function is entered.
	s.Player.Resources[ResSpiritStones] = q.BalanceAfter
	setHeldQuantity(&s.Player.Inventory, q.ItemID, q.HeldAfter)
	s.World.MarketStock[q.ItemID] = q.StockAfter
	s.Pending.Confirmation = nil
	reason := "market_buy"
	if q.Side == TradeSell {
		reason = "market_sell"
	}
	result.Delta.Entries = []DeltaEntry{
		{Field: "resources.spirit_stones", Before: q.BalanceBefore, After: q.BalanceAfter, Reason: reason},
		{Field: "items." + q.ItemID, Before: q.HeldBefore, After: q.HeldAfter, Reason: reason},
		{Field: "market_stock." + q.ItemID, Before: q.StockBefore, After: q.StockAfter, Reason: reason},
	}
	result.Events = []ResultEvent{{Kind: reason, ID: q.ItemID}}
	result.MonthCostApplied = 0
	return result
}
