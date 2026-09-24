package engine

import (
	"encoding/json"
	"testing"
)

// manuscript and trial are local stand-ins for the content package's
// constructors, which the engine package cannot import (content imports
// engine). They produce the same ConfigValue shape.
func manuscript(v int) ConfigValue {
	return ConfigValue{Provenance: ProvenanceManuscript, Value: v}
}

func trial(v int, note string) ConfigValue {
	return ConfigValue{Provenance: ProvenanceTrial, Value: v, Note: note}
}

// creationTestCatalogue is a self-contained catalogue exercising every creation
// rule without importing internal/content (which would be a cycle). It mirrors
// the shipped M1 shape: one origin with a stat grant, one constitution, a
// talent with a multiplicative rate grant, and a talent with an additive
// attribute grant.
func creationTestCatalogue() *Catalogue {
	return &Catalogue{
		Version:                  ContentVersion,
		M1Paths:                  []Path{PathHuman},
		CultivationMoodThreshold: ConfigValue{Provenance: ProvenanceDesignNote, Value: StartingMood, Note: "test mood threshold"},
		Realms: []RealmDefinition{{
			ID: RealmQiRefining, NameZH: "炼气", Order: 0,
			LifespanYears: manuscript(100),
			MonthBase:     manuscript(10 * SCALE),
			TierThreshold: trial(100*SCALE, "test"),
		}},
		Origins: []OriginDefinition{
			{ID: OriginCommoner, NameZH: "平民"},
			{ID: OriginMerchant, NameZH: "商贾", Effects: []GrantEffect{{
				Kind: GrantAdditive, Target: "resources.spirit_stones",
				Amount: 300, Reason: "test stones",
			}}},
			{ID: OriginHunter, NameZH: "猎户", Effects: []GrantEffect{{
				Kind: GrantPermanent, Target: "hp.max",
				Amount: 20, Reason: "test hp max",
			}}},
		},
		SpiritRoots: []SpiritRootDefinition{
			{ID: RootTrue, NameZH: "真灵根", Multiplier: manuscript(13 * SCALE / 10)},
			{ID: RootHeavenly, NameZH: "天灵根", Multiplier: manuscript(20 * SCALE / 10)},
		},
		Techniques: []TechniqueDefinition{{
			ID: "yellow_breath", NameZH: "吐纳诀", Grade: GradeYellow, IsPrimary: true,
			GradeMultiplier: manuscript(SCALE),
		}},
		Constitutions: []ConstitutionDefinition{
			{ID: "ordinary", NameZH: "凡体"},
			{ID: "frail", NameZH: "体弱", Effects: []GrantEffect{{
				Kind: GrantAdditive, Target: "attributes.constitution",
				Amount: -2, Reason: "test frail",
			}}},
		},
		Talents: []TalentDefinition{
			{ID: "sharp_mind", NameZH: "灵慧", Effects: []GrantEffect{{
				Kind: GrantAdditive, Target: "attributes.comprehension",
				Amount: 3, Reason: "test comprehension",
			}}},
			{ID: "great_mind", NameZH: "天授", Effects: []GrantEffect{{
				Kind: GrantAdditive, Target: "attributes.comprehension",
				Amount: 6, Reason: "test large comprehension",
			}}},
			{ID: "robust_frame", NameZH: "壮体", Effects: []GrantEffect{{
				Kind: GrantAdditive, Target: "attributes.strength",
				Amount: 3, Reason: "test strength",
			}}},
			{ID: "innate_dao_body", NameZH: "先天道体", Effects: []GrantEffect{{
				Kind: GrantMultiplicative, Target: "cultivation_rate",
				Amount: 15 * SCALE / 10, Reason: "test dao body",
			}}, Exclusive: []string{"innate_dao_body"}},
		},
	}
}

// creationState builds a CREATION-phase state with the default draft, so the
// creation tests exercise the real pipeline rather than calling helpers
// directly.
func creationState() *GameState {
	return &GameState{
		SchemaVersion:  SchemaVersion,
		RulesVersion:   RulesVersion,
		ContentVersion: ContentVersion,
		GameID:         "game-create",
		BranchID:       "branch-create",
		Revision:       1,
		Phase:          PhaseCreation,
		RNG:            NewRNGSet(SeedFromIdentity("game-create", "branch-create")).Snapshot(),
		Player:         nil,
		World: &World{
			NPCs:             map[string]NPC{},
			VisitedLocations: []string{"village"},
			CurrentLocation:  "village",
			Quests:           map[string]QuestState{},
			Flags:            map[string]bool{},
		},
		Pending: PendingState{Creation: NewCreationDraft("game-create", "branch-create")},
		Idempotency: IdempotencyTable{
			Entries: map[string]IdempotencyEntry{},
			Limit:   DefaultIdempotencyLimit,
		},
	}
}

// newCreationEngine wires an engine over the creation test catalogue.
func newCreationEngine(t *testing.T) (*Engine, *MemoryStore) {
	t.Helper()
	state := creationState()
	store := NewMemoryStore(state)
	e := NewEngine(store, creationTestCatalogue(), state)
	return e, store
}

// creationCmd builds a creation command bound to the current revision and token.
func creationCmd(e *Engine, actionID string, kind CommandKind, cp *CreationPayload) Command {
	c := cmd(e, actionID, kind)
	c.Payload.Creation = cp
	return c
}

// --- The acceptance criteria -------------------------------------------------

// TestFirstScreenCarriesAuthorByline is acceptance criterion one: the first
// creation screen is signed 作者：雾见川.
func TestFirstScreenCarriesAuthorByline(t *testing.T) {
	if CreationAuthor != "雾见川" {
		t.Fatalf("author is %q, want 雾见川", CreationAuthor)
	}
	if CreationAuthorLine != "作者：雾见川" {
		t.Fatalf("author line is %q, want 作者：雾见川", CreationAuthorLine)
	}
}

