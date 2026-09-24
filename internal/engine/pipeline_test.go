package engine

import "testing"

// readyState builds a minimal but valid READY-phase state: a living character
// with a sane lifespan and no pending sub-state.
func readyState() *GameState {
	return &GameState{
		SchemaVersion:  SchemaVersion,
		RulesVersion:   RulesVersion,
		ContentVersion: ContentVersion,
		GameID:         "game-test",
		BranchID:       "branch-main",
		Revision:       1,
		Phase:          PhaseReady,
		Counters:       Counters{},
		RNG:            NewRNGSet(SeedFromIdentity("game-test", "branch-main")).Snapshot(),
		Player: &Player{
			Identity:           Identity{Surname: "试", GivenName: "道者"},
			AgeMonths:          216, // 18 years
			Lifespan:           LifespanLedger{BaseYears: 80},
			Origin:             OriginCommoner,
			Path:               PathHuman,
			SpiritRoot:         RootTrue,
			Attributes:         Attributes{Aptitude: 10, Growth: map[string]int{}},
			PrimaryTechniqueID: "yellow_breath",
			HP:                 Vitals{Current: 40 * SCALE, Max: 40 * SCALE},
			MP:                 Vitals{Current: 20 * SCALE, Max: 20 * SCALE},
			Realm:              RealmQiRefining,
			Tier:               TierEarly,
			Resources:          map[Resource]int64{ResSpiritStones: 1000},
			Condition:          Condition{Mood: StartingMood},
			Relations:          []Relation{},
			Proficiencies:      map[string]int64{},
		},
		World: &World{
			NPCs:             map[string]NPC{},
			VisitedLocations: []string{"village"},
			CurrentLocation:  "village",
			Quests:           map[string]QuestState{},
			Flags:            map[string]bool{},
		},
		Idempotency: IdempotencyTable{
			Entries: map[string]IdempotencyEntry{},
			Limit:   DefaultIdempotencyLimit,
		},
	}
}

// newTestEngine wires an engine over a memory store.
//
// The catalogue is built locally rather than loaded from internal/content:
// content imports engine, so importing content here would be a cycle. The
// pipeline's tests only need a breakthrough entry to exist, not the shipped M1
// data, which internal/content's own tests cover.
func newTestEngine(t *testing.T, state *GameState) (*Engine, *MemoryStore) {
	t.Helper()
	return newTestEngineWithCatalogue(t, state, testCatalogue())
}

func newTestEngineWithCatalogue(t *testing.T, state *GameState, cat *Catalogue) (*Engine, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore(state)
	e := NewEngine(store, cat, state)
	return e, store
}

// testCatalogue is a minimal catalogue covering every realm/tier the pipeline
// tests walk through, so a breakthrough lookup succeeds instead of failing for
// a reason unrelated to the behaviour under test.
func testCatalogue() *Catalogue {
	cat := &Catalogue{
		Version: ContentVersion, M1Paths: []Path{PathHuman},
		CultivationMoodThreshold: ConfigValue{Provenance: ProvenanceDesignNote, Value: StartingMood, Note: "test mood threshold"},
		SpiritRoots:              []SpiritRootDefinition{{ID: RootTrue, NameZH: "真灵根", Multiplier: ConfigValue{Provenance: ProvenanceManuscript, Value: 13 * SCALE / 10}}},
		Techniques: []TechniqueDefinition{{
			ID: "yellow_breath", NameZH: "吐纳诀", Grade: GradeYellow, IsPrimary: true,
			GradeMultiplier: ConfigValue{Provenance: ProvenanceManuscript, Value: SCALE},
		}},
	}
	for i, realm := range RealmOrder {
		cat.Realms = append(cat.Realms, RealmDefinition{
			ID: realm, NameZH: string(realm), Order: i,
			MonthBase:     ConfigValue{Provenance: ProvenanceManuscript, Value: 10 * SCALE},
			TierThreshold: ConfigValue{Provenance: ProvenanceTrial, Value: 100 * SCALE, Note: "test threshold"},
		})
	}

	// One breakthrough per adjacent tier pair in the human path, so any
	// reasonable starting point has a defined transition.
	for i := 0; i < len(RealmOrder); i++ {
		for j := 0; j < len(TierOrder); j++ {
			// Minor advance within the realm.
			if j+1 < len(TierOrder) {
				cat.Breakthroughs = append(cat.Breakthroughs, BreakthroughDefinition{
					Path:      PathHuman,
					FromRealm: RealmOrder[i],
					FromTier:  TierOrder[j],
					ToRealm:   RealmOrder[i],
					ToTier:    TierOrder[j+1],
				})
			}
			// Major advance to the next realm's early tier.
			if j == len(TierOrder)-1 && i+1 < len(RealmOrder) {
				cat.Breakthroughs = append(cat.Breakthroughs, BreakthroughDefinition{
					Path:      PathHuman,
					FromRealm: RealmOrder[i],
					FromTier:  TierOrder[j],
					ToRealm:   RealmOrder[i+1],
					ToTier:    TierEarly,
				})
			}
		}
	}

	// Zero-effect fixture events. Tests that exercise month-end timing and the
	// idempotency rules set Pending.Event by hand, so the catalogue only needs
	// to contain the ids they name; the choices carry no cost and no effect
	// because those tests assert on *when* a month advances, not on what a
	// choice does. Weight is zero so no fixture event can win the monthly draw
	// and perturb a test that is not about event selection.
	for _, id := range []string{"EVT-001", "EVT-007", "EVT-009"} {
		cat.Events = append(cat.Events, EventDefinition{
			ID: id, NameZH: "测试事件", Scene: "test",
			Priority: ConfigValue{Provenance: ProvenanceDesignNote, Value: 10, Note: "test priority"},
			Weight:   ConfigValue{Provenance: ProvenanceDesignNote, Value: 0, Note: "fixture never drawn"},
			Choices: []EventChoice{
				{ID: "A", TextZH: "选项甲"},
				{ID: "B", TextZH: "选项乙"},
			},
		})
	}

	return cat
}

