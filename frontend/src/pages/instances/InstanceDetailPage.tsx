import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  Ban,
  BarChart3,
  Clock3,
  ChevronDown,
  Copy,
  Cpu,
  Eye,
  EyeOff,
  ExternalLink,
  HardDrive,
  KeyRound,
  MemoryStick,
  Network,
  Play,
  Plus,
  RotateCw,
  Square,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import ConfirmDialog from "../../components/ConfirmDialog";
import { InstanceServiceFrame } from "../../components/InstanceServiceFrame";
import UserLayout from "../../components/UserLayout";
import { WorkspaceFileManager } from "../../components/WorkspaceFileManager";
import { useI18n } from "../../contexts/I18nContext";
import { useInstanceStatusWebSocket } from "../../hooks/useWebSocket";
import type { InstanceStatusUpdate } from "../../hooks/useWebSocket";
import { instanceService } from "../../services/instanceService";
import { skillService } from "../../services/skillService";
import type {
  ExternalAccessExpirationMode,
  ExternalAccessExpirationPreset,
  ExternalAccessRequest,
  Instance,
  InstanceAvailability,
  InstanceExternalAccess,
  InstanceRuntimeCommand,
  InstanceRuntimeDetails,
  InstanceStatus,
  DesktopStreamProfile,
} from "../../types/instance";
import type { InstanceSkill, Skill } from "../../types/skill";

const META_POLL_INTERVAL_MS = 5000;
const RUNTIME_POLL_INTERVAL_MS = 5000;
const DESKTOP_STREAM_PROFILES: Array<{
  id: DesktopStreamProfile;
  labelKey: string;
  detail: string;
}> = [
  { id: "low", labelKey: "instances.desktopStreamLow", detail: "30 FPS / CRF 42" },
  { id: "standard", labelKey: "instances.desktopStreamStandard", detail: "35 FPS / CRF 34" },
  { id: "high", labelKey: "instances.desktopStreamHigh", detail: "40 FPS / CRF 24" },
];

function availabilityForStatus(status: string): InstanceAvailability {
  if (status === "running") {
    return "available";
  }
  if (status === "creating") {
    return "starting";
  }
  return "unavailable";
}

function availabilityLabel(availability: InstanceAvailability) {
  switch (availability) {
    case "available":
      return "Available";
    case "starting":
      return "Starting";
    default:
      return "Unavailable";
  }
}

function availabilityClass(availability: InstanceAvailability) {
  switch (availability) {
    case "available":
      return "border-emerald-200 bg-emerald-50 text-emerald-700";
    case "starting":
      return "border-amber-200 bg-amber-50 text-amber-700";
    default:
      return "border-slate-200 bg-slate-50 text-slate-600";
  }
}

function typeLabel(type: string) {
  return type === "hermes" ? "Hermes" : type === "openclaw" ? "OpenClaw" : type;
}

