package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEnvelope is a stand-in for the engine's save document.
//
// It deliberately has the same shape that matters to storage: a version, a
// payload, and a digest over the payload. The storage layer is generic over the
// envelope precisely so its tests do not need the engine, and using a stand-in
// here means a failure points at storage rather than at engine internals.
type fakeEnvelope struct {
	EnvelopeVersion int    `json:"envelope_version"`
	GameID          string `json:"game_id"`
	Revision        uint64 `json:"revision"`
	Payload         string `json:"payload"`
	Digest          string `json:"digest"`
}

func (f *fakeEnvelope) computeDigest() string {
	// A digest over the fields that matter, with the digest field excluded, so
	// it changes exactly when the content does.
	return "d:" + f.Payload + ":" + f.GameID + ":" + itoa(int(f.Revision))
}

// Seal implements the write-side codec contract.
func (f *fakeEnvelope) Seal() {
	f.Digest = f.computeDigest()
}

// Verify implements the read-side codec contract.
func (f *fakeEnvelope) Verify() bool {
	return f.Digest == f.computeDigest()
}

// EnvelopeVersionNumber implements the read-side codec contract.
func (f *fakeEnvelope) EnvelopeVersionNumber() int { return f.EnvelopeVersion }

func fakeCodec() Codec[fakeEnvelope] {
	return Codec[fakeEnvelope]{
		New: func() *fakeEnvelope { return &fakeEnvelope{} },
		Seal: func(e *fakeEnvelope) {
			e.Seal()
		},
		Verify: func(e *fakeEnvelope) bool { return e.Verify() },
		Version: func(e *fakeEnvelope) int {
			return e.EnvelopeVersionNumber()
		},
		GameID:   func(e *fakeEnvelope) string { return e.GameID },
		Revision: func(e *fakeEnvelope) uint64 { return e.Revision },
	}
}

func newFakeEnvelope(gameID string, revision uint64, payload string) *fakeEnvelope {
	e := &fakeEnvelope{
		EnvelopeVersion: CurrentEnvelopeVersion,
		GameID:          gameID,
		Revision:        revision,
		Payload:         payload,
	}
	e.Seal()
	return e
}

// newTestStore builds a store over a temporary data root.
func newTestStore(t *testing.T, gameID string) *SnapshotStore[fakeEnvelope] {
	t.Helper()
	layout := LayoutFor(t.TempDir())
	store, err := NewSnapshotStore(layout, gameID, fakeCodec())
	if err != nil {
		t.Fatalf("cannot create the store: %v", err)
	}
	return store
}

// TestSnapshotStoreRoundTrips is the baseline: what was committed is what loads.
//
// Without this, every other test here could pass on a store that silently saved
// nothing.
func TestSnapshotStoreRoundTrips(t *testing.T) {
	store := newTestStore(t, "hero")

	env := newFakeEnvelope("hero", 1, "hello")
	if err := store.Commit(env); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	result, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if result.Envelope == nil {
		t.Fatal("load returned no envelope")
	}
	if result.Envelope.Payload != "hello" {
		t.Fatalf("payload = %q, want %q", result.Envelope.Payload, "hello")
	}
	if result.Source != "current" {
		t.Fatalf("source = %q, want %q", result.Source, "current")
	}
	if result.Recovered {
		t.Error("a clean load reported a recovery")
	}
}

// TestSnapshotStoreLoadAbsentIsNotAnError matters because the first launch has no
// save, and treating that as a failure would force every caller to special-case
// it -- and would make a first-run screen show an error.
func TestSnapshotStoreLoadAbsentIsNotAnError(t *testing.T) {
	store := newTestStore(t, "hero")

	_, err := store.Load()
	if !errors.Is(err, ErrSaveNotFound) {
		t.Fatalf("error = %v, want ErrSaveNotFound", err)
	}
}

