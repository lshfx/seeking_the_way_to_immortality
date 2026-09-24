// Package content ships the versioned, non-executable M1 game data and the
// loader that validates it. Nothing here may execute: no file paths, no
// scripts, no network. A catalogue is Go data, checked by the engine's
// validator before it is used.
//
// The M1 catalogue is deliberately incomplete in one specific way: it
// configures all ten realms (because lifespan and breakthrough tables must be
// complete to validate) but opens only the human path. Configuring a realm is
// not the same as shipping it — see M1Paths below.
package content

import (
	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

// Version is this catalogue's revision. It is carried into every save so that
// a save referencing content that no longer exists can be refused rather than
// silently misread.
const Version = 3

// Provenance shorthand. Writing engine.ProvenanceManuscript on every line
// obscures which numbers are actually unapproved, so the catalogue uses short
// helpers and the validator still sees the full provenance value.
func manuscript(v int) engine.ConfigValue {
	return engine.ConfigValue{Provenance: engine.ProvenanceManuscript, Value: v}
}

// trial marks an extrapolated number. The note is mandatory: the validator
// rejects a trial value without one, precisely so that the reason travels with
// the number.
func trial(v int, note string) engine.ConfigValue {
	return engine.ConfigValue{Provenance: engine.ProvenanceTrial, Value: v, Note: note}
}

// designNote marks a figure introduced by this project's design document
// rather than the original manuscript.
func designNote(v int, note string) engine.ConfigValue {
	return engine.ConfigValue{Provenance: engine.ProvenanceDesignNote, Value: v, Note: note}
}

// M1 builds the frozen M1 catalogue.
//
// It is a function rather than a package-level variable so callers cannot
// mutate the shared catalogue: each call returns a fresh copy, and a test that
// breaks a field to check validation cannot corrupt another test's fixture.
func M1() engine.Catalogue {
	c := engine.Catalogue{Version: Version}
	c.CultivationMoodThreshold = designNote(50, "心境阈值：50；高于阈值加成，等于阈值按普通倍率")
	c.EventBaseChancePermille = designNote(200, "每世界月一次基础奇遇检定：20%（设计 R17）")
	c.Realms = m1Realms()
	c.Origins = m1Origins()
	c.SpiritRoots = m1SpiritRoots()
	c.Constitutions = m1Constitutions()
	c.Talents = m1Talents()
	c.Items = m1Items()
	c.Techniques = m1Techniques()
	c.CultivationModifiers = nil // M1 currently ships no temporary aptitude/rate effects.
	c.Skills = m1Skills()
	c.NPCs = m1NPCs()
	c.Locations = m1Locations()
	c.Dialogues = m1Dialogues()
	c.Quests = m1Quests()
	c.Sects = m1Sects()
	c.Events = m1Events()
	c.Breakthroughs = m1Breakthroughs()
	// Only the human path is open. The earthly and heavenly paths are declared
	// in the engine's enum so content referencing them can be rejected, but
	// listing them here would claim a milestone the project has not built.
	c.M1Paths = []engine.Path{engine.PathHuman}
	return c
}

// m1Realms configures all ten realms.
//
// Lifespan figures come from the original manuscript. The monthly base rates
// are manuscript figures for the lower realms; crystallizing and nascent soul
// are marked as trial extrapolations because the source design supplies them
// as "补" (filled in) rather than drawn from the author's rules. Tier
// thresholds are trial values throughout, and the羽化/登仙 thresholds are the
// ones the design document explicitly calls unapproved.
func m1Realms() []engine.RealmDefinition {
	return []engine.RealmDefinition{
		{
			ID: engine.RealmQiRefining, NameZH: "炼气", Order: 0,
			LifespanYears: manuscript(100),
			MonthBase:     manuscript(10 * engine.SCALE),
			TierThreshold: trial(100*engine.SCALE,
				"每小阶阈值由设计文档给出为试算值，非原稿参数"),
		},
		{
			ID: engine.RealmFoundation, NameZH: "筑基", Order: 1,
			LifespanYears: manuscript(200),
			MonthBase:     manuscript(9 * engine.SCALE),
			TierThreshold: trial(300*engine.SCALE,
				"阈值试算；筑基寿元 200 年来自原稿"),
		},
		{
			// Crystallizing's base rate is one of the two the source design
			// flags as filled-in rather than authored, which is exactly why
			// the validator demands all ten realms rather than trusting a list.
			ID: engine.RealmCrystallizing, NameZH: "结晶", Order: 2,
			LifespanYears: manuscript(300),
			MonthBase: trial(85*engine.SCALE/10,
				"原稿未给月基数，设计文档标为「补」，属试算值"),
			TierThreshold: trial(600*engine.SCALE,
				"阈值试算；结晶是易被漏配的境界之一"),
		},
		{
			ID: engine.RealmGoldenCore, NameZH: "金丹", Order: 3,
			LifespanYears: manuscript(500),
			MonthBase:     manuscript(8 * engine.SCALE),
			TierThreshold: trial(900*engine.SCALE, "阈值试算"),
		},
		{
			// Nascent soul is the second filled-in base rate.
			ID: engine.RealmNascentSoul, NameZH: "具灵", Order: 4,
			LifespanYears: manuscript(800),
			MonthBase: trial(75*engine.SCALE/10,
				"原稿未给月基数，设计文档标为「补」，属试算值"),
			TierThreshold: trial(1200*engine.SCALE,
				"阈值试算；具灵是另一个易被漏配的境界"),
		},
		{
			ID: engine.RealmSoulForming, NameZH: "元婴", Order: 5,
			LifespanYears: manuscript(1200),
			MonthBase:     manuscript(7 * engine.SCALE),
			TierThreshold: trial(1800*engine.SCALE, "阈值试算"),
		},
		{
			ID: engine.RealmSpiritSeizing, NameZH: "化神", Order: 6,
			LifespanYears: manuscript(2000),
			MonthBase:     manuscript(6 * engine.SCALE),
			TierThreshold: trial(3000*engine.SCALE, "阈值试算"),
		},
		{
			ID: engine.RealmEnlightening, NameZH: "悟道", Order: 7,
			LifespanYears: manuscript(5000),
			MonthBase:     manuscript(5 * engine.SCALE),
			TierThreshold: trial(6000*engine.SCALE, "阈值试算"),
		},
		{
			// The design document states plainly that the羽化 threshold exceeds
			// the manuscript's "several thousand" magnitude and is not approved
			// for use as a high-realm release parameter. It is configured so
			// the table is complete, and marked so it cannot pass as settled.
			ID: engine.RealmAscension, NameZH: "羽化", Order: 8,
			LifespanYears: manuscript(10000),
			MonthBase:     manuscript(4 * engine.SCALE),
			TierThreshold: trial(12000*engine.SCALE,
				"超出原稿「数千点」量级，设计文档标明为变更候选，尚未批准"),
		},
		{
			ID: engine.RealmImmortal, NameZH: "登仙", Order: 9,
			LifespanYears: manuscript(50000),
			MonthBase:     manuscript(3 * engine.SCALE),
			TierThreshold: trial(24000*engine.SCALE,
				"超出原稿「数千点」量级，设计文档标明为变更候选，尚未批准"),
		},
	}
}

// m1Origins configures the three starting backgrounds. Only merchant and
// hunter have mechanical effects in the manuscript.
func m1Origins() []engine.OriginDefinition {
	return []engine.OriginDefinition{
		{
			ID: engine.OriginCommoner, NameZH: "平民",
			Description: "家世平常，无额外资财，亦无所长；一切靠自己挣。",
			Effects:     nil,
		},
		{
			ID: engine.OriginMerchant, NameZH: "商贾",
			Description: "出身商户，手头宽裕；额外三百灵石可作启动之资。",
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "resources.spirit_stones",
				Amount: 300, Reason: "商贾出身额外资财",
			}},
		},
		{
			ID: engine.OriginHunter, NameZH: "猎户",
			Description: "山林里长大，体魄结实；气血上限与起始气血各多二十。",
			Effects: []engine.GrantEffect{
				{
					Kind: engine.GrantPermanent, Target: "hp.max",
					Amount: 20, Reason: "猎户出身气血上限加成",
				},
				{
					Kind: engine.GrantAdditive, Target: "hp.current",
					Amount: 20, Reason: "猎户出身起始气血加成",
				},
			},
		},
	}
}

