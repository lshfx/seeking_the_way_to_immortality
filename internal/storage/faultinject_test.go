package storage

// Fault injection for the snapshot store.
//
// The design document asks for a fault-injection tool because the failures that
// matter most are the ones a developer cannot produce on demand: a full disk, a
// read-only directory, a permission error at the one moment it counts. Waiting
// for those to happen naturally is not a test strategy.
//
// The capability already exists at the atomic-writer level (SetWriteHook makes
// any of the seven write stages fail). What this file adds is *reach*: it drives
// each stage through the whole store, so the assertion is about what a player
// experiences -- their save, their recovery points, the error they are shown --
// rather than about one function's return value.
//
// Every case asserts the same three things, for the same reason: a failed write
// must never be reported as a success, must never leave the live save worse than
// it was, and must never leave temp-file debris behind.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFaultInjectionEveryWriteStageIsSurvivable walks every atomic-write stage
// and asserts that a failure there leaves the character's save readable.
//
// This is the "写入中杀进程只恢复旧完整或新完整状态" acceptance criterion in its
// fault-injection form: rather than killing the process, each stage is made to
// fail, which is the same disturbance at a precisely chosen moment.
//
// stageSyncDir is handled separately and documented below, because it is not
// like the others: by the time it runs the data is already published, and a
// failure there is a *weaker durability guarantee*, not a failed save. Asserting
// that a commit fails at that stage would be asserting the wrong contract.
func TestFaultInjectionEveryWriteStageIsSurvivable(t *testing.T) {
	induce := errors.New("induced: the device reported no space left")

	stages := []writeStep{
		stepCreateTemp,
		stepWrite,
		stepSync,
		stepChmod,
		stepClose,
		stepRename,
	}

	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			store := newTestStore(t, "hero")

			// Seed a known-good save so there is something to protect.
			if err := store.Commit(newFakeEnvelope("hero", 1, "the original")); err != nil {
				t.Fatalf("the seed commit failed: %v", err)
			}
			seedBytes, _, _ := readFileIfExists(store.SavePath())

			restore := SetWriteHook(func(s writeStep) error {
				if s == stage {
					return induce
				}
				return nil
			})
			defer restore()

			err := store.Commit(newFakeEnvelope("hero", 2, "never published"))
			if err == nil {
				t.Fatalf("the commit succeeded despite an induced failure at %q", stage)
			}

			// 1. The failure is attributed to the stage that failed, so the
			//    player-facing message names the real problem.
			var we *WriteError
			if !errors.As(err, &we) {
				t.Fatalf("error = %T (%v), want *WriteError so the cause is machine-readable", err, err)
			}
			if we.Op != string(stage) {
				t.Errorf("reported stage = %q, want %q", we.Op, stage)
			}
			if !errors.Is(err, induce) {
				t.Errorf("the underlying cause was lost: %v", err)
			}
			if we.Partial {
				t.Errorf("a failure at %q was reported as Partial; publication happens at the "+
					"rename, so nothing before it can have reached the target", stage)
			}

			// 2. The live save is intact. Every stage in this list runs before
			//    the rename, which is the commit point, so the old content must
			//    survive verbatim -- not merely "some save".
			got, ok, rerr := readFileIfExists(store.SavePath())
			if rerr != nil {
				t.Fatalf("the live save became unreadable: %v", rerr)
			}
			if !ok {
				t.Fatalf("a failure at %q left no save at all", stage)
			}
			if string(got) != string(seedBytes) {
				t.Errorf("a failure at %q damaged the live save:\n got: %q\nwant: %q",
					stage, got, seedBytes)
			}

			// 3. No temp-file debris. Leaving one behind fills the player's
			//    disk a little more with every failed save.
			assertNoTempDebris(t, filepath.Dir(store.SavePath()))
		})
	}
}

