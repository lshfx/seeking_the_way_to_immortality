package engine

import (
	"errors"
	"fmt"
)

const (
	StartingMood                = 50
	CultivationAptitudePerPoint = SCALE / 20
	MoodAboveMultiplier         = 12 * SCALE / 10
	MoodAtMultiplier            = SCALE
	MoodBelowMultiplier         = SCALE / 2
	ClosedCultivationMultiplier = 2 * SCALE
)

var ErrCultivationUnavailable = errors.New("cultivation cannot be calculated from this state")

// CultivationBonusView is a named term that explains why the forecast differs
// from the character's unmodified base rate.
type CultivationBonusView struct {
	ID        string
	Name      string
	Group     string
	RateBonus int64
	Reason    string
}

// CultivationPreview contains both the formula result and the next ordinary
// month outcome. It is safe for a panel to display because it performs no
// mutation, random draw, or clock advancement.
type CultivationPreview struct {
	Rate                  int64
	CurrentXP             int64
	Threshold             int64
	NextGain              int64
	XPAfterNextAction     int64
	ProgressBasisPoints   int64
	MonthsToThreshold     int64
	EffectiveAptitude     int
	MoodBand              MoodBand
	PrimaryTechniqueID    string
	SecondaryTechniqueIDs []string
	Bonuses               []CultivationBonusView
	AtThreshold           bool
}

