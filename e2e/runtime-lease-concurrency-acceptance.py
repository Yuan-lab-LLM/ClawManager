"""Server54: create owned canaries through CM, observe SQL read-only, then clean up.

prepare exercises two OpenClaw slots, fills the pool to one free slot, and starts
three more instances to exercise automatic expansion. Other Lite types receive
two concurrent fresh instances each. cleanup discovers and verifies every exact-name batch intent before deletion.
No global policy, Runtime Deployment or business SQL mutations.
"""
import argparse
import datetime
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import time
import uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('phase', choices=['prepare', 'cleanup'])
parser.add_argument('--root', required=True)
parser.add_argument('--timeout', type=int, default=420)
args = parser.parse_args()
ROOT = Path(args.root)
ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
os.umask(0o077)
NS = 'clawmanager-system'
CTX = ssl.create_default_context(cadata=(ROOT / 'system-ca.crt').read_text())
TOKEN = (ROOT / 'admin-token.private').read_text().strip()


def stamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def save():
    path = ROOT / 'acceptance.json'
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(REPORT, ensure_ascii=False, indent=2))
    temporary.replace(path)


def api(method, path, body=None, allow_missing=False):
    connection = http.client.HTTPSConnection('10.130.81.54', 30443, timeout=120)
    connection.sock = CTX.wrap_socket(socket.create_connection(('10.130.81.54', 30443), timeout=120), server_hostname='clawmanager.server54')
    connection.request(method, '/api/v1' + path, json.dumps(body) if body is not None else None, {'Authorization': 'Bearer ' + TOKEN, 'Content-Type': 'application/json'})
    response = connection.getresponse()
    status, raw = response.status, response.read()
    connection.close()
    if allow_missing and status == 404:
        return None
    data = json.loads(raw or '{}')
    if status >= 300:
        raise RuntimeError(f'{method} {path}: HTTP {status}: {data.get("message", data.get("error"))}')
    return data.get('data')


