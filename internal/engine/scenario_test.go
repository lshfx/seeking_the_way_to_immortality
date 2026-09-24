package engine

import "testing"

// Tests for the TASK-11 scenario layer: the named cast actually existing, travel
// moving the player, and events belonging to a place.
//
// These are engine tests rather than content tests because the properties are
// about what play does, not about what the catalogue says: a catalogue can
// declare three NPCs and a connected graph while the world still contains
// nobody and the player still cannot move.

// ---------------------------------------------------------------------------
// The cast exists
// ---------------------------------------------------------------------------

// castCatalogue is the creation fixture plus a declared cast and a two-node
// graph. It is built on creationTestCatalogue because these tests run the real
// creation path, which needs a complete set of origins and roots.
func castCatalogue() *Catalogue {
	cat := creationTestCatalogue()
	cat.Locations = []LocationDefinition{
		{ID: "village", NameZH: "村", Environment: EnvNormal, Safe: true, Neighbors: []string{"woods"}},
		{ID: "woods", NameZH: "林", Environment: EnvNormal, Safe: false, Neighbors: []string{"village"}},
	}
	cat.NPCs = []NPCDefinition{
		{
			ID: "npc_a", NameZH: "甲", Adult: true, FromManuscript: true,
			InitialRealm: RealmQiRefining, InitialTier: TierEarly,
			LifespanYears: 100, InitialAgeYears: 30,
			Faction: "散修", Personality: "谨慎", HomeLocation: "village",
			DialogueIDs: []string{"dlg_a"},
		},
		{
			ID: "npc_b", NameZH: "乙", Adult: true, NewSetting: true,
			InitialRealm: RealmFoundation, InitialTier: TierEarly,
			LifespanYears: 200, InitialAgeYears: 55,
			Faction: "宗门", Personality: "温和", HomeLocation: "woods",
		},
	}
	return cat
}

func TestConfirmingACharacterPopulatesTheNamedCast(t *testing.T) {
	e := newCastEngine(t)
	confirmCharacter(t, e)

	if got := len(e.State().World.NPCs); got != 2 {
		t.Fatalf("the world holds %d NPCs after creation, want 2: a catalogue that "+
			"declares a cast is not the same as a world that contains one", got)
	}
	a, ok := NPCByID(e.State().World, "npc_a")
	if !ok {
		t.Fatal("npc_a was not spawned")
	}
	if !a.IsAlive {
		t.Fatal("a freshly spawned NPC is not alive")
	}
	if a.AgeMonths != 30*12 {
		t.Fatalf("npc_a age = %d months, want %d", a.AgeMonths, 30*12)
	}
	if a.Location != "village" {
		t.Fatalf("npc_a location = %q, want the declared home location", a.Location)
	}
	if a.Faction != "散修" || a.Personality != "谨慎" {
		t.Fatalf("npc_a disposition was not carried over: %+v", a)
	}
	if len(a.KnownFacts) != 0 {
		t.Fatalf("a spawned NPC starts knowing %d facts; it must start knowing none", len(a.KnownFacts))
	}
}

func TestSpawningTheCastIsIdempotent(t *testing.T) {
	// A retried creation, or a save written before the cast existed, must not
	// reset an age the world has already advanced.
	e := newCastEngine(t)
	cat := castCatalogue()

	if err := SpawnNPCs(e.State().World, cat); err != nil {
		t.Fatalf("first spawn: %v", err)
	}
	e.State().World.NPCs["npc_a"] = NPC{ID: "npc_a", IsAlive: false, AgeMonths: 999, LifespanYears: 100}

	if err := SpawnNPCs(e.State().World, cat); err != nil {
		t.Fatalf("second spawn: %v", err)
	}

	a, _ := NPCByID(e.State().World, "npc_a")
	if a.AgeMonths != 999 || a.IsAlive {
		t.Fatalf("re-spawning overwrote an existing NPC: %+v", a)
	}
	if got := len(e.State().World.NPCs); got != 2 {
		t.Fatalf("re-spawning changed the cast size to %d", got)
	}
}

