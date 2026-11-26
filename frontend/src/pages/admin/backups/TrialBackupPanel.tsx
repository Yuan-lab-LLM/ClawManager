import { useEffect, useRef, useState } from 'react';
import { isAxiosError } from 'axios';
import { useI18n } from '../../../contexts/I18nContext';
import api from '../../../services/api';
import TrialEnvironment, { type TrialEnvironmentStatus, type TrialReadiness as Readiness } from './TrialEnvironment';
import TrialBackupScope, { type TrialBackupSelection } from './TrialBackupScope';
import {
  createTrialHandoff, downloadTrialBlob, downloadTrialJson, isTrialBackupID, matchesTrialHandoff, readTrialHandoff, type TrialRestoreHandoff,
} from './trialRestoreHandoff';

const route = '/admin/system-backup/trial';
type Task = {
  id: string;
  backup_id?: string;
  status: 'running' | 'succeeded' | 'failed';
  stage: string;
  error?: string;
  readback_verified: boolean;
  backup_mode?: 'all' | 'selected';
  ciphertext_sha256?: string;
  counts?: Record<string, number>;
  hermes_history?: { sessions: number; messages: number; transcript_sha256: string };
  workspace_files?: { entries: number; file_bytes: number; archive_sha256: string };
};
type RestorePreflight = Pick<Task, 'readback_verified' | 'ciphertext_sha256' | 'counts' | 'hermes_history' | 'workspace_files'> & {
  workspace_directory?: { user_id: number; instance_id: number; relative_dir: string };
  backup_id: string;
  missing?: string;
};

const mountReadinessCodes = new Set(['source_credential', 'restore_credential', 'restore_read_credential', 'encryption_key']);

function waitingForProjectedMaterials(ready?: Readiness) {
  const environment = ready?.environment;
  if (environment?.state !== 'prepared' || !environment.profile) return false;
  const missing = environment.profile.side === 'source' ? ready?.missing : ready?.restore_missing;
  return missing?.some((item) => mountReadinessCodes.has(item)) ?? false;
}

