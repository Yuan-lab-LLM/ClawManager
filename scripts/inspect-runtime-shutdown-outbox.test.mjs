import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {createHash, createHmac} from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {inspectShutdownOutbox} from './inspect-runtime-shutdown-outbox.mjs';

const sha = raw => createHash('sha256').update(raw).digest('hex');
const event = {instance_id: 8, owner_user_id: 7, generation: 3, claim_id: 'a'.repeat(64), outcome: 'cleanup_confirmed_release_observed'};
function fixture(t, supplied) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'shutdown-inspect-'));
  t.after(() => fs.rmSync(directory, {recursive: true, force: true}));
  const opts = {directory, key: 'secret', sourceScope: 'scope'};
  const namespace = sha('scope\0secret');
  const root = path.join(directory, 'shutdown-outbox', namespace);
  const delivery = {id: sha(JSON.stringify(event)), event};
  const mac = createHmac('sha256', 'secret').update('shutdown-observation-outbox-v1\0' + namespace + '\0').update(JSON.stringify(delivery)).digest('hex');
  const raw = JSON.stringify({delivery, mac});
  assert.equal(namespace, '2175f6c735708bd66a937e7f9793de2404e840aa8cfec09b807504fa0d8aac5b');
  assert.equal(mac, 'b1e49507a05a405c39654c732ce941395ff16269c11a3fd3044e856e232aee64');
  fs.mkdirSync(root, {recursive: true});
  const file = path.join(root, delivery.id + '.json');
  fs.writeFileSync(file, supplied ? supplied(raw) : raw);
  return {opts, root, file, raw, namespace};
}
test('verified Go wire record remains unchanged; diagnostics never authorize lifecycle', t => {
  const f = fixture(t), before = fs.readFileSync(f.file);
  const report = inspectShutdownOutbox(f.opts);
  assert.equal(report.records[0].id, 'be03a0044a9280ba19845b5200e99e638cc5dffec72991503e2d1332aa9c7825');
  assert.deepEqual(report.records[0].event, event);
  assert.equal(report.records[0].status, 'verified_local_record');
  assert.equal(report.action, 'retain');
  assert.equal(report.lifecycle_authorized, false);
  assert.equal(report.server_acceptance, 'not_queried');
  assert.equal(report.blocked_locally, false);
  assert.deepEqual(fs.readFileSync(f.file), before);
  assert.deepEqual(fs.readdirSync(f.root), [report.records[0].id + '.json']);
});
test('absent scope is read-only and rotated scope inventories old namespace without adopting', t => {
  const f = fixture(t);
  const report = inspectShutdownOutbox({...f.opts, key: 'rotated'});
  assert.equal(report.scope_present, false);
  assert.deepEqual(report.other_namespace_ids, [f.namespace]);
  assert.deepEqual(report.records, []);
  const absent = path.join(f.opts.directory, 'missing', 'base');
  assert.equal(inspectShutdownOutbox({...f.opts, directory: absent}).scope_present, false);
  assert.equal(fs.existsSync(path.join(f.opts.directory, 'missing')), false);
});
for (const [name, mutate, expected] of [
  ['partial', () => '{', 'invalid_envelope'],
  ['bad MAC', raw => raw.replace(/"mac":"./, '"mac":"z'), 'authentication_failed'],
  ['unknown field', raw => raw.replace('{', '{"extra":true,'), 'invalid_envelope'],
  ['duplicate field', raw => raw.replace('"generation":3', '"generation":9,"generation":3'), 'noncanonical_encoding'],
  ['trailing space', raw => raw + '\n', 'noncanonical_encoding'],
  ['wrong ID', raw => raw.replace('be03a0', '000000'), 'identity_mismatch'],
  ['large file', () => 'x'.repeat(2049), 'unsafe_file'],
]) test(`${name} blocks locally and preserves evidence`, t => {
  const f = fixture(t, mutate), before = fs.readFileSync(f.file);
  const result = inspectShutdownOutbox(f.opts);
  assert.equal(result.records[0].status, expected);
  assert.equal(result.records[0].event, undefined);
  assert.equal(result.blocked_locally, true);
  assert.deepEqual(fs.readFileSync(f.file), before);
});
test('unknown names are hashed, not opened or rendered; record-directory is unsafe', t => {
  const f = fixture(t), name = 'sensitive-private-file';
  fs.writeFileSync(path.join(f.root, name), 'SECRET CONTENT');
  const badID = 'b'.repeat(64);
  fs.mkdirSync(path.join(f.root, badID + '.json'));
  const result = inspectShutdownOutbox(f.opts), text = JSON.stringify(result);
  assert.equal(result.blocked_locally, true);
  assert.ok(result.records.some(r => r.entry_hash === sha(name) && r.status === 'unknown_entry'));
  assert.ok(result.records.some(r => r.id === badID && r.status === 'unsafe_file'));
  assert.equal(text.includes(name), false);
  assert.equal(text.includes('SECRET CONTENT'), false);
});
test('oversized queue is diagnosed but bounded; no partial report beyond scan limit', t => {
  const f = fixture(t);
  for (let i = 0; i < 256; i++) fs.writeFileSync(path.join(f.root, `unknown-${i}`), '');
  const result = inspectShutdownOutbox(f.opts);
  assert.equal(result.queue_at_capacity, true);
  assert.equal(result.blocked_locally, true);
  assert.equal(result.records.length, 257);
  for (let i = 256; i < 1024; i++) fs.writeFileSync(path.join(f.root, `unknown-${i}`), '');
  assert.throws(() => inspectShutdownOutbox(f.opts), {code: 'SCAN_LIMIT_EXCEEDED'});
});
test('namespace inventory is bounded and unknown root entries are not followed', t => {
  const f = fixture(t);
  fs.writeFileSync(path.join(path.dirname(f.root), 'private-entry'), 'not a namespace');
  const result = inspectShutdownOutbox(f.opts);
  assert.deepEqual(result.unknown_root_entries, [sha('private-entry')]);
  for (let i = 0; i < 255; i++) fs.mkdirSync(path.join(path.dirname(f.root), i.toString(16).padStart(64, '0')));
  assert.throws(() => inspectShutdownOutbox(f.opts), {code: 'SCAN_LIMIT_EXCEEDED'});
});
test('CLI rejects mutations and never exposes OS paths or credentials', t => {
  const f = fixture(t), script = fileURLToPath(new URL('./inspect-runtime-shutdown-outbox.mjs', import.meta.url));
  const env = {...process.env, CLAWMANAGER_SHUTDOWN_OUTBOX_DIR: f.opts.directory, CLAWMANAGER_SHUTDOWN_OUTBOX_KEY: f.opts.key, CLAWMANAGER_SHUTDOWN_OUTBOX_SCOPE: f.opts.sourceScope};
  for (const args of [['--apply'], ['--key', 'private-secret']]) {
    const run = spawnSync(process.execPath, [script, ...args], {env, encoding: 'utf8'});
    assert.equal(run.status, 1);
    assert.deepEqual(JSON.parse(run.stderr), {error: 'INVALID_ARGUMENTS', action: 'retain'});
    assert.equal(run.stdout, '');
  }
  const good = spawnSync(process.execPath, [script], {env, encoding: 'utf8'});
  assert.equal(good.status, 0);
  assert.equal(good.stderr, '');
  fs.writeFileSync(f.file, '{');
  const blocked = spawnSync(process.execPath, [script], {env, encoding: 'utf8'});
  assert.equal(blocked.status, 2);
  assert.equal(JSON.parse(blocked.stdout).blocked_locally, true);
});
test('invalid source configuration does not create a directory', () => {
  for (const opts of [{}, {directory: '.'}, {directory: path.parse(process.cwd()).root, key: 'x', sourceScope: 'x'}]) {
    assert.throws(() => inspectShutdownOutbox(opts), {code: 'INVALID_CONFIGURATION'});
  }
});
test('Linux links are never followed', {skip: process.platform === 'win32'}, t => {
  const f = fixture(t);
  fs.unlinkSync(f.file);
  const outside = path.join(f.opts.directory, 'outside');
  fs.writeFileSync(outside, f.raw);
  fs.symlinkSync(outside, f.file);
  assert.equal(inspectShutdownOutbox(f.opts).records[0].status, 'unsafe_file');
  assert.equal(fs.readFileSync(outside, 'utf8'), f.raw);
  const linked = path.join(f.opts.directory, 'linked');
  fs.symlinkSync(f.opts.directory, linked);
  assert.throws(() => inspectShutdownOutbox({...f.opts, directory: linked}), {code: 'SOURCE_READ_FAILED'});
  fs.unlinkSync(f.file);
  fs.rmdirSync(f.root);
  fs.symlinkSync(f.opts.directory, f.root);
  assert.throws(() => inspectShutdownOutbox(f.opts), {code: 'SOURCE_READ_FAILED'});
});
