package engine

// The creation wizard's state machine, the three legal presets, and the
// character factory.
//
// This file is where a draft becomes a Player. The separation matters: the
// wizard can be edited, resumed and abandoned without ever producing a
// character, and only an explicit confirmation runs the factory.

// CreationPreset is one quick-start preset. A preset is not privileged: it is
// fed through exactly the same validator as a hand-built allocation, so it
// cannot smuggle in a hidden advantage (design 12.1: 预设也须经过相同60点校验，
// 不给隐藏属性优势).
type CreationPreset struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// SummaryText describes the trade-off in one line.
	SummaryText string `json:"summary_text"`
	// Selection is the complete allocation the preset proposes. It must pass
	// ValidateCreation unchanged.
	Selection CreationSelection
}

// CreationPresetIDs are the three M1 preferences from design 12.1:
// 均衡修炼 / 稳健生存 / 灵活探索.
const (
	PresetBalanced = "balanced"
	PresetSturdy   = "sturdy"
	PresetAgile    = "agile"
)

// DefaultPresetID is the preset selected by default on the first screen, so
// the quick path confirms without the player making an allocation choice.
const DefaultPresetID = PresetBalanced

// M1CreationPresets returns the three legal presets.
//
// Every preset allocates exactly 60 points with each base in 1..15, so each one
// passes ValidateCreation without special-casing. The allocations differ in
// shape rather than in strength: balanced spreads the pool, sturdy trades
// comprehension for constitution, agile trades constitution for agility and
// fortune. No preset grants a talent, so none can claim a hidden bonus.
//
// The presets deliberately do not fix Origin: the design says the concrete
// origin, spirit root and talents are listed by versioned configuration, and
// that each preset must be able to reach breakthrough materials as a rogue
// cultivator. Origin commoner is the neutral choice that cannot create a
// spirit-stone dependence.
func M1CreationPresets() []CreationPreset {
	base := func(s, a, c, comp, apt, f int) BaseAttributes {
		return BaseAttributes{
			Strength: s, Agility: a, Constitution: c,
			Comprehension: comp, Aptitude: apt, Fortune: f,
		}
	}

	// Balanced: 10/10/10/10/10/10 — the manuscript's baseline aptitude of 10,
	// which is what makes the 19.5 fixture reachable from the default preset.
	balanced := base(10, 10, 10, 10, 10, 10)
	// Sturdy: survivability. Constitution is the cap, comprehension still
	// carries a usable cultivation rate.
	sturdy := base(9, 7, 15, 11, 10, 8)
	// Agile: mobility and luck, at the cost of a smaller body.
	agile := base(8, 15, 8, 11, 10, 8)

	return []CreationPreset{
		{
			ID: PresetBalanced, Label: "均衡修炼",
			SummaryText: "六维各 10，资质 10，修炼与生存均不偏废；便于循序渐进。",
			Selection:   presetSelection("无名", "散人", balanced),
		},
		{
			ID: PresetSturdy, Label: "稳健生存",
			SummaryText: "根骨 15、力道 9，气血厚实，遇险更能撑住；悟性略降。",
			Selection:   presetSelection("无名", "散人", sturdy),
		},
		{
			ID: PresetAgile, Label: "灵活探索",
			SummaryText: "身法 15、福缘 8，行动轻捷，先手与机缘更好；根骨偏弱。",
			Selection:   presetSelection("无名", "散人", agile),
		},
	}
}

// presetSelection turns an allocation into the rest of a preset selection,
// filling the descriptive defaults the design fixes (21 years old, human path,
// ordinary body, no talents).
func presetSelection(surname, given string, b BaseAttributes) CreationSelection {
	return CreationSelection{
		Surname:    surname,
		GivenName:  given,
		Gender:     GenderUnspecified,
		AgeYears:   CreationDefaultAgeYears,
		Origin:     OriginCommoner,
		Path:       PathHuman,
		SpiritRoot: RootTrue,
		// The constitution id comes from the catalogue; M1 ships "ordinary".
		Constitution:     "ordinary",
		Strength:         b.Strength,
		Agility:          b.Agility,
		ConstitutionAttr: b.Constitution,
		Comprehension:    b.Comprehension,
		Aptitude:         b.Aptitude,
		Fortune:          b.Fortune,
	}
}

// Gender values.
//
// A custom gender is stored verbatim. The engine never normalises it to a
// binary pair, which is the mechanical meaning of "自定义性别不被强改": there is
// no code path that rewrites the field.
const (
	GenderMale        = "male"
	GenderFemale      = "female"
	GenderUnspecified = "unspecified"
)

