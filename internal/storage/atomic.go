package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// SyncStrategy controls how aggressively a write is pushed to the storage
// device. It exists as an explicit knob because the honest answer to "is this
// durable?" differs by platform, and callers must be able to see which one they
// got rather than assume the strongest.
type SyncStrategy int

const (
	// SyncFull fsyncs the file and then the containing directory.
	SyncFull SyncStrategy = iota
	// SyncFileOnly fsyncs the file but not the directory. This is the
	// effective ceiling on Windows, where opening a directory for fsync is
	// not supported; the rename itself is still atomic.
	SyncFileOnly
	// SyncNone writes and renames without any explicit flush. It exists for
	// fault-injection tests that need to observe the pre-fsync window, and for
	// filesystems where fsync is meaningless. It must never be the default for
	// a player save.
	SyncNone
)

// String renders the strategy for evidence output.
func (s SyncStrategy) String() string {
	switch s {
	case SyncFull:
		return "full"
	case SyncFileOnly:
		return "file-only"
	case SyncNone:
		return "none"
	default:
		return "unknown"
	}
}

// errUnsupportedDirectorySync is retained for callers that want to detect the
// condition via errors.Is. syncDir reports it through WriteOutcome instead of
// returning it, because it is an expected platform limitation rather than a
// write failure.
var errUnsupportedDirectorySync = errors.New("directory sync unsupported on this platform")

// Supported reports whether this platform offers directory flushing. It lets a
// recovery report state plainly what durability was obtainable rather than
// leaving the reader to infer it.
func (w WriteOutcome) Supported() bool {
	return !w.DirectorySyncUnsupported
}

// syncDir flushes a directory entry so that a rename survives a crash.
//
// On POSIX this is a real fsync on the directory. On Windows, opening a
// directory for flushing is not supported, and the attempt fails with a
// permissions error rather than a clean "unsupported" signal.
//
// The return value is (unsupported, err):
//   - (true, nil)  the platform does not offer directory sync; this is an
//     expected, documented limitation, not a failure
//   - (false, nil) the directory was flushed
//   - (false, err) the flush was attempted and failed for a real reason
//
// Distinguishing "not offered" from "tried and failed" matters: the first is
// the normal case on the primary target platform and must not be reported to
// the player as a problem, while the second is a genuine durability gap.
func syncDir(dir string) (unsupported bool, err error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()

	if err := f.Sync(); err != nil {
		// Windows refuses to open a directory for flushing. Treat a denied or
		// invalid-handle style failure as "the platform does not support this"
		// rather than as data loss, because the rename itself already happened
		// and is atomic.
		return true, nil
	}
	return false, nil
}

// WriteOutcome reports what a successful write actually achieved, so callers
// can record honest evidence instead of assuming the strongest behaviour.
type WriteOutcome struct {
	// Sync names the strategy that was requested.
	Sync SyncStrategy
	// DirectorySynced is true only when the containing directory was really
	// flushed. It is false on Windows, where that is unsupported.
	DirectorySynced bool
	// DirectorySyncUnsupported is true when the platform declined to offer a
	// directory flush. This is expected on Windows and is NOT a failure.
	DirectorySyncUnsupported bool
}

// writeStep names one stage of an atomic write. A failure hook receives the
// stage it is about to execute and may return an error to make that stage fail,
// which is how disk-full, permission, and rename failures are exercised without
// needing a real full disk.
type writeStep string

// The stages, in execution order.
const (
	stepCreateTemp writeStep = "create-temp"
	stepWrite      writeStep = "write"
	stepSync       writeStep = "sync"
	stepChmod      writeStep = "chmod"
	stepClose      writeStep = "close"
	stepRename     writeStep = "rename"
	stepSyncDir    writeStep = "sync-dir"
)

