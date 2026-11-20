import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';
const source=readFileSync(new URL('../src/lib/modelCatalogSync.ts',import.meta.url),'utf8');
const {outputText}=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext}});
const {modelCatalogSyncPresentation:present}=await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
test('legacy and failed observations never claim a successful write or unavailable models',()=>{
  assert.match(present(undefined,'zh-CN').label,/未确认/);
  assert.match(present(undefined,'zh-CN').title,/不代表模型不可用/);
  assert.match(present({state:'failed',generation:3,model_count:0},'zh-CN').label,/同步失败/);
  assert.doesNotMatch(present({state:'unknown',generation:3,model_count:0},'en').label,/synced/);
});
test('successful receipt is explicitly startup-only and shows confirmed model count',()=>{
  const value=present({state:'applied',generation:3,model_count:4,revision:'a'.repeat(64)},'zh-CN');
  assert.match(value.label,/已同步 \(4\)/);
  assert.match(value.title,/不是实时模型健康探测/);
  assert.match(present({state:'empty',generation:3,model_count:0},'zh-CN').label,/暂无可用模型/);
});
test('badge excludes stale generations and request callbacks after teardown',()=>{
  const badge=readFileSync(new URL('../src/components/ModelCatalogSyncBadge.tsx',import.meta.url),'utf8');
  assert.match(badge,/report\.value\.generation === generation/);
  assert.match(badge,/if \(!cancelled\) setReport/);
  assert.match(badge,/cancelled = true; clearTimeout/);
});
