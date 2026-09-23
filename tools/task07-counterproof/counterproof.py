#!/usr/bin/env python3
"""Counter-proof harness for TASK-07 (character creation).

Every mutation reverts one piece of protection in the creation code so the
corresponding test must fail. A test suite that still passes with the protection
removed is not testing that protection.

The mechanics are deliberately the same as tools/task06-counterproof: line
endings are DETECTED rather than assumed, every substitution asserts it matched
exactly once, a recorded baseline detects contamination, and a catch requires
that the package BUILT and at least one named test actually FAILED. Those four
rules each exist because a weaker version produced a false CAUGHT in this
project.

Usage:
    python3 tools/task07-counterproof/counterproof.py --list
    python3 tools/task07-counterproof/counterproof.py --record-baseline
    python3 tools/task07-counterproof/counterproof.py --check-baseline
    python3 tools/task07-counterproof/counterproof.py --check-coverage
    python3 tools/task07-counterproof/counterproof.py <mutation-name>
    python3 tools/task07-counterproof/counterproof.py --all
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
BACKUP_DIR = os.path.join(ROOT, ".task-cache", "counterproof-task07")
BASELINE = os.path.join(BACKUP_DIR, "baseline.sha256")

# Files any mutation may edit. As in TASK-06 this list is COMPLETE rather than a
# sample, because a mutation against an unlisted file is applied and never
# reverted. verify_coverage() fails the run if a mutation targets a file that is
# not here.
GUARDED = (
    "internal/engine/creation.go",
    "internal/engine/creation_factory.go",
    "internal/engine/creation_validate.go",
    "internal/engine/creation_view.go",
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
    """Fail the run if the tree does not match its recorded clean state."""
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
            "pattern:\n%s" % (found, path, old_n[:500])
        )
    write(path, text.replace(old_n, new_n))


# --- Mutations ---------------------------------------------------------------
# Each returns the path it edited. Every mutation reverts exactly one rule, so a
# failure names one protection rather than a defect cluster.


def m_base_total_budget_removed():
    """Drop the exactly-60 rule, leaving only the per-field range check.

    With this reverted, a 59- or 61-point allocation is accepted, so the
    acceptance rule "59/61 不可提交" is no longer enforced.
    """
    path = "internal/engine/creation.go"
    patch(
        path,
        "\tif total := b.Total(); total != CreationBasePoints {\n",
        "\tif total := b.Total(); false && total != CreationBasePoints {\n",
    )
    return path


def m_base_range_removed():
    """Drop the 1..15 per-attribute rule.

    With this reverted a base of 0 or 16 is accepted, so the acceptance rule
    "单项 1～15" is no longer enforced.
    """
    path = "internal/engine/creation.go"
    patch(
        path,
        "\t\tif v < CreationBaseMin || v > CreationBaseMax {\n",
        "\t\tif false && (v < CreationBaseMin || v > CreationBaseMax) {\n",
    )
    return path


def m_final_range_removed():
    """Drop the 1..20 final-value rule.

    This is the rule that makes "超限21不可提交" true. With it reverted,
    comprehension 15 + a +6 talent lands at 21 and is accepted.
    """
    path = "internal/engine/creation_validate.go"
    patch(
        path,
        "\t\tif final < CreationFinalMin || final > CreationFinalMax {\n",
        "\t\tif false && (final < CreationFinalMin || final > CreationFinalMax) {\n",
    )
    return path


def m_age_range_removed():
    """Drop the 16..60 creation-age rule."""
    path = "internal/engine/creation_validate.go"
    patch(
        path,
        "\tif sel.AgeYears < CreationAgeMinYears || sel.AgeYears > CreationAgeMaxYears {\n",
        "\tif false && (sel.AgeYears < CreationAgeMinYears || sel.AgeYears > CreationAgeMaxYears) {\n",
    )
    return path


def m_path_open_check_removed():
    """Accept a path M1 does not open.

    With this reverted, 天道 is accepted during drafting, so a character could be
    created on a path the milestone does not support.
    """
    path = "internal/engine/creation_validate.go"
    patch(
        path,
        "\t\t} else if !sel.Path.OpenInM1() {\n"
        '\t\t\tr.add(CErrPathNotOpen, "path",\n'
        '\t\t\t\t"path "+string(sel.Path)+" is not open in M1")\n'
        "\t\t}\n",
        "\t\t}\n",
    )
    return path


def m_fixed_results_not_checked():
    """Stop refusing an edit to an already-fixed attribute.

    This is the mechanism behind "刷新/恢复不重复随机初始化". With it reverted, a
    resumed draft can move a fixed value, so refreshing re-rolls the character.
    """
    path = "internal/engine/creation.go"
    patch(
        path,
        "\t\tif int(existing) != proposed.Get(f.Key) {\n",
        "\t\tif false && int(existing) != proposed.Get(f.Key) {\n",
    )
    return path


def m_fix_allocation_not_recorded():
    """Make FixAllocation a no-op, so nothing is ever locked.

    With this reverted the draft never records a fixed value, so a resume has
    nothing to restore and every render re-reads whatever is current.
    """
    path = "internal/engine/creation_factory.go"
    patch(
        path,
        "\tfor _, f := range attributingFields {\n"
        "\t\td.FixedResults[fixedPrefixAttribute+f.Key] = int64(b.Get(f.Key))\n"
        "\t}\n",
        "\t_ = b\n",
    )
    return path


def m_custom_gender_coerced():
    """Normalise the gender field to a binary pair.

    This is the defect "自定义性别不被强改" forbids. With it reverted, a custom
    gender is rewritten during the grant-free part of creation.
    """
    path = "internal/engine/creation_factory.go"
    patch(
        path,
        "\td.Identity = Identity{\n"
        "\t\tSurname:    sel.Surname,\n"
        "\t\tGivenName:  sel.GivenName,\n"
        "\t\tGender:     sel.Gender,\n"
        "\t\tAppearance: sel.Appearance,\n"
        "\t}\n",
        "\tgender := sel.Gender\n"
        '\tif gender != GenderMale && gender != GenderFemale {\n'
        '\t\tgender = GenderUnspecified\n'
        "\t}\n"
        "\td.Identity = Identity{\n"
        "\t\tSurname:    sel.Surname,\n"
        "\t\tGivenName:  sel.GivenName,\n"
        "\t\tGender:     gender,\n"
        "\t\tAppearance: sel.Appearance,\n"
        "\t}\n",
    )
    return path


def m_preset_not_validated():
    """Make the preset list return an allocation that breaks the budget.

    A preset must pass the identical validation, or it grants a hidden
    advantage. With this reverted the balanced preset overspends, so the
    "presets all pass the same validation" test must fail.
    """
    path = "internal/engine/creation_factory.go"
    patch(
        path,
        "\tbalanced := base(10, 10, 10, 10, 10, 10)\n",
        "\tbalanced := base(10, 10, 10, 10, 10, 12)\n",
    )
    return path


def m_fixed_guard_removed():
    """Stop refusing an edit to an already-fixed attribute.

    This is the mechanism behind "刷新/恢复不重复随机初始化". With it reverted, a
    resumed draft can move a fixed value, so refreshing re-rolls the character
    and farms a better allocation.
    """
    path = "internal/engine/creation.go"
    patch(
        path,
        "\t\tif int(existing) != proposed.Get(f.Key) {\n",
        "\t\tif false && int(existing) != proposed.Get(f.Key) {\n",
    )
    return path


def m_author_byline_wrong():
    """Change the byline.

    With this reverted the first screen no longer reads 作者：雾见川, so the
    byline acceptance criterion fails.
    """
    path = "internal/engine/creation.go"
    patch(
        path,
        'const CreationAuthor = "雾见川"\n',
        'const CreationAuthor = "无名氏"\n',
    )
    return path


def m_spirit_root_multiplier_ignored():
    """Ignore the spirit root multiplier when deriving the cultivation rate.

    This breaks the 19.5 baseline fixture: a true root (1.3) would no longer be
    applied, so the rate stops matching the design's worked example.
    """
    path = "internal/engine/creation_factory.go"
    patch(
        path,
        "\trate = rate * rootMultiplier / SCALE\n",
        "\t_ = rootMultiplier\n",
    )
    return path


def m_aptitude_factor_dropped():
    """Drop the (1 + 0.05 * aptitude) factor.

    A second way of breaking the 19.5 baseline: aptitude no longer enters the
    monthly rate at all.
    """
    path = "internal/engine/creation_factory.go"
    patch(
        path,
        "\trate = rate * aptitudeFactor / SCALE\n",
        "\t_ = aptitudeFactor\n",
    )
    return path


def m_closed_path_shown_available():
    """Stop marking a closed path as disabled.

    With this reverted the creation screen offers 天道 as selectable, which the
    design forbids: an unopened entrance must be marked, not shown as available.
    """
    path = "internal/engine/creation_view.go"
    patch(
        path,
        "\t\tif !open[p.id] {\n"
        '\t\t\topt.Disabled = true\n'
        '\t\t\topt.Reason = "本版未开放"\n'
        "\t\t}\n",
        "\t\t_ = open\n",
    )
    return path


MUTATIONS = {
    # name: (function, package, -run pattern)
    "base-total-budget-removed": (
        m_base_total_budget_removed, "./internal/engine/",
        "TestBaseTotalMustBeExactlySixty|TestPipelineRefusesFiftyNineAndSixtyOneAndTwentyOne",
    ),
    "base-range-removed": (
        m_base_range_removed, "./internal/engine/",
        "TestBaseRangeBoundaries",
    ),
    "final-range-removed": (
        m_final_range_removed, "./internal/engine/",
        "TestFinalTwentyOneIsRefusedSeparately|TestPipelineRefusesFiftyNineAndSixtyOneAndTwentyOne",
    ),
    "age-range-removed": (
        m_age_range_removed, "./internal/engine/",
        "TestCreationAgeBoundaries",
    ),
    "path-open-check-removed": (
        m_path_open_check_removed, "./internal/engine/",
        "TestClosedPathIsRefusedAtCreation",
    ),
    "fixed-results-not-checked": (
        m_fixed_results_not_checked, "./internal/engine/",
        "TestResumeDoesNotReRoll",
    ),
    "fix-allocation-not-recorded": (
        m_fix_allocation_not_recorded, "./internal/engine/",
        "TestResumeDoesNotReRoll|TestCreationCostsNoMonth",
    ),
    "custom-gender-coerced": (
        m_custom_gender_coerced, "./internal/engine/",
        "TestCustomGenderIsNotCoerced",
    ),
    "preset-not-validated": (
        m_preset_not_validated, "./internal/engine/",
        "TestPresetsAllPassTheSameValidation|TestPresetConfirmsInThreeActions",
    ),
    "author-byline-wrong": (
        m_author_byline_wrong, "./internal/engine/",
        "TestFirstScreenCarriesAuthorByline",
    ),
    "spirit-root-multiplier-ignored": (
        m_spirit_root_multiplier_ignored, "./internal/engine/",
        "TestCultivationRateExactBaseline|TestCultivationRateScalesWithSpiritRoot|TestConfirmProducesAValidPlayer",
    ),
    "aptitude-factor-dropped": (
        m_aptitude_factor_dropped, "./internal/engine/",
        "TestCultivationRateExactBaseline|TestCultivationRateScalesWithAptitude|TestConfirmProducesAValidPlayer",
    ),
    "closed-path-shown-available": (
        m_closed_path_shown_available, "./internal/wiring/",
        "TestClosedPathsAreMarkedNotHidden",
    ),
    "fixed-guard-removed": (
        m_fixed_guard_removed, "./internal/engine/",
        "TestResumeDoesNotReRoll",
    ),}


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
        print("CAUGHT   %-34s %s" % (name, why))
        return True
    print("NOT CAUGHT %-32s %s  [edited %s]" % (name, why, path))
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
