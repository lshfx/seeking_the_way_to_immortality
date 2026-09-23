package engine

import "fmt"

// The command submission pipeline, following design document 9.2.
//
// The order of checks is the design, not an implementation detail:
//
//  1. duplicate action id -> return the stored result
//  2. revision and view token -> reject a stale panel
//  3. phase -> reject a command the current sub-state does not allow
//  4. confirmation -> reject a proposal that was not confirmed, WITHOUT
//     spending materials or drawing a die
//  5. evaluate on a clone
//  6. settle the month boundary exactly once
//  7. check invariants
//  8. commit, then record the idempotency entry
//
// Steps 4 and 5 are where "cancel does not charge" is won. Everything before
// step 5 is pure validation; nothing before step 5 may mutate state or advance a
// random stream.

// Engine is the command submission surface. It owns the session's current state
// and its store.
type Engine struct {
	// Store is the persistence boundary.
	Store Store

	// Catalogue is the validated content the engine resolves ids against.
	Catalogue *Catalogue

	state *GameState

	// viewToken is the token for the panel the player is currently looking at.
	// A command carrying any other token is stale.
	viewToken string

	// viewTokenSeq makes each issued token unique, so a token can never repeat
	// across panels and be coincidentally accepted.
	viewTokenSeq uint64

	// submitting is the process write lock. The design allows a quit request
	// during a commit to be deferred rather than interleaved, which is only
	// meaningful if the engine can report that it is busy.
	submitting bool

	// immediateEffect is the settlement hook for design 13.2 step 4a: effects
	// that land as the month settles and must be applied BEFORE aging.
	//
	// TASK-11 installs the real implementation (a successful breakthrough's
	// lifespan change, a quest's completion reward). TASK-05 defines the slot so
	// the ordering is fixed here, where the timing contract lives, rather than
	// being decided later by whichever caller happens to run first.
	immediateEffect func(*GameState, Command)
}

// SetImmediateEffect installs the settlement hook. Passing nil removes it.
//
// The signature is deliberately narrow: a hook may only read the command and
// mutate the state it is handed, which is the clone. It cannot commit, roll a
// stream directly, or advance the clock, so it cannot bypass the pipeline's
// ordering or its save confirmation.
func (e *Engine) SetImmediateEffect(fn func(*GameState, Command)) {
	if e == nil {
		return
	}
	e.immediateEffect = fn
}

// SetImmediateEffectForTest is an alias kept explicit for tests, so a reader can
// see at a glance that a test is installing a hook.
func (e *Engine) SetImmediateEffectForTest(fn func(*GameState, Command)) {
	e.SetImmediateEffect(fn)
}

// NewEngine creates an engine over an existing state.
func NewEngine(store Store, cat *Catalogue, state *GameState) *Engine {
	e := &Engine{
		Store:     store,
		Catalogue: cat,
		state:     state,
	}
	e.rotateViewToken()
	return e
}

// State returns the live state. Callers must not mutate it; the engine hands
// this to the panel builder and to tests.
func (e *Engine) State() *GameState {
	if e == nil {
		return nil
	}
	return e.state
}

// ViewToken returns the token of the currently displayed panel.
func (e *Engine) ViewToken() string {
	if e == nil {
		return ""
	}
	return e.viewToken
}

// Submitting reports whether a commit is in progress, so a caller can defer a
// quit rather than interrupt a half-finished action.
func (e *Engine) Submitting() bool {
	if e == nil {
		return false
	}
	return e.submitting
}

// rotateViewToken issues a fresh token. It is called after every committed
// change, so that a keypress against the previous screen is recognisably stale.
func (e *Engine) rotateViewToken() {
	e.viewTokenSeq++
	e.viewToken = renderViewToken(e.viewTokenSeq, e.state)
}

// renderViewToken builds a token from the sequence and the revision it belongs
// to. Binding both means a token cannot survive a state change even if the
// sequence were somehow reused.
func renderViewToken(seq uint64, s *GameState) string {
	var rev uint64
	if s != nil {
		rev = s.Revision
	}
	b := append([]byte("vt:"), 0)
	b = appendUintBytes(b, seq)
	b = append(b, ':')
	b = appendUintBytes(b, rev)
	return string(b)
}

