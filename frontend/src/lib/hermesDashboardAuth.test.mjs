import assert from 'node:assert/strict';
import test from 'node:test';
import { resolveHermesDashboardUrl, isHermesDashboardErrorDocument } from './hermesDashboardAuth.ts';

test('managed classic dashboard stays same-origin and instance-scoped without credentials', () => {
  const origin = 'https://manager.example:39443';
  const path = '/api/v1/instances/199/proxy/chat/';
  assert.equal(resolveHermesDashboardUrl(path, 199, origin), path);
  assert.equal(resolveHermesDashboardUrl(origin + path, 199, origin), path);
  for (const value of [null, '', 'http://manager.example:9001'+path, 'https://evil.example'+path, '/api/v1/instances/210/proxy/chat/', path+'?token=secret', path+'#secret', 'https://user:password@manager.example:39443'+path, '/api/v1/instances/199/proxy/login']) {
    assert.equal(resolveHermesDashboardUrl(value, 199, origin), null);
  }
});

test('only structured BFF error pages trigger recovery, not chat text', () => {
  assert.equal(isHermesDashboardErrorDocument('{"success":false,"error":"desktop_session_required"}'), true);
  for (const text of ['a message mentioning desktop_session_required', '{"success":true,"error":"desktop_session_required"}', '{"success":false,"error":"unrelated"}', 'null']) {
    assert.equal(isHermesDashboardErrorDocument(text), false);
  }
});