// TestBaseTotalMustBeExactlySixty is the acceptance rule that 59 and 61 are
// both unsubmittable. It checks the arithmetic layer directly and then confirms
// the same allocation is refused by the pipeline, so the rule is enforced at
// both the validator and the command boundary.
func TestBaseTotalMustBeExactlySixty(t *testing.T) {
	cat := creationTestCatalogue()

	cases := []struct {
		name  string
		base  BaseAttributes
		total int
	}{
		{"fifty-nine", BaseAttributes{10, 10, 10, 10, 10, 9}, 59},
		{"sixty", BaseAttributes{10, 10, 10, 10, 10, 10}, 60},
		{"sixty-one", BaseAttributes{10, 10, 10, 10, 10, 11}, 61},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.base.Total(); got != tc.total {
				t.Fatalf("fixture total is %d, want %d", got, tc.total)
			}
			r := ValidateBase(tc.base)
			wantOK := tc.total == CreationBasePoints
			if r.OK() != wantOK {
				t.Fatalf("ValidateBase(total=%d).OK()=%v, want %v (defects=%+v)",
					tc.total, r.OK(), wantOK, r.Defects)
			}
			if !wantOK && !r.Has(CErrPointTotal) {
				t.Fatalf("total %d did not report CErrPointTotal; codes=%v",
					tc.total, r.Codes())
			}

			// The same allocation through the full validator.
			sel := presetSelection("试", "道者", tc.base)
			sel.Origin = OriginCommoner
			rep := ValidateCreation(cat, sel)
			if rep.OK() != wantOK {
				t.Fatalf("ValidateCreation(total=%d).OK()=%v, want %v (defects=%+v)",
					tc.total, rep.OK(), wantOK, rep.Defects)
			}
		})
	}
}

