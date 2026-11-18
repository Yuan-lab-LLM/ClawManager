import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createAgentTypes, defaultCreateImage } from '../src/lib/instanceCreation.ts';

const source = readFileSync(new URL('../src/pages/instances/CreateInstancePage.tsx', import.meta.url), 'utf8');
const instanceTypes = readFileSync(new URL('../src/types/instance.ts', import.meta.url), 'utf8');

const form = source.slice(source.indexOf('className="cm-create"'));
assert.match(form, /role="radiogroup" aria-label=\{c\('mode'\)\}/, 'Lite and Pro are selectable in the creation form');
assert.match(form, /\(\['lite','pro'\] as const\)\.map/);
assert.match(form, /role="switch"[^>]+disabled=\{pro\}/, 'Batch creation is restricted to Lite');
assert.match(source, /if \(next === 'pro'\) setBatch\(false\)/, 'Switching to Pro clears a Lite batch');
assert.match(form, /\{pro && <section[^>]+><h2>\{t\('instances\.resourceConfiguration'\)\}/, 'Dedicated resources are Pro-only');
assert.match(form, /\{pro && canImport && <details/, 'Initial configuration is Pro-only');
assert.match(source, /inventory\.length \+ requested > quota\.max_instances \|\| \(pro &&/, 'Dedicated quota is only counted for Pro');
assert.match(source, /image_registry: image\.image/, 'Creation uses the configured default image');

for (const historicalType of ['workbuddy', 'codex', 'claude-code']) {
  assert.ok(instanceTypes.includes(`id: "${historicalType}"`), 'Historical runtime types remain renderable');
  assert.ok(!createAgentTypes.some(type => type.id === historicalType), 'Historical runtimes are not offered for new creation');
}
assert.ok(createAgentTypes.some(type => type.id === 'opencode'));
const lite = { instance_type: 'opencode', runtime_type: 'gateway', image: 'lite-default' };
const pro = { instance_type: 'opencode', runtime_type: 'desktop', image: 'pro-default' };
assert.equal(defaultCreateImage([lite, pro], 'opencode', 'lite'), lite);
assert.equal(defaultCreateImage([lite, pro], 'opencode', 'pro'), pro);

console.log('Single-page creation preserves Lite/Pro modes, batch limits and configured default images.');
