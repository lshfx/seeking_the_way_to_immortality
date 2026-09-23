package engine

import "testing"

// Store and idempotency-ledger tests.

// TestMemoryStoreCommitPromotesWorkingState covers the happy path.
func TestMemoryStoreCommitPromotesWorkingState(t *testing.T) {
	state := readyState()
	store := NewMemoryStore(state)

	// The working copy must be independent of the caller's.
	loaded, ok := store.Load()
	if !ok {
		t.Fatal("a fresh store must load")
	}

	loaded.Counters.WorldMonth = 5
	if err := store.Commit(loaded); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if got := store.Committed().Counters.WorldMonth; got != 5 {
		t.Fatalf("committed world month = %d, want 5", got)
	}
}

// TestMemoryStoreReturnsIndependentCopies is the property that makes
// "abort leaves no trace" real: a caller mutating the loaded state must not
// silently change the committed snapshot.
func TestMemoryStoreReturnsIndependentCopies(t *testing.T) {
	state := readyState()
	state.Counters.WorldMonth = 3
	store := NewMemoryStore(state)

	loaded, _ := store.Load()
	loaded.Counters.WorldMonth = 99
	loaded.Player.AgeMonths = 9999
	loaded.Player.Resources[ResSpiritStones] = 0

	committed := store.Committed()
	if committed.Counters.WorldMonth != 3 {
		t.Fatalf("the committed snapshot followed the working copy: month = %d, want 3", committed.Counters.WorldMonth)
	}
	if committed.Player.AgeMonths == 9999 {
		t.Fatal("the committed snapshot followed the working copy's age")
	}
	if committed.Player.Resources[ResSpiritStones] == 0 {
		t.Fatal("the committed snapshot followed the working copy's resources")
	}
}

// TestMemoryStoreFailedCommitLeavesCommittedIntact models the crash-safety rule:
// a failed write must not partially apply.
func TestMemoryStoreFailedCommitLeavesCommittedIntact(t *testing.T) {
	state := readyState()
	state.Counters.WorldMonth = 7
	store := NewMemoryStore(state)

	good := CloneGameState(state)
	good.Counters.WorldMonth = 8
	if err := store.Commit(good); err != nil {
		t.Fatalf("setup commit: %v", err)
	}

	// Now fail the next commit and try to write something corrupt.
	store.FailNextCommit()

	bad := CloneGameState(state)
	bad.Counters.WorldMonth = 999
	if err := store.Commit(bad); err == nil {
		t.Fatal("the induced failure did not surface as an error")
	}

	if got := store.Committed().Counters.WorldMonth; got != 8 {
		t.Fatalf("committed world month = %d after a failed commit, want 8 (unchanged)", got)
	}
}

// TestMemoryStoreCommitRejectsNil keeps a nil state from being treated as a
// successful write.
func TestMemoryStoreCommitRejectsNil(t *testing.T) {
	store := NewMemoryStore(readyState())
	if err := store.Commit(nil); err == nil {
		t.Fatal("committing nil must fail rather than report success")
	}
}

// TestMemoryStoreLoadOnNilStore is the defensive path.
func TestMemoryStoreLoadOnNilStore(t *testing.T) {
	var store *MemoryStore
	if _, ok := store.Load(); ok {
		t.Fatal("a nil store must not report a successful load")
	}
}

// TestRequestFingerprintIsStableForIdenticalCommands is the precondition for
// deduplication: the same request must always fingerprint the same way.
func TestRequestFingerprintIsStableForIdenticalCommands(t *testing.T) {
	c := Command{
		ActionID:         "a1",
		SessionID:        "s1",
		ExpectedRevision: 7,
		ViewToken:        "vt:1:7",
		Kind:             KindCultivate,
		TargetID:         "target",
		Payload:          Payload{Quantity: 3, ActionKind: ActionKind("steady")},
	}

	first := RequestFingerprint(c)

	for i := 0; i < 50; i++ {
		if got := RequestFingerprint(c); got != first {
			t.Fatalf("fingerprint changed between calls on iteration %d", i)
		}
	}
}

