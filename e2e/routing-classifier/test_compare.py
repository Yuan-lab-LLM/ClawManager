import unittest
import compare_laya as evaluation


class EvaluationTests(unittest.TestCase):
    def test_errors_count_in_denominator(self):
        rows = [
            {"expected_tier": 1, "laya": {"tier": 1, "latency_ms": 10}},
            {"expected_tier": 2, "laya": {"tier": 1, "latency_ms": 20}},
            {"expected_tier": 3, "laya": {"tier": 4, "latency_ms": 30}},
            {"expected_tier": 4, "laya": {"tier": None, "latency_ms": 50}},
        ]
        result = evaluation.summarize(rows, "laya")
        self.assertEqual(result["accuracy"], .25)
        self.assertEqual((result["under"], result["over"], result["failures"]), (1, 1, 1))
        self.assertEqual(result["p50_ms"], 20)
        self.assertEqual(result["p95_ms"], 30)
        self.assertEqual(result["confusion_expected_rows_predicted_columns"][2][3], 1)

    def test_all_errors(self):
        result = evaluation.summarize([{"expected_tier": 1, "laya": {"tier": None}}], "laya")
        self.assertEqual(result["accuracy"], 0)
        self.assertIsNone(result["p50_ms"])

    def test_fixed_rubric_has_four_options(self):
        self.assertEqual(set(evaluation.QUESTIONS["tier"]["criteria"]), set(evaluation.TIER_LABELS))


if __name__ == "__main__":
    unittest.main()
