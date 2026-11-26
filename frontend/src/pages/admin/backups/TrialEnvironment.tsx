import { useI18n } from '../../../contexts/I18nContext';
import { downloadTrialJson } from './trialRestoreHandoff';
import type { TrialWorkspaceSource } from './TrialWorkspacePicker';
import TrialReadinessDetails, { type ReadinessDetails } from './TrialReadinessDetails';

type Placement = {
  cluster: string;
  namespace: string;
  image: string;
  service_account: string;
  artifact_claim: string;
  artifact_storage_class?: string;
  workspace_claim?: string;
  workspace_storage_class?: string;
  instance_storage_class?: string;
  key_secret: string;
  source_read_secret?: string;
  restore_write_secret?: string;
  restore_read_secret?: string;
  runtime_secret?: string;
  hermes_history?: { user_id: number; instance_id: number; image: string; script_configmap: string; run_as_user: number };
  workspace_files?: TrialWorkspaceSource;
};

type PlacementReference = Exclude<keyof Placement, 'hermes_history' | 'workspace_files'>;

export type TrialEnvironmentStatus = {
  state: 'unregistered' | 'invalid' | 'registered' | 'waiting' | 'running' | 'prepared' | 'failed' | 'unavailable';
  profile?: {
    revision: string;
    side: 'source' | 'target';
    source: Placement;
    target: Placement;
    prepare_job?: { name: string; uid: string };
  };
};

export type TrialReadiness = {
  missing: string[] | null;
  restore_missing: string[] | null;
  environment?: TrialEnvironmentStatus;
  import_available?: boolean;
  details?: ReadinessDetails;
};

type Props = {
  environment?: TrialEnvironmentStatus;
  readiness?: TrialReadiness;
  checking: boolean;
  disabled: boolean;
  checkDisabled: boolean;
  preparing: boolean;
  onCheck: () => void;
  onPrepare: () => void;
  label: (code: string) => string;
};

