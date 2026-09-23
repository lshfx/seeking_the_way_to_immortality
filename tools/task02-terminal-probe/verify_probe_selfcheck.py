"""Non-interactive self-check for TASK-02 recovery and input decoding.

Some TASK-02 acceptance items can be proven without a human watching a screen:
input decoding, exit-key mapping, console-mode save/restore, and the fact that
rendering never mutates game state. This script exercises those paths and prints
a pass/fail table plus a machine-readable evidence block.

It deliberately does NOT claim to replace the manual desktop-terminal checks
(resize-drag behaviour, real colour appearance, font rendering, Ctrl+C reaching
a running process). Those still need a person.

Usage:

    python verify_probe_selfcheck.py
"""

from __future__ import annotations

import argparse
import io
import json
import os
import sys
from typing import Iterable

from interactive_probe import (
    MODEL,
    ProbeState,
    TerminalInput,
    TerminalSession,
    _read_escape_sequence,
    display_width,
    handle_key,
    render_panel,
    safe_text,
)

BEGIN = "----- TASK02-SELFCHECK-BEGIN -----"
END = "----- TASK02-SELFCHECK-END -----"


def _check(name: str, condition: bool, detail: str) -> dict[str, object]:
    return {"check": name, "passed": bool(condition), "detail": detail}


def check_input_decoding() -> list[dict[str, object]]:
    """Verify key decoding without needing a real keyboard."""
    results: list[dict[str, object]] = []

    keyboard = TerminalInput()

    # EOF and the two common interrupt/EOF control bytes must map to a safe exit.
    for raw, label in ((("EOF"), "EOF"), ("\x04", "Ctrl+D/EOT"), ("\x1a", "Ctrl+Z/SUB")):
        keyboard._read_char = lambda _t, raw=raw: raw
        key = keyboard.read_key()
        results.append(
            _check(
                f"input/{label} -> safe exit",
                key == "q" and handle_key(ProbeState(), key) is True,
                f"decoded={key!r}",
            )
        )

    # Ctrl+C must be a distinct, still-exiting key.
    keyboard._read_char = lambda _t: "\x03"
    key = keyboard.read_key()
    results.append(
        _check("input/Ctrl+C -> CTRL_C + exit", key == "CTRL_C" and handle_key(ProbeState(), key) is True, f"decoded={key!r}")
    )

    # Enter, Tab and CR/LF variants.
    keyboard._read_char = lambda _t: "\r"
    enter_key = keyboard.read_key()
    results.append(_check("input/CR -> ENTER", enter_key == "ENTER", f"decoded={enter_key!r}"))

    keyboard._read_char = lambda _t: "\t"
    tab_key = keyboard.read_key()
    results.append(_check("input/TAB -> TAB", tab_key == "TAB", f"decoded={tab_key!r}"))

    # Arrow keys via CSI sequences must not be mistaken for commands.
    arrow = iter(["[", "A"])
    arrow_key = _read_escape_sequence(lambda _t: next(arrow, None))
    results.append(_check("input/CSI up -> UP", arrow_key == "UP", f"decoded={arrow_key!r}"))

    # Windows native extended-key form (0x00/0xE0 prefix + suffix letter).
    # A missing/slow suffix used to drop the key entirely, which is why arrow
    # keys failed in real PowerShell and conhost sessions.
    for raw, expected in (
        ("\x00H", "UP"),
        ("\x00P", "DOWN"),
        ("\x00K", "LEFT"),
        ("\x00M", "RIGHT"),
        ("\xe0H", "UP"),
        ("\xe0M", "RIGHT"),
    ):
        chars = iter(list(raw))
        keyboard = TerminalInput()
        keyboard._read_char = lambda _t, it=chars: next(it, None)
        decoded = keyboard.read_key()
        results.append(
            _check(
                f"input/Windows extended {raw!r} -> {expected}",
                decoded == expected,
                f"decoded={decoded!r}",
            )
        )

    # A late suffix must be retried rather than discarded.
    late = iter(["\x00", None, None, "H"])
    keyboard = TerminalInput()
    keyboard._read_char = lambda _t: next(late, None)
    late_key = keyboard.read_key()
    results.append(
        _check(
            "input/late extended suffix is retried",
            late_key == "UP",
            f"decoded={late_key!r} (must not be IGNORE)",
        )
    )

    # A prefix whose suffix never arrives must not become an unknown-key message.
    never = iter(["\x00"] + [None] * 8)
    keyboard = TerminalInput()
    keyboard._read_char = lambda _t: next(never, None)
    retry_key = keyboard.read_key()
    state = ProbeState()
    before_state = (state.page, state.selected, state.focus, state.message)
    exited = handle_key(state, retry_key)
    after_state = (state.page, state.selected, state.focus, state.message)
    results.append(
        _check(
            "input/exhausted prefix is inert",
            retry_key == "PREFIX_RETRY" and exited is False and before_state == after_state,
            f"decoded={retry_key!r}, exited={exited}, state_unchanged={before_state == after_state}",
        )
    )

    # Arrow keys must actually move the selection when focus is on choices.
    state = ProbeState()
    handle_key(state, "DOWN")
    moved = state.selected
    handle_key(state, "UP")
    back = state.selected
    results.append(
        _check(
            "action/arrow keys move selection",
            moved == 1 and back == 0,
            f"after DOWN selected={moved}, after UP selected={back}",
        )
    )

    # An OSC payload containing 'q' must be swallowed, never delivered as exit.
    osc = iter(["]", "0", ";", "q", "\x07", "n"])
    osc_key = _read_escape_sequence(lambda _t: next(osc, None))
    after = next(osc, None)
    results.append(
        _check(
            "input/OSC payload swallowed",
            osc_key == "IGNORE" and after == "n",
            f"decoded={osc_key!r}, next={after!r} (the 'q' inside OSC must not become input)",
        )
    )

    return results


