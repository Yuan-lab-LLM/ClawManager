import assert from 'node:assert/strict';
import { test } from 'node:test';
import { summarizePod, filterPods, slotState, availableTargets, canManageSlot, canEvictPod, recoveryInstanceIds, failedMoveRetrySelections } from '../src/lib/runtimeManagement.ts';

const pod = { id: 1, runtime_type: 'opencode', namespace: 'runtime', deployment_name: 'code', pod_name: 'code-a', node_name: 'node-a', state: 'ready', agent_reported: true, capacity: 4, used_slots: 2, draining: false, slots: [
  { index: 1, ready: true, instance_id: 10, instance_name: 'builder', owner: 'alice', instance_state: 'running' },
  { index: 2, ready: true, instance_id: 11, instance_state: 'error' },
  { index: 3, ready: true },
  { index: 4, ready: false, state: 'waiting' },
] };

test('agent health remains independent from bound instance state', () => {
  assert.deepEqual(summarizePod(pod), { capacity: 4, ready: 3, occupied: 2, bound: undefined, residual: 0, available: 1, running: 1, starting: 0, migrating: 0, error: 1, idle: 1, releasing: 0, unknown: 0, unavailable: 1 });
  assert.equal(slotState(pod.slots[1]), 'error');
  assert.equal(slotState({ ready: false, instance_id: 10, instance_state: 'running' }), 'unavailable');
});
test('paused and reserved pools retain occupancy but accept no incoming instances', () => {
  for (const extra of [{ scheduling_enabled: false }, { draining: true }, { state: 'unhealthy' }, { pool_role: 'upgrade-target' }, { scheduling_paused: true }]) {
    const result = summarizePod({ ...pod, ...extra });
    assert.equal(result.available, 0);
    assert.equal(result.occupied, 2);
  }
});
test('unknown legacy container health is never fabricated', () => {
  const result = summarizePod({ ...pod, slots: undefined, capacity: 32, used_slots: 17 });
  assert.equal(result.capacity, 32);
  assert.equal(result.ready, 0);
  assert.equal(result.available, 0);
  assert.equal(result.occupied, 17);
});
test('incomplete binding inventory never advertises occupied slots as available', () => {
  const result = summarizePod({ ...pod, used_slots: 3 });
  assert.equal(result.occupied, 3);
  assert.equal(result.available, 0);
  assert.equal(summarizePod({ ...pod, used_slots: 0, slots: [{ index: 1, ready: true }], bindings_reported: false }).available, 0);
});
test('search includes owners and instances, combines node/type/scheduling filters', () => {
  assert.equal(filterPods([pod], { search: 'ALICE' }).length, 1);
  assert.equal(filterPods([pod], { search: 'builder', node: 'node-a', type: 'opencode', scheduling: 'enabled' }).length, 1);
  assert.equal(filterPods([pod], { node: 'node-b' }).length, 0);
  assert.equal(filterPods([pod], { type: 'hermes' }).length, 0);
});
test('incoming targets require matching runtime, ready empty slots and compatible immutable image', () => {
  const source = { ...pod, image_ref: 'image@sha256:abc' };
  const target = { ...source, id: 2 };
  const candidates = [target, { ...target, id: 3, runtime_type: 'hermes' }, { ...target, id: 4, scheduling_enabled: false }, { ...target, id: 5, image_ref: 'image@sha256:def' }];
  assert.deepEqual(availableTargets(candidates, source).map(p => p.id), [2]);
});
test('explicitly enabled upgrade targets retain incoming capacity while reserved and lab pools do not', () => {
  assert.equal(summarizePod({ ...pod, pool_role: 'upgrade-target', scheduling_enabled: true }).available, 1);
  for (const extra of [
    { pool_role: 'upgrade-target' },
    { pool_role: 'upgrade-target', scheduling_enabled: false },
    { pool_role: 'upgrade-lab', scheduling_enabled: true },
    { pool_role: 'upgrade-target', pool_purpose: 'openclaw-upgrade-lab', scheduling_enabled: true },
    { pool_role: 'unknown', scheduling_enabled: true },
    { pool_role: 'upgrade-target', scheduling_enabled: true, scheduling_paused: true },
  ]) assert.equal(summarizePod({ ...pod, ...extra }).available, 0);
});
test('manual and eviction targets exclude current source Pods', () => {
  const source = { ...pod, image_ref: 'image@sha256:abc' };
  assert.deepEqual(availableTargets([source, { ...source, id: 2 }], source).map(p => p.id), [2]);
});
test('batch targets exclude every source Pod and require compatibility with every source', () => {
  const source = { ...pod, image_ref: 'image@sha256:abc' };
  const otherSource = { ...source, id: 2 };
  const target = { ...source, id: 3 };
  assert.deepEqual(availableTargets([source, otherSource, target], [source, otherSource]).map(p => p.id), [3]);
  assert.deepEqual(availableTargets([target], [source, { ...otherSource, image_ref: 'different' }]), []);
  assert.deepEqual(availableTargets([target], [source, { ...otherSource, runtime_type: 'hermes' }]), []);
  assert.deepEqual(availableTargets([target], []), []);
});
test('targets require known matching namespace and known controller identity', () => {
  const source = { ...pod, image_ref: 'image@sha256:abc' };
  const target = { ...source, id: 2 };
  assert.deepEqual(availableTargets([target], source).map(p => p.id), [2]);
  for (const extra of [
    { namespace: 'other' },
    { namespace: undefined }, { deployment_name: undefined },
    { namespace: '' }, { deployment_name: '' },
  ]) {
    assert.deepEqual(availableTargets([{ ...target, ...extra }], source), []);
    assert.deepEqual(availableTargets([target], { ...source, ...extra }), []);
    assert.deepEqual(availableTargets([target], [source, { ...source, id: 3, ...extra }]), []);
  }
});

