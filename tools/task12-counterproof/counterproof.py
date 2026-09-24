#!/usr/bin/env python3
"""Counter-proof harness for TASK-12 (injury, lifespan, healing and karma).

Every mutation reverts exactly one protection so the corresponding test must
fail. A suite that still passes with the protection removed is not testing that
protection.

The mechanics are deliberately identical to the task06-, task07-, task10- and
task11-counterproof harnesses: line endings are DETECTED rather than assumed,
every substitution asserts it matched exactly once, a recorded baseline detects
contamination, and a catch requires that the package BUILT and that at least one
named test actually FAILED.

Usage:
    python3 tools/task12-counterproof/counterproof.py --list
    python3 tools/task12-counterproof/counterproof.py --record-baseline
    python3 tools/task12-counterproof/counterproof.py --check-baseline
    python3 tools/task12-counterproof/counterproof.py --check-coverage
    python3 tools/task12-counterproof/counterproof.py <mutation-name>
    python3 tools/task12-counterproof/counterproof.py --all
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
BACKUP_DIR = os.path.join(ROOT, ".task-cache", "counterproof-task12")
BASELINE = os.path.join(BACKUP_DIR, "baseline.sha256")

# Files any mutation may edit. This list is COMPLETE rather than a sample,
# because a mutation against an unlisted file is applied and never reverted.
GUARDED = (
    "internal/content/cast.go",
    "internal/content/catalogue.go",
    "internal/engine/creation_factory.go",
    "internal/engine/heal.go",
    "internal/engine/injury.go",
    "internal/engine/karma.go",
    "internal/engine/pipeline.go",
)


def read(path):
    with io.open(os.path.join(ROOT, path), encoding="utf-8", newline="") as fh:
        return fh.read()


def write(path, text):
    with io.open(os.path.join(ROOT, path), "w", encoding="utf-8", newline="") as fh:
        fh.write(text)


def digest(path):
    with open(os.path.join(ROOT, path), "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


def fingerprint():
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
    recorded = read_baseline()
    if recorded is None:
        return ["no baseline recorded; run --record-baseline against a verified-clean tree"]

    current = fingerprint()
    problems = []
    for path in sorted(set(recorded) | set(current)):
        was, now = recorded.get(path), current.get(path)
        if was is None:
            problems.append("%s is guarded but not in the baseline" % path)
        elif now is None:
            problems.append("%s is in the baseline but is gone" % path)
        elif was != now:
            problems.append(
                "%s does not match the recorded clean state; a previous run left it "
                "modified (expected %s, found %s)" % (path, was[:12], now[:12])
            )
    return problems


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


def restore(saved):
    for path, bkp in saved.items():
        with io.open(bkp, encoding="utf-8", newline="") as fh:
            write(path, fh.read())


def patch(path, old, new):
    """Replace old with new, requiring exactly one match.

    `old` is written with plain \\n and rewritten to the file's detected
    convention, so a CRLF file matches. A zero-match or multi-match is a refusal
    rather than a silent no-op.
    """
    text = read(path)
    nl = "\r\n" if "\r\n" in text else "\n"
    old_n = old.replace("\n", nl)
    new_n = new.replace("\n", nl)

    found = text.count(old_n)
    if found != 1:
        raise AssertionError(
            "pattern matched %d times in %s, want 1; refusing a silent no-op.\n"
            "pattern:\n%s" % (found, path, old_n[:400])
        )
    write(path, text.replace(old_n, new_n))


# --- Mutations ---------------------------------------------------------------


def m_band_threshold_truncated():
    """Go back to precomputing the thresholds with a truncating division.

    This is the defect the exhaustive band test actually caught: with a ceiling
    of 99 the truncated threshold is 32, so 32 health — 32.3%, inside (0,33%) —
    was classified as 重伤 instead of 垂死.
    """
    path = "internal/engine/injury.go"
    patch(
        path,
        "\tswitch {\n"
        "\tcase hp*100 < max*33:\n\t\treturn BandDying\n"
        "\tcase hp*100 < max*66:\n\t\treturn BandHeavy\n",
        "\theavyThreshold := max * 33 / 100\n"
        "\tlightThreshold := max * 66 / 100\n"
        "\tswitch {\n"
        "\tcase hp < heavyThreshold:\n\t\treturn BandDying\n"
        "\tcase hp < lightThreshold:\n\t\treturn BandHeavy\n",
    )
    return path


def m_band_boundary_exclusive():
    """Make the 33% boundary exclusive, so exactly 33% is 垂死.

    Design 6.2 writes the band as [33%,66%), so the threshold belongs to the
    more severe band.
    """
    path = "internal/engine/injury.go"
    patch(
        path,
        "\tcase hp*100 < max*33:\n",
        "\tcase hp*100 <= max*33:\n",
    )
    return path


def m_band_penalty_accumulated():
    """Subtract the penalty from a running total instead of deriving it.

    With this reverted the penalty is applied once per recomputation, so a
    character who is looked at twice is twice as slow.
    """
    path = "internal/engine/injury.go"
    patch(
        path,
        "\tbase := int64(p.Attributes.Agility) * SCALE\n"
        "\tp.Derived.Speed = applyPermillePenalty(base, InjuryPenaltyPermille(p, cat))\n",
        "\tpenalty := InjuryPenaltyPermille(p, cat)\n"
        "\tif penalty > 0 && p.Derived.Speed > 0 {\n"
        "\t\tp.Derived.Speed -= p.Derived.Speed * int64(penalty) / PermilleScale\n"
        "\t}\n",
    )
    return path


def m_band_penalty_not_applied():
    """Stop applying the band penalty at all."""
    path = "internal/engine/injury.go"
    patch(
        path,
        "\tbase := int64(p.Attributes.Agility) * SCALE\n"
        "\tp.Derived.Speed = applyPermillePenalty(base, InjuryPenaltyPermille(p, cat))\n",
        "\tp.Derived.Speed = int64(p.Attributes.Agility) * SCALE\n",
    )
    return path


def m_rest_clears_everything():
    """Make a month of rest clear every affliction, not only the configured ones.

    This is the "疗伤不清除无关异常" criterion reverted: a poison would be cured by
    a nap, and the antidote TASK-13 ships would have no reason to exist.
    """
    path = "internal/engine/heal.go"
    patch(
        path,
        "\t\tif affliction != nil && affliction.ClearedByRest {\n",
        "\t\tif affliction != nil {\n",
    )
    return path


def m_rest_restores_nothing():
    """Stop restoring health."""
    path = "internal/engine/heal.go"
    patch(
        path,
        "\tbefore := p.HP.Current\n"
        "\trestore := p.HP.Max * int64(restorePermille) / PermilleScale\n",
        "\tbefore := p.HP.Current\n"
        "\trestore := int64(0)\n"
        "\t_ = restorePermille\n",
    )
    return path


def m_conversion_can_kill():
    """Let the conversion burn the last of the character's health."""
    path = "internal/engine/heal.go"
    patch(
        path,
        "\tif p.HP.Current <= burn {\n",
        "\tif false && p.HP.Current <= burn {\n",
    )
    return path


