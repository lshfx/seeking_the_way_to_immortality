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
        self.assertEqual(safe_text("名\x1b[2J字\x07"), "名[2J字")

    def test_long_name_is_marked_as_clipped_without_hiding_controls(self):
        model = replace(MODEL, status="道号 " + "云栖" * 40)
        visible = "\n".join(render_panel(model, ProbeState(), 48, 16))
        self.assertIn("...", visible)
        self.assertIn("1. 查看传音符", visible)
        self.assertIn("q退", visible)

    def test_color_profiles_are_distinct(self):
        plain = "\n".join(render_panel(MODEL, ProbeState(), 80, 24, "none"))
        basic = "\n".join(render_panel(MODEL, ProbeState(), 80, 24, "basic"))
        truecolor = "\n".join(render_panel(MODEL, ProbeState(), 80, 24, "truecolor"))
        self.assertNotIn("\x1b[", plain)
        self.assertIn("\x1b[", basic)
        self.assertNotIn("38;2;", basic)
        self.assertIn("38;2;", truecolor)

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


if __name__ == "__main__":
    unittest.main()
