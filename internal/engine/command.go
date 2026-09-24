package engine

// This file defines the in-flight sub-states and the command/result protocol.
// The design document 9.2 requires that any state which can be observed by the
// player — a queued event, an unfinished month action, an unresolved combat
// round — be serialisable, so that quitting mid-way and resuming cannot
// re-roll or double-settle it.

// PendingState is a tagged union of the sub-states a phase can imply. At most
// one member is non-nil, and which one is dictated by Phase. Validation
// cross-checks the two so that a save cannot hold an EVENT_PENDING phase with
// no event.
type PendingState struct {
	Event        *PendingEvent        `json:"event,omitempty"`
	Combat       *PendingCombat       `json:"combat,omitempty"`
	Breakthrough *PendingBreakthrough `json:"breakthrough,omitempty"`
	MonthAction  *ActiveMonthAction   `json:"month_action,omitempty"`
	Confirmation *PendingConfirmation `json:"confirmation,omitempty"`
	// Creation holds an unconfirmed character draft. A draft does not occupy a
	// world month and resuming it must not regenerate or re-roll anything
	// already fixed.
	Creation *CreationDraft `json:"creation,omitempty"`

	// EventQueue holds event instances that have been raised but are not being
	// shown, because design R17 allows only one pending node at a time. The
	// queue is ordered highest priority first, and a tie is broken by the
	// order the instances were raised, so promotion is deterministic.
	//
	// A queued instance is a commitment, not a suggestion: it is already
	// recorded in World.RaisedEvents, so letting it expire still consumes the
	// event's occurrence budget.
	EventQueue []QueuedEvent `json:"event_queue,omitempty"`
}

// QueuedEvent is one raised-but-not-shown event instance.
//
// Expiry is stored as an absolute world month rather than a remaining count.
// A remaining count would have to be decremented on every settled month, and a
// single missed tick would leave the instance alive forever; an absolute month
// is correct even if a month is somehow settled twice.
type QueuedEvent struct {
	EventID    string `json:"event_id"`
	InstanceID string `json:"instance_id"`
	// Priority is copied from the definition at raise time. Copying rather
	// than re-reading the catalogue means a content update cannot silently
	// reorder a queue the player is already sitting on.
	Priority int `json:"priority"`
	// RaisedAtWorldMonth records when the instance entered the queue.
	RaisedAtWorldMonth int64 `json:"raised_at_world_month"`
	// ExpiresAtWorldMonth is the month at which the instance is dropped. Zero
	// means the instance never expires.
	ExpiresAtWorldMonth int64 `json:"expires_at_world_month"`
	// ChoiceIDs is frozen at raise time for the same reason PendingEvent
	// freezes its own: resuming must not re-roll the candidate set.
	ChoiceIDs []string `json:"choice_ids"`
}

// PendingEvent is an event instance awaiting the player's choice.
type PendingEvent struct {
	EventID    string `json:"event_id"`
	InstanceID string `json:"instance_id"`
	// ParentActionID ties this event to the action that raised it. A zero-month
	// choice inherits the parent's timing rather than starting a new action.
	ParentActionID string `json:"parent_action_id"`
	// ChoiceIDs is the frozen candidate set. It must not change across a
	// reload; re-rolling candidates on reopen is the bug this guards against.
	ChoiceIDs  []string `json:"choice_ids"`
	WorldMonth int64    `json:"world_month"`
}

// PendingCombat is an unresolved combat round. Combat never advances the world
// month on its own; it settles inside the parent action.
type PendingCombat struct {
	CombatID string `json:"combat_id"`
	// ParentActionID is the month-costing action this battle belongs to, if
	// any. When set, the parent's month cost is charged exactly once, after the
	// battle resolves.
	ParentActionID string      `json:"parent_action_id,omitempty"`
	Round          int64       `json:"round"`
	Enemies        []Combatant `json:"enemies"`
	AllyState      Combatant   `json:"ally_state"`
	// SpiritEnergy is the separate combat resource (灵力). It is distinct from
	// the player's MP and clears when the battle ends.
	SpiritEnergy SpiritEnergy `json:"spirit_energy"`
	// AwaitingPlayer records whose input the round is waiting for. A resume
	// must not hand the enemy a free attack.
	AwaitingPlayer bool `json:"awaiting_player"`
	// FleeAllowed / Resolution record the battle's state.
	Resolution CombatResolution `json:"resolution"`
}

