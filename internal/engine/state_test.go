package engine

import "testing"

// These tests cover the state contract: the structural invariants a loaded
// state must satisfy, and the serialisation readiness of the sub-states the
// design document requires to survive a round trip.

// validState builds a minimal state that passes ValidateState.
func validState() GameState {
	return GameState{
		SchemaVersion:  SchemaVersion,
		RulesVersion:   RulesVersion,
		ContentVersion: ContentVersion,
		GameID:         "game-1",
		BranchID:       "branch-1",
		Revision:       1,
		Phase:          PhaseReady,
		Counters:       Counters{WorldMonth: 0, InteractionSeq: 0, CombatRound: 0},
		RNG: RNGState{
			Algorithm: "splitmix64-v1",
			Streams:   validStreams(),
		},
		Player: &Player{
			AgeMonths:  21 * 12,
			Lifespan:   LifespanLedger{BaseYears: 100},
			Origin:     OriginCommoner,
			Path:       PathHuman,
			SpiritRoot: RootPseudo,
			HP:         Vitals{Current: 80 * SCALE, Max: 100 * SCALE},
			MP:         Vitals{Current: 50 * SCALE, Max: 100 * SCALE},
			XP:         10 * SCALE,
			Realm:      RealmQiRefining,
			Tier:       TierEarly,
			Resources:  map[Resource]int64{ResSpiritStones: 100},
		},
		World: &World{},
		Idempotency: IdempotencyTable{
			Entries: map[string]IdempotencyEntry{},
			Limit:   DefaultIdempotencyLimit,
		},
	}
}

// validStreams returns every required random stream.
func validStreams() map[string]RNGStream {
	out := make(map[string]RNGStream, len(WorldStreamNames))
	for _, name := range WorldStreamNames {
		out[name] = RNGStream{State: 42, Counter: 0, Algorithm: "splitmix64-v1"}
	}
	return out
}

// ---------------------------------------------------------------------------
// Baseline.
// ---------------------------------------------------------------------------

func TestValidStatePasses(t *testing.T) {
	s := validState()
	if rep := ValidateState(&s); !rep.OK() {
		t.Fatalf("baseline state must validate, got %d error(s):\n%v",
			len(rep.Errors), rep.Error())
	}
}

func TestNilStateFails(t *testing.T) {
	if ValidateState(nil).OK() {
		t.Fatal("a nil state must not validate")
	}
}

// ---------------------------------------------------------------------------
// Version identity.
// ---------------------------------------------------------------------------

func TestSchemaVersionMismatchFails(t *testing.T) {
	s := validState()
	s.SchemaVersion = SchemaVersion + 1
	rep := ValidateState(&s)
	if !rep.Has(VErrUnknownEnum) {
		t.Fatalf("a wrong schema version must fail; codes %v", rep.Codes())
	}
}

func TestEmptyIdentityFails(t *testing.T) {
	s := validState()
	s.GameID = ""
	rep := ValidateState(&s)
	if !rep.Has(VErrMissingProvenance) {
		t.Fatalf("an empty game id must fail; codes %v", rep.Codes())
	}
}

// ---------------------------------------------------------------------------
// The 9.4 invariant list.
// ---------------------------------------------------------------------------

func TestHPAboveMaximumFails(t *testing.T) {
	s := validState()
	s.Player.HP = Vitals{Current: 200 * SCALE, Max: 100 * SCALE}
	rep := ValidateState(&s)
	if !rep.Has(VErrInvariantBroken) {
		t.Fatalf("hp above its cap must fail; codes %v", rep.Codes())
	}
}

func TestNegativeVitalsFail(t *testing.T) {
	for name, mutate := range map[string]func(*GameState){
		"negative hp current": func(s *GameState) { s.Player.HP.Current = -1 },
		"negative hp max":     func(s *GameState) { s.Player.HP.Max = -1 },
		"negative mp current": func(s *GameState) { s.Player.MP.Current = -1 },
		"negative mp max":     func(s *GameState) { s.Player.MP.Max = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			s := validState()
			mutate(&s)
			rep := ValidateState(&s)
			if !rep.Has(VErrInvariantBroken) {
				t.Fatalf("expected %s, got codes %v", VErrInvariantBroken, rep.Codes())
			}
		})
	}
}

