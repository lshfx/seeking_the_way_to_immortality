package engine

// The read side of the creation state machine: everything a screen needs to
// render the wizard, already resolved.
//
// It lives in the engine rather than in the panel package because resolving a
// draft into displayable strings requires the catalogue (to look up an origin's
// Chinese label, for instance), and the panel package must stay import-free so
// that the engine's boundary test, which imports panel, cannot create a cycle.
// The session layer (TASK-09) reads this projection and passes it to
// panel.BuildCreationBlock.

// CreationView is the fully resolved description of one creation screen.
type CreationView struct {
	// Author is the byline for the first screen.
	Author string `json:"author"`
	// Step / TotalSteps describe the wizard position.
	Step       int `json:"step"`
	TotalSteps int `json:"total_steps"`

	// Identity, already rendered.
	Surname    string `json:"surname"`
	GivenName  string `json:"given_name"`
	DaoName    string `json:"dao_name"`
	Gender     string `json:"gender"`
	Appearance string `json:"appearance"`
	AgeYears   int    `json:"age_years"`

	// Content, resolved to id plus label where the catalogue has one.
	OriginID      Origin       `json:"origin_id"`
	OriginLabel   string       `json:"origin_label"`
	OriginOptions []OptionView `json:"origin_options,omitempty"`

	PathID      Path         `json:"path_id"`
	PathOptions []OptionView `json:"path_options,omitempty"`

	SpiritRootID   SpiritRoot   `json:"spirit_root_id"`
	SpiritRootOpts []OptionView `json:"spirit_root_options,omitempty"`

	ConstitutionID   string       `json:"constitution_id"`
	ConstitutionOpts []OptionView `json:"constitution_options,omitempty"`

	TalentIDs     []string     `json:"talent_ids,omitempty"`
	TalentOptions []OptionView `json:"talent_options,omitempty"`

	// The six attributes, resolved from the fixed allocation.
	Attributes BaseAttributes `json:"attributes"`

	// Point accounting.
	BasePointsSpent int `json:"base_points_spent"`
	BasePointsTotal int `json:"base_points_total"`

	// Presets are the legal quick starts.
	Presets          []CreationPreset `json:"presets,omitempty"`
	SelectedPresetID string           `json:"selected_preset_id,omitempty"`

	// FixedFields lists the attribute keys already locked.
	FixedFields []string `json:"fixed_fields,omitempty"`

	// DraftSaved reports that the draft is outside the world month.
	DraftSaved bool `json:"draft_saved"`

	// YaoIntent / YaoPending describe the deferred judgment.
	YaoIntent  bool `json:"yao_intent,omitempty"`
	YaoPending bool `json:"yao_pending,omitempty"`

	// CanConfirm reports that the draft is ready to finalise.
	CanConfirm bool `json:"can_confirm"`
}

// OptionView is one selectable content option, resolved for display.
type OptionView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Disabled marks an option that is configured but not playable, e.g. a
	// path closed in M1. It is shown rather than hidden so the player is not
	// misled into thinking nothing else exists.
	Disabled bool   `json:"disabled,omitempty"`
	Reason   string `json:"disabled_reason,omitempty"`
}

