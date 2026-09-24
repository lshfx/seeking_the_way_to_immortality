package engine

// This file defines the persisted state document. Everything here must be
// serialisable and must survive a save/load round trip byte-for-byte in its
// mechanical content, because TASK-05 guarantees that the same snapshot plus
// the same action sequence yields the same mechanical hash.

// GameState is the complete mechanical state of one playthrough. It contains
// no presentation data (that is PanelModel's job) and no credentials.
type GameState struct {
	// Versioning. A save is only interpretable together with the rules and
	// content that produced it.
	SchemaVersion  int `json:"schema_version"`
	RulesVersion   int `json:"rules_version"`
	ContentVersion int `json:"content_version"`

	// Identity and lineage. BranchID distinguishes parallel timelines created
	// by loading an earlier checkpoint; it is never reused.
	GameID   string `json:"game_id"`
	BranchID string `json:"branch_id"`

	// Revision increments on every committed domain change. A command carries
	// the revision it was issued against, so a stale panel cannot mutate a
	// newer state.
	Revision uint64 `json:"revision"`

	Phase Phase `json:"phase"`

	// The three counters. Each advances for a different reason, which is why
	// they are separate rather than one "time" number.
	Counters Counters `json:"counters"`

	// Independent random streams. Isolating them keeps a combat roll from
	// shifting world event selection.
	RNG RNGState `json:"rng"`

	Player *Player `json:"player"`
	World  *World  `json:"world"`

	// PendingState holds whatever sub-state the phase implies: a queued event,
	// a combat round, a tribulation choice, or an in-flight month action.
	Pending PendingState `json:"pending"`

	// Idempotency keeps the outcome of recent actions so that a resend returns
	// the original result instead of applying the effect twice.
	Idempotency IdempotencyTable `json:"idempotency"`

	// LogCursor points at the durable log tail. Logs are appended outside the
	// mechanical state so that prose cannot affect replay.
	LogCursor uint64 `json:"log_cursor"`
}

// Counters tracks the three independent clocks.
type Counters struct {
	// WorldMonth advances only when a month-costing action settles. It is the
	// only source of aging.
	WorldMonth int64 `json:"world_month"`
	// InteractionSeq advances on each committed domain action of any kind. It
	// does not advance on queries, empty input, or duplicate submits.
	InteractionSeq int64 `json:"interaction_seq"`
	// CombatRound advances once per submitted combat action. It resets when a
	// battle ends and never advances the world month.
	CombatRound int64 `json:"combat_round"`
}

// RNGState holds the serialisable state of every independent random stream.
// Algorithm identity and counter are stored so that replay under a newer
// build cannot silently change an old outcome.
type RNGState struct {
	Algorithm string               `json:"algorithm"`
	Streams   map[string]RNGStream `json:"streams"`
}

// RNGStream is one named stream: world events, combat, drops and NPC each get
// their own so that advancing one does not perturb another.
type RNGStream struct {
	State     uint64 `json:"state"`
	Counter   uint64 `json:"counter"`
	Algorithm string `json:"algorithm"`
}

// Stream names. Using constants keeps content and code from drifting apart.
const (
	StreamWorld  = "world"  // 世界事件
	StreamCombat = "combat" // 战斗
	StreamDrop   = "drop"   // 掉落
	StreamNPC    = "npc"    // NPC
)

// WorldStreamNames lists every stream that must exist in a valid save.
var WorldStreamNames = []string{StreamWorld, StreamCombat, StreamDrop, StreamNPC}

