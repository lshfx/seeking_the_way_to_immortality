package engine

// In-memory storage and the idempotency ledger.
//
// TASK-06 provides the durable file-backed store. This file provides the
// interface the pipeline depends on plus an in-memory implementation, so the
// engine's correctness (deduplication, revision checks, settlement) can be
// tested without a filesystem — and so the engine keeps its boundary rule of
// never importing os.

// Store is the persistence boundary as the engine sees it.
//
// It is deliberately tiny. The design document forbids reporting success when
// the write failed (11.5.4), so Commit returns an error and the pipeline
// downgrades SaveState to SaveFailed rather than assuming success.
type Store interface {
	// Load returns the working copy of the state. The returned value is owned
	// by the caller and must not be aliased by the store.
	Load() (*GameState, bool)
	// Commit durably records state. It must either fully succeed or leave the
	// previously committed state intact.
	Commit(state *GameState) error
}

// ErrStoreCommit is returned when a store cannot persist. The engine does not
// invent a cause; TASK-06 supplies a real one.
type ErrStoreCommit struct {
	Reason string
}

// Error implements error.
func (e ErrStoreCommit) Error() string {
	if e.Reason == "" {
		return "store commit failed"
	}
	return "store commit failed: " + e.Reason
}

// MemoryStore is the in-memory Store used by tests and by a session that has not
// yet been given a file path.
//
// It keeps two copies: the committed snapshot and the working state. Commit
// promotes the working copy. A failed commit therefore leaves the committed
// snapshot untouched, which is what makes "abort without side effects"
// testable.
type MemoryStore struct {
	committed *GameState
	working   *GameState

	// failNext makes the next Commit fail, so the SAVE_ERROR path can be
	// exercised. Tests set it; production never does.
	failNext bool
}

// NewMemoryStore creates a store holding a deep copy of state.
func NewMemoryStore(state *GameState) *MemoryStore {
	cp := CloneGameState(state)
	return &MemoryStore{committed: cp, working: CloneGameState(state)}
}

// Load returns the working copy.
func (m *MemoryStore) Load() (*GameState, bool) {
	if m == nil || m.working == nil {
		return nil, false
	}
	return m.working, true
}

// Commit promotes the working state to committed.
func (m *MemoryStore) Commit(state *GameState) error {
	if m == nil {
		return ErrStoreCommit{Reason: "nil store"}
	}
	if m.failNext {
		m.failNext = false
		return ErrStoreCommit{Reason: "induced failure"}
	}
	if state == nil {
		return ErrStoreCommit{Reason: "nil state"}
	}
	m.committed = CloneGameState(state)
	m.working = CloneGameState(state)
	return nil
}

// Committed returns the last durably committed state, for assertions.
func (m *MemoryStore) Committed() *GameState {
	if m == nil {
		return nil
	}
	return m.committed
}

// FailNextCommit makes the next Commit fail. It exists so the SAVE_ERROR
// behaviour can be tested against a real code path rather than by assertion on
// a mock.
func (m *MemoryStore) FailNextCommit() {
	if m != nil {
		m.failNext = true
	}
}

// CloneGameState returns a deep copy of the mechanical state.
//
// The pipeline evaluates a command against a clone and only promotes it on
// success. That is what makes a rejected command a genuine no-op: it cannot
// leave a partially applied effect behind, which is the failure mode that makes
// "cancel does not charge" so easy to get wrong.
func CloneGameState(s *GameState) *GameState {
	if s == nil {
		return nil
	}
	out := *s

	out.Counters = s.Counters

	// RNG streams: copy the map, not the pointer.
	out.RNG = RNGState{
		Algorithm: s.RNG.Algorithm,
		Streams:   make(map[string]RNGStream, len(s.RNG.Streams)),
	}
	for k, v := range s.RNG.Streams {
		out.RNG.Streams[k] = v
	}

	out.Player = clonePlayer(s.Player)
	out.World = cloneWorld(s.World)

	out.Pending = clonePendingState(s.Pending)

	out.Idempotency = cloneIdempotency(s.Idempotency)

	if s.LogCursor != 0 {
		out.LogCursor = s.LogCursor
	}

	return &out
}

