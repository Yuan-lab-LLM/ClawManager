"""Owned test instances only; real automatic sleep and CM API wake, read-only SQL.

Run on 10.130.81.54: prepare creates 4 singles + 4 batches of 2 and waits
for native automatic sleep. finish wakes remaining sleepers, verifies generation
and file integrity, then removes only the recorded test-owned instances.
No Runtime deployment, global policy, business timestamps or ledger mutations.
"""
import argparse
import html
import platform
import sys
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

ALL_TYPES = ['openclaw', 'hermes', 'opencode', 'deepseek-harness']
ALL_PURPOSES = ['single', 'batch']

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('phase', choices=['prepare', 'finish', 'report'])
parser.add_argument('--root', required=True)
parser.add_argument('--types', nargs='+', choices=ALL_TYPES, default=ALL_TYPES)
parser.add_argument('--purposes', nargs='+', choices=ALL_PURPOSES, default=ALL_PURPOSES)
parser.add_argument('--batch-count', type=int, default=2)
parser.add_argument('--sleep-timeout', type=int, default=420)
parser.add_argument('--start-timeout', type=int, default=240)
parser.add_argument('--metadata', action='append', default=[])
parser.add_argument('--token-file')
args = parser.parse_args()
if args.batch_count < 1 or args.batch_count > 100:
    parser.error('--batch-count must be between 1 and 100')
ROOT = Path(args.root)
ROOT.mkdir(parents=True, exist_ok=True)
os.chmod(ROOT, 0o700)
def render_report():
    report = json.loads((ROOT / 'report.json').read_text())
    rows = ''.join('<tr><td>' + '</td><td>'.join(html.escape(str(row.get(key, 'UNKNOWN'))) for key in ['type', 'case', 'result', 'duration_seconds', 'evidence']) + '</td></tr>' for row in report.get('results', []))
    document = '<!doctype html><meta charset="utf-8"><title>Runtime lifecycle acceptance</title><style>body{font:14px system-ui;margin:32px}table{border-collapse:collapse;width:100%}td,th{border:1px solid #ccc;padding:8px;white-space:pre-wrap;overflow-wrap:anywhere}pre{white-space:pre-wrap}</style><h1>Runtime lifecycle acceptance</h1><p>Missing or blocked evidence is not a pass.</p><table><tr><th>Type</th><th>Case</th><th>Status</th><th>Seconds</th><th>Evidence</th></tr>' + rows + '</table><h2>Full evidence and cleanup</h2><pre>' + html.escape(json.dumps(report, ensure_ascii=False, indent=2)) + '</pre>'
    (ROOT / 'report.html').write_text(document)

if args.phase == 'report':
    render_report()
    sys.exit(0)
if not args.token_file:
    parser.error('--token-file is required for prepare and finish')
TOKEN = Path(args.token_file).read_text().strip()
TYPES = list(dict.fromkeys(args.types))
PURPOSES = list(dict.fromkeys(args.purposes))
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
    if method in ('POST', 'DELETE') and 'REPORT' in globals():
        REPORT.setdefault('api_observations', []).append({'at': stamp(), 'method': method, 'path': path, 'status': status})
    if raw and status < 300: return data
    result = json.loads(data or '{}')
    if status >= 300: raise RuntimeError(f'{method} {path} HTTP {status}: {result.get("error", result.get("message"))}')
    return result.get('data')


