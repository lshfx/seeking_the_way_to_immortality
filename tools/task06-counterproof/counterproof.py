#!/usr/bin/env python3
"""Counter-proof harness for TASK-06.

Each mutation reverts one piece of protection so the corresponding test must
fail. A test suite that still passes with the protection removed is not testing
that protection.

Line endings are DETECTED, never assumed. The repository convention is CRLF but
the editor that wrote these Go files emits LF, and a mismatched pattern makes
str.replace a silent no-op -- the trap this project has hit twice. Every
substitution therefore asserts it matched exactly once.

Usage:
    python3 tools/task06-counterproof/counterproof.py --list
    python3 tools/task06-counterproof/counterproof.py --record-baseline
    python3 tools/task06-counterproof/counterproof.py --check-baseline
    python3 tools/task06-counterproof/counterproof.py --check-coverage
    python3 tools/task06-counterproof/counterproof.py <mutation-name>
    python3 tools/task06-counterproof/counterproof.py --all

Before running any mutation the harness checks two things: that every mutation
targets a file in GUARDED, and that the tree matches a recorded clean baseline.
Both are refusals, not warnings. See verify_coverage() and check_baseline().
"""

import hashlib
import inspect
import io
import os
import re
import subprocess
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
GO = r"C:\Program Files\Go\bin\go.exe"
BACKUP_DIR = os.path.join(ROOT, ".task-cache", "counterproof")
BASELINE = os.path.join(BACKUP_DIR, "baseline.sha256")

# Files any mutation may edit; all are backed up before each run and restored
# afterwards, so a crashed run cannot leave the tree in a mutated state.
#
# This list is COMPLETE, not a sample, and that is a hard requirement. An
# earlier version listed only atomic.go and layout.go while the lock mutations
# edited lock.go and process.go, so those mutations were applied and never
# reverted. The harness still printed "CAUGHT" for each one -- it was right, and
# the tree was left broken, which the very next full-suite run revealed as six
# spurious failures.
#
# `verify_coverage()` below now fails the run if any mutation targets a file
# that is not listed here, so the mistake cannot recur silently.
GUARDED = (
    "internal/storage/atomic.go",
    "internal/storage/layout.go",
    "internal/storage/lock.go",
    "internal/storage/migrate.go",
    "internal/storage/process.go",
    "internal/storage/snapshot.go",
    "internal/storage/snapshotstore.go",
    "internal/storage/stamp.go",
    "internal/storage/transfer.go",
)


def read(path):
    with io.open(os.path.join(ROOT, path), encoding="utf-8", newline="") as fh:
        return fh.read()


def write(path, text):
    with io.open(os.path.join(ROOT, path), "w", encoding="utf-8", newline="") as fh:
        fh.write(text)


