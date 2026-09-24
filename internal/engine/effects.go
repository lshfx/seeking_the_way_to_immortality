package engine

import "fmt"

// The content effect DSL.
//
// Content is data, never code. Design R20 and the TASK-10 acceptance criteria
// both require that a content pack cannot execute a file, open a network
// connection, or run an arbitrary script. The mechanism that makes that true is
// not a sandbox: it is that the only things content can name are the strings
// below, and every one of them is resolved by an explicit switch case here.
//
// An unknown target, condition kind or cost kind is therefore a hard failure,
// never a silent no-op. Silence is the dangerous outcome: a typo in a target
// would produce an event that appears to fire, shows its text, and quietly does
// nothing, which a player would read as "this choice is a trap" rather than as a
// content bug.
//
// Everything in this file is pure with respect to time and randomness: it reads
// and writes state it is handed, draws no die, and advances no clock.

// Effect targets.
//
// A target is a flat, greppable name. Nothing here can be confused with a
// filesystem path, a URL or a symbol, which is the point: the only things a
// content pack can reach are these names, and every one of them is resolved by
// an explicit switch case in this file.
const (
	targetHPMax     = "hp.max"
	targetHPCurrent = "hp.current"
	targetMPMax     = "mp.max"
	targetMPCurrent = "mp.current"
	targetXP        = "xp"
	targetMood      = "mood"
	targetDebt      = "debt"

	targetAttack  = "attack"
	targetDefense = "defense"

	targetSpiritStones    = "resources.spirit_stones"
	targetCultivationRate = "cultivation_rate"

	// Prefix targets. A prefix must be followed by a non-empty name.
	targetAttrPrefix         = "attributes."
	targetResourcesPrefix    = "resources."
	targetItemsPrefix        = "items."
	targetFlagsPrefix        = "flags."
	targetRelationsPrefix    = "relations."
	targetBreakthroughPrefix = "breakthrough."
)

// attributeKeys are the six base attributes a growth grant may name.
var attributeKeys = []string{
	"strength", "agility", "constitution", "comprehension", "aptitude", "fortune",
}

// EffectScope says where an effect is allowed to be used.
//
// The two scopes are genuinely different capabilities, not a formality. A
// creation grant is folded by the factory before a character exists, so it may
// set a starting cultivation-rate multiplier; a runtime grant is applied to a
// living character whose rate is recomputed every month, so the same target
// would be silently overwritten within one month. Rejecting it at load time is
// the difference between "this content does not do what it says" and a loud
// failure at the moment the content is written.
type EffectScope int

const (
	// ScopeCreation is for origins, constitutions, talents and techniques.
	ScopeCreation EffectScope = iota
	// ScopeRuntime is for event choices, item use and quest rewards.
	ScopeRuntime
)

// ValidEffectTarget reports whether target may be named in the given scope.
//
// Content validation calls this at load time so a bad target is reported with
// its location, rather than surfacing as a runtime rejection mid-session.
func ValidEffectTarget(target string, scope EffectScope) bool {
	switch target {
	case targetHPMax, targetHPCurrent, targetMPMax, targetMPCurrent,
		targetXP, targetMood, targetDebt, targetAttack, targetDefense:
		return true
	case targetCultivationRate:
		return scope == ScopeCreation
	case targetSpiritStones:
		return true
	}

	if name, ok := trimPrefix(target, targetAttrPrefix); ok {
		return containsString(attributeKeys, name)
	}
	if name, ok := trimPrefix(target, targetResourcesPrefix); ok {
		return Resource(name).Valid()
	}
	if _, ok := trimPrefix(target, targetItemsPrefix); ok {
		// The item id itself is checked against the catalogue by
		// ValidateCatalogue, which has the catalogue in hand.
		return true
	}
	if _, ok := trimPrefix(target, targetFlagsPrefix); ok {
		return true
	}
	if _, ok := trimPrefix(target, targetRelationsPrefix); ok {
		return true
	}
	if _, ok := trimPrefix(target, targetBreakthroughPrefix); ok {
		return true
	}
	return false
}

