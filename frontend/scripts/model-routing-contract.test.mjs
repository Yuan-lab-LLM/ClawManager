import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const source = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const page = source('../src/pages/admin/ModelManagementPage.tsx');
const plan = source('../src/components/ModelDiscoveryPlan.tsx');
const audit = source('../src/pages/admin/AIAuditPage.tsx');
const translations = source('../src/lib/i18n.ts');

test('model tier is editable, persisted, and included in cancel-edit snapshot', () => {
  assert.match(page, /quality_tier: card\.quality_tier/);
  assert.match(page, /quality_tier: Number\(card\.quality_tier\) \|\| 0/);
  assert.match(plan, /\[0, 1, 2, 3, 4\]\.map/);
  assert.match(plan, /suggested_quality_tier: Number\(e\.target\.value\)/);
  assert.match(source('../src/services/modelService.ts'), /quality_tier\?: number/);
});

test('deployment concurrency capacity is editable and persisted', () => {
  assert.match(page, /max_concurrent_requests: card\.max_concurrent_requests/);
  assert.match(page, /max_concurrent_requests: Number\(card\.max_concurrent_requests\) \|\| 0/);
  assert.match(page, /max_concurrent_requests: Number\.parseInt\(event\.target\.value, 10\) \|\| 0/);
  assert.match(source('../src/services/modelService.ts'), /max_concurrent_requests\?: number/);
});

test('provider reasoning metadata does not bypass managed reasoning controls', () => {
  assert.match(page, /supportsManagedReasoningControl\([\s\S]*plannedReasoningEnabled\(model\)/);
  assert.match(plan, /supportsManagedReasoningControl\(connection\.providerType, connection\.protocolType, connection\.baseUrl, model\.id, model\.reasoning_control_mode\)/);
  assert.match(plan, /disabled=\{disabled \|\| !reasoningSupported\}/);
});

test('bulk model import is chunked at the backend batch limit', () => {
  assert.match(page, /offset \+= 200/);
  assert.match(page, /payloads\.slice\(offset, offset \+ 200\)/);
});

test('audit detail exposes routing metadata and warnings', () => {
  assert.match(audit, /gateway\.routing\.selected/);
  assert.match(audit, /gateway\.routing\.rejected/);
  assert.match(audit, /renderCodeBlock\(event\.details\)/);
  assert.match(audit, /event\.severity === 'warn'/);
});

test('routing labels exist in all five model-management locales', () => {
  for (const key of ['qualityTier', 'qualityTier0', 'qualityTier1', 'qualityTier2', 'qualityTier3', 'qualityTier4', 'qualityTierHelp', 'maxConcurrentRequests', 'maxConcurrentRequestsHelp', 'maxConcurrentUnlimited', 'routingDecisions', 'routingDecisionsHelp']) {
    assert.equal(translations.match(new RegExp(`\\b${key}:`, 'g'))?.length, 5, key);
  }
});
