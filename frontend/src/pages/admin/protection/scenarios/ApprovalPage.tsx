import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import { FEATURES } from '../../../../config/features';

// 人因审批 (scenario g) — 对齐 KSecForAIDemo/scenario-g-approval.html

type RiskTone = 'red' | 'orange' | 'amber';

interface ApprovalCase {
  sla: string;
  pulse: boolean;
  risk: number;
  riskLabel: string;
  riskTone: RiskTone;
  inst: string;
  user: string;
  tool: string;
  cmd: string;
  scope: string;
  time: string;
  events: number;
  rules: string[];
}

const CASES: ApprovalCase[] = [
  {
    sla: '04:32', pulse: true, risk: 87, riskLabel: '非常高', riskTone: 'red',
    inst: 'openclaw-prod-east-12', user: 'user_8842', tool: 'mysql_exec',
    cmd: 'DROP TABLE prod_users;', scope: '生产用户表（1.2M 行）',
    time: '02:14:33', events: 23, rules: ['dangerous-sql-3'],
  },
  {
    sla: '12:15', pulse: false, risk: 64, riskLabel: '高', riskTone: 'orange',
    inst: 'openclaw-ops-bot-3', user: 'user_5523', tool: 'shell_exec',
    cmd: 'systemctl stop prod-api', scope: '生产 API 服务',
    time: '02:38:12', events: 8, rules: ['service-control', 'non-business-hour'],
  },
  {
    sla: '18:42', pulse: false, risk: 52, riskLabel: '中', riskTone: 'amber',
    inst: 'openclaw-finance-svc', user: 'user_1142', tool: 'http_request',
    cmd: 'PUT /api/users/{id}/role', scope: '用户权限修改',
    time: '03:01:08', events: 3, rules: ['privileged-action'],
  },
];

const TABS = ['待审批 (3)', '已处理', '已超时', '审批策略'] as const;

const PRINCIPLES: Array<[string, string, string]> = [
  ['1', '默认按钮为「拒绝」', '所有审批默认聚焦"拒绝"，要求主动选择"允许"'],
  ['2', '强制完整命令预览', '禁止任何省略，含语法高亮与命中模式标注'],
  ['3', '强制操作摘要', '≥ 20 字理由说明，未填则"允许"按钮 disabled'],
  ['4', '复选确认事项', '3 项强制勾选（影响/可逆性/时段）'],
  ['5', '24h 行为统计', '同实例历史风险事件、命中规则、异常趋势'],
];

const toneClass = (t: RiskTone) => `tone-${t === 'amber' ? 'orange' : t}`;
const badgeClass = (t: RiskTone) => `badge-${t === 'amber' ? 'orange' : t}`;

