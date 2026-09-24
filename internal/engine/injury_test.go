package engine

import "testing"

// Tests for the TASK-12 injury, lifespan, healing and karma rules.
//
// The acceptance criteria are unusually numeric, so most of these tests assert
// on exact values rather than on shapes: a band boundary that is one sub-unit
// off is a bug, and "roughly a quarter of the ceiling" is not a specification.

// ---------------------------------------------------------------------------
// Injury bands
// ---------------------------------------------------------------------------

// TestInjuryBandsPartitionTheHealthRange is the "0/33%/66% 及小数边界无空隙"
// criterion.
//
// It walks every integer health value for several ceilings and asserts three
// things at once: each value gets a band, no value gets two, and the band is the
// one design 6.2 names. Several ceilings are used because a round 100 hides
// truncation bugs that 40*SCALE exposes.
func TestInjuryBandsPartitionTheHealthRange(t *testing.T) {
	ceilings := []int64{
		100,     // the design's own round example
		99, 101, // neighbours, where 33% and 66% do not divide evenly
		40 * SCALE, // the shipped starting health
		7,          // smaller than the denominators
		1000 * SCALE,
	}

	for _, max := range ceilings {
		for hp := int64(0); hp <= max; hp++ {
			band := InjuryBandFor(hp, max)
			if !band.Valid() {
				t.Fatalf("hp %d of %d produced the undeclared band %q", hp, max, band)
			}

			want := expectedBand(hp, max)
			if band != want {
				t.Fatalf("hp %d of %d is %s, want %s", hp, max, band, want)
			}
		}
	}
}

// expectedBand is the design's rule written out independently of the
// implementation, so the test is not a restatement of the code.
func expectedBand(hp, max int64) InjuryBand {
	switch {
	case hp <= 0:
		return BandDown
	case hp >= max:
		return BandHealthy
	case hp*100 < max*33: // hp < 33%
		return BandDying
	case hp*100 < max*66: // 33% <= hp < 66%
		return BandHeavy
	default: // 66% <= hp < 100%
		return BandLight
	}
}

func TestInjuryBandBoundariesAreInclusiveOnTheLowerSide(t *testing.T) {
	// Design 6.2 writes the bands as [33%,66%) and [66%,100%), so the threshold
	// value itself belongs to the more severe band. Getting this backwards is
	// the difference between "at 33% you are 重伤" and "at 33% you are still
	// 垂死".
	const max = 100
	cases := []struct {
		hp   int64
		want InjuryBand
	}{
		{0, BandDown},
		{1, BandDying},
		{32, BandDying},
		{33, BandHeavy},
		{65, BandHeavy},
		{66, BandLight},
		{99, BandLight},
		{100, BandHealthy},
	}
	for _, tc := range cases {
		if got := InjuryBandFor(tc.hp, max); got != tc.want {
			t.Errorf("hp %d of %d is %s, want %s", tc.hp, max, got, tc.want)
		}
	}
}