func TestSpawningRefusesAnNPCThatDeclaresNoAge(t *testing.T) {
	// Design 14 requires every age to be explicit. Ageing an NPC with no
	// declared age would be inventing a number, so the spawn refuses.
	world := &World{NPCs: map[string]NPC{}}
	cat := &Catalogue{NPCs: []NPCDefinition{{
		ID: "npc_x", Adult: true, LifespanYears: 100, HomeLocation: "village",
	}}}
	if err := SpawnNPCs(world, cat); err == nil {
		t.Fatal("an NPC with no declared age was spawned; the age would have to be invented")
	}
}

func TestTheCastAgesWithTheWorldMonth(t *testing.T) {
	e := newCastEngine(t)
	confirmCharacter(t, e)
	before, _ := NPCByID(e.State().World, "npc_a")

	settleMonth(t, e, "wait-1")

	after, _ := NPCByID(e.State().World, "npc_a")
	if after.AgeMonths != before.AgeMonths+1 {
		t.Fatalf("npc_a age went from %d to %d over one settled month, want +1",
			before.AgeMonths, after.AgeMonths)
	}
}

func TestASpawnedNPCBecomesAvailableOnceMet(t *testing.T) {
	// The point of spawning the cast is that `npc_available` can hold. Before
	// TASK-11 the world never contained an NPC, so the condition was
	// unsatisfiable by construction.
	e := newCastEngine(t)
	cat := castCatalogue()
	confirmCharacter(t, e)

	cond := Precondition{Kind: CondNPCAvailable, Key: "npc_a"}
	if ok, err := EvalPrecondition(e.State(), cat, cond); err != nil || ok {
		t.Fatalf("an unmet NPC should not be available (ok=%v err=%v)", ok, err)
	}

	// Meeting them is what the relation effect records.
	if err := applyGrantEffect(e.State(), cat, GrantEffect{
		Kind: GrantAdditive, Target: "relations.npc_a", Amount: 3, Reason: "初见",
	}, &DeltaLedger{}); err != nil {
		t.Fatalf("recording the meeting failed: %v", err)
	}

	if ok, err := EvalPrecondition(e.State(), cat, cond); err != nil || !ok {
		t.Fatalf("a met, living NPC should be available (ok=%v err=%v)", ok, err)
	}
}

func TestADeadNPCIsNotAvailable(t *testing.T) {
	e := newCastEngine(t)
	cat := castCatalogue()
	confirmCharacter(t, e)

	if err := applyGrantEffect(e.State(), cat, GrantEffect{
		Kind: GrantAdditive, Target: "relations.npc_a", Amount: 3, Reason: "初见",
	}, &DeltaLedger{}); err != nil {
		t.Fatalf("recording the meeting failed: %v", err)
	}

	npc, _ := NPCByID(e.State().World, "npc_a")
	npc.IsAlive = false
	e.State().World.NPCs["npc_a"] = npc

	cond := Precondition{Kind: CondNPCAvailable, Key: "npc_a"}
	if ok, _ := EvalPrecondition(e.State(), cat, cond); ok {
		t.Fatal("a dead NPC is still available; design 9.4 forbids a dead NPC accepting new dialogue")
	}
}

// ---------------------------------------------------------------------------
// Travel
// ---------------------------------------------------------------------------

func TestTravelMovesBetweenNeighbours(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	r := e.Submit(travelCommand(e, "t1", "woods"))
	if !r.OK {
		t.Fatalf("travel rejected: %s %v", r.Code, r.Events)
	}
	if got := e.State().World.CurrentLocation; got != "woods" {
		t.Fatalf("current location = %q, want woods", got)
	}
	if !containsString(e.State().World.VisitedLocations, "woods") {
		t.Fatalf("the destination was not recorded as visited: %v", e.State().World.VisitedLocations)
	}
	if r.MonthCostApplied != 0 {
		t.Fatalf("travel applied %d months, want 0", r.MonthCostApplied)
	}
	if !hasResultEvent(r, ResultTravelled, "woods") {
		t.Fatalf("travel was not reported: %+v", r.Events)
	}
}

