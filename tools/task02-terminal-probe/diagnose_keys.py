"""Capture the raw characters a real terminal sends for each key.

TASK-02 found that arrow keys did not move the selection in PowerShell or
conhost, while digits and W/S did. The decoder handles the documented Windows
``\\x00``/``\\xe0`` prefix form, so the open question is what the terminal
*actually* transmits. This tool answers that instead of guessing.

Run it in the terminal under test, press the requested keys, then copy the
report back.

    python diagnose_keys.py

Press Ctrl+C (or 'q' when asked) to finish.

Windows delivers extended keys as a *prefix before the letter*: ``\\x00H`` is
Up, ``\\x00P`` is Down, and so on -- note that Up is the only one whose byte
order makes the mapping look obvious, which is exactly why the first version
of this tool mis-decoded the rest.

Two earlier defects in this tool, both fixed and covered by tests:

1. The raw dump printed each character's UTF-16 **low byte first**, so a real
   ``H`` was displayed as ``48 00`` and the order looked reversed.
2. :func:`classify` compared the captured text against ``"\\x00H"`` while the
   capture returns the prefix as ``U+00E0`` (``msvcrt.getwch`` maps the second
   keyboard byte to ``à``). Real PowerShell/conhost input therefore never
   matched, and every key except the first was reported as "未识别".
"""

from __future__ import annotations

import argparse
import os
import sys
import time
from typing import Iterable

from interactive_probe import TerminalSession

BEGIN = "----- TASK02-KEYDIAG-BEGIN -----"
END = "----- TASK02-KEYDIAG-END -----"

# Human-readable names for the control codes we expect to see.
CONTROL_NAMES = {
    "\x00": "NUL(0x00)",
    "\xe0": "EXT-80(0xE0)",
    "\x1b": "ESC(0x1B)",
    "\r": "CR",
    "\n": "LF",
    "\t": "TAB",
    "\x03": "ETX(Ctrl+C)",
    "\x04": "EOT(Ctrl+D)",
    "\x1a": "SUB(Ctrl+Z)",
    "\x08": "BS",
    "\x7f": "DEL",
}

# Windows console key codes: an extended key arrives as a prefix character
# followed by a lowercase letter. Up happens to be "H", which is why a naive
# reading of a hex dump can look like the bytes are reversed.
EXTENDED_PREFIXES = ("\x00", "\xe0")
EXTENDED_SUFFIXES = {
    "H": ("UP", "方向键 上"),
    "P": ("DOWN", "方向键 下"),
    "K": ("LEFT", "方向键 左"),
    "M": ("RIGHT", "方向键 右"),
    "G": ("HOME", "Home"),
    "O": ("END", "End"),
    "I": ("PAGEUP", "PageUp"),
    "Q": ("PAGEDOWN", "PageDown"),
    "R": ("INSERT", "Insert"),
    "S": ("DELETE", "Delete"),
    "\\": ("CLEAR", "小键盘 5"),
    "/": ("HELP", "Help"),
}

PROMPTS = [
    ("up", "方向键 上 / Up arrow"),
    ("down", "方向键 下 / Down arrow"),
    ("left", "方向键 左 / Left arrow"),
    ("right", "方向键 右 / Right arrow"),
    ("enter", "Enter"),
    ("tab", "Tab"),
    ("digit1", "数字键 1"),
    ("w", "字母 w"),
]


def describe(ch: str) -> str:
    if ch in CONTROL_NAMES:
        return CONTROL_NAMES[ch]
    if ch == " ":
        return "SPACE"
    code = ord(ch)
    if code < 32:
        return f"CTRL(0x{code:02X})"
    return f"{ch!r}(U+{code:04X})"


def read_raw(timeout: float = 8.0, tail: float = 0.4) -> list[str]:
    """Collect the characters belonging to a single keypress.

    The earlier version re-armed a 150 ms tail window on *every* character it
    read, so pressing several keys quickly merged them into one capture. In the
    first real run that shifted every result by one prompt: the ``enter`` row
    actually contained the Right-arrow sequence and the ``tab`` row contained
    a stray ``P``. Wait for the first character, drain the burst, and stop.
    """
    import msvcrt

    captured: list[str] = []
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if msvcrt.kbhit():
            break
        time.sleep(0.005)
    else:
        return captured

    captured.append(msvcrt.getwch())
    tail_deadline = time.monotonic() + tail
    while time.monotonic() < tail_deadline:
        if msvcrt.kbhit():
            captured.append(msvcrt.getwch())
        else:
            time.sleep(0.005)
    return captured


