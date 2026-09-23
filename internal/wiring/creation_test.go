package wiring

import (
	"testing"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
)

// TestAuthorBylineDoesNotDrift is the drift guard for the byline.
//
// The string is declared twice on purpose: engine must be able to assert its
// own rule without importing panel, and panel must stay import-free so the
// engine's boundary test can import it. A silent divergence would mean the
// engine believes the screen is signed while the screen is not.
func TestAuthorBylineDoesNotDrift(t *testing.T) {
	if engine.CreationAuthor != panel.CreationAuthor {
		t.Fatalf("author drifted: engine=%q panel=%q",
			engine.CreationAuthor, panel.CreationAuthor)
	}
	if engine.CreationAuthorLine != panel.CreationAuthorLine {
		t.Fatalf("author line drifted: engine=%q panel=%q",
			engine.CreationAuthorLine, panel.CreationAuthorLine)
	}
	if engine.CreationAuthorLine != "作者：雾见川" {
		t.Fatalf("author line is %q, want 作者：雾见川", engine.CreationAuthorLine)
	}
}

// TestFourteenCreationConstantsDoNotDrift is the drift guard for the numbers the
// two packages both declare. The panel needs them to render "54/60" and to
// label a step without importing engine.
func TestCreationConstantsDoNotDrift(t *testing.T) {
	if engine.CreationTotalSteps != 2 {
		t.Fatalf("engine total steps is %d, want 2", engine.CreationTotalSteps)
	}
	// The panel's attribute field list must match the engine's, in order,
	// because the renderer and the validator walk the same rows.
	want := engine.AttributingFieldKeys()
	got := panel.CreationAttributeFields()
	if len(want) != len(got) {
		t.Fatalf("attribute field count drifted: engine=%v panel=%v", want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("attribute field %d drifted: engine=%q panel=%q",
				i, want[i], got[i])
		}
	}
}

// TestFirstScreenCarriesTheByline is the end-to-end form of acceptance criterion
// one: a freshly opened draft renders a step-one screen whose Author field is
// the byline.
func TestFirstScreenCarriesTheByline(t *testing.T) {
	cat := engine.Catalogue{
		Version: engine.ContentVersion,
		M1Paths: []engine.Path{engine.PathHuman},
	}
	draft := engine.NewCreationDraft("game", "branch")

	screen := BuildCreationScreen(&cat, draft)
	if screen.Block.Author != "作者：雾见川" {
		t.Fatalf("the first screen's author is %q, want 作者：雾见川",
			screen.Block.Author)
	}
	if screen.Block.Step != 1 {
		t.Fatalf("a new draft opens on step %d, want 1", screen.Block.Step)
	}

	// Step one must offer the identity fields, not the attribute rows.
	ids := fieldIDs(screen.Block)
	if !containsStr(ids, panel.FieldGender) {
		t.Fatalf("step one does not offer the gender field: %v", ids)
	}
	if containsStr(ids, "comprehension") {
		t.Fatalf("step one offers an attribute row: %v", ids)
	}

	// The preset list must be present so the quick path is available.
	if len(screen.Block.Presets) != 3 {
		t.Fatalf("step one offers %d presets, want 3", len(screen.Block.Presets))
	}
	if screen.Block.SelectedPresetID != engine.DefaultPresetID {
		t.Fatalf("selected preset is %q, want the default %q",
			screen.Block.SelectedPresetID, engine.DefaultPresetID)
	}
}

// TestNoDuplicateCreationFieldIDs is a regression guard for a real collision: the
// 根骨 attribute is keyed "constitution" and the 体质 content choice was too, so
// a renderer looking a row up by id could get either one. The content choices
// are now namespaced; this test fails if any collision is reintroduced.
func TestNoDuplicateCreationFieldIDs(t *testing.T) {
	cat := engine.Catalogue{
		Version: engine.ContentVersion,
		M1Paths: []engine.Path{engine.PathHuman},
	}

	for _, step := range []int{engine.CreationStepIdentity, engine.CreationStepDetails} {
		draft := engine.NewCreationDraft("game", "branch")
		draft.Step = step

		screen := BuildCreationScreen(&cat, draft)
		seen := map[string]bool{}
		for _, f := range screen.Block.Fields {
			if seen[f.ID] {
				t.Fatalf("step %d renders two fields with id %q", step, f.ID)
			}
			seen[f.ID] = true
		}
	}
}