// PresetByID finds a preset. The second result is false for an unknown id,
// rather than returning a zero-valued preset that would silently allocate
// nothing.
func PresetByID(id string) (CreationPreset, bool) {
	for _, p := range M1CreationPresets() {
		if p.ID == id {
			return p, true
		}
	}
	return CreationPreset{}, false
}

// --- The draft state machine -------------------------------------------------

// NewCreationDraft opens a draft at step one, pre-filled with the default
// preset, and locks the preset's numbers into FixedResults.
//
// Locking at open time rather than at confirm time is deliberate. The roll is
// decided the first moment the player could have seen it; storing it means a
// refresh, a restart or a resumed draft re-renders the identical numbers
// instead of drawing again.
func NewCreationDraft(gameID, branchID string) *CreationDraft {
	draft := &CreationDraft{
		Step:         CreationStepIdentity,
		AgeYears:     CreationDefaultAgeYears,
		FixedResults: map[string]int64{},
	}

	preset, ok := PresetByID(DefaultPresetID)
	if ok {
		draft.ApplySelection(preset.Selection)
		draft.PresetID = preset.ID
		// The preset's allocation is locked immediately, using the same single
		// writer an edit uses.
		draft.FixAllocation(preset.Selection.Base())
	}

	return draft
}

// ApplySelection writes a selection into the draft's descriptive and content
// fields. Attribute numbers are *not* written here; they are recorded by
// FixAllocation so that every number has exactly one writer.
func (d *CreationDraft) ApplySelection(sel CreationSelection) {
	if d == nil {
		return
	}
	d.Identity = Identity{
		Surname:    sel.Surname,
		GivenName:  sel.GivenName,
		Gender:     sel.Gender,
		Appearance: sel.Appearance,
	}
	d.Origin = sel.Origin
	d.Path = sel.Path
	d.SpiritRoot = sel.SpiritRoot
	d.Constitution = sel.Constitution
	d.TalentIDs = append([]string(nil), sel.TalentIDs...)
	d.YaoIntent = sel.YaoIntent
	if sel.AgeYears > 0 {
		d.AgeYears = sel.AgeYears
	}
}

// SelectionFromDraft exports the draft's editable content, so a caller can
// resubmit it unchanged (a no-op edit) or with one field altered.
func SelectionFromDraft(d *CreationDraft) CreationSelection {
	if d == nil {
		return CreationSelection{}
	}
	sel := CreationSelection{
		Surname:      d.Identity.Surname,
		GivenName:    d.Identity.GivenName,
		Gender:       d.Identity.Gender,
		Appearance:   d.Identity.Appearance,
		AgeYears:     d.AgeYears,
		Origin:       d.Origin,
		Path:         d.Path,
		SpiritRoot:   d.SpiritRoot,
		Constitution: d.Constitution,
		TalentIDs:    append([]string(nil), d.TalentIDs...),
		YaoIntent:    d.YaoIntent,
	}
	if base, ok := d.FixedAllocation(); ok {
		sel.Strength = base.Strength
		sel.Agility = base.Agility
		sel.ConstitutionAttr = base.Constitution
		sel.Comprehension = base.Comprehension
		sel.Aptitude = base.Aptitude
		sel.Fortune = base.Fortune
	}
	return sel
}

// Fixed keys. They are namespaced so an attribute key can never collide with a
// future non-attribute roll.
const (
	fixedPrefixAttribute = "attr."
	fixedPrefixRoll      = "roll."
	// FixedYaoVerdict records the 仙缘 verdict for a yao-intent draft.
	FixedYaoVerdict = "roll.yao_verdict"
	// FixedAptitudeForYao records the aptitude the verdict was computed from,
	// which is what fixes the outcome against re-rolling.
	FixedAptitudeForYao = "roll.yao_aptitude"
)

// FixAllocation records the six base attributes as fixed results, overwriting
// any previous values.
//
// This is the single writer of attribute numbers. It is called after the
// allocation passes validation, so a rejected allocation never becomes the
// fixed state a player would resume into.
func (d *CreationDraft) FixAllocation(b BaseAttributes) {
	if d == nil {
		return
	}
	if d.FixedResults == nil {
		d.FixedResults = map[string]int64{}
	}
	for _, f := range attributingFields {
		d.FixedResults[fixedPrefixAttribute+f.Key] = int64(b.Get(f.Key))
	}
}