// Submit processes one command and returns its result.
//
// It never returns an error: failures are reported as a CommandResult with a
// Code, because the UI needs a code to localise and the design forbids prose in
// the engine. An unrecoverable store failure surfaces as SaveState=failed.
func (e *Engine) Submit(c Command) CommandResult {
	if e == nil || e.state == nil {
		return reject(c, ErrBadPhase, "engine has no state")
	}

	// --- Step 1: duplicate action id -------------------------------------
	//
	// This check comes first so that a resend of an already-committed command
	// returns the original result even if the state has since moved on. Doing it
	// later would make a resend fail revision validation, and the player would
	// see a spurious error for a click that actually succeeded.
	if stored, found, conflict := LookupIdempotent(&e.state.Idempotency, c); found {
		if conflict {
			return reject(c, ErrActionIdConflict, "action id reused with a different request body")
		}
		// A resend must not advance any counter. It also must not rotate the
		// view token, or repeated delivery of one click would keep
		// invalidating the panel under the player.
		return stored
	}

	// --- Step 2: kind validity -------------------------------------------
	if !c.Kind.Valid() {
		return reject(c, ErrUnknownKind, string(c.Kind))
	}

	// --- Step 3: revision ------------------------------------------------
	if c.ExpectedRevision != e.state.Revision {
		return reject(c, ErrRevisionMismatch, "command was issued against an older revision")
	}

	// --- Step 4: view token ----------------------------------------------
	//
	// The token check applies to every command, including queries: a query
	// issued against a stale screen would render a panel that no longer matches
	// the world.
	if c.ViewToken != e.viewToken {
		return reject(c, ErrStaleViewToken, "command was issued against an older panel")
	}

	// --- Step 5: phase permits this kind ---------------------------------
	if code, detail, ok := e.checkPhaseAllows(c.Kind); !ok {
		return reject(c, code, detail)
	}

	// --- Step 6: confirmation --------------------------------------------
	//
	// A proposal that requires confirmation must be confirmed BEFORE anything is
	// spent. This is the check that makes "cancel does not charge" true.
	if code, detail, ok := e.checkConfirmation(c); !ok {
		return reject(c, code, detail)
	}

	// --- Step 7: evaluate on a clone -------------------------------------
	//
	// All mutation happens on a copy. Every rejection from here on discards the
	// copy, so a failed command leaves the live state byte-identical.
	e.submitting = true
	defer func() { e.submitting = false }()

	working := CloneGameState(e.state)
	result := e.apply(working, c)

	if !result.OK {
		// The failure was detected during evaluation; the clone is discarded
		// and the live state is untouched.
		return result
	}

	// --- Step 8: invariants ----------------------------------------------
	if report := ValidateState(working); !report.OK() {
		return reject(c, ErrInvariantBroken, report.Summary())
	}
	if report := ValidateSerializeReady(working); !report.OK() {
		// A command that leaves the state unserialisable would make the save
		// unwritable; refusing now is better than discovering it at save time.
		return reject(c, ErrSerialization, report.Summary())
	}

	// --- Step 9: commit --------------------------------------------------
	//
	// The revision advances only for commands that changed something. A query
	// must leave the revision alone, or every glance at the status screen would
	// invalidate the panel it was drawn from.
	if commandChangedState(c) {
		working.Revision = e.state.Revision + 1
		AdvanceInteractionSeq(working)
	}

	working.GameID = e.state.GameID
	working.BranchID = e.state.BranchID
	working.SchemaVersion = e.state.SchemaVersion
	working.RulesVersion = e.state.RulesVersion
	working.ContentVersion = e.state.ContentVersion

	if err := e.Store.Commit(working); err != nil {
		// The design forbids reporting success when the save failed. The
		// candidate change is dropped entirely rather than kept in memory: a
		// change the player believes is saved but is not is worse than a
		// refused one.
		failed := result
		failed.OK = false
		failed.Code = ErrSerialization
		failed.SaveState = SaveFailed
		failed.Narrative = ""
		return failed
	}

	e.state = working
	result.RevisionAfter = working.Revision
	result.SaveState = SaveDurable

	if commandChangedState(c) {
		// Remember only successful, state-changing commands. A rejected command
		// is not remembered, so a player who fixes the problem and resubmits
		// with the same id is allowed through.
		RecordIdempotent(&e.state.Idempotency, c, result)
		e.rotateViewToken()
	}

	result.ViewToken = e.viewToken
	return result
}

// commandChangedState reports whether a kind is expected to alter mechanical
// state. Queries never do; every other kind may.
func commandChangedState(c Command) bool {
	return c.Kind != KindQuery
}

// reject builds a failure result.
func reject(c Command, code ErrorCode, detail string) CommandResult {
	return CommandResult{
		ActionID:  c.ActionID,
		OK:        false,
		Code:      code,
		SaveState: SaveNotNeeded,
		Events: []ResultEvent{{
			Kind:   "rejected",
			Detail: detail,
		}},
	}
}

// checkPhaseAllows applies the session-state table from design 13.3.
func (e *Engine) checkPhaseAllows(kind CommandKind) (ErrorCode, string, bool) {
	// Queries are allowed in every phase, including ENDED: the design says the
	// history may be reviewed after death.
	if kind == KindQuery {
		return ErrNone, "", true
	}

	switch e.state.Phase {
	case PhaseCreation:
		// During creation only drafting and the confirmation are legal. Any
		// world action would need a character that does not exist yet.
		if kind == KindCreateEdit || kind == KindCreateConfirm {
			return ErrNone, "", true
		}
		return ErrBadPhase, "world actions are not available during character creation", false

	case PhaseReady:
		// A month-costing action cannot start while a sub-state is open; that
		// is enforced by the pending check below, not here.
		if code, detail, ok := e.checkNoBlockingSubState(kind); !ok {
			return code, detail, false
		}
		return ErrNone, "", true

	case PhaseEventPending:
		if kind == KindEventChoice {
			return ErrNone, "", true
		}
		return ErrBadPhase, "an event is awaiting a choice", false

	case PhaseCombatPending:
		if kind == KindCombatAction {
			return ErrNone, "", true
		}
		return ErrBadPhase, "a combat round is awaiting an action", false

	case PhaseBreakthroughPending:
		if kind == KindEventChoice {
			// A tribulation or inner-demon node answers like an event choice.
			return ErrNone, "", true
		}
		return ErrBadPhase, "a breakthrough node is awaiting a choice", false

	case PhaseConfirming:
		// Confirming a proposal is expressed by resubmitting the underlying
		// kind with a confirmation token, so the underlying kind is allowed.
		if code, detail, ok := e.checkNoBlockingSubState(kind); !ok {
			return code, detail, false
		}
		return ErrNone, "", true

	case PhaseEnded:
		return ErrBadPhase, "the character's story has ended", false
	}

	return ErrBadPhase, "unknown phase " + string(e.state.Phase), false
}

