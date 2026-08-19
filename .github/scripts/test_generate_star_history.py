#!/usr/bin/env python3

import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

import generate_star_history as chart


class GenerateStarHistoryTests(unittest.TestCase):
    def test_nice_ceiling(self):
        self.assertEqual(chart.nice_ceiling(1_913), 2_000)
        self.assertEqual(chart.nice_ceiling(241), 250)
        self.assertEqual(chart.nice_ceiling(0), 1)

    def test_renders_both_themes(self):
        updated_at = datetime(2026, 8, 19, tzinfo=timezone.utc)
        timestamps = [
            updated_at - timedelta(days=500 - index * 5) for index in range(101)
        ]
        with tempfile.TemporaryDirectory() as directory:
            output_dir = Path(directory)
            chart.write_charts(
                output_dir,
                "Yuan-lab-LLM/ClawManager",
                timestamps,
                updated_at,
            )
            for theme in ("light", "dark"):
                content = (output_dir / f"star-history-{theme}.svg").read_text(
                    encoding="utf-8"
                )
                self.assertIn("Yuan-lab-LLM/ClawManager", content)
                self.assertIn("101 current GitHub stars", content)
                self.assertIn("2026-08-19 UTC", content)
                self.assertTrue(content.startswith("<svg"))

    def test_renders_empty_history_on_leap_day(self):
        updated_at = datetime(2028, 2, 29, tzinfo=timezone.utc)
        content = chart.render_svg(
            "Yuan-lab-LLM/ClawManager", [], updated_at, "light"
        )
        self.assertIn("No stars yet", content)
        self.assertIn("0 current GitHub stars", content)


if __name__ == "__main__":
    unittest.main()
