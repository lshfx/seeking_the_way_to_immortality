package engine

import "fmt"

// Healing, and the month-end cost of carrying an affliction.
//
// Design 13.1 gives 疗伤 a one-month cost, the same as cultivation and explicit
// waiting, so a month of rest is a real alternative to a month of progress
// rather than a free action. That is what makes 忍伤 a decision: carrying a wound
// costs you the month you would have spent mending it.

// Defaults for the two healing numbers, used when a catalogue does not
// configure them. Both are balance values and are configured in the shipped
// content; the defaults exist so a bare test catalogue still behaves.
const (
	// DefaultHealRestorePermille restores a quarter of the ceiling per month.
	DefaultHealRestorePermille = 250
	// DefaultHealConvertBurnPermille spends a tenth of the ceiling per
	// conversion.
	DefaultHealConvertBurnPermille = 100
)

// HealMode selects what a month of 疗伤 does.
type HealMode string

// The two modes.
const (
	// HealRest is 养伤: recover health and let rest-clearable afflictions mend.
	HealRest HealMode = "rest"
	// HealConvert is 气血换灵力: burn health to refill 灵力.
	//
	// It exists because 灵力 has no other way to come back outside combat, and a
	// resource that only ever goes down is not a resource. The trade is
	// deliberately bad — the point is that it is available when the alternative
	// is nothing, not that it is efficient.
	HealConvert HealMode = "convert"
)

// Valid reports whether m is a declared heal mode.
func (m HealMode) Valid() bool {
	switch m {
	case HealRest, HealConvert:
		return true
	default:
		return false
	}
}

// applyHeal runs one month of 疗伤.
//
// The month is charged by the caller through the ordinary parent-action path, so
// this function only decides what the month buys. It refuses rather than clamps
// when a mode cannot be honoured, because a month spent for nothing is worse
// than a refused command.
func (e *Engine) applyHeal(s *GameState, c Command, result CommandResult) CommandResult {
	if s.Player == nil {
		return reject(c, ErrPreconditionUnmet, "there is no character to heal")
	}

	mode := c.Payload.HealMode
	if mode == "" {
		mode = HealRest
	}
	if !mode.Valid() {
		return reject(c, ErrPreconditionUnmet, "unknown healing mode "+string(mode))
	}

	switch mode {
	case HealRest:
		e.healByRest(s, result)
	case HealConvert:
		if code, detail, ok := e.healByConversion(s, result); !ok {
			return reject(c, code, detail)
		}
	}

	// Health changed, so the injury band may have changed with it. Speed is
	// recomputed from the band rather than adjusted, which is what keeps the
	// penalty from stacking.
	refreshDerived(s.Player, e.Catalogue)

	result.MonthCostApplied = 0 // charged when the parent action settles
	return result
}

// healByRest restores health and lets rest-clearable afflictions mend.
func (e *Engine) healByRest(s *GameState, result CommandResult) {
	p := s.Player

	restorePermille := DefaultHealRestorePermille
	if e.Catalogue != nil && e.Catalogue.HealRestorePermille.Provenance != "" {
		restorePermille = e.Catalogue.HealRestorePermille.Value
	}

	before := p.HP.Current
	restore := p.HP.Max * int64(restorePermille) / PermilleScale
	after := before + restore
	if after > p.HP.Max {
		after = p.HP.Max
	}
	p.HP.Current = after
	if after != before {
		result.Delta.Entries = append(result.Delta.Entries, DeltaEntry{
			Field: "hp.current", Before: before, After: after,
			Reason: "养伤一月，气血回复",
		})
	}

	// Rest mends what rest can mend, and only that. A poison and an internal
	// wound are deliberately not rest-clearable: content decides, through
	// ClearedByRest, rather than this function knowing which kinds are "bad
	// enough".
	kept := make([]TimedEffect, 0, len(p.Condition.Effects))
	cleared := 0
	for _, effect := range p.Condition.Effects {
		affliction := findAffliction(e.Catalogue, effect.ID)
		if affliction != nil && affliction.ClearedByRest {
			cleared++
			result.Events = append(result.Events, ResultEvent{
				Kind: "affliction_mended", ID: effect.ID,
				Detail: affliction.NameZH,
			})
			continue
		}
		kept = append(kept, effect)
	}
	if len(kept) == 0 {
		// Preserve nil rather than an empty slice, for the same digest-stability
		// reason decrementEffects documents.
		if len(p.Condition.Effects) > 0 {
			p.Condition.Effects = nil
		}
	} else {
		p.Condition.Effects = kept
	}
	if cleared > 0 {
		result.Events = append(result.Events, ResultEvent{
			Kind: "healed", ID: "rest", Detail: itoa64(int64(cleared)),
		})
	}
}