func TestTravelRefusesANonNeighbour(t *testing.T) {
	// The graph is what makes the low-risk training ground different from the
	// dwelling. Without this check the player could jump anywhere and the graph
	// would be decorative.
	state := readyState()
	e, _ := newTestEngine(t, state)

	r := e.Submit(travelCommand(e, "t1", "nowhere_declared"))
	if r.OK {
		t.Fatal("travel to an undeclared location was accepted")
	}
	if r.Code != ErrUnknownTarget {
		t.Fatalf("code = %s, want %s", r.Code, ErrUnknownTarget)
	}
	if e.State().World.CurrentLocation != "village" {
		t.Fatalf("a rejected travel moved the player to %q", e.State().World.CurrentLocation)
	}
}

func TestTravelToTheCurrentLocationIsRefused(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	r := e.Submit(travelCommand(e, "t1", "village"))
	if r.OK {
		t.Fatal("travelling to where the player already is was accepted")
	}
	if r.Code != ErrPreconditionUnmet {
		t.Fatalf("code = %s, want %s", r.Code, ErrPreconditionUnmet)
	}
}

func TestTravelDrawsNoDomainDie(t *testing.T) {
	state := readyState()
	cat := testCatalogue()
	cat.EventBaseChancePermille = ConfigValue{
		Provenance: ProvenanceDesignNote, Value: PermilleScale, Note: "test",
	}
	cat.Events = []EventDefinition{{
		ID: "EVT-HERE", NameZH: "测试", Scene: "village", Purpose: "测试",
		TextZH:   "一个测试节点。",
		Priority: ConfigValue{Provenance: ProvenanceDesignNote, Value: 50},
		Weight:   ConfigValue{Provenance: ProvenanceDesignNote, Value: 10},
		Choices:  []EventChoice{{ID: "go", TextZH: "行"}},
	}}
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	beforeDraws := worldCounter(t, e.State())
	beforeMonth := e.State().Counters.WorldMonth

	if r := e.Submit(travelCommand(e, "t1", "woods")); !r.OK {
		t.Fatalf("travel rejected: %s", r.Code)
	}

	if e.State().Counters.WorldMonth != beforeMonth {
		t.Fatalf("travel advanced the world month to %d", e.State().Counters.WorldMonth)
	}
	if got := worldCounter(t, e.State()); got != beforeDraws {
		t.Fatalf("travel drew from the world stream (%d -> %d); a fixed graph exists so that "+
			"shuttling back and forth cannot farm anything", beforeDraws, got)
	}
	if e.State().Pending.Event != nil {
		t.Fatal("travel raised an event")
	}
}

// ---------------------------------------------------------------------------
// Scene gating
// ---------------------------------------------------------------------------

func TestEventsAreGatedByScene(t *testing.T) {
	// Design 14 gives every node a scene. Without this check a sect patrol can
	// fire while the player is sitting in their cave.
	state := readyState()
	cat := testCatalogue()
	cat.EventBaseChancePermille = ConfigValue{
		Provenance: ProvenanceDesignNote, Value: PermilleScale, Note: "test",
	}
	cat.Events = []EventDefinition{{
		ID: "EVT-WOODS", NameZH: "林中", Scene: "woods", Purpose: "测试",
		TextZH:   "林中的测试节点。",
		Priority: ConfigValue{Provenance: ProvenanceDesignNote, Value: 50},
		Weight:   ConfigValue{Provenance: ProvenanceDesignNote, Value: 10},
		Choices:  []EventChoice{{ID: "go", TextZH: "行"}},
	}}
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	// At the village the node must not fire, even though the base check is a
	// certainty and the node has no eligibility conditions.
	settleMonth(t, e, "wait-1")
	if got := raisedCount(e.State(), "EVT-WOODS"); got != 0 {
		t.Fatalf("a woods node fired %d times while the player was at the village", got)
	}

	// After travelling there, the same month-end pass raises it.
	if r := e.Submit(travelCommand(e, "t1", "woods")); !r.OK {
		t.Fatalf("travel rejected: %s", r.Code)
	}
	settleMonth(t, e, "wait-2")
	if got := raisedCount(e.State(), "EVT-WOODS"); got != 1 {
		t.Fatalf("the node did not fire after the player travelled to its scene (raises = %d)", got)
	}
}

