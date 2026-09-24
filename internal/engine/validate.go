package engine

// The validator is TASK-04's main deliverable of substance. It enforces, as
// machine-checked rules, what the design document states in prose:
//
//   - duplicate ids fail;
//   - dangling references fail;
//   - negative costs fail;
//   - a missing breakthrough table fails (never silently defaults);
//   - all ten realms carry their lifespan, so crystallizing and nascent soul
//     cannot be forgotten;
//   - configuring a high realm does not open it, only M1Paths does;
//   - unapproved parameters must be explicitly marked rather than passing as
//     settled balance.
//
// Every failure is reported as a ValidationError with a code and a location,
// so a broken content pack is diagnosable rather than merely rejected.

import (
	"fmt"
	"sort"
	"strings"
)

// ValidationCode classifies one content defect.
type ValidationCode string

// Validation codes.
const (
	VErrDuplicateID         ValidationCode = "DUPLICATE_ID"
	VErrDanglingRef         ValidationCode = "DANGLING_REFERENCE"
	VErrNegativeCost        ValidationCode = "NEGATIVE_COST"
	VErrMissingRealm        ValidationCode = "MISSING_REALM"
	VErrMissingBreakthrough ValidationCode = "MISSING_BREAKTHROUGH"
	VErrMissingProvenance   ValidationCode = "MISSING_PROVENANCE"
	VErrUnapprovedParam     ValidationCode = "UNAPPROVED_PARAMETER"
	VErrPathNotOpen         ValidationCode = "PATH_NOT_OPEN_IN_M1"
	VErrUnknownEnum         ValidationCode = "UNKNOWN_ENUM_VALUE"
	VErrBadCondition        ValidationCode = "MALFORMED_CONDITION"
	VErrRiskNotPreviewed    ValidationCode = "RISK_NOT_PREVIEWED"
	VErrNonPositiveAmount   ValidationCode = "NON_POSITIVE_AMOUNT"
	VErrBadChain            ValidationCode = "BROKEN_EVENT_CHAIN"
	VErrBadTravelGraph      ValidationCode = "BROKEN_TRAVEL_GRAPH"
	VErrTutorialReward      ValidationCode = "TUTORIAL_REPEATABLE_REWARD"
	VErrDuplicateOrder      ValidationCode = "DUPLICATE_REALM_ORDER"
	VErrBadGrade            ValidationCode = "UNKNOWN_GRADE"
	VErrBadProvenance       ValidationCode = "UNKNOWN_PROVENANCE"
	// VErrNodeText marks a node whose own display text is missing or the wrong
	// length. Design 14 asks for one to three sentences; a node with no text is
	// a menu, not a scene.
	VErrNodeText ValidationCode = "BAD_NODE_TEXT"
	// VErrUnreachableGoal marks an early goal with a branch no content can
	// reach, or whose only success path requires sect membership.
	VErrUnreachableGoal ValidationCode = "UNREACHABLE_GOAL_BRANCH"
	// VErrPreconditionUnmet marks a state whose structure cannot support the
	// phase it claims to be in.
	VErrPreconditionUnmet ValidationCode = "PRECONDITION_UNMET"
	// VErrInvariantBroken marks a violated design-document 9.4 invariant.
	VErrInvariantBroken ValidationCode = "INVARIANT_BROKEN"
	// VErrSerialization marks a sub-state that could not survive a round trip.
	VErrSerialization ValidationCode = "SERIALIZATION_FAILED"
)

// ValidationError is one defect, with enough context to locate it.
type ValidationError struct {
	Code ValidationCode `json:"code"`
	// Where names the offending element, e.g. "items[3].id".
	Where string `json:"where"`
	// ID is the offending id when one is known.
	ID string `json:"id,omitempty"`
	// Detail explains the defect in one sentence.
	Detail string `json:"detail"`
}

// Error implements the error interface.
func (e ValidationError) Error() string {
	if e.ID != "" {
		return fmt.Sprintf("%s at %s (id=%s): %s", e.Code, e.Where, e.ID, e.Detail)
	}
	return fmt.Sprintf("%s at %s: %s", e.Code, e.Where, e.Detail)
}

// ValidationReport collects every defect found. Validation is total: it does
// not stop at the first error, so a content author sees the whole list.
type ValidationReport struct {
	Errors []ValidationError `json:"errors"`
}

// OK reports whether the catalogue passed.
func (r ValidationReport) OK() bool { return len(r.Errors) == 0 }

// Error renders the report as a single error, or nil when clean.
func (r ValidationReport) Error() error {
	if r.OK() {
		return nil
	}
	parts := make([]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		parts = append(parts, e.Error())
	}
	return fmt.Errorf("content validation failed with %d error(s):\n  %s",
		len(r.Errors), strings.Join(parts, "\n  "))
}