export default function TrialEnvironment({
  environment, readiness, checking, disabled, checkDisabled, preparing, onCheck, onPrepare, label,
}: Props) {
  const { locale } = useI18n();
  const zh = locale.startsWith('zh');
  const profile = environment?.profile;
  const state = environment?.state;
  const details = readiness?.details?.side === profile?.side ? readiness?.details : undefined;
  const statuses: Record<TrialEnvironmentStatus['state'], string> = zh ? {
    unregistered: '平台尚未登记试行配置',
    invalid: '登记的试行配置无效，请联系平台管理员核对',
    registered: '已登记配置，尚未绑定准备任务',
    waiting: '准备任务可启动',
    running: '正在准备试行环境',
    prepared: '准备任务已完成',
    failed: '准备任务失败，请联系平台管理员处理后重新登记',
    unavailable: '无法读取或启动已登记的准备任务',
  } : {
    unregistered: 'The platform has not registered trial configuration',
    invalid: 'The registered trial configuration is invalid. Contact the platform administrator.',
    registered: 'Configuration is registered. No preparation job is assigned.',
    waiting: 'Ready to run the preparation job',
    running: 'Preparing the trial environment',
    prepared: 'Preparation job completed',
    failed: 'The preparation job failed. Ask the platform administrator to resolve it and register a replacement.',
    unavailable: 'Cannot read or start the registered preparation job',
  };
  const fieldLabels: Record<PlacementReference, string> = zh ? {
    cluster: '集群', namespace: '命名空间', image: '镜像', service_account: '服务账号',
    artifact_claim: '产物 PVC', artifact_storage_class: '产物存储类', workspace_claim: '工作区 PVC',
    workspace_storage_class: '工作区存储类', instance_storage_class: '实例存储类',
    key_secret: '加密密钥 Secret', source_read_secret: '源只读 Secret', restore_write_secret: '恢复写入 Secret',
    restore_read_secret: '独立复核 Secret', runtime_secret: '运行账号 Secret',
  } : {
    cluster: 'Cluster', namespace: 'Namespace', image: 'Image', service_account: 'Service account',
    artifact_claim: 'Artifact PVC', artifact_storage_class: 'Artifact storage class', workspace_claim: 'Workspace PVC',
    workspace_storage_class: 'Workspace storage class', instance_storage_class: 'Instance storage class',
    key_secret: 'Encryption key Secret', source_read_secret: 'Source reader Secret', restore_write_secret: 'Restore writer Secret',
    restore_read_secret: 'Independent verifier Secret', runtime_secret: 'Runtime account Secret',
  };

  function downloadCheck() {
    if (!profile || !readiness || !state) return;
    downloadTrialJson(`backup-trial-check-${profile.side}-${profile.revision}.json`, {
      kind: 'clawmanager.trial-environment-check.v1',
      profile,
      local_check: {
        side: profile.side,
        preparation_state: state,
        missing: (profile.side === 'source' ? readiness.missing : readiness.restore_missing) ?? [],
        ...(details && { checks: details.checks }),
      },
    });
  }

  const showSourceCheck = !!readiness && profile?.side !== 'target';
  const showTargetCheck = !!readiness && profile?.side !== 'source';
  const sourceMissing = readiness?.missing ?? [];
  const targetMissing = readiness?.restore_missing ?? [];
  const button = 'app-button-secondary max-md:min-h-11';

  return <div className="space-y-2 rounded-[var(--cm-radius-panel)] border border-[var(--cm-border-subtle)] p-3" aria-labelledby="trial-environment-title">
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <h3 id="trial-environment-title" className="text-sm font-medium text-[var(--cm-ink)]">{zh ? '试行环境配置' : 'Trial environment configuration'}</h3>
      {profile && <span className="text-sm text-[var(--cm-muted)]">{zh ? '版本 ' : 'Revision '}{profile.revision}</span>}
    </div>
    {(state || checking) && <p role="status" className={state === 'failed' || state === 'invalid' || state === 'unavailable' ? 'text-sm text-[var(--cm-warning)]' : 'text-sm text-[var(--cm-text)]'}>
      {checking ? (zh ? '正在检查当前环境…' : 'Checking the current environment…')
        : preparing ? (zh ? '正在启动准备任务…' : 'Starting preparation…')
          : state && statuses[state]}
    </p>}
    <div className="flex flex-wrap gap-2">
      <button type="button" className={button} disabled={checkDisabled || checking} onClick={onCheck}>
        {zh ? '检查试行就绪状态' : 'Check trial readiness'}
      </button>
      {profile?.prepare_job && <button type="button" className={button}
        disabled={disabled || preparing || state !== 'waiting'} onClick={onPrepare}>
        {zh ? '准备试行环境' : 'Prepare trial environment'}
      </button>}
      {profile && <button type="button" className={button}
        onClick={() => downloadTrialJson(`backup-trial-${profile.side}-${profile.revision}.json`, profile)}>
        {zh ? '下载配置摘要' : 'Download configuration summary'}
      </button>}
      {profile && readiness && <button type="button" className={button} onClick={downloadCheck}>
        {zh ? '下载环境检查报告' : 'Download environment check report'}
      </button>}
    </div>
    <div aria-live="polite" className="space-y-1 text-sm">
      {showSourceCheck && (sourceMissing.length
        ? <p className="text-[var(--cm-warning)]">{zh ? '备份缺项 · ' : 'Backup requires · '}{sourceMissing.map(label).join(' · ')}</p>
        : <p className="text-[var(--cm-success)]">{zh ? '源库身份、基础表结构和备份存储已校验' : 'Source database identity, required tables, and backup storage verified'}</p>)}
      {showTargetCheck && (targetMissing.length
        ? <p className="text-[var(--cm-warning)]">{zh ? '恢复缺项 · ' : 'Restore requires · '}{targetMissing.map(label).join(' · ')}</p>
        : <p className="text-[var(--cm-success)]">{zh ? '目标库身份、基础表结构和导入卷已校验' : 'Target database identity, required tables, and import mount verified'}</p>)}
    </div>
    {details && <TrialReadinessDetails details={details} label={label} zh={zh} />}
    {profile && <>
      <details>
        <summary className="cursor-pointer text-sm text-[var(--cm-ink)]">{zh ? '查看源和目标配置' : 'View source and target configuration'}</summary>
        <div className="mt-2 grid gap-3 lg:grid-cols-2">
          {(['source', 'target'] as const).map((side) => <div key={side} className="min-w-0">
            <h4 className="text-sm font-medium text-[var(--cm-ink)]">{side === 'source' ? (zh ? '备份源' : 'Backup source') : (zh ? '隔离目标' : 'Isolated target')}
              {profile.side === side && (zh ? '（当前）' : ' (current)')}</h4>
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 py-1 text-sm text-[var(--cm-text)]">
              {(Object.keys(fieldLabels) as PlacementReference[]).filter((key) => profile[side][key]).map((key) =>
                <div className="contents" key={key}><dt>{fieldLabels[key]}</dt><dd className="break-all font-mono">{profile[side][key]}</dd></div>)}
              {profile[side].hermes_history && <>
                <dt>{zh ? 'Hermes 采集实例' : 'Hermes capture instance'}</dt>
                <dd>#{profile[side].hermes_history.instance_id} · {zh ? '归属账号' : 'Owner'} #{profile[side].hermes_history.user_id}</dd>
                <dt>{zh ? '采集镜像' : 'Capture image'}</dt>
                <dd className="break-all font-mono">{profile[side].hermes_history.image}</dd>
                <dt>{zh ? '采集脚本 ConfigMap' : 'Capture script ConfigMap'}</dt>
                <dd className="break-all font-mono">{profile[side].hermes_history.script_configmap}</dd>
                <dt>{zh ? '采集 UID' : 'Capture UID'}</dt><dd>{profile[side].hermes_history.run_as_user}</dd>
              </>}
              {profile[side].workspace_files && <>
                <dt>{zh ? '工作区采集实例' : 'Workspace capture instance'}</dt>
                <dd>#{profile[side].workspace_files.instance_id} · {profile[side].workspace_files.runtime}
                  {' · '}{zh ? '归属账号' : 'Owner'} #{profile[side].workspace_files.user_id}</dd>
                <dt>{zh ? '可备份目录范围' : 'Backup directory scope'}</dt>
                <dd className="break-all font-mono">{profile[side].workspace_files.relative_root}</dd>
              </>}
            </dl>
          </div>)}
        </div>
      </details>
    </>}
  </div>;
}