def read_raw_timed(timeout: float = 8.0, gap: float = 0.6) -> list[dict[str, object]]:
    """Capture a keypress **with inter-character arrival timing**.

    Ordering arguments about Windows extended keys (which byte is the prefix,
    which is the suffix) cannot be settled by looking at a dump where both
    bytes are printed in a single line -- that is exactly how the earlier
    `48 E0` reading was mistaken for a reversed byte order. Timing settles it:
    the character that arrives *first* is the one the terminal emitted first.
    """
    import msvcrt

    captured: list[dict[str, object]] = []
    started = time.monotonic()
    deadline = started + timeout
    while time.monotonic() < deadline and not captured:
        if msvcrt.kbhit():
            now = time.monotonic()
            captured.append({"ch": msvcrt.getwch(), "after_ms": round((now - started) * 1000, 1)})
            break
        time.sleep(0.002)
    if not captured:
        return captured

    while True:
        now = time.monotonic()
        if msvcrt.kbhit():
            captured.append({"ch": msvcrt.getwch(), "after_ms": round((now - started) * 1000, 1)})
            continue
        if time.monotonic() - started > gap:
            break
        time.sleep(0.002)
    return captured


def hexdump(chars: list[str]) -> str:
    """Print each captured character as a single logical byte.

    ``msvcrt.getwch`` returns one ``str`` per keyboard byte, so listing the
    UTF-16 code unit's bytes would reverse the visual order and show a real
    ``H`` as ``48 00``. Print the code point instead: for these inputs it is
    always in ``0x00``-``0xFF`` and reads exactly like the byte stream.
    """
    return " ".join(f"{ord(ch):02X}" for ch in chars)


def _hex_text(chars: list[str]) -> str:
    return hexdump(chars) or "(无)"


def classify(chars: list[str]) -> str:
    """Map captured characters to a key name.

    The extended-key prefix is normalised first: ``msvcrt.getwch`` reports the
    second keyboard byte ``0xE0`` as the character ``U+00E0``, so a literal
    comparison against ``"\\xe0H"`` never matches real input.
    """
    joined = "".join(chars)
    if not joined:
        return "TIMEOUT(未收到任何字符)"
    first = ord(joined[0])
    if first == 0x00 or first == 0xE0:
        if len(joined) == 1:
            return "仅收到扩展键前缀，未收到后缀（按键被拆开或丢字节）"
        suffix = joined[1:]
        if suffix in EXTENDED_SUFFIXES:
            name, zh = EXTENDED_SUFFIXES[suffix]
            return f"{name}（Windows 扩展键：前缀 0x{first:02X} + '{suffix}' / {zh}）"
        return f"扩展键前缀 0x{first:02X} + 未知后缀 {suffix!r}"
    if joined.startswith("\x1b["):
        tail = joined[2:]
        final = tail[-1:] if tail else ""
        ansi = {
            "A": "UP", "B": "DOWN", "C": "RIGHT", "D": "LEFT",
            "H": "HOME", "F": "END",
            "~": "PAGEUP/PAGEDOWN/INSERT/DELETE(取决于数字前缀)",
        }
        return f"CSI 序列：{tail!r} → {ansi.get(final, '未映射')}（ANSI/VT 格式）"
    if joined == "\x1b":
        return "裸 ESC"
    return "未识别"


ORDERED_KEYS = tuple(
    sorted({name for name, _ in EXTENDED_SUFFIXES.values()})
)


def _is_decode(classification: str) -> bool:
    return classification.split("（")[0] in ORDERED_KEYS


