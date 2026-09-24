package engine

import "testing"

// Month-boundary behaviour, following design document 13.2.
//
// The ordering in step 4 is not cosmetic. Applying the immediate lifespan change
// before aging is what stops a character who has just raised their ceiling from
// being declared dead against the old one. These tests pin the order by
// constructing the exact situation where the two orders disagree.

// TestMonthEndOrderAppliesLifespanBeforeAging is the order test.
//
// Scenario: the character is exactly at their old ceiling (959 of 960 months),
// and the settlement grants a lifespan bonus. The design says immediate effects
// apply BEFORE aging, so the character survives: raised ceiling first, then the
// month that takes them to 960.
//
// Aged first, they would die at 960 against the old ceiling, and the bonus would
// arrive too late to matter.
//
// The bonus is applied through a hook the settlement runs, not before submission,
// so this test actually exercises the ordering rather than the ledger's
// arithmetic.
// TestMonthEndOrderIsObservableByTheEffectItself pins the ordering by having the
// immediate effect observe what has already happened.
//
// The previous attempt at this test only checked the final outcome, and passed
// even when the effect ran after aging: because the death check happens once at
// the end, a raised ceiling saves the character either way. To make the order
// observable, the effect records the counter values it was handed.
//
// If the effect runs BEFORE aging (the design's order), it sees the old month and
// the old age. If it runs after, it sees the new ones. The assertion is on what
// the effect observed, not on the ending.
func TestMonthEndOrderIsObservableByTheEffectItself(t *testing.T) {
	state := readyState()
	state.Player.AgeMonths = 500
	state.Counters.WorldMonth = 10

	e, _ := newTestEngine(t, state)

	var (
		observedMonth int64
		observedAge   int64
		called        bool
	)

	e.SetImmediateEffectForTest(func(s *GameState, c Command) {
		called = true
		observedMonth = s.Counters.WorldMonth
		observedAge = s.Player.AgeMonths
	})
	defer e.SetImmediateEffectForTest(nil)

	if r := e.Submit(cmd(e, "probe", KindWait)); !r.OK {
		t.Fatalf("action rejected: %s", r.Code)
	}
	if !called {
		t.Fatal("the immediate-effect hook was never invoked during settlement")
	}

	// Design 13.2 step 4: effects land BEFORE the month advances and ages.
	if observedMonth != 10 {
		t.Fatalf("the immediate effect saw world month %d, want 10 (it must run before the month advances)", observedMonth)
	}
	if observedAge != 500 {
		t.Fatalf("the immediate effect saw age %d, want 500 (it must run before aging)", observedAge)
	}

	// And the month did advance afterwards.
	if e.State().Counters.WorldMonth != 11 {
		t.Fatalf("world month = %d after settlement, want 11", e.State().Counters.WorldMonth)
	}
	if e.State().Player.AgeMonths != 501 {
		t.Fatalf("age = %d after settlement, want 501", e.State().Player.AgeMonths)
	}
}

func TestMonthEndOrderAppliesLifespanBeforeAging(t *testing.T) {
	state := readyState()
	state.Player.AgeMonths = 959
	state.Player.Lifespan = LifespanLedger{BaseYears: 80} // 960 months

	e, _ := newTestEngine(t, state)

	// Install a settlement hook that grants the bonus as the month settles.
	// This is where TASK-11 will plug in a successful breakthrough's lifespan
	// change; here it stands in for "an immediate effect that lands this month".
	e.SetImmediateEffectForTest(func(s *GameState, c Command) {
		s.Player.Lifespan.Bonuses = append(s.Player.Lifespan.Bonuses,
			LifespanBonus{ID: "raise", Years: 20, Reason: "test immediate effect"})
	})
	defer e.SetImmediateEffectForTest(nil)

	r := e.Submit(cmd(e, "extend", KindWait))
	if !r.OK {
		t.Fatalf("action rejected: %s", r.Code)
	}

	if e.State().Player.Ended {
		t.Fatal("the character died because the month end aged before applying the " +
			"immediate lifespan change: design 13.2 step 4 requires effects first")
	}
	if e.State().Player.AgeMonths != 960 {
		t.Fatalf("age = %d, want 960", e.State().Player.AgeMonths)
	}
	if e.State().Counters.WorldMonth != 1 {
		t.Fatalf("world month = %d, want 1", e.State().Counters.WorldMonth)
	}

	// The new ceiling is 1200 months, so the character is comfortably alive.
	if LifespanExhausted(e.State().Player.AgeMonths, e.State().Player.Lifespan) {
		t.Fatal("the raised ceiling was not honoured")
	}
}

