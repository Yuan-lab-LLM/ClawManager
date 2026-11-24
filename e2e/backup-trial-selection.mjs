const id = value => Number.isSafeInteger(value) && value > 0;
const escaped = text => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const keys = (value, required, optional = []) => value && typeof value === 'object' && !Array.isArray(value) &&
  required.every(key => Object.hasOwn(value, key)) && Object.keys(value).every(key => required.includes(key) || optional.includes(key));
const validSession = value => typeof value === 'string' && value.length > 0 && value.trim() === value &&
  Buffer.byteLength(value) <= 256 && !/[\x00-\x1f\x7f]/.test(value);
const validDirectory = value => typeof value === 'string' && Buffer.byteLength(value) <= 240 &&
  value !== 'home' && !/[\\:\x00\r\n]/.test(value) && value.split('/').every(part => part && !part.startsWith('.'));

function requireSelection(condition) {
  if (!condition) throw new Error('Invalid or unavailable trial selection');
}

export function validateSelection(input, profile) {
  requireSelection(keys(input, ['selection', 'expected_counts']));
  const selection = input.selection;
  requireSelection(keys(selection, ['mode', 'user_ids', 'instances'], ['hermes_sessions', 'workspace_directory']) &&
    selection.mode === 'selected' && Array.isArray(selection.user_ids) && Array.isArray(selection.instances) &&
    selection.user_ids.length <= 256 && selection.instances.length <= 256 &&
    selection.user_ids.length + selection.instances.length > 0 && selection.user_ids.every(id) &&
    new Set(selection.user_ids).size === selection.user_ids.length &&
    selection.instances.every(choice => keys(choice, ['id', 'user_id']) && id(choice.id) && id(choice.user_id)) &&
    new Set(selection.instances.map(choice => choice.id)).size === selection.instances.length);
  const checkBinding = (choice, registered) => registered?.instance_id === choice.instance_id &&
    registered.user_id === choice.user_id && selection.instances.some(instance =>
      instance.id === choice.instance_id && instance.user_id === choice.user_id);
  if (Object.hasOwn(selection, 'hermes_sessions')) {
    const choice = selection.hermes_sessions;
    requireSelection(keys(choice, ['user_id', 'instance_id', 'session_ids']) && checkBinding(choice, profile.source.hermes_history) &&
      Array.isArray(choice.session_ids) && choice.session_ids.length > 0 && choice.session_ids.length <= 64 &&
      choice.session_ids.every(validSession) && new Set(choice.session_ids).size === choice.session_ids.length);
  }
  if (Object.hasOwn(selection, 'workspace_directory')) {
    const choice = selection.workspace_directory;
    const registered = profile.source.workspace_files;
    requireSelection(keys(choice, ['user_id', 'instance_id', 'relative_dir']) && checkBinding(choice, registered) &&
      validDirectory(choice.relative_dir) && validDirectory(registered.relative_root) &&
      (choice.relative_dir === registered.relative_root || choice.relative_dir.startsWith(registered.relative_root + '/')));
  }
}

async function findOnPage(control, more, busy, timeout) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const remaining = Math.max(1, deadline - Date.now());
    await busy.waitFor({ state: 'hidden', timeout: remaining });
    const count = await control.count();
    if (count) {
      requireSelection(count === 1);
      return control;
    }
    requireSelection(await more.count() === 1);
    await more.click({ timeout: remaining });
  }
  throw new Error('Trial selection inventory timed out');
}

export async function chooseBackupSelection(panel, selection, profile, timeout) {
  await panel.getByRole('button', { name: /^(选择备份内容|Choose backup contents)$/ }).click({ timeout });
  const scope = panel.getByRole('group', { name: /^(备份内容|Backup contents)$/ });
  await scope.getByRole('radio', { name: /^(选择账号和实例|Select accounts and instances)$/ }).waitFor({ timeout });
  const busy = scope.getByText(/^(正在读取可选内容…|Loading available content…)$/);
  const users = scope.getByRole('heading', { name: /^(账号|Accounts)$/ }).locator('xpath=..');
  const instances = scope.getByRole('heading', { name: /^(实例|Instances)$/ }).locator('xpath=..');
  for (const user of selection.user_ids) {
    const choice = users.getByRole('checkbox', { name: new RegExp(` #${user}(?: · (?:管理员|Administrator))?$`) });
    const control = await findOnPage(choice, users.getByRole('button', { name: /^(更多账号|More accounts)$/ }), busy, timeout);
    await control.check({ timeout });
  }
  for (const instance of selection.instances) {
    const choice = instances.getByRole('checkbox', { name: new RegExp(` #${instance.id} · [^·]+ · (?:账号|Owner) #${instance.user_id}$`) });
    const control = await findOnPage(choice, instances.getByRole('button', { name: /^(更多实例|More instances)$/ }), busy, timeout);
    await control.check({ timeout });
  }
  if (selection.hermes_sessions) {
    const history = scope.locator('[aria-labelledby="trial-hermes-sessions"]');
    await history.waitFor({ timeout });
    for (const session of selection.hermes_sessions.session_ids) {
      const choice = history.getByRole('checkbox', { name: new RegExp(`(?:^| )${escaped(session)}$`) });
      const control = await findOnPage(choice, history.getByRole('button', { name: /^(更多会话|More sessions)$/ }),
        history.getByText(/^(正在读取会话…|Loading sessions…)$/), timeout);
      await control.check({ timeout });
    }
  }
  if (selection.workspace_directory) {
    const workspace = scope.locator('[aria-labelledby="trial-workspace-directory"]');
    await workspace.waitFor({ timeout });
    const root = profile.source.workspace_files.relative_root;
    const destination = selection.workspace_directory.relative_dir;
    const relative = destination === root ? [] : destination.slice(root.length + 1).split('/');
    const more = workspace.getByRole('button', { name: /^(更多目录|More directories)$/ });
    const loading = workspace.getByText(/^(正在读取目录…|Loading directories…)$/);
    let directory = root;
    for (const name of relative) {
      const control = await findOnPage(workspace.getByRole('button', { name, exact: true }), more, loading, timeout);
      await control.click({ timeout });
      directory += '/' + name;
    }
    requireSelection((await workspace.locator('code[aria-label]').textContent({ timeout })) === directory);
    await workspace.getByRole('checkbox', { name: /^(备份当前目录及子目录|Back up this directory and its subdirectories)$/ }).check({ timeout });
  }
  return scope.getByRole('button', { name: /^(创建所选备份|Create selected backup)$/ });
}
