package content

import (
	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
)

// m1Quests configures the M1 commissions.
//
// The safe chore is the manuscript's income baseline: one game month, 30
// spirit stones net, no random bonus. The design document's arithmetic
// (ceil(400/30)=14 runs to afford a 500-stone foundation pill from a 100-stone
// start) depends on that number, so it is marked as a manuscript figure rather
// than adjusted silently.
func m1Quests() []engine.QuestDefinition {
	return []engine.QuestDefinition{
		{
			ID: "quest_safe_chore", NameZH: "坊市杂务", Kind: engine.QuestChore,
			MonthCost: manuscript(1),
			Rewards: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "resources.spirit_stones",
				Amount: 30, Reason: "安全杂务净收入 30 灵石（原稿算例）",
			}},
			Repeatable: true, CooldownMonths: 0,
			GiverNPCID:  "steward_wen",
			Description: "替坊市跑腿搬货，不涉风险，月结三十灵石。",
		},
		{
			ID: "quest_gather_herbs", NameZH: "采药委托", Kind: engine.QuestGather,
			MonthCost: manuscript(1),
			Rewards: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "resources.spirit_stones",
				Amount: 15, Reason: "采药基础报酬（设计注）",
			}},
			RequiredItems: []engine.ItemStack{
				{ItemID: "spirit_herb", Quantity: 3},
			},
			Repeatable:  true,
			GiverNPCID:  "steward_wen",
			Description: "后山采三株草药来交，报酬不高，胜在稳定。",
		},
		{
			ID: "quest_sect_patrol", NameZH: "宗内巡逻", Kind: engine.QuestPatrol,
			MonthCost: manuscript(1),
			Rewards: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "resources.contribution",
				Amount: 20, Reason: "宗门巡逻贡献（设计文档候选 20~40）",
			}},
			Repeatable: true, RequiresSect: true,
			GiverNPCID:  "yunqi",
			Description: "在宗门内外巡视一月，记功一次。",
		},
		{
			ID: "quest_spar", NameZH: "同门切磋", Kind: engine.QuestSpar,
			MonthCost: manuscript(1),
			Rewards: []engine.GrantEffect{{
				Kind: engine.GrantAdditive, Target: "resources.reputation",
				Amount: 5, Reason: "切磋胜出声望（设计注）",
			}},
			Repeatable: true, RequiresSect: true,
			GiverNPCID:  "yunqi",
			Description: "与同门比试一场，点到为止。",
		},
	}
}

// m1Sects configures the joinable sect.
//
// The monthly income is the design document's candidate range of 20–40; 20 is
// taken as the floor so the first balance pass is the conservative one.
func m1Sects() []engine.SectDefinition {
	return []engine.SectDefinition{
		{
			ID: "qingyun_sect", NameZH: "青云宗",
			MonthlyIncome: designNote(20, "宗门月任务净收入候选区间 20~40，取下限作首轮平衡"),
			EntryQuestID:  "quest_sect_patrol",
			Description:   "山中大宗，不问出身，只论心性与韧性。",
		},
	}
}

