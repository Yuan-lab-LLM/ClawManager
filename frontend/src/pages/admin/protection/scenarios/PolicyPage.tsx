import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import { useI18n } from '../../../../contexts/I18nContext';

// Policy Governance (scenario m) — aligned with KSecForAIDemo/scenario-m-policy.html

type Tone = 'red' | 'orange' | 'amber' | 'blue' | 'purple' | 'green' | 'teal' | 'slate';
type Mode = 'enforce' | 'observe' | 'off';

const SCOPES: Array<[string, string, Tone, string, number, string, string]> = [
  ['1', 'host', 'blue', 'hostDesc', 12, '#1d4ed8', 'hostTip'],
  ['2', 'instance', 'orange', 'instanceDesc', 22, '#b45309', 'instanceTip'],
];

const TARGETS: Array<[string, string]> = [
  ['runtime', '安全策略配置.toolCallGov[]'],
  ['host', 'gRPC containerPolicy.yaml'],
  ['audit', '安全基线配置.rules[]'],
];

const TEMPLATES: Array<[string, string, Tone, number]> = [
  ['financeStrict', 'financeStrictDesc', 'red', 12],
  ['productionStandard', 'productionStandardDesc', 'blue', 8],
  ['devObservation', 'devObservationDesc', 'amber', 6],
  ['testSandbox', 'testSandboxDesc', 'slate', 4],
  ['mcpService', 'mcpServiceDesc', 'teal', 5],
  ['multiAgent', 'multiAgentDesc', 'purple', 7],
];

const RULE_JSON = `{
  "rule_id": "block-sql-drop",
  "scope": {
    "type": "instance",
    "target": "openclaw-finance-svc"
  },
  "match": {
    "tool": "mysql_exec",
    "pattern": "DROP\\\\s+TABLE\\\\s+.*"
  },
  "action": "block",
  "severity": "high"
}`;

const targetBadge = (t: string) => (t === 'runtime' ? 'badge-red' : t === 'host' ? 'badge-blue' : 'badge-purple');
const modeBadge = (m: Mode) => (m === 'enforce' ? 'badge-red' : m === 'observe' ? 'badge-orange' : 'badge-slate');

const POLICIES: Array<[string, string, 'host' | 'instance', string, Mode, string, string]> = [
  ['cis-host-baseline', 'cisHostBaseline', 'host', 'node-east-1, node-east-2, +6', 'enforce', 'synced', '2h 前 / 张三'],
  ['ransome-host-guard', 'ransomeHostGuard', 'host', '所有节点 (8)', 'enforce', 'synced', '5h 前 / 李四'],
  ['agent-prod-strict', 'agentProdStrict', 'instance', 'openclaw-prod-east-12', 'enforce', 'synced', '1h 前 / 张三'],
  ['agent-finance-bot', 'agentFinanceBot', 'instance', 'openclaw-finance-svc', 'enforce', 'synced', '1d 前 / 李四'],
  ['observation-mode-test', 'observationModeTest', 'instance', 'openclaw-staging-7', 'observe', 'synced', '3d 前 / 王五'],
  ['emergency-deny-east12', 'emergencyDenyEast12', 'instance', 'openclaw-prod-east-12', 'enforce', 'synced', '23m 前 / SYSTEM'],
];