// m1SpiritRoots configures the five grades plus the matching variant.
//
// Multipliers are manuscript figures. The variant bonus is the manuscript's
// "对应系变异另乘 1.2", expressed in SCALE units so it multiplies correctly
// without floating point.
func m1SpiritRoots() []engine.SpiritRootDefinition {
	return []engine.SpiritRootDefinition{
		{
			ID: engine.RootHeavenly, NameZH: "天灵根",
			Multiplier: manuscript(20 * engine.SCALE / 10),
		},
		{
			ID: engine.RootEarthly, NameZH: "地灵根",
			Multiplier: manuscript(16 * engine.SCALE / 10),
		},
		{
			ID: engine.RootTrue, NameZH: "真灵根",
			Multiplier: manuscript(13 * engine.SCALE / 10),
		},
		{
			ID: engine.RootPseudo, NameZH: "伪灵根",
			Multiplier: manuscript(10 * engine.SCALE / 10),
		},
		{
			ID: engine.RootVariant, NameZH: "变异灵根",
			Multiplier: manuscript(18 * engine.SCALE / 10),
		},
		{
			ID: engine.RootVariantX, NameZH: "对应系变异灵根",
			Multiplier:   manuscript(18 * engine.SCALE / 10),
			VariantBonus: manuscript(12 * engine.SCALE / 10),
		},
	}
}

// m1Constitutions configures the body types. M1 ships a minimal set; the
// design document does not freeze a full list, so anything beyond "普通" is a
// design-note addition rather than a manuscript figure.
func m1Constitutions() []engine.ConstitutionDefinition {
	return []engine.ConstitutionDefinition{
		{
			ID: "ordinary", NameZH: "凡体",
			Effects: nil,
		},
	}
}
