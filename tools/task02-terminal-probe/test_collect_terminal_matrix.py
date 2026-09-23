"""Tests for the TASK-02 terminal matrix evidence collector."""

import json
import re
import unittest

import collect_terminal_matrix as collector
from interactive_probe import COLOR_MODES, MIN_HEIGHT, MIN_WIDTH, display_width


ANSI = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")


class CollectorTests(unittest.TestCase):
    def test_report_is_json_serialisable_and_labelled(self):
        report = collector.build_report("单元测试终端")
        payload = json.dumps(report, ensure_ascii=False)
        self.assertIn("单元测试终端", payload)
        self.assertEqual(report["label"], "单元测试终端")

    def test_environment_reports_minimum_size_as_an_explicit_fact(self):
        env = collector.probe_environment()
        for key in (
            "stdout_isatty",
            "reported_size",
            "meets_min_size",
            "encoding",
            "cjk_roundtrip_ok",
            "color_tier_selected",
        ):
            self.assertIn(key, env)
        self.assertEqual(env["min_size"], f"{MIN_WIDTH}x{MIN_HEIGHT}")
        # meets_min_size must be derived from the reported size, not assumed.
        columns, lines = (int(part) for part in env["reported_size"].split("x"))
        self.assertEqual(env["meets_min_size"], columns >= MIN_WIDTH and lines >= MIN_HEIGHT)

    def test_acceptance_sizes_all_keep_exact_geometry(self):
        report = collector.build_report("单元测试终端")
        sizes = [row["size"] for row in report["layout_acceptance"]]
        self.assertEqual(sizes, ["80x24", "64x20", "48x16", "40x10"])
        for row in report["layout_acceptance"]:
            with self.subTest(size=row["size"]):
                self.assertTrue(row["row_count_ok"], row)
                self.assertTrue(row["all_rows_exact_width"], row)
                self.assertTrue(row["widths_uniform"], row)
                self.assertTrue(row["escape_free"], row)
                self.assertTrue(row["expectation_met"], row)

    def test_sub_minimum_size_never_reports_menu_visible(self):
        row = collector.snapshot_checks(40, 10, "none")
        self.assertFalse(row["expectation_met"] is False)
        self.assertIn("尺寸提示", row["expectation"])

    def test_color_tiers_cover_every_mode_without_truecolor_leakage(self):
        report = collector.build_report("单元测试终端")
        modes = [row["mode"] for row in report["color_tiers"]]
        self.assertEqual(modes, list(COLOR_MODES))
        by_mode = {row["mode"]: row for row in report["color_tiers"]}
        self.assertTrue(by_mode["none"]["has_escape"] is False)
        self.assertTrue(by_mode["basic"]["has_escape"] is True)
        # A lower tier must never emit truecolor codes.
        self.assertFalse(by_mode["basic"]["uses_truecolor"])
        self.assertFalse(by_mode["compat"]["uses_truecolor"])
        self.assertTrue(by_mode["compat"]["uses_256"])
        self.assertTrue(by_mode["truecolor"]["uses_truecolor"])

    def test_evidence_markers_wrap_a_parseable_block(self):
        report = collector.build_report("标记测试")
        import io
        import contextlib

        buffer = io.StringIO()
        with contextlib.redirect_stdout(buffer):
            collector.main(["--label", "标记测试", "--json-only"])
        output = buffer.getvalue()
        self.assertIn(collector.BEGIN, output)
        self.assertIn(collector.END, output)
        block = output.split(collector.BEGIN)[1].split(collector.END)[0].strip()
        parsed = json.loads(block)
        self.assertEqual(parsed["label"], "标记测试")


if __name__ == "__main__":
    unittest.main()
