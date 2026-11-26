import { useEffect, useState } from 'react';
import { useI18n } from '../../../contexts/I18nContext';
import api from '../../../services/api';
import TrialHermesHistoryPicker, { type TrialHermesSource } from './TrialHermesHistoryPicker';
import TrialWorkspacePicker, { type TrialWorkspaceSource } from './TrialWorkspacePicker';

type UserOption = { id: number; name: string; active_admin: boolean };
type InstanceChoice = { id: number; user_id: number };
type InstanceOption = InstanceChoice & { name: string; type: string; mode: string; owner_active_admin: boolean };
type PreparedContent = { user_id: number; instance_id: number; sha256: string; sessions?: number; relative_dir?: string };
type Options = {
  users: UserOption[];
  instances: InstanceOption[];
  next_user_id?: number;
  next_instance_id?: number;
  hermes_history?: PreparedContent;
  hermes_source?: TrialHermesSource;
  workspace_files?: PreparedContent;
  workspace_source?: TrialWorkspaceSource;
};

export type TrialBackupSelection = {
  mode: 'all' | 'selected';
  user_ids?: number[];
  instances?: InstanceChoice[];
  hermes_history_sha256?: string;
  hermes_sessions?: TrialHermesSource & { session_ids: string[] };
  workspace_selection_sha256?: string;
  workspace_directory?: { user_id: number; instance_id: number; relative_dir: string };
};

const optionsRoute = '/admin/system-backup/trial/backup-options';
const selectionLimit = 256;
const button = 'app-button-secondary max-md:min-h-11';

