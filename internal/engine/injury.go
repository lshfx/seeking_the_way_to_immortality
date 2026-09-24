package engine

// Injury, and the derived statistics that depend on it.
//
// The central design decision here is that a penalty is *derived*, never
// *accumulated*. Design 6.2 gives 重伤 a 遁速 penalty, and the acceptance
// criterion is that the penalty does not stack: a character who is 重伤 is 30%
// slower, not 30% slower per time the game happens to look. Recomputing from the
// character's base attributes makes that true by construction, whereas
// subtracting from a running total makes it true only as long as nobody calls
// the function twice.

// InjuryBandFor returns the band a current/maximum health pair falls in.
//
// Design 6.2 fixes the boundaries: 0 is 后果判定, (0,33%) is 垂死, [33%,66%) is
// 重伤, [66%,100%) is 轻伤, 100% is healthy. The implementation uses integer
// thresholds and strict less-than, so the bands partition the range exactly:
// there is no value that belongs to two bands and none that belongs to none.
func InjuryBandFor(hp, max int64) InjuryBand {
	if hp <= 0 {
		return BandDown
	}
	if max <= 0 {
		// A character with no ceiling cannot be banded. Reporting healthy for a
		// positive current value keeps the function total rather than inventing
		// a sixth band; the state validator rejects a non-positive ceiling.
		return BandHealthy
	}
	if hp >= max {
		return BandHealthy
	}

	// The thresholds are compared by cross-multiplication rather than by
	// precomputing max*33/100.
	//
	// Precomputing truncates, and truncating moves the boundary: with a ceiling
	// of 99, max*33/100 is 32, so 32 health — which is 32.3%, comfortably inside
	// (0,33%) — was classified as 重伤 instead of 垂死. Multiplying both sides
	// keeps the comparison exact for every ceiling, and int64 has room: health
	// is at most a few hundred million sub-units.
	switch {
	case hp*100 < max*33:
		return BandDying
	case hp*100 < max*66:
		return BandHeavy
	default:
		return BandLight
	}
}

// findInjuryBand looks up a band's configured penalties.
func findInjuryBand(cat *Catalogue, band InjuryBand) *InjuryBandDefinition {
	if cat == nil {
		return nil
	}
	for i := range cat.InjuryBands {
		if cat.InjuryBands[i].Band == band {
			return &cat.InjuryBands[i]
		}
	}
	return nil
}

// InjuryBandName returns the band's display name, or its identifier when the
// catalogue does not configure it.
func InjuryBandName(cat *Catalogue, band InjuryBand) string {
	if def := findInjuryBand(cat, band); def != nil && def.NameZH != "" {
		return def.NameZH
	}
	return string(band)
}

// InjuryPenaltyPermille sums the 遁速 penalties acting on a character.
//
// The band penalty and every active affliction's penalty are added together;
// the sum is applied once, as a fraction of the base speed.
func InjuryPenaltyPermille(p *Player, cat *Catalogue) int {
	if p == nil {
		return 0
	}
	penalty := 0
	if def := findInjuryBand(cat, InjuryBandFor(p.HP.Current, p.HP.Max)); def != nil {
		penalty += def.SpeedPenaltyPermille.Value
	}
	for _, effect := range p.Condition.Effects {
		if affliction := findAffliction(cat, effect.ID); affliction != nil {
			penalty += affliction.SpeedPenaltyPermille.Value
		}
	}
	if penalty < 0 {
		return 0
	}
	if penalty > PermilleScale {
		// A penalty above 100% would make speed negative, which the state
		// contract forbids. Clamping here keeps a content mistake from turning
		// into a broken invariant somewhere else.
		return PermilleScale
	}
	return penalty
}

// refreshDerived recomputes the statistics that injuries influence.
//
// Speed is recomputed from Agility every time, so calling this twice gives the
// same answer. Attack and Defence are deliberately NOT recomputed: they are
// written directly by content effects, and recomputing them from attributes
// would silently discard a granted bonus. Speed has no such grant target, so it
// is the one derived value that is safe to own here.
func refreshDerived(p *Player, cat *Catalogue) {
	if p == nil {
		return
	}
	base := int64(p.Attributes.Agility) * SCALE
	p.Derived.Speed = applyPermillePenalty(base, InjuryPenaltyPermille(p, cat))
}

// applyPermillePenalty reduces a value by a permille fraction of itself.
//
// The result is floored at zero: a penalty may make a character slow, but it may
// not make a statistic negative, because the state contract treats a negative
// derived value as a broken invariant rather than as a very slow character.
func applyPermillePenalty(value int64, penaltyPermille int) int64 {
	if value <= 0 {
		return 0
	}
	if penaltyPermille <= 0 {
		return value
	}
	if penaltyPermille >= PermilleScale {
		return 0
	}
	kept := value * int64(PermilleScale-penaltyPermille) / PermilleScale
	if kept < 0 {
		return 0
	}
	return kept
}
