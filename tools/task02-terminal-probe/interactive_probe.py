"""Dependency-free interactive probe for TASK-02 terminal validation.

This is deliberately a UI experiment, not game code: Enter only changes the
diagnostic message. No world clock, random generator, or save file exists here.
"""

from __future__ import annotations

import argparse
import os
import shutil
import sys
import time
import unicodedata
from dataclasses import dataclass
from typing import Callable, Iterable


MIN_WIDTH = 48
MIN_HEIGHT = 16


@dataclass(frozen=True)
class Choice:
    label: str


@dataclass(frozen=True)
class PanelModel:
    """Read-only display contract draft; this object contains no game rules."""

    title: str
    status: str
    scene: str
    story_pages: tuple[str, ...]
    risk: str
    choices: tuple[Choice, ...]
    details: tuple[str, ...]


@dataclass
class ProbeState:
    page: int = 0
    selected: int = 0
    focus: str = "choices"
    message: str = "仅验证终端交互；当前无游戏状态、无存档。"


MODEL = PanelModel(
    title="《问道长生》 - 终端交互试验",
    status="清微 | 炼气中期 | 修为 51/100",
    scene="青岳 - 洞府静室",
    story_pages=(
        "你在蒲团上醒来，门外传音符微微发烫。",
        "传音符渐渐安静。翻页只更新画面，不推进游戏月份。",
        "窗口可以缩放；当前选择和场景页会原样保留。",
    ),
    risk="风险：仅为界面演示；不推进月份，不写存档。",
    choices=(Choice("查看传音符"), Choice("继续修炼"), Choice("返回洞府")),
    details=(
        "气血 76/100 | 灵力 67/100 | 灵石 480",
        "本机离线探针 | 无网络 | 无游戏结算 | 无存档",
    ),
)


def safe_text(value: str) -> str:
    """Remove terminal control and non-printing format characters from text."""
    return "".join(
        ch
        for ch in str(value)
        if ch in "\n\r" or (unicodedata.category(ch)[0] != "C" and ch not in "\x1b\x07")
    )


def char_width(ch: str) -> int:
    category = unicodedata.category(ch)
    if category in {"Mn", "Me", "Cf"} or category.startswith("C"):
        return 0
    return 2 if unicodedata.east_asian_width(ch) in {"W", "F"} else 1


def display_width(value: str) -> int:
    return sum(char_width(ch) for ch in safe_text(value))


def clip_display(value: str, width: int) -> str:
    if width <= 0:
        return ""
    result: list[str] = []
    used = 0
    for ch in safe_text(value):
        size = char_width(ch)
        if size == 0:
            if result:
                result.append(ch)
            continue
        if used + size > width:
            break
        result.append(ch)
        used += size
    return "".join(result)


def wrap_display(value: str, width: int) -> list[str]:
    if width < 1:
        return [""]
    lines: list[str] = []
    current: list[str] = []
    used = 0
    for ch in safe_text(value):
        if ch in "\r\n":
            lines.append("".join(current))
            current, used = [], 0
            continue
        size = char_width(ch)
        if size == 0:
            if current:
                current.append(ch)
            continue
        if size > width:
            ch, size = "?", 1
        if current and used + size > width:
            lines.append("".join(current))
            current, used = [], 0
        current.append(ch)
        used += size
    if current or not lines:
        lines.append("".join(current))
    return lines


def color_code(mode: str, tone: str) -> str:
    if mode == "none":
        return ""
    if mode == "basic":
        codes = {"title": "1;36", "status": "36", "risk": "1;33", "focus": "1;32"}
        return f"\x1b[{codes.get(tone, '37')}m"
    colors = {
        "title": "38;2;116;196;181",
        "status": "38;2;183;196;207",
        "risk": "38;2;235;179;92",
        "focus": "38;2;130;210;157",
    }
    return f"\x1b[{colors.get(tone, '38;2;212;212;212')}m"