export default function TrialBackupScope({ disabled, onCreate }: {
  disabled: boolean;
  onCreate: (selection: TrialBackupSelection) => void;
}) {
  const { locale } = useI18n();
  const zh = locale.startsWith('zh');
  const [options, setOptions] = useState<Options>();
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [mode, setMode] = useState<TrialBackupSelection['mode']>('selected');
  const [users, setUsers] = useState<UserOption[]>([]);
  const [instances, setInstances] = useState<InstanceOption[]>([]);
  const [includeHistory, setIncludeHistory] = useState(false);
  const [historySessions, setHistorySessions] = useState<string[]>([]);
  const [includeWorkspace, setIncludeWorkspace] = useState(false);
  const [workspaceDir, setWorkspaceDir] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    api.get<Options>(optionsRoute, { signal: controller.signal })
      .then(({ data }) => { if (!controller.signal.aborted) setOptions(data); })
      .catch(() => { if (!controller.signal.aborted) setFailed(true); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);

  async function loadMore(kind: 'users' | 'instances') {
    if (!options || loading) return;
    setLoading(true);
    setFailed(false);
    try {
      const { data } = await api.get<Options>(optionsRoute, {
        params: kind === 'users' ? { after_user: options.next_user_id } : { after_instance: options.next_instance_id },
      });
      setOptions((current) => current && (kind === 'users'
        ? { ...current, users: [...current.users, ...data.users], next_user_id: data.next_user_id }
        : { ...current, instances: [...current.instances, ...data.instances], next_instance_id: data.next_instance_id }));
    } catch {
      setFailed(true);
    } finally {
      setLoading(false);
    }
  }

  const hasAdmin = users.some((user) => user.active_admin) || instances.some((instance) => instance.owner_active_admin);
  const history = options?.hermes_history;
  const historySource = options?.hermes_source;
  const workspace = options?.workspace_files;
  const workspaceSource = options?.workspace_source;
  const inScope = (item?: TrialHermesSource) => !!item && (mode === 'all' ||
    instances.some((instance) => instance.id === item.instance_id && instance.user_id === item.user_id));
  const ready = !!options && !disabled && !loading && (mode === 'all' || hasAdmin);
  const usersTotal = new Set([...users.map((user) => user.id), ...instances.map((instance) => instance.user_id)]).size;

  function create() {
    if (!ready) return;
    onCreate({
      mode,
      ...(mode === 'selected' && {
        user_ids: users.map((user) => user.id),
        instances: instances.map(({ id, user_id }) => ({ id, user_id })),
      }),
      ...(includeHistory && inScope(history) && { hermes_history_sha256: history?.sha256 }),
      ...(historySource && inScope(historySource) && historySessions.length > 0 && {
        hermes_sessions: { ...historySource, session_ids: historySessions },
      }),
      ...(includeWorkspace && inScope(workspace) && { workspace_selection_sha256: workspace?.sha256 }),
      ...(workspaceSource && inScope(workspaceSource) && workspaceDir && { workspace_directory: {
        user_id: workspaceSource.user_id, instance_id: workspaceSource.instance_id, relative_dir: workspaceDir,
      } }),
    });
  }

  return <fieldset disabled={disabled} className="space-y-3 rounded-[var(--cm-radius-panel)] border border-[var(--cm-border-subtle)] p-3 text-sm text-[var(--cm-text)]">
    <legend className="px-1 font-medium">{zh ? '备份内容' : 'Backup contents'}</legend>
    {loading && <p role="status">{zh ? '正在读取可选内容…' : 'Loading available content…'}</p>}
    {failed && <div className="flex flex-wrap items-center gap-2">
      <p role="alert">{zh ? '无法读取可选内容。' : 'Cannot load available content.'}</p>
      {!options && <button type="button" className={button} onClick={() => {
        setFailed(false);
        setLoading(true);
        setRevision((value) => value + 1);
      }}>{zh ? '重试' : 'Retry'}</button>}
    </div>}
    {options && <>
      <label className="mr-4 inline-flex items-center gap-1">
        <input type="radio" name="trial-backup-scope" checked={mode === 'selected'} onChange={() => setMode('selected')} />
        {zh ? '选择账号和实例' : 'Select accounts and instances'}
      </label>
      <label className="inline-flex items-center gap-1">
        <input type="radio" name="trial-backup-scope" checked={mode === 'all'} onChange={() => setMode('all')} />
        {zh ? '全部账号和实例' : 'All accounts and instances'}
      </label>
      {mode === 'selected' && <>
        <p>{zh ? '选择实例会一并备份归属账号和配额。选定范围需包含一个启用的管理员。'
          : 'Selecting an instance includes its owner account and quota. Include an active administrator in the selection.'}</p>
        <div className="grid gap-3 md:grid-cols-2">
          <div className="min-w-0 space-y-1">
            <h4 className="font-medium">{zh ? '账号' : 'Accounts'}</h4>
            <div className="max-h-60 space-y-1 overflow-y-auto">
              {options.users.map((user) => {
                const checked = users.some((item) => item.id === user.id);
                return <label key={user.id} className="flex items-start gap-2">
                  <input type="checkbox" checked={checked} disabled={!checked && users.length >= selectionLimit} onChange={() => setUsers(
                    (current) => checked ? current.filter((item) => item.id !== user.id) : [...current, user]
                  )} />
                  <span className="break-all">{user.name} #{user.id}{user.active_admin && (zh ? ' · 管理员' : ' · Administrator')}</span>
                </label>;
              })}
            </div>
            {options.next_user_id && <button type="button" className={button} disabled={loading} onClick={() => void loadMore('users')}>
              {zh ? '更多账号' : 'More accounts'}
            </button>}
          </div>
          <div className="min-w-0 space-y-1">
            <h4 className="font-medium">{zh ? '实例' : 'Instances'}</h4>
            <div className="max-h-60 space-y-1 overflow-y-auto">
              {options.instances.map((instance) => {
                const checked = instances.some((item) => item.id === instance.id);
                return <label key={instance.id} className="flex items-start gap-2">
                  <input type="checkbox" checked={checked} disabled={!checked && instances.length >= selectionLimit} onChange={() => setInstances(
                    (current) => checked ? current.filter((item) => item.id !== instance.id) : [...current, instance]
                  )} />
                  <span className="break-all">{instance.name} #{instance.id} · {instance.type} {instance.mode} · {zh ? '账号' : 'Owner'} #{instance.user_id}</span>
                </label>;
              })}
            </div>
            {options.next_instance_id && <button type="button" className={button} disabled={loading} onClick={() => void loadMore('instances')}>
              {zh ? '更多实例' : 'More instances'}
            </button>}
          </div>
        </div>
        <p role="status">{zh ? `已包括 ${usersTotal} 个账号和 ${instances.length} 个实例` : `Including ${usersTotal} accounts and ${instances.length} instances`}</p>
      </>}
      {history && <label className="flex items-start gap-2">
        <input type="checkbox" checked={includeHistory && inScope(history)} disabled={!inScope(history)}
          onChange={(event) => {
            setIncludeHistory(event.target.checked);
            if (event.target.checked) setHistorySessions([]);
          }} />
        <span>{zh ? `已准备的 Hermes 会话 · 实例 #${history.instance_id} · ${history.sessions} 场`
          : `Prepared Hermes history · Instance #${history.instance_id} · ${history.sessions} sessions`}</span>
      </label>}
      {historySource && (inScope(historySource)
        ? <TrialHermesHistoryPicker key={`${historySource.user_id}/${historySource.instance_id}`} zh={zh} source={historySource}
            selected={historySessions} disabled={includeHistory && inScope(history)} onChange={(ids) => {
              setIncludeHistory(false);
              setHistorySessions(ids);
            }} />
        : <p>{zh ? `勾选实例 #${historySource.instance_id} 后可选择 Hermes 会话。`
          : `Include instance #${historySource.instance_id} to select its Hermes sessions.`}</p>)}
      {workspace && <label className="flex items-start gap-2">
        <input type="checkbox" checked={includeWorkspace && inScope(workspace)} disabled={!inScope(workspace)}
          onChange={(event) => {
            setIncludeWorkspace(event.target.checked);
            if (event.target.checked) setWorkspaceDir('');
          }} />
        <span className="break-all">{zh ? `已准备的工作区目录 · 实例 #${workspace.instance_id}`
          : `Prepared workspace directory · Instance #${workspace.instance_id}`} · {workspace.relative_dir}</span>
      </label>}
      {workspaceSource && (inScope(workspaceSource)
        ? <TrialWorkspacePicker key={`${workspaceSource.user_id}/${workspaceSource.instance_id}/${workspaceSource.relative_root}`}
            source={workspaceSource} selected={workspaceDir} disabled={includeWorkspace && inScope(workspace)} zh={zh}
            onChange={(dir) => { setIncludeWorkspace(false); setWorkspaceDir(dir); }} />
        : <p>{zh ? `勾选实例 #${workspaceSource.instance_id} 后可选择工作区目录。`
          : `Include instance #${workspaceSource.instance_id} to select a workspace directory.`}</p>)}
      <button type="button" className="app-button-primary max-md:min-h-11" disabled={!ready} onClick={create}>
        {zh ? '创建所选备份' : 'Create selected backup'}
      </button>
    </>}
  </fieldset>;
}