// TestSnapshotStoreRotatesThePreviousSave is the mechanism the recovery path
// depends on. Without rotation there is nothing to fall back to.
func TestSnapshotStoreRotatesThePreviousSave(t *testing.T) {
	store := newTestStore(t, "hero")

	if err := store.Commit(newFakeEnvelope("hero", 1, "first")); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}
	if err := store.Commit(newFakeEnvelope("hero", 2, "second")); err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	// The current save holds the new content.
	cur, err := store.decodeFile(store.SavePath())
	if err != nil {
		t.Fatalf("cannot read the current save: %v", err)
	}
	if cur.Payload != "second" {
		t.Fatalf("current payload = %q, want %q", cur.Payload, "second")
	}

	// And the rotation path holds the old content, intact.
	prev, err := store.decodeFile(store.PrevSavePath())
	if err != nil {
		t.Fatalf("the previous save was not preserved or not readable: %v", err)
	}
	if prev.Payload != "first" {
		t.Fatalf("previous payload = %q, want %q", prev.Payload, "first")
	}
}

// TestCommitRotatesBeforeItOverwrites is the ordering guard for Commit.
//
// Commit performs two writes: the outgoing save is copied to the rotation path,
// then the new save replaces the current one. If those are swapped -- or if the
// rotation is skipped -- a failure between them leaves the character with no
// complete save at all, which is the single worst outcome this package can
// produce.
//
// Ordering cannot be observed from the final state of a *successful* commit:
// both writes succeed and both files are correct, whichever order they ran in.
// The observation has to be at the turning point. So the second atomic write is
// made to fail at its last stage, and the rotation file is read afterwards:
//
//   - rotation ran first  -> save.json.prev holds revision 1, complete
//   - overwrite ran first -> save.json.prev is absent or stale, and the only
//     copy of revision 1 has already been destroyed
//
// The failure stage is stepRename, which fires *before* the rename. That
// matters: it means the current save genuinely survives, so the test is not
// conflating "the rotation happened" with "the overwrite never got anywhere".
//
// This test exists because the counter-proof for this property reported NOT
// CAUGHT. The original mutation only removed the *error handling* around the
// rotation, so the rotation still happened and every functional test still
// passed -- correctly, since the property under test was untouched. A mutation
// must break the property it claims to break; when it does not, the mutation is
// the defect, not the test.
func TestCommitRotatesBeforeItOverwrites(t *testing.T) {
	store := newTestStore(t, "hero")

	if err := store.Commit(newFakeEnvelope("hero", 1, "first")); err != nil {
		t.Fatalf("seed commit failed: %v", err)
	}

	// Arm the hook so only the second atomic write of the commit fails. The
	// rotation uses SyncFileOnly and the commit uses the store's own strategy;
	// counting calls is what distinguishes them without depending on that.
	writes := 0
	restore := SetWriteHook(func(stage writeStep) error {
		if stage != stepRename {
			return nil
		}
		writes++
		if writes == 2 {
			return errors.New("induced failure just before the overwrite is published")
		}
		return nil
	})
	defer restore()

	err := store.Commit(newFakeEnvelope("hero", 2, "second"))
	if !errors.Is(err, err) || err == nil {
		t.Fatalf("the commit was expected to fail; err = %v", err)
	}
	if writes < 2 {
		t.Fatalf("only %d atomic writes happened; the test did not reach the overwrite "+
			"and would prove nothing about ordering", writes)
	}

	// The rotation must already be complete *and* the current save must still
	// hold revision 1, because the induced failure fired before its rename.
	prev, err := store.decodeFile(store.PrevSavePath())
	if err != nil {
		t.Fatalf("the outgoing save was not rotated before the overwrite was attempted: %v", err)
	}
	if prev.Payload != "first" {
		t.Fatalf("rotated payload = %q, want %q: the rotation ran after the overwrite, "+
			"or the overwrite destroyed revision 1 before it was copied aside",
			prev.Payload, "first")
	}

	cur, err := store.decodeFile(store.SavePath())
	if err != nil {
		t.Fatalf("the current save did not survive the induced failure: %v", err)
	}
	if cur.Payload != "first" {
		t.Fatalf("current payload = %q, want %q: the failed commit damaged the live save",
			cur.Payload, "first")
	}
}