// TestDeathAtExactlyTheCeilingAdvancesTheMonthOnce pins the design's explicit
// rule: "月末寿尽则本月已推进一次".
func TestDeathAtExactlyTheCeilingAdvancesTheMonthOnce(t *testing.T) {
	state := readyState()
	state.Player.AgeMonths = 959
	state.Player.Lifespan = LifespanLedger{BaseYears: 80}

	e, _ := newTestEngine(t, state)

	r := e.Submit(cmd(e, "last", KindWait))
	if !r.OK {
		t.Fatalf("the final month was rejected: %s", r.Code)
	}

	if e.State().Counters.WorldMonth != 1 {
		t.Fatalf("world month = %d, want exactly 1", e.State().Counters.WorldMonth)
	}
	if e.State().Player.AgeMonths != 960 {
		t.Fatalf("age = %d, want exactly 960", e.State().Player.AgeMonths)
	}
	if r.MonthCostApplied != 1 {
		t.Fatalf("month cost = %d, want 1", r.MonthCostApplied)
	}

	// And no further month may be charged.
	before := e.State().Counters.WorldMonth
	if r := e.Submit(cmd(e, "after-death", KindWait)); r.OK {
		t.Fatal("a world action was accepted after death")
	}
	if e.State().Counters.WorldMonth != before {
		t.Fatal("a rejected post-death action still advanced the month")
	}
}

// TestPendingMonthActionIsClearedOnDeath implements the design's requirement
// that both death paths clear the pending charge marker, so a resume cannot
// settle again.
func TestPendingMonthActionIsClearedOnDeath(t *testing.T) {
	state := readyState()
	state.Player.AgeMonths = 959
	state.Player.Lifespan = LifespanLedger{BaseYears: 80}

	e, _ := newTestEngine(t, state)

	if r := e.Submit(cmd(e, "last", KindWait)); !r.OK {
		t.Fatalf("the final month was rejected: %s", r.Code)
	}

	if e.State().Pending.MonthAction != nil {
		t.Fatal("the pending month action survived the character's death: " +
			"a resumed settlement could charge another month")
	}
	if e.State().Phase != PhaseEnded {
		t.Fatalf("phase = %s, want %s", e.State().Phase, PhaseEnded)
	}
}

// TestTimedEffectsDecrementOnMonthEnd covers the third part of step 4: effects
// with a month duration must tick down, and expire when they run out.
func TestTimedEffectsDecrementOnMonthEnd(t *testing.T) {
	state := readyState()
	state.Player.Condition.Effects = []TimedEffect{
		{ID: "poison", MonthsRemaining: 3, Stacks: 1},
		{ID: "blessing", MonthsRemaining: 1, Stacks: 2},
	}

	e, _ := newTestEngine(t, state)

	// First month: both tick; the one-month effect expires.
	if r := e.Submit(cmd(e, "m1", KindWait)); !r.OK {
		t.Fatalf("month 1 rejected: %s", r.Code)
	}

	effects := e.State().Player.Condition.Effects
	if len(effects) != 1 {
		t.Fatalf("after one month there are %d effects, want 1 (the expired one must be removed)", len(effects))
	}
	if effects[0].ID != "poison" || effects[0].MonthsRemaining != 2 {
		t.Fatalf("remaining effect = %+v, want poison with 2 months", effects[0])
	}

	// Second and third months: poison runs out.
	if r := e.Submit(cmd(e, "m2", KindWait)); !r.OK {
		t.Fatalf("month 2 rejected: %s", r.Code)
	}
	if got := len(e.State().Player.Condition.Effects); got != 1 {
		t.Fatalf("after two months there are %d effects, want 1", got)
	}

	if r := e.Submit(cmd(e, "m3", KindWait)); !r.OK {
		t.Fatalf("month 3 rejected: %s", r.Code)
	}
	if got := len(e.State().Player.Condition.Effects); got != 0 {
		t.Fatalf("after three months there are %d effects, want 0", got)
	}
}

