import assert from 'node:assert/strict';
import fs from 'node:fs';
import ts from 'typescript';
const source = fs.readFileSync(new URL('../src/lib/teamI18n.ts', import.meta.url), 'utf8');
const exports = {};
new Function('exports', ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText)(exports);
const { teamTranslations, teamStatusLabel } = exports;
const keys = Object.keys(teamTranslations.en).sort();
for (const locale of ['en', 'zh', 'ja', 'ko', 'de']) {
  assert.deepEqual(Object.keys(teamTranslations[locale]).sort(), keys, `${locale} has every team control`);
  for (const value of Object.values(teamTranslations[locale])) assert(value.trim());
  for (const status of ['creating', 'running', 'deleting', 'deleted', 'failed']) assert.notEqual(teamStatusLabel(locale, status), status);
  assert.equal(teamStatusLabel(locale, 'future-backend-state'), 'future-backend-state', 'unknown status stays honest');
}
assert.equal(teamStatusLabel('zh', 'running'), '运行中');
console.log('PASS team control translations in five locales and known/unknown status labels');
