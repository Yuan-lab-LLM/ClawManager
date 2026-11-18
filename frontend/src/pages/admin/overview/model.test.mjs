import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  appendSample,
  distribution,
  modelDistribution,
  percentOf,
  resourceSample,
  timeFraction,
} from './model.ts';
test('actual missing stays unknown and a confirmed zero stays zero', () => {
  assert.equal(percentOf(null, 100), null);
  assert.equal(percentOf(undefined, 100), null);
  assert.equal(percentOf(0, 100), 0);
  assert.equal(percentOf(120, 100), 120);
  assert.equal(percentOf(12, 0), null);
  const sample = resourceSample(
    {
      cpu: { used: null, capacity: 10, requested: 2, allocatable: 8 },
      memory: { used: 0, capacity: 10, requested: 1, allocatable: 8 },
      disk: { capacity: 10, requested: 0, allocatable: 8 },
    },
    '2026-11-16T10:00:00Z',
  );
  assert.deepEqual(sample.cpu, { actual: null, requested: 25 });
  assert.deepEqual(sample.memory, { actual: 0, requested: 12.5 });
  assert.deepEqual(sample.disk, { actual: null, requested: 0 });
});
test('Pod and instance pies keep independent denominators and never include slots', () => {
  const rows = [
    { type: 'openclaw', runtime_pods: 3, instances: 100 },
    { type: 'hermes', runtime_pods: 1, instances: 50 },
  ];
  assert.deepEqual(
    distribution(rows, 'runtime_pods').map((r) => r.share),
    [75, 25],
  );
  assert.ok(
    Math.abs(distribution(rows, 'instances')[0].share - 200 / 3) < 1e-8,
  );
  assert.ok(
    Math.abs(distribution(rows, 'instances')[1].share - 100 / 3) < 1e-8,
  );
  assert.deepEqual(
    distribution(
      rows.map((r) => ({ ...r, instances: 0 })),
      'instances',
    ),
    [],
  );
});
test('model top groups preserve the full token denominator', () => {
  const rows = Array.from({ length: 9 }, (_, i) => ({
    name: 'm' + i,
    tokens: 9 - i,
  }));
  const slices = modelDistribution(rows, 'Other', 6);
  assert.equal(slices.length, 6);
  assert.equal(slices.at(-1).name, 'Other');
  assert.equal(
    slices.reduce((n, r) => n + r.count, 0),
    45,
  );
  assert.ok(Math.abs(slices.reduce((n, r) => n + r.share, 0) - 100) < 1e-8);
});
test('observations start only when collected and preserve collection gaps', () => {
  const sample = (at) => ({
    at,
    cpu: { actual: 0, requested: 30 },
    memory: { actual: null, requested: 20 },
    disk: { actual: 50, requested: 0 },
  });
  const first = sample('2026-11-16T10:00:00Z');
  assert.deepEqual(appendSample([], first), [first]);
  const later = appendSample([first], sample('2026-11-16T10:05:00Z'));
  assert.equal(later.length, 3);
  assert.equal(later[1].cpu.actual, null);
  assert.equal(later[1].cpu.requested, null);
  assert.equal(appendSample(later, later.at(-1)).length, 3);
  assert.equal(appendSample(later, sample('2026-11-16T11:00:00Z')).length, 1);
});

test('resource chart uses elapsed time instead of evenly spaced sample indices', () => {
  const times = [
    '2026-11-16T10:00:00Z',
    '2026-11-16T10:00:30Z',
    '2026-11-16T10:05:00Z',
  ];
  assert.equal(timeFraction(times, 0), 0);
  assert.equal(timeFraction(times, 1), 0.1);
  assert.equal(timeFraction(times, 2), 1);
  assert.equal(timeFraction([times[0]], 0), 0);
});
