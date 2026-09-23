"""Tests for terminal identity detection.

An earlier TASK-02 evidence run recorded ``WT_SESSION=有`` alongside empty
``TERM``/``TERM_PROGRAM``/``COLORTERM``. That combination is correct but easy to
misread, so identity is now decided explicitly and only from variables that were
actually observed to exist.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

from detect_terminal import WATCHED, detect


def with_env(values: dict[str, str]):
    """Run ``detect`` with exactly the given watched variables set."""
    env = {name: "" for name in WATCHED}
    env.update(values)
    # A completely empty environment for the watched names; real unset vars are
    # also removed so detect() cannot pick up the host's actual terminal.
    return mock.patch.dict(os.environ, env, clear=False)


class DetectTerminalTest(unittest.TestCase):
    def test_wt_session_is_decisive(self) -> None:
        with mock.patch.dict(os.environ, {"WT_SESSION": "abc-123"}, clear=False):
            info = detect()
        self.assertEqual(info["verdict"], "Windows Terminal")
        self.assertIn("WT_SESSION", info["evidence"])

    def test_wt_session_alone_beats_empty_colour_vars(self) -> None:
        # This is the exact combination from the real Windows Terminal run.
        overrides = {name: "" for name in WATCHED}
        overrides["WT_SESSION"] = "679d7097-3803-41f3-8842-f37341b4bee2"
        with mock.patch.dict(os.environ, overrides, clear=False):
            for name in ("TERM", "TERM_PROGRAM", "COLORTERM", "WT_PROFILE_ID"):
                os.environ.pop(name, None)
            info = detect()
        self.assertEqual(info["verdict"], "Windows Terminal")
        self.assertEqual(info["env"]["TERM"], "")
        self.assertEqual(info["env"]["COLORTERM"], "")

    def test_wt_profile_id_alone_is_also_decisive(self) -> None:
        with mock.patch.dict(os.environ, {"WT_PROFILE_ID": "{guid}"}, clear=False):
            os.environ.pop("WT_SESSION", None)
            info = detect()
        self.assertEqual(info["verdict"], "Windows Terminal")

    def test_bare_console_is_named_as_conhost(self) -> None:
        overrides = {name: "" for name in WATCHED}
        with mock.patch.dict(os.environ, overrides, clear=False):
            for name in WATCHED:
                os.environ.pop(name, None)
            info = detect()
        self.assertIn("conhost", info["verdict"])

    def test_vscode_is_not_mistaken_for_windows_terminal(self) -> None:
        overrides = {name: "" for name in WATCHED}
        overrides["TERM_PROGRAM"] = "vscode"
        overrides["VSCODE_INJECTION"] = "1"
        with mock.patch.dict(os.environ, overrides, clear=False):
            os.environ.pop("WT_SESSION", None)
            info = detect()
        self.assertIn("VS Code", info["verdict"])

    def test_wezterm_and_alacritty_are_recognised(self) -> None:
        overrides = {name: "" for name in WATCHED}
        overrides["WEZTERM_EXECUTABLE"] = "wezterm.exe"
        with mock.patch.dict(os.environ, overrides, clear=False):
            os.environ.pop("WT_SESSION", None)
            self.assertEqual(detect()["verdict"], "WezTerm")

        overrides = {name: "" for name in WATCHED}
        overrides["ALACRITTY_LOG"] = "alacritty.log"
        with mock.patch.dict(os.environ, overrides, clear=False):
            os.environ.pop("WT_SESSION", None)
            self.assertEqual(detect()["verdict"], "Alacritty")

    def test_colour_vars_are_never_used_to_claim_identity(self) -> None:
        """COLORTERM=truecolor must not by itself be read as a terminal name.

        Colour capability and terminal identity are different questions; the
        first evidence run conflated them by trying to infer the product from
        the colour tier that was selected.
        """
        overrides = {name: "" for name in WATCHED}
        overrides["COLORTERM"] = "truecolor"
        with mock.patch.dict(os.environ, overrides, clear=False):
            for name in WATCHED:
                if name != "COLORTERM":
                    os.environ.pop(name, None)
            verdict = detect()["verdict"]
        self.assertNotIn("Windows Terminal", verdict)
        self.assertNotIn("VS Code", verdict)
        # With colour vars only, the honest answer is "cannot identify".
        self.assertIn("未标识", verdict)

    def test_report_is_json_serialisable(self) -> None:
        import json

        info = detect()
        json.dumps(info, ensure_ascii=False)


if __name__ == "__main__":
    unittest.main()
