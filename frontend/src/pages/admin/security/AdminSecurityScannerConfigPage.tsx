import React, { useEffect, useMemo, useState } from 'react';
import { useI18n } from '../../../contexts/I18nContext';
import { adminService, type SecurityScanConfig } from '../../../services/adminService';
import { modelService, type LLMModel } from '../../../services/modelService';
import { Badge, SecurityCenterShell, useSecurityCenterData } from './securityCenterShared';

const AdminSecurityScannerConfigPage: React.FC = () => {
  const { t } = useI18n();
  const { config, setConfig, error, setError, summary } = useSecurityCenterData();
  const [draftConfig, setDraftConfig] = useState<SecurityScanConfig | null>(null);
  const [isDirty, setIsDirty] = useState(false);
  const [savingConfig, setSavingConfig] = useState(false);
  const [models, setModels] = useState<LLMModel[]>([]);
  const [showLLMAPIKey, setShowLLMAPIKey] = useState(false);
  const [showMetaLLMAPIKey, setShowMetaLLMAPIKey] = useState(false);

  useEffect(() => {
    if (!config) {
      setDraftConfig(null);
      setIsDirty(false);
      return;
    }
    if (!isDirty) {
      setDraftConfig(config);
    }
  }, [config, isDirty]);

  useEffect(() => {
    let cancelled = false;
    const loadModels = async () => {
      try {
        const items = await modelService.getModels();
        if (!cancelled) {
          setModels(items.filter((item) => item.is_active));
        }
      } catch {
        if (!cancelled) {
          setModels([]);
        }
      }
    };
    void loadModels();
    return () => {
      cancelled = true;
    };
  }, []);

  const llmModelOptions = useMemo(() => models.map((item) => ({
    id: String(item.id ?? `${item.provider_type}:${item.provider_model_name}`),
    label: `${item.display_name} · ${item.provider_model_name}`,
    model: item,
  })), [models]);
  const analyzerOptions = useMemo(() => ([
    { value: 'static', label: 'Static', description: t('securityCenter.scanner.analyzers.static') },
    { value: 'bytecode', label: 'Bytecode', description: t('securityCenter.scanner.analyzers.bytecode') },
    { value: 'pipeline', label: 'Pipeline', description: t('securityCenter.scanner.analyzers.pipeline') },
    { value: 'behavioral', label: 'Behavioral', description: t('securityCenter.scanner.analyzers.behavioral') },
    { value: 'llm', label: 'LLM', description: t('securityCenter.scanner.analyzers.llm') },
    { value: 'meta', label: 'Meta', description: t('securityCenter.scanner.analyzers.meta') },
  ]), [t]);

  const handleSaveConfig = async () => {
    if (!draftConfig) {
      return;
    }
    try {
      setSavingConfig(true);
      setError(null);
      const saved = await adminService.saveSecurityConfig(draftConfig);
      setDraftConfig(saved);
      setIsDirty(false);
      setConfig(saved);
    } catch (err: any) {
      setError(err.response?.data?.error || t('securityCenter.errors.saveConfig'));
    } finally {
      setSavingConfig(false);
    }
  };

  return (
    <SecurityCenterShell summary={summary}>
      <div className="space-y-6">
        {error ? (
          <div className="rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
        ) : null}

        <section className="rounded-[28px] border border-[#dbe4f0] bg-white p-6 shadow-[0_24px_60px_-42px_rgba(30,64,175,0.35)]">
            <div className="flex items-center justify-between gap-4">
              <div>
                <h2 className="text-lg font-semibold text-[#171212]">{t('securityCenter.scanner.title')}</h2>
                <p className="mt-1 text-sm text-[#8f8681]">{t('securityCenter.scanner.subtitle')}</p>
              </div>
              <button
                type="button"
                onClick={handleSaveConfig}
                disabled={!config || savingConfig}
                className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {savingConfig ? t('securityCenter.scanner.saving') : t('securityCenter.scanner.saveAndApply')}
              </button>
            </div>

            {draftConfig ? (
              <div className="mt-6 space-y-5">
                <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-sm font-semibold text-[#171212]">{t('securityCenter.scanner.serviceStatus')}</div>
                      <div className="mt-1 text-xs text-[#8f8681]">
                        {t('securityCenter.scanner.namespaceDeployment', {
                          namespace: draftConfig.skill_scanner_config.namespace,
                          deployment: draftConfig.skill_scanner_config.deployment_name,
                        })}
                      </div>
                    </div>
                    <Badge tone={draftConfig.scanner_status.connected ? 'green' : 'slate'}>
                      {draftConfig.scanner_status.connected ? t('securityCenter.dashboard.connected') : t('securityCenter.dashboard.disconnected')}
                    </Badge>
                  </div>
                  <div className="mt-3 text-sm text-[#6f6661]">{draftConfig.scanner_status.status_label}</div>
                  <div className="mt-3 flex flex-wrap gap-2">
                    {draftConfig.scanner_status.available_capabilities.map((item) => (
                      <span
                        key={item}
                        className="inline-flex rounded-full border border-[#d9e0e7] bg-[#f6f8fb] px-3 py-1 text-xs font-semibold text-[#556070]"
                      >
                        {item}
                      </span>
                    ))}
                  </div>
                </div>

                <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                  <div className="text-sm font-semibold text-[#171212]">{t('securityCenter.scanner.llmConfig')}</div>
                  <p className="mt-1 text-xs leading-5 text-[#8f8681]">
                    {t('securityCenter.scanner.llmConfigDesc')}
                  </p>
                  <div className="mt-4 grid grid-cols-1 gap-4">
                    <ModelImportField
                      label={t('securityCenter.scanner.primaryLlmIntegration')}
                      helper={t('securityCenter.scanner.primaryLlmIntegrationHelp')}
                      options={llmModelOptions}
                      onSelect={(model) => {
                        setIsDirty(true);
                        setDraftConfig((current) =>
                          current
                            ? {
                                ...current,
                                skill_scanner_config: {
                                  ...current.skill_scanner_config,
                                  llm_api_key: model.api_key ?? '',
                                  llm_model: model.provider_model_name,
                                  llm_base_url: model.base_url,
                                },
                              }
                            : current,
                        );
                      }}
                    />
                    <ConfigField
                      label="LLM API Key"
                      helper={t('securityCenter.scanner.llmApiKeyHelp')}
                      value={draftConfig.skill_scanner_config.llm_api_key}
                      type={showLLMAPIKey ? 'text' : 'password'}
                      revealable
                      revealed={showLLMAPIKey}
                      onToggleReveal={() => setShowLLMAPIKey((current) => !current)}
                      onChange={(value) => {
                        setIsDirty(true);
                        setDraftConfig((current) =>
                          current
                            ? { ...current, skill_scanner_config: { ...current.skill_scanner_config, llm_api_key: value } }
                            : current,
                        );
                      }}
                    />
                    <ConfigField
                      label="LLM Model"
                      helper={t('securityCenter.scanner.llmModelHelp')}
                      value={draftConfig.skill_scanner_config.llm_model}
                      onChange={(value) => {
                        setIsDirty(true);
                        setDraftConfig((current) =>
                          current
                            ? { ...current, skill_scanner_config: { ...current.skill_scanner_config, llm_model: value } }
                            : current,
                        );
                      }}
                    />
                    <ConfigField
                      label="LLM Base URL"
                      helper={t('securityCenter.scanner.llmBaseUrlHelp')}
                      value={draftConfig.skill_scanner_config.llm_base_url}
                      onChange={(value) => {
                        setIsDirty(true);
                        setDraftConfig((current) =>
                          current
                            ? { ...current, skill_scanner_config: { ...current.skill_scanner_config, llm_base_url: value } }
                            : current,
                        );
                      }}
                    />
                    <ModelImportField
                      label={t('securityCenter.scanner.metaLlmIntegration')}
                      helper={t('securityCenter.scanner.metaLlmIntegrationHelp')}
                      options={llmModelOptions}
                      onSelect={(model) => {
                        setIsDirty(true);
                        setDraftConfig((current) =>
                          current
                            ? {
                                ...current,
                                skill_scanner_config: {
                                  ...current.skill_scanner_config,
                                  meta_llm_api_key: model.api_key ?? '',
                                  meta_llm_model: model.provider_model_name,
                                  meta_llm_base_url: model.base_url,
                                },
                              }
                            : current,
                        );
                      }}
                    />
                    <ConfigField
                      label="Meta LLM API Key"
                      helper={t('securityCenter.scanner.metaLlmApiKeyHelp')}
                      value={draftConfig.skill_scanner_config.meta_llm_api_key}
                      type={showMetaLLMAPIKey ? 'text' : 'password'}
                      revealable
                      revealed={showMetaLLMAPIKey}
                      onToggleReveal={() => setShowMetaLLMAPIKey((current) => !current)}
                      onChange={(value) => {
                        setIsDirty(true);
                        setDraftConfig((current) =>
                          current
                            ? { ...current, skill_scanner_config: { ...current.skill_scanner_config, meta_llm_api_key: value } }
                            : current,
                        );
                      }}
                    />
                    <ConfigField
                      label="Meta LLM Model"
                      helper={t('securityCenter.scanner.metaLlmModelHelp')}
                      value={draftConfig.skill_scanner_config.meta_llm_model}
                      onChange={(value) => {
                        setIsDirty(true);
                        setDraftConfig((current) =>
                          current
                            ? { ...current, skill_scanner_config: { ...current.skill_scanner_config, meta_llm_model: value } }
                            : current,
                        );
                      }}
                    />
                    <ConfigField
                      label="Meta LLM Base URL"
                      helper={t('securityCenter.scanner.metaLlmBaseUrlHelp')}
                      value={draftConfig.skill_scanner_config.meta_llm_base_url}
                      onChange={(value) => {
                        setIsDirty(true);
                        setDraftConfig((current) =>
                          current
                            ? { ...current, skill_scanner_config: { ...current.skill_scanner_config, meta_llm_base_url: value } }
                            : current,
                        );
                      }}
                    />
                  </div>
                </div>

                <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                  <div className="text-sm font-semibold text-[#171212]">{t('securityCenter.scanner.currentMode')}</div>
                  <p className="mt-1 text-xs leading-5 text-[#8f8681]">
                    {t('securityCenter.scanner.currentModeDesc')}
                  </p>
                  <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
                    {[
                      { value: 'quick', label: t('securityCenter.scanner.quickMode'), description: t('securityCenter.scanner.quickModeDesc') },
                      { value: 'deep', label: t('securityCenter.scanner.deepMode'), description: t('securityCenter.scanner.deepModeDesc') },
                    ].map((option) => {
                      const checked = (draftConfig.active_mode || draftConfig.default_mode) === option.value;
                      return (
                        <label
                          key={option.value}
                          className={`flex cursor-pointer items-start gap-3 rounded-2xl border px-4 py-4 transition ${
                            checked ? 'border-[#9fc7ee] bg-[#eef7ff]' : 'border-[#dbe4f0] bg-white'
                          }`}
                        >
                          <input
                            type="radio"
                            name="active-scan-mode"
                            checked={checked}
                            onChange={() => {
                              setIsDirty(true);
                              setDraftConfig((current) =>
                                current ? { ...current, active_mode: option.value, default_mode: option.value } : current,
                              );
                            }}
                            className="mt-1 h-4 w-4 border-[#bfdbfe] text-[#2563eb] focus:ring-[#2563eb]"
                          />
                          <div>
                            <div className="text-sm font-semibold text-[#171212]">{option.label}</div>
                            <div className="mt-1 text-xs leading-5 text-[#8f8681]">{option.description}</div>
                          </div>
                        </label>
                      );
                    })}
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
                  <ScanMethodCard
                    title={t('securityCenter.scanModes.quick')}
                    timeout={draftConfig.quick_timeout_seconds}
                    analyzers={draftConfig.quick_analyzers}
                    analyzerOptions={analyzerOptions}
                    onTimeoutChange={(value) => {
                      setIsDirty(true);
                      setDraftConfig((current) => (current ? { ...current, quick_timeout_seconds: value } : current));
                    }}
                    onToggleAnalyzer={(analyzer) => {
                      setIsDirty(true);
                      setDraftConfig((current) =>
                        current
                          ? {
                              ...current,
                              quick_analyzers: toggleAnalyzer(current.quick_analyzers, analyzer),
                            }
                          : current,
                      );
                    }}
                  />
                  <ScanMethodCard
                    title={t('securityCenter.scanModes.deep')}
                    timeout={draftConfig.deep_timeout_seconds}
                    analyzers={draftConfig.deep_analyzers}
                    analyzerOptions={analyzerOptions}
                    onTimeoutChange={(value) => {
                      setIsDirty(true);
                      setDraftConfig((current) => (current ? { ...current, deep_timeout_seconds: value } : current));
                    }}
                    onToggleAnalyzer={(analyzer) => {
                      setIsDirty(true);
                      setDraftConfig((current) =>
                        current
                          ? {
                              ...current,
                              deep_analyzers: toggleAnalyzer(current.deep_analyzers, analyzer),
                            }
                          : current,
                      );
                    }}
                  />
                </div>
                <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] px-4 py-4 text-sm leading-6 text-[#6f6661]">
                  {t('securityCenter.scanner.applySummary')}
                </div>
              </div>
            ) : (
              <div className="mt-6 text-sm text-[#8f8681]">{t('securityCenter.scanner.loadingConfig')}</div>
            )}
        </section>
      </div>
    </SecurityCenterShell>
  );
};

function ScanMethodCard({
  title,
  timeout,
  analyzers,
  analyzerOptions,
  onTimeoutChange,
  onToggleAnalyzer,
}: {
  title: string;
  timeout: number;
  analyzers: string[];
  analyzerOptions: Array<{ value: string; label: string; description: string }>;
  onTimeoutChange: (value: number) => void;
  onToggleAnalyzer: (value: string) => void;
}) {
  const { t } = useI18n();
  const quickLabel = t('securityCenter.scanModes.quick');
  return (
    <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
      <div className="text-sm font-semibold text-[#171212]">{title}</div>
      <div className="mt-4">
        <label className="block text-sm font-medium text-gray-700">{t('securityCenter.scanner.timeoutSeconds')}</label>
        <input
          type="number"
          min={title === quickLabel ? 5 : 10}
          value={timeout}
          onChange={(event) => onTimeoutChange(Number(event.target.value) || (title === quickLabel ? 30 : 120))}
          className="app-input mt-1 w-full"
        />
      </div>
      <div className="mt-4">
        <div className="text-sm font-medium text-gray-700">{t('securityCenter.scanner.invocationMethod')}</div>
        <div className="mt-3 space-y-3">
          {analyzerOptions.map((option) => {
            const checked = analyzers.includes(option.value);
            return (
              <label
                key={`${title}-${option.value}`}
                className="flex cursor-pointer items-start gap-3 rounded-2xl border border-[#dbe4f0] bg-white px-4 py-3"
              >
                <input
                  type="checkbox"
                  checked={checked}
                  onChange={() => onToggleAnalyzer(option.value)}
                  className="mt-1 h-4 w-4 rounded border-[#bfdbfe] text-[#2563eb] focus:ring-[#2563eb]"
                />
                <div>
                  <div className="text-sm font-semibold text-[#171212]">{option.label}</div>
                  <div className="mt-1 text-xs leading-5 text-[#8f8681]">{option.description}</div>
                </div>
              </label>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function ConfigField({
  label,
  helper,
  value,
  onChange,
  type = 'text',
  revealable = false,
  revealed = false,
  onToggleReveal,
}: {
  label: string;
  helper: string;
  value: string;
  onChange: (value: string) => void;
  type?: 'text' | 'password';
  revealable?: boolean;
  revealed?: boolean;
  onToggleReveal?: () => void;
}) {
  const { t } = useI18n();
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700">{label}</label>
      <p className="mt-1 text-xs text-[#8f8681]">{helper}</p>
      <div className="mt-2 flex items-center gap-3">
        <input
          type={type}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          className="app-input w-full"
        />
        {revealable ? (
          <button
            type="button"
            onClick={onToggleReveal}
            className="inline-flex min-w-[68px] justify-center rounded-xl border border-[#dbe4f0] bg-white px-3 py-2 text-sm font-medium text-[#6f6661] transition hover:border-[#d6c7be] hover:bg-[#f8fbff]"
          >
            {revealed ? t('securityCenter.scanner.hide') : t('securityCenter.scanner.reveal')}
          </button>
        ) : null}
      </div>
    </div>
  );
}

function ModelImportField({
  label,
  helper,
  options,
  onSelect,
}: {
  label: string;
  helper: string;
  options: Array<{ id: string; label: string; model: LLMModel }>;
  onSelect: (model: LLMModel) => void;
}) {
  const { t } = useI18n();
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700">{label}</label>
      <p className="mt-1 text-xs text-[#8f8681]">{helper}</p>
      <select
        defaultValue=""
        onChange={(event) => {
          const selected = options.find((item) => item.id === event.target.value);
          if (selected) {
            onSelect(selected.model);
          }
        }}
        className="app-input mt-2 w-full"
      >
        <option value="">{t('securityCenter.scanner.selectAiGatewayModel')}</option>
        {options.map((option) => (
          <option key={option.id} value={option.id}>
            {option.label}
          </option>
        ))}
      </select>
    </div>
  );
}

function toggleAnalyzer(items: string[], target: string): string[] {
  if (items.includes(target)) {
    return items.filter((item) => item !== target);
  }
  return [...items, target];
}

export default AdminSecurityScannerConfigPage;