test('default and managed deployments are mutual candidates for every runtime, with backend preflight deciding storage and protocol compatibility', () => {
  for (const runtime_type of ['openclaw', 'hermes', 'opencode', 'deepseek-harness']) {
    const source = { ...pod, runtime_type, image_ref: 'image@sha256:abc' };
    const managed = { ...source, id: 2, deployment_name: 'managed-copy', capacity: 2, used_slots: 0, slots: [{ index: 1, ready: true }, { index: 2, ready: true }] };
    assert.deepEqual(availableTargets([managed], source).map(p => p.id), [2]);
    assert.deepEqual(availableTargets([source], managed).map(p => p.id), [1]);
    assert.deepEqual(availableTargets([{ ...managed, image_ref: 'image@sha256:def' }], source), []);
    assert.deepEqual(availableTargets([{ ...managed, scheduling_paused: true }], source), []);
    assert.deepEqual(availableTargets([{ ...managed, pool_role: 'upgrade-lab' }], source), []);
  }
});


test('explicit unknown observations never become idle or available, even with healthy containers', () => {
  const slot = { index: 1, ready: true, reconciliation_state: 'unknown', binding_confirmed: false };
  assert.equal(slotState(slot), 'unknown');
  assert.equal(slotState({ ...slot, ready: false }), 'unknown');
  const summary = summarizePod({ ...pod, capacity: 1, used_slots: 0, unknown_slots: 1, slots: [slot] });
  assert.equal(summary.idle, 0);
  assert.equal(summary.unknown, 1);
  assert.equal(summary.available, 0);
});

test('observed Hermes25 without a SQL binding remains occupied and erroneous', () => {
  const slot = { index: 1, ready: true, instance_id: 25, instance_state: 'running', reconciliation_state: 'residual', binding_confirmed: false };
  assert.equal(slotState(slot), 'error');
  const summary = summarizePod({ ...pod, capacity: 1, used_slots: 0, slots: [slot] });
  assert.equal(summary.occupied, 1);
  assert.equal(summary.error, 1);
  assert.equal(summary.available, 0);
  assert.equal(slotState({ ...slot, expected_instance_id: 118, reconciliation_state: 'conflict' }), 'error');
});

