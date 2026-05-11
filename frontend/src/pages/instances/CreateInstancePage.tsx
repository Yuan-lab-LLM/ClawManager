import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import OpenClawConfigPlanSection, { type OpenClawInjectionMode } from '../../components/OpenClawConfigPlanSection';
import UserLayout from '../../components/UserLayout';
import { useAuth } from '../../contexts/AuthContext';
import { instanceService } from '../../services/instanceService';
import { skillService } from '../../services/skillService';
import { userService } from '../../services/userService';
import { INSTANCE_TYPES, PRESET_CONFIGS } from '../../types/instance';
import type { CreateInstanceRequest, InstanceType } from '../../types/instance';
import type { Instance } from '../../types/instance';
import type { OpenClawConfigCompilePreview } from '../../types/openclawConfig';
import type { Skill } from '../../types/skill';
import type { UserQuota } from '../../types/user';
import { useI18n } from '../../contexts/I18nContext';
import { systemSettingsService } from '../../services/systemSettingsService';

const getApiErrorText = (error: unknown) => {
  if (typeof error !== 'object' || error === null || !('response' in error)) {
    return undefined;
  }

  const response = (error as { response?: { data?: { error?: unknown } } }).response;
  return typeof response?.data?.error === 'string' ? response.data.error : undefined;
};

