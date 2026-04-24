import React, { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import UserLayout from "../../components/UserLayout";
import { useI18n } from "../../contexts/I18nContext";
import {
  findOpenClawChannelTemplate,
  OPENCLAW_CHANNEL_TEMPLATES,
} from "../../lib/openclawChannelTemplates";
import { openclawConfigService } from "../../services/openclawConfigService";
import { skillService } from "../../services/skillService";
import type {
  OpenClawConfigBundle,
  OpenClawConfigBundleItem,
  OpenClawConfigMode,
  OpenClawConfigResource,
  OpenClawInjectionSnapshot,
  OpenClawResourceType,
  UpsertOpenClawConfigBundleRequest,
  UpsertOpenClawConfigResourceRequest,
} from "../../types/openclawConfig";
import { OPENCLAW_RESOURCE_TYPES } from "../../types/openclawConfig";
import type { Skill } from "../../types/skill";

type ConfigCenterTab = "resources" | "bundles" | "injections";

const CONFIG_CENTER_HIDDEN_RESOURCE_TYPES: OpenClawResourceType[] = [
  "session_template",
  "log_policy",
];
const CONFIG_CENTER_RESOURCE_TYPES = OPENCLAW_RESOURCE_TYPES.filter(
  (item) => !CONFIG_CENTER_HIDDEN_RESOURCE_TYPES.includes(item.value),
);
const CONFIG_CENTER_CONFIGURABLE_RESOURCE_TYPES: OpenClawResourceType[] = [
  "channel",
  "skill",
];
const CONFIG_CENTER_PAGE_SIZE = 8;

const RESOURCE_TYPE_I18N_KEYS: Record<OpenClawResourceType, string> = {
  channel: "openClawResourcesPage.resourceTypes.channel",
  skill: "openClawResourcesPage.resourceTypes.skill",
  session_template: "openClawResourcesPage.resourceTypes.session_template",
  log_policy: "openClawResourcesPage.resourceTypes.log_policy",
  agent: "openClawResourcesPage.resourceTypes.agent",
  scheduled_task: "openClawResourcesPage.resourceTypes.scheduled_task",
};

const INJECTION_MODE_I18N_KEYS: Record<OpenClawConfigMode | "archive", string> =
  {
    none: "openClawResourcesPage.recordModes.none",
    manual: "openClawResourcesPage.recordModes.manual",
    bundle: "openClawResourcesPage.recordModes.bundle",
    archive: "openClawResourcesPage.recordModes.archive",
  };

const SNAPSHOT_STATUS_I18N_KEYS: Record<string, string> = {
  compiled: "openClawResourcesPage.recordStatuses.compiled",
  active: "openClawResourcesPage.recordStatuses.active",
  failed: "openClawResourcesPage.recordStatuses.failed",
};

const CHANNEL_TEMPLATE_LABEL_I18N_KEYS: Record<string, string> = {
  telegram: "openClawResourcesPage.templates.telegram.label",
  "dingtalk-connector":
    "openClawResourcesPage.templates.dingtalkConnector.label",
  slack: "openClawResourcesPage.templates.slack.label",
  feishu: "openClawResourcesPage.templates.feishu.label",
};

const CHANNEL_TEMPLATE_DESCRIPTION_I18N_KEYS: Record<string, string> = {
  telegram: "openClawResourcesPage.templates.telegram.description",
  "dingtalk-connector":
    "openClawResourcesPage.templates.dingtalkConnector.description",
  slack: "openClawResourcesPage.templates.slack.description",
  feishu: "openClawResourcesPage.templates.feishu.description",
};

const defaultContentByType: Record<OpenClawResourceType, string> = {
  channel: JSON.stringify({}, null, 2),
  skill: JSON.stringify(
    {
      schemaVersion: 1,
      kind: "skill",
      format: "skill/custom@v1",
      dependsOn: [],
      config: {},
    },
    null,
    2,
  ),
  session_template: JSON.stringify(
    {
      schemaVersion: 1,
      kind: "session_template",
      format: "session/default@v1",
      dependsOn: [],
      config: {},
    },
    null,
    2,
  ),
  log_policy: JSON.stringify(
    {
      schemaVersion: 1,
      kind: "log_policy",
      format: "log/policy@v1",
      dependsOn: [],
      config: {},
    },
    null,
    2,
  ),
  agent: JSON.stringify(
    {
      schemaVersion: 1,
      kind: "agent",
      format: "agent/default@v1",
      dependsOn: [],
      config: {},
    },
    null,
    2,
  ),
  scheduled_task: JSON.stringify(
    {
      schemaVersion: 1,
      kind: "scheduled_task",
      format: "task/default@v1",
      dependsOn: [],
      config: {},
    },
    null,
    2,
  ),
};

const newResourceForm = (resourceType: OpenClawResourceType) => ({
  id: undefined as number | undefined,
  resource_type: resourceType,
  resource_key: "",
  name: "",
  description: "",
  enabled: true,
  tagsText: "",
  contentText: defaultContentByType[resourceType],
});

const newBundleForm = () => ({
  id: undefined as number | undefined,
  name: "",
  description: "",
  enabled: true,
  itemIds: [] as number[],
});

const splitTagText = (value: string): string[] =>
  value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);

const mergeTagText = (current: string, additions: string[]): string =>
  Array.from(new Set([...splitTagText(current), ...additions])).join(", ");

type ChannelEditorMode = "form" | "json";

type SupportedChannelEditorId =
  | "dingtalk-connector"
  | "feishu"
  | "slack"
  | "telegram";

interface SupportedChannelEditorField {
  key: string;
  labelKey: string;
  placeholderKey: string;
}

