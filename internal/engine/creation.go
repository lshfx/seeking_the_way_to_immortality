package engine

// Character creation: the two-step wizard, the point-budget validator, the
// three legal presets, and the factory that turns a confirmed draft into a
// Player.
//
// The rules here are frozen by ADR-001 §2.1 and design 6.1 / 12.1:
//
//   - Six base attributes total exactly 60 points; each base is 1..15.
//   - After origin and talent grants, each *final* attribute must be 1..20.
//     A value out of range is refused outright rather than silently clamped,
//     so the player re-allocates instead of losing points to a hidden ceiling.
//   - Creation age is 16..60; the M1 preset default is 21.
//   - Creation advances no world month and draws no world die. A draft is a
//     separate slot precisely so that editing it cannot touch the world.
//   - A roll that has been locked in (recorded in FixedResults) is never
//     re-rolled, so refreshing or resuming cannot farm a better outcome. That
//     is the mechanical meaning of "不能靠刷新反复抽".
//
// Nothing in this file reads the clock, the filesystem, or an entropy source.

// Creation point-budget and boundary constants. They are exported so the panel
// can display the same numbers the engine enforces rather than restating them.
const (
	// CreationBasePoints is the total pool the six base attributes must sum
	// to exactly. A total below it leaves points unspent; a total above it
	// overdraws. Both are refused.
	CreationBasePoints = 60
	// CreationBaseMin / CreationBaseMax bound one base attribute.
	CreationBaseMin = 1
	CreationBaseMax = 15
	// CreationFinalMin / CreationFinalMax bound one attribute once the
	// origin, constitution and talent grants are applied.
	CreationFinalMin = 1
	CreationFinalMax = 20

	// CreationAgeMinYears / CreationAgeMaxYears bound the creation age.
	CreationAgeMinYears = 16
	CreationAgeMaxYears = 60
	// CreationDefaultAgeYears is the M1 preset default (design 12.1: 默认21岁).
	CreationDefaultAgeYears = 21

	// CreationTotalSteps is the wizard's step count. The design freezes it at
	// two: step one is name/age/appearance plus a preset, step two is the
	// detailed allocation.
	CreationTotalSteps = 2

	// CreationStepIdentity is the 1-based first step.
	CreationStepIdentity = 1
	// CreationStepDetails is the 1-based second step.
	CreationStepDetails = 2

	// CreationTalentPoints is the talent budget noted by design 6.1 (默认5点).
	// It is recorded here so the panel can show it; the per-talent costs are
	// still marked as pending balance values in the content catalogue.
	CreationTalentPoints = 5
)

// CreationAuthor is the byline shown on the first creation screen. Design 6.1
// and ADR-001 §2 both require the first page to carry it, and R06 keeps the
// byline even after the LaTeX output ban, so it is a contract constant rather
// than loose presentation text.
const CreationAuthor = "雾见川"

// CreationAuthorLine is the rendered byline. Keeping the composed string here
// means every renderer shows the identical wording.
const CreationAuthorLine = "作者：" + CreationAuthor

// CreationError codes. Each rejected rule has its own stable code so a test can
// assert *which* rule fired rather than merely that something failed, and so the
// UI can point at the offending field.
type CreationErrorCode string

// Creation validation codes.
const (
	// CErrPointTotal marks a base total that is not exactly 60.
	CErrPointTotal CreationErrorCode = "CREATION_POINT_TOTAL"
	// CErrBaseRange marks a base attribute outside 1..15.
	CErrBaseRange CreationErrorCode = "CREATION_BASE_RANGE"
	// CErrFinalRange marks a final attribute outside 1..20.
	CErrFinalRange CreationErrorCode = "CREATION_FINAL_RANGE"
	// CErrAgeRange marks an age outside 16..60.
	CErrAgeRange CreationErrorCode = "CREATION_AGE_RANGE"
	// CErrUnknownID marks an origin, spirit root, constitution or talent that
	// is not in the catalogue.
	CErrUnknownID CreationErrorCode = "CREATION_UNKNOWN_ID"
	// CErrUnknownPreset marks a preset id that is not declared.
	CErrUnknownPreset CreationErrorCode = "CREATION_UNKNOWN_PRESET"
	// CErrExclusiveTalent marks two mutually exclusive talents taken together.
	CErrExclusiveTalent CreationErrorCode = "CREATION_EXCLUSIVE_TALENT"
	// CErrPathNotOpen marks a path that M1 does not open.
	CErrPathNotOpen CreationErrorCode = "CREATION_PATH_NOT_OPEN"
	// CErrMissingField marks a required descriptive field left empty.
	CErrMissingField CreationErrorCode = "CREATION_MISSING_FIELD"
	// CErrStepOutOfRange marks a step index outside 1..2.
	CErrStepOutOfRange CreationErrorCode = "CREATION_STEP_OUT_OF_RANGE"
	// CErrAlreadyFixed marks an attempt to change a value a roll already
	// locked. It is what makes a refresh unable to re-roll.
	CErrAlreadyFixed CreationErrorCode = "CREATION_ALREADY_FIXED"
)