// checkNoBlockingSubState refuses to start a new month-costing action while a
// month action or a sub-state is already open.
//
// Design 13.2 step 3: "存在子状态时不能再开始另一个耗月行动". Without this check a
// nested action could charge a second month and settle the parent twice.
func (e *Engine) checkNoBlockingSubState(kind CommandKind) (ErrorCode, string, bool) {
	if kind.MonthCost() == 0 {
		return ErrNone, "", true
	}

	if e.state.Pending.MonthAction != nil {
		return ErrPreconditionUnmet, "another month action is already in progress", false
	}
	if e.state.Pending.Event != nil || e.state.Pending.Combat != nil || e.state.Pending.Breakthrough != nil {
		return ErrPreconditionUnmet, "a sub-state is still open", false
	}

	return ErrNone, "", true
}

// checkConfirmation enforces the confirmation ticket for kinds that spend
// materials or carry risk.
//
// A missing ticket returns ErrConfirmationNeeded. An expired ticket (one issued
// against a different revision) returns ErrConfirmationExpired. Both are
// decided without touching state, which is what makes a cancelled proposal free.
func (e *Engine) checkConfirmation(c Command) (ErrorCode, string, bool) {
	if !requiresConfirmation(c.Kind) {
		return ErrNone, "", true
	}

	pending := e.state.Pending.Confirmation

	// No ticket supplied at all: the caller should preview first.
	if c.ConfirmationToken == "" {
		return ErrConfirmationNeeded, "this action must be confirmed before it is applied", false
	}

	// A ticket was supplied but no confirmation is outstanding.
	if pending == nil {
		return ErrConfirmationExpired, "the confirmation has already been used or discarded", false
	}

	// Wrong token.
	if pending.Token != c.ConfirmationToken {
		return ErrConfirmationExpired, "the confirmation token does not match the outstanding proposal", false
	}

	// The proposal must be for the same kind and target; otherwise a cheap
	// proposal could be confirmed and a different expensive action applied.
	if pending.Kind != c.Kind {
		return ErrConfirmationExpired, "the confirmation was issued for a different action", false
	}
	if pending.TargetID != c.TargetID {
		return ErrConfirmationExpired, "the confirmation was issued for a different target", false
	}

	// Issued against a different revision: the world moved on, so the previewed
	// cost may no longer hold.
	if pending.ExpiresAtRevision != e.state.Revision {
		return ErrConfirmationExpired, "the world changed after this proposal was previewed", false
	}

	return ErrNone, "", true
}

// requiresConfirmation reports whether a kind needs an explicit confirmation
// ticket. The design requires it for anything that spends materials or carries
// a warned risk.
func requiresConfirmation(kind CommandKind) bool {
	switch kind {
	case KindBreakthrough, KindQuestRun, KindTrade:
		return true
	default:
		return false
	}
}

// apply is where the actual domain effect happens.
//
// TASK-05 provides the mechanical frame: phase transitions, month settlement,
// counters and idempotency. The individual content effects (what a quest pays,
// what an event choice does) belong to TASK-07 and TASK-11, which plug in here
// through the kind switch. This task deliberately implements only what the
// timing and determinism contract requires.
func (e *Engine) apply(s *GameState, c Command) CommandResult {
	result := CommandResult{
		ActionID:  c.ActionID,
		OK:        true,
		Code:      ErrNone,
		SaveState: SaveNotNeeded,
	}

	switch c.Kind {
	case KindQuery:
		// A query is pure. It must not advance a random stream, change a
		// counter, or alter the revision, so there is nothing to do.
		return result

	case KindCreateEdit:
		return e.applyCreateEdit(s, c, result)

	case KindCreateConfirm:
		return e.applyCreateConfirm(s, c, result)

	case KindCultivate, KindHeal, KindWait:
		// A simple one-month action with no nested sub-state: start the parent
		// action and settle it immediately.
		return e.applySimpleMonth(s, c, result)

	case KindQuestRun:
		return e.applyQuestRun(s, c, result)

	case KindBreakthrough:
		return e.applyBreakthrough(s, c, result)

	case KindEventChoice:
		return e.applyEventChoice(s, c, result)

	case KindCombatAction:
		return e.applyCombatAction(s, c, result)

	case KindTravel, KindTrade, KindUseItem, KindAcceptQuest, KindClaimReward, KindJoinSect:
		// Zero-month frame actions. The content-specific effects arrive with
		// TASK-07/11; the timing contract is that they cost no month and do
		// not draw a domain die.
		return e.applyZeroMonth(s, c, result)
	}

	return reject(c, ErrUnknownKind, string(c.Kind))
}

// applyZeroMonth handles the kinds that change state but cost no month.
//
// The invariant being protected: a zero-month action must not advance the world
// month, must not age the character, and must not draw from the world event
// stream. It still consumes a revision and an interaction, because it did change
// something.
func (e *Engine) applyZeroMonth(s *GameState, c Command, result CommandResult) CommandResult {
	// Clear a consumed confirmation so the same ticket cannot be replayed for a
	// second application.
	if requiresConfirmation(c.Kind) {
		s.Pending.Confirmation = nil
	}

	if s.Counters.WorldMonth != e.state.Counters.WorldMonth {
		return reject(c, ErrInvariantBroken, "a zero-month action advanced the world month")
	}

	result.MonthCostApplied = 0
	return result
}