export default function TrialBackupPanel() {
  const { locale } = useI18n();
  const zh = locale.startsWith('zh');
  const [ready, setReady] = useState<Readiness>();
  const [task, setTask] = useState<Task>();
  const [backupID, setBackupID] = useState('');
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);
  const [preflight, setPreflight] = useState<RestorePreflight>();
  const [checkingEnvironment, setCheckingEnvironment] = useState(true);
  const [checkingBackup, setCheckingBackup] = useState(false);
  const [preparing, setPreparing] = useState(false);
  const [revision, setRevision] = useState(0);
  const [handoff, setHandoff] = useState<TrialRestoreHandoff>();
  const [handoffError, setHandoffError] = useState<'invalid' | 'placement'>();
  const [readingHandoff, setReadingHandoff] = useState(false);
  const [transferring, setTransferring] = useState<'download' | 'upload'>();
  const [importState, setImportState] = useState<'received' | 'invalid' | 'conflict' | 'unavailable'>();
  const [scopeOpen, setScopeOpen] = useState(false);
  const deliveryInput = useRef<HTMLInputElement>(null);
  const profile = ready?.environment?.profile;
  const mayAutofillBackup = !!ready && profile?.side !== 'target';
  const running = task?.status === 'running';
  const preparationRunning = ready?.environment?.state === 'running';
  const materialsPending = waitingForProjectedMaterials(ready);
  const actionPending = running || busy || checkingBackup || preparing || readingHandoff || !!transferring;
  const canStartBackup = !!ready && !ready.missing?.length && !actionPending;
  const handoffPlacementDiffers = !!handoff && !!ready && (profile?.side !== 'target' || !matchesTrialHandoff(handoff, profile));
  const canCheck = !!ready && !ready.restore_missing?.length && !actionPending && !handoffPlacementDiffers && isTrialBackupID(backupID);
  const matchingPreflight = preflight?.backup_id === backupID ? preflight : undefined;
  const handoffDigestDiffers = !!handoff && matchingPreflight?.readback_verified === true &&
    matchingPreflight.ciphertext_sha256 !== handoff.ciphertext_sha256;
  const canRestore = canCheck && matchingPreflight?.readback_verified && !matchingPreflight.missing && !handoffDigestDiffers;
  const exportHandoff = profile?.side === 'source' && task?.status === 'succeeded' && task.readback_verified && task.id === task.backup_id
    ? createTrialHandoff(profile, task.backup_id, task.ciphertext_sha256) : undefined;

  useEffect(() => {
    const controller = new AbortController();
    api.get<Readiness>(route + '/readiness', { signal: controller.signal })
      .then(({ data }) => { if (!controller.signal.aborted) setReady(data); })
      .catch(() => { if (!controller.signal.aborted) setError(true); })
      .finally(() => { if (!controller.signal.aborted) setCheckingEnvironment(false); });
    return () => controller.abort();
  }, [revision]);

  useEffect(() => {
    if (!preparationRunning && !materialsPending) return;
    const controller = new AbortController();
    // A completed Job can precede kubelet's next projected Secret refresh.
    const materialDeadline = materialsPending ? Date.now() + 120_000 : undefined;
    let timer = window.setTimeout(poll, 2500);
    async function poll() {
      try {
        const { data } = await api.get<Readiness>(route + '/readiness', { signal: controller.signal });
        if (controller.signal.aborted) return;
        setReady(data);
        if (data.environment?.state !== 'running' && !waitingForProjectedMaterials(data)) return;
      } catch {
        if (controller.signal.aborted) return;
      }
      if (!materialDeadline || Date.now() < materialDeadline) timer = window.setTimeout(poll, 2500);
    }
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [preparationRunning, materialsPending]);

  useEffect(() => {
    const controller = new AbortController();
    let timer: number | undefined;
    async function poll() {
      try {
        const { data } = await api.get<{ task: Task | null }>(route + '/task', { signal: controller.signal });
        if (controller.signal.aborted) return;
        setTask(data.task ?? undefined);
        const completed = data.task?.status === 'succeeded' ? data.task.backup_id : undefined;
        if (completed && mayAutofillBackup) setBackupID((current) => current || completed);
      } catch {
        if (!controller.signal.aborted) setError(true);
      }
      if (!controller.signal.aborted) timer = window.setTimeout(poll, 2000);
    }
    void poll();
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [revision, mayAutofillBackup]);

  function refreshReadiness() {
    setCheckingEnvironment(true);
    setReady(undefined);
    setPreflight(undefined);
    setRevision((value) => value + 1);
  }

  async function prepareEnvironment() {
    setPreparing(true);
    setError(false);
    try {
      const { data } = await api.post<TrialEnvironmentStatus>(route + '/environment/prepare');
      if (data.state === 'prepared') {
        refreshReadiness();
      } else {
        setReady((current) => current && { ...current, environment: data });
      }
    } catch {
      setError(true);
    } finally {
      setPreparing(false);
    }
  }

  async function checkBackup() {
    if (!canCheck) return;
    setCheckingBackup(true);
    setError(false);
    setPreflight(undefined);
    try {
      const { data } = await api.post<RestorePreflight>(route + '/backups/' + backupID + '/preflight');
      setPreflight(data);
    } catch {
      setError(true);
    } finally {
      setCheckingBackup(false);
    }
  }

  async function start(restore: boolean, selection?: TrialBackupSelection) {
    if (restore ? !canRestore : !canStartBackup) return;
    setBusy(true);
    setError(false);
    try {
      const suffix = restore ? '/backups/' + backupID + '/restore' : '/backups';
      const digest = restore ? matchingPreflight?.ciphertext_sha256 : undefined;
      const { data } = await api.post<Task>(route + suffix, selection,
        digest ? { params: { expected_ciphertext_sha256: digest } } : undefined);
      setTask(data);
      if (!restore) setBackupID('');
      if (selection) setScopeOpen(false);
    } catch {
      setError(true);
    } finally {
      setBusy(false);
      refreshReadiness();
    }
  }

  function clearHandoff() {
    setHandoff(undefined);
    setHandoffError(undefined);
    setBackupID('');
    setPreflight(undefined);
    setImportState(undefined);
  }

  async function importHandoff(file: File) {
    clearHandoff();
    setReadingHandoff(true);
    try {
      const selection = await readTrialHandoff(file);
      if (profile?.side !== 'target' || !matchesTrialHandoff(selection, profile)) {
        setHandoffError('placement');
        return;
      }
      setBackupID(selection.backup_id);
      setHandoff(selection);
    } catch {
      setHandoffError('invalid');
    } finally {
      setReadingHandoff(false);
    }
  }

  async function downloadDelivery() {
    if (!exportHandoff || actionPending) return;
    setTransferring('download');
    setError(false);
    try {
      const { data } = await api.get<Blob>(`${route}/backups/${exportHandoff.backup_id}/transfer`, {
        responseType: 'blob',
        params: { expected_ciphertext_sha256: exportHandoff.ciphertext_sha256 },
        timeout: 240_000,
      });
      downloadTrialBlob(`backup-trial-${exportHandoff.backup_id}.tar`, data);
    } catch {
      setError(true);
    } finally {
      setTransferring(undefined);
    }
  }

  async function uploadDelivery(file: File) {
    if (!ready?.import_available || profile?.side !== 'target' || actionPending) return;
    clearHandoff();
    setTransferring('upload');
    setError(false);
    try {
      const { data } = await api.post<TrialRestoreHandoff>(route + '/artifacts/import', file, {
        headers: { 'Content-Type': 'application/x-tar' },
        timeout: 240_000,
      });
      const verified = createTrialHandoff(profile, data.backup_id, data.ciphertext_sha256);
      if (!verified || !matchesTrialHandoff(data, profile)) {
        setImportState('invalid');
        return;
      }
      setHandoff(verified);
      setBackupID(verified.backup_id);
      setImportState('received');
    } catch (err) {
      const code = isAxiosError<{ code?: string }>(err) ? err.response?.data?.code : undefined;
      setImportState(code === 'invalid_trial_artifact_package' ? 'invalid'
        : code === 'trial_artifact_import_conflict' ? 'conflict' : 'unavailable');
    } finally {
      setTransferring(undefined);
    }
  }

  const labels: Record<string, string> = zh ? {
    prepare: '准备', capture: '采集', encrypt: '加密写入', readback: '独立读回',
    restore: '写入隔离库', verify: '恢复对账', done: '已完成',
    restore_hermes_history: '恢复 Hermes 对话', hermes_history: 'Hermes 对话无法校验或恢复',
    restore_workspace_files: '恢复工作区文件', workspace_files: '工作区文件无法校验或恢复',
    restore_workspace_instance: '目标工作区缺少实例根目录，请联系平台准备',
    backup_scope_changed: '选定范围或准备内容已变化，请重新读取后选择。',
    running: '运行中', succeeded: '成功', failed: '失败',
    source_credential: '试行源库凭据', source_isolation: '源库身份或只读授权',
    source_schema: '源库表结构', encryption_key: '试行加密密钥', storage_mounts: '专用存储挂载',
    storage_write_readback: '存储写入或独立读回', restore_isolation: '目标隔离与 MySQL 身份',
    restore_credential: '目标写入凭据', restore_read_credential: '目标只读凭据',
    restore_placement: '隔离目标的存储与命名空间', restore_database_name: '隔离库名称',
    restore_artifact_mount: '试行产物只读挂载',
    restore_artifact_configuration: '请检查导入目录和试行加密密钥',
    restore_artifact_missing: '找不到这份备份的密文或回执',
    restore_artifact_readback: '这份备份的完整性或解密校验失败',
    restore_artifact_changed: '导入密文已变化，请核对交接单后重新预检',
    restore_artifact_writable: '导入卷可写，请重新以只读方式挂载',
    trial_environment_source: '请在已登记的源环境创建备份',
    trial_environment_target: '请在已登记的隔离目标恢复',
    trial_environment_preparation: '试行环境准备尚未通过',
    restore_target_schema: '目标库缺少用户、配额或实例所需的表结构，请检查迁移',
    restore_target_database: '无法检查目标库的表结构',
    restore_database_mismatch: '写入和只读凭据指向不同的库', restore_accounts_shared: '写入和只读必须使用独立账号',
    restore_writer_database: '无法使用写入凭据连接目标库', restore_writer_isolation: '写入连接的目标库身份不匹配',
    restore_reader_database: '无法使用只读凭据连接或检查目标库', restore_reader_isolation: '只读连接身份或 SELECT 授权不符合要求',
    database_system_trust: '使用系统 CA 信任库', database_ca_invalid: '数据库 CA 文件无法读取或解析',
    restore_writer_grants: '账号需要目标库专属的 SELECT、INSERT、UPDATE、DELETE 权限',
    trial_artifact_import_unavailable: '接收器无法在导入卷写入并读回',
  } : {
    hermes_history: 'The target Hermes workspace failed its check',
    workspace_files: 'The target workspace files failed their check',
    restore_workspace_instance: 'Prepare the instance root directory on the target workspace',
    backup_scope_changed: 'The selection or prepared content changed. Reload the options and select again.',
    restore_artifact_configuration: 'Check the import directory and trial encryption key',
    restore_artifact_missing: 'The backup ciphertext or receipt is missing',
    restore_artifact_readback: 'The backup integrity or decryption check failed',
    restore_artifact_changed: 'The imported ciphertext changed. Check the handoff and run preflight again.',
    restore_artifact_writable: 'Mount the trial import volume as read-only',
    trial_environment_source: 'Create backups in the registered source environment',
    trial_environment_target: 'Restore only in the registered target environment',
    trial_environment_preparation: 'Trial environment preparation is pending',
    restore_target_schema: 'The target lacks the required user, quota, or instance schema. Check its migrations.',
    restore_target_database: 'Cannot inspect the target database schema',
    restore_database_mismatch: 'Writer and reader credentials must point to the same target database',
    restore_accounts_shared: 'Writer and reader must use separate accounts',
    restore_writer_database: 'Cannot connect to the target database with the writer credentials',
    restore_writer_isolation: 'The writer connection returned a different target identity',
    restore_reader_database: 'Cannot connect to or inspect the target database with the reader credentials',
    restore_reader_isolation: 'The reader identity or SELECT grants failed validation',
    database_system_trust: 'Uses the system CA trust store', database_ca_invalid: 'Cannot read or parse the database CA file',
    restore_writer_grants: 'The writer needs SELECT, INSERT, UPDATE, and DELETE on the target database',
    trial_artifact_import_unavailable: 'The importer could not write and verify the import volume',
  };
  const label = (code: string) => labels[code] ?? code.replaceAll('_', ' ');
  const importStatus = zh ? {
    received: '原件已到目标导入卷，请检查此备份。',
    invalid: '交付包无效，或与当前已登记的源和目标不匹配。',
    conflict: '目标已有同一备份 ID 的不同原件，请核对目标导入卷。',
    unavailable: '导入服务暂不可用，请检查试行环境后重试。',
  } : {
    received: 'The original files are on the target import volume. Run the backup check.',
    invalid: 'The package is invalid or belongs to a different registered source and target.',
    conflict: 'The target already has different files for this backup ID. Check the import volume.',
    unavailable: 'The import service is unavailable. Check trial readiness and retry.',
  };
  const button = 'app-button-secondary max-md:min-h-11';
  const disclosure = 'w-fit cursor-pointer rounded-sm py-1 font-medium text-[var(--cm-muted)] hover:text-[var(--cm-ink)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--cm-focus)] max-md:py-3';
  const importDeliveryLabel = zh ? '导入备份交付包' : 'Import backup delivery package';
  return <section className="min-w-0 space-y-4 rounded-[var(--cm-radius-panel)] border border-[var(--cm-border-subtle)] bg-[var(--cm-surface)] p-4" aria-labelledby="trial-backup-title">
    <h2 id="trial-backup-title" className="font-semibold text-[var(--cm-ink)]">{zh ? '备份（试行版）' : 'Backup (trial)'}</h2>
    <p className="text-sm text-[var(--cm-muted)]">{zh
      ? '范围包括用户、配额、实例规格和启动所需配置。恢复到预先配置的隔离目标，实例初始为停止状态。'
      : 'Includes users, quotas, instance specifications and startup configuration. Instances are restored stopped to the configured isolated target.'}</p>
    <TrialEnvironment environment={ready?.environment} readiness={ready} checking={checkingEnvironment}
      disabled={running || busy || checkingBackup || readingHandoff || !!transferring} checkDisabled={checkingBackup || preparing || readingHandoff || !!transferring}
      preparing={preparing} label={label} onCheck={() => { setError(false); refreshReadiness(); }}
      onPrepare={() => void prepareEnvironment()} />
    <div className="flex flex-wrap items-center gap-2">
      {profile?.side !== 'target' && !scopeOpen && <button type="button" className="app-button-primary max-md:min-h-11" disabled={!canStartBackup} onClick={() => void start(false)}>
        {zh ? '创建试行备份' : 'Create trial backup'}
      </button>}
      {(profile?.side === 'source' || scopeOpen) && <button type="button" className={button}
        disabled={scopeOpen ? actionPending : !canStartBackup} aria-expanded={scopeOpen}
        onClick={() => setScopeOpen((open) => !open)}>
        {scopeOpen ? (zh ? '返回快速备份' : 'Return to quick backup') : (zh ? '选择备份内容' : 'Choose backup contents')}
      </button>}
    </div>
    {scopeOpen && <TrialBackupScope disabled={!canStartBackup} onCreate={(selection) => void start(false, selection)} />}
    {error && <p role="alert" className="text-sm text-[var(--cm-error)]">{zh ? '试行请求失败，请检查就绪状态后重试。' : 'Trial request failed. Check readiness and try again.'}</p>}
    {profile?.side === 'target' && <div className="space-y-2 text-sm text-[var(--cm-text)]">
      {ready?.import_available && <>
        <button type="button" className={button} disabled={actionPending}
          onClick={() => deliveryInput.current?.click()}>
          {transferring === 'upload' ? (zh ? '正在导入交付包…' : 'Importing the delivery package…') : importDeliveryLabel}
        </button>
        <input ref={deliveryInput} type="file" accept=".tar,application/x-tar" aria-label={importDeliveryLabel} hidden
          disabled={actionPending} onChange={(event) => {
            const file = event.currentTarget.files?.[0];
            event.currentTarget.value = '';
            if (file) void uploadDelivery(file);
          }} />
      </>}
      {importState && <p role={importState === 'received' ? 'status' : 'alert'} className={importState === 'received' ? 'text-[var(--cm-success)]' : 'text-[var(--cm-error)]'}>
        {importStatus[importState]}
      </p>}
      <details open={!ready?.import_available || undefined}>
        <summary className={disclosure}>{zh ? '已有备份原件' : 'Use backup files already on the import volume'}</summary>
        <div className="space-y-2 py-1">
          <p className="break-words">{zh ? '先将 .trial 密文和 .receipt 回执放入目标导入卷 ' : 'Place the .trial ciphertext and .receipt on the target import volume '}
            <code>{profile.target.artifact_claim}</code>{zh ? '，再选择交接单。' : ', then select the handoff.'}
          </p>
          <label className="flex min-w-0 flex-wrap items-center gap-2">
            {zh ? '选择恢复交接单' : 'Select a restore handoff'}
            <input type="file" accept=".json,application/json" disabled={actionPending}
              className="max-w-full text-xs file:mr-2 file:rounded-md file:border-0 file:bg-[var(--cm-subtle)] file:px-2 file:py-1"
              onChange={(event) => {
                const file = event.currentTarget.files?.[0];
                event.currentTarget.value = '';
                if (file) void importHandoff(file);
              }} />
          </label>
        </div>
      </details>
    </div>}
    {(handoffError || handoffPlacementDiffers) && <p role="alert" className="text-sm text-[var(--cm-error)]">
      {handoffError === 'invalid'
        ? (zh ? '无法读取交接单，请选择从源端下载的 JSON 文件。' : 'Cannot read the handoff. Select the JSON file downloaded from the source.')
        : (zh ? '交接单与当前登记的源或目标不匹配。' : 'The handoff does not match the registered source or target.')}
    </p>}
    {profile?.side !== 'source' && <div className="flex flex-wrap items-end gap-2">
      <label className="flex min-w-0 flex-col gap-1 text-sm text-[var(--cm-text)]">
        {zh ? '试行备份 ID' : 'Trial backup ID'}
        <input value={backupID} maxLength={32} readOnly={!!handoff} disabled={checkingBackup || readingHandoff || !!transferring} onChange={(event) => {
          setBackupID(event.target.value.trim());
          setPreflight(undefined);
          setHandoffError(undefined);
          setImportState(undefined);
        }}
          className="app-input w-72 max-w-full font-mono max-md:min-h-11" />
      </label>
      {handoff && <button type="button" className={button} disabled={actionPending} onClick={clearHandoff}>
        {zh ? '清除备份选择' : 'Clear backup selection'}
      </button>}
      <button type="button" className={button} disabled={!canCheck} onClick={() => void checkBackup()}>
        {checkingBackup ? (zh ? '正在检查备份…' : 'Checking backup…') : (zh ? '检查此备份' : 'Check this backup')}
      </button>
      <button type="button" className="app-button-primary max-md:min-h-11" disabled={!canRestore}
        onClick={() => void start(true)}>{zh ? '恢复到隔离环境' : 'Restore to isolated target'}</button>
    </div>}
    {matchingPreflight && <div aria-live="polite" className="space-y-1 text-sm text-[var(--cm-text)]">
      <p className={matchingPreflight.missing || handoffDigestDiffers ? 'text-[var(--cm-warning)]' : 'text-[var(--cm-success)]'}>{matchingPreflight.missing
        ? label(matchingPreflight.missing)
        : handoffDigestDiffers
          ? (zh ? '目标导入的密文摘要与交接单不一致。' : 'The imported ciphertext digest differs from the handoff.')
          : (zh ? '产物读回和恢复内容预检通过' : 'Artifact readback and restore content checks passed')}</p>
      {matchingPreflight.ciphertext_sha256 && <p className="break-all">{zh ? '密文 SHA-256' : 'Ciphertext SHA-256'} <code>{matchingPreflight.ciphertext_sha256}</code></p>}
      {matchingPreflight.counts && <p>{zh
        ? `${matchingPreflight.counts.users ?? 0} 个用户 · ${matchingPreflight.counts.instances ?? 0} 个实例`
        : `${matchingPreflight.counts.users ?? 0} users · ${matchingPreflight.counts.instances ?? 0} instances`}</p>}
      {matchingPreflight.hermes_history && <p>{zh
        ? `${matchingPreflight.hermes_history.messages} 条 Hermes 消息`
        : `${matchingPreflight.hermes_history.messages} Hermes messages`}</p>}
      {matchingPreflight.workspace_files && <p>{zh
        ? `${matchingPreflight.workspace_files.entries} 项工作区内容`
        : `${matchingPreflight.workspace_files.entries} workspace entries`}</p>}
      {matchingPreflight.workspace_directory && <p className="break-all">
        {zh ? '工作区' : 'Workspace'} #{matchingPreflight.workspace_directory.instance_id} ·{' '}
        <code>{matchingPreflight.workspace_directory.relative_dir}</code>
      </p>}
    </div>}
    {task && <div role="status" className="space-y-2 text-sm text-[var(--cm-text)]">
      <p>{zh ? '任务' : 'Task'} <code>{task.id}</code> · {label(task.status)} · {label(task.stage)}{task.error && ` · ${label(task.error)}`}</p>
      {task.backup_mode === 'selected' && <p>{zh ? '范围：选定账号和实例' : 'Scope: selected accounts and instances'}</p>}
      {task.backup_id && <p>{zh ? '试行产物' : 'Trial artifact'} <code>{task.backup_id}</code></p>}
      {task.ciphertext_sha256 && <p className="break-all">{zh ? '密文 SHA-256' : 'Ciphertext SHA-256'} <code>{task.ciphertext_sha256}</code></p>}
      {exportHandoff && <div className="space-y-2">
        <button type="button" className="app-button-primary max-md:min-h-11" disabled={actionPending} onClick={() => void downloadDelivery()}>
          {transferring === 'download' ? (zh ? '正在下载交付包…' : 'Downloading delivery package…')
            : (zh ? '下载备份交付包' : 'Download backup delivery package')}
        </button>
        <details>
          <summary className={disclosure}>{zh ? '单独传输备份原件' : 'Transfer backup files separately'}</summary>
          <div className="space-y-2 py-1">
            <p className="break-all">{zh ? '请另行交付 ' : 'Deliver '}
              <code>{exportHandoff.backup_id}.trial</code>{zh ? ' 和 ' : ' and '}<code>{exportHandoff.backup_id}.receipt</code>
              {zh ? ' 到目标导入卷 ' : ' separately to the target import volume '}<code>{exportHandoff.target.artifact_claim}</code>{zh ? '。' : '.'}
            </p>
            <button type="button" className={button} disabled={actionPending}
              onClick={() => downloadTrialJson(`backup-trial-restore-${exportHandoff.backup_id}.json`, exportHandoff)}>
              {zh ? '下载恢复交接单' : 'Download restore handoff'}
            </button>
          </div>
        </details>
      </div>}
      {task.readback_verified && <p className="text-[var(--cm-success)]">{zh ? '独立读回校验通过' : 'Independent readback verified'}</p>}
      {task.hermes_history && <p>{zh
        ? `Hermes 对话 · ${task.hermes_history.sessions} 个会话 · ${task.hermes_history.messages} 条消息`
        : `Hermes history · ${task.hermes_history.sessions} sessions · ${task.hermes_history.messages} messages`}</p>}
      {task.workspace_files && <p>{zh
        ? `工作区文件 · ${task.workspace_files.entries} 项 · ${task.workspace_files.file_bytes.toLocaleString()} 字节`
        : `Workspace files · ${task.workspace_files.entries} entries · ${task.workspace_files.file_bytes.toLocaleString()} bytes`}</p>}
      {task.counts && <dl className="flex flex-wrap gap-x-5 gap-y-1">{Object.entries(task.counts).map(([table, count]) =>
        <div className="flex gap-2" key={table}><dt>{table}</dt><dd>{count}</dd></div>)}</dl>}
    </div>}
  </section>;
}