def _timing_verdict(entries: list[dict[str, object]]) -> str:
    """Decide the extended-key byte order from arrival order, not from a dump.

    The order-independent question is: of the two characters captured for an
    arrow key, which one is the ``H``/``P``/``K``/``M`` letter and which is the
    ``0xE0`` prefix, and which arrived first? Whichever arrives first is the
    byte the terminal transmitted first.
    """
    firsts: list[str] = []
    for entry in entries:
        chars = entry["chars"]
        if not chars:
            continue
        first = ord(chars[0]["ch"])  # type: ignore[index]
        if len(chars) == 1:
            firsts.append("PREFIX_ONLY")
        elif first <= 0xFF and first not in (0x00, 0xE0):
            firsts.append("LETTER_FIRST")
        elif first in (0x00, 0xE0):
            firsts.append("PREFIX_FIRST")
        else:
            firsts.append("OTHER")

    if not firsts or all(f == "PREFIX_ONLY" for f in firsts):
        return "样本不足：只捕获到前缀，无法判定顺序。请重跑并放慢按键节奏。"
    if all(f == "LETTER_FIRST" for f in firsts):
        return (
            "**后缀字母先到**：终端发送的是「字母 + 0xE0」，即顺序为 `H E0`。"
            "这说明 msvcrt 读到的是与文档相反的顺序，解码器需要接受两种顺序。"
        )
    if all(f == "PREFIX_FIRST" for f in firsts):
        return (
            "**0xE0 前缀先到**：终端发送的是「0xE0 + 字母」，与 Windows 文档一致。"
            "此时若仍看到 `48 E0`，说明字符在采集/打印环节被重排，而不是终端发错。"
        )
    return f"顺序不一致（{', '.join(firsts)}），需要人工检查。"


def main(argv: Iterable[str] | None = None) -> int:
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")

    parser = argparse.ArgumentParser(description="TASK-02 按键原始字符诊断")
    parser.add_argument("--json-only", action="store_true")
    parser.add_argument(
        "--timing",
        action="store_true",
        help="记录每个字符的到达时刻，用于判定扩展键的字节顺序",
    )
    args = parser.parse_args(list(argv) if argv is not None else None)

    if os.name != "nt":
        print("此诊断工具只支持 Windows（依赖 msvcrt）。", file=sys.stderr)
        return 2
    if not (sys.stdin.isatty() and sys.stdout.isatty()):
        print("需要在真实交互终端中运行；重定向模式下无法捕获按键。", file=sys.stderr)
        return 2

    session = TerminalSession()
    results: list[dict[str, object]] = []
    try:
        session.enter()
        for key_id, label in PROMPTS:
            sys.stdout.write("\x1b[2J\x1b[H")
            sys.stdout.write(f"请按下：{label}\r\n")
            sys.stdout.write("（8 秒内无输入则记为 TIMEOUT）\r\n")
            sys.stdout.flush()
            if args.timing:
                timed = read_raw_timed(8.0)
                chars = [t["ch"] for t in timed]  # type: ignore[misc]
                entry = {
                    "key": key_id,
                    "label": label,
                    "raw_chars": chars,
                    "raw_described": [describe(c) for c in chars],
                    "raw_hex": hexdump(chars),
                    "timing_ms": [t["after_ms"] for t in timed],
                    "classification": classify(chars),
                }
                entry["decoded"] = (
                    entry["classification"].split("（")[0]
                    if _is_decode(entry["classification"])
                    else None
                )
                results.append(entry)
                arrival = " -> ".join(
                    f"{t['ch']!r}@{t['after_ms']}ms" for t in timed  # type: ignore[index]
                ) or "(无)"
                sys.stdout.write(f"\r\n到达顺序：{arrival}\r\n")
                sys.stdout.write(f"判定：{entry['classification']}\r\n")
            else:
                chars = read_raw(8.0)
                classification = classify(chars)
                entry = {
                    "key": key_id,
                    "label": label,
                    "raw_chars": chars,
                    "raw_described": [describe(c) for c in chars],
                    "raw_hex": hexdump(chars),
                    "classification": classification,
                    "decoded": classification.split("（")[0] if _is_decode(classification) else None,
                }
                results.append(entry)
                sys.stdout.write(f"\r\n收到：{_hex_text(chars)}\r\n")
                sys.stdout.write(f"判定：{classification}\r\n")
            sys.stdout.flush()
            time.sleep(0.6)
    except KeyboardInterrupt:
        pass
    finally:
        session.restore()

    print()
    if args.timing:
        print("按键原始字符 + 到达时刻：")
        for entry in results:
            print(f"  {entry['key']:<8} {_hex_text(entry['raw_chars']):<10} "
                  f"{entry['timing_ms']}  {entry['classification']}")
        print()
        print("顺序判定：" + _timing_verdict(results))
    else:
        print("按键原始字符汇总（每项一个码位，等价于一个键盘字节）：")
        for entry in results:
            print(f"  {entry['key']:<8} {_hex_text(entry['raw_chars']):<16} {entry['classification']}")

    print()
    print(BEGIN)
    import json

    payload: dict[str, object] = {"results": results, "os": os.name}
    if args.timing:
        payload["timing_verdict"] = _timing_verdict(results)
    print(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True))
    print(END)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