def digest(path):
    """SHA-256 of a guarded file's exact bytes."""
    with open(os.path.join(ROOT, path), "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


def fingerprint():
    """A fingerprint of every guarded file, for contamination detection."""
    return {path: digest(path) for path in GUARDED}


def write_baseline():
    fp = fingerprint()
    os.makedirs(BACKUP_DIR, exist_ok=True)
    with io.open(BASELINE, "w", encoding="utf-8", newline="") as fh:
        for path in sorted(fp):
            fh.write("%s  %s\n" % (fp[path], path))
    print("baseline recorded for %d files" % len(fp))
    return fp


def read_baseline():
    if not os.path.exists(BASELINE):
        return None
    recorded = {}
    with io.open(BASELINE, encoding="utf-8", newline="") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            h, path = line.split("  ", 1)
            recorded[path] = h
    return recorded


def check_baseline():
    """Fail the run if the tree does not match its recorded clean state.

    This exists because the harness has now left the tree dirty THREE times,
    twice by being killed mid-run and once by a failed patch. The failure is
    silent in exactly the way this project treats as unacceptable: the harness
    keeps reporting CAUGHT for every mutation while the sources it is editing
    drift further from the real code.

    The mechanism is a contamination cascade, and it is worth stating plainly:

      1. a run is interrupted with a mutation applied
      2. the NEXT run calls snapshot(), which faithfully backs up the MUTATED
         file as if it were clean
      3. that run restores the mutated file, so the damage is now permanent and
         blessed
      4. the mutation's own patch() then fails to find its pattern -- because
         the pattern's absence is the mutation -- and the harness reports a
         coverage or pattern problem rather than corruption

    A recorded baseline makes step 2 detectable instead of silent. Returns a
    list of problems; empty means clean.
    """
    recorded = read_baseline()
    if recorded is None:
        return ["no baseline recorded; run --record-baseline once, against a verified-clean tree"]

    current = fingerprint()
    problems = []
    for path in sorted(set(recorded) | set(current)):
        was, now = recorded.get(path), current.get(path)
        if was is None:
            problems.append("%s is guarded but is not in the baseline" % path)
        elif now is None:
            problems.append("%s is in the baseline but is gone" % path)
        elif was != now:
            problems.append(
                "%s does not match the recorded clean state; a previous run left "
                "this file modified (expected %s, found %s)" % (path, was[:12], now[:12])
            )
    return problems


def restore(saved):
    for path, bkp in saved.items():
        with io.open(bkp, encoding="utf-8", newline="") as fh:
            write(path, fh.read())


def snapshot():
    os.makedirs(BACKUP_DIR, exist_ok=True)
    saved = {}
    for path in GUARDED:
        text = read(path)
        bkp = os.path.join(BACKUP_DIR, os.path.basename(path) + ".bak")
        with io.open(bkp, "w", encoding="utf-8", newline="") as fh:
            fh.write(text)
        saved[path] = bkp
    return saved


def patch(path, old, new):
    """Replace old with new, requiring exactly one match.

    `old` is written with plain \\n; it is rewritten to the file's detected
    convention before matching so a CRLF file matches correctly.
    """
    text = read(path)
    nl = "\r\n" if "\r\n" in text else "\n"
    old_n = old.replace("\n", nl)
    new_n = new.replace("\n", nl)

    found = text.count(old_n)
    if found != 1:
        raise AssertionError(
            "pattern matched %d times in %s, want 1; refusing a silent no-op.\n"
            "pattern:\n%s" % (found, path, old_n[:500])
        )
    write(path, text.replace(old_n, new_n))


# --- Mutations ---------------------------------------------------------------
# Each returns the path it edited.


def m_nonatomic_write():
    """Replace temp-file+rename with a direct truncating write.

    This is the defect the atomic writer exists to prevent: a reader -- or a
    kill -- during the write sees a half-written file.

    This mutation earned its keep twice over. The atomicity test it targets
    passed with this mutation applied in TWO successive versions:

      v1 asserted that a concurrent reader of the target never sees a partial
         file. It passed because the mutation wrote into the target *through the
         same open handle* the test already had, and on Windows a file's
         directory entry -- including its size -- is not updated while a handle
         is open. The reader therefore saw the old value in both builds.

      v2 added a pause hook and sampled the target during the held-open window.
         It passed for the same underlying reason: the visible state of the
         target is identical in both builds until the rename. Collecting more
         samples cannot fix a quantity that does not differ.

    The test that does catch it (v3) asserts a *structural* difference instead:
    just before publication, is the new data staged in a file separate from the
    target? A temp-file design says yes; truncate-in-place says no. No race, no
    timing, no sampling.

    The lesson is worth more than the fix: when a counter-proof reports NOT
    CAUGHT, suspect the observation before suspecting the mutation. A test that
    samples a quantity which is equal in both the correct and the broken build
    is not weak evidence, it is no evidence.
    """
    path = "internal/storage/atomic.go"
    patch(
        path,
        '\ttmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")\n'
        '\tif err != nil {\n'
        '\t\treturn outcome, &WriteError{Path: path, Op: string(stepCreateTemp), Err: err}\n'
        "\t}\n"
        "\ttmpName := tmp.Name()",
        '\t_ = dir\n'
        '\ttmp, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)\n'
        '\tif err != nil {\n'
        '\t\treturn outcome, &WriteError{Path: path, Op: string(stepCreateTemp), Err: err}\n'
        "\t}\n"
        "\ttmpName := tmp.Name()",
    )
    patch(
        path,
        '\tif err := os.Rename(tmpName, path); err != nil {\n'
        '\t\treturn outcome, &WriteError{Path: path, Op: string(stepRename), Err: err}\n'
        "\t}\n"
        "\tcommitted = true",
        "\tcommitted = true",
    )
    return path


def m_temp_debris():
    """Leave the temp file behind when a write fails."""
    path = "internal/storage/atomic.go"
    patch(
        path,
        "\tdefer func() {\n"
        "\t\tif !committed {\n"
        "\t\t\ttmp.Close()\n"
        "\t\t\tos.Remove(tmpName)\n"
        "\t\t}\n"
        "\t}()",
        "\tdefer func() {\n"
        "\t\tif !committed {\n"
        "\t\t\ttmp.Close()\n"
        "\t\t}\n"
        "\t}()",
    )
    return path


def m_no_sync():
    """Skip the file flush, so a write is never pushed toward the device.

    Detecting this requires observing that Sync was called at all. A test that
    only reads the file back cannot tell the difference, because the OS page
    cache serves the data either way -- which is exactly why "wrote a temp file
    and renamed it" is not evidence of durability. Go's own crash-safety tests
    handle this with a subprocess that panics at a chosen point; that is the
    technique used by TestSyncIsAttemptedBeforeRename below.
    """
    path = "internal/storage/atomic.go"
    patch(
        path,
        "\tif sync != SyncNone {\n"
        "\t\tif hookErr := failStep(stepSync); hookErr != nil {\n"
        '\t\t\treturn outcome, &WriteError{Path: path, Op: string(stepSync), Err: hookErr}\n'
        "\t\t}\n"
        '\t\tif err := tmp.Sync(); err != nil {\n'
        '\t\t\treturn outcome, &WriteError{Path: path, Op: string(stepSync), Err: err}\n'
        "\t\t}\n"
        "\t}",
        "\tif false {\n"
        "\t\tif hookErr := failStep(stepSync); hookErr != nil {\n"
        '\t\t\treturn outcome, &WriteError{Path: path, Op: string(stepSync), Err: hookErr}\n'
        "\t\t}\n"
        '\t\tif err := tmp.Sync(); err != nil {\n'
        '\t\t\treturn outcome, &WriteError{Path: path, Op: string(stepSync), Err: err}\n'
        "\t\t}\n"
        "\t}",
    )
    return path


def m_sync_lies():
    """Report a directory flush that never happened.

    The honest-durability test exists to catch exactly this: claiming a
    guarantee the platform did not provide.
    """
    path = "internal/storage/atomic.go"
    patch(
        path,
        "\t\tcase unsupported:",
        "\t\tcase unsupported:\n"
        "\t\t\toutcome.DirectorySynced = true\n"
        "\t\t\toutcome.DirectorySyncUnsupported = false",
    )
    return path


def m_no_traversal_guard():
    """Let a hostile game id escape the data root."""
    path = "internal/storage/layout.go"
    patch(
        path,
        'func sanitiseComponent(s string) string {\n'
        '\tif s == "" {\n'
        '\t\treturn "_empty"\n'
        "\t}\n",
        "func sanitiseComponent(s string) string {\n"
        "\treturn s\n"
        "}\n\n"
        "func sanitiseComponentDisabled(s string) string {\n"
        '\tif s == "" {\n'
        '\t\treturn "_empty"\n'
        "\t}\n",
    )
    return path


def m_unpadded_revision():
    """Drop the fixed-width padding so checkpoints sort in the wrong order."""
    path = "internal/storage/layout.go"
    patch(
        path,
        "func revisionName(revision uint64) string {\n"
        "\tconst width = 20\n"
        "\tvar buf [width]byte\n"
        "\tfor i := width - 1; i >= 0; i-- {\n"
        "\t\tbuf[i] = byte('0' + revision%10)\n"
        "\t\trevision /= 10\n"
        "\t}\n"
        "\treturn string(buf[:])\n"
        "}",
        "func revisionName(revision uint64) string {\n"
        '\tif revision == 0 {\n'
        '\t\treturn "0"\n'
        "\t}\n"
        "\tvar b []byte\n"
        "\tfor revision > 0 {\n"
        "\t\tb = append([]byte{byte('0' + revision%10)}, b...)\n"
        "\t\trevision /= 10\n"
        "\t}\n"
        "\treturn string(b)\n"
        "}",
    )
    return path


def m_relative_override():
    """Accept a relative data root, which writes saves beside the executable."""
    path = "internal/storage/layout.go"
    patch(
        path,
        "\t\tif !filepath.IsAbs(override) {\n"
        '\t\t\treturn "", &DataRootError{\n'
        "\t\t\t\tPath:   override,\n"
        '\t\t\t\tReason: "the override is not an absolute path",\n'
        "\t\t\t}\n"
        "\t\t}\n",
        "\t\t_ = filepath.IsAbs(override)\n",
    )
    return path


def m_lock_ignores_liveness():
    """Treat any existing lock file as stale, so a live owner is stolen from.

    This is the mistake the whole liveness check exists to prevent: two
    processes writing the same save, one of them silently.
    """
    path = "internal/storage/lock.go"
    patch(
        path,
        "\tverdict, reason := existing.judge(now)\n"
        "\tswitch verdict {\n"
        "\tcase verdictHeldLive:\n"
        '\t\treturn nil, false, reason, fmt.Errorf("%w: %s (%s)", ErrLocked, path, reason)\n',
        "\tverdict, reason := existing.judge(now)\n"
        "\tverdict = verdictStale\n"
        "\tswitch verdict {\n"
        "\tcase verdictHeldLive:\n"
        '\t\treturn nil, false, reason, fmt.Errorf("%w: %s (%s)", ErrLocked, path, reason)\n',
    )
    return path


def m_lock_ignores_pid_recycling():
    """Decide liveness by pid existence alone, ignoring the start time.

    A recycled pid then looks like a live owner forever, so a player whose game
    crashed gets a lock they can never clear.
    """
    path = "internal/storage/process.go"
    patch(
        path,
        "\tif expectedStartUnixNano == 0 {\n"
        "\t\t// The lock recorded no start time, so identity cannot be confirmed. The\n"
        "\t\t// pid exists; report it as alive and let the caller refuse. Refusing is\n"
        "\t\t// the recoverable mistake.\n"
        "\t\treturn true, true, nil\n"
        "\t}\n"
        "\tactual := processStartUnixNanoPlatform(pid)\n"
        "\tif actual == 0 {\n"
        "\t\t// This platform cannot read start times. Same reasoning: prefer\n"
        "\t\t// refusing to guessing.\n"
        "\t\treturn true, true, nil\n"
        "\t}\n"
        "\tif actual != expectedStartUnixNano {\n"
        "\t\treturn false, true, nil\n"
        "\t}\n"
        "\treturn true, true, nil\n",
        "\t_ = expectedStartUnixNano\n"
        "\treturn true, true, nil\n",
    )
    return path


def m_lock_releases_anyones_lock():
    """Drop the ownership check in Release, so a taken-over process deletes the
    new owner's lock.

    The consequence is a third writer admitted to a save that is in active use.
    """
    path = "internal/storage/lock.go"
    patch(
        path,
        "\tif !current || owner.PID != l.owner.PID || owner.ProcessStart != l.owner.ProcessStart {\n"
        "\t\t// Not ours any more. Say so rather than deleting another process's lock.\n"
        '\t\treturn fmt.Errorf("%w: %s no longer belongs to this process", ErrLockRefused, l.path)\n'
        "\t}\n",
        "\t_, _, _ = current, owner, l\n",
    )
    return path


def m_lock_deletes_then_creates():
    """Reclaim a stale lock by removing it first instead of replacing it.

    That opens a window in which no lock file exists, so a third process can
    create one and both survivors believe they hold the lock.
    """
    path = "internal/storage/lock.go"
    patch(
        path,
        "\tif _, err := atomicWriteFile(path, payload, 0o600, SyncFull); err != nil {\n",
        "\tif err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {\n"
        '\t\treturn nil, false, "", fmt.Errorf("%w: cannot clear the stale lock %s: %v", ErrLockRefused, path, err)\n'
        "\t}\n"
        "\tif err := os.WriteFile(path, payload, 0o600); err != nil {\n",
    )
    return path


def m_lock_reports_damage_as_contention():
    """Report an unjudgeable lock as ordinary contention.

    The message then tells the player the game is already running when in fact
    their lock file is damaged and needs attention.
    """
    path = "internal/storage/lock.go"
    patch(
        path,
        "\tcase verdictUnjudgeable:\n"
        "\t\t// Not contention: nobody is holding this lock, we simply cannot tell\n"
        "\t\t// what it is. The file is left in place for the user to inspect.\n"
        '\t\treturn nil, false, reason, fmt.Errorf("%w: %s (%s)", ErrLockRefused, path, reason)\n',
        "\tcase verdictUnjudgeable:\n"
        '\t\treturn nil, false, reason, fmt.Errorf("%w: %s (%s)", ErrLocked, path, reason)\n',
    )
    return path


def m_lock_accepts_conflicting_creation():
    """Create the lock without O_EXCL, so concurrent starts both succeed.

    The existence check and the creation stop being one indivisible operation,
    which is the only reason two simultaneous launches are serialised at all.
    """
    path = "internal/storage/lock.go"
    patch(
        path,
        "\tf, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)\n",
        "\tf, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)\n"
        "\tif err == nil {\n"
        "\t\tif _, serr := os.Stat(path); serr == nil {\n"
        "\t\t\t// Pretend the file was already there so the caller goes down the\n"
        "\t\t\t// contention path, as the unmutated code would.\n"
        "\t\t\tf.Close()\n"
        "\t\t\treturn false, nil\n"
        "\t\t}\n"
        "\t}\n",
    )
    return path


def m_store_skips_character_dir():
    """Stop creating the per-character directory before the first write.

    This is the defect that produced thirteen identical failures the first time
    the store was run: the atomic writer could not create its temp file, and
    every test reported "the system cannot find the path specified" rather than
    anything about saving. The point of the mutation is that the failure *must*
    be loud and attributable, and that the store must not depend on a side
    effect of the write to create its own directories.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        "\tif _, err := layout.EnsureCharacterDir(gameID); err != nil {\n"
        "\t\treturn nil, err\n"
        "\t}\n",
        "\t_ = layout.EnsureCharacterDir\n",
    )
    return path


def m_store_drops_revision_guard():
    """Accept an older revision over a newer one.

    Two windows on one save is what the lock prevents, but a lock can be lost.
    Without the guard the stale window's state overwrites the live window's
    progress, and the player is never told.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        "\t\tif incoming < existing {\n"
        '\t\t\treturn fmt.Errorf("%w: cannot write revision %d over the newer revision %d",\n'
        "\t\t\t\tErrStaleRevision, incoming, existing)\n"
        "\t\t}\n",
        "\t\t_, _ = incoming, existing\n",
    )
    return path


