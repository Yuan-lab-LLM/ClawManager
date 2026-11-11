import assert from 'node:assert/strict';
import { test } from 'node:test';

const helper = () => import('../src/lib/browserOrigin.ts?' + crypto.randomUUID());
const location = (href='https://manager.example/instances/118?panel=preview#draft') => {
  const replacements=[];
  const url=new URL(href);
  globalThis.window={location:{href,origin:url.origin,replace:value=>replacements.push(value)}};
  return replacements;
};

test('legacy documents allow multiple instances; mode pins one instance across intermediate screens',async()=>{
 const {allowBrowserInstance}=await helper();const replacements=location();
 assert(allowBrowserInstance(1));assert(allowBrowserInstance(2));
 const mode={browser_origin_mode:'chrome-single-host',manager_origin:'https://manager.example'};
 assert(allowBrowserInstance(118,mode));
 assert(allowBrowserInstance(null));assert(allowBrowserInstance(118));
 assert.equal(allowBrowserInstance(119),false);
 assert.equal(allowBrowserInstance(119),false);
 assert.deepEqual(replacements,[window.location.href]);
});

test('canonical manager redirect preserves the route without touching browser storage',async()=>{
 const {allowBrowserInstance}=await helper();const replacements=location();
 assert.equal(allowBrowserInstance(118,{browser_origin_mode:'chrome-single-host',manager_origin:'https://canonical.example:8443'}),false);
 assert.deepEqual(replacements,['https://canonical.example:8443/instances/118?panel=preview#draft']);
});

test('invalid canonical metadata fails closed and external viewers require the trusted manager path',async()=>{
 const {allowBrowserInstance,trustedPreviewViewer}=await helper();location();
 assert.equal(allowBrowserInstance(118,{browser_origin_mode:'chrome-single-host'}),false);
 assert.equal(allowBrowserInstance(118,{browser_origin_mode:'chrome-single-host',manager_origin:'javascript:alert(1)'}),false);
 assert.equal(trustedPreviewViewer('https://preview.example/bootstrap#token'),undefined);
 assert.equal(trustedPreviewViewer('https://manager.example/arbitrary#token'),undefined);
 assert.equal(trustedPreviewViewer('https://manager.example/__clawmanager_browser/view#opaque'),'https://manager.example/__clawmanager_browser/view#opaque');
});
