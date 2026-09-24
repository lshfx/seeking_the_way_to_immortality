package engine

// The content catalogue is the set of versioned, non-executable game data:
// realms, origins, spirit roots, talents, items, techniques, NPCs, locations,
// quests, events and the M1 breakthrough path. The engine holds the types and
// the validator; internal/content holds the shipped data. Nothing here may
// execute content: no file paths, no scripts, no network.
//
// Every configured number carries a ConfigValue so that original-manuscript
// parameters and extrapolated trial values are never silently mixed, and so
// that unapproved parameters are visible rather than presented as settled.

// Catalogue is one complete, validated content set.
type Catalogue struct {
	Version int `json:"version"`
	// CultivationMoodThreshold is the configured boundary between the neutral
	// mood band and the high-mood bonus. Mood itself is stored as 0..100.
	CultivationMoodThreshold ConfigValue `json:"cultivation_mood_threshold"`
	// EventBaseChancePermille is the base chance, in permille, that a settled
	// world month offers a random event. Design R17 fixes the design intent at
	// 20%; carrying it as a ConfigValue keeps the number auditable and lets a
	// later balance pass move it without editing code.
	EventBaseChancePermille ConfigValue `json:"event_base_chance_permille"`

	Realms        []RealmDefinition        `json:"realms"`
	Origins       []OriginDefinition       `json:"origins"`
	SpiritRoots   []SpiritRootDefinition   `json:"spirit_roots"`
	Constitutions []ConstitutionDefinition `json:"constitutions"`
	Talents       []TalentDefinition       `json:"talents"`
	Items         []ItemDefinition         `json:"items"`
	Techniques    []TechniqueDefinition    `json:"techniques"`
	// Afflictions are the named injuries, poisons, internal wounds and mood
	// disorders a character can carry. An entry in Condition.Effects refers to
	// one by id.
	//
	// TASK-08 shipped an empty CultivationModifiers list for the narrower case
	// of a temporary aptitude or rate adjustment. TASK-12 replaces it rather
	// than adding a second list beside it: two overlapping catalogues would let
	// a content author put an effect in the one the engine does not read.
	Afflictions []AfflictionDefinition `json:"afflictions"`
	// InjuryBands configure what each 伤势区间 costs. Design 6.2 fixes the
	// boundaries; the penalties are balance numbers and live here.
	InjuryBands []InjuryBandDefinition `json:"injury_bands"`
	// KarmaTiers describe how a 业力 total is described to the player. The
	// boundaries are configured because "heavy karma" is a judgement, not a
	// formula.
	KarmaTiers []KarmaTierDefinition `json:"karma_tiers"`
	// HealRestorePermille is the fraction of the health ceiling a month of
	// 养伤 restores, in permille.
	HealRestorePermille ConfigValue `json:"heal_restore_permille"`
	// HealConvertBurnPermille is the fraction of the health ceiling that
	// 气血换灵力 spends, in permille.
	HealConvertBurnPermille ConfigValue `json:"heal_convert_burn_permille"`
	// Skills are the combat skills techniques grant. Damage rules read their
	// numbers, so they belong in the validated catalogue rather than in code.
	Skills    []SkillDefinition    `json:"skills"`
	NPCs      []NPCDefinition      `json:"npcs"`
	Locations []LocationDefinition `json:"locations"`
	Quests    []QuestDefinition    `json:"quests"`
	Events    []EventDefinition    `json:"events"`
	// Breakthroughs holds the realm-advance tables. The M1 human path must be
	// present and complete or validation fails.
	Breakthroughs []BreakthroughDefinition `json:"breakthroughs"`
	// Sects are the joinable organisations.
	Sects []SectDefinition `json:"sects"`
	// EarlyGoals are the two near-term objectives M1 offers. They are declared
	// rather than implied so that "both can be advanced as a rogue cultivator"
	// is checkable: a goal whose only success path runs through a sect is not
	// the goal ADR-001 describes.
	EarlyGoals []EarlyGoalDefinition `json:"early_goals"`
	// Dialogues are the short exchanges NPCs can offer. M1 ships short
	// exchanges only; romance arcs are out of scope.
	Dialogues []DialogueDefinition `json:"dialogues"`

	// M1Paths declares which cultivation paths this catalogue actually opens.
	// Merely configuring a high realm does not open it: a path is playable
	// only if it is listed here, and only the human path may be listed for M1.
	M1Paths []Path `json:"m1_paths"`
}