// TestPipelineRefusesFiftyNineAndSixtyOneAndTwentyOne is the end-to-end form of
// the acceptance rule: through the real command pipeline, an allocation whose
// total is 59 or 61, or whose final value reaches 21, is refused and the draft
// is left untouched.
func TestPipelineRefusesFiftyNineAndSixtyOneAndTwentyOne(t *testing.T) {
	cases := []struct {
		name string
		sel  CreationSelection
	}{
		{
			name: "total-fifty-nine",
			sel:  CreationSelection{Strength: 10, Agility: 10, ConstitutionAttr: 10, Comprehension: 10, Aptitude: 10, Fortune: 9},
		},
		{
			name: "total-sixty-one",
			sel:  CreationSelection{Strength: 10, Agility: 10, ConstitutionAttr: 10, Comprehension: 10, Aptitude: 10, Fortune: 11},
		},
		{
			// Comprehension 15 base + great_mind(+6) = 21, one over the final
			// ceiling. The total is 60 and every base is legal, so this case
			// isolates the final-range rule rather than tripping the others.
			name: "final-twenty-one",
			sel:  CreationSelection{Strength: 15, Agility: 2, ConstitutionAttr: 2, Comprehension: 15, Aptitude: 11, Fortune: 15, AgeYears: 21, TalentIDs: []string{"great_mind"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := newCreationEngine(t)
			before := CloneGameState(e.State())

			r := e.Submit(creationCmd(e, "act-1", KindCreateEdit,
				&CreationPayload{Selection: tc.sel}))
			if r.OK {
				t.Fatalf("the pipeline accepted an illegal allocation %+v", tc.sel)
			}
			if r.Code != ErrPreconditionUnmet {
				t.Fatalf("rejection code is %q, want %q (events=%+v)",
					r.Code, ErrPreconditionUnmet, r.Events)
			}

			// Nothing was written: no revision move, no draft change.
			after := e.State()
			if after.Revision != before.Revision {
				t.Fatalf("revision moved from %d to %d on a rejected edit",
					before.Revision, after.Revision)
			}
			if !sameDraft(before.Pending.Creation, after.Pending.Creation) {
				t.Fatalf("the draft changed despite the rejection:\nbefore=%+v\nafter=%+v",
					before.Pending.Creation, after.Pending.Creation)
			}
		})
	}
}

// TestFinalTwentyOneIsRefusedSeparately pins the final-range rule on its own,
// so a regression that only kept the 60-point check would be visible.
func TestFinalTwentyOneIsRefusedSeparately(t *testing.T) {
	cat := creationTestCatalogue()

	// A 60-point allocation with every base inside 1..15. Base comprehension is
	// the ceiling 15, which is legal on its own.
	sel := CreationSelection{
		Surname: "试", GivenName: "道者", AgeYears: 21,
		Origin: OriginCommoner, Path: PathHuman, SpiritRoot: RootTrue,
		Constitution: "ordinary",
		Strength:     15, Agility: 2, ConstitutionAttr: 2,
		Comprehension: 15, Aptitude: 11, Fortune: 15,
	}
	if sel.Base().Total() != CreationBasePoints {
		t.Fatalf("fixture total is %d, want 60", sel.Base().Total())
	}

	// Without a talent the final comprehension is 15, which is legal.
	if r := ValidateCreation(cat, sel); !r.OK() {
		t.Fatalf("a legal 60-point allocation was rejected: %+v", r.Defects)
	}

	// With a +6 talent it becomes 21 and must be refused for the range rule.
	sel.TalentIDs = []string{"great_mind"}
	r := ValidateCreation(cat, sel)
	if r.OK() {
		t.Fatal("comprehension 21 after a +6 talent was accepted")
	}
	if !r.Has(CErrFinalRange) {
		t.Fatalf("expected CErrFinalRange, got %v (%+v)", r.Codes(), r.Defects)
	}
	if r.Has(CErrPointTotal) || r.Has(CErrBaseRange) {
		t.Fatalf("another rule also fired, so the case is not isolated: %v", r.Codes())
	}
}

// TestBaseRangeBoundaries pins 1..15 on the base attributes: 0 and 16 are both
// refused, and the rule is distinct from the total rule.
func TestBaseRangeBoundaries(t *testing.T) {
	cat := creationTestCatalogue()

	// Comprehension 16 requires giving back a point elsewhere to keep the
	// total at 60, so the only defect is the range.
	sel := presetSelection("试", "道者", BaseAttributes{10, 10, 10, 16, 10, 4})
	sel.Origin = OriginCommoner
	r := ValidateCreation(cat, sel)
	if r.OK() {
		t.Fatal("a base of 16 was accepted")
	}
	if !r.Has(CErrBaseRange) {
		t.Fatalf("expected CErrBaseRange, got %v", r.Codes())
	}
	if r.Has(CErrPointTotal) {
		t.Fatalf("the total rule fired too, so the case is not isolated: %v", r.Codes())
	}

	// A base of 0 is also refused. The point is moved to fortune so the total
	// still holds.
	sel = presetSelection("试", "道者", BaseAttributes{0, 12, 12, 12, 12, 12})
	sel.Origin = OriginCommoner
	r = ValidateCreation(cat, sel)
	if r.OK() {
		t.Fatal("a base of 0 was accepted")
	}
	if !r.Has(CErrBaseRange) {
		t.Fatalf("expected CErrBaseRange for a base of 0, got %v", r.Codes())
	}
}

// TestCreationCostsNoMonth is acceptance criterion "创角不耗月": neither drafting
// nor confirming advances the world month, ages the character, or draws a
// random stream.
func TestCreationCostsNoMonth(t *testing.T) {
	e, _ := newCreationEngine(t)

	startMonth := e.State().Counters.WorldMonth
	startRNG := rngCounters(e.State())

	// Step one: edit the descriptive fields.
	sel := CreationSelection{Surname: "云", GivenName: "长生", Gender: "custom-gender", AgeYears: 21}
	r := e.Submit(creationCmd(e, "edit-1", KindCreateEdit, &CreationPayload{Selection: sel}))
	if !r.OK {
		t.Fatalf("a legal edit was rejected: %s", r.Code)
	}

	// Step two: advance to the details step.
	r = e.Submit(creationCmd(e, "edit-2", KindCreateEdit,
		&CreationPayload{AdvanceStep: true}))
	if !r.OK {
		t.Fatalf("advancing a step was rejected: %s", r.Code)
	}

	// Step three: confirm.
	r = e.Submit(creationCmd(e, "confirm-1", KindCreateConfirm,
		&CreationPayload{Confirm: true}))
	if !r.OK {
		t.Fatalf("confirming a legal draft was rejected: %s", r.Code)
	}
	if r.MonthCostApplied != 0 {
		t.Fatalf("creation charged %d month(s)", r.MonthCostApplied)
	}

	s := e.State()
	if s.Counters.WorldMonth != startMonth {
		t.Fatalf("world month advanced from %d to %d during creation",
			startMonth, s.Counters.WorldMonth)
	}
	if s.Phase != PhaseReady {
		t.Fatalf("phase is %q after confirmation, want %q", s.Phase, PhaseReady)
	}
	if s.Player == nil {
		t.Fatal("no character was produced")
	}
	// The default age is 21, so the character is 21*12 = 252 months old. The
	// age must be the chosen one, not one month more.
	if want := int64(21 * MonthsPerYear); s.Player.AgeMonths != want {
		t.Fatalf("age is %d months, want %d", s.Player.AgeMonths, want)
	}
	if got := rngCounters(s); got != startRNG {
		t.Fatalf("creation advanced a random stream: %s -> %s", startRNG, got)
	}
}

// TestCustomGenderIsNotCoerced is acceptance criterion "自定义性别不被强改". The
// engine stores whatever gender it is given and has no code path that rewrites
// it to a binary pair.
func TestCustomGenderIsNotCoerced(t *testing.T) {
	const custom = "无性别/自定"

	e, _ := newCreationEngine(t)

	sel := CreationSelection{
		Surname: "云", GivenName: "长生", Gender: custom, AgeYears: 21,
	}
	if r := e.Submit(creationCmd(e, "edit-1", KindCreateEdit,
		&CreationPayload{Selection: sel})); !r.OK {
		t.Fatalf("a custom gender was rejected: %s", r.Code)
	}
	if r := e.Submit(creationCmd(e, "edit-2", KindCreateEdit,
		&CreationPayload{AdvanceStep: true})); !r.OK {
		t.Fatalf("advancing a step was rejected: %s", r.Code)
	}
	if r := e.Submit(creationCmd(e, "confirm-1", KindCreateConfirm,
		&CreationPayload{Confirm: true})); !r.OK {
		t.Fatalf("confirmation was rejected: %s", r.Code)
	}

	if got := e.State().Player.Identity.Gender; got != custom {
		t.Fatalf("gender is %q, want it preserved as %q", got, custom)
	}
}

// TestResumeDoesNotReRoll is acceptance criterion "刷新/恢复不重复随机初始化".
// The fixed allocation survives a round trip through the persistence encoding,
// and an attempt to change a fixed attribute is refused rather than honoured.
func TestResumeDoesNotReRoll(t *testing.T) {
	e, _ := newCreationEngine(t)

	// Fix the allocation at the preset's values.
	first := SelectionFromDraft(e.State().Pending.Creation)
	base := first.Base()
	if base.Total() != CreationBasePoints {
		t.Fatalf("preset total is %d, want 60", base.Total())
	}

	// Round trip the draft through the persistence encoding, which is what a
	// resume does.
	restoredDraft, ok := roundTripDraft(t, e.State().Pending.Creation)
	if !ok {
		t.Fatal("the creation draft did not survive a round trip")
	}

	// The restored draft's allocation is identical.
	got, fixed := restoredDraft.FixedAllocation()
	if !fixed {
		t.Fatal("the restored draft has no fixed allocation")
	}
	if got != base {
		t.Fatalf("restored allocation is %+v, want %+v (a resume re-rolled something)",
			got, base)
	}

	// A command that tries to move a fixed attribute must be refused. The
	// restored draft is installed in a fresh engine, which is the resume path.
	restored := CloneGameState(e.State())
	restored.Pending.Creation = restoredDraft
	e2 := NewEngine(NewMemoryStore(restored), creationTestCatalogue(), restored)

	changed := first
	changed.Strength = first.Strength + 1
	changed.Fortune = first.Fortune - 1
	if changed.Base().Total() != CreationBasePoints {
		t.Fatalf("the change fixture broke the total: %d", changed.Base().Total())
	}
	r := e2.Submit(creationCmd(e2, "edit-reroll", KindCreateEdit,
		&CreationPayload{Selection: changed}))
	if r.OK {
		t.Fatal("a resumed draft allowed a fixed attribute to be re-rolled")
	}
	if !hasEventDetail(r.Events, "already fixed") {
		t.Fatalf("the rejection did not name the fixed value: %+v", r.Events)
	}
}

// TestPresetConfirmsInThreeActions is the quick-entry target from design 12.1:
// the default preset confirms within three actions.
func TestPresetConfirmsInThreeActions(t *testing.T) {
	e, _ := newCreationEngine(t)

	// Action 1: keep the default preset, set the name.
	r := e.Submit(creationCmd(e, "a1", KindCreateEdit, &CreationPayload{
		Selection: CreationSelection{Surname: "云", GivenName: "长生"},
	}))
	if !r.OK {
		t.Fatalf("action 1 was rejected: %s", r.Code)
	}

	// Action 2: move to the details step.
	r = e.Submit(creationCmd(e, "a2", KindCreateEdit, &CreationPayload{AdvanceStep: true}))
	if !r.OK {
		t.Fatalf("action 2 was rejected: %s", r.Code)
	}

	// Action 3: confirm.
	r = e.Submit(creationCmd(e, "a3", KindCreateConfirm, &CreationPayload{Confirm: true}))
	if !r.OK {
		t.Fatalf("action 3 was rejected: %s", r.Code)
	}
	if e.State().Player == nil {
		t.Fatal("no character after the three-action path")
	}
}

// TestPresetsAllPassTheSameValidation is the design rule that a preset grants no
// hidden advantage: every preset must pass the identical 60-point validation.
func TestPresetsAllPassTheSameValidation(t *testing.T) {
	cat := creationTestCatalogue()

	presets := M1CreationPresets()
	if len(presets) != 3 {
		t.Fatalf("there are %d presets, want 3", len(presets))
	}
	seen := map[string]bool{}
	for _, p := range presets {
		if seen[p.ID] {
			t.Fatalf("preset id %q is duplicated", p.ID)
		}
		seen[p.ID] = true

		sel := p.Selection
		sel.Surname = "试"
		sel.GivenName = "道者"
		if sel.AgeYears == 0 {
			sel.AgeYears = CreationDefaultAgeYears
		}
		if sel.Base().Total() != CreationBasePoints {
			t.Fatalf("preset %q totals %d, want 60", p.ID, sel.Base().Total())
		}
		r := ValidateCreation(cat, sel)
		if !r.OK() {
			t.Fatalf("preset %q failed validation: %+v", p.ID, r.Defects)
		}
	}

	// The default preset must be one of them.
	if _, ok := PresetByID(DefaultPresetID); !ok {
		t.Fatalf("the default preset %q is not declared", DefaultPresetID)
	}
}

// TestYaoIntentJudgmentIsDeferredAndLocked is the design rule from 6.1: the
// 妖族 intent is recorded first, judged after the allocation, and cannot be
// re-rolled by refreshing.
func TestYaoIntentJudgmentIsDeferredAndLocked(t *testing.T) {
	e, _ := newCreationEngine(t)

	// Record the intent, with no verdict yet: this must succeed.
	r := e.Submit(creationCmd(e, "yao-1", KindCreateEdit, &CreationPayload{
		Selection: CreationSelection{Surname: "狐", GivenName: "九", YaoIntent: true},
	}))
	if !r.OK {
		t.Fatalf("recording a yao intent was rejected: %s", r.Code)
	}
	if _, has := e.State().Pending.Creation.FixedResults[FixedYaoVerdict]; has {
		t.Fatal("a verdict was locked before the allocation was known")
	}

	// Supply the verdict: it locks, together with the aptitude it was based on.
	r = e.Submit(creationCmd(e, "yao-2", KindCreateEdit, &CreationPayload{
		YaoVerdictPermille: 600, YaoVerdictGiven: true,
	}))
	if !r.OK {
		t.Fatalf("supplying a yao verdict was rejected: %s", r.Code)
	}
	locked, ok := e.State().Pending.Creation.FixedResults[FixedYaoVerdict]
	if !ok || locked != 600 {
		t.Fatalf("verdict is %v (present=%v), want 600 locked", locked, ok)
	}
	if _, ok := e.State().Pending.Creation.FixedResults[FixedAptitudeForYao]; !ok {
		t.Fatal("the aptitude the verdict was based on was not recorded")
	}

	// A second verdict must not overwrite the first: that is the anti-refresh
	// rule.
	r = e.Submit(creationCmd(e, "yao-3", KindCreateEdit, &CreationPayload{
		YaoVerdictPermille: 999, YaoVerdictGiven: true,
	}))
	if !r.OK {
		t.Fatalf("a repeat verdict should be ignored, not rejected: %s", r.Code)
	}
	after := e.State().Pending.Creation.FixedResults[FixedYaoVerdict]
	if after != 600 {
		t.Fatalf("the verdict was re-rolled to %d; refresh farming is possible", after)
	}

	// A failed judgment must block confirmation, and the draft must stay open
	// so the player can choose again.
	e2, _ := newCreationEngine(t)
	e2.Submit(creationCmd(e2, "y2-1", KindCreateEdit, &CreationPayload{
		Selection: CreationSelection{Surname: "狐", GivenName: "九", YaoIntent: true},
	}))
	e2.Submit(creationCmd(e2, "y2-2", KindCreateEdit, &CreationPayload{
		YaoVerdictPermille: 0, YaoVerdictGiven: true,
	}))
	e2.Submit(creationCmd(e2, "y2-3", KindCreateEdit, &CreationPayload{AdvanceStep: true}))
	r = e2.Submit(creationCmd(e2, "y2-4", KindCreateConfirm, &CreationPayload{Confirm: true}))
	if r.OK {
		t.Fatal("a draft whose yao judgment failed was still confirmed")
	}
	if e2.State().Phase != PhaseCreation {
		t.Fatalf("phase is %q after a failed judgment, want it to remain CREATION",
			e2.State().Phase)
	}
	if e2.State().Pending.Creation == nil {
		t.Fatal("the draft was discarded, so the player cannot choose again")
	}
}

// TestClosedPathIsRefusedAtCreation checks that a path M1 does not open is
// refused while drafting, not silently accepted and then rejected at confirm.
func TestClosedPathIsRefusedAtCreation(t *testing.T) {
	e, _ := newCreationEngine(t)

	r := e.Submit(creationCmd(e, "p1", KindCreateEdit, &CreationPayload{
		Selection: CreationSelection{Path: PathHeavenly},
	}))
	if r.OK {
		t.Fatal("a path closed in M1 was accepted during drafting")
	}
}

// TestWorldActionsAreRefusedDuringCreation confirms the phase gate: a month
// action during CREATION is refused, so no world month can be spent before a
// character exists.
func TestWorldActionsAreRefusedDuringCreation(t *testing.T) {
	e, _ := newCreationEngine(t)

	for _, kind := range []CommandKind{KindCultivate, KindHeal, KindWait, KindQuestRun} {
		r := e.Submit(cmd(e, "w-"+string(kind), kind))
		if r.OK {
			t.Fatalf("%s was accepted during creation", kind)
		}
		if r.Code != ErrBadPhase {
			t.Fatalf("%s was refused with %q, want %q", kind, r.Code, ErrBadPhase)
		}
	}
}

// TestCreationStateIsValidWithoutPlayer pins the structural rule the wizard
// depends on: CREATION must carry a draft and may omit the player.
func TestCreationStateIsValidWithoutPlayer(t *testing.T) {
	s := creationState()
	if report := ValidateState(s); !report.OK() {
		t.Fatalf("a valid creation state was rejected: %+v", report.Errors)
	}

	// Removing the draft must be a structural error, because the phase would
	// have nothing to render.
	s2 := creationState()
	s2.Pending.Creation = nil
	if report := ValidateState(s2); report.OK() {
		t.Fatal("CREATION without a draft was accepted")
	}
}

// TestCreationAgeBoundaries pins the 16..60 age rule from ADR-001 §2.1. 15 and
// 61 are refused; 16 and 60 are accepted. The rule is exercised through the
// pipeline so the boundary is enforced where a player actually hits it.
func TestCreationAgeBoundaries(t *testing.T) {
	cat := creationTestCatalogue()

	cases := []struct {
		age    int
		wantOK bool
	}{
		{15, false},
		{16, true},
		{60, true},
		{61, false},
	}

	for _, tc := range cases {
		sel := presetSelection("试", "道者", BaseAttributes{10, 10, 10, 10, 10, 10})
		sel.AgeYears = tc.age
		r := ValidateCreation(cat, sel)
		if r.OK() != tc.wantOK {
			t.Fatalf("age %d: OK()=%v, want %v (defects=%+v)",
				tc.age, r.OK(), tc.wantOK, r.Defects)
		}
		if !tc.wantOK && !r.Has(CErrAgeRange) {
			t.Fatalf("age %d was refused without CErrAgeRange; codes=%v",
				tc.age, r.Codes())
		}

		// The same boundary through the command pipeline.
		e, _ := newCreationEngine(t)
		payload := &CreationPayload{Selection: CreationSelection{AgeYears: tc.age}}
		res := e.Submit(creationCmd(e, "age", KindCreateEdit, payload))
		if res.OK != tc.wantOK {
			t.Fatalf("pipeline age %d: OK=%v, want %v (code=%s)",
				tc.age, res.OK, tc.wantOK, res.Code)
		}
	}
}

// TestCultivationRateExactBaseline pins the design's worked example (7.1):
// 炼气, aptitude 10, a true spirit root (1.3), 黄阶 (1.0), 普通 (1.0) and a
// passing mood gives 19.5 per ordinary month, and 39 when closed-door training
// doubles it. The display value is 19.5, so the stored SCALE sub-units are
// 195000.
//
// Asserting the exact number rather than "> 0" matters: several of the formula's
// factors multiply by 1.0 in this fixture, so dropping one of them leaves a
// positive rate and a `> 0` assertion cannot see it. The counter-proof harness
// found exactly that gap.
func TestCultivationRateExactBaseline(t *testing.T) {
	cat := creationTestCatalogue()
	draft := NewCreationDraft("game", "branch")

	// The default preset allocates aptitude 10 and a true root by construction.
	player, report := CreatePlayer(cat, draft)
	if !report.OK() {
		t.Fatalf("the default draft was rejected: %+v", report.Defects)
	}

	if player.Attributes.Aptitude != 10 {
		t.Fatalf("aptitude is %d, want 10 for the baseline fixture",
			player.Attributes.Aptitude)
	}
	if player.PrimaryTechniqueID != "yellow_breath" || player.Condition.Mood != StartingMood {
		t.Fatalf("creation defaults = technique %q / mood %d; want yellow_breath / %d",
			player.PrimaryTechniqueID, player.Condition.Mood, StartingMood)
	}

	const wantRate = 195000 // 19.5 in SCALE sub-units
	if player.Derived.CultivationRate != wantRate {
		t.Fatalf("cultivation rate is %d, want %d (19.5); the design's worked "+
			"example no longer reproduces",
			player.Derived.CultivationRate, wantRate)
	}

	// Closed-door training is twice an ordinary month (design 7.1: 闭关2).
	if got := player.Derived.CultivationRate * 2; got != 390000 {
		t.Fatalf("closed-door rate is %d, want 390000 (39)", got)
	}

	// EffectiveAptitude starts from the base aptitude; temporary modifiers are
	// applied to the derived value and never rewrite this source value.
	if player.Derived.EffectiveAptitude != 10 {
		t.Fatalf("effective aptitude is %d, want 10",
			player.Derived.EffectiveAptitude)
	}
}

func TestGrantAccumulatorSumsBonusesByGroupWithoutOrderDependence(t *testing.T) {
	effects := []GrantEffect{
		{Kind: GrantMultiplicative, Target: targetCultivationRate, Amount: 15 * SCALE / 10, Group: "innate", Reason: "加成甲"},
		{Kind: GrantMultiplicative, Target: targetCultivationRate, Amount: 12 * SCALE / 10, Group: "innate", Reason: "加成乙"},
	}
	first := newGrantAccumulator()
	for _, effect := range effects {
		first.apply(effect)
	}
	if got := first.rateScale(); got != 17*SCALE/10 {
		t.Fatalf("same-group multiplier = %d, want 17000 (add +50%% and +20%%)", got)
	}

	second := newGrantAccumulator()
	second.apply(effects[1])
	second.apply(effects[0])
	if second.rateScale() != first.rateScale() {
		t.Fatalf("changing content order changed the same-group multiplier: %d vs %d", second.rateScale(), first.rateScale())
	}
	effects[1].Group = "other"
	separate := newGrantAccumulator()
	for _, effect := range effects {
		separate.apply(effect)
	}
	if got := separate.rateScale(); got != 18*SCALE/10 {
		t.Fatalf("independent groups multiplier = %d, want 18000 (1.5 × 1.2)", got)
	}
}

// TestCultivationRateScalesWithAptitude proves the aptitude term is actually in
// the formula rather than being a constant: a higher aptitude must yield a
// strictly higher rate.
func TestCultivationRateScalesWithAptitude(t *testing.T) {
	cat := creationTestCatalogue()

	// Two drafts that differ only in aptitude, both legal 60-point allocations
	// with no talents, so the only difference is the aptitude term. Aptitude 4
	// against aptitude 15 stays inside the base ceiling.
	low := NewCreationDraft("game", "branch")
	lowSelection := presetSelection("试", "道者", BaseAttributes{12, 12, 12, 12, 4, 8})
	low.ApplySelection(lowSelection)
	low.FixAllocation(lowSelection.Base())

	high := NewCreationDraft("game", "branch")
	highSelection := presetSelection("试", "道者", BaseAttributes{9, 9, 9, 9, 15, 9})
	high.ApplySelection(highSelection)
	high.FixAllocation(highSelection.Base())

	lowPlayer, lowReport := CreatePlayer(cat, low)
	if !lowReport.OK() {
		t.Fatalf("the low-aptitude draft was rejected: %+v", lowReport.Defects)
	}
	highPlayer, highReport := CreatePlayer(cat, high)
	if !highReport.OK() {
		t.Fatalf("the high-aptitude draft was rejected: %+v", highReport.Defects)
	}

	if highPlayer.Attributes.Aptitude <= lowPlayer.Attributes.Aptitude {
		t.Fatalf("the fixture is wrong: aptitudes are %d and %d",
			lowPlayer.Attributes.Aptitude, highPlayer.Attributes.Aptitude)
	}
	if highPlayer.Derived.CultivationRate <= lowPlayer.Derived.CultivationRate {
		t.Fatalf("aptitude %d yielded rate %d, not more than aptitude %d's rate %d",
			highPlayer.Attributes.Aptitude, highPlayer.Derived.CultivationRate,
			lowPlayer.Attributes.Aptitude, lowPlayer.Derived.CultivationRate)
	}
}

// TestCultivationRateScalesWithSpiritRoot proves the spirit root multiplier is in
// the formula. A heavenly root (2.0) must yield more than a true root (1.3) from
// otherwise identical inputs.
func TestCultivationRateScalesWithSpiritRoot(t *testing.T) {
	cat := creationTestCatalogue()

	trueRoot := NewCreationDraft("game", "branch")
	trueRoot.SpiritRoot = RootTrue
	truePlayer, report := CreatePlayer(cat, trueRoot)
	if !report.OK() {
		t.Fatalf("the true-root draft was rejected: %+v", report.Defects)
	}

	heavenly := NewCreationDraft("game", "branch")
	heavenly.SpiritRoot = RootHeavenly
	heavenlyPlayer, report := CreatePlayer(cat, heavenly)
	if !report.OK() {
		t.Fatalf("the heavenly-root draft was rejected: %+v", report.Defects)
	}

	if heavenlyPlayer.Derived.CultivationRate <= truePlayer.Derived.CultivationRate {
		t.Fatalf("a heavenly root (2.0) yielded %d, not more than a true root "+
			"(1.3) at %d",
			heavenlyPlayer.Derived.CultivationRate, truePlayer.Derived.CultivationRate)
	}
}

// TestConfirmProducesAValidPlayer confirms the factory's output passes the
// state validator, which is the invariant the pipeline enforces after apply.
func TestConfirmProducesAValidPlayer(t *testing.T) {
	e, _ := newCreationEngine(t)

	e.Submit(creationCmd(e, "c1", KindCreateEdit, &CreationPayload{
		Selection: CreationSelection{Surname: "云", GivenName: "长生", Gender: "custom"},
	}))
	e.Submit(creationCmd(e, "c2", KindCreateEdit, &CreationPayload{AdvanceStep: true}))
	r := e.Submit(creationCmd(e, "c3", KindCreateConfirm, &CreationPayload{Confirm: true}))
	if !r.OK {
		t.Fatalf("confirmation failed: %s (%+v)", r.Code, r.Events)
	}

	s := e.State()
	if s.Player == nil {
		t.Fatal("no player")
	}
	if s.Pending.Creation != nil {
		t.Fatal("the draft was not cleared")
	}
	if report := ValidateState(s); !report.OK() {
		t.Fatalf("the produced state is invalid: %+v", report.Errors)
	}
	if report := ValidateSerializeReady(s); !report.OK() {
		t.Fatalf("the produced state is not serialisable: %+v", report.Errors)
	}
	// The rate must be populated, since TASK-08 reads it.
	if s.Player.Derived.CultivationRate <= 0 {
		t.Fatalf("cultivation rate is %d, want a positive value",
			s.Player.Derived.CultivationRate)
	}
	if s.Player.Lifespan.TotalYears() != 100 {
		t.Fatalf("lifespan is %d years, want the 炼气 ceiling of 100",
			s.Player.Lifespan.TotalYears())
	}
}

// TestCannotConfirmBeforeTheFinalStep prevents finalising a character whose
// second step the player never saw.
func TestCannotConfirmBeforeTheFinalStep(t *testing.T) {
	e, _ := newCreationEngine(t)

	r := e.Submit(creationCmd(e, "early-confirm", KindCreateConfirm,
		&CreationPayload{Confirm: true}))
	if r.OK {
		t.Fatal("a draft on step one was confirmed")
	}
	if e.State().Phase != PhaseCreation {
		t.Fatalf("phase is %q, want it to remain CREATION", e.State().Phase)
	}
}

// TestStepMovementIsBounded checks the wizard cannot walk off either end.
func TestStepMovementIsBounded(t *testing.T) {
	e, _ := newCreationEngine(t)

	// Back from step one.
	r := e.Submit(creationCmd(e, "back", KindCreateEdit, &CreationPayload{BackStep: true}))
	if r.OK {
		t.Fatal("the wizard went back from step one")
	}

	// Forward twice: the second must fail.
	if r := e.Submit(creationCmd(e, "fwd-1", KindCreateEdit,
		&CreationPayload{AdvanceStep: true})); !r.OK {
		t.Fatalf("the first advance failed: %s", r.Code)
	}
	r = e.Submit(creationCmd(e, "fwd-2", KindCreateEdit, &CreationPayload{AdvanceStep: true}))
	if r.OK {
		t.Fatal("the wizard advanced past the final step")
	}
}

// TestIdempotentConfirmDoesNotRecreate proves a resent confirmation returns the
// stored result and does not build a second character.
//
// The resend must be the *identical* command, which is what a transport retry
// actually sends. Rebuilding the command from the new state would produce a
// different revision and view token, and the fingerprint would rightly call
// that a conflict rather than a duplicate.
func TestIdempotentConfirmDoesNotRecreate(t *testing.T) {
	e, _ := newCreationEngine(t)

	e.Submit(creationCmd(e, "i1", KindCreateEdit, &CreationPayload{
		Selection: CreationSelection{Surname: "云", GivenName: "长生"},
	}))
	e.Submit(creationCmd(e, "i2", KindCreateEdit, &CreationPayload{AdvanceStep: true}))

	// Build the confirm command once, then submit the same value twice.
	confirm := creationCmd(e, "i3", KindCreateConfirm, &CreationPayload{Confirm: true})
	first := e.Submit(confirm)
	if !first.OK {
		t.Fatalf("first confirm failed: %s", first.Code)
	}
	revAfterFirst := e.State().Revision
	playerAfterFirst := e.State().Player

	second := e.Submit(confirm)
	if !second.OK {
		t.Fatalf("a resend was rejected: %s", second.Code)
	}
	if second.RevisionAfter != first.RevisionAfter {
		t.Fatalf("a resend moved the revision: %d -> %d",
			first.RevisionAfter, second.RevisionAfter)
	}
	if e.State().Revision != revAfterFirst {
		t.Fatalf("the state revision moved on a resend: %d -> %d",
			revAfterFirst, e.State().Revision)
	}
	if e.State().Player != playerAfterFirst {
		t.Fatal("a resend rebuilt the character")
	}
	if e.State().Phase != PhaseReady {
		t.Fatalf("phase is %q after a resend, want %q", e.State().Phase, PhaseReady)
	}
}

// TestCreateEditWithDifferentBodyConflicts proves the payload participates in
// the fingerprint: the same action id with a different allocation is a conflict.
func TestCreateEditWithDifferentBodyConflicts(t *testing.T) {
	e, _ := newCreationEngine(t)

	first := e.Submit(creationCmd(e, "dup", KindCreateEdit, &CreationPayload{
		Selection: CreationSelection{Surname: "云"},
	}))
	if !first.OK {
		t.Fatalf("first edit failed: %s", first.Code)
	}

	// Same id, different body.
	second := e.Submit(creationCmd(e, "dup", KindCreateEdit, &CreationPayload{
		Selection: CreationSelection{Surname: "雨"},
	}))
	if second.OK {
		t.Fatal("the same action id with a different body was accepted")
	}
	if second.Code != ErrActionIdConflict {
		t.Fatalf("code is %q, want %q", second.Code, ErrActionIdConflict)
	}
}

// --- helpers -----------------------------------------------------------------

// sameDraft reports whether two drafts are mechanically identical, so a test
// can assert that a rejected command changed nothing.
func sameDraft(a, b *CreationDraft) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Step != b.Step || a.Confirmed != b.Confirmed || a.YaoIntent != b.YaoIntent {
		return false
	}
	if a.Identity != b.Identity || a.Origin != b.Origin || a.Path != b.Path ||
		a.SpiritRoot != b.SpiritRoot || a.Constitution != b.Constitution ||
		a.AgeYears != b.AgeYears {
		return false
	}
	if len(a.TalentIDs) != len(b.TalentIDs) {
		return false
	}
	for i := range a.TalentIDs {
		if a.TalentIDs[i] != b.TalentIDs[i] {
			return false
		}
	}
	if len(a.FixedResults) != len(b.FixedResults) {
		return false
	}
	for k, v := range a.FixedResults {
		if b.FixedResults[k] != v {
			return false
		}
	}
	return true
}

