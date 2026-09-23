package content

import (
	"strings"
	"testing"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

// The shipped M1 catalogue is the artefact TASK-04 delivers, so these tests
// assert on it directly rather than on a fixture. A fixture can be valid while
// the shipped data is not, which is the failure mode that matters.

func TestM1CatalogueValidates(t *testing.T) {
	c := M1()
	rep := engine.ValidateCatalogue(&c)
	if !rep.OK() {
		t.Fatalf("the shipped M1 catalogue must validate, got %d error(s):\n%v",
			len(rep.Errors), rep.Error())
	}
}

func TestLoadM1ReturnsValidatedCatalogue(t *testing.T) {
	c, err := LoadM1()
	if err != nil {
		t.Fatalf("LoadM1: %v", err)
	}
	if c.Version != Version {
		t.Fatalf("loaded catalogue version %d, want %d", c.Version, Version)
	}
	// Loading twice must yield independent copies, so a caller mutating one
	// cannot corrupt another.
	a, _ := LoadM1()
	b, _ := LoadM1()
	a.Realms[0].NameZH = "mutated"
	bb, _ := LoadM1()
	if bb.Realms[0].NameZH == "mutated" {
		t.Fatal("LoadM1 returned shared state; callers must not be able to corrupt the catalogue")
	}
	_ = b
}

func TestM1ConfiguresAllTenRealms(t *testing.T) {
	c := M1()
	byID := map[engine.Realm]bool{}
	for _, r := range c.Realms {
		byID[r.ID] = true
	}
	for _, want := range engine.RealmOrder {
		if !byID[want] {
			t.Errorf("realm %s is missing from the shipped catalogue", want)
		}
	}
	if len(c.Realms) != len(engine.RealmOrder) {
		t.Errorf("expected exactly %d realms, got %d", len(engine.RealmOrder), len(c.Realms))
	}
}

func TestM1OpensOnlyTheHumanPath(t *testing.T) {
	c := M1()
	if len(c.M1Paths) != 1 || c.M1Paths[0] != engine.PathHuman {
		t.Fatalf("M1 must open exactly the human path, got %v", c.M1Paths)
	}
}

func TestM1ConfiguresHighRealmsWithoutOpeningThem(t *testing.T) {
	// The point of the milestone distinction: configuring羽化/登仙 is required
	// for a complete table, but they are not open, and opening them would need
	// ADR-001 revised first.
	c := M1()
	configured := map[engine.Realm]bool{}
	for _, r := range c.Realms {
		configured[r.ID] = true
	}
	for _, high := range []engine.Realm{engine.RealmAscension, engine.RealmImmortal} {
		if !configured[high] {
			t.Errorf("high realm %s should still be configured so the table is complete", high)
		}
	}
	if !c.M1Paths[0].OpenInM1() {
		t.Errorf("the human path should report itself open in M1")
	}
	for _, other := range []engine.Path{engine.PathEarthly, engine.PathHeavenly} {
		if other.OpenInM1() {
			t.Errorf("path %s must not report itself open in M1", other)
		}
	}
}

func TestM1RealmLifespansAreOrdered(t *testing.T) {
	c := M1()
	byID := map[engine.Realm]int{}
	for _, r := range c.Realms {
		byID[r.ID] = r.LifespanYears.Value
	}
	prev := 0
	prevRealm := engine.Realm("")
	for _, realm := range engine.RealmOrder {
		got, ok := byID[realm]
		if !ok {
			t.Fatalf("realm %s missing", realm)
		}
		if got <= prev {
			t.Fatalf("realm %s lifespan %d does not exceed %s's %d",
				realm, got, prevRealm, prev)
		}
		prev = got
		prevRealm = realm
	}
}

func TestM1HighRealmThresholdsAreMarkedUnapproved(t *testing.T) {
	c := M1()
	for _, r := range c.Realms {
		if r.ID != engine.RealmAscension && r.ID != engine.RealmImmortal {
			continue
		}
		if r.TierThreshold.Provenance.Approved() {
			t.Errorf("realm %s threshold is marked %s, but the design document says it is not approved",
				r.ID, r.TierThreshold.Provenance)
		}
		if strings.TrimSpace(r.TierThreshold.Note) == "" {
			t.Errorf("realm %s threshold is unapproved and must carry a note", r.ID)
		}
	}
}

func TestM1CrystallizingAndNascentSoulBasesAreMarkedTrial(t *testing.T) {
	c := M1()
	for _, r := range c.Realms {
		if r.ID != engine.RealmCrystallizing && r.ID != engine.RealmNascentSoul {
			continue
		}
		if r.MonthBase.Provenance != engine.ProvenanceTrial {
			t.Errorf("realm %s base rate is marked %s; the design document marks it as a trial fill-in",
				r.ID, r.MonthBase.Provenance)
		}
		if strings.TrimSpace(r.MonthBase.Note) == "" {
			t.Errorf("realm %s base rate is a trial value and must carry a note", r.ID)
		}
	}
}

func TestM1BreakthroughChainIsComplete(t *testing.T) {
	c := M1()
	// Every realm except the last needs a major table, and every realm needs
	// the three minor advances.
	major := map[engine.Realm]bool{}
	minorCount := map[engine.Realm]int{}
	for _, b := range c.Breakthroughs {
		if b.Path != engine.PathHuman {
			continue
		}
		if b.IsMajor {
			major[b.FromRealm] = true
		} else {
			minorCount[b.FromRealm]++
		}
	}
	for i, realm := range engine.RealmOrder {
		if i < len(engine.RealmOrder)-1 && !major[realm] {
			t.Errorf("realm %s has no major breakthrough table", realm)
		}
		if minorCount[realm] != 3 {
			t.Errorf("realm %s has %d minor advances, want 3", realm, minorCount[realm])
		}
	}
}

func TestM1OnlyConfiguresTheHumanBreakthroughChain(t *testing.T) {
	c := M1()
	for _, b := range c.Breakthroughs {
		if b.Path != engine.PathHuman {
			t.Errorf("breakthrough for path %s is configured, but M1 opens only the human path",
				b.Path)
		}
	}
}

func TestM1HasTwelveDistinctEventNodes(t *testing.T) {
	// The design document requires twelve genuinely different nodes, not the
	// same text renamed. Asserting distinct ids, purposes and shapes catches
	// the rename-inflation failure.
	c := M1()
	if len(c.Events) != 12 {
		t.Fatalf("expected 12 event nodes, got %d", len(c.Events))
	}
	ids := map[string]bool{}
	purposes := map[string]string{}
	for _, e := range c.Events {
		if ids[e.ID] {
			t.Errorf("duplicate event id %s", e.ID)
		}
		ids[e.ID] = true
		// The list is generated as EVT-001..EVT-012.
		if !strings.HasPrefix(e.ID, "EVT-") {
			t.Errorf("event id %s does not follow the EVT-nnn convention", e.ID)
		}
		if strings.TrimSpace(e.Purpose) == "" {
			t.Errorf("event %s has no stated purpose, so it cannot be reviewed as non-padding", e.ID)
		}
		if prev, dup := purposes[e.Purpose]; dup {
			t.Errorf("events %s and %s share a purpose, which suggests padding", prev, e.ID)
		}
		purposes[e.Purpose] = e.ID
		if len(e.Choices) == 0 {
			t.Errorf("event %s offers no choices", e.ID)
		}
	}
	for i := 1; i <= 12; i++ {
		want := "EVT-00" + string(rune('0'+i))
		if i >= 10 {
			want = "EVT-0" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		}
		if !ids[want] {
			t.Errorf("expected node %s to exist", want)
		}
	}
}

func TestM1TutorialGrantsNoFarmableReward(t *testing.T) {
	c := M1()
	for _, e := range c.Events {
		if !e.IsTutorial {
			continue
		}
		for _, ch := range e.Choices {
			for _, eff := range ch.Effects {
				if eff.Kind == engine.GrantAdditive && strings.HasPrefix(eff.Target, "resources.") {
					t.Errorf("tutorial event %s choice %s grants %s, which could be farmed",
						e.ID, ch.ID, eff.Target)
				}
			}
		}
	}
}

func TestM1EveryRiskyChoicePreviewsItsRisk(t *testing.T) {
	c := M1()
	for _, e := range c.Events {
		for _, ch := range e.Choices {
			for i, risk := range ch.Risks {
				if strings.TrimSpace(risk.PreviewText) == "" {
					t.Errorf("event %s choice %s risk[%d] is not previewed", e.ID, ch.ID, i)
				}
				if risk.ProbabilityPct.Value < 0 || risk.ProbabilityPct.Value > 100 {
					t.Errorf("event %s choice %s risk[%d] probability %d is out of range",
						e.ID, ch.ID, i, risk.ProbabilityPct.Value)
				}
			}
		}
	}
}

func TestM1NPCsAreAllDeclaredAdult(t *testing.T) {
	c := M1()
	for _, n := range c.NPCs {
		if !n.Adult {
			t.Errorf("npc %s is not declared adult; romance-adjacent content must be gated from the start",
				n.ID)
		}
	}
}

func TestM1OriginalNPCsAreMarkedAsNewSetting(t *testing.T) {
	// The design document requires original material to be distinguishable
	// from manuscript material.
	c := M1()
	for _, n := range c.NPCs {
		if n.FromManuscript && n.NewSetting {
			t.Errorf("npc %s claims both manuscript and new-setting provenance", n.ID)
		}
		if !n.FromManuscript && !n.NewSetting {
			t.Errorf("npc %s declares neither manuscript nor new-setting provenance", n.ID)
		}
	}
}

func TestM1TravelGraphIsFullyReversible(t *testing.T) {
	c := M1()
	neighbors := map[string][]string{}
	for _, l := range c.Locations {
		neighbors[l.ID] = l.Neighbors
	}
	for _, l := range c.Locations {
		for _, nb := range l.Neighbors {
			found := false
			for _, back := range neighbors[nb] {
				if back == l.ID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("travel edge %s -> %s has no return edge", l.ID, nb)
			}
		}
	}
}

func TestM1TravellingDoesNotRefreshRewards(t *testing.T) {
	// Zero-month travel between safe nodes must not be a source of income.
	// The guarantee is structural: rewards live on quests and events (which
	// carry cooldowns or one-off guards), never on locations. This test pins
	// that structure by asserting every repeatable income source is a quest
	// with an explicit repeatability rule, and that travel itself grants
	// nothing because LocationDefinition has no effects field at all.
	c := M1()
	for _, q := range c.Quests {
		if !q.Repeatable {
			continue
		}
		// A repeatable quest must be explicitly marked, so a later edit that
		// makes a quest repeatable is a visible change rather than a silent
		// money loop.
		if q.MonthCost.Value <= 0 {
			t.Errorf("repeatable quest %s has month cost %d; a repeatable income source must cost time",
				q.ID, q.MonthCost.Value)
		}
	}
}

func TestM1QuestRewardsArePositiveAndExplained(t *testing.T) {
	c := M1()
	for _, q := range c.Quests {
		if len(q.Rewards) == 0 {
			t.Errorf("quest %s grants nothing, so it is pointless", q.ID)
		}
		for i, r := range q.Rewards {
			if r.Amount <= 0 {
				t.Errorf("quest %s reward[%d] has non-positive amount %d", q.ID, i, r.Amount)
			}
			if strings.TrimSpace(r.Reason) == "" {
				t.Errorf("quest %s reward[%d] has no reason", q.ID, i)
			}
		}
	}
}

func TestM1SafeChoreMatchesTheDesignArithmetic(t *testing.T) {
	// The design document derives "about 3.5 years" from 14 chore runs at 30
	// stones each, to afford a 500-stone pill from a 100-stone start. If the
	// chore reward or the pill price changes, that documented arithmetic is no
	// longer true and the doc must be updated too. This test is the tripwire.
	c := M1()

	choreReward := int64(0)
	for _, q := range c.Quests {
		if q.ID == "quest_safe_chore" {
			for _, r := range q.Rewards {
				if r.Target == "resources.spirit_stones" {
					choreReward = r.Amount
				}
			}
		}
	}
	if choreReward != 30 {
		t.Errorf("safe chore reward is %d, but the design document's arithmetic assumes 30",
			choreReward)
	}

	pillPrice := int64(0)
	for _, it := range c.Items {
		if it.ID == "foundation_pill" {
			pillPrice = int64(it.BuyPrice.Value)
		}
	}
	if pillPrice != 500 {
		t.Errorf("foundation pill price is %d, but the design document's arithmetic assumes 500",
			pillPrice)
	}

	// ceil((500-100)/30) = 14 runs.
	const start = 100
	needed := pillPrice - start
	runs := (needed + choreReward - 1) / choreReward
	if runs != 14 {
		t.Errorf("arithmetic yields %d chore runs, but the design document states 14", runs)
	}
	// Balance after buying: 100 + 14*30 - 500 = 20.
	leftover := start + runs*choreReward - pillPrice
	if leftover != 20 {
		t.Errorf("leftover is %d, but the design document states 20", leftover)
	}
}

func TestM1NamedPricesMatchTheManuscript(t *testing.T) {
	c := M1()
	want := map[string]int{
		"qi_gathering_pill": 20,
		"foundation_pill":   500,
	}
	for _, it := range c.Items {
		if v, ok := want[it.ID]; ok {
			if it.BuyPrice.Value != v {
				t.Errorf("item %s price is %d, but the manuscript states %d",
					it.ID, it.BuyPrice.Value, v)
			}
			if it.BuyPrice.Provenance != engine.ProvenanceManuscript {
				t.Errorf("item %s price is named in the manuscript but marked %s",
					it.ID, it.BuyPrice.Provenance)
			}
		}
	}
}

func TestM1RecyclePriceIsHalfOfBuyPrice(t *testing.T) {
	// The manuscript's candidate recycle ratio is 50%. Asserting the configured
	// numbers (rather than recomputing) keeps the ratio auditable.
	c := M1()
	for _, it := range c.Items {
		if it.BuyPrice.Value == 0 {
			continue
		}
		want := it.BuyPrice.Value / 2
		if it.SellPrice.Value != want {
			t.Errorf("item %s recycles at %d, expected half of %d = %d",
				it.ID, it.SellPrice.Value, it.BuyPrice.Value, want)
		}
	}
}

func TestM1SpiritRootMultipliersMatchTheManuscript(t *testing.T) {
	c := M1()
	want := map[engine.SpiritRoot]int{
		engine.RootHeavenly: 20 * engine.SCALE / 10,
		engine.RootEarthly:  16 * engine.SCALE / 10,
		engine.RootTrue:     13 * engine.SCALE / 10,
		engine.RootPseudo:   10 * engine.SCALE / 10,
		engine.RootVariant:  18 * engine.SCALE / 10,
	}
	for _, sr := range c.SpiritRoots {
		if v, ok := want[sr.ID]; ok {
			if sr.Multiplier.Value != v {
				t.Errorf("spirit root %s multiplier is %d, want %d",
					sr.ID, sr.Multiplier.Value, v)
			}
			if sr.Multiplier.Provenance != engine.ProvenanceManuscript {
				t.Errorf("spirit root %s multiplier is a manuscript figure but marked %s",
					sr.ID, sr.Multiplier.Provenance)
			}
		}
	}
}

func TestM1TechniqueGradeMultipliersMatchTheManuscript(t *testing.T) {
	c := M1()
	want := map[engine.TechniqueGrade]int{
		engine.GradeYellow:   10 * engine.SCALE / 10,
		engine.GradeMystic:   13 * engine.SCALE / 10,
		engine.GradeEarth:    17 * engine.SCALE / 10,
		engine.GradeHeaven:   22 * engine.SCALE / 10,
		engine.GradeImmortal: 30 * engine.SCALE / 10,
	}
	for _, tch := range c.Techniques {
		if v, ok := want[tch.Grade]; ok {
			if tch.GradeMultiplier.Value != v {
				t.Errorf("technique %s grade multiplier is %d, want %d",
					tch.ID, tch.GradeMultiplier.Value, v)
			}
		}
	}
}

func TestM1MerchantGrantsThreeHundredStones(t *testing.T) {
	c := M1()
	for _, o := range c.Origins {
		if o.ID != engine.OriginMerchant {
			continue
		}
		found := false
		for _, e := range o.Effects {
			if e.Target == "resources.spirit_stones" && e.Amount == 300 {
				found = true
			}
		}
		if !found {
			t.Error("merchant origin must grant 300 spirit stones, per the design document")
		}
	}
}

func TestM1HunterGrantsTwentyMaxAndCurrentHP(t *testing.T) {
	c := M1()
	for _, o := range c.Origins {
		if o.ID != engine.OriginHunter {
			continue
		}
		var maxGain, currentGain int64
		for _, e := range o.Effects {
			switch e.Target {
			case "hp.max":
				maxGain = e.Amount
			case "hp.current":
				currentGain = e.Amount
			}
		}
		if maxGain != 20 || currentGain != 20 {
			t.Errorf("hunter origin must grant +20 to both hp.max and hp.current; got max=%d current=%d",
				maxGain, currentGain)
		}
	}
}

func TestM1EveryGrantNamesItsReason(t *testing.T) {
	// The ledger cannot explain a change that has no reason, so every grant in
	// the shipped catalogue must carry one.
	c := M1()
	check := func(where string, effects []engine.GrantEffect) {
		for i, e := range effects {
			if strings.TrimSpace(e.Reason) == "" {
				t.Errorf("%s[%d] (target %s) has no reason", where, i, e.Target)
			}
		}
	}
	for _, o := range c.Origins {
		check("origins."+string(o.ID), o.Effects)
	}
	for _, con := range c.Constitutions {
		check("constitutions."+con.ID, con.Effects)
	}
	for _, tal := range c.Talents {
		check("talents."+tal.ID, tal.Effects)
	}
	for _, it := range c.Items {
		check("items."+it.ID, it.Effects)
	}
	for _, q := range c.Quests {
		check("quests."+q.ID, q.Rewards)
	}
	for _, e := range c.Events {
		for _, ch := range e.Choices {
			check("events."+e.ID+".choices."+ch.ID, ch.Effects)
		}
	}
}

// ---------------------------------------------------------------------------
// Index.
// ---------------------------------------------------------------------------

func TestIndexResolvesEveryID(t *testing.T) {
	c := MustLoadM1()
	idx := NewIndex(c)

	if len(idx.Realms) != len(c.Realms) {
		t.Errorf("realm index has %d entries for %d realms", len(idx.Realms), len(c.Realms))
	}
	if len(idx.Items) != len(c.Items) {
		t.Errorf("item index has %d entries for %d items", len(idx.Items), len(c.Items))
	}
	if len(idx.Events) != len(c.Events) {
		t.Errorf("event index has %d entries for %d events", len(idx.Events), len(c.Events))
	}
	if len(idx.Breakthroughs) != len(c.Breakthroughs) {
		t.Errorf("breakthrough index has %d entries for %d rows",
			len(idx.Breakthroughs), len(c.Breakthroughs))
	}

	// Spot-check the lookup that later tasks depend on most: the breakthrough
	// row for a given position.
	k := BreakthroughKey{Path: engine.PathHuman, FromRealm: engine.RealmQiRefining, FromTier: engine.TierPerfection}
	b, ok := idx.Breakthroughs[k]
	if !ok {
		t.Fatal("breakthrough index is missing the炼气 perfection -> 筑基 early row")
	}
	if !b.IsMajor {
		t.Error("the炼气 perfection row must be a major breakthrough")
	}
	if b.ToRealm != engine.RealmFoundation {
		t.Errorf("the row advances to %s, want %s", b.ToRealm, engine.RealmFoundation)
	}
}

func TestIndexRealmByOrderIsDense(t *testing.T) {
	c := MustLoadM1()
	idx := NewIndex(c)
	if len(idx.RealmByOrder) != len(engine.RealmOrder) {
		t.Fatalf("RealmByOrder has %d entries, want %d",
			len(idx.RealmByOrder), len(engine.RealmOrder))
	}
	for i, r := range idx.RealmByOrder {
		if r.ID != engine.RealmOrder[i] {
			t.Errorf("RealmByOrder[%d] is %s, want %s", i, r.ID, engine.RealmOrder[i])
		}
	}
}

func TestIndexLookupsReturnContent(t *testing.T) {
	c := MustLoadM1()
	idx := NewIndex(c)

	if it, ok := idx.Items["foundation_pill"]; !ok || it.NameZH != "筑基丹" {
		t.Errorf("foundation pill lookup failed: %+v ok=%v", it, ok)
	}
	if n, ok := idx.NPCs["gu_qingxuan"]; !ok || !n.FromManuscript {
		t.Errorf("顾清玄 lookup failed or lost its manuscript provenance: %+v ok=%v", n, ok)
	}
	if l, ok := idx.Locations["market"]; !ok || !l.Safe {
		t.Errorf("market lookup failed or is not safe: %+v ok=%v", l, ok)
	}
}

func TestMustLoadM1PanicsOnlyOnInvalidContent(t *testing.T) {
	// The shipped catalogue is valid, so MustLoadM1 must not panic. This is a
	// guard against a build-breaking content edit slipping in.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("MustLoadM1 panicked on the shipped catalogue: %v", r)
		}
	}()
	_ = MustLoadM1()
}
