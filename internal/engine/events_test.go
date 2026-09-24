package engine

import "testing"

// Tests for the TASK-10 event scheduler, the whitelist effect DSL and the
// month-end queue.
//
// The fixtures here drive the scheduler through its real entry point
// (runMonthEndEvents, reached by settling a month) wherever the behaviour under
// test is reachable that way. A few paths — expiry in particular — are driven
// directly, because M1's phase rules make them unreachable through play; see
// the note on TestQueuedEventExpiresWhenItsDeadlinePasses.

// eventCatalogue builds the pipeline fixture catalogue with the given events
// and an explicit base event chance.
func eventCatalogue(chancePermille int, events ...EventDefinition) *Catalogue {
	cat := testCatalogue()
	cat.EventBaseChancePermille = ConfigValue{
		Provenance: ProvenanceDesignNote, Value: chancePermille, Note: "test base chance",
	}
	cat.Events = append(cat.Events, events...)
	return cat
}

// weightedEvent is a minimal drawable event.
func weightedEvent(id string, weight, priority int) EventDefinition {
	return EventDefinition{
		ID: id, NameZH: id, Scene: "test", Purpose: "测试节点",
		Priority: ConfigValue{Provenance: ProvenanceDesignNote, Value: priority, Note: "测试"},
		Weight:   ConfigValue{Provenance: ProvenanceDesignNote, Value: weight, Note: "测试"},
		Choices:  []EventChoice{{ID: "go", TextZH: "前往"}},
	}
}

// settleMonth submits an explicit wait, which costs one month and therefore runs
// the month-end pass.
func settleMonth(t *testing.T, e *Engine, actionID string) CommandResult {
	t.Helper()
	r := e.Submit(cmd(e, actionID, KindWait))
	if !r.OK {
		t.Fatalf("wait rejected: %s %v", r.Code, r.Events)
	}
	return r
}

// spendMonth reaches the next month, answering whatever node is being shown
// first. A pending node replaces the scene actions (design 12.4), so a test that
// wants to walk several months has to answer along the way — which is itself the
// property TestPendingNodeBlocksMonthActions pins down.
func spendMonth(t *testing.T, e *Engine, tag string) {
	t.Helper()
	if pe := e.State().Pending.Event; pe != nil {
		ev := findEvent(e.Catalogue, pe.EventID)
		if ev == nil || len(ev.Choices) == 0 {
			t.Fatalf("pending event %s has no declared choices", pe.EventID)
		}
		answerPending(t, e, "answer-"+tag, ev.Choices[0].ID)
	}
	settleMonth(t, e, "wait-"+tag)
}

// worldCounter reads the persisted world stream's draw count.
func worldCounter(t *testing.T, s *GameState) uint64 {
	t.Helper()
	set, ok := LoadRNGSet(s.RNG)
	if !ok {
		t.Fatal("the world random stream could not be loaded from the test state")
	}
	return set.World().Counter()
}

// ---------------------------------------------------------------------------
// The base chance
// ---------------------------------------------------------------------------

// TestBaseEventChanceMatchesTheDesignFigure checks design 11.1: "20%基础检定
// 10万次、误差不超过0.5个百分点". The check is on the probability primitive
// itself; eligibility and balance are separate questions and are covered by
// their own tests.
func TestBaseEventChanceMatchesTheDesignFigure(t *testing.T) {
	if DefaultEventBaseChancePermille != 200 {
		t.Fatalf("the default base event chance is %d permille; design R17 states 20%%",
			DefaultEventBaseChancePermille)
	}

	const trials = 100000
	stream := NewStream(SeedFromIdentity("statistics", "world"))

	hits := 0
	for i := 0; i < trials; i++ {
		if stream.Chance(DefaultEventBaseChancePermille) {
			hits++
		}
	}

	// |hits/trials - 0.20| <= 0.005, expressed in integers so the assertion
	// does not itself depend on floating point.
	diff := hits*1000 - trials*200
	if diff < 0 {
		diff = -diff
	}
	if diff > trials*5 {
		t.Fatalf("the base check fired %d times in %d draws (%.3f%%); design 11.1 allows "+
			"at most 0.5 percentage points of error around 20%%",
			hits, trials, float64(hits)*100/trials)
	}
}

func TestZeroBaseChanceNeverRaises(t *testing.T) {
	state := readyState()
	cat := eventCatalogue(0, weightedEvent("EVT-R", 10, 50))
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	for i := 0; i < 50; i++ {
		settleMonth(t, e, "wait-"+itoa64(int64(i)))
		if e.State().Pending.Event != nil {
			t.Fatalf("an event was raised with the base chance set to zero")
		}
	}
	if got := raisedCount(e.State(), "EVT-R"); got != 0 {
		t.Fatalf("raised %d instances with the base chance set to zero, want 0", got)
	}
}

func TestCertainBaseChanceAlwaysRaises(t *testing.T) {
	state := readyState()
	cat := eventCatalogue(PermilleScale, weightedEvent("EVT-R", 10, 50))
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	settleMonth(t, e, "wait-1")
	if e.State().Pending.Event == nil {
		t.Fatal("no event was raised with the base chance set to certainty")
	}
	if e.State().Phase != PhaseEventPending {
		t.Fatalf("phase = %s, want EVENT_PENDING", e.State().Phase)
	}
}