// applyGrantEffect applies one named grant to the state and records it in the
// ledger. It returns an error rather than a rejection code because the caller
// decides whether a failure is a content bug (validation) or a runtime refusal.
func applyGrantEffect(s *GameState, cat *Catalogue, eff GrantEffect, ledger *DeltaLedger) error {
	if s == nil {
		return fmt.Errorf("no state to apply %q to", eff.Target)
	}
	if s.Player == nil {
		return fmt.Errorf("effect %q needs a character", eff.Target)
	}

	switch eff.Target {
	case targetHPCurrent:
		return adjustVitals(s, eff, ledger, &s.Player.HP, "hp.current", false)
	case targetHPMax:
		return adjustVitals(s, eff, ledger, &s.Player.HP, "hp.max", true)
	case targetMPCurrent:
		return adjustVitals(s, eff, ledger, &s.Player.MP, "mp.current", false)
	case targetMPMax:
		return adjustVitals(s, eff, ledger, &s.Player.MP, "mp.max", true)
	case targetXP:
		return adjustXP(s, eff, ledger)
	case targetMood:
		return adjustMood(s, eff, ledger)
	case targetDebt:
		return adjustDebt(s, eff, ledger)
	case targetAttack:
		return adjustDerived(s, eff, ledger, &s.Player.Derived.Attack, "attack")
	case targetDefense:
		return adjustDerived(s, eff, ledger, &s.Player.Derived.Defense, "defense")
	case targetSpiritStones:
		return adjustResource(s, eff, ledger, ResSpiritStones)
	case targetCultivationRate:
		return fmt.Errorf("effect %q is a creation-time target and cannot be applied at runtime: "+
			"the monthly recomputation would overwrite it within one month", eff.Target)
	}

	if name, ok := trimPrefix(eff.Target, targetAttrPrefix); ok {
		return adjustAttribute(s, eff, ledger, name)
	}
	if name, ok := trimPrefix(eff.Target, targetResourcesPrefix); ok {
		return adjustResource(s, eff, ledger, Resource(name))
	}
	if name, ok := trimPrefix(eff.Target, targetItemsPrefix); ok {
		return adjustItem(s, cat, eff, ledger, name)
	}
	if name, ok := trimPrefix(eff.Target, targetFlagsPrefix); ok {
		return setFlag(s, eff, ledger, name)
	}
	if name, ok := trimPrefix(eff.Target, targetRelationsPrefix); ok {
		return adjustRelation(s, eff, ledger, name)
	}
	if name, ok := trimPrefix(eff.Target, targetBreakthroughPrefix); ok {
		return adjustBreakthroughBonus(s, eff, ledger, name)
	}

	return fmt.Errorf("unknown effect target %q", eff.Target)
}

// adjustVitals applies a grant to one side of a current/max pair.
//
// Current values are clamped into [0, max] because the state contract requires
// it. Max values are floored at zero and never allowed to fall below the
// current value, so an effect cannot leave a character holding more health than
// their ceiling allows.
func adjustVitals(s *GameState, eff GrantEffect, ledger *DeltaLedger, v *Vitals, field string, isMax bool) error {
	before := v.Current
	if isMax {
		before = v.Max
	}

	after, err := applyGrantArithmetic(before, eff)
	if err != nil {
		return err
	}

	if isMax {
		if after < 0 {
			after = 0
		}
		if v.Current > after {
			// Lowering a ceiling below the current value would violate the
			// "current <= max" invariant. The current value comes down with
			// it, and that reduction is recorded separately so the ledger
			// explains the whole change rather than only the ceiling.
			ledger.Entries = append(ledger.Entries, DeltaEntry{
				Field:  field[:len(field)-len(".max")] + ".current",
				Before: v.Current, After: after,
				Reason: eff.Reason + "（上限下调，当前值随之降低）",
			})
			v.Current = after
		}
		v.Max = after
	} else {
		if after < 0 {
			after = 0
		}
		if after > v.Max {
			after = v.Max
		}
		v.Current = after
	}

	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: field, Before: before, After: after, Reason: eff.Reason,
	})
	return nil
}

// adjustXP applies a grant to accumulated cultivation points.
func adjustXP(s *GameState, eff GrantEffect, ledger *DeltaLedger) error {
	after, err := applyGrantArithmetic(s.Player.XP, eff)
	if err != nil {
		return err
	}
	if after < 0 {
		return fmt.Errorf("effect %q would take xp below zero", eff.Target)
	}
	before := s.Player.XP
	s.Player.XP = after
	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: "xp", Before: before, After: after, Reason: eff.Reason,
	})
	return nil
}

