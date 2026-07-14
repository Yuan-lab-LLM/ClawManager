import React from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../components/AdminLayout';
import { CATEGORIES, getScenariosByCategory } from './_data';
import { useSecurityCenterData } from '../security/securityCenterShared';
import { useI18n } from '../../../contexts/I18nContext';

// cat-4 (数据与组件可信) 用 SKILL 技能扫描真实数据；其他类目仍走静态 mock
const CatTrustLiveStats: React.FC = () => {
  const { t } = useI18n();
  const k = 'secplane.protection.category';
  const { summary, loading } = useSecurityCenterData();
  const total = summary.total;
  const pct = total > 0 ? Math.round((summary.completed / total) * 100) : 0;
  return (
    <div className="grid grid-cols-4 gap-3">
      <div className="stat-card">
        <div className="stat-card-label">{t(`${k}.cat4Stat1Label`)}</div>
        <div className="stat-card-value">{loading ? '…' : total}</div>
        <div className="stat-card-sub muted-strong">{t(`${k}.cat4Stat1Sub`)}</div>
      </div>
      <div className="stat-card">
        <div className="stat-card-label">{t(`${k}.cat4Stat2Label`)}</div>
        <div className={`stat-card-value ${summary.highRisk > 0 ? 'tone-red' : 'tone-green'}`}>
          {loading ? '…' : summary.highRisk}
        </div>
        <div className="stat-card-sub muted-strong">{t(`${k}.cat4Stat2Sub`)}</div>
      </div>
      <div className="stat-card">
        <div className="stat-card-label">{t(`${k}.cat4Stat3Label`)}</div>
        <div className={`stat-card-value ${summary.mediumRisk > 0 ? 'tone-orange' : ''}`}>
          {loading ? '…' : summary.mediumRisk}
        </div>
        <div className="stat-card-sub muted-strong">{t(`${k}.cat4Stat3Sub`)}</div>
      </div>
      <div className="stat-card">
        <div className="stat-card-label">{t(`${k}.cat4Stat4Label`)}</div>
        <div className={`stat-card-value ${pct === 100 && total > 0 ? 'tone-green' : ''}`}>
          {loading ? '…' : total > 0 ? `${summary.completed}/${total}` : '0'}
        </div>
        <div className="stat-card-sub muted-strong">{t(`${k}.cat4Stat4Sub`, { pct })}</div>
      </div>
    </div>
  );
};

// 每个类目页头部的统计卡 (mock，后续接 secplaneService stats API)
// cat-4 不在表里——用上面的 CatTrustLiveStats 走真实接口
const CAT_STATS: Record<string, { label: string; value: string; tone?: string; sub: string }[]> = {
  'cat-1': [
    { label: '场景数', value: '6', sub: '六层运行时防护' },
    { label: '24h 拦截', value: '624', tone: 'tone-red', sub: '含输入/工具/输出' },
    { label: '防护层数', value: '5', sub: '五层纵深防御' },
    { label: '规则覆盖', value: '100%', tone: 'tone-green', sub: '运行时全链路' },
  ],
  'cat-2': [
    { label: '场景数', value: '1', sub: '统一身份与权限' },
    { label: '24h 权限拒绝', value: '12', tone: 'tone-red', sub: 'RBAC + 最小权限' },
    { label: '受管身份', value: '48', sub: '智能体 + agent token' },
    { label: '过期策略', value: '24h', tone: 'tone-green', sub: 'agent token 默认 TTL' },
  ],
  'cat-6': [],  // rendered via t() below
  'cat-5': [
    { label: '场景数', value: '2', sub: '熔断 + 全链路审计' },
    { label: '24h 熔断', value: '3', tone: 'tone-red', sub: '应急处置' },
    { label: '审计事件', value: '3.2K', sub: '24h 跨产品' },
    { label: '健康分', value: '92', tone: 'tone-green', sub: '运营驾驶舱' },
  ],
  'cat-7': [
    { label: '场景数', value: '1', sub: '策略治理' },
    { label: '模板', value: '14', sub: '内置策略模板' },
    { label: '已应用实例', value: '124', sub: '策略下发' },
    { label: '配置版本', value: 'r4', tone: 'tone-green', sub: '最新策略修订' },
  ],
  'cat-3': [
    { label: '场景数', value: '2', sub: '协同治理 + 配额限制' },
    { label: '当前总线', value: 'Redis Stream', tone: 'tone-orange', sub: 'leader_mediated Team' },
    { label: '关键风险', value: '4', tone: 'tone-red', sub: '伪造 / 窃听 / DoS / 额度失控' },
    { label: '治理目标', value: 'ACL + Quota', tone: 'tone-green', sub: '最小权限 + Tokens 治理' },
  ],
};