// ---------------------------------------------------------------------------
// One node at a time, and the queue
// ---------------------------------------------------------------------------

func TestOnlyOneNodeIsShownAndTheRestQueue(t *testing.T) {
	state := readyState()

	// The forced event is raised first, because forced events are raised in
	// catalogue order before the draw, but it carries the LOWER priority. That
	// ordering is deliberate: it is what makes this test sensitive to the
	// priority rule rather than to the queue's insertion order.
	forced := weightedEvent("EVT-FORCED", 0, 10)
	forced.Forced = true

	cat := eventCatalogue(PermilleScale, forced, weightedEvent("EVT-RANDOM", 10, 90))
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	settleMonth(t, e, "wait-1")

	if e.State().Pending.Event == nil {
		t.Fatal("no event is pending after a month that raised two")
	}
	if len(e.State().Pending.EventQueue) != 1 {
		t.Fatalf("queue holds %d instances, want exactly 1: only one node may be shown at a time",
			len(e.State().Pending.EventQueue))
	}
	if got := e.State().Pending.Event.EventID; got != "EVT-RANDOM" {
		t.Fatalf("pending event = %s, want the higher-priority EVT-RANDOM", got)
	}
	if got := e.State().Pending.EventQueue[0].EventID; got != "EVT-FORCED" {
		t.Fatalf("queued event = %s, want the lower-priority EVT-FORCED", got)
	}
	if got := raisedCount(e.State(), "EVT-FORCED"); got != 1 {
		t.Fatalf("forced raises = %d, want 1", got)
	}
	if got := raisedCount(e.State(), "EVT-RANDOM"); got != 1 {
		t.Fatalf("random raises = %d, want 1", got)
	}
}

func TestAnsweringANodePromotesTheNextQueuedOne(t *testing.T) {
	state := readyState()

	high := weightedEvent("EVT-HIGH", 0, 90)
	high.Forced = true
	high.Choices = []EventChoice{{ID: "go", TextZH: "前往"}}
	low := weightedEvent("EVT-LOW", 0, 10)
	low.Forced = true

	cat := eventCatalogue(0, high, low)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	settleMonth(t, e, "wait-1")
	if got := e.State().Pending.Event.EventID; got != "EVT-HIGH" {
		t.Fatalf("pending = %s, want EVT-HIGH", got)
	}

	// Answering the pending node must reveal the queued one without spending a
	// month: the player should not have to burn a month to see what was already
	// raised.
	before := e.State().Counters.WorldMonth
	r := e.Submit(Command{
		ActionID: "answer-1", SessionID: "s",
		ExpectedRevision: e.State().Revision, ViewToken: e.ViewToken(),
		Kind: KindEventChoice, Payload: Payload{ChoiceID: "go"},
	})
	if !r.OK {
		t.Fatalf("answering the pending event was rejected: %s", r.Code)
	}
	if e.State().Counters.WorldMonth != before {
		t.Fatalf("answering a month-end node advanced the month to %d, want %d",
			e.State().Counters.WorldMonth, before)
	}
	if e.State().Pending.Event == nil || e.State().Pending.Event.EventID != "EVT-LOW" {
		t.Fatalf("the queued node was not promoted; pending = %+v", e.State().Pending.Event)
	}
	if len(e.State().Pending.EventQueue) != 0 {
		t.Fatalf("queue still holds %d instances after promotion", len(e.State().Pending.EventQueue))
	}
}

func TestPromotionTieIsBrokenByRaiseOrder(t *testing.T) {
	state := readyState()

	first := weightedEvent("EVT-FIRST", 0, 50)
	first.Forced = true
	second := weightedEvent("EVT-SECOND", 0, 50)
	second.Forced = true

	cat := eventCatalogue(0, first, second)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	settleMonth(t, e, "wait-1")

	if got := e.State().Pending.Event.EventID; got != "EVT-FIRST" {
		t.Fatalf("pending = %s, want EVT-FIRST: equal priority must fall back to raise order, "+
			"otherwise a reload could change which node the player sees", got)
	}
}

// ---------------------------------------------------------------------------
// Cooldown, occurrence caps and expiry
// ---------------------------------------------------------------------------

// TestPendingNodeBlocksMonthActions pins the rule that makes the queue drain
// rather than grow: design 12.4 replaces the four scene actions with the event's
// options while a node waits, so no month can settle and no new instance can be
// raised behind the player's back.
func TestPendingNodeBlocksMonthActions(t *testing.T) {
	state := readyState()
	cat := eventCatalogue(PermilleScale, weightedEvent("EVT-R", 10, 50))
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	settleMonth(t, e, "wait-1")
	if e.State().Pending.Event == nil {
		t.Fatal("no node is pending")
	}

	before := e.State().Counters.WorldMonth
	r := e.Submit(cmd(e, "wait-2", KindWait))
	if r.OK {
		t.Fatal("a month action was accepted while an event node was waiting")
	}
	if r.Code != ErrBadPhase {
		t.Fatalf("code = %s, want %s", r.Code, ErrBadPhase)
	}
	if e.State().Counters.WorldMonth != before {
		t.Fatalf("a rejected month action still advanced the month to %d", e.State().Counters.WorldMonth)
	}
}