// adjustMood applies a grant to 心境 and clamps it to the contract's [0,100].
//
// Clamping rather than refusing is deliberate here: mood is a bounded scale, so
// "raise mood by 20" at mood 95 is a legitimate content line whose natural
// reading is "it cannot go above 100". The ledger records the value that was
// actually applied, so a clamp is visible rather than hidden.
func adjustMood(s *GameState, eff GrantEffect, ledger *DeltaLedger) error {
	after, err := applyGrantArithmetic(int64(s.Player.Condition.Mood), eff)
	if err != nil {
		return err
	}
	if after < 0 {
		after = 0
	}
	if after > 100 {
		after = 100
	}
	before := int64(s.Player.Condition.Mood)
	s.Player.Condition.Mood = int(after)
	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: "mood", Before: before, After: after, Reason: eff.Reason,
	})
	return nil
}

// adjustDebt applies a grant to the debt ledger.
func adjustDebt(s *GameState, eff GrantEffect, ledger *DeltaLedger) error {
	after, err := applyGrantArithmetic(s.Player.Debt, eff)
	if err != nil {
		return err
	}
	if after < 0 {
		// Paying off more debt than is owed is not a way to mint money: the
		// ledger floors at zero and the excess is discarded.
		after = 0
	}
	before := s.Player.Debt
	s.Player.Debt = after
	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: "debt", Before: before, After: after, Reason: eff.Reason,
	})
	return nil
}

// adjustResource applies a grant to one tracked resource.
func adjustResource(s *GameState, eff GrantEffect, ledger *DeltaLedger, res Resource) error {
	if !res.Valid() {
		return fmt.Errorf("unknown resource %q", string(res))
	}
	if s.Player.Resources == nil {
		s.Player.Resources = map[Resource]int64{}
	}
	before := s.Player.Resources[res]
	after, err := applyGrantArithmetic(before, eff)
	if err != nil {
		return err
	}
	if after < 0 {
		// Design 9.4: resources are non-negative. A content effect that would
		// drive one below zero is a content bug, not a player choice, because
		// affordability is checked through the cost path instead.
		return fmt.Errorf("effect %q would take %s below zero", eff.Target, string(res))
	}
	s.Player.Resources[res] = after
	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: "resources." + string(res), Before: before, After: after,
		Reason: eff.Reason,
	})
	return nil
}

// adjustItem applies a grant to an inventory stack.
//
// A negative amount removes items. Removal refuses to go below zero rather than
// clamping, because a content line that "takes 3 herbs" when the player holds 2
// is a content error: silently taking 2 would make the outcome depend on the
// player's inventory in a way the preview never stated.
func adjustItem(s *GameState, cat *Catalogue, eff GrantEffect, ledger *DeltaLedger, itemID string) error {
	if cat != nil && findItem(cat, itemID) == nil {
		return fmt.Errorf("effect %q names unknown item %q", eff.Target, itemID)
	}
	if s.Player.Inventory.Stacks == nil {
		s.Player.Inventory.Stacks = []ItemStack{}
	}

	before := heldQuantity(&s.Player.Inventory, itemID)
	after, err := applyGrantArithmetic(before, eff)
	if err != nil {
		return err
	}
	if after < 0 {
		return fmt.Errorf("effect %q would take %s below zero", eff.Target, itemID)
	}
	setHeldQuantity(&s.Player.Inventory, itemID, after)

	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: "items." + itemID, Before: before, After: after,
		Reason: eff.Reason,
	})
	return nil
}

// setFlag applies a grant to a named world flag.
//
// A flag is a boolean, so only the additive kind is meaningful and only a
// non-zero amount turns it on. Multiplicative on a flag is a content error: it
// reads as if it scaled something, which would silently do nothing.
func setFlag(s *GameState, eff GrantEffect, ledger *DeltaLedger, name string) error {
	if name == "" {
		return fmt.Errorf("effect %q names an empty flag", eff.Target)
	}
	if eff.Kind != GrantAdditive {
		return fmt.Errorf("flag %q accepts only an additive grant, got %q", name, string(eff.Kind))
	}
	if s.World == nil {
		return fmt.Errorf("effect %q needs a world", eff.Target)
	}
	if s.World.Flags == nil {
		s.World.Flags = map[string]bool{}
	}
	before := s.World.Flags[name]
	after := eff.Amount != 0
	s.World.Flags[name] = after

	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: "flags." + name, Before: boolToInt(before), After: boolToInt(after),
		Reason: eff.Reason,
	})
	return nil
}

// adjustDerived applies a grant to one cached combat statistic.
func adjustDerived(s *GameState, eff GrantEffect, ledger *DeltaLedger, field *int64, name string) error {
	after, err := applyGrantArithmetic(*field, eff)
	if err != nil {
		return err
	}
	if after < 0 {
		return fmt.Errorf("effect %q would take %s below zero", eff.Target, name)
	}
	before := *field
	*field = after
	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: name, Before: before, After: after, Reason: eff.Reason,
	})
	return nil
}