test('confirmed empty and bound observations retain their actual states', () => {
  assert.equal(slotState({ ready: true, reconciliation_state: 'consistent' }), 'idle');
  assert.equal(slotState({ ready: true, instance_id: 118, instance_state: 'running', reconciliation_state: 'consistent', binding_confirmed: true }), 'running');
});


test('only confirmed bindings authorize owner actions while legacy payloads remain compatible', () => {
  const confirmed = { index: 1, ready: true, instance_id: 118, instance_state: 'running', reconciliation_state: 'consistent', binding_confirmed: true };
  assert.equal(canManageSlot(confirmed), true);
  assert.equal(canEvictPod({ ...pod, used_slots: 1, reconciliation_state: 'consistent', slots: [confirmed] }), true);
  assert.equal(canManageSlot({ ...confirmed, reconciliation_state: undefined, binding_confirmed: undefined }), true);
  for (const reconciliation_state of ['residual', 'conflict', 'unknown']) {
    assert.equal(canManageSlot({ ...confirmed, reconciliation_state }), false);
    assert.equal(canEvictPod({ ...pod, used_slots: 1, slots: [confirmed, { index: 2, ready: true, reconciliation_state }] }), false);
  }
  assert.equal(canManageSlot({ ...confirmed, binding_confirmed: false }), false);
  assert.equal(canEvictPod({ ...pod, used_slots: 1, unknown_slots: 1, slots: [confirmed] }), false);
});

test('a confirmed empty slot remains available beside unknown or residual observations', () => {
  const slots = [
    { index: 1, ready: true, reconciliation_state: 'consistent' },
    { index: 2, ready: true, reconciliation_state: 'unknown' },
    { index: 3, ready: true, instance_id: 25, reconciliation_state: 'residual' },
  ];
  const summary = summarizePod({ ...pod, used_slots: 0, reconciliation_state: 'unknown', capacity: 3, slots });
  assert.equal(summary.available, 1);
  assert.equal(summary.idle, 1);
  assert.equal(summary.unknown, 1);
  assert.equal(summary.error, 1);
});


test('recovery scope uses database expected owners and rejects unconfirmed physical occupants', () => {
  assert.deepEqual(recoveryInstanceIds({ slots: [
    { instance_id: 10, instance_state: 'running', binding_confirmed: true },
    { instance_id: 99, expected_instance_id: 11, reconciliation_state: 'conflict' },
    { instance_id: 12, reconciliation_state: 'residual', binding_confirmed: false },
    { expected_instance_id: 13, ready: false },
  ] }), [10, 11, 13]);
  assert.deepEqual(recoveryInstanceIds({}), []);
});

test('SQL binding count is separate from unconfirmed residual owners', () => {
 const summary=summarizePod({...pod,bound_instances:0,used_slots:2,slots:[{ready:true,index:1,instance_id:25,reconciliation_state:'residual',binding_confirmed:false},{ready:true,index:2,instance_id:161,reconciliation_state:'unknown',binding_confirmed:false}]});
 assert.equal(summary.bound,0);
 assert.equal(summary.residual,2);
 assert.equal(summary.occupied,2);
 assert.equal(summary.available,0);
});