// RealmDefinition configures one realm. All ten must be present, including
// crystallizing and nascent soul whose base rates are trial extrapolations.
type RealmDefinition struct {
	ID     Realm  `json:"id"`
	NameZH string `json:"name_zh"`
	Order  int    `json:"order"` // 0-based position in RealmOrder

	// LifespanYears is the realm's lifespan ceiling. The manuscript supplies
	// these figures for every realm.
	LifespanYears ConfigValue `json:"lifespan_years"`
	// MonthBase is the monthly cultivation base rate in SCALE sub-units.
	MonthBase ConfigValue `json:"month_base"`
	// TierThreshold is the per-tier cultivation requirement in XP sub-units.
	TierThreshold ConfigValue `json:"tier_threshold"`
}

// OriginDefinition configures a starting background.
type OriginDefinition struct {
	ID          Origin `json:"id"`
	NameZH      string `json:"name_zh"`
	Description string `json:"description"`
	// Effects are the mechanical starting grants. Each is named and applied
	// once at creation.
	Effects []GrantEffect `json:"effects"`
}

// SpiritRootDefinition configures an innate talent grade.
type SpiritRootDefinition struct {
	ID     SpiritRoot `json:"id"`
	NameZH string     `json:"name_zh"`
	// Multiplier is the cultivation multiplier in SCALE sub-units.
	Multiplier ConfigValue `json:"multiplier"`
	// VariantBonus is an extra multiplier applied only to the matching
	// variant root (对应系变异另乘 1.2). Zero when not applicable.
	VariantBonus ConfigValue `json:"variant_bonus"`
}

// ConstitutionDefinition configures a body type (体质).
type ConstitutionDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// Effects are named stat grants.
	Effects []GrantEffect `json:"effects"`
}

// TalentDefinition configures a talent (天赋).
type TalentDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// Effects are named stat grants or formula modifiers.
	Effects []GrantEffect `json:"effects"`
	// Exclusive lists talent ids that cannot be taken alongside this one.
	Exclusive []string `json:"exclusive,omitempty"`
}

// GrantEffect is one named mechanical grant. The Kind decides which field of
// the target it touches, so a grant is data rather than a function.
type GrantEffect struct {
	// Kind selects what is granted.
	Kind GrantKind `json:"kind"`
	// Target names the field, e.g. "hp.max" or "resources.spirit_stones".
	Target string `json:"target"`
	// Amount is the delta in the target's unit.
	Amount int64 `json:"amount"`
	// Group identifies independent bonuses that stack additively with other
	// bonuses in the same group. An empty group uses the default group.
	Group string `json:"group,omitempty"`
	// Reason documents why, and appears in the ledger.
	Reason string `json:"reason"`
}

// GrantKind classifies a grant.
type GrantKind string

// Grant kinds.
const (
	GrantAdditive       GrantKind = "additive"       // 直接加减
	GrantMultiplicative GrantKind = "multiplicative" // 乘数，单位 SCALE
	GrantPermanent      GrantKind = "permanent"      // 永久上限提升
)

// Valid reports whether g is a declared grant kind.
func (g GrantKind) Valid() bool {
	switch g {
	case GrantAdditive, GrantMultiplicative, GrantPermanent:
		return true
	default:
		return false
	}
}

// ItemCategory classifies an item.
type ItemCategory string

// Item categories.
const (
	ItemConsumable ItemCategory = "consumable" // 丹药等消耗品
	ItemMaterial   ItemCategory = "material"   // 材料
	ItemEquipment  ItemCategory = "equipment"  // 装备
	ItemQuest      ItemCategory = "quest"      // 任务物品
	ItemCurrency   ItemCategory = "currency"   // 灵石等（仅作展示）
)

// Valid reports whether c is a declared item category.
func (c ItemCategory) Valid() bool {
	switch c {
	case ItemConsumable, ItemMaterial, ItemEquipment, ItemQuest, ItemCurrency:
		return true
	default:
		return false
	}
}

