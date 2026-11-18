import assert from 'node:assert/strict';
import path from 'node:path';
import { createRequire } from 'node:module';
import { chromium, expect } from '@playwright/test';
const root = path.resolve(import.meta.dirname, '../frontend');
const require = createRequire(path.join(root, 'package.json'));
const { createServer } = require('vite');
process.chdir(root);
const fixture = `
import React, {useState} from 'react'; import {createRoot} from 'react-dom/client';
import {MemoryRouter} from 'react-router-dom'; import UserLayout from '/src/components/UserLayout.tsx'; import {AuthProvider} from '/src/contexts/AuthContext.tsx';
import ConfirmDialog from '/src/components/ConfirmDialog.tsx'; import ExpandableSearch from '/src/components/ExpandableSearch.tsx'; import FilterSelect from '/src/components/FilterSelect.tsx'; import ActionMenu from '/src/components/ActionMenu.tsx';
import {I18nProvider} from '/src/contexts/I18nContext.tsx';
import '/src/styles/design-tokens.css'; import '/src/index.css'; import '/src/styles/management-ui.css';
const h=React.createElement;
function DynamicControls(){const [options,setOptions]=useState([{value:'a',label:'Alpha'},{value:'b',label:'Bravo'},{value:'c',label:'Charlie'}]),[value,setValue]=useState('a'),[action,setAction]=useState(''); window.updateOptions=setOptions; return h('div',null,h(FilterSelect,{label:'Live filter',value,options,onChange:setValue}),h('output',{'aria-label':'Live value'},value),h(ActionMenu,{label:'Live actions',items:options.map(o=>({id:o.value,label:o.label,disabled:o.disabled,onSelect:()=>setAction(o.value)}))}),h('output',{'aria-label':'Live action'},action));}
function App(){const [open,setOpen]=useState(false),[loading,setLoading]=useState(false),[query,setQuery]=useState(new URLSearchParams(location.search).get('q')||''),[value,setValue]=useState('a'),[disabled,setDisabled]=useState(false),[action,setAction]=useState('');
const options=[{value:'a',label:'Alpha'},{value:'b',label:'Blocked',disabled:true},{value:'c',label:'Charlie'},{value:'d',label:'Delta '+ 'long option '.repeat(25)}];
return h(UserLayout,{title:'Controls'},h('div',{style:{padding:16}},
h(DynamicControls),h(ExpandableSearch,{value:query,onChange:setQuery,label:'Search instances',placeholder:'Search instances'}),h('output',{'aria-label':'Query'},query),
h(FilterSelect,{label:'Status',value,options,onChange:setValue,disabled}),h('button',{onClick:()=>setDisabled(!disabled)},'Toggle disabled'),h('button',{onClick:()=>setTimeout(()=>setDisabled(true),250)},'Disable soon'),
h(FilterSelect,{label:'Empty filter',value:'',options:[],onChange:()=>{}}),
h(ActionMenu,{label:'Resource actions',items:[{id:'disabled',label:'Unavailable',disabled:true,onSelect:()=>setAction('wrong')},{id:'edit',label:'Edit resource',onSelect:()=>setAction('edit')},{id:'delete',label:'Delete resource permanently',destructive:true,onSelect:()=>setAction('delete')}]}),h('output',{'aria-label':'Action result'},action),
h('button',{onClick:()=>setOpen(true)},'Delete resource'),h(ConfirmDialog,{open,loading,title:'Delete',message:'All data is removed',confirmLabel:'Confirm',cancelLabel:'Cancel',onConfirm:()=>{setLoading(true);setTimeout(()=>setLoading(false),700)},onCancel:()=>setOpen(false)}),
h('button',{onClick:()=>document.getElementById('nested').showModal()},'Open nested dialog'),
h('dialog',{id:'nested','aria-label':'Nested dialog',onCancel:e=>{e.preventDefault();e.currentTarget.close()}},h(FilterSelect,{label:'Nested status',value,options,onChange:setValue}),h(ActionMenu,{label:'Nested actions',items:[{id:'edit',label:'Edit nested',onSelect:()=>setAction('nested')}]})),
h('div',{style:{height:900}},'Outside target'),h('div',{style:{overflow:'hidden',height:35}},h(FilterSelect,{label:'Bottom filter',value,options,onChange:setValue}))))};
createRoot(document.getElementById('root')).render(h(MemoryRouter,null,h(AuthProvider,null,h(I18nProvider,null,h(App)))));
`;
const server=await createServer({root,server:{port:0},plugins:[{name:'controls-fixture',resolveId(id){if(id==='/controls.tsx')return '\0controls.tsx'},load(id){if(id==='\0controls.tsx')return fixture},configureServer(dev){dev.middlewares.use(async(req,res,next)=>{if(req.url?.split('?')[0]!='/controls')return next();res.setHeader('Content-Type','text/html');res.end(await dev.transformIndexHtml('/controls','<div id="root"></div><script type="module" src="/controls.tsx"></script>'));});}}]});
let browser;
try {
  await server.listen();
  browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_EXECUTABLE_PATH});
  const page=await browser.newPage({viewport:{width:390,height:700}});
  page.setDefaultTimeout(10000); const errors=[]; page.on('pageerror',error=>errors.push(error.message));
  await page.route('**/api/**',route=>route.fulfill({json:{data:{}}}));
  const origin='http://127.0.0.1:'+server.httpServer.address().port;
  await page.goto(origin+'/controls');
  const alpha={value:'a',label:'Alpha'},bravo={value:'b',label:'Bravo'},charlie={value:'c',label:'Charlie'};
  const update=options=>page.evaluate(options=>window.updateOptions(options),options);
  for (const [label,role,result] of [['Live filter','option','Live value'],['Live actions','menuitem','Live action']]) {
    const trigger=page.getByRole('button',{name:new RegExp('^'+label)});
    const popup=page.getByRole(role==='option'?'listbox':'menu',{name:label,exact:true});
    const item=name=>popup.getByRole(role,{name,exact:true});
    await update([alpha,bravo,charlie]); await trigger.press('ArrowDown'); await page.keyboard.press('ArrowDown'); await expect(item('Bravo')).toBeFocused();
    await update([bravo,charlie]); await expect(item('Bravo')).toBeFocused(); await page.keyboard.press('Enter'); await expect(page.getByLabel(result,{exact:true})).toHaveText('b'); await expect(trigger).toBeFocused();
    await update([alpha,bravo,charlie]); await trigger.press('ArrowDown'); await page.keyboard.press('b'); await expect(item('Bravo')).toBeFocused();
    await update([charlie,alpha,bravo]); await page.keyboard.press('ArrowDown'); await expect(item('Charlie')).toBeFocused();
    await page.keyboard.press('ArrowUp'); await expect(item('Bravo')).toBeFocused();
    await update([bravo,charlie,alpha]); await page.waitForTimeout(710); await page.keyboard.press('c'); await expect(item('Charlie')).toBeFocused();
    await update([bravo,alpha]); await expect(item('Bravo')).toBeFocused();
    await update([{...bravo,disabled:true},alpha]); await expect(item('Alpha')).toBeFocused();
    await page.keyboard.press('Enter'); await expect(page.getByLabel(result,{exact:true})).toHaveText('a');
    await trigger.press('ArrowDown'); await update([{...bravo,disabled:true},{...alpha,disabled:true}]); await expect(popup).toBeFocused(); await expect(popup.locator('[tabindex="0"]')).toHaveCount(0);
    await page.keyboard.press('Enter'); await expect(popup).toBeVisible(); await expect(page.getByLabel(result,{exact:true})).toHaveText('a');
    await update([]); await expect(popup).toBeFocused(); await page.keyboard.press('ArrowDown'); await page.keyboard.press('Enter'); await expect(popup).toBeVisible();
    await update([alpha,bravo,charlie]); await expect(item('Alpha')).toBeFocused(); await page.keyboard.press('End'); await expect(item('Charlie')).toBeFocused(); await page.keyboard.press('Home'); await expect(item('Alpha')).toBeFocused();
    await page.keyboard.press('Escape'); await expect(trigger).toBeFocused();
    console.log('PASS '+label+' live removal, reorder, disable, empty, movement, typeahead and identity selection');
  }
  const input=page.getByRole('textbox',{name:'Search instances'}),search=page.getByRole('button',{name:'Search instances',exact:true});
  await expect(input).toBeHidden(); await search.click(); await expect(input).toBeFocused();
  await input.fill('research'); await input.press('Escape'); await expect(input).toHaveValue('research'); await expect(input).toBeVisible();
  await page.getByRole('button',{name:'Clear search'}).click(); await expect(input).toHaveValue(''); await expect(input).toBeFocused();
  await input.press('Escape'); await expect(input).toBeHidden(); await expect(search).toBeFocused();
  console.log('PASS controlled search expand, retain, explicit clear and focus');
  const status=page.getByRole('button',{name:'Status: Alpha',exact:true});
  await status.press('ArrowDown'); const list=page.getByRole('listbox',{name:'Status',exact:true});
  await expect(list).toBeVisible(); await expect(page.getByRole('option',{name:'Alpha',exact:true})).toBeFocused();
  await page.keyboard.press('ArrowDown'); await expect(page.getByRole('option',{name:'Charlie',exact:true})).toBeFocused();
  await page.keyboard.press('Home'); await page.keyboard.press('c'); await page.keyboard.press('Enter');
  await expect(page.getByRole('button',{name:'Status: Charlie',exact:true})).toBeFocused();
  await page.getByRole('button',{name:'Status: Charlie',exact:true}).click();
  await page.getByRole('option',{name:'Blocked',exact:true}).click({force:true}); await expect(list).toBeVisible();
  await page.keyboard.press('End'); await expect(page.getByRole('option',{name:/^Delta/})).toBeFocused();
  const bounds=await list.boundingBox(); assert(bounds.x>=0 && bounds.x+bounds.width<=390);
  await page.keyboard.press('Escape'); await expect(list).toBeHidden();
  await page.getByRole('button',{name:'Status: Charlie',exact:true}).click(); await page.getByText('Outside target',{exact:true}).click(); await expect(list).toBeHidden();
  await page.getByRole('button',{name:'Toggle disabled'}).click(); await expect(page.getByRole('button',{name:'Status: Charlie',exact:true})).toBeDisabled();
  await page.getByRole('button',{name:'Toggle disabled'}).click();
  await page.getByRole('button',{name:'Empty filter: No options',exact:true}).click(); await expect(page.getByRole('listbox',{name:'Empty filter'})).toContainText('No options'); await page.keyboard.press('Escape');
  await page.getByRole('button',{name:'Disable soon'}).click(); await page.getByRole('button',{name:'Status: Charlie',exact:true}).click(); await expect(list).toBeHidden(); await expect(page.getByRole('button',{name:'Status: Charlie',exact:true})).toBeDisabled(); await page.getByRole('button',{name:'Toggle disabled'}).click();
  console.log('PASS filter keyboard, typeahead, disabled, empty, long option and outside dismissal');
  await page.getByRole('button',{name:'Resource actions'}).press('ArrowDown');
  await expect(page.getByRole('menuitem',{name:'Edit resource',exact:true})).toBeFocused(); await page.getByRole('menuitem',{name:'Unavailable',exact:true}).click({force:true}); await expect(page.getByLabel('Action result')).toHaveText(''); await expect(page.getByRole('menu',{name:'Resource actions'})).toBeVisible();
  await page.keyboard.press('End'); await expect(page.getByRole('menuitem',{name:/Delete resource permanently/})).toContainText('destructive action'); await page.keyboard.press('Enter');
  await expect(page.getByLabel('Action result')).toHaveText('delete'); await expect(page.getByRole('button',{name:'Resource actions'})).toBeFocused();
  console.log('PASS action menu semantics, disabled and destructive actions');
  await page.getByRole('button',{name:'Open nested dialog'}).click();
  const nested=page.getByRole('dialog',{name:'Nested dialog'}); await nested.getByRole('button',{name:/Nested status:/}).click();
  await page.keyboard.press('Escape'); await expect(nested).toBeVisible(); await expect(page.getByRole('listbox',{name:'Nested status'})).toBeHidden();
  await nested.getByRole('button',{name:'Nested actions'}).click(); await page.keyboard.press('Escape'); await expect(nested).toBeVisible();
  await page.keyboard.press('Escape'); await expect(nested).toBeHidden();
  console.log('PASS Escape closes inner popover before native dialog');
  const deleteButton=page.getByRole('button',{name:'Delete resource',exact:true});
  await deleteButton.click(); const dialog=page.getByRole('dialog',{name:'Delete',exact:true}); await expect(dialog).toBeVisible();
  await dialog.getByRole('button',{name:'Confirm',exact:true}).click(); await expect(dialog.getByRole('button',{name:'Cancel'})).toBeDisabled();
  await page.keyboard.press('Escape'); await expect(dialog).toBeVisible(); await expect(dialog).toContainText('All data is removed');
  await expect(dialog.getByRole('button',{name:'Cancel'})).toBeEnabled(); await page.keyboard.press('Escape'); await expect(dialog).toBeHidden(); await expect(deleteButton).toBeFocused();
  console.log('PASS confirmation semantics, loading, retained context and focus restoration');
  await page.getByLabel('Account',{exact:true}).click();
  await page.getByRole('button',{name:'Language: English',exact:true}).click();
  await page.keyboard.press('Escape'); await expect(page.getByRole('button',{name:'Language: English',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Language: English',exact:true}).click(); await page.getByRole('option',{name:'Deutsch',exact:true}).click();
  await expect(page.getByRole('button',{name:/Deutsch$/})).toBeVisible(); await expect(page.locator('html')).toHaveAttribute('lang','de');
  await page.keyboard.press('Escape'); await expect(page.getByLabel('Konto',{exact:true})).toBeFocused();
  await page.getByLabel('Konto',{exact:true}).click(); await page.getByRole('button',{name:/Deutsch$/}).click(); await page.getByText('Outside target',{exact:true}).click(); await expect(page.getByRole('listbox',{name:'Sprache'})).toBeHidden(); await expect(page.getByRole('button',{name:/Deutsch$/})).toBeHidden();
  console.log('PASS account locale selection, nested Escape, outside dismissal, containment and focus');
  await page.evaluate(()=>localStorage.setItem('clawmanager_locale','en')); await page.goto(origin+'/controls?q=initial'); await expect(input).toHaveValue('initial'); await expect(input).toBeVisible(); await input.fill(''); await expect(input).toBeVisible(); await input.fill('new query'); await page.getByRole('button',{name:'Clear search'}).click(); await expect(input).toBeVisible(); await expect(input).toBeFocused(); await input.press('Escape'); await expect(input).toBeHidden();
  await page.getByRole('button',{name:/Bottom filter:/}).click(); const bottom=page.getByRole('listbox',{name:'Bottom filter',includeHidden:true}); await expect(bottom).toBeVisible();
  const triggerBounds=await page.getByRole('button',{name:/Bottom filter:/}).boundingBox(),popupBounds=await bottom.boundingBox(); assert(popupBounds.y+popupBounds.height<=triggerBounds.y,'Bottom popup flips above its trigger');
  await page.setViewportSize({width:320,height:600}); await expect.poll(async()=>{const resized=await bottom.boundingBox();return !resized || (resized.x>=0 && resized.x+resized.width<=320 && resized.y>=0 && resized.y+resized.height<=600);}).toBeTruthy(); if (!await bottom.isVisible()) await page.getByRole('button',{name:/Bottom filter:/}).click(); await expect(bottom).toBeVisible(); await page.screenshot({path:'/tmp/cm-uiux08-20261111/task3-popup-320.png'}); await page.evaluate(()=>window.scrollTo(0,0)); await expect(bottom).toBeHidden();
  await page.keyboard.press('Escape'); assert.deepEqual(errors,[]); console.log('PASS URL value, clipped-container top layer, bottom flip and viewport resize');
} finally {await browser?.close();await server.close();}
