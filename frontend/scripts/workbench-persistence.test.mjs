import assert from 'node:assert/strict';
import { test } from 'node:test';
import { initialWorkbench, workbenchReducer } from '../src/components/workbench/state.ts';
import { readWorkbenchView, writeWorkbenchView, workbenchViewKey, sharedWorkbenchViewKey, isRestoredPreviewCurrent } from '../src/components/workbench/persistence.ts';

const storage = () => {
  const values = new Map();
  return { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), values };
};
test('expanded workspace and sessions drawer restore and persist explicit dismissal', () => {
  const db = storage(), key = workbenchViewKey(7, 42);
  let state = initialWorkbench(6);
  state = workbenchReducer(state, { type: 'view.patch', patch: { full: true, showSessions: true } });
  writeWorkbenchView(key, { state, visible: true }, db);
  const restored = readWorkbenchView(key, 6, db).state;
  assert.equal(restored.full, true);
  assert.equal(restored.showSessions, true);
  state = workbenchReducer(restored, { type: 'view.patch', patch: { full: false, showSessions: false } });
  writeWorkbenchView(key, { state, visible: true }, db);
  assert.equal(readWorkbenchView(key, 6, db).state.full, false);
  assert.equal(readWorkbenchView(key, 6, db).state.showSessions, false);
});
function browserState() {
  let state = workbenchReducer(initialWorkbench(6), { type: 'browser.new', id: 'first' });
  state = workbenchReducer(state, { type: 'browser.navigate', id: 'first', address: 'http://localhost:3000/app?sort=asc&token=private-query#access_token=private-fragment', url: 'https://p-private-capability.invalid/', viewer_url: 'https://manager.test/view#private-viewer', credentialless: true, previewError: 'private-error', generation: 6 });
  state = workbenchReducer(state, { type: 'browser.new', id: 'second' });
  state = workbenchReducer(state, { type: 'tab.hide', id: 'second' });
  state = workbenchReducer(state, { type: 'git.patch', patch: { message: 'Commit draft', url: 'https://user:private-password@example.test/repo' } });
  return state;
}

test('reentry restores scoped view and drafts without capabilities or URL credentials', () => {
  const db = storage(), key = workbenchViewKey(7, 42);
  writeWorkbenchView(key, { state: browserState(), visible: false }, db);
  const raw = db.getItem(key);
  assert(!raw.includes('private-'), 'Tokens, viewer URLs, errors and URL credentials must not be durable');
  const restored = readWorkbenchView(key, 6, db);
  assert.equal(restored.visible, false);
  assert.equal(restored.state.panel, 'preview');
  assert.equal(restored.state.active, 'first');
  assert.deepEqual(restored.state.tabs.map(tab => [tab.id, tab.hidden]), [['first', false], ['second', true]]);
  assert.equal(restored.state.tabs[0].address, 'http://localhost:3000/app?sort=asc');
  assert.equal(restored.state.tabs[0].url, undefined);
  assert.equal(restored.state.tabs[0].viewer_url, undefined);
  assert.equal(restored.state.tabs[0].restorePreview, true);
  assert.equal(restored.state.git.message, 'Commit draft');
  assert.equal(restored.state.git.url, 'https://example.test/repo');
  assert.equal(readWorkbenchView(workbenchViewKey(8, 42), 6, db).state.tabs.length, 0);
  assert.equal(readWorkbenchView(workbenchViewKey(7, 43), 6, db).state.tabs.length, 0);
  assert.equal(workbenchViewKey(undefined, 42), null);
  assert.equal(workbenchViewKey(7, null), null);
});

test('mixed tab order, active original terminal and hidden state survive without fabricating a live session', () => {
  const db = storage(), key = workbenchViewKey(7, 42);
  let state = workbenchReducer(browserState(), { type: 'terminal.open', session: { id: 'term', name: 'Terminal', generation: 6, exited: false, created_at: 'now', cwd: '' } });
  state = workbenchReducer(state, { type: 'tab.remove', id: 'first' });
  writeWorkbenchView(key, { state, visible: true }, db);
  const restored = readWorkbenchView(key, 6, db);
  assert.deepEqual(restored.state.tabs.map(tab => tab.id), ['second', 'term']);
  assert.equal(restored.state.active, 'term');
  assert.equal(restored.state.tabs[0].hidden, true);
  assert.equal(restored.state.tabs[1].kind, 'terminal');
  assert.equal(restored.state.tabs[1].restoreTerminal, true);
  assert.equal(restored.state.tabs[1].session, undefined);
  assert.equal(restored.state.tabs[1].terminalGeneration, 6);
});

