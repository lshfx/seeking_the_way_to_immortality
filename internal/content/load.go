package content

import (
	"fmt"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

// LoadM1 returns the validated M1 catalogue.
//
// It validates before returning, so a caller cannot obtain a catalogue that
// has not passed the checks. That matters because TASK-04's acceptance
// criteria are about validation actually running: a loader that skipped the
// check would make the criteria unverifiable.
//
// The returned catalogue is a fresh copy, so a caller that mutates it cannot
// affect another caller.
func LoadM1() (engine.Catalogue, error) {
	c := M1()
	if report := engine.ValidateCatalogue(&c); !report.OK() {
		return engine.Catalogue{}, fmt.Errorf("M1 catalogue is invalid: %w", report.Error())
	}
	return c, nil
}

// MustLoadM1 returns the validated M1 catalogue or panics.
//
// It is for tests and for the process entry point, where an invalid shipped
// catalogue is a build defect rather than a runtime condition. Code that can
// recover should call LoadM1 instead.
func MustLoadM1() engine.Catalogue {
	c, err := LoadM1()
	if err != nil {
		panic(err)
	}
	return c
}

// Index provides id lookups over a validated catalogue.
//
// Building an index is separate from validation because lookups are a hot path
// in later tasks while validation runs once. The index is derived, never
// hand-maintained, so it cannot drift from the catalogue.
type Index struct {
	Realms        map[engine.Realm]engine.RealmDefinition
	RealmByOrder  []engine.RealmDefinition
	Origins       map[engine.Origin]engine.OriginDefinition
	SpiritRoots   map[engine.SpiritRoot]engine.SpiritRootDefinition
	Constitutions map[string]engine.ConstitutionDefinition
	Talents       map[string]engine.TalentDefinition
	Items         map[string]engine.ItemDefinition
	Techniques    map[string]engine.TechniqueDefinition
	Skills        map[string]engine.SkillDefinition
	NPCs          map[string]engine.NPCDefinition
	Locations     map[string]engine.LocationDefinition
	Dialogues     map[string]engine.DialogueDefinition
	Quests        map[string]engine.QuestDefinition
	Sects         map[string]engine.SectDefinition
	Events        map[string]engine.EventDefinition
	// Breakthroughs is keyed by path/from-realm/from-tier.
	Breakthroughs map[BreakthroughKey]engine.BreakthroughDefinition

	// Version is the catalogue version the index was built from.
	Version int
}

// BreakthroughKey identifies one breakthrough row.
type BreakthroughKey struct {
	Path      engine.Path
	FromRealm engine.Realm
	FromTier  engine.RealmTier
}

// NewIndex builds lookup maps over a catalogue. It does not validate; callers
// should pass a catalogue that has already passed ValidateCatalogue.
func NewIndex(c engine.Catalogue) *Index {
	idx := &Index{
		Realms:        make(map[engine.Realm]engine.RealmDefinition, len(c.Realms)),
		RealmByOrder:  make([]engine.RealmDefinition, len(c.Realms)),
		Origins:       make(map[engine.Origin]engine.OriginDefinition, len(c.Origins)),
		SpiritRoots:   make(map[engine.SpiritRoot]engine.SpiritRootDefinition, len(c.SpiritRoots)),
		Constitutions: make(map[string]engine.ConstitutionDefinition, len(c.Constitutions)),
		Talents:       make(map[string]engine.TalentDefinition, len(c.Talents)),
		Items:         make(map[string]engine.ItemDefinition, len(c.Items)),
		Techniques:    make(map[string]engine.TechniqueDefinition, len(c.Techniques)),
		Skills:        make(map[string]engine.SkillDefinition, len(c.Skills)),
		NPCs:          make(map[string]engine.NPCDefinition, len(c.NPCs)),
		Locations:     make(map[string]engine.LocationDefinition, len(c.Locations)),
		Dialogues:     make(map[string]engine.DialogueDefinition, len(c.Dialogues)),
		Quests:        make(map[string]engine.QuestDefinition, len(c.Quests)),
		Sects:         make(map[string]engine.SectDefinition, len(c.Sects)),
		Events:        make(map[string]engine.EventDefinition, len(c.Events)),
		Breakthroughs: make(map[BreakthroughKey]engine.BreakthroughDefinition, len(c.Breakthroughs)),
		Version:       c.Version,
	}

	for _, r := range c.Realms {
		idx.Realms[r.ID] = r
		// Order is 0-based and dense; place defensively so an out-of-range
		// value cannot panic before validation has had its say.
		if r.Order >= 0 && r.Order < len(idx.RealmByOrder) {
			idx.RealmByOrder[r.Order] = r
		}
	}
	for _, o := range c.Origins {
		idx.Origins[o.ID] = o
	}
	for _, s := range c.SpiritRoots {
		idx.SpiritRoots[s.ID] = s
	}
	for _, cc := range c.Constitutions {
		idx.Constitutions[cc.ID] = cc
	}
	for _, t := range c.Talents {
		idx.Talents[t.ID] = t
	}
	for _, it := range c.Items {
		idx.Items[it.ID] = it
	}
	for _, t := range c.Techniques {
		idx.Techniques[t.ID] = t
	}
	for _, s := range c.Skills {
		idx.Skills[s.ID] = s
	}
	for _, n := range c.NPCs {
		idx.NPCs[n.ID] = n
	}
	for _, l := range c.Locations {
		idx.Locations[l.ID] = l
	}
	for _, d := range c.Dialogues {
		idx.Dialogues[d.ID] = d
	}
	for _, q := range c.Quests {
		idx.Quests[q.ID] = q
	}
	for _, s := range c.Sects {
		idx.Sects[s.ID] = s
	}
	for _, e := range c.Events {
		idx.Events[e.ID] = e
	}
	for _, b := range c.Breakthroughs {
		idx.Breakthroughs[BreakthroughKey{b.Path, b.FromRealm, b.FromTier}] = b
	}

	return idx
}