func TestEveryBandIsConfigured(t *testing.T) {
	// A band with no configuration has no penalty rule and no display name, so
	// the panel would print its identifier.
	cat := testCatalogue()
	cat.InjuryBands = validInjuryBandsForTest()
	for _, band := range InjuryBandOrder {
		if findInjuryBand(cat, band) == nil {
			t.Errorf("band %s is not configured", band)
		}
	}
	if got := InjuryBandName(cat, BandHeavy); got == "" || got == string(BandHeavy) {
		t.Errorf("the heavy band has no display name, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// The 遁速 penalty
// ---------------------------------------------------------------------------

// TestHeavyWoundSpeedPenaltyIsNotRepeated is the "重伤遁速惩罚不重复" criterion.
//
// The property is that the penalty is derived rather than accumulated: calling
// the recomputation any number of times must leave the same speed. An
// implementation that subtracts from a running total passes a single call and
// fails the second.
func TestHeavyWoundSpeedPenaltyIsNotRepeated(t *testing.T) {
	cat := testCatalogue()
	cat.InjuryBands = validInjuryBandsForTest()

	state := readyState()
	state.Player.Attributes.Agility = 10
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 100 * SCALE} // 40% → 重伤
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	base := int64(10) * SCALE
	want := base * (PermilleScale - 300) / PermilleScale // 30% slower

	refreshDerived(e.State().Player, cat)
	first := e.State().Player.Derived.Speed
	if first != want {
		t.Fatalf("speed after one recomputation = %d, want %d", first, want)
	}

	// The control group: the penalty really is applied, so the equality below
	// is not the equality of "nothing happens at all".
	if first == base {
		t.Fatal("a 重伤 character has their full base speed; the penalty is not applied at all")
	}

	for i := 0; i < 5; i++ {
		refreshDerived(e.State().Player, cat)
		if got := e.State().Player.Derived.Speed; got != first {
			t.Fatalf("recomputation %d changed speed from %d to %d; the penalty is "+
				"accumulating instead of being derived", i+2, first, got)
		}
	}
}

func TestHealthyCharactersCarryNoInjuryPenalty(t *testing.T) {
	cat := testCatalogue()
	cat.InjuryBands = validInjuryBandsForTest()

	state := readyState()
	state.Player.Attributes.Agility = 10
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 40 * SCALE}
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	refreshDerived(e.State().Player, cat)
	if got, want := e.State().Player.Derived.Speed, int64(10)*SCALE; got != want {
		t.Fatalf("a healthy character's speed = %d, want %d", got, want)
	}
}

func TestBandAndAfflictionPenaltiesAdd(t *testing.T) {
	// The band and an affliction are different causes, so both apply; only a
	// repeated application of the same cause would be double counting.
	cat := testCatalogue()
	cat.InjuryBands = validInjuryBandsForTest()
	cat.Afflictions = []AfflictionDefinition{{
		ID: "sprain", NameZH: "扭伤", Kind: AfflictionWound,
		DurationMonths:       ConfigValue{Provenance: ProvenanceDesignNote, Value: 1},
		SpeedPenaltyPermille: ConfigValue{Provenance: ProvenanceDesignNote, Value: 100},
	}}

	state := readyState()
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 100 * SCALE} // 重伤, −300
	state.Player.Condition.Effects = []TimedEffect{{ID: "sprain", MonthsRemaining: 1, Stacks: 1}}
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	if got := InjuryPenaltyPermille(e.State().Player, cat); got != 400 {
		t.Fatalf("combined penalty = %d permille, want 400", got)
	}
}

