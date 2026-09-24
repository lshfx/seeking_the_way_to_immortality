package engine

import "fmt"

// Spawning the named cast into the world.
//
// TASK-04 declared three NPCs in the catalogue, but nothing ever put them into
// World.NPCs, so in play they did not exist: `npc_available` conditions could
// never hold, dialogue was unreachable, and the design's "至少3名具名NPC" was
// only a list in a data file. This file is the missing half.
//
// The cast is spawned once, when the character is confirmed, rather than being
// looked up on demand. That matters for determinism: an NPC's age advances with
// the world month, so an NPC that is only materialised when first spoken to
// would have to be aged retroactively, and the answer would depend on when the
// player happened to look.

// SpawnNPCs copies the catalogue's named NPCs into the world.
//
// It is idempotent: an NPC already present is left exactly as it is. That is
// what stops a retried creation, or a save written before this existed, from
// resetting an age the world has already advanced.
func SpawnNPCs(world *World, cat *Catalogue) error {
	if world == nil {
		return fmt.Errorf("no world to populate")
	}
	if cat == nil {
		return fmt.Errorf("no catalogue to populate the world from")
	}
	if world.NPCs == nil {
		world.NPCs = map[string]NPC{}
	}

	for i := range cat.NPCs {
		def := &cat.NPCs[i]
		if def.ID == "" {
			return fmt.Errorf("catalogue NPC %d has no id", i)
		}
		if _, exists := world.NPCs[def.ID]; exists {
			continue
		}
		if def.InitialAgeYears <= 0 {
			// An NPC with no declared age cannot be spawned safely: ageing it
			// would be inventing a number, and design 14 requires the age to be
			// explicit. Refusing here keeps the failure at the content layer.
			return fmt.Errorf("catalogue NPC %q declares no age", def.ID)
		}

		world.NPCs[def.ID] = NPC{
			ID:            def.ID,
			IsAlive:       true,
			AgeMonths:     int64(def.InitialAgeYears) * 12,
			LifespanYears: def.LifespanYears,
			Realm:         def.InitialRealm,
			Tier:          def.InitialTier,
			Faction:       def.Faction,
			Personality:   def.Personality,
			Location:      def.HomeLocation,
			// Schedule and Goals are TASK-23's; KnownFacts starts empty so that
			// "has this NPC been told" is never true by accident.
			KnownFacts: []string{},
		}
	}

	return nil
}

// NPCByID returns the world's record for one NPC.
func NPCByID(world *World, id string) (NPC, bool) {
	if world == nil {
		return NPC{}, false
	}
	npc, ok := world.NPCs[id]
	return npc, ok
}
