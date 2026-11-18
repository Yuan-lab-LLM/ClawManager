// Operator-only diagnostics. No network, queue creation, ACK, migration or deletion.
import fs from 'node:fs';
import path from 'node:path';
import {createHash, createHmac, timingSafeEqual} from 'node:crypto';
import {pathToFileURL} from 'node:url';
import {checkDirectory, sameFile} from './inspect-openclaw-receipts.mjs';

const hex = /^[a-f0-9]{64}$/;
const hash = data => createHash('sha256').update(data).digest('hex');
const outcomes = new Set(['cleanup_unconfirmed', 'cleanup_confirmed_release_unconfirmed', 'cleanup_confirmed_release_observed']);
const fail = code => { throw Object.assign(new Error(code), {code}); };
const integer = (v, min, max = Number.MAX_SAFE_INTEGER) => Number.isSafeInteger(v) && v >= min && v <= max;
const keys = (v, expected) => v && !Array.isArray(v) && Object.keys(v).sort().join(',') === expected;
const equalHex = (a, b) => typeof a === 'string' && hex.test(a) && timingSafeEqual(Buffer.from(a, 'hex'), Buffer.from(b, 'hex'));

function namesAt(directory, limit) {
  const before = checkDirectory(directory);
  const dir = fs.opendirSync(directory), names = [];
  try {
    for (let e; (e = dir.readSync()) !== null;) {
      if (names.length >= limit) fail('SCAN_LIMIT_EXCEEDED');
      names.push(e.name);
    }
  } finally { dir.closeSync(); }
  if (!sameFile(before, checkDirectory(directory))) fail('SOURCE_CHANGED_DURING_READ');
  return {before, names: names.sort()};
}

function readRecord(file) {
  const before = fs.lstatSync(file, {bigint: true});
  if (!before.isFile() || before.isSymbolicLink() || before.size > 2048n) return {status: 'unsafe_file'};
  const fd = fs.openSync(file, fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW ?? 0));
  let raw;
  try {
    const opened = fs.fstatSync(fd, {bigint: true});
    if (!opened.isFile() || !sameFile(before, opened, true)) fail('SOURCE_CHANGED_DURING_READ');
    const buffer = Buffer.alloc(2049);
    const length = fs.readSync(fd, buffer, 0, buffer.length, 0);
    raw = buffer.subarray(0, length);
    if (!sameFile(opened, fs.fstatSync(fd, {bigint: true}))) fail('SOURCE_CHANGED_DURING_READ');
  } finally { fs.closeSync(fd); }
  if (!sameFile(before, fs.lstatSync(file, {bigint: true}))) fail('SOURCE_CHANGED_DURING_READ');
  if (raw.length > 2048) return {status: 'unsafe_file'};
  return {raw};
}

function inspectRecord(root, name, key, namespace) {
  // Never open unknown names, links, directories or oversized files.
  const id = name.endsWith('.json') ? name.slice(0, -5) : '';
  if (!hex.test(id)) return {entry_hash: hash(name), status: 'unknown_entry'};
  const result = readRecord(path.join(root, name));
  if (result.status) return {id, status: result.status};
  let value;
  try { value = JSON.parse(result.raw.toString('utf8')); }
  catch { return {id, status: 'invalid_envelope'}; }
  const d = value?.delivery, e = d?.event;
  if (!keys(value, 'delivery,mac') || !keys(d, 'event,id') || !keys(e, 'claim_id,generation,instance_id,outcome,owner_user_id') ||
      !integer(e.instance_id, 1) || !integer(e.owner_user_id, 1) || !integer(e.generation, 0, 2147483647) ||
      typeof e.claim_id !== 'string' || !hex.test(e.claim_id) || !outcomes.has(e.outcome)) return {id, status: 'invalid_envelope'};
  // Fixed struct order mirrors encoding/json in both Go runtime modules.
  const event = {instance_id: e.instance_id, owner_user_id: e.owner_user_id, generation: e.generation, claim_id: e.claim_id, outcome: e.outcome};
  const delivery = {id: hash(JSON.stringify(event)), event};
  if (d.id !== id || delivery.id !== id) return {id, status: 'identity_mismatch'};
  const mac = createHmac('sha256', key).update('shutdown-observation-outbox-v1\0' + namespace + '\0').update(JSON.stringify(delivery)).digest('hex');
  if (!equalHex(value.mac, mac)) return {id, status: 'authentication_failed'};
  const canonical = Buffer.from(JSON.stringify({delivery, mac}));
  if (!result.raw.equals(canonical)) return {id, status: 'noncanonical_encoding'};
  return {id, status: 'verified_local_record', event};
}