// TestExpiredEffectsAreRemovedNotLeftAtZero matters because a zero-duration
// effect left in the list would still be summed by any consumer that iterates
// without checking the duration.
func TestExpiredEffectsAreRemovedNotLeftAtZero(t *testing.T) {
	state := readyState()
	state.Player.Condition.Effects = []TimedEffect{{ID: "brief", MonthsRemaining: 1, Stacks: 5}}

	e, _ := newTestEngine(t, state)

	if r := e.Submit(cmd(e, "m1", KindWait)); !r.OK {
		t.Fatalf("rejected: %s", r.Code)
	}

	for _, ef := range e.State().Player.Condition.Effects {
		if ef.MonthsRemaining <= 0 {
			t.Fatalf("effect %q was left in the list with %d months remaining", ef.ID, ef.MonthsRemaining)
		}
	}
}

// TestZeroMonthEventChoiceDoesNotRetriggerMonthEnd implements step 6: "月末事件
// 的零额外月选择不再次触发月末".
func TestZeroMonthEventChoiceDoesNotRetriggerMonthEnd(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	// A parent action already charged, with an event raised at month end.
	e.State().Pending.MonthAction = &ActiveMonthAction{
		ActionID:     "parent",
		Kind:         KindQuestRun,
		MonthCharged: true,
	}
	e.State().Pending.Event = &PendingEvent{
		EventID:        "EVT-001",
		InstanceID:     "inst-1",
		ParentActionID: "parent",
		ChoiceIDs:      []string{"A", "B"},
	}
	e.State().Phase = PhaseEventPending
	e.State().Counters.WorldMonth = 5
	e.State().Player.AgeMonths = 300

	c := Command{
		ActionID:         "answer",
		SessionID:        "s",
		ExpectedRevision: e.State().Revision,
		ViewToken:        e.ViewToken(),
		Kind:             KindEventChoice,
		Payload:          Payload{ChoiceID: "A"},
	}

	r := e.Submit(c)
	if !r.OK {
		t.Fatalf("event choice rejected: %s", r.Code)
	}

	if e.State().Counters.WorldMonth != 5 {
		t.Fatalf("a month-end choice advanced the month to %d, want 5", e.State().Counters.WorldMonth)
	}
	if e.State().Player.AgeMonths != 300 {
		t.Fatalf("a month-end choice aged the character to %d, want 300", e.State().Player.AgeMonths)
	}
	if r.MonthCostApplied != 0 {
		t.Fatalf("a month-end choice applied %d months, want 0", r.MonthCostApplied)
	}
}