def colorize(value: str, mode: str, tone: str = "body") -> str:
    code = color_code(mode, tone)
    return f"{code}{value}\x1b[0m" if code else value


def _frame_row(value: str, width: int, mode: str, tone: str = "body") -> str:
    inner = width - 2
    value = safe_text(value).replace("\r", " ").replace("\n", " ")
    if display_width(value) > inner and inner >= 4:
        clipped = clip_display(value, inner - 3) + "..."
    else:
        clipped = clip_display(value, inner)
    padding = " " * max(0, inner - display_width(clipped))
    return "|" + colorize(clipped, mode, tone) + padding + "|"


def _frame_border(width: int) -> str:
    return "+" + ("-" * max(0, width - 2)) + "+" if width >= 2 else "-" * width


def _boxed(lines: list[tuple[str, str]], width: int, height: int, mode: str) -> list[str]:
    result = [_frame_border(width)]
    result.extend(_frame_row(text, width, mode, tone) for text, tone in lines)
    result.append(_frame_border(width))
    if len(result) > height:
        # Keep the warning and controls reachable if a future edit adds content.
        result = result[: height - 1] + [_frame_border(width)]
    while len(result) < height:
        result.insert(-1, _frame_row("", width, mode))
    return result


def _notice(width: int, height: int, message: str, mode: str) -> list[str]:
    if width >= 4:
        lines = [(line, "risk") for line in wrap_display(message, width - 4)]
        return _boxed(lines, width, height, mode)
    rows = [clip_display(line, width) for line in wrap_display(message, width)]
    return (rows + [""] * height)[:height]


def render_panel(
    model: PanelModel,
    state: ProbeState,
    width: int,
    height: int,
    mode: str = "none",
) -> list[str]:
    width = max(1, width)
    height = max(1, height)
    if width < MIN_WIDTH or height < MIN_HEIGHT:
        return _notice(width, height, f"窗口过小：需至少 {MIN_WIDTH}x{MIN_HEIGHT}；未执行行动。", mode)

    page = max(0, min(state.page, len(model.story_pages) - 1))
    lines: list[tuple[str, str]] = [
        (model.title, "title"),
        (model.status, "status"),
        (_frame_border(width)[1:-1], "body"),
        (model.scene, "title"),
    ]
    story_tone = "focus" if state.focus == "details" else "body"
    lines.extend((line, story_tone) for line in wrap_display(model.story_pages[page], width - 2))
    lines.append((model.risk, "risk"))
    lines.append((_frame_border(width)[1:-1], "body"))
    for index, choice in enumerate(model.choices):
        marker = ">" if index == state.selected and state.focus == "choices" else " "
        lines.append((f"{marker} {index + 1}. {choice.label}", "focus" if marker == ">" else "body"))
    focus_text = "选项" if state.focus == "choices" else "说明"
    lines.append((f"焦点：{focus_text}", "focus"))
    lines.append(("反馈：" + state.message, "body"))
    if width >= 64 and height >= 20:
        detail_tone = "focus" if state.focus == "details" else "status"
        lines.extend((detail, detail_tone) for detail in model.details)
    lines.append(("W/S移 1-3选 Enter预览 n/p页 Tab焦点 q退", "body"))
    lines.append((f"场景页 {page + 1}/{len(model.story_pages)} - 翻页不会结算", "body"))

    rows = _boxed(lines, width, height, mode)
    # The compact acceptance size is 48x16. If content grows, preserve the
    # choices and controls by falling back to the explicit small-window notice.
    if len(lines) + 2 > height:
        return _notice(width, height, "布局内容过多；请扩大窗口；未执行行动。", mode)
    return rows


