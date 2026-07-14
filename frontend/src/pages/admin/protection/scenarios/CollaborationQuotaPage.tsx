import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import ApplyDispatchButton from '../../../../components/secplane/ApplyDispatchButton';
import { secplaneService, type SecplaneAlert } from '../../../../services/secplaneService';
import { useInstanceHealth } from '../../secplane/runtime/useInstanceHealth';

type Tone = 'red' | 'orange' | 'blue' | 'green' | 'purple' | 'slate';
type RuleMode = 'enforce' | 'observe' | 'off';
type Period = 'daily' | 'weekly' | 'monthly';
type SeverityFilter = 'all' | 'warn' | 'critical';

interface QuotaRow {
  id: string;
  scope: 'instance' | 'team';
  target: string;
  mode: RuleMode;
  daily: number;
  weekly: number;
  monthly: number;
}

interface UsageSnapshot {
  quotaId: string;
  dailyUsed: number;
  weeklyUsed: number;
  monthlyUsed: number;
}

interface QuotaPolicyState {
  gatewayMode: RuleMode;
  warnAtPercent: number;
  preferredPeriod: Period;
  rows: QuotaRow[];
  updatedAt: string | null;
}

interface DispatchRecord {
  id: string;
  revision: string;
  ts: string;
  targetScope: string;
  rowCount: number;
  successCount: number;
  failedCount: number;
}

interface QuotaAlertRow {
  id: string;
  ts: string;
  severity: 'warn' | 'critical';
  scope: 'instance' | 'team' | 'platform';
  target: string;
  period: Period | 'platform';
  usage: string;
  action: string;
  detail: string;
}

const STORAGE_KEY = 'secplane.collaboration.quota-policy.v1';
const STORAGE_DISPATCH_KEY = 'secplane.collaboration.quota-dispatch.v1';
const TABS = ['策略配置', '策略下发', '日志告警'] as const;

const DEFAULT_ROWS: QuotaRow[] = [
  { id: 'inst-1', scope: 'instance', target: 'openclaw-prod-east-12', mode: 'enforce', daily: 180000, weekly: 900000, monthly: 3600000 },
  { id: 'inst-2', scope: 'instance', target: 'openclaw-research-07', mode: 'observe', daily: 240000, weekly: 1200000, monthly: 4800000 },
  { id: 'team-1', scope: 'team', target: 'team-alpha', mode: 'enforce', daily: 600000, weekly: 3000000, monthly: 12000000 },
  { id: 'team-2', scope: 'team', target: 'team-red', mode: 'observe', daily: 480000, weekly: 2400000, monthly: 9600000 },
];

const DEFAULT_USAGE: UsageSnapshot[] = [
  { quotaId: 'inst-1', dailyUsed: 151200, weeklyUsed: 623000, monthlyUsed: 2420000 },
  { quotaId: 'inst-2', dailyUsed: 98000, weeklyUsed: 540000, monthlyUsed: 1810000 },
  { quotaId: 'team-1', dailyUsed: 522000, weeklyUsed: 2430000, monthlyUsed: 10050000 },
  { quotaId: 'team-2', dailyUsed: 406000, weeklyUsed: 1710000, monthlyUsed: 7020000 },
];

const DEFAULT_POLICY: QuotaPolicyState = {
  gatewayMode: 'enforce',
  warnAtPercent: 80,
  preferredPeriod: 'monthly',
  rows: DEFAULT_ROWS,
  updatedAt: null,
};

const badgeClass = (tone: Tone) =>
  tone === 'red'
    ? 'badge badge-red'
    : tone === 'orange'
      ? 'badge badge-orange'
      : tone === 'blue'
        ? 'badge badge-blue'
        : tone === 'green'
          ? 'badge badge-green'
          : tone === 'purple'
            ? 'badge badge-purple'
            : 'badge badge-slate';

const fmt = (value: number) => value.toLocaleString('en-US');

const formatTime = (iso: string | null) => {
  if (!iso) return '未保存';
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return iso;
  return `${t.getMonth() + 1}-${`${t.getDate()}`.padStart(2, '0')} ${`${t.getHours()}`.padStart(2, '0')}:${`${t.getMinutes()}`.padStart(2, '0')}`;
};

