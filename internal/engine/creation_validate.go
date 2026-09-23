package engine

// Creation input validation and catalogue lookups.
//
// Validation is total: it collects every defect into one report so the player
// sees the whole list rather than fixing one field only to discover the next.
// It is also pure: nothing here mutates state, which is what lets the pipeline
// validate a proposal on the clone and then discard it if it fails.

// ValidateCreation checks a selection against the frozen creation rules.
//
// The rule order is deliberate. Structural problems (unknown ids, a closed
// path, an empty required field) are reported alongside the arithmetic ones
// rather than short-circuiting, because a player editing a preset wants to see
// everything at once. The one exception is that the *final* range check is
// skipped when the grants cannot be resolved: without the catalogue entries the
// final values are not defined, so reporting a range defect would be inventing
// a number rather than checking one.
func ValidateCreation(cat *Catalogue, sel CreationSelection) CreationReport {
	var r CreationReport

	// Descriptive fields. A character with no given name cannot be logged, and
	// the profile says so.
	if sel.Surname == "" && sel.GivenName == "" {
		r.add(CErrMissingField, "identity",
			"a surname or a given name is required")
	}

	// Age.
	if sel.AgeYears < CreationAgeMinYears || sel.AgeYears > CreationAgeMaxYears {
		r.add(CErrAgeRange, "age_years",
			"age "+itoa(sel.AgeYears)+" is outside "+
				itoa(CreationAgeMinYears)+".."+itoa(CreationAgeMaxYears))
	}

	// Content ids. An empty id means "not chosen yet" and is allowed for the
	// optional fields; a non-empty id must resolve.
	if sel.Origin != "" {
		if _, ok := findOrigin(cat, sel.Origin); !ok {
			r.add(CErrUnknownID, "origin", "unknown origin "+string(sel.Origin))
		}
	}
	if sel.SpiritRoot != "" {
		if _, ok := findSpiritRoot(cat, sel.SpiritRoot); !ok {
			r.add(CErrUnknownID, "spirit_root",
				"unknown spirit root "+string(sel.SpiritRoot))
		}
	}
	if sel.Constitution != "" {
		if _, ok := findConstitution(cat, sel.Constitution); !ok {
			r.add(CErrUnknownID, "constitution",
				"unknown constitution "+sel.Constitution)
		}
	}
	for _, id := range sel.TalentIDs {
		if _, ok := findTalent(cat, id); !ok {
			r.add(CErrUnknownID, "talents", "unknown talent "+id)
		}
	}

	// Path must be open in M1. This mirrors the state validator's PATH_NOT_OPEN
	// rule so the wizard refuses at edit time rather than at confirm time.
	if sel.Path != "" {
		if !sel.Path.Valid() {
			r.add(CErrUnknownID, "path", "unknown path "+string(sel.Path))
		} else if !sel.Path.OpenInM1() {
			r.add(CErrPathNotOpen, "path",
				"path "+string(sel.Path)+" is not open in M1")
		}
	}

	// Talent exclusivity. Two talents that exclude each other cannot both be
	// taken, and the pair is reported rather than silently dropping one.
	r.Defects = append(r.Defects, validateTalentExclusivity(cat, sel.TalentIDs)...)

	// Attribute arithmetic: total exactly 60, each base in 1..15.
	base := sel.Base()
	r.Defects = append(r.Defects, ValidateBase(base).Defects...)

	// Final range: base + grants must land in 1..20. This is the rule that
	// makes "超限21不可提交" true, and it is why the check cannot be folded into
	// ValidateBase: it depends on the content grants, not on the allocation.
	r.Defects = append(r.Defects, validateFinalRange(cat, sel, base)...)

	return r
}

// validateTalentExclusivity reports any pair of mutually exclusive talents.
func validateTalentExclusivity(cat *Catalogue, ids []string) []CreationDefect {
	if len(ids) < 2 || cat == nil {
		return nil
	}

	var out []CreationDefect
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			a, okA := findTalent(cat, ids[i])
			b, okB := findTalent(cat, ids[j])
			if !okA || !okB {
				continue
			}
			if talentExcludes(a, b.ID) || talentExcludes(b, a.ID) {
				out = append(out, CreationDefect{
					Code:  CErrExclusiveTalent,
					Field: "talents",
					Detail: "talents " + ids[i] + " and " + ids[j] +
						" cannot be taken together",
				})
			}
		}
	}
	return out
}

