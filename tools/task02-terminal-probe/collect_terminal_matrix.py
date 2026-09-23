"""TASK-02 terminal matrix evidence collector.

Runs inside whatever terminal you launch it from and records reproducible facts
about that environment: reported size, colour tier, encoding, TTY status, and a
rendered snapshot. It performs no game action and writes no save data.

Usage (run this *from* the terminal under test):

    python collect_terminal_matrix.py --label "Windows Terminal"

It prints a machine-readable block between the EVIDENCE markers so results from
several terminals can be pasted into the TASK-02 record without hand-editing.
"""

from __future__ import annotations

import argparse
import json
import os
import platform
import shutil
import sys
from typing import Iterable

from interactive_probe import (
    COLOR_MODES,
    MIN_HEIGHT,
    MIN_WIDTH,
    MODEL,
    ProbeState,
    _auto_color,
    display_width,
    render_panel,
    safe_text,
)

BEGIN = "----- TASK02-EVIDENCE-BEGIN -----"
END = "----- TASK02-EVIDENCE-END -----"


def probe_environment() -> dict[str, object]:
    """Collect non-invasive facts about the current terminal."""
    stdout_tty = bool(getattr(sys.stdout, "isatty", lambda: False)())
    stdin_tty = bool(getattr(sys.stdin, "isatty", lambda: False)())
    size = shutil.get_terminal_size(fallback=(0, 0))
    encoding = getattr(sys.stdout, "encoding", None) or "unknown"

    cjk_ok = False
    cjk_detail = ""
    if stdout_tty:
        # Render one Chinese row and confirm it survives the encode/decode path.
        sample = "问答校验：道号清微，修为五十一。"
        try:
            round_tripped = sample.encode(encoding, errors="strict").decode(encoding, errors="strict")
            cjk_ok = round_tripped == sample
            cjk_detail = "编码往返一致" if cjk_ok else f"往返不一致：{round_tripped!r}"
        except (UnicodeEncodeError, UnicodeDecodeError, LookupError) as exc:
            cjk_ok = False
            cjk_detail = f"编码往返失败：{exc.__class__.__name__}"
    else:
        cjk_detail = "非 TTY，跳过编码往返检查"

    return {
        "label": None,  # filled by caller
        "platform": platform.platform(),
        "python": sys.version.split()[0],
        "stdout_isatty": stdout_tty,
        "stdin_isatty": stdin_tty,
        "reported_size": f"{size.columns}x{size.lines}",
        "meets_min_size": size.columns >= MIN_WIDTH and size.lines >= MIN_HEIGHT,
        "min_size": f"{MIN_WIDTH}x{MIN_HEIGHT}",
        "encoding": encoding,
        "cjk_roundtrip_ok": cjk_ok,
        "cjk_detail": cjk_detail,
        "term": os.environ.get("TERM", ""),
        "term_program": os.environ.get("TERM_PROGRAM", ""),
        "colorterm": os.environ.get("COLORTERM", ""),
        "wt_session": os.environ.get("WT_SESSION", ""),
        "no_color": os.environ.get("NO_COLOR", ""),
        "color_tier_selected": _auto_color(),
        "tier_manually_supported": [],
    }


def snapshot_checks(width: int, height: int, mode: str) -> dict[str, object]:
    """Verify rendered rows keep the exact requested geometry."""
    rows = render_panel(MODEL, ProbeState(), width, height, mode)
    widths = [display_width(row) for row in rows]
    joined = "\n".join(rows)
    at_least_min = width >= MIN_WIDTH and height >= MIN_HEIGHT
    # Above the minimum the menu and quit key must be present; below it only the
    # resize notice is allowed and choices must stay hidden.
    if at_least_min:
        expected_ok = "q退" in joined and "1. 查看传音符" in joined
        expectation = "菜单与退出键可见"
    else:
        expected_ok = "窗口过小" in joined and "1. 查看传音符" not in joined
        expectation = "仅显示尺寸提示"
    return {
        "size": f"{width}x{height}",
        "mode": mode,
        "rows": len(rows),
        "row_count_ok": len(rows) == height,
        "all_rows_exact_width": all(w == width for w in widths),
        "widths_uniform": len(set(widths)) == 1,
        "actual_width": widths[0] if widths else 0,
        "expectation": expectation,
        "expectation_met": expected_ok,
        "escape_free": "\x1b" not in joined,
    }


def build_report(label: str) -> dict[str, object]:
    env = probe_environment()
    env["label"] = label

    acceptance = [
        snapshot_checks(w, h, "none")
        for (w, h) in ((80, 24), (64, 20), (48, 16), (40, 10))
    ]
    color_tiers = [
        {
            "mode": mode,
            "has_escape": any("\x1b[" in row for row in render_panel(MODEL, ProbeState(), 80, 24, mode)),
            "uses_truecolor": "38;2;" in "\n".join(render_panel(MODEL, ProbeState(), 80, 24, mode)),
            "uses_256": "38;5;" in "\n".join(render_panel(MODEL, ProbeState(), 80, 24, mode)),
        }
        for mode in COLOR_MODES
    ]

    return {
        "label": label,
        "environment": env,
        "layout_acceptance": acceptance,
        "color_tiers": color_tiers,
    }


def _print_human(report: dict[str, object]) -> None:
    env = report["environment"]
    print(f"终端标签：{report['label']}")
    print(f"平台：{env['platform']}")
    print(f"Python：{env['python']}")
    print(f"stdout TTY：{env['stdout_isatty']}  stdin TTY：{env['stdin_isatty']}")
    print(f"报告尺寸：{env['reported_size']}（最小要求 {env['min_size']}，满足={env['meets_min_size']}）")
    print(f"输出编码：{env['encoding']}  中文往返：{env['cjk_detail']}")
    print(f"TERM={env['term']!r} TERM_PROGRAM={env['term_program']!r} COLORTERM={env['colorterm']!r}")
    print(f"WT_SESSION={'有' if env['wt_session'] else '无'} NO_COLOR={env['no_color']!r}")
    print(f"自动色档：{env['color_tier_selected']}")
    print()
    print("布局检查：")
    for row in report["layout_acceptance"]:
        ok = row["row_count_ok"] and row["all_rows_exact_width"] and row["escape_free"] and row["expectation_met"]
        print(
            f"  [{'PASS' if ok else 'FAIL'}] {row['size']:>6} mode={row['mode']:<4} "
            f"rows={row['rows']} width={row['actual_width']} "
            f"expect={row['expectation']}({row['expectation_met']}) escape_free={row['escape_free']}"
        )
    print()
    print("色档检查：")
    for row in report["color_tiers"]:
        print(
            f"  {row['mode']:<10} escape={str(row['has_escape']):<5} "
            f"24bit={str(row['uses_truecolor']):<5} 256={row['uses_256']}"
        )


def main(argv: Iterable[str] | None = None) -> int:
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")

    parser = argparse.ArgumentParser(description="TASK-02 终端矩阵证据采集（不执行游戏行动）")
    parser.add_argument("--label", default="未知终端", help="终端名称，例如 'Windows Terminal'")
    parser.add_argument("--json-only", action="store_true", help="只输出 JSON 证据块")
    args = parser.parse_args(list(argv) if argv is not None else None)

    report = build_report(args.label)
    if not args.json_only:
        _print_human(report)
        print()

    print(BEGIN)
    print(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True))
    print(END)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