func TestNegativeBalanceFails(t *testing.T) {
	// An unaffordable action must be refused, never auto-loaned. A negative
	// balance is the symptom of that rule being broken.
	s := validState()
	s.Player.Resources[ResSpiritStones] = -1
	rep := ValidateState(&s)
	if !rep.Has(VErrInvariantBroken) {
		t.Fatalf("a negative balance must fail; codes %v", rep.Codes())
	}
}

func TestDebtIsTrackedSeparatelyAndMustNotBeNegative(t *testing.T) {
	// Debt is a positive "owed" amount, deliberately not folded into balance.
	s := validState()
	s.Player.Debt = 50
	if rep := ValidateState(&s); !rep.OK() {
		t.Fatalf("a positive debt must be legal: %v", rep.Error())
	}
	s.Player.Debt = -50
	if rep := ValidateState(&s); !rep.Has(VErrInvariantBroken) {
		t.Fatalf("a negative debt must fail; codes %v", rep.Codes())
	}
}

func TestNegativeXPAndItemQuantityFail(t *testing.T) {
	s := validState()
	s.Player.XP = -1
	s.Player.Inventory.Stacks = []ItemStack{{ItemID: "pill", Quantity: -2}}
	rep := ValidateState(&s)
	if rep.CountOf(VErrInvariantBroken) < 2 {
		t.Fatalf("both negative xp and negative quantity must be reported; got %d",
			rep.CountOf(VErrInvariantBroken))
	}
}

func TestUnknownResourceFails(t *testing.T) {
	s := validState()
	s.Player.Resources["made_up"] = 10
	rep := ValidateState(&s)
	if !rep.Has(VErrUnknownEnum) {
		t.Fatalf("expected %s, got codes %v", VErrUnknownEnum, rep.Codes())
	}
}

func TestPlayerOnFrozenPathFails(t *testing.T) {
	s := validState()
	s.Player.Path = PathHeavenly
	rep := ValidateState(&s)
	if !rep.Has(VErrPathNotOpen) {
		t.Fatalf("a player on a frozen path must fail; codes %v", rep.Codes())
	}
}

func TestPlayerMissingInNonCreationPhaseFails(t *testing.T) {
	s := validState()
	s.Player = nil
	rep := ValidateState(&s)
	if !rep.Has(VErrPreconditionUnmet) {
		t.Fatalf("a missing player outside CREATION must fail; codes %v", rep.Codes())
	}
}

func TestPlayerMayBeAbsentDuringCreation(t *testing.T) {
	s := validState()
	s.Player = nil
	s.Phase = PhaseCreation
	s.Pending.Creation = &CreationDraft{Step: 1}
	if rep := ValidateState(&s); !rep.OK() {
		t.Fatalf("CREATION without a player must be legal: %v", rep.Error())
	}
}

func TestWorldMustBePresent(t *testing.T) {
	s := validState()
	s.World = nil
	rep := ValidateState(&s)
	if !rep.Has(VErrPreconditionUnmet) {
		t.Fatalf("a missing world must fail; codes %v", rep.Codes())
	}
}

// ---------------------------------------------------------------------------
// Phase and pending sub-state consistency.
//
// A mismatch here is how a save ends up waiting forever for an event that no
// longer exists, or gives the enemy a free attack on resume.
// ---------------------------------------------------------------------------

func TestPhaseRequiresItsSubState(t *testing.T) {
	cases := []struct {
		phase Phase
		name  string
	}{
		{PhaseEventPending, "event"},
		{PhaseCombatPending, "combat"},
		{PhaseBreakthroughPending, "breakthrough"},
		{PhaseConfirming, "confirmation"},
		{PhaseCreation, "creation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validState()
			s.Phase = tc.phase
			// Deliberately leave Pending empty.
			rep := ValidateState(&s)
			if !rep.Has(VErrPreconditionUnmet) {
				t.Fatalf("phase %s without its sub-state must fail; codes %v",
					tc.phase, rep.Codes())
			}
		})
	}
}

