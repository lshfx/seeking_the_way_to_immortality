package storage

import (
	"errors"
	"strings"
	"testing"
)

// bumpVersion is a minimal migration that only changes the declared version.
// Real migrations map content ids; this one exists so the chain mechanics can be
// tested without pretending to model a schema change.
func bumpVersion(from, to int) Migration {
	return Migration{
		From: from,
		To:   to,
		Apply: func(doc map[string]any) error {
			if got, _ := doc["envelope_version"].(int); got != from {
				return errors.New("the document does not declare the version this step expects")
			}
			return nil
		},
	}
}

func refusingMigration(from, to int) Migration {
	return Migration{
		From: from,
		To:   to,
		Apply: func(map[string]any) error {
			return errors.New("a referenced content id no longer exists")
		},
	}
}

func TestPlanMigrationNeedsNoStepsForTheCurrentVersion(t *testing.T) {
	plan, err := PlanMigration(1, 1, nil)
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}
	if plan.RequiresMigration() {
		t.Error("a document already at the target version was given migration steps")
	}
}

// TestPlanMigrationBuildsAnOrderedChain is the basic composition guarantee. A
// chain applied out of order would produce a document with mixed versions.
func TestPlanMigrationBuildsAnOrderedChain(t *testing.T) {
	available := []Migration{bumpVersion(3, 4), bumpVersion(1, 2), bumpVersion(2, 3)}

	plan, err := PlanMigration(1, 4, available)
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("chain length = %d, want 3", len(plan.Steps))
	}
	for i, step := range plan.Steps {
		if want := i + 1; step.From != want || step.To != want+1 {
			t.Errorf("step %d is %d->%d, want %d->%d", i, step.From, step.To, want, want+1)
		}
	}
}

// TestPlanMigrationRefusesAGapRatherThanStoppingPartWay is the property the
// whole design hinges on. Applying three of four steps and then giving up would
// leave a document declaring a version it does not have.
func TestPlanMigrationRefusesAGapRatherThanStoppingPartWay(t *testing.T) {
	// 2->3 is missing.
	available := []Migration{bumpVersion(1, 2), bumpVersion(3, 4)}

	plan, err := PlanMigration(1, 4, available)
	if !errors.Is(err, ErrMigrationUnavailable) {
		t.Fatalf("error = %v, want ErrMigrationUnavailable", err)
	}
	if len(plan.Steps) != 0 {
		t.Errorf("a failed plan returned %d steps; a caller might apply them", len(plan.Steps))
	}
}

// TestPlanMigrationRefusesDowngrades pins that a newer save is never quietly
// flattened into an older shape.
func TestPlanMigrationRefusesDowngrades(t *testing.T) {
	// A migration table that would allow 2->1 exists, and must still be
	// ignored: migration steps are defined forward only, and PlanMigration
	// refuses the direction before consulting them.
	backwards := []Migration{{From: 2, To: 1, Apply: func(map[string]any) error { return nil }}}

	_, err := PlanMigration(2, 1, backwards)
	if !errors.Is(err, ErrMigrationUnavailable) {
		t.Fatalf("error = %v, want ErrMigrationUnavailable for a downgrade", err)
	}
	if !strings.Contains(err.Error(), "downgrading") {
		t.Errorf("the error does not mention downgrading: %v", err)
	}
}

// TestPlanMigrationRejectsVersionSkippingSteps pins that a step which claims to
// jump two versions is a definition error, not something the chain tolerates.
// Accepting it would let a step's declared To disagree with its actual effect.
func TestPlanMigrationRejectsVersionSkippingSteps(t *testing.T) {
	available := []Migration{bumpVersion(1, 3)}

	_, err := PlanMigration(1, 3, available)
	if err == nil {
		t.Fatal("a version-skipping migration was accepted")
	}
	if !strings.Contains(err.Error(), "skips a version") {
		t.Errorf("error = %v, want it to name the version skip", err)
	}
}

// TestPlanMigrationRejectsAnAmbiguousChain pins that two steps starting at the
// same version is refused rather than resolved by slice order. Which one runs
// would otherwise depend on the order a table happened to be written in.
func TestPlanMigrationRejectsAnAmbiguousChain(t *testing.T) {
	available := []Migration{bumpVersion(1, 2), bumpVersion(1, 2)}

	_, err := PlanMigration(1, 2, available)
	if err == nil {
		t.Fatal("an ambiguous migration chain was accepted")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error = %v, want it to name the ambiguity", err)
	}
}

