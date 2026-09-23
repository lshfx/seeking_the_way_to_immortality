// Package panel defines the read-only presentation contract. The panel layer
// renders what the engine and session have already decided; it never computes
// a domain number, never draws a random value, and never advances a clock.
//
// That separation is what makes the mechanical guarantees testable: if a
// renderer cannot compute, it cannot change an outcome. TASK-04 defines the
// contract; TASK-09 renders it.
package panel

// SchemaVersion identifies the shape of the panel document. It is versioned
// separately from the engine schema because a renderer may legitimately support
// an older panel than the engine emits.
const SchemaVersion = 1

// Model is the complete description of one screen. Everything a renderer needs
// is here; nothing here requires the renderer to consult the engine.
type Model struct {
	SchemaVersion int `json:"schema_version"`

	// ViewToken is the identity of this screen. A command must echo it back,
	// so a key pressed while looking at an older screen cannot land on the
	// newer one. This is the mechanism behind the design document's rule that
	// switching screens discards the old key queue.
	ViewToken string `json:"view_token"`
	// ParentActionID ties this screen to the action that produced it, if any.
	// A zero-month event choice inherits the parent's timing through this.
	ParentActionID string `json:"parent_action_id,omitempty"`
	// Revision is the state revision this screen describes. A command issued
	// against it carries the same number, so a stale screen is detectable.
	Revision uint64 `json:"revision"`

	// Title is the screen heading.
	Title string `json:"title"`
	// Calendar is the in-game date, already formatted by the session.
	Calendar Calendar `json:"calendar"`
	// Scene names the current location and situation.
	Scene string `json:"scene"`

	// Status is the compact always-visible block.
	Status StatusBlock `json:"status"`
	// Details are the expandable sections. They are separate so a renderer can
	// collapse them without recomputing anything.
	Details []DetailSection `json:"details,omitempty"`

	// Event is the pending event narration and its options, when the phase is
	// EVENT_PENDING.
	Event *EventBlock `json:"event,omitempty"`
	// Combat is the pending combat view, when the phase is COMBAT_PENDING.
	Combat *CombatBlock `json:"combat,omitempty"`
	// Breakthrough is the pending tribulation/choice view.
	Breakthrough *BreakthroughBlock `json:"breakthrough,omitempty"`
	// Confirmation is the proposal awaiting a yes/no, when CONFIRMING.
	Confirmation *ConfirmationBlock `json:"confirmation,omitempty"`
	// Creation is the character-creation wizard view, when CREATION.
	Creation *CreationBlock `json:"creation,omitempty"`

	// Options are the selectable actions on this screen. A panel with no
	// options is a terminal or error screen.
	Options []Option `json:"options"`

	// Changes summarises what the last committed action did, so the player can
	// see the consequence rather than infer it.
	Changes []ChangeLine `json:"changes,omitempty"`

	// Save reports durability. The renderer must show a failure rather than
	// implying success.
	Save SaveBlock `json:"save"`

	// Notice is a transient message (an error, a hint). It never carries
	// mechanical state.
	Notice *Notice `json:"notice,omitempty"`

	// Error, when set, means this screen is an error state and the session must
	// not advance world actions from here.
	Error *ErrorBlock `json:"error,omitempty"`
}

// Calendar is the formatted in-game date. The session formats it; the panel
// does not derive it, because deriving a date is a domain calculation.
type Calendar struct {
	// WorldMonth is the raw counter, for debugging and tests.
	WorldMonth int64 `json:"world_month"`
	// Text is the human-readable date, e.g. "第一年 三月".
	Text string `json:"text"`
	// AgeText is the human-readable age, e.g. "二十一岁"。
	AgeText string `json:"age_text"`
	// LifespanRemainingText is the remaining-lifespan phrasing, e.g. "余寿 七十九年".
	LifespanRemainingText string `json:"lifespan_remaining_text"`
	// LifespanLow warns when remaining lifespan is short, so the renderer can
	// emphasise it without deciding the threshold itself.
	LifespanLow bool `json:"lifespan_low"`
}

// StatusBlock is the compact status line, all values precomputed.
type StatusBlock struct {
	RealmText string `json:"realm_text"`
	TierText  string `json:"tier_text"`
	// HP and MP are display strings plus a precomputed ratio for a bar. The
	// ratio is computed by the session, not the renderer, so bar length never
	// becomes a second source of truth.
	HP VitalsView `json:"hp"`
	MP VitalsView `json:"mp"`
	// XP is the tier progress.
	XP ProgressView `json:"xp"`
	// Resources are the tracked currencies, in display order.
	Resources []ResourceView `json:"resources"`
	// Debt is shown separately from resources so it is never mistaken for a
	// negative balance.
	Debt int64 `json:"debt,omitempty"`
	// Mood is the current state of mind with its band name.
	Mood TextValue `json:"mood"`
}

