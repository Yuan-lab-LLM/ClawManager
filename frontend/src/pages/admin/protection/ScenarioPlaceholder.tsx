import React from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../components/AdminLayout';
import { CATEGORIES, SCENARIOS } from './_data';
import { useI18n } from '../../../contexts/I18nContext';

export type ScenarioPlaceholderProps = { scenarioId: string };

// 用于尚未做完的 scenario 页 — 显示场景元信息 + "本期未开放" 提示 + 返回类目入口
const ScenarioPlaceholder: React.FC<ScenarioPlaceholderProps> = ({ scenarioId }) => {
  const { t } = useI18n();
  const sc = SCENARIOS.find((s) => s.id === scenarioId);
  if (!sc) {
    return (
      <AdminLayout>
        <div className="cm-content">
          <div className="panel">{t('secplane.protection.category.unknownScenario', { id: scenarioId })}</div>
        </div>
      </AdminLayout>
    );
  }
  const cat = CATEGORIES.find((c) => c.id === sc.cat);
  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('nav.secplane')}</Link>
          <span>/</span>
          {cat && <Link to={cat.path}>{cat.labelKey ? t(cat.labelKey) : cat.label}</Link>}
          {cat && <span>/</span>}
          <span className="crumb-current">{sc.label}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t('secplane.protection.category.scenarioCode', { code: sc.code })}</div>
            <h2 className="h-title">{sc.label}</h2>
            <p className="h-subtitle">{sc.subtitle}</p>
          </div>
          <div className="divider my-5" />
          <div className="flex items-center gap-3">
            <span className="badge badge-slate">{t('secplane.protection.category.underConstruction')}</span>
            <span className="text-sm muted">
              {t('secplane.protection.category.scenarioPlaceholderDesc')}
            </span>
          </div>
          <div className="flex flex-wrap gap-2 mt-5">
            {cat && (
              <Link to={cat.path} className="btn-secondary btn-sm" style={{ textDecoration: 'none' }}>
                ← {t('secplane.protection.category.backTo')} {cat.labelKey ? t(cat.labelKey) : cat.label}
              </Link>
            )}
            <Link to="/admin/secplane" className="btn-secondary btn-sm" style={{ textDecoration: 'none' }}>
              {t('secplane.protection.category.overview')}
            </Link>
          </div>
        </div>
      </div>
    </AdminLayout>
  );
};

export default ScenarioPlaceholder;
