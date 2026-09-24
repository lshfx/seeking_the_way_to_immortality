package engine

import (
	"testing"
)

// These tests cover TASK-04's acceptance criteria, each as a positive case and,
// where the criterion is a rejection, a deliberately broken fixture that must
// fail. A validator that only ever says "ok" is worthless, so every rejection
// rule below has a case that provokes it.

// validCatalogue builds a minimal catalogue that passes validation. Tests copy
// it and break one field to isolate a single rule.
func validCatalogue() Catalogue {
	return Catalogue{
		Version:                  ContentVersion,
		CultivationMoodThreshold: ConfigValue{Provenance: ProvenanceDesignNote, Value: StartingMood, Note: "test mood threshold"},
		EventBaseChancePermille:  ConfigValue{Provenance: ProvenanceDesignNote, Value: 200, Note: "test base event chance"},
		Realms:                   validRealms(),
		Origins:                  validOrigins(),
		SpiritRoots: []SpiritRootDefinition{{
			ID: RootPseudo, NameZH: "伪灵根",
			Multiplier: ConfigValue{Provenance: ProvenanceManuscript, Value: SCALE},
		}},
		Items: []ItemDefinition{{
			ID: "pill", NameZH: "丹", Category: ItemConsumable,
			BuyPrice:   ConfigValue{Provenance: ProvenanceManuscript, Value: 20},
			SellPrice:  ConfigValue{Provenance: ProvenanceManuscript, Value: 10},
			StackLimit: 99,
		}},
		Techniques: []TechniqueDefinition{{
			ID: "t1", NameZH: "诀", Grade: GradeYellow,
			GradeMultiplier: ConfigValue{Provenance: ProvenanceManuscript, Value: SCALE},
			IsPrimary:       true,
		}},
		Locations: []LocationDefinition{
			{ID: "a", NameZH: "甲", Environment: EnvNormal, Neighbors: []string{"b"}, Safe: true},
			{ID: "b", NameZH: "乙", Environment: EnvNormal, Neighbors: []string{"a"}, Safe: true},
		},
		NPCs: []NPCDefinition{{
			ID: "npc1", NameZH: "甲某", Adult: true,
			InitialRealm: RealmQiRefining, InitialTier: TierEarly,
			LifespanYears: 100, InitialAgeYears: 30, HomeLocation: "a",
		}},
		InjuryBands:             validInjuryBands(),
		KarmaTiers:              validKarmaTiers(),
		HealRestorePermille:     ConfigValue{Provenance: ProvenanceDesignNote, Value: 250, Note: "测试用"},
		HealConvertBurnPermille: ConfigValue{Provenance: ProvenanceDesignNote, Value: 100, Note: "测试用"},
		EarlyGoals: []EarlyGoalDefinition{{
			ID: "g1", NameZH: "试目标", Description: "测试目标",
			SuccessFlag: "g1_success", FailureFlag: "g1_failure", AbandonFlag: "g1_abandon",
		}},
		Quests: []QuestDefinition{{
			ID: "q1", NameZH: "务", Kind: QuestChore,
			MonthCost: ConfigValue{Provenance: ProvenanceManuscript, Value: 1},
			Rewards: []GrantEffect{{
				Kind: GrantAdditive, Target: "resources.spirit_stones",
				Amount: 30, Reason: "报酬",
			}},
		}},
		Breakthroughs: validBreakthroughs(),
		Events:        validEvents(),
		M1Paths:       []Path{PathHuman},
	}
}

// validInjuryBands configures every band, which the validator requires: an
// unconfigured band would have no penalty rule and no way to be described.
func validInjuryBands() []InjuryBandDefinition {
	out := make([]InjuryBandDefinition, 0, len(InjuryBandOrder))
	for _, band := range InjuryBandOrder {
		out = append(out, InjuryBandDefinition{
			Band:   band,
			NameZH: string(band),
			SpeedPenaltyPermille: ConfigValue{
				Provenance: ProvenanceDesignNote, Value: 0, Note: "测试用",
			},
		})
	}
	return out
}

// validKarmaTiers gives the fixture a ladder with a floor at zero.
func validKarmaTiers() []KarmaTierDefinition {
	return []KarmaTierDefinition{{
		ID: "tier_zero", NameZH: "清白",
		MinKarma: ConfigValue{Provenance: ProvenanceDesignNote, Value: 0, Note: "测试用"},
	}}
}

// validRealms returns all ten realms with complete figures.
func validRealms() []RealmDefinition {
	out := make([]RealmDefinition, 0, len(RealmOrder))
	for i, r := range RealmOrder {
		out = append(out, RealmDefinition{
			ID: r, NameZH: string(r), Order: i,
			LifespanYears: ConfigValue{Provenance: ProvenanceManuscript, Value: 100 * (i + 1)},
			MonthBase:     ConfigValue{Provenance: ProvenanceManuscript, Value: SCALE},
			TierThreshold: ConfigValue{
				Provenance: ProvenanceTrial, Value: 100 * SCALE,
				Note: "试算阈值",
			},
		})
	}
	return out
}

