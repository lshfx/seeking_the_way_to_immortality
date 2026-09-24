package engine

// Karma, lifespan projection, and the status detail the panel reads.
//
// Design R19 is the rule that shapes this file: "因果按行为和情境，不对所有击杀一刀
// 切". Karma is therefore NOT a rule of the engine. There is no code path that
// grants karma for killing something, or for any other act in the abstract;
// karma moves only when a named content effect moves it, and every such effect
// carries the reason it appears in the ledger. What this file adds is the
// vocabulary for describing a karma total, and the projection a status screen
// needs.

// KarmaTierFor returns the tier a 业力 total falls in.
//
// The highest matching floor wins, so the result does not depend on the order
// the catalogue happens to list the tiers in. A total below every floor gets no
// tier, which the validator prevents by requiring a tier at zero.
func KarmaTierFor(cat *Catalogue, karma int64) *KarmaTierDefinition {
	if cat == nil {
		return nil
	}
	var best *KarmaTierDefinition
	for i := range cat.KarmaTiers {
		tier := &cat.KarmaTiers[i]
		if int64(tier.MinKarma.Value) > karma {
			continue
		}
		if best == nil || tier.MinKarma.Value > best.MinKarma.Value {
			best = tier
		}
	}
	return best
}

// KarmaTierName returns the tier's display name, or an empty string when the
// catalogue does not describe this total.
func KarmaTierName(cat *Catalogue, karma int64) string {
	if tier := KarmaTierFor(cat, karma); tier != nil {
		return tier.NameZH
	}
	return ""
}

// Karma returns the character's 业力 total.
func Karma(p *Player) int64 {
	if p == nil {
		return 0
	}
	return p.Resources[ResKarma]
}

// AfflictionView is one active affliction as the status screen shows it.
type AfflictionView struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Kind AfflictionKind `json:"kind"`
	// MonthsRemaining is what the player was told, so the panel can say when it
	// will be over rather than only that it hurts.
	MonthsRemaining int `json:"months_remaining"`
	Stacks          int `json:"stacks"`
	// ClearsByRest tells the player whether a month of 养伤 will help, which is
	// the difference between resting and wasting a month.
	ClearsByRest bool   `json:"clears_by_rest"`
	Description  string `json:"description,omitempty"`
}

// StatusDetail is the engine's projection of everything a status screen needs.
//
// It is built from committed state and computes nothing the rules have not
// already decided: the band, the remaining lifespan and the karma tier are all
// derived here so that the panel never has to re-derive them and disagree.
type StatusDetail struct {
	Realm Realm     `json:"realm"`
	Tier  RealmTier `json:"tier"`

	HP Vitals `json:"hp"`
	MP Vitals `json:"mp"`
	XP int64  `json:"xp"`

	Band     InjuryBand `json:"band"`
	BandName string     `json:"band_name"`

	AgeMonths               int64 `json:"age_months"`
	LifespanMonths          int64 `json:"lifespan_months"`
	LifespanRemainingMonths int64 `json:"lifespan_remaining_months"`

	Afflictions []AfflictionView `json:"afflictions,omitempty"`

	Karma     int64  `json:"karma"`
	KarmaTier string `json:"karma_tier,omitempty"`

	Ended    bool     `json:"ended"`
	EndCause EndCause `json:"end_cause,omitempty"`
}

// BuildStatusDetail projects the character's state for a status screen.
//
// A nil player yields a zero detail rather than an error: the status screen is
// reachable during creation, where there is no character yet.
func BuildStatusDetail(s *GameState, cat *Catalogue) StatusDetail {
	var d StatusDetail
	if s == nil || s.Player == nil {
		return d
	}
	p := s.Player

	d.Realm = p.Realm
	d.Tier = p.Tier
	d.HP = p.HP
	d.MP = p.MP
	d.XP = p.XP

	d.Band = InjuryBandFor(p.HP.Current, p.HP.Max)
	d.BandName = InjuryBandName(cat, d.Band)

	d.AgeMonths = p.AgeMonths
	d.LifespanMonths = LifespanMonths(p.Lifespan)
	d.LifespanRemainingMonths = d.LifespanMonths - p.AgeMonths
	if d.LifespanRemainingMonths < 0 {
		// A character past their ceiling is already ended; reporting a negative
		// remainder would let a screen print "余 -3 年".
		d.LifespanRemainingMonths = 0
	}

	for _, effect := range p.Condition.Effects {
		view := AfflictionView{
			ID:              effect.ID,
			MonthsRemaining: effect.MonthsRemaining,
			Stacks:          effect.Stacks,
		}
		if affliction := findAffliction(cat, effect.ID); affliction != nil {
			view.Name = affliction.NameZH
			view.Kind = affliction.Kind
			view.ClearsByRest = affliction.ClearedByRest
			view.Description = affliction.Description
		} else {
			// An effect the catalogue no longer describes still shows up, under
			// its id, rather than vanishing from the screen while still
			// affecting the rules.
			view.Name = effect.ID
		}
		d.Afflictions = append(d.Afflictions, view)
	}

	d.Karma = Karma(p)
	d.KarmaTier = KarmaTierName(cat, d.Karma)
	d.Ended = p.Ended
	d.EndCause = p.EndCause
	return d
}

// hasLifespanLeft reports whether the character may still spend a month.
//
// Design 7.4: "开始时剩余0不能行动". A character who has reached their ceiling is
// normally already ENDED, because the month that took them there ended them; this
// guard covers the state a save written by a future build could contain, where
// the age and the phase disagree.
func hasLifespanLeft(s *GameState) bool {
	if s == nil || s.Player == nil {
		return true
	}
	return !LifespanExhausted(s.Player.AgeMonths, s.Player.Lifespan)
}