// ItemDefinition configures one item.
type ItemDefinition struct {
	ID       string       `json:"id"`
	NameZH   string       `json:"name_zh"`
	Category ItemCategory `json:"category"`
	// BuyPrice / SellPrice are in spirit-stone units. SellPrice is a separate
	// configured number, not a computed fraction, so that the configured
	// recycle ratio is visible and auditable.
	BuyPrice  ConfigValue `json:"buy_price"`
	SellPrice ConfigValue `json:"sell_price"`
	// StackLimit caps one stack. Zero means unbounded.
	StackLimit int `json:"stack_limit"`
	// Effects apply when the item is used or equipped.
	Effects []GrantEffect `json:"effects,omitempty"`
	// EquipSlot is set for equipment items only.
	EquipSlot EquipSlot `json:"equip_slot,omitempty"`
	// Description is display text.
	Description string `json:"description,omitempty"`
}

// TechniqueDefinition configures a cultivation technique (功法).
type TechniqueDefinition struct {
	ID     string         `json:"id"`
	NameZH string         `json:"name_zh"`
	Grade  TechniqueGrade `json:"grade"`
	// GradeMultiplier is the grade's contribution to the cultivation formula,
	// in SCALE sub-units.
	GradeMultiplier ConfigValue `json:"grade_multiplier"`
	// IsPrimary marks the main technique. The design document forbids stacking
	// a main-technique grade bonus with another entry meaning the same thing.
	IsPrimary bool `json:"is_primary"`
	// Effects are named passive modifiers granted while the technique is
	// active. GradeMultiplier is still applied only for the primary technique.
	Effects []GrantEffect `json:"effects,omitempty"`
	// SkillIDs are the combat skills this technique grants.
	SkillIDs []string `json:"skill_ids,omitempty"`
}

// SkillDefinition configures one combat skill. It lives in the catalogue
// because damage formulas read its numbers.
type SkillDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// Power is the skill's base power in SCALE sub-units.
	Power ConfigValue `json:"power"`
	// Element is the five-phase element, empty for unaligned skills.
	Element string `json:"element,omitempty"`
	// MP is the stamina cost.
	MP int64 `json:"mp_cost"`
	// Cooldown is in combat rounds.
	Cooldown int `json:"cooldown"`
	// IsUltimate marks a skill that needs a charge-up and can be interrupted.
	IsUltimate bool `json:"is_ultimate"`
}

// NPCDefinition configures one non-player character.
type NPCDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// FromManuscript marks a figure taken from the original author's cast.
	// Original additions must be marked so reviewers can tell them apart.
	FromManuscript bool `json:"from_manuscript"`
	// NewSetting marks a wholly original creation (新增设定).
	NewSetting bool `json:"new_setting"`
	// Adult is a hard requirement for any NPC involved in romance or marriage
	// content. M1 does not ship such content, but the flag is asserted now so
	// later content cannot bypass it.
	Adult         bool      `json:"adult"`
	InitialRealm  Realm     `json:"initial_realm"`
	InitialTier   RealmTier `json:"initial_tier"`
	LifespanYears int       `json:"lifespan_years"`
	// InitialAgeYears is the NPC's age when the world is first populated.
	//
	// Design 14 requires every NPC's age to be explicit. It is not derivable
	// from the realm or the lifespan: two qi-refining cultivators with the same
	// ceiling can be twenty years apart, and a later relationship system must
	// not have to guess which. An NPC whose age is unknown is also exactly the
	// NPC that must not be offered romance content, so the number is a safety
	// field as well as a narrative one.
	InitialAgeYears int `json:"initial_age_years"`
	// Faction, Personality, HomeLocation and DialogueIDs describe how the NPC
	// behaves; the engine stores them so a resume cannot invent a different
	// disposition.
	Faction      string `json:"faction,omitempty"`
	Personality  string `json:"personality,omitempty"`
	HomeLocation string `json:"home_location"`
	// DialogueIDs are the dialogue nodes this NPC can offer.
	DialogueIDs []string `json:"dialogue_ids,omitempty"`
	Description string   `json:"description,omitempty"`
}

// LocationDefinition is a node in the travel graph. M1 uses fixed safe nodes so
// that shuttling back and forth cannot farm anything.
type LocationDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// Environment is the ambient cultivation density here.
	Environment Environment `json:"environment"`
	// Neighbors are directly reachable location ids. Travel is zero-month.
	Neighbors []string `json:"neighbors"`
	// Safe marks a location where no combat can start.
	Safe        bool   `json:"safe"`
	Description string `json:"description,omitempty"`
}