const loadPolicy = (): QuotaPolicyState => {
  if (typeof window === 'undefined') return DEFAULT_POLICY;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULT_POLICY;
    return { ...DEFAULT_POLICY, ...(JSON.parse(raw) as Partial<QuotaPolicyState>) };
  } catch {
    return DEFAULT_POLICY;
  }
};

const loadDispatchHistory = (): DispatchRecord[] => {
  if (typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(STORAGE_DISPATCH_KEY);
    if (!raw) return [];
    return JSON.parse(raw) as DispatchRecord[];
  } catch {
    return [];
  }
};

const usageByQuota = new Map(DEFAULT_USAGE.map((item) => [item.quotaId, item]));

const ratio = (used: number, limit: number) => (limit > 0 ? Math.round((used / limit) * 100) : 0);

const CollaborationQuotaPage: React.FC = () => {
  const [tab, setTab] = useState(0);
  const [policy, setPolicy] = useState<QuotaPolicyState>(() => loadPolicy());
  const [dirty, setDirty] = useState(false);
  const [saveMsg, setSaveMsg] = useState<string | null>(null);
  const [dispatching, setDispatching] = useState(false);
  const [dispatchMsg, setDispatchMsg] = useState<string | null>(null);
  const [dispatchHistory, setDispatchHistory] = useState<DispatchRecord[]>(() => loadDispatchHistory());
  const [liveAlerts, setLiveAlerts] = useState<SecplaneAlert[]>([]);
  const [alertsError, setAlertsError] = useState<string | null>(null);
  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>('all');
  const { instances, healthy, unhealthy, error: instanceError } = useInstanceHealth();

  const savePolicy = useCallback((next: QuotaPolicyState) => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    }
    setPolicy(next);
    setDirty(false);
    setSaveMsg(`配额策略已保存 · ${formatTime(next.updatedAt)}`);
  }, []);

  const saveDispatchHistory = useCallback((next: DispatchRecord[]) => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_DISPATCH_KEY, JSON.stringify(next));
    }
    setDispatchHistory(next);
  }, []);

  const markDirty = <K extends keyof QuotaPolicyState>(key: K, value: QuotaPolicyState[K]) => {
    setPolicy((prev) => ({ ...prev, [key]: value }));
    setDirty(true);
    setSaveMsg(null);
  };

  const updateRow = (id: string, key: keyof QuotaRow, value: string | number) => {
    setPolicy((prev) => ({
      ...prev,
      rows: prev.rows.map((row) => (row.id === id ? { ...row, [key]: value } : row)),
    }));
    setDirty(true);
    setSaveMsg(null);
  };

  const addRow = (scope: 'instance' | 'team') => {
    const nextRow: QuotaRow = {
      id: `${scope}-${Date.now()}`,
      scope,
      target: scope === 'instance' ? `new-instance-${policy.rows.filter((row) => row.scope === scope).length + 1}` : `new-team-${policy.rows.filter((row) => row.scope === scope).length + 1}`,
      mode: 'observe',
      daily: 120000,
      weekly: 600000,
      monthly: 2400000,
    };
    setPolicy((prev) => ({ ...prev, rows: [...prev.rows, nextRow] }));
    setDirty(true);
    setSaveMsg(null);
  };

  const removeRow = (id: string) => {
    setPolicy((prev) => ({ ...prev, rows: prev.rows.filter((row) => row.id !== id) }));
    setDirty(true);
    setSaveMsg(null);
  };

  const handleSave = () => {
    savePolicy({ ...policy, updatedAt: new Date().toISOString() });
  };

  const loadAlerts = useCallback(async () => {
    try {
      const rows = await secplaneService.listAlerts({ limit: 12 });
      setLiveAlerts(rows);
      setAlertsError(null);
    } catch (err) {
      const e = err as { message?: string };
      setAlertsError(e.message ?? '加载日志失败');
      setLiveAlerts([]);
    }
  }, []);

  useEffect(() => {
    loadAlerts();
    const timer = window.setInterval(loadAlerts, 30_000);
    return () => window.clearInterval(timer);
  }, [loadAlerts]);

  const quotaSummary = useMemo(() => {
    const tracked = policy.rows.map((row) => {
      const usage = usageByQuota.get(row.id);
      return {
        ...row,
        dailyRatio: ratio(usage?.dailyUsed ?? 0, row.daily),
        weeklyRatio: ratio(usage?.weeklyUsed ?? 0, row.weekly),
        monthlyRatio: ratio(usage?.monthlyUsed ?? 0, row.monthly),
      };
    });
    const warnRows = tracked.filter((row) => row.dailyRatio >= policy.warnAtPercent || row.weeklyRatio >= policy.warnAtPercent || row.monthlyRatio >= policy.warnAtPercent);
    const criticalRows = tracked.filter((row) => row.dailyRatio >= 100 || row.weeklyRatio >= 100 || row.monthlyRatio >= 100);
    return { tracked, warnRows, criticalRows };
  }, [policy]);

  const quotaAlerts = useMemo<QuotaAlertRow[]>(() => {
    const rows: QuotaAlertRow[] = [];
    quotaSummary.tracked.forEach((row) => {
      const usage = usageByQuota.get(row.id);
      if (!usage) return;
      const periods: Array<[Period, number, number]> = [
        ['daily', usage.dailyUsed, row.daily],
        ['weekly', usage.weeklyUsed, row.weekly],
        ['monthly', usage.monthlyUsed, row.monthly],
      ];
      periods.forEach(([period, used, limit]) => {
        const pct = ratio(used, limit);
        if (pct >= policy.warnAtPercent) {
          rows.push({
            id: `${row.id}-${period}`,
            ts: new Date().toISOString(),
            severity: pct >= 100 ? 'critical' : 'warn',
            scope: row.scope,
            target: row.target,
            period,
            usage: `${fmt(used)} / ${fmt(limit)} (${pct}%)`,
            action: pct >= 100 ? 'BLOCK' : 'WARN',
            detail: `${row.scope === 'team' ? 'Team' : '实例'} 在 ${period === 'daily' ? '日' : period === 'weekly' ? '周' : '月'} tokens 使用达到 ${pct}%，超过 ${policy.warnAtPercent}% 告警阈值。`,
          });
        }
      });
    });
    const platform: QuotaAlertRow[] = liveAlerts.slice(0, 3).map((item) => ({
      id: `platform-${item.id}`,
      ts: item.ts,
      severity: item.severity === 'high' ? 'critical' : 'warn',
      scope: 'platform' as const,
      target: item.agent_id || item.subject || 'AI Gateway',
      period: 'platform' as const,
      usage: item.rule_id || item.rule_name || 'gateway',
      action: item.action || 'WARN',
      detail: item.evidence || item.raw_payload || '平台侧协同/网关联动告警',
    }));
    return [...rows, ...platform].sort((a, b) => new Date(b.ts).getTime() - new Date(a.ts).getTime());
  }, [liveAlerts, policy.warnAtPercent, quotaSummary.tracked]);

  const filteredAlerts = useMemo(
    () => quotaAlerts.filter((item) => severityFilter === 'all' || item.severity === severityFilter),
    [quotaAlerts, severityFilter],
  );

  const policyPreview = useMemo(
    () =>
      JSON.stringify(
        {
          ai_gateway: {
            mode: policy.gatewayMode,
            warn_at_percent: policy.warnAtPercent,
            preferred_period: policy.preferredPeriod,
          },
          instance_limits: policy.rows.filter((row) => row.scope === 'instance'),
          team_limits: policy.rows.filter((row) => row.scope === 'team'),
        },
        null,
        2,
      ),
    [policy],
  );

  const dispatchQuotaPolicy = async (instanceIds: number[] | null) => {
    const nextPolicy = { ...policy, updatedAt: new Date().toISOString() };
    savePolicy(nextPolicy);
    setDispatching(true);
    setDispatchMsg(null);
    try {
      await new Promise((resolve) => window.setTimeout(resolve, 400));
      const targets = instanceIds && instanceIds.length > 0 ? instances.filter((item) => instanceIds.includes(item.id)) : instances;
      const successCount = targets.filter((item) => item.status === 'running').length;
      const failedCount = targets.length - successCount;
      const record: DispatchRecord = {
        id: `${Date.now()}`,
        revision: `quota-${Date.now().toString(36)}`,
        ts: nextPolicy.updatedAt || new Date().toISOString(),
        targetScope: instanceIds && instanceIds.length > 0 ? `${instanceIds.length} 个指定实例` : '全部实例',
        rowCount: nextPolicy.rows.length,
        successCount,
        failedCount,
      };
      saveDispatchHistory([record, ...dispatchHistory].slice(0, 8));
      setDispatchMsg(
        targets.length === 0
          ? '没有可同步的实例。'
          : failedCount === 0
            ? `配额策略 ${record.revision} 已同步到 AI Gateway / ${successCount} 个实例。`
            : `配额策略 ${record.revision} 已同步，${successCount} 个成功，${failedCount} 个待实例恢复后重试。`,
      );
    } finally {
      setDispatching(false);
    }
  };

  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">安全防护</Link>
          <span>/</span>
          <Link to="/admin/secplane/cat-comm">协同接入与通信</Link>
          <span>/</span>
          <span className="crumb-current">配额限制</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">AI Gateway Tokens 治理</div>
            <h2 className="h-title">配额限制</h2>
            <p className="h-subtitle">
              在 AI Gateway 上对<strong>单个实例</strong>和<strong>Team 级别</strong>增加 tokens 限额，支持
              <strong>日 / 周 / 月</strong>额度配置，并在使用达到 <strong>80%</strong> 时自动产生告警。
            </p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">配额策略</div>
              <div className="stat-card-value">{policy.rows.length}</div>
              <div className="stat-card-sub muted-strong">{policy.rows.filter((row) => row.scope === 'instance').length} 实例 · {policy.rows.filter((row) => row.scope === 'team').length} Team</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">告警阈值</div>
              <div className="stat-card-value tone-orange">{policy.warnAtPercent}%</div>
              <div className="stat-card-sub muted-strong">达到即 WARN，超 100% 可拦截</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">运行中实例</div>
              <div className="stat-card-value">{healthy.length}</div>
              <div className="stat-card-sub muted-strong">{unhealthy.length} 个实例待恢复</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">超阈值对象</div>
              <div className={`stat-card-value ${quotaSummary.warnRows.length > 0 ? 'tone-red' : 'tone-green'}`}>{quotaSummary.warnRows.length}</div>
              <div className="stat-card-sub muted-strong">{quotaSummary.criticalRows.length} 个已超 100%</div>
            </div>
          </div>
        </div>

        <div className="panel">
          <div className="tabs">
            {TABS.map((label, index) => (
              <button key={label} className={`tab${index === tab ? ' tab-active' : ''}`} onClick={() => setTab(index)}>
                {label}
              </button>
            ))}
          </div>

          {tab === 0 && (
            <div className="space-y-6">
              <div className="flex items-center justify-between gap-4 flex-wrap">
                <div>
                  <div className="eyebrow">网关配额策略</div>
                  <h3 className="section-title-lg mt-1">实例 / Team tokens 限额配置</h3>
                </div>
                <div className="flex items-center gap-2">
                  {dirty && <span className="badge badge-orange">有未保存变更</span>}
                  <button className="btn-primary btn-sm" onClick={handleSave}>保存策略</button>
                </div>
              </div>
              {saveMsg && (
                <div className="alert alert-info">
                  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  {saveMsg}
                </div>
              )}

              <div className="grid grid-cols-3 gap-4">
                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-2">AI Gateway 模式</div>
                  <div className="mode-selector">
                    <button className={policy.gatewayMode === 'enforce' ? 'active-enforce' : ''} onClick={() => markDirty('gatewayMode', 'enforce')}>
                      拦截
                    </button>
                    <button className={policy.gatewayMode === 'observe' ? 'active-observe' : ''} onClick={() => markDirty('gatewayMode', 'observe')}>
                      监控
                    </button>
                    <button className={policy.gatewayMode === 'off' ? 'active-off' : ''} onClick={() => markDirty('gatewayMode', 'off')}>
                      停止
                    </button>
                  </div>
                </div>
                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-2">默认观察周期</div>
                  <div className="mode-selector">
                    <button className={policy.preferredPeriod === 'daily' ? 'active-observe' : ''} onClick={() => markDirty('preferredPeriod', 'daily')}>
                      日
                    </button>
                    <button className={policy.preferredPeriod === 'weekly' ? 'active-observe' : ''} onClick={() => markDirty('preferredPeriod', 'weekly')}>
                      周
                    </button>
                    <button className={policy.preferredPeriod === 'monthly' ? 'active-enforce' : ''} onClick={() => markDirty('preferredPeriod', 'monthly')}>
                      月
                    </button>
                  </div>
                </div>
                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-2">告警阈值</div>
                  <input className="input" type="number" min={50} max={95} value={policy.warnAtPercent} onChange={(e) => markDirty('warnAtPercent', Number(e.target.value))} />
                  <div className="text-xs muted mt-2">达到阈值立即产生告警；100% 以上按 mode 决定是否拒绝。</div>
                </div>
              </div>

              <div className="grid gap-4" style={{ gridTemplateColumns: '1fr 1fr' }}>
                {(['instance', 'team'] as const).map((scope) => (
                  <div key={scope} className="panel-warm">
                    <div className="flex items-center justify-between mb-3">
                      <div>
                        <div className="eyebrow">{scope === 'instance' ? '单实例配额' : 'Team 级配额'}</div>
                        <h3 className="section-title-lg mt-1">{scope === 'instance' ? '实例 Tokens 限额' : 'Team Tokens 限额'}</h3>
                      </div>
                      <button className="btn-secondary btn-sm" onClick={() => addRow(scope)}>新增{scope === 'instance' ? '实例' : 'Team'}策略</button>
                    </div>
                    <table className="tbl">
                      <thead>
                        <tr>
                          <th>对象</th>
                          <th style={{ width: 90 }}>模式</th>
                          <th style={{ width: 110 }}>日</th>
                          <th style={{ width: 110 }}>周</th>
                          <th style={{ width: 120 }}>月</th>
                          <th style={{ width: 50 }}></th>
                        </tr>
                      </thead>
                      <tbody>
                        {policy.rows.filter((row) => row.scope === scope).map((row) => (
                          <tr key={row.id}>
                            <td>
                              <input className="input" value={row.target} onChange={(e) => updateRow(row.id, 'target', e.target.value)} />
                            </td>
                            <td>
                              <select className="input" value={row.mode} onChange={(e) => updateRow(row.id, 'mode', e.target.value)}>
                                <option value="enforce">拦截</option>
                                <option value="observe">监控</option>
                                <option value="off">停止</option>
                              </select>
                            </td>
                            <td><input className="input" type="number" value={row.daily} onChange={(e) => updateRow(row.id, 'daily', Number(e.target.value))} /></td>
                            <td><input className="input" type="number" value={row.weekly} onChange={(e) => updateRow(row.id, 'weekly', Number(e.target.value))} /></td>
                            <td><input className="input" type="number" value={row.monthly} onChange={(e) => updateRow(row.id, 'monthly', Number(e.target.value))} /></td>
                            <td><button className="text-xs text-[#dc2626] font-semibold hover:underline" onClick={() => removeRow(row.id)}>删</button></td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ))}
              </div>

              <div className="grid gap-4" style={{ gridTemplateColumns: '0.9fr 1.1fr' }}>
                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-3">使用情况预估</div>
                  <div className="space-y-3 text-sm">
                    {quotaSummary.tracked.map((row) => (
                      <div key={row.id} className="p-3 rounded-xl bg-[#fdf6f1] border border-[#eadfd8]">
                        <div className="flex items-center justify-between gap-2">
                          <div className="font-semibold text-[#171212]">{row.target}</div>
                          <span className={badgeClass(row.scope === 'team' ? 'purple' : 'blue')}>{row.scope}</span>
                        </div>
                        <div className="grid grid-cols-3 gap-2 mt-3 text-xs muted">
                          <div>日 {row.dailyRatio}%</div>
                          <div>周 {row.weeklyRatio}%</div>
                          <div>月 {row.monthlyRatio}%</div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-2">策略预览</div>
                  <pre className="code-block text-[11px] max-h-[360px] overflow-auto">{policyPreview}</pre>
                </div>
              </div>
            </div>
          )}

          {tab === 1 && (
            <div className="space-y-6">
              <div className="flex items-center justify-between gap-4 flex-wrap">
                <div>
                  <div className="eyebrow">策略下发</div>
                  <h3 className="section-title-lg mt-1">将 tokens 配额同步到 AI Gateway 与实例标签</h3>
                </div>
                <div className="flex items-center gap-2">
                  <ApplyDispatchButton
                    onDispatch={dispatchQuotaPolicy}
                    busy={dispatching}
                    className="btn-primary btn-sm"
                    triggerLabel="保存并下发"
                    busyLabel="下发中…"
                    modalTitle="选择需要同步配额策略的实例"
                    modalHint="原型阶段按实例同步；后续可扩展为按 Team、AI Gateway 分区或命名空间下发。"
                  />
                  {dispatchMsg && <span className="text-xs muted">{dispatchMsg}</span>}
                </div>
              </div>

              {instanceError && (
                <div className="alert alert-danger">
                  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01" />
                  </svg>
                  实例列表加载失败：{instanceError}
                </div>
              )}

              <div className="grid grid-cols-4 gap-3">
                <div className="stat-card">
                  <div className="stat-card-label">AI Gateway 模式</div>
                  <div className={`stat-card-value ${policy.gatewayMode === 'enforce' ? 'tone-red' : policy.gatewayMode === 'observe' ? 'tone-orange' : 'tone-green'}`}>
                    {policy.gatewayMode}
                  </div>
                  <div className="stat-card-sub muted-strong">告警阈值 {policy.warnAtPercent}%</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">实例目标</div>
                  <div className="stat-card-value">{healthy.length}</div>
                  <div className="stat-card-sub muted-strong">运行中优先灰度</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">Team 配额</div>
                  <div className="stat-card-value">{policy.rows.filter((row) => row.scope === 'team').length}</div>
                  <div className="stat-card-sub muted-strong">统一在 Gateway 汇总计费</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">下发记录</div>
                  <div className="stat-card-value">{dispatchHistory.length}</div>
                  <div className="stat-card-sub muted-strong">最近 8 次 revision</div>
                </div>
              </div>

              <div className="panel-warm">
                <div className="eyebrow mb-3">待同步策略摘要</div>
                <table className="tbl">
                  <thead>
                    <tr>
                      <th style={{ width: 120 }}>范围</th>
                      <th>对象</th>
                      <th style={{ width: 120 }}>日额度</th>
                      <th style={{ width: 120 }}>周额度</th>
                      <th style={{ width: 120 }}>月额度</th>
                      <th style={{ width: 90 }}>模式</th>
                    </tr>
                  </thead>
                  <tbody>
                    {policy.rows.map((row) => (
                      <tr key={row.id}>
                        <td>{row.scope}</td>
                        <td>{row.target}</td>
                        <td>{fmt(row.daily)}</td>
                        <td>{fmt(row.weekly)}</td>
                        <td>{fmt(row.monthly)}</td>
                        <td><span className={badgeClass(row.mode === 'enforce' ? 'red' : row.mode === 'observe' ? 'orange' : 'slate')}>{row.mode}</span></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <div className="panel">
                <div className="eyebrow mb-3">最近下发记录</div>
                <table className="tbl">
                  <thead>
                    <tr>
                      <th style={{ width: 120 }}>时间</th>
                      <th style={{ width: 140 }}>Revision</th>
                      <th style={{ width: 120 }}>策略数</th>
                      <th style={{ width: 120 }}>目标范围</th>
                      <th style={{ width: 120 }}>结果</th>
                    </tr>
                  </thead>
                  <tbody>
                    {dispatchHistory.length === 0 && (
                      <tr>
                        <td colSpan={5} className="text-center py-6 text-sm muted">暂无下发记录。</td>
                      </tr>
                    )}
                    {dispatchHistory.map((row) => (
                      <tr key={row.id}>
                        <td><span className="text-xs muted-strong">{formatTime(row.ts)}</span></td>
                        <td><code className="text-xs">{row.revision}</code></td>
                        <td>{row.rowCount}</td>
                        <td>{row.targetScope}</td>
                        <td><span className={badgeClass(row.failedCount > 0 ? 'orange' : 'green')}>{row.successCount} 成功 / {row.failedCount} 待重试</span></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {tab === 2 && (
            <div className="space-y-6">
              <div className="flex items-center justify-between gap-4 flex-wrap">
                <div>
                  <div className="eyebrow">配额日志与告警</div>
                  <h3 className="section-title-lg mt-1">80% 预警与超限处置</h3>
                </div>
                <div className="flex items-center gap-2">
                  <select className="input" style={{ width: 140 }} value={severityFilter} onChange={(e) => setSeverityFilter(e.target.value as SeverityFilter)}>
                    <option value="all">全部</option>
                    <option value="warn">预警</option>
                    <option value="critical">严重</option>
                  </select>
                  <button className="btn-secondary btn-sm" onClick={loadAlerts}>刷新日志</button>
                </div>
              </div>

              {alertsError && (
                <div className="alert alert-danger">
                  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01" />
                  </svg>
                  {alertsError}
                </div>
              )}

              <div className="grid grid-cols-4 gap-3">
                <div className="stat-card">
                  <div className="stat-card-label">当前日志数</div>
                  <div className={`stat-card-value ${filteredAlerts.some((row) => row.severity === 'critical') ? 'tone-red' : 'tone-green'}`}>{filteredAlerts.length}</div>
                  <div className="stat-card-sub muted-strong">含平台联动告警</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">80% 预警</div>
                  <div className="stat-card-value tone-orange">{quotaAlerts.filter((row) => row.severity === 'warn').length}</div>
                  <div className="stat-card-sub muted-strong">达到阈值即发告警</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">超限对象</div>
                  <div className="stat-card-value tone-red">{quotaAlerts.filter((row) => row.severity === 'critical').length}</div>
                  <div className="stat-card-sub muted-strong">按 mode 决定阻断或仅监控</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">观察重点</div>
                  <div className="stat-card-value">{policy.preferredPeriod === 'daily' ? '日' : policy.preferredPeriod === 'weekly' ? '周' : '月'}</div>
                  <div className="stat-card-sub muted-strong">运营视角默认周期</div>
                </div>
              </div>

              <table className="tbl">
                <thead>
                  <tr>
                    <th style={{ width: 120 }}>时间</th>
                    <th style={{ width: 80 }}>等级</th>
                    <th style={{ width: 80 }}>范围</th>
                    <th style={{ width: 160 }}>对象</th>
                    <th style={{ width: 90 }}>周期</th>
                    <th style={{ width: 160 }}>使用量</th>
                    <th style={{ width: 100 }}>动作</th>
                    <th>详情</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredAlerts.length === 0 && (
                    <tr>
                      <td colSpan={8} className="text-center py-6 text-sm muted">当前筛选条件下无日志。</td>
                    </tr>
                  )}
                  {filteredAlerts.map((row) => (
                    <tr key={row.id}>
                      <td><span className="text-xs muted-strong">{formatTime(row.ts)}</span></td>
                      <td><span className={badgeClass(row.severity === 'critical' ? 'red' : 'orange')}>{row.severity}</span></td>
                      <td><span className={badgeClass(row.scope === 'team' ? 'purple' : row.scope === 'instance' ? 'blue' : 'slate')}>{row.scope}</span></td>
                      <td>{row.target}</td>
                      <td>{row.period === 'platform' ? '平台' : row.period === 'daily' ? '日' : row.period === 'weekly' ? '周' : '月'}</td>
                      <td className="text-xs muted-strong">{row.usage}</td>
                      <td><span className={badgeClass(row.action === 'BLOCK' ? 'red' : 'orange')}>{row.action}</span></td>
                      <td className="text-xs muted leading-6">{row.detail}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </AdminLayout>
  );
};

export default CollaborationQuotaPage;
