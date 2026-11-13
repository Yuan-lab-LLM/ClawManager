import assert from 'node:assert/strict';
import test from 'node:test';
import * as navigation from './instanceNavigation.ts';

const values = new Map();
globalThis.window = { sessionStorage: {
  getItem: key => values.get(key) ?? null,
  setItem: (key, value) => values.set(key, value),
  removeItem: key => values.delete(key),
} };
const instance = (id = 23) => ({
  id, user_id: 7, name: 'Agent', type: 'opencode', runtime_type: 'gateway',
  instance_mode: 'lite', status: 'running', availability: 'available',
  cpu_cores: 2, memory_gb: 4, disk_gb: 5, gpu_enabled: false, gpu_count: 0,
  os_type: 'linux', os_version: 'latest', storage_class: 'local-path',
  mount_path: '/config', workspace_path: '/home/ubuntu/workspace',
  created_at: '2026-11-12', updated_at: '2026-11-12',
  runtime_generation: 3, runtime_variant: 'linux', desktop_stream_profile: 'standard', stopped_reason: 'idle_timeout',
  access_url: 'https://example.test/?token=secret', owner: 'private owner',
  description: 'private description', pod_ip: '10.0.0.1', capabilities: ['secret'],
});

test('short lived handoff survives duplicate reads, isolates users and keeps only latest target', () => {
  values.clear();
  navigation.rememberInstanceTarget(7, instance());
  const seed = navigation.readInstanceTarget(7, 23);
  assert.equal(seed?.name, 'Agent');
  assert.equal(seed?.runtime_generation, 3);
  assert.equal(seed?.stopped_reason, 'idle_timeout');
  assert.deepEqual(navigation.readInstanceTarget(7, 23), seed);
  assert.equal(navigation.readInstanceTarget(8, 23), null);
  assert.equal(navigation.readInstanceTarget(7, 24), null);
  assert.equal(seed.mount_path, '/config');
  assert.equal(seed.workspace_path, '/home/ubuntu/workspace');
  for (const key of ['access_url', 'owner', 'description', 'pod_ip', 'capabilities']) {
    assert.equal(key in seed, false);
    assert.equal([...values.values()].join('').includes(instance()[key]), false);
  }
  navigation.rememberInstanceTarget(7, instance(24));
  assert.equal(navigation.readInstanceTarget(7, 23), null);
  assert.equal(navigation.readInstanceTarget(7, 24)?.id, 24);
  const now = Date.now;
  try {
    Date.now = () => now() + 15_001;
    assert.equal(navigation.readInstanceTarget(7, 24), null);
  } finally { Date.now = now; }
});

test('null and empty stopped reasons from live responses do not reject handoff or list snapshots', () => {
  for (const stopped_reason of [null, '', undefined, 'manual', 'idle_timeout']) {
    values.clear();
    const row = { ...instance(), stopped_reason };
    navigation.rememberInstanceTarget(7, row);
    const target = navigation.readInstanceTarget(7, row.id);
    assert.ok(target, 'live optional stopped reason keeps target eligible');
    assert.equal(target.stopped_reason, stopped_reason || undefined);
    navigation.rememberInstanceList(7, '', { instances: [row], total: 1, page: 1, limit: 50 });
    const snapshot = navigation.readInstanceList(7, '', 1);
    assert.ok(snapshot, 'live optional stopped reason keeps the entire page eligible');
    assert.equal(snapshot.data.instances[0].stopped_reason, stopped_reason || undefined);
  }
});

test('workspace paths preserve native project selection without accepting URLs or token syntax', () => {
  values.clear();
  navigation.rememberInstanceTarget(7, instance());
  const [key, raw] = [...values.entries()][0];
  const data = JSON.parse(raw);
  for (const field of ['mount_path', 'workspace_path']) {
    for (const path of ['https://example.test/', '//example.test/root', '/root?token=secret',
      '/root#secret', '/root/token=secret', '/root%3Ftoken', '/root\\secret', '/root\nsecret',
      '/root/../secret', '/root/' + 'x'.repeat(513)]) {
      values.set(key, JSON.stringify({ ...data, instance: { ...data.instance, [field]: path } }));
      assert.equal(navigation.readInstanceTarget(7, 23), null, `${field}: ${path}`);
    }
  }
});