// QuestDefinition configures one commission.
type QuestDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// Kind selects the quest's shape.
	Kind QuestKind `json:"kind"`
	// MonthCost is the world months consumed by running it.
	MonthCost ConfigValue `json:"month_cost"`
	// RequiredItems are consumed on acceptance or completion.
	RequiredItems []ItemStack `json:"required_items,omitempty"`
	// Rewards are granted on a successful claim, exactly once.
	Rewards []GrantEffect `json:"rewards"`
	// Repeatable quests must still guard the reward with RewardsClaimed.
	Repeatable bool `json:"repeatable"`
	// CooldownMonths is the minimum wait before a repeatable quest returns.
	CooldownMonths int `json:"cooldown_months"`
	// RequiresSect gates the quest behind sect membership.
	RequiresSect bool `json:"requires_sect"`
	// GiverNPCID is who offers it.
	GiverNPCID  string `json:"giver_npc_id,omitempty"`
	Description string `json:"description,omitempty"`
}

// QuestKind classifies a commission.
type QuestKind string

// Quest kinds.
const (
	QuestChore    QuestKind = "chore"    // 安全杂务
	QuestGather   QuestKind = "gather"   // 采药
	QuestDelivery QuestKind = "delivery" // 交付
	QuestPatrol   QuestKind = "patrol"   // 巡逻
	QuestSpar     QuestKind = "spar"     // 切磋
)

// Valid reports whether k is a declared quest kind.
func (k QuestKind) Valid() bool {
	switch k {
	case QuestChore, QuestGather, QuestDelivery, QuestPatrol, QuestSpar:
		return true
	default:
		return false
	}
}

// SectDefinition configures a joinable sect.
type SectDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// MonthlyIncome is the net contribution per month from sect duties.
	MonthlyIncome ConfigValue `json:"monthly_income"`
	// EntryQuestID is the quest that must be completed to join.
	EntryQuestID string `json:"entry_quest_id,omitempty"`
	Description  string `json:"description,omitempty"`
}

// BreakthroughDefinition is one realm-advance table row. A breakthrough
// without its table is a validation failure, not a default.
type BreakthroughDefinition struct {
	// Path is the cultivation path this row belongs to. M1 must provide the
	// complete human-path chain.
	Path Path `json:"path"`
	// FromRealm / FromTier identify the starting position. ToRealm/ToTier the
	// destination.
	FromRealm Realm     `json:"from_realm"`
	FromTier  RealmTier `json:"from_tier"`
	ToRealm   Realm     `json:"to_realm"`
	ToTier    RealmTier `json:"to_tier"`

	// IsMajor distinguishes a realm advance from a tier advance.
	IsMajor bool `json:"is_major"`
	// BaseSuccessPct is the base success rate in whole percent, before the
	// comprehension adjustment.
	BaseSuccessPct ConfigValue `json:"base_success_pct"`
	// ComprehensionPerPoint is the percentage added per point of comprehension
	// above the configured baseline.
	ComprehensionPerPoint ConfigValue `json:"comprehension_per_point"`
	ComprehensionBaseline ConfigValue `json:"comprehension_baseline"`
	// ClampMinPct / ClampMaxPct bound the final success rate.
	ClampMinPct ConfigValue `json:"clamp_min_pct"`
	ClampMaxPct ConfigValue `json:"clamp_max_pct"`
	// FailFallbackPct is the XP fraction retained on failure, in whole percent.
	FailFallbackPct ConfigValue `json:"fail_fallback_pct"`

	// Materials are consumed at the start of an attempt. Consumed once even if
	// the attempt spans several months.
	Materials []ItemStack `json:"materials,omitempty"`
	// TrialNodes are the tribulation / inner-demon nodes for a major
	// breakthrough. Each must reference a configured event.
	TrialNodes []string `json:"trial_nodes,omitempty"`
	// MonthCost is always one for an attempt under M1 rules.
	MonthCost ConfigValue `json:"month_cost"`
}

// EventChainNode is one link in an event's follow-up chain.
type EventChainNode struct {
	// NodeID identifies the node within the event.
	NodeID string `json:"node_id"`
	// ChoiceIDs are the choices offered at this node.
	ChoiceIDs []string `json:"choice_ids"`
}