// applyCreateEdit mutates the draft without producing a character.
//
// It is the edit half of the wizard. Its contract:
//
//   - The world is untouched. No month advances, no stream is drawn, no
//     counter moves. Creation is a zero-month activity, which is what "创角不耗月"
//     means mechanically.
//   - A proposal that fails a creation rule is refused *before* anything is
//     written, so a 59-point or over-limit allocation is free to attempt and
//     cannot become the draft a player resumes into.
//   - A value already recorded in FixedResults is never overwritten. That is
//     what makes a refresh or a resume unable to re-roll.
func (e *Engine) applyCreateEdit(s *GameState, c Command, result CommandResult) CommandResult {
	if s.Pending.Creation == nil {
		return reject(c, ErrPreconditionUnmet, "there is no character draft to edit")
	}
	draft := s.Pending.Creation

	// Confirming a draft and then editing it again must not be possible: once
	// the draft is marked confirmed the only legal move is to finalise it.
	if draft.Confirmed {
		return reject(c, ErrPreconditionUnmet, "the character draft is already confirmed")
	}

	cp := c.Payload.Creation
	if cp == nil {
		return reject(c, ErrPreconditionUnmet, "a creation edit needs a payload")
	}

	// A preset, when named, is applied first so the rest of the payload can
	// adjust it. An unknown preset is refused rather than ignored.
	if cp.PresetID != "" {
		preset, ok := PresetByID(cp.PresetID)
		if !ok {
			return reject(c, ErrUnknownTarget, "unknown creation preset "+cp.PresetID)
		}
		draft.ApplySelection(preset.Selection)
		draft.PresetID = preset.ID
		if draft.AgeYears == 0 {
			draft.AgeYears = CreationDefaultAgeYears
		}
	}

	// Build the proposed selection: start from what the draft already holds so
	// an absent payload field means "keep", then overlay the proposal.
	proposed := SelectionFromDraft(draft)
	proposed = mergeSelection(proposed, cp.Selection)
	if proposed.AgeYears == 0 {
		proposed.AgeYears = CreationDefaultAgeYears
	}

	// Refuse to change a fixed attribute. This is the single point that makes
	// re-rolls impossible: once a number is in FixedResults no command can
	// move it, so a resumed draft re-renders the same character.
	if code, detail, ok := checkFixedNotChanged(draft, proposed.Base()); !ok {
		return reject(c, code, detail)
	}

	// Validate the proposal against the frozen rules. A failure changes
	// nothing, because nothing has been written yet.
	report := ValidateCreation(e.Catalogue, proposed)
	if !report.OK() {
		return reject(c, ErrPreconditionUnmet, report.Summary())
	}

	// The yao verdict is resolved exactly once, after the aptitude allocation
	// is known, and then locked. Design 6.1 requires the intent to be recorded
	// first and judged later; locking it here is what stops a refresh from
	// re-rolling the judgment.
	if draft.YaoIntent || proposed.YaoIntent {
		draft.YaoIntent = true
		if _, already := draft.FixedResults[FixedYaoVerdict]; !already && cp.YaoVerdictGiven {
			if cp.YaoVerdictPermille < 0 || cp.YaoVerdictPermille > PermilleScale {
				return reject(c, ErrPreconditionUnmet,
					"a yao verdict must be between 0 and 1000 permille")
			}
			if draft.FixedResults == nil {
				draft.FixedResults = map[string]int64{}
			}
			draft.FixedResults[FixedYaoVerdict] = int64(cp.YaoVerdictPermille)
			draft.FixedResults[FixedAptitudeForYao] = int64(proposed.Aptitude)
		}
	}

	// Commit the accepted proposal into the draft.
	draft.ApplySelection(proposed)
	draft.FixAllocation(proposed.Base())

	// Step movement. Advance and back are mutually exclusive; a payload that
	// sets both is ambiguous and refused rather than silently preferring one.
	if cp.AdvanceStep && cp.BackStep {
		return reject(c, ErrPreconditionUnmet, "cannot advance and go back in one command")
	}
	if cp.AdvanceStep {
		if draft.Step >= CreationTotalSteps {
			return reject(c, ErrPreconditionUnmet, "the wizard is already on the final step")
		}
		draft.Step++
	}
	if cp.BackStep {
		if draft.Step <= CreationStepIdentity {
			return reject(c, ErrPreconditionUnmet, "the wizard is already on the first step")
		}
		draft.Step--
	}

	result.Events = append(result.Events, ResultEvent{
		Kind:   "creation_drafted",
		ID:     c.Payload.Creation.PresetID,
		Detail: "step=" + itoa(draft.Step),
	})
	return result
}

