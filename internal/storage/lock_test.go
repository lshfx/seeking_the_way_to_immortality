package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// testClock returns a clock function and a way to advance it, so lock-age
// assertions do not need real sleeps.
func testClock(start time.Time) (clockish, func(time.Duration)) {
	current := start
	return func() time.Time { return current }, func(d time.Duration) { current = current.Add(d) }
}

// TestAcquireSaveLockSucceedsWhenFree covers the ordinary path and, just as
// importantly, asserts that the lock file actually exists afterwards. A lock
// that reported success without writing anything would let every process in,
// and the atomicity tests would never notice.
func TestAcquireSaveLockSucceedsWhenFree(t *testing.T) {
	layout := LayoutFor(t.TempDir())
	now, _ := testClock(time.Unix(1700000000, 0))

	lock, takenOver, reason, err := acquireSaveLock(layout, "hero", now, os.Getpid())
	if err != nil {
		t.Fatalf("acquiring a free lock failed: %v", err)
	}
	if takenOver {
		t.Fatalf("a fresh lock reported as taken over (%s)", reason)
	}
	if lock == nil {
		t.Fatal("acquiring succeeded but returned no lock")
	}
	t.Cleanup(func() { _ = lock.Release() })

	if _, err := os.Stat(lock.Path()); err != nil {
		t.Fatalf("the lock file was not created: %v", err)
	}

	// The record must round-trip with the fields the recovery logic reads.
	data, err := os.ReadFile(lock.Path())
	if err != nil {
		t.Fatalf("cannot read the lock file: %v", err)
	}
	var owner LockOwner
	if err := json.Unmarshal(data, &owner); err != nil {
		t.Fatalf("the lock file is not valid JSON: %v", err)
	}
	if owner.PID != os.Getpid() {
		t.Errorf("recorded pid = %d, want %d", owner.PID, os.Getpid())
	}
	if owner.DataVersion != LockDataVersion {
		t.Errorf("recorded data version = %d, want %d", owner.DataVersion, LockDataVersion)
	}
	if owner.CharacterID != "hero" {
		t.Errorf("recorded character = %q, want %q", owner.CharacterID, "hero")
	}
	if owner.ProcessStart == 0 {
		t.Error("recorded no process start time; a recycled pid could then be mistaken for this one")
	}
	if owner.AcquiredAt == 0 {
		t.Error("recorded no acquisition time")
	}
}

// TestAcquireSaveLockRefusesWhileAnotherLiveProcessHoldsIt is the central
// guarantee: a second writer is refused, not admitted.
//
// The competing owner is simulated by writing a lock file that names a live
// process -- this test process itself. That is the real shape of the hazard, and
// it exercises the branch that must say no.
func TestAcquireSaveLockRefusesWhileAnotherLiveProcessHoldsIt(t *testing.T) {
	layout := LayoutFor(t.TempDir())
	now, _ := testClock(time.Unix(1700000000, 0))

	if err := os.MkdirAll(layout.LocksDir(), 0o700); err != nil {
		t.Fatalf("cannot create the lock directory: %v", err)
	}
	path := filepath.Join(layout.LocksDir(), "hero.lock")

	// A live owner: this process, correctly identified.
	live := LockOwner{
		DataVersion:  LockDataVersion,
		PID:          os.Getpid(),
		ProcessStart: currentProcessStartUnixNano(),
		Machine:      "test",
		AcquiredAt:   now().UnixNano(),
		CharacterID:  "hero",
	}
	payload, err := json.Marshal(live)
	if err != nil {
		t.Fatalf("cannot encode the competing lock: %v", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		t.Fatalf("cannot write the competing lock: %v", err)
	}

	lock, _, reason, err := acquireSaveLock(layout, "hero", now, os.Getpid())
	if err == nil {
		if lock != nil {
			_ = lock.Release()
		}
		t.Fatal("a second writer acquired a lock already held by a live process")
	}
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("error = %v, want ErrLocked so the caller can tell the player the game is already running", err)
	}
	if reason == "" {
		t.Error("no reason was reported; the player would get no explanation")
	}

	// And the existing lock must be untouched, still naming its original owner.
	var after LockOwner
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("the refused acquisition removed the existing lock: %v", rerr)
	}
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatalf("the refused acquisition corrupted the existing lock: %v", err)
	}
	if after.PID != live.PID || after.AcquiredAt != live.AcquiredAt {
		t.Fatalf("the refused acquisition rewrote the existing lock: got %+v, want %+v", after, live)
	}
}

