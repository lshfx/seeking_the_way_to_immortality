package content

import (
	"testing"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

// Shipped-data tests for the TASK-12 condition, lifespan and karma content.
//
// They assert on M1() rather than on a fixture, because the numbers a player
// meets are the shipped ones.

// TestShippedLifespanCeilingsMatchTheDesign pins the two figures design 7.4
// works through: 21岁炼气余79年 and 同龄筑基余179年. Both follow from the ceilings,
// so the ceilings are what is asserted.
func TestShippedLifespanCeilingsMatchTheDesign(t *testing.T) {
	c := M1()
	want := map[engine.Realm]int{
		engine.RealmQiRefining: 100,
		engine.RealmFoundation: 200,
	}
	for realm, years := range want {
		def, ok := findRealmDefinition(c, realm)
		if !ok {
			t.Errorf("the catalogue does not describe %s", realm)
			continue
		}
		if def.LifespanYears.Value != years {
			t.Errorf("%s lifespan = %d years, want %d", realm, def.LifespanYears.Value, years)
		}
	}
}

func TestEveryShippedRealmHasALifespan(t *testing.T) {
	// A realm with no ceiling would leave a character at that realm immortal,
	// and the missing number would be invisible until someone got there.
	c := M1()
	for _, realm := range engine.RealmOrder {
		def, ok := findRealmDefinition(c, realm)
		if !ok {
			t.Errorf("the catalogue does not describe %s", realm)
			continue
		}
		if def.LifespanYears.Value <= 0 {
			t.Errorf("%s has lifespan %d", realm, def.LifespanYears.Value)
		}
	}
}

func TestShippedAfflictionsCoverAllFourKinds(t *testing.T) {
	// The task list names 伤势、中毒、内伤、心境异常. Shipping fewer than four kinds
	// would make the "疗伤不清除无关异常" rule unobservable: with only wounds,
	// resting clears everything and the distinction never shows.
	c := M1()
	seen := map[engine.AfflictionKind]int{}
	for _, a := range c.Afflictions {
		seen[a.Kind]++
	}
	for _, kind := range []engine.AfflictionKind{
		engine.AfflictionWound, engine.AfflictionPoison,
		engine.AfflictionInternal, engine.AfflictionMood,
	} {
		if seen[kind] == 0 {
			t.Errorf("no shipped affliction has kind %s", kind)
		}
	}
}

func TestAtLeastOneShippedAfflictionNeedsMoreThanRest(t *testing.T) {
	// This is what makes 忍伤 a decision rather than a menu entry. If every
	// affliction were rest-clearable, the optimal play would always be to rest
	// and the 疗伤-versus-progress trade would not exist.
	c := M1()
	notRestable := 0
	for _, a := range c.Afflictions {
		if !a.ClearedByRest {
			notRestable++
		}
	}
	if notRestable == 0 {
		t.Fatal("every shipped affliction can be mended by resting, so there is never a reason " +
			"to carry one")
	}
}

func TestEveryShippedAfflictionLastsAndDoesSomething(t *testing.T) {
	c := M1()
	for _, a := range c.Afflictions {
		if a.DurationMonths.Value <= 0 {
			t.Errorf("%s lasts %d months", a.ID, a.DurationMonths.Value)
		}
		doesSomething := a.HPDrainPerMonth.Value != 0 ||
			a.MoodDrainPerMonth.Value != 0 ||
			a.SpeedPenaltyPermille.Value != 0 ||
			a.EffectiveAptitudeDelta.Value != 0 ||
			a.RateBonus.Value != 0
		if !doesSomething {
			t.Errorf("%s changes nothing, so it would occupy a slot and read as a bug", a.ID)
		}
	}
}

func TestShippedInjuryBandsCoverEveryBand(t *testing.T) {
	c := M1()
	seen := map[engine.InjuryBand]bool{}
	for _, b := range c.InjuryBands {
		seen[b.Band] = true
	}
	for _, band := range engine.InjuryBandOrder {
		if !seen[band] {
			t.Errorf("band %s is not configured", band)
		}
	}
	if c.InjuryBands[0].NameZH == "" {
		t.Error("a band has no display name")
	}
}

func TestShippedKarmaTiersStartAtZero(t *testing.T) {
	// Without a tier at zero, a character who has done nothing would have no
	// description at all.
	c := M1()
	if len(c.KarmaTiers) == 0 {
		t.Fatal("no karma tiers are shipped")
	}
	hasFloor := false
	for _, tier := range c.KarmaTiers {
		if tier.MinKarma.Value == 0 {
			hasFloor = true
		}
		if tier.NameZH == "" {
			t.Errorf("karma tier %s has no display name", tier.ID)
		}
	}
	if !hasFloor {
		t.Error("no karma tier starts at zero")
	}
}

func TestShippedHealNumbersAreUsable(t *testing.T) {
	c := M1()
	if c.HealRestorePermille.Value <= 0 || c.HealRestorePermille.Value > engine.PermilleScale {
		t.Errorf("heal restore = %d permille, want 1..1000", c.HealRestorePermille.Value)
	}
	// The conversion must burn something and must leave something: burning all
	// of it would be a suicide button, and burning none would do nothing.
	if c.HealConvertBurnPermille.Value <= 0 || c.HealConvertBurnPermille.Value >= engine.PermilleScale {
		t.Errorf("heal conversion burns %d permille, want 1..999", c.HealConvertBurnPermille.Value)
	}
}

// TestShippedPoisonIsNotCuredByRest is the shipped-content half of the
// "疗伤不清除无关异常" criterion: the fixture proves the rule works, and this
// proves the shipped data actually exercises it.
func TestShippedPoisonIsNotCuredByRest(t *testing.T) {
	c := M1()
	poisons := 0
	for _, a := range c.Afflictions {
		if a.Kind == engine.AfflictionPoison {
			poisons++
			if a.ClearedByRest {
				t.Errorf("%s is a poison that resting cures, which removes the reason to carry "+
					"an antidote", a.ID)
			}
		}
	}
	if poisons == 0 {
		t.Fatal("no poison is shipped")
	}
}

func findRealmDefinition(c engine.Catalogue, realm engine.Realm) (engine.RealmDefinition, bool) {
	for _, def := range c.Realms {
		if def.ID == realm {
			return def, true
		}
	}
	return engine.RealmDefinition{}, false
}