// TestSnapshotStoreRecoversFromAPreviousSaveWhenCurrentIsCorrupt is the headline
// recovery behaviour: a damaged current save must not cost the player everything.
func TestSnapshotStoreRecoversFromAPreviousSaveWhenCurrentIsCorrupt(t *testing.T) {
	store := newTestStore(t, "hero")

	if err := store.Commit(newFakeEnvelope("hero", 1, "good")); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}
	if err := store.Commit(newFakeEnvelope("hero", 2, "newer")); err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	// Truncate the current save, which is what a crashed write or a half-flushed
	// device looks like.
	if err := os.WriteFile(store.SavePath(), []byte(`{"envelope_version":1,"game_id":"hero","pay`), 0o600); err != nil {
		t.Fatalf("cannot damage the current save: %v", err)
	}

	result, err := store.Load()
	if err != nil {
		t.Fatalf("load did not fall back to the previous save: %v", err)
	}
	if !result.Recovered {
		t.Error("the recovery was not reported; the player would not know progress was rolled back")
	}
	if result.Source != "previous" {
		t.Fatalf("source = %q, want %q", result.Source, "previous")
	}
	if result.Envelope == nil || result.Envelope.Payload != "good" {
		t.Fatalf("recovered payload = %+v, want %q", result.Envelope, "good")
	}
	if result.CurrentProblem == nil {
		t.Error("no cause was reported for the rejected save")
	}
}

// TestSnapshotStorePreservesARejectedSave is the "refuse and preserve" rule.
//
// A save the program cannot read is still the player's data. Deleting it, or
// letting the next write overwrite it, turns a recoverable situation into a
// permanent one.
func TestSnapshotStorePreservesARejectedSave(t *testing.T) {
	store := newTestStore(t, "hero")

	if err := store.Commit(newFakeEnvelope("hero", 1, "good")); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}
	if err := store.Commit(newFakeEnvelope("hero", 2, "newer")); err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	const damaged = `{"envelope_version":1,"this is not a valid document`
	if err := os.WriteFile(store.SavePath(), []byte(damaged), 0o600); err != nil {
		t.Fatalf("cannot damage the current save: %v", err)
	}

	result, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if result.PreservedPath == "" {
		t.Fatal("the rejected save was not preserved; the player's file would be lost on the next write")
	}

	// The preserved copy must contain exactly the damaged bytes, so a later
	// version or a human can examine what the player actually had.
	got, err := os.ReadFile(result.PreservedPath)
	if err != nil {
		t.Fatalf("cannot read the preserved copy: %v", err)
	}
	if string(got) != damaged {
		t.Fatalf("preserved content = %q, want %q", got, damaged)
	}

	// And it must live in the preserved directory, not beside the save.
	if filepath.Dir(result.PreservedPath) != store.layout.PreservedDir() {
		t.Fatalf("preserved copy is at %s, want it under %s", result.PreservedPath, store.layout.PreservedDir())
	}
}

// TestSnapshotStoreReportsWhenBothCopiesAreUnusable checks the honest-failure
// path. Both copies being bad is a different situation from one being bad, and
// the error must say so.
func TestSnapshotStoreReportsWhenBothCopiesAreUnusable(t *testing.T) {
	store := newTestStore(t, "hero")

	if err := store.Commit(newFakeEnvelope("hero", 1, "good")); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}
	if err := store.Commit(newFakeEnvelope("hero", 2, "newer")); err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	for _, path := range []string{store.SavePath(), store.PrevSavePath()} {
		if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
			t.Fatalf("cannot damage %s: %v", path, err)
		}
	}

	_, err := store.Load()
	if err == nil {
		t.Fatal("load reported success with both copies damaged")
	}
	if !errors.Is(err, ErrSaveCorrupt) {
		t.Fatalf("error = %v, want ErrSaveCorrupt", err)
	}
	// The message should mention both, so the user knows this is not a single
	// unlucky file.
	if !strings.Contains(err.Error(), "previous") {
		t.Errorf("error %q does not mention the previous copy", err.Error())
	}
}

