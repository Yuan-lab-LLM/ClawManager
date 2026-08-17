import React, { useEffect, useMemo, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  Save,
  Sparkles,
  Trash2,
  Users,
} from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import UserLayout from "../../components/UserLayout";
import { customTeamTemplateService } from "../../services/customTeamTemplateService";
import type {
  CustomTeamMemberSpec,
  CustomTeamTemplate,
} from "../../types/customTeamTemplate";

const MEMBER_COUNTS = [2, 3, 4, 5, 6];

const errorMessage = (error: unknown) => {
  const apiError = error as { response?: { data?: { error?: string } } };
  const message = apiError.response?.data?.error || "";
  if (
    message.includes("requires an active AI model") ||
    message.includes("no active models are configured")
  ) {
    return "尚未配置可用的大模型，请先在模型配置中启用一个模型后再生成或调整自定义 Team。";
  }
  if (message.includes("revision conflict")) {
    return "这个模板刚刚发生了变化，请重新选择后再操作。";
  }
  return message || "操作失败，请稍后重试";
};

const replaceTemplate = (
  items: CustomTeamTemplate[],
  next: CustomTeamTemplate,
) => items.map((item) => (item.id === next.id ? next : item));

const CustomTeamTemplatesPage: React.FC = () => {
  const navigate = useNavigate();
  const [templates, setTemplates] = useState<CustomTeamTemplate[]>([]);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [intent, setIntent] = useState("");
  const [memberCount, setMemberCount] = useState("");
  const [nameDraft, setNameDraft] = useState("");
  const [intentDraft, setIntentDraft] = useState("");
  const [memberCountDraft, setMemberCountDraft] = useState("");
  const [adjustments, setAdjustments] = useState<Record<string, string>>({});
  const [adjustmentErrors, setAdjustmentErrors] = useState<Record<string, string>>({});
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const selected = useMemo(
    () => templates.find((item) => item.id === selectedId) || null,
    [selectedId, templates],
  );

  const syncDrafts = (template: CustomTeamTemplate | null) => {
    setNameDraft(template?.name || "");
    setIntentDraft(template?.intent || "");
    setMemberCountDraft(
      template?.requested_member_count
        ? String(template.requested_member_count)
        : "",
    );
  };

  useEffect(() => {
    const load = async () => {
      try {
        const items = await customTeamTemplateService.list();
        setTemplates(items);
        setSelectedId(items[0]?.id ?? null);
        syncDrafts(items[0] || null);
      } catch (err) {
        setError(errorMessage(err));
      } finally {
        setLoading(false);
      }
    };
    void load();
  }, []);

  const applyUpdated = (
    next: CustomTeamTemplate,
    options: { syncEditor?: boolean } = { syncEditor: true },
  ) => {
    setTemplates((current) => replaceTemplate(current, next));
    setSelectedId(next.id);
    if (options.syncEditor !== false) syncDrafts(next);
  };

  const selectTemplate = (template: CustomTeamTemplate) => {
    setSelectedId(template.id);
    syncDrafts(template);
    setAdjustments({});
    setAdjustmentErrors({});
    setExpanded({});
    setError(null);
  };

  const generate = async () => {
    if (!intent.trim()) {
      setError("请先描述这个 Team 要完成什么");
      return;
    }
    try {
      setBusy("generate");
      setError(null);
      const created = await customTeamTemplateService.generate(
        intent.trim(),
        memberCount ? Number(memberCount) : undefined,
      );
      setTemplates((current) => [created, ...current]);
      setSelectedId(created.id);
      syncDrafts(created);
      setAdjustments({});
      setAdjustmentErrors({});
      setExpanded({});
      setIntent("");
      setMemberCount("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };

  const rename = async () => {
    if (!selected || !nameDraft.trim()) return;
    try {
      setBusy("rename");
      setError(null);
      applyUpdated(
        await customTeamTemplateService.rename(
          selected.id,
          nameDraft.trim(),
          selected.revision,
        ),
      );
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };

  const reviseTeam = async () => {
    if (!selected) return;
    if (!nameDraft.trim()) {
      setError("模板名称不能为空");
      return;
    }
    if (!intentDraft.trim()) {
      setError("请先描述这个 Team 要完成什么");
      return;
    }
    try {
      setBusy("revise-team");
      setError(null);
      applyUpdated(
        await customTeamTemplateService.revise(
          selected.id,
          nameDraft.trim(),
          intentDraft.trim(),
          memberCountDraft ? Number(memberCountDraft) : undefined,
          selected.revision,
        ),
      );
      setAdjustments({});
      setAdjustmentErrors({});
      setExpanded({});
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };

  const adjustMember = async (member: CustomTeamMemberSpec) => {
    if (!selected) return;
    const instruction = adjustments[member.memberId]?.trim();
    if (!instruction) {
      setError(null);
      setAdjustmentErrors((current) => ({
        ...current,
        [member.memberId]: member.isLeader
          ? "请先输入希望如何调整 Leader 的延展职责"
          : "请先输入希望如何调整这个 Worker",
      }));
      return;
    }
    try {
      setBusy(`adjust:${member.memberId}`);
      setError(null);
      setAdjustmentErrors((current) => {
        const next = { ...current };
        delete next[member.memberId];
        return next;
      });
      applyUpdated(
        await customTeamTemplateService.adjustMember(
          selected.id,
          member.memberId,
          instruction,
          selected.revision,
        ),
        { syncEditor: false },
      );
      setAdjustments((current) => ({ ...current, [member.memberId]: "" }));
      setExpanded((current) => ({ ...current, [member.memberId]: true }));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };

  const regenerateAll = async () => {
    if (!selected) return;
    try {
      setBusy("regenerate-all");
      setError(null);
      applyUpdated(
        await customTeamTemplateService.regenerate(
          selected.id,
          selected.revision,
        ),
      );
      setAdjustments({});
      setAdjustmentErrors({});
      setExpanded({});
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };

  const remove = async () => {
    if (!selected || !window.confirm(`确定删除自定义模板“${selected.name}”吗？`)) return;
    try {
      setBusy("delete");
      setError(null);
      await customTeamTemplateService.delete(selected.id);
      const remaining = templates.filter((item) => item.id !== selected.id);
      setTemplates(remaining);
      setSelectedId(remaining[0]?.id ?? null);
      syncDrafts(remaining[0] || null);
      setAdjustments({});
      setAdjustmentErrors({});
      setExpanded({});
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };

  const editorChanged = Boolean(
    selected &&
      (nameDraft.trim() !== selected.name ||
        intentDraft.trim() !== selected.intent ||
        memberCountDraft !==
          (selected.requested_member_count
            ? String(selected.requested_member_count)
            : "")),
  );

  return (
    <UserLayout title="自定义 Team">
      <div className="space-y-5 pb-10">
        <button
          type="button"
          onClick={() => navigate("/teams/new")}
          className="app-button-secondary inline-flex items-center gap-2"
        >
          <ArrowLeft size={16} /> 返回创建 Team
        </button>

        {error && (
          <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
            {error}
          </div>
        )}

        <section className="app-panel overflow-hidden">
          <div className="border-b border-[#f1e5df] bg-gradient-to-r from-indigo-50/80 to-white px-6 py-5">
            <div className="flex items-start gap-3">
              <div className="rounded-xl bg-white p-2 text-indigo-600 shadow-sm">
                <Sparkles size={20} />
              </div>
              <div>
                <h2 className="text-lg font-semibold text-gray-900">生成新的自定义 Team</h2>
                <p className="mt-1 text-sm text-gray-500">
                  描述目标即可。首位成员固定为 Leader；人数留空时由系统决定，总人数最多 6 人。
                </p>
              </div>
            </div>
          </div>
          <div className="grid gap-4 p-6 lg:grid-cols-[minmax(0,1fr)_190px_auto] lg:items-end">
            <label className="block">
              <span className="text-sm font-medium text-gray-700">Team 意图</span>
              <textarea
                value={intent}
                onChange={(event) => setIntent(event.target.value)}
                rows={3}
                placeholder="例如：持续跟踪竞品、分析用户反馈，并每周输出产品决策建议"
                className="mt-1 block w-full rounded-xl border border-[#eadfd8] px-3 py-2 text-sm focus:border-indigo-400 focus:outline-none focus:ring-1 focus:ring-indigo-200"
              />
            </label>
            <MemberCountSelect value={memberCount} onChange={setMemberCount} />
            <button
              type="button"
              disabled={Boolean(busy)}
              onClick={() => void generate()}
              className="app-button-primary inline-flex items-center justify-center gap-2 disabled:opacity-50"
            >
              <Sparkles size={16} /> {busy === "generate" ? "生成中..." : "生成 Team"}
            </button>
          </div>
        </section>

        <div className="grid items-start gap-5 xl:grid-cols-[280px_minmax(0,1fr)]">
          <aside className="app-panel p-3 xl:sticky xl:top-4">
            <div className="flex items-center justify-between px-2 py-2">
              <div>
                <h2 className="font-semibold text-gray-900">我的自定义 Team</h2>
                <p className="mt-0.5 text-xs text-gray-500">选择一个模板继续编辑</p>
              </div>
              <span className="rounded-full bg-gray-100 px-2 py-1 text-xs text-gray-600">
                {templates.length}
              </span>
            </div>
            <div className="mt-2 max-h-[calc(100vh-180px)] space-y-2 overflow-y-auto pr-1">
              {loading ? (
                <p className="px-2 py-6 text-sm text-gray-500">加载中...</p>
              ) : templates.length === 0 ? (
                <p className="rounded-xl border border-dashed border-[#eadfd8] px-3 py-8 text-center text-sm text-gray-500">
                  还没有自定义 Team
                </p>
              ) : (
                templates.map((template) => (
                  <button
                    key={template.id}
                    type="button"
                    onClick={() => selectTemplate(template)}
                    className={`w-full rounded-xl border px-3 py-3 text-left transition ${
                      selectedId === template.id
                        ? "border-indigo-300 bg-indigo-50 shadow-sm"
                        : "border-transparent bg-white hover:border-[#eadfd8] hover:bg-[#fffaf6]"
                    }`}
                  >
                    <div className="truncate font-medium text-gray-900">{template.name}</div>
                    <div className="mt-1.5 flex items-center gap-2 text-xs text-gray-500">
                      <Users size={13} /> {template.resolved_member_count} 人
                      <span className="text-gray-300">·</span> 版本 {template.revision}
                    </div>
                  </button>
                ))
              )}
            </div>
          </aside>

          <main className="min-w-0 space-y-4">
            {!selected ? (
              <section className="app-panel p-12 text-center text-sm text-gray-500">
                生成或选择一个自定义 Team 后，可在这里编辑整体目标和每个 Worker 的职责。
              </section>
            ) : (
              <>
                <section className="sticky top-3 z-20 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-indigo-100 bg-white/95 px-4 py-3 shadow-md backdrop-blur">
                  <div className="min-w-0">
                    <div className="truncate font-semibold text-gray-900">{selected.name}</div>
                    <div className="mt-0.5 text-xs text-gray-500">
                      {selected.resolved_member_count} 名成员 · 版本 {selected.revision}
                      {editorChanged ? " · 有未应用的整体修改" : ""}
                    </div>
                  </div>
                  <Link
                    to={`/teams/new?template=custom-${selected.id}`}
                    className="app-button-primary inline-flex items-center gap-2"
                  >
                    使用此模板创建 Team <ArrowRight size={16} />
                  </Link>
                </section>

                <section className="app-panel overflow-hidden">
                  <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[#f1e5df] px-6 py-5">
                    <div>
                      <h2 className="text-lg font-semibold text-gray-900">整体设置</h2>
                      <p className="mt-1 text-sm text-gray-500">
                        修改意图或人数后，模型会重新规划完整团队；Leader 始终固定在首位。
                      </p>
                    </div>
                    <button
                      type="button"
                      disabled={Boolean(busy)}
                      onClick={() => void remove()}
                      className="inline-flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium text-red-600 hover:bg-red-50 disabled:opacity-50"
                    >
                      <Trash2 size={15} /> 删除模板
                    </button>
                  </div>

                  <div className="space-y-5 p-6">
                    <label className="block max-w-2xl">
                      <span className="text-sm font-medium text-gray-700">模板名称</span>
                      <div className="mt-1 flex gap-2">
                        <input
                          value={nameDraft}
                          onChange={(event) => setNameDraft(event.target.value)}
                          className="min-w-0 flex-1 rounded-xl border border-[#eadfd8] px-3 py-2 text-sm"
                        />
                        <button
                          type="button"
                          disabled={
                            Boolean(busy) ||
                            !nameDraft.trim() ||
                            nameDraft.trim() === selected.name
                          }
                          onClick={() => void rename()}
                          className="app-button-secondary inline-flex items-center gap-2 disabled:opacity-50"
                        >
                          <Save size={15} /> 仅保存名称
                        </button>
                      </div>
                    </label>

                    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_190px]">
                      <label className="block">
                        <span className="text-sm font-medium text-gray-700">Team 意图</span>
                        <textarea
                          value={intentDraft}
                          onChange={(event) => setIntentDraft(event.target.value)}
                          rows={4}
                          placeholder="描述整个 Team 的目标、工作方式和预期交付"
                          className="mt-1 block w-full rounded-xl border border-[#eadfd8] px-3 py-2 text-sm focus:border-indigo-400 focus:outline-none focus:ring-1 focus:ring-indigo-200"
                        />
                      </label>
                      <MemberCountSelect
                        value={memberCountDraft}
                        onChange={setMemberCountDraft}
                      />
                    </div>

                    <div className="rounded-xl bg-[#faf8f6] px-4 py-3 text-sm text-gray-600">
                      <span className="font-medium text-gray-800">当前团队摘要：</span>
                      {selected.spec.summary}
                    </div>

                    <div className="flex flex-wrap items-center gap-3 border-t border-[#f1e5df] pt-5">
                      <button
                        type="button"
                        disabled={Boolean(busy)}
                        onClick={() => void reviseTeam()}
                        className="app-button-primary inline-flex items-center gap-2 disabled:opacity-50"
                      >
                        <Sparkles size={16} />
                        {busy === "revise-team" ? "整体调整中..." : "按新目标更新 Team"}
                      </button>
                      <button
                        type="button"
                        disabled={Boolean(busy) || editorChanged}
                        onClick={() => void regenerateAll()}
                        className="app-button-secondary inline-flex items-center gap-2 disabled:opacity-50"
                        title={editorChanged ? "请先应用整体修改" : undefined}
                      >
                        <RefreshCw size={15} />
                        {busy === "regenerate-all" ? "重新生成中..." : "重新生成整个 Team"}
                      </button>
                      <p className="text-xs text-gray-500">
                        使用已保存的意图和人数重建全部成员；单个成员的职责调整请在下方进行。
                      </p>
                    </div>
                  </div>
                </section>

                <section className="space-y-3">
                  <div className="flex flex-wrap items-end justify-between gap-2 px-1">
                    <div>
                      <h2 className="text-lg font-semibold text-gray-900">成员职责</h2>
                      <p className="mt-1 text-sm text-gray-500">
                        默认收起便于浏览；展开成员后可用自然语言单独细化延展职责。
                      </p>
                    </div>
                    <span className="text-sm text-gray-500">共 {selected.spec.members.length} 人</span>
                  </div>

                  {selected.spec.members.map((member, index) => {
                    const open = expanded[member.memberId] ?? false;
                    const memberBusy = busy?.endsWith(`:${member.memberId}`);
                    return (
                      <article key={member.memberId} className="app-panel overflow-hidden">
                        <button
                          type="button"
                          onClick={() =>
                            setExpanded((current) => ({
                              ...current,
                              [member.memberId]: !open,
                            }))
                          }
                          className="flex w-full items-center justify-between gap-4 px-5 py-4 text-left hover:bg-[#fffaf6]"
                        >
                          <div className="min-w-0">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-gray-100 text-xs font-semibold text-gray-600">
                                {index + 1}
                              </span>
                              <span className="font-semibold text-gray-900">{member.displayName}</span>
                              <span
                                className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                                  member.isLeader
                                    ? "bg-indigo-50 text-indigo-700"
                                    : "bg-emerald-50 text-emerald-700"
                                }`}
                              >
                                {member.isLeader ? "固定 Leader" : member.role}
                              </span>
                            </div>
                            <p className="mt-1.5 truncate text-sm text-gray-500">
                              {member.summary || member.mission}
                            </p>
                          </div>
                          {open ? <ChevronUp size={18} /> : <ChevronDown size={18} />}
                        </button>

                        {open && (
                          <div className="border-t border-[#f1e5df] bg-white px-5 py-5">
                            <div className="grid gap-3 lg:grid-cols-2">
                              <RoleSection title="使命" values={[member.mission]} />
                              <RoleSection title="核心职责" values={member.responsibilities} />
                              <RoleSection title="职责边界" values={member.boundaries} />
                              <RoleSection title="预期输入" values={member.expectedInputs} />
                              <RoleSection title="交付物" values={member.deliverables} />
                              <RoleSection title="完成标准" values={member.acceptanceCriteria} />
                              <RoleSection title="协作关系" values={member.collaborationNotes} />
                              <RoleSection
                                title="能力标签（不会自动安装技能）"
                                values={member.capabilityTags}
                              />
                            </div>

                            <div className="mt-5 rounded-xl border border-indigo-100 bg-indigo-50/40 p-4">
                              <div className="text-sm font-semibold text-gray-900">
                                {member.isLeader
                                  ? "用自然语言调整 Leader 的延展职责"
                                  : "用自然语言细化这个 Worker"}
                              </div>
                              {member.isLeader && (
                                <p className="mt-1.5 text-xs leading-5 text-indigo-700">
                                  固定 Leader 基座、成员协调关系和创建后的全员介绍流程不会被修改。
                                </p>
                              )}
                              <textarea
                                value={adjustments[member.memberId] || ""}
                                onChange={(event) => {
                                  const value = event.target.value;
                                  setAdjustments((current) => ({
                                    ...current,
                                    [member.memberId]: value,
                                  }));
                                  if (value.trim()) {
                                    setAdjustmentErrors((current) => {
                                      const next = { ...current };
                                      delete next[member.memberId];
                                      return next;
                                    });
                                  }
                                }}
                                rows={3}
                                placeholder={
                                  member.isLeader
                                    ? "例如：更关注高管决策信息；汇总时优先展示风险和可执行建议。"
                                    : "例如：更强调用户访谈和竞品分析，不负责撰写代码；输出必须附带证据来源。"
                                }
                                className="mt-3 block w-full rounded-xl border border-indigo-200 bg-white px-3 py-2 text-sm"
                              />
                              {adjustmentErrors[member.memberId] && (
                                <p className="mt-2 text-sm text-red-600" role="alert">
                                  {adjustmentErrors[member.memberId]}
                                </p>
                              )}
                              <div className="mt-3 flex flex-wrap gap-2">
                                <button
                                  type="button"
                                  disabled={Boolean(busy)}
                                  onClick={() => void adjustMember(member)}
                                  className="app-button-primary disabled:opacity-50"
                                >
                                  {busy === `adjust:${member.memberId}`
                                    ? "调整中..."
                                    : member.isLeader
                                      ? "应用 Leader 职责调整"
                                      : "应用调整"}
                                </button>
                              </div>
                              {memberBusy && (
                                <p className="mt-2 text-xs text-indigo-600">
                                  模型正在结合全队职责调整，请稍候。
                                </p>
                              )}
                            </div>
                          </div>
                        )}
                      </article>
                    );
                  })}
                </section>
              </>
            )}
          </main>
        </div>
      </div>
    </UserLayout>
  );
};

function MemberCountSelect({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-gray-700">总人数</span>
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="mt-1 block w-full rounded-xl border border-[#eadfd8] bg-white px-3 py-2 text-sm"
      >
        <option value="">系统自动分配</option>
        {MEMBER_COUNTS.map((count) => (
          <option key={count} value={count}>
            {count} 人
          </option>
        ))}
      </select>
    </label>
  );
}

function RoleSection({ title, values }: { title: string; values: string[] }) {
  const clean = values.filter(Boolean);
  return (
    <div className="rounded-xl border border-[#f1e5df] bg-[#fcfbfa] p-4">
      <h3 className="text-sm font-semibold text-gray-900">{title}</h3>
      {clean.length === 0 ? (
        <p className="mt-2 text-sm text-gray-400">未设置</p>
      ) : (
        <ul className="mt-2 space-y-1.5 text-sm text-gray-600">
          {clean.map((value, index) => (
            <li key={`${value}-${index}`} className="flex gap-2">
              <span className="text-indigo-400">•</span>
              <span>{value}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export default CustomTeamTemplatesPage;
