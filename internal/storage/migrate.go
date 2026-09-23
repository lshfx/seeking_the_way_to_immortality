package storage

import (
	"errors"
	"fmt"
)

// Migration is an explicit, version-to-version transformation of a save
// document.
//
// The design document is unusually specific about this, and the specificity is
// the whole point: when an older save's pending event or combat references a
// content id that no longer exists, the migration must be an *explicit mapping*.
// If a faithful mapping is impossible the save is refused and kept, with the
// player told to continue on the old build. Silently clearing combat or
// re-rolling a pending choice is forbidden, because it changes the outcome the
// player already paid for.
//
// So a migration is not a function that "makes a save loadable". It is a
// function that either produces a byte-faithful successor or refuses.
type Migration struct {
	// From is the envelope version this migration accepts.
	From int
	// To is the version it produces. To must be exactly From+1: chaining is
	// done by applying steps in order, so a migration that skips a version
	// cannot be composed with the step it skipped.
	To int
	// Apply transforms the document. Returning an error refuses the migration,
	// which the caller turns into "keep the original and tell the user to use
	// the older build" rather than a partial upgrade.
	Apply func(doc map[string]any) error
}

// ErrMigrationUnavailable reports that a save is older than this build can
// read and no faithful migration exists for it.
//
// It is deliberately distinct from ErrSaveCorrupt. The file is not damaged; it
// is from a different build. The player's action is "run the older version" or
// "install the update that provides the migration", not "restore a backup".
var ErrMigrationUnavailable = errors.New(
	"the save is from an older version and no faithful migration is available")

// ErrMigrationRefused reports that a migration exists but declined to run,
// because it could not preserve the save's meaning exactly.
//
// Keeping this separate from "unavailable" matters for the message a player
// reads: "this build cannot open that save" and "this build could open that save
// but would have to throw part of it away" are different situations, and only
// the second one is this project's decision rather than a version gap.
var ErrMigrationRefused = errors.New(
	"refusing to migrate: the save cannot be converted without losing state")

// MigrationPlan is the ordered chain of steps needed to read a document.
type MigrationPlan struct {
	// From is the version the document declares.
	From int
	// Steps are the migrations to apply, in order. Empty for a document that
	// is already at the current version.
	Steps []Migration
}

// RequiresMigration reports whether the plan actually has work to do.
func (p MigrationPlan) RequiresMigration() bool { return len(p.Steps) > 0 }

// PlanMigration finds the chain of migrations from a document's version to the
// target version.
//
// A missing step anywhere in the chain fails the whole plan. Partial
// application is the failure mode this function exists to prevent: applying the
// first two of four steps and then stopping would leave a document that claims
// a version it does not actually have, which is worse than refusing.
func PlanMigration(from, to int, available []Migration) (MigrationPlan, error) {
	plan := MigrationPlan{From: from}
	if from == to {
		return plan, nil
	}
	if from > to {
		// Downgrading is never automatic. A newer save may hold state this
		// build has no representation for, and discarding it silently is
		// exactly what the design forbids.
		return MigrationPlan{From: from}, fmt.Errorf("%w: the save declares version %d and this build writes %d; "+
			"downgrading is not performed", ErrMigrationUnavailable, from, to)
	}

	// Index by source version. A duplicate source version would make the chain
	// ambiguous, so it is rejected rather than resolved by order.
	byFrom := make(map[int]Migration, len(available))
	for _, m := range available {
		if m.To != m.From+1 {
			return MigrationPlan{From: from}, fmt.Errorf("the migration %d->%d skips a version; "+
				"migrations must step one version at a time", m.From, m.To)
		}
		if _, dup := byFrom[m.From]; dup {
			return MigrationPlan{From: from}, fmt.Errorf("two migrations both start at version %d; "+
				"the chain is ambiguous", m.From)
		}
		byFrom[m.From] = m
	}

	for v := from; v < to; v++ {
		step, ok := byFrom[v]
		if !ok {
			// Return an EMPTY plan, not the steps collected so far. A caller
			// that logs the error and then applies whatever came back would
			// otherwise half-migrate the document; this test caught exactly
			// that, because the first version of this loop returned the
			// partial chain.
			return MigrationPlan{From: from}, fmt.Errorf("%w: no migration from version %d to %d "+
				"(this build understands versions up to %d)",
				ErrMigrationUnavailable, v, v+1, to)
		}
		plan.Steps = append(plan.Steps, step)
	}
	return plan, nil
}

// ApplyMigration runs a plan over a decoded document.
//
// It works on the generic map form rather than on a typed envelope because the
// shape changes *between* versions: a typed target can only represent one
// version's fields, and decoding an older document into a newer struct is how a
// migration silently loses data before it has had a chance to map it.
//
// A step that fails aborts the whole plan. The caller still holds the original
// bytes, so nothing is lost; the document is never partially migrated in place.
func ApplyMigration(plan MigrationPlan, doc map[string]any) error {
	if doc == nil {
		return fmt.Errorf("%w: there is no document to migrate", ErrMigrationRefused)
	}
	for _, step := range plan.Steps {
		if step.Apply == nil {
			return fmt.Errorf("%w: the migration from version %d has no implementation",
				ErrMigrationRefused, step.From)
		}
		if err := step.Apply(doc); err != nil {
			return fmt.Errorf("%w: the migration from version %d refused: %v",
				ErrMigrationRefused, step.From, err)
		}
		// Advance the declared version as each step succeeds. The final value
		// is asserted by the caller against the target, so a step that forgot
		// to update it is caught rather than trusted.
		doc["envelope_version"] = step.To
	}
	return nil
}

// CopySaveUnmigrated copies a save this build cannot faithfully convert into the
// preserved directory, so the player can still run the older build on it.
//
// The design's instruction for an unmigratable save is "refuse and keep the
// original, telling the player to continue on the old version". Keeping the
// original means more than not deleting it: the game must also not *overwrite*
// it, so this is called as part of the refusal path, before any write can
// happen, and the copy is what the player is pointed at.
func CopySaveUnmigrated(src, preservedDir, suffix string, cause error) (string, error) {
	if cause == nil {
		return "", fmt.Errorf("%w: no cause was given, so the copy would be unexplained",
			ErrMigrationRefused)
	}
	dest, err := CopySaveToPreserved(src, preservedDir, suffix)
	if err != nil {
		return "", fmt.Errorf("%w: %s could not be preserved: %v", ErrMigrationRefused, src, err)
	}
	return dest, nil
}
