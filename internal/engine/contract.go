// Package engine owns the deterministic domain rules and their data
// contracts. It must stay independent from terminals, filesystems, clocks and
// networks (enforced by boundary_test.go) so that any state can be replayed
// from a save file and produce identical mechanical results.
//
// TASK-04 defines the contracts only. No rule is executed yet; TASK-05 wires
// the command pipeline, TASK-06 the storage layer.
package engine

// SchemaVersion identifies the shape of the persisted state document. Bump it
// whenever a field is renamed, retyped, or removed so that TASK-06 can migrate
// or refuse an incompatible save instead of misreading it.
//
// RulesVersion and ContentVersion are carried alongside SchemaVersion because
// a save is only meaningful together with the rules and content that produced
// it: the same bytes replayed under different numbers are a different game.
const (
	// SchemaVersion is the on-disk structural contract for SaveEnvelope.
	SchemaVersion = 1
	// RulesVersion is the mechanical rules revision. It changes when formulas,
	// caps or timing change meaning, and must never change silently.
	RulesVersion = 1
	// ContentVersion is the revision of the shipped content catalogue loaded
	// by internal/content. A save referencing unknown ids is rejected.
	ContentVersion = 1
)

// Fixed-point scale. Every domain number is an integer so that replays and
// hashes are exact; floating point is banned from the engine. The design
// document requires at least four decimal digits of retained precision, so
// one whole unit is SCALE sub-units.
const (
	// SCALE is the number of sub-units in one whole unit (10^4).
	SCALE = 10000
	// PermilleScale converts basis points to whole units: 1000 bp == 1.0.
	PermilleScale = 1000
	// PercentScale converts whole percents to whole units: 100 pct == 1.0.
	PercentScale = 100
)

// Phase is the coarse session phase. It decides which domain operations are
// legal; the per-state input rules live in the design document 13.3 table and
// are enforced from TASK-05 onwards.
type Phase string

// Session phases. ENDED and SAVE_ERROR are terminal for world actions: an
// ended character accepts no cultivation, and a storage failure must not be
// papered over by continuing play.
const (
	PhaseCreation            Phase = "CREATION"
	PhaseReady               Phase = "READY"
	PhaseConfirming          Phase = "CONFIRMING"
	PhaseEventPending        Phase = "EVENT_PENDING"
	PhaseCombatPending       Phase = "COMBAT_PENDING"
	PhaseBreakthroughPending Phase = "BREAKTHROUGH_PENDING"
	PhaseSaveError           Phase = "SAVE_ERROR"
	PhaseEnded               Phase = "ENDED"
)

// Valid reports whether p is one of the declared phases. Validation rejects
// unknown phases rather than treating them as READY.
func (p Phase) Valid() bool {
	switch p {
	case PhaseCreation, PhaseReady, PhaseConfirming, PhaseEventPending,
		PhaseCombatPending, PhaseBreakthroughPending, PhaseSaveError, PhaseEnded:
		return true
	default:
		return false
	}
}

// AllowsWorldAction reports whether a world-advancing action (one that costs
// at least one game month) may start in this phase. Query-only operations are
// allowed in every phase and therefore are not modelled here.
func (p Phase) AllowsWorldAction() bool {
	return p == PhaseReady
}

// CommandKind is the discriminator of the command union. Every command must
// carry exactly one kind; payloads are validated against the kind so that no
// caller can smuggle an unvalidated object past the checks.
type CommandKind string

// Command kinds, grouped by the design document's 13.1 time-cost matrix.
const (
	// Zero-month, non-combat commands.
	KindQuery         CommandKind = "QUERY"          // view, help, page, settings
	KindCreateConfirm CommandKind = "CREATE_CONFIRM" // finalise a character
	KindAcceptQuest   CommandKind = "ACCEPT_QUEST"   // take a commission
	KindClaimReward   CommandKind = "CLAIM_REWARD"   // collect a one-off reward
	KindJoinSect      CommandKind = "JOIN_SECT"      // join a sect
	KindTravel        CommandKind = "TRAVEL"         // move between safe nodes
	KindTrade         CommandKind = "TRADE"          // buy/sell, non-combat items
	KindUseItem       CommandKind = "USE_ITEM"       // non-combat consumable
	KindEventChoice   CommandKind = "EVENT_CHOICE"   // answer a pending event
	KindBreakthrough  CommandKind = "BREAKTHROUGH"   // attempted advance
	KindCombatAction  CommandKind = "COMBAT_ACTION"  // attack/defend/observe/flee

	// One-month world actions.
	KindCultivate CommandKind = "CULTIVATE" // meditate one month
	KindHeal      CommandKind = "HEAL"      // recover one month
	KindWait      CommandKind = "WAIT"      // explicitly pass one month
	KindQuestRun  CommandKind = "QUEST_RUN" // execute a commission / excursion
)