def handle_key(state: ProbeState, key: str, model: PanelModel = MODEL) -> bool:
    """Apply a UI-only key; return True when the probe should exit."""
    if key in {"q", "Q", "CTRL_C"}:
        return True
    if key == "TAB":
        state.focus = "details" if state.focus == "choices" else "choices"
        state.message = "焦点已切换；尚未执行行动。"
    elif key in {"LEFT", "p", "P", "PAGEUP"}:
        state.page = (state.page - 1) % len(model.story_pages)
        state.message = "仅切换展示页；世界月份未启用。"
    elif key in {"RIGHT", "n", "N", "PAGEDOWN"}:
        state.page = (state.page + 1) % len(model.story_pages)
        state.message = "仅切换展示页；世界月份未启用。"
    elif key in {"UP", "DOWN", "w", "W", "s", "S"} and state.focus == "choices":
        delta = -1 if key in {"UP", "w", "W"} else 1
        state.selected = (state.selected + delta) % len(model.choices)
    elif key in {"1", "2", "3"} and state.focus == "choices":
        index = int(key) - 1
        if index < len(model.choices):
            state.selected = index
    elif key == "ENTER" and state.focus == "choices":
        label = model.choices[state.selected].label
        state.message = f"已预览“{label}”；仅界面测试，不扣月、不修改角色、不存档。"
    elif key == "ESC":
        state.focus = "choices"
        state.message = "已取消并返回选项；没有执行行动。"
    elif key == "?":
        state.focus = "details"
        state.message = "方向键/数字键选择；Enter仅预览；n/p翻页；q退出。"
    else:
        # ESC-prefixed terminal sequences are consumed by the decoder and
        # arrive here only as ESC/known navigation keys, never as commands.
        state.message = "按键已忽略；游戏状态未改变。"
    return False


def _read_escape_sequence(read_char: Callable[[float], str | None]) -> str:
    introducer = read_char(0.04)
    if introducer is None:
        return "ESC"
    if introducer in {"[", "O"}:
        sequence: list[str] = []
        for _ in range(64):
            char = read_char(0.04)
            if char is None:
                return "IGNORE"
            sequence.append(char)
            if "@" <= char <= "~":
                break
        tail = "".join(sequence)
        if introducer in {"[", "O"}:
            final = tail[-1:] if tail else ""
            if final == "A":
                return "UP"
            if final == "B":
                return "DOWN"
            if final == "C":
                return "RIGHT"
            if final == "D":
                return "LEFT"
            if tail.endswith("5~"):
                return "PAGEUP"
            if tail.endswith("6~"):
                return "PAGEDOWN"
            if tail == "200~":
                return "PASTE_START"
        return "IGNORE"
    if introducer in {"]", "P", "X", "^", "_"}:
        # Consume OSC/DCS-like strings through BEL or ST. Their payload is
        # always discarded so embedded q/1/2 characters cannot become input.
        previous = ""
        for _ in range(4096):
            char = read_char(0.04)
            if char is None:
                break
            if introducer == "]" and char == "\x07":
                break
            if previous == "\x1b" and char == "\\":
                break
            previous = char
        return "IGNORE"
    if introducer in {"(", ")", "#", "%"}:
        read_char(0.04)
    return "IGNORE"


def _discard_bracketed_paste(read_char: Callable[[float], str | None]) -> None:
    marker = "\x1b[201~"
    matched = 0
    for _ in range(65536):
        char = read_char(0.2)
        if char is None:
            return
        if char == marker[matched]:
            matched += 1
            if matched == len(marker):
                return
        else:
            matched = 1 if char == marker[0] else 0