const ApprovalPage: React.FC = () => {
  const [tab, setTab] = useState(0);
  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">安全防护</Link>
          <span>/</span>
          <Link to="/admin/secplane/runtime">智能体运行时安全</Link>
          <span>/</span>
          <span className="crumb-current">人因审批</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">高风险操作审批回路</div>
            <h2 className="h-title">人因审批中心</h2>
            <p className="h-subtitle">
              决策对齐引擎 触发的高风险操作审批工作流。反误导设计：默认拒绝、强制摘要、复选确认、24h 行为统计可视化。
            </p>
          </div>
        </div>

        {!FEATURES.approvalCenter && (
          <div className="panel">
            <div className="text-center py-12">
              <div className="text-base font-semibold text-[#171212] mb-2">功能开发中</div>
              <div className="text-sm muted">
                审批队列 / 工作流后端尚未就绪。开通后此处将展示待审批高风险操作、SLA 倒计时、智能体身份与风险评分。
              </div>
            </div>
          </div>
        )}

        {FEATURES.approvalCenter && <div className="panel">
          <div className="tabs">
            {TABS.map((t, i) => (
              <button key={i} className={`tab${i === tab ? ' tab-active' : ''}`} onClick={() => setTab(i)}>
                {t}
              </button>
            ))}
          </div>
          <div className="space-y-3">
            {CASES.map((a, idx) => (
              <div
                key={idx}
                className={
                  idx === 0
                    ? 'rounded-2xl border-2 border-red-200 bg-gradient-to-br from-red-50 to-white p-5'
                    : 'rounded-2xl border border-[#eadfd8] bg-white p-5'
                }
              >
                <div className="flex items-start justify-between gap-4 mb-3">
                  <div className="flex items-center gap-3">
                    {idx === 0 && (
                      <div className="w-9 h-9 rounded-xl bg-red-600 flex items-center justify-center pulse-red">
                        <svg width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="white">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01" />
                        </svg>
                      </div>
                    )}
                    <div>
                      <div className="flex items-center gap-2">
                        <span className={`badge ${badgeClass(a.riskTone)}`}>{a.riskLabel}风险</span>
                        <span className="text-xs muted-strong">触发 {a.time}</span>
                      </div>
                      <div className="font-bold text-lg text-[#171212] mt-1">
                        {a.tool} → <code className="text-base font-mono">{a.cmd}</code>
                      </div>
                      <div className="text-xs muted mt-1">{a.scope}</div>
                    </div>
                  </div>
                  <div className="text-right shrink-0">
                    <div className="eyebrow text-[10px]">SLA 倒计时</div>
                    <div className={`text-2xl font-bold ${a.pulse ? 'tone-red' : 'tone-orange'} font-mono`}>{a.sla}</div>
                  </div>
                </div>
                <div className="grid gap-3 mt-3" style={{ gridTemplateColumns: '1fr 140px 220px' }}>
                  <div className="p-3 rounded-xl bg-[#fdf6f1] border border-[#eadfd8]">
                    <div className="eyebrow text-[10px]">智能体身份</div>
                    <div className="text-sm font-semibold text-[#171212] mt-1">{a.inst}</div>
                    <div className="text-xs muted">用户 {a.user}</div>
                  </div>
                  <div className="p-3 rounded-xl bg-white border border-[#eadfd8]">
                    <div className="eyebrow text-[10px]">风险评分</div>
                    <div className={`text-2xl font-bold ${toneClass(a.riskTone)} mt-1`}>{a.risk}</div>
                    <div className="text-xs muted">/100</div>
                  </div>
                  <div className="p-3 rounded-xl bg-white border border-[#eadfd8]">
                    <div className="eyebrow text-[10px]">24h 行为</div>
                    <div className="text-sm font-bold text-[#171212] mt-1">
                      {a.events} 事件 · 命中 {a.rules.length} 规则
                    </div>
                    <div className="text-xs muted">{a.rules.join(', ')}</div>
                  </div>
                </div>
                <div className="flex items-center justify-end gap-2 mt-4">
                  <button className="btn-secondary btn-sm">查看详情</button>
                  <button className="btn-secondary btn-sm">转交上级</button>
                  <button className="inline-flex items-center justify-center gap-2 rounded-2xl border border-[#16a34a] text-[#15803d] bg-gradient-to-br from-green-50 to-white px-4 py-2 text-sm font-bold hover:shadow-lg">
                    🛡 拒绝（推荐）
                  </button>
                  <button className="inline-flex items-center justify-center gap-2 rounded-2xl border border-red-300 text-red-700 bg-red-50 px-4 py-2 text-sm font-bold opacity-50 cursor-not-allowed">
                    ✋ 允许
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>}

        {FEATURES.approvalCenter && <div className="panel-warm">
          <div className="eyebrow mb-3">反误导设计原则</div>
          <h3 className="section-title-lg mb-4">5 项强制约束</h3>
          <div className="grid grid-cols-5 gap-3 text-sm">
            {PRINCIPLES.map(([n, t, d]) => (
              <div key={n} className="p-3 rounded-xl bg-white border border-[#eadfd8]">
                <div className="flex items-center gap-2 mb-2">
                  <div className="w-6 h-6 rounded-full bg-[#dc2626] text-white text-xs font-bold flex items-center justify-center">
                    {n}
                  </div>
                  <span className="text-sm font-semibold text-[#171212]">{t}</span>
                </div>
                <div className="text-xs muted leading-5">{d}</div>
              </div>
            ))}
          </div>
        </div>}
      </div>
    </AdminLayout>
  );
};

export default ApprovalPage;