test('failed move retry resolves only the original reserved empty slot without granting ordinary management', () => {
  const slot = { index: 1, ready: true, reconciliation_state: 'conflict', binding_confirmed: false, expected_instance_id: 101 };
  const source = { ...pod, pod_uid: 'source-uid', agent_reported: true, bindings_reported: true, slots: [slot] };
  const operation = { id: 'failed-move', kind: 'migration', status: 'failed', items: [{ instance_id: 101, source_pod_id: 1, source_pod_uid: 'source-uid', source_generation: 2, source_gateway_id: 'gw-101-2', state: 'failed' }] };
  const [selection] = failedMoveRetrySelections([source], operation);
  assert.equal(selection.slot.instance_id, 101);
  assert.equal(selection.slot.generation, 2);
  assert.equal(selection.slot.gateway_id, 'gw-101-2');
  assert.equal(canManageSlot(slot), false);
  assert.equal(canManageSlot(selection.slot), false);
  assert.equal(source.slots[0].instance_id, undefined, 'retry must not alter inventory');
  assert.deepEqual(failedMoveRetrySelections([source], undefined), []);
  for (const extra of [{ status: 'completed' }, { kind: 'scale_in' }]) assert.deepEqual(failedMoveRetrySelections([source], { ...operation, ...extra }), []);
  for (const extra of [{ pod_uid: 'replacement' }, { bindings_reported: false }, { slots: [{ ...slot, expected_instance_id: 102 }] }, { slots: [{ ...slot, instance_id: 102 }] }, { slots: [{ ...slot, reconciliation_state: 'unknown' }] }]) {
    assert.deepEqual(failedMoveRetrySelections([{ ...source, ...extra }], operation), []);
  }
  for (const extra of [{ source_gateway_id: 'gw-101-9' }, { state: 'completed' }, { recovery: {} }, { orphan_recovery: {} }]) assert.deepEqual(failedMoveRetrySelections([source], { ...operation, items: [{ ...operation.items[0], ...extra }] }), []);
});


test('pending release remains distinct from idle, unknown and incoming capacity', () => {
  const slot = { index: 1, ready: true, reconciliation_state: 'releasing', binding_confirmed: false };
  const source = { ...pod, capacity: 2, used_slots: 0, slots: [slot, { index: 2, ready: true, reconciliation_state: 'unknown' }] };
  assert.equal(slotState(slot), 'releasing');
  const summary = summarizePod(source);
  assert.equal(summary.releasing, 1);
  assert.equal(summary.idle, 0);
  assert.equal(summary.unknown, 1);
  assert.equal(summary.available, 0);
  assert.equal(canManageSlot(slot), false);
  assert.equal(canManageSlot({ ...slot, instance_id: 101 }), false);
  assert.equal(canEvictPod(source), false);
  assert.deepEqual(availableTargets([{ ...source, id: 2 }], source), []);
});

test('failed move retry includes only the exact stopped SQL reservation behind unknown inventory', () => {
  const slot = { index: 1, ready: true, instance_id: 101, expected_instance_id: 101,
    generation: 2, gateway_id: 'gw-101-2', instance_state: 'stopped', reconciliation_state: 'unknown',
    binding_confirmed: false, observation_message: 'SQL binding still reserves runtime slot' };
  const source = { ...pod, pod_uid: 'source-uid', agent_reported: true, bindings_reported: true, slots: [slot] };
  const operation = { id: 'failed-balance', kind: 'balance', status: 'partial_failure', items: [{ instance_id: 101,
    source_pod_id: 1, source_pod_uid: 'source-uid', source_generation: 2, source_gateway_id: 'gw-101-2', state: 'failed' }] };
  const [selection] = failedMoveRetrySelections([source], operation);
  assert.ok(selection, 'exact stopped reservation must retain the history retry entry');
  assert.equal(selection.slot.instance_id, 101);
  assert.equal(canManageSlot(slot), false, 'unknown inventory must not grant ordinary lifecycle actions');
  assert.deepEqual(source.slots[0], slot, 'retry must not change the inventory');
  for (const extra of [{ instance_id: 102 }, { expected_instance_id: 102 }, { generation: 3 },
    { gateway_id: 'gw-101-3' }, { instance_state: 'running' }, { ready: false },
    { binding_confirmed: true }, { observation_message: 'ownership not observed' }, { reconciliation_state: 'releasing' }]) {
    assert.deepEqual(failedMoveRetrySelections([{ ...source, slots: [{ ...slot, ...extra }] }], operation), [], JSON.stringify(extra));
  }
  for (const extra of [{ pod_uid: 'replacement' }, { agent_reported: false }, { bindings_reported: false }, { state: 'unhealthy' }, { state: 'deleted' }]) {
    assert.deepEqual(failedMoveRetrySelections([{ ...source, ...extra }], operation), [], JSON.stringify(extra));
  }
});