// EventDefinition configures one event node. All twelve M1 event nodes are
// genuine distinct choices, not renamed duplicates.
type EventDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// Scene describes where it happens.
	Scene string `json:"scene"`
	// Purpose documents design intent so a reviewer can check the node is not
	// padding.
	Purpose string `json:"purpose"`
	// TextZH is the node's own display text: one to three sentences, per design
	// 14. It is what the player reads before the choices, and it lives in the
	// catalogue rather than in a template file so that M1 needs no model and no
	// network to say anything at all. Purpose is not a substitute: it is written
	// for a reviewer, this is written for the player.
	TextZH string `json:"text_zh"`

	// Eligibility is the gate list. An empty list means always eligible.
	Eligibility []Precondition `json:"eligibility"`
	// Priority orders competing events; higher wins.
	Priority ConfigValue `json:"priority"`
	// CooldownMonths is the minimum gap between occurrences.
	CooldownMonths int `json:"cooldown_months"`
	// ExpiresAfterMonths drops a queued instance that is not answered in time.
	ExpiresAfterMonths int `json:"expires_after_months"`
	// Weight is the selection weight in a weighted draw.
	Weight ConfigValue `json:"weight"`
	// MaxOccurrences caps lifetime occurrences. Zero means unbounded, but a
	// bounded event must still be safe to re-enter.
	MaxOccurrences int `json:"max_occurrences"`

	// Choices are the options, each with visible cost and effects.
	Choices []EventChoice `json:"choices"`
	// FollowUps chains to later nodes inside the same instance.
	FollowUps []EventChainNode `json:"follow_ups,omitempty"`
	// IsTutorial marks the opening node, which must pay no repeatable reward.
	IsTutorial bool `json:"is_tutorial"`

	// Forced marks an event that fires as soon as its eligibility holds,
	// without waiting for the monthly random draw. Design 13.2 step 5 settles
	// "必发事件" before the base check, and a forced event must be one that
	// genuinely cannot be missed — the opening node, a story gate — rather
	// than a way to give a random event better odds.
	//
	// A forced event is excluded from the weighted draw: allowing it to be
	// drawn as well would let it occupy the month and then fire again on the
	// next one. Validation rejects a forced event whose weight is non-zero.
	Forced bool `json:"forced,omitempty"`
}

// EventChoice is one option the player may pick.
type EventChoice struct {
	ID     string `json:"id"`
	TextZH string `json:"text_zh"`

	// Costs are visible before the choice is taken, as the design document
	// requires ("选择的成本必须看得见").
	Costs []EventCost `json:"costs"`
	// Effects apply on selection.
	Effects []GrantEffect `json:"effects"`
	// Risks names each warned risk. A risky choice that shows no risk in the
	// preview is a validation failure.
	Risks []RiskSpec `json:"risks,omitempty"`
	// Requires is an optional condition gate on this choice alone.
	Requires []Precondition `json:"requires,omitempty"`
	// NextNodeID continues the chain, empty to resolve the instance.
	NextNodeID string `json:"next_node_id,omitempty"`
}

// EventCost is one cost a choice imposes.
type EventCost struct {
	// Kind selects what is spent.
	Kind CostKind `json:"kind"`
	// ItemID / Resource identify the thing spent.
	ItemID   string   `json:"item_id,omitempty"`
	Resource Resource `json:"resource,omitempty"`
	// Amount is the quantity; it must be positive, or validation fails.
	Amount int64 `json:"amount"`
	// Months is an extra world-month cost. The design document requires an
	// extra-month option to be modelled as its own action, so this is almost
	// always zero and a non-zero value is rejected for choices that inherit a
	// parent action.
	Months int `json:"months"`
}

// CostKind classifies an event cost.
type CostKind string

// Cost kinds.
const (
	CostItem     CostKind = "item"
	CostResource CostKind = "resource"
	CostHP       CostKind = "hp"
	CostMP       CostKind = "mp"
	CostMonth    CostKind = "month"
)

// Valid reports whether k is a declared cost kind.
func (k CostKind) Valid() bool {
	switch k {
	case CostItem, CostResource, CostHP, CostMP, CostMonth:
		return true
	default:
		return false
	}
}