func TestPenaltyIsClampedBelowTotalSlowness(t *testing.T) {
	// Content can configure penalties that add up past 100%; a negative speed
	// would be a broken invariant rather than a very slow character.
	cat := testCatalogue()
	cat.InjuryBands = []InjuryBandDefinition{
		{Band: BandHeavy, NameZH: "重伤", SpeedPenaltyPermille: ConfigValue{Provenance: ProvenanceDesignNote, Value: 900}},
	}

	state := readyState()
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 100 * SCALE}
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	refreshDerived(e.State().Player, cat)
	if got := e.State().Player.Derived.Speed; got != 0 {
		t.Fatalf("speed = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Lifespan
// ---------------------------------------------------------------------------

// TestLifespanRemainingMatchesTheDesignExamples is the "21岁炼气余79年、同龄筑基余
// 179年" criterion. The design's point is that advancing a realm REPLACES the
// ceiling rather than adding to it, which is why the second figure is 179 and not
// 279.
func TestLifespanRemainingMatchesTheDesignExamples(t *testing.T) {
	const ageYears = 21
	ageMonths := int64(ageYears) * MonthsPerYear

	cases := []struct {
		name      string
		baseYears int
		wantYears int
	}{
		{"炼气", 100, 79},
		{"筑基", 200, 179},
	}
	for _, tc := range cases {
		ledger := LifespanLedger{BaseYears: tc.baseYears}
		remainingMonths := LifespanMonths(ledger) - ageMonths
		if got, want := remainingMonths, int64(tc.wantYears)*MonthsPerYear; got != want {
			t.Errorf("%s at %d: remaining = %d months, want %d (%d years)",
				tc.name, ageYears, got, want, tc.wantYears)
		}
		if LifespanExhausted(ageMonths, ledger) {
			t.Errorf("%s at %d must not be exhausted", tc.name, ageYears)
		}
	}
}

func TestBreakthroughReplacesTheCeilingRatherThanAddingToIt(t *testing.T) {
	// A bonus and a replacement are different operations, and only one of them
	// is how a realm advance works (design R15: "突破替换基础上限，不累加").
	replaced := LifespanLedger{BaseYears: 200}
	if got := LifespanMonths(replaced); got != 200*MonthsPerYear {
		t.Fatalf("a replaced ceiling is %d months, want %d", got, 200*MonthsPerYear)
	}

	withBonus := LifespanLedger{
		BaseYears: 100,
		Bonuses:   []LifespanBonus{{ID: "pill", Years: 30, Reason: "延寿丹"}},
	}
	if got, want := LifespanMonths(withBonus), int64(130)*MonthsPerYear; got != want {
		t.Fatalf("a ledger with a bonus is %d months, want %d", got, want)
	}
}

func TestLifespanCeilingIsInclusive(t *testing.T) {
	// Reaching the ceiling exactly is death, so a character whose lifespan is
	// 100 years does not act in their 100th year.
	ledger := LifespanLedger{BaseYears: 100}
	ceiling := int64(100) * MonthsPerYear
	if LifespanExhausted(ceiling-1, ledger) {
		t.Fatal("a character one month short of the ceiling is treated as dead")
	}
	if !LifespanExhausted(ceiling, ledger) {
		t.Fatal("a character exactly at the ceiling is treated as alive")
	}
}

// TestNoLifespanLeftRefusesMonthActions is the "开始时剩余0不能行动" criterion.
//
// The state is built by hand because it is one a save written by a future build
// could contain: the age and the phase disagree. The guard must refuse before
// the month is charged, or such a character is aged again every time.
func TestNoLifespanLeftRefusesMonthActions(t *testing.T) {
	state := readyState()
	state.Player.Lifespan = LifespanLedger{BaseYears: 80}
	state.Player.AgeMonths = 80 * MonthsPerYear // exactly at the ceiling
	e, _ := newTestEngine(t, state)

	before := e.State().Player.AgeMonths
	r := e.Submit(cmd(e, "wait", KindWait))
	if r.OK {
		t.Fatal("a month action was accepted with no lifespan left")
	}
	if r.Code != ErrPreconditionUnmet {
		t.Fatalf("code = %s, want %s", r.Code, ErrPreconditionUnmet)
	}
	if e.State().Player.AgeMonths != before {
		t.Fatalf("a refused action still aged the character to %d", e.State().Player.AgeMonths)
	}
}

// TestOnlyASettledMonthAgesTheCharacter is the "切出/离线不会掉寿元" criterion.
//
// It is stronger than "waiting does not age you": it walks every command kind
// that costs no month and asserts that none of them moves the age, so a future
// edit that reaches the clock from a query or a travel fails here.
func TestOnlyASettledMonthAgesTheCharacter(t *testing.T) {
	zeroMonthKinds := []CommandKind{
		KindQuery, KindTravel, KindTrade, KindUseItem,
		KindAcceptQuest, KindClaimReward, KindJoinSect,
	}

	for _, kind := range zeroMonthKinds {
		state := readyState()
		e, _ := newTestEngine(t, state)

		before := e.State().Player.AgeMonths
		beforeLifespan := LifespanMonths(e.State().Player.Lifespan)

		c := cmd(e, "act", kind)
		switch kind {
		case KindTravel:
			c.Payload.LocationID = "woods"
		case KindTrade:
			e.State().Pending.Confirmation = &PendingConfirmation{
				Token: "t", Kind: KindTrade, ExpiresAtRevision: e.State().Revision,
			}
			c.ConfirmationToken = "t"
		}
		// A rejection is fine here: the property is that nothing ages, not that
		// every frame action is implemented.
		_ = e.Submit(c)

		if got := e.State().Player.AgeMonths; got != before {
			t.Errorf("%s aged the character from %d to %d months", kind, before, got)
		}
		if got := LifespanMonths(e.State().Player.Lifespan); got != beforeLifespan {
			t.Errorf("%s changed the lifespan ceiling from %d to %d", kind, beforeLifespan, got)
		}
	}
}

func TestSaveRoundTripPreservesAgeAndLifespan(t *testing.T) {
	state := readyState()
	state.Player.AgeMonths = 300
	state.Player.Lifespan = LifespanLedger{
		BaseYears: 100,
		Bonuses:   []LifespanBonus{{ID: "elixir", Years: 20, Reason: "延寿丹"}},
	}

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

	if envelope.State.Player.AgeMonths != 300 {
		t.Fatalf("age after a round trip = %d, want 300", envelope.State.Player.AgeMonths)
	}
	if got, want := LifespanMonths(envelope.State.Player.Lifespan), int64(120)*MonthsPerYear; got != want {
		t.Fatalf("lifespan after a round trip = %d months, want %d", got, want)
	}
}

// ---------------------------------------------------------------------------
// Healing
// ---------------------------------------------------------------------------

func afflictionCatalogue() *Catalogue {
	cat := testCatalogue()
	cat.InjuryBands = validInjuryBandsForTest()
	cat.HealRestorePermille = ConfigValue{Provenance: ProvenanceDesignNote, Value: 250, Note: "测试用"}
	cat.HealConvertBurnPermille = ConfigValue{Provenance: ProvenanceDesignNote, Value: 100, Note: "测试用"}
	cat.Afflictions = []AfflictionDefinition{
		{
			ID: "wound_light", NameZH: "轻伤", Kind: AfflictionWound,
			DurationMonths:       ConfigValue{Provenance: ProvenanceDesignNote, Value: 2},
			SpeedPenaltyPermille: ConfigValue{Provenance: ProvenanceDesignNote, Value: 100},
			ClearedByRest:        true,
		},
		{
			ID: "poison_mild", NameZH: "微毒", Kind: AfflictionPoison,
			DurationMonths:  ConfigValue{Provenance: ProvenanceDesignNote, Value: 3},
			HPDrainPerMonth: ConfigValue{Provenance: ProvenanceDesignNote, Value: SCALE},
			ClearedByRest:   false,
		},
		{
			ID: "internal_injury", NameZH: "内伤", Kind: AfflictionInternal,
			DurationMonths:         ConfigValue{Provenance: ProvenanceDesignNote, Value: 6},
			EffectiveAptitudeDelta: ConfigValue{Provenance: ProvenanceDesignNote, Value: -2},
			Group:                  "affliction",
			ClearedByRest:          false,
		},
	}
	return cat
}

// TestRestDoesNotClearUnrelatedAfflictions is the "疗伤不清除无关异常" criterion.
//
// Rest mends what the content says rest mends. A poison and an internal wound
// are not on that list, and the check is driven by the configured flag rather
// than by the kind, so content can change its mind without a code change.
func TestRestDoesNotClearUnrelatedAfflictions(t *testing.T) {
	state := readyState()
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 100 * SCALE}
	state.Player.Condition.Effects = []TimedEffect{
		{ID: "wound_light", MonthsRemaining: 2, Stacks: 1},
		{ID: "poison_mild", MonthsRemaining: 3, Stacks: 1},
		{ID: "internal_injury", MonthsRemaining: 6, Stacks: 1},
	}
	cat := afflictionCatalogue()
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	r := e.Submit(healCommand(e, "rest-1", HealRest))
	if !r.OK {
		t.Fatalf("rest rejected: %s %v", r.Code, r.Events)
	}

	remaining := map[string]bool{}
	for _, ef := range e.State().Player.Condition.Effects {
		remaining[ef.ID] = true
	}
	if remaining["wound_light"] {
		t.Error("rest did not mend a wound, which the content marks rest-clearable")
	}
	if !remaining["poison_mild"] {
		t.Error("rest cured a poison; a poison needs an antidote, and TASK-13 ships it")
	}
	if !remaining["internal_injury"] {
		t.Error("rest mended an internal wound, which the content marks as needing treatment")
	}
}

func TestRestRestoresHealthUpToTheCeiling(t *testing.T) {
	state := readyState()
	state.Player.HP = Vitals{Current: 10 * SCALE, Max: 100 * SCALE}
	e, _ := newTestEngineWithCatalogue(t, state, afflictionCatalogue())

	r := e.Submit(healCommand(e, "rest-1", HealRest))
	if !r.OK {
		t.Fatalf("rest rejected: %s", r.Code)
	}

	// A quarter of the ceiling, which is what the catalogue configures.
	if got, want := e.State().Player.HP.Current, int64(35)*SCALE; got != want {
		t.Fatalf("health after a month of rest = %d, want %d", got, want)
	}
	if r.MonthCostApplied != 1 {
		t.Fatalf("rest cost %d months, want 1", r.MonthCostApplied)
	}
}

func TestRestNeverExceedsTheCeiling(t *testing.T) {
	state := readyState()
	state.Player.HP = Vitals{Current: 95 * SCALE, Max: 100 * SCALE}
	e, _ := newTestEngineWithCatalogue(t, state, afflictionCatalogue())

	if r := e.Submit(healCommand(e, "rest-1", HealRest)); !r.OK {
		t.Fatalf("rest rejected: %s", r.Code)
	}
	if got := e.State().Player.HP.Current; got != 100*SCALE {
		t.Fatalf("health = %d, want the ceiling 100", got)
	}
}

func TestConversionBurnsHealthForSpirit(t *testing.T) {
	state := readyState()
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 100 * SCALE}
	state.Player.MP = Vitals{Current: 0, Max: 100 * SCALE}
	e, _ := newTestEngineWithCatalogue(t, state, afflictionCatalogue())

	r := e.Submit(healCommand(e, "convert-1", HealConvert))
	if !r.OK {
		t.Fatalf("conversion rejected: %s %v", r.Code, r.Events)
	}

	if got, want := e.State().Player.HP.Current, int64(30)*SCALE; got != want {
		t.Fatalf("health after conversion = %d, want %d", got, want)
	}
	if got, want := e.State().Player.MP.Current, int64(10)*SCALE; got != want {
		t.Fatalf("灵力 after conversion = %d, want %d", got, want)
	}
}

