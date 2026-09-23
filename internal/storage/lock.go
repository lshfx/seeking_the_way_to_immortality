package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// LockDataVersion identifies the on-disk shape of a lock file. It exists so a
// lock written by a newer build is refused rather than misread, because
// mistaking a newer lock for a stale one is how two processes end up writing
// the same save.
const LockDataVersion = 1

// ErrLocked reports that another live process holds the save's write lock.
//
// This is deliberately a refusal, not a retry loop. The user's model is "the
// game is already running", and silently taking over a live process's save is
// how a session's progress gets destroyed.
var ErrLocked = errors.New("another process holds the save lock")

// ErrLockRefused reports that a lock could not be taken for a reason other than
// contention: an unwritable directory, a lock file written by a future version,
// or a corrupt lock whose owner cannot be determined.
//
// The distinction matters because the two cases need different messages and
// carry different risk. ErrLocked is expected and harmless; ErrLockRefused
// means the program cannot establish exclusivity and must therefore refuse to
// write rather than guess.
var ErrLockRefused = errors.New("the save lock could not be established")

// LockOwner is the evidence a lock file records about who holds it.
//
// ProcessStartedAt is not decoration. PIDs are recycled, and on Windows the
// same PID is reused relatively soon. Comparing the recorded start time against
// the live process's start time is what prevents a recycled PID from making a
// stale lock look alive -- and the reverse, a live lock look stale, which is the
// dangerous direction.
type LockOwner struct {
	DataVersion  int    `json:"data_version"`
	PID          int    `json:"pid"`
	ProcessStart int64  `json:"process_start_unix_nano"`
	Machine      string `json:"machine"`
	AcquiredAt   int64  `json:"acquired_at_unix_nano"`
	CharacterID  string `json:"character_id"`
	// ExePath is recorded so a user can see which program holds the lock. It is
	// evidence for the human reading the message, never a decision input: two
	// installs may legitimately share a data root.
	ExePath string `json:"exe_path,omitempty"`
}

// lockVerdict is what a lock file's contents say about its owner.
//
// It is a three-way answer rather than a boolean, and that is the point. Two
// very different situations both mean "you cannot write":
//
//   - the owner is a live process  -> someone is playing; expected and harmless
//   - the owner cannot be judged   -> the file is corrupt, or from a future
//     version; something is wrong and a human should look at it
//
// Collapsing them into one "locked" error would tell a player "the game is
// already running" when in fact their lock file is damaged, which is a
// misleading message for a problem they need to act on.
type lockVerdict int

const (
	// verdictFree means no lock file exists.
	verdictFree lockVerdict = iota
	// verdictHeldLive means a running process owns the lock.
	verdictHeldLive
	// verdictStale means the recorded owner is provably gone and may be
	// reclaimed.
	verdictStale
	// verdictUnjudgeable means the lock cannot be interpreted, so no takeover
	// is safe.
	verdictUnjudgeable
)

// String renders the verdict for diagnostics and tests.
func (v lockVerdict) String() string {
	switch v {
	case verdictFree:
		return "free"
	case verdictHeldLive:
		return "held-live"
	case verdictStale:
		return "stale"
	case verdictUnjudgeable:
		return "unjudgeable"
	default:
		return "unknown"
	}
}

// judge classifies this owner record.
func (o LockOwner) judge(look clockish) (verdict lockVerdict, reason string) {
	if o.DataVersion > LockDataVersion {
		return verdictUnjudgeable, fmt.Sprintf("the lock was written by a newer version (data version %d)", o.DataVersion)
	}
	if o.DataVersion <= 0 {
		return verdictUnjudgeable, "the lock records no data version, so its owner cannot be judged"
	}
	if o.PID <= 0 {
		return verdictUnjudgeable, "the lock records no usable pid, so its owner cannot be judged"
	}

	alive, found, err := processAlive(o.PID, o.ProcessStart)
	if err != nil {
		return verdictUnjudgeable, fmt.Sprintf("cannot determine whether process %d is running: %v", o.PID, err)
	}
	if !found {
		return verdictStale, fmt.Sprintf("process %d no longer exists", o.PID)
	}
	if !alive {
		// The pid exists but belongs to a different process, so the original
		// owner is gone and the pid has been recycled.
		return verdictStale, fmt.Sprintf("pid %d was recycled by a different process", o.PID)
	}
	if o.AcquiredAt != 0 {
		if held := look.Since(o.AcquiredAt); held > 0 {
			return verdictHeldLive, fmt.Sprintf("process %d is still running (held for %s)", o.PID, held.Round(time.Second))
		}
	}
	return verdictHeldLive, fmt.Sprintf("process %d is still running", o.PID)
}