interface SupportedChannelEditorDefinition {
  id: SupportedChannelEditorId;
  titleKey: string;
  descriptionKey: string;
  fields: SupportedChannelEditorField[];
  readFormState: (contentText: string) => Record<string, string> | null;
  updateContentText: (
    contentText: string,
    patch: Record<string, string>,
  ) => string;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const parseResourceContentText = (contentText: string): unknown | null => {
  try {
    return JSON.parse(contentText);
  } catch {
    return null;
  }
};

const parseChannelContentText = (
  contentText: string,
): Record<string, unknown> | null => {
  const parsed = parseResourceContentText(contentText);
  if (!isRecord(parsed)) {
    return null;
  }

  const isEnvelope =
    "schemaVersion" in parsed ||
    "kind" in parsed ||
    "format" in parsed ||
    "dependsOn" in parsed;

  if (isEnvelope && isRecord(parsed.config)) {
    return parsed.config;
  }

  return parsed;
};

const readStringArray = (value: unknown): string[] | null =>
  Array.isArray(value) && value.every((item) => typeof item === "string")
    ? [...value]
    : null;

const stringifyChannelContentText = (config: Record<string, unknown>): string =>
  JSON.stringify(config, null, 2);

// mergeChannelConfig overlays the form-owned, allowlisted fields onto the
// existing parsed config so tenant-authored keys (webhook, custom capabilities,
// extra feishu accounts, etc.) survive a Save. Allowlisted keys win on
// conflict. Every channel editor's update*ChannelContentText must route its
// output through this helper — building the saved config from the allowlist
// alone silently drops unknown fields.
const mergeChannelConfig = (
  existing: Record<string, unknown>,
  allowlisted: Record<string, unknown>,
): Record<string, unknown> => ({
  ...existing,
  ...allowlisted,
});

const channelFormatForResourceKey = (resourceKey: string): string => {
  const trimmed = resourceKey.trim();
  return trimmed ? `channel/${trimmed}@v1` : "channel/custom@v1";
};

const buildChannelEnvelopeForRequest = (
  resourceKey: string,
  contentText: string,
  invalidJsonMessage: string,
): Record<string, unknown> => {
  const config = parseChannelContentText(contentText);
  if (!config) {
    throw new Error(invalidJsonMessage);
  }

  return {
    schemaVersion: 1,
    kind: "channel",
    format: channelFormatForResourceKey(resourceKey),
    dependsOn: [],
    config,
  };
};

const readTelegramChannelFormState = (
  contentText: string,
): Record<string, string> | null => {
  const config = parseChannelContentText(contentText);
  if (!config) {
    return null;
  }

  return {
    botToken: typeof config.botToken === "string" ? config.botToken : "",
  };
};

const updateTelegramChannelContentText = (
  contentText: string,
  patch: Record<string, string>,
): string => {
  const parsed = parseChannelContentText(contentText);
  if (!parsed) {
    return contentText;
  }

  const currentForm = readTelegramChannelFormState(contentText);
  if (!currentForm) {
    return contentText;
  }

  const nextForm = {
    ...currentForm,
    ...patch,
  };

  const allowlisted = {
    enabled: true,
    botToken: nextForm.botToken,
    dmPolicy:
      typeof parsed.dmPolicy === "string" && parsed.dmPolicy
        ? parsed.dmPolicy
        : "open",
    allowFrom: readStringArray(parsed.allowFrom) || ["*"],
  };

  return stringifyChannelContentText(mergeChannelConfig(parsed, allowlisted));
};

const readDingTalkChannelFormState = (
  contentText: string,
): Record<string, string> | null => {
  const config = parseChannelContentText(contentText);
  if (!config) {
    return null;
  }

  return {
    clientId: typeof config.clientId === "string" ? config.clientId : "",
    clientSecret:
      typeof config.clientSecret === "string" ? config.clientSecret : "",
  };
};

const updateDingTalkChannelContentText = (
  contentText: string,
  patch: Record<string, string>,
): string => {
  const parsed = parseChannelContentText(contentText);
  if (!parsed) {
    return contentText;
  }

  const currentForm = readDingTalkChannelFormState(contentText);
  if (!currentForm) {
    return contentText;
  }

  const nextForm = {
    ...currentForm,
    ...patch,
  };

  const allowlisted = {
    enabled: true,
    clientId: nextForm.clientId,
    clientSecret: nextForm.clientSecret,
    allowFrom: readStringArray(parsed.allowFrom) || ["*"],
  };

  return stringifyChannelContentText(mergeChannelConfig(parsed, allowlisted));
};

const readSlackChannelFormState = (
  contentText: string,
): Record<string, string> | null => {
  const config = parseChannelContentText(contentText);
  if (!config) {
    return null;
  }

  return {
    botToken: typeof config.botToken === "string" ? config.botToken : "",
    appToken: typeof config.appToken === "string" ? config.appToken : "",
  };
};

const updateSlackChannelContentText = (
  contentText: string,
  patch: Record<string, string>,
): string => {
  const parsed = parseChannelContentText(contentText);
  if (!parsed) {
    return contentText;
  }

  const currentForm = readSlackChannelFormState(contentText);
  if (!currentForm) {
    return contentText;
  }

  const nextForm = {
    ...currentForm,
    ...patch,
  };

  const allowlisted = {
    enabled: true,
    botToken: nextForm.botToken,
    appToken: nextForm.appToken,
    groupPolicy:
      typeof parsed.groupPolicy === "string" && parsed.groupPolicy
        ? parsed.groupPolicy
        : "allowlist",
    channels: isRecord(parsed.channels)
      ? parsed.channels
      : {
          "#general": {
            allow: true,
          },
        },
    capabilities: isRecord(parsed.capabilities)
      ? parsed.capabilities
      : {
          interactiveReplies: true,
        },
  };

  return stringifyChannelContentText(mergeChannelConfig(parsed, allowlisted));
};

const readFeishuChannelFormState = (
  contentText: string,
): Record<string, string> | null => {
  const config = parseChannelContentText(contentText);
  if (!config) {
    return null;
  }

  const accounts = isRecord(config.accounts) ? config.accounts : null;
  const mainAccount =
    accounts && isRecord(accounts.main)
      ? accounts.main
      : accounts && isRecord(accounts.default)
        ? accounts.default
        : null;

  return {
    appId:
      mainAccount && typeof mainAccount.appId === "string"
        ? mainAccount.appId
        : typeof config.appId === "string"
          ? config.appId
          : "",
    appSecret:
      mainAccount && typeof mainAccount.appSecret === "string"
        ? mainAccount.appSecret
        : typeof config.appSecret === "string"
          ? config.appSecret
          : "",
  };
};

const updateFeishuChannelContentText = (
  contentText: string,
  patch: Record<string, string>,
): string => {
  const parsed = parseChannelContentText(contentText);
  if (!parsed) {
    return contentText;
  }

  const currentForm = readFeishuChannelFormState(contentText);
  if (!currentForm) {
    return contentText;
  }

  const nextForm = {
    ...currentForm,
    ...patch,
  };

  const existingAccounts = isRecord(parsed.accounts) ? parsed.accounts : {};
  const existingMain = isRecord(existingAccounts.main)
    ? existingAccounts.main
    : {};
  const allowlisted = {
    enabled: true,
    accounts: {
      ...existingAccounts,
      main: {
        ...existingMain,
        appId: nextForm.appId,
        appSecret: nextForm.appSecret,
      },
    },
  };

  return stringifyChannelContentText(mergeChannelConfig(parsed, allowlisted));
};

const detectSupportedChannelEditor = (
  resourceKey: string,
  contentText: string,
): SupportedChannelEditorId | null => {
  const config = parseChannelContentText(contentText);
  const normalizedResourceKey = resourceKey.trim().toLowerCase();
  const accounts = config && isRecord(config.accounts) ? config.accounts : null;
  const domain =
    config && typeof config.domain === "string"
      ? config.domain.toLowerCase()
      : "";
  const hasClientId = config && typeof config.clientId === "string";
  const hasClientSecret = config && typeof config.clientSecret === "string";
  const hasAppToken = config && typeof config.appToken === "string";
  const hasBotToken = config && typeof config.botToken === "string";

  if (normalizedResourceKey === "feishu" || domain === "feishu" || !!accounts) {
    return "feishu";
  }
  if (
    normalizedResourceKey === "dingtalk-connector" ||
    hasClientId ||
    hasClientSecret
  ) {
    return "dingtalk-connector";
  }
  if (normalizedResourceKey === "slack" || hasAppToken) {
    return "slack";
  }
  if (normalizedResourceKey === "telegram" || hasBotToken) {
    return "telegram";
  }

  return null;
};

const normalizeResourceContentTextForEditor = (
  resourceType: OpenClawResourceType,
  resourceKey: string,
  contentText: string,
): string => {
  if (resourceType !== "channel") {
    return contentText;
  }

  const channelConfig = parseChannelContentText(contentText);
  if (!channelConfig) {
    return contentText;
  }

  const normalizedContentText = stringifyChannelContentText(channelConfig);
  const editorId = detectSupportedChannelEditor(
    resourceKey,
    normalizedContentText,
  );
  if (editorId === "feishu") {
    const currentForm = readFeishuChannelFormState(normalizedContentText);
    return currentForm
      ? updateFeishuChannelContentText(normalizedContentText, currentForm)
      : normalizedContentText;
  }
  if (editorId === "dingtalk-connector") {
    const currentForm = readDingTalkChannelFormState(normalizedContentText);
    return currentForm
      ? updateDingTalkChannelContentText(normalizedContentText, currentForm)
      : normalizedContentText;
  }
  if (editorId === "slack") {
    const currentForm = readSlackChannelFormState(normalizedContentText);
    return currentForm
      ? updateSlackChannelContentText(normalizedContentText, currentForm)
      : normalizedContentText;
  }
  if (editorId === "telegram") {
    const currentForm = readTelegramChannelFormState(normalizedContentText);
    return currentForm
      ? updateTelegramChannelContentText(normalizedContentText, currentForm)
      : normalizedContentText;
  }

  return normalizedContentText;
};

const SUPPORTED_CHANNEL_EDITORS: Record<
  SupportedChannelEditorId,
  SupportedChannelEditorDefinition
> = {
  "dingtalk-connector": {
    id: "dingtalk-connector",
    titleKey: "openClawResourcesPage.channelEditors.dingtalkConnector.title",
    descriptionKey:
      "openClawResourcesPage.channelEditors.dingtalkConnector.description",
    fields: [
      {
        key: "clientId",
        labelKey:
          "openClawResourcesPage.channelEditors.dingtalkConnector.fields.clientId.label",
        placeholderKey:
          "openClawResourcesPage.channelEditors.dingtalkConnector.fields.clientId.placeholder",
      },
      {
        key: "clientSecret",
        labelKey:
          "openClawResourcesPage.channelEditors.dingtalkConnector.fields.clientSecret.label",
        placeholderKey:
          "openClawResourcesPage.channelEditors.dingtalkConnector.fields.clientSecret.placeholder",
      },
    ],
    readFormState: readDingTalkChannelFormState,
    updateContentText: updateDingTalkChannelContentText,
  },
  feishu: {
    id: "feishu",
    titleKey: "openClawResourcesPage.channelEditors.feishu.title",
    descriptionKey: "openClawResourcesPage.channelEditors.feishu.description",
    fields: [
      {
        key: "appId",
        labelKey:
          "openClawResourcesPage.channelEditors.feishu.fields.appId.label",
        placeholderKey:
          "openClawResourcesPage.channelEditors.feishu.fields.appId.placeholder",
      },
      {
        key: "appSecret",
        labelKey:
          "openClawResourcesPage.channelEditors.feishu.fields.appSecret.label",
        placeholderKey:
          "openClawResourcesPage.channelEditors.feishu.fields.appSecret.placeholder",
      },
    ],
    readFormState: readFeishuChannelFormState,
    updateContentText: updateFeishuChannelContentText,
  },
  slack: {
    id: "slack",
    titleKey: "openClawResourcesPage.channelEditors.slack.title",
    descriptionKey: "openClawResourcesPage.channelEditors.slack.description",
    fields: [
      {
        key: "botToken",
        labelKey:
          "openClawResourcesPage.channelEditors.slack.fields.botToken.label",
        placeholderKey:
          "openClawResourcesPage.channelEditors.slack.fields.botToken.placeholder",
      },
      {
        key: "appToken",
        labelKey:
          "openClawResourcesPage.channelEditors.slack.fields.appToken.label",
        placeholderKey:
          "openClawResourcesPage.channelEditors.slack.fields.appToken.placeholder",
      },
    ],
    readFormState: readSlackChannelFormState,
    updateContentText: updateSlackChannelContentText,
  },
  telegram: {
    id: "telegram",
    titleKey: "openClawResourcesPage.channelEditors.telegram.title",
    descriptionKey: "openClawResourcesPage.channelEditors.telegram.description",
    fields: [
      {
        key: "botToken",
        labelKey:
          "openClawResourcesPage.channelEditors.telegram.fields.botToken.label",
        placeholderKey:
          "openClawResourcesPage.channelEditors.telegram.fields.botToken.placeholder",
      },
    ],
    readFormState: readTelegramChannelFormState,
    updateContentText: updateTelegramChannelContentText,
  },
};

interface EditorModalProps {
  open: boolean;
  title: string;
  subtitle: string;
  onClose: () => void;
  busy?: boolean;
  panelClassName?: string;
  children: React.ReactNode;
}

const EditorModal: React.FC<EditorModalProps> = ({
  open,
  title,
  subtitle,
  onClose,
  busy = false,
  panelClassName = "max-w-5xl",
  children,
}) => {
  const { t } = useI18n();

  useEffect(() => {
    if (!open) {
      return undefined;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) {
        onClose();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [busy, onClose, open]);

  if (!open || typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-[rgba(23,18,18,0.42)] p-4 backdrop-blur-sm"
      onClick={(event) => {
        if (event.target === event.currentTarget && !busy) {
          onClose();
        }
      }}
    >
      <div
        className={`w-full rounded-[30px] border border-[#eadfd8] bg-white shadow-[0_36px_100px_-48px_rgba(72,44,24,0.56)] ${panelClassName}`}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4 border-b border-[#f1e3db] px-6 py-5">
          <div>
            <h3 className="text-2xl font-semibold tracking-[-0.02em] text-[#171212]">
              {title}
            </h3>
            <p className="mt-1 text-sm leading-6 text-[#6e6460]">{subtitle}</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="rounded-full border border-[#eadfd8] bg-[#f8f2ef] px-3 py-2 text-sm font-medium text-[#6e6460] hover:bg-[#f1e7e1] disabled:cursor-not-allowed disabled:opacity-50"
          >
            {t("common.close")}
          </button>
        </div>
        <div className="max-h-[calc(100vh-8rem)] overflow-y-auto px-6 py-5">
          {children}
        </div>
      </div>
    </div>,
    document.body,
  );
};

const resourceFormFromItem = (item: OpenClawConfigResource) => ({
  id: item.id,
  resource_type: item.resource_type,
  resource_key: item.resource_key,
  name: item.name,
  description: item.description || "",
  enabled: item.enabled,
  tagsText: item.tags.join(", "),
  contentText: normalizeResourceContentTextForEditor(
    item.resource_type,
    item.resource_key,
    JSON.stringify(item.content, null, 2),
  ),
});

const bundleFormFromItem = (item: OpenClawConfigBundle) => ({
  id: item.id,
  name: item.name,
  description: item.description || "",
  enabled: item.enabled,
  itemIds: item.items.map((bundleItem) => bundleItem.resource_id),
});

const skillRiskKey = (riskLevel?: string | null) => {
  switch ((riskLevel || "").toLowerCase()) {
    case "none":
      return "none";
    case "low":
      return "low";
    case "medium":
      return "medium";
    case "high":
      return "high";
    default:
      return "unknown";
  }
};

const OpenClawConfigCenterPage: React.FC = () => {
  const { t } = useI18n();
  const [tab, setTab] = useState<ConfigCenterTab>("resources");
  const [resourceType, setResourceType] =
    useState<OpenClawResourceType>("channel");
  const [resourceSearch, setResourceSearch] = useState("");
  const [resources, setResources] = useState<OpenClawConfigResource[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [bundles, setBundles] = useState<OpenClawConfigBundle[]>([]);
  const [snapshots, setSnapshots] = useState<OpenClawInjectionSnapshot[]>([]);
  const [resourceForm, setResourceForm] = useState(() =>
    newResourceForm("channel"),
  );
  const [bundleForm, setBundleForm] = useState(() => newBundleForm());
  const [selectedResourceId, setSelectedResourceId] = useState<
    number | undefined
  >();
  const [selectedBundleId, setSelectedBundleId] = useState<
    number | undefined
  >();
  const [resourceEditorOpen, setResourceEditorOpen] = useState(false);
  const [bundleEditorOpen, setBundleEditorOpen] = useState(false);
  const [resourcePage, setResourcePage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [selectedChannelTemplateId, setSelectedChannelTemplateId] =
    useState("");
  const [channelEditorMode, setChannelEditorMode] =
    useState<ChannelEditorMode>("form");
  const [skillUploadFile, setSkillUploadFile] = useState<File | null>(null);

  const loadAll = async () => {
    try {
      setLoading(true);
      setError(null);
      const [resourceItems, skillItems, bundleItems, injectionItems] =
        await Promise.all([
          openclawConfigService.listResources(),
          skillService.listSkills(),
          openclawConfigService.listBundles(),
          openclawConfigService.listInjections(50),
        ]);
      setResources(resourceItems);
      setSkills(skillItems);
      setBundles(bundleItems);
      setSnapshots(injectionItems);
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          t("openClawResourcesPage.errors.loadFailed"),
      );
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadAll();
  }, []);

  useEffect(() => {
    setResourcePage(1);
  }, [resourceType, resourceSearch, tab]);

  const filteredResources = useMemo(() => {
    const keyword = resourceSearch.trim().toLowerCase();
    return resources.filter((item) => {
      if (item.resource_type !== resourceType) {
        return false;
      }
      if (!keyword) {
        return true;
      }
      return [
        item.name,
        item.resource_key,
        item.description || "",
        item.tags.join(" "),
      ].some((value) => value.toLowerCase().includes(keyword));
    });
  }, [resourceSearch, resourceType, resources]);

  const filteredSkills = useMemo(() => {
    const keyword = resourceSearch.trim().toLowerCase();
    if (!keyword) {
      return skills;
    }
    return skills.filter((item) =>
      [item.name, item.skill_key, item.description || "", item.risk_level].some(
        (value) => value.toLowerCase().includes(keyword),
      ),
    );
  }, [resourceSearch, skills]);

  const resourceItemTotal =
    resourceType === "skill" ? filteredSkills.length : filteredResources.length;
  const resourcePageTotal = Math.max(
    1,
    Math.ceil(resourceItemTotal / CONFIG_CENTER_PAGE_SIZE),
  );
  const currentResourcePage = Math.min(resourcePage, resourcePageTotal);
  const paginatedSkills = useMemo(() => {
    const start = (currentResourcePage - 1) * CONFIG_CENTER_PAGE_SIZE;
    return filteredSkills.slice(start, start + CONFIG_CENTER_PAGE_SIZE);
  }, [currentResourcePage, filteredSkills]);
  const paginatedResources = useMemo(() => {
    const start = (currentResourcePage - 1) * CONFIG_CENTER_PAGE_SIZE;
    return filteredResources.slice(start, start + CONFIG_CENTER_PAGE_SIZE);
  }, [currentResourcePage, filteredResources]);

  useEffect(() => {
    if (resourcePage > resourcePageTotal) {
      setResourcePage(resourcePageTotal);
    }
  }, [resourcePage, resourcePageTotal]);

  const getResourceTypeLabel = (value: OpenClawResourceType) =>
    t(RESOURCE_TYPE_I18N_KEYS[value]);
  const getChannelTemplateLabel = (templateId: string) =>
    CHANNEL_TEMPLATE_LABEL_I18N_KEYS[templateId]
      ? t(CHANNEL_TEMPLATE_LABEL_I18N_KEYS[templateId])
      : templateId;
  const getChannelTemplateDescription = (
    templateId: string,
    fallback: string,
  ) =>
    CHANNEL_TEMPLATE_DESCRIPTION_I18N_KEYS[templateId]
      ? t(CHANNEL_TEMPLATE_DESCRIPTION_I18N_KEYS[templateId])
      : fallback;
  const getInjectionModeLabel = (mode: OpenClawConfigMode | "archive") =>
    INJECTION_MODE_I18N_KEYS[mode] ? t(INJECTION_MODE_I18N_KEYS[mode]) : mode;
  const getSnapshotStatusLabel = (status: string) =>
    SNAPSHOT_STATUS_I18N_KEYS[status]
      ? t(SNAPSHOT_STATUS_I18N_KEYS[status])
      : status;
  const getSkillRiskLabel = (riskLevel?: string | null) =>
    t(`openClawResourcesPage.risks.${skillRiskKey(riskLevel)}`);
  const resourceTypeOptions = useMemo(
    () =>
      CONFIG_CENTER_RESOURCE_TYPES.map((item) => ({
        ...item,
        label: getResourceTypeLabel(item.value),
      })),
    [t],
  );
  const groupedResources = useMemo(
    () =>
      resourceTypeOptions.map(({ value, label }) => ({
        value,
        label,
        items: resources.filter((item) => item.resource_type === value),
      })),
    [resourceTypeOptions, resources],
  );

  const selectedChannelTemplate = useMemo(
    () => findOpenClawChannelTemplate(selectedChannelTemplateId),
    [selectedChannelTemplateId],
  );
  const supportedChannelEditorId = useMemo(
    () =>
      resourceForm.resource_type === "channel"
        ? detectSupportedChannelEditor(
            resourceForm.resource_key,
            resourceForm.contentText,
          )
        : null,
    [
      resourceForm.contentText,
      resourceForm.resource_key,
      resourceForm.resource_type,
    ],
  );
  const supportedChannelEditor = useMemo(
    () =>
      supportedChannelEditorId
        ? SUPPORTED_CHANNEL_EDITORS[supportedChannelEditorId]
        : null,
    [supportedChannelEditorId],
  );
  const supportedChannelForm = useMemo(
    () =>
      supportedChannelEditor
        ? supportedChannelEditor.readFormState(resourceForm.contentText)
        : null,
    [resourceForm.contentText, supportedChannelEditor],
  );
  const selectedResourceTypeOption = useMemo(
    () => resourceTypeOptions.find((item) => item.value === resourceType),
    [resourceType, resourceTypeOptions],
  );
  const selectedEditorTypeOption = useMemo(
    () =>
      resourceTypeOptions.find(
        (item) => item.value === resourceForm.resource_type,
      ),
    [resourceForm.resource_type, resourceTypeOptions],
  );
  const resourceTypeIsConfigurable =
    CONFIG_CENTER_CONFIGURABLE_RESOURCE_TYPES.includes(resourceType);
  const resourceEditorIsConfigurable =
    CONFIG_CENTER_CONFIGURABLE_RESOURCE_TYPES.includes(
      resourceForm.resource_type,
    );

  const channelTemplatesByCategory = useMemo(
    () => ({
      builtin: OPENCLAW_CHANNEL_TEMPLATES.filter(
        (item) => item.category === "builtin",
      ),
      plugin: OPENCLAW_CHANNEL_TEMPLATES.filter(
        (item) => item.category === "plugin",
      ),
    }),
    [],
  );

  const openNewResourceEditor = () => {
    setError(null);
    setNotice(null);
    setSelectedResourceId(undefined);
    setSelectedChannelTemplateId("");
    setChannelEditorMode("form");
    setResourceForm(newResourceForm(resourceType));
    setResourceEditorOpen(true);
  };

  const openResourceEditor = (item: OpenClawConfigResource) => {
    setError(null);
    setNotice(null);
    setSelectedResourceId(item.id);
    setSelectedChannelTemplateId("");
    setChannelEditorMode("form");
    setResourceType(item.resource_type);
    setResourceForm(resourceFormFromItem(item));
    setResourceEditorOpen(true);
  };

  const closeResourceEditor = () => {
    if (saving) {
      return;
    }
    setResourceEditorOpen(false);
    if (!selectedResourceId) {
      setSelectedChannelTemplateId("");
      setResourceForm(newResourceForm(resourceType));
    }
  };

  const openNewBundleEditor = () => {
    setError(null);
    setNotice(null);
    setSelectedBundleId(undefined);
    setBundleForm(newBundleForm());
    setBundleEditorOpen(true);
  };

  const openBundleEditor = (item: OpenClawConfigBundle) => {
    setError(null);
    setNotice(null);
    setSelectedBundleId(item.id);
    setBundleForm(bundleFormFromItem(item));
    setBundleEditorOpen(true);
  };

  const closeBundleEditor = () => {
    if (saving) {
      return;
    }
    setBundleEditorOpen(false);
    if (!selectedBundleId) {
      setBundleForm(newBundleForm());
    }
  };

  const persistResource = async () => {
    try {
      setSaving(true);
      setError(null);
      setNotice(null);

      const payload: UpsertOpenClawConfigResourceRequest = {
        resource_type: resourceForm.resource_type,
        resource_key: resourceForm.resource_key.trim(),
        name: resourceForm.name.trim(),
        description: resourceForm.description.trim() || undefined,
        enabled: resourceForm.enabled,
        tags: splitTagText(resourceForm.tagsText),
        content:
          resourceForm.resource_type === "channel"
            ? buildChannelEnvelopeForRequest(
                resourceForm.resource_key,
                resourceForm.contentText,
                t("openClawResourcesPage.invalidChannelJson"),
              )
            : JSON.parse(resourceForm.contentText),
      };

      const saved = resourceForm.id
        ? await openclawConfigService.updateResource(resourceForm.id, payload)
        : await openclawConfigService.createResource(payload);

      await loadAll();
      setSelectedResourceId(saved.id);
      setResourceType(saved.resource_type);
      setSelectedChannelTemplateId("");
      setResourceForm(resourceFormFromItem(saved));
      setResourceEditorOpen(false);
      setNotice(
        resourceForm.id
          ? t("openClawResourcesPage.notices.resourceUpdated")
          : t("openClawResourcesPage.notices.resourceCreated"),
      );
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          err.message ||
          t("openClawResourcesPage.errors.saveResource"),
      );
    } finally {
      setSaving(false);
    }
  };

  const validateResource = async () => {
    try {
      setSaving(true);
      setError(null);
      setNotice(null);
      await openclawConfigService.validateResource({
        resource_type: resourceForm.resource_type,
        resource_key: resourceForm.resource_key.trim(),
        name: resourceForm.name.trim(),
        description: resourceForm.description.trim() || undefined,
        enabled: resourceForm.enabled,
        tags: splitTagText(resourceForm.tagsText),
        content:
          resourceForm.resource_type === "channel"
            ? buildChannelEnvelopeForRequest(
                resourceForm.resource_key,
                resourceForm.contentText,
                t("openClawResourcesPage.invalidChannelJson"),
              )
            : JSON.parse(resourceForm.contentText),
      });
      setNotice(t("openClawResourcesPage.notices.resourceJsonValid"));
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          err.message ||
          t("openClawResourcesPage.errors.validationFailed"),
      );
    } finally {
      setSaving(false);
    }
  };

  const removeResource = async () => {
    if (
      !resourceForm.id ||
      !window.confirm(
        t("openClawResourcesPage.confirms.deleteResource", {
          name: resourceForm.name,
        }),
      )
    ) {
      return;
    }

    try {
      setSaving(true);
      await openclawConfigService.deleteResource(resourceForm.id);
      await loadAll();
      setSelectedResourceId(undefined);
      setSelectedChannelTemplateId("");
      setResourceForm(newResourceForm(resourceType));
      setResourceEditorOpen(false);
      setNotice(t("openClawResourcesPage.notices.resourceDeleted"));
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          t("openClawResourcesPage.errors.deleteResource"),
      );
    } finally {
      setSaving(false);
    }
  };

  const cloneResource = async () => {
    if (!resourceForm.id) {
      return;
    }

    try {
      setSaving(true);
      const cloned = await openclawConfigService.cloneResource(resourceForm.id);
      await loadAll();
      setSelectedResourceId(cloned.id);
      setResourceType(cloned.resource_type);
      setSelectedChannelTemplateId("");
      setResourceForm(resourceFormFromItem(cloned));
      setNotice(t("openClawResourcesPage.notices.resourceCloned"));
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          t("openClawResourcesPage.errors.cloneResource"),
      );
    } finally {
      setSaving(false);
    }
  };

  const uploadSkillArchive = async () => {
    if (!skillUploadFile) {
      return;
    }

    try {
      setSaving(true);
      setError(null);
      setNotice(null);
      await skillService.importSkills(skillUploadFile);
      setSkillUploadFile(null);
      await loadAll();
      setNotice(t("openClawResourcesPage.notices.skillArchiveImported"));
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          t("openClawResourcesPage.errors.importSkillArchive"),
      );
    } finally {
      setSaving(false);
    }
  };