// adjustAttribute applies a grant to one of the six base attributes.
//
// The change lands in Attributes.Growth rather than in the base value. Design
// 6.2 requires a temporary injury to affect only effective values and never to
// rewrite a character's permanent base; keeping growth in its own map is what
// makes "this was earned in play" distinguishable from "this was chosen at
// creation", so a later re-spec cannot erase it.
func adjustAttribute(s *GameState, eff GrantEffect, ledger *DeltaLedger, key string) error {
	if !containsString(attributeKeys, key) {
		return fmt.Errorf("effect %q names an unknown attribute %q", eff.Target, key)
	}
	if eff.Kind != GrantAdditive {
		return fmt.Errorf("attribute %q accepts only an additive grant, got %q", key, string(eff.Kind))
	}
	if s.Player.Attributes.Growth == nil {
		s.Player.Attributes.Growth = map[string]int{}
	}
	before := int64(s.Player.Attributes.Growth[key])
	after, err := applyGrantArithmetic(before, eff)
	if err != nil {
		return err
	}
	s.Player.Attributes.Growth[key] = int(after)
	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: "attributes." + key, Before: before, After: after, Reason: eff.Reason,
	})
	return nil
}

// adjustRelation applies a grant to the player's standing with one NPC.
//
// The relation row is created on demand. An event that rewards helping someone
// the player has not formally met should still record the standing: refusing
// would make the reward depend on an unrelated bookkeeping flag.
func adjustRelation(s *GameState, eff GrantEffect, ledger *DeltaLedger, npcID string) error {
	if npcID == "" {
		return fmt.Errorf("effect %q names an empty NPC", eff.Target)
	}
	if eff.Kind != GrantAdditive {
		return fmt.Errorf("relation %q accepts only an additive grant, got %q", npcID, string(eff.Kind))
	}
	if s.Player.Relations == nil {
		s.Player.Relations = []Relation{}
	}

	for i := range s.Player.Relations {
		if s.Player.Relations[i].NPCID != npcID {
			continue
		}
		before := int64(s.Player.Relations[i].Value)
		after, err := applyGrantArithmetic(before, eff)
		if err != nil {
			return err
		}
		s.Player.Relations[i].Value = int(after)
		s.Player.Relations[i].Met = true
		ledger.Entries = append(ledger.Entries, DeltaEntry{
			Field: "relations." + npcID, Before: before, After: after, Reason: eff.Reason,
		})
		return nil
	}

	after, err := applyGrantArithmetic(0, eff)
	if err != nil {
		return err
	}
	s.Player.Relations = append(s.Player.Relations, Relation{NPCID: npcID, Value: int(after), Met: true})
	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: "relations." + npcID, Before: 0, After: after, Reason: eff.Reason,
	})
	return nil
}

// adjustBreakthroughBonus applies a grant to a named breakthrough accumulator.
//
// These are content-scoped counters such as a tribulation resistance earned from
// facing an inner demon. They are stored on the character rather than resolved
// on the spot, because the attempt that consumes them happens later: resolving
// "resist +15" into a success now would be the narrator deciding the outcome,
// which design R20 forbids.
//
// TASK-16 reads these when it implements the breakthrough attempt.
func adjustBreakthroughBonus(s *GameState, eff GrantEffect, ledger *DeltaLedger, name string) error {
	if name == "" {
		return fmt.Errorf("effect %q names an empty accumulator", eff.Target)
	}
	if eff.Kind != GrantAdditive {
		return fmt.Errorf("breakthrough accumulator %q accepts only an additive grant, got %q",
			name, string(eff.Kind))
	}
	if s.Player.BreakthroughBonuses == nil {
		s.Player.BreakthroughBonuses = map[string]int64{}
	}
	before := s.Player.BreakthroughBonuses[name]
	after, err := applyGrantArithmetic(before, eff)
	if err != nil {
		return err
	}
	s.Player.BreakthroughBonuses[name] = after
	ledger.Entries = append(ledger.Entries, DeltaEntry{
		Field: "breakthrough." + name, Before: before, After: after, Reason: eff.Reason,
	})
	return nil
}