// Codes returns the distinct defect codes, sorted, for test assertions that
// should not depend on ordering or counts.
func (r ValidationReport) Codes() []ValidationCode {
	seen := make(map[ValidationCode]bool, len(r.Errors))
	for _, e := range r.Errors {
		seen[e.Code] = true
	}
	out := make([]ValidationCode, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Has reports whether the report contains a defect with the given code.
func (r ValidationReport) Has(code ValidationCode) bool {
	for _, e := range r.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

// CountOf returns how many defects carry the given code.
func (r ValidationReport) CountOf(code ValidationCode) int {
	n := 0
	for _, e := range r.Errors {
		if e.Code == code {
			n++
		}
	}
	return n
}

// add appends a defect.
func (r *ValidationReport) add(code ValidationCode, where, id, detail string) {
	r.Errors = append(r.Errors, ValidationError{
		Code: code, Where: where, ID: id, Detail: detail,
	})
}

// ValidateCatalogue checks a complete catalogue and returns every defect.
//
// The order of checks is deliberate: identity and provenance first, because a
// duplicate id invalidates every later reference test; then references; then
// semantic rules.
func ValidateCatalogue(c *Catalogue) ValidationReport {
	var r ValidationReport
	if c == nil {
		r.add(VErrMissingRealm, "catalogue", "", "catalogue is nil")
		return r
	}

	validateVersions(c, &r)
	validateRealms(c, &r)
	validateProvenance(c, &r)
	validateOrigins(c, &r)
	validateSpiritRoots(c, &r)
	validateTalents(c, &r)
	validateItems(c, &r)
	validateTechniques(c, &r)
	validateAfflictions(c, &r)
	validateInjuryBands(c, &r)
	validateKarmaTiers(c, &r)
	validateSkills(c, &r)
	validateLocations(c, &r)
	validateNPCs(c, &r)
	validateQuests(c, &r)
	validateSects(c, &r)
	validateDialogues(c, &r)
	validateBreakthroughs(c, &r)
	validateEvents(c, &r)
	validateEarlyGoals(c, &r)
	validateM1Paths(c, &r)

	return r
}

// validateDialogues checks dialogue identity and that each node is anchored to
// a declared NPC.
func validateDialogues(c *Catalogue, r *ValidationReport) {
	npcIDs := map[string]bool{}
	for _, n := range c.NPCs {
		npcIDs[n.ID] = true
	}
	seen := map[string]int{}
	for i, d := range c.Dialogues {
		where := fmt.Sprintf("dialogues[%d]", i)
		if d.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "dialogue id must not be empty")
			continue
		}
		if prev, dup := seen[d.ID]; dup {
			r.add(VErrDuplicateID, where+".id", d.ID,
				fmt.Sprintf("dialogue already declared at dialogues[%d]", prev))
			continue
		}
		seen[d.ID] = i
		if d.NPCID == "" {
			r.add(VErrMissingProvenance, where+".npc_id", d.ID, "dialogue must name its NPC")
		} else if !npcIDs[d.NPCID] {
			r.add(VErrDanglingRef, where+".npc_id", d.NPCID, "dialogue NPC is not declared")
		}
		validatePreconditions(where+".requires", d.Requires, r)
	}
}

// validateVersions rejects a catalogue with no version, since a save cannot
// name the content it depends on otherwise.
func validateVersions(c *Catalogue, r *ValidationReport) {
	if c.Version <= 0 {
		r.add(VErrMissingProvenance, "version", "",
			"catalogue version must be positive; a save cannot reference unversioned content")
	}
}

// validateRealms enforces that all ten realms exist exactly once, in order,
// with their lifespan and base numbers present.
//
// The lifespan check is the one the acceptance criteria call out explicitly:
// crystallizing and nascent soul are the two realms whose base rates are trial
// extrapolations in the source design, so they are the likeliest to be dropped
// by accident. Requiring all ten makes that impossible.
func validateRealms(c *Catalogue, r *ValidationReport) {
	byID := make(map[Realm]int, len(c.Realms))
	orders := make(map[int]Realm, len(c.Realms))

	for i, realm := range c.Realms {
		where := fmt.Sprintf("realms[%d]", i)
		if !isDeclaredRealm(realm.ID) {
			r.add(VErrUnknownEnum, where+".id", string(realm.ID),
				"not one of the ten declared realms")
			continue
		}
		if prev, dup := byID[realm.ID]; dup {
			r.add(VErrDuplicateID, where+".id", string(realm.ID),
				fmt.Sprintf("realm already declared at realms[%d]", prev))
			continue
		}
		byID[realm.ID] = i

		if other, dup := orders[realm.Order]; dup {
			r.add(VErrDuplicateOrder, where+".order", string(realm.ID),
				fmt.Sprintf("order %d already used by realm %s", realm.Order, other))
		}
		orders[realm.Order] = realm.ID

		// Lifespan is mandatory for every realm, with provenance.
		if realm.LifespanYears.Provenance == "" {
			r.add(VErrMissingProvenance, where+".lifespan_years", string(realm.ID),
				"lifespan must declare where it came from")
		} else if !realm.LifespanYears.Provenance.Valid() {
			r.add(VErrBadProvenance, where+".lifespan_years.provenance",
				string(realm.LifespanYears.Provenance), "unknown provenance")
		}
		if realm.LifespanYears.Value <= 0 {
			r.add(VErrNonPositiveAmount, where+".lifespan_years", string(realm.ID),
				"lifespan must be positive")
		}
		validateConfigValue(where+".month_base", realm.MonthBase, false, r)
		validateConfigValue(where+".tier_threshold", realm.TierThreshold, false, r)
	}

	for _, want := range RealmOrder {
		if _, ok := byID[want]; !ok {
			r.add(VErrMissingRealm, "realms", string(want),
				"realm is missing from the catalogue; all ten must be configured, "+
					"including crystallizing and nascent soul whose base rates are trial values")
		}
	}
}

// isDeclaredRealm reports whether id is one of the ten realms.
func isDeclaredRealm(id Realm) bool {
	for _, r := range RealmOrder {
		if r == id {
			return true
		}
	}
	return false
}

// validateConfigValue checks provenance presence and, when required, that the
// value is approved. A trial or pending value is legal but must not be silent:
// requireNote forces the author to justify it.
func validateConfigValue(where string, v ConfigValue, requireApproved bool, r *ValidationReport) {
	if v.Provenance == "" {
		r.add(VErrMissingProvenance, where, "",
			"configured number has no provenance; manuscript and trial values must not be mixed silently")
		return
	}
	if !v.Provenance.Valid() {
		r.add(VErrBadProvenance, where+".provenance", string(v.Provenance), "unknown provenance")
		return
	}
	if requireApproved && !v.Provenance.Approved() {
		r.add(VErrUnapprovedParam, where, "",
			fmt.Sprintf("value marked %s is not approved for release", v.Provenance))
	}
	// Trial and pending values must carry a note so the reason is reviewable.
	if (v.Provenance == ProvenanceTrial || v.Provenance == ProvenancePending) && strings.TrimSpace(v.Note) == "" {
		r.add(VErrMissingProvenance, where+".note", "",
			fmt.Sprintf("value marked %s must explain why it is not yet approved", v.Provenance))
	}
}

// validateZeroAwareConfigValue checks provenance on a configured number whose
// zero value is meaningful, such as an item price of zero meaning free or
// unsellable. Unlike validateConfigValue it does not treat zero as missing.
func validateZeroAwareConfigValue(where string, v ConfigValue, r *ValidationReport) {
	if v.Provenance == "" {
		r.add(VErrMissingProvenance, where, "",
			"configured number has no provenance; manuscript and trial values must not be mixed silently")
		return
	}
	if !v.Provenance.Valid() {
		r.add(VErrBadProvenance, where+".provenance", string(v.Provenance), "unknown provenance")
		return
	}
	if (v.Provenance == ProvenanceTrial || v.Provenance == ProvenancePending) && strings.TrimSpace(v.Note) == "" {
		r.add(VErrMissingProvenance, where+".note", "",
			fmt.Sprintf("value marked %s must explain why it is not yet approved", v.Provenance))
	}
}

// validateProvenance walks every configured number and checks that trial and
// pending values across the catalogue are marked. High-realm thresholds in
// particular exceed the manuscript's "several thousand" magnitude and are
// unapproved, so they must not pass silently.
func validateProvenance(c *Catalogue, r *ValidationReport) {
	validateConfigValue("cultivation_mood_threshold", c.CultivationMoodThreshold, false, r)
	if c.CultivationMoodThreshold.Value < 0 || c.CultivationMoodThreshold.Value > 100 {
		r.add(VErrInvariantBroken, "cultivation_mood_threshold", fmt.Sprint(c.CultivationMoodThreshold.Value),
			"mood threshold must be between 0 and 100")
	}

	// The base event chance is a permille probability, so it must sit inside
	// 0..PermilleScale. A value above the scale would make the check a
	// certainty that still consumed a draw, which is a different rule from the
	// one the design states.
	validateConfigValue("event_base_chance_permille", c.EventBaseChancePermille, false, r)
	if c.EventBaseChancePermille.Value < 0 || c.EventBaseChancePermille.Value > PermilleScale {
		r.add(VErrInvariantBroken, "event_base_chance_permille", fmt.Sprint(c.EventBaseChancePermille.Value),
			"event base chance must be between 0 and 1000 permille")
	}
	for i, realm := range c.Realms {
		where := fmt.Sprintf("realms[%d]", i)
		// Realm lifespan and base rates are manuscript figures; the tier
		// thresholds for the upper realms are trial values.
		validateConfigValue(where+".lifespan_years", realm.LifespanYears, false, r)
	}
	for i, sp := range c.SpiritRoots {
		validateConfigValue(fmt.Sprintf("spirit_roots[%d].multiplier", i), sp.Multiplier, false, r)
	}
	for i, t := range c.Techniques {
		validateConfigValue(fmt.Sprintf("techniques[%d].grade_multiplier", i), t.GradeMultiplier, false, r)
	}
	for i, a := range c.Afflictions {
		where := fmt.Sprintf("afflictions[%d]", i)
		validateConfigValue(where+".duration_months", a.DurationMonths, false, r)
		if a.HPDrainPerMonth.Provenance != "" {
			validateConfigValue(where+".hp_drain_per_month", a.HPDrainPerMonth, false, r)
		}
		if a.MoodDrainPerMonth.Provenance != "" {
			validateConfigValue(where+".mood_drain_per_month", a.MoodDrainPerMonth, false, r)
		}
		if a.SpeedPenaltyPermille.Provenance != "" {
			validateConfigValue(where+".speed_penalty_per_mille", a.SpeedPenaltyPermille, false, r)
		}
		if a.EffectiveAptitudeDelta.Provenance != "" {
			validateConfigValue(where+".effective_aptitude_delta", a.EffectiveAptitudeDelta, false, r)
		}
		if a.RateBonus.Provenance != "" {
			validateConfigValue(where+".rate_bonus", a.RateBonus, false, r)
		}
	}
	for i, b := range c.InjuryBands {
		where := fmt.Sprintf("injury_bands[%d]", i)
		validateConfigValue(where+".speed_penalty_permille", b.SpeedPenaltyPermille, false, r)
	}
	for i, t := range c.KarmaTiers {
		where := fmt.Sprintf("karma_tiers[%d]", i)
		validateConfigValue(where+".min_karma", t.MinKarma, false, r)
	}
	validateConfigValue("heal_restore_permille", c.HealRestorePermille, false, r)
	if c.HealRestorePermille.Value < 0 || c.HealRestorePermille.Value > PermilleScale {
		r.add(VErrNonPositiveAmount, "heal_restore_permille",
			fmt.Sprint(c.HealRestorePermille.Value),
			"a monthly restoration is a fraction of the ceiling and must be in 0..1000 permille")
	}
	validateConfigValue("heal_convert_burn_permille", c.HealConvertBurnPermille, false, r)
	if c.HealConvertBurnPermille.Value <= 0 || c.HealConvertBurnPermille.Value >= PermilleScale {
		r.add(VErrNonPositiveAmount, "heal_convert_burn_permille",
			fmt.Sprint(c.HealConvertBurnPermille.Value),
			"a conversion must burn a positive fraction, and must leave some health behind")
	}
	for i, q := range c.Quests {
		validateConfigValue(fmt.Sprintf("quests[%d].month_cost", i), q.MonthCost, false, r)
	}
	for i, b := range c.Breakthroughs {
		where := fmt.Sprintf("breakthroughs[%d]", i)
		validateConfigValue(where+".base_success_pct", b.BaseSuccessPct, false, r)
		validateConfigValue(where+".month_cost", b.MonthCost, false, r)
	}
	for i, e := range c.Events {
		validateConfigValue(fmt.Sprintf("events[%d].priority", i), e.Priority, false, r)
		validateConfigValue(fmt.Sprintf("events[%d].weight", i), e.Weight, false, r)
	}
}

// validateOrigins checks origin ids, provenance-free grants and effect shapes.
func validateOrigins(c *Catalogue, r *ValidationReport) {
	seen := map[Origin]int{}
	for i, o := range c.Origins {
		where := fmt.Sprintf("origins[%d]", i)
		if !o.ID.Valid() {
			r.add(VErrUnknownEnum, where+".id", string(o.ID), "unknown origin")
			continue
		}
		if prev, dup := seen[o.ID]; dup {
			r.add(VErrDuplicateID, where+".id", string(o.ID),
				fmt.Sprintf("origin already declared at origins[%d]", prev))
			continue
		}
		seen[o.ID] = i
		validateGrantEffects(where+".effects", o.Effects, r, ScopeCreation)
	}
}

// validateSpiritRoots checks root ids and that multipliers are positive.
func validateSpiritRoots(c *Catalogue, r *ValidationReport) {
	seen := map[SpiritRoot]int{}
	for i, sp := range c.SpiritRoots {
		where := fmt.Sprintf("spirit_roots[%d]", i)
		if !sp.ID.Valid() {
			r.add(VErrUnknownEnum, where+".id", string(sp.ID), "unknown spirit root")
			continue
		}
		if prev, dup := seen[sp.ID]; dup {
			r.add(VErrDuplicateID, where+".id", string(sp.ID),
				fmt.Sprintf("spirit root already declared at spirit_roots[%d]", prev))
			continue
		}
		seen[sp.ID] = i
		if sp.Multiplier.Value <= 0 {
			r.add(VErrNonPositiveAmount, where+".multiplier", string(sp.ID),
				"cultivation multiplier must be positive")
		}
	}
}

// validateTalents checks talent ids, effects and exclusivity references.
func validateTalents(c *Catalogue, r *ValidationReport) {
	seen := map[string]int{}
	for i, t := range c.Talents {
		where := fmt.Sprintf("talents[%d]", i)
		if t.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "talent id must not be empty")
			continue
		}
		if prev, dup := seen[t.ID]; dup {
			r.add(VErrDuplicateID, where+".id", t.ID,
				fmt.Sprintf("talent already declared at talents[%d]", prev))
			continue
		}
		seen[t.ID] = i
		validateGrantEffects(where+".effects", t.Effects, r, ScopeCreation)
	}
	// Exclusivity must not dangle.
	for i, t := range c.Talents {
		for j, ex := range t.Exclusive {
			if _, ok := seen[ex]; !ok {
				r.add(VErrDanglingRef, fmt.Sprintf("talents[%d].exclusive[%d]", i, j), ex,
					"exclusive talent id is not declared")
			}
		}
	}
}