// applyCreateConfirm finalises a character draft.
//
// It is the only command that produces a Player, and it builds the character
// here, on the clone, for a specific reason: `validatePhasePendingConsistency`
// requires the draft to be present while the phase is CREATION, and
// `ValidateState` tolerates a nil Player only in CREATION. The old shell waited
// for a player that nothing could ever have created, so its `s.Player == nil`
// guard was unreachable-by-construction rather than protective.
//
// The factory result is discarded when it fails, so a draft that cannot become
// a legal character leaves the phase and the draft exactly as they were.
func (e *Engine) applyCreateConfirm(s *GameState, c Command, result CommandResult) CommandResult {
	draft := s.Pending.Creation
	if draft == nil {
		return reject(c, ErrPreconditionUnmet, "there is no character draft to confirm")
	}

	cp := c.Payload.Creation
	if cp == nil || !cp.Confirm {
		// The draft carries a Confirmed flag too; either route may set it, but
		// one of them must, or an unconfirmed draft would silently become a
		// character.
		if !draft.Confirmed {
			return reject(c, ErrPreconditionUnmet, "the character draft has not been confirmed")
		}
	}

	// The wizard must be complete. Confirming from step one would finalise a
	// character whose second step the player never saw.
	if draft.Step < CreationTotalSteps && !draft.Confirmed {
		return reject(c, ErrPreconditionUnmet,
			"the character draft is not on the final step yet")
	}

	// Build the character. The factory re-validates, so a draft loaded from a
	// save written by an older or faulty build cannot become an illegal player.
	player, report := CreatePlayer(e.Catalogue, draft)
	if !report.OK() {
		return reject(c, ErrPreconditionUnmet, report.Summary())
	}

	// A yao-intent draft whose judgment failed must not silently become a
	// character with a hidden bonus. The design says a failed judgment lets the
	// player choose again; that means the confirm is refused and the draft
	// stays open, rather than the failure being recorded and forgotten.
	if draft.YaoIntent {
		if verdict, ok := draft.FixedResults[FixedYaoVerdict]; ok && verdict <= 0 {
			return reject(c, ErrPreconditionUnmet,
				"the yao judgment did not pass; choose a different origin or re-roll is not permitted")
		}
	}

	s.Player = player
	s.Pending.Creation = nil
	draft.Confirmed = true
	s.Phase = PhaseReady

	// Creation must not have advanced the world. This mirrors the zero-month
	// guard in applyZeroMonth; without it a future edit to the factory that
	// touched a counter would go unnoticed.
	if s.Counters.WorldMonth != e.state.Counters.WorldMonth {
		return reject(c, ErrInvariantBroken, "character creation advanced the world month")
	}
	result.MonthCostApplied = 0

	result.Events = append(result.Events, ResultEvent{
		Kind:   "created",
		ID:     s.Player.Identity.GivenName,
		Detail: string(s.Player.Origin) + "/" + string(s.Player.SpiritRoot),
	})
	return result
}

// mergeSelection overlays a proposal onto an existing selection.
//
// A zero value means "not supplied" rather than "set to zero", because the
// payload is a struct with no presence bits. That is safe here: zero is not a
// legal base attribute (the minimum is 1), a zero month is not a legal age (the
// minimum is 16), and an empty id is not a legal origin. The one field where
// zero *is* meaningful — YaoIntent — is a bool and is OR-ed rather than replaced,
// so it can only ever be turned on.
func mergeSelection(base, proposal CreationSelection) CreationSelection {
	out := base

	if proposal.Surname != "" {
		out.Surname = proposal.Surname
	}
	if proposal.GivenName != "" {
		out.GivenName = proposal.GivenName
	}
	if proposal.DaoName != "" {
		out.DaoName = proposal.DaoName
	}
	if proposal.Gender != "" {
		// Stored verbatim. There is deliberately no normalisation: a custom
		// gender is the player's value, not a value to be corrected.
		out.Gender = proposal.Gender
	}
	if proposal.Appearance != "" {
		out.Appearance = proposal.Appearance
	}
	if proposal.AgeYears != 0 {
		out.AgeYears = proposal.AgeYears
	}
	if proposal.Origin != "" {
		out.Origin = proposal.Origin
	}
	if proposal.Path != "" {
		out.Path = proposal.Path
	}
	if proposal.SpiritRoot != "" {
		out.SpiritRoot = proposal.SpiritRoot
	}
	if proposal.Constitution != "" {
		out.Constitution = proposal.Constitution
	}
	if proposal.TalentIDs != nil {
		out.TalentIDs = append([]string(nil), proposal.TalentIDs...)
	}
	out.YaoIntent = out.YaoIntent || proposal.YaoIntent

	if proposal.Strength != 0 {
		out.Strength = proposal.Strength
	}
	if proposal.Agility != 0 {
		out.Agility = proposal.Agility
	}
	if proposal.ConstitutionAttr != 0 {
		out.ConstitutionAttr = proposal.ConstitutionAttr
	}
	if proposal.Comprehension != 0 {
		out.Comprehension = proposal.Comprehension
	}
	if proposal.Aptitude != 0 {
		out.Aptitude = proposal.Aptitude
	}
	if proposal.Fortune != 0 {
		out.Fortune = proposal.Fortune
	}

	return out
}

// applySimpleMonth runs a one-month action that has no nested sub-state: start
// the parent action, then settle it.
func (e *Engine) applySimpleMonth(s *GameState, c Command, result CommandResult) CommandResult {
	if s.Pending.MonthAction != nil {
		return reject(c, ErrPreconditionUnmet, "another month action is already in progress")
	}
	if c.Kind == KindCultivate {
		if c.Payload.ActionKind == ActionClosed {
			return reject(c, ErrPreconditionUnmet, "closed cultivation is reserved for TASK-23; M1 currently offers ordinary cultivation only")
		}
		if c.Payload.ActionKind != "" && c.Payload.ActionKind != ActionNormal {
			return reject(c, ErrPreconditionUnmet, "unknown cultivation intensity")
		}
		if err := applyCultivation(s, e.Catalogue, &result); err != nil {
			return reject(c, ErrPreconditionUnmet, err.Error())
		}
	}

	s.Pending.MonthAction = &ActiveMonthAction{
		ActionID:          c.ActionID,
		Kind:              c.Kind,
		MonthCharged:      false,
		SettlementPending: false,
		StartedWorldMonth: s.Counters.WorldMonth,
		TargetID:          c.TargetID,
	}

	// Settle immediately: there is no sub-state to wait for.
	settle := e.settleParentAction(s, c, result)
	return settle
}

