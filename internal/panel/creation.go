package panel

// Character-creation screen construction.
//
// The panel package deliberately imports nothing, including the engine, because
// the engine's boundary test forbids importing panel and a reverse import would
// create a cycle. So this file takes plain values rather than an engine draft:
// the session layer (TASK-09) reads the draft and passes the numbers in. That
// keeps the "a renderer cannot compute a domain number" rule intact while still
// making the creation screen's shape a tested contract.

// CreationViewInput is everything the creation screen needs, already resolved
// from the draft by the session. No field is derived here.
type CreationViewInput struct {
	// Author is the byline. The session passes panel.CreationAuthorLine; it is
	// a parameter so a test can prove the screen carries whatever it is given.
	Author string

	Step       int
	TotalSteps int

	// Identity fields, already rendered to strings.
	Surname    string
	GivenName  string
	DaoName    string
	Gender     string
	Appearance string
	AgeYears   int

	// Content choices.
	OriginID         string
	OriginLabel      string
	OriginOptions    []string
	SpiritRootID     string
	SpiritRootOpts   []string
	Constitution     string
	ConstitutionOpts []string
	PathID           string
	PathOptions      []string
	TalentIDs        []string
	TalentOptions    []string

	// The six attribute values, already resolved.
	Strength         int
	Agility          int
	ConstitutionAttr int
	Comprehension    int
	Aptitude         int
	Fortune          int

	// BasePointsTotal is the budget; BasePointsSpent is the current sum.
	BasePointsSpent int
	BasePointsTotal int

	// Presets are the legal quick starts.
	Presets []PresetView
	// SelectedPresetID marks the active preset.
	SelectedPresetID string

	// FixedFields lists attribute keys already locked, so the renderer can show
	// them as non-editable rather than offering an edit that will be refused.
	FixedFields []string

	// Summary lists already-fixed results.
	Summary []DetailRow

	// DraftSaved reports that the draft is persisted outside the world.
	DraftSaved bool

	// YaoIntent / YaoPending describe the deferred 妖族 judgment.
	YaoIntent  bool
	YaoPending bool
}

// CreationAuthorLine is the byline rendered on the first creation screen. It is
// declared here, in the presentation contract, so the session does not restate
// the wording; the engine's copies exist so the engine can assert its own rule
// without importing panel.
const CreationAuthor = "雾见川"
const CreationAuthorLine = "作者：" + CreationAuthor

// Step titles.
const (
	CreationStepIdentityTitle = "第一步：姓名与形貌"
	CreationStepDetailsTitle  = "第二步：详细设定"
)

// field ids, so a renderer can key on them without matching on the label.
//
// The content-choice ids carry a "choice_" prefix because two of them would
// otherwise collide with attribute keys: the 根骨 attribute is keyed
// "constitution" and so is the 体质 content choice, and the two rows are
// different things. A collision would make find-by-id ambiguous, which is the
// kind of defect a renderer hits as a silently wrong row rather than an error.
const (
	FieldSurname      = "surname"
	FieldGivenName    = "given_name"
	FieldDaoName      = "dao_name"
	FieldGender       = "gender"
	FieldAppearance   = "appearance"
	FieldAge          = "age_years"
	FieldOrigin       = "choice_origin"
	FieldPath         = "choice_path"
	FieldSpiritRoot   = "choice_spirit_root"
	FieldConstitution = "choice_constitution"
	FieldTalents      = "choice_talents"
)

// CreationAttributeFields returns the six attribute field ids in canonical
// order, matching the engine's order so a renderer and the engine agree on
// which row is which.
func CreationAttributeFields() []string {
	return []string{
		"strength", "agility", "constitution",
		"comprehension", "aptitude", "fortune",
	}
}

// CreationAttributeLabels maps an attribute key to its Chinese label.
func CreationAttributeLabels() map[string]string {
	return map[string]string{
		"strength":      "力道",
		"agility":       "身法",
		"constitution":  "根骨",
		"comprehension": "悟性",
		"aptitude":      "资质",
		"fortune":       "福缘",
	}
}