// TestRequestFingerprintIgnoresActionID is essential: the fingerprint exists to
// detect the same id carrying a different body, so folding the id in would make
// every conflict look like a match.
func TestRequestFingerprintIgnoresActionID(t *testing.T) {
	a := Command{ActionID: "first", Kind: KindWait, ExpectedRevision: 1, ViewToken: "t"}
	b := Command{ActionID: "second", Kind: KindWait, ExpectedRevision: 1, ViewToken: "t"}

	if RequestFingerprint(a) != RequestFingerprint(b) {
		t.Fatal("the fingerprint depends on the action id, which would defeat conflict detection")
	}
}

// TestRequestFingerprintIgnoresSessionID allows a resent command to arrive from
// a reconnected session describing the same intent.
func TestRequestFingerprintIgnoresSessionID(t *testing.T) {
	a := Command{ActionID: "a", SessionID: "s1", Kind: KindWait, ExpectedRevision: 1, ViewToken: "t"}
	b := Command{ActionID: "a", SessionID: "s2", Kind: KindWait, ExpectedRevision: 1, ViewToken: "t"}

	if RequestFingerprint(a) != RequestFingerprint(b) {
		t.Fatal("the fingerprint depends on the session id, which would reject a legitimate resend after reconnect")
	}
}

// TestRequestFingerprintDetectsEveryMeaningfulChange walks each field that
// changes what a command does, and asserts the fingerprint notices.
func TestRequestFingerprintDetectsEveryMeaningfulChange(t *testing.T) {
	base := Command{
		ActionID:          "a1",
		ExpectedRevision:  5,
		ViewToken:         "vt:1:5",
		Kind:              KindTrade,
		TargetID:          "shop",
		TargetInstanceID:  "inst",
		ConfirmationToken: "ticket",
		Payload: Payload{
			Quantity:   2,
			ItemID:     "healing_pill",
			QuestID:    "q1",
			NPCID:      "npc1",
			LocationID: "market",
			SkillID:    "fire",
			ChoiceID:   "A",
			QueryKind:  QueryStatus,
			Text:       "name",
			ActionKind: ActionKind("steady"),
		},
	}
	basePrint := RequestFingerprint(base)

	mutations := []struct {
		name   string
		mutate func(*Command)
	}{
		{"kind", func(c *Command) { c.Kind = KindWait }},
		{"expected_revision", func(c *Command) { c.ExpectedRevision = 6 }},
		{"view_token", func(c *Command) { c.ViewToken = "vt:2:6" }},
		{"target_id", func(c *Command) { c.TargetID = "other" }},
		{"target_instance_id", func(c *Command) { c.TargetInstanceID = "other" }},
		{"confirmation_token", func(c *Command) { c.ConfirmationToken = "other" }},
		{"payload.quantity", func(c *Command) { c.Payload.Quantity = 3 }},
		{"payload.item_id", func(c *Command) { c.Payload.ItemID = "other" }},
		{"payload.quest_id", func(c *Command) { c.Payload.QuestID = "other" }},
		{"payload.npc_id", func(c *Command) { c.Payload.NPCID = "other" }},
		{"payload.location_id", func(c *Command) { c.Payload.LocationID = "other" }},
		{"payload.skill_id", func(c *Command) { c.Payload.SkillID = "other" }},
		{"payload.choice_id", func(c *Command) { c.Payload.ChoiceID = "B" }},
		{"payload.query_kind", func(c *Command) { c.Payload.QueryKind = QueryHelp }},
		{"payload.text", func(c *Command) { c.Payload.Text = "different" }},
		{"payload.action_kind", func(c *Command) { c.Payload.ActionKind = ActionKind("intense") }},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			c := base
			m.mutate(&c)

			if got := RequestFingerprint(c); got == basePrint {
				t.Fatalf("changing %s did not change the fingerprint: a different request would be "+
					"mistaken for a resend", m.name)
			}
		})
	}
}

// TestRequestFingerprintTreatsZeroAndAbsentAlike means omitting a payload field
// and setting it to its zero value are the same request, which is what a caller
// would expect.
func TestRequestFingerprintTreatsZeroAndAbsentAlike(t *testing.T) {
	omitted := Command{ActionID: "a", Kind: KindWait, ExpectedRevision: 1, ViewToken: "t"}
	explicitZero := Command{
		ActionID: "a", Kind: KindWait, ExpectedRevision: 1, ViewToken: "t",
		Payload: Payload{Quantity: 0, ItemID: "", ChoiceID: ""},
	}

	if RequestFingerprint(omitted) != RequestFingerprint(explicitZero) {
		t.Fatal("omitting a payload field differs from setting it to its zero value")
	}
}