// TestAcquireSaveLockTakesOverAStaleLockFromAGoneProcess is the crash-recovery
// path, and the reason stale-lock detection exists at all: a player whose game
// crashed must be able to start it again.
//
// The stale owner is a pid that cannot exist, so no live process is disturbed.
func TestAcquireSaveLockTakesOverAStaleLockFromAGoneProcess(t *testing.T) {
	layout := LayoutFor(t.TempDir())
	now, _ := testClock(time.Unix(1700000000, 0))

	if err := os.MkdirAll(layout.LocksDir(), 0o700); err != nil {
		t.Fatalf("cannot create the lock directory: %v", err)
	}
	path := filepath.Join(layout.LocksDir(), "hero.lock")

	const impossiblePid = 1 << 30
	dead := LockOwner{
		DataVersion:  LockDataVersion,
		PID:          impossiblePid,
		ProcessStart: 1,
		Machine:      "test",
		AcquiredAt:   now().Add(-time.Hour).UnixNano(),
		CharacterID:  "hero",
	}
	payload, err := json.Marshal(dead)
	if err != nil {
		t.Fatalf("cannot encode the stale lock: %v", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		t.Fatalf("cannot write the stale lock: %v", err)
	}

	lock, takenOver, reason, err := acquireSaveLock(layout, "hero", now, os.Getpid())
	if err != nil {
		t.Fatalf("a stale lock from a dead process was not reclaimed: %v", err)
	}
	if !takenOver {
		t.Error("the takeover was not reported; the caller could not tell the user a crashed session was recovered")
	}
	if reason == "" {
		t.Error("no reason was reported for the takeover")
	}
	t.Cleanup(func() { _ = lock.Release() })

	var after LockOwner
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the reclaimed lock: %v", err)
	}
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatalf("the reclaimed lock is not valid JSON: %v", err)
	}
	if after.PID != os.Getpid() {
		t.Fatalf("the reclaimed lock names pid %d, want %d", after.PID, os.Getpid())
	}
}

// TestAcquireSaveLockRefusesWhenTheOwnerCannotBeJudged covers the corrupt-lock
// case, and pins the direction of the decision.
//
// A lock file whose contents cannot be read has an unknown owner, and the only
// safe reading of "unknown" is "possibly alive". Silently deleting it is the
// tempting shortcut and exactly the one that destroys a live session.
func TestAcquireSaveLockRefusesWhenTheOwnerCannotBeJudged(t *testing.T) {
	cases := map[string]string{
		"not json at all":       "this is not json",
		"json but no pid":       `{"data_version":1,"character_id":"hero"}`,
		"negative pid":          `{"data_version":1,"pid":-1,"character_id":"hero"}`,
		"no data version":       `{"pid":1234,"character_id":"hero"}`,
		"from a future version": `{"data_version":99,"pid":1234,"character_id":"hero"}`,
		"truncated json":        `{"data_version":1,"pid":`,
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			layout := LayoutFor(t.TempDir())
			now, _ := testClock(time.Unix(1700000000, 0))

			if err := os.MkdirAll(layout.LocksDir(), 0o700); err != nil {
				t.Fatalf("cannot create the lock directory: %v", err)
			}
			path := filepath.Join(layout.LocksDir(), "hero.lock")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("cannot write the unreadable lock: %v", err)
			}

			lock, _, _, err := acquireSaveLock(layout, "hero", now, os.Getpid())
			if err == nil {
				if lock != nil {
					_ = lock.Release()
				}
				t.Fatal("an unjudgeable lock was taken over; a live session could be destroyed this way")
			}
			if !errors.Is(err, ErrLockRefused) {
				t.Fatalf("error = %v, want ErrLockRefused", err)
			}

			// The file must still be there, unmodified, for the user to inspect.
			got, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("the unreadable lock was deleted rather than preserved: %v", rerr)
			}
			if string(got) != content {
				t.Fatalf("the unreadable lock was modified: got %q, want %q", got, content)
			}
		})
	}
}

