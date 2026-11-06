import assert from 'node:assert/strict';
import { test } from 'node:test';
import { initialWorkbench, workbenchReducer } from '../src/components/workbench/state.ts';
import { readWorkbenchView, writeWorkbenchView, workbenchViewKey, isRestoredPreviewCurrent } from '../src/components/workbench/persistence.ts';

const storage = () => {
  const values = new Map();
  return { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), values };
};
function browserState() {
  let state = workbenchReducer(initialWorkbench(6), { type: 'browser.new', id: 'first' });
  state = workbenchReducer(state, { type: 'browser.navigate', id: 'first', address: 'http://localhost:3000/app?sort=asc&token=private-query#access_token=private-fragment', url: 'https://p-private-capability.invalid/', viewer_url: 'https://manager.test/view#private-viewer', credentialless: true, previewError: 'private-error', generation: 6 });
  state = workbenchReducer(state, { type: 'browser.new', id: 'second' });
  state = workbenchReducer(state, { type: 'tab.hide', id: 'second' });
  state = workbenchReducer(state, { type: 'git.patch', patch: { message: 'private-draft', url: 'https://user:private-password@example.test/repo' } });
  return state;
}

test('reentry restores scoped browser definitions, order, selection and pane without capabilities or drafts', () => {
  const db = storage(), key = workbenchViewKey(7, 42);
  writeWorkbenchView(key, { state: browserState(), visible: false }, db);
  const raw = db.getItem(key);
  assert(!raw.includes('private-'), 'Tokens, viewer URLs, errors and Git drafts must not be durable');
  const restored = readWorkbenchView(key, 6, db);
  assert.equal(restored.visible, false);
  assert.equal(restored.state.panel, 'preview');
  assert.equal(restored.state.active, 'first');
  assert.deepEqual(restored.state.tabs.map(tab => [tab.id, tab.hidden]), [['first', false], ['second', true]]);
  assert.equal(restored.state.tabs[0].address, 'http://localhost:3000/app?sort=asc');
  assert.equal(restored.state.tabs[0].url, undefined);
  assert.equal(restored.state.tabs[0].viewer_url, undefined);
  assert.equal(restored.state.tabs[0].restorePreview, true);
  assert.equal(restored.state.git.message, '');
  assert.equal(readWorkbenchView(workbenchViewKey(8, 42), 6, db).state.tabs.length, 0);
  assert.equal(readWorkbenchView(workbenchViewKey(7, 43), 6, db).state.tabs.length, 0);
  assert.equal(workbenchViewKey(undefined, 42), null);
  assert.equal(workbenchViewKey(7, null), null);
});

test('hidden and removed browsers stay closed; terminal sessions are never fabricated or duplicated', () => {
  const db = storage(), key = workbenchViewKey(7, 42);
  let state = workbenchReducer(browserState(), { type: 'terminal.open', session: { id: 'term', name: 'Terminal', generation: 6, exited: false, created_at: 'now', cwd: '' } });
  state = workbenchReducer(state, { type: 'tab.remove', id: 'first' });
  writeWorkbenchView(key, { state, visible: true }, db);
  const restored = readWorkbenchView(key, 6, db);
  assert.deepEqual(restored.state.tabs.map(tab => tab.id), ['second']);
  assert.equal(restored.state.active, undefined);
  assert.equal(restored.state.tabs[0].session, undefined);
});

test('untrusted storage is bounded, ignores credential addresses and cannot hydrate runtime fields', () => {
  const db = storage(), key = workbenchViewKey(7, 42);
  db.setItem(key, JSON.stringify({ version: 1, generation: 6, panel: 'preview', visible: false, active: 'good', tabs: [
    { id: 'good', address: 'localhost:3000/path', hidden: false, url: 'https://stolen.invalid', session: { exited: false }, previewError: 'secret' },
    { id: 'credentials', address: 'http://user:password@localhost:3000/' },
    { id: 'capability', address: 'https://p-secret.invalid/' },
    ...Array.from({ length: 45 }, (_, i) => ({ id: 'tab' + i, address: '', hidden: false })),
  ] }));
  const restored = readWorkbenchView(key, 6, db);
  assert.equal(restored.state.tabs.length, 32);
  assert.equal(restored.state.tabs[0].address, 'http://localhost:3000/path');
  assert.equal(restored.state.tabs[0].url, undefined);
  assert(!restored.state.tabs.some(tab => ['credentials', 'capability'].includes(tab.id)));
  db.setItem(key, '{bad');
  assert.equal(readWorkbenchView(key, 6, db).state.tabs.length, 0);
  db.setItem(key, 'x'.repeat(300000));
  assert.equal(readWorkbenchView(key, 6, db).state.tabs.length, 0);
  const blocked = { getItem() { throw Error('blocked'); }, setItem() { throw Error('blocked'); } };
  assert.doesNotThrow(() => writeWorkbenchView(key, { state: browserState(), visible: true }, blocked));
  assert.equal(readWorkbenchView(key, 6, blocked).state.tabs.length, 0);
});

test('saved generation is fenced before restore and delayed responses cannot replace a newer generation', () => {
  const db = storage(), key = workbenchViewKey(7, 42);
  writeWorkbenchView(key, { state: browserState(), visible: true }, db);
  const restored = readWorkbenchView(key, 7, db).state;
  assert.equal(restored.generation, 7);
  assert.equal(restored.tabs[0].stale, true);
  assert.equal(restored.tabs[0].restorePreview, undefined);
  assert(restored.tabs[0].address);
  assert.equal(workbenchReducer(restored, { type: 'browser.navigate', id: 'first', generation: 6, address: 'old', url: 'https://old.invalid/' }), restored);
  writeWorkbenchView(key, { state: restored, visible: true }, db);
  const reloaded = readWorkbenchView(key, 7, db).state;
  assert.equal(reloaded.tabs[0].stale, true, 'A second reload must not revive a browser from the previous generation');
  assert.equal(reloaded.tabs[0].restorePreview, false);
});

test('a deferred restore cannot replace a newer navigation or a closed tab in the same generation', async () => {
  const address = 'http://localhost:3000/one', second = 'http://localhost:3000/two';
  let state = workbenchReducer(initialWorkbench(6), { type: 'browser.new', id: 'first' });
  state = workbenchReducer(state, { type: 'browser.navigate', id: 'first', address, url: '', previewPending: true, generation: 6 });
  let release;
  const pending = new Promise(resolve => { release = resolve; }).then(() => {
    if (isRestoredPreviewCurrent(state, 'first', address, 6)) state = workbenchReducer(state, { type: 'browser.navigate', id: 'first', address, url: 'https://old-restored.invalid/', generation: 6 });
  });
  state = workbenchReducer(state, { type: 'browser.navigate', id: 'first', address: second, url: 'https://new-navigation.invalid/', generation: 6 });
  release(); await pending;
  assert.equal(state.tabs[0].address, second);
  assert.equal(state.tabs[0].url, 'https://new-navigation.invalid/');
  state = workbenchReducer(state, { type: 'browser.navigate', id: 'first', address, url: '', previewPending: true, generation: 6 });
  assert.equal(isRestoredPreviewCurrent(state, 'first', address, 6), true);
  state = workbenchReducer(state, { type: 'tab.hide', id: 'first' });
  assert.equal(isRestoredPreviewCurrent(state, 'first', address, 6), false);
  assert.equal(state.tabs[0].previewPending, undefined, 'Closing a pending restore must leave the tab retryable');
  state = workbenchReducer(state, { type: 'tab.remove', id: 'first' });
  assert.equal(isRestoredPreviewCurrent(state, 'first', address, 6), false);
});