test('storage is revalidated rather than trusting modified metadata or instance data', () => {
  values.clear();
  navigation.rememberInstanceTarget(7, instance());
  const [key, raw] = [...values.entries()][0];
  const data = JSON.parse(raw);
  for (const changed of [
    { ...data, version: 99 }, { ...data, userId: 8 },
    { ...data, savedAt: Date.now() + 60_000 },
    { ...data, instance: { ...data.instance, id: -1 } },
    { ...data, instance: { ...data.instance, type: 'unknown' } },
    { ...data, instance: { ...data.instance, gpu_enabled: 'false' } },
    { ...data, instance: { ...data.instance, cpu_cores: null } },
    { ...data, instance: { ...data.instance, stopped_reason: 'private error text' } },
    { ...data, instance: { ...data.instance, name: 'x'.repeat(513) } },
  ]) {
    values.set(key, JSON.stringify(changed));
    assert.equal(navigation.readInstanceTarget(7, 23), null);
  }
  values.set(key, JSON.stringify({ ...data, instance: { ...data.instance, access_url: 'secret' } }));
  assert.equal('access_url' in navigation.readInstanceTarget(7, 23), false);
  values.set(key, '{broken json');
  assert.equal(navigation.readInstanceTarget(7, 23), null);
});

test('list position and last selected ID survive reloads and remain scoped to user', () => {
  values.clear();
  assert.deepEqual(navigation.readInstanceListView(7), { query: '', page: 1, scrollTop: 0 });
  navigation.writeInstanceListView(7, { query: 'Agent', page: 4, scrollTop: 412.5 });
  assert.deepEqual(navigation.readInstanceListView(7), { query: 'Agent', page: 4, scrollTop: 412.5 });
  assert.deepEqual(navigation.readInstanceListView(8), { query: '', page: 1, scrollTop: 0 });
  navigation.rememberLastInstance(7, 23);
  assert.equal(navigation.readLastInstance(7), 23);
  assert.equal(navigation.readLastInstance(8), null);
  navigation.rememberLastInstance(7, -1);
  assert.equal(navigation.readLastInstance(7), 23);
  for (const [key, raw] of values) {
    const data = JSON.parse(raw);
    values.set(key, JSON.stringify({ ...data, version: 99 }));
  }
  assert.deepEqual(navigation.readInstanceListView(7), { query: '', page: 1, scrollTop: 0 });
  assert.equal(navigation.readLastInstance(7), null);
});

test('list snapshot restores only its account, search and server page with sanitized rows', () => {
  values.clear();
  const data = { instances: [instance(), instance(24)], total: 153, page: 3, limit: 50 };
  navigation.rememberInstanceList(7, 'Agent', data);
  const snapshot = navigation.readInstanceList(7, 'Agent', 3);
  assert.equal(snapshot.data.total, 153);
  assert.equal(snapshot.data.page, 3);
  assert.equal(snapshot.data.limit, 50);
  assert.equal(snapshot.data.instances[0].stopped_reason, 'idle_timeout');
  assert.deepEqual(snapshot.data.instances.map(row => row.id), [23, 24]);
  assert.equal('access_url' in snapshot.data.instances[0], false);
  assert.equal('description' in snapshot.data.instances[0], false);
  assert.equal([...values.values()].join('').includes('token=secret'), false);
  assert.deepEqual(navigation.readInstanceList(7, 'Agent', 3), snapshot);
  assert.equal(navigation.readInstanceList(8, 'Agent', 3), null);
  assert.equal(navigation.readInstanceList(7, 'Other', 3), null);
  assert.equal(navigation.readInstanceList(7, 'Agent', 2), null);
  navigation.rememberInstanceList(7, '', { instances: [], total: 0, page: 1, limit: 50 });
  assert.equal(values.size, 1, 'each account retains at most one bounded page');
  assert.equal(navigation.readInstanceList(7, 'Agent', 3), null);
  assert.deepEqual(navigation.readInstanceList(7, '', 1).data.instances, []);
});