// validateGrantEffects checks every effect is well-formed and has a reason.
func validateGrantEffects(where string, effects []GrantEffect, r *ValidationReport, scope EffectScope) {
	for i, e := range effects {
		ew := fmt.Sprintf("%s[%d]", where, i)
		if !e.Kind.Valid() {
			r.add(VErrUnknownEnum, ew+".kind", string(e.Kind), "unknown grant kind")
		}
		if strings.TrimSpace(e.Target) == "" {
			r.add(VErrMissingProvenance, ew+".target", "", "grant target must not be empty")
		} else if !ValidEffectTarget(e.Target, scope) {
			// The whitelist is what makes "content cannot execute anything"
			// true. A target outside it must be rejected here, at load time,
			// rather than ignored at runtime: an ignored effect produces an
			// event that shows its text, appears to fire, and quietly does
			// nothing.
			r.add(VErrUnknownEnum, ew+".target", e.Target,
				"target is outside the whitelist for this scope")
		}
		// A zero-size grant is almost always a content mistake; a deliberate
		// zero would be better expressed by omitting the effect.
		if e.Amount == 0 {
			r.add(VErrNonPositiveAmount, ew+".amount", e.Target,
				"grant amount is zero; omit the effect instead of granting nothing")
		}
		if strings.TrimSpace(e.Reason) == "" {
			r.add(VErrMissingProvenance, ew+".reason", e.Target,
				"grant must name the reason so the ledger can explain it")
		}
	}
}

// validateItems checks item identity, prices and stack limits.
func validateItems(c *Catalogue, r *ValidationReport) {
	seen := map[string]int{}
	for i, it := range c.Items {
		where := fmt.Sprintf("items[%d]", i)
		if it.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "item id must not be empty")
			continue
		}
		if prev, dup := seen[it.ID]; dup {
			r.add(VErrDuplicateID, where+".id", it.ID,
				fmt.Sprintf("item already declared at items[%d]", prev))
			continue
		}
		seen[it.ID] = i

		// Negative prices are the classic exploit: buying at a negative price
		// mints money. Both prices must be non-negative, and a sell price must
		// never exceed the buy price or in-place trading becomes free profit.
		if it.BuyPrice.Value < 0 {
			r.add(VErrNegativeCost, where+".buy_price", it.ID,
				"buy price must not be negative")
		}
		if it.SellPrice.Value < 0 {
			r.add(VErrNegativeCost, where+".sell_price", it.ID,
				"sell price must not be negative")
		}
		if it.BuyPrice.Value > 0 && it.SellPrice.Value > it.BuyPrice.Value {
			r.add(VErrNegativeCost, where+".sell_price", it.ID,
				"sell price exceeds buy price, which allows cost-free arbitrage")
		}
		// Prices are configured numbers like any other, so they must declare
		// where they came from. Without this the "0 means free" case would let
		// an item carry a real price with no provenance, which is exactly the
		// manuscript-versus-trial mixing the design document forbids.
		validateZeroAwareConfigValue(where+".buy_price", it.BuyPrice, r)
		validateZeroAwareConfigValue(where+".sell_price", it.SellPrice, r)
		if it.StackLimit < 0 {
			r.add(VErrNegativeCost, where+".stack_limit", it.ID,
				"stack limit must not be negative")
		}
		if !it.Category.Valid() {
			r.add(VErrUnknownEnum, where+".category", string(it.Category), "unknown item category")
		}
		if it.Category == ItemEquipment && it.EquipSlot == "" {
			r.add(VErrMissingProvenance, where+".equip_slot", it.ID,
				"equipment item must declare its slot")
		}
		validateGrantEffects(where+".effects", it.Effects, r, ScopeRuntime)
	}
}

// validateTechniques checks technique identity and grade consistency.
func validateTechniques(c *Catalogue, r *ValidationReport) {
	seen := map[string]int{}
	for i, t := range c.Techniques {
		where := fmt.Sprintf("techniques[%d]", i)
		if t.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "technique id must not be empty")
			continue
		}
		if prev, dup := seen[t.ID]; dup {
			r.add(VErrDuplicateID, where+".id", t.ID,
				fmt.Sprintf("technique already declared at techniques[%d]", prev))
			continue
		}
		seen[t.ID] = i
		if !t.Grade.Valid() {
			r.add(VErrBadGrade, where+".grade", string(t.Grade), "unknown technique grade")
		}
		if t.GradeMultiplier.Value <= 0 {
			r.add(VErrNonPositiveAmount, where+".grade_multiplier", t.ID,
				"grade multiplier must be positive")
		}
		validateGrantEffects(where+".effects", t.Effects, r, ScopeCreation)
		for j, effect := range t.Effects {
			if effect.Kind != GrantMultiplicative || effect.Target != targetCultivationRate {
				r.add(VErrInvariantBroken, fmt.Sprintf("%s.effects[%d]", where, j), t.ID,
					"active technique effects may only grant a named cultivation-rate multiplier")
			}
		}
	}
	if findPrimaryTechnique(c, GradeYellow) == nil {
		r.add(VErrPreconditionUnmet, "techniques", "yellow_breath",
			"M1 requires a yellow-grade primary technique for the creation baseline")
	}
}

// validateAfflictions checks the affliction catalogue.
//
// An affliction with no effect at all is rejected rather than allowed: it would
// occupy a Condition.Effects slot, show up in the status detail, and do nothing,
// which reads to a player as a bug in the game rather than in the content.
func validateAfflictions(c *Catalogue, r *ValidationReport) {
	seen := map[string]int{}
	for i, a := range c.Afflictions {
		where := fmt.Sprintf("afflictions[%d]", i)
		if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.NameZH) == "" {
			r.add(VErrMissingProvenance, where, a.ID, "affliction id and display name are required")
			continue
		}
		if prev, dup := seen[a.ID]; dup {
			r.add(VErrDuplicateID, where+".id", a.ID,
				fmt.Sprintf("affliction already declared at afflictions[%d]", prev))
			continue
		}
		seen[a.ID] = i

		if !a.Kind.Valid() {
			r.add(VErrUnknownEnum, where+".kind", string(a.Kind), "unknown affliction kind")
		}
		if a.DurationMonths.Value <= 0 {
			r.add(VErrNonPositiveAmount, where+".duration_months", a.ID,
				"an affliction must last at least one settled month")
		}
		if a.RateBonus.Value <= -SCALE {
			r.add(VErrInvariantBroken, where+".rate_bonus", a.ID,
				"a rate bonus must not reduce the multiplier to zero or below")
		}
		if a.SpeedPenaltyPermille.Value < 0 || a.SpeedPenaltyPermille.Value > PermilleScale {
			r.add(VErrNonPositiveAmount, where+".speed_penalty_per_mille", a.ID,
				"a speed penalty is a fraction of the base and must be in 0..1000 permille")
		}
		if strings.TrimSpace(a.Group) == "" && a.RateBonus.Provenance != "" {
			r.add(VErrMissingProvenance, where+".group", a.ID,
				"a rate modifier must name its additive stacking group")
		}
		if !hasAnyAfflictionEffect(a) {
			r.add(VErrNonPositiveAmount, where, a.ID,
				"an affliction that changes nothing would still occupy a slot and read as a bug")
		}
	}
}