// cmd builds a command bound to the engine's current revision and view token,
// so tests exercise the behaviour under test rather than fighting the guards.
func cmd(e *Engine, actionID string, kind CommandKind) Command {
	return Command{
		ActionID:         actionID,
		SessionID:        "session-1",
		ExpectedRevision: e.State().Revision,
		ViewToken:        e.ViewToken(),
		Kind:             kind,
	}
}

// --- The acceptance criteria -------------------------------------------------

// TestSameSnapshotSameActionsSameResult is the headline determinism guarantee:
// two engines started from identical state and given identical action sequences
// must end in identical mechanical state.
func TestSameSnapshotSameActionsSameResult(t *testing.T) {
	run := func() *GameState {
		state := readyState()
		e, _ := newTestEngine(t, state)

		kinds := []CommandKind{
			KindCultivate, KindWait, KindCultivate, KindHeal, KindWait,
		}
		for i, k := range kinds {
			c := cmd(e, actionIDFor(i), k)
			r := e.Submit(c)
			if !r.OK {
				t.Fatalf("action %d (%s) was rejected: %s", i, k, r.Code)
			}
		}
		return e.State()
	}

	a := run()
	b := run()

	if a.Counters.WorldMonth != b.Counters.WorldMonth {
		t.Fatalf("world months diverged: %d vs %d", a.Counters.WorldMonth, b.Counters.WorldMonth)
	}
	if a.Counters.InteractionSeq != b.Counters.InteractionSeq {
		t.Fatalf("interaction seq diverged: %d vs %d", a.Counters.InteractionSeq, b.Counters.InteractionSeq)
	}
	if a.Player.AgeMonths != b.Player.AgeMonths {
		t.Fatalf("ages diverged: %d vs %d", a.Player.AgeMonths, b.Player.AgeMonths)
	}
	if a.Revision != b.Revision {
		t.Fatalf("revisions diverged: %d vs %d", a.Revision, b.Revision)
	}

	// The random state must also match: a replay that diverges in its RNG is
	// not a replay, even if the counters happen to agree today.
	for name, sa := range a.RNG.Streams {
		sb := b.RNG.Streams[name]
		if sa.Counter != sb.Counter || sa.State != sb.State {
			t.Fatalf("random stream %s diverged: %+v vs %+v", name, sa, sb)
		}
	}
}

// actionIDFor makes readable, distinct action ids.
func actionIDFor(i int) string {
	return "act-" + string(rune('a'+i))
}

// TestRepeatedQueryDoesNotAdvanceAnything is the "looking at the panel is free"
// rule. A player who opens the status screen twenty times must see the world
// exactly where they left it.
func TestRepeatedQueryDoesNotAdvanceAnything(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	before := CloneGameState(e.State())
	token := e.ViewToken()

	for i := 0; i < 20; i++ {
		c := Command{
			ActionID:         "query-" + string(rune('a'+i)),
			SessionID:        "session-1",
			ExpectedRevision: e.State().Revision,
			// A query must be issued against the live panel. Since a query does
			// not rotate the token, the same token stays valid, which is the
			// behaviour that makes "scroll around freely" work.
			ViewToken: token,
			Kind:      KindQuery,
			Payload:   Payload{QueryKind: QueryStatus},
		}
		r := e.Submit(c)
		if !r.OK {
			t.Fatalf("query %d rejected: %s", i, r.Code)
		}
	}

	after := e.State()

	if after.Counters.WorldMonth != before.Counters.WorldMonth {
		t.Fatalf("queries advanced the world month: %d -> %d", before.Counters.WorldMonth, after.Counters.WorldMonth)
	}
	if after.Counters.InteractionSeq != before.Counters.InteractionSeq {
		t.Fatalf("queries advanced the interaction sequence: %d -> %d", before.Counters.InteractionSeq, after.Counters.InteractionSeq)
	}
	if after.Revision != before.Revision {
		t.Fatalf("queries advanced the revision: %d -> %d", before.Revision, after.Revision)
	}
	if after.Player.AgeMonths != before.Player.AgeMonths {
		t.Fatalf("queries aged the character: %d -> %d", before.Player.AgeMonths, after.Player.AgeMonths)
	}

	for name, sb := range before.RNG.Streams {
		sa := after.RNG.Streams[name]
		if sa.Counter != sb.Counter {
			t.Fatalf("query drew from random stream %s: %d -> %d", name, sb.Counter, sa.Counter)
		}
	}
}

// TestQueriesDoNotInvalidateThePanel is the corollary: if a query rotated the
// view token, the second glance would be rejected as stale.
func TestQueriesDoNotInvalidateThePanel(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	token := e.ViewToken()

	first := Command{ActionID: "q1", SessionID: "s", ExpectedRevision: e.State().Revision, ViewToken: token, Kind: KindQuery}
	if r := e.Submit(first); !r.OK {
		t.Fatalf("first query rejected: %s", r.Code)
	}

	if got := e.ViewToken(); got != token {
		t.Fatalf("a query rotated the view token: %q -> %q", token, got)
	}

	second := Command{ActionID: "q2", SessionID: "s", ExpectedRevision: e.State().Revision, ViewToken: token, Kind: KindQuery}
	if r := e.Submit(second); !r.OK {
		t.Fatalf("second query rejected because the token changed: %s", r.Code)
	}
}

