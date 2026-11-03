import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const component = readFileSync(new URL('../src/components/InstanceLifecycleBatches.tsx', import.meta.url), 'utf8');
const list = readFileSync(new URL('../src/pages/instances/InstanceListPage.tsx', import.meta.url), 'utf8');

test('instance list keeps 100 items per page, independent of batch history', () => {
  assert.match(list, /const INSTANCE_LIST_PAGE_SIZE = 100;/);
  assert.match(list, /getInstances\(page, INSTANCE_LIST_PAGE_SIZE/);
});

test('history starts collapsed and provides an accessible toggle', () => {
  assert.match(component, /\[historyExpanded, setHistoryExpanded\] = useState\(false\)/);
  assert.match(component, /aria-expanded=\{historyExpanded\}/);
  assert.match(component, /aria-controls="instance-batch-history"/);
  assert.match(component, /id="instance-batch-history" hidden=\{!historyExpanded\}/);
  assert.match(component, /setHistoryExpanded\(value => !value\)/);
});

test('submitting opens history; unfinished work remains signposted when collapsed', () => {
  assert.match(component, /setSelected\(response.data.data.batch_id\);\s*setHistoryExpanded\(true\)/);
  assert.match(component, /batches.some\(unfinished\)/);
  // Collapse must not become a condition that disables background polling.
  assert.match(component, /\}, \[refresh\]\);/);
  assert.doesNotMatch(component, /if\s*\(!historyExpanded\)\s*return/);
});

test('selection count, confirmation limits and actual progress are explicit', () => {
  assert.match(component, /已选中 \{selectedIds.length\} 个实例（含跨页选择）/);
  assert.match(component, /本次共选中 \$\{selectedIds.length\} 个 Lite 实例/);
  assert.match(component, /最多同时执行 5 个，两类操作合计最多 10 个/);
  assert.match(component, /detail.counts.active \|\| 0/);
  assert.match(component, /detail.counts.pending \|\| 0/);
  assert.doesNotMatch(component, /全局最多 5 个，重置最多 2 个/);
});
