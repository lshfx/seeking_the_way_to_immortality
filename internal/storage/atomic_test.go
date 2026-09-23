package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Data root resolution ----------------------------------------------------

// TestResolveDataRootPrefersExplicitOverride proves the override is consulted
// first, so a test or a future portable mode can redirect storage deliberately.
func TestResolveDataRootPrefersExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DataRootEnvVar, dir)

	got, err := ResolveDataRoot()
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got != filepath.Clean(dir) {
		t.Fatalf("ResolveDataRoot = %q, want %q", got, filepath.Clean(dir))
	}
}

// TestResolveDataRootRejectsRelativeOverride is the guard against the failure
// mode the design document names explicitly: silently writing player progress
// relative to whatever directory the game happened to be launched from.
func TestResolveDataRootRejectsRelativeOverride(t *testing.T) {
	t.Setenv(DataRootEnvVar, "relative/saves")

	_, err := ResolveDataRoot()
	if err == nil {
		t.Fatal("a relative override was accepted; saves would land beside the executable")
	}

	var de *DataRootError
	if !errors.As(err, &de) {
		t.Fatalf("error = %T (%v), want *DataRootError", err, err)
	}
	if !strings.Contains(de.Reason, "absolute") {
		t.Fatalf("reason = %q, want it to mention that the path must be absolute", de.Reason)
	}
}

// TestResolveDataRootNeverFallsBackToWorkingDirectory pins the non-fallback
// rule. With no override and no platform directory, resolution must fail; if it
// ever starts returning "." or an executable-relative path, this fails.
func TestResolveDataRootNeverFallsBackToWorkingDirectory(t *testing.T) {
	t.Setenv(DataRootEnvVar, "")
	// Removing the platform source is only possible by making it unavailable,
	// which differs by OS. Instead assert the property that matters directly:
	// whatever comes back is never the working directory.
	got, err := ResolveDataRoot()
	if err != nil {
		if !errors.Is(err, ErrNoDataRoot) {
			t.Fatalf("error = %v, want ErrNoDataRoot when no source exists", err)
		}
		return
	}

	wd, wdErr := os.Getwd()
	if wdErr != nil {
		t.Fatalf("cannot determine working directory: %v", wdErr)
	}
	if filepath.Clean(got) == filepath.Clean(wd) {
		t.Fatalf("ResolveDataRoot returned the working directory %q; "+
			"progress must never be written into the launch directory", got)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("ResolveDataRoot returned a relative path %q", got)
	}
	if !strings.HasSuffix(got, AppDirName) {
		t.Fatalf("ResolveDataRoot = %q, want it to end in %q so the app owns its own folder",
			got, AppDirName)
	}
}

// TestEnsureLayoutCreatesTreeAndIsIdempotent covers the create path and the
// fact that calling it twice must not fail, because every launch calls it.
func TestEnsureLayoutCreatesTreeAndIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "WendaoChangsheng")
	t.Setenv(DataRootEnvVar, root)

	first, err := EnsureLayout()
	if err != nil {
		t.Fatalf("first EnsureLayout failed: %v", err)
	}
	info, err := os.Stat(first.Root)
	if err != nil {
		t.Fatalf("data root was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("data root is not a directory")
	}

	second, err := EnsureLayout()
	if err != nil {
		t.Fatalf("second EnsureLayout failed (must be idempotent): %v", err)
	}
	if first.Root != second.Root {
		t.Fatalf("EnsureLayout is not stable: %q then %q", first.Root, second.Root)
	}
}

