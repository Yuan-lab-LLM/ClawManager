"""Run on server54. Exercises the actual ConfigMap shell functions, no MySQL needed."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

MANIFEST = Path(__file__).parents[1] / '31-mysql-full-backup-cronjob.yaml'

def helper():
    text = MANIFEST.read_text()
    marker = '  attempt-ownership.sh: |\n'
    if marker not in text:
        return 'exit 99\n'
    return '\n'.join(line[4:] for line in text.split(marker, 1)[1].split('\n  full-backup.sh:', 1)[0].splitlines())

class Attempts(unittest.TestCase):
    def run_shell(self, commands):
        with tempfile.TemporaryDirectory() as root:
            script = Path(root) / 'helper.sh'
            script.write_text(helper())
            return subprocess.run(['bash', '-c', 'set -Eeuo pipefail; source "$1"; FULL_ROOT="$2/full"; mkdir "$FULL_ROOT"; ' + commands, 'test', str(script), root], text=True, capture_output=True)

    def test_owned_stale_attempt_reclaimed_unknown_and_symlink_preserved(self):
        result = self.run_shell('''mkdir "$FULL_ROOT/legacy.partial" "$FULL_ROOT/old.partial"; printf 'cm-mysql-full-v1\\nold\\n' > "$FULL_ROOT/old.partial/owner"; touch -d '3 days ago' "$FULL_ROOT/old.partial/heartbeat"; ln -s "$FULL_ROOT/old.partial" "$FULL_ROOT/link.partial"; reclaim_partial_attempts; test ! -e "$FULL_ROOT/old.partial"; test -d "$FULL_ROOT/legacy.partial"; test -L "$FULL_ROOT/link.partial"''')
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_active_holder_and_recent_heartbeat_survive(self):
        result = self.run_shell('''mkdir "$FULL_ROOT/live.partial" "$FULL_ROOT/recent.partial"; printf 'cm-mysql-full-v1\\nlive\\n' > "$FULL_ROOT/live.partial/owner"; printf 'cm-mysql-full-v1\\nrecent\\n' > "$FULL_ROOT/recent.partial/owner"; touch -d '3 days ago' "$FULL_ROOT/live.partial/heartbeat"; touch "$FULL_ROOT/recent.partial/heartbeat"; (exec 8>"$FULL_ROOT/.attempt-live.lock"; flock -x 8; touch "$FULL_ROOT/ready"; sleep 4) & holder=$!; while [ ! -f "$FULL_ROOT/ready" ]; do sleep .05; done; reclaim_partial_attempts; test -d "$FULL_ROOT/live.partial"; test -d "$FULL_ROOT/recent.partial"; wait "$holder"''')
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_mismatched_marker_and_symlink_control_files_survive(self):
        result = self.run_shell('''mkdir "$FULL_ROOT/mismatch.partial" "$FULL_ROOT/unsafe.partial"; printf 'cm-mysql-full-v1\\nwrong\\n' > "$FULL_ROOT/mismatch.partial/owner"; printf 'cm-mysql-full-v1\\nunsafe\\n' > "$FULL_ROOT/unsafe.partial/owner"; touch -d '3 days ago' "$FULL_ROOT/mismatch.partial/heartbeat"; ln -s "$FULL_ROOT/mismatch.partial/heartbeat" "$FULL_ROOT/unsafe.partial/heartbeat"; reclaim_partial_attempts; test -d "$FULL_ROOT/mismatch.partial"; test -d "$FULL_ROOT/unsafe.partial"''')
        self.assertEqual(result.returncode, 0, result.stderr)


class FullScript(unittest.TestCase):
    def test_failed_dump_retry_and_parallel_holder(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            (root / 'bin').mkdir()
            (root / 'backup').mkdir()
            (root / 'helper.sh').write_text(helper())
            text = MANIFEST.read_text().split('  full-backup.sh: |\n', 1)[1].split('\n---', 1)[0]
            script = '\n'.join(line[4:] for line in text.splitlines())
            script = script.replace('readonly BACKUP_ROOT=/backup', 'readonly BACKUP_ROOT="${TEST_BACKUP_ROOT:?}"').replace('source /scripts/attempt-ownership.sh', 'source "${TEST_HELPER:?}"')
            (root / 'run.sh').write_text(script)
            mysql = root / 'bin/mysql'
            mysql.write_text('#!/bin/bash\nif [[ "$*" == *--version* ]]; then echo mysql-test; else echo test-gtid; fi\n')
            dump = root / 'bin/mysqldump'
            dump.write_text('''#!/bin/bash
if [[ "$*" == *--version* ]]; then echo mysqldump-test; exit; fi
if [ -e "$TEST_FAIL" ]; then echo interrupted; exit 9; fi
printf '%s\\n' "-- CHANGE REPLICATION SOURCE TO SOURCE_LOG_FILE='binlog.000001', SOURCE_LOG_POS=123;" 'CREATE TABLE restored(id INT);'
''')
            mysql.chmod(0o700)
            dump.chmod(0o700)
            fail = root / 'fail'
            fail.touch()
            env = dict(os.environ, PATH=str(root / 'bin') + ':' + os.environ['PATH'], MYSQL_BACKUP_PASSWORD='test', TEST_BACKUP_ROOT=str(root / 'backup'), TEST_HELPER=str(root / 'helper.sh'), TEST_FAIL=str(fail))
            failed = subprocess.run(['bash', str(root / 'run.sh')], env=env, capture_output=True, text=True, timeout=5)
            self.assertNotEqual(failed.returncode, 0)
            partial = list((root / 'backup/full').glob('*.partial'))
            self.assertEqual(len(partial), 1)
            self.assertTrue((partial[0] / 'owner').is_file())
            fail.unlink()
            successful = subprocess.run(['bash', str(root / 'run.sh')], env=env, capture_output=True, text=True, timeout=5)
            self.assertEqual(successful.returncode, 0, successful.stderr)
            complete = list((root / 'backup/full').glob('*/complete'))
            self.assertEqual(len(complete), 1)
            self.assertTrue(partial[0].exists())

if __name__ == '__main__':
    unittest.main()
