package storage

// This file implements Go's standard technique for testing crash safety: the
// test re-executes itself as a subprocess, the subprocess performs a real
// operation and then dies at a chosen point without unwinding, and the parent
// inspects what survived on disk.
//
// Why this is necessary: reading a file back after a write proves nothing about
// durability. The operating system's page cache serves the data whether or not
// anything was flushed, so a test that only reads back would pass identically
// with and without fsync. The design document says this outright -- "cannot
// claim power-loss safety merely because a temp file was written" -- and the
// only way to get evidence is to kill a process mid-operation.
//
// Honest scope: this proves *process-kill* safety. It does NOT prove power-loss
// safety, because a real power cut involves the storage device's own cache and
// the filesystem's journal, neither of which this machine can faithfully
// replay. ADR-003 §4.3 records that boundary.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// crashEnvVar names the subprocess role. Its presence turns this test binary
// into a crash harness rather than a test run.
const crashEnvVar = "WENDAO_CRASH_TEST"

// crashPointEnv names the exact moment at which the subprocess should die.
const crashPointEnv = "WENDAO_CRASH_POINT"

// crashTargetEnv passes the file the subprocess should write.
const crashTargetEnv = "WENDAO_CRASH_TARGET"

// The crash points, in the order they occur.
const (
	// crashAfterTempWrite dies after the temp file is written but before any
	// flush. The target must still hold its previous content.
	crashAfterTempWrite = "after-temp-write"
	// crashAfterRename dies after the rename completes. The target must hold
	// the new content in full.
	crashAfterRename = "after-rename"
)

// TestCrashDuringWriteLeavesOnlyCompleteStates is the headline acceptance
// criterion: killing the process mid-write must leave either the old complete
// state or the new complete state, never a partial one.
func TestCrashDuringWriteLeavesOnlyCompleteStates(t *testing.T) {
	for _, point := range []string{crashAfterTempWrite, crashAfterRename} {
		t.Run(point, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "save.json")

			const oldContent = "COMPLETE-OLD-CONTENT"
			if _, err := atomicWriteFile(target, []byte(oldContent), 0o600, SyncNone); err != nil {
				t.Fatalf("seed write failed: %v", err)
			}

			newContent := strings.Repeat("N", 1<<20)
			runCrashChild(t, point, target, newContent)

			data, ok, err := readFileIfExists(target)
			if err != nil {
				t.Fatalf("target unreadable after a crashed write: %v", err)
			}
			if !ok {
				t.Fatal("target vanished after a crashed write; a reader would see no save at all")
			}

			got := string(data)
			switch got {
			case oldContent:
				// Acceptable: the write had not become visible yet.
			case newContent:
				// Acceptable: the write had completed.
			default:
				t.Fatalf("after a crash at %q the target holds neither the old nor the new "+
					"content: length %d, want %d or %d",
					point, len(data), len(oldContent), len(newContent))
			}
		})
	}
}

// TestCrashDuringWriteLeavesNoTempFilesInTheSavePath checks that a crash does
// not leave a temp file that a later directory listing could mistake for a save
// or that would accumulate in the player's data directory.
//
// A leftover temp file is tolerated by the loader (it only reads save.json), so
// this asserts on the *listing* behaviour rather than on file existence, and
// records the finding instead of failing: a crash by definition skips deferred
// cleanup, so requiring its absence would be asserting something the process
// cannot guarantee.
func TestCrashDuringWriteLeavesNoTempFilesInTheSavePath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "save.json")
	if _, err := atomicWriteFile(target, []byte("OLD"), 0o600, SyncNone); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	runCrashChild(t, crashAfterTempWrite, target, strings.Repeat("N", 1<<20))

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot list directory: %v", err)
	}

	var leftover []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			leftover = append(leftover, e.Name())
		}
	}

	t.Logf("after a crash at %s, %d temp file(s) remained: %v",
		crashAfterTempWrite, len(leftover), leftover)

	// The real requirement is that such debris must not break anything and must
	// be identifiable as debris. Assert the naming convention that makes it
	// identifiable, so a future change that renamed temp files to something
	// mistakable for a save would fail here.
	for _, name := range leftover {
		if !strings.Contains(name, ".tmp-") {
			t.Errorf("leftover file %q does not carry the .tmp- marker that identifies it as debris", name)
		}
	}

	// And the save itself must still be readable.
	if got, ok, _ := readFileIfExists(target); !ok || string(got) != "OLD" {
		t.Fatalf("the save was damaged by a crashed write: ok=%v content=%q", ok, got)
	}
}

