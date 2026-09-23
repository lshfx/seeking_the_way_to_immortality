"""Report terminal identity from environment variables that actually exist.

``WT_SESSION`` proves Windows Terminal. ``TERM``/``TERM_PROGRAM``/``COLORTERM``
are unset in a default Windows Terminal on Windows, which is why an earlier
run recorded ``WT_SESSION=有`` yet saw every colour-related variable empty. This
tool prints a definitive verdict instead of leaving the reader to reconcile the
combination by hand.

    python detect_terminal.py
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys

BEGIN = "----- TASK02-TERMINALID-BEGIN -----"
END = "----- TASK02-TERMINALID-END -----"

# Variables a terminal may set to identify itself.
WATCHED = (
    "WT_SESSION",
    "WT_PROFILE_ID",
    "TERM",
    "TERM_PROGRAM",
    "TERM_PROGRAM_VERSION",
    "COLORTERM",
    "VSCODE_INJECTION",
    "VSCODE_GIT_IPC_HANDLE",
    "ConEmuANSI",
    "ALACRITTY_LOG",
    "WEZTERM_EXECUTABLE",
    "NO_COLOR",
    "ANSICON",
)


def detect() -> dict[str, object]:
    env = {name: os.environ.get(name, "") for name in WATCHED}

    evidence: list[str] = []
    if env["WT_SESSION"]:
        evidence.append("WT_SESSION")
    if env["WT_PROFILE_ID"]:
        evidence.append("WT_PROFILE_ID")
    if env["TERM_PROGRAM"]:
        evidence.append(f"TERM_PROGRAM={env['TERM_PROGRAM']}")
    if env["WEZTERM_EXECUTABLE"]:
        evidence.append("WEZTERM_EXECUTABLE")
    if env["ALACRITTY_LOG"]:
        evidence.append("ALACRITTY_LOG")
    if env["ConEmuANSI"]:
        evidence.append("ConEmuANSI")

    if env["WT_SESSION"] or env["WT_PROFILE_ID"]:
        verdict = "Windows Terminal"
        note = "WT_SESSION/WT_PROFILE_ID 存在，这是 Windows Terminal 的权威标志。"
    elif env["VSCODE_INJECTION"] or env["VSCODE_GIT_IPC_HANDLE"]:
        verdict = "VS Code 集成终端"
        note = "检测到 VS Code 注入变量。"
    elif env["WEZTERM_EXECUTABLE"]:
        verdict = "WezTerm"
        note = ""
    elif env["ALACRITTY_LOG"]:
        verdict = "Alacritty"
        note = ""
    elif env["ConEmuANSI"]:
        verdict = "ConEmu / Cmder"
        note = ""
    elif env["TERM_PROGRAM"]:
        verdict = f"未知，TERM_PROGRAM={env['TERM_PROGRAM']}"
        note = "按 TERM_PROGRAM 无法归类，请记录原始值。"
    elif env["TERM"]:
        verdict = "非专属标志终端（按 TERM 判断）"
        note = "只有 TERM，无法确定是 Windows Terminal / conhost / 其它。"
    else:
        verdict = "传统 conhost 或未标识宿主"
        note = (
            "没有 WT_SESSION，也没有 TERM/TERM_PROGRAM/COLORTERM。"
            "这是 cmd.exe 经典窗口或旧版 PowerShell 窗口（conhost.exe）的典型特征。"
        )

    return {
        "verdict": verdict,
        "note": note,
        "evidence": evidence,
        "env": env,
        "reported_size": f"{shutil.get_terminal_size(fallback=(0, 0)).columns}x"
        f"{shutil.get_terminal_size(fallback=(0, 0)).lines}",
        "stdout_isatty": bool(getattr(sys.stdout, "isatty", lambda: False)()),
        "stdin_isatty": bool(getattr(sys.stdin, "isatty", lambda: False)()),
        "encoding": getattr(sys.stdout, "encoding", None) or "unknown",
    }


def main(argv: list[str] | None = None) -> int:
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")

    parser = argparse.ArgumentParser(description="TASK-02 终端身份判定")
    parser.add_argument("--json-only", action="store_true")
    args = parser.parse_args(argv)

    info = detect()
    if not args.json_only:
        print(f"判定：{info['verdict']}")
        if info["note"]:
            print(f"依据：{info['note']}")
        print(f"证据变量：{', '.join(info['evidence']) if info['evidence'] else '（无）'}")
        print()
        print("全部候选环境变量：")
        for name, value in info["env"].items():
            shown = value if value else "（未设置）"
            print(f"  {name:<24} {shown}")
        print()
        print(f"尺寸：{info['reported_size']}  stdout TTY：{info['stdout_isatty']}  "
              f"stdin TTY：{info['stdin_isatty']}  编码：{info['encoding']}")
        print()

    print(BEGIN)
    print(json.dumps(info, ensure_ascii=False, indent=2, sort_keys=True))
    print(END)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