// hasAnyAfflictionEffect reports whether an affliction actually does something.
func hasAnyAfflictionEffect(a AfflictionDefinition) bool {
	return a.HPDrainPerMonth.Value != 0 ||
		a.MoodDrainPerMonth.Value != 0 ||
		a.SpeedPenaltyPermille.Value != 0 ||
		a.EffectiveAptitudeDelta.Value != 0 ||
		a.RateBonus.Value != 0
}

// validateInjuryBands checks that every declared band is configured exactly once
// and that the configuration names no band the rules do not know.
func validateInjuryBands(c *Catalogue, r *ValidationReport) {
	seen := map[InjuryBand]int{}
	for i, b := range c.InjuryBands {
		where := fmt.Sprintf("injury_bands[%d]", i)
		if !b.Band.Valid() {
			r.add(VErrUnknownEnum, where+".band", string(b.Band), "unknown injury band")
			continue
		}
		if prev, dup := seen[b.Band]; dup {
			r.add(VErrDuplicateID, where+".band", string(b.Band),
				fmt.Sprintf("band already configured at injury_bands[%d]", prev))
			continue
		}
		seen[b.Band] = i
	}
	for _, band := range InjuryBandOrder {
		if _, ok := seen[band]; !ok {
			r.add(VErrMissingProvenance, "injury_bands", string(band),
				"every injury band must be configured; an unconfigured band has no penalty rule")
		}
	}
}

// validateKarmaTiers checks that the karma tiers form an ordered ladder with a
// floor at zero, so every karma total is describable.
func validateKarmaTiers(c *Catalogue, r *ValidationReport) {
	if len(c.KarmaTiers) == 0 {
		r.add(VErrMissingProvenance, "karma_tiers", "",
			"the karma tiers must be configured; without them a karma total cannot be described")
		return
	}
	seen := map[string]int{}
	lowest := 0
	hasFloor := false
	for i, t := range c.KarmaTiers {
		where := fmt.Sprintf("karma_tiers[%d]", i)
		if strings.TrimSpace(t.ID) == "" {
			r.add(VErrMissingProvenance, where+".id", "", "karma tier id must not be empty")
			continue
		}
		if prev, dup := seen[t.ID]; dup {
			r.add(VErrDuplicateID, where+".id", t.ID,
				fmt.Sprintf("karma tier already declared at karma_tiers[%d]", prev))
			continue
		}
		seen[t.ID] = i
		if t.MinKarma.Value < 0 {
			r.add(VErrNegativeCost, where+".min_karma", t.ID, "karma tiers start at zero")
		}
		if i == 0 || t.MinKarma.Value < lowest {
			lowest = t.MinKarma.Value
		}
		if t.MinKarma.Value == 0 {
			hasFloor = true
		}
	}
	if !hasFloor {
		r.add(VErrMissingProvenance, "karma_tiers", "",
			"one tier must start at zero, or a character with no karma has no tier")
	}
}

// validateSkills checks skill identity and costs.
func validateSkills(c *Catalogue, r *ValidationReport) {
	_ = c // skills are referenced from techniques; validated in place
}

// validateLocations checks the travel graph is closed and symmetric.
//
// In-place travel is zero-month and safe, which means an asymmetric edge or a
// dangling neighbour would let a player reach a location they cannot leave.
func validateLocations(c *Catalogue, r *ValidationReport) {
	ids := make(map[string]int, len(c.Locations))
	for i, l := range c.Locations {
		where := fmt.Sprintf("locations[%d]", i)
		if l.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "location id must not be empty")
			continue
		}
		if prev, dup := ids[l.ID]; dup {
			r.add(VErrDuplicateID, where+".id", l.ID,
				fmt.Sprintf("location already declared at locations[%d]", prev))
			continue
		}
		ids[l.ID] = i
		if l.Environment != "" && !l.Environment.Valid() {
			r.add(VErrUnknownEnum, where+".environment", string(l.Environment),
				"unknown environment")
		}
	}

	// Neighbours must exist and be mutual.
	for i, l := range c.Locations {
		for j, nb := range l.Neighbors {
			nw := fmt.Sprintf("locations[%d].neighbors[%d]", i, j)
			other, ok := ids[nb]
			if !ok {
				r.add(VErrDanglingRef, nw, nb,
					"neighbour location is not declared")
				continue
			}
			if !mutualNeighbor(c.Locations[other].Neighbors, l.ID) {
				r.add(VErrBadTravelGraph, nw, nb,
					fmt.Sprintf("travel edge %s -> %s has no return edge; zero-month travel must be reversible", l.ID, nb))
			}
		}
	}
}

// mutualNeighbor reports whether want appears in neighbors.
func mutualNeighbor(neighbors []string, want string) bool {
	for _, n := range neighbors {
		if n == want {
			return true
		}
	}
	return false
}

// validateNPCs checks NPC identity, adult gating and realm validity.
func validateNPCs(c *Catalogue, r *ValidationReport) {
	seen := map[string]int{}
	for i, n := range c.NPCs {
		where := fmt.Sprintf("npcs[%d]", i)
		if n.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "npc id must not be empty")
			continue
		}
		if prev, dup := seen[n.ID]; dup {
			r.add(VErrDuplicateID, where+".id", n.ID,
				fmt.Sprintf("npc already declared at npcs[%d]", prev))
			continue
		}
		seen[n.ID] = i

		if !isDeclaredRealm(n.InitialRealm) {
			r.add(VErrUnknownEnum, where+".initial_realm", string(n.InitialRealm), "unknown realm")
		}
		if !n.InitialTier.Valid() {
			r.add(VErrUnknownEnum, where+".initial_tier", string(n.InitialTier), "unknown tier")
		}
		if !n.Adult {
			// M1 ships no romance content, so this is a forward guard: any NPC
			// intended for such content later must be declared adult now.
			r.add(VErrPathNotOpen, where+".adult", n.ID,
				"npc is not declared adult; romance-adjacent content must be gated from the start")
		}
		// Design 14: "所有NPC年龄明确". An NPC with no age cannot be spawned,
		// and an NPC older than their own ceiling starts the game dead.
		if n.InitialAgeYears <= 0 {
			r.add(VErrMissingProvenance, where+".initial_age_years", n.ID,
				"npc age must be explicit; design 14 requires every NPC's age to be stated")
		} else if n.LifespanYears > 0 && n.InitialAgeYears > n.LifespanYears {
			r.add(VErrInvariantBroken, where+".initial_age_years", n.ID,
				fmt.Sprintf("npc starts at age %d, past its own lifespan of %d years",
					n.InitialAgeYears, n.LifespanYears))
		}
	}
	// Home locations and dialogue references must resolve.
	locIDs := map[string]bool{}
	for _, l := range c.Locations {
		locIDs[l.ID] = true
	}
	dialogs := map[string]bool{}
	for _, d := range c.Dialogues {
		dialogs[d.ID] = true
	}
	for i, n := range c.NPCs {
		if n.HomeLocation != "" && !locIDs[n.HomeLocation] {
			r.add(VErrDanglingRef, fmt.Sprintf("npcs[%d].home_location", i), n.HomeLocation,
				"home location is not declared")
		}
		for j, d := range n.DialogueIDs {
			if !dialogs[d] {
				r.add(VErrDanglingRef, fmt.Sprintf("npcs[%d].dialogue_ids[%d]", i, j), d,
					"dialogue id is not declared")
			}
		}
	}
}