def sql(query):
    assert query.lstrip().upper().startswith('SELECT ')
    return subprocess.check_output(['kubectl', '-n', NS, 'exec', '-i', 'deploy/mysql', '--', 'sh', '-c', 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B clawmanager'], input=query.encode()).decode().rstrip('\n')


def record(case, passed, evidence):
    row = {'case': case, 'result': 'PASS' if passed else 'FAIL', 'at': stamp(), 'evidence': evidence}
    REPORT['results'].append(row)
    save()
    print(json.dumps(row, ensure_ascii=False), flush=True)
    assert passed, case


def owned_instance(row):
    assert any(row['name'] in batch['names'] for batch in REPORT['batches']), 'Instance has no exact batch intent'
    item = api('GET', f'/instances/{row["id"]}', allow_missing=True)
    if item is None:
        return None
    item = item['instance']
    assert item['name'] == row['name'] and row['name'].startswith(REPORT['run'] + '-'), 'Ownership mismatch'
    assert int(item['user_id']) == int(REPORT['user_id']), 'User ownership mismatch'
    return item


def bindings(rows):
    ids = ','.join(str(int(r['id'])) for r in rows)
    query = f'SELECT b.instance_id,b.runtime_pod_id,b.gateway_port,b.generation,b.state,b.gateway_id,p.pod_uid,p.pod_name FROM instance_runtime_bindings b JOIN runtime_pods p ON p.id=b.runtime_pod_id WHERE b.instance_id IN ({ids}) ORDER BY b.instance_id;'
    return [dict(zip(['instance_id', 'pod_id', 'port', 'generation', 'state', 'gateway_id', 'pod_uid', 'pod_name'], line.split('\t'))) for line in sql(query).splitlines()]


def wait_running(rows):
    deadline = time.monotonic() + args.timeout
    while time.monotonic() < deadline:
        ready = True
        for row in rows:
            item = owned_instance(row)
            assert item is not None, 'New instance disappeared'
            sample = {k: item.get(k) for k in ['id', 'status', 'runtime_generation', 'runtime_error_code', 'runtime_error_message']}
            trail = row.setdefault('samples', [])
            if not trail or trail[-1]['instance'] != sample:
                trail.append({'at': stamp(), 'instance': sample})
            save()
            assert item['status'] != 'error', f'Fresh instance {row["id"]} entered error: {sample}'
            assert item['runtime_generation'] == 1, 'First create required another generation'
            ready = ready and item['status'] == 'running'
        observed = bindings(rows)
        ready = ready and len(observed) == len(rows) and all(b['state'] == 'running' and b['generation'] == '1' for b in observed)
        if ready:
            return observed
        time.sleep(2)
    raise RuntimeError('Fresh concurrent create did not reach running within deadline')


def intended_names_sql():
    names = [name for batch in REPORT['batches'] for name in batch['names']]
    assert names and len(names) == len(set(names)), 'Missing or duplicate batch intent'
    assert all(name.startswith(REPORT['run'] + '-') for name in names), 'Intent run mismatch'
    # Binary hex literals avoid SQL quoting and connection/table collation drift.
    return ','.join('0x' + name.encode().hex() for name in names)


def discover_intended_owners():
    assert int(REPORT['user_id']) > 0 and 'batches' in REPORT, 'Missing durable ownership journal'
    if not REPORT['batches']:
        return not REPORT['owned']
    expected = {name: batch for batch in REPORT['batches'] for name in batch['names']}
    query = 'SELECT id,user_id,name,type FROM instances WHERE CAST(name AS BINARY) IN (' + intended_names_sql() + ') ORDER BY id;'
    found = set()
    for line in sql(query).splitlines():
        identifier, user, name, kind = line.split('\t')
        assert name in expected and int(user) == int(REPORT['user_id']), 'Discovered owner mismatch'
        batch = expected[name]
        assert kind == batch['type'] and int(batch['user_id']) == int(user), 'Discovered batch mismatch'
        assert name not in found, 'Duplicate exact-name owner; manual inspection required'
        found.add(name)
        row = {'id': int(identifier), 'name': name, 'type': kind, 'purpose': batch['purpose']}
        assert owned_instance(row) is not None, 'SQL owner disappeared during verification'
        previous = next((entry for entry in REPORT['owned'] if entry['name'] == name), None)
        assert previous is None or previous['id'] == row['id'], 'Exact name was replaced; manual inspection required'
        if previous is None:
            REPORT['owned'].append(row)
        owners = batch.setdefault('owners', {})
        assert name not in owners or owners[name] == row['id'], 'Journal owner changed'
        owners[name] = row['id']
        save()
    for batch in REPORT['batches']:
        # Recorded verified owners remain resolved after deletion. An intent
        # never observed is uncertain: it could still commit after this read.
        batch['outcome'] = 'resolved' if all(name in batch.get('owners', {}) for name in batch['names']) else 'uncertain'
    save()
    return all(batch['outcome'] == 'resolved' for batch in REPORT['batches'])


def create_batch(kind, count, purpose):
    template = {'type': kind, 'instance_mode': 'lite', 'runtime_type': 'gateway', 'cpu_cores': 1, 'memory_gb': 2, 'disk_gb': 5, 'os_type': kind, 'os_version': 'latest'}
    prefix = REPORT['run'] + '-' + purpose
    names = [prefix + f'-{index:03d}' for index in range(1, count + 1)]
    assert not any(name in batch['names'] for batch in REPORT['batches'] for name in names), 'Batch intent already exists; do not retry POST'
    intent = {'names': names, 'type': kind, 'purpose': purpose, 'user_id': REPORT['user_id'], 'outcome': 'posting', 'owners': {}, 'at': stamp()}
    REPORT['batches'].append(intent)
    save()  # Durable exact names and user precede the only mutating POST.
    try:
        result = api('POST', '/instances/batch/lite', {'name_prefix': prefix, 'count': count, 'start_index': 1, 'template': template})
        items = [entry['instance'] for entry in result['results'] if entry.get('instance')]
        assert len(items) == count and {item['name'] for item in items} == set(names), 'Partial or unexpected batch response'
        assert discover_intended_owners(), 'Batch outcome unresolved; manual inspection required'
    except Exception:
        intent['outcome'] = 'uncertain'
        save()
        try:
            discover_intended_owners()
        except Exception:
            # Keep the original failure. Cleanup retries read-only discovery,
            # and cannot PASS until every intended outcome is accounted for.
            save()
        raise
    rows = [row for row in REPORT['owned'] if row['name'] in names]
    for row in rows:
        assert owned_instance(row) is not None
        api('PUT', f'/admin/instances/{row["id"]}/auto-stop', {'auto_stop_disabled': True})
    return rows


def physical_proof(rows):
    deadline = time.monotonic() + 90
    last = []
    while time.monotonic() < deadline:
        pods = api('GET', '/admin/runtime-pods?runtime_type=' + rows[0]['type'])['pods']
        proofs = []
        for row in rows:
            item = owned_instance(row)
            assert item and item['status'] == 'running' and item['runtime_generation'] == 1, 'Fresh instance changed during physical confirmation'
            matches = [(p, s) for p in pods for s in p.get('slots', []) if s.get('instance_id') == row['id']]
            assert len(matches) <= 1, f'Duplicate physical owners for {row["id"]}'
            if len(matches) != 1:
                continue
            pod, slot = matches[0]
            if not (slot.get('binding_confirmed') and slot.get('generation') == 1 and slot.get('instance_state') == 'running'):
                continue
            proofs.append({'instance_id': row['id'], 'pod_id': pod['id'], 'pod_uid': pod['pod_uid'], 'container': slot['container_name'], 'port': slot['gateway_port'], 'generation': slot['generation'], 'binding_confirmed': slot['binding_confirmed']})
        last = [{'pod_id': pod['id'], 'slots': [{k: slot.get(k) for k in ['container_name', 'instance_id', 'generation', 'binding_confirmed', 'reconciliation_state', 'observation_message']} for slot in pod.get('slots', [])]} for pod in pods]
        REPORT.setdefault('physical_observations', []).append({'at': stamp(), 'ids': [r['id'] for r in rows], 'confirmed': len(proofs), 'expected': len(rows)})
        save()
        if len(proofs) == len(rows):
            return proofs
        time.sleep(2)
    raise RuntimeError('Physical ownership confirmation did not converge: ' + json.dumps(last))


def cleanup():
    failures = []
    try:
        resolved = discover_intended_owners()
    except Exception as error:
        resolved = False
        failures.append({'discovery_error': str(error)})
    for row in REPORT['owned']:
        try:
            item = owned_instance(row)
            if item is not None:
                api('DELETE', f'/instances/{row["id"]}')
        except Exception as error:
            failures.append({'id': row['id'], 'error': str(error)})
        save()
    deadline = time.monotonic() + args.timeout
    while time.monotonic() < deadline:
        pending = []
        for row in REPORT['owned']:
            row['deleted'] = owned_instance(row) is None
            if not row['deleted']:
                pending.append(row['id'])
        save()
        if not pending:
            break
        time.sleep(2)
    ids = ','.join(str(int(r['id'])) for r in REPORT['owned'])
    remaining = sql('SELECT id FROM instances WHERE CAST(name AS BINARY) IN (' + intended_names_sql() + ');') if REPORT['batches'] else ''
    residual_bindings = sql(f'SELECT instance_id FROM instance_runtime_bindings WHERE instance_id IN ({ids});') if ids else ''
    REPORT['policy_after'] = api('GET', '/admin/runtime-auto-stop')
    policy_keys = ['enabled', 'check_interval_days', 'check_interval_seconds', 'idle_timeout_days', 'idle_timeout_seconds', 'version']
    policy_unchanged = all(REPORT['policy_before'].get(k) == REPORT['policy_after'].get(k) for k in policy_keys)
    REPORT['finished_at'] = stamp()
    record('Owned instance and binding cleanup; global policy unchanged', resolved and not remaining and not residual_bindings and all(r.get('deleted') for r in REPORT['owned']) and policy_unchanged, {'all_intents_resolved': resolved, 'remaining_instance_rows': remaining, 'remaining_bindings': residual_bindings, 'policy_unchanged': policy_unchanged, 'delete_request_errors': failures})


if args.phase == 'prepare':
    assert not (ROOT / 'acceptance.json').exists(), 'Use a new run directory'
    REPORT = {'user_id': api('GET', '/auth/me')['id'], 'batches': [], 'run': 'lease-' + uuid.uuid4().hex[:10], 'started_at': stamp(), 'version': api('GET', '/version'), 'policy_before': api('GET', '/admin/runtime-auto-stop'), 'owned': [], 'results': [], 'namespace': NS, 'sql_read_only': True, 'runtime_images_changed': False}
    save()
    try:
        pods = api('GET', '/admin/runtime-pods?runtime_type=openclaw')['pods']
        eligible = [p for p in pods if p['state'] == 'ready' and not p['draining'] and not p['scheduling_paused'] and p.get('deployment_name') == 'openclaw-runtime']
        assert len(eligible) == 1, 'Exact one-free-slot scenario requires one serving OpenClaw Pod'
        base = eligible[0]
        REPORT['original_openclaw_pod'] = {k: base.get(k) for k in ['id', 'pod_uid', 'capacity', 'used_slots']}
        slots_free = [s for s in base['slots'] if s.get('ready') and not s.get('instance_id') and s.get('reconciliation_state') == 'consistent']
        assert len(slots_free) >= 3, 'Insufficient healthy physical slots for concurrent test'
        first = create_batch('openclaw', 2, 'two-slots')
        first_bindings = wait_running(first)
        proof = physical_proof(first)
        record('OpenClaw two slots first-generation concurrent start', all(b['pod_id'] == str(base['id']) for b in first_bindings) and any(p['container'] == 'slot-1' for p in proof), proof)
        fill_count = base['capacity'] - base['used_slots'] - 3
        if fill_count:
            fill = create_batch('openclaw', fill_count, 'fill')
            wait_running(fill)
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            pods = api('GET', '/admin/runtime-pods?runtime_type=openclaw')['pods']
            current = next(p for p in pods if p['id'] == base['id'])
            free = [s for s in current['slots'] if s.get('ready') and not s.get('instance_id') and s.get('reconciliation_state') == 'consistent']
            if len(free) == 1 and current['used_slots'] == current['capacity'] - 1:
                break
            time.sleep(2)
        record('OpenClaw one physical slot remains before burst', len(free) == 1 and current['used_slots'] == current['capacity'] - 1, {'pod_id': base['id'], 'capacity': current['capacity'], 'used_slots': current['used_slots'], 'free_containers': [s['container_name'] for s in free]})
        burst = create_batch('openclaw', 3, 'burst')
        observed = wait_running(burst)
        proof = physical_proof(burst)
        record('OpenClaw three creates with one free slot and automatic scale-out', any(b['pod_id'] == str(base['id']) for b in observed) and any(b['pod_id'] != str(base['id']) for b in observed), proof)
        for kind in ['hermes', 'opencode', 'deepseek-harness']:
            rows = create_batch(kind, 2, kind)
            wait_running(rows)
            record(kind + ' concurrent first-generation create', True, physical_proof(rows))
        observed = bindings(REPORT['owned'])
        pairs = [(b['pod_uid'], b['port']) for b in observed]
        record('All owned bindings have unique physical slots', len(observed) == len(REPORT['owned']) and len(pairs) == len(set(pairs)), observed)
        REPORT['prepared_at'] = stamp()
        save()
    except Exception as error:
        REPORT['results'].append({'case': 'prepare aborted', 'result': 'FAIL', 'at': stamp(), 'evidence': str(error)})
        save()
        raise
else:
    REPORT = json.loads((ROOT / 'acceptance.json').read_text())
    cleanup()