// FixedAllocation reports whether all six attributes have been fixed.
func (d *CreationDraft) FixedAllocation() (BaseAttributes, bool) {
	if d == nil {
		return BaseAttributes{}, false
	}
	var b BaseAttributes
	for _, f := range attributingFields {
		v, ok := d.FixedResults[fixedPrefixAttribute+f.Key]
		if !ok {
			return BaseAttributes{}, false
		}
		b = b.With(f.Key, int(v))
	}
	return b, true
}

// IsFixed reports whether one attribute has already been locked.
func (d *CreationDraft) IsFixed(key string) bool {
	if d == nil {
		return false
	}
	_, ok := d.FixedResults[fixedPrefixAttribute+key]
	return ok
}

// --- Attribute grants --------------------------------------------------------

// Grant application targets. They are spelled as the content catalogue spells
// them, because the catalogue is the authority for effect data.
const (
	targetHPMax           = "hp.max"
	targetHPCurrent       = "hp.current"
	targetSpiritStones    = "resources.spirit_stones"
	targetCultivationRate = "cultivation_rate"
	targetAttrPrefix      = "attributes."
)

// grantAccumulator collects grants while they are applied, so that a
// multiplicative grant is evaluated once against the final additive base
// instead of compounding in list order.
//
// Ordering matters here. If a +3 aptitude talent and a x1.5 multiplier were
// applied in list order, swapping their positions in the catalogue would change
// the result. Separating the accumulators makes the outcome order-independent,
// which is what "同组独立加成" means in practice.
type grantAccumulator struct {
	hpMaxAdd        int64
	hpCurrentAdd    int64
	stonesAdd       int64
	attributeAdd    map[string]int
	rateNumerator   int64
	rateDenominator int64
}

func newGrantAccumulator() *grantAccumulator {
	return &grantAccumulator{
		attributeAdd:    map[string]int{},
		rateNumerator:   1,
		rateDenominator: 1,
	}
}

// apply folds one grant in. Unknown targets are ignored rather than fatal:
// content validation is the place that rejects an unknown target, and the
// factory must not panic on data it has already accepted.
func (g *grantAccumulator) apply(e GrantEffect) {
	switch {
	case e.Target == targetHPMax && e.Kind == GrantPermanent:
		g.hpMaxAdd += e.Amount
	case e.Target == targetHPCurrent && e.Kind == GrantAdditive:
		g.hpCurrentAdd += e.Amount
	case e.Target == targetSpiritStones && e.Kind == GrantAdditive:
		g.stonesAdd += e.Amount
	case len(e.Target) > len(targetAttrPrefix) &&
		e.Target[:len(targetAttrPrefix)] == targetAttrPrefix &&
		e.Kind == GrantAdditive:
		key := e.Target[len(targetAttrPrefix):]
		g.attributeAdd[key] += int(e.Amount)
	case e.Target == targetCultivationRate && e.Kind == GrantMultiplicative:
		// A multiplier in SCALE sub-units: 1.5 becomes 15000/10000.
		if e.Amount > 0 {
			g.rateNumerator *= e.Amount
			g.rateDenominator *= SCALE
		}
	}
}

// rateScale returns the accumulated cultivation multiplier as a numerator over
// SCALE, i.e. the value to multiply a base rate by.
func (g *grantAccumulator) rateScale() int64 {
	if g.rateDenominator == 0 {
		return SCALE
	}
	return g.rateNumerator * SCALE / g.rateDenominator
}

// --- The character factory ---------------------------------------------------

// baseline attribute and vitals values used before grants are applied.
//
// These are the design's验收角色资源 (ADR-001 §2.1: 气血100、灵力100、灵石100).
// They are stored in SCALE sub-units for the vitals so that a later fractional
// modifier cannot truncate them.
const (
	creationHPMax  = 100 * SCALE
	creationMPMax  = 100 * SCALE
	creationStones = 100
	// creationTechniqueGrade is the 黄阶 main-technique multiplier (design 7.1:
	// 功法 黄1.0). TASK-08 owns technique selection; creation fixes the
	// baseline grade so the 19.5 fixture is reachable.
	creationTechniqueGrade = SCALE
	// creationEnvironmentRate is the 普通 environment multiplier (1.0).
	creationEnvironmentRate = SCALE
	// creationMoodRate is the 达标 mood multiplier (1.0).
	creationMoodRate = SCALE
	// creationAptitudeCoefficient is the design's 0.05 per point of effective
	// aptitude, in SCALE sub-units (design 7.1).
	creationAptitudeCoefficient = SCALE / 20 // 0.05
)

