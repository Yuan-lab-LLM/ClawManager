import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import ApplyDispatchButton from '../../../../components/secplane/ApplyDispatchButton';
import {
  secplaneService,
  type CollaborationPolicy,
  type SecplaneAlert,
} from '../../../../services/secplaneService';
import { useInstanceHealth } from '../../secplane/runtime/useInstanceHealth';

type Tone = 'red' | 'orange' | 'blue' | 'green' | 'purple' | 'slate';
type SeverityFilter = 'all' | 'high' | 'medium' | 'low';
type CommunicationMode = 'leader_mediated' | 'relay_only' | 'peer_limited';
type RedisAclMode = 'password_only' | 'per_team' | 'per_member';

interface DispatchRecord {
  id: string;
  revision: string;
  ts: string;
  communicationMode: CommunicationMode;
  redisAclMode: RedisAclMode;
  relayRequired: boolean;
  targetScope: string;
  targetCount: number;
  successCount: number;
  failedCount: number;
}

interface GovernanceAlertRow {
  id: string;
  ts: string;
  severity: 'high' | 'medium' | 'low';
  category: string;
  member: string;
  action: string;
  detail: string;
  source: 'prototype' | 'platform';
}

const TABS = ['策略配置', '策略下发', '日志告警'] as const;

const DEFAULT_POLICY: CollaborationPolicy = {
  teamId: '12',
  communicationMode: 'leader_mediated',
  redisAclMode: 'per_member',
  relayRequired: true,
  identityMode: 'enforce',
  schemaMode: 'observe',
  quotaMode: 'enforce',
  approvalMode: 'observe',
  muteOnAnomaly: true,
  auditReplay: true,
  xaddRps: 20,
  xaddWindowSeconds: 1,
  streamMaxLen: 5000,
  approvalThreshold: 85,
  redisAclPreview: '',
};

const MOCK_ALERTS: GovernanceAlertRow[] = [
  {
    id: 'mock-1',
    ts: '2026-06-09T15:58:00+08:00',
    severity: 'high',
    category: '身份伪造',
    member: 'coder',
    action: 'BLOCK',
    detail: 'sender_id=leader 与 instance_id 不匹配，Relay 拒绝写入 claw:team:12:inbox:reviewer。',
    source: 'prototype',
  },
  {
    id: 'mock-2',
    ts: '2026-06-09T15:46:00+08:00',
    severity: 'high',
    category: 'ACL 越权',
    member: 'reviewer',
    action: 'DENY',
    detail: '尝试 XREAD claw:team:12:inbox:coder，被 per-member ACL 拒绝。',
    source: 'prototype',
  },
  {
    id: 'mock-3',
    ts: '2026-06-09T15:31:00+08:00',
    severity: 'medium',
    category: '速率异常',
    member: 'coder',
    action: 'THROTTLE',
    detail: '30 秒内 XADD 速率达到 46 rps，超过阈值 20 rps，已触发限流。',
    source: 'prototype',
  },
  {
    id: 'mock-4',
    ts: '2026-06-09T15:12:00+08:00',
    severity: 'medium',
    category: '审批联动',
    member: 'leader',
    action: 'APPROVAL',
    detail: '高风险转派请求命中审批阈值 85，转入审批中心待处理。',
    source: 'prototype',
  },
];

const RULE_CARDS: Array<{
  key: keyof Pick<CollaborationPolicy, 'identityMode' | 'schemaMode' | 'quotaMode' | 'approvalMode'>;
  title: string;
  subtitle: string;
}> = [
  { key: 'identityMode', title: '身份绑定', subtitle: 'member_id ↔ instance_id ↔ relay token 强绑定' },
  { key: 'schemaMode', title: '消息结构校验', subtitle: '限制 envelope 字段、来源与路由方向' },
  { key: 'quotaMode', title: '配额治理', subtitle: 'XADD 速率、stream 长度、DLQ 漂移' },
  { key: 'approvalMode', title: '高风险协同审批', subtitle: '越权转派、广播、直连请求走审批' },
];

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