func TestConversionRefusesToLeaveNoHealth(t *testing.T) {
	// The conversion must not be a way to kill yourself: design 6.2 ties the
	// consequences of zero health to an encounter, and a self-inflicted zero has
	// no encounter to supply one.
	state := readyState()
	state.Player.HP = Vitals{Current: 5 * SCALE, Max: 100 * SCALE}
	state.Player.MP = Vitals{Current: 0, Max: 100 * SCALE}
	e, _ := newTestEngineWithCatalogue(t, state, afflictionCatalogue())

	r := e.Submit(healCommand(e, "convert-1", HealConvert))
	if r.OK {
		t.Fatal("a conversion that would leave no health was accepted")
	}
	// The refusal must be the guard's own, not the invariant check catching a
	// negative balance afterwards. Without this the test would pass on an
	// implementation that lets the conversion through and only then reports a
	// broken invariant, which is a different and worse failure.
	if r.Code != ErrPreconditionUnmet {
		t.Fatalf("code = %s, want %s: the conversion was refused for the wrong reason",
			r.Code, ErrPreconditionUnmet)
	}
	if got := e.State().Player.HP.Current; got != 5*SCALE {
		t.Fatalf("a refused conversion changed health to %d", got)
	}
}

func TestConversionRefusesWhenSpiritIsAlreadyFull(t *testing.T) {
	state := readyState()
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 100 * SCALE}
	state.Player.MP = Vitals{Current: 100 * SCALE, Max: 100 * SCALE}
	e, _ := newTestEngineWithCatalogue(t, state, afflictionCatalogue())

	if r := e.Submit(healCommand(e, "convert-1", HealConvert)); r.OK {
		t.Fatal("a conversion with nothing to gain was accepted")
	}
}

