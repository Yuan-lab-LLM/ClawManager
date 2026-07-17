import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { KeyRound, Plus, X } from "lucide-react";
import { useAuth } from "../contexts/AuthContext";
import { useI18n } from "../contexts/I18nContext";
import InstanceCollapsiblePanel from "./InstanceCollapsiblePanel";
import { instanceService } from "../services/instanceService";
import { skillHubService } from "../services/skillHubService";
import { skillService } from "../services/skillService";
import type { Instance, InstanceRuntimeDetails } from "../types/instance";
import type { InstanceSkill, Skill, SkillHubTag } from "../types/skill";

const SKILLS_POLL_INTERVAL_MS = 8000;
const SKILLS_BURST_POLL_INTERVAL_MS = 3000;
const SKILLS_BURST_WINDOW_MS = 60000;
const SKILL_SYNC_POLL_MS = 2000;
const SKILL_SYNC_TIMEOUT_MS = 60000;
const INSTANCE_SKILL_PAGE_SIZE = 5;

type TranslateFn = (key: string, variables?: Record<string, string | number>) => string;

function isHubInstalledSkill(item: InstanceSkill): boolean {
  return (item.source_type || "").toLowerCase() === "injected_by_clawmanager";
}

function resolveInstanceSkillProvenance(item: InstanceSkill): "native" | "hub_installed" {
  return isHubInstalledSkill(item) ? "hub_installed" : "native";
}

function isNativeInstanceSkill(item: InstanceSkill): boolean {
  return !isHubInstalledSkill(item);
}

function fingerprintNativeSkills(skills: InstanceSkill[]): string {
  return skills
    .filter(isNativeInstanceSkill)
    .map(
      (item) =>
        `${item.skill_id}:${item.last_seen_at || ""}:${item.skill?.source_type || ""}:${item.skill?.updated_at || ""}`,
    )
    .sort()
    .join("|");
}

function countNativeSkills(skills: InstanceSkill[]): number {
  return skills.filter(isNativeInstanceSkill).length;
}

function isSkillSyncCommandFinished(status: string): boolean {
  return status === "succeeded" || status === "failed" || status === "timed_out";
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms);
  });
}

function paginateInstanceSkills(skills: InstanceSkill[], page: number) {
  const totalPages = Math.max(1, Math.ceil(skills.length / INSTANCE_SKILL_PAGE_SIZE));
  const currentPage = Math.min(page, totalPages);
  return {
    items: skills.slice(
      (currentPage - 1) * INSTANCE_SKILL_PAGE_SIZE,
      currentPage * INSTANCE_SKILL_PAGE_SIZE,
    ),
    totalPages,
    currentPage,
  };
}

function matchesSkillSearch(query: string, skill?: Skill, fallbackId?: string | number): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  const haystacks = [
    skill?.name,
    skill?.skill_key,
    skill?.owner_username,
    skill?.description,
    fallbackId != null ? String(fallbackId) : "",
  ];
  return haystacks.some((value) => (value || "").toLowerCase().includes(needle));
}