// TestSnapshotStoreRejectsATamperedSave pins that the digest is actually
// checked. Without this assertion the digest could be computed and then ignored.
func TestSnapshotStoreRejectsATamperedSave(t *testing.T) {
	store := newTestStore(t, "hero")

	env := newFakeEnvelope("hero", 1, "original")
	if err := store.Commit(env); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// Edit the payload without fixing the digest, which is exactly what a
	// hand-edit looks like.
	data, err := os.ReadFile(store.SavePath())
	if err != nil {
		t.Fatalf("cannot read the save: %v", err)
	}
	tampered := strings.Replace(string(data), `"payload":"original"`, `"payload":"cheated!"`, 1)
	if tampered == string(data) {
		t.Fatal("the test failed to modify the payload; it would prove nothing")
	}
	if err := os.WriteFile(store.SavePath(), []byte(tampered), 0o600); err != nil {
		t.Fatalf("cannot write the tampered save: %v", err)
	}

	_, err = store.Load()
	if !errors.Is(err, ErrSaveCorrupt) {
		t.Fatalf("error = %v, want ErrSaveCorrupt for a tampered payload", err)
	}
}

// TestSnapshotStoreRefusesAFutureEnvelopeVersion is the forward-compatibility
// guard: never try to interpret a document from a newer build.
func TestSnapshotStoreRefusesAFutureEnvelopeVersion(t *testing.T) {
	store := newTestStore(t, "hero")

	// Write a well-formed document that declares a future version. It is sealed
	// correctly, so only the version check can reject it -- which is the point.
	future := newFakeEnvelope("hero", 1, "from the future")
	future.EnvelopeVersion = CurrentEnvelopeVersion + 1
	future.Seal()
	data, err := json.Marshal(future)
	if err != nil {
		t.Fatalf("cannot encode the future envelope: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.SavePath()), 0o700); err != nil {
		t.Fatalf("cannot create the character directory: %v", err)
	}
	if err := os.WriteFile(store.SavePath(), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("cannot write the future save: %v", err)
	}

	_, err = store.Load()
	if !errors.Is(err, ErrSaveUnsupported) {
		t.Fatalf("error = %v, want ErrSaveUnsupported", err)
	}
	// It must be distinguishable from corruption, because the user action is
	// different: update the game, not restore a backup.
	if errors.Is(err, ErrSaveCorrupt) {
		t.Error("a future version was reported as corruption")
	}
}

// TestSnapshotStoreRefusesAnOlderRevisionOverANewerOne is the stale-window guard.
//
// Two windows on one save is exactly what the lock prevents, but a lock can be
// lost (a takeover judged wrongly, a user deleting the file). Writing an older
// revision over a newer one is how the second window's stale state destroys the
// first window's progress, so the store refuses it independently of the lock.
func TestSnapshotStoreRefusesAnOlderRevisionOverANewerOne(t *testing.T) {
	store := newTestStore(t, "hero")

	if err := store.Commit(newFakeEnvelope("hero", 10, "newest")); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	err := store.Commit(newFakeEnvelope("hero", 9, "older"))
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("error = %v, want ErrStaleRevision", err)
	}

	// And the newer content must still be on disk, untouched.
	cur, err := store.decodeFile(store.SavePath())
	if err != nil {
		t.Fatalf("cannot read the save after a refused write: %v", err)
	}
	if cur.Payload != "newest" {
		t.Fatalf("payload = %q, want %q; a refused write still modified the save", cur.Payload, "newest")
	}
}

