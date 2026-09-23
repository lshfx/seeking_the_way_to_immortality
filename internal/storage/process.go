package storage

import "os"

// processStartUnixNano reports when the given process started, in unix
// nanoseconds, or 0 when it cannot be determined.
//
// It is read from the process's own start time rather than from the lock's
// acquisition time on purpose: the question being answered is "is this still
// the same process?", and a start time is the identity, while an acquisition
// time only says when the lock was taken.
func processStartUnixNano(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	return processStartUnixNanoPlatform(pid)
}

// processAlive reports whether the process holding a lock is still the process
// that took it.
//
// The three return values carry distinct meanings and callers must not collapse
// them:
//
//   - (true, true, nil)   the pid exists and is the same process
//   - (false, true, nil)  the pid exists but is a different process, so the
//     original owner is gone and the pid was recycled
//   - (false, false, nil) the pid does not exist
//
// Losing the distinction between the second and third case would be a real
// defect in one direction or the other: treating a recycled pid as stale is
// harmless, but treating it as alive blocks the player forever, and treating a
// live process as recycled destroys their session.
func processAlive(pid int, expectedStartUnixNano int64) (alive bool, found bool, err error) {
	if pid <= 0 {
		return false, false, nil
	}
	found, err = processExistsPlatform(pid)
	if err != nil || !found {
		return false, found, err
	}
	if expectedStartUnixNano == 0 {
		// The lock recorded no start time, so identity cannot be confirmed. The
		// pid exists; report it as alive and let the caller refuse. Refusing is
		// the recoverable mistake.
		return true, true, nil
	}
	actual := processStartUnixNanoPlatform(pid)
	if actual == 0 {
		// This platform cannot read start times. Same reasoning: prefer
		// refusing to guessing.
		return true, true, nil
	}
	if actual != expectedStartUnixNano {
		return false, true, nil
	}
	return true, true, nil
}

// currentProcessStartUnixNano is the start time of this process, used for tests
// that assert the self-recorded identity is accurate.
func currentProcessStartUnixNano() int64 {
	return processStartUnixNano(os.Getpid())
}