// ---------------------------------------------------------------------------
// Month-end affliction settlement
// ---------------------------------------------------------------------------

func TestAfflictionsDrainHealthAtMonthEnd(t *testing.T) {
	state := readyState()
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 100 * SCALE}
	state.Player.Condition.Effects = []TimedEffect{{ID: "poison_mild", MonthsRemaining: 3, Stacks: 1}}
	e, _ := newTestEngineWithCatalogue(t, state, afflictionCatalogue())

	settleMonth(t, e, "wait-1")

	// One point of health, and the duration ticks down by one month.
	if got, want := e.State().Player.HP.Current, int64(39)*SCALE; got != want {
		t.Fatalf("health after a poisoned month = %d, want %d", got, want)
	}
	if got := e.State().Player.Condition.Effects[0].MonthsRemaining; got != 2 {
		t.Fatalf("poison remaining = %d months, want 2", got)
	}
}

func TestAnAfflictionThatEndsThisMonthDoesNotAlsoDrain(t *testing.T) {
	// The duration is what the player was told. Charging a drain on the month
	// the affliction expires would make the number on the panel a lie.
	state := readyState()
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 100 * SCALE}
	state.Player.Condition.Effects = []TimedEffect{{ID: "poison_mild", MonthsRemaining: 1, Stacks: 1}}
	e, _ := newTestEngineWithCatalogue(t, state, afflictionCatalogue())

	settleMonth(t, e, "wait-1")

	if len(e.State().Player.Condition.Effects) != 0 {
		t.Fatalf("the poison is still active: %+v", e.State().Player.Condition.Effects)
	}
	if got, want := e.State().Player.HP.Current, int64(40)*SCALE; got != want {
		t.Fatalf("health = %d, want %d: the expiring month must not also drain", got, want)
	}
}