// PreviewCultivation computes a fixed-point monthly rate and the next action's
// capped result. ActionClosed is available to balancing tools, but the M1
// command pipeline intentionally exposes only ActionNormal until TASK-23.
func PreviewCultivation(p *Player, world *World, cat *Catalogue, action ActionKind) (CultivationPreview, error) {
	var out CultivationPreview
	if p == nil || cat == nil {
		return out, fmt.Errorf("%w: player and catalogue are required", ErrCultivationUnavailable)
	}
	if action == "" {
		action = ActionNormal
	}
	if !action.Valid() {
		return out, fmt.Errorf("%w: unknown action intensity %q", ErrCultivationUnavailable, action)
	}

	realm, ok := findRealm(cat, p.Realm)
	if !ok || realm.MonthBase.Value <= 0 || realm.TierThreshold.Value <= 0 {
		return out, fmt.Errorf("%w: realm %q has no positive base or threshold", ErrCultivationUnavailable, p.Realm)
	}
	root, ok := findSpiritRoot(cat, p.SpiritRoot)
	if !ok || root.Multiplier.Value <= 0 {
		return out, fmt.Errorf("%w: spirit root %q is not configured", ErrCultivationUnavailable, p.SpiritRoot)
	}

	primaryID := p.PrimaryTechniqueID
	if primaryID == "" {
		primaryID = defaultPrimaryTechniqueID(cat)
	}
	primary := findTechnique(cat, primaryID)
	if primary == nil || !primary.IsPrimary || primary.GradeMultiplier.Value <= 0 {
		return out, fmt.Errorf("%w: primary technique %q is unavailable", ErrCultivationUnavailable, primaryID)
	}

	active := make([]TechniqueDefinition, 0, 1+len(p.SecondaryTechniqueIDs))
	active = append(active, *primary)
	seen := map[string]bool{primaryID: true}
	for _, id := range p.SecondaryTechniqueIDs {
		if id == "" || seen[id] {
			return out, fmt.Errorf("%w: secondary technique IDs must be non-empty and unique", ErrCultivationUnavailable)
		}
		technique := findTechnique(cat, id)
		if technique == nil || technique.IsPrimary {
			return out, fmt.Errorf("%w: %q is not configured as a secondary technique", ErrCultivationUnavailable, id)
		}
		seen[id] = true
		active = append(active, *technique)
	}

	thresholdMood := StartingMood
	if cat.CultivationMoodThreshold.Provenance != "" {
		thresholdMood = cat.CultivationMoodThreshold.Value
	}
	if thresholdMood < 0 || thresholdMood > 100 || p.Condition.Mood < 0 || p.Condition.Mood > 100 {
		return out, fmt.Errorf("%w: mood and its threshold must be between 0 and 100", ErrCultivationUnavailable)
	}
	moodBand := MoodAt
	moodMultiplier := int64(MoodAtMultiplier)
	if p.Condition.Mood > thresholdMood {
		moodBand, moodMultiplier = MoodAbove, MoodAboveMultiplier
	} else if p.Condition.Mood < thresholdMood {
		moodBand, moodMultiplier = MoodBelow, MoodBelowMultiplier
	}

	environment := EnvNormal
	if world != nil && world.CurrentLocation != "" {
		for _, location := range cat.Locations {
			if location.ID == world.CurrentLocation && location.Environment.Valid() {
				environment = location.Environment
				break
			}
		}
	}
	environmentMultiplier := environmentRate(environment)
	if environmentMultiplier <= 0 {
		return out, fmt.Errorf("%w: environment %q has no positive multiplier", ErrCultivationUnavailable, environment)
	}

	effectiveAptitude := p.Attributes.Aptitude + p.Attributes.Growth["aptitude"]
	groups := map[string]int64{}
	bonuses := make([]CultivationBonusView, 0)
	addEffect := func(effect GrantEffect, sourceID, sourceName string) {
		if effect.Kind != GrantMultiplicative || effect.Target != targetCultivationRate || effect.Amount <= 0 {
			return
		}
		group := effect.Group
		if group == "" {
			group = "default"
		}
		bonus := effect.Amount - SCALE
		groups[group] += bonus
		bonuses = append(bonuses, CultivationBonusView{
			ID: sourceID, Name: sourceName, Group: group,
			RateBonus: bonus, Reason: effect.Reason,
		})
	}

	for _, technique := range active {
		for _, effect := range technique.Effects {
			addEffect(effect, technique.ID, technique.NameZH)
		}
	}
	for _, effect := range creationRateEffects(p, cat) {
		addEffect(effect, effect.Target, effect.Reason)
	}
	for _, activeEffect := range p.Condition.Effects {
		modifier := findCultivationModifier(cat, activeEffect.ID)
		if modifier == nil {
			// Other condition effects (poison, injury narration, etc.) are
			// owned by their own systems and do not change this formula.
			continue
		}
		stacks := activeEffect.Stacks
		if stacks <= 0 {
			stacks = 1
		}
		effectiveAptitude += modifier.EffectiveAptitudeDelta.Value * stacks
		bonus := int64(modifier.RateBonus.Value) * int64(stacks)
		groups[modifier.Group] += bonus
		bonuses = append(bonuses, CultivationBonusView{
			ID: modifier.ID, Name: modifier.NameZH, Group: modifier.Group,
			RateBonus: bonus, Reason: "限时状态修正",
		})
	}
	if effectiveAptitude < 0 {
		effectiveAptitude = 0
	}

	aptitudeMultiplier := int64(SCALE) + int64(effectiveAptitude)*CultivationAptitudePerPoint
	rootMultiplier := int64(root.Multiplier.Value)
	techniqueMultiplier := int64(primary.GradeMultiplier.Value) // secondary grades never stack here
	if root.VariantBonus.Value > 0 {
		rootMultiplier = rootMultiplier * int64(root.VariantBonus.Value) / SCALE
	}
	groupMultiplier, err := groupedRateMultiplier(groups)
	if err != nil {
		return out, err
	}
	actionMultiplier := int64(SCALE)
	if action == ActionClosed {
		actionMultiplier = ClosedCultivationMultiplier
	}

	rate := int64(realm.MonthBase.Value)
	for _, factor := range []int64{
		aptitudeMultiplier, rootMultiplier, techniqueMultiplier,
		environmentMultiplier, moodMultiplier, actionMultiplier, groupMultiplier,
	} {
		rate, err = multiplyScale(rate, factor)
		if err != nil {
			return out, fmt.Errorf("%w: %v", ErrCultivationUnavailable, err)
		}
	}
	if rate <= 0 {
		return out, fmt.Errorf("%w: resulting monthly rate is not positive", ErrCultivationUnavailable)
	}

	threshold := int64(realm.TierThreshold.Value)
	if p.XP < 0 {
		return out, fmt.Errorf("%w: cultivation progress cannot be negative", ErrCultivationUnavailable)
	}
	if p.XP > threshold {
		return out, fmt.Errorf("%w: cultivation progress exceeds the current tier threshold", ErrCultivationUnavailable)
	}
	remaining := threshold - p.XP
	nextGain := rate
	if nextGain > remaining {
		nextGain = remaining
	}
	months := int64(0)
	if remaining > 0 {
		months = (remaining-1)/rate + 1
	}
	progress := int64(10000)
	if threshold > 0 {
		progress = p.XP * int64(SCALE) / threshold
		if progress > int64(SCALE) {
			progress = int64(SCALE)
		}
	}

	secondary := append([]string(nil), p.SecondaryTechniqueIDs...)
	out = CultivationPreview{
		Rate: rate, CurrentXP: p.XP, Threshold: threshold,
		NextGain: nextGain, XPAfterNextAction: p.XP + nextGain,
		ProgressBasisPoints: progress, MonthsToThreshold: months,
		EffectiveAptitude: effectiveAptitude, MoodBand: moodBand,
		PrimaryTechniqueID: primaryID, SecondaryTechniqueIDs: secondary,
		Bonuses: bonuses, AtThreshold: remaining == 0,
	}
	return out, nil
}