// applyGrantArithmetic applies the grant's kind to a value.
//
// The three kinds are not interchangeable, and conflating them is how a "×2"
// silently becomes "+2":
//
//   - additive: value + amount
//   - multiplicative: value * amount / SCALE, truncated toward zero, so a
//     factor of 15000 is ×1.5
//   - permanent: additive on a ceiling. It is rejected on anything else,
//     because "permanently raise" has no meaning for a current value or a
//     count of herbs.
func applyGrantArithmetic(before int64, eff GrantEffect) (int64, error) {
	switch eff.Kind {
	case GrantAdditive:
		return addChecked(before, eff.Amount)
	case GrantMultiplicative:
		product, err := mulChecked(before, eff.Amount)
		if err != nil {
			return 0, err
		}
		return product / SCALE, nil
	case GrantPermanent:
		if !isCeilingTarget(eff.Target) {
			return 0, fmt.Errorf("a permanent grant is only meaningful on a ceiling, not on %q", eff.Target)
		}
		return addChecked(before, eff.Amount)
	default:
		return 0, fmt.Errorf("unknown grant kind %q", string(eff.Kind))
	}
}

// isCeilingTarget reports whether a target names a ceiling that a permanent
// grant may raise.
func isCeilingTarget(target string) bool {
	return target == targetHPMax || target == targetMPMax
}

// canPayEventCost reports whether a cost is currently payable.
//
// It is separate from paying so that a choice can be previewed and refused
// before anything is written. Design 13.2 step 1 checks preconditions and
// necessary confirmation before any state change, and design R19 requires an
// unaffordable purchase to be refused rather than auto-financed.
func canPayEventCost(s *GameState, cost EventCost) (ErrorCode, string, bool) {
	if s == nil || s.Player == nil {
		return ErrPreconditionUnmet, "there is no character to pay from", false
	}
	if cost.Amount <= 0 {
		return ErrPreconditionUnmet, "a cost must be a positive amount", false
	}

	switch cost.Kind {
	case CostItem:
		held := heldQuantity(&s.Player.Inventory, cost.ItemID)
		if held < cost.Amount {
			return ErrQuantityNotHeld, fmt.Sprintf("need %d of %s but hold %d", cost.Amount, cost.ItemID, held), false
		}
	case CostResource:
		if !cost.Resource.Valid() {
			return ErrUnknownTarget, "unknown resource " + string(cost.Resource), false
		}
		if s.Player.Resources[cost.Resource] < cost.Amount {
			return ErrInsufficientFunds, fmt.Sprintf("need %d %s but hold %d",
				cost.Amount, string(cost.Resource), s.Player.Resources[cost.Resource]), false
		}
	case CostHP:
		if s.Player.HP.Current < cost.Amount {
			return ErrPreconditionUnmet, "not enough health for this cost", false
		}
	case CostMP:
		if s.Player.MP.Current < cost.Amount {
			return ErrPreconditionUnmet, "not enough stamina for this cost", false
		}
	case CostMonth:
		// Design 13.1: an option that costs an extra month must be declared as
		// its own action, because a month-end choice is settled with the
		// parent action's timing. A non-zero month here would either be
		// ignored (a lie to the player) or charge a second month (a bug), so
		// it is refused outright.
		return ErrPreconditionUnmet,
			"an event choice cannot cost a month; declare a separate month-costing action instead", false
	default:
		return ErrUnknownTarget, "unknown cost kind " + string(cost.Kind), false
	}

	return ErrNone, "", true
}

// payEventCost deducts a cost that canPayEventCost already accepted.
func payEventCost(s *GameState, cost EventCost, ledger *DeltaLedger, reason string) error {
	if code, detail, ok := canPayEventCost(s, cost); !ok {
		return fmt.Errorf("%s: %s", code, detail)
	}

	switch cost.Kind {
	case CostItem:
		before := heldQuantity(&s.Player.Inventory, cost.ItemID)
		setHeldQuantity(&s.Player.Inventory, cost.ItemID, before-cost.Amount)
		ledger.Entries = append(ledger.Entries, DeltaEntry{
			Field: "items." + cost.ItemID, Before: before, After: before - cost.Amount,
			Reason: reason,
		})
	case CostResource:
		before := s.Player.Resources[cost.Resource]
		s.Player.Resources[cost.Resource] = before - cost.Amount
		ledger.Entries = append(ledger.Entries, DeltaEntry{
			Field: "resources." + string(cost.Resource), Before: before,
			After: before - cost.Amount, Reason: reason,
		})
	case CostHP:
		before := s.Player.HP.Current
		s.Player.HP.Current = before - cost.Amount
		ledger.Entries = append(ledger.Entries, DeltaEntry{
			Field: "hp.current", Before: before, After: before - cost.Amount,
			Reason: reason,
		})
	case CostMP:
		before := s.Player.MP.Current
		s.Player.MP.Current = before - cost.Amount
		ledger.Entries = append(ledger.Entries, DeltaEntry{
			Field: "mp.current", Before: before, After: before - cost.Amount,
			Reason: reason,
		})
	}
	return nil
}