// TestEnsureLayoutReportsUnwritableRoot proves an unusable root is a clear
// error rather than a silent success.
//
// The construction deliberately avoids relying on file permission bits: the
// test process may run as a user that can write to a "read-only" directory
// (root, or an Administrator on Windows), which would make a permission-based
// test pass for the wrong reason and never actually exercise the error path.
// Instead the parent is a regular *file*, which cannot contain a directory on
// any platform and fails for a reason the player can act on.
func TestEnsureLayoutReportsUnwritableRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	t.Setenv(DataRootEnvVar, filepath.Join(file, "child"))

	_, err := EnsureLayout()
	if err == nil {
		t.Fatal("EnsureLayout accepted a root whose parent is a regular file")
	}

	var de *DataRootError
	if !errors.As(err, &de) {
		t.Fatalf("error = %T (%v), want *DataRootError", err, err)
	}
	if de.Path == "" {
		t.Error("the error must name the path it tried, so the player can check it")
	}
}

// TestEnsureLayoutReportsErrorOnPermissionDenied covers the genuine permission
// case, but skips when the process can write anyway rather than asserting a
// property the environment does not have.
func TestEnsureLayoutReportsErrorOnPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory would not deny access")
	}
	parent := t.TempDir()
	// t.TempDir() is 0700; drop the owner write bit to deny subtree creation.
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Skipf("cannot make a directory read-only here: %v", err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o700) })

	// Verify the environment actually denies the write; otherwise the test
	// would pass vacuously.
	probe := filepath.Join(parent, "probe")
	if mkErr := os.Mkdir(probe, 0o700); mkErr == nil {
		os.Remove(probe)
		t.Skip("a read-only directory did not deny directory creation here")
	}

	t.Setenv(DataRootEnvVar, filepath.Join(parent, "child"))
	if _, err := EnsureLayout(); err == nil {
		t.Fatal("EnsureLayout succeeded against a read-only parent")
	}
}

// --- Layout paths ------------------------------------------------------------

// TestLayoutPathsAreUnderTheRoot pins that every path a character owns sits
// inside that character's directory. A path that escapes the root would be a
// directory-traversal bug reachable from a corrupted save.
func TestLayoutPathsAreUnderTheRoot(t *testing.T) {
	l := LayoutFor(filepath.Join(t.TempDir(), "root"))
	const gameID = "game-1"

	charDir := filepath.Clean(l.CharDir(gameID))
	for _, p := range []string{
		l.SavePath(gameID),
		l.PrevSavePath(gameID),
		l.CheckpointDir(gameID),
		l.ManualDir(gameID),
		l.CheckpointPath(gameID, 7),
		l.ManualSavePath(gameID, "before-tribulation"),
	} {
		if !pathWithin(filepath.Clean(p), charDir) {
			t.Errorf("path %q escapes the character directory %q", p, charDir)
		}
	}

	// Settings and logs belong to the application, not to a character.
	root := filepath.Clean(l.Root)
	for _, p := range []string{l.SettingsPath(), l.LocksDir(), l.LogsDir()} {
		if !pathWithin(filepath.Clean(p), root) {
			t.Errorf("path %q escapes the root %q", p, root)
		}
	}
	if pathWithin(filepath.Clean(l.SettingsPath()), charDir) {
		t.Error("settings must live outside chars/ so deleting a character does not reset it")
	}
}

// pathWithin reports whether p is inside dir (or equals it).
func pathWithin(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

// TestSanitiseComponentRejectsTraversal is the security-relevant case: a game id
// arriving from a hand-edited or corrupted file must not be able to name a
// directory outside the data root.
func TestSanitiseComponentRejectsTraversal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	l := LayoutFor(root)

	hostile := []string{
		"..",
		".",
		"../escape",
		"..\\escape",
		"a/b",
		"a\\b",
		"....//....//etc",
		"C:\\Windows",
		"/etc/passwd",
		"CON",
		"nul.json",
		"LPT1",
		"",
	}

	for _, id := range hostile {
		dir := filepath.Clean(l.CharDir(id))
		want := filepath.Join(root, "chars")
		if !pathWithin(dir, want) {
			t.Errorf("game id %q produced %q, which escapes %q", id, dir, want)
		}
		// The escaped name must not equal the parent either: "chars/.." would
		// resolve to the root and let a save overwrite settings.
		if dir == filepath.Clean(want) {
			t.Errorf("game id %q collapses onto the chars directory itself", id)
		}
	}
}

