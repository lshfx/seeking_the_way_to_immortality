// Package wiring holds the code and tests that bridge two packages which must
// not import each other.
//
// ADR-002 keeps internal/engine free of any I/O or presentation dependency, and
// its boundary test enforces that by inspecting the real import graph. Two
// consequences follow, and both are handled here because this is the only
// package allowed to import both sides at once:
//
//   - A number or string declared on both sides can drift. The byline shown on
//     the first creation screen is declared in engine (so the engine can assert
//     its own rule) and in panel (so the renderer needs no engine import). A
//     drift guard test compares them.
//   - Turning an engine draft into a panel screen needs both types, so the
//     adapter lives here rather than in either package.
package wiring

import (
	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
)

// CreationScreen is the panel screen plus the engine view it was built from.
//
// The raw view is returned alongside the block because a renderer often needs
// the un-formatted values (for instance to render an input's current text)
// while the block carries the display strings.
type CreationScreen struct {
	View  engine.CreationView
	Block *panel.CreationBlock
}

// BuildCreationScreen projects an engine draft into a panel creation block.
//
// It is a pure translation: every number comes from the engine's already
// resolved view, and nothing is recomputed. That is what keeps the rule "a
// renderer cannot compute a domain number" true even though two packages are
// involved.
func BuildCreationScreen(cat *engine.Catalogue, draft *engine.CreationDraft) CreationScreen {
	view := engine.BuildCreationView(cat, draft)

	in := panel.CreationViewInput{
		Author:     view.Author,
		Step:       view.Step,
		TotalSteps: view.TotalSteps,

		Surname:    view.Surname,
		GivenName:  view.GivenName,
		DaoName:    view.DaoName,
		Gender:     view.Gender,
		Appearance: view.Appearance,
		AgeYears:   view.AgeYears,

		OriginID:         string(view.OriginID),
		OriginLabel:      view.OriginLabel,
		OriginOptions:    optionLabels(view.OriginOptions),
		SpiritRootID:     string(view.SpiritRootID),
		SpiritRootOpts:   optionLabels(view.SpiritRootOpts),
		Constitution:     view.ConstitutionID,
		ConstitutionOpts: optionLabels(view.ConstitutionOpts),
		PathID:           string(view.PathID),
		PathOptions:      optionLabels(view.PathOptions),
		TalentIDs:        append([]string(nil), view.TalentIDs...),
		TalentOptions:    optionLabels(view.TalentOptions),

		Strength:         view.Attributes.Strength,
		Agility:          view.Attributes.Agility,
		ConstitutionAttr: view.Attributes.Constitution,
		Comprehension:    view.Attributes.Comprehension,
		Aptitude:         view.Attributes.Aptitude,
		Fortune:          view.Attributes.Fortune,

		BasePointsSpent:  view.BasePointsSpent,
		BasePointsTotal:  view.BasePointsTotal,
		Presets:          presetViews(view.Presets),
		SelectedPresetID: view.SelectedPresetID,
		FixedFields:      append([]string(nil), view.FixedFields...),
		DraftSaved:       view.DraftSaved,
		YaoIntent:        view.YaoIntent,
		YaoPending:       view.YaoPending,
	}

	return CreationScreen{View: view, Block: panel.BuildCreationBlock(in)}
}

// optionLabels renders engine options as display labels.
//
// A disabled option keeps its label but gains the reason as a suffix, so a
// path closed in M1 reads "天道（本版未开放）" rather than looking selectable.
func optionLabels(opts []engine.OptionView) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		label := o.Label
		if o.Disabled && o.Reason != "" {
			label += "（" + o.Reason + "）"
		}
		out = append(out, label)
	}
	return out
}

// presetViews renders the engine presets as panel preset views.
func presetViews(presets []engine.CreationPreset) []panel.PresetView {
	out := make([]panel.PresetView, 0, len(presets))
	for _, p := range presets {
		out = append(out, panel.PresetView{
			ID:          p.ID,
			Label:       p.Label,
			SummaryText: p.SummaryText,
		})
	}
	return out
}