function matchesInstanceSkillSearch(item: InstanceSkill, query: string): boolean {
  return matchesSkillSearch(query, item.skill, item.skill_id);
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

function skillRiskLabel(t: TranslateFn, riskLevel?: string | null) {
  switch ((riskLevel || "").toLowerCase()) {
    case "none":
      return t("instances.skillRiskNone");
    case "low":
      return t("instances.skillRiskLow");
    case "medium":
      return t("instances.skillRiskMedium");
    case "high":
      return t("instances.skillRiskHigh");
    default:
      return t("instances.skillRiskUnknown");
  }
}

type InstanceSkillCardProps = {
  item: InstanceSkill;
  t: TranslateFn;
  locale: string;
  userId?: number;
  actionLoading: string | null;
  allowImportToLibrary: boolean;
  onImportToLibrary: (skillId: number) => void;
  onRetryPackageCollect: (skillId: number) => void;
  onPublish: (skillId: number) => void;
  onRemove: (skillId: number) => void;
  shouldShowImportToLibrary: (skill: Skill) => boolean;
  isSkillPackagePending: (skill?: Skill) => boolean;
  isSkillPackageCollectFailed: (skill?: Skill) => boolean;
  blockReasonLabel: (reason?: string) => string | null;
  isLiteInstance?: boolean;
};

function InstanceSkillCard({
  item,
  t,
  locale,
  userId,
  actionLoading,
  allowImportToLibrary,
  onImportToLibrary,
  onRetryPackageCollect,
  onPublish,
  onRemove,
  shouldShowImportToLibrary,
  isSkillPackagePending,
  isSkillPackageCollectFailed,
  blockReasonLabel,
  isLiteInstance = false,
}: InstanceSkillCardProps) {
  const provenance =
    resolveInstanceSkillProvenance(item) === "hub_installed"
      ? t("instances.skillProvenanceInjected")
      : t("instances.skillProvenanceNative");

  return (
    <div className="rounded-md border border-slate-200 px-3 py-3">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium text-slate-900">
              {item.skill?.name || t("instances.skillFallback", { id: item.skill_id })}
            </span>
            <span className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] font-medium text-slate-600">
              {provenance}
            </span>
            <span className="rounded-full border border-slate-200 bg-white px-2 py-0.5 text-[11px] font-medium text-slate-600">
              {skillRiskLabel(t, item.skill?.risk_level)}
            </span>
            {isSkillPackagePending(item.skill) ? (
              <span className="rounded-full border border-sky-200 bg-sky-50 px-2 py-0.5 text-[11px] font-medium text-sky-800">
                {t("instances.skillPackageSyncing")}
              </span>
            ) : null}
            {isSkillPackageCollectFailed(item.skill) ? (
              <span className="rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-800">
                {t("instances.skillPackageCollectFailed")}
              </span>
            ) : null}
          </div>
          <p className="mt-1 text-xs text-slate-500">
            {item.skill?.skill_key || item.skill_id}
            {item.last_seen_at
              ? ` · ${t("instances.lastSeenAt", { value: formatDateTime(item.last_seen_at, locale) })}`
              : ""}
          </p>
          {item.skill?.package_collect_error ? (
            <p className="mt-1 text-xs text-amber-800">{item.skill.package_collect_error}</p>
          ) : null}
          {item.skill?.publish_blocked_reason && !item.skill.publishable ? (
            <p className="mt-1 text-xs text-slate-500">{blockReasonLabel(item.skill.publish_blocked_reason)}</p>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-2">
          {isSkillPackageCollectFailed(item.skill) ? (
            <button
              type="button"
              onClick={() => onRetryPackageCollect(item.skill_id)}
              disabled={actionLoading === `retry-package-${item.skill_id}`}
              className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {isLiteInstance
                ? t("instances.retryPackageMaterialize")
                : t("instances.retryPackageCollect")}
            </button>
          ) : null}
          {allowImportToLibrary &&
          isNativeInstanceSkill(item) &&
          item.skill &&
          item.skill.user_id === userId &&
          item.skill.source_type === "discovered" &&
          shouldShowImportToLibrary(item.skill) ? (
            <button
              type="button"
              onClick={() => onImportToLibrary(item.skill_id)}
              disabled={
                actionLoading === `import-library-${item.skill_id}` ||
                isSkillPackagePending(item.skill)
              }
              className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {t("skillHubPage.importToLibrary")}
            </button>
          ) : null}
          {item.skill &&
          isNativeInstanceSkill(item) &&
          item.skill.user_id === userId &&
          item.skill.source_type === "uploaded" &&
          item.skill.visibility !== "public" &&
          item.skill.publishable ? (
            <button
              type="button"
              onClick={() => onPublish(item.skill_id)}
              disabled={actionLoading === `publish-hub-${item.skill_id}`}
              className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {t("skillHubPage.publishFromInstance")}
            </button>
          ) : null}
          <button
            type="button"
            onClick={() => onRemove(item.skill_id)}
            disabled={actionLoading === `remove-skill-${item.skill_id}`}
            className="cm-icon-button h-8 w-8 shrink-0"
            title={t("instances.removeSkill")}
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
    </div>
  );
}

interface InstanceSkillHubPanelProps {
  instance: Instance;
  onRuntimeDetailsChange: (details: InstanceRuntimeDetails) => void;
  onPanelExpandedChange?: (expanded: boolean) => void;
}

const InstanceSkillHubPanel: React.FC<InstanceSkillHubPanelProps> = ({
  instance,
  onRuntimeDetailsChange,
  onPanelExpandedChange,
}) => {
  const { t, locale } = useI18n();
  const { user } = useAuth();
  const instanceId = instance.id;

  const [skillLoading, setSkillLoading] = useState(false);
  const [skillError, setSkillError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [skillsBurstUntil, setSkillsBurstUntil] = useState(0);
  const [skillSyncPhase, setSkillSyncPhase] = useState<"idle" | "syncing" | "success" | "failed">("idle");
  const [skillSyncDetail, setSkillSyncDetail] = useState("");
  const [lastSkillSyncAt, setLastSkillSyncAt] = useState<string | null>(null);
  const skillSyncActiveRef = useRef(false);
  const [instanceSkills, setInstanceSkills] = useState<InstanceSkill[]>([]);
  const [availableSkills, setAvailableSkills] = useState<Skill[]>([]);
  const [hubTags, setHubTags] = useState<SkillHubTag[]>([]);
  const [publishSkillId, setPublishSkillId] = useState<number | null>(null);
  const [selectedHubTagIds, setSelectedHubTagIds] = useState<number[]>([]);
  const [instanceSkillPage, setInstanceSkillPage] = useState(1);
  const [nativeSkillSearch, setNativeSkillSearch] = useState("");
  const [hubCatalogSearch, setHubCatalogSearch] = useState("");
  const [panelExpanded, setPanelExpanded] = useState(false);

  const handlePanelExpandedChange = useCallback(
    (expanded: boolean) => {
      setPanelExpanded(expanded);
      onPanelExpandedChange?.(expanded);
    },
    [onPanelExpandedChange],
  );

  const skillsPollInterval =
    skillsBurstUntil > Date.now() ? SKILLS_BURST_POLL_INTERVAL_MS : SKILLS_POLL_INTERVAL_MS;

  const reloadSkillSection = useCallback(async () => {
    const [instanceSkillItems, catalog, tagItems] = await Promise.all([
      skillService.listInstanceSkills(instanceId),
      skillHubService.listCatalog({ page: 1, page_size: 1000 }),
      skillHubService.listTags(),
    ]);
    setInstanceSkills(instanceSkillItems);
    setHubTags(tagItems);
    setAvailableSkills(
      (catalog.items || []).filter(
        (item) =>
          item.status === "active" &&
          (item.visibility || "").toLowerCase() === "public" &&
          item.risk_level !== "medium" &&
          item.risk_level !== "high",
      ),
    );
  }, [instanceId]);

  const refreshSkills = useCallback(async () => {
    const items = await skillService.listInstanceSkills(instanceId);
    setInstanceSkills(items);
  }, [instanceId]);

  useEffect(() => {
    let disposed = false;
    const load = async () => {
      try {
        setSkillLoading(true);
        await reloadSkillSection();
        if (!disposed) {
          setSkillError(null);
        }
      } catch (err: unknown) {
        if (!disposed) {
          const message =
            (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
            "Failed to load skills";
          setSkillError(message);
        }
      } finally {
        if (!disposed) {
          setSkillLoading(false);
        }
      }
    };
    void load();
    return () => {
      disposed = true;
    };
  }, [reloadSkillSection]);

  useEffect(() => {
    skillSyncActiveRef.current = false;
    setSkillSyncPhase("idle");
    setSkillSyncDetail("");
    setNativeSkillSearch("");
    setHubCatalogSearch("");
  }, [instanceId]);

  useEffect(() => {
    if (!panelExpanded) {
      return;
    }
    const skillsTimer = window.setInterval(() => {
      if (!document.hidden) {
        void refreshSkills();
      }
    }, skillsPollInterval);
    return () => window.clearInterval(skillsTimer);
  }, [panelExpanded, refreshSkills, skillsPollInterval]);

  useEffect(() => {
    setInstanceSkillPage(1);
  }, [instanceId]);

  useEffect(() => {
    setInstanceSkillPage(1);
  }, [nativeSkillSearch]);

  const filteredInstanceSkills = useMemo(() => {
    const query = nativeSkillSearch.trim();
    if (!query) {
      return instanceSkills;
    }
    return instanceSkills.filter((item) => matchesInstanceSkillSearch(item, query));
  }, [instanceSkills, nativeSkillSearch]);

  const instanceSkillsPagination = useMemo(
    () => paginateInstanceSkills(filteredInstanceSkills, instanceSkillPage),
    [filteredInstanceSkills, instanceSkillPage],
  );
  const hubCatalogRows = useMemo(() => {
    const installedBySkillId = new Map(instanceSkills.map((item) => [item.skill_id, item]));
    return availableSkills.map((skill) => ({
      skill,
      installed: installedBySkillId.has(skill.id),
      instanceSkill: installedBySkillId.get(skill.id),
    }));
  }, [availableSkills, instanceSkills]);
  const filteredHubCatalogRows = useMemo(() => {
    const query = hubCatalogSearch.trim();
    if (!query) {
      return hubCatalogRows;
    }
    return hubCatalogRows.filter(({ skill }) => matchesSkillSearch(query, skill, skill.id));
  }, [hubCatalogRows, hubCatalogSearch]);

  const hubErrorMessage = (err: unknown, fallback: string) => {
    const errorKey = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
    if (!errorKey) {
      return fallback;
    }
    const labelKey = `skillHubPage.blockReasons.${errorKey}`;
    const label = t(labelKey);
    return label === labelKey ? errorKey : label;
  };

  const shouldShowImportToLibrary = (skill: Skill) =>
    skill.status !== "deleted" &&
    (skill.source_type !== "uploaded" ||
      skill.publish_blocked_reason === "skill_package_pending" ||
      skill.publish_blocked_reason === "skill_package_materializing");

  const isSkillPackagePending = (skill?: Skill) =>
    skill?.publish_blocked_reason === "skill_package_pending" ||
    skill?.publish_blocked_reason === "skill_package_materializing" ||
    skill?.package_materialize_status === "pending" ||
    skill?.package_materialize_status === "running";

  const isSkillPackageCollectFailed = (skill?: Skill) =>
    skill?.publish_blocked_reason === "skill_package_collect_failed" ||
    skill?.publish_blocked_reason === "skill_package_materialize_failed" ||
    skill?.package_materialize_status === "failed";

  const blockReasonLabel = (reason?: string) => {
    if (!reason) {
      return null;
    }
    const labelKey = `skillHubPage.blockReasons.${reason}`;
    const label = t(labelKey);
    return label === labelKey ? reason : label;
  };

  const hubTagLabel = (tag: SkillHubTag) => t(`skillHubPage.tags.${tag.tag_key}`) || tag.name;

  const isLiteInstance =
    instance.instance_mode === "lite" || instance.runtime_type === "gateway";

  const usesWorkspaceSkillSync =
    isLiteInstance ||
    (Boolean(instance.workspace_path?.trim()) &&
      (instance.type === "hermes" || instance.type === "openclaw"));

  const handleInstallHubSkill = async (skillId: number) => {
    if (!usesWorkspaceSkillSync) {
      const runtimeSnapshot = await instanceService.getRuntimeDetails(instanceId);
      const agentStatus =
        runtimeSnapshot.agent?.status || runtimeSnapshot.runtime?.agent_status || "offline";
      if (agentStatus === "offline") {
        setSkillError(t("instances.installSkillRequiresAgent"));
        return;
      }
    }
    try {
      setActionLoading(`install-hub-${skillId}`);
      await skillService.attachSkillToInstance(instanceId, skillId);
      setSkillsBurstUntil(Date.now() + SKILLS_BURST_WINDOW_MS);
      await reloadSkillSection();
    } catch (err: unknown) {
      setSkillError(hubErrorMessage(err, t("instances.failedToAttachSkill")));
    } finally {
      setActionLoading(null);
    }
  };

  const handleRemoveSkill = async (skillId: number) => {
    try {
      setActionLoading(`remove-skill-${skillId}`);
      await skillService.removeSkillFromInstance(instanceId, skillId);
      await reloadSkillSection();
    } catch (err: unknown) {
      setSkillError(hubErrorMessage(err, t("instances.failedToRemoveSkill")));
    } finally {
      setActionLoading(null);
    }
  };

  const handleRetryPackageCollect = async (skillId: number) => {
    try {
      setActionLoading(`retry-package-${skillId}`);
      await skillHubService.retryPackageCollect(instanceId, skillId);
      setSkillsBurstUntil(Date.now() + SKILLS_BURST_WINDOW_MS);
      await reloadSkillSection();
    } catch (err: unknown) {
      alert(hubErrorMessage(err, t("instances.skillPackageCollectFailed")));
    } finally {
      setActionLoading(null);
    }
  };

  const handleImportSkillToLibrary = async (skillId: number) => {
    try {
      setActionLoading(`import-library-${skillId}`);
      await skillHubService.importInstanceSkill(instanceId, skillId);
      await reloadSkillSection();
    } catch (err: unknown) {
      const errorKey = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
      if (errorKey === "skill_package_pending") {
        await reloadSkillSection();
        return;
      }
      alert(hubErrorMessage(err, t("skillHubPage.importToLibraryFailed")));
    } finally {
      setActionLoading(null);
    }
  };

  const handlePublishSkillToHub = async () => {
    if (publishSkillId === null || selectedHubTagIds.length === 0) {
      alert(t("skillHubPage.errors.tagsRequired"));
      return;
    }
    try {
      setActionLoading(`publish-hub-${publishSkillId}`);
      await skillHubService.publishFromInstance(instanceId, publishSkillId, selectedHubTagIds);
      setPublishSkillId(null);
      setSelectedHubTagIds([]);
      await reloadSkillSection();
    } catch (err: unknown) {
      alert(hubErrorMessage(err, t("skillHubPage.errors.publish")));
    } finally {
      setActionLoading(null);
    }
  };

  const handleSyncInstanceSkills = async () => {
    if (skillSyncPhase === "syncing") {
      return;
    }

    skillSyncActiveRef.current = true;
    setSkillSyncPhase("syncing");
    setSkillSyncDetail(t("instances.syncSkillsStatusRequesting"));

    const baselineFingerprint = fingerprintNativeSkills(instanceSkills);
    const baselineCount = countNativeSkills(instanceSkills);

    try {
      if (!usesWorkspaceSkillSync) {
        const runtimeSnapshot = await instanceService.getRuntimeDetails(instanceId);
        const agentStatus =
          runtimeSnapshot.agent?.status || runtimeSnapshot.runtime?.agent_status || "offline";
        if (agentStatus === "offline") {
          setSkillSyncPhase("failed");
          setSkillSyncDetail(t("instances.syncSkillsAgentOffline"));
          return;
        }
      }

      const command = await instanceService.syncInstanceSkills(instanceId);
      setSkillsBurstUntil(Date.now() + SKILLS_BURST_WINDOW_MS);
      setSkillSyncDetail(t("instances.syncSkillsStatusAgent", { status: command.status }));

      const deadline = Date.now() + SKILL_SYNC_TIMEOUT_MS;
      let latestSkills = instanceSkills;
      let resolved = false;
      let syncFailed = false;
      let resolvedSuccessDetail: string | null = null;
      let fingerprintChanged = false;

      while (Date.now() < deadline && skillSyncActiveRef.current) {
        const [runtimeData, skillItems] = await Promise.all([
          instanceService.getRuntimeDetails(instanceId),
          skillService.listInstanceSkills(instanceId),
        ]);
        onRuntimeDetailsChange(runtimeData);
        setInstanceSkills(skillItems);
        latestSkills = skillItems;

        const trackedCommand =
          runtimeData.commands.find((item) => item.id === command.id) || command;
        const nextFingerprint = fingerprintNativeSkills(skillItems);
        const nextCount = countNativeSkills(skillItems);

        setSkillSyncDetail(
          t("instances.syncSkillsStatusAgent", { status: trackedCommand.status }),
        );

        if (nextFingerprint !== baselineFingerprint) {
          fingerprintChanged = true;
          resolvedSuccessDetail =
            nextCount > baselineCount
              ? t("instances.syncSkillsSuccessCount", { count: nextCount })
              : t("instances.syncSkillsSuccessUpdated");
          setSkillSyncPhase("success");
          setLastSkillSyncAt(new Date().toISOString());
          setSkillSyncDetail(resolvedSuccessDetail);
          resolved = true;
          break;
        }

        if (isSkillSyncCommandFinished(trackedCommand.status)) {
          if (trackedCommand.status === "succeeded") {
            resolvedSuccessDetail =
              countNativeSkills(latestSkills) > 0
                ? t("instances.syncSkillsSuccessCount", {
                    count: countNativeSkills(latestSkills),
                  })
                : t("instances.syncSkillsSuccessNoChange");
            setSkillSyncPhase("success");
            setLastSkillSyncAt(new Date().toISOString());
            setSkillSyncDetail(resolvedSuccessDetail);
          } else {
            syncFailed = true;
            setSkillSyncPhase("failed");
            setSkillSyncDetail(
              t("instances.syncSkillsCommandFailed", {
                error: trackedCommand.error_message || trackedCommand.status,
              }),
            );
          }
          resolved = true;
          break;
        }

        await sleep(SKILL_SYNC_POLL_MS);
      }

      if (!resolved && skillSyncActiveRef.current) {
        syncFailed = true;
        setSkillSyncPhase("failed");
        setSkillSyncDetail(t("instances.syncSkillsTimeout"));
      }

      if (
        usesWorkspaceSkillSync &&
        skillSyncActiveRef.current &&
        !syncFailed &&
        resolvedSuccessDetail
      ) {
        const hasPendingMaterialize = (skills: InstanceSkill[]) =>
          skills
            .filter(isNativeInstanceSkill)
            .some(
              (item) =>
                isSkillPackagePending(item.skill) &&
                (item.skill?.package_materialize_status === "pending" ||
                  item.skill?.package_materialize_status === "running" ||
                  item.skill?.publish_blocked_reason === "skill_package_materializing" ||
                  item.skill?.publish_blocked_reason === "skill_package_pending"),
            );

        let materializedSkills = latestSkills;
        if (hasPendingMaterialize(materializedSkills)) {
          setSkillSyncDetail(t("instances.syncSkillsMaterializing"));
          const materializeDeadline = Date.now() + 30_000;
          while (Date.now() < materializeDeadline && skillSyncActiveRef.current) {
            await sleep(3_000);
            materializedSkills = await skillService.listInstanceSkills(instanceId);
            setInstanceSkills(materializedSkills);
            if (!hasPendingMaterialize(materializedSkills)) {
              break;
            }
          }
        }

        const nextCount = countNativeSkills(materializedSkills);
        setSkillSyncDetail(
          nextCount > baselineCount
            ? t("instances.syncSkillsSuccessCount", { count: nextCount })
            : fingerprintChanged
              ? t("instances.syncSkillsSuccessUpdated")
              : resolvedSuccessDetail,
        );
      }
    } catch (err: unknown) {
      setSkillSyncPhase("failed");
      setSkillSyncDetail(hubErrorMessage(err, t("instances.syncSkillsFailed")));
    } finally {
      skillSyncActiveRef.current = false;
    }
  };

  const hubInstalledCount = useMemo(
    () => hubCatalogRows.filter((row) => row.installed).length,
    [hubCatalogRows],
  );

  const skillPanelSummary = t("instances.skillPanelSummary", {
    native: countNativeSkills(instanceSkills),
    installed: hubInstalledCount,
  });

  return (
    <>
      <InstanceCollapsiblePanel
        storageKey={`clawmanager.instance-panel.skills.${instanceId}`}
        title={t("instances.skillManagement")}
        icon={<KeyRound className="h-4 w-4 text-indigo-600" />}
        summary={skillPanelSummary}
        onExpandedChange={handlePanelExpandedChange}
        headerActions={
          <button
            type="button"
            onClick={() => void handleSyncInstanceSkills()}
            disabled={instance.status !== "running" || skillSyncPhase === "syncing"}
            className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
          >
            {skillSyncPhase === "syncing" ? t("instances.syncSkillsInProgress") : t("instances.syncSkills")}
          </button>
        }
      >
        <Link to="/skill-hub" className="inline-block text-xs font-medium text-indigo-600 hover:text-indigo-800">
          {t("skillHubPage.goToHub")}
        </Link>

        {skillError ? <div className="mt-3 text-xs text-red-600">{skillError}</div> : null}
        {(skillSyncPhase !== "idle" || lastSkillSyncAt) && (
          <div
            className={`mt-3 rounded-md border px-3 py-2 text-xs ${
              skillSyncPhase === "success"
                ? "border-emerald-200 bg-emerald-50 text-emerald-900"
                : skillSyncPhase === "failed"
                  ? "border-red-200 bg-red-50 text-red-800"
                  : skillSyncPhase === "syncing"
                    ? "border-sky-200 bg-sky-50 text-sky-900"
                    : "border-slate-200 bg-slate-50 text-slate-700"
            }`}
          >
            {skillSyncDetail}
            {lastSkillSyncAt && skillSyncPhase !== "syncing" ? (
              <div className="mt-1 opacity-80">
                {t("instances.syncSkillsLastSynced", {
                  time: formatDateTime(lastSkillSyncAt, locale),
                })}
              </div>
            ) : null}
          </div>
        )}

        <div className="mt-4 space-y-4">
          <div>
            <h3 className="text-sm font-semibold text-slate-900">{t("instances.nativeSkillsTitle")}</h3>
            <p className="mt-1 text-xs text-slate-500">{t("instances.nativeSkillsDesc")}</p>
            <input
              type="search"
              className="app-input mt-3 w-full max-w-md"
              placeholder={t("instances.skillSearchPlaceholder")}
              value={nativeSkillSearch}
              onChange={(event) => setNativeSkillSearch(event.target.value)}
            />
            <div className="mt-3 space-y-2">
              {instanceSkills.length === 0 ? (
                <div className="rounded-md border border-dashed border-slate-200 px-3 py-4 text-sm text-slate-500">
                  {t("instances.noNativeSkills")}
                </div>
              ) : filteredInstanceSkills.length === 0 ? (
                <div className="rounded-md border border-dashed border-slate-200 px-3 py-4 text-sm text-slate-500">
                  {t("instances.noSkillsMatchingSearch")}
                </div>
              ) : (
                instanceSkillsPagination.items.map((item) => (
                  <InstanceSkillCard
                    key={`instance-skill-${item.skill_id}-${item.id}`}
                    item={item}
                    t={t}
                    locale={locale}
                    userId={user?.id}
                    actionLoading={actionLoading}
                    allowImportToLibrary={isNativeInstanceSkill(item)}
                    onImportToLibrary={(skillId) => void handleImportSkillToLibrary(skillId)}
                    onRetryPackageCollect={(skillId) => void handleRetryPackageCollect(skillId)}
                    onPublish={(skillId) => {
                      setPublishSkillId(skillId);
                      setSelectedHubTagIds([]);
                    }}
                    onRemove={(skillId) => void handleRemoveSkill(skillId)}
                    shouldShowImportToLibrary={shouldShowImportToLibrary}
                    isSkillPackagePending={isSkillPackagePending}
                    isSkillPackageCollectFailed={isSkillPackageCollectFailed}
                    blockReasonLabel={blockReasonLabel}
                    isLiteInstance={isLiteInstance}
                  />
                ))
              )}
              {filteredInstanceSkills.length > INSTANCE_SKILL_PAGE_SIZE ? (
                <div className="mt-4 flex items-center justify-between gap-3 border-t border-slate-200 pt-4">
                  <p className="text-sm text-slate-500">
                    {t("instances.skillPageSummary", {
                      page: instanceSkillsPagination.currentPage,
                      totalPages: instanceSkillsPagination.totalPages,
                      totalSkills: filteredInstanceSkills.length,
                    })}
                  </p>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={() =>
                        setInstanceSkillPage((current) => Math.max(1, current - 1))
                      }
                      disabled={instanceSkillsPagination.currentPage <= 1}
                      className="rounded-lg border border-slate-200 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {t("instances.previous")}
                    </button>
                    <button
                      type="button"
                      onClick={() =>
                        setInstanceSkillPage((current) =>
                          Math.min(instanceSkillsPagination.totalPages, current + 1),
                        )
                      }
                      disabled={
                        instanceSkillsPagination.currentPage >=
                        instanceSkillsPagination.totalPages
                      }
                      className="rounded-lg border border-slate-200 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {t("instances.nextPage")}
                    </button>
                  </div>
                </div>
              ) : null}
            </div>
          </div>

          <div className="border-t border-slate-200 pt-4">
            <div>
              <h3 className="text-sm font-semibold text-slate-900">{t("instances.hubCatalogTitle")}</h3>
              <p className="mt-1 text-xs text-slate-500">{t("instances.hubCatalogDesc")}</p>
            </div>
            <input
              type="search"
              className="app-input mt-3 w-full max-w-md"
              placeholder={t("instances.skillSearchPlaceholder")}
              value={hubCatalogSearch}
              onChange={(event) => setHubCatalogSearch(event.target.value)}
            />
            <div className="mt-3 space-y-2">
              {hubCatalogRows.length === 0 ? (
                <div className="rounded-md border border-dashed border-slate-200 px-3 py-4 text-sm text-slate-500">
                  {t("instances.noHubCatalogSkills")}
                </div>
              ) : filteredHubCatalogRows.length === 0 ? (
                <div className="rounded-md border border-dashed border-slate-200 px-3 py-4 text-sm text-slate-500">
                  {t("instances.noSkillsMatchingSearch")}
                </div>
              ) : (
                filteredHubCatalogRows.map(({ skill, installed }) => (
                  <div
                    key={`hub-catalog-${skill.id}`}
                    className="rounded-md border border-slate-200 px-3 py-3"
                  >
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-sm font-medium text-slate-900">{skill.name}</span>
                          <span className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] font-medium text-slate-600">
                            {skillRiskLabel(t, skill.risk_level)}
                          </span>
                          {installed ? (
                            <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[11px] font-medium text-emerald-800">
                              {t("instances.hubSkillInstalled")}
                            </span>
                          ) : null}
                          {(skill.tags || []).slice(0, 3).map((tag) => (
                            <span
                              key={tag.id}
                              className="rounded-full border border-indigo-100 bg-indigo-50 px-2 py-0.5 text-[11px] font-medium text-indigo-700"
                            >
                              {hubTagLabel(tag)}
                            </span>
                          ))}
                        </div>
                        <p className="mt-1 text-xs text-slate-500">
                          {skill.skill_key}
                          {skill.owner_username ? ` · ${skill.owner_username}` : ""}
                        </p>
                      </div>
                      <div className="flex flex-wrap gap-2">
                        {installed ? (
                          <button
                            type="button"
                            onClick={() => void handleRemoveSkill(skill.id)}
                            disabled={actionLoading === `remove-skill-${skill.id}`}
                            className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            {t("instances.removeSkill")}
                          </button>
                        ) : (
                          <button
                            type="button"
                            className="app-button-secondary"
                            disabled={
                              skillLoading ||
                              instance.status !== "running" ||
                              actionLoading === `install-hub-${skill.id}`
                            }
                            onClick={() => void handleInstallHubSkill(skill.id)}
                          >
                            <Plus className="h-4 w-4" />
                            {actionLoading === `install-hub-${skill.id}`
                              ? t("instances.installingSkill")
                              : t("instances.hubSkillInstallAction")}
                          </button>
                        )}
                      </div>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      </InstanceCollapsiblePanel>

      {publishSkillId !== null ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
          <div className="w-full max-w-lg rounded-lg bg-white p-6 shadow-xl">
            <h2 className="text-lg font-semibold text-slate-950">{t("skillHubPage.publishFromInstance")}</h2>
            <p className="mt-2 text-sm text-slate-600">{t("skillHubPage.selectTags")}</p>
            <div className="mt-4 flex flex-wrap gap-2">
              {hubTags
                .filter((tag) => !tag.admin_only || user?.role === "admin")
                .map((tag) => (
                  <label
                    key={tag.id}
                    className="flex items-center gap-2 rounded-full border border-slate-200 px-3 py-1 text-sm"
                  >
                    <input
                      type="checkbox"
                      checked={selectedHubTagIds.includes(tag.id)}
                      onChange={() => {
                        setSelectedHubTagIds((current) =>
                          current.includes(tag.id)
                            ? current.filter((id) => id !== tag.id)
                            : [...current, tag.id],
                        );
                      }}
                    />
                    {hubTagLabel(tag)}
                  </label>
                ))}
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="app-button-secondary" onClick={() => setPublishSkillId(null)}>
                {t("common.cancel")}
              </button>
              <button type="button" className="app-button-primary" onClick={() => void handlePublishSkillToHub()}>
                {t("skillHubPage.publish")}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
};

export default InstanceSkillHubPanel;