func TestTheSameEventIsNotQueuedTwice(t *testing.T) {
	// A repeatable, zero-cooldown event would otherwise fill the queue with
	// identical instances and show the player the same node over and over.
	//
	// The scheduler is driven directly rather than through a month action,
	// because this guard — like expiry — is not reachable through M1 play: a
	// pending node blocks month actions (design 12.4), so a queued instance
	// cannot survive to see another month. A save written by a future build can
	// still contain the state, and resuming it must not stack duplicates.
	repeatable := weightedEvent("EVT-REPEAT", 10, 50)

	state := readyState()
	state.Counters.WorldMonth = 2
	state.Pending.EventQueue = []QueuedEvent{{
		EventID: "EVT-REPEAT", InstanceID: "EVT-REPEAT#1", Priority: 50,
		RaisedAtWorldMonth: 1, ChoiceIDs: []string{"go"},
	}}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-REPEAT", InstanceID: "EVT-REPEAT#1", WorldMonth: 1,
	}}
	state.World.EventInstanceSeq = 1

	cat := eventCatalogue(PermilleScale, repeatable)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	var result CommandResult
	e.runMonthEndEvents(state, &result)

	if got := raisedCount(state, "EVT-REPEAT"); got != 1 {
		t.Fatalf("raises = %d, want 1: an event that is already queued must not be raised again", got)
	}
	// The existing instance is promoted rather than a duplicate being created.
	if state.Pending.Event == nil || state.Pending.Event.EventID != "EVT-REPEAT" {
		t.Fatalf("the queued instance was not promoted: %+v", state.Pending.Event)
	}
	if len(state.Pending.EventQueue) != 0 {
		t.Fatalf("queue holds %d instances, want 0", len(state.Pending.EventQueue))
	}
}

func TestCooldownBlocksARaiseUntilItElapses(t *testing.T) {
	limited := weightedEvent("EVT-CD", 10, 50)
	limited.CooldownMonths = 3

	state := readyState()
	state.Counters.WorldMonth = 4
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-CD", InstanceID: "EVT-CD#1", WorldMonth: 3,
	}}
	state.World.EventInstanceSeq = 1

	cat := eventCatalogue(PermilleScale, limited)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	// Month 5: only two months have passed, so the three-month cooldown holds.
	spendMonth(t, e, "1")
	if got := e.State().Counters.WorldMonth; got != 5 {
		t.Fatalf("world month = %d, want 5", got)
	}
	if got := raisedCount(e.State(), "EVT-CD"); got != 1 {
		t.Fatalf("cooldown of 3 months allowed a second raise after 2 months (raises = %d)", got)
	}

	// Month 6: 6 - 3 = 3, so the cooldown has elapsed.
	spendMonth(t, e, "2")
	if got := e.State().Counters.WorldMonth; got != 6 {
		t.Fatalf("world month = %d, want 6", got)
	}
	if got := raisedCount(e.State(), "EVT-CD"); got != 2 {
		t.Fatalf("cooldown elapsed but no second raise happened (raises = %d, world month = %d)",
			got, e.State().Counters.WorldMonth)
	}
}

func TestMaxOccurrencesStopsFurtherRaises(t *testing.T) {
	limited := weightedEvent("EVT-ONCE", 10, 50)
	limited.MaxOccurrences = 1

	state := readyState()
	cat := eventCatalogue(PermilleScale, limited)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	settleMonth(t, e, "wait-1")
	if got := raisedCount(e.State(), "EVT-ONCE"); got != 1 {
		t.Fatalf("raises = %d, want 1", got)
	}

	// Answer the node so play can continue, then spend more months.
	answerPending(t, e, "answer-1", "go")
	for i := 2; i <= 5; i++ {
		settleMonth(t, e, "wait-"+itoa64(int64(i)))
	}
	if got := raisedCount(e.State(), "EVT-ONCE"); got != 1 {
		t.Fatalf("a MaxOccurrences=1 event was raised %d times", got)
	}
}

// TestQueuedEventExpiresWhenItsDeadlinePasses drives the scheduler directly.
//
// The path is not reachable through play under M1's phase rules: a pending node
// replaces the scene actions (design 12.4), so no month can settle while a node
// waits, and the queue therefore drains one node per answer before a month can
// pass. It is still implemented and tested, because a save written by a future
// build — or by a task that adds an "ignore this event" action — can contain a
// queued instance that has run out of time, and resuming such a save must not
// promote a node that already expired.
func TestQueuedEventExpiresWhenItsDeadlinePasses(t *testing.T) {
	state := readyState()
	state.Counters.WorldMonth = 10
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-OLD", InstanceID: "EVT-OLD#1", WorldMonth: 8,
	}}
	state.World.EventInstanceSeq = 1
	state.Pending.EventQueue = []QueuedEvent{{
		EventID: "EVT-OLD", InstanceID: "EVT-OLD#1", Priority: 10,
		RaisedAtWorldMonth: 8, ExpiresAtWorldMonth: 10,
		ChoiceIDs: []string{"go"},
	}}

	cat := testCatalogue()
	cat.EventBaseChancePermille = ConfigValue{Provenance: ProvenanceDesignNote, Value: 0, Note: "test"}
	cat.Events = nil

	e, _ := newTestEngineWithCatalogue(t, state, cat)

	var result CommandResult
	e.runMonthEndEvents(state, &result)

	if len(state.Pending.EventQueue) != 0 {
		t.Fatalf("an expired instance is still queued: %+v", state.Pending.EventQueue)
	}
	if state.Pending.Event != nil {
		t.Fatalf("an expired instance was promoted to pending: %+v", state.Pending.Event)
	}
	if !hasResultEvent(result, ResultEventExpired, "EVT-OLD") {
		t.Fatalf("expiry was not reported in the result events: %+v", result.Events)
	}
}