// TestSanitiseComponentIsInjective checks that distinct ids yield distinct
// directories, so two characters cannot silently share one save file.
func TestSanitiseComponentIsInjective(t *testing.T) {
	ids := []string{
		"abc", "ABC", "a-b", "a_b", "a.b",
		"a/b", "a\\b", "a%b", "a b", "中", "a中b",
		"", ".", "..", "%2F", "a%2Fb",
	}
	seen := make(map[string]string, len(ids))
	for _, id := range ids {
		got := sanitiseComponent(id)
		if prev, ok := seen[got]; ok && prev != id {
			t.Errorf("ids %q and %q both map to %q", prev, id, got)
		}
		seen[got] = id
	}
}

// TestSanitiseComponentIsStable guards against a mapping that changes between
// runs, which would make an existing save unreachable after an upgrade.
func TestSanitiseComponentIsStable(t *testing.T) {
	const id = "中a/b\\c d.e"
	first := sanitiseComponent(id)
	for i := 0; i < 5; i++ {
		if got := sanitiseComponent(id); got != first {
			t.Fatalf("sanitiseComponent(%q) is not stable: %q then %q", id, first, got)
		}
	}
}

// TestRevisionNameSortsLexicographicallyInNumericOrder matters because the
// checkpoint listing and retention logic sort file names. If the padding were
// wrong, "10" would sort before "9" and the retention rule would delete the
// newest checkpoint instead of the oldest.
func TestRevisionNameSortsLexicographicallyInNumericOrder(t *testing.T) {
	nums := []uint64{0, 1, 2, 9, 10, 11, 99, 100, 1000, 1 << 40, ^uint64(0)}
	for i := 1; i < len(nums); i++ {
		if !(revisionName(nums[i-1]) < revisionName(nums[i])) {
			t.Fatalf("revisionName(%d)=%q does not sort before revisionName(%d)=%q",
				nums[i-1], revisionName(nums[i-1]), nums[i], revisionName(nums[i]))
		}
	}
	for _, n := range nums {
		if len(revisionName(n)) != 20 {
			t.Fatalf("revisionName(%d) = %q, want fixed width 20", n, revisionName(n))
		}
	}
}

// --- Atomic write ------------------------------------------------------------

// TestAtomicWriteCreatesThenReplaces covers the basic contract.
func TestAtomicWriteCreatesThenReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "save.json")

	if _, err := atomicWriteFile(path, []byte("first"), 0o600, SyncFull); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}
	if got, _, _ := readFileIfExists(path); string(got) != "first" {
		t.Fatalf("content = %q, want %q", got, "first")
	}

	if _, err := atomicWriteFile(path, []byte("second"), 0o600, SyncFull); err != nil {
		t.Fatalf("replacement write failed: %v", err)
	}
	if got, _, _ := readFileIfExists(path); string(got) != "second" {
		t.Fatalf("content = %q, want %q", got, "second")
	}
}