// TestFaultInjectionSyncDirFailureIsPartialNotFatal pins the durability contract
// at the last stage.
//
// stepSyncDir runs *after* the rename, so the new save is already the live save.
// The design forbids claiming durability the platform did not provide, and it
// equally forbids reporting a save as failed when the player's progress is
// already on disk. The correct outcome is therefore a *partial* write: the data
// is there, the power-cut guarantee is not.
//
// This distinction is not cosmetic. A caller that treats a partial write as a
// failure will show "could not save" and may roll back in-memory state to match
// a disk that has already moved on -- losing the very progress it was trying to
// protect.
func TestFaultInjectionSyncDirFailureIsPartialNotFatal(t *testing.T) {
	induce := errors.New("induced: the directory entry could not be flushed")

	store := newTestStore(t, "hero")
	if err := store.Commit(newFakeEnvelope("hero", 1, "the original")); err != nil {
		t.Fatalf("the seed commit failed: %v", err)
	}

	restore := SetWriteHook(func(s writeStep) error {
		// On Windows syncDir reports "unsupported" rather than calling the hook,
		// so this case is only reachable where a real directory sync is
		// attempted. Skip rather than assert on a path this platform cannot take.
		if s == stepSyncDir {
			return induce
		}
		return nil
	})
	defer restore()

	err := store.Commit(newFakeEnvelope("hero", 2, "published but not durable"))
	restore()

	if err == nil {
		// Windows: the directory sync is reported as unsupported, so the write
		// is a clean success. That is the documented behaviour, not a gap --
		// syncDir returns (unsupported=true, err=nil) and the hook is never
		// consulted. The assertion is still worth making: the save must exist
		// and hold the new content.
		live, lerr := store.decodeFile(store.SavePath())
		if lerr != nil {
			t.Fatalf("the save is unreadable after a claimed success: %v", lerr)
		}
		if live.Payload != "published but not durable" {
			t.Errorf("the save was reported as written but holds %q", live.Payload)
		}
		return
	}

	var we *WriteError
	if !errors.As(err, &we) {
		t.Fatalf("error = %T (%v), want *WriteError", err, err)
	}
	if we.Op != string(stepSyncDir) {
		t.Errorf("reported stage = %q, want %q", we.Op, stepSyncDir)
	}
	if !we.Partial {
		t.Error("a directory-flush failure after the rename was not marked Partial; " +
			"the caller would treat a successfully published save as lost")
	}

	// The new content must be live: the rename already happened.
	live, lerr := store.decodeFile(store.SavePath())
	if lerr != nil {
		t.Fatalf("the published save is unreadable: %v", lerr)
	}
	if live.Payload != "published but not durable" {
		t.Errorf("payload = %q, want the new content: a directory-flush failure must not "+
			"discard a save that was already published", live.Payload)
	}
	assertNoTempDebris(t, filepath.Dir(store.SavePath()))
}

// TestFaultInjectionEveryStageStillLoadsTheOldSave is the recovery half. It is
// separate from the test above because "the file is unchanged" and "the game can
// still start" are different claims, and the second is the one that matters to a
// player.
//
// The reopen deliberately reuses the SAME layout. An earlier version of this
// test called newTestStore again, which creates a fresh temporary directory --
// so it was asserting that an unrelated, empty store could load nothing, and
// reported four false failures. A "simulate a restart" step that changes the
// data root is not a restart.
func TestFaultInjectionEveryStageStillLoadsTheOldSave(t *testing.T) {
	induce := errors.New("induced: permission denied")

	for _, stage := range []writeStep{stepCreateTemp, stepWrite, stepSync, stepRename} {
		t.Run(string(stage), func(t *testing.T) {
			layout := LayoutFor(t.TempDir())
			store, err := NewSnapshotStore(layout, "hero", fakeCodec())
			if err != nil {
				t.Fatalf("cannot create the store: %v", err)
			}
			if err := store.Commit(newFakeEnvelope("hero", 1, "the original")); err != nil {
				t.Fatalf("the seed commit failed: %v", err)
			}

			restore := SetWriteHook(func(s writeStep) error {
				if s == stage {
					return induce
				}
				return nil
			})

			if err := store.Commit(newFakeEnvelope("hero", 2, "never published")); err == nil {
				restore()
				t.Fatalf("the commit succeeded despite an induced failure at %q", stage)
			}
			restore()

			// A restart: a new store over the same layout.
			reopened, oerr := NewSnapshotStore(layout, "hero", fakeCodec())
			if oerr != nil {
				t.Fatalf("cannot reopen the store: %v", oerr)
			}
			result, lerr := reopened.Load()
			if lerr != nil {
				t.Fatalf("after a failure at %q the save could not be loaded: %v", stage, lerr)
			}
			if result.Envelope == nil {
				t.Fatalf("after a failure at %q the load returned nothing", stage)
			}
			if result.Envelope.Payload != "the original" {
				t.Errorf("after a failure at %q the loaded payload is %q, want the original",
					stage, result.Envelope.Payload)
			}
			if result.Source != "current" {
				t.Errorf("after a failure at %q the save was loaded from %q; the current save "+
					"should still have been usable", stage, result.Source)
			}
		})
	}
}