// healByConversion burns health to refill 灵力.
func (e *Engine) healByConversion(s *GameState, result CommandResult) (ErrorCode, string, bool) {
	p := s.Player

	burnPermille := DefaultHealConvertBurnPermille
	if e.Catalogue != nil && e.Catalogue.HealConvertBurnPermille.Provenance != "" {
		burnPermille = e.Catalogue.HealConvertBurnPermille.Value
	}

	burn := p.HP.Max * int64(burnPermille) / PermilleScale
	if burn <= 0 {
		return ErrPreconditionUnmet, "the configured conversion burns no health, so it can do nothing", false
	}
	// The conversion must not be a way to kill yourself. Requiring the result to
	// stay strictly above zero means the trade can be reckless but never
	// suicidal, and design 6.2's consequence rules stay tied to encounters.
	if p.HP.Current <= burn {
		return ErrPreconditionUnmet,
			"there is not enough health to spare; the conversion would leave none", false
	}
	if p.MP.Current >= p.MP.Max {
		return ErrPreconditionUnmet, "灵力 is already full", false
	}

	hpBefore, mpBefore := p.HP.Current, p.MP.Current
	p.HP.Current = hpBefore - burn

	// One for one: 气血 becomes 灵力. A ratio would be a second configured
	// number for a trade whose whole point is that it is a bad deal.
	gained := burn
	mpAfter := mpBefore + gained
	if mpAfter > p.MP.Max {
		mpAfter = p.MP.Max
	}
	p.MP.Current = mpAfter

	result.Delta.Entries = append(result.Delta.Entries,
		DeltaEntry{
			Field: "hp.current", Before: hpBefore, After: p.HP.Current,
			Reason: "气血换灵力：耗气血",
		},
		DeltaEntry{
			Field: "mp.current", Before: mpBefore, After: mpAfter,
			Reason: "气血换灵力：得灵力",
		},
	)
	result.Events = append(result.Events, ResultEvent{
		Kind: "healed", ID: string(HealConvert),
		Detail: fmt.Sprintf("hp=%d;mp=%d", p.HP.Current, p.MP.Current),
	})
	return ErrNone, "", true
}

// settleAfflictions applies the month-end cost of every active affliction.
//
// It runs after effect durations have ticked down and before the death checks,
// so an affliction that ends this month does not also drain this month: the
// duration is what the player was told, and charging an extra tick would make
// the number on the panel a lie.
func (e *Engine) settleAfflictions(s *GameState, result *CommandResult) {
	if s == nil || s.Player == nil || s.Player.Ended {
		return
	}
	p := s.Player
	if len(p.Condition.Effects) == 0 {
		return
	}

	hpDrain := int64(0)
	moodDrain := 0
	for _, effect := range p.Condition.Effects {
		affliction := findAffliction(e.Catalogue, effect.ID)
		if affliction == nil {
			// An effect whose definition is gone is left alone rather than
			// guessed at; the catalogue validator is where a dangling id is
			// caught.
			continue
		}
		stacks := effect.Stacks
		if stacks <= 0 {
			stacks = 1
		}
		hpDrain += int64(affliction.HPDrainPerMonth.Value) * int64(stacks)
		moodDrain += affliction.MoodDrainPerMonth.Value * stacks
	}

	if hpDrain > 0 {
		before := p.HP.Current
		after := before - hpDrain
		if after < 0 {
			after = 0
		}
		p.HP.Current = after
		result.Delta.Entries = append(result.Delta.Entries, DeltaEntry{
			Field: "hp.current", Before: before, After: after,
			Reason: "伤势与毒发，月末损耗气血",
		})
	}

	if moodDrain != 0 {
		before := int64(p.Condition.Mood)
		after := before - int64(moodDrain)
		if after < 0 {
			after = 0
		}
		if after > 100 {
			after = 100
		}
		p.Condition.Mood = int(after)
		result.Delta.Entries = append(result.Delta.Entries, DeltaEntry{
			Field: "mood", Before: before, After: after,
			Reason: "心境受扰，月末下滑",
		})
	}

	// Health changed, so the band may have too.
	refreshDerived(p, e.Catalogue)
}

// afflictionEndCause reports whether an affliction has taken the character to
// zero health, and why.
//
// Design 6.2 ties the consequences of zero health to the encounter that caused
// it — a spar stops, a capture happens, an existing rescue applies. Outside an
// encounter there is no such rule, so an untreated wound or poison is simply
// fatal. That is the honest reading: inventing a rescue would be exactly the
// "不能临时编造救命" the design forbids.
func afflictionEndCause(p *Player, worldMonth int64) (EndCause, bool) {
	if p == nil || p.Ended {
		return EndCause{}, false
	}
	if p.HP.Current > 0 {
		return EndCause{}, false
	}
	return EndCause{
		Code:       EndCodeInjury,
		WorldMonth: worldMonth,
		Reason:     "an untreated wound or poison drained the last of their health",
	}, true
}