func TestApplyMigrationAdvancesTheDeclaredVersion(t *testing.T) {
	available := []Migration{bumpVersion(1, 2), bumpVersion(2, 3)}
	plan, err := PlanMigration(1, 3, available)
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}

	doc := map[string]any{"envelope_version": 1, "payload": "original"}
	if err := ApplyMigration(plan, doc); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if got, _ := doc["envelope_version"].(int); got != 3 {
		t.Errorf("declared version = %v, want 3", doc["envelope_version"])
	}
	if doc["payload"] != "original" {
		t.Errorf("the migration lost unrelated data: payload = %v", doc["payload"])
	}
}

// TestApplyMigrationLeavesNothingBehindWhenAStepRefuses is the "refuse and keep
// the original" requirement in its mechanical form. A partially migrated
// document must not be produced, because a caller holding it would have no way
// to tell how far the migration got.
func TestApplyMigrationLeavesNothingBehindWhenAStepRefuses(t *testing.T) {
	available := []Migration{bumpVersion(1, 2), refusingMigration(2, 3)}
	plan, err := PlanMigration(1, 3, available)
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}

	doc := map[string]any{"envelope_version": 1}
	err = ApplyMigration(plan, doc)
	if !errors.Is(err, ErrMigrationRefused) {
		t.Fatalf("error = %v, want ErrMigrationRefused", err)
	}
	// The refusal names the version that refused, so the player can be told
	// which step could not preserve their save.
	if !strings.Contains(err.Error(), "version 2") {
		t.Errorf("the error does not name the refusing step: %v", err)
	}
}

// TestApplyMigrationRefusesAStepWithNoImplementation pins that a declared but
// unimplemented migration is an error rather than a silent version bump. A
// silent bump would produce a document claiming a shape it does not have.
func TestApplyMigrationRefusesAStepWithNoImplementation(t *testing.T) {
	plan := MigrationPlan{From: 1, Steps: []Migration{{From: 1, To: 2}}}

	err := ApplyMigration(plan, map[string]any{"envelope_version": 1})
	if !errors.Is(err, ErrMigrationRefused) {
		t.Fatalf("error = %v, want ErrMigrationRefused", err)
	}
}

func TestApplyMigrationRefusesANilDocument(t *testing.T) {
	err := ApplyMigration(MigrationPlan{}, nil)
	if !errors.Is(err, ErrMigrationRefused) {
		t.Fatalf("error = %v, want ErrMigrationRefused", err)
	}
}

// TestMigrationErrorsAreDistinguishable is the classification guarantee the
// player-facing message depends on. "Run the older build" and "your file is
// damaged" must never be the same error.
func TestMigrationErrorsAreDistinguishable(t *testing.T) {
	_, unavailable := PlanMigration(1, 2, nil)
	if errors.Is(unavailable, ErrSaveCorrupt) {
		t.Error("an unavailable migration was reported as corruption")
	}
	if errors.Is(unavailable, ErrMigrationRefused) {
		t.Error("an unavailable migration was reported as a refusal")
	}

	refused := ApplyMigration(
		MigrationPlan{From: 1, Steps: []Migration{refusingMigration(1, 2)}},
		map[string]any{"envelope_version": 1})
	if errors.Is(refused, ErrSaveCorrupt) {
		t.Error("a refused migration was reported as corruption")
	}
	if errors.Is(refused, ErrMigrationUnavailable) {
		t.Error("a refused migration was reported as unavailable; the two need different advice")
	}
}

// TestCopySaveUnmigratedRefusesAMissingCause pins that the helper cannot be
// used to quietly stash a file away with no explanation.
func TestCopySaveUnmigratedRefusesAMissingCause(t *testing.T) {
	_, err := CopySaveUnmigrated("nowhere.json", t.TempDir(), "s", nil)
	if !errors.Is(err, ErrMigrationRefused) {
		t.Fatalf("error = %v, want ErrMigrationRefused", err)
	}
}

func TestCopySaveUnmigratedKeepsTheOriginal(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, "hero")

	if err := store.Commit(newFakeEnvelope("hero", 1, "old build wrote this")); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	dest, err := CopySaveUnmigrated(store.SavePath(), dir, "20260101T000000Z",
		ErrMigrationUnavailable)
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}

	// The original must still be there: this is a copy, not a move.
	if _, ok, _ := readFileIfExists(store.SavePath()); !ok {
		t.Fatal("the original save was moved instead of copied; the older build could no longer read it")
	}
	got, ok, _ := readFileIfExists(dest)
	if !ok {
		t.Fatalf("no copy was written at %s", dest)
	}
	if !strings.Contains(string(got), "old build wrote this") {
		t.Errorf("the copy does not hold the original content: %q", got)
	}
}
