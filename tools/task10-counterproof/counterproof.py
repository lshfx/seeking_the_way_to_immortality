#!/usr/bin/env python3
"""Counter-proof harness for TASK-10 (event scheduling and the whitelist DSL).

Every mutation reverts exactly one protection so the corresponding test must
fail. A suite that still passes with the protection removed is not testing that
protection.

The mechanics are deliberately identical to tools/task06-counterproof and
tools/task07-counterproof: line endings are DETECTED rather than assumed, every
substitution asserts it matched exactly once, a recorded baseline detects
contamination, and a catch requires that the package BUILT and that at least one
named test actually FAILED. Those four rules each exist because a weaker version
produced a false CAUGHT in this project.

Usage:
    python3 tools/task10-counterproof/counterproof.py --list
    python3 tools/task10-counterproof/counterproof.py --record-baseline
    python3 tools/task10-counterproof/counterproof.py --check-baseline
    python3 tools/task10-counterproof/counterproof.py --check-coverage
    python3 tools/task10-counterproof/counterproof.py <mutation-name>
    python3 tools/task10-counterproof/counterproof.py --all
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
BACKUP_DIR = os.path.join(ROOT, ".task-cache", "counterproof-task10")
BASELINE = os.path.join(BACKUP_DIR, "baseline.sha256")

# Files any mutation may edit. This list is COMPLETE rather than a sample,
# because a mutation against an unlisted file is applied and never reverted.
# verify_coverage() fails the run if a mutation targets a file that is not here.
GUARDED = (
    "internal/engine/events.go",
    "internal/engine/effects.go",
    "internal/engine/pipeline.go",
    "internal/engine/store.go",
    "internal/engine/save.go",
    "internal/engine/validate.go",
    "internal/engine/state.go",
    "internal/engine/command.go",
    "internal/content/m1.go",
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


def m_scheduler_not_invoked():
    """Stop running the month-end pass at all.

    This is the whole of TASK-10 removed in one line: no expiry, no forced
    events, no base check, no promotion.
    """
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\te.runMonthEndEvents(s, &result)\n",
        "\t_ = e\n",
    )
    return path


def m_base_chance_always_passes():
    """Make the base check a certainty.

    With this reverted, a catalogue that configures a zero chance still fires an
    event every month.
    """
    path = "internal/engine/events.go"
    patch(
        path,
        "\tpassed := stream.Chance(chance)\n",
        "\tpassed := true\n\t_ = chance\n\t_ = stream\n",
    )
    return path


def m_base_chance_ignores_configured_zero():
    """Treat a configured zero as "unset" and fall back to the default.

    This is the defect the tests caught during development: provenance, not the
    value, decides whether the catalogue configured the number, because a
    configured 0 is a legitimate content decision meaning "never".
    """
    path = "internal/engine/events.go"
    patch(
        path,
        '\tif e.Catalogue.EventBaseChancePermille.Provenance != "" {\n',
        "\tif e.Catalogue.EventBaseChancePermille.Value > 0 {\n",
    )
    return path


def m_expiry_forgets_the_occurrence():
    """Drop the ledger row when a queued instance expires.

    With this reverted, ignoring an event until it expires refunds its
    occurrence budget, so a MaxOccurrences cap becomes meaningless.
    """
    path = "internal/engine/events.go"
    patch(
        path,
        "\t\t\tresult.Events = append(result.Events, ResultEvent{\n"
        "\t\t\t\tKind:       ResultEventExpired,\n"
        "\t\t\t\tID:         q.EventID,\n"
        "\t\t\t\tInstanceID: q.InstanceID,\n"
        '\t\t\t\tDetail:     "queued event expired unanswered",\n'
        "\t\t\t})\n"
        "\t\t\tcontinue\n",
        "\t\t\tresult.Events = append(result.Events, ResultEvent{\n"
        "\t\t\t\tKind:       ResultEventExpired,\n"
        "\t\t\t\tID:         q.EventID,\n"
        "\t\t\t\tInstanceID: q.InstanceID,\n"
        '\t\t\t\tDetail:     "queued event expired unanswered",\n'
        "\t\t\t})\n"
        "\t\t\tif s.World != nil {\n"
        "\t\t\t\tfor i := range s.World.RaisedEvents {\n"
        "\t\t\t\t\tif s.World.RaisedEvents[i].InstanceID == q.InstanceID {\n"
        "\t\t\t\t\t\ts.World.RaisedEvents = append(s.World.RaisedEvents[:i], s.World.RaisedEvents[i+1:]...)\n"
        "\t\t\t\t\t\tbreak\n"
        "\t\t\t\t\t}\n"
        "\t\t\t\t}\n"
        "\t\t\t}\n"
        "\t\t\tcontinue\n",
    )
    return path


def m_cooldown_ignored():
    """Stop honouring the per-event cooldown."""
    path = "internal/engine/events.go"
    patch(
        path,
        "\tif ev.CooldownMonths > 0 {\n",
        "\tif false && ev.CooldownMonths > 0 {\n",
    )
    return path


def m_max_occurrences_ignored():
    """Stop honouring the per-event occurrence cap."""
    path = "internal/engine/events.go"
    patch(
        path,
        "\tif ev.MaxOccurrences > 0 && raisedCount(s, ev.ID) >= int64(ev.MaxOccurrences) {\n",
        "\tif false && raisedCount(s, ev.ID) >= int64(ev.MaxOccurrences) {\n",
    )
    return path


def m_forced_events_not_raised():
    """Stop raising forced events.

    With this reverted the opening node never fires, so the tutorial gate is
    silently skipped.
    """
    path = "internal/engine/events.go"
    patch(
        path,
        "\t\tif !ev.Forced {\n\t\t\tcontinue\n\t\t}\n",
        "\t\tif true {\n\t\t\tcontinue\n\t\t}\n",
    )
    return path


def m_promotion_ignores_priority():
    """Promote the first queued instance instead of the highest priority one.

    With this reverted the player is shown whichever instance happened to be
    queued first, so a crisis node can be pushed behind an ordinary one.
    """
    path = "internal/engine/events.go"
    patch(
        path,
        "\tbest := 0\n"
        "\tfor i := 1; i < len(s.Pending.EventQueue); i++ {\n"
        "\t\tcandidate := s.Pending.EventQueue[i]\n"
        "\t\tincumbent := s.Pending.EventQueue[best]\n"
        "\t\tif candidate.Priority > incumbent.Priority {\n"
        "\t\t\tbest = i\n"
        "\t\t\tcontinue\n"
        "\t\t}\n"
        "\t\tif candidate.Priority < incumbent.Priority {\n"
        "\t\t\tcontinue\n"
        "\t\t}\n"
        "\t\tif candidate.RaisedAtWorldMonth < incumbent.RaisedAtWorldMonth {\n"
        "\t\t\tbest = i\n"
        "\t\t}\n"
        "\t}\n",
        "\tbest := 0\n",
    )
    return path


def m_duplicate_queue_entry_allowed():
    """Allow the same event to be queued twice.

    With this reverted a repeatable, zero-cooldown event fills the queue with
    identical instances.
    """
    path = "internal/engine/events.go"
    patch(
        path,
        "\tfor _, q := range s.Pending.EventQueue {\n"
        "\t\tif q.EventID == ev.ID {\n"
        "\t\t\treturn false\n"
        "\t\t}\n"
        "\t}\n",
        "\t_ = s.Pending.EventQueue\n",
    )
    return path


def m_queue_not_drained_on_answer():
    """Stop promoting the next queued node when one is answered.

    With this reverted the backlog is invisible until the player spends another
    month, which contradicts "多事件排队不强制玩家一次处理完".
    """
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\te.promoteQueuedEvent(s, &result)\n\treturn result\n",
        "\treturn result\n",
    )
    return path


def m_combat_draws_a_monthly_event():
    """Run the month-end pass from a combat round.

    With this reverted a combat round draws a monthly event, which the
    acceptance criteria forbid.
    """
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\tcb.AwaitingPlayer = true\n\tresult.MonthCostApplied = 0\n\treturn result\n",
        "\tcb.AwaitingPlayer = true\n\tresult.MonthCostApplied = 0\n\te.runMonthEndEvents(s, &result)\n\treturn result\n",
    )
    return path


def m_pending_node_does_not_block_months():
    """Allow a month action while an event node is waiting.

    With this reverted a pending node no longer replaces the scene actions, so
    months can settle behind the player's back and the queue grows without
    bound.
    """
    path = "internal/engine/pipeline.go"
    patch(
        path,
        '\tcase PhaseEventPending:\n'
        '\t\tif kind == KindEventChoice {\n'
        '\t\t\treturn ErrNone, "", true\n'
        '\t\t}\n'
        '\t\treturn ErrBadPhase, "an event is awaiting a choice", false\n',
        '\tcase PhaseEventPending:\n'
        '\t\treturn ErrNone, "", true\n',
    )
    return path


def m_whitelist_not_enforced():
    """Stop rejecting an effect target outside the whitelist.

    This is what makes "DSL 不能执行文件、网络或任意脚本" checkable: without it a
    content pack can name anything and validation stays silent.
    """
    path = "internal/engine/validate.go"
    patch(
        path,
        "\t\t} else if !ValidEffectTarget(e.Target, scope) {\n",
        "\t\t} else if false && !ValidEffectTarget(e.Target, scope) {\n",
    )
    return path


def m_creation_only_target_allowed_at_runtime():
    """Let a runtime effect name cultivation_rate.

    With this reverted an event can grant a rate bonus that the monthly
    recomputation overwrites, so the content silently does nothing.
    """
    path = "internal/engine/effects.go"
    patch(
        path,
        "\tcase targetCultivationRate:\n\t\treturn scope == ScopeCreation\n",
        "\tcase targetCultivationRate:\n\t\treturn true\n",
    )
    return path


def m_unknown_runtime_target_ignored():
    """Make an unknown effect target a silent no-op.

    With this reverted the engine applies nothing and reports success, which is
    the failure mode the whitelist exists to prevent: an event that shows its
    text and quietly does nothing.
    """
    path = "internal/engine/effects.go"
    patch(
        path,
        '\treturn fmt.Errorf("unknown effect target %q", eff.Target)\n}\n',
        "\treturn nil\n}\n",
    )
    return path


def m_cost_affordability_not_prechecked():
    """Stop checking that a choice's costs are affordable.

    Affordability is guarded twice on purpose: once before anything is written,
    so a partly-affordable choice is refused outright, and once inside the payer
    itself. A mutation that reverts only one of them proves nothing, because the
    other still refuses — which is exactly what happened on the first run of this
    harness, and why both sites are reverted here.
    """
    path = "internal/engine/pipeline.go"
    patch(
        path,
        "\tfor _, cost := range choice.Costs {\n"
        "\t\tif code, detail, payable := canPayEventCost(s, cost); !payable {\n"
        "\t\t\treturn reject(c, code, detail)\n"
        "\t\t}\n"
        "\t}\n",
        "\t_ = choice.Costs\n",
    )
    path2 = "internal/engine/effects.go"
    patch(
        path2,
        "\tif code, detail, ok := canPayEventCost(s, cost); !ok {\n"
        '\t\treturn fmt.Errorf("%s: %s", code, detail)\n'
        "\t}\n",
        "\t_ = canPayEventCost\n",
    )
    return path2


def m_branch_resolves_immediately():
    """Ignore a choice's successor node.

    With this reverted a chained event resolves at its first node, so the
    follow-up's effects never apply and the instance is consumed early.
    """
    path = "internal/engine/pipeline.go"
    patch(
        path,
        '\tif choice.NextNodeID != "" {\n',
        '\tif false && choice.NextNodeID != "" {\n',
    )
    return path


def m_mood_not_clamped():
    """Let a mood grant leave the 0..100 range.

    With this reverted an event pushes mood past its contract bound and the
    command fails as a broken invariant instead of clamping.
    """
    path = "internal/engine/effects.go"
    patch(
        path,
        "\tif after < 0 {\n\t\tafter = 0\n\t}\n"
        "\tif after > 100 {\n\t\tafter = 100\n\t}\n"
        "\tbefore := int64(s.Player.Condition.Mood)\n",
        "\tbefore := int64(s.Player.Condition.Mood)\n",
    )
    return path


def m_growth_not_deep_copied():
    """Share Attributes.Growth between the clone and the live state.

    With this reverted a command that is later rejected still edits the live
    character's growth through the clone it was evaluating against.
    """
    path = "internal/engine/store.go"
    patch(
        path,
        "\tif p.Attributes.Growth != nil {\n"
        "\t\tout.Attributes.Growth = make(map[string]int, len(p.Attributes.Growth))\n"
        "\t\tfor k, v := range p.Attributes.Growth {\n"
        "\t\t\tout.Attributes.Growth[k] = v\n"
        "\t\t}\n"
        "\t}\n",
        "\t_ = p.Attributes.Growth\n",
    )
    return path


def m_queue_not_deep_copied():
    """Share the queued instances' candidate slices with the live state."""
    path = "internal/engine/store.go"
    patch(
        path,
        "\tif len(p.EventQueue) > 0 {\n"
        "\t\tout.EventQueue = make([]QueuedEvent, len(p.EventQueue))\n"
        "\t\tfor i, q := range p.EventQueue {\n"
        "\t\t\tq.ChoiceIDs = append([]string(nil), q.ChoiceIDs...)\n"
        "\t\t\tout.EventQueue[i] = q\n"
        "\t\t}\n"
        "\t}\n",
        "\t_ = p.EventQueue\n",
    )
    return path