// TestSnapshotStoreAllowsRewritingTheSameRevision matters because a save can be
// legitimately re-written without advancing the revision, for example when
// recording a log tail.
func TestSnapshotStoreAllowsRewritingTheSameRevision(t *testing.T) {
	store := newTestStore(t, "hero")

	if err := store.Commit(newFakeEnvelope("hero", 5, "first")); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}
	if err := store.Commit(newFakeEnvelope("hero", 5, "second")); err != nil {
		t.Fatalf("rewriting the same revision was refused: %v", err)
	}

	cur, err := store.decodeFile(store.SavePath())
	if err != nil {
		t.Fatalf("cannot read the save: %v", err)
	}
	if cur.Payload != "second" {
		t.Fatalf("payload = %q, want %q", cur.Payload, "second")
	}
}

// TestSnapshotStoreRejectsACharacterMismatch guards against a save being loaded
// into the wrong character slot, which a user can cause by renaming a file.
func TestSnapshotStoreRejectsACharacterMismatch(t *testing.T) {
	store := newTestStore(t, "hero")

	// Write a valid document belonging to somebody else at hero's path.
	other := newFakeEnvelope("rival", 1, "somebody else")
	data, err := json.Marshal(other)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.SavePath()), 0o700); err != nil {
		t.Fatalf("cannot create the directory: %v", err)
	}
	if err := os.WriteFile(store.SavePath(), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("cannot write: %v", err)
	}

	// The decode itself must fail on the character check, so this exercises the
	// store's own validation rather than the seal.
	_, err = store.decodeFile(store.SavePath())
	if !errors.Is(err, ErrSaveCorrupt) {
		t.Fatalf("error = %v, want ErrSaveCorrupt for a mismatched character", err)
	}
	if !strings.Contains(err.Error(), "rival") {
		t.Errorf("error %q does not name the character found in the file", err.Error())
	}
}