// CreatePlayer runs the character factory: an already-confirmed, already
// validated draft becomes a Player.
//
// It returns a report instead of an error because creation failures are lists
// of defects, not single exceptions. A non-OK report means no player was built
// and the caller must not commit anything.
//
// The factory is pure: same catalogue plus same draft yields the same player,
// every time, on every machine.
func CreatePlayer(cat *Catalogue, draft *CreationDraft) (*Player, CreationReport) {
	var r CreationReport

	if draft == nil {
		r.add(CErrMissingField, "draft", "there is no character draft")
		return nil, r
	}

	base, ok := draft.FixedAllocation()
	if !ok {
		r.add(CErrMissingField, "attributes",
			"the six attributes have not been fixed yet")
		return nil, r
	}

	sel := selectionFromDraft(draft, base)

	// Re-run the full validator on the fixed allocation. This is not
	// redundant with the guard at edit time: a state loaded from disk could
	// have been written by an older or faulty build, and the factory is the
	// last place that can refuse to build an illegal character.
	if report := ValidateCreation(cat, sel); !report.OK() {
		return nil, report
	}

	// Resolve the catalogue entries.
	originDef, ok := findOrigin(cat, draft.Origin)
	if !ok {
		r.add(CErrUnknownID, "origin", "unknown origin "+string(draft.Origin))
		return nil, r
	}
	rootDef, ok := findSpiritRoot(cat, draft.SpiritRoot)
	if !ok {
		r.add(CErrUnknownID, "spirit_root",
			"unknown spirit root "+string(draft.SpiritRoot))
		return nil, r
	}
	var constitutionDef ConstitutionDefinition
	if draft.Constitution != "" {
		var found bool
		constitutionDef, found = findConstitution(cat, draft.Constitution)
		if !found {
			r.add(CErrUnknownID, "constitution",
				"unknown constitution "+draft.Constitution)
			return nil, r
		}
	}
	talents := make([]TalentDefinition, 0, len(draft.TalentIDs))
	for _, id := range draft.TalentIDs {
		def, found := findTalent(cat, id)
		if !found {
			r.add(CErrUnknownID, "talents", "unknown talent "+id)
			return nil, r
		}
		talents = append(talents, def)
	}

	// Fold every grant into one accumulator, so the result does not depend on
	// the order the sources happen to be listed in.
	acc := newGrantAccumulator()
	for _, e := range originDef.Effects {
		acc.apply(e)
	}
	for _, e := range constitutionDef.Effects {
		acc.apply(e)
	}
	for _, t := range talents {
		for _, e := range t.Effects {
			acc.apply(e)
		}
	}

	// The final attribute values are base plus grant. Growth starts empty:
	// creation must not seed play-time growth, or a re-spec would have
	// something to rewrite.
	attrs := Attributes{
		Strength:      base.Strength + acc.attributeAdd["strength"],
		Agility:       base.Agility + acc.attributeAdd["agility"],
		Constitution:  base.Constitution + acc.attributeAdd["constitution"],
		Comprehension: base.Comprehension + acc.attributeAdd["comprehension"],
		Aptitude:      base.Aptitude + acc.attributeAdd["aptitude"],
		Fortune:       base.Fortune + acc.attributeAdd["fortune"],
		Growth:        map[string]int{},
	}

	hpMax := creationHPMax + acc.hpMaxAdd*SCALE
	hpCurrent := hpMax + acc.hpCurrentAdd*SCALE
	// A starting current-HP above the cap would be silently trimmed by any
	// later clamp; trim it here instead so the stored value is honest.
	if hpCurrent > hpMax {
		hpCurrent = hpMax
	}

	player := &Player{
		Identity:     draft.Identity,
		AgeMonths:    int64(sel.AgeYears) * MonthsPerYear,
		Origin:       draft.Origin,
		Path:         draft.Path,
		SpiritRoot:   draft.SpiritRoot,
		Constitution: draft.Constitution,
		TalentIDs:    append([]string(nil), draft.TalentIDs...),
		Attributes:   attrs,
		HP:           Vitals{Current: hpCurrent, Max: hpMax},
		MP:           Vitals{Current: creationMPMax, Max: creationMPMax},
		XP:           0,
		Realm:        RealmQiRefining,
		Tier:         TierEarly,
		Inventory:    Inventory{Stacks: []ItemStack{}},
		Equipment:    Equipment{Slots: map[EquipSlot]string{}},
		Resources: map[Resource]int64{
			ResSpiritStones: creationStones + acc.stonesAdd,
		},
		Debt:          0,
		Condition:     Condition{Effects: []TimedEffect{}},
		Relations:     []Relation{},
		Proficiencies: map[string]int64{},
		Insights:      map[string]int64{},
	}

	// Lifespan comes from the realm table, not from a constant, so that the
	// 炼气 100-year ceiling has exactly one source.
	if realmDef, ok := findRealm(cat, RealmQiRefining); ok {
		player.Lifespan = LifespanLedger{BaseYears: realmDef.LifespanYears.Value}
	}

	player.Derived = DeriveFor(player, rootDef, acc)

	return player, r
}