// validOrigins returns the three origins with no effects, which is legal.
func validOrigins() []OriginDefinition {
	return []OriginDefinition{
		{ID: OriginCommoner, NameZH: "平民"},
		{ID: OriginMerchant, NameZH: "商贾", Effects: []GrantEffect{{
			Kind: GrantAdditive, Target: "resources.spirit_stones",
			Amount: 300, Reason: "出身资财",
		}}},
		{ID: OriginHunter, NameZH: "猎户"},
	}
}

// validBreakthroughs returns a complete human-path chain for every realm.
func validBreakthroughs() []BreakthroughDefinition {
	out := []BreakthroughDefinition{}
	minor := []struct{ from, to RealmTier }{
		{TierEarly, TierMiddle}, {TierMiddle, TierLate}, {TierLate, TierPerfection},
	}
	for i, realm := range RealmOrder {
		for _, m := range minor {
			out = append(out, BreakthroughDefinition{
				Path: PathHuman, FromRealm: realm, FromTier: m.from,
				ToRealm: realm, ToTier: m.to,
				BaseSuccessPct:        ConfigValue{Provenance: ProvenanceManuscript, Value: 70},
				ComprehensionPerPoint: ConfigValue{Provenance: ProvenanceManuscript, Value: 2},
				ComprehensionBaseline: ConfigValue{Provenance: ProvenanceManuscript, Value: 10},
				ClampMinPct:           ConfigValue{Provenance: ProvenanceManuscript, Value: 5},
				ClampMaxPct:           ConfigValue{Provenance: ProvenanceManuscript, Value: 95},
				FailFallbackPct:       ConfigValue{Provenance: ProvenanceManuscript, Value: 70},
				MonthCost:             ConfigValue{Provenance: ProvenanceManuscript, Value: 1},
			})
		}
		if i < len(RealmOrder)-1 {
			out = append(out, BreakthroughDefinition{
				Path: PathHuman, FromRealm: realm, FromTier: TierPerfection,
				ToRealm: RealmOrder[i+1], ToTier: TierEarly,
				IsMajor:               true,
				BaseSuccessPct:        ConfigValue{Provenance: ProvenanceManuscript, Value: 95},
				ComprehensionPerPoint: ConfigValue{Provenance: ProvenanceManuscript, Value: 2},
				ComprehensionBaseline: ConfigValue{Provenance: ProvenanceManuscript, Value: 10},
				ClampMinPct:           ConfigValue{Provenance: ProvenanceManuscript, Value: 5},
				ClampMaxPct:           ConfigValue{Provenance: ProvenanceManuscript, Value: 95},
				FailFallbackPct:       ConfigValue{Provenance: ProvenanceManuscript, Value: 70},
				MonthCost:             ConfigValue{Provenance: ProvenanceManuscript, Value: 1},
			})
		}
	}
	return out
}

// validEvents returns one well-formed event.
func validEvents() []EventDefinition {
	return []EventDefinition{{
		ID: "EVT-X", NameZH: "试", Scene: "a", Purpose: "测试节点",
		TextZH:   "这是一个测试节点。",
		Priority: ConfigValue{Provenance: ProvenanceDesignNote, Value: 50},
		Weight:   ConfigValue{Provenance: ProvenanceDesignNote, Value: 10},
		Choices: []EventChoice{
			{ID: "c1", TextZH: "行", Effects: []GrantEffect{{
				Kind: GrantAdditive, Target: "flags.done", Amount: 1, Reason: "选择",
			}}},
			{
				// The fixture only has to satisfy reachability: the validator asks
				// that every declared goal branch be settable by some choice, not that
				// the branches be mutually exclusive. A real node spreads them over
				// separate choices, as EVT-010 and EVT-011 do.
				ID: "c2", TextZH: "止",
				Effects: []GrantEffect{
					{Kind: GrantAdditive, Target: "flags.g1_success", Amount: 1, Reason: "测试目标达成"},
					{Kind: GrantAdditive, Target: "flags.g1_failure", Amount: 1, Reason: "测试目标失败"},
					{Kind: GrantAdditive, Target: "flags.g1_abandon", Amount: 1, Reason: "测试目标放弃"},
				},
			},
		},
	}}
}

// ---------------------------------------------------------------------------
// Baseline: the fixture itself must be valid, or every negative test below
// proves nothing.
// ---------------------------------------------------------------------------

func TestValidCataloguePasses(t *testing.T) {
	c := validCatalogue()
	if rep := ValidateCatalogue(&c); !rep.OK() {
		t.Fatalf("baseline catalogue must validate, got %d error(s):\n%v",
			len(rep.Errors), rep.Error())
	}
}

// ---------------------------------------------------------------------------
// Criterion: duplicate ids fail.
// ---------------------------------------------------------------------------