class TerminalInput:
    def _read_char(self, timeout: float) -> str | None:
        if os.name == "nt":
            import msvcrt

            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                if msvcrt.kbhit():
                    return msvcrt.getwch()
                time.sleep(0.005)
            return None
        import select

        ready, _, _ = select.select([sys.stdin.fileno()], [], [], timeout)
        if not ready:
            return None
        raw = os.read(sys.stdin.fileno(), 1)
        if not raw:
            return "EOF"
        try:
            return raw.decode("ascii")
        except UnicodeDecodeError:
            return "IGNORE"

    def read_key(self, timeout: float = 0.1) -> str | None:
        char = self._read_char(timeout)
        if char is None:
            return None
        if char in {"EOF", "\x04", "\x1a"}:
            return "q"
        if char in {"\x00", "\xe0"} and os.name == "nt":
            suffix = self._read_char(0.05)
            return {
                "H": "UP", "P": "DOWN", "K": "LEFT", "M": "RIGHT",
                "I": "PAGEUP", "Q": "PAGEDOWN",
            }.get(suffix, "IGNORE")
        if char == "\x1b":
            key = _read_escape_sequence(self._read_char)
            if key == "PASTE_START":
                _discard_bracketed_paste(self._read_char)
                return "IGNORE"
            return key
        if char == "\x03":
            return "CTRL_C"
        if char in {"\r", "\n"}:
            return "ENTER"
        if char == "\t":
            return "TAB"
        if char.isprintable() and len(char) == 1 and ord(char) < 128:
            return char
        return "IGNORE"


class TerminalSession:
    """Temporarily disable line echo and restore the original console modes."""

    def __init__(self) -> None:
        self._old_termios = None
        self._win_kernel = None
        self._win_in_handle = None
        self._win_out_handle = None
        self._win_in_mode = None
        self._win_out_mode = None
        self._entered = False
        self._ansi_enabled = False

    def enter(self) -> None:
        if not sys.stdin.isatty() or not sys.stdout.isatty():
            raise RuntimeError("需要在真实交互终端中运行；重定向或管道模式不支持按键试验。")
        if os.name == "nt":
            self._enter_windows()
        else:
            import termios

            fd = sys.stdin.fileno()
            self._old_termios = termios.tcgetattr(fd)
            current = termios.tcgetattr(fd)
            current[3] &= ~(termios.ICANON | termios.ECHO)
            current[6][termios.VMIN] = 0
            current[6][termios.VTIME] = 0
            termios.tcsetattr(fd, termios.TCSANOW, current)
            self._entered = True
            self._ansi_enabled = True
        sys.stdout.write("\x1b[?25l\x1b[?2004h\x1b[2J\x1b[H")
        sys.stdout.flush()

    def _enter_windows(self) -> None:
        import ctypes
        from ctypes import wintypes

        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.GetStdHandle.argtypes = [wintypes.DWORD]
        kernel.GetStdHandle.restype = wintypes.HANDLE
        kernel.GetConsoleMode.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)]
        kernel.GetConsoleMode.restype = wintypes.BOOL
        kernel.SetConsoleMode.argtypes = [wintypes.HANDLE, wintypes.DWORD]
        kernel.SetConsoleMode.restype = wintypes.BOOL
        self._win_kernel = kernel
        self._win_in_handle = kernel.GetStdHandle(-10)  # STD_INPUT_HANDLE
        self._win_out_handle = kernel.GetStdHandle(-11)  # STD_OUTPUT_HANDLE
        in_mode, out_mode = wintypes.DWORD(), wintypes.DWORD()
        if not kernel.GetConsoleMode(self._win_in_handle, ctypes.byref(in_mode)):
            raise RuntimeError("无法读取控制台输入模式。")
        if not kernel.GetConsoleMode(self._win_out_handle, ctypes.byref(out_mode)):
            raise RuntimeError("无法读取控制台输出模式。")
        self._win_in_mode, self._win_out_mode = in_mode.value, out_mode.value
        # Keep processed input (Ctrl+C), but turn off line buffering and echo.
        if not kernel.SetConsoleMode(self._win_in_handle, in_mode.value & ~(0x0002 | 0x0004)):
            raise RuntimeError("无法暂时关闭控制台输入回显。")
        self._entered = True
        if not kernel.SetConsoleMode(self._win_out_handle, out_mode.value | 0x0004):
            self.restore()
            raise RuntimeError("此控制台不支持 ANSI 控制序列，请使用 Windows Terminal 或 VS Code 终端。")
        self._ansi_enabled = True

    def restore(self) -> None:
        if self._entered and self._ansi_enabled:
            try:
                sys.stdout.write("\x1b[0m\x1b[?2004l\x1b[?25h")
                sys.stdout.flush()
            except Exception:
                pass
        if self._win_kernel is not None:
            if self._win_in_mode is not None:
                self._win_kernel.SetConsoleMode(self._win_in_handle, self._win_in_mode)
            if self._win_out_mode is not None:
                self._win_kernel.SetConsoleMode(self._win_out_handle, self._win_out_mode)
        elif self._old_termios is not None:
            import termios

            termios.tcsetattr(sys.stdin.fileno(), termios.TCSANOW, self._old_termios)
        self._old_termios = None
        self._win_kernel = None
        self._entered = False
        self._ansi_enabled = False