const PolicyPage: React.FC = () => {
  const { t } = useI18n();
  const p = 'secplane.protection.policy';
  const [tab, setTab] = useState(0);

  const tabLabels = [
    t(`${p}.tabs.activePolicies`, { count: 34 }),
    t(`${p}.tabs.templates`, { count: 12 }),
    t(`${p}.tabs.changeAudit`, { count: 12 }),
    t(`${p}.tabs.consistencyCheck`),
  ];

  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('nav.secplane')}</Link>
          <span>/</span>
          <Link to="/admin/secplane/cat-policy">{t(`${p}.breadcrumb.parent`)}</Link>
          <span>/</span>
          <span className="crumb-current">{t(`${p}.breadcrumb.current`)}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t(`${p}.hero.eyebrow`)}</div>
            <h2 className="h-title">{t(`${p}.hero.title`)}</h2>
            <p className="h-subtitle">
              {t(`${p}.hero.subtitle`)}
            </p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">{t(`${p}.stats.activePolicies`)}</div>
              <div className="stat-card-value">34</div>
              <div className="stat-card-sub muted-strong">{t(`${p}.stats.activePoliciesSub`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${p}.stats.sync`)}</div>
              <div className="stat-card-value tone-green">100%</div>
              <div className="stat-card-sub muted-strong">{t(`${p}.stats.syncSub`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${p}.stats.weeklyChanges`)}</div>
              <div className="stat-card-value tone-orange">12</div>
              <div className="stat-card-sub muted-strong">{t(`${p}.stats.weeklyChangesSub`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${p}.stats.policyTemplates`)}</div>
              <div className="stat-card-value">12</div>
              <div className="stat-card-sub muted-strong">{t(`${p}.stats.policyTemplatesSub`)}</div>
            </div>
          </div>
        </div>

        <div className="panel">
          <div className="eyebrow mb-3">{t(`${p}.scopes.eyebrow`)}</div>
          <h3 className="section-title-lg mb-4">{t(`${p}.scopes.title`)}</h3>
          <div className="grid grid-cols-2 gap-4">
            {SCOPES.map(([n, key, , descKey, count, color, tipKey]) => (
              <div key={n} className="p-5 rounded-2xl border-2 bg-white" style={{ borderColor: color }}>
                <div className="flex items-center gap-2 mb-3">
                  <div
                    className="w-9 h-9 rounded-xl flex items-center justify-center font-bold text-white text-sm"
                    style={{ background: color }}
                  >
                    {n}
                  </div>
                  <span className="font-bold text-[#171212]">{t(`${p}.scopes.${key}`)}</span>
                </div>
                <div className="text-xs muted leading-5 mb-3">{t(`${p}.scopes.${descKey}`)}</div>
                <div className="flex items-baseline justify-between">
                  <span className="text-xs muted-strong">{t(`${p}.scopes.activeRules`)}</span>
                  <span className="text-3xl font-bold" style={{ color }}>
                    {count}
                  </span>
                </div>
                <div className="text-[10px] muted-strong mt-2 italic">{t(`${p}.scopes.${tipKey}`)}</div>
              </div>
            ))}
          </div>
          <div className="alert alert-info mt-4">
            <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            {t(`${p}.scopes.infoNote`)}
          </div>
        </div>

        <div className="panel">
          <div className="eyebrow mb-3">{t(`${p}.compiler.eyebrow`)}</div>
          <h3 className="section-title-lg mb-4">{t(`${p}.compiler.title`)}</h3>
          <div className="grid gap-6 items-center" style={{ gridTemplateColumns: '1fr auto 1fr' }}>
            <div className="panel-warm">
              <div className="eyebrow text-[10px] mb-2">{t(`${p}.compiler.inputLabel`)}</div>
              <pre className="code-block text-[11px]">{RULE_JSON}</pre>
            </div>
            <svg width="40" height="40" fill="none" viewBox="0 0 24 24" stroke="currentColor" className="muted">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M14 5l7 7m0 0l-7 7m7-7H3" />
            </svg>
            <div className="space-y-2">
              {TARGETS.map(([key, path]) => (
                <div key={key} className="p-3 rounded-xl bg-white border border-[#eadfd8] flex items-center gap-2">
                  <span className={`badge ${targetBadge(key)}`}>{t(`${p}.compiler.${key}`)}</span>
                  <code className="text-xs flex-1">{path}</code>
                  <span className="text-xs tone-green font-bold">{t(`${p}.compiler.synced`)}</span>
                </div>
              ))}
            </div>
          </div>
          <div className="alert alert-info mt-4">
            <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            {t(`${p}.compiler.infoNote`)}
          </div>
        </div>

        <div className="panel">
          <div className="tabs">
            {tabLabels.map((label, i) => (
              <button key={i} className={`tab${i === tab ? ' tab-active' : ''}`} onClick={() => setTab(i)}>
                {label}
              </button>
            ))}
          </div>
          <div className="flex items-center justify-between mb-3">
            <h3 className="section-title-lg">{t(`${p}.list.title`)}</h3>
            <div className="flex gap-2">
              <select className="input" style={{ width: 140 }}>
                <option>{t(`${p}.list.allScopes`)}</option>
                <option>{t(`${p}.list.hostLevel`)}</option>
                <option>{t(`${p}.list.instanceLevel`)}</option>
              </select>
              <select className="input" style={{ width: 140 }}>
                <option>{t(`${p}.list.allScenarios`)}</option>
                <option>{t(`${p}.list.inputSurface`)}</option>
                <option>{t(`${p}.list.decisionSurface`)}</option>
                <option>{t(`${p}.list.outboundGovernance`)}</option>
                <option>{t(`${p}.list.hostHardening`)}</option>
              </select>
              <input className="input" style={{ width: 240 }} placeholder={t(`${p}.list.searchPlaceholder`)} />
              <button className="btn-primary btn-sm">{t(`${p}.list.newPolicy`)}</button>
            </div>
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th>{t(`${p}.list.columnName`)}</th>
                <th style={{ width: 180 }}>{t(`${p}.list.columnScenario`)}</th>
                <th>{t(`${p}.list.columnScope`)}</th>
                <th>{t(`${p}.list.columnTarget`)}</th>
                <th>{t(`${p}.list.columnMode`)}</th>
                <th>{t(`${p}.list.columnSync`)}</th>
                <th>{t(`${p}.list.columnUpdated`)}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {POLICIES.map(([name, scKey, scope, target, mode, syncKey, upd]) => (
                <tr key={name}>
                  <td>
                    <span className="font-semibold text-[#171212]">{name}</span>
                  </td>
                  <td>
                    <span className="text-xs font-medium text-[#171212]">{t(`${p}.policies.${scKey}`)}</span>
                  </td>
                  <td>
                    <span className={`badge ${scope === 'host' ? 'badge-blue' : 'badge-orange'}`}>
                      {scope === 'host' ? t(`${p}.list.hostLevel`) : t(`${p}.list.instanceLevel`)}
                    </span>
                  </td>
                  <td>
                    <span className="text-xs font-mono muted truncate inline-block" style={{ maxWidth: 200 }}>
                      {target}
                    </span>
                  </td>
                  <td>
                    <span className={`badge ${modeBadge(mode)}`}>{mode.toUpperCase()}</span>
                  </td>
                  <td>
                    <span className="text-xs tone-green font-semibold">{t(`${p}.policies.${syncKey}`)}</span>
                  </td>
                  <td>
                    <span className="text-xs muted">{upd}</span>
                  </td>
                  <td>
                    <button className="icon-btn">
                      <svg width="14" height="14" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 5v.01M12 12v.01M12 19v.01" />
                      </svg>
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="panel">
          <div className="flex items-center justify-between mb-4">
            <div>
              <div className="eyebrow">{t(`${p}.templateSection.eyebrow`)}</div>
              <h3 className="section-title-lg mt-1">{t(`${p}.templateSection.title`)}</h3>
            </div>
          </div>
          <div className="grid grid-cols-4 gap-3">
            {TEMPLATES.map(([key, descKey, tone, refs]) => (
              <div
                key={key}
                className="p-4 rounded-2xl border border-[#eadfd8] bg-white hover:border-[#ef6b4a] hover:shadow-md transition cursor-pointer"
              >
                <div className="flex items-center justify-between mb-2">
                  <span className="font-bold text-[#171212] text-sm">{t(`${p}.templates.${key}`)}</span>
                  <span className={`badge badge-${tone === 'teal' ? 'green' : tone}`}>{t(`${p}.templateSection.rules`, { count: refs })}</span>
                </div>
                <div className="text-xs muted leading-5">{t(`${p}.templates.${descKey}`)}</div>
                <div className="divider" />
                <button className="btn-secondary btn-sm w-full text-xs">{t(`${p}.templateSection.apply`)}</button>
              </div>
            ))}
          </div>
        </div>
      </div>
    </AdminLayout>
  );
};

export default PolicyPage;
