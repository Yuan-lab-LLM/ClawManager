import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import OpenClawConfigPlanSection, {
  type OpenClawInjectionMode,
} from "../../components/OpenClawConfigPlanSection";
import UserLayout from "../../components/UserLayout";
import {
  systemSettingsService,
  type SystemImageSetting,
} from "../../services/systemSettingsService";
import { teamService } from "../../services/teamService";
import {
  buildAgencyAgentEnvironment,
  getAgencyAgentProfile,
} from "../../lib/agencyAgentProfiles";
import {
  BUILTIN_MEMBER_TEMPLATES,
  getTeamMemberDisplayDescription,
  type ResourcePresetKey,
  type RuntimeType,
  type TeamMemberTemplate,
  type TeamMemberTemplateMember,
} from "../../lib/teamTemplates";
import type { CreateTeamRequest, TeamCommunicationMode } from "../../types/team";
import type { InstanceMode } from "../../types/instance";
import type {
  OpenClawConfigCompilePreview,
  OpenClawConfigPlan,
} from "../../types/openclawConfig";

type EnvironmentRow = {
  id: string;
  name: string;
  value: string;
};

type TeamMemberDraft = TeamMemberTemplateMember & {
  id: string;
  instanceMode: InstanceMode;
};

const TEAM_COMMUNICATION_MODE_OPTIONS: Array<{
  value: TeamCommunicationMode;
  label: string;
  description: string;
}> = [
  {
    value: "leader_mediated",
    label: "Leader 中介协作",
    description: "任务统一进入 Leader，由 Leader 拆解、派发、回收并汇总结果。",
  },
];

const RESOURCE_PRESETS: Record<
  Exclude<ResourcePresetKey, "custom">,
  { label: string; cpuCores: number; memoryGb: number; diskGb: number }
> = {
  small: { label: "小", cpuCores: 2, memoryGb: 4, diskGb: 20 },
  medium: { label: "中", cpuCores: 4, memoryGb: 8, diskGb: 50 },
  large: { label: "大", cpuCores: 8, memoryGb: 16, diskGb: 100 },
};

const ENV_NAME_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;
const FIXED_RUNTIME_TYPE: RuntimeType = "openclaw";
const FIXED_INSTANCE_MODE: InstanceMode = "lite";
const FIXED_COMMUNICATION_MODE: TeamCommunicationMode = "leader_mediated";
const TEAM_TEMPLATE_DISPLAY_COPY: Record<
  string,
  { name: string; description: string }
> = {
  "builtin-leader-worker": {
    name: "标准双成员团队",
    description:
      "Leader 负责任务拆解、成员协调和结果整合，Worker 执行具体任务并回报进度。",
  },
  "builtin-dev-qa-docs": {
    name: "交付三成员团队",
    description:
      "Leader 负责拆解和协调，Developer 负责实现与集成，Reviewer 负责验证测试、回归风险和交付质量。",
  },
  "builtin-software-engineering-team": {
    name: "软件工程八成员团队",
    description:
      "Leader 统筹目标、协作和最终集成，PM、UI/UX、Frontend、Backend、Architect、QA、Code Reviewer 分别覆盖产品、设计、客户端、服务端、架构、验证和代码审查。",
  },
};

const getTemplateDisplayName = (template: TeamMemberTemplate) =>
  TEAM_TEMPLATE_DISPLAY_COPY[template.id]?.name || template.name;

const getTemplateDisplayDescription = (template: TeamMemberTemplate) =>
  TEAM_TEMPLATE_DISPLAY_COPY[template.id]?.description ||
  template.description ||
  "模板未提供说明。";

const SORTED_BUILTIN_MEMBER_TEMPLATES = [...BUILTIN_MEMBER_TEMPLATES].sort(
  (left, right) =>
    left.members.length - right.members.length ||
    getTemplateDisplayName(left).localeCompare(getTemplateDisplayName(right), "zh-Hans"),
);

