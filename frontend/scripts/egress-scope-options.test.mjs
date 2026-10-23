import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const source = readFileSync(new URL('../src/lib/egressScopeOptions.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { loadAllScopeOptions } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('loads more than 200 scopes even when the server caps pages at 100', async () => {
  const data = Array.from({ length: 450 }, (_, id) => ({ id: id + 1 }));
  const pages = [];
  const result = await loadAllScopeOptions(async (page) => {
    pages.push(page);
    return { items: data.slice((page - 1) * 100, page * 100), total: data.length };
  });
  assert.deepEqual(result, data);
  assert.deepEqual(pages, [1, 2, 3, 4, 5]);
});

test('a failure does not return a partial scope list or discard another successful loader', async () => {
  const results = await Promise.allSettled([
    loadAllScopeOptions(async (page) => {
      if (page === 2) throw new Error('network failure');
      return { items: [{ id: 1 }], total: 2 };
    }),
    loadAllScopeOptions(async () => ({ items: [{ id: 3 }], total: 1 })),
  ]);
  assert.equal(results[0].status, 'rejected');
  assert.deepEqual(results[1], { status: 'fulfilled', value: [{ id: 3 }] });
});

test('rejects repeated or empty pages that cannot complete the advertised total', async () => {
  let calls = 0;
  await assert.rejects(loadAllScopeOptions(async () => { calls++; return { items: [{ id: 1 }], total: 2 }; }), /Incomplete/);
  assert.equal(calls, 2);
  await assert.rejects(loadAllScopeOptions(async () => ({ items: [], total: 1 })), /Incomplete/);
  assert.deepEqual(await loadAllScopeOptions(async () => ({ items: [], total: 0 })), []);
});
