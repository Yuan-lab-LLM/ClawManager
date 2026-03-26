import React from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../components/AdminLayout';
import { useI18n } from '../../contexts/I18nContext';

const gatewaySections = [
  {
    titleKey: 'nav.models',
    descriptionKey: 'aiGatewayPage.modelsDescription',
    path: '/admin/models',
    accent: 'from-[#fff4ee] to-[#fffaf8]',
    icon: 'M12 2l8 4.5v5c0 5.8-3.6 10.8-8 12.5-4.4-1.7-8-6.7-8-12.5v-5L12 2zm0 6.5v3m0 4h.01',
  },
  {
    titleKey: 'nav.aiAudit',
    descriptionKey: 'aiGatewayPage.auditDescription',
    path: '/admin/ai-audit',
    accent: 'from-[#eef6ff] to-[#fffaf8]',
    icon: 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
  },
  {
    titleKey: 'nav.costs',
    descriptionKey: 'aiGatewayPage.costsDescription',
    path: '/admin/costs',
    accent: 'from-[#fff8ed] to-[#fffaf8]',
    icon: 'M12 8c-2.761 0-5 1.343-5 3s2.239 3 5 3 5 1.343 5 3-2.239 3-5 3m0-15c2.761 0 5 1.343 5 3m-5-3V4m0 16v-2m0-6c-2.761 0-5-1.343-5-3s2.239-3 5-3',
  },
  {
    titleKey: 'nav.riskRules',
    descriptionKey: 'aiGatewayPage.riskRulesDescription',
    path: '/admin/risk-rules',
    accent: 'from-[#f4f7ff] to-[#fffaf8]',
    icon: 'M12 2l7 4v6c0 5-3.5 9.5-7 10-3.5-.5-7-5-7-10V6l7-4zm0 6v4m0 4h.01',
  },
];

const AIGatewayPage: React.FC = () => {
  const { t } = useI18n();

  return (
    <AdminLayout title={t('nav.aiGateway')}>
      <div>
        <section className="grid gap-5 xl:grid-cols-2">
          {gatewaySections.map((section) => (
            <Link
              key={section.path}
              to={section.path}
              className={`rounded-[28px] border border-[#ead8cf] bg-gradient-to-br ${section.accent} p-6 shadow-[0_24px_60px_-42px_rgba(72,44,24,0.42)] transition-transform duration-200 hover:-translate-y-0.5 hover:shadow-[0_28px_70px_-40px_rgba(72,44,24,0.48)]`}
            >
              <div className="flex items-start justify-between gap-4">
                <div className="rounded-2xl border border-[#ead8cf] bg-white/80 p-3 text-[#b46c50] shadow-sm">
                  <svg className="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d={section.icon} />
                  </svg>
                </div>
                <span className="rounded-full border border-[#ead8cf] bg-white/85 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-[#b46c50]">
                  {t('common.open')}
                </span>
              </div>

              <h3 className="mt-5 text-xl font-semibold text-[#171212]">{t(section.titleKey)}</h3>
              <p className="mt-2 text-sm leading-7 text-[#6f625b]">{t(section.descriptionKey)}</p>
            </Link>
          ))}
        </section>
      </div>
    </AdminLayout>
  );
};

export default AIGatewayPage;