// EvalPrecondition evaluates one declarative condition against the state.
//
// Every condition kind is answered from state alone. Nothing here consults the
// clock, the filesystem or the environment, which is what makes a resume from a
// save reproduce the same eligibility decision.
//
// The key/text split is fixed as follows, and validation enforces it so a
// content pack cannot rely on a reading the evaluator does not implement:
//
//	realm / tier          TextValue = the id; ordered comparison
//	path / origin /
//	spirit_root           TextValue = the id; eq / ne only
//	has_item              Key = item id, Value = quantity
//	resource              Key = resource id, Value = quantity
//	funds                 Value = spirit-stone balance
//	quest_status          Key = quest id, TextValue = status; eq / ne
//	sect_member           Value = 1 for member, 0 for not
//	flag                  Key = flag name, TextValue = "true" or "false"
//	npc_available         Key = npc id; alive and already met
//	world_month /
//	age_months /
//	lifespan_left         Value = the threshold, in the field's own unit
//	consumed_event        Key = event id
//	skill_known           Key = skill id
//	has_debt              Value = 1 for indebted, 0 for not
func EvalPrecondition(s *GameState, cat *Catalogue, p Precondition) (bool, error) {
	if !p.Kind.Valid() {
		return false, fmt.Errorf("unknown condition kind %q", string(p.Kind))
	}
	// Only a comparing kind needs an operator. `npc_available` and
	// `consumed_event` answer from a key alone, and an operator on them would be
	// decoration a reader could mistake for meaning.
	if p.Kind.NeedsOperator() {
		if !p.Op.Valid() {
			return false, fmt.Errorf("condition %q needs a comparison operator", string(p.Kind))
		}
	} else if p.Op != "" && !p.Op.Valid() {
		return false, fmt.Errorf("unknown comparison operator %q", string(p.Op))
	}

	switch p.Kind {
	case CondRealm:
		if s.Player == nil {
			return false, nil
		}
		want, ok := realmOrderOf(Realm(p.TextValue))
		if !ok {
			return false, fmt.Errorf("unknown realm %q", p.TextValue)
		}
		return compareInts(int64(realmOrderOfOr(s.Player.Realm)), int64(want), p.Op), nil

	case CondTier:
		if s.Player == nil {
			return false, nil
		}
		want, ok := tierOrderOf(RealmTier(p.TextValue))
		if !ok {
			return false, fmt.Errorf("unknown tier %q", p.TextValue)
		}
		return compareInts(int64(tierOrderOfOr(s.Player.Tier)), int64(want), p.Op), nil

	case CondPath:
		if s.Player == nil {
			return false, nil
		}
		return compareStrings(string(s.Player.Path), p.TextValue, p.Op)

	case CondOrigin:
		if s.Player == nil {
			return false, nil
		}
		return compareStrings(string(s.Player.Origin), p.TextValue, p.Op)

	case CondSpiritRoot:
		if s.Player == nil {
			return false, nil
		}
		return compareStrings(string(s.Player.SpiritRoot), p.TextValue, p.Op)

	case CondHasItem:
		if s.Player == nil {
			return false, nil
		}
		return compareInts(heldQuantity(&s.Player.Inventory, p.Key), p.Value, p.Op), nil

	case CondResource:
		if s.Player == nil {
			return false, nil
		}
		res := Resource(p.Key)
		if !res.Valid() {
			return false, fmt.Errorf("unknown resource %q", p.Key)
		}
		return compareInts(s.Player.Resources[res], p.Value, p.Op), nil

	case CondFunds:
		if s.Player == nil {
			return false, nil
		}
		return compareInts(s.Player.Resources[ResSpiritStones], p.Value, p.Op), nil

	case CondQuestStatus:
		if s.World == nil {
			return false, nil
		}
		qs, ok := s.World.Quests[p.Key]
		if !ok {
			// An unknown quest is "not in that status" rather than an error:
			// a quest the player has never seen is a legitimate state, and
			// content routinely gates on "not yet accepted".
			return compareStrings("", p.TextValue, p.Op)
		}
		return compareStrings(string(qs.Status), p.TextValue, p.Op)

	case CondSectMember:
		if s.Player == nil {
			return false, nil
		}
		return (s.Player.SectID != "") == (p.Value == 1), nil

	case CondFlag:
		on := false
		if s.World != nil {
			on = s.World.Flags[p.Key]
		}
		want, err := parseBoolText(p.TextValue)
		if err != nil {
			return false, err
		}
		return on == want, nil

	case CondNPCAvailable:
		if s.World == nil || s.Player == nil {
			return false, nil
		}
		npc, ok := s.World.NPCs[p.Key]
		if !ok || !npc.IsAlive {
			return false, nil
		}
		for _, rel := range s.Player.Relations {
			if rel.NPCID == p.Key && rel.Met {
				return true, nil
			}
		}
		return false, nil

	case CondWorldMonth:
		return compareInts(s.Counters.WorldMonth, p.Value, p.Op), nil

	case CondAgeMonths:
		if s.Player == nil {
			return false, nil
		}
		return compareInts(s.Player.AgeMonths, p.Value, p.Op), nil

	case CondLifespanLeft:
		if s.Player == nil {
			return false, nil
		}
		remaining := LifespanMonths(s.Player.Lifespan) - s.Player.AgeMonths
		return compareInts(remaining, p.Value, p.Op), nil

	case CondConsumedEvent:
		if s.World == nil {
			return false, nil
		}
		for _, ce := range s.World.ConsumedEvents {
			if ce.EventID == p.Key {
				return true, nil
			}
		}
		return false, nil

	case CondSkillKnown:
		if s.Player == nil || cat == nil {
			return false, nil
		}
		for _, id := range knownSkillIDs(s.Player, cat) {
			if id == p.Key {
				return true, nil
			}
		}
		return false, nil

	case CondHasDebt:
		if s.Player == nil {
			return false, nil
		}
		return (s.Player.Debt > 0) == (p.Value == 1), nil
	}

	return false, fmt.Errorf("unhandled condition kind %q", string(p.Kind))
}