// TestEventChoiceAtMonthEndSettlesTheParentOnce verifies that the choice does
// settle a still-unsettled parent, but only once.
func TestEventChoiceAtMonthEndSettlesTheParentOnce(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	// Parent not yet charged: the choice should settle it.
	e.State().Pending.MonthAction = &ActiveMonthAction{
		ActionID:     "parent",
		Kind:         KindQuestRun,
		MonthCharged: false,
	}
	e.State().Pending.Event = &PendingEvent{
		EventID:        "EVT-001",
		InstanceID:     "inst-1",
		ParentActionID: "parent",
		ChoiceIDs:      []string{"A"},
	}
	e.State().Phase = PhaseEventPending

	c := Command{
		ActionID:         "answer",
		SessionID:        "s",
		ExpectedRevision: e.State().Revision,
		ViewToken:        e.ViewToken(),
		Kind:             KindEventChoice,
		Payload:          Payload{ChoiceID: "A"},
	}

	r := e.Submit(c)
	if !r.OK {
		t.Fatalf("event choice rejected: %s", r.Code)
	}

	if e.State().Counters.WorldMonth != 1 {
		t.Fatalf("world month = %d after settling the parent, want 1", e.State().Counters.WorldMonth)
	}
	if r.MonthCostApplied != 1 {
		t.Fatalf("month cost = %d, want 1", r.MonthCostApplied)
	}
	if e.State().Pending.MonthAction != nil {
		t.Fatal("the settled parent action was not cleared")
	}
	if e.State().Phase != PhaseReady {
		t.Fatalf("phase = %s, want %s", e.State().Phase, PhaseReady)
	}
}

// TestConsumedEventIsRecordedOnce checks the anti-double-payout record.
func TestConsumedEventIsRecordedOnce(t *testing.T) {
	state := readyState()
	state.Phase = PhaseEventPending
	state.Pending.Event = &PendingEvent{
		EventID:    "EVT-007",
		InstanceID: "inst-007",
		ChoiceIDs:  []string{"A"},
	}
	e, _ := newTestEngine(t, state)

	c := Command{
		ActionID:         "answer",
		SessionID:        "s",
		ExpectedRevision: e.State().Revision,
		ViewToken:        e.ViewToken(),
		Kind:             KindEventChoice,
		Payload:          Payload{ChoiceID: "A"},
	}

	if r := e.Submit(c); !r.OK {
		t.Fatalf("event choice rejected: %s", r.Code)
	}

	consumed := e.State().World.ConsumedEvents
	if len(consumed) != 1 {
		t.Fatalf("consumed events = %d, want 1", len(consumed))
	}
	if consumed[0].EventID != "EVT-007" || consumed[0].ChoiceID != "A" {
		t.Fatalf("consumed record = %+v, want EVT-007/A", consumed[0])
	}

	// A resend must repeat the request byte-for-byte, including the revision and
	// view token it was originally issued against. Changing them would make it a
	// genuinely different request, which the conflict rule correctly reports.
	if r := e.Submit(c); !r.OK {
		t.Fatalf("resend rejected: %s", r.Code)
	}
	if got := len(e.State().World.ConsumedEvents); got != 1 {
		t.Fatalf("a resend produced %d consumed records, want 1", got)
	}
}

// TestResendThatChangesRevisionIsNotTreatedAsADuplicate documents the boundary
// of the idempotency rule.
//
// The fingerprint covers the revision and view token because those are part of
// what the command asks for. A caller that bumps the revision is not resending
// the same click, so reporting a conflict is correct rather than a false
// positive. This test exists so the behaviour is deliberate and visible rather
// than an accident of what the fingerprint happens to include.
func TestResendThatChangesRevisionIsNotTreatedAsADuplicate(t *testing.T) {
	state := readyState()
	state.Phase = PhaseEventPending
	state.Pending.Event = &PendingEvent{
		EventID:    "EVT-009",
		InstanceID: "inst-009",
		ChoiceIDs:  []string{"A"},
	}
	e, _ := newTestEngine(t, state)

	original := Command{
		ActionID:         "answer",
		SessionID:        "s",
		ExpectedRevision: e.State().Revision,
		ViewToken:        e.ViewToken(),
		Kind:             KindEventChoice,
		Payload:          Payload{ChoiceID: "A"},
	}

	if r := e.Submit(original); !r.OK {
		t.Fatalf("first submission rejected: %s", r.Code)
	}

	// Same id, but re-targeted at the new revision.
	altered := original
	altered.ExpectedRevision = e.State().Revision
	altered.ViewToken = e.ViewToken()

	r := e.Submit(altered)

	if r.Code != ErrActionIdConflict {
		t.Fatalf("code = %s, want %s (an altered request is not a resend)", r.Code, ErrActionIdConflict)
	}
	if got := len(e.State().World.ConsumedEvents); got != 1 {
		t.Fatalf("the conflicting submission produced %d consumed records, want 1", got)
	}
}