const CAT_DESC: Record<string, string> = {
  'cat-1': '面向单智能体从初始化、输入、推理、决策到执行的完整运行时链路，覆盖输入面、状态面、决策面、输出面、资产保护、人因审批 6 个场景，构建运行时层纵深防御主链路。',
  'cat-4': '面向智能体所依赖的技能、插件、镜像等供应链：六分析器并行扫描代码安全 + 凭据外泄 + IOC 命中 + LLM 语义复核，识别高危技能、阻断不可信组件落地。',
  'cat-2': '智能体身份签发、调用授权、最小权限策略；agent token 生命周期管理，强制最小权限访问。',
  'cat-5': '面向运营与合规的审计回溯与应急处置：全链路事件聚合、风险评分、熔断处置、运营驾驶舱。',
  'cat-7': '统一策略中心：策略模板、版本管理、灰度发布与回滚；批量应用到实例与命名空间。',
  'cat-3': '面向 Team 多智能体协作链路与 AI Gateway 通信平面：围绕 Redis Stream 与网关 tokens 使用补齐接入认证、ACL、Relay 中转、配额限制、禁言熔断与审计回放，避免“内部成员默认互信 + tokens 无限消耗”的治理空洞。',
};

export type CategoryPageProps = { catId: string };

const CategoryPage: React.FC<CategoryPageProps> = ({ catId }) => {
  const { t } = useI18n();
  const cat = CATEGORIES.find((c) => c.id === catId);
  if (!cat) {
    return (
      <AdminLayout>
        <div className="cm-content">
          <div className="panel">{t('secplane.protection.category.unknown', { id: catId })}</div>
        </div>
      </AdminLayout>
    );
  }
  // cat-6: 隐藏「容器隔离」场景卡（保留路由，不在 overview 露出入口）
  const scs = getScenariosByCategory(catId).filter(
    (s) => !(catId === 'cat-6' && s.id === 'k'),
  );

  // cat-6 stats/desc are i18n-ized; other categories still use static mock
  const k = 'secplane.protection.category';
  const i18nStatsIds: Record<string, { label: string; value: string; tone?: string; sub: string }[]> = {
    'cat-6': [
      { label: t(`${k}.cat6Stat1Label`), value: '1', sub: t(`${k}.cat6Stat1Sub`) },
      { label: t(`${k}.cat6Stat2Label`), value: '54', sub: t(`${k}.cat6Stat2Sub`) },
      { label: t(`${k}.cat6Stat3Label`), value: '11', tone: 'tone-red', sub: t(`${k}.cat6Stat3Sub`) },
      { label: t(`${k}.cat6Stat4Label`), value: 'CIS', tone: 'tone-green', sub: t(`${k}.cat6Stat4Sub`) },
    ],
    'cat-2': [
      { label: t(`${k}.cat2Stat1Label`), value: '1', sub: t(`${k}.cat2Stat1Sub`) },
      { label: t(`${k}.cat2Stat2Label`), value: '12', tone: 'tone-red', sub: t(`${k}.cat2Stat2Sub`) },
      { label: t(`${k}.cat2Stat3Label`), value: '48', sub: t(`${k}.cat2Stat3Sub`) },
      { label: t(`${k}.cat2Stat4Label`), value: '24h', tone: 'tone-green', sub: t(`${k}.cat2Stat4Sub`) },
    ],
    'cat-7': [
      { label: t(`${k}.cat7Stat1Label`), value: '1', sub: t(`${k}.cat7Stat1Sub`) },
      { label: t(`${k}.cat7Stat2Label`), value: '14', sub: t(`${k}.cat7Stat2Sub`) },
      { label: t(`${k}.cat7Stat3Label`), value: '124', sub: t(`${k}.cat7Stat3Sub`) },
      { label: t(`${k}.cat7Stat4Label`), value: 'r4', tone: 'tone-green', sub: t(`${k}.cat7Stat4Sub`) },
    ],
    'cat-5': [
      { label: t(`${k}.cat5Stat1Label`), value: '2', sub: t(`${k}.cat5Stat1Sub`) },
      { label: t(`${k}.cat5Stat2Label`), value: '3', tone: 'tone-red', sub: t(`${k}.cat5Stat2Sub`) },
      { label: t(`${k}.cat5Stat3Label`), value: '3.2K', sub: t(`${k}.cat5Stat3Sub`) },
      { label: t(`${k}.cat5Stat4Label`), value: '92', tone: 'tone-green', sub: t(`${k}.cat5Stat4Sub`) },
    ],
  };
  const stats = catId in i18nStatsIds ? i18nStatsIds[catId] : (CAT_STATS[catId] ?? []);
  const i18nDescIds = ['cat-6', 'cat-4', 'cat-2', 'cat-7', 'cat-5'];
  const desc = i18nDescIds.includes(catId) ? t(`${k}.${catId.replace('-', '')}Desc`) : (CAT_DESC[catId] ?? '');

  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('nav.secplane')}</Link>
          <span>/</span>
          <span className="crumb-current">{cat.labelKey ? t(cat.labelKey) : cat.label}</span>
        </div>

        <div className="panel">
          <div className="flex items-start justify-between gap-6 mb-5">
            <div className="hero-block flex-1">
              <div className="h-eyebrow">{cat.sub}</div>
              <h2 className="h-title">{cat.labelKey ? t(cat.labelKey) : cat.label}</h2>
              <p className="h-subtitle">{desc}</p>
            </div>
          </div>
          {catId === 'cat-4' ? (
            <CatTrustLiveStats />
          ) : (
            stats.length > 0 && (
              <div
                className="grid gap-3"
                style={{ gridTemplateColumns: `repeat(${stats.length}, minmax(0, 1fr))` }}
              >
                {stats.map((s, i) => (
                  <div key={i} className="stat-card">
                    <div className="stat-card-label">{s.label}</div>
                    <div className={`stat-card-value ${s.tone ?? ''}`}>{s.value}</div>
                    <div className="stat-card-sub muted-strong">{s.sub}</div>
                  </div>
                ))}
              </div>
            )
          )}
        </div>

        {scs.length > 0 ? (
          <div className="grid grid-cols-2 gap-4">
            {scs.map((s) => (
              <Link key={s.id} to={s.path} className="cat-overview-card">
                <div className="flex items-start gap-3 mb-3">
                  <div className="flex-1 min-w-0">
                    <div className="text-lg font-bold text-[#171212]">{s.labelKey ? t(s.labelKey) : s.label}</div>
                    <div className="text-xs muted mt-1">{s.subtitleKey ? t(s.subtitleKey) : s.subtitle}</div>
                  </div>
                </div>
                <div className="divider" />
                <div className="flex items-center justify-between text-xs">
                  <span className="muted-strong">{t('secplane.protection.category.clickToEnter')}</span>
                  <span style={{ color: cat.color, fontWeight: 600 }}>{t('secplane.protection.category.view')} →</span>
                </div>
              </Link>
            ))}
          </div>
        ) : (
          <div className="panel">
            <div className="text-sm muted">{t('secplane.protection.category.notOpenYet')}</div>
          </div>
        )}
      </div>
    </AdminLayout>
  );
};

export default CategoryPage;