// BuildCreationView projects a draft into a screen description.
//
// It is pure and total: a nil draft yields a view at step one with no
// selections rather than a panic, because a renderer asking for a screen must
// never crash the game.
func BuildCreationView(cat *Catalogue, draft *CreationDraft) CreationView {
	v := CreationView{
		Author:          CreationAuthorLine,
		Step:            CreationStepIdentity,
		TotalSteps:      CreationTotalSteps,
		BasePointsTotal: CreationBasePoints,
		Presets:         M1CreationPresets(),
		DraftSaved:      true,
	}

	if draft == nil {
		v.Step = CreationStepIdentity
		return v
	}

	if draft.Step >= CreationStepIdentity && draft.Step <= CreationTotalSteps {
		v.Step = draft.Step
	}

	v.Surname = draft.Identity.Surname
	v.GivenName = draft.Identity.GivenName
	v.Gender = draft.Identity.Gender
	v.Appearance = draft.Identity.Appearance
	v.AgeYears = draft.AgeYears
	if v.AgeYears == 0 {
		v.AgeYears = CreationDefaultAgeYears
	}
	v.SelectedPresetID = draft.PresetID
	v.YaoIntent = draft.YaoIntent
	if draft.YaoIntent {
		_, judged := draft.FixedResults[FixedYaoVerdict]
		v.YaoPending = !judged
	}

	v.OriginID = draft.Origin
	v.PathID = draft.Path
	v.SpiritRootID = draft.SpiritRoot
	v.ConstitutionID = draft.Constitution
	v.TalentIDs = append([]string(nil), draft.TalentIDs...)

	if base, ok := draft.FixedAllocation(); ok {
		v.Attributes = base
		v.BasePointsSpent = base.Total()
	}
	for _, f := range attributingFields {
		if draft.IsFixed(f.Key) {
			v.FixedFields = append(v.FixedFields, f.Key)
		}
	}

	// Resolve labels and option lists. Option lists come from the catalogue, so
	// an option the engine cannot resolve is never offered.
	if cat != nil {
		v.OriginOptions = originOptions(cat)
		v.PathOptions = pathOptions(cat)
		v.SpiritRootOpts = spiritRootOptions(cat)
		v.ConstitutionOpts = constitutionOptions(cat)
		v.TalentOptions = talentOptions(cat)

		if def, ok := findOrigin(cat, draft.Origin); ok {
			v.OriginLabel = def.NameZH
		}
	}

	// Confirming needs the allocation to exist and pass every rule.
	if _, ok := draft.FixedAllocation(); ok {
		if ValidateCreation(cat, SelectionFromDraft(draft)).OK() {
			v.CanConfirm = draft.Step >= CreationTotalSteps && !v.YaoPending
		}
	}

	return v
}

// originOptions lists the catalogue's origins in declaration order.
func originOptions(cat *Catalogue) []OptionView {
	out := make([]OptionView, 0, len(cat.Origins))
	for _, o := range cat.Origins {
		out = append(out, OptionView{ID: string(o.ID), Label: o.NameZH})
	}
	return out
}

// pathOptions lists the declared paths, marking the closed ones rather than
// hiding them. The design requires an unopened entrance to be marked "本版尚未
// 开放" or hidden, never shown as available.
func pathOptions(cat *Catalogue) []OptionView {
	open := map[Path]bool{}
	for _, p := range cat.M1Paths {
		open[p] = true
	}

	// The full declared path set, in the order the design lists them.
	all := []struct {
		id    Path
		label string
	}{
		{PathHuman, "人道"},
		{PathEarthly, "地道"},
		{PathHeavenly, "天道"},
	}

	out := make([]OptionView, 0, len(all))
	for _, p := range all {
		opt := OptionView{ID: string(p.id), Label: p.label}
		if !open[p.id] {
			opt.Disabled = true
			opt.Reason = "本版未开放"
		}
		out = append(out, opt)
	}
	return out
}

func spiritRootOptions(cat *Catalogue) []OptionView {
	out := make([]OptionView, 0, len(cat.SpiritRoots))
	for _, s := range cat.SpiritRoots {
		out = append(out, OptionView{ID: string(s.ID), Label: s.NameZH})
	}
	return out
}

func constitutionOptions(cat *Catalogue) []OptionView {
	out := make([]OptionView, 0, len(cat.Constitutions))
	for _, c := range cat.Constitutions {
		out = append(out, OptionView{ID: c.ID, Label: c.NameZH})
	}
	return out
}

func talentOptions(cat *Catalogue) []OptionView {
	out := make([]OptionView, 0, len(cat.Talents))
	for _, t := range cat.Talents {
		out = append(out, OptionView{ID: t.ID, Label: t.NameZH})
	}
	return out
}
