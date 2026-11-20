// Offline administrative quarantine of DIAGNOSTICS, never a task/lifecycle proof.
// Requires all manager writers fenced, including old versions that lack the marker.
import fs from 'node:fs';
import path from 'node:path';
import {createHash, createHmac, timingSafeEqual, randomUUID} from 'node:crypto';
import {pathToFileURL} from 'node:url';
import {checkDirectory, sameFile} from './inspect-openclaw-receipts.mjs';

const hex = /^[a-f0-9]{64}$/;
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/;
const hash = raw => createHash('sha256').update(raw).digest('hex');
const fail = code => { throw Object.assign(new Error(code), {code}); };
const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const present = file => { try { fs.lstatSync(file); return true; } catch (e) { if (e.code === 'ENOENT') return false; throw e; } };
const identity = s => ({device: String(s.dev), inode: String(s.ino), birthtime: String(s.birthtimeNs)});
const metadata = s => ({...identity(s), size: String(s.size), mtime: String(s.mtimeNs), mode: String(s.mode)});
const sign = (key, domain, value) => createHmac('sha256', key).update(JSON.stringify([domain, value])).digest('hex');
const validText = v => typeof v === 'string' && v.trim() === v && v.length > 0 && v.length <= 200 && !/[\x00-\x1f\x7f]/.test(v);
const wrap = (key, domain, value) => ({value, mac: sign(key, domain, value)});
function sanitized(error) {
  if (error?.message === error?.code && /^[A-Z_]+$/.test(error.code)) return error;
  return Object.assign(new Error('OUTBOX_RECONCILIATION_FAILED'), {code: 'OUTBOX_RECONCILIATION_FAILED'});
}
function syncDirectory(dir) {
  if (process.platform === 'win32') return;
  const fd = fs.openSync(dir, fs.constants.O_RDONLY | (fs.constants.O_DIRECTORY ?? 0) | (fs.constants.O_NOFOLLOW ?? 0));
  try { fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
}
function readRaw(file, max = 2048) {
  const before = fs.lstatSync(file, {bigint: true});
  if (!before.isFile() || before.isSymbolicLink() || before.nlink !== 1n || before.size > BigInt(max)) fail('UNSAFE_OUTBOX_EVIDENCE');
  const fd = fs.openSync(file, fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW ?? 0));
  let raw;
  try {
    const opened = fs.fstatSync(fd, {bigint: true});
    if (!sameFile(before, opened, true) || !opened.isFile()) fail('OUTBOX_EVIDENCE_CHANGED');
    const buffer = Buffer.alloc(max + 1), n = fs.readSync(fd, buffer, 0, buffer.length, 0);
    raw = buffer.subarray(0, n);
    if (n !== Number(before.size) || n > max || !sameFile(opened, fs.fstatSync(fd, {bigint: true}))) fail('OUTBOX_EVIDENCE_CHANGED');
  } finally { fs.closeSync(fd); }
  const after = fs.lstatSync(file, {bigint: true});
  if (!sameFile(before, after) || !same(metadata(before), metadata(after)) || after.nlink !== 1n) fail('OUTBOX_EVIDENCE_CHANGED');
  return {raw, stat: metadata(before)};
}
function readAudit(file, key, domain) {
  const {raw} = readRaw(file, 524288);
  let e;
  try { e = JSON.parse(raw.toString('utf8')); } catch { fail('INVALID_OUTBOX_AUDIT'); }
  if (!e || Object.keys(e).sort().join(',') !== 'mac,value' || !hex.test(e.mac ?? '') ||
      !timingSafeEqual(Buffer.from(e.mac, 'hex'), Buffer.from(sign(key, domain, e.value), 'hex')) ||
      !raw.equals(Buffer.from(JSON.stringify(e)))) fail('INVALID_OUTBOX_AUDIT');
  return e;
}
function writeExclusive(file, value) {
  const raw = Buffer.from(JSON.stringify(value));
  if (raw.length > 524288) fail('OUTBOX_AUDIT_LIMIT');
  const fd = fs.openSync(file, 'wx', 0o600);
  try { fs.writeFileSync(fd, raw); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
}
function config(options) {
  const {directory, key, sourceScope} = options;
  if (typeof directory !== 'string' || !path.isAbsolute(directory) || path.resolve(directory) === path.parse(path.resolve(directory)).root ||
      typeof key !== 'string' || !key || typeof sourceScope !== 'string' || !sourceScope) fail('INVALID_CONFIGURATION');
  const base = path.resolve(directory), namespace = hash(sourceScope + '\0' + key), parent = path.join(base, 'shutdown-outbox');
  checkDirectory(base); checkDirectory(parent);
  return {base, parent, namespace, key, scopeHash: hash(sourceScope), queue: path.join(parent, namespace),
    maintenance: path.join(parent, '.maintenance-' + namespace), archiveRoot: path.join(base, 'shutdown-outbox-archive')};
}
function queueSnapshot(c, source = c.queue) {
  const before = checkDirectory(source), dir = fs.opendirSync(source), names = [];
  try {
    for (let e; (e = dir.readSync()) !== null;) {
      if (names.length >= 1024) fail('OUTBOX_SCAN_LIMIT');
      names.push(e.name);
    }
  } finally { dir.closeSync(); }
  const files = names.sort().map(name => {
    const {raw, stat} = readRaw(path.join(source, name));
    return {name, stat, hash: hash(raw)};
  });
  if (!sameFile(before, checkDirectory(source))) fail('OUTBOX_EVIDENCE_CHANGED');
  return {namespace: c.namespace, scope_hash: c.scopeHash, path_hash: hash(c.base),
    base: identity(checkDirectory(c.base)), parent: identity(checkDirectory(c.parent)), queue: identity(before), files};
}
const planDigest = (c, snapshot) => sign(c.key, 'shutdown-outbox-quarantine-plan-v1', snapshot);
const publicPlan = (snapshot, digest, extra = {}) => ({read_only: true, diagnostic_only: true, action: 'review_quarantine',
  approval_digest: digest, entries: snapshot.files.length, namespace: snapshot.namespace,
  requires_manager_fence: true, lifecycle_authorized: false, automatic_replay_allowed: false, ...extra});
function ensureArchiveRoot(c) {
  if (!present(c.archiveRoot)) fs.mkdirSync(c.archiveRoot, {mode: 0o700});
  checkDirectory(c.archiveRoot); syncDirectory(c.base);
}
function checkArchiveCapacity(c) {
  if (!present(c.archiveRoot)) return;
  checkDirectory(c.archiveRoot);
  const dir = fs.opendirSync(c.archiveRoot);
  try {
    let count = 0;
    while (dir.readSync() !== null) if (++count >= 256) fail('OUTBOX_ARCHIVE_HISTORY_FULL');
  } finally { dir.closeSync(); }
}
function createDestination(c, destination) {
  ensureArchiveRoot(c);
  if (present(destination)) { checkDirectory(destination); return; }
  checkArchiveCapacity(c);
  fs.mkdirSync(destination, {mode: 0o700}); syncDirectory(c.archiveRoot);
}
function validateApproval(a, resume = false) {
  if (!a || !hex.test(a.digest ?? '') || a.confirmManagerStopped !== true || (resume && a.confirmMaintenanceStopped !== true) ||
      !validText(a.reason) || !validText(a.fenceReference)) fail('EXPLICIT_OUTBOX_FENCE_APPROVAL_REQUIRED');
}
function assertMaintenance(c, operation, lockID) {
  if (!same(identity(checkDirectory(c.maintenance)), lockID) ||
      !same(readAudit(path.join(c.maintenance, 'operation.json'), c.key, 'shutdown-outbox-quarantine-operation-v1'), operation)) fail('OUTBOX_MAINTENANCE_CHANGED');
}
function localRecoveryScope() {
  if (process.platform !== 'linux') return null;
  try {
    const machine = fs.readFileSync('/etc/machine-id', 'utf8').trim(), boot = fs.readFileSync('/proc/sys/kernel/random/boot_id', 'utf8').trim(), ns = fs.readlinkSync('/proc/self/ns/pid');
    if (!/^[a-f0-9]{32}$/.test(machine) || !uuid.test(boot) || !/^pid:\[\d+\]$/.test(ns)) return null;
    return hash(JSON.stringify([machine, boot, ns]));
  } catch { return null; }
}
function requireDeadRecoveryOwner(owner) {
  const scope = localRecoveryScope();
  if (!scope || !owner.scope) fail('OUTBOX_OWNER_PROBE_UNSUPPORTED');
  if (scope !== owner.scope) fail('OUTBOX_OWNER_SCOPE_MISMATCH');
  try { process.kill(owner.pid, 0); } catch (e) { if (e.code === 'ESRCH') return; fail('OUTBOX_OWNER_STATE_UNKNOWN'); }
  fail('OUTBOX_OWNER_STILL_RUNNING');
}
function readRecoveryOwner(c, root, operation, retired = false, allowMissingAudit = false) {
  const stat = checkDirectory(root), names = fs.readdirSync(root).sort().join(',');
  if (names !== 'owner.json' && !(retired && names === 'owner.json,retirement.json')) fail('INVALID_OUTBOX_RECOVERY_OWNER');
  const envelope = readAudit(path.join(root, 'owner.json'), c.key, 'shutdown-outbox-recovery-owner-v1'), o = envelope.value;
  if (o?.version !== 1 || !uuid.test(o.token ?? '') || !Number.isInteger(o.pid) || o.pid < 1 || o.pid > 2147483647 ||
      !(o.scope === null || typeof o.scope === 'string' && hex.test(o.scope)) || o.operation_mac !== operation.mac ||
      !same(o.guard_identity, identity(stat)) || !same(o.maintenance_identity, operation.value.maintenance_identity) ||
      !hex.test(o.approval_digest ?? '') || (retired && path.basename(root) !== o.token)) fail('INVALID_OUTBOX_RECOVERY_OWNER');
  let audited = false;
  if (retired && present(path.join(root, 'retirement.json'))) {
    const a = readAudit(path.join(root, 'retirement.json'), c.key, 'shutdown-outbox-guard-retirement-audit-v1').value;
    if (a?.version !== 1 || a.owner_mac !== envelope.mac || a.operation_mac !== operation.mac || a.owner_token !== o.token ||
        a.proof !== 'absent_same_linux_scope' || !hex.test(a.approval_digest ?? '') || !validText(a.reason) || !validText(a.fence_reference)) fail('INVALID_OUTBOX_GUARD_HISTORY');
    audited = true;
  }
  if (retired && !audited && !allowMissingAudit) fail('OUTBOX_GUARD_RETIREMENT_INCOMPLETE');
  return {envelope, identity: identity(stat), audited};
}
function guardTokens(root) {
  if (!present(root)) return [];
  checkDirectory(root);
  const dir = fs.opendirSync(root), tokens = [];
  try {
    for (let e; (e = dir.readSync()) !== null;) {
      if (tokens.length >= 32) fail('OUTBOX_GUARD_HISTORY_FULL');
      if (!uuid.test(e.name)) fail('INVALID_OUTBOX_GUARD_HISTORY');
      tokens.push(e.name);
    }
  } finally { dir.closeSync(); }
  return tokens.sort();
}
function verifyGuardHistory(c, destination, operation, allowIncompleteToken) {
  const root = path.join(destination, 'retired-guards'), tokens = guardTokens(root);
  for (const token of tokens) readRecoveryOwner(c, path.join(root, token), operation, true, token === allowIncompleteToken);
  return tokens;
}
function removeOwnedGuard(c, root, guardID, owner) {
  if (!same(identity(checkDirectory(root)), guardID)) fail('OUTBOX_RECOVERY_GUARD_CHANGED');
  if (!owner) { if (fs.readdirSync(root).length) fail('OUTBOX_RECOVERY_GUARD_CHANGED'); }
  else {
    if (fs.readdirSync(root).sort().join(',') !== 'owner.json' || !same(readAudit(path.join(root, 'owner.json'), c.key, 'shutdown-outbox-recovery-owner-v1'), owner)) fail('OUTBOX_RECOVERY_GUARD_CHANGED');
    fs.unlinkSync(path.join(root, 'owner.json'));
  }
  fs.rmdirSync(root); syncDirectory(c.maintenance);
}
function verifyOperationRepair(c, operation, destination) {
  const file = path.join(destination, 'operation-repair.json');
  if (!present(file)) return false;
  const a = readAudit(file, c.key, 'shutdown-outbox-operation-repair-v1').value;
  if (a?.operation_mac !== operation.mac || !same(a.maintenance_identity, operation.value.maintenance_identity) ||
      a.proof !== 'authorize_restore_signed_prepared_copy' || !hex.test(a.approval_digest ?? '') || !validText(a.reason) || !validText(a.fence_reference)) fail('INVALID_OUTBOX_AUDIT');
  if (a.damaged_operation !== undefined) {
    const preserved = path.join(destination, 'damaged-operation.json');
    const evidence = readRaw(present(preserved) ? preserved : path.join(c.maintenance, 'operation.json'), 524288);
    if (!same(a.damaged_operation, {hash: hash(evidence.raw), stat: evidence.stat})) fail('OUTBOX_DAMAGED_OPERATION_CHANGED');
  }
  return a;
}
function loadOperation(c, destination, damageOverride) {
  const file = path.join(c.maintenance, 'operation.json');
  const missing = !present(file);
  if (!missing && !damageOverride) {
    try { return {operation: readAudit(file, c.key, 'shutdown-outbox-quarantine-operation-v1'), missing: false, damaged: false}; }
    catch (originalError) {
      const operation = readAudit(path.join(destination, 'prepared.json'), c.key, 'shutdown-outbox-quarantine-operation-v1');
      const repair = verifyOperationRepair(c, operation, destination);
      if (!repair?.damaged_operation || present(path.join(destination, 'damaged-operation.json'))) throw originalError;
      return {operation, missing: false, damaged: true};
    }
  }
  const operation = readAudit(path.join(destination, 'prepared.json'), c.key, 'shutdown-outbox-quarantine-operation-v1');
  if (damageOverride) {
    const raw = readRaw(file, 524288);
    if (!same(damageOverride, {hash: hash(raw.raw), stat: raw.stat})) fail('OUTBOX_DAMAGED_OPERATION_CHANGED');
  }
  return {operation, missing, damaged: Boolean(damageOverride)};
}
export function planOutboxQuarantine(options = {}) {
  try {
    const c = config(options);
    if (present(c.maintenance)) fail('OUTBOX_MAINTENANCE_PRESENT');
    checkArchiveCapacity(c);
    const snapshot = queueSnapshot(c);
    if (Buffer.byteLength(JSON.stringify(snapshot)) > 500000) fail('OUTBOX_AUDIT_LIMIT');
    return publicPlan(snapshot, planDigest(c, snapshot));
  } catch (e) { throw sanitized(e); }
}
function verifyCompletion(c, operation, destination) {
  const done = readAudit(path.join(destination, 'completed.json'), c.key, 'shutdown-outbox-quarantine-completion-v1').value;
  if (done?.operation_mac !== operation.mac || done.result !== 'quarantined' || !hex.test(done.approval_digest ?? '') ||
      !validText(done.fence_reference) || !validText(done.reason)) fail('INVALID_OUTBOX_AUDIT');
}
function completion(c, operation, destination, approval) {
  const file = path.join(destination, 'completed.json');
  if (!present(file)) writeExclusive(file, wrap(c.key, 'shutdown-outbox-quarantine-completion-v1', {
    operation_mac: operation.mac, result: 'quarantined', approval_digest: approval.digest,
    fence_reference: approval.fenceReference, reason: approval.reason}));
  verifyCompletion(c, operation, destination);
  syncDirectory(destination);
}
function result(id) { return {quarantined: true, archive_id: id, diagnostic_only: true, action: 'review_before_manager_restart',
  automatic_replay_allowed: false, lifecycle_authorized: false}; }
function verifyArchived(c, operation, destination) {
  if (present(c.queue) || !same(queueSnapshot(c, path.join(destination, 'queue')), operation.value.snapshot) ||
      !same(readAudit(path.join(destination, 'prepared.json'), c.key, 'shutdown-outbox-quarantine-operation-v1'), operation)) fail('OUTBOX_EVIDENCE_CHANGED');
  verifyCompletion(c, operation, destination);
  verifyGuardHistory(c, destination, operation);
  const repair = verifyOperationRepair(c, operation, destination);
  if (present(path.join(destination, 'damaged-operation.json')) && !repair?.damaged_operation) fail('INVALID_OUTBOX_AUDIT');
}
function releaseMaintenance(c, operation, lockID) {
  assertMaintenance(c, operation, lockID);
  if (fs.readdirSync(c.maintenance).sort().join(',') !== 'operation.json') fail('OUTBOX_MAINTENANCE_CHANGED');
  fs.unlinkSync(path.join(c.maintenance, 'operation.json'));
  fs.rmdirSync(c.maintenance); syncDirectory(c.parent);
}
export function applyOutboxQuarantine(options = {}, approval = {}) {
  validateApproval(approval);
  let c, archiveID, ownsLock = false;
  try {
    c = config(options);
    if (present(c.maintenance)) fail('OUTBOX_MAINTENANCE_PRESENT');
    checkArchiveCapacity(c);
    const snapshot = queueSnapshot(c);
    if (planDigest(c, snapshot) !== approval.digest) fail('APPROVED_OUTBOX_PLAN_CHANGED');
    fs.mkdirSync(c.maintenance, {mode: 0o700}); ownsLock = true;
    const lockID = identity(checkDirectory(c.maintenance)); archiveID = randomUUID();
    const operation = wrap(c.key, 'shutdown-outbox-quarantine-operation-v1', {version: 1, archive_id: archiveID,
      diagnostic_only: true, approval_digest: approval.digest, fence_reference: approval.fenceReference,
      reason: approval.reason, maintenance_identity: lockID, snapshot});
    writeExclusive(path.join(c.maintenance, 'operation.json'), operation);
    syncDirectory(c.maintenance); syncDirectory(c.parent);
    if (planDigest(c, queueSnapshot(c)) !== approval.digest) fail('APPROVED_OUTBOX_PLAN_CHANGED');
    const destination = path.join(c.archiveRoot, archiveID);
    createDestination(c, destination);
    writeExclusive(path.join(destination, 'prepared.json'), operation); syncDirectory(destination);
    assertMaintenance(c, operation, lockID);
    if (planDigest(c, queueSnapshot(c)) !== approval.digest) fail('APPROVED_OUTBOX_PLAN_CHANGED');
    fs.renameSync(c.queue, path.join(destination, 'queue'));
    syncDirectory(destination); syncDirectory(c.parent);
    if (!same(queueSnapshot(c, path.join(destination, 'queue')), snapshot) || present(c.queue)) fail('OUTBOX_EVIDENCE_CHANGED');
    completion(c, operation, destination, approval);
    verifyArchived(c, operation, destination);
    releaseMaintenance(c, operation, lockID);
    return result(archiveID);
  } catch (e) {
    const error = sanitized(e);
    error.maintenanceLockRetained = ownsLock;
    if (archiveID) error.archiveID = archiveID;
    throw error;
  }
}

function recoveryContext(options, archiveID, ownedGuard, allowIncompleteToken, damageOverride) {
  const c = config(options);
  if (!uuid.test(archiveID ?? '')) fail('INVALID_OUTBOX_ARCHIVE_ID');
  if (present(c.archiveRoot)) checkDirectory(c.archiveRoot);
  const destination = path.join(c.archiveRoot, archiveID), locked = present(c.maintenance);
  // Only a MISSING operation can use the complete signed prepared copy. Never
  // overwrite a partial/corrupt operation, or infer ownership from an empty lock.
  const loaded = locked ? loadOperation(c, destination, damageOverride) : {operation: readAudit(path.join(destination, 'prepared.json'), c.key, 'shutdown-outbox-quarantine-operation-v1'), missing: false, damaged: false};
  const {operation, missing: operationMissing, damaged: operationDamaged} = loaded;
  const op = operation.value;
  if (op?.version !== 1 || op.archive_id !== archiveID || op.diagnostic_only !== true ||
      op.snapshot?.namespace !== c.namespace || op.snapshot?.scope_hash !== c.scopeHash || op.snapshot?.path_hash !== hash(c.base) ||
      !same(op.snapshot.base, identity(checkDirectory(c.base))) || !same(op.snapshot.parent, identity(checkDirectory(c.parent))) ||
      op.approval_digest !== planDigest(c, op.snapshot) || !validText(op.reason) || !validText(op.fence_reference)) fail('INVALID_OUTBOX_AUDIT');
  if (locked) {
    if (!same(identity(checkDirectory(c.maintenance)), op.maintenance_identity)) fail('OUTBOX_MAINTENANCE_CHANGED');
    const names = fs.readdirSync(c.maintenance).sort();
    if (names.some(n => !['operation.json', 'resuming'].includes(n))) fail('OUTBOX_MAINTENANCE_CHANGED');
    if (names.includes('resuming')) {
      if (!ownedGuard) fail('OUTBOX_RECOVERY_GUARD_PRESENT');
      const owner = readRecoveryOwner(c, path.join(c.maintenance, 'resuming'), operation);
      if (!same(owner.identity, ownedGuard.identity) || !same(owner.envelope, ownedGuard.envelope)) fail('OUTBOX_RECOVERY_GUARD_CHANGED');
    } else if (ownedGuard) fail('OUTBOX_RECOVERY_GUARD_CHANGED');
  }
  const hasDestination = present(destination);
  if (hasDestination) {
    checkDirectory(destination);
    const allowed = new Set(['prepared.json', 'queue', 'completed.json', 'retired-guards', 'operation-repair.json', 'damaged-operation.json']);
    if (fs.readdirSync(destination).some(name => !allowed.has(name))) fail('INVALID_OUTBOX_AUDIT');
    const prepared = path.join(destination, 'prepared.json');
    if (present(prepared) && !same(readAudit(prepared, c.key, 'shutdown-outbox-quarantine-operation-v1'), operation)) fail('INVALID_OUTBOX_AUDIT');
    verifyGuardHistory(c, destination, operation, allowIncompleteToken);
    const repair = verifyOperationRepair(c, operation, destination);
    if (present(path.join(destination, 'damaged-operation.json')) && !repair?.damaged_operation) fail('INVALID_OUTBOX_AUDIT');
  }
  const archivedQueue = path.join(destination, 'queue'), sourcePresent = present(c.queue), archivedPresent = present(archivedQueue);
  if ((locked && sourcePresent === archivedPresent) || (!locked && !archivedPresent)) fail('OUTBOX_SOURCE_ARCHIVE_CONFLICT');
  const snapshot = queueSnapshot(c, locked && sourcePresent ? c.queue : archivedQueue);
  if (!same(snapshot, op.snapshot)) fail('OUTBOX_EVIDENCE_CHANGED');
  const preparedPresent = hasDestination && present(path.join(destination, 'prepared.json'));
  const completedPresent = hasDestination && present(path.join(destination, 'completed.json'));
  if (completedPresent) {
    if ((locked && sourcePresent) || !preparedPresent) fail('INVALID_OUTBOX_AUDIT');
    verifyCompletion(c, operation, destination);
  }
  if ((!locked && (!completedPresent || !preparedPresent)) || (!sourcePresent && !preparedPresent)) fail('INVALID_OUTBOX_AUDIT');
  const stage = !locked ? 'already_quarantined' : sourcePresent ? 'before_move' : completedPresent ? 'before_unlock' : 'after_move';
  const state = {archive_id: archiveID, stage, operation_mac: operation.mac, prepared_present: preparedPresent, destination_present: hasDestination, operation_missing: operationMissing, operation_damaged: operationDamaged};
  return {c, destination, operation, snapshot, state, locked, operationMissing, operationDamaged, digest: sign(c.key, 'shutdown-outbox-quarantine-resume-v1', state)};
}
export function planOutboxQuarantineRecovery(options = {}, archiveID) {
  try {
    const r = recoveryContext(options, archiveID);
    return publicPlan(r.snapshot, r.digest, {archive_id: archiveID, stage: r.state.stage, requires_maintenance_fence: r.locked, operation_audit_restore_required: r.operationMissing || r.operationDamaged, damaged_operation_preservation_required: r.operationDamaged});
  } catch (e) { throw sanitized(e); }
}
function damagedOperationContext(options, archiveID) {
  const c = config(options);
  if (!uuid.test(archiveID ?? '')) fail('INVALID_OUTBOX_ARCHIVE_ID');
  const destination = path.join(c.archiveRoot, archiveID);
  if (present(path.join(destination, 'operation-repair.json')) || present(path.join(destination, 'damaged-operation.json'))) fail('OUTBOX_REPAIR_AUTHORIZATION_EXISTS');
  const file = path.join(c.maintenance, 'operation.json'), raw = readRaw(file, 524288);
  try { readAudit(file, c.key, 'shutdown-outbox-quarantine-operation-v1'); fail('OUTBOX_OPERATION_NOT_DAMAGED'); }
  catch (e) { if (e.code !== 'INVALID_OUTBOX_AUDIT') throw e; }
  const damage = {hash: hash(raw.raw), stat: raw.stat};
  const r = recoveryContext(options, archiveID, undefined, undefined, damage);
  if (!r.locked || !r.operationDamaged) fail('OUTBOX_MAINTENANCE_REQUIRED');
  const digest = sign(c.key, 'shutdown-outbox-damaged-operation-plan-v1', {archive_id: archiveID, operation_mac: r.operation.mac, snapshot: r.snapshot, damage});
  return {...r, damage, digest};
}
export function planDamagedOutboxOperationRepair(options = {}, archiveID) {
  try {
    const r = damagedOperationContext(options, archiveID);
    return publicPlan(r.snapshot, r.digest, {archive_id: archiveID, action: 'review_damaged_operation_preservation',
      damaged_bytes_hash: r.damage.hash, requires_maintenance_fence: true, existing_bytes_will_be_preserved: true});
  } catch (e) { throw sanitized(e); }
}
export function authorizeDamagedOutboxOperationRepair(options = {}, archiveID, approval = {}) {
  validateApproval(approval, true);
  try {
    const r = damagedOperationContext(options, archiveID);
    if (r.digest !== approval.digest) fail('APPROVED_OUTBOX_PLAN_CHANGED');
    writeExclusive(path.join(r.destination, 'operation-repair.json'), wrap(r.c.key, 'shutdown-outbox-operation-repair-v1', {
      operation_mac: r.operation.mac, maintenance_identity: r.operation.value.maintenance_identity,
      proof: 'authorize_restore_signed_prepared_copy', approval_digest: approval.digest, reason: approval.reason,
      fence_reference: approval.fenceReference, damaged_operation: r.damage}));
    syncDirectory(r.destination);
    verifyOperationRepair(r.c, r.operation, r.destination);
    return {action: 'damaged_operation_preservation_authorized', archive_id: archiveID, diagnostic_only: true,
      maintenance_lock_retained: true, lifecycle_authorized: false, automatic_replay_allowed: false, next_step: 'review_recovery'};
  } catch (e) { const error = sanitized(e); error.archiveID = archiveID; error.maintenanceLockRetained = true; throw error; }
}
export function resumeOutboxQuarantine(options = {}, archiveID, approval = {}) {
  validateApproval(approval, true);
  let r, guard, guardID, guardOwner;
  try {
    r = recoveryContext(options, archiveID);
    if (r.digest !== approval.digest) fail('APPROVED_OUTBOX_PLAN_CHANGED');
    if (!r.locked) return result(archiveID);
    guard = path.join(r.c.maintenance, 'resuming');
    fs.mkdirSync(guard, {mode: 0o700}); guardID = identity(checkDirectory(guard));
    guardOwner = wrap(r.c.key, 'shutdown-outbox-recovery-owner-v1', {version: 1, token: randomUUID(), pid: process.pid,
      scope: localRecoveryScope(), operation_mac: r.operation.mac, maintenance_identity: r.operation.value.maintenance_identity,
      guard_identity: guardID, approval_digest: approval.digest});
    writeExclusive(path.join(guard, 'owner.json'), guardOwner); syncDirectory(guard); syncDirectory(r.c.maintenance);
    const current = recoveryContext(options, archiveID, {identity: guardID, envelope: guardOwner});
    if (current.digest !== r.digest || !same(identity(checkDirectory(guard)), guardID)) fail('APPROVED_OUTBOX_PLAN_CHANGED');
    createDestination(r.c, r.destination);
    const prepared = path.join(r.destination, 'prepared.json');
    if (!present(prepared)) { writeExclusive(prepared, r.operation); syncDirectory(r.destination); }
    if (r.operationMissing || r.operationDamaged) {
      const repair = path.join(r.destination, 'operation-repair.json');
      if (!present(repair)) writeExclusive(repair, wrap(r.c.key, 'shutdown-outbox-operation-repair-v1', {
        operation_mac: r.operation.mac, maintenance_identity: r.operation.value.maintenance_identity,
        proof: 'authorize_restore_signed_prepared_copy', approval_digest: approval.digest, reason: approval.reason, fence_reference: approval.fenceReference}));
      verifyOperationRepair(r.c, r.operation, r.destination); syncDirectory(r.destination);
      if (r.operationDamaged) {
        const file = path.join(r.c.maintenance, 'operation.json'), preserved = path.join(r.destination, 'damaged-operation.json');
        if (present(preserved)) fail('OUTBOX_DAMAGED_OPERATION_CHANGED');
        // The independently approved repair binds the original damaged bytes.
        // Keep that exact file; never truncate, replace or re-sign its contents.
        verifyOperationRepair(r.c, r.operation, r.destination);
        fs.renameSync(file, preserved); syncDirectory(r.destination); syncDirectory(r.c.maintenance);
      }
      writeExclusive(path.join(r.c.maintenance, 'operation.json'), r.operation); syncDirectory(r.c.maintenance);
    }
    if (present(r.c.queue)) {
      if (!same(queueSnapshot(r.c), r.snapshot)) fail('OUTBOX_EVIDENCE_CHANGED');
      fs.renameSync(r.c.queue, path.join(r.destination, 'queue'));
      syncDirectory(r.destination); syncDirectory(r.c.parent);
    }
    if (present(r.c.queue) || !same(queueSnapshot(r.c, path.join(r.destination, 'queue')), r.snapshot)) fail('OUTBOX_EVIDENCE_CHANGED');
    completion(r.c, r.operation, r.destination, approval);
    verifyArchived(r.c, r.operation, r.destination);
    assertMaintenance(r.c, r.operation, r.operation.value.maintenance_identity);
    removeOwnedGuard(r.c, guard, guardID, guardOwner); guard = undefined;
    releaseMaintenance(r.c, r.operation, r.operation.value.maintenance_identity);
    return {...result(archiveID), operation_audit_restored: r.operationMissing || r.operationDamaged};
  } catch (e) {
    const error = sanitized(e);
    error.archiveID = archiveID;
    error.maintenanceLockRetained = Boolean(r?.locked);
    throw error;
  } finally {
    // Only a normal failed invocation may remove its own unchanged signed guard.
    // A process crash leaves a guard that is never stolen by another invocation.
    if (guard && guardID) {
      try {
        removeOwnedGuard(r.c, guard, guardID, guardOwner);
      } catch {}
    }
  }
}

function staleGuardContext(options, archiveID, ownerToken) {
  const c = config(options);
  if (!uuid.test(archiveID ?? '') || (ownerToken !== undefined && !uuid.test(ownerToken))) fail('INVALID_OUTBOX_ARCHIVE_ID');
  checkDirectory(c.maintenance);
  const destination = path.join(c.archiveRoot, archiveID), active = path.join(c.maintenance, 'resuming'), root = path.join(destination, 'retired-guards');
  const {operation} = loadOperation(c, destination);
  let loaded, source, activeOwner;
  if (present(active)) {
    loaded = readRecoveryOwner(c, active, operation);
    if (ownerToken !== undefined && ownerToken !== loaded.envelope.value.token) fail('OUTBOX_RECOVERY_OWNER_CHANGED');
    ownerToken = loaded.envelope.value.token; source = active; activeOwner = loaded;
  } else {
    if (ownerToken === undefined) {
      const candidates = guardTokens(root).filter(t => !present(path.join(root, t, 'retirement.json')));
      if (candidates.length !== 1) fail('OUTBOX_RECOVERY_OWNER_REQUIRED');
      ownerToken = candidates[0];
    }
    source = path.join(root, ownerToken);
    loaded = readRecoveryOwner(c, source, operation, true, true);
  }
  const r = recoveryContext(options, archiveID, activeOwner, ownerToken);
  if (!r.locked) fail('OUTBOX_MAINTENANCE_REQUIRED');
  const tokens = verifyGuardHistory(c, destination, r.operation, ownerToken);
  if (activeOwner && tokens.includes(ownerToken)) fail('OUTBOX_RECOVERY_OWNER_CHANGED');
  if (!loaded.audited) requireDeadRecoveryOwner(loaded.envelope.value);
  const history = tokens.filter(t => t !== ownerToken).map(token => ({token,
    owner_mac: readRecoveryOwner(c, path.join(root, token), r.operation, true).envelope.mac,
    audit_mac: readAudit(path.join(root, token, 'retirement.json'), c.key, 'shutdown-outbox-guard-retirement-audit-v1').mac}));
  const digest = sign(c.key, 'shutdown-outbox-guard-retirement-plan-v1', {archive_id: archiveID, operation_mac: r.operation.mac,
    owner: loaded.envelope, snapshot: r.snapshot, stage: r.state.stage, operation_missing: r.operationMissing, history});
  return {...r, root, source, loaded, tokens, active: Boolean(activeOwner), digest, ownerToken};
}
export function planOutboxGuardRetirement(options = {}, archiveID, ownerToken) {
  try {
    const r = staleGuardContext(options, archiveID, ownerToken);
    return {read_only: true, diagnostic_only: true, action: 'review_outbox_recovery_guard_retirement', archive_id: archiveID,
      owner_token: r.ownerToken, approval_digest: r.digest, owner_state: r.loaded.audited ? 'already_retired' : 'absent_same_linux_scope',
      maintenance_lock_retained: true, lifecycle_authorized: false, automatic_replay_allowed: false};
  } catch (e) { throw sanitized(e); }
}
export function retireOutboxRecoveryGuard(options = {}, archiveID, ownerToken, approval = {}) {
  validateApproval(approval, true);
  try {
    let r = staleGuardContext(options, archiveID, ownerToken);
    if (r.digest !== approval.digest) fail('APPROVED_OUTBOX_PLAN_CHANGED');
    if (!r.loaded.audited) {
      if (r.active) {
        if (r.tokens.length >= 32) fail('OUTBOX_GUARD_HISTORY_FULL');
        createDestination(r.c, r.destination);
        const prepared = path.join(r.destination, 'prepared.json');
        if (!present(prepared)) { writeExclusive(prepared, r.operation); syncDirectory(r.destination); }
        if (!present(r.root)) { fs.mkdirSync(r.root, {mode: 0o700}); syncDirectory(r.destination); }
        checkDirectory(r.root);
        r = staleGuardContext(options, archiveID, r.ownerToken);
        if (r.digest !== approval.digest || !r.active) fail('APPROVED_OUTBOX_PLAN_CHANGED');
        requireDeadRecoveryOwner(r.loaded.envelope.value);
        fs.renameSync(r.source, path.join(r.root, r.ownerToken)); syncDirectory(r.root); syncDirectory(r.c.maintenance);
      }
      r = staleGuardContext(options, archiveID, r.ownerToken);
      if (r.digest !== approval.digest) fail('APPROVED_OUTBOX_PLAN_CHANGED');
      if (!r.loaded.audited) {
        requireDeadRecoveryOwner(r.loaded.envelope.value);
        writeExclusive(path.join(r.source, 'retirement.json'), wrap(r.c.key, 'shutdown-outbox-guard-retirement-audit-v1', {
          version: 1, owner_mac: r.loaded.envelope.mac, operation_mac: r.operation.mac, owner_token: r.ownerToken,
          proof: 'absent_same_linux_scope', approval_digest: approval.digest, reason: approval.reason, fence_reference: approval.fenceReference}));
        syncDirectory(r.source);
      }
    }
    r = staleGuardContext(options, archiveID, r.ownerToken);
    if (!r.loaded.audited || r.digest !== approval.digest) fail('APPROVED_OUTBOX_PLAN_CHANGED');
    return {action: 'outbox_recovery_guard_retired', archive_id: archiveID, owner_token: r.ownerToken,
      diagnostic_only: true, maintenance_lock_retained: true, lifecycle_authorized: false, automatic_replay_allowed: false, next_step: 'review_recovery'};
  } catch (e) {
    const error = sanitized(e); error.archiveID = archiveID; error.maintenanceLockRetained = true; throw error;
  }
}

export function main(argv = process.argv.slice(2), env = process.env) {
  if (argv.length === 1 && argv[0] === '--help') { console.log('Offline diagnostic quarantine. Default read-only. --resume <archive-id> plans recovery; add --retire-recovery-guard [--owner-token <token>] for dead-owner retirement, or --repair-operation for separately approved preservation of a damaged operation with a valid prepared copy. --apply --approval <digest> --confirm-manager-stopped --fence-reference <ticket> --reason <text> executes. Resume/retirement/repair also require --confirm-maintenance-stopped. Original directory/key/scope use CLAWMANAGER_SHUTDOWN_OUTBOX_* environment only. No task/lifecycle authorization or automatic replay.'); return 0; }
  try {
    const a = {};
    for (let i = 0; i < argv.length; i++) {
      const name = argv[i];
      if (Object.hasOwn(a, name)) fail('INVALID_ARGUMENTS');
      if (['--apply', '--confirm-manager-stopped', '--confirm-maintenance-stopped', '--retire-recovery-guard', '--repair-operation'].includes(name)) a[name] = true;
      else if (['--resume', '--approval', '--fence-reference', '--reason', '--owner-token'].includes(name) && argv[i + 1] && !argv[i + 1].startsWith('--')) a[name] = argv[++i];
      else fail('INVALID_ARGUMENTS');
    }
    if ((!a['--apply'] && Object.keys(a).some(k => !['--resume', '--retire-recovery-guard', '--owner-token', '--repair-operation'].includes(k))) ||
        (!a['--resume'] && (a['--confirm-maintenance-stopped'] || a['--retire-recovery-guard'] || a['--repair-operation'])) ||
        (a['--repair-operation'] && a['--retire-recovery-guard']) ||
        (a['--owner-token'] && (!a['--retire-recovery-guard'] || !uuid.test(a['--owner-token'])))) fail('INVALID_ARGUMENTS');
    const options = {directory: env.CLAWMANAGER_SHUTDOWN_OUTBOX_DIR, key: env.CLAWMANAGER_SHUTDOWN_OUTBOX_KEY, sourceScope: env.CLAWMANAGER_SHUTDOWN_OUTBOX_SCOPE};
    const approval = {digest: a['--approval'], confirmManagerStopped: a['--confirm-manager-stopped'], confirmMaintenanceStopped: a['--confirm-maintenance-stopped'], fenceReference: a['--fence-reference'], reason: a['--reason']};
    const report = a['--repair-operation'] ? a['--apply'] ? authorizeDamagedOutboxOperationRepair(options, a['--resume'], approval) : planDamagedOutboxOperationRepair(options, a['--resume']) :
      a['--retire-recovery-guard'] ? a['--apply'] ? retireOutboxRecoveryGuard(options, a['--resume'], a['--owner-token'], approval) : planOutboxGuardRetirement(options, a['--resume'], a['--owner-token']) :
      a['--resume'] ? a['--apply'] ? resumeOutboxQuarantine(options, a['--resume'], approval) : planOutboxQuarantineRecovery(options, a['--resume']) : a['--apply'] ? applyOutboxQuarantine(options, approval) : planOutboxQuarantine(options);
    console.log(JSON.stringify(report, null, 2)); return 0;
  } catch (e) {
    const error = sanitized(e);
    console.error(JSON.stringify({error: error.code, action: 'retain', maintenance_lock_retained: error.maintenanceLockRetained === true, ...(uuid.test(error.archiveID ?? '') ? {archive_id: error.archiveID} : {})}));
    return 1;
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) process.exitCode = main();