// m1Events configures the twelve M1 event nodes.
//
// The design document is explicit that these are twelve *different*
// executable choice nodes and not the same text renamed twelve times, so each
// carries a distinct Purpose and a genuinely different option set. Every risky
// option previews its risk with an honest percentage, which the validator
// enforces.
//
// The tutorial node deliberately grants nothing farmable: the validator
// rejects a tutorial choice that grants a resource, so the opening cannot be
// replayed for income.
func m1Events() []engine.EventDefinition {
	return []engine.EventDefinition{
		// EVT-001 — the opening. Sets a near-term goal; no repeatable reward.
		{
			ID: "EVT-001", NameZH: "洞府开局", Scene: "cave_dwelling",
			Purpose:        "开局定调：让玩家选择先修炼还是先了解收入路径，教程不发可重复奖励",
			Priority:       designNote(100, "开局节点优先级最高"),
			Weight:         designNote(0, "开局由脚本必发，不参与加权抽取"),
			MaxOccurrences: 1,
			IsTutorial:     true,
			Eligibility: []engine.Precondition{{
				Kind: engine.CondWorldMonth, Key: "world_month",
				Op: engine.OpLE, Value: 0,
			}},
			Choices: []engine.EventChoice{
				{
					ID: "cultivate_first", TextZH: "先静心吐纳，熟悉修炼",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.tutorial_cultivate",
						Amount: 1, Reason: "教程：选择先修炼",
					}},
				},
				{
					ID: "learn_income", TextZH: "先出门看看，摸清灵石来路",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.tutorial_income",
						Amount: 1, Reason: "教程：选择先了解收入路径",
					}},
				},
			},
		},

		// EVT-002 — the market noticeboard.
		{
			ID: "EVT-002", NameZH: "坊市委托告示", Scene: "market",
			Purpose:        "给出三条收入路径：稳妥杂务、有风险的采药、暂不接取",
			Priority:       designNote(60, "常规节点优先级"),
			Weight:         designNote(30, "常规事件权重"),
			MaxOccurrences: 0, // repeatable
			Choices: []engine.EventChoice{
				{
					ID: "take_chore", TextZH: "接下安全杂务",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.has_chore_notice",
						Amount: 1, Reason: "接下杂务告示",
					}},
				},
				{
					ID: "ask_herbs", TextZH: "问问采药的委托",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.has_herb_notice",
						Amount: 1, Reason: "询问采药委托",
					}},
				},
				{
					ID: "decline", TextZH: "暂不接取，先看看",
					Effects: nil,
				},
			},
		},

		// EVT-003 — the herb fork. The risky branch states its risk honestly.
		{
			ID: "EVT-003", NameZH: "采药岔路", Scene: "herb_woods",
			Purpose:  "在「取普通草药」与「冒已提示风险深入」之间做取舍，风险事前给出",
			Priority: designNote(55, "常规节点优先级"),
			Weight:   designNote(25, "常规事件权重"),
			Choices: []engine.EventChoice{
				{
					ID: "take_common", TextZH: "就近采些寻常草药，见好就收",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "items.spirit_herb",
						Amount: 2, Reason: "岔路：采得普通草药",
					}},
				},
				{
					ID: "go_deeper", TextZH: "往里走走，听说深处有好药",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "items.rare_herb",
						Amount: 1, Reason: "岔路：深入采得稀有草药",
					}},
					// The risk is previewed with a concrete probability, which
					// is what makes this choice honest rather than a trap.
					Risks: []engine.RiskSpec{{
						Kind:           engine.RiskInjury,
						ProbabilityPct: designNote(30, "设计注：深入受伤概率 30%"),
						Magnitude:      designNote(20, "设计注：受伤规模 20 点气血"),
						PreviewText:    "深处有野兽出没，可能受伤（约三成）",
					}},
				},
			},
		},

		// EVT-004 — quest delivery. Claim or keep the materials.
		{
			ID: "EVT-004", NameZH: "委托交付", Scene: "market",
			Purpose:  "已完成条件时选择领报酬或留材料放弃任务，两条路都成立",
			Priority: designNote(50, "常规节点优先级"),
			Weight:   designNote(20, "常规事件权重"),
			Choices: []engine.EventChoice{
				{
					ID: "claim", TextZH: "交付草药，领取报酬",
					Costs: []engine.EventCost{{
						Kind: engine.CostItem, ItemID: "spirit_herb",
						Amount: 3,
					}},
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "resources.spirit_stones",
						Amount: 15, Reason: "交付委托报酬",
					}},
					Requires: []engine.Precondition{{
						Kind: engine.CondHasItem, Key: "spirit_herb",
						Op: engine.OpGE, Value: 3,
					}},
				},
				{
					ID: "keep_materials", TextZH: "材料另有用处，放弃这次委托",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.kept_materials",
						Amount: 1, Reason: "放弃委托保留材料",
					}},
				},
			},
		},

		// EVT-005 — the sect recruitment. Both options remain viable.
		{
			ID: "EVT-005", NameZH: "青云宗招募", Scene: "qingyun_sect",
			Purpose:  "明确入宗或继续散修，两条路都能成长，不做成单行道",
			Priority: designNote(70, "关键节点优先级较高"),
			Weight:   designNote(20, "关键事件权重"),
			Choices: []engine.EventChoice{
				{
					ID: "join", TextZH: "愿入青云宗，请执事引荐",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.sect_invited",
						Amount: 1, Reason: "接受招募",
					}},
				},
				{
					ID: "stay_rogue", TextZH: "自在惯了，还是做散修",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.remained_rogue",
						Amount: 1, Reason: "选择散修路线",
					}},
				},
			},
		},

		// EVT-006 — sect patrol dispute. Reputation versus contribution.
		{
			ID: "EVT-006", NameZH: "宗门巡逻", Scene: "qingyun_sect",
			Purpose:  "调解纠纷与回报执事的代价和声望收益不同，体现两种处事风格",
			Priority: designNote(55, "常规节点优先级"),
			Weight:   designNote(20, "常规事件权重"),
			Choices: []engine.EventChoice{
				{
					ID: "mediate", TextZH: "上前调解，各让一步",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "resources.reputation",
						Amount: 8, Reason: "调解纠纷得声望",
					}},
					Risks: []engine.RiskSpec{{
						Kind:           engine.RiskReputation,
						ProbabilityPct: designNote(25, "设计注：调解失败概率 25%"),
						Magnitude:      designNote(5, "设计注：失败扣 5 点声望"),
						PreviewText:    "双方未必肯听，谈崩了会落埋怨（约两成半）",
					}},
				},
				{
					ID: "report", TextZH: "不去趟浑水，照实回报执事",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "resources.contribution",
						Amount: 10, Reason: "如实回报得贡献",
					}},
				},
			},
		},

		// EVT-007 — asking about medicine. Insufficient funds never borrows.
		{
			ID: "EVT-007", NameZH: "坊市问药", Scene: "market",
			Purpose:  "购买、询问材料来源或离开；余额不足只拒绝，绝不自动借贷",
			Priority: designNote(50, "常规节点优先级"),
			Weight:   designNote(20, "常规事件权重"),
			Choices: []engine.EventChoice{
				{
					ID: "buy_pill", TextZH: "买一枚聚气丹",
					Costs: []engine.EventCost{{
						Kind: engine.CostResource, Resource: engine.ResSpiritStones,
						Amount: 20,
					}},
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "items.qi_gathering_pill",
						Amount: 1, Reason: "购得聚气丹",
					}},
					Requires: []engine.Precondition{{
						Kind: engine.CondFunds, Key: "spirit_stones",
						Op: engine.OpGE, Value: 20,
					}},
				},
				{
					ID: "ask_source", TextZH: "问问这药材是从哪儿来的",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.knows_herb_source",
						Amount: 1, Reason: "得知采药来源",
					}},
				},
				{
					ID: "leave", TextZH: "囊中羞涩，先走一步",
					Effects: nil,
				},
			},
		},

		// EVT-008 — a friendly duel. Never forced.
		{
			ID: "EVT-008", NameZH: "同门切磋", Scene: "qingyun_sect",
			Purpose:  "接受非致命切磋或拒绝，不强制开战",
			Priority: designNote(50, "常规节点优先级"),
			Weight:   designNote(20, "常规事件权重"),
			Choices: []engine.EventChoice{
				{
					ID: "accept", TextZH: "点到为止，请指教",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.spar_started",
						Amount: 1, Reason: "接受切磋",
					}},
				},
				{
					ID: "refuse", TextZH: "今日不便，改日再来",
					Effects: nil,
				},
			},
		},

		// EVT-009 — the beast. Costs are stated before the fight.
		{
			ID: "EVT-009", NameZH: "林中妖兽", Scene: "herb_woods",
			Purpose:  "交战、绕行或撤退，实际成本事前给出，不搞突袭",
			Priority: designNote(80, "危险节点优先级高，寿尽/致命优先"),
			Weight:   designNote(25, "危险事件权重"),
			Choices: []engine.EventChoice{
				{
					ID: "fight", TextZH: "拔剑迎战",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.beast_engaged",
						Amount: 1, Reason: "选择交战妖兽",
					}},
					Risks: []engine.RiskSpec{{
						Kind:           engine.RiskCombat,
						ProbabilityPct: designNote(100, "选择交战则必然开战，概率如实标注为 100%"),
						Magnitude:      designNote(1, "一次战斗遭遇"),
						PreviewText:    "必有一战；妖兽凶悍，量力而行",
					}},
				},
				{
					ID: "detour", TextZH: "绕道而行，多走些路",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.beast_avoided",
						Amount: 1, Reason: "绕行避战",
					}},
				},
				{
					ID: "retreat", TextZH: "转身退走，性命要紧",
					Effects: nil,
				},
			},
		},

		// EVT-010 — a friend asks for help. Advances the "protect what you
		// love" early goal either way.
		{
			ID: "EVT-010", NameZH: "友人求助", Scene: "cave_dwelling",
			Purpose:  "交付资源或婉拒，两者都推动「守护所爱」早期目标",
			Priority: designNote(60, "关系节点优先级"),
			Weight:   designNote(15, "关系事件权重"),
			Choices: []engine.EventChoice{
				{
					ID: "help", TextZH: "倾囊相助",
					Costs: []engine.EventCost{{
						Kind: engine.CostResource, Resource: engine.ResSpiritStones,
						Amount: 30,
					}},
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "relations.gu_qingxuan",
						Amount: 10, Reason: "相助结下善缘",
					}},
					Requires: []engine.Precondition{{
						Kind: engine.CondFunds, Key: "spirit_stones",
						Op: engine.OpGE, Value: 30,
					}},
				},
				{
					ID: "politely_decline", TextZH: "如实说明难处，婉言相拒",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.declined_friend",
						Amount: 1, Reason: "婉拒友人求助",
					}},
				},
			},
		},

		// EVT-011 — foundation preparation. Previews the full cost and lets the
		// player cancel.
		{
			ID: "EVT-011", NameZH: "筑基准备", Scene: "cave_dwelling",
			Purpose:  "预览完整材料与后果，开始或取消；取消为 0 消耗",
			Priority: designNote(90, "突破准备节点优先级很高"),
			Weight:   designNote(0, "由突破流程触发，不参与加权抽取"),
			Choices: []engine.EventChoice{
				{
					ID: "begin", TextZH: "备齐材料，开始筑基",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "flags.foundation_ready",
						Amount: 1, Reason: "决定尝试筑基",
					}},
					Requires: []engine.Precondition{{
						Kind: engine.CondHasItem, Key: "foundation_pill",
						Op: engine.OpGE, Value: 1,
					}},
				},
				{
					ID: "cancel", TextZH: "再等等，材料尚未备齐",
					Effects: nil,
				},
			},
		},

		// EVT-012 — the inner demon. Different effects with configured basis,
		// not narrated into success on the spot.
		{
			ID: "EVT-012", NameZH: "心魔抉择", Scene: "cave_dwelling",
			Purpose:  "不同选项有配置依据的效果，成功不由临场叙事裁定",
			Priority: designNote(95, "劫难节点优先级很高"),
			Weight:   designNote(0, "由突破流程触发，不参与加权抽取"),
			Choices: []engine.EventChoice{
				{
					ID: "face_it", TextZH: "直面心魔，一念不退",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "breakthrough.demon_resist",
						Amount: 15, Reason: "直面心魔：抗性 +15（设计注）",
					}},
					Risks: []engine.RiskSpec{{
						Kind:           engine.RiskInjury,
						ProbabilityPct: designNote(40, "设计注：直面心魔受伤概率 40%"),
						Magnitude:      designNote(30, "设计注：受伤规模 30 点气血"),
						PreviewText:    "心魔反噬，可能受伤（约四成）",
					}},
				},
				{
					ID: "steady_mind", TextZH: "守心不动，以静制动",
					Effects: []engine.GrantEffect{{
						Kind: engine.GrantAdditive, Target: "breakthrough.demon_steady",
						Amount: 10, Reason: "守心：稳定加成 +10（设计注）",
					}},
				},
			},
		},
	}
}