const newDraftId = () =>
  `member-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;

const defaultMember = (
  overrides?: Partial<TeamMemberDraft>,
): TeamMemberDraft => ({
  id: newDraftId(),
  memberId: "worker-1",
  name: "",
  role: "developer",
  runtimeType: "openclaw",
  instanceMode: "lite",
  description: "",
  resourcePreset: "small",
  isLeader: false,
  cpuCores: 2,
  memoryGb: 4,
  diskGb: 20,
  gpuEnabled: false,
  gpuCount: 0,
  image: "",
  ...overrides,
});

const normalizedPreset = (
  preset?: ResourcePresetKey,
): Exclude<ResourcePresetKey, "custom"> =>
  preset && preset !== "custom" ? preset : "small";

const draftFromTemplateMember = (
  templateMember: TeamMemberTemplateMember,
  usedIds: Set<string>,
  index: number,
  isLeader: boolean,
  image: string,
): TeamMemberDraft => {
  const preset = normalizedPreset(templateMember.resourcePreset);
  const config = RESOURCE_PRESETS[preset];
  const templateRole =
    templateMember.role && templateMember.role !== "leader"
      ? templateMember.role
      : "member";

  return defaultMember({
    ...templateMember,
    memberId: uniqueMemberId(templateMember.memberId, usedIds, index),
    role: isLeader ? "leader" : templateRole,
    runtimeType: FIXED_RUNTIME_TYPE,
    instanceMode: FIXED_INSTANCE_MODE,
    image,
    resourcePreset: preset,
    isLeader,
    cpuCores: config.cpuCores,
    memoryGb: config.memoryGb,
    diskGb: config.diskGb,
    gpuEnabled: false,
    gpuCount: 0,
  });
};

const buildTemplateMembers = (template: TeamMemberTemplate, image: string) => {
  const usedIds = new Set<string>();
  const importedMembers: TeamMemberDraft[] = [];
  let leaderAssigned = false;

  template.members.forEach((templateMember, index) => {
    const shouldBeLeader = Boolean(templateMember.isLeader) && !leaderAssigned;
    if (shouldBeLeader) {
      leaderAssigned = true;
    }
    importedMembers.push(
      draftFromTemplateMember(templateMember, usedIds, index + 1, shouldBeLeader, image),
    );
  });

  if (!leaderAssigned && importedMembers.length > 0) {
    importedMembers[0] = {
      ...importedMembers[0],
      isLeader: true,
      role: "leader",
    };
  }

  return importedMembers;
};

const imageRuntimeTypeForMode = (
  mode: InstanceMode,
): NonNullable<SystemImageSetting["runtime_type"]> =>
  mode === "lite" ? "gateway" : "desktop";

const normalizedImageRuntimeType = (
  item: SystemImageSetting,
): NonNullable<SystemImageSetting["runtime_type"]> =>
  item.runtime_type === "gateway" ? "gateway" : "desktop";

const normalizeMemberId = (value: string) =>
  value
    .trim()
    .toLowerCase()
    .replace(/[_\s]+/g, "-")
    .replace(/[^a-z0-9-]/g, "")
    .slice(0, 63);

const uniqueMemberId = (raw: string, usedIds: Set<string>, fallbackIndex: number) => {
  const fallback = `member-${fallbackIndex}`;
  const base = normalizeMemberId(raw) || fallback;
  let candidate = base;
  let suffix = 2;

  while (usedIds.has(candidate)) {
    const suffixText = `-${suffix}`;
    candidate = `${base.slice(0, 63 - suffixText.length)}${suffixText}`;
    suffix += 1;
  }

  usedIds.add(candidate);
  return candidate;
};

const CreateTeamPage: React.FC = () => {
  const navigate = useNavigate();
  const initialTemplate = SORTED_BUILTIN_MEMBER_TEMPLATES[0];
  const [name, setName] = useState(initialTemplate?.teamName || "");
  const [sharedStorageGb, setSharedStorageGb] = useState(10);
  const [images, setImages] = useState<SystemImageSetting[]>([]);
  const [selectedTemplateId, setSelectedTemplateId] = useState(
    initialTemplate?.id || "",
  );
  const [members, setMembers] = useState<TeamMemberDraft[]>(() =>
    initialTemplate ? buildTemplateMembers(initialTemplate, "") : [],
  );
  const [loadingImages, setLoadingImages] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [environmentRows, setEnvironmentRows] = useState<EnvironmentRow[]>([]);
  const [openClawInjectionMode, setOpenClawInjectionMode] =
    useState<OpenClawInjectionMode>("none");
  const [openClawBundleId, setOpenClawBundleId] = useState<number | undefined>();
  const [openClawResourceIds, setOpenClawResourceIds] = useState<number[]>([]);
  const [openClawPreview, setOpenClawPreview] =
    useState<OpenClawConfigCompilePreview | null>(null);
  const [openClawPreviewLoading, setOpenClawPreviewLoading] = useState(false);
  const [openClawPreviewError, setOpenClawPreviewError] = useState<string | null>(
    null,
  );

  const memberTemplates = SORTED_BUILTIN_MEMBER_TEMPLATES;
  const selectedTemplate = useMemo(
    () =>
      memberTemplates.find((template) => template.id === selectedTemplateId) ||
      memberTemplates[0],
    [memberTemplates, selectedTemplateId],
  );
  const defaultOpenClawMemberImages = useMemo(
    () =>
      images.filter(
        (item) =>
          item.instance_type === "openclaw" &&
          item.is_enabled !== false &&
          normalizedImageRuntimeType(item) === imageRuntimeTypeForMode("lite"),
      ),
    [images],
  );
  const selectedImage = defaultOpenClawMemberImages[0];
  const openClawLiteImage = selectedImage?.image || "";

  useEffect(() => {
    const loadImages = async () => {
      try {
        setLoadingImages(true);
        const items = await systemSettingsService.getImageSettings();
        setImages(items);
      } catch {
        setImages([]);
      } finally {
        setLoadingImages(false);
      }
    };
    void loadImages();
  }, []);

  const applyTemplate = (templateId: string) => {
    const template =
      memberTemplates.find((item) => item.id === templateId) || memberTemplates[0];
    if (!template) {
      return;
    }
    setSelectedTemplateId(template.id);
    setName(template.teamName || "");
    setMembers(buildTemplateMembers(template, openClawLiteImage));
    setError(null);
  };

  const addEnvironmentRow = () => {
    setEnvironmentRows((current) => [
      ...current,
      {
        id: `env-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`,
        name: "",
        value: "",
      },
    ]);
  };

  const updateEnvironmentRow = (
    id: string,
    patch: Partial<Omit<EnvironmentRow, "id">>,
  ) => {
    setEnvironmentRows((current) =>
      current.map((row) => (row.id === id ? { ...row, ...patch } : row)),
    );
  };

  const removeEnvironmentRow = (id: string) => {
    setEnvironmentRows((current) => current.filter((row) => row.id !== id));
  };

  const buildEnvironmentOverridesPayload = () => {
    const overrides: Record<string, string> = {};
    const seenNames = new Set<string>();

    for (const row of environmentRows) {
      const envName = row.name.trim();
      const hasName = envName.length > 0;
      const hasValue = row.value.length > 0;

      if (!hasName && !hasValue) {
        continue;
      }
      if (!hasName) {
        return { error: "环境变量名称不能为空" };
      }
      if (!ENV_NAME_PATTERN.test(envName)) {
        return { error: `环境变量名称无效：${envName}` };
      }
      if (seenNames.has(envName)) {
        return { error: `环境变量名称重复：${envName}` };
      }
      seenNames.add(envName);
      overrides[envName] = row.value;
    }

    return {
      overrides: Object.keys(overrides).length > 0 ? overrides : undefined,
    };
  };

  const buildOpenClawConfigPlan = (): OpenClawConfigPlan | undefined => {
    if (openClawInjectionMode === "bundle" && openClawBundleId) {
      return { mode: "bundle", bundle_id: openClawBundleId };
    }
    if (openClawInjectionMode === "manual" && openClawResourceIds.length > 0) {
      return { mode: "manual", resource_ids: openClawResourceIds };
    }
    return undefined;
  };

  const profileForMember = (member: TeamMemberDraft) =>
    getAgencyAgentProfile(member.agentProfileKey);

  const effectiveMemberRole = (member: TeamMemberDraft) => {
    if (member.isLeader) {
      return "leader";
    }
    const profile = profileForMember(member);
    if (profile?.roleHint && profile.roleHint !== "leader") {
      return profile.roleHint;
    }
    return member.role.trim() || "member";
  };

  const effectiveMemberDescription = (member: TeamMemberDraft) => {
    const explicit = member.description.trim();
    if (explicit) {
      return explicit;
    }
    return profileForMember(member)?.summary || "";
  };

  const displayNameForMember = (member: TeamMemberDraft) => {
    const normalizedMemberId = normalizeMemberId(member.memberId);
    return member.name.trim() || `${name.trim() || "team"}-${normalizedMemberId || member.memberId}`;
  };

  const profileLabelForMember = (member: TeamMemberDraft) => {
    const profile = profileForMember(member);
    return profile?.displayName || profile?.name || member.agentProfileKey || "未指定";
  };

  const buildMemberEnvironmentOverrides = (
    member: TeamMemberDraft,
    normalizedMemberId: string,
  ): Record<string, string> | undefined => {
    const profile = profileForMember(member);
    const profileEnv = buildAgencyAgentEnvironment(profile, {
      memberId: normalizedMemberId,
      displayName: member.name.trim() || normalizedMemberId,
      role: effectiveMemberRole(member),
      runtimeType: FIXED_RUNTIME_TYPE,
      isLeader: member.isLeader,
    });
    const merged = {
      ...(environmentDraft.overrides || {}),
      ...(profileEnv || {}),
    };
    return Object.keys(merged).length > 0 ? merged : undefined;
  };

  const handleOpenClawPreviewChange = useCallback(
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

  const validationError = useMemo(() => {
    if (!name.trim()) {
      return "Team 名称不能为空";
    }
    if (!openClawLiteImage) {
      return loadingImages
        ? "正在加载 OpenClaw Lite 镜像"
        : "没有可用的 OpenClaw Lite 镜像";
    }
    if (members.length === 0) {
      return "至少需要一个成员";
    }
    if (members.filter((member) => member.isLeader).length !== 1) {
      return "必须指定且只能指定一个 Leader";
    }
    const memberIds = new Set<string>();
    for (const member of members) {
      const memberId = normalizeMemberId(member.memberId);
      if (!memberId) {
        return "成员 ID 不能为空";
      }
      if (memberIds.has(memberId)) {
        return `成员 ID 重复：${memberId}`;
      }
      memberIds.add(memberId);
      if (member.cpuCores <= 0 || member.memoryGb <= 0 || member.diskGb <= 0) {
        return `成员 ${memberId} 的资源规格无效`;
      }
    }
    return null;
  }, [loadingImages, members, name, openClawLiteImage]);

  const environmentDraft = buildEnvironmentOverridesPayload();
  const openClawPlanInvalid =
    (openClawInjectionMode === "bundle" &&
      (!openClawBundleId || Boolean(openClawPreviewError) || openClawPreviewLoading)) ||
    (openClawInjectionMode === "manual" &&
      (Boolean(openClawPreviewError) || openClawPreviewLoading)) ||
    openClawInjectionMode === "archive";
  const environmentOverrideNames = environmentDraft.overrides
    ? Object.keys(environmentDraft.overrides)
    : [];
  const resolvedChannelNames = (openClawPreview?.resolved_resources || [])
    .filter((resource) => resource.resource_type === "channel")
    .map((resource) => resource.name);
  const communicationModeOption =
    TEAM_COMMUNICATION_MODE_OPTIONS.find(
      (option) => option.value === FIXED_COMMUNICATION_MODE,
    ) || TEAM_COMMUNICATION_MODE_OPTIONS[0];

  const submitDisabled =
    submitting ||
    Boolean(validationError) ||
    Boolean(environmentDraft.error) ||
    openClawPlanInvalid;

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (validationError) {
      setError(validationError);
      return;
    }
    if (environmentDraft.error) {
      setError(environmentDraft.error);
      return;
    }
    if (openClawPlanInvalid) {
      setError(
        openClawInjectionMode === "archive"
          ? "Team 创建暂不支持 Archive 导入，请选择手动/Bundle 或关闭注入。"
          : openClawPreviewError || "OpenClaw 注入配置尚未就绪",
      );
      return;
    }
    const openClawConfigPlan = buildOpenClawConfigPlan();
    const payload: CreateTeamRequest = {
      name: name.trim(),
      communication_mode: FIXED_COMMUNICATION_MODE,
      shared_storage_gb: sharedStorageGb,
      members: members.map((member) => {
        const normalizedMemberId = normalizeMemberId(member.memberId);
        const memberDescription = effectiveMemberDescription(member);
        return {
          member_id: normalizedMemberId,
          name: displayNameForMember(member),
          role: effectiveMemberRole(member),
          mode: FIXED_INSTANCE_MODE,
          instance_mode: FIXED_INSTANCE_MODE,
          runtime_type: FIXED_RUNTIME_TYPE,
          description: memberDescription || undefined,
          is_leader: member.isLeader,
          cpu_cores: member.cpuCores,
          memory_gb: member.memoryGb,
          disk_gb: member.diskGb,
          gpu_enabled: false,
          gpu_count: 0,
          image_registry: openClawLiteImage,
          environment_overrides: buildMemberEnvironmentOverrides(
            member,
            normalizedMemberId,
          ),
          openclaw_config_plan: openClawConfigPlan,
        };
      }),
    };

    try {
      setSubmitting(true);
      setError(null);
      const created = await teamService.createTeam(payload);
      navigate(`/teams/${created.team.id}`);
    } catch (err: unknown) {
      const apiError = err as { response?: { data?: { error?: string } } };
      setError(apiError.response?.data?.error || "创建 Team 失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <UserLayout title="创建 Team">
      <form onSubmit={handleSubmit} className="space-y-6">
        {error && (
          <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
            {error}
          </div>
        )}

        <div className="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
          <div className="space-y-6">
            <section className="app-panel p-6">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div>
                  <h2 className="text-lg font-semibold text-gray-900">
                    成员公共注入
                  </h2>
                  <p className="mt-1 text-sm text-gray-500">
                    这些环境变量和 OpenClaw 配置会应用到本次创建的所有 Team 成员。
                  </p>
                </div>
                <span className="rounded-full bg-indigo-50 px-3 py-1 text-xs font-medium text-indigo-600">
                  可选
                </span>
              </div>

              <div className="mt-5 border-t border-[#f1e5df] pt-5">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <h3 className="text-sm font-semibold text-gray-900">
                      环境变量
                    </h3>
                    <p className="mt-1 text-sm text-gray-500">
                      用于成员 Pod 的额外运行参数，敏感值仍建议走后端 Secret/受控配置。
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={addEnvironmentRow}
                    className="inline-flex items-center justify-center rounded-xl border border-[#eadfd8] bg-white px-3 py-2 text-sm font-medium text-[#5f5957] hover:bg-[#fff8f5]"
                  >
                    添加变量
                  </button>
                </div>

                <div className="mt-4 space-y-3">
                  {environmentRows.length === 0 ? (
                    <div className="rounded-xl border border-dashed border-[#eadfd8] bg-white px-4 py-3 text-sm text-gray-500">
                      暂无额外环境变量。
                    </div>
                  ) : (
                    environmentRows.map((row) => (
                      <div
                        key={row.id}
                        className="grid grid-cols-1 gap-3 md:grid-cols-[minmax(0,220px)_minmax(0,1fr)_auto]"
                      >
                        <input
                          value={row.name}
                          onChange={(event) =>
                            updateEnvironmentRow(row.id, {
                              name: event.target.value,
                            })
                          }
                          placeholder="ENV_NAME"
                          className="rounded-xl border border-[#eadfd8] px-3 py-2 text-sm focus:border-[#ef4444] focus:outline-none focus:ring-1 focus:ring-[#f3d2c2]"
                        />
                        <input
                          value={row.value}
                          onChange={(event) =>
                            updateEnvironmentRow(row.id, {
                              value: event.target.value,
                            })
                          }
                          placeholder="value"
                          className="rounded-xl border border-[#eadfd8] px-3 py-2 text-sm focus:border-[#ef4444] focus:outline-none focus:ring-1 focus:ring-[#f3d2c2]"
                        />
                        <button
                          type="button"
                          onClick={() => removeEnvironmentRow(row.id)}
                          className="rounded-xl border border-red-200 bg-red-50 px-3 py-2 text-sm font-medium text-red-700 hover:bg-red-100"
                        >
                          删除
                        </button>
                      </div>
                    ))
                  )}
                  {environmentDraft.error && (
                    <p className="text-sm text-red-600">
                      {environmentDraft.error}
                    </p>
                  )}
                </div>
              </div>

              <div className="mt-6 border-t border-[#f1e5df] pt-5">
                <h3 className="text-sm font-semibold text-gray-900">
                  OpenClaw 配置
                </h3>
                <div className="mt-4">
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
                      }
                    }}
                    onSelectionChange={({ bundleId, resourceIds }) => {
                      setOpenClawBundleId(bundleId);
                      setOpenClawResourceIds(resourceIds);
                    }}
                    onPreviewChange={handleOpenClawPreviewChange}
                  />
                </div>

                {openClawInjectionMode === "archive" && (
                  <p className="mt-3 rounded-lg border border-yellow-200 bg-yellow-50 px-3 py-2 text-sm text-yellow-700">
                    Team 创建暂不支持 Archive 导入。请使用 Bundle、手动资源选择，或关闭注入。
                  </p>
                )}
                {openClawPreviewLoading && (
                  <p className="mt-3 text-sm text-gray-500">正在检查注入配置...</p>
                )}
                {openClawPreviewError && (
                  <p className="mt-3 text-sm text-red-600">
                    {openClawPreviewError}
                  </p>
                )}
              </div>
            </section>

            <section className="app-panel p-6">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div>
                  <h2 className="text-lg font-semibold text-gray-900">
                    Team 与成员
                  </h2>
                  <p className="mt-1 text-sm text-gray-500">
                    当前 {members.length} 个成员，{members.filter((member) => member.isLeader).length} 个 Leader
                  </p>
                </div>
                <span className="rounded-full bg-emerald-50 px-3 py-1 text-xs font-medium text-emerald-700">
                  OpenClaw Lite
                </span>
              </div>

              <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-2">
                <label className="block">
                  <span className="text-sm font-medium text-gray-700">
                    Team 名称
                  </span>
                  <input
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder="research-team"
                    className="mt-1 block w-full rounded-xl border border-[#eadfd8] px-3 py-2 text-sm focus:border-[#ef4444] focus:outline-none focus:ring-1 focus:ring-[#f3d2c2]"
                  />
                </label>
                <label className="block">
                  <span className="text-sm font-medium text-gray-700">
                    共享存储
                  </span>
                  <div className="mt-1 flex rounded-xl border border-[#eadfd8] bg-white">
                    <input
                      type="number"
                      min={1}
                      value={sharedStorageGb}
                      onChange={(event) =>
                        setSharedStorageGb(Math.max(1, Number(event.target.value)))
                      }
                      className="min-w-0 flex-1 rounded-l-xl border-0 px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-[#f3d2c2]"
                    />
                    <span className="flex items-center rounded-r-xl border-l border-[#eadfd8] bg-gray-50 px-3 text-sm text-gray-500">
                      GiB
                    </span>
                  </div>
                </label>
                <label className="block md:col-span-2">
                  <span className="text-sm font-medium text-gray-700">
                    协作模式
                  </span>
                  <div className="mt-1 rounded-xl border border-[#eadfd8] bg-gray-50 px-3 py-2 text-sm text-gray-700">
                    {communicationModeOption.label}
                  </div>
                  <p className="mt-1 text-xs text-gray-500">
                    {communicationModeOption.description}
                  </p>
                </label>
                <label className="block md:col-span-2">
                  <span className="text-sm font-medium text-gray-700">
                    选择模板包
                  </span>
                  <select
                    value={selectedTemplate?.id || ""}
                    onChange={(event) => applyTemplate(event.target.value)}
                    className="mt-1 block w-full rounded-xl border border-[#eadfd8] bg-white px-3 py-2 text-sm focus:border-[#ef4444] focus:outline-none focus:ring-1 focus:ring-[#f3d2c2]"
                  >
                    {memberTemplates.map((template) => (
                      <option key={template.id} value={template.id}>
                        {getTemplateDisplayName(template)} · {template.members.length} 成员
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              {selectedTemplate && (
                <div className="mt-4 rounded-xl border border-[#eadfd8] bg-[#fffaf6] px-4 py-3 text-sm">
                  <div className="font-medium text-gray-900">
                    {getTemplateDisplayName(selectedTemplate)}
                  </div>
                  <div className="mt-1 text-gray-500">
                    {getTemplateDisplayDescription(selectedTemplate)}
                  </div>
                </div>
              )}

              <div className="mt-5 overflow-hidden rounded-xl border border-[#eadfd8]">
                <div className="hidden grid-cols-[minmax(120px,0.9fr)_minmax(140px,1fr)_minmax(150px,1fr)_minmax(280px,1.8fr)] gap-3 border-b border-[#eadfd8] bg-gray-50 px-4 py-3 text-xs font-semibold uppercase tracking-wide text-gray-500 lg:grid">
                  <div>成员 ID</div>
                  <div>显示名称</div>
                  <div>角色模板</div>
                  <div>角色解释</div>
                </div>
                <div className="divide-y divide-[#eadfd8] bg-white">
                  {members.map((member) => (
                    <div
                      key={member.id}
                      className="grid grid-cols-1 gap-3 px-4 py-4 text-sm lg:grid-cols-[minmax(120px,0.9fr)_minmax(140px,1fr)_minmax(150px,1fr)_minmax(280px,1.8fr)] lg:items-center"
                    >
                      <div>
                        <div className="text-xs font-medium text-gray-500 lg:hidden">
                          成员 ID
                        </div>
                        <div className="font-medium text-gray-900">
                          {normalizeMemberId(member.memberId) || member.memberId}
                        </div>
                        <div className="mt-1 text-xs text-gray-500">
                          {member.isLeader ? "Leader" : effectiveMemberRole(member)}
                        </div>
                      </div>
                      <div>
                        <div className="text-xs font-medium text-gray-500 lg:hidden">
                          显示名称
                        </div>
                        <div className="text-gray-900">
                          {displayNameForMember(member)}
                        </div>
                      </div>
                      <div>
                        <div className="text-xs font-medium text-gray-500 lg:hidden">
                          角色模板
                        </div>
                        <div className="font-medium text-indigo-700">
                          {profileLabelForMember(member)}
                        </div>
                      </div>
                      <div>
                        <div className="text-xs font-medium text-gray-500 lg:hidden">
                          角色解释
                        </div>
                        <p className="line-clamp-3 text-gray-600">
                          {getTeamMemberDisplayDescription(
                            effectiveMemberDescription(member),
                          ) || "模板未提供职责说明。"}
                        </p>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </section>
          </div>

          <aside className="space-y-6 xl:sticky xl:top-24 xl:self-start">
            <section className="app-panel p-6">
              <h2 className="text-lg font-semibold text-gray-900">摘要</h2>
              <dl className="mt-5 space-y-4 text-sm">
                <SummaryRow label="Team" value={name || "未命名"} />
                <SummaryRow
                  label="Leader"
                  value={
                    members.find((member) => member.isLeader)?.memberId ||
                    "未指定"
                  }
                />
                <SummaryRow label="成员数" value={`${members.length}`} />
                <SummaryRow
                  label="成员模板"
                  value={
                    selectedTemplate
                      ? getTemplateDisplayName(selectedTemplate)
                      : "未选择"
                  }
                />
                <SummaryRow label="运行方式" value="OpenClaw Lite" />
                <SummaryRow
                  label="协作模式"
                  value={communicationModeOption.label}
                />
                <SummaryRow
                  label="共享存储"
                  value={`${sharedStorageGb || 0} GiB`}
                />
                <SummaryRow
                  label="默认镜像"
                  value={selectedImage?.display_name || selectedImage?.image || "无"}
                />
                <SummaryRow
                  label="环境变量"
                  value={
                    environmentOverrideNames.length > 0
                      ? environmentOverrideNames.join(", ")
                      : "无"
                  }
                />
                <SummaryRow
                  label="OpenClaw 注入"
                  value={
                    openClawInjectionMode === "bundle"
                      ? openClawBundleId
                        ? `Bundle #${openClawBundleId}`
                        : "Bundle 未选择"
                      : openClawInjectionMode === "manual"
                        ? resolvedChannelNames.length > 0
                          ? resolvedChannelNames.join(", ")
                          : `${openClawResourceIds.length} 个资源`
                        : openClawInjectionMode === "archive"
                          ? "Archive 暂不支持"
                          : "关闭"
                  }
                />
              </dl>
              {(validationError || environmentDraft.error) && (
                <p className="mt-5 rounded-lg border border-yellow-200 bg-yellow-50 px-3 py-2 text-sm text-yellow-700">
                  {validationError || environmentDraft.error}
                </p>
              )}
            </section>

            <div className="flex gap-3">
              <button
                type="button"
                onClick={() => navigate("/teams")}
                className="app-button-secondary flex-1"
              >
                取消
              </button>
              <button
                type="submit"
                disabled={submitDisabled}
                className="app-button-primary flex-1 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {submitting ? "创建中..." : "创建"}
              </button>
            </div>
          </aside>
        </div>
      </form>
    </UserLayout>
  );
};

function SummaryRow({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-gray-500">{label}</dt>
      <dd className="mt-1 break-words font-medium text-gray-900">{value}</dd>
    </div>
  );
}

export default CreateTeamPage;