// Player is the character. Numeric fields are fixed-point sub-units where the
// quantity is fractional, and plain integers where it is a count.
type Player struct {
	Identity  Identity       `json:"identity"`
	AgeMonths int64          `json:"age_months"` // 年龄月数
	Lifespan  LifespanLedger `json:"lifespan"`

	Origin                Origin     `json:"origin"`
	Path                  Path       `json:"path"`
	SpiritRoot            SpiritRoot `json:"spirit_root"`
	Constitution          string     `json:"constitution"` // 体质 id，可为空
	TalentIDs             []string   `json:"talent_ids"`   // 天赋 id
	PrimaryTechniqueID    string     `json:"primary_technique_id"`
	SecondaryTechniqueIDs []string   `json:"secondary_technique_ids,omitempty"`

	// Attributes. Static values come from creation; growth and modifiers are
	// applied on top so that a re-spec never rewrites history.
	Attributes Attributes `json:"attributes"`
	Derived    Derived    `json:"derived"`

	// Vitals in SCALE sub-units, with their caps stored separately so that a
	// buff never permanently raises the ceiling.
	HP Vitals `json:"hp"`
	MP Vitals `json:"mp"`
	XP int64  `json:"xp"` // 修为

	Realm Realm     `json:"realm"`
	Tier  RealmTier `json:"tier"`

	Inventory Inventory `json:"inventory"`
	Equipment Equipment `json:"equipment"`

	// Currency. Balance is never allowed to go negative: an unaffordable
	// purchase is refused rather than auto-loaned. Debt is a separate ledger.
	Resources map[Resource]int64 `json:"resources"`
	Debt      int64              `json:"debt"`

	SectID string `json:"sect_id,omitempty"`

	// Condition tracks the affects that modify the cultivation formula by
	// name, so that the same penalty is never applied twice.
	Condition Condition `json:"condition"`

	Relations     []Relation       `json:"relations"`
	Proficiencies map[string]int64 `json:"proficiencies"` // 熟练度
	Insights      map[string]int64 `json:"insights"`      // 悟道

	// BreakthroughBonuses are content-scoped named accumulators that a later
	// breakthrough attempt consumes, e.g. a tribulation resistance earned by
	// facing an inner demon.
	//
	// They are stored rather than resolved on the spot because the attempt
	// happens later: turning "抗性 +15" into a success immediately would be the
	// narrator deciding the outcome, which design R20 forbids.
	BreakthroughBonuses map[string]int64 `json:"breakthrough_bonuses,omitempty"`

	// Endeat records the terminal cause once the character can no longer act.
	Ended    bool     `json:"ended"`
	EndCause EndCause `json:"end_cause,omitempty"`
}

// Identity is the creation-time descriptive record. Only surname and given
// name are mechanical (they appear in logs); the rest is presentation.
type Identity struct {
	Surname    string `json:"surname"`
	GivenName  string `json:"given_name"`
	DaoName    string `json:"dao_name,omitempty"`
	Gender     string `json:"gender"`
	Appearance string `json:"appearance"`
}

// LifespanLedger records the lifespan ceiling and every named adjustment to
// it. Keeping the adjustments separate lets the UI explain a death instead of
// asserting one.
type LifespanLedger struct {
	BaseYears int `json:"base_years"` // 来自境界表的原稿寿元
	// Bonuses are named deltas, e.g. a successful breakthrough extending the
	// realm ceiling, or a pill. Each is applied once.
	Bonuses []LifespanBonus `json:"bonuses"`
}

// LifespanBonus is one named lifespan adjustment.
type LifespanBonus struct {
	ID     string `json:"id"`
	Years  int    `json:"years"`
	Reason string `json:"reason"`
}

// TotalYears returns the effective lifespan ceiling in years.
func (l LifespanLedger) TotalYears() int {
	total := l.BaseYears
	for _, b := range l.Bonuses {
		total += b.Years
	}
	return total
}

// Attributes holds the six base statistics plus growth. Each entry is a whole
// number; the cultivation formula reads Comprehension (悟性) and Aptitude
// (资质) from here.
type Attributes struct {
	// Base values set at creation.
	Strength      int `json:"strength"`
	Agility       int `json:"agility"`
	Constitution  int `json:"constitution"`  // 根骨 (body)
	Comprehension int `json:"comprehension"` // 悟性
	Aptitude      int `json:"aptitude"`      // 资质
	Fortune       int `json:"fortune"`       // 福缘
	// Growth accumulated from play. Final = Base + Growth.
	Growth map[string]int `json:"growth"`
}