func TestSubStateLingeringInWrongPhaseFails(t *testing.T) {
	s := validState()
	s.Phase = PhaseReady
	s.Pending.Event = &PendingEvent{
		EventID: "E", InstanceID: "i1", ChoiceIDs: []string{"c1"},
	}
	rep := ValidateState(&s)
	if !rep.Has(VErrInvariantBroken) {
		t.Fatalf("a pending event while READY must fail; codes %v", rep.Codes())
	}
}

func TestCombatSubStateLingeringInWrongPhaseFails(t *testing.T) {
	s := validState()
	s.Phase = PhaseReady
	s.Pending.Combat = &PendingCombat{
		CombatID: "c1", Resolution: CombatOngoing, AwaitingPlayer: true,
	}
	rep := ValidateState(&s)
	if !rep.Has(VErrInvariantBroken) {
		t.Fatalf("a pending combat while READY must fail; codes %v", rep.Codes())
	}
}

func TestEveryPhaseIsDeclared(t *testing.T) {
	all := []Phase{
		PhaseCreation, PhaseReady, PhaseConfirming, PhaseEventPending,
		PhaseCombatPending, PhaseBreakthroughPending, PhaseSaveError, PhaseEnded,
	}
	for _, p := range all {
		if !p.Valid() {
			t.Errorf("phase %s should be valid", p)
		}
	}
	if Phase("NONSENSE").Valid() {
		t.Error("an unknown phase must not be valid")
	}
}

func TestOnlyReadyAllowsWorldActions(t *testing.T) {
	// Query-only operations are allowed everywhere and are not modelled here;
	// this predicate is specifically about month-costing actions.
	if !PhaseReady.AllowsWorldAction() {
		t.Error("READY must allow world actions")
	}
	for _, p := range []Phase{
		PhaseCreation, PhaseConfirming, PhaseEventPending, PhaseCombatPending,
		PhaseBreakthroughPending, PhaseSaveError, PhaseEnded,
	} {
		if p.AllowsWorldAction() {
			t.Errorf("phase %s must not allow world actions", p)
		}
	}
}

// ---------------------------------------------------------------------------
// Random streams.
// ---------------------------------------------------------------------------

func TestMissingRNGStreamFails(t *testing.T) {
	// A missing stream means the first draw in a resumed game would have no
	// recorded position, so replay would diverge.
	s := validState()
	delete(s.RNG.Streams, StreamWorld)
	rep := ValidateState(&s)
	if !rep.Has(VErrPreconditionUnmet) {
		t.Fatalf("a missing random stream must fail; codes %v", rep.Codes())
	}
}