def check_render_never_mutates_state() -> list[dict[str, object]]:
    """Rendering, focus change and resize must not commit any game action."""
    results: list[dict[str, object]] = []

    state = ProbeState()
    before = (state.page, state.selected, state.focus, state.message)

    for size in ((80, 24), (64, 20), (48, 16), (40, 10)):
        render_panel(MODEL, state, *size)
        render_panel(MODEL, state, *size, "truecolor")

    after = (state.page, state.selected, state.focus, state.message)
    results.append(
        _check(
            "render/does not mutate state",
            before == after,
            f"before={before} after={after}",
        )
    )

    # The state object must have exactly the UI-only fields; no clock or save.
    results.append(
        _check(
            "state/has no clock or save fields",
            set(state.__dict__) == {"page", "selected", "focus", "message"},
            f"fields={sorted(state.__dict__)}",
        )
    )

    # Selecting and previewing must still not advance any month or write data.
    state2 = ProbeState()
    handle_key(state2, "2")
    handle_key(state2, "ENTER")
    results.append(
        _check(
            "action/preview says no month, no save",
            "不扣月" in state2.message and "不存档" in state2.message,
            f"message={state2.message!r}",
        )
    )

    return results


def check_console_mode_restore() -> list[dict[str, object]]:
    """TerminalSession must restore console modes on a normal (non-TTY) refusal.

    In a redirected/non-TTY run TerminalSession.enter() must refuse rather than
    leave the console in a modified state. We assert the refusal and that
    restore() is a safe no-op afterwards.
    """
    results: list[dict[str, object]] = []

    session = TerminalSession()
    refused = False
    try:
        session.enter()
    except RuntimeError as exc:
        refused = "真实交互终端" in str(exc)
        detail = f"refused with: {exc}"
    else:
        detail = "unexpectedly entered (this check expects a non-TTY stream)"

    # Only meaningful when streams are not TTYs, which is the point of the check.
    if not (sys.stdin.isatty() and sys.stdout.isatty()):
        results.append(_check("session/refuses non-TTY", refused, detail))
    else:
        results.append(_check("session/is TTY so refusal not applicable", True, "running in a real terminal"))

    # restore() must be safe to call even if enter() never succeeded.
    try:
        session.restore()
        session.restore()
        safe = True
        detail2 = "restore() twice is safe"
    except Exception as exc:  # noqa: BLE001 - we are asserting on any exception
        safe = False
        detail2 = f"restore raised: {exc!r}"
    results.append(_check("session/restore is idempotent and safe", safe, detail2))

    return results


def check_escape_and_width() -> list[dict[str, object]]:
    """Control-sequence filtering and width accounting must stay correct."""
    results: list[dict[str, object]] = []

    cases = [
        ("\x1b[2J", "clear-screen payload removed", ""),
        ("\x1b]0;title\x07", "OSC title payload removed", ""),
        ("\x1b[31mred\x1b[0m", "SGR payload keeps only the text", "red"),
    ]
    for raw, label, expected in cases:
        cleaned = safe_text(raw)
        results.append(
            _check(
                f"escape/{label}",
                cleaned == expected and "\x1b" not in cleaned,
                f"raw={raw!r} cleaned={cleaned!r} expected={expected!r}",
            )
        )

    # The specific regression: the payload must be gone, not just the ESC byte.
    cleaned = safe_text("名\x1b[2J字")
    results.append(
        _check(
            "escape/payload cannot leak as text",
            "[2J" not in cleaned,
            f"cleaned={cleaned!r} (must not contain '[2J')",
        )
    )

    # Width must count CJK as 2 and combining marks as 0.
    results.append(
        _check("width/CJK counts 2", display_width("中") == 2, f"width('中')={display_width('中')}")
    )
    results.append(
        _check(
            "width/combining counts 0",
            display_width("e\u0301") == 1,
            f"width('e+combining')={display_width('e\u0301')}",
        )
    )

    return results


def build_report() -> dict[str, object]:
    sections = {
        "input_decoding": check_input_decoding(),
        "render_state_purity": check_render_never_mutates_state(),
        "console_mode_restore": check_console_mode_restore(),
        "escape_and_width": check_escape_and_width(),
    }
    flat = [item for items in sections.values() for item in items]
    failures = [item for item in flat if not item["passed"]]
    return {
        "checks_total": len(flat),
        "checks_passed": len(flat) - len(failures),
        "failures": failures,
        "sections": sections,
        "stderr_isatty": sys.stderr.isatty(),
        "stdout_isatty": sys.stdout.isatty(),
    }


def _print_human(report: dict[str, object]) -> None:
    for section, items in report["sections"].items():
        print(f"[{section}]")
        for item in items:
            mark = "PASS" if item["passed"] else "FAIL"
            print(f"  [{mark}] {item['check']}")
            print(f"         {item['detail']}")
        print()
    print(f"合计 {report['checks_passed']}/{report['checks_total']} 通过")


def main(argv: Iterable[str] | None = None) -> int:
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")

    parser = argparse.ArgumentParser(description="TASK-02 探针自检（不替代人工终端实测）")
    parser.add_argument("--json-only", action="store_true", help="只输出 JSON 证据块")
    args = parser.parse_args(list(argv) if argv is not None else None)

    report = build_report()

    if not args.json_only:
        _print_human(report)
        print()

    print(BEGIN)
    print(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True))
    print(END)

    return 1 if report["failures"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