// TestAtomicWriteReportsHonestDurability pins what the write actually achieved.
//
// On Windows directory flushing is unsupported, and the code must say so rather
// than either failing the write (the data IS in place) or claiming a directory
// flush that never happened. This test asserts the two are mutually exclusive
// and that a SyncFull write never reports a directory sync it did not perform.
func TestAtomicWriteReportsHonestDurability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "save.json")

	outcome, err := atomicWriteFile(path, []byte("payload"), 0o600, SyncFull)
	if err != nil {
		t.Fatalf("SyncFull write failed: %v", err)
	}
	if outcome.Sync != SyncFull {
		t.Errorf("outcome.Sync = %v, want %v", outcome.Sync, SyncFull)
	}
	if outcome.DirectorySynced && outcome.DirectorySyncUnsupported {
		t.Fatal("the outcome claims both that the directory was flushed and that flushing is unsupported")
	}
	if !outcome.DirectorySynced && !outcome.DirectorySyncUnsupported {
		t.Fatal("a SyncFull write must report either a directory flush or that it is unsupported; " +
			"silence here would let an unperformed flush look like success")
	}
	if outcome.Supported() == outcome.DirectorySyncUnsupported {
		t.Error("Supported() disagrees with DirectorySyncUnsupported")
	}

	// SyncNone must claim neither.
	outcome, err = atomicWriteFile(path, []byte("payload"), 0o600, SyncNone)
	if err != nil {
		t.Fatalf("SyncNone write failed: %v", err)
	}
	if outcome.DirectorySynced {
		t.Error("a SyncNone write reported a directory flush")
	}
}

// TestAtomicWritePublishesOnlyCompleteData is the real atomicity test.
//
// The error-classification tests above only prove *which step* reported a
// failure; they say nothing about whether the bytes the player's save is
// replaced with are ever visible in a half-written state.
//
// This test has been through two failed versions, and both failure modes are
// worth keeping on the record because they are the reason the test now looks
// the way it does.
//
// Version 1 looped a reader while a 4 MiB write ran. It PASSED with the atomic
// writer replaced by a truncate-in-place, because it never sampled the window
// it claimed to check -- the write lands in the page cache as one fast memcpy
// and the observer goroutine was simply never scheduled inside it. Correctness
// must not depend on winning a race.
//
// Version 2 held the write open with a pause hook and had the reader sample the
// *target* during the suspended window. That looked deterministic and was still
// vacuous on Windows: the target's size and content do not change until the
// rename, even for a truncate-in-place writer, because the directory entry is
// not updated while a handle is open. Both implementations showed the old value,
// so the assertion could not distinguish them -- a re-run of the counter-proof
// proved it, reporting "NOT CAUGHT".
//
// What is actually observable, and actually differs, is the staging file:
//
//   - temp-file design: just before publication there is a separate file
//     holding exactly the new bytes, and the target still holds the old bytes
//   - truncate-in-place design: there is no such file; the bytes went straight
//     into the target, which is the defect
//
// So the assertion is structural and needs no race: at the instant before
// publication the new bytes must exist in a staging file, complete, and
// *separate from* the target, while the target still shows the old value. A
// partial publish is then impossible by construction, because publication is a
// single rename.
func TestAtomicWritePublishesOnlyCompleteData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save.json")

	const oldContent = "OLD"
	if _, err := atomicWriteFile(path, []byte(oldContent), 0o600, SyncNone); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	newContent := strings.Repeat("N", 4<<20)

	type observation struct {
		stagingPath    string
		stagingLen     int
		stagingHead    []byte
		targetLen      int
		targetHead     []byte
		targetReadable bool
	}
	observed := make(chan observation, 1)

	entered := make(chan struct{})
	release := make(chan struct{})
	restorePause := SetWritePauseHook(func(stagingPath string) {
		obs := observation{stagingPath: stagingPath}

		if data, ok, err := readFileIfExists(stagingPath); err == nil && ok {
			obs.stagingLen = len(data)
			obs.stagingHead = append([]byte(nil), data[:min(16, len(data))]...)
		}
		if data, ok, err := readFileIfExists(path); err == nil && ok {
			obs.targetLen = len(data)
			if len(data) >= 16 {
				obs.targetHead = append([]byte(nil), data[:16]...)
				obs.targetReadable = true
			}
		}

		observed <- obs
		close(entered)
		<-release
	})
	defer restorePause()

	writeDone := make(chan error, 1)
	go func() {
		_, err := atomicWriteFile(path, []byte(newContent), 0o600, SyncNone)
		writeDone <- err
	}()

	<-entered
	obs := <-observed
	close(release)

	if err := <-writeDone; err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 1. The new bytes must be staged somewhere other than the target. Without
	//    a separate staging file the write is in-place by definition, which is
	//    the defect this whole file exists to prevent.
	if obs.stagingPath == "" || filepath.Clean(obs.stagingPath) == filepath.Clean(path) {
		t.Fatalf("the write was not staged in a separate file (staging=%q, target=%q); "+
			"publishing must not write into the target before it is complete",
			obs.stagingPath, path)
	}
	if filepath.Dir(obs.stagingPath) != filepath.Dir(path) {
		t.Errorf("staging file %q is not on the same filesystem as the target %q; "+
			"the publish could not be atomic", obs.stagingPath, path)
	}

	// 2. The staged bytes must be complete at the moment of publication. A
	//    truncated staging file would mean a truncated save after the rename.
	if obs.stagingLen != len(newContent) {
		t.Fatalf("staged bytes = %d, want %d; publishing a partial staging file would "+
			"publish a partial save", obs.stagingLen, len(newContent))
	}
	if want := []byte(strings.Repeat("N", 16)); string(obs.stagingHead) != string(want) {
		t.Fatalf("staged head = %q, want %q", obs.stagingHead, want)
	}

	// 3. And the target must still hold the previous value, untouched. If the
	//    target has already changed, the old save was destroyed before the new
	//    one was published, so a crash at this instant loses both.
	//
	//    Only the length is compared, not the bytes. On Windows a file that is
	//    concurrently open for writing cannot always be re-read, and a read that
	//    yields no bytes must not be mistaken for a zero-filled target -- an
	//    earlier draft compared the head bytes and failed spuriously with
	//    head="" on an untouched file. Length is both sufficient and robust:
	//    the old and new payloads differ in length by four megabytes, so an
	//    in-place writer cannot hide behind it.
	if obs.targetLen != len(oldContent) {
		t.Fatalf("the target was modified before publication: len=%d, want len=%d (%d bytes were "+
			"already written into the live save)", obs.targetLen, len(oldContent), obs.targetLen)
	}
	if obs.targetReadable && string(obs.targetHead) != oldContent {
		t.Fatalf("the target was modified before publication: head=%q, want %q",
			obs.targetHead, oldContent)
	}

	// 4. Publication is all-or-nothing, so the final state is the complete new
	//    value.
	if got, ok, _ := readFileIfExists(path); !ok || len(got) != len(newContent) {
		t.Fatalf("final content length = %d (present=%v), want %d", len(got), ok, len(newContent))
	}
}