// SpiritEnergy is the per-round combat resource pool.
type SpiritEnergy struct {
	Current int `json:"current"`
	Max     int `json:"max"`
	// PerRound is the configured regeneration per round.
	PerRound int `json:"per_round"`
	// DrainBonus is granted to the next round by a successful drain.
	DrainBonus int `json:"drain_bonus"`
}

// Combatant is one participant in a battle.
type Combatant struct {
	ID      string    `json:"id"`
	HP      Vitals    `json:"hp"`
	MP      Vitals    `json:"mp"`
	Attack  int64     `json:"attack"`
	Defense int64     `json:"defense"`
	Speed   int64     `json:"speed"`
	Realm   Realm     `json:"realm"`
	Tier    RealmTier `json:"tier"`
	// Statuses are named combat conditions with remaining rounds.
	Statuses []TimedEffect `json:"statuses"`
	// UltimateCharge counts down the rounds until an ultimate is ready; a
	// prepared ultimate can be interrupted.
	UltimateCharge int `json:"ultimate_charge"`
}

// CombatResolution is a battle's outcome.
type CombatResolution string

// Combat resolutions.
const (
	CombatOngoing CombatResolution = "ongoing"
	CombatWon     CombatResolution = "won"
	CombatLost    CombatResolution = "lost"
	CombatFled    CombatResolution = "fled"
	CombatAborted CombatResolution = "aborted"
)

// PendingBreakthrough is an unresolved major breakthrough, i.e. a tribulation
// or inner-demon choice. Materials are already spent; a reload must keep the
// spent materials and the frozen candidates.
type PendingBreakthrough struct {
	BreakthroughID string `json:"breakthrough_id"`
	// ParentActionID is the month-costing action that started the attempt.
	ParentActionID string `json:"parent_action_id"`
	// NodeID is the tribulation/demon node awaiting a choice.
	NodeID string `json:"node_id"`
	// SpentMaterials records what was consumed, for the log.
	SpentMaterials []ItemStack `json:"spent_materials"`
	// ChoiceIDs is the frozen candidate set for this node.
	ChoiceIDs []string `json:"choice_ids"`
	// CompletedNodes records nodes already resolved in this attempt.
	CompletedNodes []string `json:"completed_nodes"`
	// FromRealm / ToRealm describe the transition being attempted.
	FromRealm Realm     `json:"from_realm"`
	FromTier  RealmTier `json:"from_tier"`
	ToRealm   Realm     `json:"to_realm"`
	ToTier    RealmTier `json:"to_tier"`
}

// ActiveMonthAction is a month-costing action in flight. Its single month cost
// is charged exactly once, when it settles, so a nested battle cannot charge a
// second month.
type ActiveMonthAction struct {
	ActionID string      `json:"action_id"`
	Kind     CommandKind `json:"kind"`
	// MonthCharged is set once the month has been applied. Settlement must
	// check it before advancing the counter.
	MonthCharged bool `json:"month_charged"`
	// SettlementPending is true while a nested sub-state (combat, tribulation)
	// is still open.
	SettlementPending bool `json:"settlement_pending"`
	// StartedWorldMonth records the month the action began.
	StartedWorldMonth int64 `json:"started_world_month"`
	// TargetID / TargetInstanceID identify what the action is aimed at.
	TargetID         string `json:"target_id,omitempty"`
	TargetInstanceID string `json:"target_instance_id,omitempty"`
	// StartedMaterials records materials consumed at start, so a resumed
	// action cannot consume them again.
	StartedMaterials []ItemStack `json:"started_materials"`
}

