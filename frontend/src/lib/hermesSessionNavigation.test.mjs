import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { runInNewContext } from 'node:vm';
const exports = {};
runInNewContext(ts.transpileModule(readFileSync(new URL('./hermesDesktopFrame.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText, { exports, URL });
const { readHermesSessionRoute: read, writeHermesSessionRoute: write } = exports;
const storage = () => { const m = new Map(); return { getItem: k => m.get(k) ?? null, setItem: (k,v) => m.set(k,v), values: m }; };
test('selection survives reopening and stays scoped to user and instance', () => {
  const s = storage(); write(s, 1, 229, '#/chat-older');
  assert.equal(read(s, 1, 229), '#/chat-older');
  assert.equal(read(s, 2, 229), null); assert.equal(read(s, 1, 228), null);
  assert.equal([...s.values.values()][0], '#/chat-older');
});
test('new chat is remembered; non-chat pages do not erase the last selection', () => {
  const s = storage(); write(s, 1, 229, '#/chat-older'); write(s, 1, 229, '#/settings');
  assert.equal(read(s, 1, 229), '#/chat-older');
  write(s, 1, 229, '#/'); assert.equal(read(s, 1, 229), '#/');
});
test('unsafe routes, unavailable storage and invalid identity do not affect entry', () => {
  const s = storage();
  for (const value of ['https://evil.test', '#/../secret', '#/session?token=secret', '#/a/b', '#/%ZZ', '#/<script>']) write(s, 1, 229, value);
  write(s, undefined, 229, '#/one'); write(s, 1, -1, '#/one'); assert.equal(s.values.size, 0);
  const blocked = { getItem() { throw Error('denied'); }, setItem() { throw Error('quota'); } };
  assert.equal(read(blocked, 1, 229), null); assert.doesNotThrow(() => write(blocked, 1, 229, '#/one'));
});