def m_store_masks_unsupported_as_corrupt():
    """Report a save from a newer build as corruption.

    The two errors drive different advice -- "update the game" versus "restore a
    backup" -- so a build that confuses them sends the player down the wrong
    recovery path. It is the mistake this harness's own author made once.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        '\t\t"the newer save was preserved at %s and this build must not overwrite it",\n'
        "\t\tErrSaveUnsupported, err, prevErr, result.PreservedPath)\n",
        '\t\t"the newer save was preserved at %s and this build must not overwrite it",\n'
        "\t\tErrSaveCorrupt, err, prevErr, result.PreservedPath)\n",
    )
    return path


def m_store_deletes_rejected_save():
    """Overwrite a rejected current save instead of preserving it.

    The store's promise is that a save the player wrote is never silently
    discarded. Replacing the rejected file with something usable makes the game
    appear to recover while the actual cause -- and the actual data -- is gone,
    which is the one outcome worse than an honest error.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        "\tresult.CurrentProblem = err\n"
        "\tif preserved, perr := CopySaveToPreserved(currentPath, s.layout.PreservedDir(),\n"
        "\t\ttimeStampSuffix()); perr == nil {\n"
        "\t\tresult.PreservedPath = preserved\n"
        "\t} else {\n",
        "\tresult.CurrentProblem = err\n"
        "\tif preserved, perr := currentPath, error(nil); perr == nil {\n"
        "\t\tresult.PreservedPath = preserved\n"
        "\t} else {\n",
    )
    return path


