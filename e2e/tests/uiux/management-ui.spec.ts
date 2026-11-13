import { test, expect } from '../../fixtures/test.js';
import { users } from '../../fixtures/users.js';

test.use({ storageState: users.admin.storageState });

test('@p1 instance search expands without discarding an active query', async ({ page }) => {
  await page.goto('/instances');
  const trigger = page.getByRole('button', { name: /^(搜索实例|Search instances)$/ });
  const input = page.getByRole('textbox', { name: /^(搜索实例|Search instances)$/ });
  await expect(input).toBeHidden();
  await trigger.click();
  await expect(input).toBeFocused();
  await input.fill('research');
  await input.press('Escape');
  await expect(input).toHaveValue('research');
  await expect(input).toBeVisible();
  await input.fill('');
  await input.press('Escape');
  await expect(input).toBeHidden();
  await expect(trigger).toBeFocused();
});

test('@p1 account language popup keeps the account open until its own Escape or outside click', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('clawmanager_locale', 'en'));
  await page.goto('/instances');
  await page.getByLabel('Account', { exact: true }).click();
  const language = page.getByRole('button', { name: 'Language: English', exact: true });
  await language.click();
  await expect(page.getByRole('listbox', { name: 'Language', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(language).toBeFocused();
  await language.click();
  await page.getByRole('option', { name: '中文', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh');
  const chineseLanguage = page.getByRole('button', { name: '语言: 中文', exact: true });
  await expect(chineseLanguage).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.getByLabel('账户', { exact: true })).toBeFocused();
  await expect(chineseLanguage).toBeHidden();
  await page.getByLabel('账户', { exact: true }).click();
  await chineseLanguage.click();
  await page.getByRole('navigation', { name: '当前位置', exact: true }).click();
  await expect(page.getByRole('listbox', { name: '语言', exact: true })).toBeHidden();
  await expect(chineseLanguage).toBeHidden();
});

test('@p1 instance table retains Lite selection during polling and filters reset pagination', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('clawmanager_locale', 'en'));
  let calls = 0;
  await page.route('**/api/v1/instances?*', async route => {
    calls++;
    const url = new URL(route.request().url());
    const rows = [{id: 81, name: 'selectable-lite', type: 'hermes', instance_mode: 'lite', runtime_type: 'gateway', status: calls > 1 ? 'stopped' : 'running'}, {id: 82, name: 'disabled-pro', type: 'openclaw', instance_mode: 'pro', runtime_type: 'container', status: 'running'}];
    await route.fulfill({json: {success: true, data: {instances: rows, total: url.searchParams.get('type') ? 2 : 102, page: Number(url.searchParams.get('page')), page_size: 100}}});
  });
  await page.route('**/api/v1/instances/batch/lifecycle', route => route.fulfill({json: {success: true, data: []}}));
  await page.goto('/instances?page=2');
  const lite = page.getByRole('row').filter({hasText: 'selectable-lite'});
  const pro = page.getByRole('row').filter({hasText: 'disabled-pro'});
  await lite.getByRole('button', {name: 'Actions: selectable-lite', exact: true}).click();
  await expect(page.getByRole('menuitem', {name: /Delete/})).toBeVisible();
  await page.keyboard.press('Escape');
  await lite.getByRole('checkbox').check();
  await expect(pro.getByRole('checkbox')).toBeDisabled();
  await expect.poll(() => calls, {timeout: 8000}).toBeGreaterThan(1);
  await expect(lite.getByRole('checkbox')).toBeChecked();
  await page.getByRole('button', {name: 'Filter by instance type: All types', exact: true}).click();
  await page.getByRole('option', {name: 'Hermes', exact: true}).click();
  await expect(page).toHaveURL(/type=hermes/);
  await expect(page).not.toHaveURL(/page=2/);
  await page.setViewportSize({width: 390, height: 844});
  const filters = page.getByText('Filters', {exact: true});
  await filters.click();
  await page.getByRole('button', {name: 'Filter by instance type: Hermes', exact: true}).click();
  await expect(page.getByRole('listbox', {name: 'Filter by instance type', exact: true})).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', {name: 'Filter by instance type: Hermes', exact: true})).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(filters).toBeFocused();
  await expect(page.getByRole('button', {name: 'Filter by instance type: Hermes', exact: true})).toBeHidden();
  const batchCreate = page.getByRole('link', {name: 'Batch Create', exact: true});
  await expect(batchCreate).toHaveAttribute('href', '/instances/new?batch=1');
  await batchCreate.click();
  await expect(page).toHaveURL(/\/instances\/new\?batch=1$/);
  await expect(page.getByRole('switch', {name: /^Batch create$/i})).toHaveAttribute('aria-checked', 'true');
});