// clockish is the narrow clock surface the lock needs. A function is used
// rather than a time.Time so tests can advance it without a real sleep, and so
// the package keeps its "no hidden time dependency" property.
type clockish func() time.Time

// wallClock is the production clock.
func wallClock() time.Time { return time.Now() }

// Since reports how long ago a unix-nano instant was, never negative.
func (c clockish) Since(unixNano int64) time.Duration {
	d := c().Sub(time.Unix(0, unixNano))
	if d < 0 {
		return 0
	}
	return d
}

// SaveLock is an acquired exclusive lock. It must be released, and Release is
// idempotent so a deferred call after an explicit one is harmless.
type SaveLock struct {
	path     string
	owner    LockOwner
	released bool
}

// Owner exposes the recorded owner, for diagnostics.
func (l *SaveLock) Owner() LockOwner { return l.owner }

// Path exposes the lock file path, for diagnostics.
func (l *SaveLock) Path() string { return l.path }

// Release removes the lock file.
//
// It removes the file only if it still contains this process's own record. If
// the lock was taken over (because this process was believed dead) then the file
// belongs to somebody else, and deleting it would let two writers in.
func (l *SaveLock) Release() error {
	if l == nil || l.released {
		return nil
	}
	l.released = true

	current, owner, err := readLock(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !current || owner.PID != l.owner.PID || owner.ProcessStart != l.owner.ProcessStart {
		// Not ours any more. Say so rather than deleting another process's lock.
		return fmt.Errorf("%w: %s no longer belongs to this process", ErrLockRefused, l.path)
	}
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// AcquireSaveLock takes the exclusive write lock for a character.
//
// takenOver is true when a stale lock left by a crashed process was reclaimed;
// it is reported so the caller can tell the user what happened instead of
// silently proceeding.
//
// The sequence is deliberately "write a unique temp file, then link it into
// place" rather than "check, then write". A check-then-write has a window in
// which two processes both see no lock and both write one; the exclusive
// creation of the final path is what actually serialises them.
func AcquireSaveLock(layout Layout, gameID string) (lock *SaveLock, takenOver bool, staleReason string, err error) {
	return acquireSaveLock(layout, gameID, wallClock, os.Getpid())
}

func acquireSaveLock(layout Layout, gameID string, now clockish, pid int) (*SaveLock, bool, string, error) {
	dir := layout.LocksDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, "", fmt.Errorf("%w: cannot create the lock directory: %v", ErrLockRefused, err)
	}

	path := filepath.Join(dir, sanitiseComponent(gameID)+".lock")
	owner := LockOwner{
		DataVersion:  LockDataVersion,
		PID:          pid,
		ProcessStart: processStartUnixNano(pid),
		Machine:      machineName(),
		AcquiredAt:   now().UnixNano(),
		CharacterID:  gameID,
		ExePath:      exePath(),
	}

	payload, err := json.Marshal(owner)
	if err != nil {
		return nil, false, "", fmt.Errorf("%w: cannot encode the lock record: %v", ErrLockRefused, err)
	}
	payload = append(payload, '\n')

	if wrote, err := createLockExclusively(path, payload); err != nil {
		return nil, false, "", err
	} else if wrote {
		return &SaveLock{path: path, owner: owner}, false, "", nil
	}

	// The lock file already exists. Decide whether its owner is really gone.
	_, existing, err := readLock(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Vanished between the exclusive create and the read: another
			// process released it in that window. Retry once rather than
			// failing, because this is a race we can lose harmlessly.
			if wrote, err := createLockExclusively(path, payload); err != nil {
				return nil, false, "", err
			} else if wrote {
				return &SaveLock{path: path, owner: owner}, false, "", nil
			}
			return nil, false, "", fmt.Errorf("%w: %s keeps appearing and disappearing", ErrLockRefused, path)
		}
		return nil, false, "", err
	}

	verdict, reason := existing.judge(now)
	switch verdict {
	case verdictHeldLive:
		return nil, false, reason, fmt.Errorf("%w: %s (%s)", ErrLocked, path, reason)
	case verdictUnjudgeable:
		// Not contention: nobody is holding this lock, we simply cannot tell
		// what it is. The file is left in place for the user to inspect.
		return nil, false, reason, fmt.Errorf("%w: %s (%s)", ErrLockRefused, path, reason)
	case verdictFree:
		// The file disappeared between the exclusive create and the read, and
		// readLock already reported it present, so this is a transient state.
		return nil, false, reason, fmt.Errorf("%w: %s changed while being examined, try again", ErrLockRefused, path)
	}

	// verdictStale: the owner is provably gone. Replace the lock atomically
	// rather than deleting then creating, so there is never an instant with no
	// lock file for a third process to slip into.
	if _, err := atomicWriteFile(path, payload, 0o600, SyncFull); err != nil {
		// Even after judging the owner dead we can lose the race to a live
		// process doing the same takeover. Report contention rather than
		// corruption in that case.
		return nil, false, "", fmt.Errorf("%w: cannot reclaim the stale lock %s: %v", ErrLockRefused, path, err)
	}
	return &SaveLock{path: path, owner: owner}, true, reason, nil
}

