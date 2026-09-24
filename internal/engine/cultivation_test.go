package engine

import "testing"

func cultivationTestCatalogue() *Catalogue {
	config := func(value int) ConfigValue {
		return ConfigValue{Provenance: ProvenanceDesignNote, Value: value, Note: "cultivation test fixture"}
	}
	return &Catalogue{
		Version:                  ContentVersion,
		CultivationMoodThreshold: config(50),
		Realms: []RealmDefinition{{
			ID: RealmQiRefining, NameZH: "炼气", Order: 0,
			MonthBase: config(10 * SCALE), TierThreshold: config(100 * SCALE),
		}},
		Origins: []OriginDefinition{{ID: OriginCommoner, NameZH: "平民"}},
		SpiritRoots: []SpiritRootDefinition{{
			ID: RootTrue, NameZH: "真灵根", Multiplier: config(13 * SCALE / 10),
		}},
		Techniques: []TechniqueDefinition{
			{ID: "yellow", NameZH: "吐纳诀", Grade: GradeYellow, IsPrimary: true, GradeMultiplier: config(SCALE)},
			{ID: "mystic", NameZH: "玄元诀", Grade: GradeMystic, IsPrimary: true, GradeMultiplier: config(13 * SCALE / 10)},
			// A secondary's grade is not another main-grade multiplier.
			{ID: "support", NameZH: "辅修诀", Grade: GradeImmortal, IsPrimary: false, GradeMultiplier: config(3 * SCALE)},
		},
	}
}

func cultivationTestPlayer() *Player {
	return &Player{
		Origin: OriginCommoner, SpiritRoot: RootTrue,
		Attributes: Attributes{Aptitude: 10, Growth: map[string]int{}},
		Realm:      RealmQiRefining, Tier: TierEarly,
		PrimaryTechniqueID: "yellow",
		Condition:          Condition{Mood: StartingMood},
	}
}

func TestCultivationExactBaselinesAndClosedCandidate(t *testing.T) {
	cat := cultivationTestCatalogue()
	player := cultivationTestPlayer()

	normal, err := PreviewCultivation(player, nil, cat, ActionNormal)
	if err != nil {
		t.Fatal(err)
	}
	if normal.Rate != 195000 {
		t.Fatalf("ordinary rate = %d sub-units, want 195000 (19.5)", normal.Rate)
	}
	closed, err := PreviewCultivation(player, nil, cat, ActionClosed)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Rate != 390000 {
		t.Fatalf("closed candidate rate = %d, want 390000 (39)", closed.Rate)
	}
	if closed.MonthsToThreshold != 3 {
		t.Fatalf("closed candidate months to threshold = %d, want 3", closed.MonthsToThreshold)
	}
}

func TestCultivationNamedCreationBonusAndPrimarySecondaryRules(t *testing.T) {
	cat := cultivationTestCatalogue()
	cat.Talents = []TalentDefinition{{
		ID: "dao_body", NameZH: "先天道体",
		Effects: []GrantEffect{{
			Kind: GrantMultiplicative, Target: targetCultivationRate,
			Amount: 15 * SCALE / 10, Group: "innate", Reason: "先天道体独立加成 +50%",
		}},
	}}
	player := cultivationTestPlayer()
	player.TalentIDs = []string{"dao_body"}
	player.SecondaryTechniqueIDs = []string{"support"}

	preview, err := PreviewCultivation(player, nil, cat, ActionNormal)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rate != 292500 {
		t.Fatalf("rate = %d, want 292500 (29.25); secondary grade must not stack with the primary grade", preview.Rate)
	}
	if preview.PrimaryTechniqueID != "yellow" || len(preview.SecondaryTechniqueIDs) != 1 || preview.SecondaryTechniqueIDs[0] != "support" {
		t.Fatalf("technique loadout was not preserved in the preview: %#v", preview)
	}
	if len(preview.Bonuses) != 1 || preview.Bonuses[0].Reason != "先天道体独立加成 +50%" {
		t.Fatalf("named bonus is missing from the explanation: %#v", preview.Bonuses)
	}
}

