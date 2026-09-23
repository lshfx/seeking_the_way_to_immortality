"""Regression tests for the TASK-02 key-byte diagnostic tool.

Three defects shipped in the first version of ``diagnose_keys.py``. All three
made the tool report "未识别" for input that the real probe decoded correctly,
so the evidence it produced looked like a product bug when it was a tool bug.
These tests pin the fixed behaviour.
"""

from __future__ import annotations

import unittest

from diagnose_keys import classify, describe, hexdump


class HexdumpTest(unittest.TestCase):
    def test_each_character_prints_as_one_byte(self) -> None:
        # Defect 1: the old dump encoded to UTF-16-LE and printed low byte
        # first, so a real ASCII "H" appeared as "48 00" and the byte order
        # looked reversed to a human reader.
        self.assertEqual(hexdump(["H"]), "48")
        self.assertEqual(hexdump(["P"]), "50")
        self.assertEqual(hexdump(["1"]), "31")

    def test_extended_prefix_prints_as_e0_not_two_bytes(self) -> None:
        # The prefix character is U+00E0, but it stands for the single
        # keyboard byte 0xE0. It must not print as "E0 00".
        self.assertEqual(hexdump(["\xe0"]), "E0")
        self.assertEqual(hexdump(["\x00"]), "00")

    def test_full_extended_key_sequence(self) -> None:
        self.assertEqual(hexdump(["\xe0", "H"]), "E0 48")
        self.assertEqual(hexdump(["\x00", "P"]), "00 50")

    def test_empty_capture_prints_nothing(self) -> None:
        self.assertEqual(hexdump([]), "")


class ClassifyTest(unittest.TestCase):
    def test_every_arrow_key_is_decoded(self) -> None:
        # Defect 2: the old classifier compared against literal "\x00H" and
        # "\xe0H" strings. Real terminals deliver the prefix as U+00E0 and the
        # suffix as the letter, so only Up ever matched.
        cases = {
            "\x00H": "UP",
            "\xe0P": "DOWN",
            "\x00K": "LEFT",
            "\xe0M": "RIGHT",
        }
        for raw, expected in cases.items():
            with self.subTest(raw=repr(raw)):
                self.assertTrue(classify(list(raw)).startswith(expected))

    def test_extra_extended_keys_are_named(self) -> None:
        extra = {
            "\xe0G": "HOME",
            "\x00O": "END",
            "\xe0I": "PAGEUP",
            "\x00Q": "PAGEDOWN",
            "\xe0R": "INSERT",
            "\x00S": "DELETE",
        }
        for raw, expected in extra.items():
            with self.subTest(raw=repr(raw)):
                self.assertIn(expected, classify(list(raw)))

    def test_prefix_and_suffix_are_both_named_in_the_verdict(self) -> None:
        verdict = classify(["\x00", "H"])
        self.assertIn("0x00", verdict)
        self.assertIn("UP", verdict)

    def test_lone_prefix_is_reported_as_incomplete(self) -> None:
        self.assertIn("仅收到扩展键前缀", classify(["\xe0"]))
        self.assertIn("仅收到扩展键前缀", classify(["\x00"]))
        # A lone prefix must never be reported as a recognised key.
        self.assertNotIn("UP", classify(["\xe0"]))

    def test_unknown_extended_suffix_is_surfaced_not_hidden(self) -> None:
        verdict = classify(["\xe0", "Z"])
        self.assertIn("未知后缀", verdict)

    def test_plain_ascii_keys_are_not_claimed_as_extended(self) -> None:
        self.assertEqual(classify(["1"]), "未识别")
        self.assertEqual(classify(["w"]), "未识别")
        # A bare suffix letter with no prefix must not be invented into a key.
        self.assertEqual(classify(["H"]), "未识别")

    def test_csi_arrow_sequences_are_named(self) -> None:
        self.assertIn("UP", classify(list("\x1b[A")))
        self.assertIn("DOWN", classify(list("\x1b[B")))
        self.assertIn("ANSI", classify(list("\x1b[A")))

    def test_empty_capture_is_a_timeout_not_a_key(self) -> None:
        verdict = classify([])
        self.assertIn("TIMEOUT", verdict)
        self.assertNotIn("UP", verdict)


class DescribeTest(unittest.TestCase):
    def test_extended_prefix_is_labelled_as_such(self) -> None:
        self.assertIn("0xE0", describe("\xe0"))
        self.assertIn("0x00", describe("\x00"))

    def test_named_controls_still_resolve(self) -> None:
        self.assertEqual(describe("\t"), "TAB")
        self.assertEqual(describe("\r"), "CR")
        self.assertEqual(describe("\x03"), "ETX(Ctrl+C)")


class ReadRawTest(unittest.TestCase):
    def test_tail_window_does_not_re_arm_per_character(self) -> None:
        """Defect 3: re-arming the tail merged separate keypresses.

        The old loop reset ``tail_deadline`` on every character it drained, so
        a fast typist's next key was swallowed into the previous capture. That
        shifted every row by one prompt in the first real run. The deadline
        must be computed once, before the drain loop.
        """
        import inspect

        import diagnose_keys

        source = inspect.getsource(diagnose_keys.read_raw)
        drain = source.split("while time.monotonic() < tail_deadline:", 1)[1]
        # The deadline is set up before the loop and never refreshed inside it.
        self.assertNotIn("tail_deadline =", drain)
        self.assertEqual(drain.count("captured.append("), 1)


if __name__ == "__main__":
    unittest.main()