// validateQuests checks quest identity, costs, rewards and references.
func validateQuests(c *Catalogue, r *ValidationReport) {
	seen := map[string]int{}
	for i, q := range c.Quests {
		where := fmt.Sprintf("quests[%d]", i)
		if q.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "quest id must not be empty")
			continue
		}
		if prev, dup := seen[q.ID]; dup {
			r.add(VErrDuplicateID, where+".id", q.ID,
				fmt.Sprintf("quest already declared at quests[%d]", prev))
			continue
		}
		seen[q.ID] = i

		if !q.Kind.Valid() {
			r.add(VErrUnknownEnum, where+".kind", string(q.Kind), "unknown quest kind")
		}
		if q.MonthCost.Value < 0 {
			r.add(VErrNegativeCost, where+".month_cost", q.ID,
				"month cost must not be negative")
		}
		if q.CooldownMonths < 0 {
			r.add(VErrNegativeCost, where+".cooldown_months", q.ID,
				"cooldown must not be negative")
		}
		for j, req := range q.RequiredItems {
			if req.Quantity < 0 {
				r.add(VErrNegativeCost, fmt.Sprintf("%s.required_items[%d]", where, j), req.ItemID,
					"required quantity must not be negative")
			}
		}
		validateGrantEffects(where+".rewards", q.Rewards, r, ScopeRuntime)
	}
	// Referenced items, NPCs and entry quests must exist.
	itemIDs := map[string]bool{}
	for _, it := range c.Items {
		itemIDs[it.ID] = true
	}
	npcIDs := map[string]bool{}
	for _, n := range c.NPCs {
		npcIDs[n.ID] = true
	}
	for i, q := range c.Quests {
		for j, req := range q.RequiredItems {
			if !itemIDs[req.ItemID] {
				r.add(VErrDanglingRef, fmt.Sprintf("quests[%d].required_items[%d]", i, j), req.ItemID,
					"required item is not declared")
			}
		}
		if q.GiverNPCID != "" && !npcIDs[q.GiverNPCID] {
			r.add(VErrDanglingRef, fmt.Sprintf("quests[%d].giver_npc_id", i), q.GiverNPCID,
				"quest giver is not declared")
		}
	}
}

// validateSects checks sect identity and entry-quest references.
func validateSects(c *Catalogue, r *ValidationReport) {
	seen := map[string]int{}
	questIDs := map[string]bool{}
	for _, q := range c.Quests {
		questIDs[q.ID] = true
	}
	for i, s := range c.Sects {
		where := fmt.Sprintf("sects[%d]", i)
		if s.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "sect id must not be empty")
			continue
		}
		if prev, dup := seen[s.ID]; dup {
			r.add(VErrDuplicateID, where+".id", s.ID,
				fmt.Sprintf("sect already declared at sects[%d]", prev))
			continue
		}
		seen[s.ID] = i
		if s.EntryQuestID != "" && !questIDs[s.EntryQuestID] {
			r.add(VErrDanglingRef, where+".entry_quest_id", s.EntryQuestID,
				"entry quest is not declared")
		}
		if s.MonthlyIncome.Value < 0 {
			r.add(VErrNegativeCost, where+".monthly_income", s.ID,
				"monthly income must not be negative")
		}
	}
}

// validateBreakthroughs enforces the rule the acceptance criteria name
// explicitly: a missing breakthrough table is a failure, never a default.
//
// It also checks that every material and trial node resolves, that costs are
// non-negative, and that the clamping bounds are coherent.
func validateBreakthroughs(c *Catalogue, r *ValidationReport) {
	itemIDs := map[string]bool{}
	for _, it := range c.Items {
		itemIDs[it.ID] = true
	}
	eventIDs := map[string]bool{}
	for _, e := range c.Events {
		eventIDs[e.ID] = true
	}

	type key struct {
		path      Path
		fromRealm Realm
		fromTier  RealmTier
	}
	seen := map[key]int{}

	for i, b := range c.Breakthroughs {
		where := fmt.Sprintf("breakthroughs[%d]", i)
		if !b.Path.Valid() {
			r.add(VErrUnknownEnum, where+".path", string(b.Path), "unknown cultivation path")
			continue
		}
		if !isDeclaredRealm(b.FromRealm) {
			r.add(VErrUnknownEnum, where+".from_realm", string(b.FromRealm), "unknown realm")
			continue
		}
		if !b.FromTier.Valid() {
			r.add(VErrUnknownEnum, where+".from_tier", string(b.FromTier), "unknown tier")
			continue
		}
		if !isDeclaredRealm(b.ToRealm) {
			r.add(VErrUnknownEnum, where+".to_realm", string(b.ToRealm), "unknown realm")
			continue
		}
		if !b.ToTier.Valid() {
			r.add(VErrUnknownEnum, where+".to_tier", string(b.ToTier), "unknown tier")
			continue
		}

		k := key{b.Path, b.FromRealm, b.FromTier}
		if prev, dup := seen[k]; dup {
			r.add(VErrDuplicateID, where, fmt.Sprintf("%s/%s/%s", b.Path, b.FromRealm, b.FromTier),
				fmt.Sprintf("breakthrough from this position already declared at breakthroughs[%d]", prev))
			continue
		}
		seen[k] = i

		// Clamping bounds must be ordered, or the clamp is meaningless.
		if b.ClampMinPct.Value > b.ClampMaxPct.Value {
			r.add(VErrNegativeCost, where+".clamp_min_pct", "",
				fmt.Sprintf("clamp minimum %d exceeds maximum %d",
					b.ClampMinPct.Value, b.ClampMaxPct.Value))
		}
		if b.BaseSuccessPct.Value < 0 || b.BaseSuccessPct.Value > 100 {
			r.add(VErrNonPositiveAmount, where+".base_success_pct", "",
				"base success must be a percentage in 0..100")
		}
		// An attempt costs one month under M1 rules.
		if b.MonthCost.Value < 0 {
			r.add(VErrNegativeCost, where+".month_cost", "", "month cost must not be negative")
		}
		// Materials must exist and be non-negative.
		for j, m := range b.Materials {
			if !itemIDs[m.ItemID] {
				r.add(VErrDanglingRef, fmt.Sprintf("%s.materials[%d]", where, j), m.ItemID,
					"breakthrough material is not declared")
			}
			if m.Quantity < 0 {
				r.add(VErrNegativeCost, fmt.Sprintf("%s.materials[%d]", where, j), m.ItemID,
					"material quantity must not be negative")
			}
		}
		// Trial nodes must resolve to configured events.
		for j, node := range b.TrialNodes {
			if !eventIDs[node] {
				r.add(VErrDanglingRef, fmt.Sprintf("%s.trial_nodes[%d]", where, j), node,
					"trial node event is not declared")
			}
		}
	}

	// A major breakthrough from each realm must have a table. This is the
	// "missing breakthrough table" acceptance case, checked per realm rather
	// than per path so that a gap is reported once, clearly.
	majorByFromRealm := map[Realm]bool{}
	for _, b := range c.Breakthroughs {
		if b.IsMajor {
			majorByFromRealm[b.FromRealm] = true
		}
	}
	// Only the final realm has no successor, so it needs no table.
	for idx, realm := range RealmOrder {
		if idx == len(RealmOrder)-1 {
			continue
		}
		if !majorByFromRealm[realm] {
			r.add(VErrMissingBreakthrough, "breakthroughs", string(realm),
				fmt.Sprintf("realm %s has no major breakthrough table; a missing table must fail, not default", realm))
		}
	}
}

