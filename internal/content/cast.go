package content

import (
	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

// m1Talents configures the talents. The design document does not freeze a full
// talent list, so every entry here is a design-note addition with a stated
// effect. Each talent grants a named bonus so the ledger can explain it.
func m1Talents() []engine.TalentDefinition {
	return []engine.TalentDefinition{
		{
			ID: "innate_dao_body", NameZH: "先天道体",
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantMultiplicative, Target: "cultivation_rate",
				Amount: 15 * engine.SCALE / 10,
				Reason: "先天道体独立加成 50%（设计文档第 7.1 节例）",
			}},
			// The design document treats the dao body bonus as independent, so
			// it must not be combined with a second copy of itself.
			Exclusive: []string{"innate_dao_body"},
		},
		{
			ID: "sharp_mind", NameZH: "悟性过人",
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "attributes.comprehension",
				Amount: 3, Reason: "天赋：悟性 +3",
			}},
		},
		{
			ID: "robust_frame", NameZH: "天生神力",
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "attributes.strength",
				Amount: 3, Reason: "天赋：力量 +3",
			}},
		},
		{
			ID: "swift_footed", NameZH: "轻身捷足",
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "attributes.agility",
				Amount: 3, Reason: "天赋：身法 +3",
			}},
		},
		{
			ID: "blessed_fate", NameZH: "福缘深厚",
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "attributes.fortune",
				Amount: 3, Reason: "天赋：福缘 +3",
			}},
		},
	}
}

// m1Items configures the M1 item set.
//
// Prices are manuscript figures where the design document names them
// (聚气丹 20, 筑基丹 500) and design-note additions elsewhere. The recycle
// price is the manuscript's candidate 50% of buy price, written out as its own
// number rather than computed, so the ratio is auditable.
func m1Items() []engine.ItemDefinition {
	return []engine.ItemDefinition{
		{
			// The two named pills are manuscript figures.
			ID: "qi_gathering_pill", NameZH: "聚气丹", Category: engine.ItemConsumable,
			BuyPrice: manuscript(20), SellPrice: manuscript(10), StackLimit: 99,
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "xp",
				Amount: 50 * engine.SCALE,
				Reason: "聚气丹：服下立得五十年修为（设计注：数值待平衡）",
			}},
			Description: "聚敛灵气的小丹，修行者常备。",
		},
		{
			ID: "foundation_pill", NameZH: "筑基丹", Category: engine.ItemConsumable,
			BuyPrice: manuscript(500), SellPrice: manuscript(250), StackLimit: 9,
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "breakthrough.foundation_bonus",
				Amount: 10,
				Reason: "筑基丹：筑基启动率 +10（设计注：数值待平衡）",
			}},
			Description: "筑基所需的主药，价高难得。",
		},
		{
			ID: "healing_pill", NameZH: "疗伤丹", Category: engine.ItemConsumable,
			BuyPrice:   designNote(30, "设计文档未给疗伤丹定价，按同级丹药比例给出"),
			SellPrice:  designNote(15, "回收价为买价 50%，与设计文档候选一致"),
			StackLimit: 99,
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "hp.current",
				Amount: 40 * engine.SCALE,
				Reason: "疗伤丹：回复气血（设计注：数值待平衡）",
			}},
			Description: "止血生肌的常备丹。",
		},
		{
			ID: "spirit_herb", NameZH: "普通草药", Category: engine.ItemMaterial,
			BuyPrice:    designNote(8, "采药委托的基础产物，定价为设计注"),
			SellPrice:   designNote(4, "回收价为买价 50%"),
			StackLimit:  99,
			Description: "寻常山野可见的药材，坊市多有收购。",
		},
		{
			ID: "rare_herb", NameZH: "稀有草药", Category: engine.ItemMaterial,
			BuyPrice:    designNote(40, "深入采药岔路的产出，定价为设计注"),
			SellPrice:   designNote(20, "回收价为买价 50%"),
			StackLimit:  99,
			Description: "生于险处的药材，价高而难得。",
		},
		{
			ID: "basic_sword", NameZH: "青钢剑", Category: engine.ItemEquipment,
			BuyPrice:   designNote(60, "起手武器定价为设计注"),
			SellPrice:  designNote(30, "回收价为买价 50%"),
			StackLimit: 1,
			EquipSlot:  engine.SlotWeapon,
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "attack",
				Amount: 5 * engine.SCALE,
				Reason: "青钢剑：攻击加成",
			}},
			Description: "寻常炼气修士的佩剑。",
		},
		{
			ID: "cloth_robe", NameZH: "粗布道袍", Category: engine.ItemEquipment,
			BuyPrice:   designNote(40, "起手护具定价为设计注"),
			SellPrice:  designNote(20, "回收价为买价 50%"),
			StackLimit: 1,
			EquipSlot:  engine.SlotArmor,
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "defense",
				Amount: 3 * engine.SCALE,
				Reason: "粗布道袍：防御加成",
			}},
			Description: "洗得发白的旧袍，聊胜于无。",
		},
		{
			ID: "seal_talisman", NameZH: "护身符", Category: engine.ItemEquipment,
			BuyPrice:   designNote(80, "护符定价为设计注"),
			SellPrice:  designNote(40, "回收价为买价 50%"),
			StackLimit: 1,
			EquipSlot:  engine.SlotTalisman,
			Effects: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "defense",
				Amount: 4 * engine.SCALE,
				Reason: "护身符：防御加成",
			}},
			Description: "画着镇邪符文的黄纸，贴身携带可挡一次晦气。",
		},
		{
			ID: "broken_talisman_shard", NameZH: "残符", Category: engine.ItemQuest,
			BuyPrice:    designNote(0, "任务物品不可购买"),
			SellPrice:   designNote(0, "任务物品不可回收"),
			StackLimit:  9,
			Description: "不知谁遗落的半张符纸，也许有人认得。",
		},
	}
}

