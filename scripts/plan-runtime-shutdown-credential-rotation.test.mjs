import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import {createHash, createHmac} from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {planShutdownCredentialRotation as plan} from './plan-runtime-shutdown-credential-rotation.mjs';
const sha = v => createHash('sha256').update(v).digest('hex');
function fixture(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'rotation-plan-')); t.after(() => fs.rmSync(directory, {recursive: true, force: true}));
  const options = {directory, key: 'original-key', sourceScope: 'https://backend/control/pro/8', targetKey: 'new-key', targetScope: 'https://backend/control/pro/8'};
  const ns = sha(options.sourceScope + '\0' + options.key), target = sha(options.targetScope + '\0' + options.targetKey);
  const root = path.join(directory, 'shutdown-outbox', ns); fs.mkdirSync(root, {recursive: true});
  const event = {instance_id: 8, owner_user_id: 7, generation: 3, claim_id: 'a'.repeat(64), outcome: 'cleanup_confirmed_release_observed'};
  const delivery = {id: sha(JSON.stringify(event)), event};
  const mac = createHmac('sha256', options.key).update('shutdown-observation-outbox-v1\0' + ns + '\0').update(JSON.stringify(delivery)).digest('hex');
  const file = path.join(root, delivery.id + '.json'), raw = JSON.stringify({delivery, mac}); fs.writeFileSync(file, raw);
  return {options, root, file, raw, target: path.join(directory, 'shutdown-outbox', target)};
}
test('rotation preflight binds both credentials but never publishes or resigns files', t => {
  const f = fixture(t), p = plan(f.options);
  assert.equal(p.entries, 1); assert.equal(p.executable, false); assert.equal(p.lifecycle_authorized, false);
  assert.equal(p.preserves_original_owner_and_generation, true); assert.match(p.source_approval_digest, /^[a-f0-9]{64}$/);
  assert.equal(fs.existsSync(f.target), false); assert.equal(fs.readFileSync(f.file, 'utf8'), f.raw);
  const text = JSON.stringify(p); assert.equal(text.includes(f.options.key), false); assert.equal(text.includes(f.options.targetKey), false); assert.equal(text.includes(f.options.directory), false);
});
test('target changes require a new dual approval; wrong original key cannot validate history', t => {
  const f = fixture(t), a = plan(f.options), b = plan({...f.options, targetKey: 'different-new-key'});
  assert.notEqual(a.source_approval_digest, b.source_approval_digest); assert.notEqual(a.target_approval_digest, b.target_approval_digest);
  assert.throws(() => plan({...f.options, key: 'unknown-key'}), {code: 'ROTATION_SOURCE_UNVERIFIED'});
});
test('endpoint and owner namespace changes are not inferred from equal numeric identities', t => {
  const f = fixture(t);
  for (const targetScope of ['https://another/control/pro/8', 'https://backend/control/pro/9', 'https://backend/control/lite/openclaw']) assert.throws(() => plan({...f.options, targetScope}), {code: 'CONTROL_PLANE_IDENTITY_REQUIRED'});
});
test('corrupt, unknown and maintained sources cannot be used for rotation', t => {
  const f = fixture(t); fs.writeFileSync(f.file, '{'); assert.throws(() => plan(f.options), {code: 'ROTATION_SOURCE_UNVERIFIED'});
  fs.writeFileSync(f.file, f.raw); fs.writeFileSync(path.join(f.root, 'unknown'), 'retain'); assert.throws(() => plan(f.options), {code: 'ROTATION_SOURCE_UNVERIFIED'});
  fs.unlinkSync(path.join(f.root, 'unknown')); fs.mkdirSync(path.join(path.dirname(f.root), '.maintenance-' + path.basename(f.root)));
  assert.throws(() => plan(f.options), {code: 'ROTATION_SOURCE_UNVERIFIED'});
});
test('target namespace or marker already present prevents any implicit merging', t => {
  const f = fixture(t); fs.mkdirSync(f.target); assert.throws(() => plan(f.options), {code: 'ROTATION_TARGET_NOT_EMPTY'});
  fs.rmdirSync(f.target); fs.writeFileSync(path.join(path.dirname(f.target), '.maintenance-' + path.basename(f.target)), 'partial');
  assert.throws(() => plan(f.options), {code: 'ROTATION_TARGET_NOT_EMPTY'});
});
test('CLI accepts no write or credential arguments and does not leak invalid paths', t => {
  const f = fixture(t), script = fileURLToPath(new URL('./plan-runtime-shutdown-credential-rotation.mjs', import.meta.url));
  for (const args of [['--apply'], ['--key', f.options.key]]) {
    const r = spawnSync(process.execPath, [script, ...args], {encoding: 'utf8'}); assert.equal(r.status, 1); assert.equal(JSON.parse(r.stderr).error, 'INVALID_ARGUMENTS'); assert.equal(r.stdout, '');
  }
  assert.equal(fs.readFileSync(f.file, 'utf8'), f.raw);
});