// validateEvents checks event identity, choices, costs, risks and chains.
func validateEvents(c *Catalogue, r *ValidationReport) {
	itemIDs := map[string]bool{}
	for _, it := range c.Items {
		itemIDs[it.ID] = true
	}

	seen := map[string]int{}
	for i, e := range c.Events {
		where := fmt.Sprintf("events[%d]", i)
		if e.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "event id must not be empty")
			continue
		}
		if prev, dup := seen[e.ID]; dup {
			r.add(VErrDuplicateID, where+".id", e.ID,
				fmt.Sprintf("event already declared at events[%d]", prev))
			continue
		}
		seen[e.ID] = i

		if e.Priority.Value < 0 {
			r.add(VErrNegativeCost, where+".priority", e.ID, "priority must not be negative")
		}
		if e.Weight.Value < 0 {
			r.add(VErrNegativeCost, where+".weight", e.ID, "weight must not be negative")
		}
		if e.CooldownMonths < 0 {
			r.add(VErrNegativeCost, where+".cooldown_months", e.ID, "cooldown must not be negative")
		}
		if e.ExpiresAfterMonths < 0 {
			r.add(VErrNegativeCost, where+".expires_after_months", e.ID, "expiry must not be negative")
		}
		if e.MaxOccurrences < 0 {
			r.add(VErrNegativeCost, where+".max_occurrences", e.ID, "max occurrences must not be negative")
		}
		if len(e.Choices) == 0 {
			r.add(VErrBadChain, where+".choices", e.ID, "event must offer at least one choice")
		}

		// Design 14: "节点1～3句". The node's own text is what the player reads
		// before the choices; Purpose is written for a reviewer and is not a
		// substitute. A node with no text is a menu.
		if strings.TrimSpace(e.TextZH) == "" {
			r.add(VErrNodeText, where+".text_zh", e.ID,
				"event node needs its own display text; design 14 requires one to three sentences")
		} else if n := sentenceCount(e.TextZH); n < 1 || n > 3 {
			r.add(VErrNodeText, where+".text_zh", e.ID,
				fmt.Sprintf("node text has %d sentences; design 14 requires one to three", n))
		}

		// A forced event must not also be drawable. If it were, it would take
		// the month's random slot as well as its forced slot and then be unable
		// to fire on the following month, which reads as a bug to a player who
		// cannot see the scheduler.
		if e.Forced && e.Weight.Value != 0 {
			r.add(VErrBadChain, where+".weight", e.ID,
				"a forced event must have weight 0: it is raised by eligibility, not by the weighted draw")
		}

		// Every eligibility and requirement condition must be well-formed.
		validatePreconditions(where+".eligibility", e.Eligibility, r)

		choiceIDs := map[string]bool{}
		for j, ch := range e.Choices {
			cw := fmt.Sprintf("%s.choices[%d]", where, j)
			if ch.ID == "" {
				r.add(VErrMissingProvenance, cw+".id", e.ID, "choice id must not be empty")
				continue
			}
			if choiceIDs[ch.ID] {
				r.add(VErrDuplicateID, cw+".id", ch.ID,
					"choice id is duplicated within the event")
			}
			choiceIDs[ch.ID] = true

			validatePreconditions(cw+".requires", ch.Requires, r)

			// Costs must be positive and resolvable.
			for k, cost := range ch.Costs {
				kw := fmt.Sprintf("%s.costs[%d]", cw, k)
				if !cost.Kind.Valid() {
					r.add(VErrUnknownEnum, kw+".kind", string(cost.Kind), "unknown cost kind")
				}
				if cost.Amount <= 0 {
					r.add(VErrNonPositiveAmount, kw+".amount", "",
						"cost amount must be positive; express a free option by omitting the cost")
				}
				if cost.Months < 0 {
					r.add(VErrNegativeCost, kw+".months", "",
						"extra month cost must not be negative")
				}
				if cost.Kind == CostItem && !itemIDs[cost.ItemID] {
					r.add(VErrDanglingRef, kw+".item_id", cost.ItemID, "cost item is not declared")
				}
				if cost.Kind == CostResource && !cost.Resource.Valid() {
					r.add(VErrUnknownEnum, kw+".resource", string(cost.Resource),
						"unknown resource")
				}
			}

			validateGrantEffects(cw+".effects", ch.Effects, r, ScopeRuntime)

			// A choice that carries a risk must preview it, with an honest
			// probability. This is the "cost must be visible" rule made
			// machine-checkable.
			for k, risk := range ch.Risks {
				rw := fmt.Sprintf("%s.risks[%d]", cw, k)
				if !risk.Kind.Valid() {
					r.add(VErrUnknownEnum, rw+".kind", string(risk.Kind), "unknown risk kind")
				}
				if strings.TrimSpace(risk.PreviewText) == "" {
					r.add(VErrRiskNotPreviewed, rw+".preview_text", e.ID,
						"risk is not previewed to the player; every warned risk must be shown before the choice")
				}
				if risk.ProbabilityPct.Provenance == "" {
					r.add(VErrMissingProvenance, rw+".probability_pct", "",
						"risk probability needs provenance")
				} else if risk.ProbabilityPct.Value < 0 || risk.ProbabilityPct.Value > 100 {
					r.add(VErrNonPositiveAmount, rw+".probability_pct", "",
						"risk probability must be in 0..100")
				}
			}

			// A tutorial choice must not pay a repeatable reward, because that
			// would be a free money loop.
			if e.IsTutorial && len(ch.Effects) > 0 {
				// A tutorial may grant a one-off starting item; it must not
				// grant currency that can be farmed. Currency grants on a
				// tutorial choice are rejected.
				for k, eff := range ch.Effects {
					if eff.Kind == GrantAdditive && strings.HasPrefix(eff.Target, "resources.") {
						r.add(VErrTutorialReward, fmt.Sprintf("%s.effects[%d]", cw, k), eff.Target,
							"tutorial choice grants a farmable resource; tutorial rewards must not repeat")
					}
				}
			}
		}

		// Chains and follows must resolve to choices declared in this event.
		for j, ch := range e.Choices {
			if ch.NextNodeID != "" && !eventNodeExists(e, ch.NextNodeID) {
				r.add(VErrBadChain, fmt.Sprintf("%s.choices[%d].next_node_id", where, j), ch.NextNodeID,
					"next node is not declared in this event's follow-up chain")
			}
		}
		for j, fu := range e.FollowUps {
			fw := fmt.Sprintf("%s.follow_ups[%d]", where, j)
			if fu.NodeID == "" {
				r.add(VErrBadChain, fw+".node_id", e.ID, "follow-up node id must not be empty")
				continue
			}
			if len(fu.ChoiceIDs) == 0 {
				r.add(VErrBadChain, fw+".choice_ids", fu.NodeID,
					"follow-up node must offer at least one choice")
			}
			// A follow-up node's candidates must be choices the event actually
			// declares. The scheduler freezes them into the pending instance
			// and the resolver looks each one up by id, so an undeclared id
			// would freeze a candidate set the player can never answer.
			for _, id := range fu.ChoiceIDs {
				if !choiceIDs[id] {
					r.add(VErrDanglingRef, fw+".choice_ids", id,
						"follow-up node offers a choice the event does not declare")
				}
			}
		}
	}
}

// sentenceCount counts sentence-ending punctuation in a short display text.
//
// It is a proxy for "one to three sentences", not a parser: the point is to
// catch a node that has no text at all, or a wall of prose pretending to be a
// one-line scene.
func sentenceCount(text string) int {
	n := 0
	for _, r := range text {
		switch r {
		case '。', '！', '？', '.', '!', '?':
			n++
		}
	}
	return n
}

// hasSectRequirement reports whether a condition list gates on sect membership.
func hasSectRequirement(conds []Precondition) bool {
	for _, p := range conds {
		if p.Kind == CondSectMember && p.Value == 1 {
			return true
		}
	}
	return false
}

// flagSetter is one choice that sets a world flag.
type flagSetter struct {
	eventID   string
	choiceID  string
	sectGated bool
}

// validateEarlyGoals checks that each early goal can actually end in all three
// ways, and that it can be advanced without joining a sect.
//
// ADR-001 is explicit: "两个目标均可通过散修路径推进，不以入宗、恋爱或AI为通关
// 前提". That is a property of the content graph, so it is checked here rather
// than asserted in prose. The check is a necessary condition, not a full
// reachability proof — it verifies that a success branch exists whose event and
// choice do not require sect membership, and says so rather than implying more.
func validateEarlyGoals(c *Catalogue, r *ValidationReport) {
	if len(c.EarlyGoals) == 0 {
		r.add(VErrMissingProvenance, "early_goals", "",
			"M1 must declare its early goals; ADR-001 names two")
		return
	}

	// Index every flag any event choice sets.
	setters := map[string][]flagSetter{}
	for i := range c.Events {
		ev := &c.Events[i]
		eventGated := hasSectRequirement(ev.Eligibility)
		for j := range ev.Choices {
			ch := &ev.Choices[j]
			gated := eventGated || hasSectRequirement(ch.Requires)
			for _, eff := range ch.Effects {
				name, ok := trimPrefix(eff.Target, targetFlagsPrefix)
				if !ok || eff.Kind != GrantAdditive || eff.Amount == 0 {
					continue
				}
				setters[name] = append(setters[name], flagSetter{
					eventID: ev.ID, choiceID: ch.ID, sectGated: gated,
				})
			}
		}
	}

	seen := map[string]int{}
	claimed := map[string]string{}
	for i, g := range c.EarlyGoals {
		where := fmt.Sprintf("early_goals[%d]", i)
		if g.ID == "" {
			r.add(VErrMissingProvenance, where+".id", "", "early goal id must not be empty")
			continue
		}
		if prev, dup := seen[g.ID]; dup {
			r.add(VErrDuplicateID, where+".id", g.ID,
				fmt.Sprintf("early goal already declared at early_goals[%d]", prev))
			continue
		}
		seen[g.ID] = i

		branches := []struct{ name, flag string }{
			{"success_flag", g.SuccessFlag},
			{"failure_flag", g.FailureFlag},
			{"abandon_flag", g.AbandonFlag},
		}
		for _, br := range branches {
			if br.flag == "" {
				r.add(VErrMissingProvenance, where+"."+br.name, g.ID,
					"every early goal needs a flag for each of its three outcomes")
				continue
			}
			if len(setters[br.flag]) == 0 {
				r.add(VErrUnreachableGoal, where+"."+br.name, br.flag,
					"no event choice sets this flag, so the branch cannot be reached in play")
			}
			if prev, dup := claimed[br.flag]; dup {
				r.add(VErrDuplicateID, where+"."+br.name, br.flag,
					"this flag is already claimed by "+prev+"; two goals sharing an outcome flag cannot be told apart")
			}
			claimed[br.flag] = g.ID
		}
		if g.SuccessFlag == g.FailureFlag || g.SuccessFlag == g.AbandonFlag || g.FailureFlag == g.AbandonFlag {
			r.add(VErrUnreachableGoal, where, g.ID,
				"a goal's three outcome flags must be distinct")
		}

		// The rogue-path requirement, from ADR-001.
		if g.SuccessFlag == "" {
			continue
		}
		branch := setters[g.SuccessFlag]
		if len(branch) == 0 {
			continue
		}
		open := false
		for _, s := range branch {
			if !s.sectGated {
				open = true
				break
			}
		}
		if !open {
			r.add(VErrUnreachableGoal, where+".success_flag", g.SuccessFlag,
				"every choice that advances this goal requires sect membership; "+
					"ADR-001 requires both early goals to be advanceable as a rogue cultivator")
		}
	}
}