// m1Techniques configures the cultivation techniques.
//
// Grade multipliers are manuscript figures (黄1.0 玄1.3 地1.7 天2.2 仙3.0).
func m1Techniques() []engine.TechniqueDefinition {
	return []engine.TechniqueDefinition{
		{
			ID: "yellow_breath", NameZH: "吐纳诀", Grade: engine.GradeYellow,
			GradeMultiplier: manuscript(10 * engine.SCALE / 10),
			IsPrimary:       true,
			SkillIDs:        []string{"basic_strike"},
		},
		{
			ID: "mystic_breath", NameZH: "玄元诀", Grade: engine.GradeMystic,
			GradeMultiplier: manuscript(13 * engine.SCALE / 10),
			IsPrimary:       true,
			SkillIDs:        []string{"basic_strike", "spirit_bolt"},
		},
		{
			ID: "earth_channel", NameZH: "地脉引", Grade: engine.GradeEarth,
			GradeMultiplier: manuscript(17 * engine.SCALE / 10),
			IsPrimary:       true,
			SkillIDs:        []string{"basic_strike", "spirit_bolt"},
		},
		{
			ID: "heaven_scripture", NameZH: "天章经", Grade: engine.GradeHeaven,
			GradeMultiplier: manuscript(22 * engine.SCALE / 10),
			IsPrimary:       true,
			SkillIDs:        []string{"basic_strike", "spirit_bolt"},
		},
		{
			ID: "immortal_canon", NameZH: "仙典", Grade: engine.GradeImmortal,
			GradeMultiplier: manuscript(30 * engine.SCALE / 10),
			IsPrimary:       true,
			SkillIDs:        []string{"basic_strike", "spirit_bolt"},
		},
	}
}

// m1Skills configures the combat skills.
//
// Power values are design-note additions: the manuscript supplies the damage
// formula and the elemental coefficients but not per-skill power, so these are
// explicitly marked rather than presented as authored numbers.
func m1Skills() []engine.SkillDefinition {
	return []engine.SkillDefinition{
		{
			ID: "basic_strike", NameZH: "劈击", Element: "",
			Power: designNote(10*engine.SCALE,
				"招式威力由设计注给出；原稿只定伤害公式与元素系数"),
			MP: 0, Cooldown: 0, IsUltimate: false,
		},
		{
			ID: "spirit_bolt", NameZH: "灵光弹", Element: "wood",
			Power: designNote(18*engine.SCALE,
				"招式威力由设计注给出；原稿只定伤害公式与元素系数"),
			MP: 3, Cooldown: 1, IsUltimate: false,
		},
		{
			ID: "ultimate_seal", NameZH: "镇灵印", Element: "",
			Power: designNote(40*engine.SCALE,
				"绝技威力由设计注给出；需两轮准备且可被打断，见设计文档第 7.3 节"),
			MP: 8, Cooldown: 3, IsUltimate: true,
		},
	}
}