  const removeSkillAsset = async (skillId: number) => {
    try {
      setSaving(true);
      setError(null);
      setNotice(null);
      await skillService.deleteSkill(skillId);
      await loadAll();
      setNotice(t("openClawResourcesPage.notices.skillDeleted"));
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          t("openClawResourcesPage.errors.deleteSkill"),
      );
    } finally {
      setSaving(false);
    }
  };

  const downloadSkillAsset = async (skillId: number, name: string) => {
    try {
      const blob = await skillService.downloadSkill(skillId);
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `${name || "skill"}.zip`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.URL.revokeObjectURL(url);
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          t("openClawResourcesPage.errors.downloadSkill"),
      );
    }
  };

  const applyChannelTemplate = (templateId: string) => {
    const template = findOpenClawChannelTemplate(templateId);
    if (!template) {
      return;
    }

    setResourceType("channel");
    setSelectedChannelTemplateId(template.id);
    setChannelEditorMode("form");
    setError(null);
    const templateLabel = getChannelTemplateLabel(template.id);
    const templateDescription = getChannelTemplateDescription(
      template.id,
      template.description,
    );
    setNotice(
      t("openClawResourcesPage.notices.templateLoaded", {
        name: templateLabel,
      }),
    );
    setResourceForm((current) => ({
      ...current,
      resource_type: "channel",
      resource_key: current.resource_key.trim() || template.resourceKey,
      name: current.name.trim() || templateLabel,
      description: current.description.trim() || templateDescription,
      tagsText: mergeTagText(current.tagsText, template.tags),
      contentText: JSON.stringify(template.config, null, 2),
    }));
  };

  const persistBundle = async () => {
    try {
      setSaving(true);
      setError(null);
      setNotice(null);

      const payload: UpsertOpenClawConfigBundleRequest = {
        name: bundleForm.name.trim(),
        description: bundleForm.description.trim() || undefined,
        enabled: bundleForm.enabled,
        items: bundleForm.itemIds.map(
          (resourceId, index): OpenClawConfigBundleItem => ({
            resource_id: resourceId,
            sort_order: index + 1,
            required: true,
          }),
        ),
      };

      const saved = bundleForm.id
        ? await openclawConfigService.updateBundle(bundleForm.id, payload)
        : await openclawConfigService.createBundle(payload);

      await loadAll();
      setSelectedBundleId(saved.id);
      setBundleForm(bundleFormFromItem(saved));
      setBundleEditorOpen(false);
      setNotice(
        bundleForm.id
          ? t("openClawResourcesPage.notices.bundleUpdated")
          : t("openClawResourcesPage.notices.bundleCreated"),
      );
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          t("openClawResourcesPage.errors.saveBundle"),
      );
    } finally {
      setSaving(false);
    }
  };

  const removeBundle = async () => {
    if (
      !bundleForm.id ||
      !window.confirm(
        t("openClawResourcesPage.confirms.deleteBundle", {
          name: bundleForm.name,
        }),
      )
    ) {
      return;
    }

    try {
      setSaving(true);
      await openclawConfigService.deleteBundle(bundleForm.id);
      await loadAll();
      setSelectedBundleId(undefined);
      setBundleForm(newBundleForm());
      setBundleEditorOpen(false);
      setNotice(t("openClawResourcesPage.notices.bundleDeleted"));
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          t("openClawResourcesPage.errors.deleteBundle"),
      );
    } finally {
      setSaving(false);
    }
  };

  const cloneBundle = async () => {
    if (!bundleForm.id) {
      return;
    }

    try {
      setSaving(true);
      const cloned = await openclawConfigService.cloneBundle(bundleForm.id);
      await loadAll();
      setSelectedBundleId(cloned.id);
      setBundleForm(bundleFormFromItem(cloned));
      setNotice(t("openClawResourcesPage.notices.bundleCloned"));
    } catch (err: any) {
      setError(
        err.response?.data?.error ||
          t("openClawResourcesPage.errors.cloneBundle"),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <UserLayout title={t("openClawResourcesPage.title")}>
      <div className="space-y-6">
        <div className="app-panel p-4 sm:p-6">
          <div className="flex flex-wrap gap-3">
            {[
              {
                key: "resources",
                label: t("openClawResourcesPage.tabs.resources"),
              },
              {
                key: "bundles",
                label: t("openClawResourcesPage.tabs.bundles"),
              },
              {
                key: "injections",
                label: t("openClawResourcesPage.tabs.injections"),
              },
            ].map((item) => (
              <button
                key={item.key}
                type="button"
                onClick={() => {
                  setTab(item.key as ConfigCenterTab);
                  setResourceEditorOpen(false);
                  setBundleEditorOpen(false);
                }}
                className={`rounded-xl px-4 py-2 text-sm font-medium transition ${
                  tab === item.key
                    ? "bg-indigo-600 text-white"
                    : "bg-gray-100 text-gray-700 hover:bg-gray-200"
                }`}
              >
                {item.label}
              </button>
            ))}
            <button
              type="button"
              onClick={loadAll}
              className="app-button-secondary ml-auto"
            >
              {t("common.refresh")}
            </button>
          </div>
          {error && (
            <div className="mt-4 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {error}
            </div>
          )}
          {notice && (
            <div className="mt-4 rounded-xl border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-700">
              {notice}
            </div>
          )}
        </div>

        {tab === "resources" && (
          <div className="grid grid-cols-1 gap-6 xl:grid-cols-[220px_minmax(0,1fr)]">
            <div className="app-panel p-4">
              <div className="text-xs font-semibold uppercase tracking-[0.18em] text-[#b46c50]">
                {t("openClawResourcesPage.resourceTypesTitle")}
              </div>
              <div className="mt-4 space-y-2">
                {resourceTypeOptions.map((item) => (
                  <button
                    key={item.value}
                    type="button"
                    onClick={() => setResourceType(item.value)}
                    className={`w-full rounded-2xl px-4 py-3 text-left text-sm font-medium transition ${
                      resourceType === item.value
                        ? "bg-[#fff1eb] text-red-600"
                        : "bg-gray-50 text-gray-700 hover:bg-gray-100"
                    }`}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>

            {resourceTypeIsConfigurable ? (
              <div className="app-panel p-4 sm:p-5">
                {resourceType === "skill" ? (
                  <>
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
                      <label
                        htmlFor="skill-archive-upload"
                        className="flex w-full cursor-pointer items-center gap-3 rounded-2xl border border-[#dfd6cf] bg-white px-4 py-3 text-sm text-[#3f3a36] transition hover:border-[#cfc3ba] hover:bg-[#fcfaf8]"
                      >
                        <span className="rounded-xl border border-[#d8d0ca] bg-[#f6f3f0] px-3 py-2 text-sm font-medium text-[#2f2a27]">
                          {t("openClawResourcesPage.skillActions.chooseFile")}
                        </span>
                        <span className="min-w-0 truncate text-[#6d655f]">
                          {skillUploadFile?.name ||
                            t("openClawResourcesPage.noFileSelected")}
                        </span>
                        <input
                          id="skill-archive-upload"
                          type="file"
                          accept=".zip,application/zip,application/x-zip-compressed"
                          onChange={(e) =>
                            setSkillUploadFile(e.target.files?.[0] || null)
                          }
                          className="hidden"
                        />
                      </label>
                      <button
                        type="button"
                        onClick={uploadSkillArchive}
                        disabled={!skillUploadFile || saving}
                        className="app-button-primary whitespace-nowrap disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {t("openClawResourcesPage.skillActions.uploadArchive")}
                      </button>
                    </div>
                    <div className="mt-3 text-sm text-gray-500">
                      {t("openClawResourcesPage.skillUploadHint")}
                    </div>
                    <div className="mt-4 space-y-3">
                      {loading ? (
                        <div className="text-sm text-gray-500">
                          {t("openClawResourcesPage.loadingSkills")}
                        </div>
                      ) : filteredSkills.length === 0 ? (
                        <div className="rounded-2xl border border-dashed border-gray-300 px-4 py-8 text-sm text-gray-500">
                          {t("openClawResourcesPage.noSkills")}
                        </div>
                      ) : (
                        paginatedSkills.map((item) => (
                          <div
                            key={item.id}
                            className="w-full rounded-2xl border border-gray-200 bg-white px-4 py-4 text-left"
                          >
                            <div className="flex items-start justify-between gap-3">
                              <div>
                                <div className="font-medium text-gray-900">
                                  {item.name}
                                </div>
                                <div className="mt-1 text-xs text-gray-500">
                                  {t("openClawResourcesPage.skillMeta", {
                                    key: item.skill_key,
                                    risk: getSkillRiskLabel(item.risk_level),
                                    count: item.instance_count,
                                  })}
                                </div>
                                {item.description && (
                                  <div className="mt-2 line-clamp-2 text-sm text-gray-600">
                                    {item.description}
                                  </div>
                                )}
                              </div>
                              <div className="flex flex-wrap gap-2">
                                <button
                                  type="button"
                                  onClick={() =>
                                    downloadSkillAsset(item.id, item.name)
                                  }
                                  className="app-button-secondary"
                                >
                                  {t(
                                    "openClawResourcesPage.skillActions.download",
                                  )}
                                </button>
                                <button
                                  type="button"
                                  onClick={() => removeSkillAsset(item.id)}
                                  className="rounded-lg border border-red-200 bg-red-50 px-4 py-2 text-sm font-medium text-red-700 hover:bg-red-100"
                                >
                                  {t("common.delete")}
                                </button>
                              </div>
                            </div>
                          </div>
                        ))
                      )}
                    </div>
                    {!loading && filteredSkills.length > 0 ? (
                      <div className="mt-4 flex flex-col gap-3 border-t border-[#f3e7df] pt-4 sm:flex-row sm:items-center sm:justify-between">
                        <div className="text-sm text-[#8f8681]">
                          {t("openClawResourcesPage.paginationSummary", {
                            pageSize: CONFIG_CENTER_PAGE_SIZE,
                            from:
                              (currentResourcePage - 1) *
                                CONFIG_CENTER_PAGE_SIZE +
                              1,
                            to: Math.min(
                              currentResourcePage * CONFIG_CENTER_PAGE_SIZE,
                              filteredSkills.length,
                            ),
                          })}
                        </div>
                        <div className="flex items-center gap-2">
                          <button
                            type="button"
                            onClick={() =>
                              setResourcePage((current) =>
                                Math.max(1, current - 1),
                              )
                            }
                            disabled={currentResourcePage <= 1}
                            className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {t("openClawResourcesPage.paginationPrev")}
                          </button>
                          <div className="min-w-[88px] text-center text-sm font-medium text-[#5f5957]">
                            {currentResourcePage} / {resourcePageTotal}
                          </div>
                          <button
                            type="button"
                            onClick={() =>
                              setResourcePage((current) =>
                                Math.min(resourcePageTotal, current + 1),
                              )
                            }
                            disabled={currentResourcePage >= resourcePageTotal}
                            className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {t("openClawResourcesPage.paginationNext")}
                          </button>
                        </div>
                      </div>
                    ) : null}
                  </>
                ) : (
                  <>
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
                      <input
                        value={resourceSearch}
                        onChange={(e) => setResourceSearch(e.target.value)}
                        placeholder={t(
                          "openClawResourcesPage.searchPlaceholder",
                        )}
                        className="app-input w-full"
                      />
                      <button
                        type="button"
                        onClick={openNewResourceEditor}
                        className="app-button-primary whitespace-nowrap"
                      >
                        {t("openClawResourcesPage.actions.new")}
                      </button>
                    </div>
                    <div className="mt-3 text-sm text-gray-500">
                      {t("openClawResourcesPage.resourceListHint")}
                    </div>
                    <div className="mt-4 space-y-3">
                      {loading ? (
                        <div className="text-sm text-gray-500">
                          {t("openClawResourcesPage.loadingResources")}
                        </div>
                      ) : filteredResources.length === 0 ? (
                        <div className="rounded-2xl border border-dashed border-gray-300 px-4 py-8 text-sm text-gray-500">
                          {t("openClawResourcesPage.noResources")}
                        </div>
                      ) : (
                        paginatedResources.map((item) => (
                          <button
                            key={item.id}
                            type="button"
                            onClick={() => openResourceEditor(item)}
                            className={`w-full rounded-2xl border px-4 py-4 text-left transition ${
                              selectedResourceId === item.id
                                ? "border-indigo-300 bg-indigo-50"
                                : "border-gray-200 bg-white hover:border-gray-300"
                            }`}
                          >
                            <div className="flex items-center justify-between gap-3">
                              <div>
                                <div className="font-medium text-gray-900">
                                  {item.name}
                                </div>
                                <div className="mt-1 text-xs text-gray-500">
                                  {item.resource_key}
                                </div>
                                {item.description && (
                                  <div className="mt-2 line-clamp-2 text-sm text-gray-600">
                                    {item.description}
                                  </div>
                                )}
                              </div>
                              <span
                                className={`rounded-full px-2.5 py-1 text-xs font-medium ${item.enabled ? "bg-green-50 text-green-700" : "bg-gray-100 text-gray-500"}`}
                              >
                                {item.enabled
                                  ? t("openClawResourcesPage.enabled")
                                  : t("openClawResourcesPage.disabled")}
                              </span>
                            </div>
                          </button>
                        ))
                      )}
                    </div>
                    {!loading && filteredResources.length > 0 ? (
                      <div className="mt-4 flex flex-col gap-3 border-t border-[#f3e7df] pt-4 sm:flex-row sm:items-center sm:justify-between">
                        <div className="text-sm text-[#8f8681]">
                          {t("openClawResourcesPage.paginationSummary", {
                            pageSize: CONFIG_CENTER_PAGE_SIZE,
                            from:
                              (currentResourcePage - 1) *
                                CONFIG_CENTER_PAGE_SIZE +
                              1,
                            to: Math.min(
                              currentResourcePage * CONFIG_CENTER_PAGE_SIZE,
                              filteredResources.length,
                            ),
                          })}
                        </div>
                        <div className="flex items-center gap-2">
                          <button
                            type="button"
                            onClick={() =>
                              setResourcePage((current) =>
                                Math.max(1, current - 1),
                              )
                            }
                            disabled={currentResourcePage <= 1}
                            className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {t("openClawResourcesPage.paginationPrev")}
                          </button>
                          <div className="min-w-[88px] text-center text-sm font-medium text-[#5f5957]">
                            {currentResourcePage} / {resourcePageTotal}
                          </div>
                          <button
                            type="button"
                            onClick={() =>
                              setResourcePage((current) =>
                                Math.min(resourcePageTotal, current + 1),
                              )
                            }
                            disabled={currentResourcePage >= resourcePageTotal}
                            className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {t("openClawResourcesPage.paginationNext")}
                          </button>
                        </div>
                      </div>
                    ) : null}
                  </>
                )}
              </div>
            ) : (
              <div className="app-panel flex min-h-[420px] items-center justify-center p-6 sm:p-8">
                <div className="max-w-xl rounded-[28px] border border-dashed border-[#eadfd8] bg-[#fffaf7] px-8 py-10 text-center shadow-[0_26px_80px_-56px_rgba(72,44,24,0.45)]">
                  <div className="text-xs font-semibold uppercase tracking-[0.22em] text-[#b46c50]">
                    {t("common.comingSoon")}
                  </div>
                  <h3 className="mt-4 text-2xl font-semibold tracking-[-0.03em] text-[#171212]">
                    {t("openClawResourcesPage.notConfigurableYet", {
                      type:
                        selectedResourceTypeOption?.label ||
                        t("openClawResourcesPage.thisResourceType"),
                    })}
                  </h3>
                  <p className="mt-3 text-sm leading-7 text-[#6e6460]">
                    {t("openClawResourcesPage.onlyChannelConfigurable")}
                  </p>
                </div>
              </div>
            )}
          </div>
        )}

        {tab === "bundles" && (
          <div className="app-panel p-4 sm:p-6">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <div className="text-lg font-semibold text-gray-900">
                  {t("openClawResourcesPage.tabs.bundles")}
                </div>
                <div className="text-sm text-gray-500">
                  {t("openClawResourcesPage.bundlesSubtitle")}
                </div>
              </div>
              <button
                type="button"
                onClick={openNewBundleEditor}
                className="app-button-primary"
              >
                {t("openClawResourcesPage.actions.newBundle")}
              </button>
            </div>

            <div className="mt-5 space-y-3">
              {loading ? (
                <div className="text-sm text-gray-500">
                  {t("openClawResourcesPage.loadingBundles")}
                </div>
              ) : bundles.length === 0 ? (
                <div className="rounded-2xl border border-dashed border-gray-300 px-4 py-8 text-sm text-gray-500">
                  {t("openClawResourcesPage.noBundles")}
                </div>
              ) : (
                bundles.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => openBundleEditor(item)}
                    className={`w-full rounded-2xl border px-4 py-4 text-left transition ${
                      selectedBundleId === item.id
                        ? "border-indigo-300 bg-indigo-50"
                        : "border-gray-200 bg-white hover:border-gray-300"
                    }`}
                  >
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <div className="font-medium text-gray-900">
                          {item.name}
                        </div>
                        <div className="mt-1 text-xs text-gray-500">
                          {t("openClawResourcesPage.bundleResourceCount", {
                            count: item.items.length,
                          })}
                        </div>
                        {item.description && (
                          <div className="mt-2 line-clamp-2 text-sm text-gray-600">
                            {item.description}
                          </div>
                        )}
                      </div>
                      <span
                        className={`rounded-full px-2.5 py-1 text-xs font-medium ${item.enabled ? "bg-green-50 text-green-700" : "bg-gray-100 text-gray-500"}`}
                      >
                        {item.enabled
                          ? t("openClawResourcesPage.enabled")
                          : t("openClawResourcesPage.disabled")}
                      </span>
                    </div>
                  </button>
                ))
              )}
            </div>
          </div>
        )}

        {tab === "injections" && (
          <div className="app-panel p-6">
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="text-lg font-semibold text-gray-900">
                  {t("openClawResourcesPage.tabs.injections")}
                </div>
                <div className="text-sm text-gray-500">
                  {t("openClawResourcesPage.injectionsSubtitle")}
                </div>
              </div>
            </div>

            <div className="mt-6 overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-200 text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-[0.18em] text-gray-500">
                    <th className="py-3 pr-4">
                      {t("openClawResourcesPage.snapshot")}
                    </th>
                    <th className="py-3 pr-4">
                      {t("openClawResourcesPage.mode")}
                    </th>
                    <th className="py-3 pr-4">
                      {t("openClawResourcesPage.resources")}
                    </th>
                    <th className="py-3 pr-4">
                      {t("openClawResourcesPage.envCount")}
                    </th>
                    <th className="py-3 pr-4">{t("common.status")}</th>
                    <th className="py-3">
                      {t("openClawResourcesPage.created")}
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {loading ? (
                    <tr>
                      <td
                        colSpan={6}
                        className="py-8 text-center text-gray-500"
                      >
                        {t("openClawResourcesPage.loadingSnapshots")}
                      </td>
                    </tr>
                  ) : snapshots.length === 0 ? (
                    <tr>
                      <td
                        colSpan={6}
                        className="py-8 text-center text-gray-500"
                      >
                        {t("openClawResourcesPage.noInjectionRecords")}
                      </td>
                    </tr>
                  ) : (
                    snapshots.map((item) => (
                      <tr key={item.id}>
                        <td className="py-4 pr-4 font-medium text-gray-900">
                          #{item.id}
                        </td>
                        <td className="py-4 pr-4 text-gray-600">
                          {getInjectionModeLabel(item.mode)}
                        </td>
                        <td className="py-4 pr-4 text-gray-600">
                          {item.resolved_resources.length}
                        </td>
                        <td className="py-4 pr-4 text-gray-600">
                          {item.env_names.length}
                        </td>
                        <td className="py-4 pr-4">
                          <span
                            className={`rounded-full px-2.5 py-1 text-xs font-medium ${item.status === "active" ? "bg-green-50 text-green-700" : item.status === "failed" ? "bg-red-50 text-red-700" : "bg-amber-50 text-amber-700"}`}
                          >
                            {getSnapshotStatusLabel(item.status)}
                          </span>
                          {item.error_message && (
                            <div className="mt-1 text-xs text-red-600">
                              {item.error_message}
                            </div>
                          )}
                        </td>
                        <td className="py-4 text-gray-600">
                          {new Date(item.created_at).toLocaleString()}
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>

      <EditorModal
        open={resourceEditorOpen}
        title={
          resourceForm.id
            ? t("openClawResourcesPage.editResourceTitle")
            : t("openClawResourcesPage.newResourceTitle")
        }
        subtitle={
          resourceForm.id
            ? t("openClawResourcesPage.editResourceSubtitle")
            : t("openClawResourcesPage.newResourceSubtitle")
        }
        onClose={closeResourceEditor}
        busy={saving}
        panelClassName="max-w-5xl"
      >
        <div className="space-y-4">
          {error && (
            <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {error}
            </div>
          )}
          {notice && (
            <div className="rounded-xl border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-700">
              {notice}
            </div>
          )}

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label className="block text-sm font-medium text-gray-700">
                {t("common.type")}
              </label>
              <select
                value={resourceForm.resource_type}
                onChange={(e) => {
                  const nextType = e.target.value as OpenClawResourceType;
                  setResourceType(nextType);
                  if (nextType !== "channel") {
                    setSelectedChannelTemplateId("");
                  }
                  setResourceForm((current) => ({
                    ...current,
                    resource_type: nextType,
                    contentText: current.id
                      ? current.contentText
                      : defaultContentByType[nextType],
                  }));
                }}
                className="app-input mt-1 w-full"
              >
                {resourceTypeOptions.map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">
                {t("openClawResourcesPage.resourceKey")}
              </label>
              <input
                value={resourceForm.resource_key}
                onChange={(e) =>
                  setResourceForm((current) => ({
                    ...current,
                    resource_key: e.target.value,
                  }))
                }
                className="app-input mt-1 w-full"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">
                {t("openClawResourcesPage.name")}
              </label>
              <input
                value={resourceForm.name}
                onChange={(e) =>
                  setResourceForm((current) => ({
                    ...current,
                    name: e.target.value,
                  }))
                }
                className="app-input mt-1 w-full"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">
                {t("openClawResourcesPage.tags")}
              </label>
              <input
                value={resourceForm.tagsText}
                onChange={(e) =>
                  setResourceForm((current) => ({
                    ...current,
                    tagsText: e.target.value,
                  }))
                }
                className="app-input mt-1 w-full"
                placeholder={t("openClawResourcesPage.tagsPlaceholder")}
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700">
              {t("common.description")}
            </label>
            <textarea
              value={resourceForm.description}
              onChange={(e) =>
                setResourceForm((current) => ({
                  ...current,
                  description: e.target.value,
                }))
              }
              className="app-input mt-1 min-h-24 w-full"
            />
          </div>

          <label className="flex items-center gap-3 text-sm font-medium text-gray-700">
            <input
              type="checkbox"
              checked={resourceForm.enabled}
              onChange={(e) =>
                setResourceForm((current) => ({
                  ...current,
                  enabled: e.target.checked,
                }))
              }
            />
            {t("openClawResourcesPage.enabled")}
          </label>

          {resourceEditorIsConfigurable ? (
            <>
              {resourceForm.resource_type === "channel" && (
                <div className="rounded-2xl border border-[#f4d5c6] bg-[#fff7f3] p-4">
                  <div className="flex flex-col gap-3 lg:flex-row lg:items-end">
                    <div className="flex-1">
                      <label className="block text-sm font-medium text-gray-700">
                        {t("openClawResourcesPage.channelTemplate")}
                      </label>
                      <select
                        value={selectedChannelTemplateId}
                        onChange={(e) =>
                          setSelectedChannelTemplateId(e.target.value)
                        }
                        className="app-input mt-1 w-full"
                      >
                        <option value="">
                          {t("openClawResourcesPage.chooseStarterTemplate")}
                        </option>
                        {(["builtin", "plugin"] as const).map((category) => (
                          <optgroup
                            key={category}
                            label={t(
                              `openClawResourcesPage.templateCategories.${category}`,
                            )}
                          >
                            {channelTemplatesByCategory[category].map(
                              (template) => (
                                <option key={template.id} value={template.id}>
                                  {getChannelTemplateLabel(template.id)}
                                </option>
                              ),
                            )}
                          </optgroup>
                        ))}
                      </select>
                    </div>
                    <button
                      type="button"
                      onClick={() =>
                        applyChannelTemplate(selectedChannelTemplateId)
                      }
                      disabled={!selectedChannelTemplateId}
                      className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {t("openClawResourcesPage.actions.loadTemplate")}
                    </button>
                  </div>
                  <p className="mt-3 text-xs leading-5 text-gray-600">
                    {t("openClawResourcesPage.channelTemplateHint")}
                  </p>
                  {selectedChannelTemplate && (
                    <div className="mt-3 rounded-2xl border border-white/80 bg-white/80 px-4 py-3">
                      <div className="flex flex-wrap items-center gap-2">
                        <div className="text-sm font-semibold text-gray-900">
                          {getChannelTemplateLabel(selectedChannelTemplate.id)}
                        </div>
                        <span
                          className={`rounded-full px-2.5 py-1 text-xs font-medium ${selectedChannelTemplate.category === "plugin" ? "bg-indigo-50 text-indigo-700" : "bg-emerald-50 text-emerald-700"}`}
                        >
                          {t(
                            `openClawResourcesPage.templateCategories.${selectedChannelTemplate.category}`,
                          )}
                        </span>
                        <span className="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600">
                          {t("openClawResourcesPage.templateKey", {
                            key: selectedChannelTemplate.resourceKey,
                          })}
                        </span>
                      </div>
                      <p className="mt-2 text-sm text-gray-600">
                        {getChannelTemplateDescription(
                          selectedChannelTemplate.id,
                          selectedChannelTemplate.description,
                        )}
                      </p>
                    </div>
                  )}
                </div>
              )}

              {supportedChannelEditor ? (
                <div className="rounded-2xl border border-[#eadfd8] bg-[#fffaf7] p-4">
                  <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                    <div>
                      <div className="text-sm font-medium text-gray-700">
                        {t(supportedChannelEditor.titleKey)}
                      </div>
                      <p className="mt-1 text-xs leading-5 text-gray-600">
                        {t(supportedChannelEditor.descriptionKey)}
                      </p>
                    </div>
                    <div className="inline-flex rounded-full border border-[#eadfd8] bg-white p-1">
                      {(["form", "json"] as const).map((mode) => (
                        <button
                          key={mode}
                          type="button"
                          onClick={() => setChannelEditorMode(mode)}
                          disabled={mode === "form" && !supportedChannelForm}
                          className={`rounded-full px-4 py-2 text-sm font-medium transition ${
                            channelEditorMode === mode
                              ? "bg-[#171212] text-white"
                              : "text-[#6e6460] hover:bg-[#f5ece7]"
                          } disabled:cursor-not-allowed disabled:opacity-50`}
                        >
                          {mode === "form"
                            ? t("openClawResourcesPage.editorModes.form")
                            : t("openClawResourcesPage.editorModes.json")}
                        </button>
                      ))}
                    </div>
                  </div>

                  {channelEditorMode === "form" ? (
                    supportedChannelForm ? (
                      <div
                        className={`mt-4 grid grid-cols-1 gap-4 ${
                          supportedChannelEditor.fields.length >= 3
                            ? "md:grid-cols-3"
                            : supportedChannelEditor.fields.length === 2
                              ? "md:grid-cols-2"
                              : ""
                        }`}
                      >
                        {supportedChannelEditor.fields.map((field) => (
                          <div
                            key={`${supportedChannelEditor.id}-${field.key}`}
                          >
                            <label className="block text-sm font-medium text-gray-700">
                              {t(field.labelKey)}
                            </label>
                            <input
                              value={supportedChannelForm[field.key] || ""}
                              onChange={(e) =>
                                setResourceForm((current) => ({
                                  ...current,
                                  contentText:
                                    supportedChannelEditor.updateContentText(
                                      current.contentText,
                                      {
                                        [field.key]: e.target.value,
                                      },
                                    ),
                                }))
                              }
                              className="app-input mt-1 w-full"
                              placeholder={t(field.placeholderKey)}
                            />
                          </div>
                        ))}
                      </div>
                    ) : (
                      <div className="mt-4 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700">
                        {t("openClawResourcesPage.invalidChannelJson")}
                      </div>
                    )
                  ) : (
                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700">
                        {t("openClawResourcesPage.contentJson")}
                      </label>
                      <textarea
                        value={resourceForm.contentText}
                        onChange={(e) =>
                          setResourceForm((current) => ({
                            ...current,
                            contentText: e.target.value,
                          }))
                        }
                        className="app-input mt-1 min-h-[280px] w-full font-mono text-xs"
                        spellCheck={false}
                      />
                    </div>
                  )}
                </div>
              ) : (
                <div>
                  <label className="block text-sm font-medium text-gray-700">
                    {t("openClawResourcesPage.contentJson")}
                  </label>
                  <textarea
                    value={resourceForm.contentText}
                    onChange={(e) =>
                      setResourceForm((current) => ({
                        ...current,
                        contentText: e.target.value,
                      }))
                    }
                    className="app-input mt-1 min-h-[280px] w-full font-mono text-xs"
                    spellCheck={false}
                  />
                </div>
              )}

              <div className="flex flex-wrap gap-3 border-t border-[#f1e3db] pt-4">
                <button
                  type="button"
                  onClick={persistResource}
                  disabled={saving}
                  className="app-button-primary"
                >
                  {resourceForm.id
                    ? t("openClawResourcesPage.actions.saveResource")
                    : t("openClawResourcesPage.actions.createResource")}
                </button>
                <button
                  type="button"
                  onClick={validateResource}
                  disabled={saving}
                  className="app-button-secondary"
                >
                  {t("openClawResourcesPage.actions.validateJson")}
                </button>
                {resourceForm.id && (
                  <button
                    type="button"
                    onClick={cloneResource}
                    disabled={saving}
                    className="app-button-secondary"
                  >
                    {t("openClawResourcesPage.actions.clone")}
                  </button>
                )}
                {resourceForm.id && (
                  <button
                    type="button"
                    onClick={removeResource}
                    disabled={saving}
                    className="rounded-xl border border-red-200 bg-red-50 px-4 py-2.5 text-sm font-medium text-red-700 hover:bg-red-100"
                  >
                    {t("common.delete")}
                  </button>
                )}
              </div>
            </>
          ) : (
            <div className="rounded-[28px] border border-dashed border-[#eadfd8] bg-[#fffaf7] px-8 py-10 text-center shadow-[0_26px_80px_-56px_rgba(72,44,24,0.45)]">
              <div className="text-xs font-semibold uppercase tracking-[0.22em] text-[#b46c50]">
                {t("common.comingSoon")}
              </div>
              <h4 className="mt-4 text-2xl font-semibold tracking-[-0.03em] text-[#171212]">
                {t("openClawResourcesPage.notConfigurableYet", {
                  type:
                    selectedEditorTypeOption?.label ||
                    t("openClawResourcesPage.thisResourceType"),
                })}
              </h4>
              <p className="mt-3 text-sm leading-7 text-[#6e6460]">
                {t("openClawResourcesPage.onlyChannelConfigurable")}
              </p>
            </div>
          )}
        </div>
      </EditorModal>

      <EditorModal
        open={bundleEditorOpen}
        title={
          bundleForm.id
            ? t("openClawResourcesPage.editBundleTitle")
            : t("openClawResourcesPage.newBundleTitle")
        }
        subtitle={
          bundleForm.id
            ? t("openClawResourcesPage.editBundleSubtitle")
            : t("openClawResourcesPage.newBundleSubtitle")
        }
        onClose={closeBundleEditor}
        busy={saving}
        panelClassName="max-w-4xl"
      >
        <div className="space-y-4">
          {error && (
            <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {error}
            </div>
          )}
          {notice && (
            <div className="rounded-xl border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-700">
              {notice}
            </div>
          )}

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label className="block text-sm font-medium text-gray-700">
                {t("openClawResourcesPage.bundleName")}
              </label>
              <input
                value={bundleForm.name}
                onChange={(e) =>
                  setBundleForm((current) => ({
                    ...current,
                    name: e.target.value,
                  }))
                }
                className="app-input mt-1 w-full"
              />
            </div>
            <label className="flex items-center gap-3 pt-7 text-sm font-medium text-gray-700">
              <input
                type="checkbox"
                checked={bundleForm.enabled}
                onChange={(e) =>
                  setBundleForm((current) => ({
                    ...current,
                    enabled: e.target.checked,
                  }))
                }
              />
              {t("openClawResourcesPage.enabled")}
            </label>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700">
              {t("common.description")}
            </label>
            <textarea
              value={bundleForm.description}
              onChange={(e) =>
                setBundleForm((current) => ({
                  ...current,
                  description: e.target.value,
                }))
              }
              className="app-input mt-1 min-h-24 w-full"
            />
          </div>

          <div>
            <div className="text-sm font-medium text-gray-700">
              {t("openClawResourcesPage.bundleResources")}
            </div>
            <div className="mt-3 space-y-5">
              {groupedResources.map((group) => (
                <div key={group.value}>
                  <div className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-[#b46c50]">
                    {group.label}
                  </div>
                  <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
                    {group.items.map((item) => {
                      const checked = bundleForm.itemIds.includes(item.id);
                      return (
                        <label
                          key={item.id}
                          className={`flex cursor-pointer items-start gap-3 rounded-2xl border px-4 py-3 ${checked ? "border-indigo-300 bg-indigo-50" : "border-gray-200 bg-white"}`}
                        >
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(e) =>
                              setBundleForm((current) => ({
                                ...current,
                                itemIds: e.target.checked
                                  ? [...current.itemIds, item.id]
                                  : current.itemIds.filter(
                                      (value) => value !== item.id,
                                    ),
                              }))
                            }
                          />
                          <span>
                            <span className="block font-medium text-gray-900">
                              {item.name}
                            </span>
                            <span className="mt-1 block text-xs text-gray-500">
                              {item.resource_key}
                            </span>
                          </span>
                        </label>
                      );
                    })}
                    {group.items.length === 0 && (
                      <div className="rounded-2xl border border-dashed border-gray-300 px-4 py-3 text-sm text-gray-500">
                        {t("openClawResourcesPage.noResourcesForGroup", {
                          type: group.label,
                        })}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="flex flex-wrap gap-3 border-t border-[#f1e3db] pt-4">
            <button
              type="button"
              onClick={persistBundle}
              disabled={saving}
              className="app-button-primary"
            >
              {bundleForm.id
                ? t("openClawResourcesPage.actions.saveBundle")
                : t("openClawResourcesPage.actions.createBundle")}
            </button>
            {bundleForm.id && (
              <button
                type="button"
                onClick={cloneBundle}
                disabled={saving}
                className="app-button-secondary"
              >
                {t("openClawResourcesPage.actions.clone")}
              </button>
            )}
            {bundleForm.id && (
              <button
                type="button"
                onClick={removeBundle}
                disabled={saving}
                className="rounded-xl border border-red-200 bg-red-50 px-4 py-2.5 text-sm font-medium text-red-700 hover:bg-red-100"
              >
                {t("common.delete")}
              </button>
            )}
          </div>
        </div>
      </EditorModal>
    </UserLayout>
  );
};

export default OpenClawConfigCenterPage;
