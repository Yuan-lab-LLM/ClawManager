import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

const require = createRequire(import.meta.url);
let locale = 'en';
function loadModule(url) {
  const compiled = ts.transpileModule(readFileSync(url, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
  });
  const exports = {};
  runInNewContext(compiled.outputText, { exports, require: name => name === '../contexts/I18nContext'
    ? { useI18n: () => ({ t: key => translate(locale, key) || key }) }
    : name.startsWith('.') ? loadModule(new URL(existsSync(new URL(name + '.ts', url)) ? name + '.ts' : name + '/index.ts', url)) : require(name) });
  return exports;
}
const { translate } = loadModule(new URL('../src/lib/i18n.ts', import.meta.url));
const { default: InstanceLifecycleStatus } = loadModule(new URL('../src/components/InstanceLifecycleStatus.tsx', import.meta.url));
function render(status, availability, language = 'en') {
  locale = language;
  return renderToStaticMarkup(createElement(InstanceLifecycleStatus, { status, availability }));
}

test('stopped remains an explicit lifecycle state even when access is unavailable', () => {
  for (const [locale, expected] of [['en', 'Stopped'], ['zh', '已停止']]) {
    const markup = render('stopped', 'unavailable', locale);
    assert.match(markup, new RegExp(`>${expected}</span>`));
  }
});

test('running with unavailable access stays running and reports access separately', () => {
  const markup = render('running', 'unavailable');
  assert.match(markup, />Running<\/span>/);
  assert.match(markup, />Access unavailable<\/span>/);
  assert.doesNotMatch(render('running'), /Access unavailable/);
});

test('creation, startup, errors and supplied upgrade/recovery states remain distinct', () => {
  for (const status of ['creating', 'starting', 'error', 'upgrading', 'recovering']) {
    assert.match(render(status), new RegExp(`>${status[0].toUpperCase() + status.slice(1)}</span>`));
  }
  assert.match(render('waiting_capacity'), />waiting_capacity<\/span>/);
});