// createLockExclusively writes the lock payload only if path does not exist.
//
// It reports (false, nil) when the path already exists, which is contention
// rather than an error, so the caller can inspect the existing owner.
//
// O_EXCL is what makes this safe: the existence test and the creation are one
// indivisible filesystem operation, unlike a separate stat followed by a write.
func createLockExclusively(path string, payload []byte) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("%w: cannot create %s: %v", ErrLockRefused, path, err)
	}
	// From here, any failure must not leave a half-written lock that another
	// process would read as a live owner with garbage fields.
	committed := false
	defer func() {
		f.Close()
		if !committed {
			os.Remove(path)
		}
	}()

	if _, err := f.Write(payload); err != nil {
		return false, fmt.Errorf("%w: cannot write %s: %v", ErrLockRefused, path, err)
	}
	if err := f.Sync(); err != nil {
		return false, fmt.Errorf("%w: cannot flush %s: %v", ErrLockRefused, path, err)
	}
	if err := f.Close(); err != nil {
		return false, fmt.Errorf("%w: cannot close %s: %v", ErrLockRefused, path, err)
	}
	committed = true
	return true, nil
}

// readLock reads and decodes a lock file. present is false when the file is
// absent, which callers treat as "free" rather than as an error.
func readLock(path string) (present bool, owner LockOwner, err error) {
	data, ok, err := readFileIfExists(path)
	if err != nil {
		return false, LockOwner{}, err
	}
	if !ok {
		return false, LockOwner{}, nil
	}
	if err := json.Unmarshal(data, &owner); err != nil {
		// A corrupt lock file is not treated as free. Its owner cannot be
		// judged, so the safe answer is to refuse and let the user investigate
		// or remove the file deliberately.
		return true, LockOwner{}, fmt.Errorf("%w: %s is not a readable lock record: %v", ErrLockRefused, path, err)
	}
	return true, owner, nil
}

// Verdict exposes the classification of a lock file's owner, for diagnostics.
// It lets the startup path explain *why* it refused, which is the difference
// between "the game is already running" and "your lock file is damaged".
func (o LockOwner) Verdict() string {
	v, _ := o.judge(wallClock)
	return v.String()
}

// machineName reports the machine name, falling back to an empty string when
// the host does not supply one. It is recorded as evidence only.
func machineName() string {
	if name, err := os.Hostname(); err == nil {
		return name
	}
	return ""
}

// exePath reports the running executable's path, for diagnostics only.
func exePath() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return ""
}

// runtimeGOOS is split out so the platform files can be exercised without
// importing runtime at every call site.
func runtimeGOOS() string { return runtime.GOOS }