// CreationDefect is one failed creation rule.
type CreationDefect struct {
	Code   CreationErrorCode `json:"code"`
	Field  string            `json:"field,omitempty"`
	Detail string            `json:"detail"`
}

// Error implements error.
func (d CreationDefect) Error() string {
	if d.Field != "" {
		return string(d.Code) + " at " + d.Field + ": " + d.Detail
	}
	return string(d.Code) + ": " + d.Detail
}

// CreationReport collects every defect rather than stopping at the first, so a
// player sees everything to fix in one pass.
type CreationReport struct {
	Defects []CreationDefect `json:"defects"`
}

// OK reports whether creation passed every rule.
func (r CreationReport) OK() bool { return len(r.Defects) == 0 }

// Codes lists the distinct codes present, in first-seen order.
func (r CreationReport) Codes() []CreationErrorCode {
	seen := make(map[CreationErrorCode]bool, len(r.Defects))
	out := make([]CreationErrorCode, 0, len(r.Defects))
	for _, d := range r.Defects {
		if seen[d.Code] {
			continue
		}
		seen[d.Code] = true
		out = append(out, d.Code)
	}
	return out
}

// Has reports whether a specific code is present.
func (r CreationReport) Has(code CreationErrorCode) bool {
	for _, d := range r.Defects {
		if d.Code == code {
			return true
		}
	}
	return false
}

// add appends a defect.
func (r *CreationReport) add(code CreationErrorCode, field, detail string) {
	r.Defects = append(r.Defects, CreationDefect{Code: code, Field: field, Detail: detail})
}

// checkFixedNotChanged refuses a proposal that moves an already-fixed value.
//
// This is the mechanism behind "刷新/恢复不重复随机初始化": the fixed set is
// written once and read back on resume, and any attempt to write a different
// number through it is rejected rather than honoured.
//
// It lives beside the fixed-key contract rather than in the pipeline because it
// is a rule about the draft's shape, not about command dispatch. Keeping it here
// also means the counter-proof harness can guard it in a file it already owns.
func checkFixedNotChanged(draft *CreationDraft, proposed BaseAttributes) (ErrorCode, string, bool) {
	for _, f := range attributingFields {
		existing, fixed := draft.FixedResults[fixedPrefixAttribute+f.Key]
		if !fixed {
			continue
		}
		if int(existing) != proposed.Get(f.Key) {
			return ErrPreconditionUnmet,
				f.Label + " is already fixed at " + itoa(int(existing)) +
					" and cannot be changed to " + itoa(proposed.Get(f.Key)),
				false
		}
	}
	return ErrNone, "", true
}

// Summary renders the report as one compact line suitable for a rejection
// detail. It names the first defect and counts the rest, so the message stays
// one line while still telling the player how much is wrong.
func (r CreationReport) Summary() string {
	if len(r.Defects) == 0 {
		return ""
	}
	first := r.Defects[0]
	if len(r.Defects) == 1 {
		return string(first.Code) + ": " + first.Detail
	}
	return string(first.Code) + ": " + first.Detail +
		" (and " + itoa(len(r.Defects)-1) + " more)"
}

// Error implements the error interface so a report can be returned directly.
func (r CreationReport) Error() string {
	if r.OK() {
		return ""
	}
	return r.Summary()
}

// attributingField is one of the six allocatable attributes.
type attributingField struct {
	// Key is the stable id used in FixedResults, payloads and the report.
	Key string
	// Label is the Chinese label.
	Label string
}

// AttributingFields lists the six attributes in fixed presentation order. The
// order is a contract: it decides the order defects are reported and the order
// the panel renders their rows.
var attributingFields = []attributingField{
	{Key: "strength", Label: "力道"},
	{Key: "agility", Label: "身法"},
	{Key: "constitution", Label: "根骨"},
	{Key: "comprehension", Label: "悟性"},
	{Key: "aptitude", Label: "资质"},
	{Key: "fortune", Label: "福缘"},
}

// AttributingFieldKeys returns the six attribute keys in canonical order.
func AttributingFieldKeys() []string {
	out := make([]string, 0, len(attributingFields))
	for _, f := range attributingFields {
		out = append(out, f.Key)
	}
	return out
}