// VitalsView is a current/max bar.
type VitalsView struct {
	Current int64 `json:"current"`
	Max     int64 `json:"max"`
	// Text is the preformatted "current / max".
	Text string `json:"text"`
	// RatioPercent is 0..100, computed by the session so the renderer only
	// scales a bar.
	RatioPercent int `json:"ratio_percent"`
}

// ProgressView is a progress bar with its cap.
type ProgressView struct {
	Current      int64  `json:"current"`
	Max          int64  `json:"max"`
	Text         string `json:"text"`
	RatioPercent int    `json:"ratio_percent"`
	// Full warns that the tier is capped and further cultivation would be
	// wasted, which the design document requires the UI to warn about before
	// the player wastes months.
	Full bool `json:"full"`
	// ProjectedOverflow is how much would be discarded if the player
	// cultivated again, so the warning can be concrete rather than vague.
	ProjectedOverflow int64 `json:"projected_overflow,omitempty"`
}

// ResourceView is one currency's display form.
type ResourceView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value int64  `json:"value"`
}

// TextValue is a label plus a preformatted value.
type TextValue struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// DetailSection is one collapsible block of read-only information.
type DetailSection struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Collapsed tells the renderer the initial state; the session decides it so
	// that the same revision always presents the same way.
	Collapsed bool `json:"collapsed"`
	// Rows are labelled values.
	Rows []DetailRow `json:"rows,omitempty"`
	// Lines are free text, already wrapped by the session if needed.
	Lines []string `json:"lines,omitempty"`
}

// DetailRow is one labelled value inside a detail section.
type DetailRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
	// Hint is an optional explanation.
	Hint string `json:"hint,omitempty"`
}

// EventBlock is the pending event narration and its choices.
type EventBlock struct {
	// InstanceID identifies this occurrence, distinct from previous ones.
	InstanceID string `json:"instance_id"`
	EventID    string `json:"event_id"`
	Title      string `json:"title"`
	// Narration is the event text.
	Narration string `json:"narration"`
	// Choices are the frozen candidate set. Its ids must match the engine's
	// pending event exactly; the session copies rather than derives them.
	Choices []EventChoiceView `json:"choices"`
}

// EventChoiceView is one offered option with its visible cost.
type EventChoiceView struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// CostText is the preformatted cost summary, e.g. "耗时 1 月；消耗 草药×2"。
	CostText string `json:"cost_text"`
	// RiskText names each warned risk with its honest probability, e.g.
	// "受伤 30%". The design document requires this be visible before the
	// choice, so a risky option without risk text is a contract violation.
	RiskText string `json:"risk_text,omitempty"`
	// Risks carries the structured form for a renderer that wants to style it.
	Risks []RiskView `json:"risks,omitempty"`
	// Disabled marks an option whose precondition is unmet.
	Disabled bool `json:"disabled,omitempty"`
	// DisabledReason explains why, so the player is never left guessing.
	DisabledReason string `json:"disabled_reason,omitempty"`
}

// RiskView is one warned hazard.
type RiskView struct {
	Kind            string `json:"kind"`
	ProbabilityText string `json:"probability_text"`
	PreviewText     string `json:"preview_text"`
}

// CombatBlock is the combat round view.
type CombatBlock struct {
	CombatID string `json:"combat_id"`
	Round    int64  `json:"round"`
	// Allies and Enemies are precomputed views; the renderer does not read the
	// engine's combatants directly.
	Allies  []CombatantView `json:"allies"`
	Enemies []CombatantView `json:"enemies"`
	// SpiritEnergy is the per-round combat resource with its refill.
	SpiritEnergy VitalsView `json:"spirit_energy"`
	// TurnText says whose input is awaited.
	TurnText string `json:"turn_text"`
	// Log is the recent round narration.
	Log []string `json:"log,omitempty"`
	// CanFlee reports whether fleeing is currently legal, decided by the
	// session.
	CanFlee bool `json:"can_flee"`
}

// CombatantView is one combatant's display form.
type CombatantView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	HP        VitalsView `json:"hp"`
	MP        VitalsView `json:"mp"`
	RealmText string     `json:"realm_text"`
	// StatusText lists named conditions with their remaining rounds.
	StatusText []string `json:"status_text,omitempty"`
	// UltimateText describes the charge state, since a prepared ultimate can
	// be interrupted and the player must be able to see that.
	UltimateText string `json:"ultimate_text,omitempty"`
}

// BreakthroughBlock is the tribulation / inner-demon choice view.
type BreakthroughBlock struct {
	BreakthroughID string `json:"breakthrough_id"`
	NodeID         string `json:"node_id"`
	Title          string `json:"title"`
	Narration      string `json:"narration"`
	// FromText / ToText describe the transition.
	FromText string `json:"from_text"`
	ToText   string `json:"to_text"`
	// SpentText summarises materials already consumed, so the player knows the
	// attempt is already paid for.
	SpentText string `json:"spent_text"`
	// Choices is the frozen candidate set for this node.
	Choices []EventChoiceView `json:"choices"`
}