// TestLookupIdempotentOutcomes covers the three outcomes of the lookup.
func TestLookupIdempotentOutcomes(t *testing.T) {
	table := &IdempotencyTable{
		Entries: map[string]IdempotencyEntry{},
		Limit:   DefaultIdempotencyLimit,
	}

	c := Command{ActionID: "a1", Kind: KindWait, ExpectedRevision: 1, ViewToken: "t"}

	// 1. Absent.
	if _, ok, conflict := LookupIdempotent(table, c); ok || conflict {
		t.Fatal("an empty table reported a hit")
	}

	// Record it.
	RecordIdempotent(table, c, CommandResult{ActionID: "a1", OK: true, RevisionAfter: 2})

	// 2. Present and matching.
	got, ok, conflict := LookupIdempotent(table, c)
	if !ok || conflict {
		t.Fatalf("a recorded command was not found cleanly (ok=%v conflict=%v)", ok, conflict)
	}
	if got.ActionID != "a1" || !got.OK {
		t.Fatalf("stored result = %+v, want the recorded result", got)
	}

	// 3. Present but different body.
	different := c
	different.Kind = KindCultivate
	if _, ok, conflict := LookupIdempotent(table, different); !ok || !conflict {
		t.Fatalf("a conflicting body was not reported (ok=%v conflict=%v)", ok, conflict)
	}
}

// TestLookupIdempotentOnNilTable is the defensive path.
func TestLookupIdempotentOnNilTable(t *testing.T) {
	c := Command{ActionID: "a"}
	if _, ok, conflict := LookupIdempotent(nil, c); ok || conflict {
		t.Fatal("a nil table reported a hit")
	}
}

// TestStoredResultIsACopyNotAnAlias prevents a caller from mutating the stored
// result, which would corrupt every future resend's answer.
func TestStoredResultIsACopyNotAnAlias(t *testing.T) {
	table := &IdempotencyTable{Entries: map[string]IdempotencyEntry{}, Limit: 8}
	c := Command{ActionID: "a1", Kind: KindWait}

	RecordIdempotent(table, c, CommandResult{
		ActionID: "a1",
		OK:       true,
		Delta:    DeltaLedger{Entries: []DeltaEntry{{Field: "xp", Before: 0, After: 10}}},
		Events:   []ResultEvent{{Kind: "xp_changed", ID: "xp"}},
	})

	first, _, _ := LookupIdempotent(table, c)
	first.Delta.Entries[0].After = 9999
	first.Events[0].Kind = "tampered"

	second, _, _ := LookupIdempotent(table, c)
	if second.Delta.Entries[0].After != 10 {
		t.Fatal("the stored delta was mutated through the returned copy")
	}
	if second.Events[0].Kind != "xp_changed" {
		t.Fatal("the stored events were mutated through the returned copy")
	}
}

// TestRecordIdempotentEvictsOldest is the bounded-ledger rule.
func TestRecordIdempotentEvictsOldest(t *testing.T) {
	const limit = 3
	table := &IdempotencyTable{Entries: map[string]IdempotencyEntry{}, Limit: limit}

	for i := 0; i < 5; i++ {
		c := Command{ActionID: "act-" + intToStr(i), Kind: KindWait}
		RecordIdempotent(table, c, CommandResult{ActionID: c.ActionID, OK: true})
	}

	if len(table.Entries) != limit {
		t.Fatalf("table holds %d entries, want %d", len(table.Entries), limit)
	}
	if len(table.Order) != limit {
		t.Fatalf("order holds %d entries, want %d", len(table.Order), limit)
	}

	// The oldest must be gone and the newest present.
	if _, found := table.Entries["act-0"]; found {
		t.Fatal("the oldest entry was not evicted")
	}
	if _, found := table.Entries["act-4"]; !found {
		t.Fatal("the newest entry is missing")
	}
}