// m1Breakthroughs configures the breakthrough tables for the human path.
//
// The validator requires a major table for every realm except the last, so
// this list must cover the full chain, not merely the low realms a first
// playthrough reaches. Success formulas follow the manuscript:
//
//	small: clamp(70 + 2*(comprehension-10) + bonus - bottleneck, 5, 95)
//
// which is expressed here as a base plus a per-point comprehension term with
// an explicit baseline.
func m1Breakthroughs() []engine.BreakthroughDefinition {
	// Tier advances within a realm are minor; realm advances are major.
	// The四阶 chain is: early->middle->late->perfection->(next realm)early.
	tierChain := []struct {
		from engine.RealmTier
		to   engine.RealmTier
	}{
		{engine.TierEarly, engine.TierMiddle},
		{engine.TierMiddle, engine.TierLate},
		{engine.TierLate, engine.TierPerfection},
	}

	out := make([]engine.BreakthroughDefinition, 0, len(engine.RealmOrder)*4)

	for idx, realm := range engine.RealmOrder {
		// Three minor advances inside the realm.
		for _, step := range tierChain {
			out = append(out, engine.BreakthroughDefinition{
				Path:      engine.PathHuman,
				FromRealm: realm, FromTier: step.from,
				ToRealm: realm, ToTier: step.to,
				IsMajor: false,
				// Minor advance: the manuscript's small-breakthrough formula.
				BaseSuccessPct:        manuscript(70),
				ComprehensionPerPoint: manuscript(2),
				ComprehensionBaseline: manuscript(10),
				ClampMinPct:           manuscript(5),
				ClampMaxPct:           manuscript(95),
				FailFallbackPct:       manuscript(70),
				MonthCost:             manuscript(1),
			})
		}

		// The major advance to the next realm, except from the final realm.
		if idx < len(engine.RealmOrder)-1 {
			next := engine.RealmOrder[idx+1]
			// The manuscript gives three start rates for the炼气->筑基 step:
			// human 95%, earthly 85%, heavenly 70%. M1 opens only the human
			// path, so 95 is configured and the others are deliberately
			// absent: configuring them would imply support the milestone does
			// not have.
			base := 95
			note := "炼气至筑基人道启动率 95%（原稿）；地道 85%、天道 70% 因 M1 未开放而不配置"
			var baseVal engine.ConfigValue
			if idx == 0 {
				baseVal = manuscript(base)
			} else {
				// Later realm advances are not specified by the manuscript, so
				// they are trial values that must carry their reason.
				baseVal = trial(base,
					"原稿未给该境界的启动率，沿用同量级试算值；"+note)
			}
			out = append(out, engine.BreakthroughDefinition{
				Path:      engine.PathHuman,
				FromRealm: realm, FromTier: engine.TierPerfection,
				ToRealm: next, ToTier: engine.TierEarly,
				IsMajor:               true,
				BaseSuccessPct:        baseVal,
				ComprehensionPerPoint: manuscript(2),
				ComprehensionBaseline: manuscript(10),
				ClampMinPct:           manuscript(5),
				ClampMaxPct:           manuscript(95),
				FailFallbackPct:       manuscript(70),
				MonthCost:             manuscript(1),
				// The trial nodes are the tribulation and inner-demon events.
				TrialNodes: []string{"EVT-011", "EVT-012"},
			})
		}
	}

	return out
}