// EvalPreconditions reports whether every condition holds.
//
// An empty list is satisfied, which is how "always eligible" is expressed.
func EvalPreconditions(s *GameState, cat *Catalogue, list []Precondition) (bool, error) {
	for _, p := range list {
		ok, err := EvalPrecondition(s, cat, p)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// knownSkillIDs collects every skill the player's active techniques grant.
func knownSkillIDs(p *Player, cat *Catalogue) []string {
	if p == nil || cat == nil {
		return nil
	}
	var out []string
	ids := append([]string{p.PrimaryTechniqueID}, p.SecondaryTechniqueIDs...)
	for _, id := range ids {
		if id == "" {
			continue
		}
		if tech := findTechnique(cat, id); tech != nil {
			out = append(out, tech.SkillIDs...)
		}
	}
	return out
}

// compareInts applies a comparison operator to two integers.
func compareInts(left, right int64, op CompareOp) bool {
	switch op {
	case OpEQ:
		return left == right
	case OpNE:
		return left != right
	case OpLT:
		return left < right
	case OpLE:
		return left <= right
	case OpGT:
		return left > right
	case OpGE:
		return left >= right
	}
	return false
}

// compareStrings applies a comparison operator to two strings.
//
// Only equality is defined for strings. An ordering comparison on an origin or
// a spirit root would be an arbitrary convention that content could not
// discover, so it is refused rather than answered.
func compareStrings(left, right string, op CompareOp) (bool, error) {
	switch op {
	case OpEQ:
		return left == right, nil
	case OpNE:
		return left != right, nil
	}
	return false, fmt.Errorf("operator %q is not defined for text comparisons", string(op))
}

// parseBoolText reads the true/false spelling used by flag conditions.
func parseBoolText(text string) (bool, error) {
	switch text {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("a flag condition must say \"true\" or \"false\", got %q", text)
}

// realmOrderOf returns a realm's index in RealmOrder.
func realmOrderOf(r Realm) (int, bool) {
	for i, candidate := range RealmOrder {
		if candidate == r {
			return i, true
		}
	}
	return 0, false
}

// realmOrderOfOr returns a realm's index, or -1 when it is unknown. The caller
// has already validated the player's realm, so -1 is unreachable in practice;
// returning it rather than panicking keeps a corrupt state from taking the
// process down inside a comparison.
func realmOrderOfOr(r Realm) int {
	if i, ok := realmOrderOf(r); ok {
		return i
	}
	return -1
}

// tierOrderOf returns a tier's index in TierOrder.
func tierOrderOf(t RealmTier) (int, bool) {
	for i, candidate := range TierOrder {
		if candidate == t {
			return i, true
		}
	}
	return 0, false
}

// tierOrderOfOr returns a tier's index, or -1 when it is unknown.
func tierOrderOfOr(t RealmTier) int {
	if i, ok := tierOrderOf(t); ok {
		return i
	}
	return -1
}

// trimPrefix returns the remainder of s after prefix, and whether it matched.
func trimPrefix(s, prefix string) (string, bool) {
	if len(s) <= len(prefix) || s[:len(prefix)] != prefix {
		return "", false
	}
	return s[len(prefix):], true
}

// heldQuantity returns how many of itemID the inventory holds.
func heldQuantity(inv *Inventory, itemID string) int64 {
	if inv == nil || itemID == "" {
		return 0
	}
	for _, stack := range inv.Stacks {
		if stack.ItemID == itemID {
			return stack.Quantity
		}
	}
	return 0
}

// setHeldQuantity sets a stack's count, dropping the stack when it reaches zero.
//
// Dropping rather than keeping a zero row matters for the integrity digest: a
// zero-quantity stack and an absent stack mean the same thing to the rules, so
// leaving both in the wild would let two mechanically identical states digest
// differently.
func setHeldQuantity(inv *Inventory, itemID string, quantity int64) {
	if inv == nil || itemID == "" {
		return
	}
	for i := range inv.Stacks {
		if inv.Stacks[i].ItemID != itemID {
			continue
		}
		if quantity == 0 {
			inv.Stacks = append(inv.Stacks[:i], inv.Stacks[i+1:]...)
			return
		}
		inv.Stacks[i].Quantity = quantity
		return
	}
	if quantity > 0 {
		inv.Stacks = append(inv.Stacks, ItemStack{ItemID: itemID, Quantity: quantity})
	}
}

// addChecked adds two int64 values, reporting overflow instead of wrapping.
//
// A wrapped resource total would be a catastrophic silent bug: a large reward
// would turn a rich character into a bankrupt one, and the integrity digest
// would happily record the new value.
func addChecked(a, b int64) (int64, error) {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		return 0, fmt.Errorf("arithmetic overflow adding %d to %d", b, a)
	}
	return sum, nil
}

// mulChecked multiplies two int64 values, reporting overflow instead of
// wrapping. The sign handling follows the standard division check.
func mulChecked(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	product := a * b
	if product/b != a {
		return 0, fmt.Errorf("arithmetic overflow multiplying %d by %d", a, b)
	}
	return product, nil
}

// findItem looks up an item definition.
func findItem(cat *Catalogue, id string) *ItemDefinition {
	if cat == nil {
		return nil
	}
	for i := range cat.Items {
		if cat.Items[i].ID == id {
			return &cat.Items[i]
		}
	}
	return nil
}

// findEvent looks up an event definition.
func findEvent(cat *Catalogue, id string) *EventDefinition {
	if cat == nil {
		return nil
	}
	for i := range cat.Events {
		if cat.Events[i].ID == id {
			return &cat.Events[i]
		}
	}
	return nil
}

// findEventChoice looks up one choice inside an event.
func findEventChoice(ev *EventDefinition, id string) *EventChoice {
	if ev == nil {
		return nil
	}
	for i := range ev.Choices {
		if ev.Choices[i].ID == id {
			return &ev.Choices[i]
		}
	}
	return nil
}

// followUpNode looks up a successor node inside an event's chain.
//
// The entry node is the event itself, so a successor is only ever one of the
// declared FollowUps. Returning nil for an unknown node lets the caller refuse
// the choice rather than silently resolving the instance early.
func followUpNode(ev *EventDefinition, nodeID string) *EventChainNode {
	if ev == nil {
		return nil
	}
	for i := range ev.FollowUps {
		if ev.FollowUps[i].NodeID == nodeID {
			return &ev.FollowUps[i]
		}
	}
	return nil
}

// findSkill looks up a combat skill.
func findSkill(cat *Catalogue, id string) *SkillDefinition {
	if cat == nil {
		return nil
	}
	for i := range cat.Skills {
		if cat.Skills[i].ID == id {
			return &cat.Skills[i]
		}
	}
	return nil
}

// findQuest looks up a quest definition.
func findQuest(cat *Catalogue, id string) *QuestDefinition {
	if cat == nil {
		return nil
	}
	for i := range cat.Quests {
		if cat.Quests[i].ID == id {
			return &cat.Quests[i]
		}
	}
	return nil
}
