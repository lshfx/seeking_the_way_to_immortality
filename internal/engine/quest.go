package engine

import "fmt"

// applyAcceptQuest opens one commission and reserves its declared materials.
// Acceptance is a zero-month action, but the reservation is committed so a
// player cannot accept the same commission twice before running it.
func (e *Engine) applyAcceptQuest(s *GameState, c Command, result CommandResult) CommandResult {
	if s == nil || s.Player == nil || s.World == nil || e.Catalogue == nil {
		return reject(c, ErrPreconditionUnmet, "quest acceptance needs a player, world and catalogue")
	}
	questID := c.Payload.QuestID
	if questID == "" {
		questID = c.TargetID
	}
	if questID == "" || (c.TargetID != "" && c.TargetID != questID) {
		return reject(c, ErrPreconditionUnmet, "quest acceptance needs one matching quest id")
	}
	def := findQuest(e.Catalogue, questID)
	if def == nil {
		return reject(c, ErrUnknownTarget, "quest is not declared: "+questID)
	}
	if def.RequiresSect && s.Player.SectID == "" {
		return reject(c, ErrPreconditionUnmet, "this commission is for sect members")
	}
	if !questGiverAvailable(s, def) {
		return reject(c, ErrPreconditionUnmet, "the quest giver is not available at this scene")
	}
	if s.World.Quests == nil {
		s.World.Quests = map[string]QuestState{}
	}
	qs := s.World.Quests[questID]
	if qs.QuestID == "" {
		qs.QuestID = questID
	}
	if qs.Status == QuestAccepted || qs.Status == QuestComplete {
		return reject(c, ErrPreconditionUnmet, "this quest is already in progress or awaiting its reward")
	}
	if !def.Repeatable && (qs.Status == QuestClaimed || qs.Status == QuestFailed ||
		qs.Status == QuestAbandoned || qs.Status == QuestExpired) {
		return reject(c, ErrPreconditionUnmet, "this quest cannot be accepted again")
	}
	if s.Counters.WorldMonth < qs.AvailableAfterWorldMonth {
		return reject(c, ErrPreconditionUnmet, fmt.Sprintf("this quest returns after world month %d", qs.AvailableAfterWorldMonth))
	}
	requiredTotals := map[string]int64{}
	for _, required := range def.RequiredItems {
		requiredTotals[required.ItemID] += required.Quantity
	}
	for itemID, quantity := range requiredTotals {
		if heldQuantity(&s.Player.Inventory, itemID) < quantity {
			return reject(c, ErrQuantityNotHeld, fmt.Sprintf("need %d of %s to accept this quest", quantity, itemID))
		}
	}
	reserved := append([]ItemStack(nil), def.RequiredItems...)
	for itemID, quantity := range requiredTotals {
		before := heldQuantity(&s.Player.Inventory, itemID)
		setHeldQuantity(&s.Player.Inventory, itemID, before-quantity)
		result.Delta.Entries = append(result.Delta.Entries, DeltaEntry{
			Field: "items." + itemID, Before: before,
			After: before - quantity, Reason: "quest_accept_reserve",
		})
	}
	qs.Status = QuestAccepted
	qs.RewardsClaimed = false
	qs.Progress = 0
	qs.AcceptedWorldMonth = s.Counters.WorldMonth
	qs.ResolvedWorldMonth = 0
	qs.ReservedItems = reserved
	s.World.Quests[questID] = qs
	result.Events = append(result.Events, ResultEvent{Kind: ResultQuestChanged, ID: questID, Detail: "accepted"})
	result.MonthCostApplied = 0
	return result
}