// clonePlayer deep-copies the character.
func clonePlayer(p *Player) *Player {
	if p == nil {
		return nil
	}
	out := *p

	out.TalentIDs = append([]string(nil), p.TalentIDs...)

	out.Resources = make(map[Resource]int64, len(p.Resources))
	for k, v := range p.Resources {
		out.Resources[k] = v
	}

	out.Lifespan = cloneLifespan(p.Lifespan)
	out.Inventory = cloneInventory(p.Inventory)
	out.Equipment = cloneEquipment(p.Equipment)
	out.Condition = cloneCondition(p.Condition)

	out.Relations = make([]Relation, len(p.Relations))
	copy(out.Relations, p.Relations)

	out.Proficiencies = make(map[string]int64, len(p.Proficiencies))
	for k, v := range p.Proficiencies {
		out.Proficiencies[k] = v
	}

	return &out
}

// cloneLifespan deep-copies the lifespan ledger.
func cloneLifespan(l LifespanLedger) LifespanLedger {
	out := l
	out.Bonuses = append([]LifespanBonus(nil), l.Bonuses...)
	return out
}

// cloneInventory deep-copies the item stacks.
func cloneInventory(inv Inventory) Inventory {
	out := inv
	out.Stacks = make([]ItemStack, len(inv.Stacks))
	copy(out.Stacks, inv.Stacks)
	return out
}

// cloneEquipment deep-copies the equipped slots.
func cloneEquipment(e Equipment) Equipment {
	out := e
	if e.Slots != nil {
		out.Slots = make(map[EquipSlot]string, len(e.Slots))
		for k, v := range e.Slots {
			out.Slots[k] = v
		}
	}
	return out
}

// cloneCondition deep-copies the player's status effects.
func cloneCondition(c Condition) Condition {
	out := c
	out.Effects = append([]TimedEffect(nil), c.Effects...)
	return out
}

// cloneWorld deep-copies the world half of the state.
func cloneWorld(w *World) *World {
	if w == nil {
		return nil
	}
	out := *w

	out.NPCs = make(map[string]NPC, len(w.NPCs))
	for k, n := range w.NPCs {
		out.NPCs[k] = cloneNPC(n)
	}

	out.VisitedLocations = append([]string(nil), w.VisitedLocations...)

	out.Quests = make(map[string]QuestState, len(w.Quests))
	for k, v := range w.Quests {
		out.Quests[k] = v
	}

	out.ConsumedEvents = append([]ConsumedEvent(nil), w.ConsumedEvents...)

	out.Flags = make(map[string]bool, len(w.Flags))
	for k, v := range w.Flags {
		out.Flags[k] = v
	}

	return &out
}

// cloneNPC deep-copies one NPC, including its map and slice fields. A shallow
// copy here would let a resurrected save mutate the committed snapshot's NPC.
func cloneNPC(n NPC) NPC {
	out := n
	out.Goals = append([]string(nil), n.Goals...)
	out.KnownFacts = append([]string(nil), n.KnownFacts...)

	if n.Schedule != nil {
		out.Schedule = make(map[string]string, len(n.Schedule))
		for k, v := range n.Schedule {
			out.Schedule[k] = v
		}
	}

	return out
}

// clonePendingState deep-copies the in-flight sub-state union.
func clonePendingState(p PendingState) PendingState {
	out := PendingState{}

	if p.Event != nil {
		e := *p.Event
		e.ChoiceIDs = append([]string(nil), p.Event.ChoiceIDs...)
		out.Event = &e
	}
	if p.Combat != nil {
		c := *p.Combat
		c.Enemies = make([]Combatant, len(p.Combat.Enemies))
		for i, e := range p.Combat.Enemies {
			cp := e
			cp.Statuses = append([]TimedEffect(nil), e.Statuses...)
			c.Enemies[i] = cp
		}
		c.AllyState.Statuses = append([]TimedEffect(nil), p.Combat.AllyState.Statuses...)
		out.Combat = &c
	}
	if p.Breakthrough != nil {
		b := *p.Breakthrough
		b.SpentMaterials = append([]ItemStack(nil), p.Breakthrough.SpentMaterials...)
		b.ChoiceIDs = append([]string(nil), p.Breakthrough.ChoiceIDs...)
		b.CompletedNodes = append([]string(nil), p.Breakthrough.CompletedNodes...)
		out.Breakthrough = &b
	}
	if p.MonthAction != nil {
		m := *p.MonthAction
		m.StartedMaterials = append([]ItemStack(nil), p.MonthAction.StartedMaterials...)
		out.MonthAction = &m
	}
	if p.Confirmation != nil {
		c := *p.Confirmation
		c.Preview = clonePreview(p.Confirmation.Preview)
		out.Confirmation = &c
	}
	if p.Creation != nil {
		c := *p.Creation
		c.FixedResults = make(map[string]int64, len(p.Creation.FixedResults))
		for k, v := range p.Creation.FixedResults {
			c.FixedResults[k] = v
		}
		out.Creation = &c
	}

	return out
}

