type CheckState = 'passed' | 'failed' | 'skipped' | 'not_configured';

export type ReadinessDetails = {
  side: 'source' | 'target';
  checks: { id: string; state: CheckState; reason?: string; reference?: string; sha256?: string }[];
};

const descriptions: Record<string, [string, string]> = {
  preparation: ['试行环境准备', 'Trial environment preparation'],
  database_ca: ['数据库 CA 文件', 'Database CA file'],
  source_credential: ['源库凭据', 'Source database credentials'],
  source_read: ['源库身份、只读权限和表结构', 'Source identity, read-only grants and schema'],
  source_storage_mounts: ['源归档读写挂载', 'Source archive mounts'],
  source_storage_roundtrip: ['归档写入和独立读回', 'Archive write and independent readback'],
  source_hermes: ['Hermes 会话采集服务', 'Hermes session collector'],
  source_workspace: ['源工作区只读挂载', 'Read-only source workspace mount'],
  target_isolation: ['目标数据库隔离配置', 'Target database isolation configuration'],
  target_placement: ['目标命名空间与存储类', 'Target namespace and storage class'],
  target_writer_credential: ['目标写入凭据', 'Target writer credentials'],
  target_reader_credential: ['目标复核凭据', 'Target verifier credentials'],
  target_writer: ['目标写入账号身份', 'Target writer identity'],
  target_writer_grants: ['目标账号写入权限', 'Target writer grants'],
  target_reader: ['目标独立复核账号与只读权限', 'Target verifier identity and read-only grants'],
  target_accounts: ['写入与复核账号独立性', 'Writer and verifier separation'],
  target_schema: ['目标基础表结构', 'Target required schema'],
  encryption_key: ['试行加密密钥', 'Trial encryption key'],
  target_archive: ['目标归档只读挂载', 'Read-only target archive mount'],
  target_importer: ['交付包接收器写入和读回', 'Delivery importer write and readback'],
  target_hermes: ['Hermes 会话恢复服务', 'Hermes session restore service'],
  target_workspace: ['目标工作区无覆盖发布', 'No-overwrite target workspace publication'],
};

const statuses: Record<CheckState, [string, string]> = {
  passed: ['通过', 'Passed'], failed: ['需处理', 'Needs attention'],
  skipped: ['未执行', 'Not run'], not_configured: ['未配置', 'Not configured'],
};

export default function TrialReadinessDetails({ details, label, zh }: {
  details: ReadinessDetails;
  label: (code: string) => string;
  zh: boolean;
}) {
  const language = zh ? 0 : 1;
  return <details>
    <summary className="cursor-pointer text-sm text-[var(--cm-ink)]">{zh ? '查看当前端逐项检查' : 'View checks for this environment'}</summary>
    <dl className="mt-2 divide-y divide-[var(--cm-border-subtle)] rounded-[var(--cm-radius-panel)] border border-[var(--cm-border-subtle)] p-3 text-sm">
      {details.checks.map((check) => <div key={check.id} className="grid gap-x-3 py-2 sm:grid-cols-[minmax(0,16rem)_1fr]">
        <dt className="font-medium text-[var(--cm-ink)]">{descriptions[check.id]?.[language] ?? check.id}</dt>
        <dd className="min-w-0 space-y-1 text-[var(--cm-text)]">
          <span className={check.state === 'failed' ? 'font-medium text-[var(--cm-warning)]' : undefined}>
            {statuses[check.state]?.[language] ?? check.state}
          </span>
          {check.reason && <span> · {label(check.reason)}</span>}
          {check.reference && <p className="break-all">{check.reference}</p>}
          {check.sha256 && <p className="break-all"><span>SHA-256 </span><code>{check.sha256}</code></p>}
        </dd>
      </div>)}
    </dl>
  </details>;
}