// applyClaimReward pays a completed commission exactly once. Failed and
// abandoned commissions deliberately have no success payout path.
func (e *Engine) applyClaimReward(s *GameState, c Command, result CommandResult) CommandResult {
	if s == nil || s.Player == nil || s.World == nil || e.Catalogue == nil {
		return reject(c, ErrPreconditionUnmet, "claiming a quest needs a player, world and catalogue")
	}
	questID := c.Payload.QuestID
	if questID == "" {
		questID = c.TargetID
	}
	if questID == "" || (c.TargetID != "" && c.TargetID != questID) {
		return reject(c, ErrPreconditionUnmet, "quest claim needs one matching quest id")
	}
	def := findQuest(e.Catalogue, questID)
	if def == nil {
		return reject(c, ErrUnknownTarget, "quest is not declared: "+questID)
	}
	qs, ok := s.World.Quests[questID]
	if !ok || qs.Status != QuestComplete {
		return reject(c, ErrPreconditionUnmet, "only a successful completed quest can pay a reward")
	}
	if qs.RewardsClaimed {
		return reject(c, ErrPreconditionUnmet, "this quest reward was already claimed")
	}
	for _, reward := range def.Rewards {
		if err := applyGrantEffect(s, e.Catalogue, reward, &result.Delta); err != nil {
			return reject(c, ErrPreconditionUnmet, err.Error())
		}
	}
	// Sect duty income is configured separately from contribution. Paying it
	// at claim keeps the 20-stone monthly baseline visible in the same durable
	// transaction as the duty result.
	if def.Kind == QuestPatrol && s.Player.SectID != "" {
		for _, sect := range e.Catalogue.Sects {
			if sect.ID != s.Player.SectID || sect.MonthlyIncome.Value <= 0 {
				continue
			}
			effect := GrantEffect{Kind: GrantAdditive, Target: "resources.spirit_stones",
				Amount: int64(sect.MonthlyIncome.Value), Reason: "宗门月任务净收入"}
			if err := applyGrantEffect(s, e.Catalogue, effect, &result.Delta); err != nil {
				return reject(c, ErrPreconditionUnmet, err.Error())
			}
			break
		}
	}
	qs.Status = QuestClaimed
	qs.RewardsClaimed = true
	qs.ReservedItems = nil
	s.World.Quests[questID] = qs
	result.Events = append(result.Events, ResultEvent{Kind: ResultQuestChanged, ID: questID, Detail: "claimed"})
	result.MonthCostApplied = 0
	return result
}

// applyJoinSect confirms a sect entry after the configured entry commission
// has been completed and claimed. Joining is zero-month and irreversible in M1.
func (e *Engine) applyJoinSect(s *GameState, c Command, result CommandResult) CommandResult {
	if s == nil || s.Player == nil || s.World == nil || e.Catalogue == nil {
		return reject(c, ErrPreconditionUnmet, "joining a sect needs a player, world and catalogue")
	}
	sectID := c.Payload.QuestID
	if sectID == "" {
		sectID = c.TargetID
	}
	if sectID == "" || (c.TargetID != "" && c.TargetID != sectID) {
		return reject(c, ErrPreconditionUnmet, "sect joining needs one matching sect id")
	}
	var sect *SectDefinition
	for i := range e.Catalogue.Sects {
		if e.Catalogue.Sects[i].ID == sectID {
			sect = &e.Catalogue.Sects[i]
			break
		}
	}
	if sect == nil {
		return reject(c, ErrUnknownTarget, "sect is not declared: "+sectID)
	}
	if s.Player.SectID != "" {
		return reject(c, ErrPreconditionUnmet, "the character already belongs to a sect")
	}
	if sect.EntryQuestID != "" {
		if entryDef := findQuest(e.Catalogue, sect.EntryQuestID); entryDef != nil && entryDef.LocationID != "" &&
			s.World.CurrentLocation != entryDef.LocationID {
			return reject(c, ErrPreconditionUnmet, "return to the sect entry scene before joining")
		}
	}
	if sect.EntryQuestID != "" {
		entry, ok := s.World.Quests[sect.EntryQuestID]
		if !ok || entry.Status != QuestClaimed {
			return reject(c, ErrPreconditionUnmet, "the sect entry commission must be claimed first")
		}
	}
	s.Player.SectID = sect.ID
	result.Events = append(result.Events, ResultEvent{Kind: ResultQuestChanged, ID: sect.ID, Detail: "sect_joined"})
	result.MonthCostApplied = 0
	return result
}

func questGiverAvailable(s *GameState, def *QuestDefinition) bool {
	if def == nil || def.GiverNPCID == "" {
		return true
	}
	if s == nil || s.World == nil {
		return false
	}
	npc, ok := s.World.NPCs[def.GiverNPCID]
	return ok && npc.IsAlive && npc.Location == s.World.CurrentLocation
}

func questFailureChance(def *QuestDefinition) int {
	if def == nil || def.FailureChancePermille.Provenance == "" {
		return 0
	}
	return int(def.FailureChancePermille.Value)
}