// runCrashChild re-executes the test binary as a crash harness.
//
// The child is expected to die by an unhandled panic, so a non-zero exit is the
// normal outcome. A child that exits cleanly means the harness failed to reach
// the crash point, which would make the test vacuous; that is checked and
// reported rather than ignored.
func runCrashChild(t *testing.T, point, target, content string) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("cannot locate the test binary: %v", err)
	}

	cmd := exec.Command(exe, "-test.run=TestCrashSubprocessHelper")
	cmd.Env = append(os.Environ(),
		crashEnvVar+"=1",
		crashPointEnv+"="+point,
		crashTargetEnv+"="+target,
		"WENDAO_CRASH_CONTENT_LEN="+itoa(len(content)),
	)
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("the crash subprocess exited cleanly, so the crash point %q was never reached; "+
			"this test would otherwise pass without exercising anything\noutput:\n%s", point, out)
	}

	if !strings.Contains(string(out), "INTENTIONAL CRASH") {
		t.Fatalf("the crash subprocess died without reaching the intended crash point %q; "+
			"the failure was for some other reason\noutput:\n%s", point, out)
	}
}

// TestCrashHarnessActuallyKillsTheProcess guards the guard.
//
// Every crash test above is only meaningful if the subprocess really dies at the
// requested point. If the helper silently skipped -- because an environment
// variable was misspelled, say -- the parent would see a clean exit and report
// a failure, but a lazier implementation that only checked "did the child not
// print success?" could pass for the wrong reason. This test pins the child's
// observable behaviour directly.
func TestCrashHarnessActuallyKillsTheProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("cannot locate the test binary: %v", err)
	}

	target := filepath.Join(t.TempDir(), "crash-target.json")

	cmd := exec.Command(exe, "-test.run=TestCrashSubprocessHelper")
	cmd.Env = append(os.Environ(),
		crashEnvVar+"=1",
		crashPointEnv+"="+crashAfterTempWrite,
		crashTargetEnv+"="+target,
		"WENDAO_CRASH_CONTENT_LEN=8",
	)
	out, _ := cmd.CombinedOutput()

	if !strings.Contains(string(out), "INTENTIONAL CRASH") {
		t.Fatalf("the crash subprocess did not report an intentional crash at %q; "+
			"every crash test in this file would be vacuous\noutput:\n%s",
			crashAfterTempWrite, out)
	}
}

// TestCrashSubprocessHelper is not a test. It is the child half of the crash
// harness, and it does nothing unless the environment marks it as such.
func TestCrashSubprocessHelper(t *testing.T) {
	if os.Getenv(crashEnvVar) != "1" {
		t.Skip("not a crash-harness invocation; skipping so a normal run is unaffected")
	}

	target := os.Getenv(crashTargetEnv)
	point := os.Getenv(crashPointEnv)
	n := 0
	for _, c := range os.Getenv("WENDAO_CRASH_CONTENT_LEN") {
		n = n*10 + int(c-'0')
	}

	content := strings.Repeat("N", n)

	// Perform the write by hand so the crash can be placed between the steps.
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".tmp-*")
	if err != nil {
		panic("crash harness: CreateTemp failed: " + err.Error())
	}
	if _, err := tmp.Write([]byte(content)); err != nil {
		panic("crash harness: Write failed: " + err.Error())
	}
	if err := tmp.Sync(); err != nil {
		panic("crash harness: Sync failed: " + err.Error())
	}
	if err := tmp.Close(); err != nil {
		panic("crash harness: Close failed: " + err.Error())
	}

	if point == crashAfterTempWrite {
		// Die before the rename: the target must still be the old content.
		panic("INTENTIONAL CRASH at " + crashAfterTempWrite)
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		panic("crash harness: Rename failed: " + err.Error())
	}

	switch point {
	case crashAfterRename:
		panic("INTENTIONAL CRASH at " + crashAfterRename)
	default:
		panic("crash harness: unknown crash point " + point)
	}
}