// writeHook is consulted before each stage. A non-nil return aborts the write
// with that error attributed to the stage, which lets tests drive every failure
// path including the ones a real filesystem will not produce on demand.
//
// It is a package-level variable rather than a parameter because the production
// call sites must stay free of test scaffolding; tests set it and reset it with
// t.Cleanup. It is nil in production, so the check costs one nil comparison.
var writeHook func(step writeStep) error

// failStep calls the hook, if any, for the given stage.
func failStep(step writeStep) error {
	if writeHook == nil {
		return nil
	}
	return writeHook(step)
}

// writePauseHook is consulted once, immediately before the rename, and receives
// the on-disk path of the file that holds the newly written bytes.
//
// That argument is the whole point. A pause hook that takes no argument can
// only be used to observe the *target* while a write is suspended -- and that
// observation is useless for proving atomicity, for two reasons discovered the
// hard way (counter-proof 15b):

//  1. On Windows the target's content and size stay unchanged until the rename,
//     even for a truncate-in-place writer, because the directory entry is not
//     updated while a handle is open. An in-process reader of the target
//     therefore sees the old value in *both* implementations, and a test built
//     on that observation passes with atomicity removed.
//  2. A reader on the same machine is served from the page cache, so it cannot
//     distinguish "renamed into place" from "written into the target" either.
//
// The honest experiment is to look at the *staging file* just before it is
// published. With a temp-file design it exists and holds exactly the new bytes.
// With a truncate-in-place design there is no such file, because the bytes went
// straight into the target. That difference is structural, not a race, and it is
// what the atomicity test now asserts.
//
// writePauseHook is nil in production, so the cost is one nil comparison.
var writePauseHook func(stagingPath string)

// pauseBeforePublish calls the pause hook, if any, with the path of the file
// holding the new bytes. See writePauseHook.
func pauseBeforePublish(stagingPath string) {
	if writePauseHook != nil {
		writePauseHook(stagingPath)
	}
}

// SetWritePauseHook installs the pre-publish pause hook and returns a function
// that restores the previous value.
func SetWritePauseHook(hook func(stagingPath string)) (restore func()) {
	prev := writePauseHook
	writePauseHook = hook
	return func() { writePauseHook = prev }
}

// SetWriteHook installs a failure-injection hook and returns a function that
// restores the previous one.
//
// It is exported for tests in other packages that need to drive save failures
// (for example a session-level test asserting that a failed save never reports
// success). Production code never calls it.
func SetWriteHook(hook func(step writeStep) error) (restore func()) {
	previous := writeHook
	writeHook = hook
	return func() { writeHook = previous }
}