test('restored terminals attach only the listed original same-generation session and retain ended/missing tabs', () => {
  const db = storage(), key = workbenchViewKey(7, 42);
  let state = initialWorkbench(6);
  const live = { id: 'original', name: 'Original shell', generation: 6, exited: false, created_at: 'now', cwd: 'project' };
  for (const session of [live, { ...live, id: 'ended', exited: true }, { ...live, id: 'missing' }]) state = workbenchReducer(state, { type: 'terminal.open', session });
  writeWorkbenchView(key, { state, visible: true }, db);
  state = readWorkbenchView(key, 6, db).state;
  const before = state;
  state = workbenchReducer(state, { type: 'terminal.reconcile', generation: 5, sessions: [live] });
  assert.equal(state, before);
  state = workbenchReducer(state, { type: 'terminal.reconcile', generation: 6, sessions: [live, { ...live, id: 'ended', exited: true }, { ...live, id: 'missing', generation: 5 }] });
  assert.deepEqual(state.tabs.map(tab => tab.id), ['original', 'ended', 'missing']);
  assert.equal(state.tabs[0].session.id, 'original');
  assert.equal(state.tabs[0].session.exited, false);
  assert.equal(state.tabs[1].session.exited, true);
  assert.equal(state.tabs[2].session, undefined);
  assert.equal(state.tabs[2].terminalEnded, true);
  assert.equal(state.active, 'missing');
  assert(state.tabs.every(tab => !tab.restoreTerminal));
  writeWorkbenchView(key, { state, visible: true }, db);
  const newer = readWorkbenchView(key, 7, db).state;
  assert(newer.tabs.every(tab => tab.stale && !tab.restoreTerminal));
});

test('Files folder/view and Git repository, diff selection and nonsecret drafts restore with transient operations cleared', () => {
  const db = storage(), key = workbenchViewKey(7, 42);
  let state = initialWorkbench(6);
  const files = { currentPath: 'projects/nested', previewPath: 'projects/nested/readme.md', editPath: null, pageIndex: 2, sortColumn: 'modified', sortDirection: 'desc', expanded: false, searchOpen: true, searchInput: 'readme' };
  state = workbenchReducer(state, { type: 'files.patch', patch: files });
  const git = { selected: 'second', folderPath: 'projects/nested', selection: { repo: 'second', path: 'src/main.ts', staged: true }, mode: 'changes', message: 'Draft commit message', adding: 'clone', path: 'projects/new', name: 'New project', url: 'https://user:private-password@example.test/repo?token=private-token&ref=main', branch: 'feature/draft', creatingBranch: true };
  state = workbenchReducer(state, { type: 'git.patch', patch: { ...git, busy: true, error: 'private-error' } });
  state = workbenchReducer(state, { type: 'panel', panel: 'git' });
  writeWorkbenchView(key, { state, visible: false }, db);
  const restored = readWorkbenchView(key, 6, db);
  assert.deepEqual(restored.state.files, files);
  assert.deepEqual(restored.state.git, { ...git, url: 'https://example.test/repo?ref=main', busy: false, error: '' });
  assert.equal(restored.state.panel, 'git');
  assert.equal(restored.visible, false);
  assert(!db.getItem(key).includes('private-'));
  state = workbenchReducer(state, { type: 'git.patch', patch: { url: 'https://user:private-incomplete@' } });
  writeWorkbenchView(key, { state, visible: true }, db);
  assert(!db.getItem(key).includes('private-'), 'Even an unfinished clone URL must not cache embedded credentials');
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

test('share view keys isolate links, rotations and owner UI without retaining credentials', () => {
  const db = storage();
  const key = sharedWorkbenchViewKey('a'.repeat(64), 42);
  writeWorkbenchView(key, { state: browserState(), visible: true }, db);
  assert.equal(readWorkbenchView(key, 6, db).state.tabs.length, 2);
  assert.equal(readWorkbenchView(sharedWorkbenchViewKey('b'.repeat(64), 42), 6, db).state.tabs.length, 0);
  assert.equal(readWorkbenchView(workbenchViewKey(7, 42), 6, db).state.tabs.length, 0);
  assert.equal(sharedWorkbenchViewKey('sl_bearer_code', 42), null);
  assert.equal(sharedWorkbenchViewKey('a'.repeat(64), undefined), null);
});
