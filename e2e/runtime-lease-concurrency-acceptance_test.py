"""In-memory fake API/SQL regression: never contacts a cluster or writes SQL."""
import ast
import json
from pathlib import Path
import tempfile
import types
import unittest

SOURCE = Path(__file__).with_name('runtime-lease-concurrency-acceptance.py')


def load_functions():
    tree = ast.parse(SOURCE.read_text())
    selected = ast.Module(body=[node for node in tree.body if isinstance(node, (ast.Import, ast.ImportFrom, ast.FunctionDef))], type_ignores=[])
    module = types.ModuleType('lease_acceptance_under_test')
    exec(compile(selected, str(SOURCE), 'exec'), module.__dict__)
    return module


class AcceptanceRecoveryTests(unittest.TestCase):
    def run_scenario(self, scenario):
        m = load_functions()
        with tempfile.TemporaryDirectory() as directory:
            m.ROOT = Path(directory)
            m.args = types.SimpleNamespace(timeout=1)
            policy = {'enabled': True, 'version': 123}
            m.REPORT = {'run': 'lease-test123', 'user_id': 7, 'owned': [], 'batches': [], 'results': [], 'policy_before': policy}
            m.save()
            original = {'id': 1, 'user_id': 9, 'name': 'existing-original', 'type': 'openclaw'}
            created = {1: original.copy()}
            mutations, posts = [], []
            def api(method, path, body=None, allow_missing=False):
                if method == 'POST':
                    journal = json.loads((m.ROOT / 'acceptance.json').read_text())
                    self.assertTrue(journal.get('batches'), 'exact batch intent was not saved before POST')
                    self.assertEqual(journal['batches'][0]['user_id'], 7)
                    self.assertEqual(journal['batches'][0]['names'], ['lease-test123-burst-001', 'lease-test123-burst-002'])
                    count = 1 if scenario == 'partial-uncertain' else body['count']
                    for index in range(1, count + 1):
                        item = {'id': 100 + index, 'user_id': 9 if scenario == 'wrong-owner' else 7, 'name': body['name_prefix'] + f'-{index:03d}', 'type': 'openclaw'}
                        created[item['id']] = item
                    posts.append(path)
                    if scenario != 'interrupted-journal':
                        raise TimeoutError('batch committed, response lost')
                    return {'results': [{'instance': item} for key, item in created.items() if key != 1]}
                if method == 'GET' and path.startswith('/instances/'):
                    item = created.get(int(path.rsplit('/', 1)[-1]))
                    return {'instance': item} if item else None
                if method == 'GET' and path == '/admin/runtime-auto-stop':
                    return policy.copy()
                if method in ('PUT', 'DELETE'):
                    self.assertNotEqual(path, '/admin/runtime-auto-stop', 'global policy mutation')
                    identifier = int(path.split('/')[3]) if method == 'PUT' else int(path.rsplit('/', 1)[-1])
                    self.assertNotEqual(identifier, 1, 'original instance mutation')
                    self.assertEqual(created[identifier]['user_id'], 7, 'unverified owner mutation')
                    mutations.append((method, identifier))
                    if method == 'DELETE':
                        del created[identifier]
                    return {}
                raise AssertionError((method, path))
            def sql(query):
                self.assertTrue(query.startswith('SELECT '), 'SQL mutation')
                if 'FROM instance_runtime_bindings' in query:
                    return ''
                rows = [item for item in created.values() if item['name'].encode().hex() in query]
                self.assertNotIn(original, rows, 'query included original owner')
                if query.startswith('SELECT id,user_id,name,type '):
                    return '\n'.join(f'{x["id"]}\t{x["user_id"]}\t{x["name"]}\t{x["type"]}' for x in rows)
                return '\n'.join(str(row['id']) for row in rows)
            m.api, m.sql = api, sql
            original_save = m.save
            if scenario == 'interrupted-journal':
                def interrupted_save():
                    original_save()
                    if len(m.REPORT['owned']) == 1:
                        raise SystemExit('process interrupted after first ownership record')
                m.save = interrupted_save
            try:
                m.create_batch('openclaw', 2, 'burst')
            except (TimeoutError, SystemExit):
                pass
            m.save = original_save
            # Restart cleanup from the durable journal, not in-memory IDs.
            m.REPORT = json.loads((m.ROOT / 'acceptance.json').read_text())
            if scenario in ('partial-uncertain', 'wrong-owner'):
                with self.assertRaises(AssertionError):
                    m.cleanup()
                self.assertEqual(m.REPORT['results'][-1]['result'], 'FAIL')
                self.assertFalse(m.REPORT['results'][-1]['evidence']['all_intents_resolved'])
                if scenario == 'wrong-owner':
                    self.assertFalse(mutations)
            else:
                m.cleanup()
                self.assertEqual([row['id'] for row in m.REPORT['owned']], [101, 102])
                self.assertEqual([identifier for method, identifier in mutations if method == 'DELETE'], [101, 102])
                self.assertEqual(created, {1: original})
                self.assertTrue(all(batch['outcome'] == 'resolved' for batch in m.REPORT['batches']))
                self.assertEqual(m.REPORT['results'][-1]['result'], 'PASS')
            self.assertEqual(posts, ['/instances/batch/lite'])
            self.assertEqual(created[1], original)
            self.assertEqual(policy, {'enabled': True, 'version': 123})

    def test_committed_batch_lost_response(self):
        self.run_scenario('lost-response')

    def test_interrupted_ownership_recording(self):
        self.run_scenario('interrupted-journal')

    def test_partial_unknown_outcome_cannot_pass_cleanup(self):
        self.run_scenario('partial-uncertain')

    def test_wrong_owner_is_never_mutated(self):
        self.run_scenario('wrong-owner')

    def check_physical_observation(self, persistent):
        m = load_functions()
        with tempfile.TemporaryDirectory() as directory:
            m.ROOT = Path(directory)
            m.REPORT = {'run': 'lease-physical', 'results': []}
            elapsed, requests = [0], []
            m.time = types.SimpleNamespace(monotonic=lambda: elapsed[0], sleep=lambda seconds: elapsed.__setitem__(0, elapsed[0] + seconds))
            m.owned_instance = lambda row: {'status': 'running', 'runtime_generation': 1}
            row = {'id': 101, 'type': 'openclaw'}
            def api(method, path):
                self.assertEqual((method, path), ('GET', '/admin/runtime-pods?runtime_type=openclaw'))
                requests.append(path)
                slots = [] if persistent or len(requests) == 1 else [{'instance_id': 101, 'binding_confirmed': True, 'generation': 1, 'instance_state': 'running', 'container_name': 'slot-1', 'gateway_port': 20003}]
                return {'pods': [{'id': 9, 'pod_uid': 'pod-9', 'slots': slots}]}
            m.api = api
            if persistent:
                with self.assertRaisesRegex(RuntimeError, 'did not converge'):
                    m.physical_proof([row])
                self.assertEqual(elapsed[0], 90)
            else:
                proof = m.physical_proof([row])
                self.assertEqual(len(requests), 2)
                self.assertEqual(proof[0]['container'], 'slot-1')
                self.assertEqual([r['confirmed'] for r in m.REPORT['physical_observations']], [0, 1])

    def test_delayed_physical_observation_requires_confirmation(self):
        self.check_physical_observation(False)

    def test_persistent_missing_physical_observation_fails_at_deadline(self):
        self.check_physical_observation(True)


if __name__ == '__main__':
    unittest.main()