func TestCultivationIndependentBonusesSumWithinGroupsAndMultiplyAcrossGroups(t *testing.T) {
	cat := cultivationTestCatalogue()
	cat.Origins = []OriginDefinition{{ID: OriginCommoner, NameZH: "平民", Effects: []GrantEffect{
		{Kind: GrantMultiplicative, Target: targetCultivationRate, Amount: 15 * SCALE / 10, Group: "same", Reason: "甲 +50%"},
		{Kind: GrantMultiplicative, Target: targetCultivationRate, Amount: 12 * SCALE / 10, Group: "same", Reason: "乙 +20%"},
	}}}
	player := cultivationTestPlayer()
	preview, err := PreviewCultivation(player, nil, cat, ActionNormal)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rate != 331500 {
		t.Fatalf("same-group rate = %d, want 331500 (19.5 × 1.7)", preview.Rate)
	}

	cat.Origins[0].Effects[1].Group = "other"
	preview, err = PreviewCultivation(player, nil, cat, ActionNormal)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rate != 351000 {
		t.Fatalf("cross-group rate = %d, want 351000 (19.5 × 1.5 × 1.2)", preview.Rate)
	}
}

func TestCultivationMoodBandsAndTemporaryModifierRestoreBaseAptitude(t *testing.T) {
	cat := cultivationTestCatalogue()
	cat.Afflictions = []AfflictionDefinition{{
		ID: "wounded_focus", NameZH: "轻伤", Kind: AfflictionWound,
		DurationMonths: ConfigValue{Provenance: ProvenanceDesignNote, Value: 2},
		Group:          "injury", EffectiveAptitudeDelta: ConfigValue{Provenance: ProvenanceDesignNote, Value: -2},
		RateBonus: ConfigValue{Provenance: ProvenanceDesignNote, Value: -1000},
	}}
	player := cultivationTestPlayer()
	player.Condition.Mood = 49
	lowMood, err := PreviewCultivation(player, nil, cat, ActionNormal)
	if err != nil {
		t.Fatal(err)
	}
	if lowMood.MoodBand != MoodBelow || lowMood.Rate != 97500 {
		t.Fatalf("below-threshold mood preview = %#v, want half of 19.5", lowMood)
	}
	player.Condition.Mood = 51
	highMood, err := PreviewCultivation(player, nil, cat, ActionNormal)
	if err != nil {
		t.Fatal(err)
	}
	if highMood.MoodBand != MoodAbove || highMood.Rate != 234000 {
		t.Fatalf("above-threshold mood preview = %#v, want 23.4", highMood)
	}

	player.Condition.Mood = StartingMood
	player.Condition.Effects = []TimedEffect{{ID: "wounded_focus", MonthsRemaining: 1, Stacks: 1}}
	wounded, err := PreviewCultivation(player, nil, cat, ActionNormal)
	if err != nil {
		t.Fatal(err)
	}
	if wounded.EffectiveAptitude != 8 || wounded.Rate != 163800 || player.Attributes.Aptitude != 10 {
		t.Fatalf("temporary modifier changed the wrong values: preview=%#v base aptitude=%d", wounded, player.Attributes.Aptitude)
	}
	player.Condition.Effects = nil
	restored, err := PreviewCultivation(player, nil, cat, ActionNormal)
	if err != nil {
		t.Fatal(err)
	}
	if restored.EffectiveAptitude != 10 || restored.Rate != 195000 || player.Attributes.Aptitude != 10 {
		t.Fatalf("removing a temporary modifier did not restore the base: preview=%#v base aptitude=%d", restored, player.Attributes.Aptitude)
	}
}

func TestCultivationRetainsTenMonthsOfFixedPointFractions(t *testing.T) {
	cat := cultivationTestCatalogue()
	cat.Realms[0].TierThreshold.Value = 1000 * SCALE
	player := cultivationTestPlayer()
	player.Attributes.Aptitude = 11
	var total int64
	for i := 0; i < 10; i++ {
		preview, err := PreviewCultivation(player, nil, cat, ActionNormal)
		if err != nil {
			t.Fatal(err)
		}
		total += preview.Rate
		player.XP += preview.Rate
	}
	if total != 2015000 {
		t.Fatalf("10-month total = %d sub-units, want 2015000 (201.5)", total)
	}
	if player.XP != total {
		t.Fatalf("stored progress = %d, want exact fixed-point total %d", player.XP, total)
	}
}