// MonthCost returns the world-month cost of a kind, following design document
// 13.1. Nested sub-states (combat rounds, tribulation nodes) are explicitly
// zero extra months because they settle inside the parent action.
func (k CommandKind) MonthCost() int {
	switch k {
	case KindCultivate, KindHeal, KindWait, KindQuestRun, KindBreakthrough:
		return 1
	default:
		return 0
	}
}

// Valid reports whether k is a declared kind.
func (k CommandKind) Valid() bool {
	switch k {
	case KindQuery, KindCreateConfirm, KindAcceptQuest, KindClaimReward,
		KindJoinSect, KindTravel, KindTrade, KindUseItem, KindEventChoice,
		KindBreakthrough, KindCombatAction, KindCultivate, KindHeal, KindWait,
		KindQuestRun:
		return true
	default:
		return false
	}
}

// AdvancesCombatRound reports whether the kind consumes one combat round.
// Only COMBAT_ACTION does; every other kind leaves the round counter alone.
func (k CommandKind) AdvancesCombatRound() bool {
	return k == KindCombatAction
}

// ErrorCode is a stable, machine-readable failure reason. UI text is derived
// from the code by the presentation layer; the engine never ships prose.
type ErrorCode string

// Error codes. Each maps to exactly one violation of the design document's
// 9.4 invariant list so that tests can assert on the code, not on wording.
const (
	ErrNone                ErrorCode = ""
	ErrUnknownKind         ErrorCode = "UNKNOWN_KIND"
	ErrBadPhase            ErrorCode = "BAD_PHASE"
	ErrRevisionMismatch    ErrorCode = "REVISION_MISMATCH"
	ErrStaleViewToken      ErrorCode = "STALE_VIEW_TOKEN"
	ErrDuplicateAction     ErrorCode = "DUPLICATE_ACTION"
	ErrActionIdConflict    ErrorCode = "ACTION_ID_CONFLICT"
	ErrConfirmationNeeded  ErrorCode = "CONFIRMATION_REQUIRED"
	ErrConfirmationExpired ErrorCode = "CONFIRMATION_EXPIRED"
	ErrInsufficientFunds   ErrorCode = "INSUFFICIENT_FUNDS"
	ErrUnknownTarget       ErrorCode = "UNKNOWN_TARGET"
	ErrUnknownItem         ErrorCode = "UNKNOWN_ITEM"
	ErrQuantityNotHeld     ErrorCode = "QUANTITY_NOT_HELD"
	ErrPreconditionUnmet   ErrorCode = "PRECONDITION_UNMET"
	ErrInvariantBroken     ErrorCode = "INVARIANT_BROKEN"
	ErrSerialization       ErrorCode = "SERIALIZATION_FAILED"
)

// Resource is one of the four tracked soft currencies plus the two moral
// counters. Debt is tracked separately and never folded into Balance.
type Resource string

// Tracked resources.
const (
	ResSpiritStones Resource = "spirit_stones" // 灵石
	ResMerit        Resource = "merit"         // 功德
	ResKarma        Resource = "karma"         // 业力
	ResReputation   Resource = "reputation"    // 声望
	ResContribution Resource = "contribution"  // 贡献
)

// Valid reports whether r is a declared resource.
func (r Resource) Valid() bool {
	switch r {
	case ResSpiritStones, ResMerit, ResKarma, ResReputation, ResContribution:
		return true
	default:
		return false
	}
}

// Realm is one of the ten cultivation realms. M1 freezes exactly one playable
// path (human), but all ten realms are configured because lifespan and
// breakthrough tables must be complete to validate at all.
type Realm string