test('list snapshot rejects expired, malformed and oversized pages without losing pagination', () => {
  values.clear();
  const data = { instances: [instance()], total: 153, page: 3, limit: 50 };
  navigation.rememberInstanceList(7, 'Agent', data);
  const [key, raw] = [...values.entries()][0];
  const stored = JSON.parse(raw);
  for (const changed of [
    { ...stored, version: 99 }, { ...stored, userId: 8 },
    { ...stored, savedAt: Date.now() + 60_000 },
    { ...stored, savedAt: Date.now() - 15_000 },
    { ...stored, query: 'x'.repeat(513) },
    ...[null, { ...data, total: -1 }, { ...data, total: 1.5 },
      { ...data, page: 0 }, { ...data, limit: 0 }, { ...data, limit: 51 },
      { ...data, total: 0 }, { ...data, limit: 1, instances: [instance(), instance(24)] },
      { ...data, instances: Array.from({ length: 51 }, (_, i) => instance(i + 1)) },
      { ...data, instances: [ { ...instance(), type: 'unknown' } ] },
    ].map(data => ({ ...stored, data })),
  ]) {
    values.set(key, JSON.stringify(changed));
    assert.equal(navigation.readInstanceList(7, 'Agent', 3), null);
  }
  values.set(key, raw);
  for (const invalid of [null, { ...data, limit: 51 }, { ...data, total: -1 },
    { ...data, instances: Array.from({ length: 51 }, (_, i) => instance(i + 1)) }]) {
    navigation.rememberInstanceList(7, 'Agent', invalid);
    assert.equal(values.get(key), raw);
  }
  const now = Date.now;
  try {
    Date.now = () => stored.savedAt + 15_000;
    assert.equal(navigation.readInstanceList(7, 'Agent', 3), null);
  } finally { Date.now = now; }
});

test('deleted instance cannot resume from last target or snapshot; other account and view remain', () => {
  values.clear();
  navigation.rememberLastInstance(7,23);
  navigation.rememberInstanceTarget(7,instance());
  navigation.rememberInstanceList(7,'',{instances:[instance()],total:1,page:1,limit:50});
  navigation.writeInstanceListView(7,{query:'Agent',page:2,scrollTop:420});
  navigation.rememberLastInstance(8,23);
  navigation.forgetInstance(7,23);
  assert.equal(navigation.readLastInstance(7),null);
  assert.equal(navigation.readInstanceTarget(7,23),null);
  assert.equal(navigation.readInstanceList(7,'',1),null);
  assert.deepEqual(navigation.readInstanceListView(7),{query:'Agent',page:2,scrollTop:420});
  assert.equal(navigation.readLastInstance(8),23);
  navigation.rememberLastInstance(7,24);
  navigation.rememberInstanceTarget(7,instance(24));
  navigation.forgetInstance(7,23);
  assert.equal(navigation.readLastInstance(7),24);
  assert.equal(navigation.readInstanceTarget(7,24)?.id,24);
});

test('invalid identities, malformed views and unavailable storage cannot interrupt navigation', () => {
  values.clear();
  for (const userId of [0, -1, NaN, Infinity, 1.5, Number.MAX_SAFE_INTEGER + 1]) {
    navigation.rememberInstanceTarget(userId, instance());
    navigation.rememberInstanceList(userId, '', { instances: [instance()], total: 1, page: 1, limit: 50 });
    navigation.writeInstanceListView(userId, { query: '', page: 1, scrollTop: 0 });
    navigation.rememberLastInstance(userId, 23);
    assert.equal(navigation.readInstanceTarget(userId, 23), null);
    assert.equal(navigation.readInstanceList(userId, '', 1), null);
    assert.equal(navigation.readLastInstance(userId), null);
  }
  assert.equal(values.size, 0);
  for (const view of [null, {}, { query: '', page: 0, scrollTop: 0 },
    { query: '', page: 1.5, scrollTop: 0 }, { query: '', page: 1, scrollTop: -1 },
    { query: '', page: 1, scrollTop: Infinity }, { query: 'x'.repeat(513), page: 1, scrollTop: 0 }]) {
    navigation.writeInstanceListView(7, view);
  }
  assert.equal(values.size, 0);
  Object.defineProperty(window, 'sessionStorage', { configurable: true, get() { throw Error('denied'); } });
  assert.doesNotThrow(() => navigation.rememberInstanceTarget(7, instance()));
  assert.doesNotThrow(() => navigation.rememberInstanceList(7, '', { instances: [instance()], total: 1, page: 1, limit: 50 }));
  assert.doesNotThrow(() => navigation.writeInstanceListView(7, { query: '', page: 1, scrollTop: 0 }));
  assert.doesNotThrow(() => navigation.rememberLastInstance(7, 23));
  assert.equal(navigation.readInstanceTarget(7, 23), null);
  assert.equal(navigation.readInstanceList(7, '', 1), null);
  assert.equal(navigation.readLastInstance(7), null);
  assert.deepEqual(navigation.readInstanceListView(7), { query: '', page: 1, scrollTop: 0 });
});