// Precondition is a content-level eligibility test. Conditions are declarative
// data, never code, so that a malicious content pack cannot execute anything.
type Precondition struct {
	// Kind selects the test.
	Kind ConditionKind `json:"kind"`
	// Key names what is tested.
	Key string `json:"key"`
	// Op compares.
	Op CompareOp `json:"op"`
	// Value is the right-hand side.
	Value int64 `json:"value"`
	// TextValue is the right-hand side for string comparisons.
	TextValue string `json:"text_value,omitempty"`
}

// ConditionKind classifies a precondition.
type ConditionKind string

// Condition kinds.
const (
	CondRealm         ConditionKind = "realm"          // 境界
	CondTier          ConditionKind = "tier"           // 小阶
	CondPath          ConditionKind = "path"           // 道途
	CondOrigin        ConditionKind = "origin"         // 出身
	CondSpiritRoot    ConditionKind = "spirit_root"    // 灵根
	CondHasItem       ConditionKind = "has_item"       // 持有物品
	CondResource      ConditionKind = "resource"       // 资源数量
	CondFunds         ConditionKind = "funds"          // 灵石余额
	CondQuestStatus   ConditionKind = "quest_status"   // 任务状态
	CondSectMember    ConditionKind = "sect_member"    // 是否入宗
	CondFlag          ConditionKind = "flag"           // 世界开关
	CondNPCAvailable  ConditionKind = "npc_available"  // NPC 可交互
	CondWorldMonth    ConditionKind = "world_month"    // 世界月
	CondAgeMonths     ConditionKind = "age_months"     // 年龄月数
	CondLifespanLeft  ConditionKind = "lifespan_left"  // 剩余寿元
	CondConsumedEvent ConditionKind = "consumed_event" // 已消费事件
	CondSkillKnown    ConditionKind = "skill_known"    // 已学技能
	CondHasDebt       ConditionKind = "has_debt"       // 是否有负债
)

// Valid reports whether k is a declared condition kind.
func (k ConditionKind) Valid() bool {
	switch k {
	case CondRealm, CondTier, CondPath, CondOrigin, CondSpiritRoot, CondHasItem,
		CondResource, CondFunds, CondQuestStatus, CondSectMember, CondFlag,
		CondNPCAvailable, CondWorldMonth, CondAgeMonths, CondLifespanLeft,
		CondConsumedEvent, CondSkillKnown, CondHasDebt:
		return true
	default:
		return false
	}
}

// ValueBearing reports whether the precondition compares against a numeric
// Value. Kinds that answer a yes/no question without comparing, and kinds that
// compare text, do not.
func (k ConditionKind) ValueBearing() bool {
	switch k {
	case CondHasItem, CondResource, CondFunds, CondWorldMonth, CondAgeMonths,
		CondLifespanLeft, CondSectMember, CondHasDebt:
		return true
	default:
		return false
	}
}

// UsesTextOperand reports whether the precondition compares against TextValue.
//
// It is separate from ValueBearing because "not numeric" and "compares text" are
// not the same thing: `npc_available` and `consumed_event` answer from a key
// alone and compare nothing, so requiring a text operand from them would make
// them impossible to write correctly.
func (k ConditionKind) UsesTextOperand() bool {
	switch k {
	case CondRealm, CondTier, CondPath, CondOrigin, CondSpiritRoot,
		CondQuestStatus, CondFlag:
		return true
	default:
		return false
	}
}

// NeedsOperator reports whether the kind compares two operands and therefore
// needs a comparison operator.
//
// A condition that answers "is this so?" — is the flag set, is the NPC
// available, has this event been consumed — has nothing to compare, so an
// operator on it would be decoration that a reader could mistake for meaning.
func (k ConditionKind) NeedsOperator() bool {
	switch k {
	case CondFlag, CondNPCAvailable, CondConsumedEvent, CondSkillKnown,
		CondSectMember, CondHasDebt:
		return false
	default:
		return true
	}
}

// NeedsKey reports whether the kind names a subject, such as an item or an NPC.
func (k ConditionKind) NeedsKey() bool {
	switch k {
	case CondFunds, CondWorldMonth, CondAgeMonths, CondLifespanLeft,
		CondSectMember, CondHasDebt:
		return false
	default:
		return true
	}
}

// CompareOp is a comparison operator.
type CompareOp string