def sql(query):
    assert query.lstrip().upper().startswith('SELECT ')
    return subprocess.check_output(['kubectl', '-n', NS, 'exec', '-i', 'deploy/mysql', '--', 'sh', '-c', 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B clawmanager'], input=query.encode()).decode().strip()


def brief(item):
    return {k: item.get(k) for k in ['id', 'name', 'type', 'status', 'runtime_generation', 'stopped_reason', 'auto_stop_phase', 'auto_stop_blocker', 'runtime_error_message']}


def set_exclusion(row, disabled):
    item = current(row['id'])
    assert item['name'] == row['name'] and item['name'].startswith(REPORT['run'] + '-'), 'Ownership mismatch'
    api('PUT', f'/admin/instances/{row["id"]}/auto-stop', {'auto_stop_disabled': disabled})
    row['auto_stop_disabled'] = disabled
    row.setdefault('exclusion_history', []).append({'at': stamp(), 'disabled': disabled})
    save('report.json', REPORT)


def current(i): return api('GET', f'/instances/{i}')['instance']


def record(kind, name, result, evidence):
    row = {'type': kind, 'case': name, 'result': result, 'at': stamp(), 'duration_seconds': round(time.monotonic() - tick, 2) if 'tick' in globals() else 0, 'evidence': evidence}
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
            elif desired == 'manual-stopped':
                ready = item['status'] == 'stopped' and item.get('stopped_reason') == 'manual' and item.get('auto_stop_phase') == 'completed' and sql(f'SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id={i};') == '0'
            else:
                ready = item['status'] == 'stopped' and item.get('stopped_reason') == 'idle_timeout' and sql(f'SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id={i};') == '0'
            if ready:
                if desired == 'running' and args.phase == 'finish' and not owned.get('auto_stop_disabled'):
                    set_exclusion(owned, True)
                owned[desired] = sample
                owned.setdefault('binding_evidence', []).append({'at': stamp(), 'operation_state': desired, 'rows_tsv': sql(f'SELECT id,instance_id,runtime_pod_id,gateway_id,gateway_port,state,generation FROM instance_runtime_bindings WHERE instance_id={i};')})
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
    REPORT = {'run': 'lc-' + uuid.uuid4().hex[:10], 'runner_version': 4, 'selected_types': TYPES, 'selected_purposes': PURPOSES, 'batch_count': args.batch_count, 'machine': platform.platform(), 'metadata': {str(Path(p)): json.loads(Path(p).read_text()) for p in args.metadata}, 'started_at': stamp(), 'version': api('GET', '/version'), 'policy': api('GET', '/admin/runtime-auto-stop'), 'owned': [], 'results': [], 'sql_read_only': True, 'runner_changes_global_policy': False, 'runtime_images_changed': False}
    sleep_allowed = REPORT['policy'].get('enabled') and REPORT['policy'].get('idle_timeout_seconds', 999999) <= 300
    save('report.json', REPORT)
    for kind in TYPES:
        template = {'type': kind, 'instance_mode': 'lite', 'runtime_type': 'gateway', 'cpu_cores': 1, 'memory_gb': 2, 'disk_gb': 5, 'os_type': kind, 'os_version': 'latest'}
        for purpose in PURPOSES:
            tick = time.monotonic()
            try:
                name = REPORT['run'] + '-' + kind.replace('deepseek-harness', 'dsh') + '-' + purpose
                if purpose == 'single':
                    created = [api('POST', '/instances', {**template, 'name': name})]
                else:
                    result = api('POST', '/instances/batch/lite', {'name_prefix': name, 'count': REPORT['batch_count'], 'start_index': 1, 'template': template})
                    created = [r['instance'] for r in result['results'] if r.get('instance')]
                    # Persist every successful member even if the batch is partial.
                group = []
                for item in created:
                    row = {'id': item['id'], 'name': item['name'], 'type': kind, 'purpose': purpose, 'initial_generation': item.get('runtime_generation', 0)}
                    REPORT['owned'].append(row); group.append(row)
                save('report.json', REPORT)
                for row in group:
                    set_exclusion(row, True)
                expected = 1 if purpose == 'single' else REPORT.get('batch_count', 2)
                assert len(group) == expected, f'Expected {expected}, got {len(group)}'
                assert wait_group(group, 'running', args.start_timeout), 'Not all instances reached running'
                for row in group:
                    row['before_generation'] = row['running']['runtime_generation']
                    row['marker'] = upload(row['id'])
                record(kind, purpose + ':create', 'PASS', {'ids': [r['id'] for r in group], 'seconds': round(time.monotonic() - tick, 2)})
            except Exception as error: record(kind, purpose + ':create', 'FAIL', str(error))
    candidates = [r for r in REPORT['owned'] if r.get('marker')]
    for row in candidates:
        set_exclusion(row, False)
    tick = time.monotonic()
    if sleep_allowed:
        wait_group(candidates, 'stopped', min(args.sleep_timeout, 420))
    for kind in TYPES:
        for purpose in PURPOSES:
            group = [r for r in candidates if r['type'] == kind and r['purpose'] == purpose]
            expected = 1 if purpose == 'single' else REPORT.get('batch_count', 2)
            passed = len(group) == expected and all(r.get('stopped') and r.get('sleep_proof') for r in group)
            record(kind, purpose + ':sleep', 'PASS' if passed else ('FAIL' if sleep_allowed else 'BLOCKED'), {'policy': REPORT['policy'], 'instances': [{k: r.get(k) for k in ['id', 'stopped', 'sleep_proof', 'failure']} for r in group]})
    save('sleep-ready.json', {'run': REPORT['run'], 'instances': [{k: r[k] for k in ['id', 'name', 'type', 'purpose', 'before_generation']} for r in candidates if r.get('stopped')]})
else:
    REPORT = json.loads((ROOT / 'report.json').read_text())
    TYPES = REPORT.get('selected_types', ALL_TYPES)
    PURPOSES = REPORT.get('selected_purposes', ALL_PURPOSES)
    for kind in TYPES:
        for purpose in PURPOSES:
            group = [r for r in REPORT['owned'] if r['type'] == kind and r['purpose'] == purpose and r.get('stopped')]
            tick = time.monotonic()
            if len(group) != (1 if purpose == 'single' else REPORT.get('batch_count', 2)):
                record(kind, purpose + ':wake', 'BLOCKED', 'Automatic sleep prerequisite failed'); continue
            try:
                def wake(row):
                    row.pop('failure', None)
                    if current(row['id'])['status'] == 'stopped': api('POST', f'/instances/{row["id"]}/start', {})
                with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool: list(pool.map(wake, group))
                assert wait_group(group, 'running', args.start_timeout), 'Wake did not reach running'
                for row in group:
                    item = current(row['id'])
                    assert item['runtime_generation'] > row['before_generation'], 'Wake must establish a new generation'
                    raw = api('GET', f'/instances/{row["id"]}/workspace/download?path=' + urllib.parse.quote(row['marker']['path']), raw=True)
                    assert hashlib.sha256(raw).hexdigest() == row['marker']['sha256'], 'Workspace content changed'
                    row['integrity_preserved'] = True
                for row in group:
                    set_exclusion(row, True)
                record(kind, purpose + ':wake', 'PASS', {'ids': [r['id'] for r in group], 'seconds': round(time.monotonic() - tick, 2), 'new_generation': True, 'sha256_preserved': True})
            except Exception as error: record(kind, purpose + ':wake', 'FAIL', str(error))
    for kind in TYPES:
        for purpose in PURPOSES:
            group = [r for r in REPORT['owned'] if r['type'] == kind and r['purpose'] == purpose]
            for operation in ['stop', 'start', 'restart']:
                tick = time.monotonic()
                if len(group) != (1 if purpose == 'single' else REPORT.get('batch_count', 2)):
                    record(kind, purpose + ':' + operation, 'BLOCKED', 'Create prerequisite incomplete')
                    continue
                try:
                    before = {r['id']: current(r['id'])['runtime_generation'] for r in group}
                    def operate(row):
                        item = current(row['id'])
                        assert item['name'] == row['name'] and item['name'].startswith(REPORT['run'] + '-'), 'Ownership mismatch'
                        row.pop('failure', None)
                        api('POST', f'/instances/{row["id"]}/{operation}', {})
                    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                        list(pool.map(operate, group))
                    assert wait_group(group, 'manual-stopped' if operation == 'stop' else 'running', args.start_timeout), 'Lifecycle readiness timeout'
                    for row in group:
                        if operation != 'stop':
                            item = current(row['id'])
                            assert item['runtime_generation'] > before[row['id']], 'Generation did not advance'
                            if row.get('marker'):
                                raw = api('GET', f'/instances/{row["id"]}/workspace/download?path=' + urllib.parse.quote(row['marker']['path']), raw=True)
                                assert hashlib.sha256(raw).hexdigest() == row['marker']['sha256'], 'Workspace changed'
                    record(kind, purpose + ':' + operation, 'PASS', {'ids': [r['id'] for r in group], 'instances': [brief(current(r['id'])) for r in group]})
                except Exception as error:
                    record(kind, purpose + ':' + operation, 'FAIL', str(error))
    tick = time.monotonic()
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

    for kind in TYPES:
        for purpose in PURPOSES:
            group = [r for r in REPORT['owned'] if r['type'] == kind and r['purpose'] == purpose]
            record(kind, purpose + ':delete', 'PASS' if len(group) == (1 if purpose == 'single' else REPORT.get('batch_count', 2)) and all(r.get('deleted') for r in group) else 'FAIL', {'instances': group})
    render_report()
    sys.exit(1 if any(r['result'] == 'FAIL' for r in REPORT['results']) else 0)