// TestDuplicateActionReturnsStoredResultAndChangesNothing is the idempotency
// guarantee: resending a click must not apply it twice.
func TestDuplicateActionReturnsStoredResultAndChangesNothing(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	c := cmd(e, "double-click", KindCultivate)
	first := e.Submit(c)
	if !first.OK {
		t.Fatalf("first submission rejected: %s", first.Code)
	}

	afterFirst := CloneGameState(e.State())

	// Resend the identical command.
	second := e.Submit(c)

	if !second.OK {
		t.Fatalf("resend rejected: %s", second.Code)
	}
	if second.ActionID != first.ActionID {
		t.Fatalf("resend returned a different action id: %q vs %q", second.ActionID, first.ActionID)
	}
	if second.RevisionAfter != first.RevisionAfter {
		t.Fatalf("resend reported a different revision: %d vs %d", second.RevisionAfter, first.RevisionAfter)
	}

	afterSecond := e.State()

	if afterSecond.Counters.WorldMonth != afterFirst.Counters.WorldMonth {
		t.Fatalf("resend advanced the world month: %d -> %d", afterFirst.Counters.WorldMonth, afterSecond.Counters.WorldMonth)
	}
	if afterSecond.Counters.InteractionSeq != afterFirst.Counters.InteractionSeq {
		t.Fatalf("resend advanced the interaction sequence: %d -> %d", afterFirst.Counters.InteractionSeq, afterSecond.Counters.InteractionSeq)
	}
	if afterSecond.Player.AgeMonths != afterFirst.Player.AgeMonths {
		t.Fatalf("resend aged the character: %d -> %d", afterFirst.Player.AgeMonths, afterSecond.Player.AgeMonths)
	}

	// The month must have been charged exactly once in total.
	if afterSecond.Counters.WorldMonth != 1 {
		t.Fatalf("world month = %d after one action submitted twice, want 1", afterSecond.Counters.WorldMonth)
	}
}

// TestSameActionIDDifferentBodyIsRejected is the conflict rule from the design:
// reusing an id with a different payload must be refused, not silently accepted.
func TestSameActionIDDifferentBodyIsRejected(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	c := cmd(e, "reused-id", KindCultivate)
	if r := e.Submit(c); !r.OK {
		t.Fatalf("first submission rejected: %s", r.Code)
	}

	afterFirst := CloneGameState(e.State())

	// Same id, different kind.
	conflicting := c
	conflicting.Kind = KindWait
	conflicting.ExpectedRevision = e.State().Revision
	conflicting.ViewToken = e.ViewToken()

	r := e.Submit(conflicting)

	if r.OK {
		t.Fatal("reusing an action id with a different body must be rejected")
	}
	if r.Code != ErrActionIdConflict {
		t.Fatalf("code = %s, want %s", r.Code, ErrActionIdConflict)
	}

	after := e.State()
	if after.Counters.WorldMonth != afterFirst.Counters.WorldMonth {
		t.Fatalf("the rejected conflict still advanced the world: %d -> %d",
			afterFirst.Counters.WorldMonth, after.Counters.WorldMonth)
	}
}

// TestStaleRevisionIsRejected covers the two-screens-open case: an old panel's
// keypress must not land on the new state.
func TestStaleRevisionIsRejected(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	// Commit one action so the revision moves.
	if r := e.Submit(cmd(e, "a1", KindWait)); !r.OK {
		t.Fatalf("setup action rejected: %s", r.Code)
	}

	// A command still carrying the original revision.
	stale := Command{
		ActionID:         "a2",
		SessionID:        "session-1",
		ExpectedRevision: 1,
		ViewToken:        e.ViewToken(),
		Kind:             KindWait,
	}

	before := CloneGameState(e.State())
	r := e.Submit(stale)

	if r.OK {
		t.Fatal("a command issued against a stale revision must be rejected")
	}
	if r.Code != ErrRevisionMismatch {
		t.Fatalf("code = %s, want %s", r.Code, ErrRevisionMismatch)
	}

	if e.State().Counters.WorldMonth != before.Counters.WorldMonth {
		t.Fatal("a stale command advanced the world")
	}
}

// TestStaleViewTokenIsRejected covers the stale-panel case even when the
// revision happens to match.
func TestStaleViewTokenIsRejected(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	c := cmd(e, "a1", KindWait)
	c.ViewToken = "vt:999:1" // a token from no panel this engine issued

	before := CloneGameState(e.State())
	r := e.Submit(c)

	if r.OK {
		t.Fatal("a command carrying a foreign view token must be rejected")
	}
	if r.Code != ErrStaleViewToken {
		t.Fatalf("code = %s, want %s", r.Code, ErrStaleViewToken)
	}
	if e.State().Counters.WorldMonth != before.Counters.WorldMonth {
		t.Fatal("a stale-token command advanced the world")
	}
}