def m_digest_ignores_the_queue():
    """Stop covering the queued instances in the integrity digest.

    With this reverted a tampered queue — a changed candidate set, a forged
    deadline — verifies as intact.
    """
    path = "internal/engine/save.go"
    patch(
        path,
        "\tfor _, q := range st.Pending.EventQueue {\n"
        '\t\tappendStr("state.pending.event_queue.id", q.EventID)\n'
        '\t\tappendStr("state.pending.event_queue.instance", q.InstanceID)\n'
        '\t\tappendInt("state.pending.event_queue.priority", int64(q.Priority))\n'
        '\t\tappendInt("state.pending.event_queue.raised_at", q.RaisedAtWorldMonth)\n'
        '\t\tappendInt("state.pending.event_queue.expires_at", q.ExpiresAtWorldMonth)\n'
        "\t\tfor _, id := range q.ChoiceIDs {\n"
        '\t\t\tappendStr("state.pending.event_queue.choice", id)\n'
        "\t\t}\n"
        "\t}\n",
        "\t_ = st.Pending.EventQueue\n",
    )
    return path


def m_forced_weight_rule_removed():
    """Stop requiring a forced event to have weight 0."""
    path = "internal/engine/validate.go"
    patch(
        path,
        "\t\tif e.Forced && e.Weight.Value != 0 {\n",
        "\t\tif false && e.Forced && e.Weight.Value != 0 {\n",
    )
    return path


