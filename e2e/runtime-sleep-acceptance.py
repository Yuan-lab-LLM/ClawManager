"""Owned test instances only; real automatic sleep and CM API wake, read-only SQL.

Run on 10.130.81.54: prepare creates 4 singles + 4 batches of 2 and waits
for native automatic sleep. finish wakes remaining sleepers, verifies generation
and file integrity, then removes only the recorded test-owned instances.
No Runtime deployment, global policy, business timestamps or ledger mutations.
"""
import argparse
import concurrent.futures
import datetime
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import time
import urllib.parse
import uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('phase', choices=['prepare', 'finish'])
parser.add_argument('--root', required=True)
parser.add_argument('--token-file', required=True)
args = parser.parse_args()
ROOT = Path(args.root)
ROOT.mkdir(parents=True, exist_ok=True)
os.chmod(ROOT, 0o700)
TOKEN = Path(args.token_file).read_text().strip()
TYPES = ['openclaw', 'hermes', 'opencode', 'deepseek-harness']
NS = 'clawmanager-system'
cert = subprocess.check_output(['kubectl', '-n', NS, 'exec', 'deploy/clawmanager-app', '--', 'cat', '/etc/nginx/tls/tls.crt']).decode()
CTX = ssl.create_default_context(cadata=cert)


def stamp(): return datetime.datetime.now(datetime.timezone.utc).isoformat()


def save(name, obj):
    p = ROOT / name
    p.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + '\n')
    p.chmod(0o600)


def api(method, path, body=None, content_type='application/json', raw=False):
    connection = http.client.HTTPSConnection('10.130.81.54', 30443, timeout=120)
    connection.sock = CTX.wrap_socket(socket.create_connection(('10.130.81.54', 30443), timeout=120), server_hostname='clawmanager.server54')
    payload = body if isinstance(body, bytes) else json.dumps(body).encode() if body is not None else None
    connection.request(method, '/api/v1' + path, payload, {'Authorization': 'Bearer ' + TOKEN, 'Content-Type': content_type})
    response = connection.getresponse()
    data = response.read()
    status = response.status
    connection.close()
    if raw and status < 300: return data
    result = json.loads(data or '{}')
    if status >= 300: raise RuntimeError(f'{method} {path} HTTP {status}: {result.get("error", result.get("message"))}')
    return result.get('data')