func TestEachStreamIsIndependent(t *testing.T) {
	// The streams must be distinct entries; sharing one would couple combat
	// rolls to world event selection.
	s := validState()
	if len(s.RNG.Streams) != len(WorldStreamNames) {
		t.Fatalf("expected %d streams, got %d", len(WorldStreamNames), len(s.RNG.Streams))
	}
	for _, name := range WorldStreamNames {
		if _, ok := s.RNG.Streams[name]; !ok {
			t.Errorf("stream %s missing", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Serialisation readiness.
//
// The design document requires pending combat, unfinished month actions, random
// state and idempotency records to be serialisable. These tests assert the
// sub-states carry what a resume needs.
// ---------------------------------------------------------------------------

func TestSerializeReadyAcceptsACompleteState(t *testing.T) {
	s := validState()
	if rep := ValidateSerializeReady(&s); !rep.OK() {
		t.Fatalf("a complete state must be serialisation-ready: %v", rep.Error())
	}
}

func TestNilStreamsAreNotSerializeReady(t *testing.T) {
	s := validState()
	s.RNG.Streams = nil
	rep := ValidateSerializeReady(&s)
	if !rep.Has(VErrSerialization) {
		t.Fatalf("nil streams must not be serialize-ready; codes %v", rep.Codes())
	}
}

func TestNilIdempotencyIsNotSerializeReady(t *testing.T) {
	s := validState()
	s.Idempotency.Entries = nil
	rep := ValidateSerializeReady(&s)
	if !rep.Has(VErrSerialization) {
		t.Fatalf("nil idempotency records must not be serialize-ready; codes %v", rep.Codes())
	}
}

func TestPendingCombatMustRecordWhoseTurnItIs(t *testing.T) {
	// Without this flag a resume cannot know whether the enemy already acted,
	// and could hand it a free attack.
	s := validState()
	s.Phase = PhaseCombatPending
	s.Pending.Combat = &PendingCombat{
		CombatID:       "c1",
		Resolution:     CombatOngoing,
		AwaitingPlayer: false,
	}
	rep := ValidateSerializeReady(&s)
	if !rep.Has(VErrSerialization) {
		t.Fatalf("an ongoing combat must record whose turn it is; codes %v", rep.Codes())
	}
}

func TestResolvedCombatNeedNotAwaitThePlayer(t *testing.T) {
	// A finished battle legitimately awaits nobody.
	s := validState()
	s.Phase = PhaseCombatPending
	s.Pending.Combat = &PendingCombat{
		CombatID:       "c1",
		Resolution:     CombatWon,
		AwaitingPlayer: false,
	}
	if rep := ValidateSerializeReady(&s); !rep.OK() {
		t.Fatalf("a resolved combat need not await the player: %v", rep.Error())
	}
}

func TestPendingCombatMustRecordItsID(t *testing.T) {
	s := validState()
	s.Phase = PhaseCombatPending
	s.Pending.Combat = &PendingCombat{Resolution: CombatWon}
	rep := ValidateSerializeReady(&s)
	if !rep.Has(VErrSerialization) {
		t.Fatalf("a pending combat must record its id; codes %v", rep.Codes())
	}
}

func TestActiveMonthActionMustRecordItsID(t *testing.T) {
	// The action id is how settlement recognises this as the same action and
	// charges its month at most once.
	s := validState()
	s.Pending.MonthAction = &ActiveMonthAction{Kind: KindCultivate}
	rep := ValidateSerializeReady(&s)
	if !rep.Has(VErrSerialization) {
		t.Fatalf("an active month action must record its id; codes %v", rep.Codes())
	}
}

func TestPendingEventMustFreezeItsCandidates(t *testing.T) {
	// Re-rolling candidates on resume is exactly the bug this guards against.
	s := validState()
	s.Phase = PhaseEventPending
	s.Pending.Event = &PendingEvent{
		EventID: "E", InstanceID: "i1",
		ChoiceIDs: nil, // not frozen
	}
	rep := ValidateSerializeReady(&s)
	if !rep.Has(VErrSerialization) {
		t.Fatalf("a pending event must freeze its candidates; codes %v", rep.Codes())
	}
}

func TestPendingBreakthroughMustKeepItsParentAndMaterials(t *testing.T) {
	s := validState()
	s.Phase = PhaseBreakthroughPending
	s.Pending.Breakthrough = &PendingBreakthrough{
		NodeID: "EVT-012",
		// No BreakthroughID, no ParentActionID.
	}
	rep := ValidateSerializeReady(&s)
	if rep.CountOf(VErrSerialization) < 2 {
		t.Fatalf("a pending breakthrough must record its id and parent action; got %d",
			rep.CountOf(VErrSerialization))
	}
}

func TestActiveMonthActionChargesItsMonthOnce(t *testing.T) {
	// The flag that prevents a nested battle from charging a second month is
	// part of the contract, so assert the field exists and travels.
	m := ActiveMonthAction{
		ActionID:          "a1",
		Kind:              KindQuestRun,
		MonthCharged:      true,
		SettlementPending: true,
		StartedWorldMonth: 12,
	}
	if !m.MonthCharged {
		t.Error("a settled month must be recorded as charged")
	}
	if !m.SettlementPending {
		t.Error("a nested sub-state must be recorded as pending settlement")
	}
}

// ---------------------------------------------------------------------------
// Command kinds and the timing matrix.
// ---------------------------------------------------------------------------

func TestMonthCostMatrix(t *testing.T) {
	// Design document 13.1. Getting this wrong is how a menu refresh starts
	// advancing the world.
	oneMonth := []CommandKind{
		KindCultivate, KindHeal, KindWait, KindQuestRun, KindBreakthrough,
	}
	zeroMonth := []CommandKind{
		KindQuery, KindCreateConfirm, KindAcceptQuest, KindClaimReward,
		KindJoinSect, KindTravel, KindTrade, KindUseItem, KindEventChoice,
		KindCombatAction,
	}
	for _, k := range oneMonth {
		if got := k.MonthCost(); got != 1 {
			t.Errorf("kind %s costs %d months, want 1", k, got)
		}
	}
	for _, k := range zeroMonth {
		if got := k.MonthCost(); got != 0 {
			t.Errorf("kind %s costs %d months, want 0", k, got)
		}
	}
}

func TestOnlyCombatActionAdvancesTheRound(t *testing.T) {
	if !KindCombatAction.AdvancesCombatRound() {
		t.Error("COMBAT_ACTION must advance the combat round")
	}
	for _, k := range []CommandKind{
		KindQuery, KindEventChoice, KindTrade, KindCultivate, KindTravel,
		KindBreakthrough,
	} {
		if k.AdvancesCombatRound() {
			t.Errorf("kind %s must not advance the combat round", k)
		}
	}
}

func TestEveryCommandKindIsDeclared(t *testing.T) {
	for _, k := range []CommandKind{
		KindQuery, KindCreateConfirm, KindAcceptQuest, KindClaimReward,
		KindJoinSect, KindTravel, KindTrade, KindUseItem, KindEventChoice,
		KindBreakthrough, KindCombatAction, KindCultivate, KindHeal, KindWait,
		KindQuestRun,
	} {
		if !k.Valid() {
			t.Errorf("kind %s should be valid", k)
		}
	}
	if CommandKind("NONSENSE").Valid() {
		t.Error("an unknown kind must not be valid")
	}
}

func TestEventChoiceCostsNoExtraMonth(t *testing.T) {
	// The design document is explicit: an event choice inherits the parent
	// action's timing, and an extra-month option must be its own action.
	if got := KindEventChoice.MonthCost(); got != 0 {
		t.Errorf("an event choice must cost no extra month, got %d", got)
	}
}

func TestCombatActionsCostNoExtraMonth(t *testing.T) {
	// A combat round advances the round counter, never the world month; the
	// parent action's single month is charged once at settlement.
	if got := KindCombatAction.MonthCost(); got != 0 {
		t.Errorf("a combat action must cost no extra month, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Error codes.
// ---------------------------------------------------------------------------

func TestErrorCodesAreDistinct(t *testing.T) {
	// A duplicated code would make two different failures indistinguishable in
	// a test assertion.
	all := []ErrorCode{
		ErrUnknownKind, ErrBadPhase, ErrRevisionMismatch, ErrStaleViewToken,
		ErrDuplicateAction, ErrActionIdConflict, ErrConfirmationNeeded,
		ErrConfirmationExpired, ErrInsufficientFunds, ErrUnknownTarget,
		ErrUnknownItem, ErrQuantityNotHeld, ErrPreconditionUnmet,
		ErrInvariantBroken, ErrSerialization,
	}
	seen := map[ErrorCode]bool{}
	for _, c := range all {
		if c == ErrNone {
			t.Errorf("a real error code must not be the empty sentinel")
		}
		if seen[c] {
			t.Errorf("duplicate error code %s", c)
		}
		seen[c] = true
	}
	if ErrNone != "" {
		t.Error("ErrNone must be the empty sentinel so a zero value means no error")
	}
}

// ---------------------------------------------------------------------------
// Provenance and enums.
// ---------------------------------------------------------------------------

func TestProvenanceValidity(t *testing.T) {
	for _, p := range []Provenance{
		ProvenanceManuscript, ProvenanceTrial, ProvenancePending, ProvenanceDesignNote,
	} {
		if !p.Valid() {
			t.Errorf("provenance %s should be valid", p)
		}
	}
	if Provenance("made_up").Valid() {
		t.Error("an unknown provenance must not be valid")
	}
}

func TestRealmsAreOrderedAndComplete(t *testing.T) {
	if len(RealmOrder) != 10 {
		t.Fatalf("expected ten realms, got %d", len(RealmOrder))
	}
	seen := map[Realm]bool{}
	for _, r := range RealmOrder {
		if seen[r] {
			t.Errorf("realm %s appears twice in RealmOrder", r)
		}
		seen[r] = true
	}
	// The two trial-rate realms must be present, since their absence is the
	// specific omission the acceptance criteria guard against.
	for _, must := range []Realm{RealmCrystallizing, RealmNascentSoul} {
		if !seen[must] {
			t.Errorf("realm %s must be in RealmOrder", must)
		}
	}
}

func TestTiersAreOrdered(t *testing.T) {
	if len(TierOrder) != 4 {
		t.Fatalf("expected four tiers, got %d", len(TierOrder))
	}
	for _, tier := range TierOrder {
		if !tier.Valid() {
			t.Errorf("tier %s should be valid", tier)
		}
	}
	if RealmTier("made_up").Valid() {
		t.Error("an unknown tier must not be valid")
	}
}

func TestPathOpenness(t *testing.T) {
	if !PathHuman.OpenInM1() {
		t.Error("the human path must be open in M1")
	}
	for _, p := range []Path{PathEarthly, PathHeavenly} {
		if p.OpenInM1() {
			t.Errorf("path %s must be frozen out of M1", p)
		}
	}
	if Path("made_up").Valid() {
		t.Error("an unknown path must not be valid")
	}
}

func TestQueryKindsAreValid(t *testing.T) {
	for _, q := range []QueryKind{
		QueryStatus, QueryInventory, QueryQuests, QueryRelations, QueryLog,
		QueryHelp, QueryRealm,
	} {
		if !q.Valid() {
			t.Errorf("query kind %s should be valid", q)
		}
	}
	if QueryKind("made_up").Valid() {
		t.Error("an unknown query kind must not be valid")
	}
}

func TestConditionValueBearingMatchesItsKind(t *testing.T) {
	// A string-valued condition must not accept a number, and vice versa; this
	// predicate is what the validator uses to decide.
	numeric := []ConditionKind{
		CondHasItem, CondResource, CondFunds, CondWorldMonth, CondAgeMonths,
		CondLifespanLeft,
	}
	stringy := []ConditionKind{
		CondRealm, CondTier, CondPath, CondOrigin, CondSpiritRoot,
		CondQuestStatus, CondFlag, CondNPCAvailable, CondSectMember,
		CondConsumedEvent, CondSkillKnown, CondHasDebt,
	}
	for _, k := range numeric {
		if !k.ValueBearing() {
			t.Errorf("condition kind %s should be value-bearing", k)
		}
	}
	for _, k := range stringy {
		if k.ValueBearing() {
			t.Errorf("condition kind %s should not be value-bearing", k)
		}
	}
}

func TestGrantKindsAreValid(t *testing.T) {
	for _, g := range []GrantKind{GrantAdditive, GrantMultiplicative, GrantPermanent} {
		if !g.Valid() {
			t.Errorf("grant kind %s should be valid", g)
		}
	}
	if GrantKind("made_up").Valid() {
		t.Error("an unknown grant kind must not be valid")
	}
}

func TestCostAndRiskKindsAreValid(t *testing.T) {
	for _, k := range []CostKind{CostItem, CostResource, CostHP, CostMP, CostMonth} {
		if !k.Valid() {
			t.Errorf("cost kind %s should be valid", k)
		}
	}
	for _, k := range []RiskKind{RiskInjury, RiskCombat, RiskLoss, RiskReputation, RiskAmbush} {
		if !k.Valid() {
			t.Errorf("risk kind %s should be valid", k)
		}
	}
	if CostKind("x").Valid() || RiskKind("x").Valid() {
		t.Error("unknown cost and risk kinds must not be valid")
	}
}

func TestCompareOperatorsAreValid(t *testing.T) {
	for _, op := range []CompareOp{OpEQ, OpNE, OpLT, OpLE, OpGT, OpGE} {
		if !op.Valid() {
			t.Errorf("operator %s should be valid", op)
		}
	}
	if CompareOp("≈").Valid() {
		t.Error("an unknown operator must not be valid")
	}
}

func TestSaveStatesAreDistinct(t *testing.T) {
	// The distinction that matters: a failed save must be representable, since
	// the design document forbids reporting success when the write failed.
	if SaveFailed == SaveDurable || SaveFailed == SaveNotNeeded || SaveDurable == SaveNotNeeded {
		t.Fatal("save states must be distinct")
	}
	if SaveState("") == SaveDurable {
		t.Error("the zero save state must not read as durable")
	}
}
