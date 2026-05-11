import React, { useEffect, useMemo, useState } from 'react';
import { useI18n } from '../contexts/I18nContext';
import { openclawConfigService } from '../services/openclawConfigService';
import type {
  OpenClawConfigBundle,
  OpenClawConfigCompilePreview,
  OpenClawConfigPlan,
  OpenClawConfigResource,
} from '../types/openclawConfig';

export type OpenClawInjectionMode = OpenClawConfigPlan['mode'] | 'archive';

interface OpenClawConfigPlanSectionProps {
  mode: OpenClawInjectionMode;
  bundleId?: number;
  resourceIds: number[];
  onModeChange: (mode: OpenClawInjectionMode) => void;
  onSelectionChange: (payload: { bundleId?: number; resourceIds: number[] }) => void;
  onPreviewChange: (preview: OpenClawConfigCompilePreview | null, state: { loading: boolean; error: string | null }) => void;
}

const OpenClawConfigPlanSection: React.FC<OpenClawConfigPlanSectionProps> = ({
  mode,
  bundleId,
  resourceIds,
  onModeChange,
  onSelectionChange,
  onPreviewChange,
}) => {
  const { t } = useI18n();
  const [resources, setResources] = useState<OpenClawConfigResource[]>([]);
  const [bundles, setBundles] = useState<OpenClawConfigBundle[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const load = async () => {
      try {
        setLoading(true);
        const [resourceItems, bundleItems] = await Promise.all([
          openclawConfigService.listResources(),
          openclawConfigService.listBundles(),
        ]);
        setResources(resourceItems.filter((item) => item.enabled));
        setBundles(bundleItems.filter((item) => item.enabled));
      } finally {
        setLoading(false);
      }
    };

    load();
  }, []);

  const channelResources = useMemo(
    () => resources.filter((resource) => resource.resource_type === 'channel'),
    [resources],
  );

  useEffect(() => {
    let cancelled = false;

    const compile = async () => {
      if (mode === 'none' || mode === 'archive') {
        onPreviewChange(null, { loading: false, error: null });
        return;
      }
      if (mode === 'bundle' && !bundleId) {
        onPreviewChange(null, { loading: false, error: t('openClawInjectionSection.errors.chooseBundle') });
        return;
      }
      if (mode === 'manual' && resourceIds.length === 0) {
        onPreviewChange(null, { loading: false, error: t('openClawInjectionSection.errors.chooseResource') });
        return;
      }

      try {
        onPreviewChange(null, { loading: true, error: null });
        const payload: OpenClawConfigPlan = mode === 'bundle'
          ? { mode: 'bundle', bundle_id: bundleId }
          : { mode: 'manual', resource_ids: resourceIds };
        const result = await openclawConfigService.compilePreview(payload);
        if (!cancelled) {
          onPreviewChange(result, { loading: false, error: null });
        }
      } catch (err: any) {
        if (!cancelled) {
          onPreviewChange(null, { loading: false, error: err.response?.data?.error || t('openClawInjectionSection.errors.compileFailed') });
        }
      }
    };

    compile();
    return () => {
      cancelled = true;
    };
  }, [bundleId, mode, onPreviewChange, resourceIds]);

  return (
    <div className="app-panel p-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-lg font-medium text-gray-900">{t('openClawInjectionSection.title')}</h2>
          <p className="mt-1 text-sm text-gray-500">
            {t('openClawInjectionSection.subtitle')}
          </p>
        </div>
        <span className="rounded-full bg-indigo-50 px-3 py-1 text-xs font-medium text-indigo-600">{t('openClawInjectionSection.optional')}</span>
      </div>

      <div className="mt-5 grid grid-cols-1 gap-3 md:grid-cols-3">
        {[
          { value: 'manual', label: t('openClawInjectionSection.modes.manual') },
          { value: 'bundle', label: t('openClawInjectionSection.modes.bundle') },
          { value: 'archive', label: t('openClawInjectionSection.modes.archive') },
        ].map((item) => (
          <button
            key={item.value}
            type="button"
            onClick={() => onModeChange(mode === item.value ? 'none' : item.value as OpenClawInjectionMode)}
            className={`rounded-2xl border px-4 py-3 text-left transition ${
              mode === item.value ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-gray-200 bg-white text-gray-700 hover:border-gray-300'
            }`}
          >
            <div className="font-medium">{item.label}</div>
          </button>
        ))}
      </div>

      {loading && <div className="mt-5 text-sm text-gray-500">{t('openClawInjectionSection.loading')}</div>}

      {!loading && mode === 'bundle' && (
        <div className="mt-5">
          <label className="block text-sm font-medium text-gray-700">{t('openClawInjectionSection.chooseBundle')}</label>
          <select
            value={bundleId || ''}
            onChange={(e) => onSelectionChange({ bundleId: e.target.value ? Number(e.target.value) : undefined, resourceIds: [] })}
            className="app-input mt-1 w-full"
          >
            <option value="">{t('openClawInjectionSection.selectBundle')}</option>
            {bundles.map((bundle) => (
              <option key={bundle.id} value={bundle.id}>
                {bundle.name} ({t('openClawInjectionSection.bundleOptionCount', { count: bundle.items.length })})
              </option>
            ))}
          </select>
        </div>
      )}

      {!loading && mode === 'manual' && (
        <div className="mt-5 space-y-5">
          <div>
            <div className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-[#2563eb]">{t('openClawInjectionSection.channel')}</div>
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              {channelResources.map((item) => {
                const checked = resourceIds.includes(item.id);
                return (
                  <label key={item.id} className={`flex cursor-pointer items-start gap-3 rounded-2xl border px-4 py-3 ${checked ? 'border-indigo-300 bg-indigo-50' : 'border-gray-200 bg-white'}`}>
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(e) => onSelectionChange({
                        bundleId: undefined,
                        resourceIds: e.target.checked
                          ? [...resourceIds, item.id]
                          : resourceIds.filter((value) => value !== item.id),
                      })}
                    />
                    <span>
                      <span className="block font-medium text-gray-900">{item.name}</span>
                      <span className="mt-1 block text-xs text-gray-500">{item.resource_key}</span>
                    </span>
                  </label>
                );
              })}
              {channelResources.length === 0 && (
                <div className="rounded-2xl border border-dashed border-gray-300 px-4 py-3 text-sm text-gray-500">
                  {t('openClawInjectionSection.noChannelResources')}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default OpenClawConfigPlanSection;
