package storage

import (
	"os"
	"testing"
	"time"
)

// TestWindowsFiletimeConversionMatchesKnownValue pins the epoch offset.
//
// The offset is the number of 100ns ticks between 1601-01-01 and 1970-01-01.
// If it were wrong, every process start time would be shifted by the same
// amount and all the lock tests would still agree with each other -- a defect
// that is invisible to any single-process test. So it is checked against an
// externally known value: the FILETIME for the unix epoch itself must convert
// to exactly 0.
func TestWindowsFiletimeConversionMatchesKnownValue(t *testing.T) {
	if runtimeGOOS() != "windows" {
		t.Skip("FILETIME is a Windows representation")
	}

	// The unix epoch expressed as a FILETIME is exactly the offset.
	if got := filetimeToUnixNano(filetimeEpochOffset100ns); got != 0 {
		t.Fatalf("the unix epoch converted to %d, want 0; the epoch offset is wrong", got)
	}

	// One second later must be exactly 1e9 nanoseconds later.
	if got := filetimeToUnixNano(filetimeEpochOffset100ns + filetimeTicksPerSecond); got != 1e9 {
		t.Fatalf("epoch+1s converted to %d, want %d", got, int64(1e9))
	}

	// A value before the epoch has no sensible unix representation; reporting
	// 0 rather than a negative garbage value keeps downstream comparisons sane.
	if got := filetimeToUnixNano(filetimeEpochOffset100ns - 1); got != 0 {
		t.Fatalf("a pre-epoch FILETIME converted to %d, want 0", got)
	}
}

// TestSystemTimeConversionAgreesWithTheGoClock cross-checks the platform time
// source against time.Now.
//
// Two independent clocks that disagree would make a lock look older or newer
// than it is, which is exactly how a stale-lock decision goes wrong. The
// tolerance is generous because this only needs to catch an epoch or unit
// error, not measure precision.
func TestSystemTimeConversionAgreesWithTheGoClock(t *testing.T) {
	if runtimeGOOS() != "windows" {
		t.Skip("systemTimeAsUnixNano is a Windows-specific helper")
	}

	fromPlatform := systemTimeAsUnixNano()
	fromGo := time.Now().UnixNano()
	diff := fromGo - fromPlatform
	if diff < 0 {
		diff = -diff
	}
	const tolerance = int64(2 * time.Second)
	if diff > tolerance {
		t.Fatalf("the platform clock and time.Now differ by %v, want under %v; "+
			"one of them has a wrong epoch or unit", time.Duration(diff), time.Duration(tolerance))
	}
}

// TestProcessStartTimeIsStableAndPlausible checks the start time of this very
// process: it must be readable, in the past, and the same on every read.
//
// Stability is what the lock depends on. If the value changed between the
// acquire and the liveness check, every lock would look like it had been
// taken over by a recycled pid.
func TestProcessStartTimeIsStableAndPlausible(t *testing.T) {
	pid := os.Getpid()

	first := currentProcessStartUnixNano()
	if first == 0 {
		t.Fatalf("this platform cannot report the start time of pid %d; "+
			"stale-lock detection would fall back to refusing, which is safe but "+
			"means a crashed process would block the player until they delete the lock by hand", pid)
	}

	second := currentProcessStartUnixNano()
	if first != second {
		t.Fatalf("start time changed between reads: %d then %d", first, second)
	}

	now := time.Now().UnixNano()
	if first > now {
		t.Fatalf("start time %d is in the future (now %d)", first, now)
	}
	// Nothing that can run this test started before 2000, and the machine
	// cannot have been up longer than that in a test environment.
	const year2000 = int64(946684800) * int64(time.Second)
	if first < year2000 {
		t.Fatalf("start time %d predates 2000; the epoch conversion is almost certainly wrong", first)
	}
}

// TestProcessAliveDistinguishesSelfFromAbsentPid checks both directions of the
// liveness test, because a check that always answers the same way would make
// the stale-lock logic quietly one-sided.
func TestProcessAliveDistinguishesSelfFromAbsentPid(t *testing.T) {
	self := os.Getpid()
	start := currentProcessStartUnixNano()

	alive, found, err := processAlive(self, start)
	if err != nil {
		t.Fatalf("checking this process failed: %v", err)
	}
	if !alive || !found {
		t.Fatalf("this process reported alive=%v found=%v, want true true", alive, found)
	}

	// A pid that cannot exist. It is high enough to be outside any real pid
	// range and is not expected to be allocated during the test.
	const impossiblePid = 1 << 30
	alive, found, err = processAlive(impossiblePid, 1)
	if err != nil {
		t.Fatalf("checking an impossible pid failed: %v", err)
	}
	if found {
		t.Fatalf("pid %d reported as present", impossiblePid)
	}
	if alive {
		t.Fatalf("pid %d reported as alive", impossiblePid)
	}
}

// TestProcessAliveTreatsARecycledPidAsGone is the protection that keeps a
// recycled pid from blocking the player forever.
//
// The scenario is simulated honestly: the same live pid is checked with a
// *wrong* recorded start time, which is byte-for-byte what a recycled pid looks
// like to the check. The pid really exists, so a naive existence-only test
// would call it alive.
func TestProcessAliveTreatsARecycledPidAsGone(t *testing.T) {
	self := os.Getpid()
	realStart := currentProcessStartUnixNano()
	if realStart == 0 {
		t.Skip("this platform cannot report start times, so identity cannot be checked")
	}

	// One microsecond earlier: the pid certainly exists, but the recorded
	// identity does not match.
	alive, found, err := processAlive(self, realStart-int64(time.Microsecond))
	if err != nil {
		t.Fatalf("checking a mismatched start time failed: %v", err)
	}
	if !found {
		t.Fatal("the pid should still be reported as present so callers can tell " +
			"recycling apart from absence")
	}
	if alive {
		t.Fatal("a pid whose recorded start time does not match was reported as alive; " +
			"a recycled pid would block the player forever")
	}
}

// TestProcessAliveRefusesWhenIdentityCannotBeChecked covers the recorded-start-
// time-is-zero case. The safe answer is "assume alive", because refusing to
// start is recoverable and destroying a live session is not.
func TestProcessAliveRefusesWhenIdentityCannotBeChecked(t *testing.T) {
	self := os.Getpid()
	alive, found, err := processAlive(self, 0)
	if err != nil {
		t.Fatalf("checking with no recorded start time failed: %v", err)
	}
	if !found || !alive {
		t.Fatalf("alive=%v found=%v, want true true: with no identity to compare, "+
			"the lock must be treated as live rather than stolen", alive, found)
	}
}