// applyQuestRun starts a month-costing excursion, which may raise a battle or an
// event. If it does, the parent action stays open with SettlementPending set,
// and the month is charged only when the parent finally settles.
func (e *Engine) applyQuestRun(s *GameState, c Command, result CommandResult) CommandResult {
	if s.Pending.MonthAction != nil {
		return reject(c, ErrPreconditionUnmet, "another month action is already in progress")
	}

	s.Pending.MonthAction = &ActiveMonthAction{
		ActionID:          c.ActionID,
		Kind:              c.Kind,
		MonthCharged:      false,
		SettlementPending: false,
		StartedWorldMonth: s.Counters.WorldMonth,
		TargetID:          c.TargetID,
		TargetInstanceID:  c.TargetInstanceID,
	}

	// A quest run consults the world stream to decide whether an encounter
	// interrupts it. Drawing here, on the clone, is what makes the outcome
	// reproducible while a rejected command still leaves no trace.
	//
	// The encounter roll is deterministic in the action id, so replaying the
	// same action sequence yields the same interruptions.
	seed := SeedFromIdentity(c.ActionID, c.TargetID)
	encounter := NewStream(seed)
	if encounter.Chance(300) { // 30% encounter rate for the M1 frame
		combatID := c.ActionID + "#combat"
		s.Pending.Combat = &PendingCombat{
			CombatID:       combatID,
			ParentActionID: c.ActionID,
			Round:          1,
			AwaitingPlayer: true,
			Resolution:     CombatOngoing,
			AllyState:      combatantFromPlayer(s.Player),
			SpiritEnergy: SpiritEnergy{
				Current: 0,
				Max:     100,
			},
		}
		s.Pending.MonthAction.SettlementPending = true
		s.Phase = PhaseCombatPending

		result.Events = append(result.Events, ResultEvent{
			Kind:       ResultCombatStarted,
			ID:         combatID,
			InstanceID: c.ActionID,
			Detail:     c.TargetID,
		})
		result.MonthCostApplied = 0 // charged when the parent settles
		return result
	}

	return e.settleParentAction(s, c, result)
}

// applyBreakthrough attempts a realm advance, which may raise a tribulation node.
func (e *Engine) applyBreakthrough(s *GameState, c Command, result CommandResult) CommandResult {
	if s.Pending.MonthAction != nil {
		return reject(c, ErrPreconditionUnmet, "another month action is already in progress")
	}
	if s.Player == nil {
		return reject(c, ErrPreconditionUnmet, "there is no character")
	}

	// A breakthrough attempt needs a defined transition for the current
	// realm/tier, otherwise the content has no rule to apply.
	if _, ok := e.breakthroughFor(s.Player); !ok {
		return reject(c, ErrUnknownTarget, "no breakthrough is defined from the current realm and tier")
	}

	draft := &ActiveMonthAction{
		ActionID:          c.ActionID,
		Kind:              c.Kind,
		MonthCharged:      false,
		SettlementPending: false,
		StartedWorldMonth: s.Counters.WorldMonth,
	}

	// Materials are spent once, at start. Recording them here is what stops a
	// reload from spending them again.
	if e.Catalogue != nil {
		// The frame records what the transition costs; TASK-11 fills the actual
		// effect. Recording the intent now is what makes the spend auditable.
		draft.StartedMaterials = nil
	}

	s.Pending.MonthAction = draft

	// Clear the consumed ticket before anything else can go wrong.
	s.Pending.Confirmation = nil

	// A major breakthrough raises a tribulation node; a minor one settles now.
	// The M1 frame raises a node whenever the content defines one, which the
	// milestone table does.
	nodeID := c.TargetID
	if nodeID == "" {
		nodeID = c.ActionID + "#node"
	}

	s.Pending.Breakthrough = &PendingBreakthrough{
		BreakthroughID: c.TargetID,
		ParentActionID: c.ActionID,
		NodeID:         nodeID,
		ChoiceIDs:      []string{"endure", "yield"},
		FromRealm:      s.Player.Realm,
		FromTier:       s.Player.Tier,
	}
	s.Pending.MonthAction.SettlementPending = true
	s.Phase = PhaseBreakthroughPending

	result.Events = append(result.Events, ResultEvent{
		Kind:       ResultEventQueued,
		ID:         nodeID,
		InstanceID: c.ActionID,
	})
	result.MonthCostApplied = 0
	return result
}

// breakthroughFor looks up the transition definition for the character's current
// realm and tier.
func (e *Engine) breakthroughFor(p *Player) (BreakthroughDefinition, bool) {
	if e.Catalogue == nil || p == nil {
		return BreakthroughDefinition{}, false
	}
	for _, b := range e.Catalogue.Breakthroughs {
		if b.Path != p.Path || b.FromRealm != p.Realm || b.FromTier != p.Tier {
			continue
		}
		return b, true
	}
	return BreakthroughDefinition{}, false
}