// Derived holds values computed from attributes, realm and equipment. They are
// stored rather than recomputed so that a rules-version change cannot silently
// alter an old save mid-run.
type Derived struct {
	// EffectiveAptitude is Aptitude after any named injury penalty. The design
	// document requires injury reducing effective aptitude to be a distinct,
	// named effect from a direct cultivation-efficiency penalty.
	EffectiveAptitude int `json:"effective_aptitude"`
	// CultivationRate is the cached per-month increment in XP sub-units.
	CultivationRate int64 `json:"cultivation_rate"`
	Attack          int64 `json:"attack"`
	Defense         int64 `json:"defense"`
	Speed           int64 `json:"speed"` // 遁速
}

// Vitals is a current/max pair in SCALE sub-units.
type Vitals struct {
	Current int64 `json:"current"`
	Max     int64 `json:"max"`
}

// Inventory is a stack list. Stacks are keyed by item id and never allow a
// negative quantity; removal validates the held amount first.
type Inventory struct {
	Stacks []ItemStack `json:"stacks"`
	// Capacity is the stack-slot limit. Zero means unlimited until TASK-13
	// decides otherwise.
	Capacity int `json:"capacity"`
}

// ItemStack is one held item and its count.
type ItemStack struct {
	ItemID   string `json:"item_id"`
	Quantity int64  `json:"quantity"`
}

// Equipment maps a slot to the item occupying it.
type Equipment struct {
	Slots map[EquipSlot]string `json:"slots"`
}

// EquipSlot is an equipment slot name.
type EquipSlot string

// Equipment slots. M1 uses a small fixed set.
const (
	SlotWeapon   EquipSlot = "weapon"
	SlotArmor    EquipSlot = "armor"
	SlotTalisman EquipSlot = "talisman"
)

// Condition collects named status effects and injuries. Each entry carries a
// remaining duration in game months so that settling a month can expire it.
type Condition struct {
	Effects []TimedEffect `json:"effects"`
	// Mood is the current 心境 value.
	Mood int `json:"mood"`
}

// TimedEffect is one named effect with a duration.
type TimedEffect struct {
	ID string `json:"id"`
	// MonthsRemaining decrements on each settled world month.
	MonthsRemaining int `json:"months_remaining"`
	// Stacks is the effect's magnitude, interpreted by its owning rule.
	Stacks int `json:"stacks"`
}

// Relation is the player's standing with one NPC.
type Relation struct {
	NPCID string `json:"npc_id"`
	Value int    `json:"value"`
	// Met is set once the player has actually encountered the NPC.
	Met bool `json:"met"`
}

// EndCause records why a character stopped being playable.
type EndCause struct {
	Code       string `json:"code"`
	WorldMonth int64  `json:"world_month"`
	Reason     string `json:"reason"`
}

// End codes. A dead character's cause is a stable identifier so the review
// screen can explain it without the engine shipping prose.
const (
	// EndCodeLifespan is death by running out of lifespan.
	EndCodeLifespan = "LIFESPAN"
	// EndCodeCombat is death in battle.
	EndCodeCombat = "COMBAT"
	// EndCodeInjury is death from an untreated wound, poison or other
	// affliction: the character ran out of health outside an encounter, where
	// design 6.2's encounter-specific consequences do not apply.
	EndCodeInjury = "INJURY"
	// EndCodeTribulation is death during a breakthrough tribulation.
	EndCodeTribulation = "TRIBULATION"
	// EndCodeInnerDemon is death from an inner demon.
	EndCodeInnerDemon = "INNER_DEMON"
)

// Valid reports whether c is a declared end code. An unknown code would leave
// the review screen unable to explain a death, which is the one screen that must
// never be vague.
func (c EndCause) Valid() bool {
	switch c.Code {
	case EndCodeLifespan, EndCodeCombat, EndCodeInjury, EndCodeTribulation, EndCodeInnerDemon:
		return true
	default:
		return false
	}
}

