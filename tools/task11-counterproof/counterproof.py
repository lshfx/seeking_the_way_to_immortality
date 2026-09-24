#!/usr/bin/env python3
"""Counter-proof harness for TASK-11 (M1 scenario, cast and early goals).

Every mutation reverts exactly one protection so the corresponding test must
fail. A suite that still passes with the protection removed is not testing that
protection.

The mechanics are deliberately identical to tools/task06-, task07- and
task10-counterproof: line endings are DETECTED rather than assumed, every
substitution asserts it matched exactly once, a recorded baseline detects
contamination, and a catch requires that the package BUILT and that at least one
named test actually FAILED.

Usage:
    python3 tools/task11-counterproof/counterproof.py --list
    python3 tools/task11-counterproof/counterproof.py --record-baseline
    python3 tools/task11-counterproof/counterproof.py --check-baseline
    python3 tools/task11-counterproof/counterproof.py --check-coverage
    python3 tools/task11-counterproof/counterproof.py <mutation-name>
    python3 tools/task11-counterproof/counterproof.py --all
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
BACKUP_DIR = os.path.join(ROOT, ".task-cache", "counterproof-task11")
BASELINE = os.path.join(BACKUP_DIR, "baseline.sha256")

# Files any mutation may edit. This list is COMPLETE rather than a sample,
# because a mutation against an unlisted file is applied and never reverted.
GUARDED = (
    "internal/content/cast.go",
    "internal/content/catalogue.go",
    "internal/content/m1.go",
    "internal/engine/content.go",
    "internal/engine/effects.go",
    "internal/engine/events.go",
    "internal/engine/npc.go",
    "internal/engine/pipeline.go",
    "internal/engine/save.go",
    "internal/engine/validate.go",
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


def m_npc_age_dropped():
    """Ship an NPC with no declared age.

    Design 14 requires every NPC's age to be explicit, and an NPC of unknown age
    is exactly the one that must never be offered romance content later.
    """
    path = "internal/content/cast.go"
    patch(path, "LifespanYears: 100, InitialAgeYears: 42,\n",
          "LifespanYears: 100,\n")
    return path


def m_npc_spawn_not_called():
    """Stop populating the world with the cast at creation.

    With this reverted the catalogue declares three NPCs and the world contains
    none, which is the state TASK-11 exists to fix.
    """
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\tif err := SpawnNPCs(s.World, e.Catalogue); err != nil {\n"
        "\t\treturn reject(c, ErrPreconditionUnmet, err.Error())\n"
        "\t}\n",
        "\t_ = e.Catalogue\n",
    )
    return path


def m_npc_spawn_not_idempotent():
    """Re-spawn over an NPC that already exists.

    With this reverted a retried creation, or a save written before the cast
    existed, resets an age the world has already advanced.
    """
    path = "internal/engine/npc.go"
    patch(
        path,
        "\t\tif _, exists := world.NPCs[def.ID]; exists {\n\t\t\tcontinue\n\t\t}\n",
        "\t\tif false {\n\t\t\tcontinue\n\t\t}\n",
    )
    return path


def m_npc_age_not_carried():
    """Spawn every NPC at age zero."""
    path = "internal/engine/npc.go"
    patch(
        path,
        "\t\t\tAgeMonths:     int64(def.InitialAgeYears) * 12,\n",
        "\t\t\tAgeMonths:     0,\n",
    )
    return path


def m_npc_age_not_validated():
    """Stop requiring an explicit age in the catalogue."""
    path = "internal/engine/validate.go"
    patch(
        path,
        "\t\tif n.InitialAgeYears <= 0 {\n",
        "\t\tif false && n.InitialAgeYears <= 0 {\n",
    )
    return path


def m_travel_not_implemented():
    """Make TRAVEL a no-op again, as it was before TASK-11."""
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\tcase KindTravel:\n\t\treturn e.applyTravel(s, c, result)\n",
        "\tcase KindTravel:\n\t\treturn e.applyZeroMonth(s, c, result)\n",
    )
    return path


def m_travel_accepts_any_location():
    """Let the player travel anywhere, ignoring the graph."""
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\tfor i := range e.Catalogue.Locations {\n"
        "\t\tif e.Catalogue.Locations[i].ID == from {\n"
        "\t\t\treturn containsString(e.Catalogue.Locations[i].Neighbors, to)\n"
        "\t\t}\n"
        "\t}\n"
        "\treturn false\n",
        "\treturn to != \"\"\n",
    )
    return path


def m_scene_gate_removed():
    """Stop gating events by the place the player is in.

    With this reverted a sect patrol fires while the player sits in their cave,
    and the scene column and the travel graph are both decoration.
    """
    path = "internal/engine/events.go"
    patch(
        path,
        '\tif ev.Scene != "" {\n',
        '\tif false && ev.Scene != "" {\n',
    )
    return path


def m_node_text_dropped():
    """Ship a node with no display text.

    Design 14 asks for one to three sentences; a node without text is a menu.
    """
    path = "internal/content/m1.go"
    patch(
        path,
        '\t\t\tTextZH:         "洞府清冷，蒲团上落了薄灰。你刚安顿下来，先想清楚这个月要做什么。",\n',
        '\t\t\tTextZH:         "",\n',
    )
    return path


def m_node_text_too_long():
    """Ship a node whose text is a wall of prose."""
    path = "internal/content/m1.go"
    patch(
        path,
        '\t\t\tTextZH:   "山路在这里分开：近处草药稀疏但看得见底，深处据说有好药，也有东西在动。",\n',
        '\t\t\tTextZH:   "山路在这里分开。近处草药稀疏。深处有好药。也有东西在动。",\n',
    )
    return path


def m_goal_abandon_unreachable():
    """Remove the choice that abandons one early goal.

    With this reverted the goal can only succeed or fail, so "the goal can be
    abandoned" stops being true of the content.
    """
    path = "internal/content/m1.go"
    patch(
        path,
        "\t\t\t\t\t\tKind: engine.GrantAdditive, Target: \"flags.goal_protect_abandon\",\n"
        "\t\t\t\t\t\tAmount: 1, Reason: \"「守护所爱」：选择不介入\",\n",
        "\t\t\t\t\t\tKind: engine.GrantAdditive, Target: \"flags.unused_marker\",\n"
        "\t\t\t\t\t\tAmount: 1, Reason: \"「守护所爱」：选择不介入\",\n",
    )
    return path


def m_goal_success_sect_gated():
    """Gate the rogue-path success branch behind sect membership.

    ADR-001: "两个目标均可通过散修路径推进". With this reverted one goal can only
    be advanced by joining a sect.
    """
    path = "internal/content/m1.go"
    patch(
        path,
        "\t\t\t\t\tRequires: []engine.Precondition{{\n"
        "\t\t\t\t\t\tKind: engine.CondFunds, Key: \"spirit_stones\",\n"
        "\t\t\t\t\t\tOp: engine.OpGE, Value: 30,\n"
        "\t\t\t\t\t}},\n",
        "\t\t\t\t\tRequires: []engine.Precondition{\n"
        "\t\t\t\t\t\t{\n"
        "\t\t\t\t\t\t\tKind: engine.CondFunds, Key: \"spirit_stones\",\n"
        "\t\t\t\t\t\t\tOp: engine.OpGE, Value: 30,\n"
        "\t\t\t\t\t\t},\n"
        "\t\t\t\t\t\t{Kind: engine.CondSectMember, Op: engine.OpEQ, Value: 1},\n"
        "\t\t\t\t\t},\n",
    )
    return path


def m_travel_graph_asymmetric():
    """Cut the return edge of one route."""
    path = "internal/content/cast.go"
    patch(
        path,
        '\t\t\tID: "cave_dwelling", NameZH: "洞府",\n'
        "\t\t\tEnvironment: engine.EnvNormal, Safe: true,\n"
        '\t\t\tNeighbors:   []string{"market", "herb_woods"},\n',
        '\t\t\tID: "cave_dwelling", NameZH: "洞府",\n'
        "\t\t\tEnvironment: engine.EnvNormal, Safe: true,\n"
        '\t\t\tNeighbors:   []string{"market"},\n',
    )
    return path


def m_trial_node_dropped():
    """Ship a major breakthrough with no tribulation node.

    Design 14: "补足后继雷劫节点". Without one, a major advance is a dice roll
    rather than a scene.
    """
    path = "internal/content/m1.go"
    patch(
        path,
        '\t\t\t\tTrialNodes: []string{"EVT-011", "EVT-012"},\n',
        "\t\t\t\tTrialNodes: nil,\n",
    )
    return path


def m_earthly_path_opened():
    """Claim M1 opens a path ADR-001 froze out."""
    path = "internal/content/catalogue.go"
    patch(
        path,
        "\tc.M1Paths = []engine.Path{engine.PathHuman}\n",
        "\tc.M1Paths = []engine.Path{engine.PathHuman, engine.PathEarthly}\n",
    )
    return path


def m_condition_operator_always_required():
    """Demand a comparison operator from every condition kind.

    With this reverted `npc_available` and `consumed_event` become impossible to
    write correctly: they answer from a key alone and have nothing to compare.
    """
    path = "internal/engine/effects.go"
    patch(
        path,
        "\tif p.Kind.NeedsOperator() {\n",
        "\tif true {\n",
    )
    return path


def m_digest_ignores_npcs():
    """Stop covering the cast in the integrity digest.

    With this reverted an edited NPC age or liveness verifies as intact, and
    changes who is available to talk to without the check noticing.
    """
    path = "internal/engine/save.go"
    patch(
        path,
        "\t\tfor _, id := range sortedKeys(st.World.NPCs) {\n",
        "\t\tfor _, id := range []string(nil) {\n",
    )
    return path


def m_digest_ignores_current_location():
    """Stop covering where the player is.

    With this reverted a tampered location verifies as intact, and the scene
    gate then lets a different set of events fire.
    """
    path = "internal/engine/save.go"
    patch(
        path,
        '\t\tappendStr("state.world.current_location", st.World.CurrentLocation)\n',
        "\t\t_ = st.World.CurrentLocation\n",
    )
    return path


MUTATIONS = {
    # name: (function, package, -run pattern)
    "npc-age-dropped": (
        m_npc_age_dropped, "./internal/content/",
        "TestEveryNPCHasAnExplicitAge|TestM1CatalogueValidates",
    ),
    "npc-spawn-not-called": (
        m_npc_spawn_not_called, "./internal/engine/",
        "TestConfirmingACharacterPopulatesTheNamedCast",
    ),
    "npc-spawn-not-idempotent": (
        m_npc_spawn_not_idempotent, "./internal/engine/",
        "TestSpawningTheCastIsIdempotent",
    ),
    "npc-age-not-carried": (
        m_npc_age_not_carried, "./internal/engine/",
        "TestConfirmingACharacterPopulatesTheNamedCast",
    ),
    "npc-age-not-validated": (
        m_npc_age_not_validated, "./internal/engine/",
        "TestNPCWithoutAnExplicitAgeIsRejected",
    ),
    "travel-not-implemented": (
        m_travel_not_implemented, "./internal/engine/",
        "TestTravelMovesBetweenNeighbours",
    ),
    "travel-accepts-any-location": (
        m_travel_accepts_any_location, "./internal/engine/",
        "TestTravelRefusesANonNeighbour",
    ),
    "scene-gate-removed": (
        m_scene_gate_removed, "./internal/engine/",
        "TestEventsAreGatedByScene",
    ),
    "node-text-dropped": (
        m_node_text_dropped, "./internal/content/",
        "TestEveryEventNodeHasOneToThreeSentences|TestM1CatalogueValidates",
    ),
    "node-text-too-long": (
        m_node_text_too_long, "./internal/content/",
        "TestEveryEventNodeHasOneToThreeSentences|TestM1CatalogueValidates",
    ),
    "goal-abandon-unreachable": (
        m_goal_abandon_unreachable, "./internal/content/",
        "TestBothEarlyGoalsCanSucceedFailAndBeAbandoned|TestM1CatalogueValidates",
    ),
    "goal-success-sect-gated": (
        m_goal_success_sect_gated, "./internal/content/",
        "TestBothEarlyGoalsAreAdvanceableAsARogue|TestM1CatalogueValidates",
    ),
    "travel-graph-asymmetric": (
        m_travel_graph_asymmetric, "./internal/content/",
        "TestTravelGraphIsConnectedAndSymmetric|TestM1CatalogueValidates",
    ),
    "trial-node-dropped": (
        m_trial_node_dropped, "./internal/content/",
        "TestEveryMajorBreakthroughHasATrialNode",
    ),
    "earthly-path-opened": (
        m_earthly_path_opened, "./internal/content/",
        "TestOnlyTheHumanPathIsOpen|TestM1CatalogueValidates",
    ),
    "condition-operator-always-required": (
        m_condition_operator_always_required, "./internal/engine/",
        "TestASpawnedNPCBecomesAvailableOnceMet",
    ),
    "digest-ignores-npcs": (
        m_digest_ignores_npcs, "./internal/engine/",
        "TestCanonicalStringCoversCultivationState",
    ),
    "digest-ignores-current-location": (
        m_digest_ignores_current_location, "./internal/engine/",
        "TestDigestCoversTheScenarioState",
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