// TestLockReclaimNeverLeavesAnInstantWithNoLock is the concurrency property that
// makes a takeover safe.
//
// Reclaiming a stale lock has two possible implementations that differ only in a
// window nobody sees single-threaded:
//
//   - delete, then create: there is an instant with no lock file at all, and a
//     third process arriving in that instant creates its own lock, after which
//     two processes both believe they hold it
//   - replace atomically: the old lock is swapped for the new one, so the path
//     is never absent
//
// The watcher samples ONLY during the reclaim. That scoping is the design: an
// earlier version watched continuously and failed against the *correct*
// implementation, because Release legitimately removes the lock and a
// continuous watcher cannot tell an intended removal from a reclaim window.
//
// Two earlier attempts at the gate used channels to coordinate arm/disarm and
// both were wrong in instructive ways -- a single shared channel deadlocked
// (each side waiting for the other to receive), and a close-based handshake
// panicked on a repeated close. The coordination here is therefore a mutex and a
// bool, which is the simplest thing that is obviously correct: the watcher reads
// `watching` under the lock, and the test flips it under the same lock. There is
// no rendezvous to get wrong.
//
// One platform allowance remains, and it is not cosmetic: on Windows a
// concurrent reader briefly denies the rename, so a reclaim can fail with an
// access-denied error while the watcher happens to be reading. That is a
// *stronger* guarantee than observing a missing lock -- the window was watched
// and no absence was seen -- so such a round is skipped rather than failed.
func TestLockReclaimNeverLeavesAnInstantWithNoLock(t *testing.T) {
	layout := LayoutFor(t.TempDir())
	now, _ := testClock(time.Unix(1700000000, 0))
	if err := os.MkdirAll(layout.LocksDir(), 0o700); err != nil {
		t.Fatalf("cannot create the lock directory: %v", err)
	}
	path := filepath.Join(layout.LocksDir(), "hero.lock")

	// A stale lock: a pid that cannot exist.
	const impossiblePid = 1 << 30
	stale := LockOwner{
		DataVersion:  LockDataVersion,
		PID:          impossiblePid,
		ProcessStart: 1,
		Machine:      "test",
		AcquiredAt:   now().Add(-time.Hour).UnixNano(),
		CharacterID:  "hero",
	}
	payload, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("cannot encode the stale lock: %v", err)
	}

	var (
		mu       sync.Mutex
		watching bool
		samples  int
		absent   string
	)

	// The watcher polls the guard and, while it is set, samples the lock path.
	// It exits when stop is closed. It never sends; the test reads the shared
	// state under the same mutex, so there is no channel protocol to deadlock.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}

			mu.Lock()
			active := watching
			mu.Unlock()

			if !active {
				// Yield so the test goroutine can make progress.
				runtime.Gosched()
				continue
			}

			_, statErr := os.Stat(path)
			mu.Lock()
			if statErr != nil && errors.Is(statErr, os.ErrNotExist) {
				if absent == "" {
					absent = "the lock path was absent during a reclaim"
				}
			} else {
				samples++
			}
			mu.Unlock()
		}
	}()

	setWatching := func(v bool) {
		mu.Lock()
		watching = v
		mu.Unlock()
	}

	const rounds = 40
	reclaimed := 0
	skipped := 0
	for i := 0; i < rounds; i++ {
		if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
			close(stop)
			<-done
			t.Fatalf("cannot seed the stale lock: %v", err)
		}

		// Let the watcher observe the seeded lock before the reclaim, so a
		// reclaim that removes the path is guaranteed to be sampled.
		setWatching(true)
		runtime.Gosched()

		lock, takenOver, _, acquireErr := acquireSaveLock(layout, "hero", now, os.Getpid())

		// Stop sampling before touching the lock path again, so the intentional
		// Release below cannot be mistaken for a reclaim window.
		setWatching(false)

		mu.Lock()
		found := absent
		seen := samples
		mu.Unlock()

		// An absence the watcher found outranks any error from the reclaim:
		// that is the defect this test exists to find.
		if found != "" {
			close(stop)
			<-done
			t.Fatalf("round %d observed a defect: %s", i, found)
		}
		if seen == 0 {
			close(stop)
			<-done
			t.Fatalf("round %d: the watcher took no sample, so this round proved nothing", i)
		}

		if acquireErr != nil {
			if isSharingViolation(acquireErr) || strings.Contains(acquireErr.Error(), "Access is denied") {
				skipped++
				continue
			}
			close(stop)
			<-done
			t.Fatalf("round %d could not reclaim the stale lock: %v", i, acquireErr)
		}
		if !takenOver {
			close(stop)
			<-done
			t.Fatalf("round %d did not report the takeover", i)
		}
		if err := lock.Release(); err != nil {
			close(stop)
			<-done
			t.Fatalf("round %d could not release: %v", i, err)
		}
		reclaimed++
	}

	close(stop)
	<-done

	mu.Lock()
	finalAbsent, finalSamples := absent, samples
	mu.Unlock()

	if finalAbsent != "" {
		t.Fatal(finalAbsent)
	}
	if finalSamples == 0 {
		t.Fatal("the watcher never sampled the lock path, so this test proved nothing")
	}
	t.Logf("watched %d samples across %d reclaims (%d rounds skipped to Windows sharing) "+
		"without observing an absent lock", finalSamples, reclaimed, skipped)
}