// TestRecordIdempotentDoesNotDuplicateOrderEntries is the leak this guards
// against: re-recording the same id would otherwise grow the eviction queue
// without bound.
func TestRecordIdempotentDoesNotDuplicateOrderEntries(t *testing.T) {
	table := &IdempotencyTable{Entries: map[string]IdempotencyEntry{}, Limit: 8}
	c := Command{ActionID: "same", Kind: KindWait}

	for i := 0; i < 10; i++ {
		RecordIdempotent(table, c, CommandResult{ActionID: "same", OK: true})
	}

	if len(table.Order) != 1 {
		t.Fatalf("order holds %d entries after 10 recordings of one id, want 1", len(table.Order))
	}
}

// TestRecordIdempotentAppliesDefaultLimit covers a zero-valued table.
func TestRecordIdempotentAppliesDefaultLimit(t *testing.T) {
	table := &IdempotencyTable{}
	c := Command{ActionID: "a", Kind: KindWait}
	RecordIdempotent(table, c, CommandResult{ActionID: "a", OK: true})

	if table.Limit != DefaultIdempotencyLimit {
		t.Fatalf("limit = %d, want %d", table.Limit, DefaultIdempotencyLimit)
	}
	if table.Entries == nil {
		t.Fatal("entries map was not initialised")
	}
}

// TestResetIdempotencyClearsTheLedger is the branch rule: a new branch must not
// remember the old timeline's actions, or a reused id would return the wrong
// branch's result.
func TestResetIdempotencyClearsTheLedger(t *testing.T) {
	table := &IdempotencyTable{Entries: map[string]IdempotencyEntry{}, Limit: 8}
	c := Command{ActionID: "a1", Kind: KindWait}
	RecordIdempotent(table, c, CommandResult{ActionID: "a1", OK: true})

	ResetIdempotency(table)

	if len(table.Entries) != 0 {
		t.Fatalf("entries = %d after reset, want 0", len(table.Entries))
	}
	if len(table.Order) != 0 {
		t.Fatalf("order = %d after reset, want 0", len(table.Order))
	}

	// The id must now be reusable.
	if _, ok, _ := LookupIdempotent(table, c); ok {
		t.Fatal("a reset ledger still remembers the old branch's action")
	}
}

// TestCloneGameStateHandlesNil covers the defensive path.
func TestCloneGameStateHandlesNil(t *testing.T) {
	if got := CloneGameState(nil); got != nil {
		t.Fatal("cloning nil must return nil")
	}
}

// TestCloneGameStateCopiesPendingSubStates is the part a shallow copy would get
// wrong: the pending union holds pointers, and sharing them would let a rejected
// command mutate the live sub-state.
func TestCloneGameStateCopiesPendingSubStates(t *testing.T) {
	original := readyState()
	original.Pending.Event = &PendingEvent{
		EventID:    "EVT-001",
		InstanceID: "inst",
		ChoiceIDs:  []string{"A", "B"},
	}
	original.Pending.Combat = &PendingCombat{
		CombatID: "c1",
		Enemies:  []Combatant{{ID: "enemy", HP: Vitals{Current: 10, Max: 10}, Statuses: []TimedEffect{{ID: "burn", MonthsRemaining: 2}}}},
	}
	original.Pending.Breakthrough = &PendingBreakthrough{
		BreakthroughID: "b1",
		ChoiceIDs:      []string{"endure"},
		SpentMaterials: []ItemStack{{ItemID: "pill", Quantity: 1}},
	}
	original.Pending.MonthAction = &ActiveMonthAction{
		ActionID:         "m1",
		StartedMaterials: []ItemStack{{ItemID: "herb", Quantity: 2}},
	}
	original.Pending.Confirmation = &PendingConfirmation{
		Token: "t1",
		Preview: ConfirmationPreview{
			Materials: []ItemStack{{ItemID: "pill", Quantity: 1}},
			Resources: map[Resource]int64{ResSpiritStones: 10},
			Risks:     []string{"injury"},
		},
	}
	original.Pending.Creation = &CreationDraft{
		FixedResults: map[string]int64{"root": 3},
	}

	clone := CloneGameState(original)

	// Mutate every nested slice and map on the clone.
	clone.Pending.Event.ChoiceIDs[0] = "MUTATED"
	clone.Pending.Combat.Enemies[0].Statuses[0].ID = "MUTATED"
	clone.Pending.Combat.Enemies[0].HP.Current = 999
	clone.Pending.Breakthrough.ChoiceIDs[0] = "MUTATED"
	clone.Pending.Breakthrough.SpentMaterials[0].Quantity = 999
	clone.Pending.MonthAction.StartedMaterials[0].Quantity = 999
	clone.Pending.Confirmation.Preview.Materials[0].Quantity = 999
	clone.Pending.Confirmation.Preview.Resources[ResSpiritStones] = 999
	clone.Pending.Confirmation.Preview.Risks[0] = "MUTATED"
	clone.Pending.Creation.FixedResults["root"] = 999

	// The original must be untouched.
	if original.Pending.Event.ChoiceIDs[0] != "A" {
		t.Fatal("the pending event's candidate slice was shared")
	}
	if original.Pending.Combat.Enemies[0].Statuses[0].ID != "burn" {
		t.Fatal("the combatant's status slice was shared")
	}
	if original.Pending.Combat.Enemies[0].HP.Current != 10 {
		t.Fatal("the combatant was shared")
	}
	if original.Pending.Breakthrough.ChoiceIDs[0] != "endure" {
		t.Fatal("the breakthrough's candidate slice was shared")
	}
	if original.Pending.Breakthrough.SpentMaterials[0].Quantity != 1 {
		t.Fatal("the breakthrough's spent materials were shared")
	}
	if original.Pending.MonthAction.StartedMaterials[0].Quantity != 2 {
		t.Fatal("the month action's started materials were shared")
	}
	if original.Pending.Confirmation.Preview.Materials[0].Quantity != 1 {
		t.Fatal("the confirmation preview's materials were shared")
	}
	if original.Pending.Confirmation.Preview.Resources[ResSpiritStones] != 10 {
		t.Fatal("the confirmation preview's resource map was shared")
	}
	if original.Pending.Confirmation.Preview.Risks[0] != "injury" {
		t.Fatal("the confirmation preview's risks were shared")
	}
	if original.Pending.Creation.FixedResults["root"] != 3 {
		t.Fatal("the creation draft's fixed results were shared")
	}
}

