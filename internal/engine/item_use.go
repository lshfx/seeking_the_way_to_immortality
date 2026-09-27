package engine

// applyUseItem consumes a direct-use item and applies its configured effects
// on the same candidate state. A rejected effect or disk write consumes none.
func (e *Engine) applyUseItem(s *GameState, c Command, result CommandResult) CommandResult {
	if s == nil || s.Player == nil || e.Catalogue == nil {
		return reject(c, ErrPreconditionUnmet, "item use needs a player and catalogue")
	}
	itemID, quantity := c.Payload.ItemID, c.Payload.Quantity
	if c.TargetID != itemID || itemID == "" || quantity <= 0 {
		return reject(c, ErrPreconditionUnmet, "item use needs a matching item and positive quantity")
	}
	item := findItem(e.Catalogue, itemID)
	if item == nil {
		return reject(c, ErrUnknownItem, "item is not declared")
	}
	if !item.UseOutsideCombat || item.Category != ItemConsumable || len(item.Effects) == 0 {
		return reject(c, ErrPreconditionUnmet, "item is not a direct-use consumable")
	}
	before := heldQuantity(&s.Player.Inventory, itemID)
	if before < quantity {
		return reject(c, ErrQuantityNotHeld, "item quantity is not held")
	}
	for unit := int64(0); unit < quantity; unit++ {
		useful := false
		for _, original := range item.Effects {
			effect := original
			if effect.Target == targetXP && effect.Kind == GrantAdditive {
				threshold := int64(0)
				for _, realm := range e.Catalogue.Realms {
					if realm.ID == s.Player.Realm {
						threshold = int64(realm.TierThreshold.Value)
						break
					}
				}
				if threshold <= 0 {
					return reject(c, ErrInvariantBroken, "current realm has no XP threshold")
				}
				remaining := threshold - s.Player.XP
				if remaining < 0 {
					remaining = 0
				}
				if remaining < effect.Amount {
					effect.Amount = remaining
				}
			}
			previousEntries := len(result.Delta.Entries)
			if err := applyGrantEffect(s, e.Catalogue, effect, &result.Delta); err != nil {
				return reject(c, ErrPreconditionUnmet, err.Error())
			}
			for _, entry := range result.Delta.Entries[previousEntries:] {
				if entry.After != entry.Before {
					useful = true
				}
			}
		}
		if !useful {
			return reject(c, ErrPreconditionUnmet, "item would have no useful effect")
		}
	}
	setHeldQuantity(&s.Player.Inventory, itemID, before-quantity)
	result.Delta.Entries = append(result.Delta.Entries, DeltaEntry{
		Field: "items." + itemID, Before: before, After: before - quantity, Reason: "item_use",
	})
	result.Events = append(result.Events, ResultEvent{Kind: "item_used", ID: itemID})
	result.MonthCostApplied = 0
	return result
}