func TestCultivationCapsAtThresholdAndRequiresExplicitBreakthrough(t *testing.T) {
	state := readyState()
	state.Player.XP = 90 * SCALE
	e, _ := newTestEngine(t, state)
	r := e.Submit(cmd(e, "fill-tier", KindCultivate))
	if !r.OK {
		t.Fatalf("cultivation failed: %s", r.Code)
	}
	if got := e.State().Player.XP; got != 100*SCALE {
		t.Fatalf("progress = %d, want capped threshold %d", got, 100*SCALE)
	}
	if e.State().Player.Realm != RealmQiRefining || e.State().Player.Tier != TierEarly {
		t.Fatal("reaching the threshold performed a breakthrough automatically")
	}
	if len(r.Delta.Entries) < 2 || r.Delta.Entries[0].After != 100*SCALE {
		t.Fatalf("cultivation result did not return the capped XP ledger: %#v", r.Delta)
	}
	before := e.State()
	r = e.Submit(cmd(e, "already-full", KindCultivate))
	if r.OK || e.State().Counters.WorldMonth != before.Counters.WorldMonth {
		t.Fatal("cultivating at a full threshold should be refused without charging a month")
	}
}

func TestCultivationGrantsProficiencyToActiveTechniquesAndOnlyNormalActionIsExposed(t *testing.T) {
	state := readyState()
	state.Player.SecondaryTechniqueIDs = []string{"support"}
	cat := testCatalogue()
	cat.Techniques = append(cat.Techniques, TechniqueDefinition{
		ID: "support", NameZH: "輔修诀", Grade: GradeImmortal, IsPrimary: false,
		GradeMultiplier: ConfigValue{Provenance: ProvenanceDesignNote, Value: 3 * SCALE},
	})
	e, _ := newTestEngineWithCatalogue(t, state, cat)
	r := e.Submit(cmd(e, "study-month", KindCultivate))
	if !r.OK {
		t.Fatalf("normal cultivation failed: %s", r.Code)
	}
	if len(r.Delta.Entries) == 0 || r.Delta.Entries[0].After != 195000 {
		t.Fatalf("secondary grade stacked with the primary: %#v", r.Delta)
	}
	if e.State().Player.Proficiencies["yellow_breath"] != 1 || e.State().Player.Proficiencies["support"] != 1 {
		t.Fatalf("active technique proficiency did not advance once: %#v", e.State().Player.Proficiencies)
	}

	before := e.State().Counters.WorldMonth
	closed := cmd(e, "closed-month", KindCultivate)
	closed.Payload.ActionKind = ActionClosed
	if result := e.Submit(closed); result.OK {
		t.Fatal("the M1 command pipeline must not expose closed cultivation before TASK-23")
	}
	if e.State().Counters.WorldMonth != before {
		t.Fatal("refusing the closed-cultivation candidate still charged a month")
	}
}

func TestExpiredCultivationModifierRestoresDerivedAptitudeAndRate(t *testing.T) {
	cat := testCatalogue()
	cat.Afflictions = []AfflictionDefinition{{
		ID: "temporary_wound", NameZH: "暂时伤势", Kind: AfflictionWound, Group: "injury",
		DurationMonths:         ConfigValue{Provenance: ProvenanceDesignNote, Value: 1},
		EffectiveAptitudeDelta: ConfigValue{Provenance: ProvenanceDesignNote, Value: -2},
		RateBonus:              ConfigValue{Provenance: ProvenanceDesignNote, Value: -1000},
	}}
	state := readyState()
	state.Player.Condition.Effects = []TimedEffect{{ID: "temporary_wound", MonthsRemaining: 1, Stacks: 1}}
	e, _ := newTestEngineWithCatalogue(t, state, cat)

	r := e.Submit(cmd(e, "cultivate-through-wound", KindCultivate))
	if !r.OK {
		t.Fatalf("cultivation with temporary effect failed: %s", r.Code)
	}
	player := e.State().Player
	if player.XP != 163800 {
		t.Fatalf("the month did not use the active modifier's rate: %d, want 163800", player.XP)
	}
	if len(player.Condition.Effects) != 0 {
		t.Fatalf("one-month modifier did not expire at month end: %#v", player.Condition.Effects)
	}
	if player.Attributes.Aptitude != 10 || player.Derived.EffectiveAptitude != 10 || player.Derived.CultivationRate != 195000 {
		t.Fatalf("expired modifier rewrote base stats or left stale derived values: %#v", player)
	}
}