// TestExpiredInstanceStillConsumesTheOccurrenceBudget closes the obvious
// loophole: if expiry also forgot the occurrence, a player could see a limited
// event again simply by ignoring it.
func TestExpiredInstanceStillConsumesTheOccurrenceBudget(t *testing.T) {
	limited := weightedEvent("EVT-LIMITED", 10, 50)
	limited.MaxOccurrences = 1

	state := readyState()
	state.Counters.WorldMonth = 10
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-LIMITED", InstanceID: "EVT-LIMITED#1", WorldMonth: 8,
	}}
	state.World.EventInstanceSeq = 1
	state.Pending.EventQueue = []QueuedEvent{{
		EventID: "EVT-LIMITED", InstanceID: "EVT-LIMITED#1", Priority: 10,
		RaisedAtWorldMonth: 8, ExpiresAtWorldMonth: 10,
		ChoiceIDs: []string{"go"},
	}}

	cat := eventCatalogue(PermilleScale, limited)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	var result CommandResult
	e.runMonthEndEvents(state, &result)

	if got := raisedCount(state, "EVT-LIMITED"); got != 1 {
		t.Fatalf("raises = %d after expiry, want 1: an expired instance must still count "+
			"against MaxOccurrences, or ignoring an event becomes a way to re-roll it", got)
	}
	if state.Pending.Event != nil {
		t.Fatalf("an expired, occurrence-exhausted event was raised again: %+v", state.Pending.Event)
	}
}

// ---------------------------------------------------------------------------
// What must NOT draw a monthly event
// ---------------------------------------------------------------------------

func TestQueriesDoNotDrawAMonthlyEvent(t *testing.T) {
	state := readyState()
	cat := eventCatalogue(PermilleScale, weightedEvent("EVT-R", 10, 50))
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	beforeDraws := worldCounter(t, e.State())
	beforeMonth := e.State().Counters.WorldMonth

	r := e.Submit(Command{
		ActionID: "query-1", SessionID: "s",
		ExpectedRevision: e.State().Revision, ViewToken: e.ViewToken(),
		Kind: KindQuery, Payload: Payload{QueryKind: QueryStatus},
	})
	if !r.OK {
		t.Fatalf("query rejected: %s", r.Code)
	}

	if e.State().Counters.WorldMonth != beforeMonth {
		t.Fatalf("a query advanced the world month to %d", e.State().Counters.WorldMonth)
	}
	if got := worldCounter(t, e.State()); got != beforeDraws {
		t.Fatalf("a query drew from the world stream (counter %d -> %d)", beforeDraws, got)
	}
	if e.State().Pending.Event != nil {
		t.Fatal("a query raised an event")
	}
}

func TestCombatRoundsDoNotDrawAMonthlyEvent(t *testing.T) {
	state := readyState()
	state.Phase = PhaseCombatPending
	state.Pending.Combat = &PendingCombat{
		CombatID:       "combat-1",
		Round:          1,
		AwaitingPlayer: true,
		Resolution:     CombatOngoing,
		AllyState:      combatantFromPlayer(state.Player),
		SpiritEnergy:   SpiritEnergy{Current: 0, Max: 100},
	}

	cat := eventCatalogue(PermilleScale, weightedEvent("EVT-R", 10, 50))
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	beforeDraws := worldCounter(t, e.State())
	beforeMonth := e.State().Counters.WorldMonth
	beforeRound := e.State().Counters.CombatRound

	r := e.Submit(Command{
		ActionID: "attack-1", SessionID: "s",
		ExpectedRevision: e.State().Revision, ViewToken: e.ViewToken(),
		Kind: KindCombatAction, Payload: Payload{SkillID: "basic"},
	})
	if !r.OK {
		t.Fatalf("combat action rejected: %s", r.Code)
	}

	if e.State().Counters.CombatRound != beforeRound+1 {
		t.Fatalf("combat round = %d, want %d", e.State().Counters.CombatRound, beforeRound+1)
	}
	if e.State().Counters.WorldMonth != beforeMonth {
		t.Fatalf("a combat round advanced the world month to %d", e.State().Counters.WorldMonth)
	}
	if got := worldCounter(t, e.State()); got != beforeDraws {
		t.Fatalf("a combat round drew from the world stream (counter %d -> %d)", beforeDraws, got)
	}
	if e.State().Pending.Event != nil {
		t.Fatal("a combat round raised an event")
	}
}

func TestEmptyCatalogueDoesNotBlockPlay(t *testing.T) {
	state := readyState()
	cat := testCatalogue()
	cat.EventBaseChancePermille = ConfigValue{Provenance: ProvenanceDesignNote, Value: PermilleScale, Note: "test"}
	cat.Events = nil

	e, _ := newTestEngineWithCatalogue(t, state, cat)
	settleMonth(t, e, "wait-1")

	if e.State().Phase != PhaseReady {
		t.Fatalf("phase = %s, want READY: a month with no eligible event must return the player "+
			"to the ordinary scene actions rather than blocking on an empty queue", e.State().Phase)
	}
}