const CreateInstancePage: React.FC = () => {
  const { user } = useAuth();
  const { t } = useI18n();
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [quotaLoading, setQuotaLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [step, setStep] = useState(1);
  const [submitArmed, setSubmitArmed] = useState(false);
  const [availableTypes, setAvailableTypes] = useState(INSTANCE_TYPES);
  const [quota, setQuota] = useState<UserQuota | null>(null);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [openClawImportFile, setOpenClawImportFile] = useState<File | null>(null);
  const [openClawInjectionMode, setOpenClawInjectionMode] = useState<OpenClawInjectionMode>('none');
  const [openClawBundleId, setOpenClawBundleId] = useState<number | undefined>(undefined);
  const [openClawResourceIds, setOpenClawResourceIds] = useState<number[]>([]);
  const [openClawPreview, setOpenClawPreview] = useState<OpenClawConfigCompilePreview | null>(null);
  const [openClawPreviewLoading, setOpenClawPreviewLoading] = useState(false);
  const [openClawPreviewError, setOpenClawPreviewError] = useState<string | null>(null);
  const [availableSkills, setAvailableSkills] = useState<Skill[]>([]);
  const [skillLoading, setSkillLoading] = useState(false);
  const [selectedSkillIds, setSelectedSkillIds] = useState<number[]>([]);
  const openClawImportInputRef = useRef<HTMLInputElement | null>(null);

  const [formData, setFormData] = useState<CreateInstanceRequest>({
    name: '',
    type: 'ubuntu',
    cpu_cores: 2,
    memory_gb: 4,
    disk_gb: 20,
    os_type: 'ubuntu',
    os_version: '22.04',
    gpu_enabled: false,
    gpu_count: 0,
    storage_class: ''
  });

  const getCreateErrorMessage = (rawError?: string) => {
    if (rawError === 'instance name already exists') {
      return t('instances.nameAlreadyExists');
    }
    return rawError || t('instances.createFailed');
  };

  useEffect(() => {
    const loadAvailableTypes = async () => {
      try {
        const items = await systemSettingsService.getImageSettings();
        const enabledTypes = new Set(
          items
            .filter((item) => item.is_enabled !== false)
            .map((item) => item.instance_type),
        );

        const filtered = INSTANCE_TYPES.filter((type) => enabledTypes.has(type.id));
        if (filtered.length > 0) {
          setAvailableTypes(filtered);
          setFormData((current) => {
            if (filtered.some((type) => type.id === current.type)) {
              return current;
            }

            const first = filtered[0];
            return {
              ...current,
              type: first.id as CreateInstanceRequest['type'],
              os_type: first.defaultOs,
              os_version: first.defaultVersion,
            };
          });
        }
      } catch {
        setAvailableTypes(INSTANCE_TYPES);
      }
    };

    loadAvailableTypes();
  }, []);

  useEffect(() => {
    const loadSkills = async () => {
      try {
        setSkillLoading(true);
        const items = await skillService.listSkills();
        setAvailableSkills(items.filter((item) => item.status === 'active' && item.risk_level !== 'medium' && item.risk_level !== 'high'));
      } catch {
        setAvailableSkills([]);
      } finally {
        setSkillLoading(false);
      }
    };

    void loadSkills();
  }, []);

  useEffect(() => {
    const loadQuotaAndUsage = async () => {
      if (!user) {
        setQuotaLoading(false);
        return;
      }

      try {
        setQuotaLoading(true);
        const [quotaData, instancesData] = await Promise.all([
          userService.getUserQuota(user.id),
          instanceService.getInstances(1, 1000),
        ]);

        setQuota(quotaData);
        setInstances(instancesData.instances || []);
      } catch {
        setQuota(null);
      } finally {
        setQuotaLoading(false);
      }
    };

    loadQuotaAndUsage();
  }, [user]);

  const handleTypeSelect = (typeId: string) => {
    const instanceType = availableTypes.find(t => t.id === typeId);
    if (instanceType) {
      if (typeId !== 'openclaw') {
        setOpenClawImportFile(null);
        setOpenClawInjectionMode('none');
        setOpenClawBundleId(undefined);
        setOpenClawResourceIds([]);
        setOpenClawPreview(null);
        setOpenClawPreviewError(null);
        setSelectedSkillIds([]);
      }
      setFormData({
        ...formData,
        type: typeId as CreateInstanceRequest['type'],
        os_type: instanceType.defaultOs,
        os_version: instanceType.defaultVersion,
        // Auto-set storage class for Ubuntu instances
        storage_class: typeId === 'ubuntu' ? 'external-default-sc' : ''
      });
    }
  };

  const handlePresetSelect = (preset: keyof typeof PRESET_CONFIGS) => {
    const config = PRESET_CONFIGS[preset];
    setFormData({
      ...formData,
      cpu_cores: config.cpu_cores,
      memory_gb: config.memory_gb,
      disk_gb: config.disk_gb
    });
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    // Only submit on step 3
    if (step !== 3 || !submitArmed || loading) {
      return;
    }
    
    try {
      setLoading(true);
      setError(null);
      const createPayload: CreateInstanceRequest = {
        ...formData,
        skill_ids: formData.type === 'openclaw' ? selectedSkillIds : undefined,
        openclaw_config_plan: formData.type === 'openclaw' && openClawInjectionMode === 'bundle' && openClawBundleId
          ? { mode: 'bundle', bundle_id: openClawBundleId }
          : formData.type === 'openclaw' && openClawInjectionMode === 'manual' && openClawResourceIds.length > 0
            ? { mode: 'manual', resource_ids: openClawResourceIds }
            : undefined,
      };

      const createdInstance = await instanceService.createInstance(createPayload);

      if (formData.type === 'openclaw' && openClawInjectionMode === 'archive' && openClawImportFile) {
        await waitForInstanceRunning(createdInstance.id);
        await instanceService.importOpenClawWorkspace(createdInstance.id, openClawImportFile);
      }

      navigate('/instances');
    } catch (err: unknown) {
      setError(getCreateErrorMessage(getApiErrorText(err)));
    } finally {
      setLoading(false);
    }
  };

  const canProceed = () => {
    if (step === 1) return formData.name.length >= 3;
    if (step === 2) return true;
    return true;
  };

  useEffect(() => {
    if (step !== 3) {
      setSubmitArmed(false);
      return;
    }

    const timer = window.setTimeout(() => {
      setSubmitArmed(true);
    }, 150)

    return () => window.clearTimeout(timer);
  }, [step]);

  const waitForInstanceRunning = async (instanceId: number) => {
    const timeoutAt = Date.now() + 5 * 60 * 1000;

    while (Date.now() < timeoutAt) {
      const current = await instanceService.getInstance(instanceId);
      if (current.status === 'running') {
        return;
      }
      if (current.status === 'error') {
        throw new Error(t('instances.waitingForRunningState'));
      }
      await new Promise((resolve) => window.setTimeout(resolve, 5000));
    }

    throw new Error(t('instances.timedOutWaitingForRunning'));
  };

  const usedResources = {
    instances: instances.length,
    cpu: instances.reduce((sum, instance) => sum + instance.cpu_cores, 0),
    memory: instances.reduce((sum, instance) => sum + instance.memory_gb, 0),
    storage: instances.reduce((sum, instance) => sum + instance.disk_gb, 0),
    gpu: instances.reduce((sum, instance) => sum + (instance.gpu_enabled ? instance.gpu_count : 0), 0),
  };

  const quotaChecks = quota
    ? [
        {
          key: 'instances',
          label: t('instances.quotaInstances'),
          next: usedResources.instances + 1,
          max: quota.max_instances,
          exceeded: usedResources.instances + 1 > quota.max_instances,
        },
        {
          key: 'cpu',
          label: t('common.cpu'),
          next: usedResources.cpu + formData.cpu_cores,
          max: quota.max_cpu_cores,
          exceeded: usedResources.cpu + formData.cpu_cores > quota.max_cpu_cores,
        },
        {
          key: 'memory',
          label: t('instances.memoryLabel'),
          next: usedResources.memory + formData.memory_gb,
          max: quota.max_memory_gb,
          exceeded: usedResources.memory + formData.memory_gb > quota.max_memory_gb,
        },
        {
          key: 'storage',
          label: t('instances.storageLabel'),
          next: usedResources.storage + formData.disk_gb,
          max: quota.max_storage_gb,
          exceeded: usedResources.storage + formData.disk_gb > quota.max_storage_gb,
        },
        {
          key: 'gpu',
          label: t('common.gpu'),
          next: usedResources.gpu + (formData.gpu_enabled ? formData.gpu_count || 0 : 0),
          max: quota.max_gpu_count,
          exceeded: usedResources.gpu + (formData.gpu_enabled ? formData.gpu_count || 0 : 0) > quota.max_gpu_count,
        },
      ]
    : [];

  const exceededQuotaItems = quotaChecks.filter((item) => item.exceeded);
  const quotaExceeded = exceededQuotaItems.length > 0;
  const openClawPlanInvalid = formData.type === 'openclaw' && (
    (openClawInjectionMode === 'bundle' && (!openClawBundleId || !!openClawPreviewError || openClawPreviewLoading)) ||
    (openClawInjectionMode === 'manual' && (openClawResourceIds.length === 0 || !!openClawPreviewError || openClawPreviewLoading)) ||
    (openClawInjectionMode === 'archive' && !openClawImportFile)
  );
  const createDisabled = loading || !submitArmed || quotaLoading || !quota || quotaExceeded || openClawPlanInvalid;

  const handleOpenClawPreviewChange = React.useCallback((preview: OpenClawConfigCompilePreview | null, state: { loading: boolean; error: string | null }) => {
    setOpenClawPreview(preview);
    setOpenClawPreviewLoading(state.loading);
    setOpenClawPreviewError(state.error);
  }, []);

  const renderTypeIcon = (typeId: string) => {
    if (typeId === 'openclaw') {
      return (
        <img
          src="/openclaw.png"
          alt={translatedText('instances.instanceTypes.openclaw.name', 'OpenClaw Desktop')}
          className="h-10 w-10 object-contain"
        />
      );
    }

    return (
      <svg className="h-6 w-6 text-indigo-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
      </svg>
    );
  };

  const translatedText = (key: string, fallback: string) => {
    const value = t(key);
    return value === key ? fallback : value;
  };

  const getInstanceTypeName = (type?: InstanceType) => {
    return type ? translatedText(type.nameKey, type.name) : '';
  };

  const getInstanceTypeDescription = (type: InstanceType) => {
    return translatedText(type.descriptionKey, type.description);
  };

  return (
    <UserLayout>
      {/* Progress Bar */}
      <div className="app-panel mb-6">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center py-4">
            <h1 className="text-xl font-bold text-gray-900 mr-8">{t('instances.createTitle')}</h1>
            <div className="flex items-center">
              {[1, 2, 3].map((s) => (
                <React.Fragment key={s}>
                  <div className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-medium ${
                    s === step ? 'bg-indigo-600 text-white' : 
                    s < step ? 'bg-green-500 text-white' : 'bg-gray-200 text-gray-600'
                  }`}>
                    {s < step ? '✓' : s}
                  </div>
                  {s < 3 && (
                    <div className={`w-16 h-0.5 mx-2 ${s < step ? 'bg-green-500' : 'bg-gray-200'}`} />
                  )}
                </React.Fragment>
              ))}
            </div>
            <span className="ml-4 text-sm text-gray-500">
              {t('instances.stepOf', { step, label: step === 1 ? t('instances.stepBasic') : step === 2 ? t('instances.stepType') : t('instances.stepConfig') })}
            </span>
          </div>
        </div>
      </div>

      {/* Main Content */}
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        {error && (
          <div className="mb-4 bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded">
            {error}
            <button 
              type="button"
              onClick={() => setError(null)}
              className="float-right text-red-500 hover:text-red-700"
            >
              ×
            </button>
          </div>
        )}

        <form onSubmit={handleSubmit} onKeyDown={(e) => {
          // Prevent Enter key from submitting form in step 1 and 2
          if (e.key === 'Enter' && step < 3) {
            e.preventDefault();
            if (canProceed()) {
              setStep(step + 1);
            }
          }
        }}>
          {/* Step 1: Basic Information */}
          {step === 1 && (
            <div className="app-panel p-6">
              <h2 className="text-lg font-medium text-gray-900 mb-4">{t('instances.basicInformation')}</h2>
              <div className="space-y-4">
                <div>
                  <label htmlFor="name" className="block text-sm font-medium text-gray-700">
                    {t('instances.instanceName')}
                  </label>
                  <input
                    type="text"
                    id="name"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                    className="app-input mt-1 block w-full"
                    placeholder={t('instances.instanceNamePlaceholder')}
                    minLength={3}
                    required
                  />
                  <p className="mt-1 text-xs text-gray-500">{t('instances.minimumThreeChars')}</p>
                </div>

                <div>
                  <label htmlFor="description" className="block text-sm font-medium text-gray-700">
                    {t('instances.descriptionOptional')}
                  </label>
                  <textarea
                    id="description"
                    value={formData.description || ''}
                    onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                    rows={3}
                    className="app-input mt-1 block w-full"
                    placeholder={t('instances.descriptionPlaceholder')}
                  />
                </div>
              </div>
            </div>
          )}

          {/* Step 2: Select Type */}
          {step === 2 && (
            <div className="app-panel p-6">
              <h2 className="text-lg font-medium text-gray-900 mb-4">{t('instances.selectInstanceType')}</h2>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {availableTypes.map((type) => (
                  <div
                    key={type.id}
                    onClick={() => handleTypeSelect(type.id)}
                    className={`relative cursor-pointer rounded-[24px] border p-4 transition-all hover:-translate-y-0.5 hover:shadow-[0_24px_56px_-42px_rgba(30,64,175,0.55)] ${
                      formData.type === type.id 
                        ? 'border-indigo-500 ring-2 ring-indigo-500' 
                        : 'border-gray-300'
                    }`}
                  >
                    <div className="flex items-center">
                      <div className="flex-shrink-0">
                        <div className="h-12 w-12 rounded-lg bg-indigo-100 flex items-center justify-center">
                          {renderTypeIcon(type.id)}
                        </div>
                      </div>
                      <div className="ml-4">
                        <h3 className="text-sm font-medium text-gray-900">{getInstanceTypeName(type)}</h3>
                        <p className="mt-1 text-xs text-gray-500">{getInstanceTypeDescription(type)}</p>
                      </div>
                    </div>
                    {formData.type === type.id && (
                      <div className="absolute top-2 right-2">
                        <svg className="h-5 w-5 text-indigo-600" fill="currentColor" viewBox="0 0 20 20">
                          <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
                        </svg>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Step 3: Configuration */}
          {step === 3 && (
            <div className="space-y-6">
              {/* Quick Presets */}
              <div className="app-panel p-6">
                <h2 className="text-lg font-medium text-gray-900 mb-4">{t('instances.quickConfiguration')}</h2>
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                  {Object.entries(PRESET_CONFIGS).map(([key, config]) => (
                    <button
                      key={key}
                      type="button"
                      onClick={() => handlePresetSelect(key as keyof typeof PRESET_CONFIGS)}
                      className={`rounded-[22px] border p-4 text-left transition-all hover:-translate-y-0.5 hover:shadow-[0_24px_56px_-42px_rgba(30,64,175,0.55)] ${
                        formData.cpu_cores === config.cpu_cores && 
                        formData.memory_gb === config.memory_gb && 
                        formData.disk_gb === config.disk_gb
                          ? 'border-indigo-500 ring-2 ring-indigo-500'
                          : 'border-gray-300'
                      }`}
                    >
                      <h3 className="font-medium text-gray-900">{translatedText(config.nameKey, config.name)}</h3>
                      <p className="text-sm text-gray-500 mt-1">{translatedText(config.descriptionKey, config.description)}</p>
                      <div className="mt-2 text-sm text-gray-600">
                        {t('instances.presetResourceSummary', { cpu: config.cpu_cores, memory: config.memory_gb, disk: config.disk_gb })}
                      </div>
                    </button>
                  ))}
                </div>
              </div>

              {/* Custom Configuration */}
              <div className="app-panel p-6">
                <h2 className="text-lg font-medium text-gray-900 mb-4">{t('instances.customConfiguration')}</h2>
                <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
                  <div>
                    <label htmlFor="cpu" className="block text-sm font-medium text-gray-700">
                      {t('instances.cpuCores')}
                    </label>
                    <input
                      type="number"
                      id="cpu"
                      min={1}
                      max={32}
                      value={formData.cpu_cores}
                      onChange={(e) => setFormData({ ...formData, cpu_cores: parseInt(e.target.value) || 1 })}
                      className="app-input mt-1 block w-full"
                    />
                  </div>

                  <div>
                    <label htmlFor="memory" className="block text-sm font-medium text-gray-700">
                      {t('instances.memoryGb')}
                    </label>
                    <input
                      type="number"
                      id="memory"
                      min={1}
                      max={128}
                      value={formData.memory_gb}
                      onChange={(e) => setFormData({ ...formData, memory_gb: parseInt(e.target.value) || 1 })}
                      className="app-input mt-1 block w-full"
                    />
                  </div>

                  <div>
                    <label htmlFor="disk" className="block text-sm font-medium text-gray-700">
                      {t('instances.diskGb')}
                    </label>
                    <input
                      type="number"
                      id="disk"
                      min={10}
                      max={1000}
                      value={formData.disk_gb}
                      onChange={(e) => setFormData({ ...formData, disk_gb: parseInt(e.target.value) || 10 })}
                      className="app-input mt-1 block w-full"
                    />
                  </div>

                  <div>
                    <label htmlFor="storage_class" className="block text-sm font-medium text-gray-700">
                      {t('instances.storageClassOptional')}
                    </label>
                    <input
                      type="text"
                      id="storage_class"
                      value={formData.storage_class || ''}
                      onChange={(e) => setFormData({ ...formData, storage_class: e.target.value })}
                      className="app-input mt-1 block w-full"
                      placeholder={t('instances.defaultStorageClass')}
                    />
                  </div>
                </div>

                {/* GPU Option */}
                <div className="mt-6">
                  <div className="flex items-center">
                    <input
                      id="gpu_enabled"
                      type="checkbox"
                      checked={formData.gpu_enabled}
                      onChange={(e) => setFormData({ ...formData, gpu_enabled: e.target.checked, gpu_count: e.target.checked ? 1 : 0 })}
                      className="h-4 w-4 text-indigo-600 focus:ring-indigo-500 border-gray-300 rounded"
                    />
                    <label htmlFor="gpu_enabled" className="ml-2 block text-sm text-gray-900">
                      {t('instances.enableGpu')}
                    </label>
                  </div>
                  {formData.gpu_enabled && (
                    <div className="mt-2 ml-6">
                      <label htmlFor="gpu_count" className="block text-sm font-medium text-gray-700">
                        {t('instances.gpuCount')}
                      </label>
                      <input
                        type="number"
                        id="gpu_count"
                        min={1}
                        max={4}
                        value={formData.gpu_count}
                        onChange={(e) => setFormData({ ...formData, gpu_count: parseInt(e.target.value) || 1 })}
                        className="app-input mt-1 block w-32"
                      />
                    </div>
                  )}
                </div>
              </div>

              {formData.type === 'openclaw' && (
                <div className="space-y-6">
                  <OpenClawConfigPlanSection
                    mode={openClawInjectionMode}
                    bundleId={openClawBundleId}
                    resourceIds={openClawResourceIds}
                    onModeChange={(nextMode) => {
                      setOpenClawInjectionMode(nextMode);
                      setOpenClawPreview(null);
                      setOpenClawPreviewError(null);
                      if (nextMode !== 'bundle') {
                        setOpenClawBundleId(undefined);
                      }
                      if (nextMode !== 'manual') {
                        setOpenClawResourceIds([]);
                      }
                      if (nextMode !== 'archive') {
                        setOpenClawImportFile(null);
                        if (openClawImportInputRef.current) {
                          openClawImportInputRef.current.value = '';
                        }
                      }
                    }}
                    onSelectionChange={({ bundleId, resourceIds }) => {
                      setOpenClawBundleId(bundleId);
                      setOpenClawResourceIds(resourceIds);
                    }}
                    onPreviewChange={handleOpenClawPreviewChange}
                  />

                  <div className="app-panel p-6">
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <h2 className="text-lg font-medium text-gray-900">{t('instances.skillInjection')}</h2>
                        <p className="mt-1 text-sm text-gray-500">
                          {t('instances.skillInjectionDesc')}
                        </p>
                      </div>
                      <span className="rounded-full bg-emerald-50 px-3 py-1 text-xs font-medium text-emerald-700">
                        {t('instances.selectedCount', { count: selectedSkillIds.length })}
                      </span>
                    </div>
                    <div className="mt-5 grid grid-cols-1 gap-3 md:grid-cols-2">
                      {skillLoading ? (
                        <div className="text-sm text-gray-500">{t('instances.loadingSkills')}</div>
                      ) : availableSkills.length === 0 ? (
                        <div className="rounded-2xl border border-dashed border-gray-300 px-4 py-5 text-sm text-gray-500">
                          {t('instances.noAvailableSkills')}
                        </div>
                      ) : availableSkills.map((skill) => {
                        const checked = selectedSkillIds.includes(skill.id);
                        return (
                          <label
                            key={skill.id}
                            className={`flex cursor-pointer items-start gap-3 rounded-2xl border px-4 py-3 ${
                              checked ? 'border-indigo-300 bg-indigo-50' : 'border-gray-200 bg-white'
                            }`}
                          >
                            <input
                              type="checkbox"
                              checked={checked}
                              onChange={(e) => setSelectedSkillIds((current) => (
                                e.target.checked
                                  ? [...current, skill.id]
                                  : current.filter((value) => value !== skill.id)
                              ))}
                            />
                            <span className="min-w-0">
                              <span className="block font-medium text-gray-900">{skill.name}</span>
                              <span className="mt-1 block text-xs text-gray-500">
                                {t('instances.skillRiskVersion', { key: skill.skill_key, risk: skill.risk_level, version: skill.current_version_no || 1 })}
                              </span>
                              {skill.description && <span className="mt-2 block text-sm text-gray-600">{skill.description}</span>}
                            </span>
                          </label>
                        );
                      })}
                    </div>
                  </div>

                  {openClawInjectionMode === 'archive' && (
                    <div className="app-panel p-6">
                      <div className="flex items-start justify-between gap-4">
                        <div>
                          <h2 className="text-lg font-medium text-gray-900">{t('instances.openClawImportTitle')}</h2>
                          <p className="mt-1 text-sm text-gray-500">
                            {t('instances.openClawImportDesc')}
                          </p>
                        </div>
                        <span className="rounded-full bg-indigo-50 px-3 py-1 text-xs font-medium text-indigo-600">
                          {t('instances.archiveModeRequired')}
                        </span>
                      </div>

                      <input
                        ref={openClawImportInputRef}
                        type="file"
                        accept=".tar.gz,.tgz,application/gzip,application/x-gzip,application/octet-stream"
                        className="hidden"
                        onChange={(e) => setOpenClawImportFile(e.target.files?.[0] || null)}
                      />

                      <div className="mt-5 flex flex-wrap items-center gap-3">
                        <button
                          type="button"
                          onClick={() => openClawImportInputRef.current?.click()}
                          className="app-button-secondary"
                        >
                          {openClawImportFile ? t('instances.changeOpenClawArchive') : t('instances.chooseOpenClawArchive')}
                        </button>
                        {openClawImportFile && (
                          <button
                            type="button"
                            onClick={() => {
                              setOpenClawImportFile(null);
                              if (openClawImportInputRef.current) {
                                openClawImportInputRef.current.value = '';
                              }
                            }}
                            className="rounded-lg border border-red-200 bg-red-50 px-4 py-2.5 text-sm font-medium text-red-700 hover:bg-red-100"
                          >
                            {t('instances.remove')}
                          </button>
                        )}
                      </div>

                      <div className="mt-4 rounded-lg border border-dashed border-gray-300 bg-gray-50 px-4 py-3 text-sm text-gray-600">
                        {openClawImportFile
                          ? t('instances.selectedArchive', { name: openClawImportFile.name })
                          : t('instances.noArchiveSelected')}
                      </div>
                    </div>
                  )}

                  {(openClawInjectionMode === 'bundle' || openClawInjectionMode === 'manual') && (
                    <div className="app-panel p-6">
                      <div className="flex items-start justify-between gap-4">
                        <div>
                          <h2 className="text-lg font-medium text-gray-900">{t('instances.configPreview')}</h2>
                          <p className="mt-1 text-sm text-gray-500">
                            {t('instances.configPreviewDesc')}
                          </p>
                        </div>
                        {openClawPreviewLoading && (
                          <span className="rounded-full bg-amber-50 px-3 py-1 text-xs font-medium text-amber-700">
                            {t('instances.compiling')}
                          </span>
                        )}
                      </div>

                      {openClawPreviewError && (
                        <div className="mt-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                          {openClawPreviewError}
                        </div>
                      )}

                      {openClawPreview && (
                        <div className="mt-4 space-y-4">
                          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                            <div className="rounded-2xl border border-gray-200 bg-gray-50 px-4 py-3">
                              <div className="text-xs uppercase tracking-[0.18em] text-gray-500">{t('instances.resolvedResources')}</div>
                              <div className="mt-2 text-2xl font-semibold text-gray-900">{openClawPreview.resolved_resources.length}</div>
                            </div>
                            <div className="rounded-2xl border border-gray-200 bg-gray-50 px-4 py-3">
                              <div className="text-xs uppercase tracking-[0.18em] text-gray-500">{t('instances.envVariables')}</div>
                              <div className="mt-2 text-2xl font-semibold text-gray-900">{openClawPreview.env_names.length}</div>
                            </div>
                            <div className="rounded-2xl border border-gray-200 bg-gray-50 px-4 py-3">
                              <div className="text-xs uppercase tracking-[0.18em] text-gray-500">{t('instances.payloadSize')}</div>
                              <div className="mt-2 text-2xl font-semibold text-gray-900">{openClawPreview.total_payload_bytes} B</div>
                            </div>
                          </div>

                          {openClawPreview.auto_included.length > 0 && (
                            <div className="rounded-2xl border border-indigo-200 bg-indigo-50 px-4 py-3 text-sm text-indigo-700">
                              {t('instances.autoIncludedDependencies', { names: openClawPreview.auto_included.map((item) => item.name).join(', ') })}
                            </div>
                          )}

                          {openClawPreview.warnings.length > 0 && (
                            <div className="rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700">
                              {openClawPreview.warnings.join(' ')}
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )}

              <div className="app-panel p-6">
                <h3 className="text-lg font-medium text-gray-900 mb-4">{t('instances.quotaValidation')}</h3>
                {quotaLoading ? (
                  <p className="text-sm text-gray-500">{t('userDashboard.loadingQuota')}</p>
                ) : !quota ? (
                  <p className="text-sm text-red-600">
                    {t('instances.unableToLoadQuota')}
                  </p>
                ) : (
                  <div className="space-y-3">
                    {quotaChecks.map((item) => (
                      <div
                        key={item.key}
                        className={`flex items-center justify-between rounded-lg border px-4 py-3 text-sm ${
                          item.exceeded
                            ? 'border-red-200 bg-red-50 text-red-700'
                            : 'border-green-200 bg-green-50 text-green-700'
                        }`}
                      >
                        <span className="font-medium">{item.label}</span>
                        <span>
                          {item.next} / {item.max}
                        </span>
                      </div>
                    ))}
                    {quotaExceeded && (
                      <p className="text-sm text-red-600">
                        {t('instances.requestedResourcesExceeded')}
                      </p>
                    )}
                  </div>
                )}
              </div>

              {/* Summary */}
              <div className="app-panel p-6">
                <h3 className="text-lg font-medium text-gray-900 mb-4">{t('instances.summary')}</h3>
                <dl className="grid grid-cols-1 gap-x-4 gap-y-4 sm:grid-cols-2">
                  <div>
                    <dt className="text-sm font-medium text-gray-500">{t('instances.instanceName')}</dt>
                    <dd className="mt-1 text-sm text-gray-900">{formData.name}</dd>
                  </div>
                  <div>
                    <dt className="text-sm font-medium text-gray-500">{t('common.type')}</dt>
                    <dd className="mt-1 text-sm text-gray-900">{getInstanceTypeName(availableTypes.find((type) => type.id === formData.type))}</dd>
                  </div>
                  <div>
                    <dt className="text-sm font-medium text-gray-500">{t('common.cpu')}</dt>
                    <dd className="mt-1 text-sm text-gray-900">{t('instances.cpuCoreCount', { count: formData.cpu_cores })}</dd>
                  </div>
                  <div>
                    <dt className="text-sm font-medium text-gray-500">{t('instances.memoryLabel')}</dt>
                    <dd className="mt-1 text-sm text-gray-900">{formData.memory_gb} GB</dd>
                  </div>
                  <div>
                    <dt className="text-sm font-medium text-gray-500">{t('instances.storageLabel')}</dt>
                    <dd className="mt-1 text-sm text-gray-900">{formData.disk_gb} GB</dd>
                  </div>
                  <div>
                    <dt className="text-sm font-medium text-gray-500">{t('common.gpu')}</dt>
                    <dd className="mt-1 text-sm text-gray-900">{formData.gpu_enabled ? t('instances.gpuSummary', { count: formData.gpu_count || 0 }) : t('instances.gpuDisabled')}</dd>
                  </div>
                  {formData.type === 'openclaw' && (
                    <div className="sm:col-span-2">
                      <dt className="text-sm font-medium text-gray-500">{t('instances.openClawBootstrap')}</dt>
                      <dd className="mt-1 space-y-1 text-sm text-gray-900">
                        <div>{t('instances.modeLabel')}: {openClawInjectionMode}</div>
                        {openClawInjectionMode === 'archive' && (
                          <div>{openClawImportFile ? openClawImportFile.name : t('instances.noOpenClawArchiveSelected')}</div>
                        )}
                        {(openClawInjectionMode === 'bundle' || openClawInjectionMode === 'manual') && openClawPreview && (
                          <div>{t('instances.resourceEnvPayloadSummary', { resources: openClawPreview.resolved_resources.length, envs: openClawPreview.env_names.length })}</div>
                        )}
                      </dd>
                    </div>
                  )}
                </dl>
              </div>
            </div>
          )}

          {/* Navigation Buttons */}
          <div className="mt-6 flex justify-between">
            <button
              type="button"
              onClick={() => step > 1 ? setStep(step - 1) : navigate('/instances')}
              className="app-button-secondary"
            >
              {step === 1 ? t('common.cancel') : t('instances.back')}
            </button>

            {step < 3 ? (
              <button
                type="button"
                onClick={() => setStep(step + 1)}
                disabled={!canProceed()}
                className="app-button-primary disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {t('instances.next')}
              </button>
            ) : (
              <button
                type="submit"
                disabled={createDisabled}
                className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {quotaLoading ? t('instances.checkingQuota') : loading ? (formData.type === 'openclaw' && openClawInjectionMode === 'archive' && openClawImportFile ? t('instances.creatingAndImporting') : t('instances.creatingNow')) : t('instances.createNow')}
              </button>
            )}
          </div>
        </form>
      </div>
    </UserLayout>
  );
};

export default CreateInstancePage;