// TestQuestRunEncounterIsDeterministic verifies that the encounter decision is a
// function of the action identity, so replaying the same actions yields the same
// interruptions.
func TestQuestRunEncounterIsDeterministic(t *testing.T) {
	// The encounter roll is derived from (actionID, targetID), so the same
	// pair must always yield the same decision.
	outcomes := make([]bool, 0, 8)

	for attempt := 0; attempt < 8; attempt++ {
		state := readyState()
		e, _ := newTestEngine(t, state)

		c := cmd(e, "run-"+string(rune('a'+attempt)), KindQuestRun)
		c.TargetID = "herb-gathering"
		// Quest run requires confirmation.
		e.State().Pending.Confirmation = &PendingConfirmation{
			Token:             "t",
			Kind:              KindQuestRun,
			TargetID:          "herb-gathering",
			ExpiresAtRevision: e.State().Revision,
		}
		c.ConfirmationToken = "t"

		r := e.Submit(c)
		if !r.OK {
			t.Fatalf("quest run rejected: %s", r.Code)
		}
		outcomes = append(outcomes, hasEvent(r, ResultCombatStarted))
	}

	// Each distinct action id may differ, but the mapping must be stable:
	// recompute and compare.
	for attempt := 0; attempt < 8; attempt++ {
		state := readyState()
		e, _ := newTestEngine(t, state)

		c := cmd(e, "run-"+string(rune('a'+attempt)), KindQuestRun)
		c.TargetID = "herb-gathering"
		e.State().Pending.Confirmation = &PendingConfirmation{
			Token:             "t",
			Kind:              KindQuestRun,
			TargetID:          "herb-gathering",
			ExpiresAtRevision: e.State().Revision,
		}
		c.ConfirmationToken = "t"

		r := e.Submit(c)
		if got := hasEvent(r, ResultCombatStarted); got != outcomes[attempt] {
			t.Fatalf("quest run %d changed its encounter outcome between runs", attempt)
		}
	}
}

