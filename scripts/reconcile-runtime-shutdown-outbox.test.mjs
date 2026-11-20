import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {createHash, createHmac, randomUUID} from 'node:crypto';
import {spawnSync, spawn} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {planOutboxQuarantine as plan, applyOutboxQuarantine as apply, planOutboxQuarantineRecovery as recoveryPlan, resumeOutboxQuarantine as resume, planOutboxGuardRetirement as guardPlan, retireOutboxRecoveryGuard as retireGuard, planDamagedOutboxOperationRepair as repairPlan, authorizeDamagedOutboxOperationRepair as authorizeRepair} from './reconcile-runtime-shutdown-outbox.mjs';
import {inspectShutdownOutbox} from './inspect-runtime-shutdown-outbox.mjs';

const sha = v => createHash('sha256').update(v).digest('hex');
const scriptURL = new URL('./reconcile-runtime-shutdown-outbox.mjs', import.meta.url);
function fixture(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'outbox-quarantine-'));
  t.after(() => fs.rmSync(directory, {recursive: true, force: true}));
  const opts = {directory, key: 'original-test-key', sourceScope: 'http://test/control/pro/8'};
  const namespace = sha(opts.sourceScope + '\0' + opts.key), parent = path.join(directory, 'shutdown-outbox'), queue = path.join(parent, namespace);
  fs.mkdirSync(queue, {recursive: true});
  fs.writeFileSync(path.join(queue, 'a'.repeat(64) + '.json'), '{"partial":');
  fs.writeFileSync(path.join(queue, 'operator-note'), 'retain unknown diagnostic evidence');
  return {opts, namespace, parent, queue, marker: path.join(parent, '.maintenance-' + namespace), archiveRoot: path.join(directory, 'shutdown-outbox-archive')};
}
const approval = digest => ({digest, confirmManagerStopped: true, confirmMaintenanceStopped: true, fenceReference: 'TEST-ISOLATED-ALL-WRITERS', reason: 'retain corrupt diagnostic records'});
const originalFiles = f => fs.readdirSync(f.queue).sort().map(name => [name, fs.readFileSync(path.join(f.queue, name), 'utf8')]);
function interrupt(f, phase) {
  const digest = plan(f.opts).approval_digest;
  const code = `import fs from 'node:fs'; import path from 'node:path'; import {applyOutboxQuarantine} from ${JSON.stringify(scriptURL.href)};
    const o=JSON.parse(process.env.OUTBOX_TEST_OPTIONS),a=JSON.parse(process.env.OUTBOX_TEST_APPROVAL),phase=process.env.OUTBOX_TEST_PHASE;
    const mkdir=fs.mkdirSync,rename=fs.renameSync,unlink=fs.unlinkSync;
    fs.mkdirSync=(p,...r)=>{if(phase==='before_archive'&&String(p).endsWith('shutdown-outbox-archive'))process.exit(77);return mkdir(p,...r);};
    fs.renameSync=(a,b)=>{if(phase==='before_move')process.exit(77);const v=rename(a,b);if(phase==='after_move')process.exit(77);return v;};
    fs.unlinkSync=(p)=>{if(phase==='before_unlock'&&String(p).endsWith('operation.json'))process.exit(77);const v=unlink(p);if(phase==='after_operation_unlink'&&String(p).endsWith('operation.json'))process.exit(77);return v;};
    applyOutboxQuarantine(o,a);`;
  const child = spawnSync(process.execPath, ['--input-type=module', '-e', code], {encoding: 'utf8', env: {...process.env,
    OUTBOX_TEST_OPTIONS: JSON.stringify(f.opts), OUTBOX_TEST_APPROVAL: JSON.stringify(approval(digest)), OUTBOX_TEST_PHASE: phase}});
  assert.equal(child.status, 77, child.stderr);
  let operationFile = path.join(f.marker, 'operation.json');
  if (!fs.existsSync(operationFile)) {
    const ids = fs.readdirSync(f.archiveRoot); assert.equal(ids.length, 1, 'fixture has exactly one archive');
    operationFile = path.join(f.archiveRoot, ids[0], 'prepared.json');
  }
  const operation = JSON.parse(fs.readFileSync(operationFile, 'utf8'));
  return operation.value.archive_id;
}
function crashedRecovery(f) {
  const id = interrupt(f, 'before_move'), p = recoveryPlan(f.opts, id);
  const code = `import fs from 'node:fs';import {resumeOutboxQuarantine} from ${JSON.stringify(scriptURL.href)};
    const o=JSON.parse(process.env.OUTBOX_TEST_OPTIONS),id=process.env.OUTBOX_TEST_ID,a=JSON.parse(process.env.OUTBOX_TEST_APPROVAL);
    const rename=fs.renameSync;fs.renameSync=(...args)=>{rename(...args);process.exit(78);};resumeOutboxQuarantine(o,id,a);`;
  const child = spawnSync(process.execPath, ['--input-type=module', '-e', code], {encoding: 'utf8', env: {...process.env,
    OUTBOX_TEST_OPTIONS: JSON.stringify(f.opts), OUTBOX_TEST_ID: id, OUTBOX_TEST_APPROVAL: JSON.stringify(approval(p.approval_digest))}});
  assert.equal(child.status, 78, child.stderr);
  const guard = path.join(f.marker, 'resuming'), envelope = JSON.parse(fs.readFileSync(path.join(guard, 'owner.json'), 'utf8'));
  return {id, guard, envelope, token: envelope.value.token};
}
function signed(f, domain, value) {
  return {value, mac: createHmac('sha256', f.opts.key).update(JSON.stringify([domain, value])).digest('hex')};
}
test('separate damaged-operation approval preserves exact original bytes before restoring a trusted copy', t => {
  const f = fixture(t), id = interrupt(f, 'after_move'), file = path.join(f.marker, 'operation.json'), bad = '{"partial":';
  fs.writeFileSync(file, bad);
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'INVALID_OUTBOX_AUDIT'});
  const p = repairPlan(f.opts, id); assert.equal(p.existing_bytes_will_be_preserved, true);
  assert.throws(() => authorizeRepair(f.opts, id, approval('0'.repeat(64))), {code: 'APPROVED_OUTBOX_PLAN_CHANGED'});
  assert.equal(authorizeRepair(f.opts, id, approval(p.approval_digest)).next_step, 'review_recovery');
  assert.equal(fs.readFileSync(file, 'utf8'), bad); // Authorization alone cannot replace it.
  const next = recoveryPlan(f.opts, id); assert.equal(next.damaged_operation_preservation_required, true);
  assert.equal(resume(f.opts, id, approval(next.approval_digest)).operation_audit_restored, true);
  assert.equal(fs.readFileSync(path.join(f.archiveRoot, id, 'damaged-operation.json'), 'utf8'), bad);
  assert.equal(fs.existsSync(f.marker), false);
});
test('changed damaged bytes invalidate authorization; no trustworthy prepared copy means no repair', t => {
  const f = fixture(t), id = interrupt(f, 'after_move'), file = path.join(f.marker, 'operation.json'); fs.writeFileSync(file, '{');
  const p = repairPlan(f.opts, id); fs.writeFileSync(file, '{changed');
  assert.throws(() => authorizeRepair(f.opts, id, approval(p.approval_digest)), {code: 'APPROVED_OUTBOX_PLAN_CHANGED'});
  fs.writeFileSync(path.join(f.archiveRoot, id, 'prepared.json'), '{');
  assert.throws(() => repairPlan(f.opts, id), {code: 'INVALID_OUTBOX_AUDIT'});
  assert.equal(fs.readFileSync(file, 'utf8'), '{changed');
});
test('valid signed operation cannot be quarantined as damaged', t => {
  const f = fixture(t), id = interrupt(f, 'after_move');
  assert.throws(() => repairPlan(f.opts, id), {code: 'OUTBOX_OPERATION_NOT_DAMAGED'});
});
test('repair CLI explicitly authorizes preservation and leaves replacement for a fresh recovery approval', t => {
  const f = fixture(t), id = interrupt(f, 'after_move'), file = path.join(f.marker, 'operation.json'); fs.writeFileSync(file, '{');
  const env = {...process.env, CLAWMANAGER_SHUTDOWN_OUTBOX_DIR: f.opts.directory, CLAWMANAGER_SHUTDOWN_OUTBOX_KEY: f.opts.key, CLAWMANAGER_SHUTDOWN_OUTBOX_SCOPE: f.opts.sourceScope};
  const run = args => spawnSync(process.execPath, [fileURLToPath(scriptURL), ...args], {encoding: 'utf8', env});
  const planned = run(['--resume', id, '--repair-operation']); assert.equal(planned.status, 0, planned.stderr);
  const digest = JSON.parse(planned.stdout).approval_digest;
  const authorized = run(['--resume', id, '--repair-operation', '--apply', '--approval', digest, '--confirm-manager-stopped', '--confirm-maintenance-stopped', '--fence-reference', 'TEST-ISOLATION', '--reason', 'preserve damaged evidence']);
  assert.equal(authorized.status, 0, authorized.stderr); assert.equal(JSON.parse(authorized.stdout).next_step, 'review_recovery');
  assert.equal(fs.readFileSync(file, 'utf8'), '{');
  assert.equal(resume(f.opts, id, approval(recoveryPlan(f.opts, id).approval_digest)).quarantined, true);
});
test('damaged evidence must remain unchanged after repair authorization and after preservation', t => {
  const f = fixture(t), id = interrupt(f, 'after_move'), file = path.join(f.marker, 'operation.json'); fs.writeFileSync(file, '{');
  authorizeRepair(f.opts, id, approval(repairPlan(f.opts, id).approval_digest));
  fs.writeFileSync(file, '{mutated'); assert.throws(() => recoveryPlan(f.opts, id), {code: 'OUTBOX_DAMAGED_OPERATION_CHANGED'});
});
test('real recovery exit after moving damaged evidence can finish without discarding it', t => {
  const f = fixture(t), id = interrupt(f, 'after_move'), file = path.join(f.marker, 'operation.json'); fs.writeFileSync(file, '{');
  authorizeRepair(f.opts, id, approval(repairPlan(f.opts, id).approval_digest)); const p = recoveryPlan(f.opts, id);
  const code = `import fs from 'node:fs';import {resumeOutboxQuarantine} from ${JSON.stringify(scriptURL.href)};
    const rename=fs.renameSync;fs.renameSync=(a,b)=>{const v=rename(a,b);if(String(b).endsWith('damaged-operation.json'))process.exit(81);return v;};
    resumeOutboxQuarantine(JSON.parse(process.env.OUTBOX_TEST_OPTIONS),process.env.OUTBOX_TEST_ID,JSON.parse(process.env.OUTBOX_TEST_APPROVAL));`;
  const child = spawnSync(process.execPath, ['--input-type=module', '-e', code], {encoding: 'utf8', env: {...process.env,
    OUTBOX_TEST_OPTIONS: JSON.stringify(f.opts), OUTBOX_TEST_ID: id, OUTBOX_TEST_APPROVAL: JSON.stringify(approval(p.approval_digest))}});
  assert.equal(child.status, 81, child.stderr);
  const preserved = path.join(f.archiveRoot, id, 'damaged-operation.json'); assert.equal(fs.readFileSync(preserved, 'utf8'), '{');
  assert.equal(fs.existsSync(file), false);
  if (process.platform !== 'linux') { assert.throws(() => guardPlan(f.opts, id), {code: 'OUTBOX_OWNER_PROBE_UNSUPPORTED'}); return; }
  const gp = guardPlan(f.opts, id); retireGuard(f.opts, id, gp.owner_token, approval(gp.approval_digest));
  assert.equal(resume(f.opts, id, approval(recoveryPlan(f.opts, id).approval_digest)).quarantined, true);
  assert.equal(fs.readFileSync(preserved, 'utf8'), '{');
});
function changeOwner(f, g, mutate) {
  const value = {...g.envelope.value}; mutate(value);
  fs.writeFileSync(path.join(g.guard, 'owner.json'), JSON.stringify(signed(f, 'shutdown-outbox-recovery-owner-v1', value)));
}
test('new recovery guards bind signed PID, operation and physical identity without automatic takeover', t => {
  const f = fixture(t), g = crashedRecovery(f), o = g.envelope.value;
  assert.equal(o.version, 1); assert.equal(typeof o.pid, 'number'); assert.match(o.operation_mac, /^[a-f0-9]{64}$/);
  assert.equal(typeof o.guard_identity.inode, 'string'); assert.equal(typeof o.maintenance_identity.inode, 'string');
  assert.throws(() => recoveryPlan(f.opts, g.id), {code: 'OUTBOX_RECOVERY_GUARD_PRESENT'});
  if (process.platform !== 'linux') assert.throws(() => guardPlan(f.opts, g.id), {code: 'OUTBOX_OWNER_PROBE_UNSUPPORTED'});
});
for (const phase of ['before_move', 'after_move', 'before_unlock']) test(`missing operation audit at ${phase} restores only its signed, physically bound prepared copy`, t => {
  const f = fixture(t), id = interrupt(f, phase), file = path.join(f.marker, 'operation.json');
  const original = fs.readFileSync(file); fs.unlinkSync(file);
  const p = recoveryPlan(f.opts, id);
  assert.equal(p.operation_audit_restore_required, true);
  const r = resume(f.opts, id, approval(p.approval_digest));
  assert.equal(r.operation_audit_restored, true); assert.equal(fs.existsSync(f.marker), false);
  const dest = path.join(f.archiveRoot, id);
  assert.deepEqual(fs.readFileSync(path.join(dest, 'prepared.json')), original);
  assert.equal(JSON.parse(fs.readFileSync(path.join(dest, 'operation-repair.json'), 'utf8')).value.proof, 'authorize_restore_signed_prepared_copy');
});
test('partial operation remains untouched despite a valid signed prepared copy', t => {
  const f = fixture(t), id = interrupt(f, 'after_move'), file = path.join(f.marker, 'operation.json'); fs.writeFileSync(file, '{');
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'INVALID_OUTBOX_AUDIT'});
  assert.equal(fs.readFileSync(file, 'utf8'), '{'); assert.equal(fs.existsSync(f.marker), true);
});
test('real process exit after operation unlink restores the physically bound empty marker', t => {
  const f = fixture(t), id = interrupt(f, 'after_operation_unlink');
  assert.equal(fs.readdirSync(f.marker).length, 0);
  const p = recoveryPlan(f.opts, id);
  assert.equal(p.operation_audit_restore_required, true); assert.equal(p.stage, 'before_unlock');
  assert.equal(resume(f.opts, id, approval(p.approval_digest)).operation_audit_restored, true);
  assert.equal(fs.existsSync(f.marker), false);
});
test('a missing operation without a complete signed prepared copy cannot be reconstructed', t => {
  const f = fixture(t), id = interrupt(f, 'before_archive'); fs.unlinkSync(path.join(f.marker, 'operation.json'));
  assert.throws(() => recoveryPlan(f.opts, id)); assert.equal(fs.existsSync(path.join(f.marker, 'operation.json')), false);
});
test('signed prepared copy cannot adopt a different empty maintenance directory', t => {
  const f = fixture(t), id = interrupt(f, 'before_unlock');
  fs.renameSync(f.marker, f.marker + '-original'); fs.mkdirSync(f.marker);
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'OUTBOX_MAINTENANCE_CHANGED'});
  assert.equal(fs.readdirSync(f.marker).length, 0);
});
test('partial repair authorization is preserved and cannot restore a missing operation', t => {
  const f = fixture(t), id = interrupt(f, 'after_move'); fs.unlinkSync(path.join(f.marker, 'operation.json'));
  const repair = path.join(f.archiveRoot, id, 'operation-repair.json'); fs.writeFileSync(repair, '{');
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'INVALID_OUTBOX_AUDIT'});
  assert.equal(fs.readFileSync(repair, 'utf8'), '{'); assert.equal(fs.existsSync(path.join(f.marker, 'operation.json')), false);
});
test('legacy empty and damaged recovery owners are never eligible for retirement', t => {
  const f = fixture(t), id = interrupt(f, 'before_move'), guard = path.join(f.marker, 'resuming'); fs.mkdirSync(guard);
  assert.throws(() => guardPlan(f.opts, id), {code: 'INVALID_OUTBOX_RECOVERY_OWNER'});
  fs.writeFileSync(path.join(guard, 'owner.json'), '{');
  assert.throws(() => guardPlan(f.opts, id), {code: 'INVALID_OUTBOX_AUDIT'});
  assert.equal(fs.existsSync(guard), true);
});
test('altered recovery owner JSON cannot pass its existing MAC', t => {
  const f = fixture(t), g = crashedRecovery(f), changed = structuredClone(g.envelope); changed.value.pid = process.pid;
  fs.writeFileSync(path.join(g.guard, 'owner.json'), JSON.stringify(changed));
  assert.throws(() => guardPlan(f.opts, g.id), {code: 'INVALID_OUTBOX_AUDIT'}); assert.equal(fs.existsSync(g.guard), true);
});
const linuxOnly = {skip: process.platform !== 'linux'};
test('Linux signed dead recovery owner is archived, audited, idempotently confirmed and normal recovery resumed', linuxOnly, t => {
  const f = fixture(t), g = crashedRecovery(f), p = guardPlan(f.opts, g.id);
  assert.equal(p.owner_state, 'absent_same_linux_scope'); assert.equal(p.owner_token, g.token);
  assert.throws(() => retireGuard(f.opts, g.id, g.token, {...approval(p.approval_digest), confirmMaintenanceStopped: false}), {code: 'EXPLICIT_OUTBOX_FENCE_APPROVAL_REQUIRED'});
  assert.throws(() => retireGuard(f.opts, g.id, g.token, approval('0'.repeat(64))), {code: 'APPROVED_OUTBOX_PLAN_CHANGED'});
  const r = retireGuard(f.opts, g.id, g.token, approval(p.approval_digest));
  assert.equal(r.maintenance_lock_retained, true); assert.equal(r.lifecycle_authorized, false); assert.equal(fs.existsSync(f.marker), true);
  assert.equal(fs.existsSync(g.guard), false);
  const history = path.join(f.archiveRoot, g.id, 'retired-guards', g.token);
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(history, 'owner.json'), 'utf8')), g.envelope);
  const again = guardPlan(f.opts, g.id, g.token);
  assert.equal(again.owner_state, 'already_retired');
  assert.equal(retireGuard(f.opts, g.id, g.token, approval(again.approval_digest)).next_step, 'review_recovery');
  assert.equal(resume(f.opts, g.id, approval(recoveryPlan(f.opts, g.id).approval_digest)).quarantined, true);
});
test('Linux live or reused PID never qualifies, even with a correctly signed owner', linuxOnly, t => {
  const f = fixture(t), g = crashedRecovery(f); changeOwner(f, g, o => { o.pid = process.pid; });
  assert.throws(() => guardPlan(f.opts, g.id), {code: 'OUTBOX_OWNER_STILL_RUNNING'}); assert.equal(fs.existsSync(g.guard), true);
});
test('Linux process probe permission and unknown errors cannot become absence proof', linuxOnly, t => {
  const f = fixture(t), g = crashedRecovery(f), kill = process.kill;
  try {
    for (const code of ['EPERM', 'EACCES', 'EIO']) {
      process.kill = () => { throw Object.assign(new Error('PRIVATE'), {code}); };
      assert.throws(() => guardPlan(f.opts, g.id), {code: 'OUTBOX_OWNER_STATE_UNKNOWN'});
    }
  } finally { process.kill = kill; }
  assert.equal(fs.existsSync(g.guard), true);
});
test('Linux different boot/namespace fingerprint is not comparable and cannot retire a guard', linuxOnly, t => {
  const f = fixture(t), g = crashedRecovery(f); changeOwner(f, g, o => { o.scope = '0'.repeat(64); });
  assert.throws(() => guardPlan(f.opts, g.id), {code: 'OUTBOX_OWNER_SCOPE_MISMATCH'}); assert.equal(fs.existsSync(g.guard), true);
});
test('Linux PID reappearing before guard movement leaves the active guard untouched', linuxOnly, t => {
  const f = fixture(t), g = crashedRecovery(f), p = guardPlan(f.opts, g.id), kill = process.kill;
  let probes = 0;
  try {
    process.kill = () => { if (++probes < 3) throw Object.assign(new Error('gone'), {code: 'ESRCH'}); return true; };
    assert.throws(() => retireGuard(f.opts, g.id, g.token, approval(p.approval_digest)), {code: 'OUTBOX_OWNER_STILL_RUNNING'});
  } finally { process.kill = kill; }
  assert.equal(fs.existsSync(g.guard), true);
});
test('Linux interruption after moving a guard can complete the missing retirement audit', linuxOnly, t => {
  const f = fixture(t), g = crashedRecovery(f), p = guardPlan(f.opts, g.id);
  const code = `import fs from 'node:fs';import {retireOutboxRecoveryGuard} from ${JSON.stringify(scriptURL.href)};
    const o=JSON.parse(process.env.OUTBOX_TEST_OPTIONS),id=process.env.OUTBOX_TEST_ID,t=process.env.OUTBOX_TEST_TOKEN,a=JSON.parse(process.env.OUTBOX_TEST_APPROVAL);
    const rename=fs.renameSync;fs.renameSync=(...args)=>{rename(...args);process.exit(79);};retireOutboxRecoveryGuard(o,id,t,a);`;
  const child = spawnSync(process.execPath, ['--input-type=module', '-e', code], {encoding: 'utf8', env: {...process.env,
    OUTBOX_TEST_OPTIONS: JSON.stringify(f.opts), OUTBOX_TEST_ID: g.id, OUTBOX_TEST_TOKEN: g.token, OUTBOX_TEST_APPROVAL: JSON.stringify(approval(p.approval_digest))}});
  assert.equal(child.status, 79, child.stderr);
  assert.throws(() => recoveryPlan(f.opts, g.id), {code: 'OUTBOX_GUARD_RETIREMENT_INCOMPLETE'});
  const next = guardPlan(f.opts, g.id, g.token);
  assert.equal(retireGuard(f.opts, g.id, g.token, approval(next.approval_digest)).maintenance_lock_retained, true);
  assert.equal(resume(f.opts, g.id, approval(recoveryPlan(f.opts, g.id).approval_digest)).quarantined, true);
});
test('Linux signed guard history tampering blocks later recovery', linuxOnly, t => {
  const f = fixture(t), g = crashedRecovery(f), p = guardPlan(f.opts, g.id);
  retireGuard(f.opts, g.id, g.token, approval(p.approval_digest));
  const file = path.join(f.archiveRoot, g.id, 'retired-guards', g.token, 'retirement.json');
  const data = JSON.parse(fs.readFileSync(file, 'utf8')); data.value.reason = 'changed'; fs.writeFileSync(file, JSON.stringify(data));
  assert.throws(() => recoveryPlan(f.opts, g.id), {code: 'INVALID_OUTBOX_AUDIT'});
  assert.equal(fs.existsSync(f.marker), true);
});
test('Linux dead-owner retirement composes with missing operation restoration', linuxOnly, t => {
  const f = fixture(t), g = crashedRecovery(f); fs.unlinkSync(path.join(f.marker, 'operation.json'));
  const p = guardPlan(f.opts, g.id);
  retireGuard(f.opts, g.id, g.token, approval(p.approval_digest));
  const next = recoveryPlan(f.opts, g.id); assert.equal(next.operation_audit_restore_required, true);
  assert.equal(resume(f.opts, g.id, approval(next.approval_digest)).operation_audit_restored, true);
});
test('Linux concurrent retirements preserve one original owner and signed audit', linuxOnly, async t => {
  const f = fixture(t), g = crashedRecovery(f), p = guardPlan(f.opts, g.id);
  const code = `import {retireOutboxRecoveryGuard} from ${JSON.stringify(scriptURL.href)};
    try{retireOutboxRecoveryGuard(JSON.parse(process.env.OUTBOX_TEST_OPTIONS),process.env.OUTBOX_TEST_ID,process.env.OUTBOX_TEST_TOKEN,JSON.parse(process.env.OUTBOX_TEST_APPROVAL));}catch{process.exitCode=2;}`;
  const run = () => new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ['--input-type=module', '-e', code], {stdio: 'pipe', timeout: 20000, env: {...process.env,
      OUTBOX_TEST_OPTIONS: JSON.stringify(f.opts), OUTBOX_TEST_ID: g.id, OUTBOX_TEST_TOKEN: g.token, OUTBOX_TEST_APPROVAL: JSON.stringify(approval(p.approval_digest))}});
    child.on('error', reject); child.on('exit', status => resolve(status));
  });
  const statuses = await Promise.all([run(), run()]); assert.ok(statuses.includes(0)); assert.ok(statuses.every(s => s === 0 || s === 2));
  const root = path.join(f.archiveRoot, g.id, 'retired-guards'); assert.deepEqual(fs.readdirSync(root), [g.token]);
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(root, g.token, 'owner.json'), 'utf8')), g.envelope);
  assert.equal(guardPlan(f.opts, g.id, g.token).owner_state, 'already_retired'); assert.equal(fs.existsSync(f.marker), true);
});
test('Linux guard history capacity preserves the current guard without appending evidence', linuxOnly, t => {
  const f = fixture(t), g = crashedRecovery(f), operation = JSON.parse(fs.readFileSync(path.join(f.marker, 'operation.json'), 'utf8'));
  const root = path.join(f.archiveRoot, g.id, 'retired-guards'); fs.mkdirSync(root);
  for (let i = 0; i < 32; i++) {
    const token = randomUUID(), dir = path.join(root, token); fs.mkdirSync(dir); const stat = fs.lstatSync(dir, {bigint: true});
    const o = {...g.envelope.value, token, guard_identity: {device: String(stat.dev), inode: String(stat.ino), birthtime: String(stat.birthtimeNs)}};
    const owner = signed(f, 'shutdown-outbox-recovery-owner-v1', o); fs.writeFileSync(path.join(dir, 'owner.json'), JSON.stringify(owner));
    fs.writeFileSync(path.join(dir, 'retirement.json'), JSON.stringify(signed(f, 'shutdown-outbox-guard-retirement-audit-v1', {
      version: 1, owner_mac: owner.mac, operation_mac: operation.mac, owner_token: token, proof: 'absent_same_linux_scope',
      approval_digest: 'a'.repeat(64), reason: 'previous approved retirement', fence_reference: 'TEST'})));
  }
  const p = guardPlan(f.opts, g.id);
  assert.throws(() => retireGuard(f.opts, g.id, g.token, approval(p.approval_digest)), {code: 'OUTBOX_GUARD_HISTORY_FULL'});
  assert.equal(fs.readdirSync(root).length, 32); assert.equal(fs.existsSync(g.guard), true);
});
test('retirement CLI requires archive ID and forbids owner tokens in ordinary recovery', t => {
  const f = fixture(t), env = {...process.env, CLAWMANAGER_SHUTDOWN_OUTBOX_DIR: f.opts.directory, CLAWMANAGER_SHUTDOWN_OUTBOX_KEY: f.opts.key, CLAWMANAGER_SHUTDOWN_OUTBOX_SCOPE: f.opts.sourceScope};
  for (const args of [['--retire-recovery-guard'], ['--owner-token', randomUUID()], ['--resume', randomUUID(), '--owner-token', randomUUID()], ['--retire-recovery-guard', '--resume', randomUUID(), '--owner-token', '../bad']]) {
    const r = spawnSync(process.execPath, [fileURLToPath(scriptURL), ...args], {encoding: 'utf8', env});
    assert.equal(r.status, 1); assert.equal(JSON.parse(r.stderr).error, 'INVALID_ARGUMENTS');
    assert.equal(r.stdout, ''); assert.equal(fs.existsSync(f.marker), false);
  }
});
test('default plan is read-only and never treats corrupt records as native proof', t => {
  const f = fixture(t), before = originalFiles(f), p = plan(f.opts);
  assert.equal(p.read_only, true); assert.equal(p.entries, 2);
  assert.equal(p.lifecycle_authorized, false); assert.equal(p.automatic_replay_allowed, false);
  assert.equal(p.requires_manager_fence, true);
  assert.equal(JSON.stringify(p).includes(f.opts.key), false);
  assert.equal(JSON.stringify(p).includes(f.opts.directory), false);
  assert.equal(fs.existsSync(f.marker), false); assert.equal(fs.existsSync(f.archiveRoot), false);
  assert.deepEqual(originalFiles(f), before);
});
test('explicit quarantine preserves whole namespace and signed audit; recheck does not touch fresh queue', t => {
  const f = fixture(t), before = originalFiles(f), p = plan(f.opts), r = apply(f.opts, approval(p.approval_digest));
  assert.equal(r.quarantined, true); assert.equal(r.lifecycle_authorized, false);
  assert.equal(fs.existsSync(f.queue), false); assert.equal(fs.existsSync(f.marker), false);
  const archive = path.join(f.archiveRoot, r.archive_id);
  assert.deepEqual(fs.readdirSync(archive).sort(), ['completed.json', 'prepared.json', 'queue']);
  assert.deepEqual(fs.readdirSync(path.join(archive, 'queue')).sort().map(name => [name, fs.readFileSync(path.join(archive, 'queue', name), 'utf8')]), before);
  fs.mkdirSync(f.queue); fs.writeFileSync(path.join(f.queue, 'new-record'), 'fresh evidence');
  const idempotent = recoveryPlan(f.opts, r.archive_id);
  assert.equal(idempotent.stage, 'already_quarantined');
  assert.equal(resume(f.opts, r.archive_id, approval(idempotent.approval_digest)).archive_id, r.archive_id);
  assert.equal(fs.readFileSync(path.join(f.queue, 'new-record'), 'utf8'), 'fresh evidence');
});
test('missing fence, reason, explicit stop or changed digest cannot mutate source', t => {
  const f = fixture(t), digest = plan(f.opts).approval_digest;
  for (const a of [{}, {...approval(digest), confirmManagerStopped: false}, {...approval(digest), reason: ''}, {...approval(digest), fenceReference: 'x\nsecret'}]) {
    assert.throws(() => apply(f.opts, a), {code: 'EXPLICIT_OUTBOX_FENCE_APPROVAL_REQUIRED'});
    assert.equal(fs.existsSync(f.marker), false);
  }
  assert.throws(() => apply(f.opts, approval('0'.repeat(64))), {code: 'APPROVED_OUTBOX_PLAN_CHANGED'});
  assert.equal(fs.existsSync(f.marker), false);
});
test('content or identity changes invalidate original approval', t => {
  const f = fixture(t), digest = plan(f.opts).approval_digest;
  fs.writeFileSync(path.join(f.queue, 'operator-note'), 'changed');
  assert.throws(() => apply(f.opts, approval(digest)), {code: 'APPROVED_OUTBOX_PLAN_CHANGED'});
  assert.equal(fs.existsSync(f.marker), false);
});
test('existing marker including an incomplete one is never adopted; inspector reports it even with missing queue', t => {
  const f = fixture(t);
  fs.mkdirSync(f.marker);
  assert.throws(() => plan(f.opts), {code: 'OUTBOX_MAINTENANCE_PRESENT'});
  assert.equal(inspectShutdownOutbox(f.opts).maintenance_present, true);
  assert.equal(inspectShutdownOutbox(f.opts).blocked_locally, true);
  const other = path.join(f.opts.directory, 'temporarily-moved'); fs.renameSync(f.queue, other);
  assert.equal(inspectShutdownOutbox(f.opts).scope_present, false);
  assert.equal(inspectShutdownOutbox(f.opts).blocked_locally, true);
  assert.equal(fs.readdirSync(f.marker).length, 0);
});
for (const phase of ['before_archive', 'before_move', 'after_move', 'before_unlock']) test(`real process exit at ${phase} resumes under renewed explicit approval`, t => {
  const f = fixture(t), before = originalFiles(f), id = interrupt(f, phase), p = recoveryPlan(f.opts, id);
  assert.equal(p.stage, phase === 'before_archive' || phase === 'before_move' ? 'before_move' : phase === 'after_move' ? 'after_move' : 'before_unlock');
  assert.equal(p.requires_maintenance_fence, true);
  assert.throws(() => resume(f.opts, id, {...approval(p.approval_digest), confirmMaintenanceStopped: false}), {code: 'EXPLICIT_OUTBOX_FENCE_APPROVAL_REQUIRED'});
  const r = resume(f.opts, id, approval(p.approval_digest));
  assert.equal(r.quarantined, true); assert.equal(fs.existsSync(f.marker), false);
  assert.deepEqual(fs.readdirSync(path.join(f.archiveRoot, id, 'queue')).sort().map(name => [name, fs.readFileSync(path.join(f.archiveRoot, id, 'queue', name), 'utf8')]), before);
});
test('partial or tampered prepared audit is never overwritten on recovery', t => {
  const f = fixture(t), id = interrupt(f, 'after_move');
  const file = path.join(f.archiveRoot, id, 'prepared.json'); fs.writeFileSync(file, '{');
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'INVALID_OUTBOX_AUDIT'});
  assert.equal(fs.readFileSync(file, 'utf8'), '{'); assert.equal(fs.existsSync(f.marker), true);
});
test('partial completion is retained, not regenerated as success', t => {
  const f = fixture(t), id = interrupt(f, 'after_move'), file = path.join(f.archiveRoot, id, 'completed.json');
  fs.writeFileSync(file, '{');
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'INVALID_OUTBOX_AUDIT'});
  assert.equal(fs.existsSync(f.marker), true); assert.equal(fs.readFileSync(file, 'utf8'), '{');
});
test('simultaneous source and archive under maintenance or changed archived content refuse recovery', t => {
  const f = fixture(t), id = interrupt(f, 'after_move');
  fs.mkdirSync(f.queue);
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'OUTBOX_SOURCE_ARCHIVE_CONFLICT'});
  fs.rmdirSync(f.queue);
  fs.writeFileSync(path.join(f.archiveRoot, id, 'queue', 'operator-note'), 'changed after archive');
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'OUTBOX_EVIDENCE_CHANGED'});
});
test('normal resume error removes only its own empty guard and remains retryable', t => {
  const f = fixture(t), id = interrupt(f, 'before_archive'), p = recoveryPlan(f.opts, id), mkdir = fs.mkdirSync;
  try {
    fs.mkdirSync = (file, ...rest) => { if (file === f.archiveRoot) throw new Error('PRIVATE SYSTEM ERROR'); return mkdir(file, ...rest); };
    assert.throws(() => resume(f.opts, id, approval(p.approval_digest)), {code: 'OUTBOX_RECONCILIATION_FAILED'});
  } finally { fs.mkdirSync = mkdir; }
  assert.equal(fs.existsSync(path.join(f.marker, 'resuming')), false); assert.equal(fs.existsSync(f.marker), true);
  assert.equal(resume(f.opts, id, approval(recoveryPlan(f.opts, id).approval_digest)).quarantined, true);
});
test('an existing or crashed recovery guard is never stolen', t => {
  const f = fixture(t), id = interrupt(f, 'before_move');
  fs.mkdirSync(path.join(f.marker, 'resuming'));
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'OUTBOX_RECOVERY_GUARD_PRESENT'});
  assert.equal(fs.existsSync(f.queue), true);
});
test('an actual recovery process crash leaves a guard and does not authorize takeover', t => {
  const f = fixture(t), id = interrupt(f, 'before_move'), p = recoveryPlan(f.opts, id);
  const code = `import fs from 'node:fs';import {resumeOutboxQuarantine} from ${JSON.stringify(scriptURL.href)};
    const o=JSON.parse(process.env.OUTBOX_TEST_OPTIONS),id=process.env.OUTBOX_TEST_ID,a=JSON.parse(process.env.OUTBOX_TEST_APPROVAL);
    const rename=fs.renameSync;fs.renameSync=(...args)=>{rename(...args);process.exit(78);};resumeOutboxQuarantine(o,id,a);`;
  const child = spawnSync(process.execPath, ['--input-type=module', '-e', code], {encoding: 'utf8', env: {...process.env,
    OUTBOX_TEST_OPTIONS: JSON.stringify(f.opts), OUTBOX_TEST_ID: id, OUTBOX_TEST_APPROVAL: JSON.stringify(approval(p.approval_digest))}});
  assert.equal(child.status, 78, child.stderr);
  assert.throws(() => recoveryPlan(f.opts, id), {code: 'OUTBOX_RECOVERY_GUARD_PRESENT'});
  assert.equal(fs.existsSync(path.join(f.marker, 'resuming')), true);
  assert.equal(fs.existsSync(path.join(f.archiveRoot, id, 'queue')), true);
});
test('new source appearing before unlock retains the diagnostic maintenance barrier', t => {
  const f = fixture(t), open = fs.openSync, p = plan(f.opts);
  try {
    fs.openSync = (file, ...rest) => {
      if (String(file).endsWith('completed.json') && rest[0] === 'wx') fs.mkdirSync(f.queue);
      return open(file, ...rest);
    };
    assert.throws(() => apply(f.opts, approval(p.approval_digest)), {code: 'OUTBOX_EVIDENCE_CHANGED'});
  } finally { fs.openSync = open; }
  assert.equal(fs.existsSync(f.marker), true);
  assert.equal(fs.existsSync(f.queue), true);
});
test('changed maintenance identity during final audit cannot be cleared', t => {
  const f = fixture(t), open = fs.openSync, p = plan(f.opts);
  try {
    fs.openSync = (file, ...rest) => {
      if (String(file).endsWith('completed.json') && rest[0] === 'wx') {
        fs.renameSync(f.marker, f.marker + '-original'); fs.mkdirSync(f.marker);
      }
      return open(file, ...rest);
    };
    assert.throws(() => apply(f.opts, approval(p.approval_digest)), {code: 'OUTBOX_MAINTENANCE_CHANGED'});
  } finally { fs.openSync = open; }
  assert.equal(fs.existsSync(f.marker), true);
  assert.equal(fs.existsSync(path.join(f.marker + '-original', 'operation.json')), true);
});
test('archive history cap fails read-only planning without deleting prior history', t => {
  const f = fixture(t); fs.mkdirSync(f.archiveRoot);
  for (let i = 0; i < 256; i++) fs.mkdirSync(path.join(f.archiveRoot, String(i)));
  assert.throws(() => plan(f.opts), {code: 'OUTBOX_ARCHIVE_HISTORY_FULL'});
  assert.equal(fs.readdirSync(f.archiveRoot).length, 256);
  assert.equal(fs.existsSync(f.marker), false); assert.equal(fs.existsSync(f.queue), true);
});
test('changed recovery approval cannot move source and never creates recovery guard', t => {
  const f = fixture(t), id = interrupt(f, 'before_move');
  assert.throws(() => resume(f.opts, id, approval('0'.repeat(64))), {code: 'APPROVED_OUTBOX_PLAN_CHANGED'});
  assert.equal(fs.existsSync(f.queue), true);
  assert.equal(fs.existsSync(path.join(f.marker, 'resuming')), false);
});
test('wrong original credential cannot inspect or resign another namespace', t => {
  const f = fixture(t), id = interrupt(f, 'after_move');
  assert.throws(() => recoveryPlan({...f.opts, key: 'rotated'}, id));
  assert.equal(fs.existsSync(f.marker), true);
});
test('large files and nested directories fail without writes; scan remains bounded', t => {
  const f = fixture(t), file = path.join(f.queue, 'oversized'); fs.writeFileSync(file, 'x'.repeat(2049));
  assert.throws(() => plan(f.opts), {code: 'UNSAFE_OUTBOX_EVIDENCE'});
  fs.unlinkSync(file); fs.mkdirSync(file);
  assert.throws(() => plan(f.opts), {code: 'UNSAFE_OUTBOX_EVIDENCE'});
  fs.rmdirSync(file);
  for (let i = 0; i < 1023; i++) fs.writeFileSync(path.join(f.queue, 'entry-' + i), '');
  assert.throws(() => plan(f.opts), {code: 'OUTBOX_SCAN_LIMIT'});
  assert.equal(fs.existsSync(f.marker), false);
});
test('CLI defaults read-only, forbids secret/force/duplicate arguments and requires complete approval', t => {
  const f = fixture(t), env = {...process.env, CLAWMANAGER_SHUTDOWN_OUTBOX_DIR: f.opts.directory, CLAWMANAGER_SHUTDOWN_OUTBOX_KEY: f.opts.key, CLAWMANAGER_SHUTDOWN_OUTBOX_SCOPE: f.opts.sourceScope};
  const run = args => spawnSync(process.execPath, [fileURLToPath(scriptURL), ...args], {env, encoding: 'utf8'});
  const p = run([]); assert.equal(p.status, 0, p.stderr); assert.equal(JSON.parse(p.stdout).read_only, true);
  for (const args of [['--key', 'secret'], ['--force'], ['--apply', '--apply'], ['--reason', 'without apply'], ['--resume', '../bad']]) {
    const r = run(args); assert.equal(r.status, 1); assert.equal(r.stdout, '');
    assert.equal(r.stderr.includes(f.opts.key), false); assert.equal(r.stderr.includes(f.opts.directory), false);
  }
  assert.equal(run(['--apply']).status, 1); assert.equal(fs.existsSync(f.marker), false);
});
test('Linux file links, hardlinks and linked archive destinations are refused', {skip: process.platform === 'win32'}, t => {
  const f = fixture(t), outside = path.join(f.opts.directory, 'outside'); fs.writeFileSync(outside, 'preserve');
  const link = path.join(f.queue, 'linked'); fs.symlinkSync(outside, link);
  assert.throws(() => plan(f.opts), {code: 'UNSAFE_OUTBOX_EVIDENCE'});
  fs.unlinkSync(link); fs.linkSync(outside, link);
  assert.throws(() => plan(f.opts), {code: 'UNSAFE_OUTBOX_EVIDENCE'});
  fs.unlinkSync(link);
  const p = plan(f.opts); fs.symlinkSync(f.opts.directory, f.archiveRoot);
  assert.throws(() => apply(f.opts, approval(p.approval_digest)));
  assert.equal(fs.readFileSync(outside, 'utf8'), 'preserve'); assert.equal(fs.existsSync(f.queue), true); assert.equal(fs.existsSync(f.marker), false);
});
