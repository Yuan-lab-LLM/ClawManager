import React, { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import type { AxiosError } from "axios";
import OpenClawConfigPlanSection, {
  type OpenClawInjectionMode,
} from "../../components/OpenClawConfigPlanSection";
import UserLayout from "../../components/UserLayout";
import { useAuth } from "../../contexts/AuthContext";
import { instanceService } from "../../services/instanceService";
import { skillService } from "../../services/skillService";
import { userService } from "../../services/userService";
import { INSTANCE_TYPES, PRESET_CONFIGS } from "../../types/instance";
import type { CreateInstanceRequest, InstanceMode } from "../../types/instance";
import type { Instance } from "../../types/instance";
import type { OpenClawConfigCompilePreview } from "../../types/openclawConfig";
import type { Skill } from "../../types/skill";
import type { UserQuota } from "../../types/user";
import { useI18n } from "../../contexts/I18nContext";
import {
  systemSettingsService,
  type SystemImageSetting,
} from "../../services/systemSettingsService";
import type { DesktopStreamProfile } from "../../types/instance";

type BuiltInEnvTemplate = {
  key: string;
  description: string;
  defaultValue?: string;
  defaultLabel?: string;
};

type TranslateFn = (
  key: string,
  variables?: Record<string, string | number>,
) => string;

type CustomEnvRow = {
  id: string;
  name: string;
  value: string;
};

const ENV_NAME_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;
const BYTES_PER_GIB = 1024 * 1024 * 1024;
const AGENT_PROTOCOL_VERSION = "v1";
const CUSTOM_RESOURCE_PRESET = "custom";
const SKILLS_PER_PAGE = 6;
const supportsRuntimeInjection = (type: string) =>
  type === "openclaw" || type === "hermes";
const DESKTOP_STREAM_PROFILES: Array<{
  id: DesktopStreamProfile;
  labelKey: string;
  descriptionKey: string;
  details: string;
}> = [
  {
    id: "low",
    labelKey: "instances.desktopStreamLow",
    descriptionKey: "instances.desktopStreamLowDesc",
    details: "30 FPS / CRF 42",
  },
  {
    id: "standard",
    labelKey: "instances.desktopStreamStandard",
    descriptionKey: "instances.desktopStreamStandardDesc",
    details: "35 FPS / CRF 34",
  },
  {
    id: "high",
    labelKey: "instances.desktopStreamHigh",
    descriptionKey: "instances.desktopStreamHighDesc",
    details: "40 FPS / CRF 24",
  },
];

const runtimeWorkspaceDirectory = (type: string) =>
  type === "hermes" ? ".hermes" : ".openclaw";

const runtimeProductName = (type: string) =>
  type === "hermes" ? "Hermes" : "OpenClaw";

const INSTANCE_TYPE_I18N_KEYS: Record<
  string,
  { label: string; description: string }
> = {
  ubuntu: {
    label: "instances.typeOptions.ubuntu.label",
    description: "instances.typeOptions.ubuntu.description",
  },
  debian: {
    label: "instances.typeOptions.debian.label",
    description: "instances.typeOptions.debian.description",
  },
  centos: {
    label: "instances.typeOptions.centos.label",
    description: "instances.typeOptions.centos.description",
  },
  openclaw: {
    label: "instances.typeOptions.openclaw.label",
    description: "instances.typeOptions.openclaw.description",
  },
  webtop: {
    label: "instances.typeOptions.webtop.label",
    description: "instances.typeOptions.webtop.description",
  },
  hermes: {
    label: "instances.typeOptions.hermes.label",
    description: "instances.typeOptions.hermes.description",
  },
  custom: {
    label: "instances.typeOptions.custom.label",
    description: "instances.typeOptions.custom.description",
  },
};

const CREATE_INSTANCE_TYPES = INSTANCE_TYPES.filter((type) =>
  ["openclaw", "hermes"].includes(type.id),
);

const INSTANCE_MODE_OPTIONS: {
  id: InstanceMode;
  label: string;
  descriptionKey: string;
}[] = [
  {
    id: "lite",
    label: "Lite",
    descriptionKey: "instances.instanceModeLiteDescription",
  },
  {
    id: "pro",
    label: "Pro",
    descriptionKey: "instances.instanceModeProDescription",
  },
];

const PRESET_I18N_KEYS: Record<string, { label: string; description: string }> =
  {
    small: {
      label: "instances.presetOptions.small.label",
      description: "instances.presetOptions.small.description",
    },
    medium: {
      label: "instances.presetOptions.medium.label",
      description: "instances.presetOptions.medium.description",
    },
    large: {
      label: "instances.presetOptions.large.label",
      description: "instances.presetOptions.large.description",
    },
  };

const getBuiltInEnvTemplates = (
  t: TranslateFn,
  type: CreateInstanceRequest["type"],
  diskGb: number,
): BuiltInEnvTemplate[] => {
  const templates: BuiltInEnvTemplate[] = [];
  const persistentDir = type === "hermes" ? "/config/.hermes" : "/config";

  if (type === "ubuntu") {
    templates.push(
      {
        key: "TITLE",
        description: t("instances.envDescDesktopTitleWebtop"),
        defaultValue: "ClawManager Desktop",
      },
      {
        key: "SUBFOLDER",
        description: t("instances.envDescProxySubfolder"),
        defaultLabel: t("instances.envManagedProxyPath"),
      },
    );
  }

  if (type === "webtop") {
    templates.push(
      {
        key: "TITLE",
        description: t("instances.envDescDesktopTitleWebtop"),
        defaultValue: "ClawManager Webtop",
      },
      {
        key: "SUBFOLDER",
        description: t("instances.envDescProxySubfolder"),
        defaultLabel: t("instances.envManagedProxyPath"),
      },
    );
  }

  if (type === "hermes") {
    templates.push(
      {
        key: "TITLE",
        description: t("instances.envDescDesktopTitleWebtop"),
        defaultValue: "Hermes Runtime",
      },
      {
        key: "SUBFOLDER",
        description: t("instances.envDescProxySubfolder"),
        defaultLabel: t("instances.envManagedProxyPath"),
      },
      {
        key: "CLAWMANAGER_LLM_BASE_URL",
        description: t("instances.envDescLlmBaseUrl"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_LLM_API_KEY",
        description: t("instances.envDescLlmApiKey"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_LLM_MODEL",
        description: t("instances.envDescLlmModel"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_LLM_PROVIDER",
        description: t("instances.envDescLlmProvider"),
        defaultValue: "openai-compatible",
      },
      {
        key: "CLAWMANAGER_INSTANCE_TOKEN",
        description: t("instances.envDescInstanceToken"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "OPENAI_BASE_URL",
        description: t("instances.envDescOpenAiBaseUrl"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "OPENAI_API_BASE",
        description: t("instances.envDescOpenAiApiBase"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "OPENAI_API_KEY",
        description: t("instances.envDescOpenAiApiKey"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "OPENAI_MODEL",
        description: t("instances.envDescOpenAiModel"),
        defaultValue: "auto",
      },
      {
        key: "CLAWMANAGER_AGENT_ENABLED",
        description: t("instances.envDescAgentEnabled"),
        defaultValue: "true",
      },
      {
        key: "CLAWMANAGER_AGENT_BASE_URL",
        description: t("instances.envDescAgentBaseUrl"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN",
        description: t("instances.envDescAgentBootstrapToken"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_AGENT_DISK_LIMIT_BYTES",
        description: t("instances.envDescAgentDiskLimitBytes"),
        defaultValue: String(diskGb * BYTES_PER_GIB),
      },
      {
        key: "CLAWMANAGER_AGENT_INSTANCE_ID",
        description: t("instances.envDescAgentInstanceId"),
        defaultLabel: t("instances.envAssignedAfterCreation"),
      },
      {
        key: "CLAWMANAGER_AGENT_PERSISTENT_DIR",
        description: t("instances.envDescAgentPersistentDir"),
        defaultValue: persistentDir,
      },
      {
        key: "CLAWMANAGER_AGENT_PROTOCOL_VERSION",
        description: t("instances.envDescAgentProtocolVersion"),
        defaultValue: AGENT_PROTOCOL_VERSION,
      },
    );
  }

  if (type === "openclaw") {
    templates.push(
      {
        key: "TITLE",
        description: t("instances.envDescDesktopTitleOpenClaw"),
        defaultValue: "ClawManager Desktop",
      },
      {
        key: "SUBFOLDER",
        description: t("instances.envDescProxySubfolder"),
        defaultLabel: t("instances.envManagedProxyPath"),
      },
      {
        key: "CLAWMANAGER_LLM_BASE_URL",
        description: t("instances.envDescLlmBaseUrl"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_LLM_API_KEY",
        description: t("instances.envDescLlmApiKey"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_LLM_MODEL",
        description: t("instances.envDescLlmModel"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_LLM_PROVIDER",
        description: t("instances.envDescLlmProvider"),
        defaultValue: "openai-compatible",
      },
      {
        key: "CLAWMANAGER_INSTANCE_TOKEN",
        description: t("instances.envDescInstanceToken"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "OPENAI_BASE_URL",
        description: t("instances.envDescOpenAiBaseUrl"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "OPENAI_API_BASE",
        description: t("instances.envDescOpenAiApiBase"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "OPENAI_API_KEY",
        description: t("instances.envDescOpenAiApiKey"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "OPENAI_MODEL",
        description: t("instances.envDescOpenAiModel"),
        defaultValue: "auto",
      },
      {
        key: "CLAWMANAGER_AGENT_ENABLED",
        description: t("instances.envDescAgentEnabled"),
        defaultValue: "true",
      },
      {
        key: "CLAWMANAGER_AGENT_BASE_URL",
        description: t("instances.envDescAgentBaseUrl"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN",
        description: t("instances.envDescAgentBootstrapToken"),
        defaultLabel: t("instances.envGeneratedAtRuntime"),
      },
      {
        key: "CLAWMANAGER_AGENT_DISK_LIMIT_BYTES",
        description: t("instances.envDescAgentDiskLimitBytes"),
        defaultValue: String(diskGb * BYTES_PER_GIB),
      },
      {
        key: "CLAWMANAGER_AGENT_INSTANCE_ID",
        description: t("instances.envDescAgentInstanceId"),
        defaultLabel: t("instances.envAssignedAfterCreation"),
      },
      {
        key: "CLAWMANAGER_AGENT_PERSISTENT_DIR",
        description: t("instances.envDescAgentPersistentDir"),
        defaultValue: persistentDir,
      },
      {
        key: "CLAWMANAGER_AGENT_PROTOCOL_VERSION",
        description: t("instances.envDescAgentProtocolVersion"),
        defaultValue: AGENT_PROTOCOL_VERSION,
      },
    );
  }

  return templates;
};

const getInstanceTypeLabel = (
  t: TranslateFn,
  typeId: string,
  fallback: string,
): string => {
  const keys = INSTANCE_TYPE_I18N_KEYS[typeId];
  return keys ? t(keys.label) : fallback;
};

const getInstanceTypeDescription = (
  t: TranslateFn,
  typeId: string,
  fallback: string,
): string => {
  const keys = INSTANCE_TYPE_I18N_KEYS[typeId];
  return keys ? t(keys.description) : fallback;
};

const getPresetLabel = (
  t: TranslateFn,
  presetId: string,
  fallback: string,
): string => {
  const keys = PRESET_I18N_KEYS[presetId];
  return keys ? t(keys.label) : fallback;
};

const getPresetDescription = (
  t: TranslateFn,
  presetId: string,
  fallback: string,
): string => {
  const keys = PRESET_I18N_KEYS[presetId];
  return keys ? t(keys.description) : fallback;
};

const getRuntimeImageOptionKey = (item: SystemImageSetting): string =>
  item.id != null
    ? `runtime-image:${item.id}`
    : `runtime-image:${item.instance_type}:${item.runtime_type ?? "desktop"}:${item.image}`;

const normalizeRuntimeImageType = (
  runtimeType?: SystemImageSetting["runtime_type"] | "shell",
) => (runtimeType === "gateway" || runtimeType === "shell" ? "gateway" : "desktop");

const CreateInstancePage: React.FC = () => {
  const { user } = useAuth();
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [quotaLoading, setQuotaLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [step, setStep] = useState(1);
  const [submitArmed, setSubmitArmed] = useState(false);
  const [availableTypes, setAvailableTypes] = useState(CREATE_INSTANCE_TYPES);
  const [runtimeImageSettings, setRuntimeImageSettings] = useState<
    SystemImageSetting[]
  >([]);
  const [selectedRuntimeImageKey, setSelectedRuntimeImageKey] = useState("");
  const [quota, setQuota] = useState<UserQuota | null>(null);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [openClawImportFile, setOpenClawImportFile] = useState<File | null>(
    null,
  );
  const [openClawInjectionMode, setOpenClawInjectionMode] =
    useState<OpenClawInjectionMode>("none");
  const [openClawBundleId, setOpenClawBundleId] = useState<number | undefined>(
    undefined,
  );
  const [openClawResourceIds, setOpenClawResourceIds] = useState<number[]>([]);
  const [openClawPreview, setOpenClawPreview] =
    useState<OpenClawConfigCompilePreview | null>(null);
  const [openClawPreviewLoading, setOpenClawPreviewLoading] = useState(false);
  const [openClawPreviewError, setOpenClawPreviewError] = useState<
    string | null
  >(null);
  const [availableSkills, setAvailableSkills] = useState<Skill[]>([]);
  const [skillInventorySummary, setSkillInventorySummary] = useState({ total: 0, hiddenByRisk: 0 });
  const [skillLoading, setSkillLoading] = useState(false);
  const [selectedSkillIds, setSelectedSkillIds] = useState<number[]>([]);
  const [skillPage, setSkillPage] = useState(1);
  const openClawImportInputRef = useRef<HTMLInputElement | null>(null);
  const nextCustomEnvIdRef = useRef(0);
  const [showBuiltInEnvEditor, setShowBuiltInEnvEditor] = useState(false);
  const [builtinEnvOverrides, setBuiltinEnvOverrides] = useState<
    Record<string, string>
  >({});
  const [customEnvRows, setCustomEnvRows] = useState<CustomEnvRow[]>([]);
  const [desktopStreamProfile, setDesktopStreamProfile] =
    useState<DesktopStreamProfile>("standard");
  const [resourcePresetMode, setResourcePresetMode] = useState<
    keyof typeof PRESET_CONFIGS | typeof CUSTOM_RESOURCE_PRESET
  >("medium");

  const [formData, setFormData] = useState<CreateInstanceRequest>({
    name: "",
    type: "openclaw",
    mode: "lite",
    cpu_cores: 2,
    memory_gb: 4,
    disk_gb: 20,
    os_type: "openclaw",
    os_version: "latest",
    gpu_enabled: false,
    gpu_count: 0,
    storage_class: "",
  });
  const builtInEnvTemplates = getBuiltInEnvTemplates(
    t,
    formData.type,
    formData.disk_gb,
  );
  const selectedBuiltInTemplates = builtInEnvTemplates.filter((template) =>
    Object.prototype.hasOwnProperty.call(builtinEnvOverrides, template.key),
  );
  const availableBuiltInTemplates = builtInEnvTemplates.filter(
    (template) =>
      !Object.prototype.hasOwnProperty.call(builtinEnvOverrides, template.key),
  );
  const selectedType = availableTypes.find((item) => item.id === formData.type);
  const selectedMode = formData.mode ?? "lite";
  const selectedRuntimeType =
    selectedMode === "lite" ? "gateway" : "desktop";
  const runtimeImageOptions = runtimeImageSettings.filter(
    (item) =>
      item.is_enabled !== false &&
      item.instance_type === formData.type &&
      normalizeRuntimeImageType(item.runtime_type) === selectedRuntimeType,
  );
  const selectedRuntimeImage =
    runtimeImageOptions.find(
      (item) => getRuntimeImageOptionKey(item) === selectedRuntimeImageKey,
    ) ?? runtimeImageOptions[0] ?? null;
  const usesDedicatedResources = selectedMode === "pro";
  const showRuntimeImageSelector = selectedMode === "pro";
  const instanceUsesDedicatedResources = (instance: Instance) => {
    const instanceMode = (
      instance as Instance & { instance_mode?: InstanceMode }
    ).instance_mode;
    return instanceMode === "pro" || (!instanceMode && instance.runtime_type !== "gateway");
  };

  const renderInstanceModeSelector = () => (
    <div className="rounded-[24px] border border-[#ead8cf] bg-white p-5">
      <h3 className="text-sm font-semibold uppercase tracking-[0.18em] text-[#b46c50]">
        {t("instances.instanceMode")}
      </h3>
      <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
        {INSTANCE_MODE_OPTIONS.map((mode) => {
          const selected = selectedMode === mode.id;
          return (
            <button
              key={mode.id}
              type="button"
              onClick={() =>
                setFormData((current) => ({
                  ...current,
                  mode: mode.id,
                  instance_mode: mode.id,
                }))
              }
              className={`rounded-[20px] border p-4 text-left transition-all ${
                selected
                  ? "border-indigo-500 bg-indigo-50 ring-2 ring-indigo-500"
                  : "border-gray-200 bg-white hover:border-indigo-200"
              }`}
            >
              <span className="text-sm font-semibold text-gray-950">
                {mode.label}
              </span>
              <span className="mt-1 block text-sm text-gray-500">
                {t(mode.descriptionKey)}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );

  const getCreateErrorMessage = (rawError?: string) => {
    if (rawError === "instance name already exists") {
      return t("instances.nameAlreadyExists");
    }
    return rawError || t("instances.createFailed");
  };

  useEffect(() => {
    const loadAvailableTypes = async () => {
      try {
        const items = await systemSettingsService.getImageSettings();
        const enabledItems = items.filter((item) => item.is_enabled !== false);
        setRuntimeImageSettings(enabledItems);
        const enabledTypes = new Set(
          enabledItems.map((item) => item.instance_type),
        );

        const filtered = CREATE_INSTANCE_TYPES.filter((type) =>
          enabledTypes.has(type.id),
        );
        if (filtered.length > 0) {
          setAvailableTypes(filtered);
          setFormData((current) => {
            if (filtered.some((type) => type.id === current.type)) {
              return current;
            }

            const first = filtered[0];
            return {
              ...current,
              type: first.id as CreateInstanceRequest["type"],
              os_type: first.defaultOs,
              os_version: first.defaultVersion,
            };
          });
        } else {
          setAvailableTypes([]);
        }
      } catch {
        setRuntimeImageSettings([]);
        setAvailableTypes(CREATE_INSTANCE_TYPES);
      }
    };

    loadAvailableTypes();
  }, []);

  useEffect(() => {
    const loadSkills = async () => {
      try {
        setSkillLoading(true);
        const items = await skillService.listSkills();
        const activeSkills = items.filter((item) => item.status === "active");
        const attachableSkills = activeSkills.filter(
          (item) => !["medium", "high"].includes(item.risk_level.trim().toLowerCase()),
        );
        setAvailableSkills(attachableSkills);
        setSkillInventorySummary({
          total: activeSkills.length,
          hiddenByRisk: activeSkills.length - attachableSkills.length,
        });
      } catch {
        setAvailableSkills([]);
        setSkillInventorySummary({ total: 0, hiddenByRisk: 0 });
      } finally {
        setSkillLoading(false);
      }
    };

    void loadSkills();
  }, []);

  useEffect(() => {
    if (runtimeImageOptions.length === 0) {
      setSelectedRuntimeImageKey("");
      return;
    }

    setSelectedRuntimeImageKey((current) =>
      runtimeImageOptions.some(
        (item) => getRuntimeImageOptionKey(item) === current,
      )
        ? current
        : getRuntimeImageOptionKey(runtimeImageOptions[0]),
    );
  }, [runtimeImageOptions]);

  useEffect(() => {
    const nextTotalPages = Math.max(
      1,
      Math.ceil(availableSkills.length / SKILLS_PER_PAGE),
    );
    setSkillPage((current) => Math.min(current, nextTotalPages));
  }, [availableSkills.length]);

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
    const instanceType = availableTypes.find((t) => t.id === typeId);
    if (instanceType) {
      if (!supportsRuntimeInjection(typeId)) {
        setOpenClawImportFile(null);
        setOpenClawInjectionMode("none");
        setOpenClawBundleId(undefined);
        setOpenClawResourceIds([]);
        setOpenClawPreview(null);
        setOpenClawPreviewError(null);
        setSelectedSkillIds([]);
      }
      setFormData({
        ...formData,
        type: typeId as CreateInstanceRequest["type"],
        os_type: instanceType.defaultOs,
        os_version: instanceType.defaultVersion,
        storage_class: "",
      });
    }
  };

  const handlePresetSelect = (
    preset: keyof typeof PRESET_CONFIGS | typeof CUSTOM_RESOURCE_PRESET,
  ) => {
    if (preset === CUSTOM_RESOURCE_PRESET) {
      setResourcePresetMode(CUSTOM_RESOURCE_PRESET);
      setFormData((current) => ({
        ...current,
        cpu_cores: PRESET_CONFIGS.medium.cpu_cores,
        memory_gb: PRESET_CONFIGS.medium.memory_gb,
        disk_gb: PRESET_CONFIGS.medium.disk_gb,
      }));
      return;
    }

    const config = PRESET_CONFIGS[preset];
    setResourcePresetMode(preset);
    setFormData((current) => ({
      ...current,
      cpu_cores: config.cpu_cores,
      memory_gb: config.memory_gb,
      disk_gb: config.disk_gb,
    }));
  };

  const createCustomEnvRow = (): CustomEnvRow => ({
    id: `custom-env-${nextCustomEnvIdRef.current++}`,
    name: "",
    value: "",
  });

  const addBuiltInOverride = () => {
    if (availableBuiltInTemplates.length === 0) {
      return;
    }

    const nextKey = availableBuiltInTemplates[0].key;
    setBuiltinEnvOverrides((current) => ({
      ...current,
      [nextKey]: "",
    }));
  };

  const removeBuiltInOverride = (keyToRemove: string) => {
    setBuiltinEnvOverrides((current) => {
      const next = { ...current };
      delete next[keyToRemove];
      return next;
    });
  };

  const updateBuiltInOverrideKey = (previousKey: string, nextKey: string) => {
    if (previousKey === nextKey || !nextKey) {
      return;
    }

    setBuiltinEnvOverrides((current) => {
      const next = { ...current };
      const previousValue = next[previousKey] ?? "";
      delete next[previousKey];
      next[nextKey] = previousValue;
      return next;
    });
  };

  const buildEnvironmentOverridesPayload = () => {
    const overrides: Record<string, string> = {};
    const builtInKeys = new Set(builtInEnvTemplates.map((item) => item.key));

    for (const template of builtInEnvTemplates) {
      if (
        Object.prototype.hasOwnProperty.call(builtinEnvOverrides, template.key)
      ) {
        overrides[template.key] = builtinEnvOverrides[template.key];
      }
    }

    const seenCustomNames = new Set<string>();

    for (const row of customEnvRows) {
      const name = row.name.trim();
      const hasName = name.length > 0;
      const hasValue = row.value.length > 0;

      if (!hasName && !hasValue) {
        continue;
      }

      if (!hasName) {
        return { error: t("instances.customEnvNameRequired") };
      }

      if (!ENV_NAME_PATTERN.test(name)) {
        return { error: t("instances.invalidEnvName", { name }) };
      }

      if (builtInKeys.has(name)) {
        return {
          error: t("instances.reservedBuiltinEnvName", { name }),
        };
      }

      if (seenCustomNames.has(name)) {
        return { error: t("instances.duplicateEnvName", { name }) };
      }

      seenCustomNames.add(name);
      overrides[name] = row.value;
    }

    return {
      overrides: Object.keys(overrides).length > 0 ? overrides : undefined,
    };
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    // Only submit on step 3
    if (step !== 3 || !submitArmed || loading) {
      return;
    }

    try {
      const { overrides, error: environmentError } =
        buildEnvironmentOverridesPayload();
      if (environmentError) {
        setError(environmentError);
        return;
      }

      setLoading(true);
      setError(null);
      const createPayload: CreateInstanceRequest = {
        ...formData,
        mode: selectedMode,
        instance_mode: selectedMode,
        runtime_type: selectedRuntimeType,
        cpu_cores: usesDedicatedResources
          ? formData.cpu_cores
          : PRESET_CONFIGS.small.cpu_cores,
        memory_gb: usesDedicatedResources
          ? formData.memory_gb
          : PRESET_CONFIGS.small.memory_gb,
        disk_gb: usesDedicatedResources
          ? formData.disk_gb
          : PRESET_CONFIGS.small.disk_gb,
        gpu_enabled: usesDedicatedResources ? formData.gpu_enabled : false,
        gpu_count: usesDedicatedResources ? formData.gpu_count : 0,
        desktop_stream_profile:
          selectedRuntimeType === "desktop" ? desktopStreamProfile : undefined,
        image_registry: selectedRuntimeImage?.image,
        image_tag: selectedRuntimeImage ? undefined : formData.image_tag,
        environment_overrides: overrides,
        skill_ids: supportsRuntimeInjection(formData.type)
          ? selectedSkillIds
          : undefined,
        openclaw_config_plan:
          supportsRuntimeInjection(formData.type) &&
          openClawInjectionMode === "bundle" &&
          openClawBundleId
            ? { mode: "bundle", bundle_id: openClawBundleId }
            : supportsRuntimeInjection(formData.type) &&
                openClawInjectionMode === "manual" &&
                openClawResourceIds.length > 0
              ? { mode: "manual", resource_ids: openClawResourceIds }
              : undefined,
      };

      const createdInstance =
        await instanceService.createInstance(createPayload);

      if (
        supportsRuntimeInjection(formData.type) &&
        openClawInjectionMode === "archive" &&
        openClawImportFile
      ) {
        await waitForInstanceRunning(createdInstance.id);
        if (formData.type === "hermes") {
          await instanceService.importHermesWorkspace(
            createdInstance.id,
            openClawImportFile,
          );
        } else {
          await instanceService.importOpenClawWorkspace(
            createdInstance.id,
            openClawImportFile,
          );
        }
      }

      navigate("/instances");
    } catch (err: unknown) {
      const responseError = err as AxiosError<{ error?: string }>;
      setError(getCreateErrorMessage(responseError.response?.data?.error));
    } finally {
      setLoading(false);
    }
  };

  const canProceed = () => {
    if (step === 1) return formData.name.length >= 3;
    if (step === 2) return availableTypes.length > 0;
    return true;
  };

  useEffect(() => {
    if (step !== 3) {
      setSubmitArmed(false);
      return;
    }

    const timer = window.setTimeout(() => {
      setSubmitArmed(true);
    }, 150);

    return () => window.clearTimeout(timer);
  }, [step]);

  const waitForInstanceRunning = async (instanceId: number) => {
    const timeoutAt = Date.now() + 5 * 60 * 1000;

    while (Date.now() < timeoutAt) {
      const current = await instanceService.getInstance(instanceId);
      if (current.status === "running") {
        return;
      }
      if (current.status === "error") {
        throw new Error(t("instances.waitingForRunningState"));
      }
      await new Promise((resolve) => window.setTimeout(resolve, 5000));
    }

    throw new Error(t("instances.timedOutWaitingForRunning"));
  };

  const usedResources = {
    instances: instances.length,
    cpu: instances.reduce(
      (sum, instance) =>
        instanceUsesDedicatedResources(instance) ? sum + instance.cpu_cores : sum,
      0,
    ),
    memory: instances.reduce(
      (sum, instance) =>
        instanceUsesDedicatedResources(instance) ? sum + instance.memory_gb : sum,
      0,
    ),
    storage: instances.reduce(
      (sum, instance) =>
        instanceUsesDedicatedResources(instance) ? sum + instance.disk_gb : sum,
      0,
    ),
    gpu: instances.reduce(
      (sum, instance) =>
        instanceUsesDedicatedResources(instance) && instance.gpu_enabled
          ? sum + instance.gpu_count
          : sum,
      0,
    ),
  };

  const quotaChecks = quota
    ? [
        {
          key: "instances",
          label: t("instances.quotaInstances"),
          next: usedResources.instances + 1,
          max: quota.max_instances,
          exceeded: usedResources.instances + 1 > quota.max_instances,
        },
        ...(usesDedicatedResources
          ? [
              {
                key: "cpu",
                label: t("common.cpu"),
                next: usedResources.cpu + formData.cpu_cores,
                max: quota.max_cpu_cores,
                exceeded:
                  usedResources.cpu + formData.cpu_cores >
                  quota.max_cpu_cores,
              },
              {
                key: "memory",
                label: t("instances.memoryLabel"),
                next: usedResources.memory + formData.memory_gb,
                max: quota.max_memory_gb,
                exceeded:
                  usedResources.memory + formData.memory_gb >
                  quota.max_memory_gb,
              },
              {
                key: "storage",
                label: t("instances.storageLabel"),
                next: usedResources.storage + formData.disk_gb,
                max: quota.max_storage_gb,
                exceeded:
                  usedResources.storage + formData.disk_gb >
                  quota.max_storage_gb,
              },
              {
                key: "gpu",
                label: t("instances.gpuLabel"),
                next:
                  usedResources.gpu +
                  (formData.gpu_enabled ? formData.gpu_count || 0 : 0),
                max: quota.max_gpu_count,
                exceeded:
                  usedResources.gpu +
                    (formData.gpu_enabled ? formData.gpu_count || 0 : 0) >
                  quota.max_gpu_count,
              },
            ]
          : []),
      ]
    : [];

  const exceededQuotaItems = quotaChecks.filter((item) => item.exceeded);
  const quotaExceeded = exceededQuotaItems.length > 0;
  const openClawPlanInvalid =
    supportsRuntimeInjection(formData.type) &&
    ((openClawInjectionMode === "bundle" &&
      (!openClawBundleId ||
        !!openClawPreviewError ||
        openClawPreviewLoading)) ||
      (openClawInjectionMode === "manual" &&
        (!!openClawPreviewError || openClawPreviewLoading)) ||
      (openClawInjectionMode === "archive" && !openClawImportFile));
  const createDisabled =
    loading ||
    !submitArmed ||
    quotaLoading ||
    !quota ||
    quotaExceeded ||
    openClawPlanInvalid;

  const handleOpenClawPreviewChange = React.useCallback(
    (
      preview: OpenClawConfigCompilePreview | null,
      state: { loading: boolean; error: string | null },
    ) => {
      setOpenClawPreview(preview);
      setOpenClawPreviewLoading(state.loading);
      setOpenClawPreviewError(state.error);
    },
    [],
  );

  const environmentDraft = buildEnvironmentOverridesPayload();
  const environmentOverrideNames = environmentDraft.overrides
    ? Object.keys(environmentDraft.overrides)
    : [];
  const builtinOverrideCount = selectedBuiltInTemplates.length;
  const selectedSkillNames = selectedSkillIds.map(
    (skillId) =>
      availableSkills.find((skill) => skill.id === skillId)?.name ||
      `Skill #${skillId}`,
  );
  const totalSkillPages = Math.max(
    1,
    Math.ceil(availableSkills.length / SKILLS_PER_PAGE),
  );
  const skillRiskPolicySummary = locale.startsWith("zh")
    ? `\u5b89\u5168\u7b56\u7565\u5df2\u9690\u85cf ${skillInventorySummary.hiddenByRisk} \u4e2a\u4e2d/\u9ad8\u98ce\u9669\u6280\u80fd\uff1b\u5f53\u524d\u53ef\u9009 ${availableSkills.length}/${skillInventorySummary.total} \u4e2a\u3002`
    : `Risk policy hid ${skillInventorySummary.hiddenByRisk} medium/high-risk skills; ${availableSkills.length}/${skillInventorySummary.total} are available.`;
  const paginatedSkills = availableSkills.slice(
    (skillPage - 1) * SKILLS_PER_PAGE,
    skillPage * SKILLS_PER_PAGE,
  );
  const resolvedChannelNames = (openClawPreview?.resolved_resources || [])
    .filter((resource) => resource.resource_type === "channel")
    .map((resource) => resource.name);

  const renderTypeIcon = (typeId: string) => {
    if (typeId === "openclaw") {
      return (
        <img
          src="/openclaw.png"
          alt="OpenClaw"
          className="h-10 w-10 object-contain"
        />
      );
    }

    if (typeId === "hermes") {
      return (
        <img
          src="/hermes.png"
          alt="Hermes"
          className="h-10 w-10 object-contain"
        />
      );
    }

    return (
      <svg
        className="h-6 w-6 text-indigo-600"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
      >
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
        />
      </svg>
    );
  };

  const renderSummaryTagList = (
    items: string[],
    emptyText: string,
    tone: "neutral" | "indigo" | "emerald" = "neutral",
  ) => {
    if (items.length === 0) {
      return <p className="mt-2 text-sm text-gray-400">{emptyText}</p>;
    }

    const toneClassName =
      tone === "indigo"
        ? "border-indigo-200 bg-indigo-50 text-indigo-700"
        : tone === "emerald"
          ? "border-emerald-200 bg-emerald-50 text-emerald-700"
          : "border-gray-200 bg-gray-50 text-gray-700";

    return (
      <div className="mt-2 flex flex-wrap gap-2">
        {items.map((item, index) => (
          <span
            key={`${item}-${index}`}
            className={`rounded-full border px-3 py-1 text-xs font-medium ${toneClassName}`}
          >
            {item}
          </span>
        ))}
      </div>
    );
  };

  return (
    <UserLayout>
      {/* Progress Bar */}
      <div className="app-panel mb-6">
        <div className="w-full px-4 sm:px-6 lg:px-8">
          <div className="flex items-center py-4">
            <h1 className="text-xl font-bold text-gray-900 mr-8">
              {t("instances.createTitle")}
            </h1>
            <div className="flex items-center">
              {[1, 2, 3].map((s) => (
                <React.Fragment key={s}>
                  <div
                    className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-medium ${
                      s === step
                        ? "bg-indigo-600 text-white"
                        : s < step
                          ? "bg-green-500 text-white"
                          : "bg-gray-200 text-gray-600"
                    }`}
                  >
                    {s < step ? "\u2713" : s}
                  </div>
                  {s < 3 && (
                    <div
                      className={`w-16 h-0.5 mx-2 ${s < step ? "bg-green-500" : "bg-gray-200"}`}
                    />
                  )}
                </React.Fragment>
              ))}
            </div>
            <span className="ml-4 text-sm text-gray-500">
              {t("instances.stepOf", {
                step,
                label:
                  step === 1
                    ? t("instances.stepBasic")
                    : step === 2
                      ? t("instances.stepType")
                      : t("instances.stepConfig"),
              })}
            </span>
          </div>
        </div>
      </div>

      {/* Main Content */}
      <div className="w-full">
        {error && (
          <div className="mb-4 bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded">
            {error}
            <button
              type="button"
              onClick={() => setError(null)}
              className="float-right text-red-500 hover:text-red-700"
            >
              \u00d7
            </button>
          </div>
        )}

        <form
          onSubmit={handleSubmit}
          onKeyDown={(e) => {
            // Prevent Enter key from submitting form in step 1 and 2
            if (e.key === "Enter" && step < 3) {
              e.preventDefault();
              if (canProceed()) {
                setStep(step + 1);
              }
            }
          }}
        >
          {/* Step 1: Basic Information */}
          {step === 1 && (
            <div className="app-panel p-6">
              <h2 className="text-lg font-medium text-gray-900 mb-4">
                {t("instances.basicInformation")}
              </h2>
              <div className="space-y-4">
                <div>
                  <label
                    htmlFor="name"
                    className="block text-sm font-medium text-gray-700"
                  >
                    {t("instances.instanceName")}
                  </label>
                  <input
                    type="text"
                    id="name"
                    value={formData.name}
                    onChange={(e) =>
                      setFormData({ ...formData, name: e.target.value })
                    }
                    className="app-input mt-1 block w-full"
                    placeholder={t("instances.instanceNamePlaceholder")}
                    minLength={3}
                    required
                  />
                  <p className="mt-1 text-xs text-gray-500">
                    {t("instances.minimumThreeChars")}
                  </p>
                </div>

                <div>
                  <label
                    htmlFor="description"
                    className="block text-sm font-medium text-gray-700"
                  >
                    {t("instances.descriptionOptional")}
                  </label>
                  <textarea
                    id="description"
                    value={formData.description || ""}
                    onChange={(e) =>
                      setFormData({ ...formData, description: e.target.value })
                    }
                    rows={3}
                    className="app-input mt-1 block w-full"
                    placeholder={t("instances.descriptionPlaceholder")}
                  />
                </div>

                {renderInstanceModeSelector()}
              </div>
            </div>
          )}

          {/* Step 2: Select Type */}
          {step === 2 && (
            <div className="app-panel p-6">
              <h2 className="text-lg font-medium text-gray-900 mb-4">
                {t("instances.selectInstanceType")}
              </h2>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {availableTypes.map((type) => (
                  <div
                    key={type.id}
                    onClick={() => handleTypeSelect(type.id)}
                    className={`relative cursor-pointer rounded-[24px] border p-4 transition-all hover:-translate-y-0.5 hover:shadow-[0_24px_56px_-42px_rgba(72,44,24,0.55)] ${
                      formData.type === type.id
                        ? "border-indigo-500 ring-2 ring-indigo-500"
                        : "border-gray-300"
                    }`}
                  >
                    <div className="flex items-center">
                      <div className="flex-shrink-0">
                        <div className="h-12 w-12 rounded-lg bg-indigo-100 flex items-center justify-center">
                          {renderTypeIcon(type.id)}
                        </div>
                      </div>
                      <div className="ml-4">
                        <h3 className="text-sm font-medium text-gray-900">
                          {getInstanceTypeLabel(t, type.id, type.name)}
                        </h3>
                        <p className="mt-1 text-xs text-gray-500">
                          {getInstanceTypeDescription(
                            t,
                            type.id,
                            type.description,
                          )}
                        </p>
                      </div>
                    </div>
                    {formData.type === type.id && (
                      <div className="absolute top-2 right-2">
                        <svg
                          className="h-5 w-5 text-indigo-600"
                          fill="currentColor"
                          viewBox="0 0 20 20"
                        >
                          <path
                            fillRule="evenodd"
                            d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z"
                            clipRule="evenodd"
                          />
                        </svg>
                      </div>
                    )}
                  </div>
                ))}
              </div>

              {showRuntimeImageSelector && (
                <div className="mt-6 rounded-[24px] border border-[#ead8cf] bg-[rgba(255,248,245,0.72)] p-5">
                  <div>
                    <h3 className="text-sm font-semibold uppercase tracking-[0.18em] text-[#b46c50]">
                      {t("instances.instanceImage")}
                    </h3>
                    <p className="mt-1 text-sm text-gray-500">
                      {t("instances.runtimeImageSelectionHint")}
                    </p>
                  </div>

                  {runtimeImageOptions.length === 0 ? (
                    <p className="mt-4 text-sm text-gray-500">
                      {t("instances.runtimeImageUnavailable")}
                    </p>
                  ) : (
                    <div className="mt-4 grid grid-cols-1 gap-3 lg:grid-cols-2">
                      {runtimeImageOptions.map((item) => {
                        const optionKey = getRuntimeImageOptionKey(item);
                        const selected = optionKey === selectedRuntimeImageKey;

                        return (
                          <button
                            key={optionKey}
                            type="button"
                            onClick={() => setSelectedRuntimeImageKey(optionKey)}
                            className={`rounded-[20px] border p-4 text-left transition-all hover:-translate-y-0.5 hover:shadow-[0_24px_56px_-42px_rgba(72,44,24,0.55)] ${
                              selected
                                ? "border-indigo-500 bg-white ring-2 ring-indigo-500"
                                : "border-[#ead8cf] bg-white"
                            }`}
                          >
                            <div className="flex items-start justify-between gap-3">
                              <div>
                                <div className="flex flex-wrap items-center gap-2">
                                  <h4 className="text-sm font-semibold text-gray-900">
                                    {item.display_name}
                                  </h4>
                                  <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                                    normalizeRuntimeImageType(item.runtime_type) === "gateway"
                                      ? "bg-emerald-50 text-emerald-700"
                                      : "bg-indigo-50 text-indigo-700"
                                  }`}>
                                    {t(
                                      normalizeRuntimeImageType(item.runtime_type) === "gateway"
                                        ? "instances.runtimeTypeGateway"
                                        : "instances.runtimeTypeDesktop",
                                    )}
                                  </span>
                                </div>
                                <p className="mt-1 text-xs uppercase tracking-[0.16em] text-[#b46c50]">
                                  {getInstanceTypeLabel(
                                    t,
                                    item.instance_type,
                                    item.instance_type,
                                  )}
                                </p>
                              </div>
                              {selected && (
                                <span className="rounded-full bg-indigo-100 px-2.5 py-1 text-xs font-medium text-indigo-700">\u2713</span>
                              )}
                            </div>
                            <p className="mt-3 break-all rounded-2xl bg-[#f8f5f2] px-3 py-2 font-mono text-xs text-[#5f5957]">
                              {item.image}
                            </p>
                          </button>
                        );
                      })}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {/* Step 3: Configuration */}
          {step === 3 && (
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1fr)_360px] xl:items-start">
              <div className="flex flex-col gap-6">
                {usesDedicatedResources && (
                  <div className="app-panel order-1 p-6">
                    <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                      <div>
                        <h2 className="text-lg font-medium text-gray-900">
                          {t("instances.quickConfiguration")}
                        </h2>
                      </div>
                      <button
                        type="button"
                        onClick={() =>
                          setFormData((current) => ({
                            ...current,
                            gpu_enabled: !current.gpu_enabled,
                            gpu_count: !current.gpu_enabled ? 1 : 0,
                          }))
                        }
                        className={`inline-flex items-center justify-between gap-3 self-start rounded-full border px-3 py-2 text-sm font-medium transition ${
                          formData.gpu_enabled
                            ? "border-emerald-200 bg-emerald-50 text-emerald-700"
                            : "border-gray-200 bg-white text-gray-600"
                        }`}
                        aria-pressed={formData.gpu_enabled}
                      >
                        <span className="whitespace-nowrap">
                          {t("instances.enableGpu")}
                        </span>
                        <span
                          className={`relative h-6 w-11 rounded-full transition ${
                            formData.gpu_enabled
                              ? "bg-emerald-500"
                              : "bg-gray-300"
                          }`}
                          aria-hidden="true"
                        >
                          <span
                            className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition ${
                              formData.gpu_enabled ? "left-[22px]" : "left-0.5"
                            }`}
                          />
                        </span>
                      </button>
                    </div>

                    <div className="mt-5 grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
                      {Object.entries(PRESET_CONFIGS).map(([key, config]) => {
                        const selected = resourcePresetMode === key;

                        return (
                          <button
                            key={key}
                            type="button"
                            onClick={() =>
                              handlePresetSelect(
                                key as keyof typeof PRESET_CONFIGS,
                              )
                            }
                            className={`flex h-[188px] flex-col justify-between rounded-[22px] border p-4 text-left transition-all hover:-translate-y-0.5 hover:shadow-[0_24px_56px_-42px_rgba(72,44,24,0.55)] ${
                              selected
                                ? "border-indigo-500 ring-2 ring-indigo-500"
                                : "border-gray-300"
                            }`}
                          >
                            <h3 className="font-medium text-gray-900">
                              {getPresetLabel(t, key, config.name)}
                            </h3>
                            <p className="mt-1 text-sm text-gray-500">
                              {getPresetDescription(
                                t,
                                key,
                                config.description,
                              )}
                            </p>
                            <div className="mt-2 text-sm text-gray-600">
                              {t("instances.resourcePresetSummary", {
                                cpu: config.cpu_cores,
                                memory: config.memory_gb,
                                disk: config.disk_gb,
                              })}
                            </div>
                          </button>
                        );
                      })}

                      <div
                        role="button"
                        tabIndex={0}
                        onClick={() =>
                          handlePresetSelect(CUSTOM_RESOURCE_PRESET)
                        }
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            handlePresetSelect(CUSTOM_RESOURCE_PRESET);
                          }
                        }}
                        className={`flex h-[188px] flex-col justify-between overflow-hidden rounded-[22px] border p-4 text-left transition-all hover:-translate-y-0.5 hover:shadow-[0_24px_56px_-42px_rgba(72,44,24,0.55)] ${
                          resourcePresetMode === CUSTOM_RESOURCE_PRESET
                            ? "border-indigo-500 ring-2 ring-indigo-500 bg-indigo-50"
                            : "border-gray-300"
                        }`}
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div>
                            <h3 className="font-medium text-gray-900">
                              {t("instances.customPresetTitle")}
                            </h3>
                            <p className="mt-1 text-sm text-gray-500">
                              {t("instances.customPresetDescription")}
                            </p>
                          </div>
                          {resourcePresetMode === CUSTOM_RESOURCE_PRESET && (
                            <span className="rounded-full bg-indigo-100 px-2.5 py-1 text-xs font-medium text-indigo-700">
                              {t("instances.customPresetEditing")}
                            </span>
                          )}
                        </div>

                        {resourcePresetMode === CUSTOM_RESOURCE_PRESET ? (
                          <div className="mt-3 grid grid-cols-3 gap-2">
                            <div className="rounded-2xl border border-indigo-200 bg-white/90 px-2 py-2">
                              <label
                                htmlFor="custom_cpu"
                                className="block text-[10px] font-semibold uppercase tracking-[0.18em] text-gray-500"
                              >
                                {t("instances.presetCpuShort")}
                              </label>
                              <input
                                type="number"
                                id="custom_cpu"
                                min={0.1}
                                max={32}
                                step={0.1}
                                value={formData.cpu_cores}
                                onChange={(e) =>
                                  setFormData((current) => ({
                                    ...current,
                                    cpu_cores:
                                      parseFloat(e.target.value) || 0.1,
                                  }))
                                }
                                className="mt-1 h-9 w-full rounded-xl border border-gray-200 bg-white px-2 text-sm font-medium text-gray-900 outline-none transition focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                                onClick={(e) => e.stopPropagation()}
                              />
                            </div>
                            <div className="rounded-2xl border border-indigo-200 bg-white/90 px-2 py-2">
                              <label
                                htmlFor="custom_memory"
                                className="block text-[10px] font-semibold uppercase tracking-[0.18em] text-gray-500"
                              >
                                {t("instances.presetRamShort")}
                              </label>
                              <input
                                type="number"
                                id="custom_memory"
                                min={1}
                                max={128}
                                value={formData.memory_gb}
                                onChange={(e) =>
                                  setFormData((current) => ({
                                    ...current,
                                    memory_gb:
                                      parseInt(e.target.value) || 1,
                                  }))
                                }
                                className="mt-1 h-9 w-full rounded-xl border border-gray-200 bg-white px-2 text-sm font-medium text-gray-900 outline-none transition focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                                onClick={(e) => e.stopPropagation()}
                              />
                            </div>
                            <div className="rounded-2xl border border-indigo-200 bg-white/90 px-2 py-2">
                              <label
                                htmlFor="custom_disk"
                                className="block text-[10px] font-semibold uppercase tracking-[0.18em] text-gray-500"
                              >
                                {t("instances.presetDiskShort")}
                              </label>
                              <input
                                type="number"
                                id="custom_disk"
                                min={10}
                                max={1000}
                                value={formData.disk_gb}
                                onChange={(e) =>
                                  setFormData((current) => ({
                                    ...current,
                                    disk_gb: parseInt(e.target.value) || 10,
                                  }))
                                }
                                className="mt-1 h-9 w-full rounded-xl border border-gray-200 bg-white px-2 text-sm font-medium text-gray-900 outline-none transition focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                                onClick={(e) => e.stopPropagation()}
                              />
                            </div>
                          </div>
                        ) : (
                          <div className="mt-3 text-sm text-gray-600">
                            {t("instances.mediumDefaultPreset", {
                              cpu: PRESET_CONFIGS.medium.cpu_cores,
                              memory: PRESET_CONFIGS.medium.memory_gb,
                              disk: PRESET_CONFIGS.medium.disk_gb,
                            })}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                )}

                {selectedRuntimeType === "desktop" && (
                  <div className="mt-6 rounded-[24px] border border-gray-200 bg-gray-50/80 p-4">
                    <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                      <div>
                        <h3 className="text-sm font-semibold uppercase tracking-[0.2em] text-gray-500">
                          {t("instances.desktopStreamProfile")}
                        </h3>
                        <p className="mt-2 text-sm text-gray-500">
                          {t("instances.desktopStreamProfileDesc")}
                        </p>
                      </div>
                      <span className="rounded-full bg-white px-3 py-1 text-xs font-medium text-gray-500">
                        {t("instances.restartRequiredAfterChange")}
                      </span>
                    </div>

                    <div className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-3">
                      {DESKTOP_STREAM_PROFILES.map((profile) => {
                        const selected = desktopStreamProfile === profile.id;
                        return (
                          <button
                            key={profile.id}
                            type="button"
                            onClick={() => setDesktopStreamProfile(profile.id)}
                            className={`flex min-h-[138px] flex-col justify-between rounded-[20px] border p-4 text-left transition ${
                              selected
                                ? "border-indigo-500 bg-white ring-2 ring-indigo-500"
                                : "border-gray-200 bg-white hover:border-indigo-200"
                            }`}
                          >
                            <div>
                              <h4 className="text-sm font-semibold text-gray-900">
                                {t(profile.labelKey)}
                              </h4>
                              <p className="mt-2 text-sm leading-5 text-gray-500">
                                {t(profile.descriptionKey)}
                              </p>
                            </div>
                            <p className="mt-4 font-mono text-xs text-gray-400">
                              {profile.details}
                            </p>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                )}

                <div className="app-panel order-3 p-6">
                  <div className="flex items-start justify-between gap-4">
                    <div>
                      <h2 className="text-lg font-medium text-gray-900">
                        {t("instances.environmentVariables")}
                      </h2>
                    </div>
                  </div>

                  {builtInEnvTemplates.length > 0 && (
                    <div className="mt-5 rounded-[24px] border border-gray-200 bg-gray-50/80 p-4">
                      <div className="flex items-start justify-between gap-4">
                        <div>
                          <h3 className="text-sm font-semibold uppercase tracking-[0.2em] text-gray-500">
                            {t("instances.clawManagerBuiltIns")}
                          </h3>
                        </div>
                        <button
                          type="button"
                          onClick={() =>
                            setShowBuiltInEnvEditor((current) => !current)
                          }
                          className="rounded-lg border border-indigo-200 bg-white px-4 py-2 text-sm font-medium text-indigo-700 hover:bg-indigo-50"
                        >
                          {showBuiltInEnvEditor
                            ? t("instances.hideBuiltIns")
                            : builtinOverrideCount > 0
                              ? t("instances.editBuiltInsCount", {
                                  count: builtinOverrideCount,
                                })
                              : t("instances.editBuiltIns")}
                        </button>
                      </div>

                      {!showBuiltInEnvEditor ? (
                        <div className="mt-4 rounded-2xl border border-dashed border-gray-300 bg-white px-4 py-4 text-sm text-gray-500">
                          {builtinOverrideCount > 0
                            ? t("instances.builtinOverridesConfigured", {
                                count: builtinOverrideCount,
                              })
                            : t("instances.usingBuiltInDefaults", {
                                count: builtInEnvTemplates.length,
                              })}
                        </div>
                      ) : (
                        <div className="mt-4 space-y-3">
                          <div className="flex flex-wrap items-center justify-between gap-3">
                            <p className="text-sm text-gray-500">
                              {t("instances.builtinOverrideHelp")}
                            </p>
                            <button
                              type="button"
                              onClick={addBuiltInOverride}
                              disabled={availableBuiltInTemplates.length === 0}
                              className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                            >
                              {t("instances.addBuiltinVariable")}
                            </button>
                          </div>

                          {selectedBuiltInTemplates.length === 0 ? (
                            <div className="rounded-2xl border border-dashed border-gray-300 bg-white px-4 py-4 text-sm text-gray-500">
                              {t("instances.noBuiltinOverrides")}
                            </div>
                          ) : (
                            selectedBuiltInTemplates.map((template) => {
                              const selectableTemplates = [
                                template,
                                ...availableBuiltInTemplates,
                              ];
                              const defaultSummary =
                                template.defaultValue !== undefined
                                  ? t("instances.defaultValue", {
                                      value: template.defaultValue,
                                    })
                                  : t("instances.defaultValue", {
                                      value:
                                        template.defaultLabel ||
                                        t("instances.envManagedAtRuntime"),
                                    });

                              return (
                                <div
                                  key={template.key}
                                  className="rounded-[22px] border border-gray-200 bg-white p-4"
                                >
                                  <div className="grid grid-cols-1 gap-4 lg:grid-cols-[220px_minmax(0,1fr)_auto]">
                                    <div>
                                      <label className="block text-xs font-semibold uppercase tracking-[0.18em] text-gray-500">
                                        {t("instances.variable")}
                                      </label>
                                      <select
                                        value={template.key}
                                        onChange={(e) =>
                                          updateBuiltInOverrideKey(
                                            template.key,
                                            e.target.value,
                                          )
                                        }
                                        className="app-input mt-1 w-full font-mono"
                                      >
                                        {selectableTemplates.map((option) => (
                                          <option
                                            key={option.key}
                                            value={option.key}
                                          >
                                            {option.key}
                                          </option>
                                        ))}
                                      </select>
                                    </div>

                                    <div>
                                      <label className="block text-xs font-semibold uppercase tracking-[0.18em] text-gray-500">
                                        {t("instances.overrideValue")}
                                      </label>
                                      <input
                                        type="text"
                                        value={
                                          builtinEnvOverrides[template.key] ??
                                          ""
                                        }
                                        onChange={(e) =>
                                          setBuiltinEnvOverrides((current) => ({
                                            ...current,
                                            [template.key]: e.target.value,
                                          }))
                                        }
                                        className="app-input mt-1 block w-full font-mono"
                                        placeholder={
                                          template.defaultValue ??
                                          template.defaultLabel ??
                                          t(
                                            "instances.overrideValuePlaceholder",
                                          )
                                        }
                                      />
                                      <p className="mt-2 text-xs text-gray-400">
                                        {defaultSummary}
                                      </p>
                                    </div>

                                    <div className="flex h-full flex-col">
                                      <span
                                        className="block select-none text-xs font-semibold uppercase tracking-[0.18em] text-transparent"
                                        aria-hidden="true"
                                      >
                                        {t("instances.actionLabel")}
                                      </span>
                                      <button
                                        type="button"
                                        onClick={() =>
                                          removeBuiltInOverride(template.key)
                                        }
                                        className="mt-1 h-[46px] rounded-lg border border-red-200 bg-red-50 px-4 text-sm font-medium text-red-700 hover:bg-red-100"
                                      >
                                        {t("instances.remove")}
                                      </button>
                                    </div>
                                  </div>

                                  <p className="mt-3 text-sm text-gray-500">
                                    {template.description}
                                  </p>
                                </div>
                              );
                            })
                          )}
                        </div>
                      )}
                    </div>
                  )}

                  <div
                    className={`${builtInEnvTemplates.length > 0 ? "mt-8" : "mt-5"} rounded-[24px] border border-gray-200 bg-gray-50/80 p-4`}
                  >
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <h3 className="text-sm font-semibold uppercase tracking-[0.2em] text-gray-500">
                          {t("instances.customVariables")}
                        </h3>
                      </div>
                      <button
                        type="button"
                        onClick={() =>
                          setCustomEnvRows((current) => [
                            ...current,
                            createCustomEnvRow(),
                          ])
                        }
                        className="app-button-secondary"
                      >
                        {t("instances.addVariable")}
                      </button>
                    </div>

                    {customEnvRows.length === 0 ? (
                      <div className="mt-4 rounded-[22px] border border-dashed border-gray-300 bg-white px-4 py-5 text-sm text-gray-500">
                        {t("instances.noCustomEnvironmentVariables")}
                      </div>
                    ) : (
                      <div className="mt-4 space-y-3">
                        {customEnvRows.map((row) => (
                          <div
                            key={row.id}
                            className="grid grid-cols-1 gap-3 rounded-[22px] border border-gray-200 bg-white p-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]"
                          >
                            <input
                              type="text"
                              value={row.name}
                              onChange={(e) =>
                                setCustomEnvRows((current) =>
                                  current.map((item) =>
                                    item.id === row.id
                                      ? { ...item, name: e.target.value }
                                      : item,
                                  ),
                                )
                              }
                              className="app-input w-full font-mono"
                              placeholder={t(
                                "instances.variableNamePlaceholder",
                              )}
                            />
                            <input
                              type="text"
                              value={row.value}
                              onChange={(e) =>
                                setCustomEnvRows((current) =>
                                  current.map((item) =>
                                    item.id === row.id
                                      ? { ...item, value: e.target.value }
                                      : item,
                                  ),
                                )
                              }
                              className="app-input w-full font-mono"
                              placeholder={t(
                                "instances.variableValuePlaceholder",
                              )}
                            />
                            <button
                              type="button"
                              onClick={() =>
                                setCustomEnvRows((current) =>
                                  current.filter((item) => item.id !== row.id),
                                )
                              }
                              className="rounded-lg border border-red-200 bg-red-50 px-4 py-2 text-sm font-medium text-red-700 hover:bg-red-100"
                            >
                              {t("instances.remove")}
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  <div className="mt-8 flex flex-col-reverse gap-3 border-t border-gray-200 pt-6 sm:flex-row sm:items-center sm:justify-between">
                    <button
                      type="button"
                      onClick={() => setStep(2)}
                      className="app-button-secondary self-start"
                    >
                      {t("instances.back")}
                    </button>

                    <button
                      type="submit"
                      disabled={createDisabled}
                      className="app-button-primary self-start disabled:cursor-not-allowed disabled:opacity-50 sm:self-auto"
                    >
                      {quotaLoading
                        ? t("instances.checkingQuota")
                        : loading
                          ? supportsRuntimeInjection(formData.type) &&
                            openClawInjectionMode === "archive" &&
                            openClawImportFile
                            ? t("instances.creatingAndImporting")
                            : t("instances.creatingNow")
                          : t("instances.createNow")}
                    </button>
                  </div>
                </div>

                {supportsRuntimeInjection(formData.type) && (
                  <div className="app-panel order-2 p-6">
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <h2 className="text-lg font-medium text-gray-900">
                          {t("instances.runtimeInjection", {
                            runtime: runtimeProductName(formData.type),
                          })}
                        </h2>
                      </div>
                      <span className="rounded-full bg-indigo-50 px-3 py-1 text-xs font-medium text-indigo-600">
                        {t("instances.optional")}
                      </span>
                    </div>

                    <div className="mt-5">
                      <OpenClawConfigPlanSection
                        embedded
                        hideHeader
                        mode={openClawInjectionMode}
                        bundleId={openClawBundleId}
                        resourceIds={openClawResourceIds}
                        onModeChange={(nextMode) => {
                          setOpenClawInjectionMode(nextMode);
                          setOpenClawPreview(null);
                          setOpenClawPreviewError(null);
                          if (nextMode !== "bundle") {
                            setOpenClawBundleId(undefined);
                          }
                          if (nextMode !== "manual") {
                            setOpenClawResourceIds([]);
                            setSelectedSkillIds([]);
                          }
                          if (nextMode !== "archive") {
                            setOpenClawImportFile(null);
                            if (openClawImportInputRef.current) {
                              openClawImportInputRef.current.value = "";
                            }
                          }
                        }}
                        onSelectionChange={({ bundleId, resourceIds }) => {
                          setOpenClawBundleId(bundleId);
                          setOpenClawResourceIds(resourceIds);
                        }}
                        onPreviewChange={handleOpenClawPreviewChange}
                      />
                    </div>

                    {openClawInjectionMode === "manual" && (
                      <div className="mt-6 border-t border-gray-200 pt-6">
                        <div className="flex items-start justify-between gap-4">
                          <div>
                            <h3 className="text-xs font-semibold uppercase tracking-[0.18em] text-[#b46c50]">
                              {t("instances.skillsSection")}
                            </h3>
                          </div>
                          <span className="rounded-full bg-emerald-50 px-3 py-1 text-xs font-medium text-emerald-700">
                            {t("instances.selectedCount", {
                              count: selectedSkillIds.length,
                            })}
                          </span>
                        </div>

                        {selectedSkillNames.length > 0 && (
                          <div className="mt-4">
                            {renderSummaryTagList(
                              selectedSkillNames,
                              t("instances.noReusableSkillsSelected"),
                              "emerald",
                            )}
                          </div>
                        )}

                        {skillInventorySummary.hiddenByRisk > 0 && (
                          <div className="mt-4 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
                            {skillRiskPolicySummary}
                          </div>
                        )}

                        <div className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-2">
                          {skillLoading ? (
                            <div className="text-sm text-gray-500">
                              {t("openClawResourcesPage.loadingSkills")}
                            </div>
                          ) : availableSkills.length === 0 ? (
                            <div className="rounded-2xl border border-dashed border-gray-300 px-4 py-5 text-sm text-gray-500">
                              {t("instances.noAvailableSkillsForInjection")}
                            </div>
                          ) : (
                            paginatedSkills.map((skill) => {
                              const checked = selectedSkillIds.includes(
                                skill.id,
                              );
                              return (
                                <label
                                  key={skill.id}
                                  className={`flex cursor-pointer items-start gap-3 rounded-2xl border px-4 py-3 ${
                                    checked
                                      ? "border-indigo-300 bg-indigo-50"
                                      : "border-gray-200 bg-white"
                                  }`}
                                >
                                  <input
                                    type="checkbox"
                                    checked={checked}
                                    onChange={(e) =>
                                      setSelectedSkillIds((current) =>
                                        e.target.checked
                                          ? [...current, skill.id]
                                          : current.filter(
                                              (value) => value !== skill.id,
                                            ),
                                      )
                                    }
                                  />
                                  <span className="min-w-0">
                                    <span className="block font-medium text-gray-900">
                                      {skill.name}
                                    </span>
                                    <span className="mt-1 block text-xs text-gray-500">
                                      {t("instances.skillRiskVersionLabel", {
                                        key: skill.skill_key,
                                        risk: skill.risk_level,
                                        version: skill.current_version_no || 1,
                                      })}
                                    </span>
                                    {skill.description && (
                                      <span className="mt-2 block text-sm text-gray-600">
                                        {skill.description}
                                      </span>
                                    )}
                                  </span>
                                </label>
                              );
                            })
                          )}
                        </div>

                        {!skillLoading && availableSkills.length > 0 && (
                          <div className="mt-4 flex items-center justify-between gap-3 border-t border-gray-200 pt-4">
                            <p className="text-sm text-gray-500">
                              {t("instances.skillPageSummary", {
                                page: skillPage,
                                totalPages: totalSkillPages,
                                totalSkills: availableSkills.length,
                              })}
                            </p>
                            <div className="flex items-center gap-2">
                              <button
                                type="button"
                                onClick={() =>
                                  setSkillPage((current) =>
                                    Math.max(1, current - 1),
                                  )
                                }
                                disabled={skillPage <= 1}
                                className="rounded-lg border border-gray-200 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                {t("instances.previous")}
                              </button>
                              <button
                                type="button"
                                onClick={() =>
                                  setSkillPage((current) =>
                                    Math.min(totalSkillPages, current + 1),
                                  )
                                }
                                disabled={skillPage >= totalSkillPages}
                                className="rounded-lg border border-gray-200 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                {t("instances.nextPage")}
                              </button>
                            </div>
                          </div>
                        )}
                      </div>
                    )}

                    {openClawInjectionMode === "archive" && (
                      <div className="mt-6 border-t border-gray-200 pt-6">
                        <div className="flex items-start justify-between gap-4">
                          <div>
                            <h3 className="text-sm font-semibold uppercase tracking-[0.2em] text-gray-500">
                              {t("instances.archiveImport")}
                            </h3>
                          </div>
                          <span className="rounded-full bg-indigo-50 px-3 py-1 text-xs font-medium text-indigo-600">
                            {t("instances.archiveModeRequiresFile")}
                          </span>
                        </div>

                        <input
                          ref={openClawImportInputRef}
                          type="file"
                          accept=".tar.gz,.tgz,application/gzip,application/x-gzip,application/octet-stream"
                          className="hidden"
                          onChange={(e) =>
                            setOpenClawImportFile(e.target.files?.[0] || null)
                          }
                        />

                        <div className="mt-5 flex flex-wrap items-center gap-3">
                          <button
                            type="button"
                            onClick={() =>
                              openClawImportInputRef.current?.click()
                            }
                            className="app-button-secondary"
                          >
                            {openClawImportFile
                              ? t("instances.changeRuntimeArchive", {
                                  directory: runtimeWorkspaceDirectory(
                                    formData.type,
                                  ),
                                })
                              : t("instances.chooseRuntimeArchive", {
                                  directory: runtimeWorkspaceDirectory(
                                    formData.type,
                                  ),
                                })}
                          </button>
                          {openClawImportFile && (
                            <button
                              type="button"
                              onClick={() => {
                                setOpenClawImportFile(null);
                                if (openClawImportInputRef.current) {
                                  openClawImportInputRef.current.value = "";
                                }
                              }}
                              className="rounded-lg border border-red-200 bg-red-50 px-4 py-2.5 text-sm font-medium text-red-700 hover:bg-red-100"
                            >
                              {t("instances.remove")}
                            </button>
                          )}
                        </div>

                        <div className="mt-4 rounded-lg border border-dashed border-gray-300 bg-gray-50 px-4 py-3 text-sm text-gray-600">
                          {openClawImportFile
                            ? t("instances.selectedArchive", {
                                name: openClawImportFile.name,
                              })
                            : t("instances.noRuntimeArchiveSelected", {
                                directory: runtimeWorkspaceDirectory(
                                  formData.type,
                                ),
                              })}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>

              <div className="space-y-6 xl:sticky xl:top-24">
                <div className="app-panel p-6">
                  <h3 className="text-lg font-medium text-gray-900 mb-4">
                    {t("instances.quotaValidation")}
                  </h3>
                  {quotaLoading ? (
                    <p className="text-sm text-gray-500">
                      {t("userDashboard.loadingQuota")}
                    </p>
                  ) : !quota ? (
                    <p className="text-sm text-red-600">
                      {t("instances.unableToLoadQuota")}
                    </p>
                  ) : (
                    <div className="space-y-3">
                      {quotaChecks.map((item) => (
                        <div
                          key={item.key}
                          className={`flex items-center justify-between rounded-lg border px-4 py-3 text-sm ${
                            item.exceeded
                              ? "border-red-200 bg-red-50 text-red-700"
                              : "border-green-200 bg-green-50 text-green-700"
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
                          {t("instances.requestedResourcesExceeded")}
                        </p>
                      )}
                    </div>
                  )}
                </div>

                {/* Summary */}
                <div className="app-panel p-6">
                  <h3 className="text-lg font-medium text-gray-900">
                    {t("instances.summary")}
                  </h3>
                  <dl className="mt-5 grid grid-cols-1 gap-x-4 gap-y-4 sm:grid-cols-2">
                    <div>
                      <dt className="text-sm font-medium text-gray-500">
                        {t("instances.instanceName")}
                      </dt>
                      <dd className="mt-1 text-sm text-gray-900">
                        {formData.name}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-sm font-medium text-gray-500">
                        {t("common.type")}
                      </dt>
                      <dd className="mt-1 text-sm text-gray-900">
                        {selectedType
                          ? getInstanceTypeLabel(
                              t,
                              selectedType.id,
                              selectedType.name,
                            )
                          : formData.type}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-sm font-medium text-gray-500">
                        {t("instances.instanceMode")}
                      </dt>
                      <dd className="mt-1 text-sm text-gray-900">
                        {selectedMode === "lite" ? "Lite" : "Pro"}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-sm font-medium text-gray-500">
                        {t("instances.runtimeType")}
                      </dt>
                      <dd className="mt-1 text-sm text-gray-900">
                        {selectedRuntimeType === "gateway"
                          ? t("instances.runtimeTypeGateway")
                          : t("instances.runtimeTypeDesktop")}
                      </dd>
                    </div>
                    {showRuntimeImageSelector && (
                      <div>
                        <dt className="text-sm font-medium text-gray-500">
                          {t("instances.instanceImage")}
                        </dt>
                        <dd className="mt-1 text-sm text-gray-900">
                          {selectedRuntimeImage?.display_name ||
                            selectedRuntimeImage?.image ||
                            t("instances.runtimeImageUnavailable")}
                        </dd>
                        {selectedRuntimeImage?.image && (
                          <p className="mt-1 break-all font-mono text-xs text-gray-500">
                            {selectedRuntimeImage.image}
                          </p>
                        )}
                      </div>
                    )}
                    {usesDedicatedResources && (
                      <>
                        <div>
                          <dt className="text-sm font-medium text-gray-500">
                            {t("common.cpu")}
                          </dt>
                          <dd className="mt-1 text-sm text-gray-900">
                            {t("instances.cpuCoresValue", {
                              value: formData.cpu_cores,
                            })}
                          </dd>
                        </div>
                        <div>
                          <dt className="text-sm font-medium text-gray-500">
                            {t("instances.memoryLabel")}
                          </dt>
                          <dd className="mt-1 text-sm text-gray-900">
                            {formData.memory_gb} GB
                          </dd>
                        </div>
                        <div>
                          <dt className="text-sm font-medium text-gray-500">
                            {t("instances.storageLabel")}
                          </dt>
                          <dd className="mt-1 text-sm text-gray-900">
                            {formData.disk_gb} GB
                          </dd>
                        </div>
                        <div>
                          <dt className="text-sm font-medium text-gray-500">
                            {t("instances.gpuLabel")}
                          </dt>
                          <dd className="mt-1 text-sm text-gray-900">
                            {formData.gpu_enabled
                              ? t("instances.gpuCountValue", {
                                  count: formData.gpu_count ?? 0,
                                })
                              : t("instances.gpuDisabled")}
                          </dd>
                        </div>
                      </>
                    )}
                    <div className="sm:col-span-2">
                      <dt className="text-sm font-medium text-gray-500">
                        {t("instances.envInjection")}
                      </dt>
                      {environmentDraft.error ? (
                        <p className="mt-2 text-sm text-red-600">
                          {environmentDraft.error}
                        </p>
                      ) : (
                        renderSummaryTagList(
                          environmentOverrideNames,
                          t("instances.noEnvOverridesConfigured"),
                          "neutral",
                        )
                      )}
                    </div>
                    {supportsRuntimeInjection(formData.type) && (
                      <>
                        <div className="sm:col-span-2">
                          <dt className="text-sm font-medium text-gray-500">
                            {t("instances.channelInjection")}
                          </dt>
                          {openClawInjectionMode === "archive" ? (
                            <p className="mt-2 text-sm text-gray-400">
                              {t("instances.archiveSkipsChannelInjection")}
                            </p>
                          ) : openClawPreviewLoading ? (
                            <p className="mt-2 text-sm text-gray-400">
                              {t("instances.compilingRuntimePreview")}
                            </p>
                          ) : openClawPreviewError ? (
                            <p className="mt-2 text-sm text-red-600">
                              {openClawPreviewError}
                            </p>
                          ) : (
                            renderSummaryTagList(
                              resolvedChannelNames,
                              t("instances.noChannelsSelectedForInjection"),
                              "indigo",
                            )
                          )}
                        </div>
                        <div className="sm:col-span-2">
                          <dt className="text-sm font-medium text-gray-500">
                            {t("instances.skillInjection")}
                          </dt>
                          {renderSummaryTagList(
                            selectedSkillNames,
                            t("instances.noReusableSkillsSelected"),
                            "emerald",
                          )}
                        </div>
                      </>
                    )}
                  </dl>
                </div>
              </div>
            </div>
          )}

          {/* Navigation Buttons */}
          {step < 3 && (
            <div className="mt-6 flex justify-between">
              <button
                type="button"
                onClick={() =>
                  step > 1 ? setStep(step - 1) : navigate("/instances")
                }
                className="app-button-secondary"
              >
                {step === 1 ? t("common.cancel") : t("instances.back")}
              </button>

              <button
                type="button"
                onClick={() => setStep(step + 1)}
                disabled={!canProceed()}
                className="app-button-primary disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {t("instances.next")}
              </button>
            </div>
          )}
        </form>
      </div>
    </UserLayout>
  );
};

export default CreateInstancePage;