// PendingConfirmation is a UI-only transient. Exiting discards it without
// spending materials, and the next session re-previews the proposal.
type PendingConfirmation struct {
	Token string `json:"token"`
	// Kind is what is being confirmed, and TargetID what it applies to.
	Kind     CommandKind `json:"kind"`
	TargetID string      `json:"target_id"`
	// Cost preview, so the confirmation is not shown without its price.
	Preview ConfirmationPreview `json:"preview"`
	// ExpiresAtRevision guards against confirming against a changed state.
	ExpiresAtRevision uint64 `json:"expires_at_revision"`
}

// ConfirmationPreview carries the visible cost of a proposal. The design
// document requires the cost of a choice to be visible before it is taken.
type ConfirmationPreview struct {
	MonthCost int                `json:"month_cost"`
	Materials []ItemStack        `json:"materials"`
	Resources map[Resource]int64 `json:"resources"`
	// Risks names each warned risk in plain ids for the UI to localise.
	Risks []string `json:"risks"`
}

// CreationDraft is an unconfirmed character being edited. It deliberately has
// its own storage slot so that drafting does not touch the world.
//
// It is a draft, not a character: it holds no Player, and no code path here can
// produce one. Only an explicit confirmation, run against a fully validated
// draft, invokes the factory.
type CreationDraft struct {
	Step       int        `json:"step"`
	Identity   Identity   `json:"identity"`
	Origin     Origin     `json:"origin"`
	Path       Path       `json:"path"`
	SpiritRoot SpiritRoot `json:"spirit_root"`
	// Constitution is the body-type id, empty when the catalogue default is
	// intended. TASK-07 adds it because the design's collection list includes
	// 体质; leaving it out would make the second step unable to record one.
	Constitution string `json:"constitution,omitempty"`
	// TalentIDs are the chosen talents. Design 6.1 gives a 5-point talent
	// budget; the per-talent costs are still pending balance values, so the
	// list is recorded and validated for existence and exclusivity only.
	TalentIDs []string `json:"talent_ids,omitempty"`
	// YaoIntent records a 妖族 origin *intent* without resolving it. Design
	// 6.1 requires the intent to be recorded first and judged later, after the
	// aptitude allocation, so that the judgment cannot be gamed by reordering
	// the steps.
	YaoIntent bool `json:"yao_intent,omitempty"`
	// AgeYears is the chosen creation age. It lives here rather than being
	// derived at confirm time so that a resumable draft shows the same age.
	AgeYears int `json:"age_years,omitempty"`
	// PresetID records which preset pre-filled the draft, for display and for
	// the "confirm within three actions" path. It confers no mechanical
	// advantage: a preset is validated like any other allocation.
	PresetID string `json:"preset_id,omitempty"`
	// FixedResults records the rolls already locked in, so resuming a draft
	// cannot re-roll them.
	FixedResults map[string]int64 `json:"fixed_results"`
	Confirmed    bool             `json:"confirmed"`
}