// isSharingViolation reports whether err is a Windows sharing violation, which
// a concurrent reader can observe while a rename is swapping the target.
//
// The check is deliberately by message rather than by syscall.Errno: importing
// syscall for one constant would add a platform-specific dependency to code
// whose whole point is to stay portable, and a false negative here only makes
// the atomicity test stricter.
func isSharingViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "being used by another process") ||
		strings.Contains(msg, "sharing violation")
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [24]byte
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

// TestAtomicWriteCleansUpTempAfterFailure checks the debris rule on a genuine
// mid-write failure rather than a failure to create the temp file.
//
// The target's directory is made unwritable *after* the file has been written
// once, so the temp file is created successfully and then the rename fails.
// That is the only path where debris could plausibly be left behind.
func TestAtomicWriteCleansUpTempAfterFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory would not deny the rename")
	}
	if os.PathSeparator == '\\' {
		// Fabricating a rename-only failure on Windows requires holding the
		// target open, which this test deliberately does not do (it would be
		// testing the OS, not our cleanup). The directory-listing assertion in
		// TestAtomicWriteLeavesNoTempFiles still covers the success path.
		t.Skip("rename-only failure is not portably fabricable on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "save.json")
	if _, err := atomicWriteFile(path, []byte("first"), 0o600, SyncNone); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skipf("cannot make the directory read-only: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	// Confirm the environment denies the operation; otherwise this test would
	// pass without exercising the cleanup path at all.
	if _, err := atomicWriteFile(path, []byte("second"), 0o600, SyncNone); err == nil {
		t.Skip("the read-only directory did not deny the write here")
	}

	// The original content must survive, and no temp file may remain.
	if got, ok, _ := readFileIfExists(path); !ok || string(got) != "first" {
		t.Fatalf("a failed write damaged the existing file: ok=%v content=%q", ok, got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot list the directory: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("a failed write left temp file %q behind", e.Name())
		}
	}
}

// TestAtomicWriteCleansUpTempAtEveryFailureStage drives every stage of the
// write through the failure hook and asserts two things each time: the error
// names the stage that failed, and no debris is left in the directory.
//
// This exists because the real-filesystem failure test can only fabricate the
// failure at create-temp (by making the parent a file), and the rename-only
// failure is not portably fabricable on Windows. Without the hook, the deferred
// cleanup on those paths would be untested on the primary target platform --
// which is exactly how dead cleanup code survives.
func TestAtomicWriteCleansUpTempAtEveryFailureStage(t *testing.T) {
	induce := errors.New("induced failure")

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
			dir := t.TempDir()
			path := filepath.Join(dir, "save.json")

			const original = "ORIGINAL"
			if _, err := atomicWriteFile(path, []byte(original), 0o600, SyncNone); err != nil {
				t.Fatalf("seed write failed: %v", err)
			}

			restore := SetWriteHook(func(s writeStep) error {
				if s == stage {
					return induce
				}
				return nil
			})
			defer restore()

			_, err := atomicWriteFile(path, []byte("REPLACEMENT"), 0o600, SyncFull)
			if err == nil {
				t.Fatalf("the write succeeded despite an induced failure at %q", stage)
			}

			var we *WriteError
			if !errors.As(err, &we) {
				t.Fatalf("error = %T (%v), want *WriteError", err, err)
			}
			if we.Op != string(stage) {
				t.Errorf("reported stage = %q, want %q so the player-facing message names the real problem",
					we.Op, stage)
			}

			// No debris, whatever the stage.
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("cannot list the directory: %v", err)
			}
			for _, e := range entries {
				if strings.Contains(e.Name(), ".tmp-") {
					t.Errorf("a failure at %q left temp file %q behind", stage, e.Name())
				}
			}

			// The previous content must survive every stage except a successful
			// rename, and the rename hook fires *before* the rename happens.
			if got, ok, _ := readFileIfExists(path); !ok || string(got) != original {
				t.Errorf("a failure at %q damaged the existing save: ok=%v content=%q", stage, ok, got)
			}
		})
	}
}