def _auto_color() -> str:
    if not sys.stdout.isatty() or os.environ.get("NO_COLOR") is not None or os.environ.get("TERM") == "dumb":
        return "none"
    if os.environ.get("COLORTERM", "").lower() in {"truecolor", "24bit"}:
        return "truecolor"
    return "basic"


def _parse_size(value: str) -> tuple[int, int]:
    try:
        width_text, height_text = value.lower().split("x", 1)
        width, height = int(width_text), int(height_text)
        if width < 1 or height < 1:
            raise ValueError
        return width, height
    except ValueError as exc:
        raise argparse.ArgumentTypeError("尺寸格式应为 WIDTHxHEIGHT，例如 80x24") from exc


def _snapshot(args: argparse.Namespace) -> int:
    width, height = args.size or (80, 24)
    mode = _auto_color() if args.color == "auto" else args.color
    for line in render_panel(MODEL, ProbeState(), width, height, mode):
        print(line)
    return 0


def run_interactive(color: str) -> int:
    terminal = TerminalSession()
    interrupted = False
    try:
        terminal.enter()
        state = ProbeState()
        keyboard = TerminalInput()
        last_size = None
        last_state = None
        done = False
        while not done:
            size = shutil.get_terminal_size(fallback=(80, 24))
            current_size = (size.columns, size.lines)
            state_key = (state.page, state.selected, state.focus, state.message)
            if current_size != last_size or state_key != last_state:
                sys.stdout.write("\x1b[2J\x1b[H")
                for line in render_panel(MODEL, state, *current_size, color):
                    sys.stdout.write(line + "\r\n")
                sys.stdout.flush()
                last_size, last_state = current_size, state_key
            key = keyboard.read_key(0.1)
            if key is not None:
                done = handle_key(state, key)
    except KeyboardInterrupt:
        interrupted = True
    except (OSError, RuntimeError) as exc:
        print(f"终端试验无法启动：{exc}", file=sys.stderr)
        return 2
    finally:
        terminal.restore()
    sys.stdout.write("\r\n交互试验已退出；输入回显、光标和终端模式已恢复。\r\n")
    if interrupted:
        sys.stdout.write("退出方式：Ctrl+C。\r\n")
    sys.stdout.flush()
    return 0


def main(argv: Iterable[str] | None = None) -> int:
    # Windows redirected streams otherwise inherit an OEM code page, which can
    # corrupt Chinese snapshots even when the target terminal is Unicode-safe.
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")
    parser = argparse.ArgumentParser(description="TASK-02 无依赖终端交互探针（不会运行游戏行动）")
    parser.add_argument("--snapshot", action="store_true", help="输出静态布局快照，不进入交互模式")
    parser.add_argument("--size", type=_parse_size, help="快照尺寸，例如 80x24、64x20、48x16")
    parser.add_argument("--color", choices=("auto", "truecolor", "basic", "none"), default="auto")
    args = parser.parse_args(list(argv) if argv is not None else None)
    if args.snapshot:
        return _snapshot(args)
    return run_interactive(_auto_color() if args.color == "auto" else args.color)


if __name__ == "__main__":
    raise SystemExit(main())
