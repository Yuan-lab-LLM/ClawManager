import assert from 'node:assert/strict';
import { test } from 'node:test';
import { INSTANCE_STATUS_FILTERS, INSTANCE_TYPE_FILTERS, instanceStatusFilterQuery } from '../src/lib/instanceListFilters.ts';

test('status choices use lifecycle states including Auto Sleep, not access availability', () => {
  assert.deepEqual([...INSTANCE_STATUS_FILTERS], ['running', 'auto_sleep', 'stopped', 'creating', 'error', 'deleting']);
});

test('Auto Sleep and Stopped produce distinct server-side queries', () => {
  assert.deepEqual(instanceStatusFilterQuery('auto_sleep'), { status: 'stopped', auto_sleep: true });
  assert.deepEqual(instanceStatusFilterQuery('stopped'), { status: 'stopped', auto_sleep: false });
  assert.deepEqual(instanceStatusFilterQuery('running'), { status: 'running' });
  assert.deepEqual(instanceStatusFilterQuery('all'), {});
});

test('team edition filters offer the four supported runtimes', () => {
  assert.deepEqual([...INSTANCE_TYPE_FILTERS], ['openclaw', 'hermes', 'opencode', 'deepseek-harness']);
});