def m_conversion_gains_nothing():
    """Stop granting 灵力 for the health spent."""
    path = "internal/engine/heal.go"
    patch(
        path,
        "\tgained := burn\n",
        "\tgained := int64(0)\n\t_ = burn\n",
    )
    return path


def m_afflictions_do_not_drain():
    """Stop charging the month-end cost of an affliction."""
    path = "internal/engine/heal.go"
    patch(
        path,
        "\tif hpDrain > 0 {\n",
        "\tif false && hpDrain > 0 {\n",
    )
    return path


def m_afflictions_drain_on_expiry():
    """Drain before the durations tick, so an affliction charges its last month."""
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\ttickTimedEffects(s)\n"
        "\n"
        "\t// 4c-bis. The month-end cost of carrying an affliction. It runs after the\n"
        "\t// durations have ticked, so an affliction that ends this month does not\n"
        "\t// also drain this month, and before the death checks, so a poison that\n"
        "\t// finishes a character is reported as the poison rather than as old age.\n"
        "\te.settleAfflictions(s, &result)\n",
        "\te.settleAfflictions(s, &result)\n\ttickTimedEffects(s)\n",
    )
    return path


def m_poison_death_not_reported():
    """Stop ending a character whose health was drained to zero."""
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\tif cause, dead := afflictionEndCause(s.Player, s.Counters.WorldMonth); dead {\n",
        "\tif cause, dead := afflictionEndCause(s.Player, s.Counters.WorldMonth); false && dead {\n",
    )
    return path


def m_lifespan_guard_removed():
    """Let a character with no lifespan left start another month."""
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\tif !hasLifespanLeft(e.state) {\n",
        "\tif false && !hasLifespanLeft(e.state) {\n",
    )
    return path