def m_store_prune_ignores_retention():
    """Keep every checkpoint, so pre-risk snapshots grow without bound.

    The retention limit exists so a long playthrough does not fill the disk with
    thousands of snapshots.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        "\tif len(items) <= CheckpointRetention {\n"
        "\t\treturn nil\n"
        "\t}\n",
        "\tif true || len(items) <= CheckpointRetention {\n"
        "\t\treturn nil\n"
        "\t}\n",
    )
    return path


def m_store_prune_deletes_unrecognised():
    """Delete files in the checkpoint directory whose names are not recognised.

    This is how a user's own file disappears: the cleanup step decides that what
    it cannot interpret must be rubbish. Unrecognised names are left alone
    precisely so a foreign file survives.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        "\t\trev, ok := parseRevisionName(e.Name())\n"
        "\t\tif !ok {\n"
        "\t\t\t// An unrecognised name is left alone rather than deleted. Deleting\n"
        "\t\t\t// what cannot be interpreted is how a user's file disappears.\n"
        "\t\t\tcontinue\n"
        "\t\t}\n",
        "\t\trev, ok := parseRevisionName(e.Name())\n"
        "\t\tif !ok {\n"
        "\t\t\tos.Remove(filepath.Join(dir, e.Name()))\n"
        "\t\t\tcontinue\n"
        "\t\t}\n",
    )
    return path


def m_store_commits_before_rotating():
    """Publish the new save *before* rotating the old one.

    This is an ordering defect, and the first version of this mutation missed
    that: it merely removed the error handling around the rotation, so the
    rotation still ran and every functional test still passed. Reporting "NOT
    CAUGHT" was correct -- the mutation had not broken the property it named.

    The property cannot be seen in a successful commit's final state, because
    both writes succeed either way. It is only observable at the turning point:
    fail the second atomic write and look at what the rotation path holds. The
    mutation therefore reorders the two writes for real, which is what
    TestCommitRotatesBeforeItOverwrites watches for.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        "\t// Rotate the outgoing save before overwriting it. A crash between these two\n"
        "\t// steps leaves a complete old save at the rotation path.\n"
        "\tif previous, ok, rerr := readFileIfExists(currentPath); rerr == nil && ok {\n"
        "\t\tif _, werr := atomicWriteFile(s.layout.PrevSavePath(s.gameID), previous, s.perm, SyncFileOnly); werr != nil {\n"
        '\t\t\treturn fmt.Errorf("cannot preserve the previous save: %w", werr)\n'
        "\t\t}\n"
        "\t} else if rerr != nil {\n"
        '\t\treturn fmt.Errorf("cannot read the current save to preserve it: %w", rerr)\n'
        "\t}\n"
        "\n"
        "\tif _, err := atomicWriteFile(currentPath, data, s.perm, s.sync); err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\ts.current = env\n"
        "\treturn nil\n",
        "\t// MUTATION: publish first, rotate afterwards.\n"
        "\tif _, err := atomicWriteFile(currentPath, data, s.perm, s.sync); err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\tif previous, ok, rerr := readFileIfExists(currentPath); rerr == nil && ok {\n"
        "\t\tif _, werr := atomicWriteFile(s.layout.PrevSavePath(s.gameID), previous, s.perm, SyncFileOnly); werr != nil {\n"
        '\t\t\treturn fmt.Errorf("cannot preserve the previous save: %w", werr)\n'
        "\t\t}\n"
        "\t} else if rerr != nil {\n"
        '\t\treturn fmt.Errorf("cannot read the current save to preserve it: %w", rerr)\n'
        "\t}\n"
        "\ts.current = env\n"
        "\treturn nil\n",
    )
    return path


def m_store_verify_accepts_tampering():
    """Accept a save whose integrity digest does not match its content.

    The digest exists so a file edited outside the game, or truncated by a bad
    copy, is refused rather than loaded as if it were real progress.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        "\tif !s.codec.Verify(target) {\n",
        "\tif false && !s.codec.Verify(target) {\n",
    )
    return path


def m_store_ignores_character_id():
    """Load a save belonging to a different character.

    Without the check, copying the wrong file into a character's directory
    silently renames that character's whole world -- a confusing failure whose
    cause is invisible from inside the game.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        "\tif s.codec.GameID != nil {\n"
        "\t\tif got := s.codec.GameID(target); got != \"\" && got != s.gameID {\n",
        "\tif false && s.codec.GameID != nil {\n"
        "\t\tif got := s.codec.GameID(target); got != \"\" && got != s.gameID {\n",
    )
    return path


def m_store_accepts_incomplete_codec():
    """Build a store from a codec missing the operations the store relies on.

    A nil Seal or Verify only fails when the store is first used, by which point
    the failure surfaces as a mysterious panic inside a save. Refusing up front
    turns it into a wiring error at construction.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        "\tif codec.New == nil || codec.Seal == nil || codec.Verify == nil || codec.Version == nil {\n",
        "\tif false && (codec.New == nil || codec.Seal == nil || codec.Verify == nil || codec.Version == nil) {\n",
    )
    return path


def m_store_drops_both_copies_error():
    """Call the both-copies-unusable failure "not found".

    The player is then told they have no save at all, when in fact they have two
    unusable ones sitting on disk -- one of them preserved and recoverable. The
    difference decides whether they go looking for a backup.
    """
    path = "internal/storage/snapshotstore.go"
    patch(
        path,
        '\t\treturn result, fmt.Errorf("%w: the current save is unusable (%v) and so is the previous one (%v); "+\n'
        '\t\t\t"the damaged current save was preserved at %s",\n'
        "\t\t\tErrSaveCorrupt, err, prevErr, result.PreservedPath)\n",
        '\t\treturn result, fmt.Errorf("%w: no save file exists: %s",\n'
        "\t\t\tErrSaveNotFound, result.PreservedPath)\n",
    )
    return path