// eventNodeExists reports whether nodeID is a follow-up node of the event or
// the event's own id (the entry node).
func eventNodeExists(e EventDefinition, nodeID string) bool {
	if nodeID == e.ID {
		return true
	}
	for _, fu := range e.FollowUps {
		if fu.NodeID == nodeID {
			return true
		}
	}
	return false
}

// validatePreconditions checks condition shape: kind known, operator known, and
// numeric versus string comparison fields used consistently.
func validatePreconditions(where string, conds []Precondition, r *ValidationReport) {
	for i, c := range conds {
		cw := fmt.Sprintf("%s[%d]", where, i)
		if !c.Kind.Valid() {
			r.add(VErrBadCondition, cw+".kind", string(c.Kind), "unknown condition kind")
			continue
		}
		// Only a comparing kind needs an operator. Demanding one from
		// `npc_available` or `consumed_event` would make them impossible to
		// write correctly, since they answer from a key alone.
		if c.Kind.NeedsOperator() {
			if !c.Op.Valid() {
				r.add(VErrBadCondition, cw+".op", string(c.Op), "unknown comparison operator")
			}
		} else if c.Op != "" && !c.Op.Valid() {
			r.add(VErrBadCondition, cw+".op", string(c.Op), "unknown comparison operator")
		}
		if c.Kind.NeedsKey() && c.Key == "" {
			r.add(VErrBadCondition, cw+".key", "", "condition must name what it tests")
		}
		// A numeric kind must not be given a text operand, and a text kind must
		// not be given a number. Kinds that compare neither accept neither.
		if c.Kind.ValueBearing() && c.TextValue != "" {
			r.add(VErrBadCondition, cw+".text_value", "",
				fmt.Sprintf("condition kind %s compares numbers, not text", c.Kind))
		}
		if c.Kind.UsesTextOperand() && c.TextValue == "" {
			r.add(VErrBadCondition, cw+".text_value", "",
				fmt.Sprintf("condition kind %s compares text and needs text_value", c.Kind))
		}
		if !c.Kind.ValueBearing() && !c.Kind.UsesTextOperand() && c.TextValue != "" {
			r.add(VErrBadCondition, cw+".text_value", "",
				fmt.Sprintf("condition kind %s does not compare text", c.Kind))
		}
	}
}

// validateM1Paths enforces that the catalogue does not claim to open a path
// that ADR-001 froze out of M1.
//
// This is the "只配置高境界不意味着开放" rule: a catalogue may legitimately
// describe the earthly and heavenly paths for completeness, but listing them
// as open would ship content the milestone does not support.
func validateM1Paths(c *Catalogue, r *ValidationReport) {
	if len(c.M1Paths) == 0 {
		r.add(VErrPathNotOpen, "m1_paths", "",
			"no cultivation path is declared open; M1 requires exactly the human path")
		return
	}
	seen := map[Path]bool{}
	for i, p := range c.M1Paths {
		where := fmt.Sprintf("m1_paths[%d]", i)
		if !p.Valid() {
			r.add(VErrUnknownEnum, where, string(p), "unknown path")
			continue
		}
		if seen[p] {
			r.add(VErrDuplicateID, where, string(p), "path listed more than once")
			continue
		}
		seen[p] = true
		if !p.OpenInM1() {
			r.add(VErrPathNotOpen, where, string(p),
				"path is frozen out of M1; ADR-001 must be revised before it may be opened")
		}
	}
	if !seen[PathHuman] {
		r.add(VErrPathNotOpen, "m1_paths", string(PathHuman),
			"the human path must be open; M1 is frozen to it")
	}
}

// ValidateState checks the structural invariants a loaded or freshly created
// GameState must satisfy. It is separate from catalogue validation because it
// takes no catalogue: TASK-05 layers content-aware checks on top.
func ValidateState(s *GameState) ValidationReport {
	var r ValidationReport
	if s == nil {
		r.add(VErrMissingProvenance, "state", "", "state is nil")
		return r
	}
	if s.SchemaVersion != SchemaVersion {
		r.add(VErrUnknownEnum, "schema_version", fmt.Sprint(s.SchemaVersion),
			fmt.Sprintf("expected schema version %d", SchemaVersion))
	}
	if s.GameID == "" {
		r.add(VErrMissingProvenance, "game_id", "", "game id must not be empty")
	}
	if s.BranchID == "" {
		r.add(VErrMissingProvenance, "branch_id", "", "branch id must not be empty")
	}
	if !s.Phase.Valid() {
		r.add(VErrUnknownEnum, "phase", string(s.Phase), "unknown phase")
	}
	if s.Player == nil {
		// A state in CREATION has no player yet; every other phase requires one.
		if s.Phase != PhaseCreation {
			r.add(VErrPreconditionUnmet, "player", "",
				fmt.Sprintf("phase %s requires a player", s.Phase))
		}
	}
	if s.World == nil {
		r.add(VErrPreconditionUnmet, "world", "", "world state must be present")
	}

	// The phase and pending sub-state must agree. A mismatch is how a save
	// ends up waiting forever for an event that no longer exists.
	validatePhasePendingConsistency(s, &r)

	// The queued event instances and the occurrence ledger must agree with each
	// other. A queue entry with no ledger row would let an event escape its
	// occurrence cap by being raised and then quietly forgotten.
	validateEventQueueAndLedger(s, &r)

	// Every RNG stream a rule may draw from must exist, or the first draw in a
	// resumed game would panic instead of replaying.
	for _, name := range WorldStreamNames {
		if _, ok := s.RNG.Streams[name]; !ok {
			r.add(VErrPreconditionUnmet, "rng.streams."+name, name,
				"required random stream is missing; replay would diverge")
		}
	}

	if s.Player != nil {
		validatePlayerState(s.Player, &r)
	}
	return r
}

// validatePhasePendingConsistency cross-checks phase against the sub-state
// that phase implies.
func validatePhasePendingConsistency(s *GameState, r *ValidationReport) {
	switch s.Phase {
	case PhaseEventPending:
		if s.Pending.Event == nil {
			r.add(VErrPreconditionUnmet, "pending.event", "",
				"EVENT_PENDING requires a pending event")
		}
	case PhaseCombatPending:
		if s.Pending.Combat == nil {
			r.add(VErrPreconditionUnmet, "pending.combat", "",
				"COMBAT_PENDING requires a pending combat")
		}
	case PhaseBreakthroughPending:
		if s.Pending.Breakthrough == nil {
			r.add(VErrPreconditionUnmet, "pending.breakthrough", "",
				"BREAKTHROUGH_PENDING requires a pending breakthrough")
		}
	case PhaseConfirming:
		if s.Pending.Confirmation == nil {
			r.add(VErrPreconditionUnmet, "pending.confirmation", "",
				"CONFIRMING requires a pending confirmation")
		}
	case PhaseCreation:
		if s.Pending.Creation == nil {
			r.add(VErrPreconditionUnmet, "pending.creation", "",
				"CREATION requires a creation draft")
		}
	}

	// Conversely, a sub-state must not linger in a phase that cannot use it.
	if s.Pending.Event != nil && s.Phase != PhaseEventPending {
		r.add(VErrInvariantBroken, "pending.event", "",
			fmt.Sprintf("pending event present while phase is %s", s.Phase))
	}
	if s.Pending.Combat != nil && s.Phase != PhaseCombatPending {
		r.add(VErrInvariantBroken, "pending.combat", "",
			fmt.Sprintf("pending combat present while phase is %s", s.Phase))
	}
}