// TestCancelledConfirmationChargesNothing is the "cancel does not cost" rule.
// The breakthrough needs a ticket; submitting without one must be refused before
// anything is spent or drawn.
func TestCancelledConfirmationChargesNothing(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	before := CloneGameState(e.State())

	// No confirmation token at all.
	c := cmd(e, "bt-1", KindBreakthrough)
	c.TargetID = "qi-refining-early-to-middle"

	r := e.Submit(c)

	if r.OK {
		t.Fatal("a breakthrough without confirmation must be rejected")
	}
	if r.Code != ErrConfirmationNeeded {
		t.Fatalf("code = %s, want %s", r.Code, ErrConfirmationNeeded)
	}

	after := e.State()

	// Nothing may have been spent.
	if len(after.Player.Inventory.Stacks) != len(before.Player.Inventory.Stacks) {
		t.Fatal("an unconfirmed breakthrough changed the inventory")
	}
	if after.Player.Resources[ResSpiritStones] != before.Player.Resources[ResSpiritStones] {
		t.Fatal("an unconfirmed breakthrough spent resources")
	}
	if after.Counters.WorldMonth != before.Counters.WorldMonth {
		t.Fatal("an unconfirmed breakthrough advanced the world month")
	}
	if after.Player.AgeMonths != before.Player.AgeMonths {
		t.Fatal("an unconfirmed breakthrough aged the character")
	}

	// And no random stream may have been drawn from: an unconfirmed proposal
	// must not pre-roll its outcome.
	for name, sb := range before.RNG.Streams {
		sa := after.RNG.Streams[name]
		if sa.Counter != sb.Counter {
			t.Fatalf("an unconfirmed breakthrough drew from stream %s (%d -> %d)", name, sb.Counter, sa.Counter)
		}
	}
}

// TestExpiredConfirmationIsRejected covers a ticket issued against a revision
// that has since moved on. The previewed cost can no longer be trusted.
func TestExpiredConfirmationIsRejected(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	// Prepare an outstanding proposal tied to the current revision.
	e.State().Pending.Confirmation = &PendingConfirmation{
		Token:             "ticket-1",
		Kind:              KindBreakthrough,
		TargetID:          "some-target",
		ExpiresAtRevision: e.State().Revision,
	}

	// The world moves on.
	if r := e.Submit(cmd(e, "mover", KindWait)); !r.OK {
		t.Fatalf("setup action rejected: %s", r.Code)
	}

	c := cmd(e, "bt-2", KindBreakthrough)
	c.TargetID = "some-target"
	c.ConfirmationToken = "ticket-1"

	r := e.Submit(c)

	if r.OK {
		t.Fatal("a confirmation issued against an older revision must be rejected")
	}
	if r.Code != ErrConfirmationExpired {
		t.Fatalf("code = %s, want %s", r.Code, ErrConfirmationExpired)
	}
}

// TestConfirmationMustMatchKindAndTarget prevents confirming a cheap proposal
// and applying an expensive action with the same ticket.
func TestConfirmationMustMatchKindAndTarget(t *testing.T) {
	cases := []struct {
		name    string
		pending PendingConfirmation
		reqKind CommandKind
		reqTgt  string
	}{
		{
			name:    "wrong kind",
			pending: PendingConfirmation{Token: "t", Kind: KindTrade, TargetID: "x", ExpiresAtRevision: 1},
			reqKind: KindBreakthrough,
			reqTgt:  "x",
		},
		{
			name:    "wrong target",
			pending: PendingConfirmation{Token: "t", Kind: KindBreakthrough, TargetID: "cheap", ExpiresAtRevision: 1},
			reqKind: KindBreakthrough,
			reqTgt:  "expensive",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := readyState()
			e, _ := newTestEngine(t, state)

			e.State().Pending.Confirmation = &tc.pending

			c := cmd(e, "ath-1", tc.reqKind)
			c.TargetID = tc.reqTgt
			c.ConfirmationToken = "t"

			r := e.Submit(c)

			if r.OK {
				t.Fatalf("a mismatched confirmation must be rejected")
			}
			if r.Code != ErrConfirmationExpired {
				t.Fatalf("code = %s, want %s", r.Code, ErrConfirmationExpired)
			}
		})
	}
}

// TestWorldDoesNotAdvanceWithoutInput is the "no input, no time" rule stated
// positively: submitting nothing leaves the world frozen. The real-time version
// of this rule is structural (the engine never reads a clock); this test pins
// the mechanical half.
func TestWorldDoesNotAdvanceWithoutInput(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	before := CloneGameState(e.State())

	// Simulate "the player walked away": nothing is submitted.
	after := e.State()

	if after.Counters.WorldMonth != before.Counters.WorldMonth {
		t.Fatal("the world advanced with no input")
	}
	if after.Player.AgeMonths != before.Player.AgeMonths {
		t.Fatal("the character aged with no input")
	}
}

// TestMonthCostMatrixIsEnforced checks every kind's actual month cost against
// the design's 13.1 table, by running it rather than by reading the table.
func TestMonthCostMatrixIsEnforced(t *testing.T) {
	cases := []struct {
		kind      CommandKind
		monthCost int
	}{
		{KindQuery, 0},
		{KindTravel, 0},
		{KindTrade, 0},
		{KindUseItem, 0},
		{KindCultivate, 1},
		{KindHeal, 1},
		{KindWait, 1},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			state := readyState()
			e, _ := newTestEngine(t, state)

			c := cmd(e, "act-1", tc.kind)
			if tc.kind == KindTrade {
				// Trade requires a confirmation ticket; give it one so the test
				// measures the month cost rather than the confirmation guard.
				e.State().Pending.Confirmation = &PendingConfirmation{
					Token:             "t",
					Kind:              KindTrade,
					TargetID:          "",
					ExpiresAtRevision: e.State().Revision,
				}
				c.ConfirmationToken = "t"
			}

			r := e.Submit(c)
			if !r.OK {
				t.Fatalf("%s rejected: %s", tc.kind, r.Code)
			}

			if r.MonthCostApplied != tc.monthCost {
				t.Fatalf("%s applied %d months, want %d", tc.kind, r.MonthCostApplied, tc.monthCost)
			}

			if got := e.State().Counters.WorldMonth; got != int64(tc.monthCost) {
				t.Fatalf("%s moved the world to month %d, want %d", tc.kind, got, tc.monthCost)
			}
		})
	}
}