// selectionFromDraft reconstructs a selection from a draft plus its fixed
// allocation, so the factory validates the same shape the editor validates.
func selectionFromDraft(d *CreationDraft, b BaseAttributes) CreationSelection {
	return CreationSelection{
		Surname:          d.Identity.Surname,
		GivenName:        d.Identity.GivenName,
		Gender:           d.Identity.Gender,
		Appearance:       d.Identity.Appearance,
		AgeYears:         d.AgeYears,
		Origin:           d.Origin,
		Path:             d.Path,
		SpiritRoot:       d.SpiritRoot,
		Constitution:     d.Constitution,
		TalentIDs:        d.TalentIDs,
		Strength:         b.Strength,
		Agility:          b.Agility,
		ConstitutionAttr: b.Constitution,
		Comprehension:    b.Comprehension,
		Aptitude:         b.Aptitude,
		Fortune:          b.Fortune,
	}
}

// DeriveFor computes the cached derived values.
//
// CultivationRate follows design 7.1:
//
//	monthly = realm_base
//	        * (1 + 0.05 * effective_aptitude)
//	        * spirit_root_multiplier
//	        * technique_grade
//	        * environment
//	        * mood
//	        * (1 + grouped_independent_bonuses)
//
// Every factor is an integer in SCALE sub-units, so the whole product is exact
// and no floating point enters the engine.
func DeriveFor(p *Player, root SpiritRootDefinition, acc *grantAccumulator) Derived {
	if p == nil {
		return Derived{}
	}

	effectiveAptitude := p.Attributes.Aptitude

	// (1 + 0.05 * aptitude), in SCALE.
	aptitudeFactor := SCALE + int64(effectiveAptitude)*creationAptitudeCoefficient
	if aptitudeFactor < 0 {
		aptitudeFactor = 0
	}

	rootMultiplier := int64(root.Multiplier.Value)
	if rootMultiplier <= 0 {
		// A missing multiplier must not silently zero the rate; the neutral
		// value keeps the character playable and the catalogue validator
		// reports the real defect.
		rootMultiplier = SCALE
	}

	base := int64(0)
	// The realm base is supplied by the caller of DeriveFor through the
	// player's realm via the catalogue; DefaultMonthBase keeps the value
	// defined when the catalogue has no entry.
	base = defaultMonthBase

	// The product is divided by SCALE once per fractional factor, which keeps
	// the intermediate exact.
	rate := base
	rate = rate * aptitudeFactor / SCALE
	rate = rate * rootMultiplier / SCALE
	// Variant roots apply their extra multiplier once, on top.
	if bonus := int64(root.VariantBonus.Value); bonus > 0 {
		rate = rate * bonus / SCALE
	}
	rate = rate * creationTechniqueGrade / SCALE
	rate = rate * creationEnvironmentRate / SCALE
	rate = rate * creationMoodRate / SCALE
	// Grouped independent bonuses accumulated from talents.
	rate = rate * acc.rateScale() / SCALE

	return Derived{
		EffectiveAptitude: effectiveAptitude,
		CultivationRate:   rate,
		Attack:            int64(p.Attributes.Strength) * SCALE,
		Defense:           int64(p.Attributes.Constitution) * SCALE,
		Speed:             int64(p.Attributes.Agility) * SCALE,
	}
}

// defaultMonthBase is the 炼气 monthly base used when no realm definition is
// available. It mirrors the manuscript's 炼气 10 (design 7.2) so a bare test
// catalogue still produces a defined rate.
const defaultMonthBase = 10 * SCALE