// BuildCreationBlock builds the wizard view.
//
// It only formats and arranges; it computes no domain value. The point total is
// passed in rather than summed, and the step title is selected rather than
// derived from a state machine.
func BuildCreationBlock(in CreationViewInput) *CreationBlock {
	block := &CreationBlock{
		Author:           in.Author,
		Step:             in.Step,
		TotalSteps:       in.TotalSteps,
		StepTitle:        creationStepTitle(in.Step),
		Presets:          append([]PresetView(nil), in.Presets...),
		SelectedPresetID: in.SelectedPresetID,
		BasePointsSpent:  in.BasePointsSpent,
		BasePointsTotal:  in.BasePointsTotal,
		DraftSaved:       in.DraftSaved,
		Summary:          append([]DetailRow(nil), in.Summary...),
		YaoIntent:        in.YaoIntent,
		YaoPending:       in.YaoPending,
	}

	fixed := map[string]bool{}
	for _, k := range in.FixedFields {
		fixed[k] = true
	}

	// Step one collects the descriptive identity fields; step two collects the
	// mechanical ones. Splitting them this way is the design's two-step freeze.
	if in.Step <= 1 {
		block.Fields = append(block.Fields,
			CreationField{
				ID: FieldSurname, Label: "姓", Value: in.Surname,
				Kind: "text", Editable: true,
				Hint: "留空则用道号代替",
			},
			CreationField{
				ID: FieldGivenName, Label: "名", Value: in.GivenName,
				Kind: "text", Editable: true,
			},
			CreationField{
				ID: FieldDaoName, Label: "道号", Value: in.DaoName,
				Kind: "text", Editable: true,
				Hint: "可留空；称号常用于江湖记述",
			},
			CreationField{
				ID: FieldGender, Label: "性别", Value: in.Gender,
				Kind: "text", Editable: true,
				// Stated plainly because the engine stores it verbatim: there
				// is no normalisation a player needs to work around.
				Hint: "可自定义，不会强制改写",
			},
			CreationField{
				ID: FieldAppearance, Label: "外观", Value: in.Appearance,
				Kind: "text", Editable: true,
			},
			CreationField{
				ID: FieldAge, Label: "年龄", Value: itoa(in.AgeYears),
				Kind: "text", Editable: true,
				Hint: "十六至六十岁",
			},
		)
		return block
	}

	// Step two: the mechanical allocation.
	block.Fields = append(block.Fields,
		CreationField{
			ID: FieldOrigin, Label: "出身", Value: in.OriginLabel,
			Kind: "choice", Options: in.OriginOptions, Editable: true,
		},
		CreationField{
			ID: FieldPath, Label: "道途", Value: in.PathID,
			Kind: "choice", Options: in.PathOptions, Editable: true,
			Hint: "本版只开放人道",
		},
		CreationField{
			ID: FieldSpiritRoot, Label: "灵根", Value: in.SpiritRootID,
			Kind: "choice", Options: in.SpiritRootOpts, Editable: true,
		},
		CreationField{
			ID: FieldConstitution, Label: "体质", Value: in.Constitution,
			Kind: "choice", Options: in.ConstitutionOpts, Editable: true,
		},
		CreationField{
			ID: FieldTalents, Label: "天赋", Value: joinIDs(in.TalentIDs),
			Kind: "choice", Options: in.TalentOptions, Editable: true,
			Hint: "五点天赋预算；未实现的预设不会带入",
		},
	)

	labels := CreationAttributeLabels()
	values := map[string]int{
		"strength":      in.Strength,
		"agility":       in.Agility,
		"constitution":  in.ConstitutionAttr,
		"comprehension": in.Comprehension,
		"aptitude":      in.Aptitude,
		"fortune":       in.Fortune,
	}
	for _, key := range CreationAttributeFields() {
		hint := "基础一到十五；加出身与天赋后须在一到二十"
		if fixed[key] {
			hint = "已固定，不能再改"
		}
		block.Fields = append(block.Fields, CreationField{
			ID:       key,
			Label:    labels[key],
			Value:    itoa(values[key]),
			Kind:     "text",
			Editable: !fixed[key],
			Hint:     hint,
		})
	}

	return block
}

// creationStepTitle returns the title for a step, defaulting to the details
// title for any out-of-range value so a corrupt step cannot render an empty
// heading.
func creationStepTitle(step int) string {
	if step <= 1 {
		return CreationStepIdentityTitle
	}
	return CreationStepDetailsTitle
}

// itoa renders a small int. The panel keeps its own copy so the package stays
// import-free.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// joinIDs renders a list of ids with a comma separator for display. A comma is
// chosen because it reads naturally in a field value; ids never contain one.
func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ", "
		}
		out += id
	}
	return out
}