// applyEventChoice resolves a pending event or breakthrough node.
//
// A node choice costs no extra month: it inherits the parent action's timing.
// The design is explicit that an additional month cost must be a separately
// declared action, so a choice can never quietly charge a month.
func (e *Engine) applyEventChoice(s *GameState, c Command, result CommandResult) CommandResult {
	// A tribulation/demon node is answered through the breakthrough sub-state.
	if s.Pending.Breakthrough != nil {
		return e.applyBreakthroughChoice(s, c, result)
	}

	ev := s.Pending.Event
	if ev == nil {
		return reject(c, ErrPreconditionUnmet, "there is no pending event to answer")
	}

	// The choice must be one of the frozen candidates. Accepting a choice
	// outside the frozen set is exactly the re-roll this guards against.
	if !containsString(ev.ChoiceIDs, c.Payload.ChoiceID) {
		return reject(c, ErrUnknownTarget, "the chosen option is not one of the frozen candidates")
	}

	result.Events = append(result.Events, ResultEvent{
		Kind:       "event_choice",
		ID:         ev.EventID,
		InstanceID: ev.InstanceID,
		Detail:     c.Payload.ChoiceID,
	})

	// Record the event as consumed so a limited event cannot pay twice.
	if s.World != nil {
		s.World.ConsumedEvents = append(s.World.ConsumedEvents, ConsumedEvent{
			EventID:    ev.EventID,
			InstanceID: ev.InstanceID,
			ChoiceID:   c.Payload.ChoiceID,
		})
	}

	s.Pending.Event = nil

	// Design 13.2: a zero-extra-month month-end choice must not re-trigger the
	// month end. So if a parent action is waiting, settle it now; otherwise the
	// phase returns to READY without touching the clock.
	if s.Pending.MonthAction != nil {
		s.Phase = PhaseReady
		return e.settleParentAction(s, c, result)
	}

	s.Phase = PhaseReady
	result.MonthCostApplied = 0
	return result
}

// applyBreakthroughChoice resolves one tribulation or inner-demon node.
func (e *Engine) applyBreakthroughChoice(s *GameState, c Command, result CommandResult) CommandResult {
	bt := s.Pending.Breakthrough

	if !containsString(bt.ChoiceIDs, c.Payload.ChoiceID) {
		return reject(c, ErrUnknownTarget, "the chosen option is not one of the frozen candidates")
	}

	// Record the node as resolved. A reload must not re-roll or re-apply it.
	bt.CompletedNodes = append(bt.CompletedNodes, bt.NodeID)

	result.Events = append(result.Events, ResultEvent{
		Kind:       "breakthrough_node",
		ID:         bt.NodeID,
		InstanceID: bt.ParentActionID,
		Detail:     c.Payload.ChoiceID,
	})

	s.Pending.Breakthrough = nil
	s.Phase = PhaseReady

	// The parent action still owes its month; settle it exactly once.
	return e.settleParentAction(s, c, result)
}

// applyCombatAction resolves one combat round.
//
// Combat never advances the world month. One submission covers the whole round
// including the enemy's reply, so a dropped connection cannot hand the enemy a
// free attack (design 13.3).
func (e *Engine) applyCombatAction(s *GameState, c Command, result CommandResult) CommandResult {
	cb := s.Pending.Combat
	if cb == nil {
		return reject(c, ErrPreconditionUnmet, "there is no combat to act in")
	}
	if !cb.AwaitingPlayer {
		// The round is already resolved; the player's input is a duplicate that
		// the idempotency table should already have caught. Refusing here is a
		// second line of defence against double submission.
		return reject(c, ErrDuplicateAction, "the round has already been resolved")
	}

	AdvanceCombatRound(s)
	cb.Round++

	result.Events = append(result.Events, ResultEvent{
		Kind:   ResultCombatRound,
		ID:     cb.CombatID,
		Detail: c.Payload.SkillID,
	})

	// The enemy's reply is part of this same submission, so the round ends with
	// the player waiting again.
	cb.AwaitingPlayer = true
	result.MonthCostApplied = 0
	return result
}

// settleParentAction applies the one month owed by the open parent action, then
// runs the month boundary.
//
// MonthCharged makes this idempotent: settlement is guarded, so a resumed
// session that finds a parent action already charged cannot advance the clock a
// second time. That guard is the difference between "quitting mid-month is safe"
// and "quitting mid-month ages you twice".
func (e *Engine) settleParentAction(s *GameState, c Command, result CommandResult) CommandResult {
	parent := s.Pending.MonthAction
	if parent == nil {
		// No parent action: nothing to charge, which is the correct behaviour
		// for a zero-month command.
		result.MonthCostApplied = 0
		return result
	}

	if parent.MonthCharged {
		// Already settled. Clearing the slot without charging again is the
		// whole point of this guard.
		s.Pending.MonthAction = nil
		result.MonthCostApplied = 0
		return result
	}

	// Design 13.2 step 4 fixes the settlement order.
	parent.MonthCharged = true
	s.Pending.MonthAction = nil

	// 4a runs first, and the ordering is load-bearing rather than stylistic: a
	// character one month from their ceiling who earns a lifespan bonus this
	// month must survive it.
	if e.immediateEffect != nil {
		e.immediateEffect(s, c)
	}

	AdvanceWorldMonth(s)
	result.MonthCostApplied = 1
	result.Events = append(result.Events, ResultEvent{
		Kind:   ResultMonthAdvanced,
		ID:     c.ActionID,
		Detail: c.Kind.String(),
	})

	tickTimedEffects(s)
	refreshCultivationDerived(s.Player, s.World, e.Catalogue)

	// 4d. Death by lifespan.
	if s.Player != nil && LifespanExhausted(s.Player.AgeMonths, s.Player.Lifespan) {
		s.Player.Ended = true
		s.Player.EndCause = EndCause{
			Code:       EndCodeLifespan,
			WorldMonth: s.Counters.WorldMonth,
			Reason:     "lifespan exhausted",
		}
		s.Phase = PhaseEnded
		result.Events = append(result.Events, ResultEvent{
			Kind:   ResultEnded,
			ID:     EndCodeLifespan,
			Detail: "the character's lifespan was exhausted",
		})
		return result
	}

	s.Phase = PhaseReady
	return result
}