// Comparison operators.
const (
	OpEQ CompareOp = "eq"
	OpNE CompareOp = "ne"
	OpLT CompareOp = "lt"
	OpLE CompareOp = "le"
	OpGT CompareOp = "gt"
	OpGE CompareOp = "ge"
)

// Valid reports whether o is a declared operator.
func (o CompareOp) Valid() bool {
	switch o {
	case OpEQ, OpNE, OpLT, OpLE, OpGT, OpGE:
		return true
	default:
		return false
	}
}

// RiskSpec declares a warned risk attached to a choice.
type RiskSpec struct {
	// Kind selects the hazard.
	Kind RiskKind `json:"kind"`
	// ProbabilityPct is the warned chance in whole percent, so the UI can show
	// an honest number instead of an adjective.
	ProbabilityPct ConfigValue `json:"probability_pct"`
	// Magnitude is the consequence size in the hazard's unit.
	Magnitude ConfigValue `json:"magnitude"`
	// PreviewText is what the player is told before choosing.
	PreviewText string `json:"preview_text"`
}

// RiskKind classifies a hazard.
type RiskKind string

// Risk kinds.
const (
	RiskInjury     RiskKind = "injury"     // 受伤
	RiskCombat     RiskKind = "combat"     // 遭遇战斗
	RiskLoss       RiskKind = "loss"       // 损失物品
	RiskReputation RiskKind = "reputation" // 声望下降
	RiskAmbush     RiskKind = "ambush"     // 深入风险
)

// Valid reports whether k is a declared risk kind.
func (k RiskKind) Valid() bool {
	switch k {
	case RiskInjury, RiskCombat, RiskLoss, RiskReputation, RiskAmbush:
		return true
	default:
		return false
	}
}

// EarlyGoalDefinition declares one of M1's two early objectives.
//
// The goal is not a quest with its own state machine; it is a named intent whose
// outcome is recorded by three world flags. That keeps it inside the effect DSL
// TASK-10 already validates, so a branch cannot exist without a choice that sets
// it — which is the property that makes "the goal can succeed, fail or be
// abandoned" a fact rather than a claim.
type EarlyGoalDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// Description is the goal as the player would state it.
	Description string `json:"description"`
	// SuccessFlag, FailureFlag and AbandonFlag are the world flags set by the
	// content when the goal ends that way. All three must be reachable, and all
	// three must be distinct: a goal that cannot fail is not a goal.
	SuccessFlag string `json:"success_flag"`
	FailureFlag string `json:"failure_flag"`
	AbandonFlag string `json:"abandon_flag"`
}

// InjuryBand is the severity band a character's health puts them in. Design 6.2
// fixes the boundaries: 0 enters the consequence rules, (0,33%) is 垂死,
// [33%,66%) is 重伤, [66%,100%) is 轻伤 and 100% is healthy.
//
// The bands are a partition, not a set of overlapping thresholds. Two bands that
// both claim the same value would make the panel disagree with the rules, and
// the disagreement would show up as a character who is 重伤 and 轻伤 at once.
type InjuryBand string

// The five bands, from worst to best.
const (
	// BandDown is exactly zero health: the consequence rules of design 6.2
	// apply, and which one applies comes from the encounter, not from here.
	BandDown InjuryBand = "down" // 气血为 0，进入后果判定
	// BandDying is (0, 33%): alive but failing.
	BandDying InjuryBand = "dying" // 垂死
	// BandHeavy is [33%, 66%): badly hurt, and the band design 6.2 names for
	// the 遁速 penalty.
	BandHeavy InjuryBand = "heavy" // 重伤
	// BandLight is [66%, 100%): hurt but functional.
	BandLight InjuryBand = "light" // 轻伤
	// BandHealthy is 100%: full health.
	BandHealthy InjuryBand = "healthy" // 健康
)

// InjuryBandOrder lists the bands from worst to best. The order is part of the
// contract: tests assert completeness against it, so a new band cannot be added
// without also being configured.
var InjuryBandOrder = []InjuryBand{BandDown, BandDying, BandHeavy, BandLight, BandHealthy}

// Valid reports whether b is a declared band.
func (b InjuryBand) Valid() bool {
	switch b {
	case BandDown, BandDying, BandHeavy, BandLight, BandHealthy:
		return true
	default:
		return false
	}
}