def m_followup_choices_not_checked():
    """Stop checking that a follow-up node offers declared choices."""
    path = "internal/engine/validate.go"
    patch(
        path,
        "\t\t\tfor _, id := range fu.ChoiceIDs {\n"
        "\t\t\t\tif !choiceIDs[id] {\n",
        "\t\t\tfor _, id := range fu.ChoiceIDs {\n"
        "\t\t\t\tif false && !choiceIDs[id] {\n",
    )
    return path


def m_queue_ledger_agreement_not_checked():
    """Stop checking queued instances against the occurrence ledger."""
    path = "internal/engine/validate.go"
    patch(
        path,
        "\t\traised, ok := ledger[q.InstanceID]\n"
        "\t\tif !ok {\n",
        "\t\traised, ok := ledger[q.InstanceID]\n"
        "\t\tif !ok {\n"
        "\t\t\traised = RaisedEvent{EventID: q.EventID, InstanceID: q.InstanceID}\n"
        "\t\t\tok = true\n"
        "\t\t}\n"
        "\t\tif false {\n",
    )
    return path


def m_opening_node_not_forced():
    """Ship the opening event as an ordinary drawable node.

    With this reverted the tutorial gate is a 20%-per-month dice roll instead of
    a guaranteed opening, so a new player can miss it entirely.
    """
    path = "internal/content/m1.go"
    patch(
        path,
        '\t\t\tForced:         true,\n',
        "\t\t\tForced:         false,\n",
    )
    return path