// ---------------------------------------------------------------------------
// Choice resolution: conditions, costs, effects, branches
// ---------------------------------------------------------------------------

func TestUnmetChoiceConditionIsRejectedAndChangesNothing(t *testing.T) {
	state := readyState()
	state.Phase = PhaseEventPending

	ev := weightedEvent("EVT-GATED", 0, 50)
	ev.Choices = []EventChoice{{
		ID: "go", TextZH: "前往",
		Requires: []Precondition{{
			Kind: CondFunds, Op: OpGE, Value: 100000,
		}},
		Effects: []GrantEffect{{
			Kind: GrantAdditive, Target: targetSpiritStones, Amount: 500, Reason: "报酬",
		}},
	}}
	state.Pending.Event = &PendingEvent{
		EventID: "EVT-GATED", InstanceID: "EVT-GATED#1", ChoiceIDs: []string{"go"},
	}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-GATED", InstanceID: "EVT-GATED#1", WorldMonth: 0,
	}}
	state.World.EventInstanceSeq = 1

	cat := eventCatalogue(0, ev)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	beforeStones := e.State().Player.Resources[ResSpiritStones]
	beforeRevision := e.State().Revision

	r := e.Submit(Command{
		ActionID: "answer-1", SessionID: "s",
		ExpectedRevision: e.State().Revision, ViewToken: e.ViewToken(),
		Kind: KindEventChoice, Payload: Payload{ChoiceID: "go"},
	})
	if r.OK {
		t.Fatal("a choice whose condition is unmet was accepted")
	}
	if r.Code != ErrPreconditionUnmet {
		t.Fatalf("code = %s, want %s", r.Code, ErrPreconditionUnmet)
	}
	if got := e.State().Player.Resources[ResSpiritStones]; got != beforeStones {
		t.Fatalf("stones changed from %d to %d on a rejected choice", beforeStones, got)
	}
	if e.State().Revision != beforeRevision {
		t.Fatalf("revision advanced on a rejected choice")
	}
	if e.State().Pending.Event == nil {
		t.Fatal("a rejected choice consumed the pending event")
	}
}

func TestUnaffordableChoiceIsRejectedWithoutBorrowing(t *testing.T) {
	state := readyState()
	state.Phase = PhaseEventPending
	state.Player.Resources[ResSpiritStones] = 10

	ev := weightedEvent("EVT-PRICEY", 0, 50)
	ev.Choices = []EventChoice{{
		ID: "buy", TextZH: "买下",
		Costs: []EventCost{{Kind: CostResource, Resource: ResSpiritStones, Amount: 100}},
		Effects: []GrantEffect{{
			Kind: GrantAdditive, Target: "items.spirit_herb", Amount: 1, Reason: "买到草药",
		}},
	}}
	state.Pending.Event = &PendingEvent{
		EventID: "EVT-PRICEY", InstanceID: "EVT-PRICEY#1", ChoiceIDs: []string{"buy"},
	}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-PRICEY", InstanceID: "EVT-PRICEY#1", WorldMonth: 0,
	}}
	state.World.EventInstanceSeq = 1

	cat := eventCatalogue(0, ev)
	cat.Items = append(cat.Items, ItemDefinition{
		ID: "spirit_herb", NameZH: "灵草", Category: ItemMaterial,
		BuyPrice:  ConfigValue{Provenance: ProvenanceManuscript, Value: 50},
		SellPrice: ConfigValue{Provenance: ProvenanceManuscript, Value: 25},
	})
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	r := e.Submit(Command{
		ActionID: "answer-1", SessionID: "s",
		ExpectedRevision: e.State().Revision, ViewToken: e.ViewToken(),
		Kind: KindEventChoice, Payload: Payload{ChoiceID: "buy"},
	})
	if r.OK {
		t.Fatal("an unaffordable choice was accepted")
	}
	if r.Code != ErrInsufficientFunds {
		t.Fatalf("code = %s, want %s", r.Code, ErrInsufficientFunds)
	}
	if got := e.State().Player.Resources[ResSpiritStones]; got != 10 {
		t.Fatalf("stones = %d, want 10", got)
	}
	if got := e.State().Player.Debt; got != 0 {
		t.Fatalf("debt = %d, want 0: design R19 forbids a purchase from creating debt automatically", got)
	}
	if got := heldQuantity(&e.State().Player.Inventory, "spirit_herb"); got != 0 {
		t.Fatalf("the item was granted despite the rejected payment (held %d)", got)
	}
}