// m1NPCs configures the M1 cast.
//
// Gu Qingxuan (顾清玄) and Yunqi (云栖) come from the original manuscript. The
// sect steward is an original addition and is marked as such, because the
// design document requires original material to be distinguishable. Every NPC
// is declared adult: M1 ships no romance content, and the flag is asserted now
// so that later content cannot bypass it.
func m1NPCs() []engine.NPCDefinition {
	return []engine.NPCDefinition{
		{
			ID: "gu_qingxuan", NameZH: "顾清玄",
			FromManuscript: true, Adult: true,
			InitialRealm: engine.RealmQiRefining, InitialTier: engine.TierPerfection,
			LifespanYears: 100, InitialAgeYears: 42,
			Faction: "散修", Personality: "谨慎寡言",
			HomeLocation: "cave_dwelling",
			DialogueIDs:  []string{"dlg_gu_meet", "dlg_gu_hint"},
			Description:  "同住一山的散修，话不多，偶尔提点后辈。",
		},
		{
			ID: "yunqi", NameZH: "云栖",
			FromManuscript: true, Adult: true,
			InitialRealm: engine.RealmFoundation, InitialTier: engine.TierEarly,
			LifespanYears: 200, InitialAgeYears: 63,
			Faction: "青云宗", Personality: "温和持重",
			HomeLocation: "qingyun_sect",
			DialogueIDs:  []string{"dlg_yunqi_meet"},
			Description:  "青云宗执事，负责招收新血。",
		},
		{
			// Original addition. The design document permits one clearly-marked
			// original client, and requires the marking.
			ID: "steward_wen", NameZH: "温管事",
			FromManuscript: false, NewSetting: true, Adult: true,
			InitialRealm: engine.RealmQiRefining, InitialTier: engine.TierLate,
			LifespanYears: 100, InitialAgeYears: 47,
			Faction: "坊市", Personality: "精明市侩",
			HomeLocation: "market",
			DialogueIDs:  []string{"dlg_wen_meet"},
			Description:  "坊市管事，什么生意都肯谈，只是价钱从不含糊。",
		},
	}
}

// m1Locations configures the travel graph.
//
// Travel between M1 nodes is zero-month and safe, so the graph must be
// symmetric: an edge without a return would strand the player somewhere
// shuttling back and forth cannot farm. The validator enforces that symmetry.
func m1Locations() []engine.LocationDefinition {
	return []engine.LocationDefinition{
		{
			ID: "cave_dwelling", NameZH: "洞府",
			Environment: engine.EnvNormal, Safe: true,
			Neighbors:   []string{"market", "herb_woods"},
			Description: "栖身的山洞，陈设简陋，胜在清静。",
		},
		{
			ID: "market", NameZH: "坊市",
			Environment: engine.EnvNormal, Safe: true,
			Neighbors:   []string{"cave_dwelling", "qingyun_sect"},
			Description: "人来人往的集散地，丹药器物俱可寻。",
		},
		{
			ID: "qingyun_sect", NameZH: "青云宗",
			Environment: engine.EnvRich, Safe: true,
			Neighbors:   []string{"market", "herb_woods"},
			Description: "依山而建的宗门，灵气比外间浓郁许多。",
		},
		{
			// The herb woods are where the gathering quest's fork happens, but
			// the node itself is safe; the risk is inside the event, not in
			// standing here.
			ID: "herb_woods", NameZH: "采药山林",
			Environment: engine.EnvNormal, Safe: false,
			Neighbors:   []string{"cave_dwelling", "qingyun_sect"},
			Description: "山深林密，草药多，野兽也多。",
		},
	}
}

// m1Dialogues configures the short exchanges M1 ships. Long arcs and romance
// are explicitly out of scope for M1.
func m1Dialogues() []engine.DialogueDefinition {
	return []engine.DialogueDefinition{
		{
			ID: "dlg_gu_meet", NPCID: "gu_qingxuan",
			TextZH: "「山里清静，修行要紧。若无别事，莫要扰我。」",
		},
		{
			ID: "dlg_gu_hint", NPCID: "gu_qingxuan",
			TextZH: "「往后山去，药材多，风险也多。量力而行。」",
		},
		{
			ID: "dlg_yunqi_meet", NPCID: "yunqi",
			TextZH: "「青云宗不看出身，只看心性。你若有意，按告示去做便是。」",
		},
		{
			ID: "dlg_wen_meet", NPCID: "steward_wen",
			TextZH: "「买卖好说，明码标价。想赊账？坊市没有这条规矩。」",
		},
	}
}