MUTATIONS = {
    # name: (function, package, -run pattern)
    "scheduler-not-invoked": (
        m_scheduler_not_invoked, "./internal/engine/",
        "TestCertainBaseChanceAlwaysRaises|TestOnlyOneNodeIsShownAndTheRestQueue|TestMaxOccurrencesStopsFurtherRaises",
    ),
    "base-chance-always-passes": (
        m_base_chance_always_passes, "./internal/engine/",
        "TestZeroBaseChanceNeverRaises",
    ),
    "base-chance-ignores-configured-zero": (
        m_base_chance_ignores_configured_zero, "./internal/engine/",
        "TestZeroBaseChanceNeverRaises",
    ),
    "expiry-forgets-the-occurrence": (
        m_expiry_forgets_the_occurrence, "./internal/engine/",
        "TestExpiredInstanceStillConsumesTheOccurrenceBudget",
    ),
    "cooldown-ignored": (
        m_cooldown_ignored, "./internal/engine/",
        "TestCooldownBlocksARaiseUntilItElapses",
    ),
    "max-occurrences-ignored": (
        m_max_occurrences_ignored, "./internal/engine/",
        "TestMaxOccurrencesStopsFurtherRaises",
    ),
    "forced-events-not-raised": (
        m_forced_events_not_raised, "./internal/engine/",
        "TestOnlyOneNodeIsShownAndTheRestQueue|TestAnsweringANodePromotesTheNextQueuedOne",
    ),
    "promotion-ignores-priority": (
        m_promotion_ignores_priority, "./internal/engine/",
        "TestOnlyOneNodeIsShownAndTheRestQueue",
    ),
    "duplicate-queue-entry-allowed": (
        m_duplicate_queue_entry_allowed, "./internal/engine/",
        "TestTheSameEventIsNotQueuedTwice",
    ),
    "queue-not-drained-on-answer": (
        m_queue_not_drained_on_answer, "./internal/engine/",
        "TestAnsweringANodePromotesTheNextQueuedOne",
    ),
    "combat-draws-a-monthly-event": (
        m_combat_draws_a_monthly_event, "./internal/engine/",
        "TestCombatRoundsDoNotDrawAMonthlyEvent",
    ),
    "pending-node-does-not-block-months": (
        m_pending_node_does_not_block_months, "./internal/engine/",
        "TestPendingNodeBlocksMonthActions",
    ),
    "whitelist-not-enforced": (
        m_whitelist_not_enforced, "./internal/engine/",
        "TestUnknownEffectTargetIsRejected",
    ),
    "creation-only-target-allowed-at-runtime": (
        m_creation_only_target_allowed_at_runtime, "./internal/engine/",
        "TestRuntimeEffectCannotNameACreationOnlyTarget",
    ),
    "unknown-runtime-target-ignored": (
        m_unknown_runtime_target_ignored, "./internal/engine/",
        "TestUnknownRuntimeEffectTargetIsRejectedAtRuntime",
    ),
    "cost-affordability-not-prechecked": (
        m_cost_affordability_not_prechecked, "./internal/engine/",
        "TestUnaffordableChoiceIsRejectedWithoutBorrowing",
    ),
    "branch-resolves-immediately": (
        m_branch_resolves_immediately, "./internal/engine/",
        "TestBranchAdvancesTheInstanceWithoutSettlingASecondMonth",
    ),
    "mood-not-clamped": (
        m_mood_not_clamped, "./internal/engine/",
        "TestMoodGrantIsClampedToTheContractRange",
    ),
    "growth-not-deep-copied": (
        m_growth_not_deep_copied, "./internal/engine/",
        "TestCloningDoesNotShareTheQueueBackingArrays",
    ),
    "queue-not-deep-copied": (
        m_queue_not_deep_copied, "./internal/engine/",
        "TestCloningDoesNotShareTheQueueBackingArrays",
    ),
    "digest-ignores-the-queue": (
        m_digest_ignores_the_queue, "./internal/engine/",
        "TestDigestCoversEventSchedulingState|TestQueueSurvivesASaveRoundTripWithoutRerolling",
    ),
    "forced-weight-rule-removed": (
        m_forced_weight_rule_removed, "./internal/engine/",
        "TestForcedEventMustNotAlsoBeDrawn",
    ),
    "followup-choices-not-checked": (
        m_followup_choices_not_checked, "./internal/engine/",
        "TestEventFollowUpOfferingAnUndeclaredChoiceIsRejected",
    ),
    "queue-ledger-agreement-not-checked": (
        m_queue_ledger_agreement_not_checked, "./internal/engine/",
        "TestQueuedInstanceWithoutALedgerRowIsRejected",
    ),
    "opening-node-not-forced": (
        m_opening_node_not_forced, "./internal/content/",
        "TestM1OpeningEventIsForced",
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
        print("CAUGHT     %-42s %s" % (name, why))
        return True
    print("NOT CAUGHT %-42s %s  [edited %s]" % (name, why, path))
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