// TestSnapshotStoreCheckpointRetentionPrunesOldest is the retention rule: keep
// the newest N, drop the rest.
func TestSnapshotStoreCheckpointRetentionPrunesOldest(t *testing.T) {
	store := newTestStore(t, "hero")

	const total = CheckpointRetention + 3
	for rev := uint64(1); rev <= total; rev++ {
		if _, err := store.Checkpoint(newFakeEnvelope("hero", rev, "checkpoint"), rev); err != nil {
			t.Fatalf("checkpoint at revision %d failed: %v", rev, err)
		}
	}

	revs, err := store.ListCheckpoints()
	if err != nil {
		t.Fatalf("cannot list checkpoints: %v", err)
	}
	if len(revs) != CheckpointRetention {
		t.Fatalf("retained %d checkpoints, want %d", len(revs), CheckpointRetention)
	}

	// The retained ones must be the newest, and they must be in descending
	// order.
	for i, rev := range revs {
		want := uint64(total - i)
		if rev != want {
			t.Fatalf("checkpoint %d = revision %d, want %d; retention kept the wrong ones", i, rev, want)
		}
	}

	// The oldest must actually be gone from disk, not merely absent from the
	// listing.
	if _, err := os.Stat(store.layout.CheckpointPath("hero", 1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the oldest checkpoint still exists on disk: %v", err)
	}
}

// TestSnapshotStoreRetentionSurvivesLexicographicSorting is the reason the
// revision name is zero-padded.
//
// Without padding, "10" sorts before "9" and retention keeps the wrong set --
// silently deleting the newest checkpoint and keeping the oldest.
func TestSnapshotStoreRetentionSurvivesLexicographicSorting(t *testing.T) {
	store := newTestStore(t, "hero")

	// Revisions that differ in digit count are where an unpadded scheme breaks.
	revs := []uint64{2, 9, 10, 11, CheckpointRetention + 10}
	for _, rev := range revs {
		if _, err := store.Checkpoint(newFakeEnvelope("hero", rev, "checkpoint"), rev); err != nil {
			t.Fatalf("checkpoint at revision %d failed: %v", rev, err)
		}
	}

	got, err := store.ListCheckpoints()
	if err != nil {
		t.Fatalf("cannot list: %v", err)
	}

	// Whatever was retained must be the numerically largest revisions.
	wantCount := CheckpointRetention
	if len(revs) < wantCount {
		wantCount = len(revs)
	}
	if len(got) != wantCount {
		t.Fatalf("retained %d, want %d", len(got), wantCount)
	}
	highest := uint64(CheckpointRetention + 10)
	if got[0] != highest {
		t.Fatalf("newest retained checkpoint = %d, want %d; an unpadded name would sort wrongly",
			got[0], highest)
	}
}

// TestSnapshotStorePruningIgnoresUnrecognisedFiles is the safety rule for
// cleanup.
//
// A prune that deletes whatever it does not understand is how a user's file
// disappears because it was named unexpectedly. Unrecognised names are left.
func TestSnapshotStorePruningIgnoresUnrecognisedFiles(t *testing.T) {
	store := newTestStore(t, "hero")

	dir := store.layout.CheckpointDir("hero")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("cannot create the checkpoint directory: %v", err)
	}

	// A file the naming scheme would never produce, plus a directory.
	const stranger = "my-backup.json"
	if err := os.WriteFile(filepath.Join(dir, stranger), []byte("do not delete me"), 0o600); err != nil {
		t.Fatalf("cannot write the stranger file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatalf("cannot create the subdirectory: %v", err)
	}

	// Overflow the retention limit so pruning actually runs.
	for rev := uint64(1); rev <= CheckpointRetention+2; rev++ {
		if _, err := store.Checkpoint(newFakeEnvelope("hero", rev, "checkpoint"), rev); err != nil {
			t.Fatalf("checkpoint %d failed: %v", rev, err)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, stranger)); err != nil {
		t.Fatalf("pruning deleted an unrecognised file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subdir")); err != nil {
		t.Fatalf("pruning deleted a directory: %v", err)
	}
}

// TestSnapshotStoreKeepAllWhenUnderTheLimit checks the boundary: pruning must not
// run at all when nothing needs removing.
func TestSnapshotStoreKeepAllWhenUnderTheLimit(t *testing.T) {
	store := newTestStore(t, "hero")

	for rev := uint64(1); rev <= CheckpointRetention; rev++ {
		if _, err := store.Checkpoint(newFakeEnvelope("hero", rev, "checkpoint"), rev); err != nil {
			t.Fatalf("checkpoint %d failed: %v", rev, err)
		}
	}

	got, err := store.ListCheckpoints()
	if err != nil {
		t.Fatalf("cannot list: %v", err)
	}
	if len(got) != CheckpointRetention {
		t.Fatalf("retained %d, want %d (nothing should have been pruned)", len(got), CheckpointRetention)
	}
}

// TestSnapshotStoreIsPerCharacter checks that two characters do not share files.
func TestSnapshotStoreIsPerCharacter(t *testing.T) {
	layout := LayoutFor(t.TempDir())

	hero, err := NewSnapshotStore(layout, "hero", fakeCodec())
	if err != nil {
		t.Fatalf("cannot create the hero store: %v", err)
	}
	rival, err := NewSnapshotStore(layout, "rival", fakeCodec())
	if err != nil {
		t.Fatalf("cannot create the rival store: %v", err)
	}

	if err := hero.Commit(newFakeEnvelope("hero", 1, "hero data")); err != nil {
		t.Fatalf("hero commit failed: %v", err)
	}
	if err := rival.Commit(newFakeEnvelope("rival", 1, "rival data")); err != nil {
		t.Fatalf("rival commit failed: %v", err)
	}

	hres, err := hero.Load()
	if err != nil {
		t.Fatalf("hero load failed: %v", err)
	}
	if hres.Envelope.Payload != "hero data" {
		t.Fatalf("hero loaded %q, want %q", hres.Envelope.Payload, "hero data")
	}

	rres, err := rival.Load()
	if err != nil {
		t.Fatalf("rival load failed: %v", err)
	}
	if rres.Envelope.Payload != "rival data" {
		t.Fatalf("rival loaded %q, want %q", rres.Envelope.Payload, "rival data")
	}
}

// TestSnapshotStoreRejectsAnIncompleteCodec catches a wiring mistake at
// construction rather than at the first save, when the player is watching.
func TestSnapshotStoreRejectsAnIncompleteCodec(t *testing.T) {
	layout := LayoutFor(t.TempDir())

	full := fakeCodec()
	cases := map[string]Codec[fakeEnvelope]{
		"no New":     {Seal: full.Seal, Verify: full.Verify, Version: full.Version},
		"no Seal":    {New: full.New, Verify: full.Verify, Version: full.Version},
		"no Verify":  {New: full.New, Seal: full.Seal, Version: full.Version},
		"no Version": {New: full.New, Seal: full.Seal, Verify: full.Verify},
	}

	for name, codec := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewSnapshotStore(layout, "hero", codec); err == nil {
				t.Fatal("an incomplete codec was accepted; the failure would surface at the first save")
			}
		})
	}
}