func TestDuplicateIDsFail(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(*Catalogue)
	}{
		{"duplicate item", func(c *Catalogue) {
			c.Items = append(c.Items, c.Items[0])
		}},
		{"duplicate realm", func(c *Catalogue) {
			c.Realms = append(c.Realms, c.Realms[0])
		}},
		{"duplicate location", func(c *Catalogue) {
			c.Locations = append(c.Locations, c.Locations[0])
		}},
		{"duplicate npc", func(c *Catalogue) {
			c.NPCs = append(c.NPCs, c.NPCs[0])
		}},
		{"duplicate quest", func(c *Catalogue) {
			c.Quests = append(c.Quests, c.Quests[0])
		}},
		{"duplicate event", func(c *Catalogue) {
			c.Events = append(c.Events, c.Events[0])
		}},
		{"duplicate technique", func(c *Catalogue) {
			c.Techniques = append(c.Techniques, c.Techniques[0])
		}},
		{"duplicate origin", func(c *Catalogue) {
			c.Origins = append(c.Origins, c.Origins[0])
		}},
		{"duplicate spirit root", func(c *Catalogue) {
			c.SpiritRoots = append(c.SpiritRoots, c.SpiritRoots[0])
		}},
		{"duplicate choice in one event", func(c *Catalogue) {
			c.Events[0].Choices = append(c.Events[0].Choices, c.Events[0].Choices[0])
		}},
		{"duplicate breakthrough row", func(c *Catalogue) {
			c.Breakthroughs = append(c.Breakthroughs, c.Breakthroughs[0])
		}},
		{"duplicate m1 path", func(c *Catalogue) {
			c.M1Paths = append(c.M1Paths, PathHuman)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCatalogue()
			tc.break_(&c)
			rep := ValidateCatalogue(&c)
			if !rep.Has(VErrDuplicateID) {
				t.Fatalf("expected %s, got codes %v",
					VErrDuplicateID, rep.Codes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Criterion: invalid references fail.
// ---------------------------------------------------------------------------

func TestDanglingReferencesFail(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(*Catalogue)
	}{
		{"quest requires undeclared item", func(c *Catalogue) {
			c.Quests[0].RequiredItems = []ItemStack{{ItemID: "ghost", Quantity: 1}}
		}},
		{"quest giver is undeclared npc", func(c *Catalogue) {
			c.Quests[0].GiverNPCID = "ghost"
		}},
		{"location neighbour undeclared", func(c *Catalogue) {
			c.Locations[0].Neighbors = []string{"ghost"}
		}},
		{"npc home location undeclared", func(c *Catalogue) {
			c.NPCs[0].HomeLocation = "ghost"
		}},
		{"npc dialogue undeclared", func(c *Catalogue) {
			c.NPCs[0].DialogueIDs = []string{"ghost"}
		}},
		{"talent exclusivity undeclared", func(c *Catalogue) {
			c.Talents = []TalentDefinition{{ID: "t", Exclusive: []string{"ghost"}}}
		}},
		{"event cost item undeclared", func(c *Catalogue) {
			c.Events[0].Choices[0].Costs = []EventCost{{
				Kind: CostItem, ItemID: "ghost", Amount: 1,
			}}
		}},
		{"breakthrough material undeclared", func(c *Catalogue) {
			c.Breakthroughs[0].Materials = []ItemStack{{ItemID: "ghost", Quantity: 1}}
		}},
		{"breakthrough trial node undeclared", func(c *Catalogue) {
			c.Breakthroughs[0].TrialNodes = []string{"EVT-GHOST"}
		}},

		{"sect entry quest undeclared", func(c *Catalogue) {
			c.Sects = []SectDefinition{{ID: "s", EntryQuestID: "ghost"}}
		}},
		{"dialogue npc undeclared", func(c *Catalogue) {
			c.Dialogues = []DialogueDefinition{{ID: "d", NPCID: "ghost"}}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCatalogue()
			tc.break_(&c)
			rep := ValidateCatalogue(&c)
			if !rep.Has(VErrDanglingRef) {
				t.Fatalf("expected %s, got codes %v",
					VErrDanglingRef, rep.Codes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Criterion: negative costs fail.
// ---------------------------------------------------------------------------

func TestNegativeCostsFail(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(*Catalogue)
	}{
		{"negative item buy price", func(c *Catalogue) {
			c.Items[0].BuyPrice.Value = -1
		}},
		{"negative item sell price", func(c *Catalogue) {
			c.Items[0].SellPrice.Value = -1
		}},
		{"sell price above buy price", func(c *Catalogue) {
			// The classic in-place arbitrage: buy low, sell high, repeat.
			c.Items[0].BuyPrice.Value = 10
			c.Items[0].SellPrice.Value = 11
		}},
		{"negative stack limit", func(c *Catalogue) {
			c.Items[0].StackLimit = -1
		}},
		{"negative quest month cost", func(c *Catalogue) {
			c.Quests[0].MonthCost.Value = -1
		}},
		{"negative quest cooldown", func(c *Catalogue) {
			c.Quests[0].CooldownMonths = -1
		}},
		{"negative required quantity", func(c *Catalogue) {
			c.Quests[0].RequiredItems = []ItemStack{{ItemID: "pill", Quantity: -1}}
		}},
		{"negative event priority", func(c *Catalogue) {
			c.Events[0].Priority.Value = -1
		}},
		{"negative event weight", func(c *Catalogue) {
			c.Events[0].Weight.Value = -1
		}},
		{"negative event cooldown", func(c *Catalogue) {
			c.Events[0].CooldownMonths = -1
		}},
		{"negative event expiry", func(c *Catalogue) {
			c.Events[0].ExpiresAfterMonths = -1
		}},
		{"negative breakthrough month cost", func(c *Catalogue) {
			c.Breakthroughs[0].MonthCost.Value = -1
		}},
		{"negative breakthrough material quantity", func(c *Catalogue) {
			c.Breakthroughs[0].Materials = []ItemStack{{ItemID: "pill", Quantity: -1}}
		}},
		{"negative sect income", func(c *Catalogue) {
			c.Sects = []SectDefinition{{ID: "s", MonthlyIncome: ConfigValue{
				Provenance: ProvenanceDesignNote, Value: -5,
			}}}
		}},
		{"negative cost months", func(c *Catalogue) {
			c.Events[0].Choices[0].Costs = []EventCost{{
				Kind: CostMonth, Amount: 1, Months: -1,
			}}
		}},
		{"non-positive event cost amount", func(c *Catalogue) {
			c.Events[0].Choices[0].Costs = []EventCost{{
				Kind: CostResource, Resource: ResSpiritStones, Amount: 0,
			}}
		}},
		{"clamp min above max", func(c *Catalogue) {
			c.Breakthroughs[0].ClampMinPct.Value = 90
			c.Breakthroughs[0].ClampMaxPct.Value = 10
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCatalogue()
			tc.break_(&c)
			rep := ValidateCatalogue(&c)
			// Several of these rules share a code; assert on the family.
			if !rep.Has(VErrNegativeCost) && !rep.Has(VErrNonPositiveAmount) {
				t.Fatalf("expected a cost rejection, got codes %v", rep.Codes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Criterion: a missing breakthrough table fails.
//
// This is the criterion the design document states most plainly: a missing
// table must be an error, not a silent default.
// ---------------------------------------------------------------------------

func TestMissingBreakthroughTableFails(t *testing.T) {
	c := validCatalogue()
	// Drop every major table. Each realm should now be reported missing its
	// breakthrough, for every realm that has a successor.
	kept := make([]BreakthroughDefinition, 0, len(c.Breakthroughs))
	for _, b := range c.Breakthroughs {
		if !b.IsMajor {
			kept = append(kept, b)
		}
	}
	c.Breakthroughs = kept

	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrMissingBreakthrough) {
		t.Fatalf("expected %s, got codes %v", VErrMissingBreakthrough, rep.Codes())
	}
	// Every realm except the last has a successor, so every one needs a table.
	want := len(RealmOrder) - 1
	if got := rep.CountOf(VErrMissingBreakthrough); got != want {
		t.Fatalf("expected %d missing tables (one per realm with a successor), got %d",
			want, got)
	}
}

func TestSingleMissingBreakthroughTableFails(t *testing.T) {
	c := validCatalogue()
	// Remove only the炼气 major advance. This is the specific gap that would
	// silently skip the first realm transition.
	kept := make([]BreakthroughDefinition, 0, len(c.Breakthroughs))
	for _, b := range c.Breakthroughs {
		if b.IsMajor && b.FromRealm == RealmQiRefining {
			continue
		}
		kept = append(kept, b)
	}
	c.Breakthroughs = kept

	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrMissingBreakthrough) {
		t.Fatalf("expected %s for the removed realm, got %v",
			VErrMissingBreakthrough, rep.Codes())
	}
	if rep.CountOf(VErrMissingBreakthrough) != 1 {
		t.Fatalf("expected exactly one missing table, got %d",
			rep.CountOf(VErrMissingBreakthrough))
	}
}

// ---------------------------------------------------------------------------
// Criterion: all ten realms carry their lifespan, so crystallizing and
// nascent soul cannot be forgotten.
// ---------------------------------------------------------------------------

func TestEveryRealmMustBePresent(t *testing.T) {
	// Remove each realm in turn; every removal must be detected by id.
	for _, missing := range RealmOrder {
		t.Run(string(missing), func(t *testing.T) {
			c := validCatalogue()
			kept := make([]RealmDefinition, 0, len(c.Realms))
			for _, r := range c.Realms {
				if r.ID == missing {
					continue
				}
				kept = append(kept, r)
			}
			c.Realms = kept
			rep := ValidateCatalogue(&c)
			if !rep.Has(VErrMissingRealm) {
				t.Fatalf("removing realm %s was not detected; codes %v",
					missing, rep.Codes())
			}
		})
	}
}

func TestCrystallizingAndNascentSoulLifespansAreRequired(t *testing.T) {
	// These two are the realms whose base rates the source design marks as
	// filled-in, making them the likeliest to be dropped. Blanking their
	// lifespan must fail.
	for _, realm := range []Realm{RealmCrystallizing, RealmNascentSoul} {
		t.Run(string(realm), func(t *testing.T) {
			c := validCatalogue()
			for i := range c.Realms {
				if c.Realms[i].ID == realm {
					c.Realms[i].LifespanYears = ConfigValue{}
				}
			}
			rep := ValidateCatalogue(&c)
			if !rep.Has(VErrMissingProvenance) {
				t.Fatalf("blank lifespan on %s was not detected; codes %v",
					realm, rep.Codes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Criterion: configuring a high realm does not open it.
//
// The catalogue describes all ten realms for completeness, yet only the human
// path is playable. These tests pin the difference between "configured" and
// "open".
// ---------------------------------------------------------------------------

func TestHighRealmsMayBeConfiguredWithoutBeingOpen(t *testing.T) {
	c := validCatalogue()
	// All ten realms configured, one path open: valid by construction.
	if len(c.Realms) != len(RealmOrder) {
		t.Fatalf("fixture must configure all ten realms")
	}
	if len(c.M1Paths) != 1 || c.M1Paths[0] != PathHuman {
		t.Fatalf("fixture must open exactly the human path")
	}
	if rep := ValidateCatalogue(&c); !rep.OK() {
		t.Fatalf("configured-but-not-open catalogue must validate: %v", rep.Error())
	}
}

func TestOpeningAFrozenPathFails(t *testing.T) {
	for _, frozen := range []Path{PathEarthly, PathHeavenly} {
		t.Run(string(frozen), func(t *testing.T) {
			c := validCatalogue()
			c.M1Paths = append(c.M1Paths, frozen)
			rep := ValidateCatalogue(&c)
			if !rep.Has(VErrPathNotOpen) {
				t.Fatalf("opening %s was not rejected; codes %v", frozen, rep.Codes())
			}
		})
	}
}

func TestNoPathOpenFails(t *testing.T) {
	c := validCatalogue()
	c.M1Paths = nil
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrPathNotOpen) {
		t.Fatalf("an empty path list must fail; codes %v", rep.Codes())
	}
}

func TestHumanPathMustBeOpen(t *testing.T) {
	c := validCatalogue()
	// Listing only a frozen path leaves the human path closed.
	c.M1Paths = []Path{PathEarthly}
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrPathNotOpen) {
		t.Fatalf("a catalogue without the human path must fail; codes %v", rep.Codes())
	}
	// Both the frozen path and the missing human path should be reported.
	if rep.CountOf(VErrPathNotOpen) < 2 {
		t.Fatalf("expected both the frozen path and the missing human path to be reported, got %d",
			rep.CountOf(VErrPathNotOpen))
	}
}

// ---------------------------------------------------------------------------
// Criterion: unapproved parameters carry a clear mark.
// ---------------------------------------------------------------------------

func TestUnapprovedParametersMustBeMarked(t *testing.T) {
	// A trial value with no note is rejected: the mark is the note plus the
	// provenance, and a bare trial number is exactly the "silently mixed"
	// failure the design document warns about.
	c := validCatalogue()
	c.Realms[0].MonthBase = ConfigValue{Provenance: ProvenanceTrial, Value: SCALE}
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrMissingProvenance) {
		t.Fatalf("a trial value without a note must fail; codes %v", rep.Codes())
	}
}

func TestMissingProvenanceFails(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(*Catalogue)
	}{
		{"realm lifespan without provenance", func(c *Catalogue) {
			c.Realms[0].LifespanYears = ConfigValue{Value: 100}
		}},
		{"item buy price without provenance", func(c *Catalogue) {
			c.Items[0].BuyPrice = ConfigValue{Value: 20}
		}},
		{"spirit root multiplier without provenance", func(c *Catalogue) {
			c.SpiritRoots[0].Multiplier = ConfigValue{Value: SCALE}
		}},
		{"technique grade multiplier without provenance", func(c *Catalogue) {
			c.Techniques[0].GradeMultiplier = ConfigValue{Value: SCALE}
		}},
		{"quest month cost without provenance", func(c *Catalogue) {
			c.Quests[0].MonthCost = ConfigValue{Value: 1}
		}},
		{"event priority without provenance", func(c *Catalogue) {
			c.Events[0].Priority = ConfigValue{Value: 50}
		}},
		{"breakthrough base success without provenance", func(c *Catalogue) {
			c.Breakthroughs[0].BaseSuccessPct = ConfigValue{Value: 70}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCatalogue()
			tc.break_(&c)
			rep := ValidateCatalogue(&c)
			if !rep.Has(VErrMissingProvenance) {
				t.Fatalf("expected %s, got codes %v",
					VErrMissingProvenance, rep.Codes())
			}
		})
	}
}

func TestUnknownProvenanceFails(t *testing.T) {
	c := validCatalogue()
	c.Realms[0].LifespanYears = ConfigValue{Provenance: "made_up", Value: 100}
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrBadProvenance) {
		t.Fatalf("expected %s, got codes %v", VErrBadProvenance, rep.Codes())
	}
}

func TestProvenanceApprovalRule(t *testing.T) {
	// The approval predicate itself: manuscript and design-note numbers are
	// settled; trial and pending are not.
	approved := []Provenance{ProvenanceManuscript, ProvenanceDesignNote}
	for _, p := range approved {
		if !p.Approved() {
			t.Errorf("%s should be approved for release", p)
		}
	}
	unapproved := []Provenance{ProvenanceTrial, ProvenancePending}
	for _, p := range unapproved {
		if p.Approved() {
			t.Errorf("%s must not count as approved", p)
		}
	}
}

// ---------------------------------------------------------------------------
// Supporting rules.
// ---------------------------------------------------------------------------

func TestTutorialChoiceMustNotGrantAResource(t *testing.T) {
	// A tutorial that pays currency is a farmable loop.
	c := validCatalogue()
	c.Events[0].IsTutorial = true
	c.Events[0].Choices[0].Effects = []GrantEffect{{
		Kind: GrantAdditive, Target: "resources.spirit_stones",
		Amount: 10, Reason: "教程奖励",
	}}
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrTutorialReward) {
		t.Fatalf("expected %s, got codes %v", VErrTutorialReward, rep.Codes())
	}
}

func TestTutorialMayGrantAOneOffFlag(t *testing.T) {
	// The same tutorial granting a flag is fine: a flag cannot be farmed.
	c := validCatalogue()
	c.Events[0].IsTutorial = true
	if rep := ValidateCatalogue(&c); !rep.OK() {
		t.Fatalf("a tutorial granting only a flag must validate: %v", rep.Error())
	}
}

func TestRiskWithoutPreviewFails(t *testing.T) {
	// A warned risk must be shown to the player before the choice.
	c := validCatalogue()
	c.Events[0].Choices[0].Risks = []RiskSpec{{
		Kind:           RiskInjury,
		ProbabilityPct: ConfigValue{Provenance: ProvenanceDesignNote, Value: 30},
		Magnitude:      ConfigValue{Provenance: ProvenanceDesignNote, Value: 20},
		PreviewText:    "", // not previewed
	}}
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrRiskNotPreviewed) {
		t.Fatalf("expected %s, got codes %v", VErrRiskNotPreviewed, rep.Codes())
	}
}

func TestRiskProbabilityOutOfRangeFails(t *testing.T) {
	for _, bad := range []int{-1, 101} {
		c := validCatalogue()
		c.Events[0].Choices[0].Risks = []RiskSpec{{
			Kind:           RiskInjury,
			ProbabilityPct: ConfigValue{Provenance: ProvenanceDesignNote, Value: bad},
			Magnitude:      ConfigValue{Provenance: ProvenanceDesignNote, Value: 20},
			PreviewText:    "可能受伤",
		}}
		rep := ValidateCatalogue(&c)
		if !rep.Has(VErrNonPositiveAmount) {
			t.Fatalf("probability %d was not rejected; codes %v", bad, rep.Codes())
		}
	}
}

func TestNonAdultNPCFails(t *testing.T) {
	// M1 ships no romance content, and the flag is asserted now so later
	// content cannot bypass it.
	c := validCatalogue()
	c.NPCs[0].Adult = false
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrPathNotOpen) {
		t.Fatalf("a non-adult NPC must fail; codes %v", rep.Codes())
	}
}

func TestAsymmetricTravelGraphFails(t *testing.T) {
	// Zero-month travel must be reversible, or a player can reach a node they
	// cannot leave.
	c := validCatalogue()
	c.Locations[0].Neighbors = []string{"b"}
	// Remove b's return edge.
	c.Locations[1].Neighbors = nil
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrBadTravelGraph) {
		t.Fatalf("expected %s, got codes %v", VErrBadTravelGraph, rep.Codes())
	}
}

func TestMalformedConditionFails(t *testing.T) {
	cases := []struct {
		name string
		cond Precondition
	}{
		{"unknown kind", Precondition{Kind: "nonsense", Key: "k", Op: OpEQ, Value: 1}},
		{"unknown operator", Precondition{Kind: CondWorldMonth, Key: "k", Op: "≈", Value: 1}},
		{"missing key", Precondition{Kind: CondHasItem, Op: OpEQ, Value: 1}},
		{"operator on a kind that has nothing to compare", Precondition{
			Kind: CondNPCAvailable, Key: "npc", Op: "≈",
		}},
		{"text value on a numeric kind", Precondition{
			Kind: CondWorldMonth, Key: "k", Op: OpEQ, TextValue: "x",
		}},
		{"numeric kind without text on a string kind", Precondition{
			Kind: CondFlag, Key: "k", Op: OpEQ,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCatalogue()
			c.Events[0].Eligibility = []Precondition{tc.cond}
			rep := ValidateCatalogue(&c)
			if !rep.Has(VErrBadCondition) {
				t.Fatalf("expected %s, got codes %v", VErrBadCondition, rep.Codes())
			}
		})
	}
}

func TestUnknownEnumValuesFail(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(*Catalogue)
	}{
		{"unknown realm", func(c *Catalogue) {
			c.Realms[0].ID = "made_up_realm"
		}},
		{"unknown origin", func(c *Catalogue) {
			c.Origins[0].ID = "made_up_origin"
		}},
		{"unknown spirit root", func(c *Catalogue) {
			c.SpiritRoots[0].ID = "made_up_root"
		}},
		{"unknown item category", func(c *Catalogue) {
			c.Items[0].Category = "made_up_category"
		}},

		{"unknown quest kind", func(c *Catalogue) {
			c.Quests[0].Kind = "made_up_kind"
		}},
		{"unknown cost kind", func(c *Catalogue) {
			c.Events[0].Choices[0].Costs = []EventCost{{
				Kind: "made_up", Amount: 1,
			}}
		}},
		{"unknown risk kind", func(c *Catalogue) {
			c.Events[0].Choices[0].Risks = []RiskSpec{{
				Kind:           "made_up",
				ProbabilityPct: ConfigValue{Provenance: ProvenanceDesignNote, Value: 10},
				Magnitude:      ConfigValue{Provenance: ProvenanceDesignNote, Value: 1},
				PreviewText:    "x",
			}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCatalogue()
			tc.break_(&c)
			rep := ValidateCatalogue(&c)
			if !rep.Has(VErrUnknownEnum) {
				t.Fatalf("expected %s, got codes %v", VErrUnknownEnum, rep.Codes())
			}
		})
	}
}

func TestZeroAmountGrantFails(t *testing.T) {
	c := validCatalogue()
	c.Origins[1].Effects[0].Amount = 0
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrNonPositiveAmount) {
		t.Fatalf("a zero grant must fail; codes %v", rep.Codes())
	}
}

func TestGrantWithoutReasonFails(t *testing.T) {
	c := validCatalogue()
	c.Origins[1].Effects[0].Reason = ""
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrMissingProvenance) {
		t.Fatalf("a grant without a reason must fail; codes %v", rep.Codes())
	}
}

func TestEquipmentWithoutSlotFails(t *testing.T) {
	c := validCatalogue()
	c.Items[0].Category = ItemEquipment
	c.Items[0].EquipSlot = ""
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrMissingProvenance) {
		t.Fatalf("equipment without a slot must fail; codes %v", rep.Codes())
	}
}

func TestNilCatalogueFails(t *testing.T) {
	rep := ValidateCatalogue(nil)
	if rep.OK() {
		t.Fatal("a nil catalogue must not pass validation")
	}
}

func TestReportCodesAreSortedAndUnique(t *testing.T) {
	c := validCatalogue()
	c.Items = append(c.Items, c.Items[0]) // duplicate
	c.Quests[0].MonthCost.Value = -1      // negative
	c.M1Paths = []Path{PathHeavenly}      // frozen path

	rep := ValidateCatalogue(&c)
	codes := rep.Codes()
	for i := 1; i < len(codes); i++ {
		if codes[i] <= codes[i-1] {
			t.Fatalf("codes must be sorted and deduplicated, got %v", codes)
		}
	}
}

func TestValidationIsTotalNotFailFast(t *testing.T) {
	// A broken catalogue should report every defect, not stop at the first, so
	// a content author can fix the whole list in one pass.
	c := validCatalogue()
	c.Items = append(c.Items, c.Items[0])        // duplicate item
	c.Quests[0].MonthCost.Value = -1             // negative cost
	c.Locations[0].Neighbors = []string{"ghost"} // dangling reference

	rep := ValidateCatalogue(&c)
	if len(rep.Errors) < 3 {
		t.Fatalf("expected at least three distinct defects, got %d: %v",
			len(rep.Errors), rep.Error())
	}
}

// ---------------------------------------------------------------------------
// Code specificity: the validator uses targeted codes where it can, which makes
// a failure diagnosable without reading the detail string. These two assert the
// targeted codes rather than the generic fallback.
// ---------------------------------------------------------------------------

func TestEventChainToUndeclaredNodeReportsBrokenChain(t *testing.T) {
	c := validCatalogue()
	c.Events[0].Choices[0].NextNodeID = "ghost"
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrBadChain) {
		t.Fatalf("expected %s, got codes %v", VErrBadChain, rep.Codes())
	}
}

func TestUnknownTechniqueGradeReportsUnknownGrade(t *testing.T) {
	c := validCatalogue()
	c.Techniques[0].Grade = "made_up_grade"
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrBadGrade) {
		t.Fatalf("expected %s, got codes %v", VErrBadGrade, rep.Codes())
	}
}

func TestEventChainToOwnFollowUpNodeIsAccepted(t *testing.T) {
	// A chain that points at a declared follow-up node must be legal, so the
	// chain check is not merely rejecting everything. The follow-up node's
	// candidates must be choices the event actually declares: the scheduler
	// freezes them into the pending instance, and an undeclared id would freeze
	// a candidate set the player could never answer.
	c := validCatalogue()
	c.Events[0].FollowUps = []EventChainNode{{NodeID: "node2", ChoiceIDs: []string{"c1"}}}
	c.Events[0].Choices[0].NextNodeID = "node2"
	if rep := ValidateCatalogue(&c); !rep.OK() {
		t.Fatalf("a chain to a declared follow-up node must validate: %v", rep.Error())
	}
}

func TestEventFollowUpOfferingAnUndeclaredChoiceIsRejected(t *testing.T) {
	c := validCatalogue()
	c.Events[0].FollowUps = []EventChainNode{{NodeID: "node2", ChoiceIDs: []string{"c9"}}}
	c.Events[0].Choices[0].NextNodeID = "node2"
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrDanglingRef) {
		t.Fatalf("expected %s for a follow-up node offering an undeclared choice, got codes %v",
			VErrDanglingRef, rep.Codes())
	}
}

// TestNPCWithoutAnExplicitAgeIsRejected and its sibling pin the TASK-11 age
// rules. Design 14 requires every NPC's age to be explicit, and an NPC older
// than its own ceiling would start the game dead.
func TestNPCWithoutAnExplicitAgeIsRejected(t *testing.T) {
	c := validCatalogue()
	c.NPCs[0].InitialAgeYears = 0
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrMissingProvenance) {
		t.Fatalf("expected %s for an NPC with no age, got codes %v",
			VErrMissingProvenance, rep.Codes())
	}
}

func TestNPCOlderThanItsLifespanIsRejected(t *testing.T) {
	c := validCatalogue()
	c.NPCs[0].LifespanYears = 40
	c.NPCs[0].InitialAgeYears = 41
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrInvariantBroken) {
		t.Fatalf("expected %s for an NPC past its own lifespan, got codes %v",
			VErrInvariantBroken, rep.Codes())
	}
}

func TestForcedEventMustNotAlsoBeDrawn(t *testing.T) {
	c := validCatalogue()
	c.Events[0].Forced = true
	// validEvents gives weight 10; a forced event that could also win the
	// weighted draw would consume both slots and then be unable to fire.
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrBadChain) {
		t.Fatalf("expected %s for a forced event with a non-zero weight, got codes %v",
			VErrBadChain, rep.Codes())
	}
}

func TestForcedEventWithZeroWeightIsAccepted(t *testing.T) {
	c := validCatalogue()
	c.Events[0].Forced = true
	c.Events[0].Weight = ConfigValue{Provenance: ProvenanceDesignNote, Value: 0, Note: "forced, not drawn"}
	if rep := ValidateCatalogue(&c); !rep.OK() {
		t.Fatalf("a forced event with weight 0 must validate: %v", rep.Error())
	}
}

func TestUnknownEffectTargetIsRejected(t *testing.T) {
	c := validCatalogue()
	c.Events[0].Choices[0].Effects = []GrantEffect{{
		Kind: GrantAdditive, Target: "resources.spirit_stones; rm -rf /", Amount: 1, Reason: "注入尝试",
	}}
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrUnknownEnum) {
		t.Fatalf("expected %s for a target outside the whitelist, got codes %v",
			VErrUnknownEnum, rep.Codes())
	}
}