// rngCounters renders every stream's counter, so a test can prove creation did
// not draw from any of them.
func rngCounters(s *GameState) string {
	if s == nil {
		return ""
	}
	out := ""
	for _, name := range WorldStreamNames {
		st, ok := s.RNG.Streams[name]
		if !ok {
			out += name + "=missing;"
			continue
		}
		out += name + "=" + itoa(int(st.Counter)) + ";"
	}
	return out
}

// hasEventDetail reports whether any event carries the given substring.
func hasEventDetail(events []ResultEvent, sub string) bool {
	for _, e := range events {
		if contains(e.Detail, sub) {
			return true
		}
	}
	return false
}

// contains is a substring test kept local so the engine needs no strings import
// in its test helpers beyond what it already uses.
func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// roundTripDraft serialises and deserialises a draft through JSON, which is the
// encoding the persistence layer uses. It is the engine-side stand-in for a
// resume: the storage package owns the full state codec, but a draft's own
// round trip is what "刷新/恢复" hinges on, and JSON is exactly what is written.
func roundTripDraft(t *testing.T, d *CreationDraft) (*CreationDraft, bool) {
	t.Helper()
	blob, err := json.Marshal(d)
	if err != nil {
		t.Logf("marshal failed: %v", err)
		return nil, false
	}
	var out CreationDraft
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Logf("unmarshal failed: %v", err)
		return nil, false
	}
	return &out, true
}