// The ten realms, ascending.
const (
	RealmQiRefining    Realm = "qi_refining"    // 炼气
	RealmFoundation    Realm = "foundation"     // 筑基
	RealmCrystallizing Realm = "crystallizing"  // 结晶
	RealmGoldenCore    Realm = "golden_core"    // 金丹
	RealmNascentSoul   Realm = "nascent_soul"   // 具灵
	RealmSoulForming   Realm = "soul_forming"   // 元婴
	RealmSpiritSeizing Realm = "spirit_seizing" // 化神
	RealmEnlightening  Realm = "enlightening"   // 悟道
	RealmAscension     Realm = "ascension"      // 羽化
	RealmImmortal      Realm = "immortal"       // 登仙
)

// RealmOrder lists the realms from lowest to highest. The order is part of the
// contract: breakthrough validation walks it, and tests assert completeness so
// that no realm is missing its lifespan entry.
var RealmOrder = []Realm{
	RealmQiRefining, RealmFoundation, RealmCrystallizing, RealmGoldenCore,
	RealmNascentSoul, RealmSoulForming, RealmSpiritSeizing, RealmEnlightening,
	RealmAscension, RealmImmortal,
}

// RealmTier is the sub-stage within a realm. Every realm has four tiers, so a
// realm's progress is (tier index, accumulated cultivation).
type RealmTier string

// The four tiers of every realm.
const (
	TierEarly      RealmTier = "early"      // 初期
	TierMiddle     RealmTier = "middle"     // 中期
	TierLate       RealmTier = "late"       // 后期
	TierPerfection RealmTier = "perfection" // 圆满
)

// TierOrder lists tiers from lowest to highest.
var TierOrder = []RealmTier{TierEarly, TierMiddle, TierLate, TierPerfection}

// Valid reports whether t is a declared tier.
func (t RealmTier) Valid() bool {
	switch t {
	case TierEarly, TierMiddle, TierLate, TierPerfection:
		return true
	default:
		return false
	}
}

// Path is a cultivation path. M1 freezes the human path only; the other two
// exist in the enum so that content referencing them is rejected rather than
// silently ignored.
type Path string

// Cultivation paths.
const (
	PathHuman Path = "human" // 人道
	// The following two are declared but frozen out of M1. Content that
	// requires them must fail validation, not quietly proceed.
	PathEarthly  Path = "earthly"  // 地道（M1 不开放）
	PathHeavenly Path = "heavenly" // 天道（M1 不开放）
)

// OpenInM1 reports whether the path may be selected in M1. Only the human path
// is open; ADR-001 must be revised before this changes, and doing so also
// invalidates the acceptance of TASK-04/11/13/16/17.
func (p Path) OpenInM1() bool { return p == PathHuman }

// Valid reports whether p is a declared path.
func (p Path) Valid() bool {
	switch p {
	case PathHuman, PathEarthly, PathHeavenly:
		return true
	default:
		return false
	}
}

// Origin is a starting background (出身).
type Origin string

// M1 origins. The merchant and hunter are the two with mechanical effects
// recorded in the design document 7.4.
const (
	OriginCommoner Origin = "commoner" // 平民
	OriginMerchant Origin = "merchant" // 商贾：额外 300 灵石
	OriginHunter   Origin = "hunter"   // 猎户：气血上限与初始当前值均 +20
)

// Valid reports whether o is a declared origin.
func (o Origin) Valid() bool {
	switch o {
	case OriginCommoner, OriginMerchant, OriginHunter:
		return true
	default:
		return false
	}
}

// SpiritRoot is an innate talent grade (灵根).
type SpiritRoot string

// Spirit roots with their cultivation multipliers expressed in SCALE units.
const (
	RootHeavenly SpiritRoot = "heavenly"  // 天 2.0
	RootEarthly  SpiritRoot = "earthly"   // 地 1.6
	RootTrue     SpiritRoot = "true"      // 真 1.3
	RootPseudo   SpiritRoot = "pseudo"    // 伪 1.0
	RootVariant  SpiritRoot = "variant"   // 变异 1.8
	RootVariantX SpiritRoot = "variant_x" // 对应系变异（另乘 1.2）
)

// Valid reports whether r is a declared spirit root.
func (r SpiritRoot) Valid() bool {
	switch r {
	case RootHeavenly, RootEarthly, RootTrue, RootPseudo, RootVariant, RootVariantX:
		return true
	default:
		return false
	}
}

// TechniqueGrade is a cultivation technique grade (功法品级).
type TechniqueGrade string