// talentExcludes reports whether t declares other as exclusive.
func talentExcludes(t TalentDefinition, other string) bool {
	for _, id := range t.Exclusive {
		if id == other {
			return true
		}
	}
	return false
}

// validateFinalRange checks each attribute once the content grants are applied.
func validateFinalRange(cat *Catalogue, sel CreationSelection, base BaseAttributes) []CreationDefect {
	acc, ok := grantAccumulatorFor(cat, sel)
	if !ok {
		// A grant could not be resolved, so the final value is undefined.
		// Reporting a range defect here would be asserting a number the code
		// cannot actually compute; the unresolved id is already reported.
		return nil
	}

	var out []CreationDefect
	for _, f := range attributingFields {
		final := base.Get(f.Key) + acc.attributeAdd[f.Key]
		if final < CreationFinalMin || final > CreationFinalMax {
			out = append(out, CreationDefect{
				Code:  CErrFinalRange,
				Field: "attributes." + f.Key,
				Detail: f.Label + " final value " + itoa(final) +
					" is outside " + itoa(CreationFinalMin) + ".." +
					itoa(CreationFinalMax) +
					" after origin, constitution and talent grants",
			})
		}
	}
	return out
}

// grantAccumulatorFor folds the selection's grants, reporting false when any
// referenced catalogue entry is missing.
func grantAccumulatorFor(cat *Catalogue, sel CreationSelection) (*grantAccumulator, bool) {
	acc := newGrantAccumulator()

	if sel.Origin != "" {
		def, ok := findOrigin(cat, sel.Origin)
		if !ok {
			return nil, false
		}
		for _, e := range def.Effects {
			acc.apply(e)
		}
	}
	if sel.Constitution != "" {
		def, ok := findConstitution(cat, sel.Constitution)
		if !ok {
			return nil, false
		}
		for _, e := range def.Effects {
			acc.apply(e)
		}
	}
	for _, id := range sel.TalentIDs {
		def, ok := findTalent(cat, id)
		if !ok {
			return nil, false
		}
		for _, e := range def.Effects {
			acc.apply(e)
		}
	}

	return acc, true
}

// --- Catalogue lookups -------------------------------------------------------
//
// Each lookup returns (value, false) rather than a zero value for a missing id,
// so a caller cannot mistake "absent" for "present with zero effect".

func findOrigin(cat *Catalogue, id Origin) (OriginDefinition, bool) {
	if cat == nil || id == "" {
		return OriginDefinition{}, false
	}
	for _, o := range cat.Origins {
		if o.ID == id {
			return o, true
		}
	}
	return OriginDefinition{}, false
}

func findSpiritRoot(cat *Catalogue, id SpiritRoot) (SpiritRootDefinition, bool) {
	if cat == nil || id == "" {
		return SpiritRootDefinition{}, false
	}
	for _, s := range cat.SpiritRoots {
		if s.ID == id {
			return s, true
		}
	}
	return SpiritRootDefinition{}, false
}

func findConstitution(cat *Catalogue, id string) (ConstitutionDefinition, bool) {
	if cat == nil || id == "" {
		return ConstitutionDefinition{}, false
	}
	for _, c := range cat.Constitutions {
		if c.ID == id {
			return c, true
		}
	}
	return ConstitutionDefinition{}, false
}

func findTalent(cat *Catalogue, id string) (TalentDefinition, bool) {
	if cat == nil || id == "" {
		return TalentDefinition{}, false
	}
	for _, t := range cat.Talents {
		if t.ID == id {
			return t, true
		}
	}
	return TalentDefinition{}, false
}

func findRealm(cat *Catalogue, id Realm) (RealmDefinition, bool) {
	if cat == nil || id == "" {
		return RealmDefinition{}, false
	}
	for _, r := range cat.Realms {
		if r.ID == id {
			return r, true
		}
	}
	return RealmDefinition{}, false
}