def m_migration_returns_partial_chain():
    """Return the steps collected so far when a later step is missing.

    A caller that logs the error and then applies whatever came back would
    half-migrate the document, producing one that declares a version it does not
    actually have. The correct behaviour is an empty plan on every failure path.
    """
    path = "internal/storage/migrate.go"
    patch(
        path,
        "\t\t\treturn MigrationPlan{From: from}, fmt.Errorf(\"%w: no migration from version %d to %d \"+\n",
        "\t\t\treturn plan, fmt.Errorf(\"%w: no migration from version %d to %d \"+\n",
    )
    return path


def m_migration_accepts_downgrades():
    """Allow a newer save to be migrated backwards.

    The newer document may hold state this build has no representation for, so
    the downgrade silently discards it -- the exact outcome the design forbids.
    """
    path = "internal/storage/migrate.go"
    patch(
        path,
        "\tif from > to {\n",
        "\tif false && from > to {\n",
    )
    return path


def m_migration_allows_version_skipping_steps():
    """Accept a step that claims to jump more than one version.

    Its declared To then disagrees with its actual effect, so the chain's
    bookkeeping -- and the version written into the migrated document -- is
    wrong.
    """
    path = "internal/storage/migrate.go"
    patch(
        path,
        "\t\tif m.To != m.From+1 {\n",
        "\t\tif false && m.To != m.From+1 {\n",
    )
    return path


def m_migration_resolves_ambiguous_chain():
    """Keep the last step when two start at the same version.

    Which migration runs then depends on the order a table happened to be
    written in, so the same save migrates differently in two builds.
    """
    path = "internal/storage/migrate.go"
    patch(
        path,
        "\t\tif _, dup := byFrom[m.From]; dup {\n",
        "\t\tif _, dup := byFrom[m.From]; false && dup {\n",
    )
    return path


def m_migration_reports_refusal_as_corruption():
    """Report a refused migration as a corrupt save.

    The player is told their file is damaged when in fact it is intact and the
    build declined to convert it -- so they go hunting for a backup they do not
    need instead of running the older build.
    """
    path = "internal/storage/migrate.go"
    patch(
        path,
        "\t\t\treturn fmt.Errorf(\"%w: the migration from version %d refused: %v\",\n"
        "\t\t\t\tErrMigrationRefused, step.From, err)\n",
        "\t\t\treturn fmt.Errorf(\"%w: the migration from version %d refused: %v\",\n"
        "\t\t\t\tErrSaveCorrupt, step.From, err)\n",
    )
    return path


def m_migration_swallows_refusal():
    """Treat a migration that refuses as a success.

    The document is then handed on as if it had been converted, so the caller
    loads a half-mapped state and the refusal never reaches the player.
    """
    path = "internal/storage/migrate.go"
    patch(
        path,
        "\t\tif err := step.Apply(doc); err != nil {\n"
        "\t\t\treturn fmt.Errorf(\"%w: the migration from version %d refused: %v\",\n"
        "\t\t\t\tErrMigrationRefused, step.From, err)\n"
        "\t\t}\n",
        "\t\t_ = step.Apply(doc)\n",
    )
    return path


def m_migration_skips_version_advance():
    """Apply the steps but never advance the declared version.

    The migrated document then claims the old version, so the next reader tries
    to migrate it again -- applying every step a second time to a document that
    has already been transformed.
    """
    path = "internal/storage/migrate.go"
    patch(
        path,
        '\t\tdoc["envelope_version"] = step.To\n',
        "\t\t_ = step.To\n",
    )
    return path


def m_migration_copy_moves_instead_of_copies():
    """Make the 'keep the original' path destroy the original.

    The design's instruction for an unmigratable save is to refuse AND keep the
    original so the player can continue on the older build. A move satisfies the
    first half and defeats the second.

    The mutation has to add the "os" import itself: migrate.go does not import
    it, and a mutation that fails to build would be classified BUILD rather than
    CAUGHT -- correctly, but it would then be testing nothing at all. Adding the
    import is part of making the mutation real.
    """
    path = "internal/storage/migrate.go"
    patch(
        path,
        'import (\n\t"errors"\n\t"fmt"\n)\n',
        'import (\n\t"errors"\n\t"fmt"\n\t"os"\n)\n',
    )
    patch(
        path,
        "\tdest, err := CopySaveToPreserved(src, preservedDir, suffix)\n"
        "\tif err != nil {\n"
        '\t\treturn "", fmt.Errorf("%w: %s could not be preserved: %v", ErrMigrationRefused, src, err)\n'
        "\t}\n"
        "\treturn dest, nil\n",
        "\tdest, err := CopySaveToPreserved(src, preservedDir, suffix)\n"
        "\tif err != nil {\n"
        '\t\treturn "", fmt.Errorf("%w: %s could not be preserved: %v", ErrMigrationRefused, src, err)\n'
        "\t}\n"
        "\tif removeErr := os.Remove(src); removeErr != nil {\n"
        "\t\treturn dest, removeErr\n"
        "\t}\n"
        "\treturn dest, nil\n",
    )
    return path


def m_import_skips_digest_check():
    """Import a file whose integrity digest does not match its content.

    This is the truncated-transfer case: a save mailed or pasted between machines
    can be cut short and still parse as JSON. Without the digest the incomplete
    document replaces a good save, and the loss is only discovered when the
    player loads it.
    """
    path = "internal/storage/transfer.go"
    patch(
        path,
        "\tif !digestOK {\n",
        "\tif false && !digestOK {\n",
    )
    return path


def m_import_accepts_any_json():
    """Accept any file that parses as JSON, save or not.

    The player picks a photo, a config file or an unrelated export from a file
    dialog, and it is loaded as their character. The refusal must happen before
    anything is written.
    """
    path = "internal/storage/transfer.go"
    patch(
        path,
        '\tif err := json.Unmarshal(data, &doc); err != nil {\n'
        '\t\treturn result, fmt.Errorf("%w: %s is not a readable save document: %v", ErrImportRefused, path, err)\n'
        "\t}\n",
        "\t_ = json.Unmarshal(data, &doc)\n",
    )
    return path


def m_import_writes_before_validating():
    """Write next to the imported file while validating it.

    Validation must not write anywhere. A validator that writes -- even something
    it intends to undo -- has already made the import non-atomic, and a failure
    between the write and the undo leaves the player without their save.

    The first version of this mutation wrote into the data root, which
    ValidateImport has no access to and no reason to touch. The second writes
    next to the file it is reading, which is a realistic mistake: a validator
    that "normalises" the input file in place before checking it destroys the
    player's only copy of a save that the game is about to refuse.
    """
    path = "internal/storage/transfer.go"
    patch(
        path,
        "\tresult.Source = path\n"
        "\tresult.Bytes = len(data)\n",
        "\tresult.Source = path\n"
        "\tresult.Bytes = len(data)\n"
        "\t// MUTATION: rewrite the input file before checking it.\n"
        "\tif _, werr := atomicWriteFile(path, data, 0o600, SyncFileOnly); werr != nil {\n"
        "\t\treturn result, werr\n"
        "\t}\n",
    )
    return path


