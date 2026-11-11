import assert from 'node:assert/strict';
import { test } from 'node:test';
const helpers = await import('../src/components/workbench/gitPresentation.ts').catch(() => ({}));
const change = {path:'new name.txt',previous_path:'old name.txt',index_status:'R',worktree_status:'M',staged:true,untracked:false,additions:25,deletions:8};
test('untracked unknown totals stay unknown, known large counts survive', () => {
  assert.equal(typeof helpers.changeStats, 'function');
  assert.equal(helpers.changeStats({...change,untracked:true,staged:false,additions:0,deletions:0},false),null);
  assert.deepEqual(helpers.changeStats({...change,untracked:true,stats_known:true,additions:12000,deletions:0},false),{additions:12000,deletions:0});
  assert.equal(helpers.changeStats({...change,binary:true},true),null);
});
test('mixed states use side-specific counts and never total counts', () => {
  assert.equal(typeof helpers.changeStats, 'function');
  assert.equal(helpers.changeStats(change,true),null);
  const c = {...change,staged_additions:4,staged_deletions:1,worktree_additions:21,worktree_deletions:7};
  assert.deepEqual(helpers.changeStats(c,true),{additions:4,deletions:1});
  assert.deepEqual(helpers.changeStats(c,false),{additions:21,deletions:7});
});
test('bulk staging sends explicit unique rename paths and refuses over 1000', () => {
  assert.equal(typeof helpers.bulkPaths, 'function');
  assert.deepEqual(helpers.bulkPaths([change,{...change,path:'plain.txt',previous_path:undefined}],true),['old name.txt','new name.txt','plain.txt']);
  assert.deepEqual(helpers.bulkPaths([{...change,worktree_status:"R"}],false),['old name.txt','new name.txt']);
  assert.equal(helpers.bulkPaths(Array.from({length:1001},(_,i)=>({...change,path:`${i}`,previous_path:undefined})),false),null);
});
test('unified parser retains quoted metadata, line numbers and no-newline marker', () => {
  assert.equal(typeof helpers.parseUnifiedDiff, 'function');
  const rows=helpers.parseUnifiedDiff('diff --git "a/a b" "b/a b"\n--- "a/a b"\n+++ "b/a b"\n@@ -8,2 +8,2 @@\n context\n-old\n+new\n\\ No newline at end of file\n');
  assert.equal(rows[0].kind,'meta');
  assert.deepEqual(rows.slice(4).map(({kind,oldLine,newLine})=>[kind,oldLine,newLine]),[['context',8,8],['deletion',9,undefined],['addition',undefined,9],['meta',undefined,undefined]]);
});
test('image preview paths are workspace relative and MIME allowlist excludes SVG/HTML', () => {
  assert.equal(typeof helpers.imagePreviewPath, 'function');
  assert.equal(helpers.imagePreviewPath('repo','assets/photo.PNG'),'repo/assets/photo.PNG');
  assert.equal(helpers.imagePreviewPath('repo','other.bin','repo/image.webp'),'repo/image.webp');
  assert.equal(helpers.imagePreviewPath('repo','x.svg'),null);
  assert.equal(helpers.imagePreviewPath('repo','../x.png'),null);
  assert.equal(helpers.safeImageMime('image/svg+xml'),false);
  assert.equal(helpers.safeImageMime('image/png'),true);
});

 test('Git image previews match the workspace PNG/JPEG/GIF/WebP capabilities', () => {
  for (const ext of ['png','jpg','jpeg','gif','webp']) assert.equal(helpers.imagePreviewPath('repo', `asset.${ext}`), `repo/asset.${ext}`);
  for (const ext of ['avif','bmp','ico','svg']) assert.equal(helpers.imagePreviewPath('repo', `asset.${ext}`), null);
  for (const mime of ['image/png','image/jpeg','image/gif','image/webp']) assert.equal(helpers.safeImageMime(mime),true);
  for (const mime of ['image/avif','image/bmp','image/x-icon','image/vnd.microsoft.icon']) assert.equal(helpers.safeImageMime(mime),false);
 });