test('@p1 instance activity policy and long errors remain keyboard accessible', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('clawmanager_locale', 'en'));
  let blocker = 'original-policy-blocker';
  let status = 'error';
  const fullError = Array.from({length: 100}, (_, index) => `Diagnostic ${index}: detailed runtime failure`).join('\n');
  await page.route('**/api/v1/instances?*', route => route.fulfill({json: {success: true, data: {instances: [{id: 83, name: 'policy-lite', type: 'hermes', instance_mode: 'lite', runtime_type: 'gateway', status, stopped_reason: 'manual', runtime_error_message: fullError, auto_stop_disabled: true, auto_stop_phase: 'checking-policy', auto_stop_blocker: blocker, defaults_policy_status: 'configured_pending_verification', monitoring_since: '2026-11-10T08:00:00Z', last_gateway_activity_at: '2026-11-10T09:00:00Z'}], total: 1}}}));
  await page.route('**/api/v1/instances/batch/lifecycle', route => route.fulfill({json: {success: true, data: []}}));
  await page.goto('/instances');
  const actions = page.getByRole('button', {name: 'Actions: policy-lite', exact: true});
  await actions.click();
  await page.getByRole('menuitem', {name: 'Activity and policy', exact: true}).click();
  const policy = page.getByRole('dialog', {name: 'Activity and policy: policy-lite', exact: true});
  await expect(policy).toContainText('original-policy-blocker');
  await expect(policy).toContainText('checking-policy');
  await expect(policy).toContainText('Keep running');
  await expect(policy).toContainText('Last model activity');
  await expect(policy).toContainText('Monitoring since');
  await expect(policy).toContainText('Configured; native verification pending');
  blocker = 'updated-policy-blocker';
  await expect(policy).toContainText(blocker, {timeout: 8000});
  status = 'stopped';
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(policy).toContainText('Stopped manually');
  status = 'error';
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await page.keyboard.press('Escape');
  await expect(actions).toBeFocused();
  const error = page.getByRole('button', {name: 'Error details: policy-lite', exact: true});
  await error.click();
  const details = page.getByRole('dialog', {name: 'Error details: policy-lite', exact: true});
  await expect(details).toBeFocused();
  await expect(details).toContainText('Diagnostic 99');
  await expect.poll(() => details.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true);
  await page.keyboard.press('PageDown');
  await expect.poll(() => details.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
  await page.keyboard.press('Escape');
  await expect(details).toBeHidden();
  await expect(error).toBeFocused();
});