// TestInteractionSeqIncreasesOnlyForCommittedActions pins the third counter's
// rule: it counts committed domain actions, not queries and not duplicates.
func TestInteractionSeqIncreasesOnlyForCommittedActions(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	// Two real actions.
	for i := 0; i < 2; i++ {
		if r := e.Submit(cmd(e, "real-"+string(rune('a'+i)), KindWait)); !r.OK {
			t.Fatalf("action rejected: %s", r.Code)
		}
	}
	if got := e.State().Counters.InteractionSeq; got != 2 {
		t.Fatalf("interaction seq = %d after two actions, want 2", got)
	}

	// A query must not count.
	q := cmd(e, "q-1", KindQuery)
	q.Payload.QueryKind = QueryStatus
	if r := e.Submit(q); !r.OK {
		t.Fatalf("query rejected: %s", r.Code)
	}
	if got := e.State().Counters.InteractionSeq; got != 2 {
		t.Fatalf("a query moved the interaction seq to %d, want 2", got)
	}

	// A rejected action must not count.
	bad := cmd(e, "bad-1", KindBreakthrough) // missing confirmation
	if r := e.Submit(bad); r.OK {
		t.Fatal("expected the unconfirmed breakthrough to be rejected")
	}
	if got := e.State().Counters.InteractionSeq; got != 2 {
		t.Fatalf("a rejected action moved the interaction seq to %d, want 2", got)
	}
}

// TestRejectedCommandLeavesStateByteIdentical is the strongest form of the
// no-side-effect rule. Every rejection path must be a true no-op.
func TestRejectedCommandLeavesStateByteIdentical(t *testing.T) {
	rejections := []struct {
		name  string
		build func(e *Engine) Command
	}{
		{
			name: "unknown kind",
			build: func(e *Engine) Command {
				c := cmd(e, "r1", KindQuery)
				c.Kind = "NOT_A_KIND"
				return c
			},
		},
		{
			name: "stale revision",
			build: func(e *Engine) Command {
				c := cmd(e, "r2", KindWait)
				c.ExpectedRevision = 999
				return c
			},
		},
		{
			name: "stale view token",
			build: func(e *Engine) Command {
				c := cmd(e, "r3", KindWait)
				c.ViewToken = "bogus"
				return c
			},
		},
		{
			name: "confirmation required",
			build: func(e *Engine) Command {
				return cmd(e, "r4", KindBreakthrough)
			},
		},
	}

	for _, tc := range rejections {
		t.Run(tc.name, func(t *testing.T) {
			state := readyState()
			e, store := newTestEngine(t, state)

			before := CloneGameState(e.State())
			committedBefore := store.Committed()

			r := e.Submit(tc.build(e))
			if r.OK {
				t.Fatalf("expected rejection, got success")
			}

			// Compare the mechanical content that the digest covers.
			if got, want := stateDigest(e.State()), stateDigest(before); got != want {
				t.Fatalf("a rejected command changed the state digest:\n got %s\nwant %s", got, want)
			}
			if got, want := stateDigest(store.Committed()), stateDigest(committedBefore); got != want {
				t.Fatalf("a rejected command reached the store:\n got %s\nwant %s", got, want)
			}
		})
	}
}

// stateDigest renders a comparable digest of everything a rejected command must
// not have touched.
func stateDigest(s *GameState) string {
	if s == nil {
		return "<nil>"
	}
	env := &SaveEnvelope{
		EnvelopeVersion: EnvelopeVersion,
		SchemaVersion:   s.SchemaVersion,
		RulesVersion:    s.RulesVersion,
		ContentVersion:  s.ContentVersion,
		GameID:          s.GameID,
		BranchID:        s.BranchID,
		Revision:        s.Revision,
		State:           *s,
	}
	return env.CanonicalString()
}

// digestCovers reports whether the canonical digest is sensitive to a single
// field, by mutating a throwaway clone and comparing digests.
//
// It exists because the first version of the effect-expiry test asserted digest
// stability on a field the digest does not actually cover, and passed its own
// premise for the wrong reason. Any test that reasons about digest coverage
// must prove the coverage first; this helper makes that proof a one-liner.
func digestCovers(t *testing.T, mutate func(*GameState)) bool {
	t.Helper()
	base := readyState()
	changed := CloneGameState(base)
	mutate(changed)
	return stateDigest(base) != stateDigest(changed)
}

// TestSaveFailureIsNotReportedAsSuccess implements the design's rule that a
// failed save must never be shown as saved.
func TestSaveFailureIsNotReportedAsSuccess(t *testing.T) {
	state := readyState()
	e, store := newTestEngine(t, state)

	store.FailNextCommit()

	before := CloneGameState(e.State())
	r := e.Submit(cmd(e, "will-fail", KindWait))

	if r.OK {
		t.Fatal("a command whose save failed must not report success")
	}
	if r.SaveState != SaveFailed {
		t.Fatalf("save state = %q, want %q", r.SaveState, SaveFailed)
	}

	// The change must have been dropped entirely, not kept in memory.
	if got, want := stateDigest(e.State()), stateDigest(before); got != want {
		t.Fatal("a save failure still left the change applied in memory")
	}
}

// TestSaveFailureDoesNotConsumeTheActionID is important for recoverability: if
// the failed attempt recorded its action id, the player could never retry with
// the same id.
func TestSaveFailureDoesNotConsumeTheActionID(t *testing.T) {
	state := readyState()
	e, store := newTestEngine(t, state)

	store.FailNextCommit()
	if r := e.Submit(cmd(e, "retry-me", KindWait)); r.OK {
		t.Fatal("the first attempt should have failed")
	}

	// Retry with the same id now that the store works.
	c := cmd(e, "retry-me", KindWait)
	r := e.Submit(c)

	if !r.OK {
		t.Fatalf("a retry after a save failure was refused: %s", r.Code)
	}
	if r.Code == ErrActionIdConflict {
		t.Fatal("a failed attempt must not occupy its action id")
	}
	if e.State().Counters.WorldMonth != 1 {
		t.Fatalf("world month = %d after a successful retry, want 1", e.State().Counters.WorldMonth)
	}
}