// NPC is one non-player character. Only the fields that affect determinism are
// stored; prose lives in content.
type NPC struct {
	ID        string `json:"id"`
	IsAlive   bool   `json:"is_alive"`
	AgeMonths int64  `json:"age_months"`
	// LifespanYears is the NPC ceiling in years.
	LifespanYears int       `json:"lifespan_years"`
	Realm         Realm     `json:"realm"`
	Tier          RealmTier `json:"tier"`

	Faction     string `json:"faction"`
	Personality string `json:"personality"`
	Location    string `json:"location"`
	// Schedule maps a coarse time slot to a location id.
	Schedule map[string]string `json:"schedule"`
	// Goals are content ids the NPC is pursuing.
	Goals []string `json:"goals"`

	// KnownFacts is the set of fact ids this NPC has learned. A dead NPC
	// accepts no gifts and no new dialogue, enforced from TASK-05.
	KnownFacts []string `json:"known_facts"`
}

// World is the mutable world state.
type World struct {
	NPCs map[string]NPC `json:"npcs"`
	// Locations visited, for the travel graph.
	VisitedLocations []string `json:"visited_locations"`
	CurrentLocation  string   `json:"current_location"`
	// Quests holds per-quest progress keyed by quest id.
	Quests map[string]QuestState `json:"quests"`
	// ConsumedEvents records event ids already resolved, so a limited event
	// cannot pay out twice across saves.
	ConsumedEvents []ConsumedEvent `json:"consumed_events"`
	// Flags are named world switches set by event effects.
	Flags map[string]bool `json:"flags"`

	// RaisedEvents records every event instance that has been raised, whether
	// or not it was ever answered.
	//
	// It is separate from ConsumedEvents on purpose. Occurrence limits and
	// cooldowns are measured from *raising*, because measuring them from
	// answering would let a player exhaust nothing: simply ignoring an event
	// until it expires would leave the event free to fire again, which makes a
	// MaxOccurrences cap meaningless.
	RaisedEvents []RaisedEvent `json:"raised_events,omitempty"`
	// EventInstanceSeq numbers raised instances. A counter rather than a
	// derived value, so two instances of the same event raised in the same
	// month still get distinct ids.
	EventInstanceSeq int64 `json:"event_instance_seq,omitempty"`
}

// RaisedEvent is one event instance that entered play.
type RaisedEvent struct {
	EventID    string `json:"event_id"`
	InstanceID string `json:"instance_id"`
	// WorldMonth is the month in which the instance was raised. Cooldown is
	// measured from here.
	WorldMonth int64 `json:"world_month"`
	// Forced marks an instance raised by the forced path rather than the
	// random draw, so the log can explain why it happened.
	Forced bool `json:"forced,omitempty"`
}

// QuestState tracks one quest's progress.
type QuestState struct {
	QuestID string      `json:"quest_id"`
	Status  QuestStatus `json:"status"`
	// RewardsClaimed guards the one-off payout.
	RewardsClaimed bool `json:"rewards_claimed"`
	// Progress counts whatever the quest tracks (usually completed steps).
	Progress int `json:"progress"`
}

// QuestStatus is a quest's lifecycle position.
type QuestStatus string

// Quest statuses.
const (
	QuestOffered   QuestStatus = "offered"
	QuestAccepted  QuestStatus = "accepted"
	QuestComplete  QuestStatus = "complete"
	QuestClaimed   QuestStatus = "claimed"
	QuestAbandoned QuestStatus = "abandoned"
	QuestExpired   QuestStatus = "expired"
)

// ConsumedEvent records one resolved event instance.
type ConsumedEvent struct {
	// EventID is the definition id; InstanceID distinguishes this occurrence
	// from previous ones, so a repeatable event still cannot double-pay.
	EventID    string `json:"event_id"`
	InstanceID string `json:"instance_id"`
	ChoiceID   string `json:"choice_id"`
	WorldMonth int64  `json:"world_month"`
}