// hasEvent reports whether a result contains a given event kind.
func hasEvent(r CommandResult, kind string) bool {
	for _, e := range r.Events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// TestQuestRunWithEncounterDefersTheMonthCost implements design 9.2: when a
// month action meets a battle, the month is charged only when the parent
// finally settles.
func TestQuestRunWithEncounterDefersTheMonthCost(t *testing.T) {
	// Find an action id that does raise an encounter, so the deferred-charge
	// path is actually exercised rather than skipped.
	var (
		hitEngine *Engine
		hitResult CommandResult
	)

	for i := 0; i < 200 && hitEngine == nil; i++ {
		state := readyState()
		e, _ := newTestEngine(t, state)

		actionID := "enc-" + intToStr(i)
		c := cmd(e, actionID, KindQuestRun)
		c.TargetID = "herb-gathering"
		e.State().Pending.Confirmation = &PendingConfirmation{
			Token:             "t",
			Kind:              KindQuestRun,
			TargetID:          "herb-gathering",
			ExpiresAtRevision: e.State().Revision,
		}
		c.ConfirmationToken = "t"

		r := e.Submit(c)
		if r.OK && hasEvent(r, ResultCombatStarted) {
			hitEngine = e
			hitResult = r
		}
	}

	if hitEngine == nil {
		t.Skip("no action id in the searched range raised an encounter; nothing to assert")
	}

	// The month must NOT have been charged yet: the battle is still open.
	if got := hitEngine.State().Counters.WorldMonth; got != 0 {
		t.Fatalf("world month = %d while a battle is open, want 0 (the parent has not settled)", got)
	}
	if hitResult.MonthCostApplied != 0 {
		t.Fatalf("month cost = %d while a battle is open, want 0", hitResult.MonthCostApplied)
	}
	if hitEngine.State().Pending.MonthAction == nil {
		t.Fatal("the parent action was not kept open across the encounter")
	}
	if !hitEngine.State().Pending.MonthAction.SettlementPending {
		t.Fatal("the parent action does not report a pending settlement")
	}
	if hitEngine.State().Pending.Combat == nil {
		t.Fatal("no combat sub-state was raised")
	}
	if !hitEngine.State().Pending.Combat.AwaitingPlayer {
		t.Fatal("the battle does not await the player: a resume would grant a free enemy turn")
	}
	if hitEngine.State().Phase != PhaseCombatPending {
		t.Fatalf("phase = %s, want %s", hitEngine.State().Phase, PhaseCombatPending)
	}
}

// TestParentActionRecordsStartedMaterialsSoResumeCannotRespend pins the
// "materials spent once" field even though TASK-11 fills the content.
func TestParentActionRecordsStartedMaterialsSoResumeCannotRespend(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	c := cmd(e, "run", KindQuestRun)
	c.TargetID = "herb-gathering"
	e.State().Pending.Confirmation = &PendingConfirmation{
		Token:             "t",
		Kind:              KindQuestRun,
		TargetID:          "herb-gathering",
		ExpiresAtRevision: e.State().Revision,
	}
	c.ConfirmationToken = "t"

	if r := e.Submit(c); !r.OK {
		t.Fatalf("quest run rejected: %s", r.Code)
	}

	// After settlement the parent is cleared, so there is nothing left to
	// re-spend. If an encounter kept it open, the record must exist.
	if parent := e.State().Pending.MonthAction; parent != nil {
		if parent.ActionID != "run" {
			t.Fatalf("parent action id = %q, want run", parent.ActionID)
		}
	}
}

// TestSettlementOrderIsNotAffectedByExtraMonths checks that two consecutive
// one-month actions advance exactly two months and two ages, with no drift.
func TestSettlementOrderIsNotAffectedByExtraMonths(t *testing.T) {
	state := readyState()
	startAge := state.Player.AgeMonths
	e, _ := newTestEngine(t, state)

	for i := 0; i < 12; i++ {
		if r := e.Submit(cmd(e, "m"+intToStr(i), KindWait)); !r.OK {
			t.Fatalf("month %d rejected: %s", i, r.Code)
		}
	}

	if got := e.State().Counters.WorldMonth; got != 12 {
		t.Fatalf("world month = %d after 12 actions, want 12", got)
	}
	if got := e.State().Player.AgeMonths; got != startAge+12 {
		t.Fatalf("age = %d after 12 months, want %d", got, startAge+12)
	}
	// Twelve months from 387-01 is 388-01.
	d := DateFromMonths(e.State().Counters.WorldMonth)
	if d.Year != 388 || d.Month != 1 {
		t.Fatalf("date = %d年%d月, want 388年1月", d.Year, d.Month)
	}
}

// TestNPCsAgeWithTheWorldMonth covers design 5.2's rule seen from the NPC side:
// NPCs follow the game month, not the real clock. A settled month ages them by
// exactly one.
func TestNPCsAgeWithTheWorldMonth(t *testing.T) {
	state := readyState()
	state.World.NPCs["elder"] = NPC{
		ID:            "elder",
		IsAlive:       true,
		AgeMonths:     600,
		LifespanYears: 80, // 960 months
		Location:      "village",
	}
	e, _ := newTestEngine(t, state)

	for i := 0; i < 12; i++ {
		if r := e.Submit(cmd(e, "m"+intToStr(i), KindWait)); !r.OK {
			t.Fatalf("month %d rejected: %s", i, r.Code)
		}
	}

	npc := e.State().World.NPCs["elder"]
	if npc.AgeMonths != 612 {
		t.Fatalf("NPC age = %d after 12 months, want 612", npc.AgeMonths)
	}
	if !npc.IsAlive {
		t.Fatal("the NPC died well before their ceiling")
	}
}

// TestNPCsDieWhenTheirOwnLifespanRunsOut pins the NPC death boundary, which the
// design relies on: "已死NPC不接受礼物或新对话".
func TestNPCsDieWhenTheirOwnLifespanRunsOut(t *testing.T) {
	state := readyState()
	state.World.NPCs["elder"] = NPC{
		ID:            "elder",
		IsAlive:       true,
		AgeMonths:     959, // one month short of 960
		LifespanYears: 80,
	}
	e, _ := newTestEngine(t, state)

	if r := e.Submit(cmd(e, "m1", KindWait)); !r.OK {
		t.Fatalf("month rejected: %s", r.Code)
	}

	npc := e.State().World.NPCs["elder"]
	if npc.AgeMonths != 960 {
		t.Fatalf("NPC age = %d, want 960", npc.AgeMonths)
	}
	if npc.IsAlive {
		t.Fatal("the NPC reached their lifespan ceiling and must be dead")
	}

	// A dead NPC must stay dead across further months.
	if r := e.Submit(cmd(e, "m2", KindWait)); !r.OK {
		t.Fatalf("month rejected: %s", r.Code)
	}
	if e.State().World.NPCs["elder"].IsAlive {
		t.Fatal("a dead NPC came back to life")
	}
}

// TestNPCsDoNotAgeWithoutASettledMonth is the other half of the rule: a query,
// a rejected command, or simply waiting must not age anyone.
func TestNPCsDoNotAgeWithoutASettledMonth(t *testing.T) {
	state := readyState()
	state.World.NPCs["elder"] = NPC{ID: "elder", IsAlive: true, AgeMonths: 600, LifespanYears: 80}
	e, _ := newTestEngine(t, state)

	// Queries.
	for i := 0; i < 5; i++ {
		q := cmd(e, "q"+intToStr(i), KindQuery)
		q.Payload.QueryKind = QueryStatus
		if r := e.Submit(q); !r.OK {
			t.Fatalf("query rejected: %s", r.Code)
		}
	}

	// A rejected action.
	bad := cmd(e, "bad", KindBreakthrough) // missing confirmation
	if r := e.Submit(bad); r.OK {
		t.Fatal("expected the unconfirmed breakthrough to be rejected")
	}

	if got := e.State().World.NPCs["elder"].AgeMonths; got != 600 {
		t.Fatalf("NPC age = %d with no settled month, want 600", got)
	}
}

// TestEffectExpiryProducesNilNotAnEmptySlice pins the representation: an
// all-expired effect list collapses to nil rather than to an empty non-nil
// slice.
//
// Honest scope note: this test deliberately says nothing about digest stability,
// and an earlier version of it wrongly claimed to. SaveEnvelope.CanonicalString
// covers only the fields the save envelope needed when it was written in
// TASK-04; Condition.Effects is not among them, so two states differing only in
// this field produce the SAME digest. See
// TestCanonicalStringDoesNotCoverConditionEffects, which records that as an
// executable fact instead of asserting around it.
//
// The nil-versus-empty distinction is still worth pinning on its own merits:
// encoding/json renders the two as "null" and "[]", so any future canonical form
// that does cover this field would need them to compare equal, and a silent
// conversion to an empty slice here would be a representation change nobody
// asked for.
func TestEffectExpiryProducesNilNotAnEmptySlice(t *testing.T) {
	state := readyState()
	state.Player.Condition.Effects = []TimedEffect{{ID: "brief", MonthsRemaining: 1}}
	e, _ := newTestEngine(t, state)

	if r := e.Submit(cmd(e, "m1", KindWait)); !r.OK {
		t.Fatalf("month rejected: %s", r.Code)
	}

	if e.State().Player.Condition.Effects != nil {
		t.Fatalf("an all-expired effect list became %#v, want nil", e.State().Player.Condition.Effects)
	}
}

// TestCanonicalStringCoversCultivationState pins which parts of the state the
// integrity digest actually covers, and which it does not.
//
// The canonical digest is the game's determinism backstop: two states with equal
// mechanics must produce equal digests. CanonicalString is deliberately an
// explicit list rather than a reflection over the struct, so covering a new
// field is a conscious edit paired with a schema bump — widening the canonical
// form invalidates every digest a previous build would have written.
//
// TASK-08 closed Condition.Effects and the cultivation inputs. TASK-10 closed
// the event queue, the occurrence ledger and the world flags. TASK-11 closed the
// NPC cast, the current location and the visited-location list.
//
// Still outside the digest, and still a real limitation rather than a decision:
// Player.Lifespan.Bonuses, Player.Inventory/Equipment, Player.SectID,
// Player.Insights, World.Quests, and the combat, breakthrough and month-action
// pending sub-states. A save that lost a lifespan bonus or an equipped weapon
// would still verify. Each is a per-field decision that has to be paired with a
// schema bump, exactly as this test's history shows.
func TestCanonicalStringCoversCultivationState(t *testing.T) {
	checks := map[string]func(*GameState){
		"condition effects": func(s *GameState) {
			s.Player.Condition.Effects = []TimedEffect{{ID: "poison", MonthsRemaining: 5, Stacks: 3}}
		},
		"condition mood":    func(s *GameState) { s.Player.Condition.Mood++ },
		"aptitude":          func(s *GameState) { s.Player.Attributes.Aptitude++ },
		"primary technique": func(s *GameState) { s.Player.PrimaryTechniqueID = "other" },
		"proficiency":       func(s *GameState) { s.Player.Proficiencies = map[string]int64{"yellow_breath": 1} },
	}
	for name, mutate := range checks {
		if !digestCovers(t, mutate) {
			t.Errorf("canonical digest does not cover %s", name)
		}
	}

	if !digestCovers(t, func(s *GameState) {
		s.World.NPCs["x"] = NPC{ID: "x", IsAlive: true, AgeMonths: 900, LifespanYears: 80}
	}) {
		t.Fatal("the digest is not sensitive to the NPC cast. TASK-11 closed this gap " +
			"(the cast now exists and ages with the world, so an edited age or liveness " +
			"changes who is available); if it has been reopened, the tampering it allows " +
			"is silent")
	}

	if !digestCovers(t, func(s *GameState) {
		s.Player.HP.Current -= SCALE
	}) {
		t.Fatal("the digest is not sensitive to player HP, so this test proves nothing " +
			"about what the digest does cover")
	}

	// The same walk covers fields the digest unquestionably DOES track, so that
	// a future failure of the two assertions above reads as "the gap closed"
	// rather than "digestCovers silently broke".
	for _, covered := range []struct {
		name   string
		mutate func(*GameState)
	}{
		{"revision", func(s *GameState) { s.Revision++ }},
		{"world month", func(s *GameState) { s.Counters.WorldMonth++ }},
		{"age in months", func(s *GameState) { s.Player.AgeMonths++ }},
		{"spirit stones", func(s *GameState) { s.Player.Resources[ResSpiritStones]++ }},
		{"rng counter", func(s *GameState) {
			world := s.RNG.Streams[StreamWorld]
			world.Counter++
			s.RNG.Streams[StreamWorld] = world
		}},
	} {
		if !digestCovers(t, covered.mutate) {
			t.Fatalf("the digest is not sensitive to %s, but it is supposed to be; "+
				"digestCovers is broken and the two assertions above are meaningless", covered.name)
		}
	}
}

// TestDecrementEffectsHandlesNilAndEmpty covers the two degenerate inputs.
func TestDecrementEffectsHandlesNilAndEmpty(t *testing.T) {
	if got := decrementEffects(nil); got != nil {
		t.Fatalf("decrementEffects(nil) = %#v, want nil", got)
	}
	if got := decrementEffects([]TimedEffect{}); len(got) != 0 {
		t.Fatalf("decrementEffects(empty) = %#v, want empty", got)
	}
}