// TestParseRevisionNameIsStrict pins the parser's strictness, which is what makes
// pruning safe. A permissive parser would let a foreign filename be treated as a
// checkpoint and then deleted.
func TestParseRevisionNameIsStrict(t *testing.T) {
	valid := revisionName(42) + ".json"
	got, ok := parseRevisionName(valid)
	if !ok || got != 42 {
		t.Fatalf("parseRevisionName(%q) = (%d, %v), want (42, true)", valid, got, ok)
	}

	invalid := []string{
		"",
		".json",
		"42.json",                 // unpadded, a different scheme
		revisionName(42) + ".bak", // wrong extension
		revisionName(42),          // missing extension
		strings.Repeat("9", 20),   // no extension
		strings.Repeat("x", 20) + ".json",
		strings.Repeat("9", 21) + ".json",
		"0000000000000000000-json",
	}

	for _, name := range invalid {
		if _, ok := parseRevisionName(name); ok {
			t.Errorf("parseRevisionName(%q) accepted an unrecognised name", name)
		}
	}
}

// TestParseRevisionNameRoundTrips checks the two functions agree, including at
// the extremes where a fixed-width scheme is most likely to break.
func TestParseRevisionNameRoundTrips(t *testing.T) {
	cases := []uint64{0, 1, 9, 10, 99, 100, 1 << 32, ^uint64(0)}
	for _, rev := range cases {
		name := revisionName(rev) + ".json"
		got, ok := parseRevisionName(name)
		if !ok || got != rev {
			t.Errorf("round trip of %d produced (%d, %v) via %q", rev, got, ok, name)
		}
	}
}

// TestCopySaveToPreservedRefusesAMissingSource keeps the preserve step honest: it
// must not report success when there was nothing to copy.
func TestCopySaveToPreservedRefusesAMissingSource(t *testing.T) {
	dir := t.TempDir()
	_, err := CopySaveToPreserved(filepath.Join(dir, "absent.json"), filepath.Join(dir, "preserved"), "suffix")
	if !errors.Is(err, ErrSaveNotFound) {
		t.Fatalf("error = %v, want ErrSaveNotFound", err)
	}
}

// TestTimeStampSuffixIsSortableAndPinned pins the exact format, because these
// names appear in listings a user reads and sorts.
func TestTimeStampSuffixIsSortableAndPinned(t *testing.T) {
	original := nowStamp
	t.Cleanup(func() { nowStamp = original })

	// A time chosen so every field differs, making a wrong format visible.
	nowStamp = func() time.Time {
		return time.Date(2026, 3, 7, 9, 5, 4, 0, time.UTC)
	}

	got := timeStampSuffix()
	const want = "20260307T090504Z"
	if got != want {
		t.Fatalf("timeStampSuffix() = %q, want %q", got, want)
	}

	// Two timestamps a second apart must sort in time order lexicographically,
	// which is what makes a directory listing readable.
	nowStamp = func() time.Time {
		return time.Date(2026, 3, 7, 9, 5, 5, 0, time.UTC)
	}
	later := timeStampSuffix()
	if !(got < later) {
		t.Fatalf("%q does not sort before %q", got, later)
	}
}