// TestCloneGameStateCopiesWorldNPCs covers the NPC map, whose members hold maps
// and slices of their own.
func TestCloneGameStateCopiesWorldNPCs(t *testing.T) {
	original := readyState()
	original.World.NPCs["npc-1"] = NPC{
		ID:         "npc-1",
		IsAlive:    true,
		Schedule:   map[string]string{"morning": "market"},
		Goals:      []string{"goal-1"},
		KnownFacts: []string{"fact-1"},
	}

	clone := CloneGameState(original)

	clone.World.NPCs["npc-1"].Schedule["morning"] = "MUTATED"
	clone.World.NPCs["npc-1"].Goals[0] = "MUTATED"
	clone.World.NPCs["npc-1"].KnownFacts[0] = "MUTATED"

	npc := original.World.NPCs["npc-1"]
	if npc.Schedule["morning"] != "market" {
		t.Fatal("the NPC's schedule map was shared")
	}
	if npc.Goals[0] != "goal-1" {
		t.Fatal("the NPC's goals slice was shared")
	}
	if npc.KnownFacts[0] != "fact-1" {
		t.Fatal("the NPC's known facts slice was shared")
	}
}

// TestCloneGameStateCopiesLifespanBonuses guards the ledger that decides death.
func TestCloneGameStateCopiesLifespanBonuses(t *testing.T) {
	original := readyState()
	original.Player.Lifespan.Bonuses = []LifespanBonus{{ID: "b1", Years: 10}}

	clone := CloneGameState(original)
	clone.Player.Lifespan.Bonuses[0].Years = 999

	if original.Player.Lifespan.Bonuses[0].Years != 10 {
		t.Fatal("the lifespan bonus slice was shared")
	}
	// And the derived ceiling must not have moved.
	if got := LifespanMonths(original.Player.Lifespan); got != int64(90*12) {
		t.Fatalf("the original ceiling moved to %d, want %d", got, 90*12)
	}
}

// TestErrStoreCommitRendersAMessage covers the error type's presentation.
func TestErrStoreCommitRendersAMessage(t *testing.T) {
	if got := (ErrStoreCommit{}).Error(); got == "" {
		t.Fatal("ErrStoreCommit with no reason rendered an empty message")
	}
	if got := (ErrStoreCommit{Reason: "disk full"}).Error(); got == "" {
		t.Fatal("ErrStoreCommit with a reason rendered an empty message")
	}
}
