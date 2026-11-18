// Read-only dual-key handoff preflight. It does NOT publish/resign queue files.
import fs from 'node:fs';
import path from 'node:path';
import {createHash, createHmac} from 'node:crypto';
import {pathToFileURL} from 'node:url';
import {inspectShutdownOutbox} from './inspect-runtime-shutdown-outbox.mjs';
import {checkDirectory, sameFile} from './inspect-openclaw-receipts.mjs';

const hash = v => createHash('sha256').update(v).digest('hex');
const sign = (key, value) => createHmac('sha256', key).update(JSON.stringify(value)).digest('hex');
const fail = code => { throw Object.assign(new Error(code), {code}); };
const present = file => { try { fs.lstatSync(file); return true; } catch (e) { if (e.code === 'ENOENT') return false; throw e; } };
const identity = s => ({device: String(s.dev), inode: String(s.ino), birthtime: String(s.birthtimeNs)});
function stableHash(file) {
  const before = fs.lstatSync(file, {bigint: true});
  if (!before.isFile() || before.isSymbolicLink() || before.nlink !== 1n || before.size > 2048n) fail('UNSAFE_ROTATION_SOURCE');
  const fd = fs.openSync(file, fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW ?? 0));
  let raw;
  try {
    const opened = fs.fstatSync(fd, {bigint: true}), buffer = Buffer.alloc(2049);
    if (!sameFile(before, opened, true)) fail('ROTATION_SOURCE_CHANGED');
    const n = fs.readSync(fd, buffer, 0, buffer.length, 0); raw = buffer.subarray(0, n);
    if (n !== Number(before.size) || n > 2048 || !sameFile(opened, fs.fstatSync(fd, {bigint: true}))) fail('ROTATION_SOURCE_CHANGED');
  } finally { fs.closeSync(fd); }
  if (!sameFile(before, fs.lstatSync(file, {bigint: true}))) fail('ROTATION_SOURCE_CHANGED');
  return {hash: hash(raw), identity: identity(before), size: String(before.size), mtime: String(before.mtimeNs)};
}
export function planShutdownCredentialRotation({directory, key, sourceScope, targetKey, targetScope} = {}) {
  if (typeof targetKey !== 'string' || !targetKey || targetKey === key || typeof targetScope !== 'string' || !targetScope) fail('INVALID_ROTATION_CONFIGURATION');
  // Equal numeric user/instance IDs at another endpoint do NOT prove the same
  // control plane. Endpoint rotation requires a separately verified realm ID.
  if (targetScope !== sourceScope) fail('CONTROL_PLANE_IDENTITY_REQUIRED');
  let report;
  try { report = inspectShutdownOutbox({directory, key, sourceScope}); } catch { fail('ROTATION_SOURCE_UNVERIFIED'); }
  if (!report.scope_present || report.maintenance_present || report.blocked_locally || report.records.length > 256 || report.records.some(r => r.status !== 'verified_local_record')) fail('ROTATION_SOURCE_UNVERIFIED');
  const parent = path.join(directory, 'shutdown-outbox'), root = path.join(parent, report.namespace);
  const targetNamespace = hash(targetScope + '\0' + targetKey), targetRoot = path.join(parent, targetNamespace);
  if (present(targetRoot) || present(path.join(parent, '.maintenance-' + targetNamespace))) fail('ROTATION_TARGET_NOT_EMPTY');
  const before = checkDirectory(root), baseID = identity(checkDirectory(directory)), parentID = identity(checkDirectory(parent));
  const files = report.records.map(r => ({id: r.id, event: r.event, ...stableHash(path.join(root, r.id + '.json'))}));
  const again = inspectShutdownOutbox({directory, key, sourceScope});
  if (JSON.stringify(again) !== JSON.stringify(report) || !sameFile(before, checkDirectory(root)) ||
      JSON.stringify(baseID) !== JSON.stringify(identity(checkDirectory(directory))) || JSON.stringify(parentID) !== JSON.stringify(identity(checkDirectory(parent))) ||
      present(targetRoot) || present(path.join(parent, '.maintenance-' + targetNamespace))) fail('ROTATION_SOURCE_CHANGED');
  // Bind raw ORIGINAL bytes as well as IDs; a future executor must retain this
  // source, preserve original owner/generation, and verify both approvals.
  const manifest = {version: 1, scope_hash: hash(sourceScope), path_hash: hash(path.resolve(directory)),
    source_namespace: report.namespace, target_namespace: targetNamespace, base: baseID, parent: parentID, source: identity(before), files};
  const sourceApproval = sign(key, ['shutdown-outbox-rotation-source-v1', manifest]);
  const targetApproval = sign(targetKey, ['shutdown-outbox-rotation-target-v1', manifest, sourceApproval]);
  return {read_only: true, diagnostic_only: true, executable: false, lifecycle_authorized: false,
    source_namespace: report.namespace, target_namespace: targetNamespace, entries: files.length,
    source_approval_digest: sourceApproval, target_approval_digest: targetApproval,
    blocker: 'ROTATION_EXECUTOR_AND_DURABLE_HANDOFF_REQUIRED', preserves_original_owner_and_generation: true};
}
export function main(argv = process.argv.slice(2), env = process.env) {
  if (argv.length === 1 && argv[0] === '--help') { console.log('Read-only dual-key rotation preflight. Original source uses CLAWMANAGER_SHUTDOWN_OUTBOX_{DIR,KEY,SCOPE}; target uses CLAWMANAGER_SHUTDOWN_OUTBOX_TARGET_{KEY,SCOPE}. No key arguments, writes, migration or replay.'); return 0; }
  try {
    if (argv.length) fail('INVALID_ARGUMENTS');
    console.log(JSON.stringify(planShutdownCredentialRotation({directory: env.CLAWMANAGER_SHUTDOWN_OUTBOX_DIR, key: env.CLAWMANAGER_SHUTDOWN_OUTBOX_KEY, sourceScope: env.CLAWMANAGER_SHUTDOWN_OUTBOX_SCOPE,
      targetKey: env.CLAWMANAGER_SHUTDOWN_OUTBOX_TARGET_KEY, targetScope: env.CLAWMANAGER_SHUTDOWN_OUTBOX_TARGET_SCOPE}), null, 2)); return 0;
  } catch (e) {
    const allowed = new Set(['INVALID_ARGUMENTS', 'INVALID_ROTATION_CONFIGURATION', 'CONTROL_PLANE_IDENTITY_REQUIRED', 'ROTATION_SOURCE_UNVERIFIED', 'ROTATION_TARGET_NOT_EMPTY', 'ROTATION_SOURCE_CHANGED', 'UNSAFE_ROTATION_SOURCE']);
    console.error(JSON.stringify({error: allowed.has(e.code) ? e.code : 'ROTATION_PREFLIGHT_FAILED', action: 'retain'})); return 1;
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) process.exitCode = main();