func TestChoiceCostsAndEffectsAreAppliedAndRecorded(t *testing.T) {
	state := readyState()
	state.Phase = PhaseEventPending
	state.Player.Resources[ResSpiritStones] = 100
	state.Player.Condition.Mood = 50

	ev := weightedEvent("EVT-PAY", 0, 50)
	ev.Choices = []EventChoice{{
		ID: "work", TextZH: "接活",
		Costs: []EventCost{{Kind: CostHP, Amount: 5 * SCALE}},
		Effects: []GrantEffect{
			{Kind: GrantAdditive, Target: targetSpiritStones, Amount: 30, Reason: "工钱"},
			{Kind: GrantAdditive, Target: targetMood, Amount: 20, Reason: "心情好转"},
			{Kind: GrantAdditive, Target: "flags.done_chore", Amount: 1, Reason: "完成杂务"},
			{Kind: GrantAdditive, Target: "relations.gu_qingxuan", Amount: 5, Reason: "结识"},
			{Kind: GrantAdditive, Target: "breakthrough.demon_resist", Amount: 15, Reason: "心魔抗性"},
		},
	}}
	state.Pending.Event = &PendingEvent{
		EventID: "EVT-PAY", InstanceID: "EVT-PAY#1", ChoiceIDs: []string{"work"},
	}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-PAY", InstanceID: "EVT-PAY#1", WorldMonth: 0,
	}}
	state.World.EventInstanceSeq = 1

	cat := eventCatalogue(0, ev)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	r := answerPendingResult(t, e, "answer-1", "work")
	if !r.OK {
		t.Fatalf("choice rejected: %s", r.Code)
	}

	p := e.State().Player
	if got := p.Resources[ResSpiritStones]; got != 130 {
		t.Fatalf("stones = %d, want 130", got)
	}
	if got := p.HP.Current; got != 35*SCALE {
		t.Fatalf("hp = %d, want %d", got, 35*SCALE)
	}
	if got := p.Condition.Mood; got != 70 {
		t.Fatalf("mood = %d, want 70", got)
	}
	if !e.State().World.Flags["done_chore"] {
		t.Fatal("the flag was not set")
	}
	if got := relationValue(p, "gu_qingxuan"); got != 5 {
		t.Fatalf("relation = %d, want 5", got)
	}
	if got := p.BreakthroughBonuses["demon_resist"]; got != 15 {
		t.Fatalf("breakthrough bonus = %d, want 15", got)
	}
	if len(r.Delta.Entries) == 0 {
		t.Fatal("the ledger is empty: every change must be attributable so the UI can explain the month")
	}
}

func TestMoodGrantIsClampedToTheContractRange(t *testing.T) {
	state := readyState()
	state.Phase = PhaseEventPending
	state.Player.Condition.Mood = 95

	ev := weightedEvent("EVT-JOY", 0, 50)
	ev.Choices = []EventChoice{{
		ID: "rejoice", TextZH: "大喜",
		Effects: []GrantEffect{{Kind: GrantAdditive, Target: targetMood, Amount: 20, Reason: "大喜"}},
	}}
	state.Pending.Event = &PendingEvent{
		EventID: "EVT-JOY", InstanceID: "EVT-JOY#1", ChoiceIDs: []string{"rejoice"},
	}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-JOY", InstanceID: "EVT-JOY#1", WorldMonth: 0,
	}}
	state.World.EventInstanceSeq = 1

	cat := eventCatalogue(0, ev)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	if r := answerPendingResult(t, e, "answer-1", "rejoice"); !r.OK {
		t.Fatalf("choice rejected: %s", r.Code)
	}
	if got := e.State().Player.Condition.Mood; got != 100 {
		t.Fatalf("mood = %d, want 100: the state contract bounds mood to 0..100", got)
	}
}

func TestBranchAdvancesTheInstanceWithoutSettlingASecondMonth(t *testing.T) {
	state := readyState()
	state.Phase = PhaseEventPending

	ev := weightedEvent("EVT-CHAIN", 0, 50)
	ev.Choices = []EventChoice{
		{
			ID: "dig", TextZH: "深挖",
			Effects: []GrantEffect{{
				Kind: GrantAdditive, Target: "flags.dug", Amount: 1, Reason: "开始挖掘",
			}},
			NextNodeID: "deep",
		},
		{
			ID: "deep_go", TextZH: "继续",
			Effects: []GrantEffect{{
				Kind: GrantAdditive, Target: targetSpiritStones, Amount: 7, Reason: "挖到灵石",
			}},
		},
	}
	ev.FollowUps = []EventChainNode{{NodeID: "deep", ChoiceIDs: []string{"deep_go"}}}
	state.Pending.Event = &PendingEvent{
		EventID: "EVT-CHAIN", InstanceID: "EVT-CHAIN#1", ChoiceIDs: []string{"dig"},
	}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-CHAIN", InstanceID: "EVT-CHAIN#1", WorldMonth: 0,
	}}
	state.World.EventInstanceSeq = 1

	cat := eventCatalogue(0, ev)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	beforeMonth := e.State().Counters.WorldMonth
	beforeStones := e.State().Player.Resources[ResSpiritStones]

	r := answerPendingResult(t, e, "answer-1", "dig")
	if !r.OK {
		t.Fatalf("branching choice rejected: %s", r.Code)
	}
	if e.State().Counters.WorldMonth != beforeMonth {
		t.Fatalf("a branch advanced the world month to %d, want %d",
			e.State().Counters.WorldMonth, beforeMonth)
	}
	if e.State().Pending.Event == nil {
		t.Fatal("the branch resolved the instance instead of advancing it")
	}
	if got := e.State().Pending.Event.ChoiceIDs; len(got) != 1 || got[0] != "deep_go" {
		t.Fatalf("the next node's candidates were not frozen: %v", got)
	}
	if got := e.State().Player.Resources[ResSpiritStones]; got != beforeStones {
		t.Fatalf("the follow-up node's effect fired early (stones %d -> %d)", beforeStones, got)
	}
	if len(e.State().World.ConsumedEvents) != 0 {
		t.Fatalf("a branching choice consumed the instance: %+v", e.State().World.ConsumedEvents)
	}

	// The follow-up resolves the instance.
	if r := answerPendingResult(t, e, "answer-2", "deep_go"); !r.OK {
		t.Fatalf("follow-up choice rejected: %s", r.Code)
	}
	if got := e.State().Player.Resources[ResSpiritStones]; got != beforeStones+7 {
		t.Fatalf("stones = %d, want %d", got, beforeStones+7)
	}
	if e.State().Pending.Event != nil {
		t.Fatal("the instance was not resolved by its final node")
	}
	if len(e.State().World.ConsumedEvents) != 1 {
		t.Fatalf("consumed records = %d, want 1", len(e.State().World.ConsumedEvents))
	}
	if e.State().Counters.WorldMonth != beforeMonth {
		t.Fatalf("resolving a month-end chain advanced the month to %d", e.State().Counters.WorldMonth)
	}
}