// validateEventQueueAndLedger checks the queued event instances against the
// occurrence ledger that raised them.
//
// The two are written together, so any disagreement means the document was
// edited outside the game or written by a build with a different scheduler.
// Resuming from such a state would silently change which events can still fire.
func validateEventQueueAndLedger(s *GameState, r *ValidationReport) {
	if s == nil || s.World == nil {
		return
	}

	// The ledger must be well-formed before the queue is checked against it.
	ledger := map[string]RaisedEvent{}
	for i, re := range s.World.RaisedEvents {
		where := fmt.Sprintf("world.raised_events[%d]", i)
		if re.EventID == "" {
			r.add(VErrMissingProvenance, where+".event_id", "", "raised event id must not be empty")
			continue
		}
		if re.InstanceID == "" {
			r.add(VErrMissingProvenance, where+".instance_id", "", "raised instance id must not be empty")
			continue
		}
		if _, dup := ledger[re.InstanceID]; dup {
			r.add(VErrDuplicateID, where+".instance_id", re.InstanceID,
				"instance id is already recorded; instance ids must be unique")
			continue
		}
		if re.WorldMonth > s.Counters.WorldMonth {
			r.add(VErrInvariantBroken, where+".world_month", fmt.Sprint(re.WorldMonth),
				"an event cannot have been raised in a month that has not happened")
		}
		ledger[re.InstanceID] = re
	}

	if s.World.EventInstanceSeq < int64(len(s.World.RaisedEvents)) {
		r.add(VErrInvariantBroken, "world.event_instance_seq", fmt.Sprint(s.World.EventInstanceSeq),
			"the instance counter is behind the ledger; a later raise would reuse an instance id")
	}

	seen := map[string]bool{}
	for i, q := range s.Pending.EventQueue {
		where := fmt.Sprintf("pending.event_queue[%d]", i)
		if q.EventID == "" || q.InstanceID == "" {
			r.add(VErrMissingProvenance, where, "", "a queued instance needs both an event id and an instance id")
			continue
		}
		if seen[q.EventID] {
			r.add(VErrDuplicateID, where+".event_id", q.EventID,
				"the same event is queued twice; the scheduler raises at most one instance at a time")
		}
		seen[q.EventID] = true

		if len(q.ChoiceIDs) == 0 {
			r.add(VErrSerialization, where+".choice_ids", q.EventID,
				"a queued instance must freeze its candidates; re-rolling on promotion is forbidden")
		}
		if q.ExpiresAtWorldMonth != 0 && q.ExpiresAtWorldMonth < q.RaisedAtWorldMonth {
			r.add(VErrInvariantBroken, where+".expires_at_world_month", fmt.Sprint(q.ExpiresAtWorldMonth),
				"a queued instance cannot expire before it was raised")
		}

		raised, ok := ledger[q.InstanceID]
		if !ok {
			r.add(VErrDanglingRef, where+".instance_id", q.InstanceID,
				"a queued instance has no matching entry in the occurrence ledger")
			continue
		}
		if raised.EventID != q.EventID {
			r.add(VErrDanglingRef, where+".event_id", q.EventID,
				"the queued event id disagrees with the ledger entry for this instance")
		}
	}

	// A pending instance must not also sit in the queue. Promotion removes it
	// from the queue, so both holding it would mean two answers are owed for
	// one occurrence.
	if s.Pending.Event != nil {
		for i, q := range s.Pending.EventQueue {
			if q.InstanceID == s.Pending.Event.InstanceID {
				r.add(VErrDuplicateID, fmt.Sprintf("pending.event_queue[%d].instance_id", i),
					q.InstanceID, "the pending event is also queued")
			}
		}
	}
}

// validatePlayerState checks the design document's 9.4 invariant list.
func validatePlayerState(p *Player, r *ValidationReport) {
	// hp/mp within their caps.
	if p.HP.Current < 0 || p.HP.Max < 0 {
		r.add(VErrInvariantBroken, "player.hp", "", "hp must not be negative")
	}
	if p.HP.Max > 0 && p.HP.Current > p.HP.Max {
		r.add(VErrInvariantBroken, "player.hp.current", fmt.Sprint(p.HP.Current),
			fmt.Sprintf("hp %d exceeds its maximum %d", p.HP.Current, p.HP.Max))
	}
	if p.MP.Current < 0 || p.MP.Max < 0 {
		r.add(VErrInvariantBroken, "player.mp", "", "mp must not be negative")
	}
	if p.MP.Max > 0 && p.MP.Current > p.MP.Max {
		r.add(VErrInvariantBroken, "player.mp.current", fmt.Sprint(p.MP.Current),
			fmt.Sprintf("mp %d exceeds its maximum %d", p.MP.Current, p.MP.Max))
	}

	// xp, balance and item quantities are never negative. Debt is tracked
	// separately, which is exactly why balance must not absorb it.
	if p.XP < 0 {
		r.add(VErrInvariantBroken, "player.xp", fmt.Sprint(p.XP), "xp must not be negative")
	}
	if p.Condition.Mood < 0 || p.Condition.Mood > 100 {
		r.add(VErrInvariantBroken, "player.condition.mood", fmt.Sprint(p.Condition.Mood),
			"mood must be between 0 and 100")
	}
	if p.Debt < 0 {
		r.add(VErrInvariantBroken, "player.debt", fmt.Sprint(p.Debt),
			"debt is recorded as a positive owed amount")
	}
	for res, amount := range p.Resources {
		if !res.Valid() {
			r.add(VErrUnknownEnum, "player.resources", string(res), "unknown resource")
		}
		if amount < 0 {
			r.add(VErrInvariantBroken, "player.resources."+string(res), fmt.Sprint(amount),
				"balance must not be negative; an unaffordable action is refused, not auto-loaned")
		}
	}
	for i, st := range p.Inventory.Stacks {
		if st.Quantity < 0 {
			r.add(VErrInvariantBroken, fmt.Sprintf("player.inventory.stacks[%d].quantity", i), st.ItemID,
				"item quantity must not be negative")
		}
	}

	// Realm and tier must be declared, and age must not precede zero.
	if !isDeclaredRealm(p.Realm) {
		r.add(VErrUnknownEnum, "player.realm", string(p.Realm), "unknown realm")
	}
	if !p.Tier.Valid() {
		r.add(VErrUnknownEnum, "player.tier", string(p.Tier), "unknown tier")
	}
	if p.AgeMonths < 0 {
		r.add(VErrInvariantBroken, "player.age_months", fmt.Sprint(p.AgeMonths),
			"age must not be negative")
	}
	if p.Path != "" && !p.Path.Valid() {
		r.add(VErrUnknownEnum, "player.path", string(p.Path), "unknown path")
	}
	if p.Path != "" && !p.Path.OpenInM1() {
		r.add(VErrPathNotOpen, "player.path", string(p.Path),
			"player is on a path M1 does not open")
	}
	if p.Origin != "" && !p.Origin.Valid() {
		r.add(VErrUnknownEnum, "player.origin", string(p.Origin), "unknown origin")
	}
	if p.SpiritRoot != "" && !p.SpiritRoot.Valid() {
		r.add(VErrUnknownEnum, "player.spirit_root", string(p.SpiritRoot), "unknown spirit root")
	}

	// A deceased player must not be mid-combat or mid-event.
	if p.Ended && p.EndCause.Code == "" {
		r.add(VErrInvariantBroken, "player.end_cause", "",
			"an ended character must record why it ended")
	}
}

// ValidateSerializeReady asserts that every sub-state the design document
// requires to be serialisable is actually populated by the state, so a save
// taken mid-action can be resumed rather than silently losing the sub-state.
//
// It is intentionally narrow: it does not serialise, it checks that the
// structures which must survive a round trip are non-nil when they should be.
func ValidateSerializeReady(s *GameState) ValidationReport {
	var r ValidationReport
	if s == nil {
		r.add(VErrSerialization, "state", "", "state is nil")
		return r
	}
	if s.RNG.Streams == nil {
		r.add(VErrSerialization, "rng.streams", "",
			"random state must be serialisable; a nil stream map cannot record a replay position")
	}
	if s.Idempotency.Entries == nil {
		r.add(VErrSerialization, "idempotency.entries", "",
			"idempotency records must be serialisable so a duplicate submit can return the original result")
	}
	// A pending combat round must carry an explicit round and an awaiting flag,
	// or resuming cannot know whose turn it is and might give the enemy a free
	// attack.
	if s.Pending.Combat != nil {
		c := s.Pending.Combat
		if c.CombatID == "" {
			r.add(VErrSerialization, "pending.combat.combat_id", "",
				"combat id must be recorded so a resumed battle is the same battle")
		}
		if !c.AwaitingPlayer && c.Resolution == CombatOngoing {
			r.add(VErrSerialization, "pending.combat.awaiting_player", "",
				"an ongoing combat must record whether it awaits the player, or a resume would grant a free enemy turn")
		}
	}
	// An active month action must record whether its month was already charged,
	// so settlement advances the counter at most once.
	if s.Pending.MonthAction != nil {
		m := s.Pending.MonthAction
		if m.ActionID == "" {
			r.add(VErrSerialization, "pending.month_action.action_id", "",
				"active month action must record its action id")
		}
	}
	// A pending event must freeze its candidate set; an empty set would have to
	// be re-rolled on resume, which the design document forbids.
	if s.Pending.Event != nil && len(s.Pending.Event.ChoiceIDs) == 0 {
		r.add(VErrSerialization, "pending.event.choice_ids", "",
			"pending event must freeze its candidate choices; re-rolling on resume is forbidden")
	}
	// A pending breakthrough must keep its spent materials and completed nodes.
	if s.Pending.Breakthrough != nil {
		b := s.Pending.Breakthrough
		if b.BreakthroughID == "" {
			r.add(VErrSerialization, "pending.breakthrough.breakthrough_id", "",
				"pending breakthrough must record its id")
		}
		if b.ParentActionID == "" {
			r.add(VErrSerialization, "pending.breakthrough.parent_action_id", "",
				"pending breakthrough must keep its parent action so timing is inherited")
		}
	}
	return r
}