// TestWriteHookIsNilInProduction pins that the injection points cost nothing
// and do nothing unless a test installs one.
//
// Both hooks are checked. The pause hook was added after counter-proof 15
// showed the atomicity test could not observe the window it claimed to: its
// absence is not a nicety, it is what makes that test non-vacuous.
func TestWriteHookIsNilInProduction(t *testing.T) {
	if writeHook != nil {
		t.Fatal("the write hook is non-nil; a previous test leaked it and production writes would be affected")
	}
	if err := failStep(stepRename); err != nil {
		t.Fatalf("failStep with no hook returned %v, want nil", err)
	}
	if writePauseHook != nil {
		t.Fatal("the pause hook is non-nil; a previous test leaked it and production writes would be affected")
	}
	// Installing then restoring must leave the package exactly as it was found.
	restore := SetWritePauseHook(func(stagingPath string) {})
	if writePauseHook == nil {
		t.Fatal("SetWritePauseHook did not install the hook")
	}
	restore()
	if writePauseHook != nil {
		t.Fatal("the pause hook restore function did not clear the hook")
	}
}

// TestAtomicWriteLeavesNoTempFiles is the debris check on the success path: a
// write must not leave files that a later listing could mistake for a save, and
// that a player would see accumulating in their data directory.
func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save.json")

	for i := 0; i < 3; i++ {
		if _, err := atomicWriteFile(path, []byte("payload"), 0o600, SyncNone); err != nil {
			t.Fatalf("write %d failed: %v", i, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot list directory: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "save.json" {
			t.Errorf("unexpected leftover file %q after successful writes", e.Name())
		}
	}
}

// TestAtomicWriteRejectsUnwritableTarget covers the disk-full and permission
// failure class: the error must name the step and leave the previous content
// intact.
func TestAtomicWriteRejectsUnwritableTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save.json")

	if _, err := atomicWriteFile(path, []byte("good"), 0o600, SyncNone); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	// Target a path whose "directory" is a regular file: CreateTemp must fail.
	blocked := filepath.Join(path, "child.json")
	_, err := atomicWriteFile(blocked, []byte("nope"), 0o600, SyncNone)
	if err == nil {
		t.Fatal("write into a path under a regular file succeeded")
	}

	var we *WriteError
	if !errors.As(err, &we) {
		t.Fatalf("error = %T (%v), want *WriteError", err, err)
	}
	if we.Op != "create-temp" {
		t.Errorf("failed step = %q, want %q so the player-facing message is accurate", we.Op, "create-temp")
	}
	if !strings.Contains(we.Error(), blocked) {
		t.Errorf("error %q does not mention the target path", we.Error())
	}

	// The original file must still be readable and unchanged.
	if got, ok, _ := readFileIfExists(path); !ok || string(got) != "good" {
		t.Fatalf("a failed write damaged the existing file: ok=%v content=%q", ok, got)
	}
}