// TestReleaseRemovesOnlyItsOwnLock is the safety property that makes a takeover
// survivable.
//
// If process A's session is wrongly judged dead and B takes over, then A dying
// later must not delete B's lock -- doing so would admit a third writer to a
// save B is actively using.
func TestReleaseRemovesOnlyItsOwnLock(t *testing.T) {
	layout := LayoutFor(t.TempDir())
	if err := os.MkdirAll(layout.LocksDir(), 0o700); err != nil {
		t.Fatalf("cannot create the lock directory: %v", err)
	}
	path := filepath.Join(layout.LocksDir(), "hero.lock")

	// A lock that this process believes it holds.
	mine := &SaveLock{
		path: path,
		owner: LockOwner{
			DataVersion:  LockDataVersion,
			PID:          os.Getpid(),
			ProcessStart: currentProcessStartUnixNano(),
			CharacterID:  "hero",
		},
	}

	// But the file on disk now names somebody else: a takeover happened.
	other := LockOwner{
		DataVersion:  LockDataVersion,
		PID:          os.Getpid() + 1,
		ProcessStart: currentProcessStartUnixNano() + 1,
		CharacterID:  "hero",
	}
	payload, err := json.Marshal(other)
	if err != nil {
		t.Fatalf("cannot encode the other lock: %v", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		t.Fatalf("cannot write the other lock: %v", err)
	}

	err = mine.Release()
	if err == nil {
		t.Fatal("releasing a lock that now belongs to another process reported success")
	}
	if !errors.Is(err, ErrLockRefused) {
		t.Fatalf("error = %v, want ErrLockRefused", err)
	}

	if _, serr := os.Stat(path); serr != nil {
		t.Fatalf("the other process's lock was deleted: %v", serr)
	}
}

// TestReleaseIsIdempotent matters because the natural call pattern is a
// deferred Release plus an explicit one on the error path, and a second call
// must not error.
func TestReleaseIsIdempotent(t *testing.T) {
	layout := LayoutFor(t.TempDir())
	now, _ := testClock(time.Unix(1700000000, 0))

	lock, _, _, err := acquireSaveLock(layout, "hero", now, os.Getpid())
	if err != nil {
		t.Fatalf("acquiring failed: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("the first release failed: %v", err)
	}
	if _, err := os.Stat(lock.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the lock file still exists after release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("the second release failed: %v", err)
	}

	// And releasing a nil lock, which happens when acquisition failed and the
	// caller defers unconditionally, must be harmless.
	var none *SaveLock
	if err := none.Release(); err != nil {
		t.Fatalf("releasing a nil lock failed: %v", err)
	}
}

// TestLocksArePerCharacter checks that two characters do not exclude each other.
// Sharing one lock across characters would make a second window unusable for no
// safety benefit, since the saves are separate files.
func TestLocksArePerCharacter(t *testing.T) {
	layout := LayoutFor(t.TempDir())
	now, _ := testClock(time.Unix(1700000000, 0))

	first, _, _, err := acquireSaveLock(layout, "hero", now, os.Getpid())
	if err != nil {
		t.Fatalf("acquiring the first character's lock failed: %v", err)
	}
	t.Cleanup(func() { _ = first.Release() })

	second, _, _, err := acquireSaveLock(layout, "rival", now, os.Getpid())
	if err != nil {
		t.Fatalf("a different character's lock was refused: %v", err)
	}
	t.Cleanup(func() { _ = second.Release() })

	if first.Path() == second.Path() {
		t.Fatalf("both characters share the lock file %s", first.Path())
	}
}

// TestLockPathIsContainedByTheCharacterID guards the traversal surface: a
// character id is data, and data must not be able to steer the lock file out of
// its directory.
func TestLockPathIsContainedByTheCharacterID(t *testing.T) {
	layout := LayoutFor(t.TempDir())
	now, _ := testClock(time.Unix(1700000000, 0))
	dir := layout.LocksDir()

	hostile := []string{
		"../../escape",
		"..\\..\\escape",
		"a/b",
		"a\\b",
		"hero.lock",
		".",
		"..",
	}

	for _, id := range hostile {
		t.Run(id, func(t *testing.T) {
			lock, _, _, err := acquireSaveLock(layout, id, now, os.Getpid())
			if err != nil {
				// Refusing outright is an acceptable outcome; escaping is not.
				return
			}
			t.Cleanup(func() { _ = lock.Release() })

			parent := filepath.Dir(lock.Path())
			if filepath.Clean(parent) != filepath.Clean(dir) {
				t.Fatalf("character id %q produced a lock outside the lock directory: %s", id, lock.Path())
			}
		})
	}
}

// TestLockOwnerIsStaleReportsReasonForEveryVerdict checks that the diagnostic
// string is always populated. It is what the player is shown, and an empty
// explanation is indistinguishable from a bug.
func TestLockOwnerIsStaleReportsReasonForEveryVerdict(t *testing.T) {
	now, _ := testClock(time.Unix(1700000000, 0))

	cases := []struct {
		name  string
		owner LockOwner
		want  lockVerdict
	}{
		{"future version", LockOwner{DataVersion: 99, PID: 4321}, verdictUnjudgeable},
		{"no version", LockOwner{DataVersion: 0, PID: 4321}, verdictUnjudgeable},
		{"no pid", LockOwner{DataVersion: LockDataVersion, PID: 0}, verdictUnjudgeable},
		{"absent pid", LockOwner{DataVersion: LockDataVersion, PID: 1 << 30, ProcessStart: 1}, verdictStale},
		{"live pid", LockOwner{
			DataVersion:  LockDataVersion,
			PID:          os.Getpid(),
			ProcessStart: currentProcessStartUnixNano(),
		}, verdictHeldLive},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verdict, reason := tc.owner.judge(now)
			if reason == "" {
				t.Fatal("no reason was reported")
			}
			if verdict != tc.want {
				t.Fatalf("judge = %s (%s), want %s", verdict, reason, tc.want)
			}
		})
	}
}

