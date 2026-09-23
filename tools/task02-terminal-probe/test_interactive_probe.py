import re
import unittest
from dataclasses import replace

from interactive_probe import (
    MODEL,
    ProbeState,
    TerminalInput,
    _read_escape_sequence,
    _discard_bracketed_paste,
    display_width,
    handle_key,
    clip_display,
    render_panel,
    safe_text,
)


ANSI = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")


class InteractiveProbeTests(unittest.TestCase):
    def test_acceptance_sizes_fit_without_clipping_choices(self):
        for width, height in ((80, 24), (64, 20), (48, 16)):
            with self.subTest(size=(width, height)):
                rows = render_panel(MODEL, ProbeState(), width, height)
                visible = [ANSI.sub("", row) for row in rows]
                self.assertEqual(len(rows), height)
                self.assertTrue(all(display_width(row) == width for row in visible))
                self.assertIn("1. 查看传音符", "\n".join(visible))
                self.assertIn("2. 继续修炼", "\n".join(visible))
                self.assertIn("3. 返回洞府", "\n".join(visible))
                self.assertIn("q退", "\n".join(visible))
                self.assertNotIn("窗口过小", "\n".join(visible))

    def test_under_minimum_shows_resize_prompt_and_hides_choices(self):
        rows = render_panel(MODEL, ProbeState(), 40, 10)
        visible = "\n".join(ANSI.sub("", row) for row in rows)
        self.assertIn("窗口过小", visible)
        self.assertIn("48x16", visible)
        self.assertIn("未执行行动", visible)
        self.assertNotIn("1. 查看传音符", visible)
        self.assertEqual(len(rows), 10)

    def test_unicode_display_width_and_control_filtering(self):
        combined = "e\u0301中"
        self.assertEqual(display_width(combined), 3)
        self.assertEqual(clip_display(combined, 1), "e\u0301")
        # The full escape sequence must be removed, not just its ESC byte.
        # Leaving the "[2J" payload behind was a real defect.
        self.assertEqual(safe_text("名\x1b[2J字\x07"), "名字")
        self.assertNotIn("[2J", safe_text("名\x1b[2J字\x07"))

    def test_escape_sequence_payloads_never_reach_the_frame(self):
        # Every row must stay free of escape payload text, and must keep the
        # exact requested width, even when the source contains control strings.
        model = replace(MODEL, status="清微\x1b[2J | 修为\x1b]0;evil\x07 51/100")
        rows = render_panel(model, ProbeState(), 48, 16, "none")
        joined = "\n".join(rows)
        self.assertNotIn("[2J", joined)
        self.assertNotIn("evil", joined)
        self.assertNotIn("\x1b", joined)
        self.assertTrue(all(display_width(row) == 48 for row in rows))

    def test_long_name_is_marked_as_clipped_without_hiding_controls(self):
        model = replace(MODEL, status="道号 " + "云栖" * 40)
        rows = render_panel(model, ProbeState(), 48, 16)
        visible = "\n".join(rows)
        self.assertIn("...", visible)
        self.assertIn("1. 查看传音符", visible)
        self.assertIn("q退", visible)
        # A clipped frame must still be exactly the requested size. Asserting
        # width *after* truncation is only meaningful if truncation is gone.
        self.assertEqual(len(rows), 16)
        self.assertTrue(all(display_width(row) == 48 for row in rows))

    def test_clipping_cannot_hide_row_overflow(self):
        # Directly prove the guard: a frame that cannot fit must raise rather
        # than quietly drop rows.
        from interactive_probe import LayoutOverflow, _boxed

        with self.assertRaises(LayoutOverflow):
            _boxed([(f"行{i}", "body") for i in range(10)], 48, 8, "none")

    def test_color_profiles_are_distinct(self):
        plain = "\n".join(render_panel(MODEL, ProbeState(), 80, 24, "none"))
        basic = "\n".join(render_panel(MODEL, ProbeState(), 80, 24, "basic"))
        compat = "\n".join(render_panel(MODEL, ProbeState(), 80, 24, "compat"))
        truecolor = "\n".join(render_panel(MODEL, ProbeState(), 80, 24, "truecolor"))
        self.assertNotIn("\x1b[", plain)
        self.assertIn("\x1b[", basic)
        # compat must be a genuine downgrade step: 256-colour, not 24-bit.
        self.assertIn("38;5;", compat)
        self.assertNotIn("38;2;", compat)
        self.assertNotIn("38;5;", basic)
        self.assertIn("38;2;", truecolor)
        # Each tier must produce different bytes from its neighbours.
        self.assertNotEqual(basic, compat)
        self.assertNotEqual(compat, truecolor)

    def test_color_fallback_never_invents_truecolor_support(self):
        from interactive_probe import _auto_color

        def resolved(env):
            import os
            from unittest import mock

            with mock.patch.dict(os.environ, env, clear=True):
                with mock.patch("sys.stdout") as out:
                    out.isatty.return_value = True
                    return _auto_color()

        self.assertEqual(resolved({"TERM": "xterm-256color"}), "compat")
        self.assertEqual(resolved({"TERM": "xterm"}), "basic")
        self.assertEqual(resolved({"COLORTERM": "truecolor", "TERM": "xterm"}), "truecolor")
        self.assertEqual(resolved({"NO_COLOR": "1", "COLORTERM": "truecolor"}), "none")
        self.assertEqual(resolved({"TERM": "dumb"}), "none")

    def test_ui_navigation_does_not_have_or_advance_a_game_clock(self):
        state = ProbeState()
        self.assertFalse(handle_key(state, "2"))
        self.assertEqual(state.selected, 1)
        self.assertFalse(handle_key(state, "ENTER"))
        self.assertIn("不扣月", state.message)
        self.assertFalse(handle_key(state, "n"))
        self.assertEqual(state.page, 1)
        self.assertEqual(set(state.__dict__), {"page", "selected", "focus", "message"})

    def test_eof_and_ctrl_c_are_exit_inputs(self):
        keyboard = TerminalInput()
        keyboard._read_char = lambda _timeout: "EOF"
        self.assertEqual(keyboard.read_key(), "q")
        keyboard._read_char = lambda _timeout: "\x03"
        key = keyboard.read_key()
        self.assertEqual(key, "CTRL_C")
        self.assertTrue(handle_key(ProbeState(), key))

    def test_focus_and_resize_rendering_do_not_commit_actions(self):
        state = ProbeState()
        self.assertFalse(handle_key(state, "TAB"))
        self.assertEqual(state.focus, "details")
        self.assertFalse(handle_key(state, "2"))
        self.assertEqual(state.selected, 0)
        render_panel(MODEL, state, 80, 24)
        render_panel(MODEL, state, 48, 16)
        self.assertEqual((state.selected, state.page, state.focus), (0, 0, "details"))

    def test_escape_sequences_are_consumed_not_interpreted_as_menu_keys(self):
        arrow = iter(["[", "1", ";", "5", "A"])
        self.assertEqual(_read_escape_sequence(lambda _: next(arrow, None)), "UP")

        osc = iter(["]", "5", "2", ";", "q", "1", "2", "\x07", "q"])
        self.assertEqual(_read_escape_sequence(lambda _: next(osc, None)), "IGNORE")
        # The payload key q and following digit were consumed with the control string.
        self.assertEqual(next(osc, None), "q")

        paste_start = iter(["[", "2", "0", "0", "~"])
        self.assertEqual(_read_escape_sequence(lambda _: next(paste_start, None)), "PASTE_START")
        pasted = iter(list("q123") + list("\x1b[201~") + ["n"])
        _discard_bracketed_paste(lambda _: next(pasted, None))
        self.assertEqual(next(pasted, None), "n")

    def test_windows_extended_keys_decode_with_prefix(self):
        # The documented Windows form: a 0x00/0xE0 prefix then a suffix letter.
        cases = [
            ("\x00H", "UP"), ("\x00P", "DOWN"), ("\x00K", "LEFT"), ("\x00M", "RIGHT"),
            ("\xe0H", "UP"), ("\xe0P", "DOWN"), ("\xe0K", "LEFT"), ("\xe0M", "RIGHT"),
            ("\x00I", "PAGEUP"), ("\x00Q", "PAGEDOWN"),
            ("\x00G", "HOME"), ("\x00O", "END"),
        ]
        for raw, expected in cases:
            with self.subTest(raw=raw):
                chars = iter(list(raw))
                keyboard = TerminalInput()
                keyboard._read_char = lambda _t: next(chars, None)
                self.assertEqual(keyboard.read_key(), expected)

    def test_slow_extended_key_suffix_is_retried_not_dropped(self):
        # Regression: a suffix that arrives after the first short window used to
        # be dropped entirely, so arrow keys silently stopped working on real
        # PowerShell/conhost sessions. It must be retried and still decode.
        arrivals = iter(["\x00", None, None, "H"])
        keyboard = TerminalInput()
        keyboard._read_char = lambda _t: next(arrivals, None)
        self.assertEqual(
            keyboard.read_key(),
            "UP",
            "a late suffix must be retried, not discarded",
        )

    def test_exhausted_prefix_reports_retry_token_not_silent_drop(self):
        # If the suffix never arrives, report a dedicated token so the caller can
        # ignore it deliberately instead of logging an unknown key.
        arrivals = iter(["\x00"] + [None] * 8)
        keyboard = TerminalInput()
        keyboard._read_char = lambda _t: next(arrivals, None)
        self.assertEqual(keyboard.read_key(), "PREFIX_RETRY")

    def test_prefix_retry_never_changes_state_or_exits(self):
        state = ProbeState()
        before = (state.page, state.selected, state.focus, state.message)
        self.assertFalse(handle_key(state, "PREFIX_RETRY"))
        self.assertEqual((state.page, state.selected, state.focus, state.message), before)

    def test_reversed_extended_keys_also_decode(self):
        """Evidence runs captured the letter *before* the 0xE0 byte.

        Real captures from Windows Terminal and PowerShell recorded ``H`` then
        ``à`` for the Up arrow. Whether that order comes from the terminal or
        from the collection layer, the decoder must not depend on it.
        """
        cases = [
            ("H\xe0", "UP"), ("P\xe0", "DOWN"), ("K\xe0", "LEFT"), ("M\xe0", "RIGHT"),
            ("H\x00", "UP"), ("P\x00", "DOWN"), ("K\x00", "LEFT"), ("M\x00", "RIGHT"),
            ("G\xe0", "HOME"), ("O\x00", "END"),
        ]
        for raw, expected in cases:
            with self.subTest(raw=raw):
                chars = iter(list(raw))
                keyboard = TerminalInput()
                keyboard._read_char = lambda _t: next(chars, None)
                self.assertEqual(keyboard.read_key(), expected)

    def test_reversed_form_does_not_swallow_real_command_keys(self):
        """A lone letter must never be turned into an arrow key or dropped.

        Accepting the reversed order means peeking one character past a letter
        such as ``w``. If the peek finds another ordinary character, the first
        must still be reported as itself and the second must not be lost.
        """
        # Two command keys typed back to back, quickly.
        arrivals = iter(["w", "s"])
        keyboard = TerminalInput()
        keyboard._read_char = lambda _t: next(arrivals, None)
        self.assertEqual(keyboard.read_key(), "w")
        self.assertEqual(keyboard.read_key(), "s", "the peeked character must not be dropped")

    def test_all_command_letters_survive_a_trailing_peek(self):
        for letter in ("w", "s", "n", "p", "q", "Q"):
            with self.subTest(letter=letter):
                arrivals = iter([letter])
                keyboard = TerminalInput()
                keyboard._read_char = lambda _t: next(arrivals, None)
                self.assertEqual(keyboard.read_key(), letter)

    def test_letter_then_timeout_still_returns_the_letter(self):
        # No second character at all: the letter is just a letter.
        arrivals = iter(["n"] + [None] * 4)
        keyboard = TerminalInput()
        keyboard._read_char = lambda _t: next(arrivals, None)
        self.assertEqual(keyboard.read_key(), "n")

    def test_digits_are_never_treated_as_extended_keys(self):
        # Only H/P/K/M/I/Q/R/S/G/O are extended suffixes; digits must pass through.
        for digit in "123":
            with self.subTest(digit=digit):
                arrivals = iter([digit] + [None] * 3)
                keyboard = TerminalInput()
                keyboard._read_char = lambda _t: next(arrivals, None)
                self.assertEqual(keyboard.read_key(), digit)


if __name__ == "__main__":
    unittest.main()
