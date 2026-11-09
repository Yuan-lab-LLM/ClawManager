import json
from pathlib import Path
import unittest

import prompt_ablation as experiment


class PromptAblationTests(unittest.TestCase):
    def setUp(self):
        self.client = experiment.load_baseline(Path(__file__).resolve().parents[2] / 'deployments/laya/call_laya.py')

    def test_single_factor_variants(self):
        baseline, short, concrete, plain, combined = experiment.variants(self.client.QUESTIONS['tier'])
        self.assertEqual(baseline['question'], self.client.QUESTIONS['tier'])
        self.assertEqual(short['question']['criteria'], baseline['question']['criteria'])
        self.assertEqual(short['format'], baseline['format'])
        self.assertEqual(concrete['question']['instructions'], baseline['question']['instructions'])
        self.assertEqual(concrete['format'], baseline['format'])
        self.assertEqual(plain['question'], baseline['question'])
        self.assertEqual(plain['format'], 'plain')
        self.assertEqual(combined['question']['criteria'], experiment.CONCRETE)

    def test_input_never_contains_reference_metadata(self):
        case = {'id': 'do-not-send', 'expected_tier': 4, 'turns': [{'text': '旧问题'}, {'text': '<新问题>&'}]}
        state = experiment.state_for(case['turns'], 'json')
        self.assertNotIn('expected_tier', state)
        self.assertNotIn('do-not-send', state)
        self.assertNotIn('<', state)
        self.assertEqual([t['text'] for t in json.loads(state)], ['旧问题', '<新问题>&'])
        self.assertEqual(experiment.state_for(case['turns'], 'plain'), '历史用户任务：旧问题\n当前任务：<新问题>&')

    def test_failures_are_counted_not_dropped(self):
        score = experiment.metrics([{'expected_tier': 2, 'tier': 2, 'ms': 1}, {'expected_tier': 2, 'tier': None, 'ms': 2}])
        self.assertEqual(score['agreement'], 0.5)
        self.assertEqual(score['failures'], 1)

    def test_new_corpus_is_balanced_and_distinct(self):
        root = Path(__file__).resolve().parent
        old = json.loads((root / 'cases.zh.json').read_text(encoding='utf-8'))
        new = json.loads((root / 'prompt-holdout.zh.json').read_text(encoding='utf-8'))
        self.assertEqual(len(new), 20)
        self.assertEqual(len({c['id'] for c in old + new}), 60)
        for tier in range(1, 5):
            self.assertEqual(sum(c['expected_tier'] == tier for c in new), 5)
        old_tasks = {c['turns'][-1]['text'] for c in old}
        self.assertTrue(all(c['turns'][-1]['text'] not in old_tasks for c in new))


if __name__ == '__main__':
    unittest.main()