for (const capacity of [8, 16, 64, 999999]) {
  test(`@p1 runtime table keeps capacity ${capacity} bounded at narrow width`, async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('clawmanager_locale', 'en'));
    const pod = {id: 991, namespace: 'geometry', pod_name: `runtime-long-name-${capacity}`, node_name: 'node-with-a-long-name', runtime_type: 'hermes', state: 'ready', capacity, used_slots: 0, bound_instances: 0, agent_reported: true, bindings_reported: true, slots: Array.from({length: Math.min(capacity, 64)}, (_,index) => ({index, container_name: `agent-${index}`, ready: true}))};
    await page.route('**/api/v1/admin/runtime-pods{,?*}', route => route.fulfill({json: {success: true, data: {pods: [pod], summary: {pods:1, online:1, schedulable:1, capacity, ready:0, occupied:0, bound_instances:0, available:0, running:0, starting:0, migrating:0, error:0, idle:0, unavailable:0, unknown:0}, pagination:{total:1,total_pages:1}, nodes:[pod.node_name]}}}));
    await page.goto('/admin/runtime-pods');
    const row = page.getByRole('row').filter({hasText: pod.pod_name});
    await expect(row).toBeVisible();
    await expect(row.getByRole('button', {name: pod.pod_name, exact:true})).toBeVisible();
    await expect(row.locator('.runtime-slot-overview')).toHaveCount(1);
    for (const width of [1440, 390]) {
      await page.setViewportSize({width,height:900});
      const graph = await row.locator('.runtime-slot-overview').boundingBox();
      const count = await row.locator('.runtime-slot-count').boundingBox();
      const actions = await row.locator('.runtime-row-actions').boundingBox();
      expect(graph!.x + graph!.width).toBeLessThanOrEqual(count!.x);
      expect(count!.x + count!.width).toBeLessThanOrEqual(actions!.x);
      if (width === 1440) expect(actions!.x + actions!.width).toBeLessThanOrEqual(width);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({path: `/tmp/cm-uiux08-20261111/task5-capacity-${capacity}-${width}.png`});
    }
    await page.setViewportSize({width:1440,height:900});
    const actions = row.getByRole('button',{name:`Actions: ${pod.pod_name}`,exact:true});
    await actions.press('Enter');
    await expect(page.getByRole('menu',{name:`Actions: ${pod.pod_name}`,exact:true})).toBeVisible();
    await expect(page.getByRole('heading',{name:pod.pod_name,exact:true})).toBeHidden();
    await page.keyboard.press('Escape');
    await expect(actions).toBeFocused();
    await row.getByRole('button',{name:pod.pod_name,exact:true}).press('Enter');
    await expect(page.getByRole('heading',{name:pod.pod_name,exact:true})).toBeVisible();
    await expect(page.getByRole('button',{name:/^Slot \d+ · Idle$/})).toHaveCount(Math.min(capacity,64));
    const statusFilter = page.getByRole('button',{name:'Instance state: All',exact:true});
    await statusFilter.click();
    await expect(page.getByRole('listbox',{name:'Instance state',exact:true})).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(statusFilter).toBeFocused();
  });
}

for (const scenario of [{name:'no-inventory',capacity:8,agent_reported:true,slots:[]}, {name:'unreported',capacity:16,agent_reported:false,slots:[]}, {name:'offline',state:'unhealthy',capacity:16,agent_reported:true,slots:[]}, {name:'unknown',capacity:64,agent_reported:true,slots:[{index:0,ready:true,container_name:'slot-0',reconciliation_state:'unknown'}]}, {name:'missing-observations',capacity:8,agent_reported:true,slots:Array.from({length:8},(_,index)=>({index,ready:false,container_name:`agent-${index}`,state:'unknown',reconciliation_state:'unknown'}))}, {name:'zero-capacity',capacity:0,agent_reported:true,slots:[]}]) {
  test(`@p1 runtime ${scenario.name} preserves unknown inventory and authoritative zero totals`, async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('clawmanager_locale','en'));
    const pod = {id:992, namespace:'reports',pod_name:scenario.name,node_name:'node',runtime_type:'hermes',state:'ready',used_slots:0,bound_instances:0,...scenario};
    await page.route('**/api/v1/admin/runtime-pods{,?*}', route => route.fulfill({json:{data:{pods:[pod],summary:{pods:1,online:0,schedulable:0,capacity:scenario.capacity,ready:0,occupied:0,bound_instances:0,available:0,running:0,starting:0,migrating:0,error:0,idle:0,unavailable:0,unknown:0},pagination:{total:1,total_pages:1},nodes:['node']}}}));
    await page.goto('/admin/runtime-pods');
    const row = page.getByRole('row').filter({hasText:scenario.name});
    await expect(row).toBeVisible();
    await expect(row.locator('.runtime-status')).toHaveText(scenario.agent_reported === false ? 'Unknown' : 'state' in scenario && scenario.state === 'unhealthy' ? 'Offline' : 'Online');
    await expect(page.locator('.runtime-metrics').getByText(`0 / ${scenario.capacity}`,{exact:true})).toHaveCount(2);
    await row.getByRole('button',{name:scenario.name,exact:true}).press('Enter');
    await expect(page.getByRole('heading',{name:scenario.name,exact:true})).toBeVisible();
    await expect(page.getByText(`Unknown / ${scenario.capacity}`,{exact:true})).toBeVisible();
    await expect(page.getByRole('button',{name:'Back to overview',exact:true})).toBeVisible();
  });
}