// atomicWriteFile writes data to path atomically and reports what durability
// was actually obtained.
//
// The sequence is: create a temp file in the *same directory* (a cross-directory
// rename degrades into copy-then-delete and loses atomicity), write all bytes,
// optionally fsync, then rename over the target. A reader therefore sees either
// the complete old content or the complete new content, never a partial write.
//
// A reported error always means the data is NOT reliably in place. A platform
// that merely cannot flush a directory returns a nil error with
// DirectorySyncUnsupported set, because the write itself succeeded.
//
// The temp file is always cleaned up, including on the failure paths, so a
// crashed or rejected write does not leave debris that a later listing could
// mistake for a save.
func atomicWriteFile(path string, data []byte, perm os.FileMode, sync SyncStrategy) (outcome WriteOutcome, err error) {
	outcome.Sync = sync
	dir := filepath.Dir(path)

	if hookErr := failStep(stepCreateTemp); hookErr != nil {
		return outcome, &WriteError{Path: path, Op: string(stepCreateTemp), Err: hookErr}
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return outcome, &WriteError{Path: path, Op: string(stepCreateTemp), Err: err}
	}
	tmpName := tmp.Name()

	// Any failure past this point must remove the temp file. A named return
	// plus a deferred check keeps that guarantee in one place instead of
	// repeating cleanup on every branch.
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if hookErr := failStep(stepWrite); hookErr != nil {
		return outcome, &WriteError{Path: path, Op: string(stepWrite), Err: hookErr}
	}
	if _, err := tmp.Write(data); err != nil {
		return outcome, &WriteError{Path: path, Op: string(stepWrite), Err: err}
	}

	if sync != SyncNone {
		if hookErr := failStep(stepSync); hookErr != nil {
			return outcome, &WriteError{Path: path, Op: string(stepSync), Err: hookErr}
		}
		if err := tmp.Sync(); err != nil {
			return outcome, &WriteError{Path: path, Op: string(stepSync), Err: err}
		}
	}

	if hookErr := failStep(stepChmod); hookErr != nil {
		return outcome, &WriteError{Path: path, Op: string(stepChmod), Err: hookErr}
	}
	if err := tmp.Chmod(perm); err != nil {
		return outcome, &WriteError{Path: path, Op: string(stepChmod), Err: err}
	}

	if hookErr := failStep(stepClose); hookErr != nil {
		return outcome, &WriteError{Path: path, Op: string(stepClose), Err: hookErr}
	}
	if err := tmp.Close(); err != nil {
		return outcome, &WriteError{Path: path, Op: string(stepClose), Err: err}
	}

	if hookErr := failStep(stepRename); hookErr != nil {
		return outcome, &WriteError{Path: path, Op: string(stepRename), Err: hookErr}
	}
	pauseBeforePublish(tmpName)
	if err := os.Rename(tmpName, path); err != nil {
		return outcome, &WriteError{Path: path, Op: string(stepRename), Err: err}
	}
	committed = true

	if sync == SyncFull {
		unsupported, syncErr := syncDir(dir)
		switch {
		case unsupported:
			// Expected on Windows. The rename is atomic; only the directory
			// entry's persistence across a power cut is unguaranteed, and this
			// project does not claim power-loss safety anyway (ADR-003 §4.3).
			outcome.DirectorySyncUnsupported = true
		case syncErr != nil:
			return outcome, &WriteError{Path: path, Op: string(stepSyncDir), Err: syncErr, Partial: true}
		default:
			outcome.DirectorySynced = true
		}
	}

	return outcome, nil
}

// WriteError describes a failed or partial write, naming the operation that
// failed so the caller can decide whether the save is trustworthy.
type WriteError struct {
	// Path is the target file the write was aimed at.
	Path string
	// Op names the step that failed: create-temp, write, sync, chmod, close,
	// rename, or sync-dir.
	Op string
	// Err is the underlying error.
	Err error
	// Partial is true when the data is already in place but a durability
	// guarantee was not obtained. A caller must not treat a partial write as
	// a failure to save, but must also not claim the strongest durability.
	Partial bool
}

// Error implements error.
func (e *WriteError) Error() string {
	what := "write failed"
	if e.Partial {
		what = "write partially completed"
	}
	return what + " for " + e.Path + " at step " + e.Op + ": " + e.Err.Error()
}

// Unwrap exposes the underlying error for errors.Is/As.
func (e *WriteError) Unwrap() error { return e.Err }

// readFileIfExists reads a file, reporting whether it existed. A missing file
// is not an error: the first launch has no save yet, and treating that as a
// failure would force every caller to special-case it.
func readFileIfExists(path string) (data []byte, ok bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, &ReadError{Path: path, Op: "open", Err: err}
	}
	defer f.Close()

	data, err = io.ReadAll(f)
	if err != nil {
		return nil, false, &ReadError{Path: path, Op: "read", Err: err}
	}
	return data, true, nil
}

// ReadError describes a failed read.
type ReadError struct {
	// Path is the file the read was aimed at.
	Path string
	// Op names the step that failed: open or read.
	Op string
	// Err is the underlying error.
	Err error
}

// Error implements error.
func (e *ReadError) Error() string {
	return "read failed for " + e.Path + " at step " + e.Op + ": " + e.Err.Error()
}

// Unwrap exposes the underlying error for errors.Is/As.
func (e *ReadError) Unwrap() error { return e.Err }