def sql(query):
    assert query.lstrip().upper().startswith('SELECT ')
    return subprocess.check_output(['kubectl', '-n', NS, 'exec', '-i', 'deploy/mysql', '--', 'sh', '-c', 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B clawmanager'], input=query.encode()).decode().strip()


def brief(item):
    return {k: item.get(k) for k in ['id', 'name', 'type', 'status', 'runtime_generation', 'stopped_reason', 'auto_stop_phase', 'auto_stop_blocker', 'runtime_error_message']}


def current(i): return api('GET', f'/instances/{i}')['instance']


def record(kind, name, result, evidence):
    row = {'type': kind, 'case': name, 'result': result, 'at': stamp(), 'evidence': evidence}
    REPORT['results'].append(row)
    save('report.json', REPORT)
    print(json.dumps(row, ensure_ascii=False), flush=True)


def upload(i):
    content = ('CM UIUX2 sleep integrity ' + REPORT['run'] + ' #' + str(i)).encode()
    boundary = 'CMTest' + uuid.uuid4().hex
    payload = ('--' + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="uiux2-sleep-canary.txt"\r\nContent-Type: text/plain\r\n\r\n').encode() + content + ('\r\n--' + boundary + '--\r\n').encode()
    r = api('POST', f'/instances/{i}/workspace/upload?path=', payload, 'multipart/form-data; boundary=' + boundary)
    return {'path': r.get('path', 'uiux2-sleep-canary.txt'), 'sha256': hashlib.sha256(content).hexdigest()}


def wait_group(rows, desired, seconds):
    pending = {r['id']: r for r in rows}
    deadline = time.monotonic() + seconds
    last_log = 0
    while pending and time.monotonic() < deadline:
        for i, owned in list(pending.items()):
            item = current(i)
            sample = brief(item)
            trail = owned.setdefault('samples', [])
            if not trail or trail[-1]['instance'] != sample: trail.append({'at': stamp(), 'instance': sample})
            if desired == 'running':
                ready = item['status'] == 'running' and sql(f"SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id={i} AND state='running';") == '1'
            else:
                ready = item['status'] == 'stopped' and item.get('stopped_reason') == 'idle_timeout' and sql(f'SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id={i};') == '0'
            if ready:
                owned[desired] = sample
                if desired == 'stopped':
                    proof = sql(f"SELECT phase,generation,stop_kind FROM instance_idle_stop_operations WHERE instance_id={i} ORDER BY created_at DESC LIMIT 1;")
                    if not proof.startswith('completed\t'): continue
                    owned['sleep_proof'] = proof
                del pending[i]
            elif item['status'] == 'error':
                owned['failure'] = sample
                del pending[i]
        save('report.json', REPORT)
        if time.monotonic() - last_log > 15:
            print(json.dumps({'waiting_for': desired, 'remaining_ids': list(pending), 'at': stamp()}), flush=True)
            last_log = time.monotonic()
        if pending: time.sleep(3)
    for i, owned in pending.items(): owned['failure'] = {'timeout_seconds': seconds, 'waiting_for': desired, 'last': owned['samples'][-1]}
    return not pending and all('failure' not in r for r in rows)


if args.phase == 'prepare':
    assert not (ROOT / 'report.json').exists(), 'Use a new run directory'
    REPORT = {'run': 'u2-' + str(int(time.time()))[-7:], 'started_at': stamp(), 'version': api('GET', '/version'), 'policy': api('GET', '/admin/runtime-auto-stop'), 'owned': [], 'results': [], 'sql_read_only': True, 'global_policy_changed': False, 'runtime_images_changed': False}
    assert REPORT['policy']['enabled'], 'Existing automatic sleep policy is disabled'
    assert REPORT['policy']['idle_timeout_seconds'] <= 300, 'Existing policy exceeds bounded test window'
    save('report.json', REPORT)
    for kind in TYPES:
        template = {'type': kind, 'instance_mode': 'lite', 'runtime_type': 'gateway', 'cpu_cores': 1, 'memory_gb': 2, 'disk_gb': 5, 'os_type': kind, 'os_version': 'latest'}
        for purpose in ['single', 'batch']:
            tick = time.monotonic()
            try:
                name = REPORT['run'] + '-' + kind.replace('deepseek-harness', 'dsh') + '-' + purpose
                if purpose == 'single':
                    created = [api('POST', '/instances', {**template, 'name': name})]
                else:
                    result = api('POST', '/instances/batch/lite', {'name_prefix': name, 'count': 2, 'start_index': 1, 'template': template})
                    created = [r['instance'] for r in result['results'] if r.get('instance')]
                    # Persist every successful member even if the batch is partial.
                group = []
                for item in created:
                    row = {'id': item['id'], 'name': item['name'], 'type': kind, 'purpose': purpose, 'initial_generation': item.get('runtime_generation', 0)}
                    REPORT['owned'].append(row); group.append(row)
                save('report.json', REPORT)
                expected = 1 if purpose == 'single' else 2
                assert len(group) == expected, f'Expected {expected}, got {len(group)}'
                assert wait_group(group, 'running', 150), 'Not all instances reached running'
                for row in group:
                    row['before_generation'] = row['running']['runtime_generation']
                    row['marker'] = upload(row['id'])
                record(kind, purpose + ':create', 'PASS', {'ids': [r['id'] for r in group], 'seconds': round(time.monotonic() - tick, 2)})
            except Exception as error: record(kind, purpose + ':create', 'FAIL', str(error))
    candidates = [r for r in REPORT['owned'] if r.get('marker')]
    wait_group(candidates, 'stopped', 420)
    for kind in TYPES:
        for purpose in ['single', 'batch']:
            group = [r for r in candidates if r['type'] == kind and r['purpose'] == purpose]
            expected = 1 if purpose == 'single' else 2
            passed = len(group) == expected and all(r.get('stopped') and r.get('sleep_proof') for r in group)
            record(kind, purpose + ':sleep', 'PASS' if passed else 'FAIL', {'instances': [{k: r.get(k) for k in ['id', 'stopped', 'sleep_proof', 'failure']} for r in group]})
    save('sleep-ready.json', {'run': REPORT['run'], 'instances': [{k: r[k] for k in ['id', 'name', 'type', 'purpose', 'before_generation']} for r in candidates if r.get('stopped')]})
else:
    REPORT = json.loads((ROOT / 'report.json').read_text())
    for kind in TYPES:
        for purpose in ['single', 'batch']:
            group = [r for r in REPORT['owned'] if r['type'] == kind and r['purpose'] == purpose and r.get('stopped')]
            tick = time.monotonic()
            if len(group) != (1 if purpose == 'single' else 2):
                record(kind, purpose + ':wake', 'BLOCKED', 'Automatic sleep prerequisite failed'); continue
            try:
                def wake(row):
                    if current(row['id'])['status'] == 'stopped': api('POST', f'/instances/{row["id"]}/start', {})
                with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool: list(pool.map(wake, group))
                assert wait_group(group, 'running', 150), 'Wake did not reach running'
                for row in group:
                    item = current(row['id'])
                    assert item['runtime_generation'] > row['before_generation'], 'Wake must establish a new generation'
                    raw = api('GET', f'/instances/{row["id"]}/workspace/download?path=' + urllib.parse.quote(row['marker']['path']), raw=True)
                    assert hashlib.sha256(raw).hexdigest() == row['marker']['sha256'], 'Workspace content changed'
                    row['integrity_preserved'] = True
                record(kind, purpose + ':wake', 'PASS', {'ids': [r['id'] for r in group], 'seconds': round(time.monotonic() - tick, 2), 'new_generation': True, 'sha256_preserved': True})
            except Exception as error: record(kind, purpose + ':wake', 'FAIL', str(error))
    for row in REPORT['owned']:
        try:
            item = current(row['id'])
            assert item['name'] == row['name'] and item['name'].startswith(REPORT['run'] + '-'), 'Test ownership mismatch'
            api('DELETE', f'/instances/{row["id"]}')
            row['cleanup_submitted'] = True
        except Exception as error: row['cleanup_error'] = str(error)
    deadline = time.monotonic() + 120
    pending = [r for r in REPORT['owned'] if r.get('cleanup_submitted')]
    while pending and time.monotonic() < deadline:
        for row in pending[:]:
            try: current(row['id'])
            except RuntimeError as error:
                if 'HTTP 404:' in str(error): row['deleted'] = True; pending.remove(row)
                else: row['cleanup_error'] = str(error)
        if pending: time.sleep(3)
    record('platform', 'cleanup', 'PASS' if all(r.get('deleted') for r in REPORT['owned']) else 'FAIL', {'deleted': [r['id'] for r in REPORT['owned'] if r.get('deleted')], 'remaining': [r['id'] for r in REPORT['owned'] if not r.get('deleted')]})
    REPORT['finished_at'] = stamp(); save('report.json', REPORT)