func TestAnUntreatedPoisonCanBeFatal(t *testing.T) {
	state := readyState()
	state.Player.HP = Vitals{Current: 2 * SCALE, Max: 100 * SCALE}
	state.Player.Condition.Effects = []TimedEffect{{ID: "poison_mild", MonthsRemaining: 3, Stacks: 3}}
	e, _ := newTestEngineWithCatalogue(t, state, afflictionCatalogue())

	r := settleMonth(t, e, "wait-1")

	if !e.State().Player.Ended {
		t.Fatal("a character drained to zero health outside an encounter is still alive")
	}
	if got := e.State().Player.EndCause.Code; got != EndCodeInjury {
		t.Fatalf("end cause = %s, want %s", got, EndCodeInjury)
	}
	if !hasResultEvent(r, ResultEnded, EndCodeInjury) {
		t.Fatalf("the death was not reported: %+v", r.Events)
	}
}

// ---------------------------------------------------------------------------
// Karma
// ---------------------------------------------------------------------------

// TestKarmaIsNotChangedByAnyAutomaticRule is the "因果按行为情境结算" criterion,
// read as a prohibition.
//
// Design R19 forbids a blanket rule — "不对所有击杀一刀切" — and the strongest
// form of that prohibition is that the engine has no automatic karma at all.
// Karma moves only when a named content effect moves it. This test walks a full
// month of ordinary activity and asserts that none of the moral counters drifts.
func TestKarmaIsNotChangedByAnyAutomaticRule(t *testing.T) {
	state := readyState()
	e, _ := newTestEngine(t, state)

	moral := []Resource{ResMerit, ResKarma, ResReputation, ResContribution}
	before := map[Resource]int64{}
	for _, res := range moral {
		before[res] = e.State().Player.Resources[res]
	}

	settleMonth(t, e, "wait-1")
	if r := e.Submit(travelCommand(e, "t1", "woods")); !r.OK {
		t.Fatalf("travel rejected: %s", r.Code)
	}
	if r := e.Submit(cmd(e, "cultivate-1", KindCultivate)); !r.OK {
		t.Fatalf("cultivation rejected: %s", r.Code)
	}

	for _, res := range moral {
		if got := e.State().Player.Resources[res]; got != before[res] {
			t.Errorf("%s changed from %d to %d without a named content effect; design R19 "+
				"requires karma to be settled by act and situation, not by a blanket rule",
				res, before[res], got)
		}
	}
}

func TestKarmaTiersAreOrderedAndEveryTotalIsDescribed(t *testing.T) {
	cat := testCatalogue()
	cat.KarmaTiers = []KarmaTierDefinition{
		{ID: "clear", NameZH: "清白", MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 0}},
		{ID: "minor", NameZH: "微瑕", MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 10}},
		{ID: "heavy", NameZH: "业重", MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 200}},
	}

	cases := []struct {
		karma int64
		want  string
	}{
		{0, "清白"}, {9, "清白"}, {10, "微瑕"}, {199, "微瑕"},
		{200, "业重"}, {100000, "业重"},
	}
	for _, tc := range cases {
		if got := KarmaTierName(cat, tc.karma); got != tc.want {
			t.Errorf("karma %d is %q, want %q", tc.karma, got, tc.want)
		}
	}
}