func TestAnEventWithNoSceneFiresAnywhere(t *testing.T) {
	// The control group for scene gating: an empty scene means "anywhere", so
	// the rule above is not simply rejecting every event.
	state := readyState()
	cat := testCatalogue()
	cat.EventBaseChancePermille = ConfigValue{
		Provenance: ProvenanceDesignNote, Value: PermilleScale, Note: "test",
	}
	cat.Events = []EventDefinition{{
		ID: "EVT-ANY", NameZH: "随处", Scene: "", Purpose: "测试",
		TextZH:   "随处可遇的测试节点。",
		Priority: ConfigValue{Provenance: ProvenanceDesignNote, Value: 50},
		Weight:   ConfigValue{Provenance: ProvenanceDesignNote, Value: 10},
		Choices:  []EventChoice{{ID: "go", TextZH: "行"}},
	}}
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	settleMonth(t, e, "wait-1")
	if got := raisedCount(e.State(), "EVT-ANY"); got != 1 {
		t.Fatalf("a scene-less node fired %d times, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// Digest coverage of the scenario state
// ---------------------------------------------------------------------------

func TestDigestCoversTheScenarioState(t *testing.T) {
	// Control group first: if digestCovers is broken, every assertion below
	// would pass for the wrong reason.
	if !digestCovers(t, func(s *GameState) { s.Counters.WorldMonth++ }) {
		t.Fatal("digestCovers is not sensitive to the world month; the assertions below prove nothing")
	}

	covered := []struct {
		name   string
		mutate func(*GameState)
	}{
		// Where the player is decides which events can fire, so a tampered
		// location would change the game while verifying as intact.
		{"current location", func(s *GameState) { s.World.CurrentLocation = "elsewhere" }},
		{"visited locations", func(s *GameState) {
			s.World.VisitedLocations = append(s.World.VisitedLocations, "elsewhere")
		}},
		// The cast ages with the world, so an edited age or liveness changes who
		// is available to talk to.
		{"npc age", func(s *GameState) {
			s.World.NPCs["x"] = NPC{ID: "x", IsAlive: true, AgeMonths: 900}
		}},
		{"npc liveness", func(s *GameState) {
			s.World.NPCs["x"] = NPC{ID: "x", IsAlive: false}
		}},
	}

	for _, c := range covered {
		if !digestCovers(t, c.mutate) {
			t.Errorf("the integrity digest is not sensitive to %s; a tampered save would be "+
				"accepted and would change what the player can do", c.name)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func travelCommand(e *Engine, actionID, destination string) Command {
	c := cmd(e, actionID, KindTravel)
	c.Payload.LocationID = destination
	return c
}

// newCastEngine wires an engine over the creation fixture plus a declared cast.
func newCastEngine(t *testing.T) *Engine {
	t.Helper()
	state := creationState()
	return NewEngine(NewMemoryStore(state), castCatalogue(), state)
}

// confirmCharacter runs the three-action quick path to a playable character:
// apply a legal preset, advance to the details step, confirm.
func confirmCharacter(t *testing.T, e *Engine) {
	t.Helper()
	if e.State().Pending.Creation == nil {
		t.Fatal("the fixture is not in creation")
	}
	steps := []struct {
		id    string
		kind  CommandKind
		pload *CreationPayload
	}{
		{"preset", KindCreateEdit, &CreationPayload{PresetID: DefaultPresetID}},
		{"advance", KindCreateEdit, &CreationPayload{AdvanceStep: true}},
		{"confirm", KindCreateConfirm, &CreationPayload{Confirm: true}},
	}
	for _, step := range steps {
		if r := e.Submit(creationCmd(e, step.id, step.kind, step.pload)); !r.OK {
			t.Fatalf("creation step %q was rejected: %s %v", step.id, r.Code, r.Events)
		}
	}
}