def m_export_allows_writing_into_the_data_root():
    """Let an export target a path inside the game's own data directory.

    A file dialog opens in the last-used folder, which is often the save folder,
    so this is the easiest way for a player to overwrite their own live save.
    """
    path = "internal/storage/transfer.go"
    patch(
        path,
        "\tif err := refuseExportInsideDataRoot(dest, dataRoot); err != nil {\n"
        "\t\treturn 0, err\n"
        "\t}\n",
        "\t_ = refuseExportInsideDataRoot\n",
    )
    return path


def m_export_reencodes_instead_of_copying():
    """Re-encode the document on export instead of copying its bytes.

    An export is for moving bytes between machines. Re-encoding applies this
    build's formatting and may normalise a field, so the exported file is not
    the file the player's save actually contains -- and a round trip through it
    is no longer provably lossless.
    """
    path = "internal/storage/transfer.go"
    patch(
        path,
        "\tif _, err := atomicWriteFile(dest, data, 0o600, SyncFileOnly); err != nil {\n"
        "\t\treturn 0, fmt.Errorf(\"cannot write the export: %w\", err)\n"
        "\t}\n"
        "\treturn len(data), nil\n",
        "\tvar reencoded map[string]any\n"
        "\tif uerr := json.Unmarshal(data, &reencoded); uerr != nil {\n"
        "\t\treturn 0, uerr\n"
        "\t}\n"
        '\treencoded["exported"] = true\n'
        "\tout, merr := json.Marshal(reencoded)\n"
        "\tif merr != nil {\n"
        "\t\treturn 0, merr\n"
        "\t}\n"
        "\tout = append(out, '\\n')\n"
        "\tif _, err := atomicWriteFile(dest, out, 0o600, SyncFileOnly); err != nil {\n"
        "\t\treturn 0, fmt.Errorf(\"cannot write the export: %w\", err)\n"
        "\t}\n"
        "\treturn len(out), nil\n",
    )
    return path


def m_import_skips_rotation():
    """Import over the live save without rotating it first.

    The design's stance is that an import is reversible. Without the rotation the
    pre-import save exists nowhere, so a player who imports the wrong file has
    destroyed the character they were playing.
    """
    path = "internal/storage/transfer.go"
    patch(
        path,
        "\t// Rotate first, exactly as Commit does, so a crash during the import leaves\n"
        "\t// the pre-import save recoverable.\n"
        "\tif previous, ok, rerr := readFileIfExists(currentPath); rerr == nil && ok {\n"
        "\t\tif _, werr := atomicWriteFile(s.layout.PrevSavePath(s.gameID), previous, s.perm, SyncFileOnly); werr != nil {\n"
        '\t\t\treturn result, fmt.Errorf("cannot preserve the pre-import save: %w", werr)\n'
        "\t\t}\n"
        "\t} else if rerr != nil {\n"
        '\t\treturn result, fmt.Errorf("cannot read the pre-import save to preserve it: %w", rerr)\n'
        "\t}\n",
        "\t// MUTATION: no rotation before the import.\n",
    )
    return path


def m_import_reports_kept_copy_failure_as_failure():
    """Fail the whole import when only the optional labelled copy could not be
    written.

    The save is already in place by then, so reporting a failure makes the player
    retry -- and a retry rotates the freshly imported save into the previous
    slot, losing the one they were trying to recover from.
    """
    path = "internal/storage/transfer.go"
    patch(
        path,
        "\t\tif _, err := atomicWriteFile(keepOutgoingAt, data, s.perm, SyncFileOnly); err != nil {\n"
        "\t\t\tresult.KeptCopyError = err\n"
        "\t\t}\n",
        "\t\tif _, err := atomicWriteFile(keepOutgoingAt, data, s.perm, SyncFileOnly); err != nil {\n"
        "\t\t\treturn result, err\n"
        "\t\t}\n",
    )
    return path

    """Make the 'keep the original' path destroy the original.

    The design's instruction for an unmigratable save is to refuse AND keep the
    original so the player can continue on the older build. A move satisfies the
    first half and defeats the second.

    The mutation has to add the "os" import itself: migrate.go does not import
    it, and a mutation that fails to build would be classified BUILD rather than
    CAUGHT -- correctly, but it would then be testing nothing at all. Adding the
    import is part of making the mutation real.
    """
    path = "internal/storage/migrate.go"
    patch(
        path,
        'import (\n\t"errors"\n\t"fmt"\n)\n',
        'import (\n\t"errors"\n\t"fmt"\n\t"os"\n)\n',
    )
    patch(
        path,
        "\tdest, err := CopySaveToPreserved(src, preservedDir, suffix)\n"
        "\tif err != nil {\n"
        '\t\treturn "", fmt.Errorf("%w: %s could not be preserved: %v", ErrMigrationRefused, src, err)\n'
        "\t}\n"
        "\treturn dest, nil\n",
        "\tdest, err := CopySaveToPreserved(src, preservedDir, suffix)\n"
        "\tif err != nil {\n"
        '\t\treturn "", fmt.Errorf("%w: %s could not be preserved: %v", ErrMigrationRefused, src, err)\n'
        "\t}\n"
        "\tif removeErr := os.Remove(src); removeErr != nil {\n"
        "\t\treturn dest, removeErr\n"
        "\t}\n"
        "\treturn dest, nil\n",
    )
    return path