def m_travel_ages_the_character():
    """Make travel advance the world month.

    Design 13.1 gives travel a zero-month cost; with this reverted, moving
    between fixed nodes ages the character, which is exactly the "现实经过时间不
    改年龄" invariant read the other way round.
    """
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\ts.World.CurrentLocation = to\n",
        "\tAdvanceWorldMonth(s)\n\ts.World.CurrentLocation = to\n",
    )
    return path


def m_karma_granted_automatically():
    """Grant karma for a settled month.

    Design R19 forbids a blanket rule: "因果按行为和情境，不对所有击杀一刀切". With
    this reverted the engine has a karma rule of its own, and the ledger can no
    longer attribute every change to a named act.
    """
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\tAdvanceWorldMonth(s)\n",
        "\tAdvanceWorldMonth(s)\n"
        "\tif s.Player != nil {\n"
        "\t\tif s.Player.Resources == nil {\n"
        "\t\t\ts.Player.Resources = map[Resource]int64{}\n"
        "\t\t}\n"
        "\t\ts.Player.Resources[ResKarma]++\n"
        "\t}\n",
    )
    return path


def m_karma_tier_lowest_wins():
    """Let the lowest matching tier win instead of the highest."""
    path = "internal/engine/karma.go"
    patch(
        path,
        "\t\tif best == nil || tier.MinKarma.Value > best.MinKarma.Value {\n",
        "\t\tif best == nil {\n",
    )
    return path


def m_remaining_lifespan_negative():
    """Report a negative remaining lifespan."""
    path = "internal/engine/karma.go"
    patch(
        path,
        "\tif d.LifespanRemainingMonths < 0 {\n",
        "\tif false && d.LifespanRemainingMonths < 0 {\n",
    )
    return path


def m_creation_attack_grant_dropped():
    """Drop creation grants to attack again.

    This is the silent drop TASK-12 found: the target is inside the whitelist, so
    validation accepted it, and the accumulator ignored it, so a talent that
    granted attack did nothing.
    """
    path = "internal/engine/creation_factory.go"
    patch(
        path,
        "\tcase e.Target == targetAttack && e.Kind == GrantAdditive:\n"
        "\t\tg.attackAdd += e.Amount\n",
        "",
    )
    return path


def m_shipped_poison_cured_by_rest():
    """Ship a poison that resting cures."""
    path = "internal/content/cast.go"
    patch(
        path,
        "\t\t\tID: \"poison_mild\", NameZH: \"微毒\", Kind: engine.AfflictionPoison,\n"
        "\t\t\tDurationMonths:  designNote(3, \"设计注：微毒约三月\"),\n"
        "\t\t\tHPDrainPerMonth: designNote(engine.SCALE, \"设计注：微毒每月损耗一点气血\"),\n"
        "\t\t\tClearedByRest:   false,\n",
        "\t\t\tID: \"poison_mild\", NameZH: \"微毒\", Kind: engine.AfflictionPoison,\n"
        "\t\t\tDurationMonths:  designNote(3, \"设计注：微毒约三月\"),\n"
        "\t\t\tHPDrainPerMonth: designNote(engine.SCALE, \"设计注：微毒每月损耗一点气血\"),\n"
        "\t\t\tClearedByRest:   true,\n",
    )
    return path