func TestUnknownRuntimeEffectTargetIsRejectedAtRuntime(t *testing.T) {
	// Content validation rejects an unknown target at load time, so this
	// exercises the second line of defence: an engine handed a catalogue that
	// bypassed validation must still refuse to run an unrecognised effect
	// rather than treating it as a no-op.
	state := readyState()
	state.Phase = PhaseEventPending

	ev := weightedEvent("EVT-EVIL", 0, 50)
	ev.Choices = []EventChoice{{
		ID: "go", TextZH: "前往",
		Effects: []GrantEffect{{
			Kind: GrantAdditive, Target: "os.exec", Amount: 1, Reason: "尝试执行外部程序",
		}},
	}}
	state.Pending.Event = &PendingEvent{
		EventID: "EVT-EVIL", InstanceID: "EVT-EVIL#1", ChoiceIDs: []string{"go"},
	}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-EVIL", InstanceID: "EVT-EVIL#1", WorldMonth: 0,
	}}
	state.World.EventInstanceSeq = 1

	cat := eventCatalogue(0, ev)
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	r := answerPendingResult(t, e, "answer-1", "go")
	if r.OK {
		t.Fatal("an effect with a target outside the whitelist was applied")
	}
	if e.State().Pending.Event == nil {
		t.Fatal("the rejected effect still consumed the pending event")
	}
}

// ---------------------------------------------------------------------------
// Persistence of the queue
// ---------------------------------------------------------------------------

func TestCloningDoesNotShareTheQueueBackingArrays(t *testing.T) {
	state := readyState()
	state.Pending.EventQueue = []QueuedEvent{{
		EventID: "EVT-A", InstanceID: "EVT-A#1", Priority: 10,
		RaisedAtWorldMonth: 1, ExpiresAtWorldMonth: 5,
		ChoiceIDs: []string{"go"},
	}}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-A", InstanceID: "EVT-A#1", WorldMonth: 1,
	}}
	state.World.EventInstanceSeq = 1
	state.Player.Attributes.Growth = map[string]int{"aptitude": 1}
	state.Player.BreakthroughBonuses = map[string]int64{"demon_resist": 5}

	clone := CloneGameState(state)

	clone.Pending.EventQueue[0].ChoiceIDs[0] = "MUTATED"
	if state.Pending.EventQueue[0].ChoiceIDs[0] != "go" {
		t.Fatal("the queue's candidate slice is shared between the clone and the original")
	}

	clone.Player.Attributes.Growth["aptitude"] = 99
	if state.Player.Attributes.Growth["aptitude"] != 1 {
		t.Fatal("Attributes.Growth is shared between the clone and the original; a rejected " +
			"command would still edit the live state through the clone")
	}

	clone.Player.BreakthroughBonuses["demon_resist"] = 99
	if state.Player.BreakthroughBonuses["demon_resist"] != 5 {
		t.Fatal("BreakthroughBonuses is shared between the clone and the original")
	}

	clone.World.RaisedEvents[0].WorldMonth = 99
	if state.World.RaisedEvents[0].WorldMonth != 1 {
		t.Fatal("the occurrence ledger is shared between the clone and the original")
	}
}

func TestQueueSurvivesASaveRoundTripWithoutRerolling(t *testing.T) {
	state := readyState()
	state.Pending.EventQueue = []QueuedEvent{{
		EventID: "EVT-A", InstanceID: "EVT-A#1", Priority: 10,
		RaisedAtWorldMonth: 1, ExpiresAtWorldMonth: 5,
		ChoiceIDs: []string{"go", "stay"},
	}}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-A", InstanceID: "EVT-A#1", WorldMonth: 1,
	}}
	state.World.EventInstanceSeq = 1

	envelope := &SaveEnvelope{
		EnvelopeVersion: EnvelopeVersion,
		SchemaVersion:   SchemaVersion,
		RulesVersion:    RulesVersion,
		ContentVersion:  ContentVersion,
		GameID:          state.GameID,
		BranchID:        state.BranchID,
		Revision:        state.Revision,
		State:           *state,
	}
	envelope.ComputeIntegrity()
	if !envelope.VerifyIntegrity() {
		t.Fatal("the envelope does not verify after computing its own integrity")
	}

	// The digest must be sensitive to the queue: a tampered candidate set would
	// otherwise change what the player can answer without the check noticing.
	envelope.State.Pending.EventQueue[0].ChoiceIDs[0] = "tampered"
	if envelope.VerifyIntegrity() {
		t.Fatal("the integrity digest does not cover the queued candidate set")
	}
}