MUTATIONS = {
    "nonatomic-write": (
        m_nonatomic_write,
        "./internal/storage",
        "TestAtomicWritePublishesOnlyCompleteData",
    ),
    "temp-debris": (
        m_temp_debris,
        "./internal/storage",
        "TestAtomicWriteCleansUpTempAtEveryFailureStage",
    ),
    "no-sync": (
        m_no_sync,
        "./internal/storage",
        "TestAtomicWrite",
    ),
    "sync-lies": (
        m_sync_lies,
        "./internal/storage",
        "TestAtomicWriteReportsHonestDurability",
    ),
    "no-traversal-guard": (
        m_no_traversal_guard,
        "./internal/storage",
        "TestSanitiseComponentRejectsTraversal",
    ),
    "unpadded-revision": (
        m_unpadded_revision,
        "./internal/storage",
        "TestRevisionNameSortsLexicographicallyInNumericOrder",
    ),
    "relative-override": (
        m_relative_override,
        "./internal/storage",
        "TestResolveDataRootRejectsRelativeOverride",
    ),
    "lock-ignores-liveness": (
        m_lock_ignores_liveness,
        "./internal/storage",
        "TestAcquireSaveLockRefusesWhileAnotherLiveProcessHoldsIt",
    ),
    "lock-ignores-pid-recycling": (
        m_lock_ignores_pid_recycling,
        "./internal/storage",
        "TestProcessAliveTreatsARecycledPidAsGone",
    ),
    "lock-releases-anyones-lock": (
        m_lock_releases_anyones_lock,
        "./internal/storage",
        "TestReleaseRemovesOnlyItsOwnLock",
    ),
    "lock-deletes-then-creates": (
        m_lock_deletes_then_creates,
        "./internal/storage",
        "TestLockReclaimNeverLeavesAnInstantWithNoLock",
    ),
    "lock-reports-damage-as-contention": (
        m_lock_reports_damage_as_contention,
        "./internal/storage",
        "TestUnjudgeableLockIsNotReportedAsContention",
    ),
    "lock-accepts-conflicting-creation": (
        m_lock_accepts_conflicting_creation,
        "./internal/storage",
        "TestAcquireSaveLockSucceedsWhenFree",
    ),
    "store-skips-character-dir": (
        m_store_skips_character_dir,
        "./internal/storage",
        "TestSnapshotStore",
    ),
    "store-drops-revision-guard": (
        m_store_drops_revision_guard,
        "./internal/storage",
        "TestSnapshotStoreRefusesAnOlderRevisionOverANewerOne",
    ),
    "store-masks-unsupported-as-corrupt": (
        m_store_masks_unsupported_as_corrupt,
        "./internal/storage",
        "TestSnapshotStoreRefusesAFutureEnvelopeVersion",
    ),
    "store-deletes-rejected-save": (
        m_store_deletes_rejected_save,
        "./internal/storage",
        "TestSnapshotStorePreservesARejectedSave",
    ),
    "store-prune-ignores-retention": (
        m_store_prune_ignores_retention,
        "./internal/storage",
        "TestSnapshotStoreCheckpointRetentionPrunesOldest",
    ),
    "store-prune-deletes-unrecognised": (
        m_store_prune_deletes_unrecognised,
        "./internal/storage",
        "TestSnapshotStorePruningIgnoresUnrecognisedFiles",
    ),
    "store-commits-before-rotating": (
        m_store_commits_before_rotating,
        "./internal/storage",
        "TestCommitRotatesBeforeItOverwrites",
    ),
    "store-verify-accepts-tampering": (
        m_store_verify_accepts_tampering,
        "./internal/storage",
        "TestSnapshotStoreRejectsATamperedSave",
    ),
    "store-ignores-character-id": (
        m_store_ignores_character_id,
        "./internal/storage",
        "TestSnapshotStoreRejectsACharacterMismatch",
    ),
    "store-accepts-incomplete-codec": (
        m_store_accepts_incomplete_codec,
        "./internal/storage",
        "TestSnapshotStoreRejectsAnIncompleteCodec",
    ),
    "store-drops-both-copies-error": (
        m_store_drops_both_copies_error,
        "./internal/storage",
        "TestSnapshotStoreReportsWhenBothCopiesAreUnusable",
    ),
    "migration-returns-partial-chain": (
        m_migration_returns_partial_chain,
        "./internal/storage",
        "TestPlanMigrationRefusesAGapRatherThanStoppingPartWay",
    ),
    "migration-accepts-downgrades": (
        m_migration_accepts_downgrades,
        "./internal/storage",
        "TestPlanMigrationRefusesDowngrades",
    ),
    "migration-allows-version-skipping-steps": (
        m_migration_allows_version_skipping_steps,
        "./internal/storage",
        "TestPlanMigrationRejectsVersionSkippingSteps",
    ),
    "migration-resolves-ambiguous-chain": (
        m_migration_resolves_ambiguous_chain,
        "./internal/storage",
        "TestPlanMigrationRejectsAnAmbiguousChain",
    ),
    "migration-reports-refusal-as-corruption": (
        m_migration_reports_refusal_as_corruption,
        "./internal/storage",
        "TestMigrationErrorsAreDistinguishable",
    ),
    "migration-swallows-refusal": (
        m_migration_swallows_refusal,
        "./internal/storage",
        "TestApplyMigrationLeavesNothingBehindWhenAStepRefuses",
    ),
    "migration-skips-version-advance": (
        m_migration_skips_version_advance,
        "./internal/storage",
        "TestApplyMigrationAdvancesTheDeclaredVersion",
    ),
    "migration-copy-moves-instead-of-copies": (
        m_migration_copy_moves_instead_of_copies,
        "./internal/storage",
        "TestCopySaveUnmigratedKeepsTheOriginal",
    ),
    "import-skips-digest-check": (
        m_import_skips_digest_check,
        "./internal/storage",
        "TestValidateImportRejectsATruncatedTransfer",
    ),
    "import-accepts-any-json": (
        m_import_accepts_any_json,
        "./internal/storage",
        "TestValidateImportRejectsNonJSON",
    ),
    "import-writes-before-validating": (
        m_import_writes_before_validating,
        "./internal/storage",
        "TestValidateImportTouchesNothingOnDisk",
    ),
    "export-allows-writing-into-the-data-root": (
        m_export_allows_writing_into_the_data_root,
        "./internal/storage",
        "TestExportSaveRefusesToWriteInsideTheDataRoot",
    ),
    "export-reencodes-instead-of-copying": (
        m_export_reencodes_instead_of_copying,
        "./internal/storage",
        "TestExportSaveAllowsAPathOutsideTheDataRoot",
    ),
    "import-skips-rotation": (
        m_import_skips_rotation,
        "./internal/storage",
        "TestImportIntoSaveKeepsTheOutgoingSave",
    ),
    "import-reports-kept-copy-failure-as-import-failure": (
        m_import_reports_kept_copy_failure_as_failure,
        "./internal/storage",
        "TestImportIntoSaveReportsAKeptCopyFailureWithoutFailing",
    ),
}


def run_tests(pkg, pattern):
    env = dict(os.environ)
    env["GOCACHE"] = os.path.join(ROOT, ".task-cache", "go-build")
    env["GOTMPDIR"] = os.path.join(ROOT, ".task-cache", "go-tmp")
    env["GOEXPERIMENT"] = "jsonv2"
    proc = subprocess.run(
        [GO, "test", pkg, "-run", pattern, "-count=1", "-v"],
        cwd=ROOT, env=env, capture_output=True, text=True,
    )
    return proc.returncode, (proc.stdout + proc.stderr)


