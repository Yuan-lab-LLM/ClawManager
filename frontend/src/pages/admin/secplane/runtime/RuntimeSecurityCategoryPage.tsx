import React from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import { useI18n } from '../../../../contexts/I18nContext';

interface Scenario {
  letter: string;
  labelKey: string;
  sub: string;
  descKey: string;
  route: string;
  defenses: string;
}

const SCENARIOS: Scenario[] = [
  {
    letter: 'A',
    labelKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.input.label',
    sub: 'INPUT SURFACE',
    descKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.input.desc',
    route: '/admin/secplane/runtime/input',
    defenses: 'UserRiskScan · PromptGuard · ToolCallEnforcement · ToolResultScan',
  },
  {
    letter: 'B',
    labelKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.state.label',
    sub: 'STATE SURFACE',
    descKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.state.desc',
    route: '/admin/secplane/runtime/state',
    defenses: 'MemoryGuard · SelfProtection',
  },
  {
    letter: 'C',
    labelKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.decision.label',
    sub: 'DECISION SURFACE',
    descKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.decision.desc',
    route: '/admin/secplane/runtime/decision',
    defenses: 'CommandBlock · EncodingGuard · ScriptProvenanceGuard',
  },
  {
    letter: 'D',
    labelKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.output.label',
    sub: 'OUTPUT SURFACE',
    descKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.output.desc',
    route: '/admin/secplane/runtime/output',
    defenses: 'OutputRedaction',
  },
  {
    letter: 'F',
    labelKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.asset.label',
    sub: 'ASSET PROTECTION',
    descKey: 'secplane.runtime.runtimeSecurityCategory.scenarios.asset.desc',
    route: '/admin/secplane/runtime/asset',
    defenses: 'SelfProtection · ProtectedPaths/Skills/Plugins',
  },
];

const RuntimeSecurityCategoryPage: React.FC = () => {
  const { t } = useI18n();
  return (
    <AdminLayout title={t('secplane.runtime.shared.crumbSecurity')}>
      <div className="secp-scope space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('secplane.runtime.shared.crumbSecurity')}</Link>
          <span>/</span>
          <span className="crumb-current">{t('secplane.runtime.runtimeSecurityCategory.crumbCurrent')}</span>
        </div>

        {/* Hero */}
        <div className="panel">
          <div className="hero-block mb-5">
            <div className="h-eyebrow">{t('secplane.runtime.runtimeSecurityCategory.heroEyebrow')}</div>
            <h2 className="h-title">{t('secplane.runtime.runtimeSecurityCategory.heroTitle')}</h2>
            <p className="h-subtitle">
              {t('secplane.runtime.runtimeSecurityCategory.heroSubtitle1')}
              {t('secplane.runtime.runtimeSecurityCategory.heroSubtitle2')}
              <code className="mx-1 px-1 py-0.5 rounded bg-[#fdf6f1] text-[#7a4a30] text-xs">install_skill</code>
              {t('secplane.runtime.runtimeSecurityCategory.heroSubtitle3')}
            </p>
          </div>
          <div className="grid grid-cols-4 gap-3">
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.runtimeSecurityCategory.statScenarios')}</div>
              <div className="stat-card-value">{SCENARIOS.length}</div>
              <div className="stat-card-sub muted-strong">{t('secplane.runtime.runtimeSecurityCategory.statScenariosSub')}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.runtimeSecurityCategory.statDefenses')}</div>
              <div className="stat-card-value">14</div>
              <div className="stat-card-sub muted-strong">ClawAegisEx defense</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statChannel')}</div>
              <div className="stat-card-value tone-green">install_skill</div>
              <div className="stat-card-sub muted-strong">bundle → workspace → hot-reload</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.runtimeSecurityCategory.statCoverage')}</div>
              <div className="stat-card-value tone-green">100%</div>
              <div className="stat-card-sub muted-strong">{t('secplane.runtime.runtimeSecurityCategory.statCoverageSub')}</div>
            </div>
          </div>
        </div>

        {/* Scenario cards */}
        <div className="grid grid-cols-2 gap-4">
          {SCENARIOS.map((s) => (
            <Link key={s.letter} to={s.route} className="scenario-card">
              <div className="flex items-start gap-3 mb-3">
                <div
                  style={{
                    width: 36,
                    height: 36,
                    borderRadius: 12,
                    background: 'linear-gradient(135deg, #fdf6f1, #f5e9df)',
                    border: '1px solid #eadfd8',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    fontSize: '1rem',
                    fontWeight: 700,
                    color: '#7d5744',
                    flexShrink: 0,
                  }}
                >
                  {s.letter}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="eyebrow" style={{ color: '#ef6b4a' }}>{s.sub}</div>
                  <div className="text-lg font-bold text-[#171212]">{t(s.labelKey)}</div>
                  <div className="text-xs muted mt-1">{t(s.descKey)}</div>
                </div>
              </div>
              <div className="divider"></div>
              <div className="flex items-center justify-between text-xs">
                <span className="muted-strong" style={{ fontFamily: 'ui-monospace, monospace', fontSize: '0.6875rem' }}>
                  {s.defenses}
                </span>
                <span style={{ color: '#ef6b4a', fontWeight: 600 }}>{t('secplane.runtime.runtimeSecurityCategory.viewDetail')}</span>
              </div>
            </Link>
          ))}
        </div>
      </div>
    </AdminLayout>
  );
};

export default RuntimeSecurityCategoryPage;