// TestWriteErrorDistinguishesPartialFromFailed is important because a partial
// write means the data IS in place and only a durability guarantee is missing.
// Reporting it as a plain failure would make the game tell the player the save
// did not happen when it did.
func TestWriteErrorDistinguishesPartialFromFailed(t *testing.T) {
	partial := &WriteError{Path: "p", Op: "sync-dir", Err: errors.New("nope"), Partial: true}
	if !strings.Contains(partial.Error(), "partially") {
		t.Errorf("partial error %q should say the write partially completed", partial.Error())
	}

	failed := &WriteError{Path: "p", Op: "rename", Err: errors.New("nope")}
	if strings.Contains(failed.Error(), "partially") {
		t.Errorf("a plain failure %q must not claim partial success", failed.Error())
	}
	if !errors.Is(failed, failed.Err) {
		t.Error("WriteError must unwrap to its cause for errors.Is")
	}
}

// TestReadFileIfExistsTreatsMissingAsAbsentNotError matters because the first
// launch has no save, and treating that as a failure would force every caller
// to special-case it.
func TestReadFileIfExistsTreatsMissingAsAbsentNotError(t *testing.T) {
	dir := t.TempDir()
	data, ok, err := readFileIfExists(filepath.Join(dir, "absent.json"))
	if err != nil {
		t.Fatalf("a missing file produced an error: %v", err)
	}
	if ok {
		t.Fatal("a missing file reported as present")
	}
	if data != nil {
		t.Fatalf("data = %q, want nil", data)
	}

	path := filepath.Join(dir, "present.json")
	if err := os.WriteFile(path, []byte("hi"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	data, ok, err = readFileIfExists(path)
	if err != nil || !ok || string(data) != "hi" {
		t.Fatalf("present file read = (%q, %v, %v), want (\"hi\", true, nil)", data, ok, err)
	}
}

// TestSyncStrategyStrings records the evidence vocabulary used in the recovery
// report, so a change cannot silently relabel what was actually done.
func TestSyncStrategyStrings(t *testing.T) {
	cases := map[SyncStrategy]string{
		SyncFull:         "full",
		SyncFileOnly:     "file-only",
		SyncNone:         "none",
		SyncStrategy(99): "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("SyncStrategy(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}
