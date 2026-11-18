// Isolated test harness only. No production credentials, endpoints or volumes.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {spawnSync} from 'node:child_process';

assert.equal(process.platform,'linux','Linux contract tests must not silently skip');
assert.equal(process.version,'v24.21.0','Use the pinned native runtime Node version');
assert.match(fs.readFileSync('/etc/machine-id','utf8').trim(),/^[a-f0-9]{32}$/);
assert.match(fs.readlinkSync('/proc/self/ns/pid'),/^pid:\[\d+\]$/);
assert.ok(process.env.OPENCLAW_RECEIPT_STORE_MODULE,'Runtime writer compatibility must run');
assert.ok(fs.existsSync(process.env.OPENCLAW_RECEIPT_STORE_MODULE));
const tests=['/tests/inspect-openclaw-receipts.test.mjs','/tests/inspect-runtime-shutdown-outbox.test.mjs','/tests/plan-runtime-shutdown-credential-rotation.test.mjs','/tests/reconcile-runtime-shutdown-outbox.test.mjs','/tests/reconcile-openclaw-executions.test.mjs','/tests/reconcile-openclaw-writer.test.mjs'];
if(process.argv.includes('--include-runtime')){
  assert.ok(process.env.OPENCLAW_NATIVE_FIXTURE_DIR,'Native fixture tests must not silently skip');
  tests.push(...['test_receipt_retry','test_task_receipt_store','test_managed_turn_bridge','test_runtime_task_guard','test_managed_model_reference_102','test_runtime_capability_renewal','test_runtime_writer_guard','test_managed_shutdown_scope','test_shutdown_receipt_store'].map(name=>'/runtime/'+name+'.mjs'));
}
const result=spawnSync(process.execPath,['--test','--test-reporter=spec',...tests],{stdio:'inherit',timeout:180000});
if(result.error)throw result.error;
process.exit(result.status??1);
