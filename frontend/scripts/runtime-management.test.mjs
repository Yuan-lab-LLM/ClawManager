import assert from 'node:assert/strict';
import { test } from 'node:test';
import { summarizePod, filterPods, slotState, availableTargets } from '../src/lib/runtimeManagement.ts';

const pod = { id: 1, runtime_type: 'opencode', namespace: 'runtime', deployment_name: 'code', pod_name: 'code-a', node_name: 'node-a', state: 'ready', agent_reported: true, capacity: 4, used_slots: 2, draining: false, slots: [
  { index: 1, ready: true, instance_id: 10, instance_name: 'builder', owner: 'alice', instance_state: 'running' },
  { index: 2, ready: true, instance_id: 11, instance_state: 'error' },
  { index: 3, ready: true },
  { index: 4, ready: false, state: 'waiting' },
] };

test('agent health remains independent from bound instance state', () => {
  assert.deepEqual(summarizePod(pod), { capacity: 4, ready: 3, occupied: 2, available: 1, running: 1, starting: 0, migrating: 0, error: 1, idle: 1, unavailable: 1 });
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