function formatBytes(value?: number) {
  if (!value || value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size.toFixed(size >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

function supportsWorkspace(instance: Instance) {
  return instance.type === "openclaw" || instance.type === "hermes" || Boolean(instance.workspace_path);
}

function workspaceInitialPath(instance: Instance, isDedicated: boolean) {
  const type = instance.type.trim().toLowerCase();
  if (type === "hermes") {
    return isDedicated ? ".hermes" : "home/.hermes";
  }
  if (type === "openclaw" && !isDedicated) {
    return "home/.openclaw";
  }
  return isDedicated ? "/config" : undefined;
}

function isUsableSkillStatus(skill: Skill) {
  const status = skill.status.trim().toLowerCase();
  return status === "" || status === "active" || status === "enabled";
}
function isAttachedInstanceSkill(item: InstanceSkill) {
  return item.status.trim().toLowerCase() !== "removed" && !item.removed_at;
}

function getErrorMessage(err: unknown, fallback: string) {
  const responseError = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
  if (responseError) {
    return responseError;
  }
  return err instanceof Error ? err.message : fallback;
}

function absoluteExternalURL(value?: string) {
  if (!value) {
    return "";
  }
  try {
    return new URL(value, window.location.origin).toString();
  } catch {
    return value;
  }
}

const externalExpirationOptions: {
  value: ExternalAccessExpirationPreset | "custom" | "permanent";
  label: string;
}[] = [
  { value: "1h", label: "1 hour" },
  { value: "24h", label: "24 hours" },
  { value: "7d", label: "7 days" },
  { value: "30d", label: "30 days" },
  { value: "custom", label: "Custom" },
  { value: "permanent", label: "Permanent" },
];

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function firstNumber(...values: unknown[]): number | null {
  for (const value of values) {
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
    if (typeof value === "string" && value.trim() !== "") {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) {
        return parsed;
      }
    }
  }
  return null;
}

function formatPercent(value: number | null) {
  if (value === null) {
    return "--";
  }
  return `${Math.max(0, Math.min(100, value)).toFixed(value >= 10 ? 0 : 1)}%`;
}

function formatDateTime(value: string | undefined, locale: string) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString(locale);
}

function resourcePercent(used: number | null, total: number | null) {
  if (used === null || total === null || total <= 0) {
    return null;
  }
  return (used / total) * 100;
}

function resourceRows(runtimeDetails: InstanceRuntimeDetails | null, instance: Instance) {
  const systemInfo = asRecord(runtimeDetails?.runtime?.system_info);
  const cpuInfo = asRecord(systemInfo?.cpu);
  const memoryInfo = asRecord(systemInfo?.memory);
  const diskInfo = asRecord(systemInfo?.disk);
  const networkInfo = asRecord(systemInfo?.network);

  const cpuPercent = firstNumber(
    cpuInfo?.usage_percent,
    cpuInfo?.percent,
    cpuInfo?.used_percent,
  );
  const memoryTotal = firstNumber(memoryInfo?.total_bytes, memoryInfo?.total);
  const memoryUsed = firstNumber(memoryInfo?.used_bytes, memoryInfo?.used);
  const diskTotal = firstNumber(diskInfo?.total_bytes, diskInfo?.total);
  const diskUsed = firstNumber(diskInfo?.used_bytes, diskInfo?.used);
  const networkDown = firstNumber(
    networkInfo?.rx_bytes_per_second,
    networkInfo?.download_bytes_per_second,
    networkInfo?.bytes_recv_per_second,
  );
  const networkUp = firstNumber(
    networkInfo?.tx_bytes_per_second,
    networkInfo?.upload_bytes_per_second,
    networkInfo?.bytes_sent_per_second,
  );

  return [
    {
      label: "CPU",
      value: cpuPercent === null ? `${instance.cpu_cores} cores` : formatPercent(cpuPercent),
      detail: cpuPercent === null ? "Requested capacity" : "Runtime usage",
      percent: cpuPercent,
      icon: Cpu,
    },
    {
      label: "Memory",
      value:
        memoryUsed !== null && memoryTotal !== null
          ? `${formatBytes(memoryUsed)} / ${formatBytes(memoryTotal)}`
          : `${instance.memory_gb} GB`,
      detail:
        memoryUsed !== null && memoryTotal !== null
          ? "Runtime usage"
          : "Requested capacity",
      percent: resourcePercent(memoryUsed, memoryTotal),
      icon: MemoryStick,
    },
    {
      label: "Disk",
      value:
        diskUsed !== null && diskTotal !== null
          ? `${formatBytes(diskUsed)} / ${formatBytes(diskTotal)}`
          : `${instance.disk_gb} GB`,
      detail:
        diskUsed !== null && diskTotal !== null
          ? "Runtime usage"
          : "Requested capacity",
      percent: resourcePercent(diskUsed, diskTotal),
      icon: HardDrive,
    },
    {
      label: "Network",
      value:
        networkDown !== null || networkUp !== null
          ? `${formatBytes(networkDown ?? 0)}/s down, ${formatBytes(networkUp ?? 0)}/s up`
          : "--",
      detail: "Live throughput",
      percent: null,
      icon: Network,
    },
  ];
}

function eventTime(command: InstanceRuntimeCommand) {
  return command.finished_at || command.started_at || command.dispatched_at || command.issued_at;
}

function eventTone(status: string) {
  switch (status.toLowerCase()) {
    case "completed":
    case "succeeded":
      return "border-emerald-200 bg-emerald-50 text-emerald-700";
    case "failed":
    case "timeout":
      return "border-red-200 bg-red-50 text-red-700";
    case "running":
    case "dispatched":
      return "border-indigo-200 bg-indigo-50 text-indigo-700";
    default:
      return "border-slate-200 bg-slate-50 text-slate-600";
  }
}

const InstanceDetailPage: React.FC = () => {
  const { t, locale } = useI18n();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const instanceId = id ? Number(id) : null;

  const [instance, setInstance] = useState<Instance | null>(null);
  const [status, setStatus] = useState<InstanceStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [externalAccess, setExternalAccess] = useState<InstanceExternalAccess | null>(null);
  const [externalShareURL, setExternalShareURL] = useState("");
  const [externalPassword, setExternalPassword] = useState("");
  const [externalPasswordVisible, setExternalPasswordVisible] = useState(false);
  const [externalAccessPanelOpen, setExternalAccessPanelOpen] = useState(false);
  const [externalActionLoading, setExternalActionLoading] = useState<string | null>(null);
  const [externalError, setExternalError] = useState<string | null>(null);
  const [copyState, setCopyState] = useState<string | null>(null);
  const [externalExpiresMode, setExternalExpiresMode] =
    useState<ExternalAccessExpirationMode>("preset");
  const [externalExpiresPreset, setExternalExpiresPreset] =
    useState<ExternalAccessExpirationPreset>("24h");
  const [externalCustomExpiresAt, setExternalCustomExpiresAt] = useState("");
  const [runtimeDetails, setRuntimeDetails] = useState<InstanceRuntimeDetails | null>(null);
  const [runtimeError, setRuntimeError] = useState<string | null>(null);
  const [instanceSkills, setInstanceSkills] = useState<InstanceSkill[]>([]);
  const [availableSkills, setAvailableSkills] = useState<Skill[]>([]);
  const [skillInventorySummary, setSkillInventorySummary] = useState({ total: 0, hiddenByRisk: 0 });
  const [selectedSkillIds, setSelectedSkillIds] = useState<number[]>([]);
  const [skillPickerOpen, setSkillPickerOpen] = useState(false);
  const skillPickerRef = useRef<HTMLDivElement | null>(null);
  const skillUploadInputRef = useRef<HTMLInputElement | null>(null);
  const desktopFrameRef = useRef<HTMLDivElement | null>(null);
  const [skillLoading, setSkillLoading] = useState(false);
  const [skillError, setSkillError] = useState<string | null>(null);
  const [skillNotice, setSkillNotice] = useState<string | null>(null);
  const [workspaceRefreshKey, setWorkspaceRefreshKey] = useState(0);
  const [desktopFrameHeight, setDesktopFrameHeight] = useState<number | null>(null);
  const [desktopStreamProfile, setDesktopStreamProfile] =
    useState<DesktopStreamProfile>("standard");
  const [desktopStreamSavedProfile, setDesktopStreamSavedProfile] =
    useState<DesktopStreamProfile | "">("");
  const [desktopStreamMessage, setDesktopStreamMessage] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<string | null>(null);

  const fetchMeta = useCallback(
    async (targetInstanceId: number, options?: { background?: boolean }) => {
      try {
        if (!options?.background) {
          setLoading(true);
        }
        const [instanceData, statusData] = await Promise.all([
          instanceService.getInstance(targetInstanceId),
          instanceService.getInstanceStatus(targetInstanceId),
        ]);
        setInstance(instanceData);
        setStatus(statusData);
        setError(null);
      } catch (err: unknown) {
        setError(getErrorMessage(err, t("instances.failedToLoad")));
      } finally {
        if (!options?.background) {
          setLoading(false);
        }
      }
    },
    [t],
  );

  const fetchExternalAccess = useCallback(async (targetInstanceId: number) => {
    try {
      const result = await instanceService.getExternalAccess(targetInstanceId);
      const access = result.external_access ?? null;
      setExternalAccess(access);
      setExternalShareURL(access?.enabled ? absoluteExternalURL(result.share_url) : "");
      setExternalPassword(access?.enabled && access.auth_mode === "password" ? result.password ?? "" : "");
      setExternalPasswordVisible(false);
      setExternalError(null);
    } catch (err: unknown) {
      setExternalError(getErrorMessage(err, "Failed to load external access"));
    }
  }, []);

  const fetchRuntimeDetails = useCallback(async (targetInstanceId: number) => {
    try {
      const data = await instanceService.getRuntimeDetails(targetInstanceId);
      setRuntimeDetails(data);
      setRuntimeError(null);
    } catch (err: unknown) {
      setRuntimeError(getErrorMessage(err, "Failed to load runtime details"));
    }
  }, []);

  const fetchSkills = useCallback(async (targetInstanceId: number) => {
    try {
      setSkillLoading(true);
      const attached = await skillService.listInstanceSkills(targetInstanceId);
      let reusable: Skill[] = [];
      try {
        reusable = await skillService.listAvailableInstanceSkills(targetInstanceId);
      } catch {
        reusable = [];
      }
      if (reusable.length === 0) {
        reusable = await skillService.listSkills();
      }
      const activeAttached = attached.filter(isAttachedInstanceSkill);
      const attachedIds = new Set(activeAttached.map((item) => item.skill_id));
      const activeReusableSkills = reusable.filter(isUsableSkillStatus);
      const riskAllowedSkills = activeReusableSkills.filter(
        (skill) => !["medium", "high"].includes(skill.risk_level.trim().toLowerCase()),
      );
      setInstanceSkills(activeAttached);
      setSkillInventorySummary({
        total: activeReusableSkills.length,
        hiddenByRisk: activeReusableSkills.length - riskAllowedSkills.length,
      });
      const attachableSkills = riskAllowedSkills.filter((skill) => !attachedIds.has(skill.id));
      const attachableSkillIds = new Set(attachableSkills.map((skill) => skill.id));
      setAvailableSkills(attachableSkills);
      setSelectedSkillIds((current) => current.filter((skillId) => attachableSkillIds.has(skillId)));
      if (attachableSkills.length === 0) {
        setSkillPickerOpen(false);
      }
      setSkillError(null);
    } catch (err: unknown) {
      setSkillError(getErrorMessage(err, "Failed to load skills"));
    } finally {
      setSkillLoading(false);
    }
  }, []);
  useEffect(() => {
    const savedProfile = instance?.desktop_stream_profile || "";
    setDesktopStreamProfile(savedProfile || "standard");
    setDesktopStreamSavedProfile(savedProfile);
    setDesktopStreamMessage(null);
  }, [instance?.desktop_stream_profile, instance?.id]);

  useEffect(() => {
    if (!instanceId || Number.isNaN(instanceId)) {
      setError(t("instances.instanceNotFound"));
      setLoading(false);
      return;
    }
    void fetchMeta(instanceId);
    void fetchExternalAccess(instanceId);
  }, [fetchExternalAccess, fetchMeta, instanceId, t]);

  useEffect(() => {
    if (!instanceId || Number.isNaN(instanceId)) {
      return;
    }
    const timer = window.setInterval(() => {
      if (!document.hidden) {
        void fetchMeta(instanceId, { background: true });
      }
    }, META_POLL_INTERVAL_MS);

    return () => window.clearInterval(timer);
  }, [fetchMeta, instanceId]);

  useInstanceStatusWebSocket(
    useCallback(
      (update: InstanceStatusUpdate) => {
        if (!instanceId || update.instance_id !== instanceId) {
          return;
        }
        setStatus((current) => ({
          ...(current ?? { instance_id: instanceId, created_at: "" }),
          status: update.status,
          availability: availabilityForStatus(update.status),
        }));
        setInstance((current) =>
          current ? { ...current, status: update.status as Instance["status"] } : current,
        );
      },
      [instanceId],
    ),
  );

  const isDedicatedInstance = useMemo(
    () =>
      Boolean(
        instance &&
          (instance.instance_mode === "pro" || instance.runtime_type !== "gateway"),
      ),
    [instance],
  );

  useEffect(() => {
    if (!skillPickerOpen) {
      return undefined;
    }
    const handlePointerDown = (event: MouseEvent) => {
      if (!skillPickerRef.current?.contains(event.target as Node)) {
        setSkillPickerOpen(false);
      }
    };
    document.addEventListener("mousedown", handlePointerDown);
    return () => document.removeEventListener("mousedown", handlePointerDown);
  }, [skillPickerOpen]);

  useLayoutEffect(() => {
    const element = desktopFrameRef.current;
    if (!element) {
      setDesktopFrameHeight(null);
      return undefined;
    }

    const updateDesktopFrameHeight = () => {
      const nextHeight = Math.round(element.getBoundingClientRect().height);
      setDesktopFrameHeight((current) => (nextHeight > 0 && current !== nextHeight ? nextHeight : current));
    };

    updateDesktopFrameHeight();

    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", updateDesktopFrameHeight);
      return () => window.removeEventListener("resize", updateDesktopFrameHeight);
    }

    const observer = new ResizeObserver(updateDesktopFrameHeight);
    observer.observe(element);
    return () => observer.disconnect();
  }, [instance?.id, isDedicatedInstance]);
  useEffect(() => {
    if (!instanceId || Number.isNaN(instanceId)) {
      setRuntimeDetails(null);
      setRuntimeError(null);
      setInstanceSkills([]);
      setAvailableSkills([]);
      setSkillInventorySummary({ total: 0, hiddenByRisk: 0 });
      setSelectedSkillIds([]);
      setSkillPickerOpen(false);
      setSkillNotice(null);
      return;
    }
    void fetchSkills(instanceId);
    if (isDedicatedInstance) {
      void fetchRuntimeDetails(instanceId);
    } else {
      setRuntimeDetails(null);
      setRuntimeError(null);
    }
  }, [fetchRuntimeDetails, fetchSkills, instanceId, isDedicatedInstance]);
  useEffect(() => {
    if (!instanceId || Number.isNaN(instanceId) || !isDedicatedInstance) {
      return;
    }
    const timer = window.setInterval(() => {
      if (!document.hidden) {
        void fetchRuntimeDetails(instanceId);
      }
    }, RUNTIME_POLL_INTERVAL_MS);

    return () => window.clearInterval(timer);
  }, [fetchRuntimeDetails, instanceId, isDedicatedInstance]);

  const availability = useMemo<InstanceAvailability>(() => {
    if (status?.availability) {
      return status.availability;
    }
    if (status?.status) {
      return availabilityForStatus(status.status);
    }
    return instance ? availabilityForStatus(instance.status) : "unavailable";
  }, [instance, status]);

  const handleAction = async (action: "start" | "stop" | "restart" | "delete") => {
    if (!instance) {
      return;
    }

    try {
      setActionLoading(action);
      if (action === "restart") {
        setActionMessage(t("instances.restartInProgress"));
      } else {
        setActionMessage(null);
      }
      switch (action) {
        case "start":
          await instanceService.startInstance(instance.id);
          break;
        case "stop":
          await instanceService.stopInstance(instance.id);
          break;
        case "restart":
          await instanceService.restartInstance(instance.id);
          setActionMessage(t("instances.restartSubmitted"));
          break;
        case "delete":
          await instanceService.deleteInstance(instance.id);
          setShowDeleteDialog(false);
          navigate("/instances");
          return;
      }
      await fetchMeta(instance.id, { background: true });
    } catch (err: unknown) {
      alert(getErrorMessage(err, t("instances.failedToLoad")));
    } finally {
      setActionLoading(null);
    }
  };

  const buildExternalAccessRequest = (): ExternalAccessRequest | null => {
    if (externalExpiresMode === "permanent") {
      return { expires_mode: "permanent" };
    }
    if (externalExpiresMode === "custom") {
      if (!externalCustomExpiresAt) {
        setExternalError("Custom expiration time is required");
        return null;
      }
      return {
        expires_mode: "custom",
        expires_at: new Date(externalCustomExpiresAt).toISOString(),
      };
    }
    return {
      expires_mode: "preset",
      expires_preset: externalExpiresPreset,
    };
  };

  const handleExternalAction = async (action: "share-link" | "password" | "disable") => {
    if (!instance) {
      return;
    }
    try {
      setExternalActionLoading(action);
      setExternalError(null);
      if (action === "share-link") {
        const request = buildExternalAccessRequest();
        if (!request) {
          return;
        }
        const result = await instanceService.enableExternalShareLink(instance.id, request);
        setExternalAccess(result.access);
        setExternalShareURL(absoluteExternalURL(result.share_url));
        setExternalPassword("");
        setExternalPasswordVisible(false);
        setExternalAccessPanelOpen(true);
      } else if (action === "password") {
        const request = buildExternalAccessRequest();
        if (!request) {
          return;
        }
        const result = await instanceService.createExternalAccessPassword(instance.id, request);
        setExternalAccess(result.access);
        setExternalPassword(result.password);
        setExternalPasswordVisible(false);
        setExternalShareURL(absoluteExternalURL(result.share_url));
        setExternalAccessPanelOpen(true);
      } else {
        await instanceService.disableExternalAccess(instance.id);
        await fetchExternalAccess(instance.id);
        setExternalShareURL("");
        setExternalPassword("");
        setExternalPasswordVisible(false);
        setExternalAccessPanelOpen(false);
      }
    } catch (err: unknown) {
      setExternalError(getErrorMessage(err, "External access update failed"));
      setExternalAccessPanelOpen(true);
    } finally {
      setExternalActionLoading(null);
    }
  };

  const saveDesktopStreamProfile = async () => {
    if (!instance || desktopStreamProfile === desktopStreamSavedProfile) {
      return true;
    }

    await instanceService.updateInstance(instance.id, {
      desktop_stream_profile: desktopStreamProfile,
    });
    const updatedInstance = await instanceService.getInstance(instance.id);
    setInstance(updatedInstance);
    const savedProfile = updatedInstance.desktop_stream_profile || desktopStreamProfile;
    setDesktopStreamSavedProfile(savedProfile);
    setDesktopStreamProfile(savedProfile);
    return true;
  };

  const handleSaveDesktopStreamProfile = async () => {
    if (!instance || desktopStreamProfile === desktopStreamSavedProfile) {
      return;
    }

    try {
      setActionLoading("desktop-stream-profile");
      setDesktopStreamMessage(null);
      setActionMessage(null);
      await saveDesktopStreamProfile();
      setDesktopStreamMessage(t("instances.desktopStreamSavedRestartRequired"));
    } catch (streamError) {
      console.error("Failed to update desktop stream profile", streamError);
      setDesktopStreamMessage(t("instances.desktopStreamSaveFailed"));
    } finally {
      setActionLoading(null);
    }
  };

  const handleApplyDesktopStreamProfile = async () => {
    if (!instance) {
      return;
    }

    try {
      setActionLoading("desktop-stream-restart");
      setDesktopStreamMessage(t("instances.restartInProgress"));
      setActionMessage(t("instances.restartInProgress"));
      await saveDesktopStreamProfile();
      await instanceService.restartInstance(instance.id);
      setDesktopStreamMessage(t("instances.restartSubmitted"));
      setActionMessage(t("instances.restartSubmitted"));
      await fetchMeta(instance.id, { background: true });
    } catch (streamError) {
      console.error("Failed to apply desktop stream profile", streamError);
      setDesktopStreamMessage(t("instances.desktopStreamSaveFailed"));
    } finally {
      setActionLoading(null);
    }
  };

  const copyExternalValue = async (key: string, value: string) => {
    if (!value) {
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      setCopyState(key);
      window.setTimeout(() => setCopyState((current) => (current === key ? null : current)), 1800);
    } catch (err: unknown) {
      setExternalError(getErrorMessage(err, "Copy failed"));
    }
  };

  const attachSelectedSkills = async () => {
    if (!instance || selectedSkillIds.length === 0) {
      return;
    }
    const skillOptionIds = new Set(skillOptions.map((skill) => skill.id));
    const skillIds = selectedSkillIds.filter((skillId) => skillOptionIds.has(skillId));
    if (skillIds.length === 0) {
      setSelectedSkillIds([]);
      return;
    }
    try {
      setSkillLoading(true);
      setSkillError(null);
      await Promise.all(skillIds.map((skillId) => skillService.attachSkillToInstance(instance.id, skillId)));
      setSelectedSkillIds([]);
      setSkillPickerOpen(false);
      setSkillNotice(`Attached ${skillIds.length} skill${skillIds.length === 1 ? "" : "s"}.`);
      await fetchSkills(instance.id);
      refreshWorkspaceFiles();
    } catch (err: unknown) {
      setSkillError(getErrorMessage(err, "Failed to attach skill"));
    } finally {
      setSkillLoading(false);
    }
  };

  const refreshWorkspaceFiles = () => {
    setWorkspaceRefreshKey((current) => current + 1);
  };

  const handleSkillArchiveUpload = async (fileList?: FileList | null) => {
    const file = fileList?.[0];
    if (skillUploadInputRef.current) {
      skillUploadInputRef.current.value = "";
    }
    if (!instance || !file) {
      return;
    }
    if (!file.name.toLowerCase().endsWith(".zip")) {
      setSkillError("Only .zip skill archives are supported.");
      setSkillNotice(null);
      return;
    }

    try {
      setSkillLoading(true);
      setSkillError(null);
      setSkillNotice(null);
      const importedSkills = await skillService.importSkills(file);
      let attachedCount = 0;
      const failures: string[] = [];
      for (const importedSkill of importedSkills) {
        try {
          await skillService.attachSkillToInstance(instance.id, importedSkill.id);
          attachedCount += 1;
        } catch (err: unknown) {
          failures.push(`${importedSkill.name || importedSkill.skill_key}: ${getErrorMessage(err, "attach failed")}`);
        }
      }
      setSelectedSkillIds([]);
      setSkillPickerOpen(false);
      await fetchSkills(instance.id);
      refreshWorkspaceFiles();
      const importedCount = importedSkills.length;
      if (failures.length > 0) {
        setSkillError(failures.join("; "));
      }
      if (attachedCount > 0) {
        setSkillNotice(`Imported ${importedCount} skill${importedCount === 1 ? "" : "s"}; attached ${attachedCount}.`);
      } else {
        setSkillNotice(`Imported ${importedCount} skill${importedCount === 1 ? "" : "s"}; none attached.`);
      }
    } catch (err: unknown) {
      setSkillError(getErrorMessage(err, "Failed to upload skill archive"));
      setSkillNotice(null);
    } finally {
      setSkillLoading(false);
    }
  };
  const toggleSelectedSkill = (skillId: number) => {
    setSelectedSkillIds((current) =>
      current.includes(skillId) ? current.filter((item) => item !== skillId) : [...current, skillId],
    );
  };

  const removeInstanceSkill = async (skillId: number) => {
    if (!instance) {
      return;
    }
    try {
      setSkillLoading(true);
      setSkillError(null);
      await skillService.removeSkillFromInstance(instance.id, skillId);
      setInstanceSkills((current) => current.filter((item) => item.skill_id !== skillId));
      setSkillNotice("Skill removed.");
      await fetchSkills(instance.id);
      refreshWorkspaceFiles();
    } catch (err: unknown) {
      setSkillError(getErrorMessage(err, "Failed to remove skill"));
    } finally {
      setSkillLoading(false);
    }
  };

  if (loading) {
    return (
      <UserLayout title={t("instances.details")}>
        <div className="flex h-64 items-center justify-center text-sm text-slate-500">
          {t("common.loading")}
        </div>
      </UserLayout>
    );
  }

  if (error || !instance) {
    return (
      <UserLayout title={t("instances.details")}>
        <div className="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error || t("instances.instanceNotFound")}
        </div>
      </UserLayout>
    );
  }

  const renderHeaderSection = (actionAccessory?: React.ReactNode) => (
    <div className="flex shrink-0 flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
      <div className="min-w-0">
        <Link
          to="/instances"
          className="mb-3 inline-flex items-center gap-2 text-sm font-medium text-slate-500 hover:text-slate-950"
        >
          <ArrowLeft className="h-4 w-4" />
          {t("instances.back")}
        </Link>
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="truncate text-2xl font-semibold text-slate-950">{instance.name}</h1>
          <span
            className={`inline-flex rounded-md border px-2 py-1 text-xs font-medium ${availabilityClass(
              availability,
            )}`}
          >
            {availabilityLabel(availability)}
          </span>
        </div>
        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm text-slate-500">
          <span>{typeLabel(instance.type)}</span>
          <span>Mode {instance.instance_mode === "pro" ? "Pro" : "Lite"}</span>
          <span>Runtime {instance.runtime_type}</span>
          <span>{formatBytes(status?.workspace_usage_bytes ?? instance.workspace_usage_bytes)}</span>
          <span>{formatDateTime(instance.updated_at, locale)}</span>
        </div>
      </div>

      <div className="flex flex-col items-start gap-2 lg:items-end">
        <div className="flex flex-wrap justify-start gap-2 lg:justify-end">
          {actionAccessory}
          {instance.status === "running" ? (
            <button
              type="button"
              className="app-button-secondary"
              onClick={() => void handleAction("stop")}
              disabled={actionLoading === "stop"}
            >
              <Square className="h-4 w-4" />
              {t("common.stop")}
            </button>
          ) : instance.status === "stopped" ? (
            <button
              type="button"
              className="app-button-primary"
              onClick={() => void handleAction("start")}
              disabled={actionLoading === "start"}
            >
              <Play className="h-4 w-4" />
              {t("common.start")}
            </button>
          ) : null}
          <button
            type="button"
            className="app-button-secondary"
            onClick={() => void handleAction("restart")}
            disabled={actionLoading === "restart" || instance.status === "deleting"}
          >
            <RotateCw className="h-4 w-4" />
            {t("common.restart")}
          </button>
          <button
            type="button"
            className="app-button-secondary border-red-200 text-red-700 hover:border-red-300 hover:bg-red-50 hover:text-red-800"
            onClick={() => setShowDeleteDialog(true)}
            disabled={instance.status === "deleting"}
          >
            <Trash2 className="h-4 w-4" />
            {t("common.delete")}
          </button>
        </div>
      </div>
    </div>
  );

  const shareLinkControl = instance.runtime_type.toLowerCase() !== "shell" && (
    <div className="relative">
      <button
        type="button"
        data-share-enabled={externalAccess?.enabled ? "true" : "false"}
        data-share-auth={externalAccess?.enabled ? externalAccess.auth_mode : "disabled"}
        className={`app-button-secondary ${
          externalAccess?.enabled
            ? "border-emerald-200 bg-emerald-50 text-emerald-700 hover:border-emerald-300 hover:bg-emerald-100 hover:text-emerald-800"
            : ""
        }`}
        disabled={externalActionLoading !== null}
        onClick={() => setExternalAccessPanelOpen((open) => !open)}
      >
        <ExternalLink className="h-4 w-4" />
        Share Link
        {externalAccess?.enabled && (
          <span
            className="ml-1 inline-flex h-5 min-w-5 items-center justify-center rounded bg-white/80 px-1.5 py-0.5 text-[11px] font-semibold"
            title={externalAccess.auth_mode === "password" ? "Key authentication enabled" : "Share link enabled"}
          >
            {externalAccess.auth_mode === "password" ? (
              <KeyRound className="h-3.5 w-3.5" aria-hidden="true" />
            ) : (
              "On"
            )}
          </span>
        )}
      </button>
      {externalAccessPanelOpen && (
        <div
          data-panel="share-link-popover"
          className="absolute right-0 top-full z-20 mt-2 w-[min(92vw,34rem)] rounded-lg border border-slate-200 bg-white p-4 text-left shadow-lg"
        >
          <div className="mb-3 flex items-center justify-between gap-3">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <div className="text-sm font-semibold text-slate-950">Share Link</div>
              </div>
              <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-slate-500">
                {externalAccess?.enabled && (
                  <span>
                    {externalAccess.expires_at
                      ? `Expires ${new Date(externalAccess.expires_at).toLocaleString(locale)}`
                      : "Permanent"}
                  </span>
                )}
                {externalAccess?.last_used_at && (
                  <span>Last used {new Date(externalAccess.last_used_at).toLocaleString(locale)}</span>
                )}
              </div>
            </div>
            <button
              type="button"
              className="cm-icon-button h-8 w-8 shrink-0"
              title="Close"
              onClick={() => setExternalAccessPanelOpen(false)}
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          {externalError && <div className="mb-2 text-xs text-red-600">{externalError}</div>}
          <div className="grid gap-3 text-xs">
            <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
              <select
                value={
                  externalExpiresMode === "permanent"
                    ? "permanent"
                    : externalExpiresMode === "custom"
                      ? "custom"
                      : externalExpiresPreset
                }
                onChange={(event) => {
                  const value = event.target.value;
                  if (value === "permanent") {
                    setExternalExpiresMode("permanent");
                    return;
                  }
                  if (value === "custom") {
                    setExternalExpiresMode("custom");
                    return;
                  }
                  setExternalExpiresMode("preset");
                  setExternalExpiresPreset(value as ExternalAccessExpirationPreset);
                }}
                className="h-10 rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-700 shadow-sm outline-none transition focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                disabled={externalActionLoading !== null}
                aria-label="Share link expiration"
              >
                {externalExpirationOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
              {externalExpiresMode === "custom" && (
                <input
                  type="datetime-local"
                  value={externalCustomExpiresAt}
                  onChange={(event) => setExternalCustomExpiresAt(event.target.value)}
                  className="h-10 rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-700 shadow-sm outline-none transition focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                  disabled={externalActionLoading !== null}
                  aria-label="Custom share link expiration"
                />
              )}
            </div>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                className="app-button-primary"
                disabled={externalActionLoading !== null}
                onClick={() => void handleExternalAction("share-link")}
              >
                <ExternalLink className="h-4 w-4" />
                Create Link
              </button>
              <button
                type="button"
                className="app-button-secondary"
                disabled={externalActionLoading !== null}
                onClick={() => void handleExternalAction("password")}
              >
                <KeyRound className="h-4 w-4" />
                Create Password
              </button>
              {externalAccess?.enabled && (
                <button
                  type="button"
                  className="app-button-secondary border-red-200 text-red-700 hover:border-red-300 hover:bg-red-50 hover:text-red-800"
                  disabled={externalActionLoading !== null}
                  onClick={() => void handleExternalAction("disable")}
                >
                  <Ban className="h-4 w-4" />
                  Disable
                </button>
              )}
            </div>
            {externalShareURL && (
              <div className="grid gap-1.5">
                <label className="text-xs font-semibold uppercase text-slate-500">
                  Short Link
                </label>
                <div className="flex min-w-0 items-center gap-2 rounded-md border border-slate-200 bg-slate-50 px-2 py-1.5">
                  <input
                    type="text"
                    readOnly
                    aria-label="Share link URL"
                    value={externalShareURL}
                    className="min-w-0 flex-1 truncate bg-transparent font-mono text-xs text-slate-700 outline-none"
                  />
                  <button
                    type="button"
                    className="cm-icon-button h-7 w-7 shrink-0"
                    title="Copy share link"
                    aria-label="Copy share link"
                    onClick={() => void copyExternalValue("share-url", externalShareURL)}
                  >
                    <Copy className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            )}
            {externalAccess?.enabled && externalAccess.auth_mode === "password" && externalPassword && (
              <div className="grid gap-1.5">
                <label className="text-xs font-semibold uppercase text-slate-500">
                  Password
                </label>
                <div className="flex min-w-0 items-center gap-2 rounded-md border border-slate-200 bg-slate-50 px-2 py-1.5">
                  <input
                    type={externalPasswordVisible ? "text" : "password"}
                    readOnly
                    aria-label="Share link password"
                    value={externalPassword}
                    className="min-w-0 flex-1 truncate bg-transparent font-mono text-xs text-slate-700 outline-none"
                  />
                  <button
                    type="button"
                    className="cm-icon-button h-7 w-7 shrink-0"
                    title="Copy password"
                    aria-label="Copy password"
                    onClick={() => void copyExternalValue("password", externalPassword)}
                  >
                    <Copy className="h-3.5 w-3.5" />
                  </button>
                  <button
                    type="button"
                    className="cm-icon-button h-7 w-7 shrink-0"
                    title={externalPasswordVisible ? "Hide password" : "Show password"}
                    aria-label={externalPasswordVisible ? "Hide password" : "Show password"}
                    onClick={() => setExternalPasswordVisible((visible) => !visible)}
                  >
                    {externalPasswordVisible ? (
                      <EyeOff className="h-3.5 w-3.5" />
                    ) : (
                      <Eye className="h-3.5 w-3.5" />
                    )}
                  </button>
                </div>
              </div>
            )}
            {!externalAccess?.enabled && !externalShareURL && !externalPassword && !externalError && (
              <div className="rounded-md border border-dashed border-slate-200 px-3 py-4 text-center text-sm text-slate-500">
                Create a share link or password to view access details.
              </div>
            )}
            {copyState && <span className="text-xs font-medium text-emerald-700">Copied</span>}
          </div>
        </div>
      )}
    </div>
  );

  const renderSkillPanel = () => (
    <section className="cm-surface px-4 py-4">
      <div className="mb-3 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="flex items-center gap-2">
            <KeyRound className="h-4 w-4 text-indigo-600" />
            <h2 className="text-sm font-semibold text-slate-950">Instance Skills</h2>
          </div>
          <div className="mt-1 text-xs text-slate-500">{instanceSkills.length} attached</div>
        </div>
        <button
          type="button"
          className="app-button-secondary shrink-0 disabled:cursor-not-allowed disabled:opacity-50"
          disabled={skillLoading}
          onClick={() => skillUploadInputRef.current?.click()}
          title="Upload skill archive"
        >
          <Upload className="h-4 w-4" />
          <span>Upload ZIP</span>
        </button>
        <input
          ref={skillUploadInputRef}
          type="file"
          accept=".zip,application/zip,application/x-zip-compressed"
          className="hidden"
          onChange={(event) => void handleSkillArchiveUpload(event.target.files)}
        />
      </div>
      {skillNotice && <div className="mb-3 text-xs text-emerald-700">{skillNotice}</div>}
      {skillError && <div className="mb-3 text-xs text-red-600">{skillError}</div>}
      {skillInventorySummary.hiddenByRisk > 0 && (
        <div className="mb-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
          {skillRiskPolicySummary}
        </div>
      )}
      <div ref={skillPickerRef} className="mb-3 flex gap-2">
        <div className="relative min-w-0 flex-1">
          <button
            type="button"
            className="flex w-full items-center justify-between gap-3 rounded-md border border-slate-200 bg-white px-3 py-2 text-left text-sm text-slate-700 outline-none transition hover:border-indigo-200 focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 disabled:cursor-not-allowed disabled:bg-slate-50 disabled:text-slate-400"
            disabled={skillLoading || skillOptions.length === 0}
            aria-haspopup="listbox"
            aria-expanded={skillPickerOpen}
            data-skill-picker="multi-select"
            onClick={() => setSkillPickerOpen((open) => !open)}
          >
            <span className="min-w-0 truncate">{skillOptions.length === 0 ? "No available skills" : skillPickerLabel}</span>
            <ChevronDown className={`h-4 w-4 shrink-0 text-slate-400 transition ${skillPickerOpen ? "rotate-180" : ""}`} />
          </button>
          {skillPickerOpen && (
            <div className="absolute left-0 right-0 z-30 mt-1 overflow-hidden rounded-md border border-slate-200 bg-white shadow-lg">
              <div className="max-h-64 divide-y divide-slate-100 overflow-y-auto py-1" role="listbox" aria-multiselectable="true">
                {skillOptions.map((skill) => (
                  <label
                    key={skill.id}
                    className="flex min-h-10 cursor-pointer items-center gap-3 px-3 py-2 text-sm text-slate-800 transition hover:bg-slate-50"
                  >
                    <input
                      type="checkbox"
                      checked={selectedSkillIdSet.has(skill.id)}
                      onChange={() => toggleSelectedSkill(skill.id)}
                      disabled={skillLoading}
                      className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                    />
                    <span className="min-w-0 truncate">{skill.name}</span>
                  </label>
                ))}
              </div>
            </div>
          )}
        </div>
        <button
          type="button"
          className="app-button-secondary"
          disabled={skillLoading || selectedSkillIds.length === 0}
          onClick={() => void attachSelectedSkills()}
          title={selectedSkillIds.length > 0 ? `Attach ${selectedSkillIds.length} skills` : "Attach selected skills"}
        >
          <Plus className="h-4 w-4" />
          {selectedSkillIds.length > 0 && <span className="text-xs">{selectedSkillIds.length}</span>}
        </button>
      </div>
      {skillEmptyMessage && <div className="mb-3 text-xs text-slate-500">{skillEmptyMessage}</div>}
      <div className="max-h-[260px] overflow-y-auto pr-1">
        {instanceSkills.length === 0 ? (
          <div className="rounded-md border border-dashed border-slate-200 px-3 py-6 text-center text-sm text-slate-500">
            No skills attached.
          </div>
        ) : (
          <div className="space-y-2">
            {instanceSkills.map((item) => (
              <div key={item.id} className="flex items-center justify-between gap-3 rounded-md border border-slate-200 px-3 py-2">
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium text-slate-900">
                    {item.skill?.name || `Skill #${item.skill_id}`}
                  </div>
                  <div className="mt-1 text-xs text-slate-500">
                    {item.status}{item.last_seen_at ? ` - ${formatDateTime(item.last_seen_at, locale)}` : ""}
                  </div>
                </div>
                <button
                  type="button"
                  className="cm-icon-button h-7 w-7 shrink-0"
                  disabled={skillLoading}
                  title="Remove skill"
                  onClick={() => void removeInstanceSkill(item.skill_id)}
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </section>
  );
  const renderLiteWorkspace = () => (
    <div className="flex min-h-0 flex-col gap-4">
      {renderHeaderSection(shareLinkControl)}
      {renderActionMessage()}
      <section className="grid min-h-0 items-stretch gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(360px,28rem)]">
        <div ref={desktopFrameRef} className="aspect-video min-h-[420px] min-w-0 overflow-hidden">
          <InstanceServiceFrame
            instanceId={instance.id}
            instanceName={instance.name}
            instanceType={instance.type}
            availability={availability}
          />
        </div>
        {supportsWorkspace(instance) ? (
          <div className="min-h-[420px] min-w-0 overflow-hidden xl:min-h-0" style={desktopFrameHeight ? { height: desktopFrameHeight } : undefined}>
            <WorkspaceFileManager
              instanceId={instance.id}
              initialPath={workspaceInitialPath(instance, isDedicatedInstance)}
              onMutation={() => fetchSkills(instance.id)}
              refreshKey={workspaceRefreshKey}
            />
          </div>
        ) : (
          <div className="cm-surface flex min-h-[420px] items-center justify-center overflow-hidden text-sm text-slate-500 xl:min-h-0" style={desktopFrameHeight ? { height: desktopFrameHeight } : undefined}>
            No workspace
          </div>
        )}
      </section>
      {renderSkillPanel()}
    </div>
  );
  const runtime = runtimeDetails?.runtime;
  const agent = runtimeDetails?.agent;
  const commands = [...(runtimeDetails?.commands ?? [])].sort((left, right) => {
    const leftTime = new Date(eventTime(left)).getTime();
    const rightTime = new Date(eventTime(right)).getTime();
    return (Number.isFinite(rightTime) ? rightTime : 0) - (Number.isFinite(leftTime) ? leftTime : 0);
  });
  const attachedSkillIds = new Set(instanceSkills.map((item) => item.skill_id));
  const skillOptions = availableSkills.filter((skill) => !attachedSkillIds.has(skill.id));
  const selectedSkillIdSet = new Set(selectedSkillIds);
  const skillPickerLabel = selectedSkillIds.length > 0 ? `${selectedSkillIds.length} selected` : "Select skills";
  const skillRiskPolicySummary = locale.startsWith("zh")
    ? `\u5b89\u5168\u7b56\u7565\u5df2\u9690\u85cf ${skillInventorySummary.hiddenByRisk} \u4e2a\u4e2d/\u9ad8\u98ce\u9669\u6280\u80fd\uff1b\u5f53\u524d\u53ef\u6dfb\u52a0 ${skillOptions.length} \u4e2a\u3002`
    : `Risk policy hid ${skillInventorySummary.hiddenByRisk} medium/high-risk skills; ${skillOptions.length} can still be added.`;
  const skillEmptyMessage =
    skillOptions.length > 0
      ? null
      : skillInventorySummary.total === 0
        ? "No skills loaded from the resource pool."
        : skillInventorySummary.hiddenByRisk > 0
          ? "All loaded skills are hidden by risk policy or already attached."
          : "All loaded skills are already attached.";  const desktopStreamDirty = desktopStreamProfile !== desktopStreamSavedProfile;
  const restartActionActive = actionLoading === "restart" || actionLoading === "desktop-stream-restart";

  const renderActionMessage = () =>
    actionMessage ? (
      <div className="flex items-center gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
        <RotateCw className={`h-4 w-4 ${restartActionActive ? "animate-spin" : ""}`} />
        <span>{actionMessage}</span>
      </div>
    ) : null;
  const overviewResourceRows = resourceRows(runtimeDetails, instance).filter((row) =>
    ["CPU", "Memory", "Disk"].includes(row.label),
  );
  const runtimeOverviewRows = [
    { label: "Infra", value: runtime?.infra_status || instance.status, detail: "Infrastructure", percent: null },
    { label: "Agent", value: agent?.status || runtime?.agent_status || "-", detail: "Runtime agent", percent: null },
    { label: "OpenClaw", value: runtime?.openclaw_status || "-", detail: "Process", percent: null },
    ...overviewResourceRows,
  ];

  const renderProWorkspace = () => (
    <div className="flex flex-col gap-4">
      {renderHeaderSection(shareLinkControl)}
      {renderActionMessage()}
      <section
        data-layout="pro-desktop-workspace"
        className="grid items-stretch gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(360px,28rem)]"
      >
        <div ref={desktopFrameRef} className="aspect-video min-h-[420px] min-w-0 overflow-hidden xl:min-h-0">
          <InstanceServiceFrame
            instanceId={instance.id}
            instanceName={instance.name}
            instanceType={instance.type}
            availability={availability}
          />
        </div>

        {supportsWorkspace(instance) ? (
          <div className="min-h-[420px] min-w-0 overflow-hidden xl:min-h-0" style={desktopFrameHeight ? { height: desktopFrameHeight } : undefined}>
            <WorkspaceFileManager
              instanceId={instance.id}
              initialPath={workspaceInitialPath(instance, isDedicatedInstance)}
              onMutation={() => fetchSkills(instance.id)}
              refreshKey={workspaceRefreshKey}
            />
          </div>
        ) : (
          <div className="cm-surface flex min-h-[420px] items-center justify-center overflow-hidden text-sm text-slate-500 xl:min-h-0" style={desktopFrameHeight ? { height: desktopFrameHeight } : undefined}>
            No workspace
          </div>
        )}
      </section>

      {(instance.runtime_type || "desktop") === "desktop" && (
        <section className="cm-surface px-4 py-4">
          <div className="mb-3 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <h2 className="text-sm font-semibold text-slate-950">
                {t("instances.desktopStreamProfile")}
              </h2>
              <p className="mt-1 text-xs text-slate-500">
                {t("instances.desktopStreamProfileDesc")}
              </p>
            </div>
            <div className="flex shrink-0 flex-wrap gap-2 sm:justify-end">
              <button
                type="button"
                onClick={handleSaveDesktopStreamProfile}
                disabled={
                  actionLoading === "desktop-stream-profile" ||
                  actionLoading === "desktop-stream-restart" ||
                  !desktopStreamDirty
                }
                className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {actionLoading === "desktop-stream-profile" ? t("common.saving") : t("common.save")}
              </button>
              <button
                type="button"
                onClick={handleApplyDesktopStreamProfile}
                disabled={actionLoading === "desktop-stream-profile" || actionLoading === "desktop-stream-restart"}
                className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
              >
                <RotateCw className={`h-4 w-4 ${actionLoading === "desktop-stream-restart" ? "animate-spin" : ""}`} />
                {actionLoading === "desktop-stream-restart"
                  ? t("instances.restarting")
                  : desktopStreamDirty
                    ? t("instances.saveAndRestart")
                    : t("instances.restartNow")}
              </button>
            </div>
          </div>
          <div className="grid gap-2 sm:grid-cols-3">
            {DESKTOP_STREAM_PROFILES.map((profile) => {
              const selected = desktopStreamProfile === profile.id;
              return (
                <button
                  key={profile.id}
                  type="button"
                  onClick={() => setDesktopStreamProfile(profile.id)}
                  className={`min-h-[82px] rounded-md border px-3 py-2 text-left transition ${
                    selected
                      ? "border-indigo-500 bg-indigo-50 ring-2 ring-indigo-100"
                      : "border-slate-200 bg-white hover:border-indigo-200"
                  }`}
                >
                  <div className="text-sm font-semibold text-slate-950">{t(profile.labelKey)}</div>
                  <div className="mt-2 font-mono text-xs text-slate-500">{profile.detail}</div>
                </button>
              );
            })}
          </div>
          <p className="mt-3 text-xs text-slate-500">
            {desktopStreamMessage || t("instances.restartRequiredAfterChange")}
          </p>
        </section>
      )}

      <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(280px,22rem)]">
        {renderSkillPanel()}

        <section data-section="runtime-overview" className="cm-surface px-3 py-3">
          <div className="mb-2 flex items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <BarChart3 className="h-4 w-4 text-indigo-600" />
              <h2 className="text-sm font-semibold text-slate-950">Runtime Overview</h2>
            </div>
            <span className="text-xs text-slate-500">
              {formatDateTime(runtime?.last_reported_at, locale)}
            </span>
          </div>
          {runtimeError && <div className="mb-2 text-xs text-red-600">{runtimeError}</div>}
          <div className="grid gap-1.5">
            {runtimeOverviewRows.map((row) => (
              <Metric key={row.label} label={row.label} value={row.value} detail={row.detail} percent={row.percent} />
            ))}
          </div>
        </section>
      </section>

      <section className="cm-surface px-4 py-4">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Clock3 className="h-4 w-4 text-indigo-600" />
            <h2 className="text-sm font-semibold text-slate-950">Runtime Events</h2>
          </div>
          <span className="text-xs text-slate-500">{commands.length} events</span>
        </div>
        <div className="max-h-[280px] overflow-y-auto pr-1">
          {commands.length === 0 ? (
            <div className="rounded-md border border-dashed border-slate-200 px-3 py-6 text-center text-sm text-slate-500">
              No runtime events yet.
            </div>
          ) : (
            <div className="space-y-2">
              {commands.slice(0, 12).map((command) => (
                <div key={command.id} className="rounded-md border border-slate-200 px-3 py-2">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="text-sm font-medium text-slate-900">{command.command_type}</span>
                    <span className={`rounded-md border px-2 py-0.5 text-xs font-medium ${eventTone(command.status)}`}>
                      {command.status}
                    </span>
                  </div>
                  <div className="mt-1 text-xs text-slate-500">
                    {formatDateTime(eventTime(command), locale)}
                  </div>
                  {command.error_message && (
                    <div className="mt-1 text-xs text-red-600">{command.error_message}</div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </section>
    </div>
  );

  return (
    <UserLayout title={instance.name}>
      <ConfirmDialog
        open={showDeleteDialog}
        title={t("common.delete")}
        message={t("instances.confirmDelete")}
        confirmLabel={t("common.delete")}
        cancelLabel={t("common.cancel")}
        destructive
        loading={actionLoading === "delete"}
        onCancel={() => setShowDeleteDialog(false)}
        onConfirm={() => void handleAction("delete")}
      />

      {isDedicatedInstance ? renderProWorkspace() : renderLiteWorkspace()}
    </UserLayout>
  );
};

function Metric({
  label,
  value,
  detail,
  percent,
}: {
  label: string;
  value: string;
  detail: string;
  percent: number | null;
}) {
  return (
    <div className="rounded-md border border-slate-200 px-2.5 py-1.5">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="text-xs text-slate-500">{label}</div>
          <div className="truncate text-sm font-semibold text-slate-950">{value}</div>
        </div>
        <div className="shrink-0 text-right text-[11px] text-slate-400">{detail}</div>
      </div>
      {percent !== null && (
        <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-slate-100">
          <div
            className="h-full rounded-full bg-indigo-500"
            style={{ width: formatPercent(percent) }}
          />
        </div>
      )}
    </div>
  );
}

export default InstanceDetailPage;