// TestPhaseGateRejectsWorldActionsWhenEnded covers the death rule: a dead
// character accepts no world action.
func TestPhaseGateRejectsWorldActionsWhenEnded(t *testing.T) {
	state := readyState()
	state.Phase = PhaseEnded
	state.Player.Ended = true
	state.Player.EndCause = EndCause{Code: EndCodeLifespan, WorldMonth: 100}

	e, _ := newTestEngine(t, state)

	before := CloneGameState(e.State())

	for _, kind := range []CommandKind{KindCultivate, KindWait, KindTravel, KindHeal} {
		c := cmd(e, "after-death-"+string(kind), kind)
		r := e.Submit(c)

		if r.OK {
			t.Fatalf("%s was accepted after death", kind)
		}
		if r.Code != ErrBadPhase {
			t.Fatalf("%s after death returned %s, want %s", kind, r.Code, ErrBadPhase)
		}
	}

	// A query must still work: the review screen has to be reachable.
	q := cmd(e, "review", KindQuery)
	q.Payload.QueryKind = QueryLog
	if r := e.Submit(q); !r.OK {
		t.Fatalf("querying the log after death was refused: %s", r.Code)
	}

	if got, want := stateDigest(e.State()), stateDigest(before); got != want {
		t.Fatal("a post-death world action changed the state")
	}
}

// TestLifespanDeathEndsTheGame checks the terminal boundary is actually reached
// and recorded, rather than the character silently living past their ceiling.
func TestLifespanDeathEndsTheGame(t *testing.T) {
	state := readyState()
	// One month short of the 960-month ceiling.
	state.Player.AgeMonths = 959
	e, _ := newTestEngine(t, state)

	r := e.Submit(cmd(e, "final-month", KindWait))
	if !r.OK {
		t.Fatalf("the final month was rejected: %s", r.Code)
	}

	if !e.State().Player.Ended {
		t.Fatal("reaching the lifespan ceiling must end the character")
	}
	if e.State().Phase != PhaseEnded {
		t.Fatalf("phase = %s, want %s", e.State().Phase, PhaseEnded)
	}
	if e.State().Player.EndCause.Code != EndCodeLifespan {
		t.Fatalf("end cause = %q, want %q", e.State().Player.EndCause.Code, EndCodeLifespan)
	}

	// The month must still have advanced exactly once. The design is explicit:
	// "月末寿尽则本月已推进一次".
	if e.State().Counters.WorldMonth != 1 {
		t.Fatalf("world month = %d at death, want 1", e.State().Counters.WorldMonth)
	}
	if e.State().Player.AgeMonths != 960 {
		t.Fatalf("age = %d at death, want 960", e.State().Player.AgeMonths)
	}
}

// TestNestedActionCannotStartWhileMonthActionOpen implements design 13.2 step 3.
func TestNestedActionCannotStartWhileMonthActionOpen(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	// Leave a parent action open, as a quest run interrupted by combat would.
	e.State().Pending.MonthAction = &ActiveMonthAction{
		ActionID:          "open-parent",
		Kind:              KindQuestRun,
		MonthCharged:      false,
		SettlementPending: true,
	}

	for _, kind := range []CommandKind{KindCultivate, KindWait, KindHeal} {
		c := cmd(e, "nested-"+string(kind), kind)
		r := e.Submit(c)

		if r.OK {
			t.Fatalf("%s started while another month action was open", kind)
		}
		if r.Code != ErrPreconditionUnmet {
			t.Fatalf("%s returned %s, want %s", kind, r.Code, ErrPreconditionUnmet)
		}
	}
}