func creationRateEffects(p *Player, cat *Catalogue) []GrantEffect {
	if p == nil || cat == nil {
		return nil
	}
	out := make([]GrantEffect, 0)
	for _, origin := range cat.Origins {
		if origin.ID == p.Origin {
			out = append(out, origin.Effects...)
			break
		}
	}
	for _, constitution := range cat.Constitutions {
		if constitution.ID == p.Constitution {
			out = append(out, constitution.Effects...)
			break
		}
	}
	for _, id := range p.TalentIDs {
		for _, talent := range cat.Talents {
			if talent.ID == id {
				out = append(out, talent.Effects...)
				break
			}
		}
	}
	return out
}

func groupedRateMultiplier(groups map[string]int64) (int64, error) {
	rate := int64(SCALE)
	for _, group := range sortedKeys(groups) {
		factor := int64(SCALE) + groups[group]
		if factor <= 0 {
			return 0, fmt.Errorf("rate-bonus group %q reduces cultivation to zero", group)
		}
		var err error
		rate, err = multiplyScale(rate, factor)
		if err != nil {
			return 0, err
		}
	}
	return rate, nil
}

func multiplyScale(value, factor int64) (int64, error) {
	if value < 0 || factor < 0 {
		return 0, errors.New("negative scale operand")
	}
	if factor != 0 && value > (int64(^uint64(0)>>1))/factor {
		return 0, errors.New("fixed-point multiplication overflow")
	}
	return value * factor / SCALE, nil
}

func environmentRate(environment Environment) int64 {
	switch environment {
	case EnvBarren:
		return 6 * SCALE / 10
	case EnvNormal:
		return SCALE
	case EnvRich:
		return 15 * SCALE / 10
	case EnvBlessed:
		return 2 * SCALE
	case EnvGrotto:
		return 25 * SCALE / 10
	default:
		return 0
	}
}

func findTechnique(cat *Catalogue, id string) *TechniqueDefinition {
	if cat == nil {
		return nil
	}
	for i := range cat.Techniques {
		if cat.Techniques[i].ID == id {
			return &cat.Techniques[i]
		}
	}
	return nil
}

func findPrimaryTechnique(cat *Catalogue, grade TechniqueGrade) *TechniqueDefinition {
	if cat == nil {
		return nil
	}
	for i := range cat.Techniques {
		if cat.Techniques[i].IsPrimary && cat.Techniques[i].Grade == grade {
			return &cat.Techniques[i]
		}
	}
	return nil
}

func defaultPrimaryTechniqueID(cat *Catalogue) string {
	if technique := findPrimaryTechnique(cat, GradeYellow); technique != nil {
		return technique.ID
	}
	return ""
}

func findCultivationModifier(cat *Catalogue, id string) *CultivationModifierDefinition {
	if cat == nil {
		return nil
	}
	for i := range cat.CultivationModifiers {
		if cat.CultivationModifiers[i].ID == id {
			return &cat.CultivationModifiers[i]
		}
	}
	return nil
}