// Technique grades with their multipliers expressed in SCALE units.
const (
	GradeYellow   TechniqueGrade = "yellow"   // 黄 1.0
	GradeMystic   TechniqueGrade = "mystic"   // 玄 1.3
	GradeEarth    TechniqueGrade = "earth"    // 地 1.7
	GradeHeaven   TechniqueGrade = "heaven"   // 天 2.2
	GradeImmortal TechniqueGrade = "immortal" // 仙 3.0
)

// Valid reports whether g is a declared grade.
func (g TechniqueGrade) Valid() bool {
	switch g {
	case GradeYellow, GradeMystic, GradeEarth, GradeHeaven, GradeImmortal:
		return true
	default:
		return false
	}
}

// Environment is the ambient cultivation density (环境).
type Environment string

// Environments with their multipliers expressed in SCALE units.
const (
	EnvBarren  Environment = "barren"  // 贫瘠 0.6
	EnvNormal  Environment = "normal"  // 普通 1.0
	EnvRich    Environment = "rich"    // 浓郁 1.5
	EnvBlessed Environment = "blessed" // 福地 2.0
	EnvGrotto  Environment = "grotto"  // 洞天 2.5
)

// Valid reports whether e is a declared environment.
func (e Environment) Valid() bool {
	switch e {
	case EnvBarren, EnvNormal, EnvRich, EnvBlessed, EnvGrotto:
		return true
	default:
		return false
	}
}

// MoodBand is the mood band (心境) relative to a configured threshold.
type MoodBand string

// Mood bands with their multipliers expressed in SCALE units.
const (
	MoodAbove MoodBand = "above" // 高于阈值 1.2
	MoodAt    MoodBand = "at"    // 等于 1.0
	MoodBelow MoodBand = "below" // 低于 0.5
)

// Valid reports whether m is a declared mood band.
func (m MoodBand) Valid() bool {
	switch m {
	case MoodAbove, MoodAt, MoodBelow:
		return true
	default:
		return false
	}
}

// ActionKind distinguishes the two meditation intensities (行动倍率).
type ActionKind string

// Meditation intensities with their multipliers expressed in SCALE units.
const (
	ActionNormal ActionKind = "normal" // 普通修炼 1
	ActionClosed ActionKind = "closed" // 闭关 2
)

// Valid reports whether a is a declared action kind.
func (a ActionKind) Valid() bool {
	switch a {
	case ActionNormal, ActionClosed:
		return true
	default:
		return false
	}
}

// ConfigValue marks the provenance of a configured number. The design document
// insists that the original manuscript's figures and the extrapolated trial
// values are never silently mixed, and that unapproved parameters are visible
// rather than presented as settled balance.
type ConfigValue struct {
	// Provenance records where the number came from.
	Provenance Provenance `json:"provenance"`
	// Value is the number itself, in the unit documented by its owner.
	Value int `json:"value"`
	// Note is an optional short justification. Required when Provenance is
	// ProvenanceTrial or ProvenancePending so reviewers see the reasoning.
	Note string `json:"note,omitempty"`
}

// Provenance classifies where a configured number came from.
type Provenance string

// Provenance values, from most to least authoritative.
const (
	// ProvenanceManuscript is a figure taken from the original author's rules.
	ProvenanceManuscript Provenance = "manuscript"
	// ProvenanceTrial is an extrapolated number used to make the game
	// playable, explicitly not final balance.
	ProvenanceTrial Provenance = "trial"
	// ProvenancePending is a placeholder that must be replaced before release.
	// TASK-04 only requires it be marked, not resolved.
	ProvenancePending Provenance = "pending"
	// ProvenanceDesignNote is a number introduced by this project's design
	// document rather than the manuscript or a trial fit.
	ProvenanceDesignNote Provenance = "design_note"
)

// Valid reports whether p is a declared provenance.
func (p Provenance) Valid() bool {
	switch p {
	case ProvenanceManuscript, ProvenanceTrial, ProvenancePending, ProvenanceDesignNote:
		return true
	default:
		return false
	}
}

// Approved reports whether the value may ship as a settled parameter. Trial
// and pending values are playable but must not be presented as final balance,
// and the high-realm thresholds in particular are unapproved.
func (p Provenance) Approved() bool {
	return p == ProvenanceManuscript || p == ProvenanceDesignNote
}