func TestRuntimeEffectCannotNameACreationOnlyTarget(t *testing.T) {
	// cultivation_rate is folded by the creation factory and recomputed every
	// month at runtime, so an event that granted it would be silently
	// overwritten within one month. Rejecting it at load time is the difference
	// between content that does not work and content that fails loudly.
	c := validCatalogue()
	c.Events[0].Choices[0].Effects = []GrantEffect{{
		Kind: GrantMultiplicative, Target: targetCultivationRate, Amount: 15 * SCALE / 10, Reason: "尝试",
	}}
	rep := ValidateCatalogue(&c)
	if !rep.Has(VErrUnknownEnum) {
		t.Fatalf("expected %s for a creation-only target in a runtime effect, got codes %v",
			VErrUnknownEnum, rep.Codes())
	}
}

func TestCreationEffectMayNameACreationOnlyTarget(t *testing.T) {
	// The control group: the same target must remain legal where the factory
	// consumes it, otherwise the rule above would be rejecting a legitimate
	// number rather than an out-of-scope one.
	c := validCatalogue()
	c.Origins[0].Effects = []GrantEffect{{
		Kind: GrantMultiplicative, Target: targetCultivationRate, Amount: 15 * SCALE / 10, Reason: "出身加成",
	}}
	if rep := ValidateCatalogue(&c); !rep.OK() {
		t.Fatalf("a creation effect naming cultivation_rate must validate: %v", rep.Error())
	}
}