// ---------------------------------------------------------------------------
// State validation of the queue
// ---------------------------------------------------------------------------

func TestQueuedInstanceWithoutALedgerRowIsRejected(t *testing.T) {
	// The ledger is the authority on what has been raised. A queue entry with no
	// matching row means the document was edited outside the game, and resuming
	// from it would let an event escape its occurrence cap.
	state := readyState()
	state.Counters.WorldMonth = 1
	state.Pending.EventQueue = []QueuedEvent{{
		EventID: "EVT-GHOST", InstanceID: "EVT-GHOST#1", Priority: 10,
		RaisedAtWorldMonth: 1, ChoiceIDs: []string{"go"},
	}}

	report := ValidateState(state)
	if !report.Has(VErrDanglingRef) {
		t.Fatalf("expected %s for a queued instance with no ledger row, got codes %v",
			VErrDanglingRef, report.Codes())
	}
}

func TestQueuedInstanceWithAMatchingLedgerRowIsAccepted(t *testing.T) {
	// The control group: the rule above must not be rejecting every queue.
	state := readyState()
	state.Counters.WorldMonth = 1
	state.Pending.EventQueue = []QueuedEvent{{
		EventID: "EVT-OK", InstanceID: "EVT-OK#1", Priority: 10,
		RaisedAtWorldMonth: 1, ChoiceIDs: []string{"go"},
	}}
	state.World.RaisedEvents = []RaisedEvent{{
		EventID: "EVT-OK", InstanceID: "EVT-OK#1", WorldMonth: 1,
	}}
	state.World.EventInstanceSeq = 1

	if report := ValidateState(state); !report.OK() {
		t.Fatalf("a consistent queue must validate: %v", report.Error())
	}
}

func TestDigestCoversEventSchedulingState(t *testing.T) {
	// Control group first: if digestCovers is broken, every assertion below
	// would pass for the wrong reason.
	if !digestCovers(t, func(s *GameState) { s.Counters.WorldMonth++ }) {
		t.Fatal("digestCovers is not sensitive to the world month; the assertions below prove nothing")
	}

	covered := []struct {
		name   string
		mutate func(*GameState)
	}{
		{"event instance sequence", func(s *GameState) { s.World.EventInstanceSeq++ }},
		{"raised event ledger", func(s *GameState) {
			s.World.RaisedEvents = append(s.World.RaisedEvents, RaisedEvent{
				EventID: "EVT-X", InstanceID: "EVT-X#9", WorldMonth: 1,
			})
		}},
		{"consumed event ledger", func(s *GameState) {
			s.World.ConsumedEvents = append(s.World.ConsumedEvents, ConsumedEvent{
				EventID: "EVT-X", InstanceID: "EVT-X#1", ChoiceID: "go", WorldMonth: 1,
			})
		}},
		{"world flag", func(s *GameState) { s.World.Flags["tampered"] = true }},
		{"queued event", func(s *GameState) {
			s.Pending.EventQueue = append(s.Pending.EventQueue, QueuedEvent{
				EventID: "EVT-Q", InstanceID: "EVT-Q#1", ChoiceIDs: []string{"go"},
			})
		}},
		{"pending event", func(s *GameState) {
			s.Pending.Event = &PendingEvent{EventID: "EVT-P", InstanceID: "EVT-P#1", ChoiceIDs: []string{"go"}}
		}},
		{"breakthrough bonus", func(s *GameState) {
			s.Player.BreakthroughBonuses = map[string]int64{"demon_resist": 1}
		}},
		{"relation", func(s *GameState) {
			s.Player.Relations = append(s.Player.Relations, Relation{NPCID: "npc", Value: 1, Met: true})
		}},
	}

	for _, c := range covered {
		if !digestCovers(t, c.mutate) {
			t.Errorf("the integrity digest is not sensitive to %s; a tampered save would be "+
				"accepted and would change which events fire", c.name)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func answerPending(t *testing.T, e *Engine, actionID, choiceID string) {
	t.Helper()
	if r := answerPendingResult(t, e, actionID, choiceID); !r.OK {
		t.Fatalf("event choice %q rejected: %s", choiceID, r.Code)
	}
}

func answerPendingResult(t *testing.T, e *Engine, actionID, choiceID string) CommandResult {
	t.Helper()
	return e.Submit(Command{
		ActionID: actionID, SessionID: "s",
		ExpectedRevision: e.State().Revision, ViewToken: e.ViewToken(),
		Kind: KindEventChoice, Payload: Payload{ChoiceID: choiceID},
	})
}

func hasResultEvent(r CommandResult, kind, id string) bool {
	for _, ev := range r.Events {
		if ev.Kind == kind && ev.ID == id {
			return true
		}
	}
	return false
}

func relationValue(p *Player, npcID string) int {
	for _, rel := range p.Relations {
		if rel.NPCID == npcID {
			return rel.Value
		}
	}
	return 0
}