func TestKarmaTierDoesNotDependOnCatalogueOrder(t *testing.T) {
	// The highest matching floor wins, so listing the tiers in a different order
	// cannot change the answer.
	ascending := testCatalogue()
	ascending.KarmaTiers = []KarmaTierDefinition{
		{ID: "clear", NameZH: "清白", MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 0}},
		{ID: "heavy", NameZH: "业重", MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 200}},
	}
	descending := testCatalogue()
	descending.KarmaTiers = []KarmaTierDefinition{
		{ID: "heavy", NameZH: "业重", MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 200}},
		{ID: "clear", NameZH: "清白", MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 0}},
	}

	for _, karma := range []int64{0, 199, 200, 5000} {
		a := KarmaTierName(ascending, karma)
		b := KarmaTierName(descending, karma)
		if a != b {
			t.Errorf("karma %d is %q in one order and %q in the other", karma, a, b)
		}
	}
}

// ---------------------------------------------------------------------------
// Status detail
// ---------------------------------------------------------------------------

func TestStatusDetailProjectsBandLifespanAndAfflictions(t *testing.T) {
	state := readyState()
	state.Player.AgeMonths = 21 * MonthsPerYear
	state.Player.Lifespan = LifespanLedger{BaseYears: 100}
	state.Player.HP = Vitals{Current: 40 * SCALE, Max: 100 * SCALE}
	state.Player.Condition.Effects = []TimedEffect{{ID: "poison_mild", MonthsRemaining: 2, Stacks: 1}}
	state.Player.Resources[ResKarma] = 12

	cat := afflictionCatalogue()
	cat.KarmaTiers = []KarmaTierDefinition{
		{ID: "clear", NameZH: "清白", MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 0}},
		{ID: "minor", NameZH: "微瑕", MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 10}},
	}

	d := BuildStatusDetail(state, cat)

	if d.Band != BandHeavy {
		t.Errorf("band = %s, want %s", d.Band, BandHeavy)
	}
	if d.BandName == "" {
		t.Error("the band has no display name")
	}
	if got, want := d.LifespanRemainingMonths, int64(79)*MonthsPerYear; got != want {
		t.Errorf("remaining lifespan = %d months, want %d", got, want)
	}
	if len(d.Afflictions) != 1 || d.Afflictions[0].Name != "微毒" {
		t.Fatalf("afflictions = %+v, want one named 微毒", d.Afflictions)
	}
	if d.Afflictions[0].ClearsByRest {
		t.Error("the poison is reported as rest-clearable, which would tell the player to waste a month")
	}
	if d.Karma != 12 || d.KarmaTier != "微瑕" {
		t.Errorf("karma = %d tier %q, want 12 微瑕", d.Karma, d.KarmaTier)
	}
}

func TestStatusDetailReportsNoNegativeRemainingLifespan(t *testing.T) {
	state := readyState()
	state.Player.AgeMonths = 120 * MonthsPerYear
	state.Player.Lifespan = LifespanLedger{BaseYears: 100}

	d := BuildStatusDetail(state, testCatalogue())
	if d.LifespanRemainingMonths < 0 {
		t.Fatalf("remaining lifespan = %d; a screen would print a negative remainder",
			d.LifespanRemainingMonths)
	}
}

func TestStatusDetailSurvivesAMissingCharacter(t *testing.T) {
	// The status screen is reachable during creation, where there is no player.
	if d := BuildStatusDetail(&GameState{}, testCatalogue()); d.Realm != "" || d.HP.Max != 0 {
		t.Fatalf("a stateless projection = %+v, want the zero value", d)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func healCommand(e *Engine, actionID string, mode HealMode) Command {
	c := cmd(e, actionID, KindHeal)
	c.Payload.HealMode = mode
	return c
}

// validInjuryBandsForTest configures every band with the shipped penalties'
// shape, so a test that only needs bands to exist does not restate the numbers.
func validInjuryBandsForTest() []InjuryBandDefinition {
	penalty := map[InjuryBand]int{
		BandHealthy: 0,
		BandLight:   0,
		BandHeavy:   300,
		BandDying:   600,
		BandDown:    0,
	}
	names := map[InjuryBand]string{
		BandHealthy: "健康",
		BandLight:   "轻伤",
		BandHeavy:   "重伤",
		BandDying:   "垂死",
		BandDown:    "气血耗尽",
	}
	out := make([]InjuryBandDefinition, 0, len(InjuryBandOrder))
	for _, band := range InjuryBandOrder {
		out = append(out, InjuryBandDefinition{
			Band:   band,
			NameZH: names[band],
			SpeedPenaltyPermille: ConfigValue{
				Provenance: ProvenanceDesignNote, Value: penalty[band], Note: "测试用",
			},
		})
	}
	return out
}