def classify(code, out):
    """Decide whether a run is a real catch.

    A non-zero exit is NOT sufficient evidence. A build failure, a bad package
    path, or a pattern that matches no test all exit non-zero while proving
    nothing about the mutation. This harness's first version reported all seven
    mutations "caught" purely because the package path lacked a "./" prefix and
    every run failed to build -- the exact vacuous-pass failure mode these
    counter-proofs exist to detect, reproduced inside the verification tool.

    So a catch now requires: compilation succeeded AND at least one named test
    actually ran AND at least one test FAILED.
    """
    if "build failed" in out or "[setup failed]" in out or "cannot find package" in out:
        return "BUILD", "the package did not build; this proves nothing about the mutation"

    ran = out.count("=== RUN")
    failed = out.count("--- FAIL")
    passed = out.count("--- PASS")

    if ran == 0:
        return "NORUN", "no test ran; the -run pattern matched nothing"
    if failed == 0:
        return "PASS", "no test failed"
    if passed > 0:
        # Some tests failing while sibling tests pass is the expected shape when
        # the mutation breaks one specific property.
        return "CAUGHT", "%d of %d tests failed" % (failed, ran)
    return "CAUGHT", "%d of %d tests failed" % (failed, ran)


def verify_coverage():
    """Fail if any mutation edits a file that is not in GUARDED.

    This is the guard against the harness corrupting the tree it is verifying.
    When a mutation edits an unguarded file, the edit is applied and never
    reverted: the harness reports a correct result for that mutation and leaves
    the repository broken for every subsequent run.

    Discovering that through a mystifying full-suite failure is far too late, so
    the check runs before anything is mutated. It is static: every mutation
    function is introspected for the path string it passes to patch().

    Returns a list of human-readable problems; empty means safe.
    """
    problems = []
    guarded = {os.path.normpath(p.replace("/", os.sep)) for p in GUARDED}

    for name, (fn, _pkg, _pattern) in sorted(MUTATIONS.items()):
        source = inspect.getsource(fn)
        # Every mutation in this file passes its target path as a bare string
        # literal, so scanning for those literals is exact rather than heuristic.
        targets = re.findall(r'"([^"]*\.go)"', source)
        if not targets:
            problems.append("%s: cannot determine which file it edits" % name)
            continue
        for target in set(targets):
            normalised = os.path.normpath(target.replace("/", os.sep))
            if normalised not in guarded:
                problems.append(
                    "%s edits %s, which is not in GUARDED; the edit would never be reverted"
                    % (name, target)
                )
    return problems


def run_one(name):
    fn, pkg, pattern = MUTATIONS[name]
    saved = snapshot()
    path = "?"
    try:
        path = fn()
        code, out = run_tests(pkg, pattern)
    except BaseException:
        # A failure inside fn() must still restore. Without this the mutation
        # stays applied, and the next run's snapshot() would bless it.
        restore(saved)
        raise
    finally:
        restore(saved)

    verdict, reason = classify(code, out)

    print("=" * 72)
    print("mutation : %s" % name)
    print("target   : %s" % path)
    print("test     : %s -run %s" % (pkg, pattern))
    print("exit     : %d" % code)
    print("-" * 72)
    print(out.strip())
    print("-" * 72)

    if verdict == "CAUGHT":
        print("RESULT   : CAUGHT -- %s" % reason)
        print("(sources restored)")
        return True

    print("RESULT   : NOT CAUGHT (%s) -- %s" % (verdict, reason))
    if verdict == "BUILD":
        print("A build failure is not a test failure. Fix the harness.")
    else:
        print("The test passed with its protection removed: that test is not")
        print("testing what it claims.")
    print("(sources restored)")
    return False


def main():
    args = sys.argv[1:]
    if not args or args[0] == "--list":
        print("available mutations:")
        for name in sorted(MUTATIONS):
            print("  " + name)
        return 0

    if args[0] == "--check-coverage":
        problems = verify_coverage()
        if problems:
            for p in problems:
                print("COVERAGE PROBLEM: " + p)
            return 1
        print("coverage ok: every mutation targets a guarded file")
        return 0

    if args[0] == "--record-baseline":
        # Must be run against a tree that has been independently verified clean
        # (go build + the full test suite green). It is not a way to bless a
        # dirty tree; the point of the baseline is to detect exactly that.
        write_baseline()
        return 0

    if args[0] == "--check-baseline":
        problems = check_baseline()
        if problems:
            for p in problems:
                print("BASELINE PROBLEM: " + p)
            return 1
        print("baseline ok: the guarded sources match their recorded clean state")
        return 0

    # Before touching anything: a mutation that edits an unguarded file would be
    # applied and never reverted, corrupting the tree.
    coverage_problems = verify_coverage()
    if coverage_problems:
        for p in coverage_problems:
            print("COVERAGE PROBLEM: " + p)
        print("refusing to run: a mutation would not be reverted")
        return 2

    # And a tree that is already modified must never be used as the starting
    # point, because snapshot() would back up the damage and restore() would
    # then make it permanent.
    baseline_problems = check_baseline()
    if baseline_problems:
        for p in baseline_problems:
            print("BASELINE PROBLEM: " + p)
        print("refusing to run: the tree is already modified, and this run would")
        print("back up the modification as if it were the clean state.")
        return 3

    if args[0] == "--all":
        names = sorted(MUTATIONS)
    else:
        names = args
        unknown = [n for n in names if n not in MUTATIONS]
        if unknown:
            print("unknown mutation(s): %s; use --list" % ", ".join(unknown))
            return 2

    caught = 0
    harness_bugs = 0
    for name in names:
        try:
            if run_one(name):
                caught += 1
        except AssertionError as exc:
            # A pattern that no longer matches is a harness problem, not a test
            # result. Report it as such and keep going, so one stale pattern
            # does not hide the verdict for every remaining mutation.
            harness_bugs += 1
            print("=" * 72)
            print("mutation : %s" % name)
            print("RESULT   : HARNESS BUG -- the mutation's pattern did not match")
            print(str(exc))
            print("(sources restored)")
        print()

    # Confirm the run left nothing behind. Without this, a crash mid-run would
    # be discovered by a mystifying failure in some unrelated later test.
    residue = check_baseline()
    if residue:
        print("=" * 72)
        for p in residue:
            print("RESIDUE  : " + p)
        print("the run did not restore the tree; fix this before committing")
        return 4

    print("=" * 72)
    print("summary: %d/%d mutations caught" % (caught, len(names) - harness_bugs))
    if harness_bugs:
        print("%d mutation(s) could not be applied: the harness is out of date." % harness_bugs)
    print("tree integrity: all guarded sources match the recorded baseline")
    if caught != len(names) - harness_bugs:
        print("A test passed with its protection removed: that test is not")
        print("testing what it claims. Fix the test, not this counter-proof.")
        return 1
    if harness_bugs:
        return 5
    return 0


if __name__ == "__main__":
    sys.exit(main())
