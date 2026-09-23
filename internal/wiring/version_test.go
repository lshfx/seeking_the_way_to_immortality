// Package wiring holds the tests that check two independently-declared pieces of
// configuration still agree.
//
// The storage layer declares CurrentEnvelopeVersion rather than importing
// engine.EnvelopeVersion, because ADR-002 keeps internal/engine free of any
// dependency on I/O and internal/storage is a sibling package that must not
// create a cycle. The cost of that decision is one duplicated number, and a
// duplicated number drifts. The comment in snapshot.go promises a test catches
// the drift; this is that test.
//
// It lives in its own package because it is the only place in the tree that is
// allowed to import both sides at once. Putting it inside internal/storage would
// mean storage importing engine, which is the cycle the design forbids.
package wiring

import (
	"testing"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/storage"
)

// TestEnvelopeVersionsAgree is the drift guard.
//
// If these two ever disagree, the failure is silent and severe in both
// directions:
//
//   - storage higher than engine: storage will accept and write a document the
//     engine cannot interpret, and the game loads it as an empty state.
//   - storage lower than engine: storage refuses its own engine's saves as
//     "from a newer version", and every player is told to update a build that
//     is already current.
func TestEnvelopeVersionsAgree(t *testing.T) {
	if storage.CurrentEnvelopeVersion != engine.EnvelopeVersion {
		t.Fatalf("the envelope versions disagree: storage writes and accepts up to %d, "+
			"while the engine declares %d; a save written by one is not readable by the other",
			storage.CurrentEnvelopeVersion, engine.EnvelopeVersion)
	}
}

// TestSchemaVersionIsDeclared documents the other half of the contract.
//
// SchemaVersion is carried inside the envelope and is *not* duplicated as a
// storage constant, so there is nothing to drift. The test records that fact
// rather than asserting on a number, because the interesting property is that
// the storage layer has no opinion about the schema -- it treats the document as
// opaque JSON and delegates every field decision to the codec.
func TestSchemaVersionIsDeclared(t *testing.T) {
	if engine.SchemaVersion <= 0 {
		t.Fatalf("the engine declares schema version %d; a non-positive version "+
			"cannot be distinguished from a missing one", engine.SchemaVersion)
	}
	if engine.RulesVersion <= 0 || engine.ContentVersion <= 0 {
		t.Fatalf("rules version %d and content version %d must both be positive: a save is "+
			"only meaningful together with the rules and content that produced it",
			engine.RulesVersion, engine.ContentVersion)
	}
}