// clonePreview deep-copies a confirmation preview.
func clonePreview(p ConfirmationPreview) ConfirmationPreview {
	out := p
	out.Materials = append([]ItemStack(nil), p.Materials...)
	if p.Resources != nil {
		out.Resources = make(map[Resource]int64, len(p.Resources))
		for k, v := range p.Resources {
			out.Resources[k] = v
		}
	}
	out.Risks = append([]string(nil), p.Risks...)
	return out
}

// cloneIdempotency deep-copies the deduplication table.
func cloneIdempotency(t IdempotencyTable) IdempotencyTable {
	out := IdempotencyTable{
		Order: append([]string(nil), t.Order...),
		Limit: t.Limit,
	}
	if t.Entries != nil {
		out.Entries = make(map[string]IdempotencyEntry, len(t.Entries))
		for k, v := range t.Entries {
			cp := v
			cp.Result = cloneResult(v.Result)
			out.Entries[k] = cp
		}
	}
	return out
}

// cloneResult deep-copies a stored command result.
func cloneResult(r CommandResult) CommandResult {
	out := r
	out.Delta = cloneDelta(r.Delta)
	out.Events = append([]ResultEvent(nil), r.Events...)
	return out
}

// cloneDelta deep-copies a delta ledger.
func cloneDelta(d DeltaLedger) DeltaLedger {
	return DeltaLedger{Entries: append([]DeltaEntry(nil), d.Entries...)}
}

// RequestFingerprint renders a stable fingerprint of a command's identity.
//
// It deliberately excludes ActionID: the fingerprint exists precisely to detect
// the *same* action id arriving with a *different* body, so folding the id in
// would make every conflict look like a match.
//
// It excludes SessionID too, because a resent command may legitimately come from
// a reconnected session while describing the same intent.
//
// Only fields that change what the command *does* are included. A payload field
// left at its zero value is skipped so that omitting it and setting it to zero
// are not treated as different commands.
func RequestFingerprint(c Command) string {
	var b []byte

	appendField := func(tag, v string) {
		if v == "" {
			return
		}
		b = append(b, tag...)
		b = append(b, '=')
		b = append(b, v...)
		b = append(b, '\n')
	}
	appendNum := func(tag string, v int64) {
		if v == 0 {
			return
		}
		b = append(b, tag...)
		b = append(b, '=')
		b = appendIntBytes(b, v)
		b = append(b, '\n')
	}

	appendField("kind", string(c.Kind))
	appendNum("revision", int64(c.ExpectedRevision))
	appendField("view_token", c.ViewToken)
	appendField("target", c.TargetID)
	appendField("target_instance", c.TargetInstanceID)
	appendField("confirmation", c.ConfirmationToken)

	// Payload.
	appendNum("qty", c.Payload.Quantity)
	appendField("item", c.Payload.ItemID)
	appendField("quest", c.Payload.QuestID)
	appendField("npc", c.Payload.NPCID)
	appendField("location", c.Payload.LocationID)
	appendField("skill", c.Payload.SkillID)
	appendField("choice", c.Payload.ChoiceID)
	appendField("query", string(c.Payload.QueryKind))
	appendField("text", c.Payload.Text)
	appendField("action_kind", string(c.Payload.ActionKind))

	// Character creation. The whole proposed selection is folded in, because
	// two creation commands with the same action id but different allocations
	// are different requests and must be reported as a conflict rather than
	// having one silently win.
	if cp := c.Payload.Creation; cp != nil {
		sel := cp.Selection
		appendField("cr.surname", sel.Surname)
		appendField("cr.given", sel.GivenName)
		appendField("cr.dao", sel.DaoName)
		appendField("cr.gender", sel.Gender)
		appendField("cr.appearance", sel.Appearance)
		appendNum("cr.age", int64(sel.AgeYears))
		appendField("cr.origin", string(sel.Origin))
		appendField("cr.path", string(sel.Path))
		appendField("cr.root", string(sel.SpiritRoot))
		appendField("cr.constitution", sel.Constitution)
		appendField("cr.talents", joinStrings(sel.TalentIDs))
		if sel.YaoIntent {
			appendNum("cr.yao_intent", 1)
		}
		appendNum("cr.strength", int64(sel.Strength))
		appendNum("cr.agility", int64(sel.Agility))
		appendNum("cr.constitution_attr", int64(sel.ConstitutionAttr))
		appendNum("cr.comprehension", int64(sel.Comprehension))
		appendNum("cr.aptitude", int64(sel.Aptitude))
		appendNum("cr.fortune", int64(sel.Fortune))
		appendField("cr.preset", cp.PresetID)
		if cp.AdvanceStep {
			appendNum("cr.advance", 1)
		}
		if cp.BackStep {
			appendNum("cr.back", 1)
		}
		if cp.Confirm {
			appendNum("cr.confirm", 1)
		}
		appendNum("cr.yao_verdict", int64(cp.YaoVerdictPermille))
		if cp.YaoVerdictGiven {
			appendNum("cr.yao_verdict_given", 1)
		}
	}

	return fnv1a64Hex(b)
}