MUTATIONS = {
    # name: (function, package, -run pattern)
    "band-threshold-truncated": (
        m_band_threshold_truncated, "./internal/engine/",
        "TestInjuryBandsPartitionTheHealthRange",
    ),
    "band-boundary-exclusive": (
        m_band_boundary_exclusive, "./internal/engine/",
        "TestInjuryBandsPartitionTheHealthRange|TestInjuryBandBoundariesAreInclusiveOnTheLowerSide",
    ),
    "band-penalty-accumulated": (
        m_band_penalty_accumulated, "./internal/engine/",
        "TestHeavyWoundSpeedPenaltyIsNotRepeated",
    ),
    "band-penalty-not-applied": (
        m_band_penalty_not_applied, "./internal/engine/",
        "TestHeavyWoundSpeedPenaltyIsNotRepeated",
    ),
    "rest-clears-everything": (
        m_rest_clears_everything, "./internal/engine/",
        "TestRestDoesNotClearUnrelatedAfflictions",
    ),
    "rest-restores-nothing": (
        m_rest_restores_nothing, "./internal/engine/",
        "TestRestRestoresHealthUpToTheCeiling",
    ),
    "conversion-can-kill": (
        m_conversion_can_kill, "./internal/engine/",
        "TestConversionRefusesToLeaveNoHealth",
    ),
    "conversion-gains-nothing": (
        m_conversion_gains_nothing, "./internal/engine/",
        "TestConversionBurnsHealthForSpirit",
    ),
    "afflictions-do-not-drain": (
        m_afflictions_do_not_drain, "./internal/engine/",
        "TestAfflictionsDrainHealthAtMonthEnd",
    ),
    "afflictions-drain-on-expiry": (
        m_afflictions_drain_on_expiry, "./internal/engine/",
        "TestAnAfflictionThatEndsThisMonthDoesNotAlsoDrain",
    ),
    "poison-death-not-reported": (
        m_poison_death_not_reported, "./internal/engine/",
        "TestAnUntreatedPoisonCanBeFatal",
    ),
    "lifespan-guard-removed": (
        m_lifespan_guard_removed, "./internal/engine/",
        "TestNoLifespanLeftRefusesMonthActions",
    ),
    "travel-ages-the-character": (
        m_travel_ages_the_character, "./internal/engine/",
        "TestOnlyASettledMonthAgesTheCharacter",
    ),
    "karma-granted-automatically": (
        m_karma_granted_automatically, "./internal/engine/",
        "TestKarmaIsNotChangedByAnyAutomaticRule",
    ),
    "karma-tier-lowest-wins": (
        m_karma_tier_lowest_wins, "./internal/engine/",
        "TestKarmaTiersAreOrderedAndEveryTotalIsDescribed",
    ),
    "remaining-lifespan-negative": (
        m_remaining_lifespan_negative, "./internal/engine/",
        "TestStatusDetailReportsNoNegativeRemainingLifespan",
    ),
    "creation-attack-grant-dropped": (
        m_creation_attack_grant_dropped, "./internal/engine/",
        "TestCreationGrantsReachTheCharacter",
    ),
    "shipped-poison-cured-by-rest": (
        m_shipped_poison_cured_by_rest, "./internal/content/",
        "TestShippedPoisonIsNotCuredByRest|TestM1CatalogueValidates",
    ),
}


def classify(code, out):
    """Decide whether a run is a real catch.

    A non-zero exit is not sufficient evidence: a build failure, a bad package
    path, or a -run pattern that matches nothing all exit non-zero while proving
    nothing. A catch requires compilation succeeded AND a named test ran AND at
    least one FAILED.
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
    return "CAUGHT", "%d of %d tests failed" % (failed, ran)


def verify_coverage():
    """Fail if any mutation edits a file that is not in GUARDED."""
    problems = []
    guarded = {os.path.normpath(p.replace("/", os.sep)) for p in GUARDED}

    for name, (fn, _pkg, _pattern) in sorted(MUTATIONS.items()):
        source = inspect.getsource(fn)
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
    try:
        path = fn()
        cmd = [GO, "test", pkg, "-run", pattern, "-v", "-count=1"]
        proc = subprocess.run(
            cmd, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
            errors="replace", timeout=600,
        )
        verdict, why = classify(proc.returncode, proc.stdout)
    finally:
        restore(saved)

    if verdict == "CAUGHT":
        print("CAUGHT     %-38s %s" % (name, why))
        return True
    print("NOT CAUGHT %-38s %s  [edited %s]" % (name, why, path))
    print("  --- output tail ---")
    for line in proc.stdout.splitlines()[-25:]:
        print("  " + line)
    return False


def main(argv):
    if len(argv) != 2:
        print(__doc__)
        return 2

    arg = argv[1]

    if arg == "--list":
        for name in sorted(MUTATIONS):
            print(name)
        return 0

    if arg == "--record-baseline":
        write_baseline()
        return 0

    if arg == "--check-baseline":
        problems = check_baseline()
        if problems:
            for p in problems:
                print("CONTAMINATED: " + p)
            return 1
        print("baseline matches; tree is clean")
        return 0

    if arg == "--check-coverage":
        problems = verify_coverage()
        if problems:
            for p in problems:
                print("COVERAGE: " + p)
            return 1
        print("every mutation targets a guarded file")
        return 0

    problems = verify_coverage()
    if problems:
        for p in problems:
            print("COVERAGE: " + p)
        return 1

    base_problems = check_baseline()
    if base_problems:
        for p in base_problems:
            print("CONTAMINATED: " + p)
        return 1

    if arg == "--all":
        names = sorted(MUTATIONS)
    elif arg in MUTATIONS:
        names = [arg]
    else:
        print("unknown mutation %r; try --list" % arg)
        return 2

    caught = 0
    for name in names:
        if run_one(name):
            caught += 1

    print("\n%d/%d CAUGHT" % (caught, len(names)))
    return 0 if caught == len(names) else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
