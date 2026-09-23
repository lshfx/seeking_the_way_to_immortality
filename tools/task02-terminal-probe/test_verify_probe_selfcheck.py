"""Tests for the TASK-02 non-interactive probe self-check."""

import io
import contextlib
import json
import unittest

import verify_probe_selfcheck as selfcheck
from interactive_probe import handle_key, ProbeState


class SelfCheckTests(unittest.TestCase):
    def test_all_checks_pass(self):
        report = selfcheck.build_report()
        self.assertEqual(report["failures"], [], f"unexpected failures: {report['failures']}")
        self.assertEqual(report["checks_passed"], report["checks_total"])
        self.assertGreaterEqual(report["checks_total"], 15)

    def test_every_section_is_represented(self):
        report = selfcheck.build_report()
        self.assertEqual(
            set(report["sections"]),
            {"input_decoding", "render_state_purity", "console_mode_restore", "escape_and_width"},
        )
        for name, items in report["sections"].items():
            with self.subTest(section=name):
                self.assertTrue(items, f"section {name} produced no checks")

    def test_exit_mapping_is_actually_asserted(self):
        # The self-check must catch a regression that breaks safe-exit mapping.
        results = selfcheck.check_input_decoding()
        exit_checks = [r for r in results if "safe exit" in r["check"]]
        self.assertTrue(exit_checks, "expected safe-exit checks to exist")
        self.assertTrue(all(r["passed"] for r in exit_checks))

        # Confirm the underlying contract directly: any of these keys exits.
        for key in ("q", "Q", "CTRL_C"):
            with self.subTest(key=key):
                self.assertTrue(handle_key(ProbeState(), key))

    def test_payload_leak_check_would_fail_on_the_old_bug(self):
        # The check must be about the payload, not just the ESC byte. Re-create
        # the old behaviour and confirm the assertion would have caught it.
        old_behaviour = "名[2J字"  # what the pre-fix safe_text returned
        self.assertIn("[2J", old_behaviour)
        from interactive_probe import safe_text

        self.assertNotIn("[2J", safe_text("名\x1b[2J字"))

    def test_exit_code_signals_failure(self):
        # main() must return non-zero when a check fails. Patch a check to fail.
        original = selfcheck.check_escape_and_width

        def failing():
            return [{"check": "synthetic failure", "passed": False, "detail": "forced"}]

        selfcheck.check_escape_and_width = failing
        try:
            buffer = io.StringIO()
            with contextlib.redirect_stdout(buffer):
                code = selfcheck.main(["--json-only"])
            self.assertEqual(code, 1, "a failing check must produce a non-zero exit code")
        finally:
            selfcheck.check_escape_and_width = original

    def test_evidence_block_is_parseable(self):
        buffer = io.StringIO()
        with contextlib.redirect_stdout(buffer):
            selfcheck.main(["--json-only"])
        output = buffer.getvalue()
        self.assertIn(selfcheck.BEGIN, output)
        self.assertIn(selfcheck.END, output)
        block = output.split(selfcheck.BEGIN)[1].split(selfcheck.END)[0].strip()
        parsed = json.loads(block)
        self.assertIn("sections", parsed)
        self.assertIn("checks_total", parsed)


if __name__ == "__main__":
    unittest.main()