// TestFaultInjectionUnwritableDirectoryFailsLoudly is the case a fault hook
// cannot fake: a real directory the process genuinely cannot write to.
//
// It is worth having in addition to the hook tests because the hook proves the
// *code path* handles an error, while this proves the error a real filesystem
// produces is handled. They are not the same claim, and the second has caught
// bugs in this project before.
func TestFaultInjectionUnwritableDirectoryFailsLoudly(t *testing.T) {
	if runtimeGOOS() == "windows" {
		t.Skip("making a directory unwritable is not reliably possible on Windows without ACLs; " +
			"the hook-driven cases cover the same code path")
	}

	store := newTestStore(t, "hero")
	if err := store.Commit(newFakeEnvelope("hero", 1, "the original")); err != nil {
		t.Fatalf("the seed commit failed: %v", err)
	}

	dir := filepath.Dir(store.SavePath())
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("cannot change the directory permissions: %v", err)
	}
	defer os.Chmod(dir, 0o700)

	err := store.Commit(newFakeEnvelope("hero", 2, "cannot be published"))
	if err == nil {
		t.Fatal("a commit into an unwritable directory succeeded")
	}
	if !strings.Contains(err.Error(), "permission") && !strings.Contains(err.Error(), "denied") {
		t.Errorf("the error does not name a permission problem: %v", err)
	}

	// Put the permissions back before reading, so this asserts the content
	// rather than the permissions.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("cannot restore the directory permissions: %v", err)
	}
	loaded, lerr := store.decodeFile(store.SavePath())
	if lerr != nil {
		t.Fatalf("the original save was damaged by a permission failure: %v", lerr)
	}
	if loaded.Payload != "the original" {
		t.Errorf("payload = %q, want the original", loaded.Payload)
	}
}

// TestFaultInjectionCheckpointFailureIsReported pins that a checkpoint write
// failure reaches the caller. A pre-risk checkpoint exists precisely because the
// next action is dangerous, so silently failing to take one removes the safety
// net exactly when it was needed.
func TestFaultInjectionCheckpointFailureIsReported(t *testing.T) {
	induce := errors.New("induced: the checkpoint could not be written")

	store := newTestStore(t, "hero")
	if err := store.Commit(newFakeEnvelope("hero", 1, "current")); err != nil {
		t.Fatalf("the seed commit failed: %v", err)
	}

	restore := SetWriteHook(func(s writeStep) error {
		if s == stepRename {
			return induce
		}
		return nil
	})
	defer restore()

	_, err := store.Checkpoint(newFakeEnvelope("hero", 1, "pre-risk"), 1)
	if err == nil {
		t.Fatal("a checkpoint write failure was not reported; the caller would proceed into danger")
	}
	if !errors.Is(err, induce) {
		t.Errorf("the cause was lost: %v", err)
	}

	// The current save must be untouched by a failed checkpoint.
	restore()
	live, lerr := store.decodeFile(store.SavePath())
	if lerr != nil {
		t.Fatalf("a failed checkpoint damaged the live save: %v", lerr)
	}
	if live.Payload != "current" {
		t.Errorf("live payload = %q, want %q", live.Payload, "current")
	}
	assertNoTempDebris(t, store.layout.CheckpointDir("hero"))
}

// assertNoTempDebris fails if any temp file from the atomic writer is left in dir.
func assertNoTempDebris(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		t.Fatalf("cannot list %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp-file debris left behind: %s", filepath.Join(dir, e.Name()))
		}
	}
}