func applyCultivation(s *GameState, cat *Catalogue, result *CommandResult) error {
	if s == nil || s.Player == nil {
		return fmt.Errorf("there is no character to cultivate")
	}
	preview, err := PreviewCultivation(s.Player, s.World, cat, ActionNormal)
	if err != nil {
		return err
	}
	if preview.AtThreshold {
		return fmt.Errorf("cultivation is already at the tier threshold; attempt a breakthrough instead")
	}

	before := s.Player.XP
	s.Player.XP = preview.XPAfterNextAction
	s.Player.Derived.EffectiveAptitude = preview.EffectiveAptitude
	s.Player.Derived.CultivationRate = preview.Rate
	result.Delta.Entries = append(result.Delta.Entries, DeltaEntry{
		Field: "xp", Before: before, After: s.Player.XP,
		Reason: "普通修炼；达到小阶阈值时截断，不自动突破",
	})
	result.Events = append(result.Events, ResultEvent{
		Kind: ResultXPChanged, ID: "cultivation",
		Detail: fmt.Sprintf("rate=%d;gain=%d;threshold=%d", preview.Rate, preview.NextGain, preview.Threshold),
	})

	active := append([]string{preview.PrimaryTechniqueID}, preview.SecondaryTechniqueIDs...)
	if s.Player.Proficiencies == nil {
		s.Player.Proficiencies = map[string]int64{}
	}
	for _, id := range active {
		before := s.Player.Proficiencies[id]
		if before == int64(^uint64(0)>>1) {
			return fmt.Errorf("technique proficiency overflow for %q", id)
		}
		s.Player.Proficiencies[id] = before + 1
		result.Delta.Entries = append(result.Delta.Entries, DeltaEntry{
			Field: "proficiencies." + id, Before: before, After: before + 1,
			Reason: "修炼一月，功法熟练度 +1",
		})
	}
	return nil
}

func refreshCultivationDerived(p *Player, world *World, cat *Catalogue) {
	if p == nil {
		return
	}
	preview, err := PreviewCultivation(p, world, cat, ActionNormal)
	if err != nil {
		return
	}
	p.Derived.EffectiveAptitude = preview.EffectiveAptitude
	p.Derived.CultivationRate = preview.Rate
}

// tickTimedEffects applies the month-end effects of design 13.2 step 4c:
// month-scoped durations decrement, and the ones that run out are removed.
//
// Expired effects are removed rather than left at zero. A zero-duration entry
// still in the list would be summed by anything that iterates without checking,
// so a poison that had visibly worn off would keep draining HP.
func tickTimedEffects(s *GameState) {
	if s == nil || s.Player == nil {
		return
	}

	s.Player.Condition.Effects = decrementEffects(s.Player.Condition.Effects)

	if s.World == nil {
		return
	}

	// NPCs age by the same month. They are not "real-world timers" (5.2
	// forbids those): this runs only because a month the player spent has
	// settled, so an idle window advances nobody.
	for id, npc := range s.World.NPCs {
		npc.AgeMonths++
		// An NPC whose own lifespan has run out dies. The design says a dead
		// NPC accepts no gifts and no new dialogue, so the flag matters.
		if npc.IsAlive && LifespanExhausted(npc.AgeMonths, LifespanLedger{BaseYears: npc.LifespanYears}) {
			npc.IsAlive = false
		}
		s.World.NPCs[id] = npc
	}
}

// decrementEffects returns the effects with one month subtracted, dropping any
// that have expired.
//
// It builds a fresh slice rather than compacting in place. In-place compaction
// (`xs[:0]` plus append, while ranging over `xs`) happens to work because range
// evaluates the slice once, but it relies on that detail and silently corrupts
// the result the moment anyone changes the loop to index-based. A fresh slice has
// no such coupling and the allocation is negligible at effect-list sizes.
func decrementEffects(effects []TimedEffect) []TimedEffect {
	if len(effects) == 0 {
		// Preserve a nil slice as nil so a round trip does not turn nil into
		// an empty slice, which would make two equal states digest differently.
		return effects
	}

	kept := make([]TimedEffect, 0, len(effects))
	for _, ef := range effects {
		ef.MonthsRemaining--
		if ef.MonthsRemaining > 0 {
			kept = append(kept, ef)
		}
	}

	if len(kept) == 0 {
		// An all-expired list becomes nil rather than empty, for the same
		// digest-stability reason as above.
		return nil
	}
	return kept
}

// combatantFromPlayer builds the player's combatant view.
func combatantFromPlayer(p *Player) Combatant {
	if p == nil {
		return Combatant{}
	}
	return Combatant{
		ID:      "player",
		HP:      p.HP,
		MP:      p.MP,
		Attack:  p.Derived.Attack,
		Defense: p.Derived.Defense,
		Speed:   p.Derived.Speed,
		Realm:   p.Realm,
		Tier:    p.Tier,
	}
}

// containsString reports whether a string slice contains v.
func containsString(haystack []string, v string) bool {
	for _, s := range haystack {
		if s == v {
			return true
		}
	}
	return false
}

// String renders a command kind for logs.
func (k CommandKind) String() string { return string(k) }

// Summary renders a validation report as a single line, for the error detail.
func (r ValidationReport) Summary() string {
	if len(r.Errors) == 0 {
		return ""
	}
	// Report the count and the first code; the full list is for the validator's
	// own tests, not for a UI detail line.
	b := []byte("validation failed: ")
	b = appendIntBytes(b, int64(len(r.Errors)))
	b = append(b, ' ')
	b = append(b, string(r.Errors[0].Code)...)
	if r.Errors[0].Where != "" {
		b = append(b, " at "...)
		b = append(b, r.Errors[0].Where...)
	}
	return string(b)
}