// joinStrings renders a string slice with a separator that cannot appear in an
// id, so two different lists can never produce the same joined text.
func joinStrings(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	var b []byte
	for i, x := range xs {
		if i > 0 {
			b = append(b, 0x1f)
		}
		b = append(b, x...)
	}
	return string(b)
}

// LookupIdempotent finds a previously committed result for this action id.
//
// It returns three outcomes:
//   - ok=false: no record, the caller should execute the command.
//   - ok=true, conflict=false: a resend; return the stored result unchanged.
//   - ok=true, conflict=true: the same id arrived with a different body, which
//     the design says must be rejected rather than silently accepted.
func LookupIdempotent(t *IdempotencyTable, c Command) (result CommandResult, ok bool, conflict bool) {
	if t == nil || t.Entries == nil {
		return CommandResult{}, false, false
	}
	entry, found := t.Entries[c.ActionID]
	if !found {
		return CommandResult{}, false, false
	}
	if entry.RequestFingerprint != RequestFingerprint(c) {
		return CommandResult{}, true, true
	}
	return cloneResult(entry.Result), true, false
}

// RecordIdempotent stores a committed result, evicting the oldest entries beyond
// the limit.
//
// Only successful, state-changing commands are recorded. A rejected command is
// not remembered, because a player who fixes the problem and resubmits with the
// same id must be allowed through.
func RecordIdempotent(t *IdempotencyTable, c Command, r CommandResult) {
	if t == nil {
		return
	}
	if t.Entries == nil {
		t.Entries = map[string]IdempotencyEntry{}
	}
	if t.Limit <= 0 {
		t.Limit = DefaultIdempotencyLimit
	}

	// Re-recording an existing id must not duplicate its order entry, or the
	// eviction queue would grow without bound.
	if _, exists := t.Entries[c.ActionID]; !exists {
		t.Order = append(t.Order, c.ActionID)
	}

	t.Entries[c.ActionID] = IdempotencyEntry{
		ActionID:           c.ActionID,
		RequestFingerprint: RequestFingerprint(c),
		Result:             cloneResult(r),
	}

	for len(t.Order) > t.Limit {
		oldest := t.Order[0]
		t.Order = t.Order[1:]
		delete(t.Entries, oldest)
	}
}

// ResetIdempotency clears the table. A new branch starts with no memory of the
// old timeline's actions, so an action id reused after a reload cannot be
// mistaken for a duplicate and silently return the old branch's result.
func ResetIdempotency(t *IdempotencyTable) {
	if t == nil {
		return
	}
	t.Entries = map[string]IdempotencyEntry{}
	t.Order = nil
}