// sourceScope is EXACTLY the producer scope, not a normalized guessed URL.
// Different credential namespaces are inventoried only; never guessed/adopted.
export function inspectShutdownOutbox({directory, key, sourceScope} = {}) {
  if (typeof directory !== 'string' || !path.isAbsolute(directory) || path.resolve(directory) === path.parse(path.resolve(directory)).root ||
      typeof key !== 'string' || key.length === 0 || typeof sourceScope !== 'string' || sourceScope.length === 0) fail('INVALID_CONFIGURATION');
  const namespace = hash(sourceScope + '\0' + key), root = path.join(directory, 'shutdown-outbox'), selected = path.join(root, namespace);
  const report = {version: 1, diagnostic_only: true, action: 'retain', namespace, scope_present: false,
    other_namespace_ids: [], unknown_root_entries: [], records: [], queue_at_capacity: false, blocked_locally: false, maintenance_present: false,
    server_acceptance: 'not_queried', lifecycle_authorized: false};
  try {
    let inventory;
    try { inventory = namesAt(root, 256); }
    catch (error) {
      if (error.code === 'ENOENT') return report;
      throw error;
    }
    for (const name of inventory.names) {
      if (name === '.maintenance-' + namespace) {
        report.maintenance_present = true;
        report.blocked_locally = true;
        continue; // Presence only; never read or follow an operator marker.
      }
      const s = fs.lstatSync(path.join(root, name));
      if (name === namespace && (!s.isDirectory() || s.isSymbolicLink())) fail('SOURCE_READ_FAILED');
      if (hex.test(name) && s.isDirectory() && !s.isSymbolicLink()) {
        if (name === namespace) report.scope_present = true;
        else report.other_namespace_ids.push(name);
      } else report.unknown_root_entries.push(hash(name));
    }
    if (report.scope_present) {
      const scan = namesAt(selected, 1024);
      report.queue_at_capacity = scan.names.length >= 256;
      for (const name of scan.names) report.records.push(inspectRecord(selected, name, key, namespace));
      if (!sameFile(scan.before, checkDirectory(selected))) fail('SOURCE_CHANGED_DURING_READ');
      report.blocked_locally ||= scan.names.length > 256 || report.records.some(r => r.status !== 'verified_local_record');
    }
    if (!sameFile(inventory.before, checkDirectory(root))) fail('SOURCE_CHANGED_DURING_READ');
    return report;
  } catch (error) {
    if (['SCAN_LIMIT_EXCEEDED', 'SOURCE_CHANGED_DURING_READ'].includes(error.code)) throw error;
    // No raw filenames, local paths, OS messages or secrets in CLI errors.
    fail('SOURCE_READ_FAILED');
  }
}

export function main(argv = process.argv.slice(2), env = process.env) {
  if (argv.length === 1 && argv[0] === '--help') {
    console.log('Read-only shutdown diagnostics. Set CLAWMANAGER_SHUTDOWN_OUTBOX_DIR, CLAWMANAGER_SHUTDOWN_OUTBOX_KEY and CLAWMANAGER_SHUTDOWN_OUTBOX_SCOPE through a protected environment. No arguments, network, ACK, migration or deletion.');
    return 0;
  }
  try {
    if (argv.length) fail('INVALID_ARGUMENTS');
    const result = inspectShutdownOutbox({directory: env.CLAWMANAGER_SHUTDOWN_OUTBOX_DIR, key: env.CLAWMANAGER_SHUTDOWN_OUTBOX_KEY, sourceScope: env.CLAWMANAGER_SHUTDOWN_OUTBOX_SCOPE});
    console.log(JSON.stringify(result, null, 2));
    return result.blocked_locally ? 2 : 0;
  } catch (error) {
    console.error(JSON.stringify({error: ['INVALID_CONFIGURATION', 'INVALID_ARGUMENTS', 'SOURCE_READ_FAILED', 'SCAN_LIMIT_EXCEEDED', 'SOURCE_CHANGED_DURING_READ'].includes(error.code) ? error.code : 'INSPECTION_FAILED', action: 'retain'}));
    return 1;
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) process.exitCode = main();