// TestCreationGrantsReachTheCharacter is a TASK-12 regression test.
//
// The creation factory's accumulator used to ignore any target it did not
// recognise, and four targets the shipped creation content actually names —
// attack, defence, xp and breakthrough.* — were being dropped without a word.
// The catalogue validator accepted them (they are inside the whitelist), so the
// only symptom was that a talent did nothing.
//
// The assertion is on the finished character rather than on the accumulator,
// because that is where the drop was invisible.
func TestCreationGrantsReachTheCharacter(t *testing.T) {
	cat := creationTestCatalogue()
	cat.Talents = append(cat.Talents, TalentDefinition{
		ID: "battle_born", NameZH: "天生战骨",
		Effects: []GrantEffect{
			{Kind: GrantAdditive, Target: targetAttack, Amount: 4, Reason: "测试攻击"},
			{Kind: GrantAdditive, Target: targetDefense, Amount: 3, Reason: "测试防御"},
			{Kind: GrantAdditive, Target: targetXP, Amount: 500, Reason: "测试修为"},
			{Kind: GrantAdditive, Target: "breakthrough.demon_resist", Amount: 7, Reason: "测试抗性"},
		},
	})

	// The draft NewCreationDraft builds is already a legal allocation, so the
	// only thing this test changes is which talent it carries.
	draft := NewCreationDraft("game", "branch")
	draft.TalentIDs = []string{"battle_born"}
	draft.Confirmed = true
	draft.Step = CreationTotalSteps

	player, report := CreatePlayer(cat, draft)
	if !report.OK() {
		t.Fatalf("creating a character with a granting talent failed: %v", report.Error())
	}

	wantAttack := int64(player.Attributes.Strength)*SCALE + 4*SCALE
	if player.Derived.Attack != wantAttack {
		t.Errorf("attack = %d, want %d: the creation grant was dropped",
			player.Derived.Attack, wantAttack)
	}
	wantDefense := int64(player.Attributes.Constitution)*SCALE + 3*SCALE
	if player.Derived.Defense != wantDefense {
		t.Errorf("defence = %d, want %d: the creation grant was dropped",
			player.Derived.Defense, wantDefense)
	}
	if player.XP != 500 {
		t.Errorf("xp = %d, want 500: the creation grant was dropped", player.XP)
	}
	if got := player.BreakthroughBonuses["demon_resist"]; got != 7 {
		t.Errorf("breakthrough bonus = %d, want 7: the creation grant was dropped", got)
	}
}