const formatTime = (iso: string | null | undefined) => {
  if (!iso) return '未保存';
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return iso;
  return `${t.getMonth() + 1}-${`${t.getDate()}`.padStart(2, '0')} ${`${t.getHours()}`.padStart(2, '0')}:${`${t.getMinutes()}`.padStart(2, '0')}`;
};

const persistenceForAcl = (policy: CollaborationPolicy) => {
  if (policy.redisAclMode === 'password_only') {
    return {
      read: 'claw:team:*',
      write: 'claw:team:*',
      commands: 'AUTH / XADD / XREAD / XREVRANGE',
    };
  }
  if (policy.redisAclMode === 'per_team') {
    return {
      read: `~claw:team:${policy.teamId}:*`,
      write: `~claw:team:${policy.teamId}:*`,
      commands: '+XADD +XREAD +XREVRANGE -KEYS -MONITOR',
    };
  }
  return {
    read: `~claw:team:${policy.teamId}:inbox:<member> ~claw:team:${policy.teamId}:events`,
    write: `~claw:team:${policy.teamId}:events`,
    commands: '+XADD +XREAD -KEYS -MONITOR -PSUBSCRIBE',
  };
};

const CollaborationGovernancePage: React.FC = () => {
  const [tab, setTab] = useState(0);
  const [policy, setPolicy] = useState<CollaborationPolicy>(DEFAULT_POLICY);
  const [policyLoaded, setPolicyLoaded] = useState(false);
  const [policyError, setPolicyError] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [saveMsg, setSaveMsg] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [dispatching, setDispatching] = useState(false);
  const [dispatchMsg, setDispatchMsg] = useState<string | null>(null);
  const [dispatchHistory, setDispatchHistory] = useState<DispatchRecord[]>([]);
  const [liveAlerts, setLiveAlerts] = useState<SecplaneAlert[]>([]);
  const [alertsError, setAlertsError] = useState<string | null>(null);
  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>('all');
  const { instances, healthy, unhealthy, error: instanceError } = useInstanceHealth();

  // Load policy from backend on mount. Falls back to DEFAULT_POLICY on error
  // so the UI is still usable when the backend is unreachable.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const remote = await secplaneService.getCollabPolicy();
        if (cancelled) return;
        setPolicy({ ...DEFAULT_POLICY, ...remote });
        setPolicyLoaded(true);
        setPolicyError(null);
      } catch (err) {
        if (cancelled) return;
        const e = err as { message?: string };
        setPolicyError(e.message ?? '加载策略失败');
        setPolicyLoaded(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const savePolicy = useCallback(async (next: CollaborationPolicy) => {
    setSaving(true);
    try {
      const saved = await secplaneService.saveCollabPolicy(next);
      setPolicy({ ...DEFAULT_POLICY, ...saved });
      setDirty(false);
      setSaveMsg(`策略已保存 · ${formatTime(saved.updatedAt)}`);
    } catch (err) {
      const e = err as { message?: string };
      setSaveMsg(`保存失败：${e.message ?? '未知错误'}`);
    } finally {
      setSaving(false);
    }
  }, []);

  const markDirty = <K extends keyof CollaborationPolicy>(key: K, value: CollaborationPolicy[K]) => {
    setPolicy((prev) => ({ ...prev, [key]: value }));
    setDirty(true);
    setSaveMsg(null);
  };

  const handleSave = () => {
    void savePolicy({ ...policy, updatedAt: new Date().toISOString() });
  };

  const handleReset = () => {
    setPolicy(DEFAULT_POLICY);
    setDirty(true);
    setSaveMsg('已恢复为默认协同治理草案，记得保存。');
  };

  const loadAlerts = useCallback(async () => {
    try {
      const rows = await secplaneService.listCollabAlerts(50);
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

  const governanceAlerts = useMemo<GovernanceAlertRow[]>(() => {
    const mapped = liveAlerts.slice(0, 50).map((item) => ({
      id: `platform-${item.id}`,
      ts: item.ts,
      severity: item.severity,
      category: item.rule_id || item.rule_name || '协同治理',
      member: item.agent_id || item.subject || 'platform',
      action: item.action || 'WARN',
      detail: item.evidence || item.raw_payload || '协同治理事件',
      source: 'platform' as const,
    }));
    return [...MOCK_ALERTS, ...mapped].sort((a, b) => new Date(b.ts).getTime() - new Date(a.ts).getTime());
  }, [liveAlerts]);

  const filteredAlerts = useMemo(
    () => governanceAlerts.filter((item) => severityFilter === 'all' || item.severity === severityFilter),
    [governanceAlerts, severityFilter],
  );

  const policyPreview = useMemo(
    () =>
      JSON.stringify(
        {
          team_id: policy.teamId,
          communication_mode: policy.communicationMode,
          relay_required: policy.relayRequired,
          redis_acl_mode: policy.redisAclMode,
          guardrails: {
            identity_binding: policy.identityMode,
            schema_validation: policy.schemaMode,
            quota_control: policy.quotaMode,
            approval_gate: policy.approvalMode,
          },
          runtime_limits: {
            xadd_rps: policy.xaddRps,
            xadd_window_secs: policy.xaddWindowSeconds,
            stream_maxlen: policy.streamMaxLen,
            approval_threshold: policy.approvalThreshold,
          },
          response_actions: {
            mute_on_anomaly: policy.muteOnAnomaly,
            audit_replay: policy.auditReplay,
          },
        },
        null,
        2,
      ),
    [policy],
  );

  const aclPreview = persistenceForAcl(policy);

  const highAlerts = governanceAlerts.filter((item) => item.severity === 'high').length;
  const livePlatformAlerts = governanceAlerts.filter((item) => item.source === 'platform').length;

  const dispatchPolicy = async (instanceIds: number[] | null) => {
    setDispatching(true);
    setDispatchMsg(null);
    try {
      // Save first so the dispatched bundle includes the latest policy.
      await savePolicy({ ...policy, updatedAt: new Date().toISOString() });
      const result = await secplaneService.dispatchCollabPolicy(instanceIds ?? undefined);
      const targets = result.targets ?? [];
      const successCount = targets.filter((t) => t.status === 'completed' || t.status === 'pending').length;
      const failedCount = targets.filter((t) => t.status === 'failed').length;
      const record: DispatchRecord = {
        id: `${Date.now()}`,
        revision: result.revision ?? `cg-${Date.now().toString(36)}`,
        ts: new Date().toISOString(),
        communicationMode: policy.communicationMode,
        redisAclMode: policy.redisAclMode,
        relayRequired: policy.relayRequired,
        targetScope: instanceIds && instanceIds.length > 0 ? `${instanceIds.length} 个指定实例` : '全部实例',
        targetCount: targets.length,
        successCount,
        failedCount,
      };
      setDispatchHistory((prev) => [record, ...prev].slice(0, 8));
      if (targets.length === 0) {
        setDispatchMsg('没有可下发的实例，建议先启动 Team 相关实例。');
      } else if (failedCount === 0) {
        setDispatchMsg(`策略 ${record.revision} 已派发，${successCount} 个实例待 agent 拉取生效。`);
      } else {
        setDispatchMsg(`策略 ${record.revision} 已派发，${successCount} 个成功，${failedCount} 个因实例未运行待重试。`);
      }
    } catch (err) {
      const e = err as { message?: string };
      setDispatchMsg(`下发失败：${e.message ?? '未知错误'}`);
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
          <span className="crumb-current">协同治理</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">多智能体 Team 总线治理</div>
            <h2 className="h-title">协同治理策略中心</h2>
            <p className="h-subtitle">
              面向 Team 的 Redis Stream 协同链路，统一管理 Relay 接入、Redis ACL、消息配额、越权审批和日志告警。
              当前原型页支持<strong>策略配置、策略下发、日志告警</strong>三类操作闭环。
            </p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">当前协同模式</div>
              <div className="stat-card-value">{policy.communicationMode === 'leader_mediated' ? 'leader' : policy.communicationMode === 'relay_only' ? 'relay' : 'peer'}</div>
              <div className="stat-card-sub muted-strong">{policy.redisAclMode} / Relay {policy.relayRequired ? '开启' : '关闭'}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">受管实例</div>
              <div className="stat-card-value">{instances.length}</div>
              <div className="stat-card-sub muted-strong">{healthy.length} running · {unhealthy.length} pending</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">协同告警</div>
              <div className={`stat-card-value ${highAlerts > 0 ? 'tone-red' : 'tone-green'}`}>{governanceAlerts.length}</div>
              <div className="stat-card-sub muted-strong">{highAlerts} 条高危 · 平台联动 {livePlatformAlerts}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">策略版本</div>
              <div className="stat-card-value">{dispatchHistory.length + 1}</div>
              <div className="stat-card-sub muted-strong">最近保存：{formatTime(policy.updatedAt)}</div>
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
                  <div className="eyebrow">策略草案</div>
                  <h3 className="section-title-lg mt-1">Team 协同治理配置</h3>
                </div>
                <div className="flex items-center gap-2">
                  {dirty && <span className="badge badge-orange">有未保存变更</span>}
                  <button className="btn-secondary btn-sm" onClick={handleReset}>恢复默认</button>
                  <button className="btn-primary btn-sm" onClick={handleSave} disabled={saving || !policyLoaded}>
                    {saving ? '保存中…' : '保存策略'}
                  </button>
                </div>
              </div>
              {policyError && (
                <div className="alert alert-danger">
                  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01" />
                  </svg>
                  加载策略失败：{policyError}（显示默认草案，保存将覆盖服务端）
                </div>
              )}
              {!policyLoaded && !policyError && (
                <div className="alert alert-info">正在从服务端加载协同治理策略…</div>
              )}
              {saveMsg && (
                <div className="alert alert-info">
                  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  {saveMsg}
                </div>
              )}

              <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                <div className="eyebrow mb-2">Team ID 绑定</div>
                <div className="flex items-center gap-3 mt-2">
                  <input
                    className="input"
                    type="text"
                    value={policy.teamId}
                    onChange={(e) => markDirty('teamId', e.target.value)}
                    placeholder="留空或 12 表示不绑定具体 team"
                    style={{ maxWidth: 200 }}
                  />
                  <div className="text-xs muted leading-6">
                    policy 只对 teamId 匹配的 team 生效。填 <code className="text-[11px]">12</code>（默认）= 不绑定任何真实 team，所有 dispatch 放行（bootstrap 用）。
                    填新建 team 的数字 ID（如 <code className="text-[11px]">3</code>）后，4 条规则才会在该 team 的 dispatch 上生效。
                  </div>
                </div>
              </div>

              <div className="grid grid-cols-3 gap-4">
                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-2">通信模式</div>
                  <div className="mode-selector">
                    <button className={policy.communicationMode === 'leader_mediated' ? 'active-enforce' : ''} onClick={() => markDirty('communicationMode', 'leader_mediated')}>
                      leader-mediated
                    </button>
                    <button className={policy.communicationMode === 'relay_only' ? 'active-observe' : ''} onClick={() => markDirty('communicationMode', 'relay_only')}>
                      relay-only
                    </button>
                    <button className={policy.communicationMode === 'peer_limited' ? 'active-off' : ''} onClick={() => markDirty('communicationMode', 'peer_limited')}>
                      peer-limited
                    </button>
                  </div>
                  <div className="text-xs muted mt-3 leading-6">
                    控制成员是否必须经 Leader / Relay 转派，还是允许有限的 peer-to-peer 协作。
                  </div>
                </div>
                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-2">Redis ACL 模式</div>
                  <div className="mode-selector">
                    <button className={policy.redisAclMode === 'password_only' ? 'active-off' : ''} onClick={() => markDirty('redisAclMode', 'password_only')}>
                      password
                    </button>
                    <button className={policy.redisAclMode === 'per_team' ? 'active-observe' : ''} onClick={() => markDirty('redisAclMode', 'per_team')}>
                      per-team
                    </button>
                    <button className={policy.redisAclMode === 'per_member' ? 'active-enforce' : ''} onClick={() => markDirty('redisAclMode', 'per_member')}>
                      per-member
                    </button>
                  </div>
                  <div className="text-xs muted mt-3 leading-6">
                    从统一 password 演进到按 Team / Member 的 key-pattern 与命令级最小权限。
                  </div>
                </div>
                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-2">控制面开关</div>
                  <label className="flex items-center justify-between py-2 text-sm text-[#171212]">
                    <span>强制经 Relay 接入</span>
                    <input type="checkbox" checked={policy.relayRequired} onChange={(e) => markDirty('relayRequired', e.target.checked)} />
                  </label>
                  <label className="flex items-center justify-between py-2 text-sm text-[#171212]">
                    <span>异常成员自动禁言</span>
                    <input type="checkbox" checked={policy.muteOnAnomaly} onChange={(e) => markDirty('muteOnAnomaly', e.target.checked)} />
                  </label>
                  <label className="flex items-center justify-between py-2 text-sm text-[#171212]">
                    <span>审计回放保留</span>
                    <input type="checkbox" checked={policy.auditReplay} onChange={(e) => markDirty('auditReplay', e.target.checked)} />
                  </label>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4">
                {RULE_CARDS.map((card) => {
                  const mode = policy[card.key];
                  return (
                    <div key={card.key} className="p-5 rounded-2xl border border-[#eadfd8] bg-white">
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <div className="font-bold text-[#171212]">{card.title}</div>
                          <div className="text-xs muted mt-1">{card.subtitle}</div>
                        </div>
                        <span className={badgeClass(mode === 'enforce' ? 'red' : mode === 'observe' ? 'orange' : 'slate')}>
                          {mode === 'enforce' ? '拦截' : mode === 'observe' ? '监控' : '停止'}
                        </span>
                      </div>
                      <div className="mode-selector mt-4">
                        <button className={mode === 'enforce' ? 'active-enforce' : ''} onClick={() => markDirty(card.key, 'enforce')}>
                          拦截
                        </button>
                        <button className={mode === 'observe' ? 'active-observe' : ''} onClick={() => markDirty(card.key, 'observe')}>
                          监控
                        </button>
                        <button className={mode === 'off' ? 'active-off' : ''} onClick={() => markDirty(card.key, 'off')}>
                          停止
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>

              <div className="grid gap-4" style={{ gridTemplateColumns: '1fr 1fr 1.15fr' }}>
                <div className="panel-warm">
                  <div className="eyebrow mb-3">运行时阈值</div>
                  <div className="space-y-3">
                    <label className="block">
                      <div className="text-xs muted-strong mb-1">XADD 限流阈值（次数）</div>
                      <input className="input" type="number" value={policy.xaddRps} min={1} onChange={(e) => markDirty('xaddRps', Number(e.target.value))} />
                    </label>
                    <label className="block">
                      <div className="text-xs muted-strong mb-1">限流窗口（秒）</div>
                      <input className="input" type="number" value={policy.xaddWindowSeconds} min={1} onChange={(e) => markDirty('xaddWindowSeconds', Number(e.target.value))} />
                      <div className="text-xs muted mt-1">窗口内 XADD 次数超过阈值即触发限流。默认 1 秒；改大（如 5）方便手动测试。</div>
                    </label>
                    <label className="block">
                      <div className="text-xs muted-strong mb-1">每条 Stream 最大长度</div>
                      <input className="input" type="number" value={policy.streamMaxLen} min={100} step={100} onChange={(e) => markDirty('streamMaxLen', Number(e.target.value))} />
                    </label>
                    <label className="block">
                      <div className="text-xs muted-strong mb-1">审批阈值（风险分）</div>
                      <input className="input" type="number" value={policy.approvalThreshold} min={1} max={100} onChange={(e) => markDirty('approvalThreshold', Number(e.target.value))} />
                    </label>
                  </div>
                </div>

                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-3">ACL 预期效果</div>
                  <div className="space-y-3 text-sm">
                    <div>
                      <div className="text-xs muted-strong mb-1">读权限</div>
                      <code className="text-[11px] text-[#171212]">{aclPreview.read}</code>
                    </div>
                    <div>
                      <div className="text-xs muted-strong mb-1">写权限</div>
                      <code className="text-[11px] text-[#171212]">{aclPreview.write}</code>
                    </div>
                    <div>
                      <div className="text-xs muted-strong mb-1">命令集</div>
                      <code className="text-[11px] text-[#171212]">{aclPreview.commands}</code>
                    </div>
                  </div>
                </div>

                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-2">策略预览</div>
                  <pre className="code-block text-[11px] max-h-[320px] overflow-auto">{policyPreview}</pre>
                </div>
              </div>
            </div>
          )}

          {tab === 1 && (
            <div className="space-y-6">
              <div className="flex items-center justify-between gap-4 flex-wrap">
                <div>
                  <div className="eyebrow">策略下发</div>
                  <h3 className="section-title-lg mt-1">将协同治理策略发布到 Team 相关实例</h3>
                </div>
                <div className="flex items-center gap-2">
                  <ApplyDispatchButton
                    onDispatch={dispatchPolicy}
                    busy={dispatching}
                    className="btn-primary btn-sm"
                    triggerLabel="保存并下发"
                    busyLabel="下发中…"
                    modalTitle="选择协同治理策略下发目标"
                    modalHint="原型阶段按实例选择目标；后续可扩展为按 Team、命名空间或标签下发。"
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
                  <div className="stat-card-label">运行中实例</div>
                  <div className="stat-card-value">{healthy.length}</div>
                  <div className="stat-card-sub muted-strong">推荐作为首批灰度目标</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">待处理实例</div>
                  <div className={`stat-card-value ${unhealthy.length > 0 ? 'tone-orange' : 'tone-green'}`}>{unhealthy.length}</div>
                  <div className="stat-card-sub muted-strong">未运行实例会进入待重试</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">下发记录</div>
                  <div className="stat-card-value">{dispatchHistory.length}</div>
                  <div className="stat-card-sub muted-strong">保留最近 8 次 revision</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">当前通道</div>
                  <div className="stat-card-value tone-blue">Prototype</div>
                  <div className="stat-card-sub muted-strong">后续可接 secplane / relay dispatch</div>
                </div>
              </div>

              <div className="grid gap-4" style={{ gridTemplateColumns: '1.15fr 0.85fr' }}>
                <div className="panel-warm">
                  <div className="eyebrow mb-3">待下发内容</div>
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th style={{ width: 160 }}>配置项</th>
                        <th>当前值</th>
                        <th style={{ width: 120 }}>生效方式</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr>
                        <td>communication_mode</td>
                        <td>{policy.communicationMode}</td>
                        <td>即时切换</td>
                      </tr>
                      <tr>
                        <td>redis_acl_mode</td>
                        <td>{policy.redisAclMode}</td>
                        <td>Relay / Redis 侧同步</td>
                      </tr>
                      <tr>
                        <td>identity_binding</td>
                        <td>{policy.identityMode}</td>
                        <td>总线准入校验</td>
                      </tr>
                      <tr>
                        <td>quota_control</td>
                        <td>{policy.quotaMode} · {policy.xaddRps}/{policy.xaddWindowSeconds}s / {policy.streamMaxLen} maxlen</td>
                        <td>运行时阈值</td>
                      </tr>
                    </tbody>
                  </table>
                </div>

                <div className="p-4 rounded-2xl border border-[#eadfd8] bg-white">
                  <div className="eyebrow mb-3">发布建议</div>
                  <div className="space-y-3 text-sm muted leading-6">
                    <div>1. 先对 running 实例灰度下发，验证 Relay 路由和 ACL 命中日志。</div>
                    <div>2. 若当前仍为 <code className="text-[11px]">password_only</code>，建议先切到 observe 再逐步切换到 per-member。</div>
                    <div>3. 对异常成员启用自动禁言时，需配合审批中心或人工解封流程。</div>
                  </div>
                </div>
              </div>

              <div className="panel">
                <div className="eyebrow mb-3">最近下发记录</div>
                <table className="tbl">
                  <thead>
                    <tr>
                      <th style={{ width: 120 }}>时间</th>
                      <th style={{ width: 140 }}>Revision</th>
                      <th>策略摘要</th>
                      <th style={{ width: 120 }}>目标范围</th>
                      <th style={{ width: 120 }}>结果</th>
                    </tr>
                  </thead>
                  <tbody>
                    {dispatchHistory.length === 0 && (
                      <tr>
                        <td colSpan={5} className="text-center py-6 text-sm muted">暂无下发记录。保存策略后可选择实例进行首次下发。</td>
                      </tr>
                    )}
                    {dispatchHistory.map((item) => (
                      <tr key={item.id}>
                        <td><span className="text-xs muted-strong">{formatTime(item.ts)}</span></td>
                        <td><code className="text-xs">{item.revision}</code></td>
                        <td className="text-sm">
                          {item.communicationMode} · {item.redisAclMode} · Relay {item.relayRequired ? 'on' : 'off'}
                        </td>
                        <td className="text-xs">{item.targetScope} / {item.targetCount}</td>
                        <td>
                          <span className={badgeClass(item.failedCount > 0 ? 'orange' : 'green')}>
                            {item.successCount} 成功 / {item.failedCount} 待重试
                          </span>
                        </td>
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
                  <div className="eyebrow">日志与告警</div>
                  <h3 className="section-title-lg mt-1">协同链路审计、越权告警与平台联动</h3>
                </div>
                <div className="flex items-center gap-2">
                  <select className="input" style={{ width: 140 }} value={severityFilter} onChange={(e) => setSeverityFilter(e.target.value as SeverityFilter)}>
                    <option value="all">全部严重度</option>
                    <option value="high">高危</option>
                    <option value="medium">中危</option>
                    <option value="low">低危</option>
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
                  <div className="stat-card-label">当前视图告警</div>
                  <div className={`stat-card-value ${filteredAlerts.some((item) => item.severity === 'high') ? 'tone-red' : 'tone-green'}`}>{filteredAlerts.length}</div>
                  <div className="stat-card-sub muted-strong">筛选后日志数</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">身份类高危</div>
                  <div className="stat-card-value tone-red">{governanceAlerts.filter((item) => item.category.includes('身份')).length}</div>
                  <div className="stat-card-sub muted-strong">伪造 / 越权 / ACL</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">平台联动</div>
                  <div className="stat-card-value tone-purple">{livePlatformAlerts}</div>
                  <div className="stat-card-sub muted-strong">来自 secplane 通用告警流</div>
                </div>
                <div className="stat-card">
                  <div className="stat-card-label">自动动作</div>
                  <div className="stat-card-value">{policy.muteOnAnomaly ? 'Mute' : 'Alert'}</div>
                  <div className="stat-card-sub muted-strong">异常成员 {policy.muteOnAnomaly ? '自动禁言' : '仅告警'}</div>
                </div>
              </div>

              <table className="tbl">
                <thead>
                  <tr>
                    <th style={{ width: 120 }}>时间</th>
                    <th style={{ width: 90 }}>等级</th>
                    <th style={{ width: 120 }}>类型</th>
                    <th style={{ width: 120 }}>成员 / 来源</th>
                    <th style={{ width: 120 }}>动作</th>
                    <th>详情</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredAlerts.length === 0 && (
                    <tr>
                      <td colSpan={6} className="text-center py-6 text-sm muted">当前筛选条件下无日志。</td>
                    </tr>
                  )}
                  {filteredAlerts.map((item) => (
                    <tr key={item.id}>
                      <td><span className="text-xs muted-strong">{formatTime(item.ts)}</span></td>
                      <td>
                        <span className={badgeClass(item.severity === 'high' ? 'red' : item.severity === 'medium' ? 'orange' : 'blue')}>
                          {item.severity}
                        </span>
                      </td>
                      <td><span className="text-sm font-semibold text-[#171212]">{item.category}</span></td>
                      <td className="text-xs">{item.member}</td>
                      <td>
                        <span className={badgeClass(item.action === 'BLOCK' || item.action === 'DENY' ? 'red' : item.action === 'THROTTLE' || item.action === 'APPROVAL' ? 'orange' : 'slate')}>
                          {item.action}
                        </span>
                      </td>
                      <td className="text-xs muted leading-6">
                        {item.detail}
                        <div className="mt-1">
                          <span className={badgeClass(item.source === 'platform' ? 'purple' : 'blue')}>
                            {item.source === 'platform' ? '平台联动' : '协同原型'}
                          </span>
                        </div>
                      </td>
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

export default CollaborationGovernancePage;