// TestDetailsStepShowsFixedAttributes confirms the second step renders the six
// attribute rows and marks the fixed ones non-editable, so the renderer does not
// offer an edit the engine will refuse.
func TestDetailsStepShowsFixedAttributes(t *testing.T) {
	cat := engine.Catalogue{
		Version: engine.ContentVersion,
		M1Paths: []engine.Path{engine.PathHuman},
	}
	draft := engine.NewCreationDraft("game", "branch")
	draft.Step = engine.CreationStepDetails

	screen := BuildCreationScreen(&cat, draft)
	if screen.Block.TotalSteps != 2 {
		t.Fatalf("total steps is %d, want 2", screen.Block.TotalSteps)
	}

	// Every attribute row is present, and each is marked non-editable because
	// the draft fixed the preset's allocation when it opened.
	for _, key := range engine.AttributingFieldKeys() {
		f, ok := findField(screen.Block, key)
		if !ok {
			t.Fatalf("the details step does not render the %q row", key)
		}
		if f.Editable {
			t.Fatalf("the %q row is editable but the value is already fixed", key)
		}
	}

	// The point accounting is the budget total, taken from the engine.
	if screen.Block.BasePointsTotal != engine.CreationBasePoints {
		t.Fatalf("budget shown is %d, want %d",
			screen.Block.BasePointsTotal, engine.CreationBasePoints)
	}
	if screen.Block.BasePointsSpent != engine.CreationBasePoints {
		t.Fatalf("the default preset spends %d of %d points",
			screen.Block.BasePointsSpent, engine.CreationBasePoints)
	}
}

// TestClosedPathsAreMarkedNotHidden checks the design rule that an unopened
// entrance is marked rather than silently omitted.
func TestClosedPathsAreMarkedNotHidden(t *testing.T) {
	cat := engine.Catalogue{
		Version: engine.ContentVersion,
		M1Paths: []engine.Path{engine.PathHuman},
	}
	draft := engine.NewCreationDraft("game", "branch")
	draft.Step = engine.CreationStepDetails

	view := engine.BuildCreationView(&cat, draft)
	if len(view.PathOptions) != 3 {
		t.Fatalf("there are %d path options, want all three declared paths",
			len(view.PathOptions))
	}

	var human, heavenly engine.OptionView
	for _, o := range view.PathOptions {
		switch o.ID {
		case string(engine.PathHuman):
			human = o
		case string(engine.PathHeavenly):
			heavenly = o
		}
	}
	if human.Disabled {
		t.Fatal("the human path is disabled but M1 opens it")
	}
	if !heavenly.Disabled || heavenly.Reason == "" {
		t.Fatalf("the heavenly path is not marked closed: %+v", heavenly)
	}
}

// TestPanelModelCarriesTheCreationBlock confirms the block lands in the model's
// Creation slot, which is where the renderer looks for it.
func TestPanelModelCarriesTheCreationBlock(t *testing.T) {
	cat := engine.Catalogue{
		Version: engine.ContentVersion,
		M1Paths: []engine.Path{engine.PathHuman},
	}
	draft := engine.NewCreationDraft("game", "branch")
	screen := BuildCreationScreen(&cat, draft)

	m := panel.Model{SchemaVersion: panel.SchemaVersion, Creation: screen.Block}
	if m.Creation == nil {
		t.Fatal("the model has no creation block")
	}
	if m.Creation.Author == "" {
		t.Fatal("the creation block carries no author")
	}
}

// --- helpers -----------------------------------------------------------------

func fieldIDs(b *panel.CreationBlock) []string {
	out := make([]string, 0, len(b.Fields))
	for _, f := range b.Fields {
		out = append(out, f.ID)
	}
	return out
}

func findField(b *panel.CreationBlock, id string) (panel.CreationField, bool) {
	for _, f := range b.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return panel.CreationField{}, false
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