test('@p1 runtime maintenance popup stays above native dialog and returns focus', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('clawmanager_locale','en'));
  await page.route('**/api/v1/admin/runtime-pods/scale-options', route => route.fulfill({json:{data:{items:[{runtime_type:'hermes',replicas:2,capacity:16}],unavailable:[],limits:{min_pods:1,max_pods:10}}}}));
  await page.goto('/admin/runtime-pods');
  const maintenance = page.getByRole('button',{name:'Pod maintenance',exact:true});
  await maintenance.click();
  const dialog = page.getByRole('dialog',{name:'Pod maintenance',exact:true});
  const runtimeType = dialog.getByRole('button',{name:/^Runtime type:/});
  await runtimeType.click();
  await expect(dialog.getByRole('listbox',{name:'Runtime type',exact:true})).toBeVisible();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  await expect(runtimeType).toHaveAccessibleName('Runtime type: Hermes');
  await expect(runtimeType).toBeFocused();
  await runtimeType.click();
  await page.keyboard.press('Escape');
  await expect(runtimeType).toBeFocused();
  await expect(dialog).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(maintenance).toBeFocused();
});

for (const ready of [false,true]) {
  test(`@p1 runtime reported physical health ${ready} survives unknown binding`, async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('clawmanager_locale','en'));
    const pod = {id:993,namespace:'health',pod_name:'reported-health',runtime_type:'hermes',state:'ready',capacity:1,used_slots:0,bound_instances:0,agent_reported:true,bindings_reported:false,slots:[{index:1,container_name:'agent-1',state:ready?'running':'waiting',ready,reconciliation_state:'unknown'}]};
    await page.route('**/api/v1/admin/runtime-pods{,?*}',route=>route.fulfill({json:{data:{pods:[pod],summary:{pods:1,online:1,capacity:1,ready:0,available:0,bound_instances:0},pagination:{total:1,total_pages:1},nodes:[]}}}));
    await page.goto('/admin/runtime-pods');
    await page.getByRole('button',{name:pod.pod_name,exact:true}).click();
    const health = page.locator('.cm-surface').filter({has:page.getByText('Agents ready',{exact:true})});
    await expect(health).toContainText(`${ready ? 1 : 0} / 1`);
    await expect(health).not.toContainText('Unknown / 1');
  });
}

test('@p1 runtime missing bound summary never substitutes occupancy', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('clawmanager_locale','en'));
  const pod={id:995,namespace:'summary',pod_name:'unknown-bindings',runtime_type:'hermes',state:'ready',capacity:8,used_slots:3,agent_reported:true,slots:[]};
  await page.route('**/api/v1/admin/runtime-pods{,?*}',route=>route.fulfill({json:{data:{pods:[pod],summary:{pods:1,online:1,capacity:8,ready:0,occupied:3,available:0},pagination:{total:1,total_pages:1},nodes:[]}}}));
  await page.goto('/admin/runtime-pods');
  const bound=page.locator('.runtime-metric').filter({has:page.getByText('Bound',{exact:true})});
  await expect(bound).toContainText('Unknown / 8');
  await expect(bound).toContainText('Instance occupancy 3');
  await expect(page.getByRole('row').filter({hasText:pod.pod_name})).toContainText('Bound Unknown / 8');
});

test('@p1 runtime overview retains every slot state with full detail entry', async ({page}) => {
  await page.addInitScript(() => localStorage.setItem('clawmanager_locale','en'));
  const states=['running','starting','migrating','error','idle','releasing','unavailable','unknown'];
  const pod={id:996,namespace:'states',pod_name:'all-slot-states',runtime_type:'hermes',state:'ready',capacity:8,used_slots:4,bound_instances:4,agent_reported:true,bindings_reported:true,slots:states.map((state,index)=>({index:index+1,container_name:`agent-${index+1}`,ready:state!=='unavailable',state:state==='unavailable'?'waiting':'running',...(index<4?{instance_id:index+1,instance_state:state}:{}),...(['releasing','unknown'].includes(state)?{reconciliation_state:state}:{})}))};
  await page.route('**/api/v1/admin/runtime-pods{,?*}',route=>route.fulfill({json:{data:{pods:[pod],summary:{pods:1,online:1,capacity:8,ready:7,bound_instances:4,available:1},pagination:{total:1,total_pages:1},nodes:[]}}}));
  await page.goto('/admin/runtime-pods');
  const distribution=page.getByRole('img',{name:/Slot distribution/});
  for (const state of ['Running','Starting','Migrating','Instance error','Idle','Pending release','Unavailable','Unknown']) await expect(distribution).toHaveAccessibleName(new RegExp(`${state} 1`));
  await page.getByRole('button',{name:pod.pod_name,exact:true}).click();
  await expect(page.getByRole('button',{name:/^Slot \d+ · /})).toHaveCount(8);
  await expect(page.getByRole('button',{name:'Slot 3 · Migrating',exact:true})).toBeVisible();
  await page.setViewportSize({width:390,height:844});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});