// ConfirmationBlock is a proposal awaiting confirmation, with its cost.
type ConfirmationBlock struct {
	Token   string `json:"token"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	// CostText is the preformatted cost. The design document requires the cost
	// of a choice to be visible, so a confirmation that hides its cost is a
	// contract violation.
	CostText string `json:"cost_text"`
	// RiskText names the warned risks of proceeding.
	RiskText string `json:"risk_text,omitempty"`
	// ConfirmLabel / CancelLabel are the button texts.
	ConfirmLabel string `json:"confirm_label"`
	CancelLabel  string `json:"cancel_label"`
	// ExpiresAtRevision is the revision after which this proposal is void.
	ExpiresAtRevision uint64 `json:"expires_at_revision"`
}

// CreationBlock is the character-creation wizard view.
type CreationBlock struct {
	// Author is the byline shown on the first screen. Design 6.1 and ADR-001 §2
	// require the first page to carry it, and R06 keeps it even after the LaTeX
	// output ban. It is a field rather than a renderer constant so a test can
	// assert the screen actually carries it.
	Author string `json:"author"`
	// Step is the 1-based step index.
	Step       int    `json:"step"`
	TotalSteps int    `json:"total_steps"`
	StepTitle  string `json:"step_title"`
	// Fields are the editable inputs for this step.
	Fields []CreationField `json:"fields,omitempty"`
	// Presets are the quick presets, so the default path confirms in three
	// actions or fewer.
	Presets []PresetView `json:"presets,omitempty"`
	// SelectedPresetID names the preset currently applied, so the renderer can
	// mark which one is active.
	SelectedPresetID string `json:"selected_preset_id,omitempty"`
	// BasePointsSpent / BasePointsTotal expose the point budget, so the
	// renderer can show "54/60" without computing it.
	BasePointsSpent int `json:"base_points_spent"`
	BasePointsTotal int `json:"base_points_total"`
	// DraftSaved reports that the draft is persisted and does not occupy a
	// world month.
	DraftSaved bool `json:"draft_saved"`
	// Summary lists the fixed results, so resuming cannot silently re-roll
	// something the player already saw.
	Summary []DetailRow `json:"summary,omitempty"`
	// YaoIntent reports that a 妖族 origin intent was recorded but not yet
	// judged, so the renderer can show the pending state honestly.
	YaoIntent bool `json:"yao_intent,omitempty"`
	// YaoPending reports that the judgment is still outstanding.
	YaoPending bool `json:"yao_pending,omitempty"`
}

// CreationField is one editable creation input.
type CreationField struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value string `json:"value"`
	// Kind is "text" or "choice".
	Kind    string   `json:"kind"`
	Options []string `json:"options,omitempty"`
	// Editable is false for a value already fixed by a roll.
	Editable bool   `json:"editable"`
	Hint     string `json:"hint,omitempty"`
}

// PresetView is one quick-start preset.
type PresetView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// SummaryText describes what the preset chooses.
	SummaryText string `json:"summary_text"`
}

// Option is one selectable action on a screen.
type Option struct {
	// Key is the input key that selects it, e.g. "1" or "c".
	Key   string `json:"key"`
	Label string `json:"label"`
	// CommandKind is what selecting it will submit, so a test can assert that
	// a screen offers only legal kinds for its phase.
	CommandKind string `json:"command_kind"`
	// TargetID / ChoiceID carry the option's argument.
	TargetID string `json:"target_id,omitempty"`
	ChoiceID string `json:"choice_id,omitempty"`
	// CostText is the visible cost, when the option has one.
	CostText string `json:"cost_text,omitempty"`
	// Disabled marks an option the phase forbids.
	Disabled       bool   `json:"disabled,omitempty"`
	DisabledReason string `json:"disabled_reason,omitempty"`
}

// ChangeLine is one "what changed" entry, already phrased.
type ChangeLine struct {
	// Field is the mechanical path the change refers to.
	Field string `json:"field"`
	// Text is the human phrasing, e.g. "修为 +19.5"。
	Text string `json:"text"`
	// Direction is one of "up", "down", "flat", for styling.
	Direction string `json:"direction"`
}

// SaveBlock reports durability.
type SaveBlock struct {
	// State is "durable", "failed" or "not_needed".
	State string `json:"state"`
	// Text is the phrasing shown to the player.
	Text string `json:"text"`
	// LastDurableRevision is the newest revision known to be on disk. When a
	// save fails, the player is told which version they can fall back to.
	LastDurableRevision uint64 `json:"last_durable_revision,omitempty"`
}

// Notice is a transient message.
type Notice struct {
	// Kind is "info", "warn" or "error".
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// ErrorBlock is a terminal error state.
type ErrorBlock struct {
	// Code is the engine's stable error code.
	Code string `json:"code"`
	// Text is the human phrasing.
	Text string `json:"text"`
	// Recoverable reports whether the player may retry without losing the last
	// confirmed version.
	Recoverable bool `json:"recoverable"`
}

// KeyHint documents one input key for a help or footer line.
type KeyHint struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}