// Command is the full submission envelope. The design document 9.2 fixes this
// shape: an action id, the session it belongs to, the revision and view token
// it was issued against, a kind, an optional target, a payload and an optional
// confirmation token.
type Command struct {
	ActionID         string `json:"action_id"`
	SessionID        string `json:"session_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	// ViewToken binds the command to the panel the player was looking at, so a
	// key pressed against a stale screen cannot land on a new event.
	ViewToken        string      `json:"view_token"`
	Kind             CommandKind `json:"kind"`
	TargetID         string      `json:"target_id,omitempty"`
	TargetInstanceID string      `json:"target_instance_id,omitempty"`
	// Payload is kind-specific and must be validated against the kind.
	Payload Payload `json:"payload"`
	// ConfirmationToken is required for proposals that spend materials or
	// carry risk. Absent when unneeded.
	ConfirmationToken string `json:"confirmation_token,omitempty"`
}

// Payload is the kind-specific argument set. It is a struct rather than a map
// so that an unknown field cannot be smuggled through; validation checks the
// combination of kinds and populated fields.
type Payload struct {
	// Quantity is used by trade and item use.
	Quantity int64 `json:"quantity,omitempty"`
	// ItemID identifies the item for trade/item/quest payloads.
	ItemID string `json:"item_id,omitempty"`
	// QuestID identifies a quest for accept/claim/run.
	QuestID string `json:"quest_id,omitempty"`
	// NPCID identifies the counterpart for dialogue, gifts and duels.
	NPCID string `json:"npc_id,omitempty"`
	// LocationID is the travel destination.
	LocationID string `json:"location_id,omitempty"`
	// SkillID identifies a combat skill.
	SkillID string `json:"skill_id,omitempty"`
	// ChoiceID is the player's answer to a pending event node.
	ChoiceID string `json:"choice_id,omitempty"`
	// QueryKind selects which read-only panel is requested.
	QueryKind QueryKind `json:"query_kind,omitempty"`
	// Text carries free-form input such as a name. A "q" typed here is the
	// letter q, not a quit command.
	Text string `json:"text,omitempty"`
	// ActionKind selects meditation intensity for cultivation.
	ActionKind ActionKind `json:"action_kind,omitempty"`

	// --- Character creation (TASK-07) ------------------------------------
	//
	// A creation command carries the *proposed* selection rather than
	// referring to the draft, so the pipeline can validate it on the clone
	// before any of it is written. A rejected proposal therefore leaves no
	// trace, which is what makes an invalid allocation free to attempt.

	// Creation carries the proposed character. It is set by CREATE_EDIT and
	// CREATE_CONFIRM and ignored by every other kind.
	Creation *CreationPayload `json:"creation,omitempty"`
}

// CreationPayload is the proposed character for a creation command.
//
// Every field is optional; an absent field means "keep what the draft has", so
// a caller can change one attribute without restating the whole character.
type CreationPayload struct {
	// Selection is the proposed content and allocation.
	Selection CreationSelection `json:"selection"`
	// PresetID applies a preset to the draft before the selection, so the
	// quick path can be expressed as one command.
	PresetID string `json:"preset_id,omitempty"`
	// AdvanceStep moves the wizard forward when true. It is explicit rather
	// than implied so that a plain edit does not skip a step.
	AdvanceStep bool `json:"advance_step,omitempty"`
	// BackStep moves the wizard back one step when true.
	BackStep bool `json:"back_step,omitempty"`
	// Confirm marks the draft ready to finalise. CREATE_CONFIRM requires it.
	Confirm bool `json:"confirm,omitempty"`
	// YaoVerdict is the 仙缘 judgment result for a yao-intent draft,
	// expressed in permille so the engine stays integer-only. It is supplied
	// by the caller once, then locked into FixedResults; a later command
	// cannot overwrite it, which is what stops a refresh from re-rolling.
	//
	// YaoVerdictGiven must be set for the value to count. A plain int cannot
	// distinguish "no verdict supplied" from "verdict 0", and treating the
	// zero value as a real verdict would lock a failed judgment on the very
	// first edit, before the player ever saw one.
	YaoVerdictPermille int  `json:"yao_verdict_permille,omitempty"`
	YaoVerdictGiven    bool `json:"yao_verdict_given,omitempty"`
}

// QueryKind enumerates the read-only panels. A query never changes mechanical
// state or advances a random stream.
type QueryKind string

// Query kinds.
const (
	QueryStatus    QueryKind = "status"
	QueryInventory QueryKind = "inventory"
	QueryQuests    QueryKind = "quests"
	QueryRelations QueryKind = "relations"
	QueryLog       QueryKind = "log"
	QueryHelp      QueryKind = "help"
	QueryRealm     QueryKind = "realm"
)

// Valid reports whether q is a declared query.
func (q QueryKind) Valid() bool {
	switch q {
	case QueryStatus, QueryInventory, QueryQuests, QueryRelations, QueryLog,
		QueryHelp, QueryRealm:
		return true
	default:
		return false
	}
}

// CommandResult is the outcome of one submission. It is also its own
// idempotency record: a duplicate action id returns the stored result instead
// of re-applying the effect.
type CommandResult struct {
	ActionID string `json:"action_id"`
	// RevisionAfter is the state revision once the command committed. For a
	// query it equals the revision it was issued against.
	RevisionAfter uint64 `json:"revision_after"`
	// ViewToken is the token for the panel produced by this result.
	ViewToken string `json:"view_token"`
	// OK is false when Code is non-empty.
	OK   bool      `json:"ok"`
	Code ErrorCode `json:"code,omitempty"`

	// MonthCostApplied records the months actually charged.
	MonthCostApplied int `json:"month_cost_applied"`
	// Delta is the ledger of mechanical changes, for the log and the UI's
	// "what changed" line.
	Delta DeltaLedger `json:"delta"`
	// Events is the ordered list of things that happened, by id.
	Events []ResultEvent `json:"events"`
	// SaveState reports whether the result is durable. The design document
	// forbids reporting success when the save failed.
	SaveState SaveState `json:"save_state"`
	// Narrative is optional prose from a template or the AI layer. It never
	// affects mechanical state, and regenerating it must not re-run the action.
	Narrative string `json:"narrative,omitempty"`
}

// SaveState reports the durability of a committed result.
type SaveState string

// Save states.
const (
	// SaveDurable means the change is on disk.
	SaveDurable SaveState = "durable"
	// SaveFailed means the change is not on disk; the session must enter
	// SAVE_ERROR and must not continue world actions.
	SaveFailed SaveState = "failed"
	// SaveNotNeeded means the command changed nothing (a query).
	SaveNotNeeded SaveState = "not_needed"
)

// DeltaLedger is an ordered, named list of mechanical changes. Every entry is
// attributable so that a summary can explain the month rather than assert it.
type DeltaLedger struct {
	Entries []DeltaEntry `json:"entries"`
}

// DeltaEntry is one change.
type DeltaEntry struct {
	// Field is a stable path-ish name, e.g. "xp", "hp.current",
	// "resources.spirit_stones".
	Field string `json:"field"`
	// Before and After are the values on either side, in the field's unit.
	Before int64 `json:"before"`
	After  int64 `json:"after"`
	// Reason names the rule that caused it.
	Reason string `json:"reason"`
}

// ResultEvent is one thing that happened during a commit.
type ResultEvent struct {
	Kind string `json:"kind"`
	// ID identifies the event, quest, item or combat involved.
	ID         string `json:"id"`
	InstanceID string `json:"instance_id,omitempty"`
	// Detail is a short machine-readable qualifier.
	Detail string `json:"detail,omitempty"`
}

// Result event kinds.
const (
	ResultMonthAdvanced = "month_advanced"
	ResultEventQueued   = "event_queued"
	ResultEventRaised   = "event_raised"
	ResultEventExpired  = "event_expired"
	ResultEventPromoted = "event_promoted"
	ResultTravelled     = "travelled"
	ResultCombatStarted = "combat_started"
	ResultCombatRound   = "combat_round"
	ResultCombatEnded   = "combat_ended"
	ResultItemGained    = "item_gained"
	ResultItemLost      = "item_lost"
	ResultXPChanged     = "xp_changed"
	ResultRealmChanged  = "realm_changed"
	ResultQuestChanged  = "quest_changed"
	ResultEnded         = "ended"
)

// IdempotencyTable remembers recent submissions. It is bounded: only the most
// recent entries are kept, which is safe because a resent command is always
// the most recent one.
type IdempotencyTable struct {
	// Entries is keyed by action id.
	Entries map[string]IdempotencyEntry `json:"entries"`
	// Order preserves insertion order for bounded eviction.
	Order []string `json:"order"`
	// Limit is the maximum number of retained entries.
	Limit int `json:"limit"`
}

// IdempotencyEntry is one remembered submission.
type IdempotencyEntry struct {
	ActionID string `json:"action_id"`
	// RequestFingerprint detects the same action id used with a different
	// body, which must be rejected rather than silently accepted.
	RequestFingerprint string        `json:"request_fingerprint"`
	Result             CommandResult `json:"result"`
}

// DefaultIdempotencyLimit is the retained action count. TASK-05 may tune it;
// the contract only fixes that it is bounded.
const DefaultIdempotencyLimit = 64