// TestSettlementChargesTheMonthExactlyOnce is the guard that stops a resumed
// session from charging the same month twice.
func TestSettlementChargesTheMonthExactlyOnce(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	// A parent action whose month has already been charged, as a reload would
	// find after the month had settled but before the slot was cleared.
	e.State().Pending.MonthAction = &ActiveMonthAction{
		ActionID:     "already-charged",
		Kind:         KindQuestRun,
		MonthCharged: true,
	}

	before := e.State().Counters.WorldMonth
	beforeAge := e.State().Player.AgeMonths

	// Answer a pending event, which triggers settlement of the parent.
	e.State().Pending.Event = &PendingEvent{
		EventID:    "EVT-001",
		InstanceID: "inst-1",
		ChoiceIDs:  []string{"A"},
	}
	e.State().Phase = PhaseEventPending

	c := Command{
		ActionID:         "answer-1",
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

	if e.State().Counters.WorldMonth != before {
		t.Fatalf("an already-charged parent advanced the month again: %d -> %d",
			before, e.State().Counters.WorldMonth)
	}
	if e.State().Player.AgeMonths != beforeAge {
		t.Fatalf("an already-charged parent aged the character again: %d -> %d",
			beforeAge, e.State().Player.AgeMonths)
	}
	if r.MonthCostApplied != 0 {
		t.Fatalf("month cost applied = %d, want 0", r.MonthCostApplied)
	}
	if e.State().Pending.MonthAction != nil {
		t.Fatal("the settled parent action was not cleared")
	}
}

// TestEventChoiceRejectsUnfrozenOption is the anti-re-roll rule: only the
// candidates frozen when the event was raised may be chosen.
func TestEventChoiceRejectsUnfrozenOption(t *testing.T) {
	state := readyState()
	state.Phase = PhaseEventPending
	state.Pending.Event = &PendingEvent{
		EventID:    "EVT-001",
		InstanceID: "inst-1",
		ChoiceIDs:  []string{"A", "B"},
	}
	e, _ := newTestEngine(t, state)

	before := CloneGameState(e.State())

	c := cmd(e, "cheat", KindEventChoice)
	c.Payload.ChoiceID = "C" // not a frozen candidate

	r := e.Submit(c)

	if r.OK {
		t.Fatal("an option outside the frozen candidate set must be rejected")
	}
	if r.Code != ErrUnknownTarget {
		t.Fatalf("code = %s, want %s", r.Code, ErrUnknownTarget)
	}
	if got, want := stateDigest(e.State()), stateDigest(before); got != want {
		t.Fatal("the rejected choice mutated the state")
	}
}

// TestCombatRoundDoesNotAdvanceTheWorldMonth pins the design's separation of
// combat rounds from world time.
func TestCombatRoundDoesNotAdvanceTheWorldMonth(t *testing.T) {
	state := readyState()
	state.Phase = PhaseCombatPending
	state.Pending.Combat = &PendingCombat{
		CombatID:       "combat-1",
		ParentActionID: "parent-1",
		Round:          1,
		AwaitingPlayer: true,
		Resolution:     CombatOngoing,
		AllyState:      combatantFromPlayer(state.Player),
	}
	state.Pending.MonthAction = &ActiveMonthAction{
		ActionID:          "parent-1",
		Kind:              KindQuestRun,
		MonthCharged:      false,
		SettlementPending: true,
	}

	e, _ := newTestEngine(t, state)

	before := e.State().Counters.WorldMonth
	beforeAge := e.State().Player.AgeMonths

	for i := 0; i < 5; i++ {
		c := cmd(e, "round-"+string(rune('a'+i)), KindCombatAction)
		c.Payload.SkillID = "basic-attack"

		if r := e.Submit(c); !r.OK {
			t.Fatalf("combat round %d rejected: %s", i, r.Code)
		}
	}

	if e.State().Counters.WorldMonth != before {
		t.Fatalf("combat advanced the world month: %d -> %d", before, e.State().Counters.WorldMonth)
	}
	if e.State().Player.AgeMonths != beforeAge {
		t.Fatalf("combat aged the character: %d -> %d", beforeAge, e.State().Player.AgeMonths)
	}
	if e.State().Counters.CombatRound != 5 {
		t.Fatalf("combat round = %d, want 5", e.State().Counters.CombatRound)
	}
}

// TestCombatRoundCannotBeResolvedTwice covers the double-submission defence
// beyond the idempotency table.
func TestCombatRoundCannotBeResolvedTwice(t *testing.T) {
	state := readyState()
	state.Phase = PhaseCombatPending
	state.Pending.Combat = &PendingCombat{
		CombatID:       "combat-1",
		Round:          1,
		AwaitingPlayer: false, // the round is already resolved
		Resolution:     CombatOngoing,
		AllyState:      combatantFromPlayer(state.Player),
	}
	e, _ := newTestEngine(t, state)

	c := cmd(e, "late-click", KindCombatAction)
	r := e.Submit(c)

	if r.OK {
		t.Fatal("acting in an already-resolved round must be rejected")
	}
	if r.Code != ErrDuplicateAction {
		t.Fatalf("code = %s, want %s", r.Code, ErrDuplicateAction)
	}
}

// TestPhaseGateTable covers design 13.3: each phase permits only its own kind.
func TestPhaseGateTable(t *testing.T) {
	cases := []struct {
		phase   Phase
		allowed CommandKind
		blocked CommandKind
	}{
		{PhaseCreation, KindCreateConfirm, KindCultivate},
		{PhaseEventPending, KindEventChoice, KindCultivate},
		{PhaseCombatPending, KindCombatAction, KindCultivate},
		{PhaseBreakthroughPending, KindEventChoice, KindCultivate},
	}

	for _, tc := range cases {
		t.Run(string(tc.phase), func(t *testing.T) {
			state := readyState()
			state.Phase = tc.phase
			populatePendingFor(state)
			e, _ := newTestEngine(t, state)

			blocked := cmd(e, "blocked", tc.blocked)
			r := e.Submit(blocked)
			if r.OK {
				t.Fatalf("%s was accepted during %s", tc.blocked, tc.phase)
			}
			if r.Code != ErrBadPhase {
				t.Fatalf("%s during %s returned %s, want %s", tc.blocked, tc.phase, r.Code, ErrBadPhase)
			}
		})
	}
}

// populatePendingFor fills the sub-state that a phase requires, so the phase
// check is exercised rather than a missing-sub-state check.
func populatePendingFor(s *GameState) {
	switch s.Phase {
	case PhaseCreation:
		s.Pending.Creation = &CreationDraft{Confirmed: true}
	case PhaseEventPending:
		s.Pending.Event = &PendingEvent{EventID: "E", InstanceID: "i", ChoiceIDs: []string{"A"}}
	case PhaseCombatPending:
		s.Pending.Combat = &PendingCombat{
			CombatID: "c", Round: 1, AwaitingPlayer: true,
			Resolution: CombatOngoing,
			AllyState:  combatantFromPlayer(s.Player),
		}
	case PhaseBreakthroughPending:
		s.Pending.Breakthrough = &PendingBreakthrough{
			BreakthroughID: "b", ParentActionID: "p", NodeID: "n",
			ChoiceIDs: []string{"endure"},
		}
		s.Pending.MonthAction = &ActiveMonthAction{ActionID: "p", Kind: KindBreakthrough}
	}
}

// TestSubmittingFlagIsClearedAfterSuccessAndFailure guards the deferred-quit
// mechanism: a stuck flag would make the engine refuse to shut down.
func TestSubmittingFlagIsClearedAfterSuccessAndFailure(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	if e.Submitting() {
		t.Fatal("a fresh engine reports itself as submitting")
	}

	if r := e.Submit(cmd(e, "ok", KindWait)); !r.OK {
		t.Fatalf("action rejected: %s", r.Code)
	}
	if e.Submitting() {
		t.Fatal("the submitting flag was left set after a success")
	}

	bad := cmd(e, "bad", KindQuery)
	bad.Kind = "NOT_A_KIND"
	if r := e.Submit(bad); r.OK {
		t.Fatal("expected the invalid kind to be rejected")
	}
	if e.Submitting() {
		t.Fatal("the submitting flag was left set after a rejection")
	}
}

// TestCloneIsolationPreventsCrossContamination verifies the clone really is a
// deep copy. A shallow copy would let a rejected command corrupt the live
// state through a shared map.
func TestCloneIsolationPreventsCrossContamination(t *testing.T) {
	original := readyState()
	clone := CloneGameState(original)

	// Mutate every map and slice on the clone.
	clone.Counters.WorldMonth = 99
	clone.RNG.Streams[StreamWorld] = RNGStream{State: 1, Counter: 1}
	clone.Player.AgeMonths = 999
	clone.Player.HP.Current = 1
	clone.Player.Resources[ResSpiritStones] = 0
	clone.Player.Lifespan.Bonuses = append(clone.Player.Lifespan.Bonuses, LifespanBonus{ID: "x", Years: 1})
	clone.Player.Inventory.Stacks = append(clone.Player.Inventory.Stacks, ItemStack{ItemID: "x", Quantity: 1})
	clone.Player.Condition.Effects = append(clone.Player.Condition.Effects, TimedEffect{ID: "poison", MonthsRemaining: 1})
	clone.World.CurrentLocation = "elsewhere"
	clone.World.NPCs["npc-1"] = NPC{ID: "npc-1", IsAlive: true}
	clone.World.Quests["q-1"] = QuestState{QuestID: "q-1"}
	clone.World.Flags["flag"] = true
	clone.Idempotency.Entries["x"] = IdempotencyEntry{ActionID: "x"}

	if original.Counters.WorldMonth != 0 {
		t.Fatal("counters were shared")
	}
	if original.RNG.Streams[StreamWorld].Counter != 0 {
		t.Fatal("rng streams were shared")
	}
	if original.Player.AgeMonths != 216 {
		t.Fatal("player fields were shared")
	}
	if original.Player.Resources[ResSpiritStones] != 1000 {
		t.Fatal("the resources map was shared")
	}
	if len(original.Player.Lifespan.Bonuses) != 0 {
		t.Fatal("the lifespan bonus slice was shared")
	}
	if len(original.Player.Inventory.Stacks) != 0 {
		t.Fatal("the inventory slice was shared")
	}
	if len(original.Player.Condition.Effects) != 0 {
		t.Fatal("the condition effects slice was shared")
	}
	if original.World.CurrentLocation != "village" {
		t.Fatal("world fields were shared")
	}
	if len(original.World.NPCs) != 0 {
		t.Fatal("the NPC map was shared")
	}
	if len(original.World.Quests) != 0 {
		t.Fatal("the quest map was shared")
	}
	if len(original.World.Flags) != 0 {
		t.Fatal("the flags map was shared")
	}
	if len(original.Idempotency.Entries) != 0 {
		t.Fatal("the idempotency map was shared")
	}
}

// TestIdempotencyTableIsBounded keeps the ledger from growing without limit.
func TestIdempotencyTableIsBounded(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	limit := DefaultIdempotencyLimit

	// Submit more actions than the limit. Each needs a fresh revision, so use
	// separate engines sharing one state to avoid fighting the guards.
	for i := 0; i < limit+20; i++ {
		e.State().Revision = uint64(i + 1)
		c := Command{
			ActionID:         "act-" + intToStr(i),
			SessionID:        "s",
			ExpectedRevision: e.State().Revision,
			ViewToken:        e.ViewToken(),
			Kind:             KindWait,
		}
		// Keep the token current without going through a commit.
		e.rotateViewToken()
		c.ViewToken = e.ViewToken()

		if r := e.Submit(c); !r.OK {
			t.Fatalf("action %d rejected: %s", i, r.Code)
		}
	}

	table := e.State().Idempotency
	if len(table.Entries) > limit {
		t.Fatalf("idempotency table holds %d entries, limit is %d", len(table.Entries), limit)
	}
	if len(table.Order) > limit {
		t.Fatalf("idempotency order holds %d entries, limit is %d", len(table.Order), limit)
	}
}

// intToStr renders a small non-negative int without importing strconv.
func intToStr(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// TestViewTokenChangesAfterACommittedChange is what makes a stale key
// recognisable: after the world moves, the old token must no longer be accepted.
func TestViewTokenChangesAfterACommittedChange(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	oldToken := e.ViewToken()

	if r := e.Submit(cmd(e, "mover", KindWait)); !r.OK {
		t.Fatalf("action rejected: %s", r.Code)
	}

	newToken := e.ViewToken()
	if newToken == oldToken {
		t.Fatal("the view token did not change after a committed change")
	}

	// The old token must now be refused.
	stale := Command{
		ActionID:         "stale",
		SessionID:        "s",
		ExpectedRevision: e.State().Revision,
		ViewToken:        oldToken,
		Kind:             KindWait,
	}
	if r := e.Submit(stale); r.OK {
		t.Fatal("the previous panel's token was still accepted")
	}
}
