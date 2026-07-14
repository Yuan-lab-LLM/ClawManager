import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import { useI18n } from '../../../../contexts/I18nContext';

// 全链路审计 (scenario j) — 对齐 KSecForAIDemo/scenario-j-audit.html

type Tone = 'red' | 'orange' | 'amber' | 'blue' | 'purple' | 'green';

const AuditPage: React.FC = () => {
  const { t } = useI18n();
  const k = 'secplane.protection.govern.audit';
  const [tab, setTab] = useState(0);

  const TABS = [
    t(`${k}.tabs.liveStream`, { count: '12.8k' }),
    t(`${k}.tabs.complianceReport`),
    t(`${k}.tabs.traceQuery`),
    t(`${k}.tabs.logArchive`),
  ] as const;

  const EVENTS: Array<[string, string, string, string, string, string, string, Tone]> = [
    ['刚刚', '运行时层', '输入面防护', 'jailbreak-dan-v3', 'openclaw-prod-east-12', '7a3f9c1e', 'BLOCK', 'red'],
    ['1m', '主机层', '容器隔离', 'container-escape-setns', 'openclaw-prod-east-12', '7a3f9c1e', 'BLOCK', 'red'],
    ['1m', '审计', '输出面防护', 'credential-monitor-aws', 'openclaw-finance-svc', 'b2e8d4a7', 'WARN', 'orange'],
    ['2m', '运行时层', '决策面防护', 'dangerous-sql-drop', 'openclaw-ops-bot-3', 'c4f7a1e8', 'APPROVAL', 'orange'],
    ['3m', '主机层', '宿主加固', 'ransomware-yara-hit', 'node-east-3', 'd5c1e8f3', 'KILL', 'red'],
    ['4m', '运行时层', '出站治理', 'outbound-non-allowlist', 'openclaw-mcp-router', 'e9b3d2a6', 'BLOCK', 'red'],
    ['5m', '审计', '组件可信扫描', '供应链 IOC 库-hit', 'skill-prod-v3.1', 'f8a2b9c4', 'BLOCK', 'red'],
    ['7m', '运行时层', '输出面防护', 'output-api-key-redact', 'openclaw-dev-test-1', 'a1b2c3d4', 'REDACT', 'amber'],
  ];

  const SOURCES: Array<[string, Tone, number, string]> = [
    [t(`${k}.sources.runtimeEvents`), 'red', 8243, '64%'],
    [t(`${k}.sources.hostGrpc`), 'blue', 3104, '24%'],
    [t(`${k}.sources.auditSecurity`), 'purple', 1500, '12%'],
  ];

  const SCENE_HITS: Array<[string, number, Tone]> = [
    [t(`${k}.sceneHits.inputSurface`), 2412, 'red'],
    [t(`${k}.sceneHits.decisionSurface`), 1872, 'red'],
    [t(`${k}.sceneHits.outboundGovernance`), 1234, 'red'],
    [t(`${k}.sceneHits.hostAnomaly`), 847, 'orange'],
    [t(`${k}.sceneHits.componentTrust`), 623, 'orange'],
    [t(`${k}.sceneHits.outputSurface`), 412, 'orange'],
  ];

  const REPORTS = [
    t(`${k}.reports.owasp`),
    t(`${k}.reports.mitre`),
    t(`${k}.reports.csa`),
    t(`${k}.reports.compliance`),
  ];

  const sourceBadgeTone = (src: string) =>
    src === '运行时层' || src === 'Runtime Layer' || src === 'ランタイム層' || src === '런타임 계층' || src === 'Runtime-Ebene'
      ? 'badge-red'
      : src === '主机层' || src === 'Host Layer' || src === 'ホスト層' || src === '호스트 계층' || src === 'Host-Ebene'
        ? 'badge-blue'
        : 'badge-purple';

  const barGradient = (tone: Tone) =>
    tone === 'red'
      ? 'linear-gradient(90deg, #ef6b4a, #dc2626)'
      : tone === 'blue'
      ? 'linear-gradient(90deg, #3b82f6, #1d4ed8)'
      : 'linear-gradient(90deg, #a855f7, #6b21a8)';

  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('nav.secplane')}</Link>
          <span>/</span>
          <Link to="/admin/secplane/cat-govern">{t(`${k}.breadcrumb.parent`)}</Link>
          <span>/</span>
          <span className="crumb-current">{t(`${k}.breadcrumb.current`)}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t(`${k}.hero.eyebrow`)}</div>
            <h2 className="h-title">{t(`${k}.hero.title`)}</h2>
            <p className="h-subtitle">{t(`${k}.hero.subtitle`)}</p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">{t(`${k}.stats.events24h`)}</div>
              <div className="stat-card-value">12,847</div>
              <div className="stat-card-sub muted-strong">{t(`${k}.stats.events24hSub`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${k}.stats.highRiskEvents`)}</div>
              <div className="stat-card-value tone-red">847</div>
              <div className="stat-card-sub muted-strong">{t(`${k}.stats.highRiskEventsSub`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${k}.stats.traceCoverage`)}</div>
              <div className="stat-card-value tone-green">100%</div>
              <div className="stat-card-sub muted-strong">{t(`${k}.stats.traceCoverageSub`)}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t(`${k}.stats.persistenceDelay`)}</div>
              <div className="stat-card-value">3.2s</div>
              <div className="stat-card-sub muted-strong">{t(`${k}.stats.persistenceDelaySub`)}</div>
            </div>
          </div>
        </div>

        <div className="panel">
          <div className="tabs">
            {TABS.map((tabLabel, i) => (
              <button key={i} className={`tab${i === tab ? ' tab-active' : ''}`} onClick={() => setTab(i)}>
                {tabLabel}
              </button>
            ))}
          </div>
          <div className="flex items-center justify-between mb-3">
            <h3 className="section-title-lg">{String(TABS[tab]).replace(/\s*\(.*\)\s*$/, '')}</h3>
            <div className="flex gap-2">
              <select className="input" style={{ width: 140 }}>
                <option>{t(`${k}.filters.allSources`)}</option>
                <option>{t(`${k}.filters.runtimeLayer`)}</option>
                <option>{t(`${k}.filters.hostLayer`)}</option>
                <option>{t(`${k}.filters.auditLayer`)}</option>
              </select>
              <select className="input" style={{ width: 140 }}>
                <option>{t(`${k}.filters.allScenarios`)}</option>
                <option>{t(`${k}.filters.inputSurface`)}</option>
                <option>{t(`${k}.filters.decisionSurface`)}</option>
                <option>{t(`${k}.filters.outputSurface`)}</option>
                <option>{t(`${k}.filters.outboundGovernance`)}</option>
                <option>{t(`${k}.filters.hostHardening`)}</option>
              </select>
              <select className="input" style={{ width: 140 }}>
                <option>{t(`${k}.filters.allSeverities`)}</option>
                <option>{t(`${k}.filters.critical`)}</option>
                <option>{t(`${k}.filters.high`)}</option>
                <option>{t(`${k}.filters.medium`)}</option>
                <option>{t(`${k}.filters.observation`)}</option>
              </select>
              <input className="input" style={{ width: 240 }} placeholder={t(`${k}.filters.searchPlaceholder`)} />
              <button className="btn-secondary btn-sm">{t(`${k}.filters.exportJsonl`)}</button>
            </div>
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 80 }}>{t(`${k}.table.time`)}</th>
                <th style={{ width: 90 }}>{t(`${k}.table.source`)}</th>
                <th style={{ width: 120 }}>{t(`${k}.table.defenseScenario`)}</th>
                <th>{t(`${k}.table.rule`)}</th>
                <th>{t(`${k}.table.instance`)}</th>
                <th>{t(`${k}.table.traceId`)}</th>
                <th style={{ width: 90 }}>{t(`${k}.table.action`)}</th>
                <th style={{ width: 50 }}></th>
              </tr>
            </thead>
            <tbody>
              {EVENTS.map(([evTime, src, sc, rule, inst, trace, act, tone], i) => (
                <tr key={i}>
                  <td><span className="muted-strong text-xs">{evTime}</span></td>
                  <td><span className={`badge ${sourceBadgeTone(src)}`}>{src}</span></td>
                  <td><span className="text-xs font-medium text-[#171212]">{sc}</span></td>
                  <td><code className="text-xs">{rule}</code></td>
                  <td><span className="font-mono text-xs">{inst}</span></td>
                  <td><code className="text-xs muted">{trace}</code></td>
                  <td><span className={`badge badge-${tone === 'amber' ? 'orange' : tone}`}>{act}</span></td>
                  <td>
                    <button className="icon-btn" aria-label="详情">
                      <svg width="14" height="14" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 5l7 7-7 7" />
                      </svg>
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="grid grid-cols-3 gap-4">
          <div className="panel">
            <div className="eyebrow mb-3">{t(`${k}.stats.events24h`)}</div>
            <h3 className="section-title-lg mb-4">{t(`${k}.stats.events24h`)}</h3>
            <div className="space-y-3">
              {SOURCES.map(([name, tone, count, pct]) => (
                <div key={name}>
                  <div className="flex justify-between mb-1">
                    <span className="text-sm font-semibold text-[#171212]">{name}</span>
                    <span className={`text-sm font-bold tone-${tone}`}>
                      {count.toLocaleString()} ({pct})
                    </span>
                  </div>
                  <div className="mini-bar-track">
                    <div className="mini-bar-fill" style={{ width: pct, background: barGradient(tone) }} />
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="panel">
            <div className="eyebrow mb-3">{t(`${k}.sceneHits.title`)}</div>
            <h3 className="section-title-lg mb-4">{t(`${k}.sceneHits.title`)}</h3>
            <div className="space-y-2 text-sm">
              {SCENE_HITS.map(([name, n, tone]) => (
                <div key={name} className="flex items-center justify-between p-2 rounded-lg hover:bg-[#fdf6f1]">
                  <span className="text-[#171212]">{name}</span>
                  <span className={`font-bold tone-${tone}`}>{n}</span>
                </div>
              ))}
            </div>
          </div>

          <div className="panel">
            <div className="eyebrow mb-3">{t(`${k}.reports.eyebrow`)}</div>
            <h3 className="section-title-lg mb-4">{t(`${k}.reports.title`)}</h3>
            <div className="space-y-2 mb-4">
              {REPORTS.map((r) => (
                <button key={r} className="btn-secondary btn-sm w-full justify-start">
                  <svg width="14" height="14" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth="2"
                      d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"
                    />
                  </svg>
                  {r}
                </button>
              ))}
            </div>
            <div className="alert alert-success">
              <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 12l2 2 4-4" />
              </svg>
              {t(`${k}.reports.owaspFull`)}
            </div>
          </div>
        </div>
      </div>
    </AdminLayout>
  );
};

export default AuditPage;