// Wounded reports whether the band is anything worse than healthy.
func (b InjuryBand) Wounded() bool {
	return b != BandHealthy && b != ""
}

// InjuryBandDefinition configures one band's penalties.
type InjuryBandDefinition struct {
	Band   InjuryBand `json:"band"`
	NameZH string     `json:"name_zh"`
	// SpeedPenaltyPermille slows 遁速 by this fraction, in permille. Design 6.2
	// names the 遁速 penalty explicitly; attack and defence are deliberately not
	// configured here, because the design does not give them one and inventing
	// a number would be a balance decision nobody made.
	SpeedPenaltyPermille ConfigValue `json:"speed_penalty_permille"`
	// Note explains the band in the status detail.
	Note string `json:"note,omitempty"`
}

// AfflictionKind classifies an affliction. The four kinds are the ones design
// 6.2 and the task list name, and they differ in what clears them rather than
// only in flavour: rest mends a wound, and does not cure a poison.
type AfflictionKind string

// The four affliction kinds.
const (
	AfflictionWound    AfflictionKind = "wound"    // 伤势
	AfflictionPoison   AfflictionKind = "poison"   // 中毒
	AfflictionInternal AfflictionKind = "internal" // 内伤
	AfflictionMood     AfflictionKind = "mood"     // 心境异常
)

// Valid reports whether k is a declared affliction kind.
func (k AfflictionKind) Valid() bool {
	switch k {
	case AfflictionWound, AfflictionPoison, AfflictionInternal, AfflictionMood:
		return true
	default:
		return false
	}
}

// AfflictionDefinition describes one named affliction.
//
// Every number is configured rather than hardcoded, because these are balance
// values: how long a wound takes to close, and how much a poison drains, are
// exactly the numbers a later pass will move.
type AfflictionDefinition struct {
	ID     string         `json:"id"`
	NameZH string         `json:"name_zh"`
	Kind   AfflictionKind `json:"kind"`
	// DurationMonths is how many settled months the affliction lasts when
	// applied. It is a whole number of months because the game's only clock is
	// the world month.
	DurationMonths ConfigValue `json:"duration_months"`
	// HPDrainPerMonth is health lost each settled month, in SCALE sub-units.
	HPDrainPerMonth ConfigValue `json:"hp_drain_per_month,omitempty"`
	// MoodDrainPerMonth lowers 心境 each settled month.
	MoodDrainPerMonth ConfigValue `json:"mood_drain_per_month,omitempty"`
	// SpeedPenaltyPermille slows 遁速 while the affliction is active.
	SpeedPenaltyPermille ConfigValue `json:"speed_penalty_per_mille,omitempty"`
	// EffectiveAptitudeDelta and RateBonus mirror the temporary-adjustment
	// model TASK-08 introduced: they move derived values and never rewrite a
	// character's base attributes.
	EffectiveAptitudeDelta ConfigValue `json:"effective_aptitude_delta,omitempty"`
	RateBonus              ConfigValue `json:"rate_bonus,omitempty"`
	// Group controls additive stacking for rate bonuses; different groups
	// multiply independently.
	Group string `json:"group,omitempty"`
	// ClearedByRest marks an affliction a month of 养伤 mends. A poison is
	// deliberately not rest-clearable: it needs an antidote, which is TASK-13's
	// business, and making rest cure everything would remove the reason to
	// carry one.
	ClearedByRest bool `json:"cleared_by_rest"`
	// Description is display text.
	Description string `json:"description,omitempty"`
}

// KarmaTierDefinition describes one 业力 band.
type KarmaTierDefinition struct {
	ID     string `json:"id"`
	NameZH string `json:"name_zh"`
	// MinKarma is the lowest 业力 total in this tier. Tiers are sorted by this
	// value, and the highest matching tier wins.
	MinKarma ConfigValue `json:"min_karma"`
	Note     string      `json:"note,omitempty"`
}

// DialogueDefinition configures one dialogue node. M1 ships short exchanges
// only; romance and long arcs are explicitly out of scope.
type DialogueDefinition struct {
	ID     string `json:"id"`
	NPCID  string `json:"npc_id"`
	TextZH string `json:"text_zh"`
	// ResponseIDs are the available replies.
	ResponseIDs []string `json:"response_ids,omitempty"`
	// Requires gates the node.
	Requires []Precondition `json:"requires,omitempty"`
}