test('@p1 shared shell keeps settings, resources, skill hub, admin and instance object context', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('clawmanager_locale', 'en'));
  for (const route of ['/settings', '/openclaw-configs', '/skill-hub', '/admin']) {
    await page.goto(route);
    await expect(page.locator('.cm-management-shell')).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Current page', exact: true })).toBeVisible();
    await expect(page.getByRole('main')).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`${route}$`));
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await expect(page.locator('.cm-management-header')).toBeInViewport();
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1);
    }
  }
  // Only instance metadata is synthetic; real auth/adjacent routes use the isolated backend.
  const instance = { id: 99229, name: 'Compatibility workspace', type: 'hermes', instance_mode: 'lite', runtime_type: 'gateway', status: 'stopped', user_id: 1, cpu_cores: 2, memory_gb: 4, disk_gb: 20, gpu_enabled: false, gpu_count: 0, os_type: 'linux', os_version: 'ubuntu', storage_class: 'standard', mount_path: '/workspace', created_at: '2026-11-11T08:00:00Z', updated_at: '2026-11-11T08:00:00Z' };
  await page.route('**/api/v1/instances/99229{,/**}', route => {
    const pathname = new URL(route.request().url()).pathname;
    if (!['/99229', '/status', '/runtime', '/external-access', '/skills', '/skills/available'].some(suffix => pathname.endsWith(suffix))) return route.fulfill({ status: 404, json: { error: 'Optional fixture endpoint unavailable' } });
    const data = pathname.includes('/skills') ? [] : pathname.endsWith('/status') ? { instance_status: { instance_id: instance.id, status: 'stopped', availability: 'unavailable', created_at: instance.created_at } }
      : pathname.endsWith('/runtime') ? { commands: [], skills: [] }
      : pathname.endsWith('/external-access') ? { external_access: null }
      : pathname.endsWith('/99229') ? { instance } : {};
    return route.fulfill({ json: { success: true, data } });
  });
  await page.goto('/instances/99229');
  await expect(page.getByRole('heading', { name: 'Compatibility workspace', exact: true })).toBeVisible();
  for (const width of [1440, 1280, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.locator('.cm-management-header')).toBeInViewport();
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1);
  }
});


test('@p1 account omits Portal while keeping Console, language, focus and version in both shells', async ({ page }) => {
  // This shell behavior does not require a production account or resource mutation.
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname;
    const data = path.endsWith('/auth/me') ? { id: 701, username: 'shell-fixture', role: 'admin', is_active: true }
      : path.endsWith('/version') ? { version: 'shell-fixture', commit: 'fixture', build_time: 'fixture' }
      : path.endsWith('/instances') ? { instances: [], total: 0 }
      : path.endsWith('/admin/runtime-pods') ? { pods: [], summary: { pods: 0, online: 0, capacity: 0, ready: 0, available: 0, bound_instances: 0 }, pagination: { total: 0, total_pages: 1 }, nodes: [] }
      : [];
    return route.fulfill({ json: { success: true, data } });
  });
  await page.addInitScript(() => localStorage.setItem('clawmanager_locale', 'en'));
  await page.goto('/instances');
  await expect(page.getByTestId('clawmanager-version')).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Main navigation', exact: true }).getByRole('link', { name: 'Portal', exact: true })).toHaveCount(0);
  await page.getByLabel('Account', { exact: true }).click();
  await expect(page.getByRole('link', { name: 'Portal', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Console', exact: true })).toBeVisible();
  const language = page.getByRole('button', { name: 'Language: English', exact: true });
  await language.click();
  await page.keyboard.press('Escape');
  await expect(language).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.getByLabel('Account', { exact: true })).toBeFocused();
  await page.goto('/admin/runtime-pods');
  await expect(page.getByTestId('clawmanager-version')).toBeVisible();
  await page.getByLabel('Account', { exact: true }).click();
  await expect(page.getByRole('link', { name: 'Portal', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Workspace', exact: true }).last()).toBeVisible();
  await page.getByRole('button', { name: 'Language: English', exact: true }).click();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: 'Language: English', exact: true })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.getByLabel('Account', { exact: true })).toBeFocused();
});