// BaseAttributes is an allocation of the six base attributes. It is a struct
// rather than a map so that a typo cannot silently create a seventh attribute
// that no rule ever checks.
type BaseAttributes struct {
	Strength      int
	Agility       int
	Constitution  int
	Comprehension int
	Aptitude      int
	Fortune       int
}

// Total returns the sum of the six base attributes.
func (b BaseAttributes) Total() int {
	return b.Strength + b.Agility + b.Constitution +
		b.Comprehension + b.Aptitude + b.Fortune
}

// Get returns one attribute by canonical key. Unknown keys return 0.
func (b BaseAttributes) Get(key string) int {
	switch key {
	case "strength":
		return b.Strength
	case "agility":
		return b.Agility
	case "constitution":
		return b.Constitution
	case "comprehension":
		return b.Comprehension
	case "aptitude":
		return b.Aptitude
	case "fortune":
		return b.Fortune
	default:
		return 0
	}
}

// With returns a copy with one attribute set. An unknown key returns the
// receiver unchanged, so a malformed input cannot invent an attribute.
func (b BaseAttributes) With(key string, value int) BaseAttributes {
	switch key {
	case "strength":
		b.Strength = value
	case "agility":
		b.Agility = value
	case "constitution":
		b.Constitution = value
	case "comprehension":
		b.Comprehension = value
	case "aptitude":
		b.Aptitude = value
	case "fortune":
		b.Fortune = value
	}
	return b
}

// ValidateBase checks the two rules that apply to the allocation on its own:
// the total is exactly CreationBasePoints, and every base is 1..15.
//
// The point total is checked before the per-field range so that a report for a
// 59-point allocation does not also emit six range defects; the player's first
// problem is that points are unspent, not that a field is fine.
func ValidateBase(b BaseAttributes) CreationReport {
	var r CreationReport

	if total := b.Total(); total != CreationBasePoints {
		detail := "attributes must total exactly 60 points"
		switch {
		case total < CreationBasePoints:
			detail = "attributes total " + itoa(total) + ", " +
				itoa(CreationBasePoints-total) + " point(s) unspent"
		default:
			detail = "attributes total " + itoa(total) + ", " +
				itoa(total-CreationBasePoints) + " point(s) over the budget"
		}
		r.add(CErrPointTotal, "attributes", detail)
	}

	for _, f := range attributingFields {
		v := b.Get(f.Key)
		if v < CreationBaseMin || v > CreationBaseMax {
			r.add(CErrBaseRange, "attributes."+f.Key,
				f.Label+" base value "+itoa(v)+" is outside "+
					itoa(CreationBaseMin)+".."+itoa(CreationBaseMax))
		}
	}

	return r
}

// CreationSelection is the full editable content of a draft: the descriptive
// fields, the content ids and the attribute allocation.
//
// It is carried in the command payload rather than read from the draft, so that
// the pipeline can validate a *proposed* allocation on the clone before it is
// ever written to state. A rejected proposal therefore leaves no trace.
type CreationSelection struct {
	Surname      string     `json:"surname,omitempty"`
	GivenName    string     `json:"given_name,omitempty"`
	DaoName      string     `json:"dao_name,omitempty"`
	Gender       string     `json:"gender,omitempty"`
	Appearance   string     `json:"appearance,omitempty"`
	AgeYears     int        `json:"age_years,omitempty"`
	Origin       Origin     `json:"origin,omitempty"`
	Path         Path       `json:"path,omitempty"`
	SpiritRoot   SpiritRoot `json:"spirit_root,omitempty"`
	Constitution string     `json:"constitution,omitempty"`
	TalentIDs    []string   `json:"talent_ids,omitempty"`
	YaoIntent    bool       `json:"yao_intent,omitempty"`

	Strength         int `json:"strength,omitempty"`
	Agility          int `json:"agility,omitempty"`
	ConstitutionAttr int `json:"constitution_attr,omitempty"`
	Comprehension    int `json:"comprehension,omitempty"`
	Aptitude         int `json:"aptitude,omitempty"`
	Fortune          int `json:"fortune,omitempty"`
}

// Base returns the six-value allocation as a BaseAttributes.
func (s CreationSelection) Base() BaseAttributes {
	return BaseAttributes{
		Strength:      s.Strength,
		Agility:       s.Agility,
		Constitution:  s.ConstitutionAttr,
		Comprehension: s.Comprehension,
		Aptitude:      s.Aptitude,
		Fortune:       s.Fortune,
	}
}

// itoa renders a small int without importing strconv. The engine keeps its
// dependency surface minimal, and every number formatted here is a small
// attribute or point count.
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