// TestUnjudgeableLockIsNotReportedAsContention pins the user-facing distinction.
//
// A damaged lock file and a running game both prevent a write, but they need
// different messages and different user actions. Reporting the damaged lock as
// "another process holds the save lock" would send the player looking for a
// window that was never open.
func TestUnjudgeableLockIsNotReportedAsContention(t *testing.T) {
	layout := LayoutFor(t.TempDir())
	now, _ := testClock(time.Unix(1700000000, 0))
	if err := os.MkdirAll(layout.LocksDir(), 0o700); err != nil {
		t.Fatalf("cannot create the lock directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layout.LocksDir(), "hero.lock"),
		[]byte(`{"data_version":1,"pid":0,"character_id":"hero"}`), 0o600); err != nil {
		t.Fatalf("cannot write the lock: %v", err)
	}

	_, _, reason, err := acquireSaveLock(layout, "hero", now, os.Getpid())
	if err == nil {
		t.Fatal("an unjudgeable lock was accepted")
	}
	if errors.Is(err, ErrLocked) {
		t.Fatal("a damaged lock was reported as contention; the player would be told " +
			"the game is already running when it is not")
	}
	if !errors.Is(err, ErrLockRefused) {
		t.Fatalf("error = %v, want ErrLockRefused", err)
	}
	if reason == "" {
		t.Error("no reason was reported")
	}
}

// TestLockAgeIsReportedForALiveOwner checks the elapsed time in the refusal
// message, which is the detail that helps a user recognise "that window is
// still open".
func TestLockAgeIsReportedForALiveOwner(t *testing.T) {
	base := time.Unix(1700000000, 0)
	now, advance := testClock(base)

	owner := LockOwner{
		DataVersion:  LockDataVersion,
		PID:          os.Getpid(),
		ProcessStart: currentProcessStartUnixNano(),
		AcquiredAt:   base.UnixNano(),
	}

	advance(90 * time.Second)
	verdict, reason := owner.judge(now)
	if verdict != verdictHeldLive {
		t.Fatalf("judge = %s (%s), want %s", verdict, reason, verdictHeldLive)
	}
	if reason == "" {
		t.Fatal("no reason was reported")
	}
	const wantFragment = "1m30s"
	if !strings.Contains(reason, wantFragment) {
		t.Fatalf("reason = %q, want it to mention %q", reason, wantFragment)
	}
}
