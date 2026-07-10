import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { MonitorUp } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import UserLayout from "../../components/UserLayout";
import { useAuth } from "../../contexts/AuthContext";
import { getTeamMemberDisplayDescription } from "../../lib/teamTemplates";
import { teamService } from "../../services/teamService";
import type {
  TeamDetails,
  TeamEvent,
  TeamMember,
  TeamTask,
  TeamWorkItem,
  TeamWorkspaceFileEntry,
} from "../../types/team";

const statusStyle = (status: string) => {
  switch (status) {
    case "running":
    case "idle":
    case "succeeded":
      return "border-green-200 bg-green-50 text-green-700";
    case "busy":
    case "dispatched":
      return "border-blue-200 bg-blue-50 text-blue-700";
    case "creating":
    case "pending":
    case "stale":
      return "border-yellow-200 bg-yellow-50 text-yellow-700";
    case "failed":
      return "border-red-200 bg-red-50 text-red-700";
    case "offline":
      return "border-gray-200 bg-gray-50 text-gray-700";
    default:
      return "border-gray-200 bg-gray-50 text-gray-700";
  }
};

const availabilityStyle = (availability?: string) => {
  switch (availability) {
    case "idle":
      return "border-green-200 bg-green-50 text-green-700";
    case "busy":
      return "border-blue-200 bg-blue-50 text-blue-700";
    case "blocked":
      return "border-red-200 bg-red-50 text-red-700";
    case "offline":
      return "border-gray-200 bg-gray-50 text-gray-700";
    default:
      return "border-gray-200 bg-gray-50 text-gray-700";
  }
};

const formatDateTime = (value?: string) => {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
};

const compactJson = (value?: Record<string, unknown>) => {
  if (!value) {
    return "-";
  }
  try {
    return JSON.stringify(value);
  } catch {
    return "-";
  }
};

const asRecord = (value: unknown): Record<string, unknown> | undefined =>
  value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;

const parseJsonRecord = (value: unknown): Record<string, unknown> | undefined => {
  if (typeof value !== "string" || !value.trim().startsWith("{")) {
    return undefined;
  }
  try {
    return asRecord(JSON.parse(value));
  } catch {
    return undefined;
  }
};

const TEAM_TASK_HISTORY_PAGE_SIZE = 20;
const TEAM_EVENT_HISTORY_PAGE_SIZE = 50;

const mergeByIdDesc = <T extends { id: number }>(...groups: T[][]) => {
  const merged = new Map<number, T>();
  for (const group of groups) {
    for (const item of group) {
      merged.set(item.id, item);
    }
  }
  return [...merged.values()].sort((a, b) => b.id - a.id);
};

const taskStateRank: Record<TeamTask["status"], number> = {
  pending: 0,
  dispatched: 1,
  running: 2,
  stale: 3,
  failed: 4,
  succeeded: 5,
};

const taskStateTimestamp = (task: TeamTask) => {
  const values = [
    task.updated_at,
    task.finished_at,
    task.started_at,
    task.dispatched_at,
    task.created_at,
  ];
  return values.reduce((latest, value) => {
    if (!value) {
      return latest;
    }
    const timestamp = new Date(value).getTime();
    return Number.isNaN(timestamp) ? latest : Math.max(latest, timestamp);
  }, 0);
};

const shouldReplaceTaskState = (current: TeamTask, candidate: TeamTask) => {
  const currentTime = taskStateTimestamp(current);
  const candidateTime = taskStateTimestamp(candidate);
  if (candidateTime !== currentTime) {
    return candidateTime > currentTime;
  }
  return taskStateRank[candidate.status] >= taskStateRank[current.status];
};

const mergeTasksByLatestState = (...groups: TeamTask[][]) => {
  const merged = new Map<number, TeamTask>();
  for (const group of groups) {
    for (const task of group) {
      const current = merged.get(task.id);
      if (!current || shouldReplaceTaskState(current, task)) {
        merged.set(task.id, task);
      }
    }
  }
  return [...merged.values()].sort((a, b) => b.id - a.id);
};

const oldestID = (items: { id: number }[]) =>
  items.reduce<number | undefined>(
    (current, item) => (current === undefined ? item.id : Math.min(current, item.id)),
    undefined,
  );

const normalizeEventPayload = (event: TeamEvent) => {
  const payload = event.payload || {};
  const embedded = parseJsonRecord(payload.payload);
  return embedded ? { ...embedded, ...payload } : payload;
};

const payloadText = (
  payload: Record<string, unknown> | undefined,
  keys: string[],
) => {
  if (!payload) {
    return "";
  }
  for (const key of keys) {
    const value = payload[key];
    if (typeof value === "string" && value.trim()) {
      return value.trim();
    }
    if (typeof value === "number" || typeof value === "boolean") {
      return String(value);
    }
  }
  return "";
};

const payloadRecordCandidates = (
  payload: Record<string, unknown> | undefined,
) => {
  if (!payload) {
    return [];
  }
  const records: Record<string, unknown>[] = [payload];
  for (const key of ["sent", "message", "metadata", "data", "envelope", "task"]) {
    const direct = asRecord(payload[key]);
    if (direct) {
      records.push(direct);
      continue;
    }
    const parsed = parseJsonRecord(payload[key]);
    if (parsed) {
      records.push(parsed);
    }
  }
  return records;
};

const payloadTextDeep = (
  payload: Record<string, unknown> | undefined,
  keys: string[],
) => {
  for (const record of payloadRecordCandidates(payload)) {
    const text = payloadText(record, keys);
    if (text) {
      return text;
    }
  }
  return "";
};

const payloadBool = (
  payload: Record<string, unknown> | undefined,
  keys: string[],
) => {
  if (!payload) {
    return undefined;
  }
  for (const key of keys) {
    const value = payload[key];
    if (typeof value === "boolean") {
      return value;
    }
    if (typeof value === "number") {
      return value !== 0;
    }
    if (typeof value === "string" && value.trim()) {
      const normalized = value.trim().toLowerCase();
      if (["true", "yes", "1"].includes(normalized)) {
        return true;
      }
      if (["false", "no", "0"].includes(normalized)) {
        return false;
      }
    }
  }
  return undefined;
};

const payloadCollaborationStep = (
  payload: Record<string, unknown> | undefined,
) => asRecord(payload?.collaborationStep);

const payloadNumber = (
  payload: Record<string, unknown> | undefined,
  keys: string[],
) => {
  if (!payload) {
    return undefined;
  }
  for (const key of keys) {
    const value = payload[key];
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
    if (typeof value === "string" && value.trim() && !Number.isNaN(Number(value))) {
      return Number(value);
    }
  }
  return undefined;
};

const taskTitleText = (task: TeamTask) =>
  payloadText(task.payload, ["title", "intent"]) || `任务 #${task.id}`;

const taskPromptText = (task: TeamTask) =>
  payloadText(task.payload, [
    "prompt",
    "instruction",
    "instructions",
    "goal",
    "query",
  ]);

const taskIntentText = (payload?: Record<string, unknown>) =>
  payloadText(payload, ["intent", "runtime_intent", "currentIntent"]);

const memberKeyFromEvent = (
  event: TeamEvent,
  memberById: Map<number, TeamMember>,
) =>
  event.member_id
    ? memberById.get(event.member_id)?.member_key || `#${event.member_id}`
    : payloadText(event.payload, ["memberId", "member_id", "to", "from"]) || "-";

const eventVerb = (eventType: string) => {
  switch (eventType) {
    case "assignment":
      return "任务拆解";
    case "ack":
      return "领取";
    case "result":
      return "交付结果";
    case "blocker":
      return "阻塞";
    case "warning":
      return "提示";
    case "outbound":
      return "发送/转派";
    case "reply":
      return "回复";
    case "progress":
      return "进度";
    case "completion":
      return "完成回执";
    case "task_received":
      return "收到任务";
    case "task_started":
      return "开始执行";
    case "task_progress":
      return "进度更新";
    case "leader_plan":
      return "执行计划";
    case "worker_plan":
      return "成员计划";
    case "worker_progress":
      return "成员进度";
    case "assignment_heartbeat":
      return "运行心跳";
    case "assignment_check_requested":
      return "自动检查";
    case "assignment_check_result":
      return "检查反馈";
    case "leader_synthesis":
      return "汇总整理";
    case "completion_candidate":
      return "候选结果";
    case "completion_validation_warning":
      return "产物校验";
    case "assignment_recovery_started":
      return "自动恢复";
    case "assignment_reissued":
      return "重新派发";
    case "assignment_recovery_exhausted":
      return "需要处理";
    case "task_assigned":
      return "任务转派";
    case "peer_request":
      return "直连协作";
    case "peer_handoff":
      return "协作交接";
    case "peer_review_request":
      return "请求评审";
    case "peer_reply":
      return "协作回复";
    case "task_completed":
      return "完成任务";
    case "task_failed":
      return "任务失败";
    case "message_failed":
      return "消息失败";
    case "task_stale":
      return "长时间无进展";
    default:
      return eventType;
  }
};

const eventTone = (eventType: string) => {
  if (eventType === "task_completed" || eventType === "completion" || eventType === "reply" || eventType === "result") {
    return "border-green-200 bg-green-50 text-green-700";
  }
  if (eventType === "task_failed" || eventType === "message_failed" || eventType === "dlq" || eventType === "blocker") {
    return "border-red-200 bg-red-50 text-red-700";
  }
  if (eventType === "task_stale" || eventType === "assignment_check_requested" || eventType === "assignment_recovery_started" || eventType === "assignment_reissued") {
    return "border-yellow-200 bg-yellow-50 text-yellow-700";
  }
  if (eventType === "assignment_heartbeat" || eventType === "assignment_check_result" || eventType === "leader_plan" || eventType === "worker_plan" || eventType === "worker_progress" || eventType === "leader_synthesis") {
    return "border-sky-200 bg-sky-50 text-sky-700";
  }
  if (eventType.startsWith("peer_")) {
    return "border-violet-200 bg-violet-50 text-violet-700";
  }
  return "border-blue-200 bg-blue-50 text-blue-700";
};

type CollaborationItem = {
  event: TeamEvent;
  payload: Record<string, unknown>;
  collaborationStep?: Record<string, unknown>;
  eventType: string;
  actor: string;
  from: string;
  to: string;
  taskKey: string;
  taskLabel: string;
  content: string;
  occurredAt?: string;
  timeMs: number;
};

type CollaborationGroup = {
  key: string;
  label: string;
  title: string;
  status: string;
  route: string[];
  latestAt: number;
  task?: TeamTask;
  items: CollaborationItem[];
};

type TeamSidePanelView = "kanban" | "files";
type KanbanDetailSize = "short" | "medium" | "long";
type WorkspacePreviewState = {
  path: string;
  name: string;
  content: string;
};

const teamWorkspaceHeight = (
  view: TeamSidePanelView,
  detailSize: KanbanDetailSize,
  communicationMode?: string,
) => {
  if (view === "files") {
    return 760;
  }
  if (isPeerCommunicationMode(communicationMode)) {
    return 900;
  }
  switch (detailSize) {
    case "long":
      return 1220;
    case "medium":
      return 1040;
    default:
      return 820;
  }
};

const groupStartTime = (group: CollaborationGroup) => {
  const taskTime = group.task?.created_at ? new Date(group.task.created_at).getTime() : 0;
  if (Number.isFinite(taskTime) && taskTime > 0) {
    return taskTime;
  }
  const itemTimes = group.items
    .map((item) => item.timeMs)
    .filter((value) => Number.isFinite(value) && value > 0);
  return itemTimes.length > 0 ? Math.min(...itemTimes) : group.latestAt;
};

const groupQueryText = (group: CollaborationGroup) =>
  (group.task ? taskPromptText(group.task) : "") ||
  group.title ||
  group.label ||
  "未命名任务";

const isUserQuestionAnchorGroup = (group: CollaborationGroup) => {
  if (!group.task) {
    return false;
  }
  if (payloadBool(group.task.payload, ["anchorEligible", "anchor_eligible"]) === false) {
    return false;
  }
  const origin = payloadText(group.task.payload, ["origin", "source"]).toLowerCase();
  const intent = payloadText(group.task.payload, ["intent"]).toLowerCase();
  if (intent === "team_bootstrap_introduction" || origin === "system_bootstrap") {
    return false;
  }
  if (group.task.created_by === undefined || group.task.created_by === null) {
    return false;
  }
  if (
    origin &&
    !["user", "user_query", "clawmanager_user", "manual_dispatch"].includes(origin)
  ) {
    return false;
  }
  const query = taskPromptText(group.task) || taskTitleText(group.task) || group.title || "";
  const normalized = query.trim();
  if (!normalized) {
    return false;
  }
  if (/^(task[-_][a-z0-9-]+|team-\d+-task-\d+)$/i.test(normalized)) {
    return false;
  }
  if (/^team\s+message$/i.test(normalized)) {
    return false;
  }
  return true;
};

const isControlPlaneTeamTask = (task?: TeamTask) => {
  if (!task) {
    return false;
  }
  const messageId = (task.message_id || "").toLowerCase();
  const origin = payloadText(task.payload, ["origin", "source"]).toLowerCase();
  const intent = payloadText(task.payload, ["intent"]).toLowerCase();
  const executionMode = payloadText(task.payload, ["executionMode", "execution_mode"]).toLowerCase();
  return (
    messageId.includes("bootstrap-introduction") ||
    origin === "system_bootstrap" ||
    intent === "team_bootstrap_introduction" ||
    executionMode === "leader_control_plane_snapshot"
  );
};

// Server ingestion order is authoritative. Runtime clocks can drift and Redis
// replay can deliver old occurred_at values after a newer user query.
const eventTimeValue = (event: TeamEvent) => event.created_at;

const eventTimeMs = (event: TeamEvent) => {
  const value = eventTimeValue(event);
  const ms = value ? new Date(value).getTime() : 0;
  return Number.isFinite(ms) ? ms : 0;
};

const collaborationEventType = (
  event: TeamEvent,
  payload: Record<string, unknown>,
) => payloadText(payload, ["event", "event_type", "type"]) || event.event_type;

const canonicalTaskKey = (taskId: number | string) => `clawmanager-task-${taskId}`;

const payloadTaskIDs = (payload: Record<string, unknown>) =>
  [
    "taskId",
    "task_id",
    "currentTaskId",
    "runtimeTaskId",
    "rootTaskId",
    "root_task_id",
    "parentTaskId",
    "parent_task_id",
    "parentMessageId",
    "parent_message_id",
    "inReplyTo",
    "in_reply_to",
    "MessageThreadId",
  ]
    .map((key) => payloadText(payload, [key]))
    .concat(
      (() => {
        const step = payloadCollaborationStep(payload);
        return step
          ? [
              payloadText(step, ["rootTaskId", "root_task_id"]),
              payloadText(step, ["taskId", "task_id"]),
              payloadText(step, ["parentTaskId", "parent_task_id"]),
            ]
          : [];
      })(),
    )
    .filter(Boolean);

const taskCreatorKey = (task?: TeamTask) =>
  task?.created_by ? `user-${task.created_by}` : "user";

const taskKeyFromEvent = (
  event: TeamEvent,
  payload: Record<string, unknown>,
  taskKeyByEventTaskID: Map<string, string>,
  taskKeyByMessageID: Map<string, string>,
) => {
  if (event.task_id) {
    return canonicalTaskKey(event.task_id);
  }
  const messageId = payloadText(payload, ["messageId", "message_id"]) || event.message_id;
  if (messageId) {
    const taskKey = taskKeyByMessageID.get(messageId);
    if (taskKey) {
      return taskKey;
    }
  }
  for (const taskId of payloadTaskIDs(payload)) {
    const taskKey = taskKeyByEventTaskID.get(taskId);
    if (taskKey) {
      return taskKey;
    }
  }
  const taskId = payloadTaskIDs(payload)[0];
  if (taskId) {
    return taskId;
  }
  const inReplyTo = payloadText(payload, ["inReplyTo", "in_reply_to"]);
  if (inReplyTo) {
    const taskKey = taskKeyByMessageID.get(inReplyTo);
    if (taskKey) {
      return taskKey;
    }
    return `reply:${inReplyTo}`;
  }
  if (messageId) {
    return `message:${messageId}`;
  }
  return `event:${event.id}`;
};

const taskLabelFromKey = (key: string, event: TeamEvent) => {
  if (event.task_id) {
    return `ClawManager #${event.task_id}`;
  }
  if (key.startsWith("message:")) {
    return key.replace("message:", "message ");
  }
  if (key.startsWith("reply:")) {
    return key.replace("reply:", "reply to ");
  }
  if (key.startsWith("event:")) {
    return "未归类事件";
  }
  return key;
};

const terminalResultText = (payload: Record<string, unknown> | undefined) =>
  payloadTextDeep(payload, [
    "resultMarkdown",
    "result_markdown",
    "result",
    "answer",
  ]);

const taskResultText = (task?: TeamTask) =>
  terminalResultText(task?.result) ||
  payloadText(task?.result, ["summary", "message", "text"]) ||
  terminalResultText(task?.payload);

function isTerminalResultEventType(eventType: string) {
  return (
    eventType === "task_completed" ||
    eventType === "completion" ||
    eventType === "task_failed" ||
    eventType === "message_failed"
  );
}

const collaborationContent = (
  payload: Record<string, unknown>,
  eventType = "",
) => {
  const eventKind = payloadText(payload, ["eventKind", "event_kind", "kind"]).toLowerCase();
  const isBusinessResult =
    isTerminalResultEventType(eventType) ||
    eventType === "completion_deferred" ||
    eventKind === "completion_deferred" ||
    payloadBool(payload, ["assignmentResultOnly", "assignment_result_only"]) === true;
  if (isBusinessResult) {
    const fullResult = terminalResultText(payload);
    if (fullResult) {
      return fullResult;
    }
  }
  const step = payloadCollaborationStep(payload);
  const fullBusinessBody = payloadText(step, ["content", "detail", "resultMarkdown", "result", "answer", "text", "message"]);
  const businessNarrativeKinds = new Set([
    "leader_plan", "worker_plan", "worker_progress", "leader_synthesis", "leader_synthesis_reminder", "leader_decision_reminder",
    "agent_narrative", "agent_plan", "agent_assignment", "agent_handoff", "agent_progress", "agent_delivery", "agent_review", "agent_synthesis",
  ]);
  if (
    fullBusinessBody &&
    (businessNarrativeKinds.has(eventKind) || ["outbound", "team_send", "task_assigned", "peer_handoff", "peer_request", "peer_review_request"].includes(eventType))
  ) {
    return fullBusinessBody;
  }
  const stepSummary = payloadText(step, isTerminalResultEventType(eventType) ? ["content", "detail", "summary"] : ["summary", "detail", "content"]);
  if (stepSummary) {
    return stepSummary;
  }
  const resultMarkdown = payloadTextDeep(payload, ["resultMarkdown"]);
  if (resultMarkdown) {
    return resultMarkdown;
  }
  const title = payloadTextDeep(payload, ["title"]);
  const text = payloadTextDeep(payload, [
    "text",
    "prompt",
    "instruction",
    "instructions",
    "goal",
    "query",
  ]);
  if (title && text && !text.includes(title)) {
    return `**${title}**\n\n${text}`;
  }
  if (text) {
    return text;
  }
  return payloadTextDeep(payload, [
    "resultMarkdown",
    "summary",
    "lastSummary",
    "diagnostic",
    "error",
    "error_message",
    "message",
    "title",
  ]);
};

const routeFromItem = (item: CollaborationItem) =>
  [item.from, item.actor, item.to].filter((value, index, values) =>
    value && values.indexOf(value) === index,
  );

const eventActorKey = (
  event: TeamEvent,
  payload: Record<string, unknown>,
  eventType: string,
  from: string,
  memberById: Map<number, TeamMember>,
  task?: TeamTask,
) => {
  if ((eventType === "outbound" || eventType === "task_assigned") && task?.created_by) {
    return taskCreatorKey(task);
  }
  if (from && from !== "clawmanager") {
    return from;
  }
  if (from === "clawmanager" && task?.created_by) {
    return taskCreatorKey(task);
  }
  if (event.member_id) {
    return memberById.get(event.member_id)?.member_key || `#${event.member_id}`;
  }
  return payloadText(payload, ["memberId", "member_id", "from", "to"]) || "system";
};

const inferGroupStatus = (items: CollaborationItem[], task?: TeamTask) => {
  if (task?.status) {
    if (task.status === "succeeded" && isDispatchOnlyResult(taskResultText(task))) {
      return items.some((item) => item.eventType === "outbound" || item.eventType === "task_assigned" || item.eventType === "team_send" || isDispatchOnlyResult(item.content))
        ? "dispatched"
        : "running";
    }
    return task.status;
  }
  const sorted = [...items].sort((a, b) => b.timeMs - a.timeMs);
  const latest = sorted[0];
  const terminal = sorted.find((item) => {
    if (payloadBool(item.payload, ["memberTerminalOnly", "member_terminal_only"]) || isDispatchOnlyResult(item.content)) {
      return false;
    }
    const status = payloadText(item.payload, ["status"]).toLowerCase();
    return (
      isFailedCollaborationItem(item) ||
      item.eventType === "task_completed" ||
      item.eventType === "completion" ||
      ["succeeded", "success", "completed", "complete", "done", "finished", "ok"].includes(status)
    );
  });
  if (terminal) {
    const status = payloadText(terminal.payload, ["status"]).toLowerCase();
    if (isFailedCollaborationItem(terminal) && !["succeeded", "success", "completed", "complete", "done", "finished", "ok"].includes(status)) {
      return "failed";
    }
    return "succeeded";
  }
  if (latest?.eventType === "reply") {
    return "replied";
  }
  if (
    items.some(
      (item) =>
        item.eventType === "progress" ||
        item.eventType === "task_started" ||
        item.eventType === "peer_request" ||
        item.eventType === "peer_handoff" ||
        item.eventType === "peer_review_request",
    )
  ) {
    return "running";
  }
  if (items.some((item) => item.eventType === "outbound" || item.eventType === "task_assigned" || item.eventType === "team_send")) {
    return "dispatched";
  }
  return "observed";
};

const isFailedCollaborationItem = (item: CollaborationItem) => {
  const status = payloadText(item.payload, ["status", "task_status", "taskStatus"]).toLowerCase();
  if (["succeeded", "success", "completed", "complete", "done", "finished", "ok", "warning"].includes(status)) {
    return false;
  }
  const eventType = item.eventType;
  if (eventType !== "task_failed" && eventType !== "message_failed") {
    return status === "failed" || status === "failure" || status === "error" || status === "blocked";
  }
  const content = item.content.toLowerCase();
  if (content.includes("dispatch finished without reply/completion") || content.includes("without reply/completion")) {
    return false;
  }
  return /error|failed|failure|exception|timeout|forbidden|失败|错误|异常|超时|blocked/.test(content) ||
    status === "failed" ||
    status === "failure" ||
    status === "error" ||
    status === "blocked";
};

const buildCollaborationGroups = (
  events: TeamEvent[],
  tasks: TeamTask[],
  memberById: Map<number, TeamMember>,
) => {
  const taskByID = new Map(tasks.map((task) => [task.id, task]));
  const taskByKey = new Map<string, TeamTask>();
  const taskKeyByEventTaskID = new Map<string, string>();
  const taskKeyByMessageID = new Map<string, string>();

  for (const task of tasks) {
    const taskKey = canonicalTaskKey(task.id);
    taskByKey.set(taskKey, task);
    taskKeyByEventTaskID.set(taskKey, taskKey);
    taskKeyByEventTaskID.set(String(task.id), taskKey);
    taskKeyByEventTaskID.set(`team-${task.team_id}-task-${task.id}`, taskKey);
    if (task.message_id) {
      taskKeyByMessageID.set(task.message_id, taskKey);
    }
  }

  for (const event of events) {
    const payload = normalizeEventPayload(event);
    const messageID = payloadText(payload, ["messageId", "message_id"]) || event.message_id;
    const canonicalKey =
      (event.task_id ? canonicalTaskKey(event.task_id) : "") ||
      (messageID && taskKeyByMessageID.get(messageID)) ||
      "";
    if (!canonicalKey) {
      continue;
    }
    for (const taskID of payloadTaskIDs(payload)) {
      taskKeyByEventTaskID.set(taskID, canonicalKey);
    }
    const inReplyTo = payloadText(payload, ["inReplyTo", "in_reply_to"]);
    if (messageID) {
      taskKeyByMessageID.set(messageID, canonicalKey);
    }
    if (inReplyTo) {
      taskKeyByMessageID.set(inReplyTo, canonicalKey);
    }
  }
  const groups = new Map<string, CollaborationGroup>();

  for (const event of events) {
    const payload = normalizeEventPayload(event);
    const step = payloadCollaborationStep(payload);
    const eventType = collaborationEventType(event, payload);
    const from =
      payloadText(step, ["actor", "from", "sourceMemberId", "source_member_id"]) ||
      payloadText(payload, ["from", "sourceMemberId", "source_member_id"]);
    const to =
      payloadText(step, ["target", "to", "recipient", "targetMemberId", "target_member_id"]) ||
      payloadText(payload, ["to", "recipient", "targetMemberId", "target_member_id", "memberId"]);
    const taskKey = taskKeyFromEvent(
      event,
      payload,
      taskKeyByEventTaskID,
      taskKeyByMessageID,
    );
    const existingTask =
      taskByKey.get(taskKey) ||
      (event.task_id ? taskByID.get(event.task_id) : undefined);
    const actor = eventActorKey(event, payload, eventType, from, memberById, existingTask);
    const item: CollaborationItem = {
      event,
      payload,
      collaborationStep: step || undefined,
      eventType,
      actor,
      from,
      to,
      taskKey,
      taskLabel: taskLabelFromKey(taskKey, event),
      content: collaborationContent(payload, eventType),
      occurredAt: eventTimeValue(event),
      timeMs: eventTimeMs(event),
    };
    const current = groups.get(taskKey);
    if (current) {
      current.items.push(item);
      current.latestAt = Math.max(current.latestAt, item.timeMs);
      current.route = [...current.route, ...routeFromItem(item)].filter(
        (value, index, values) => values.indexOf(value) === index,
      );
      if (!current.task && existingTask) {
        current.task = existingTask;
      }
    } else {
      groups.set(taskKey, {
        key: taskKey,
        label: item.taskLabel,
        title:
          payloadText(payload, ["title", "intent"]) ||
          (existingTask ? taskTitleText(existingTask) : item.taskLabel),
        status: "observed",
        route: routeFromItem(item),
        latestAt: item.timeMs,
        task: existingTask,
        items: [item],
      });
    }
  }

  for (const task of tasks) {
    const key = canonicalTaskKey(task.id);
    if (!groups.has(key)) {
      const target = memberById.get(task.target_member_id)?.member_key || `#${task.target_member_id}`;
      groups.set(key, {
        key,
        label: `ClawManager #${task.id}`,
        title: taskTitleText(task),
        status: task.status,
        route: ["ClawManager", target],
        latestAt: new Date(task.updated_at || task.created_at).getTime(),
        task,
        items: [],
      });
    }
  }

  return [...groups.values()]
    .map((group) => ({
      ...group,
      status: inferGroupStatus(group.items, group.task),
      items: [...group.items].sort((a, b) => a.timeMs - b.timeMs || a.event.id - b.event.id),
    }))
    .sort((a, b) => b.latestAt - a.latestAt);
};

const TeamDetailPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { user } = useAuth();
  const teamId = id ? Number(id) : null;
  const [details, setDetails] = useState<TeamDetails | null>(null);
  const [loadedTasks, setLoadedTasks] = useState<TeamTask[]>([]);
  const [loadedEvents, setLoadedEvents] = useState<TeamEvent[]>([]);
  const [hasMoreTasks, setHasMoreTasks] = useState(false);
  const [hasMoreEvents, setHasMoreEvents] = useState(false);
  const taskHistoryExhausted = useRef(false);
  const eventHistoryExhausted = useRef(false);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [targetMember] = useState("");
  const [taskTitle] = useState("server-smoke");
  const [taskPrompt, setTaskPrompt] = useState("");
  const [dispatching, setDispatching] = useState(false);
  const [dispatchError, setDispatchError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [selectedGroupKey, setSelectedGroupKey] = useState<string | null>(null);
  const [sidePanelView, setSidePanelView] = useState<TeamSidePanelView>("kanban");
  const [kanbanDetailSize, setKanbanDetailSize] = useState<KanbanDetailSize>("short");
  const [workspacePreview, setWorkspacePreview] = useState<WorkspacePreviewState | null>(null);

  useEffect(() => {
    setLoadedTasks([]);
    setLoadedEvents([]);
    setHasMoreTasks(false);
    setHasMoreEvents(false);
    taskHistoryExhausted.current = false;
    eventHistoryExhausted.current = false;
    setHistoryError(null);
    setSelectedGroupKey(null);
  }, [teamId]);

  const loadTeam = useCallback(
    async (options?: { background?: boolean }) => {
      if (!teamId || Number.isNaN(teamId)) {
        setError("Team ID 无效");
        setLoading(false);
        return;
      }
      try {
        if (!options?.background) {
          setLoading(true);
        }
        const data = await teamService.getTeam(teamId);
        setDetails(data);
        setLoadedTasks((current) => mergeTasksByLatestState(current, data.tasks || []));
        setLoadedEvents((current) => mergeByIdDesc(data.events || [], current));
        setHasMoreTasks((current) =>
          taskHistoryExhausted.current
            ? false
            : current || (data.tasks?.length || 0) >= TEAM_TASK_HISTORY_PAGE_SIZE,
        );
        setHasMoreEvents((current) =>
          eventHistoryExhausted.current
            ? false
            : current || (data.events?.length || 0) >= TEAM_EVENT_HISTORY_PAGE_SIZE,
        );
        setError(null);
      } catch (err: any) {
        setError(err.response?.data?.error || "加载 Team 失败");
      } finally {
        setLoading(false);
      }
    },
    [teamId],
  );

  useEffect(() => {
    void loadTeam();
  }, [loadTeam]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      void loadTeam({ background: true });
    }, 5000);
    return () => window.clearInterval(timer);
  }, [loadTeam]);

  const memberById = useMemo(() => {
    const result = new Map<number, TeamMember>();
    details?.members.forEach((member) => result.set(member.id, member));
    return result;
  }, [details?.members]);

  const tasks = loadedTasks.length > 0 ? loadedTasks : details?.tasks || [];
  const events = loadedEvents.length > 0 ? loadedEvents : details?.events || [];
  const currentUserLabel = useMemo(() => {
    const username = typeof user?.username === "string" ? user.username.trim() : "";
    const email = typeof user?.email === "string" ? user.email.trim() : "";
    const baseLabel = username || email;
    return baseLabel ? `${baseLabel}（当前用户）` : "当前用户";
  }, [user?.email, user?.username]);
  const currentUserKey =
    typeof user?.id === "number" ? `user-${user.id}` : "current-user";
  const collaborationGroups = useMemo(
    () => buildCollaborationGroups(events, tasks, memberById),
    [events, tasks, memberById],
  );
  const activeProcessGroup = useMemo(
    () =>
      collaborationGroups.find((group) => group.key === selectedGroupKey) ||
      selectActiveProcessGroup(collaborationGroups),
    [collaborationGroups, selectedGroupKey],
  );
  const mainWorkspaceHeight = teamWorkspaceHeight(
    sidePanelView,
    kanbanDetailSize,
    details?.team.communication_mode,
  );

  const handleDeleteTeam = async () => {
    if (!teamId || !window.confirm(`删除 Team「${details?.team.name || teamId}」？`)) {
      return;
    }
    try {
      setActionLoading("delete-team");
      await teamService.deleteTeam(teamId);
      navigate("/teams");
    } catch (err: any) {
      alert(err.response?.data?.error || "删除 Team 失败");
    } finally {
      setActionLoading(null);
    }
  };

  const handlePreviewWorkspacePath = useCallback(
    async (workspacePath: string) => {
      if (!details?.team.id) {
        return;
      }
      const relPath = workspaceLinkToRelativePath(workspacePath);
      if (!relPath || !isPreviewableWorkspacePath(relPath)) {
        window.alert("当前文件不支持在线预览");
        return;
      }
      try {
        const result = await teamService.previewWorkspaceFile(details.team.id, relPath);
        setWorkspacePreview({
          path: result.path,
          name: result.name,
          content: result.content,
        });
      } catch (err: any) {
        window.alert(err.response?.data?.error || "预览文件失败");
      }
    },
    [details?.team.id],
  );

  const handleDownloadWorkspacePreview = useCallback(async () => {
    if (!details?.team.id || !workspacePreview) {
      return;
    }
    try {
      const blob = await teamService.downloadWorkspaceFile(details.team.id, workspacePreview.path);
      downloadBlob(blob, workspacePreview.name);
    } catch (err: any) {
      window.alert(err.response?.data?.error || "下载文件失败");
    }
  }, [details?.team.id, workspacePreview]);

  const handleDispatch = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!teamId || !taskPrompt.trim()) {
      setDispatchError("任务内容不能为空");
      return;
    }
    try {
      setDispatching(true);
      setDispatchError(null);
      setSelectedGroupKey(null);
      await teamService.dispatchTask(teamId, {
        target_member_id: targetMember.trim(),
        payload: {
          title: taskTitle.trim() || "Team task",
          prompt: taskPrompt.trim(),
          responseLocale: "zh-CN",
        },
      });
      setTaskPrompt("");
      await loadTeam({ background: true });
    } catch (err: any) {
      setDispatchError(err.response?.data?.error || "派发任务失败");
    } finally {
      setDispatching(false);
    }
  };

  const handleLoadMoreHistory = async () => {
    if (!teamId || historyLoading || (!hasMoreTasks && !hasMoreEvents)) {
      return;
    }
    try {
      setHistoryLoading(true);
      setHistoryError(null);
      const [taskHistory, eventHistory] = await Promise.all([
        hasMoreTasks
          ? teamService.getTeamTasks(teamId, oldestID(tasks), TEAM_TASK_HISTORY_PAGE_SIZE)
          : Promise.resolve(null),
        hasMoreEvents
          ? teamService.getTeamEvents(teamId, oldestID(events), TEAM_EVENT_HISTORY_PAGE_SIZE)
          : Promise.resolve(null),
      ]);
      if (taskHistory) {
        setLoadedTasks((current) => mergeTasksByLatestState(current, taskHistory.tasks || []));
        setHasMoreTasks(taskHistory.has_more);
        taskHistoryExhausted.current = !taskHistory.has_more;
      }
      if (eventHistory) {
        setLoadedEvents((current) => mergeByIdDesc(current, eventHistory.events || []));
        setHasMoreEvents(eventHistory.has_more);
        eventHistoryExhausted.current = !eventHistory.has_more;
      }
    } catch (err: any) {
      setHistoryError(err.response?.data?.error || "加载历史消息失败");
    } finally {
      setHistoryLoading(false);
    }
  };

  if (loading) {
    return (
      <UserLayout>
        <div className="flex min-h-[60vh] items-center justify-center text-lg text-gray-600">
          正在加载...
        </div>
      </UserLayout>
    );
  }

  if (error || !details) {
    return (
      <UserLayout title="Team">
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-red-700">
          {error || "Team 不存在"}
        </div>
      </UserLayout>
    );
  }

  return (
    <UserLayout
      title={details.team.name}
      titleAccessory={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2 rounded-2xl border border-[#f1e7e1] bg-white/80 px-3 py-2 shadow-[0_14px_34px_-30px_rgba(72,44,24,0.45)] backdrop-blur">
          <span
            className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${statusStyle(details.team.status)}`}
          >
            {details.team.status}
          </span>
          <span className="text-sm font-medium text-gray-700">
            Team #{details.team.id}
          </span>
          <span className="hidden text-sm text-gray-300 sm:inline">·</span>
          <span className="max-w-[220px] truncate text-sm text-gray-600 xl:max-w-[320px]">
            共享目录：{details.team.shared_mount_path}
          </span>
          <div className="ml-1 flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={() => void loadTeam({ background: true })}
              className="inline-flex h-9 items-center justify-center rounded-xl border border-[#eadfd8] bg-white px-4 text-sm font-medium text-[#5f5957] hover:bg-[#fff8f5]"
            >
              刷新
            </button>
            <button
              type="button"
              onClick={handleDeleteTeam}
              disabled={actionLoading === "delete-team"}
              className="inline-flex h-9 items-center justify-center rounded-xl border border-red-200 bg-red-50 px-4 text-sm font-medium text-red-700 hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {actionLoading === "delete-team" ? "删除中..." : "删除 Team"}
            </button>
            <Link
              to="/teams"
              className="inline-flex h-9 items-center justify-center rounded-xl border border-[#eadfd8] bg-white px-4 text-sm font-medium text-[#5f5957] hover:bg-[#fff8f5]"
            >
              返回列表
            </Link>
          </div>
        </div>
      }
    >
      <div className="space-y-4">
        <div className="grid grid-cols-1 items-start gap-6 2xl:grid-cols-[minmax(0,1.18fr)_minmax(560px,0.86fr)]">
          <div className="min-w-0 self-start">
            <div
              className="transition-[height] duration-300"
              style={{ height: mainWorkspaceHeight }}
            >
              <CollaborationPanel
                team={details.team}
                groups={collaborationGroups}
                members={details.members}
                memberById={memberById}
                leaderMemberId={details.leader_member_id}
                currentUserLabel={currentUserLabel}
                currentUserKey={currentUserKey}
                taskPrompt={taskPrompt}
                dispatching={dispatching}
                dispatchError={dispatchError}
                historyLoading={historyLoading}
                historyError={historyError}
                hasMoreHistory={hasMoreTasks || hasMoreEvents}
                activeGroupKey={activeProcessGroup?.key}
                onTaskPromptChange={setTaskPrompt}
                onDispatch={handleDispatch}
                onLoadMoreHistory={handleLoadMoreHistory}
                onSelectGroup={setSelectedGroupKey}
                sidePanelView={sidePanelView}
                onSidePanelViewChange={setSidePanelView}
                onWorkspaceFileOpen={handlePreviewWorkspacePath}
              />
            </div>
          </div>

          <aside
            className="self-start transition-[height] duration-300"
            style={{ height: mainWorkspaceHeight }}
          >
            {sidePanelView === "files" ? (
              <TeamWorkspaceBrowser
                teamId={details.team.id}
                rootPath={details.team.shared_mount_path}
                heightClass="h-full"
              />
            ) : (
              <InteractionProcessPanel
                group={activeProcessGroup}
                workItems={details.work_items || []}
                memberById={memberById}
                leaderMemberId={details.leader_member_id}
                communicationMode={details.team.communication_mode}
                compact
                expanded
                heightClass="h-full"
                showToggle={false}
                onDetailSizeChange={setKanbanDetailSize}
                onWorkspaceFileOpen={handlePreviewWorkspacePath}
              />
            )}
          </aside>
        </div>

        <div className="grid grid-cols-1 gap-6">
          <section className="app-panel overflow-hidden">
            <div className="border-b border-[#f1e7e1] px-5 py-4">
              <h2 className="text-lg font-semibold text-gray-900">成员与团队配置</h2>
            </div>
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-[#f1e7e1] text-sm">
                <thead className="bg-[#fff8f5] text-left text-xs font-semibold uppercase tracking-[0.14em] text-[#b46c50]">
                  <tr>
                    <th className="px-5 py-3">成员</th>
                    <th className="px-5 py-3">角色</th>
                    <th className="px-5 py-3">Runtime</th>
                    <th className="px-5 py-3">Mode</th>
                    <th className="px-5 py-3">职责</th>
                    <th className="px-5 py-3">状态</th>
                    <th
                      className="px-5 py-3"
                      title="Runtime 最近上报的可用态，和 ClawManager 调度状态分开显示"
                    >
                      Runtime 可用态
                    </th>
                    <th className="px-5 py-3">最后在线</th>
                    <th className="px-5 py-3">实例</th>
                    <th className="min-w-[140px] px-5 py-3">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#f1e7e1] bg-white">
                  {details.members.map((member) => (
                    <tr key={member.id}>
                      <td className="px-5 py-4">
                        <div className="font-medium text-gray-900">
                          {member.display_name}
                        </div>
                        <div className="mt-1 font-mono text-xs text-gray-500">
                          {member.member_key}
                        </div>
                      </td>
                      <td className="px-5 py-4 text-gray-600">{member.role}</td>
                      <td className="px-5 py-4 text-gray-600">
                        {member.runtime_type || "openclaw"}
                      </td>
                      <td className="px-5 py-4 text-gray-600">
                        {member.instance_mode || "lite"}
                      </td>
                      <td className="min-w-[280px] max-w-md px-5 py-4">
                        <DescriptionPreview
                          text={getTeamMemberDisplayDescription(member.description)}
                        />
                      </td>
                      <td className="px-5 py-4">
                        <span
                          className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${statusStyle(member.status)}`}
                        >
                          {member.status}
                        </span>
                      </td>
                      <td className="max-w-xs px-5 py-4">
                        <span
                          className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${availabilityStyle(member.availability)}`}
                        >
                          {member.availability || "unknown"}
                        </span>
                        {(member.blocked_reason || member.last_summary) && (
                          <div className="mt-2 line-clamp-3 text-xs text-gray-500">
                            {member.blocked_reason || member.last_summary}
                          </div>
                        )}
                        {(member.runtime_task_id || member.runtime_intent) && (
                          <div className="mt-1 break-all font-mono text-[11px] text-gray-400">
                            {member.runtime_intent || "-"} ·{" "}
                            {member.runtime_task_id || "-"}
                          </div>
                        )}
                      </td>
                      <td className="px-5 py-4 text-gray-600">
                        {formatDateTime(member.last_seen_at)}
                      </td>
                      <td className="px-5 py-4">
                        {member.instance_id ? (
                          <Link
                            to={`/instances/${member.instance_id}`}
                            className="text-[#dc2626] hover:underline"
                          >
                            #{member.instance_id}
                          </Link>
                        ) : (
                          "-"
                        )}
                      </td>
                      <td className="min-w-[140px] px-5 py-4">
                        <div className="flex flex-wrap gap-2">
                          {member.instance_id ? (
                            <Link
                              to={`/instances/${member.instance_id}`}
                              target="_blank"
                              rel="noreferrer"
                              className="group/desktop inline-flex min-w-[108px] items-center justify-center gap-1.5 whitespace-nowrap rounded-full border border-slate-300 bg-[linear-gradient(180deg,#ffffff,#e9eef3)] px-3.5 py-2 text-xs font-semibold text-slate-700 shadow-[0_8px_18px_-14px_rgba(15,23,42,0.65)] transition-all duration-200 hover:-translate-y-0.5 hover:border-sky-300 hover:bg-[linear-gradient(180deg,#ffffff,#e1eef7)] hover:text-sky-700 hover:shadow-[0_12px_24px_-15px_rgba(14,116,144,0.55)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-300/45"
                            >
                              <MonitorUp
                                aria-hidden="true"
                                className="h-3.5 w-3.5 transition-transform duration-200 group-hover/desktop:-translate-y-0.5"
                                strokeWidth={1.8}
                              />
                              访问桌面
                            </Link>
                          ) : (
                            <button
                              type="button"
                              disabled
                              className="inline-flex min-w-[108px] items-center justify-center gap-1.5 whitespace-nowrap rounded-full border border-slate-200 bg-[linear-gradient(180deg,#f8fafc,#edf1f5)] px-3.5 py-2 text-xs font-semibold text-slate-400"
                            >
                              <MonitorUp
                                aria-hidden="true"
                                className="h-3.5 w-3.5"
                                strokeWidth={1.8}
                              />
                              访问桌面
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        </div>

      </div>
      {workspacePreview && (
        <WorkspacePreviewModal
          preview={workspacePreview}
          onClose={() => setWorkspacePreview(null)}
          onDownload={() => void handleDownloadWorkspacePreview()}
        />
      )}
    </UserLayout>
  );
};

function TeamWorkspaceBrowser({
  teamId,
  rootPath,
  heightClass = "h-[320px]",
}: {
  teamId: number;
  rootPath: string;
  heightClass?: string;
}) {
  const [path, setPath] = useState("");
  const [entries, setEntries] = useState<TeamWorkspaceFileEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [uploadMenuOpen, setUploadMenuOpen] = useState(false);
  const [preview, setPreview] = useState<WorkspacePreviewState | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const folderInputRef = useRef<HTMLInputElement | null>(null);

  const loadFiles = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await teamService.listWorkspaceFiles(teamId, path);
      setEntries(result.entries || []);
    } catch (err: any) {
      setError(err.response?.data?.error || "加载共享目录失败");
    } finally {
      setLoading(false);
    }
  }, [path, teamId]);

  useEffect(() => {
    void loadFiles();
  }, [loadFiles]);

  const runAction = async (key: string, action: () => Promise<void>) => {
    try {
      setActionLoading(key);
      setError(null);
      await action();
      await loadFiles();
    } catch (err: any) {
      setError(err.response?.data?.error || "操作失败");
    } finally {
      setActionLoading(null);
    }
  };

  const handleCreateFolder = () => {
    const name = window.prompt("新建文件夹名称");
    if (!name?.trim()) {
      return;
    }
    void runAction("mkdir", () =>
      teamService.createWorkspaceFolder(teamId, { path, name: name.trim() }),
    );
  };

  const handleRename = (entry: TeamWorkspaceFileEntry) => {
    const newName = window.prompt("重命名为", entry.name);
    if (!newName?.trim() || newName.trim() === entry.name) {
      return;
    }
    void runAction(`rename-${entry.path}`, () =>
      teamService.renameWorkspaceEntry(teamId, {
        path: entry.path,
        new_name: newName.trim(),
      }),
    );
  };

  const handleDelete = (entry: TeamWorkspaceFileEntry) => {
    if (!window.confirm(`删除「${entry.name}」？`)) {
      return;
    }
    void runAction(`delete-${entry.path}`, () =>
      teamService.deleteWorkspaceEntry(teamId, entry.path),
    );
  };

  const handlePreview = async (entry: TeamWorkspaceFileEntry) => {
    try {
      setActionLoading(`preview-${entry.path}`);
      setError(null);
      const result = await teamService.previewWorkspaceFile(teamId, entry.path);
      setPreview({ path: result.path, name: result.name, content: result.content });
    } catch (err: any) {
      setError(err.response?.data?.error || "预览文件失败");
    } finally {
      setActionLoading(null);
    }
  };

  const handleDownload = async (entry: TeamWorkspaceFileEntry) => {
    try {
      setActionLoading(`download-${entry.path}`);
      setError(null);
      const blob = await teamService.downloadWorkspaceFile(teamId, entry.path);
      downloadBlob(blob, workspaceDownloadName(entry));
    } catch (err: any) {
      setError(err.response?.data?.error || "下载文件失败");
    } finally {
      setActionLoading(null);
    }
  };

  const handleDownloadPreview = async () => {
    if (!preview) {
      return;
    }
    try {
      setActionLoading(`download-${preview.path}`);
      setError(null);
      const blob = await teamService.downloadWorkspaceFile(teamId, preview.path);
      downloadBlob(blob, preview.name);
    } catch (err: any) {
      setError(err.response?.data?.error || "下载文件失败");
    } finally {
      setActionLoading(null);
    }
  };

  const handleUpload = async (fileList: FileList | null, mode: "file" | "folder") => {
    if (!fileList || fileList.length === 0) {
      return;
    }
    const files = Array.from(fileList);
    const relativePaths = files.map((file) =>
      mode === "folder"
        ? (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name
        : file.name,
    );
    await runAction("upload", () =>
      teamService.uploadWorkspaceFiles(teamId, path, files, relativePaths),
    );
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
    if (folderInputRef.current) {
      folderInputRef.current.value = "";
    }
  };

  const crumbs = workspaceBreadcrumbs(path);

  return (
    <section className={`app-panel flex flex-col overflow-hidden rounded-[14px] border-slate-200 bg-white shadow-[0_24px_56px_-44px_rgba(15,23,42,0.55)] transition-[height] duration-300 ${heightClass}`}>
      <div className="flex shrink-0 items-center justify-between gap-3 border-b border-slate-200 px-4 py-3">
        <div className="min-w-0 flex flex-wrap items-center gap-2 text-sm font-semibold text-slate-800">
          <button
            type="button"
            onClick={() => setPath("")}
            className="rounded-lg text-slate-800 hover:text-red-600"
          >
            Workspace
          </button>
          <span className="text-slate-300">/</span>
          {crumbs.length === 0 ? (
            <span className="truncate text-slate-500">{rootPath || "/team"}</span>
          ) : (
            crumbs.map((crumb) => (
              <React.Fragment key={crumb.path || "root"}>
                <button
                  type="button"
                  onClick={() => setPath(crumb.path)}
                  className="max-w-[120px] truncate rounded-lg text-slate-700 hover:text-red-600"
                >
                  {crumb.label}
                </button>
                <span className="text-slate-300">/</span>
              </React.Fragment>
            ))
          )}
        </div>
        <div className="relative flex shrink-0 items-center gap-2">
          <WorkspaceIconButton title="刷新" onClick={() => void loadFiles()}>
            <Icon name="refresh" />
          </WorkspaceIconButton>
          <WorkspaceIconButton title="新建文件夹" onClick={handleCreateFolder}>
            <Icon name="folder-plus" />
          </WorkspaceIconButton>
          <WorkspaceIconButton title="上传" onClick={() => setUploadMenuOpen((value) => !value)}>
            <Icon name="upload" />
          </WorkspaceIconButton>
          {uploadMenuOpen && (
            <div className="absolute right-0 top-11 z-30 w-40 overflow-hidden rounded-xl border border-slate-200 bg-white py-1.5 shadow-[0_18px_44px_-28px_rgba(15,23,42,0.75)]">
              <button
                type="button"
                onClick={() => {
                  setUploadMenuOpen(false);
                  fileInputRef.current?.click();
                }}
                className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-slate-700 hover:bg-slate-50"
              >
                <Icon name="upload-file" />
                上传文件
              </button>
              <button
                type="button"
                onClick={() => {
                  setUploadMenuOpen(false);
                  folderInputRef.current?.click();
                }}
                className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-slate-700 hover:bg-slate-50"
              >
                <Icon name="folder-upload" />
                上传文件夹
              </button>
            </div>
          )}
        </div>
      </div>

      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(event) => void handleUpload(event.target.files, "file")}
      />
      <input
        ref={(node) => {
          folderInputRef.current = node;
          if (node) {
            node.setAttribute("webkitdirectory", "");
            node.setAttribute("directory", "");
          }
        }}
        type="file"
        multiple
        className="hidden"
        onChange={(event) => void handleUpload(event.target.files, "folder")}
      />

      <div className="grid shrink-0 grid-cols-[minmax(0,1fr)_82px_130px_176px] border-b border-slate-200 bg-slate-50 px-4 py-2 text-xs font-bold uppercase tracking-[0.04em] text-slate-500">
        <div>Name</div>
        <div>Size</div>
        <div>Modified</div>
        <div className="text-right">Actions</div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {error && (
          <div className="mx-4 mt-3 rounded-xl border border-red-100 bg-red-50 px-3 py-2 text-xs text-red-700">
            {error}
          </div>
        )}
        {loading ? (
          <div className="p-8 text-center text-sm text-slate-400">加载共享目录...</div>
        ) : entries.length === 0 ? (
          <div className="p-8 text-center text-sm text-slate-400">当前目录为空</div>
        ) : (
          entries.map((entry) => (
            <div
              key={entry.path}
              className="grid grid-cols-[minmax(0,1fr)_82px_130px_176px] items-center border-b border-slate-100 px-4 py-3 text-sm transition hover:bg-slate-50/70"
            >
              <button
                type="button"
                onClick={() => {
                  if (entry.type === "directory") {
                    setPath(entry.path);
                  } else if (entry.previewable) {
                    void handlePreview(entry);
                  }
                }}
                className="flex min-w-0 items-center gap-2 text-left font-semibold text-slate-800"
              >
                <Icon name={entry.type === "directory" ? "folder" : "file"} />
                <span className="truncate">{entry.name}</span>
              </button>
              <div className="text-slate-500">{entry.type === "directory" ? "-" : formatWorkspaceSize(entry.size)}</div>
              <div className="truncate text-slate-500">{formatWorkspaceModified(entry.modified_at)}</div>
              <div className="flex justify-end gap-1.5">
                {entry.type === "file" && entry.previewable && (
                  <WorkspaceIconButton
                    title="预览"
                    compact
                    disabled={actionLoading === `preview-${entry.path}`}
                    onClick={() => void handlePreview(entry)}
                  >
                    <Icon name="eye" />
                  </WorkspaceIconButton>
                )}
                <WorkspaceIconButton
                  title={entry.type === "directory" ? "下载文件夹" : "下载"}
                  compact
                  disabled={actionLoading === `download-${entry.path}`}
                  onClick={() => void handleDownload(entry)}
                >
                  <Icon name="download" />
                </WorkspaceIconButton>
                <WorkspaceIconButton
                  title="重命名"
                  compact
                  disabled={actionLoading === `rename-${entry.path}`}
                  onClick={() => handleRename(entry)}
                >
                  <Icon name="edit" />
                </WorkspaceIconButton>
                <WorkspaceIconButton
                  title="删除"
                  compact
                  danger
                  disabled={actionLoading === `delete-${entry.path}`}
                  onClick={() => handleDelete(entry)}
                >
                  <Icon name="trash" />
                </WorkspaceIconButton>
              </div>
            </div>
          ))
        )}
      </div>

      {preview && (
        <WorkspacePreviewModal
          preview={preview}
          onClose={() => setPreview(null)}
          onDownload={() => void handleDownloadPreview()}
        />
      )}
    </section>
  );
}

function WorkspacePreviewModal({
  preview,
  onClose,
  onDownload,
}: {
  preview: WorkspacePreviewState;
  onClose: () => void;
  onDownload: () => void;
}) {
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(preview.content);
      setCopyState("copied");
      window.setTimeout(() => setCopyState("idle"), 1600);
    } catch {
      setCopyState("failed");
      window.setTimeout(() => setCopyState("idle"), 1600);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/40 px-4 py-6 backdrop-blur-sm">
      <div className="flex max-h-[82vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl">
        <div className="flex items-center justify-between gap-3 border-b border-slate-200 px-5 py-3">
          <div className="min-w-0">
            <div className="text-xs font-semibold uppercase tracking-[0.14em] text-slate-400">Preview</div>
            <div className="mt-1 truncate text-base font-semibold text-slate-900">{preview.name}</div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <button
              type="button"
              onClick={onDownload}
              className="rounded-xl border border-slate-200 px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-50"
            >
              下载
            </button>
            <button
              type="button"
              onClick={() => void handleCopy()}
              className="rounded-xl border border-slate-200 px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-50"
            >
              {copyState === "copied" ? "已复制" : copyState === "failed" ? "复制失败" : "复制全部"}
            </button>
            <button
              type="button"
              onClick={onClose}
              className="rounded-xl border border-slate-200 px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-50"
            >
              关闭
            </button>
          </div>
        </div>
        <div className="min-h-0 overflow-auto bg-slate-50 p-5">
          <pre className="whitespace-pre-wrap break-words rounded-xl bg-white p-4 text-sm leading-6 text-slate-800 shadow-inner">
            {preview.content}
          </pre>
        </div>
      </div>
    </div>
  );
}

function WorkspaceIconButton({
  title,
  children,
  compact = false,
  danger = false,
  disabled = false,
  onClick,
}: {
  title: string;
  children: React.ReactNode;
  compact?: boolean;
  danger?: boolean;
  disabled?: boolean;
  onClick: () => void;
}) {
  const tone = danger
    ? "border-red-200 text-red-500 hover:bg-red-50"
    : "border-slate-200 text-slate-600 hover:bg-slate-50 hover:text-slate-900";
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      disabled={disabled}
      onClick={onClick}
      className={`inline-flex items-center justify-center rounded-lg border bg-white transition disabled:cursor-wait disabled:opacity-50 ${tone} ${
        compact ? "h-8 w-8" : "h-9 w-9"
      }`}
    >
      {children}
    </button>
  );
}

function Icon({ name }: { name: string }) {
  const common = "h-4 w-4 shrink-0";
  switch (name) {
    case "refresh":
      return (
        <svg className={common} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M20 12a8 8 0 1 1-2.34-5.66" />
          <path d="M20 4v6h-6" />
        </svg>
      );
    case "folder-plus":
      return (
        <svg className={common} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" />
          <path d="M12 11v5" />
          <path d="M9.5 13.5h5" />
        </svg>
      );
    case "upload":
    case "upload-file":
      return (
        <svg className={common} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M12 16V4" />
          <path d="m7 9 5-5 5 5" />
          <path d="M20 16v3a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1v-3" />
        </svg>
      );
    case "folder-upload":
      return (
        <svg className={common} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" />
          <path d="M12 16v-5" />
          <path d="m9.5 13.5 2.5-2.5 2.5 2.5" />
        </svg>
      );
    case "folder":
      return (
        <svg className={`${common} text-slate-500`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" />
        </svg>
      );
    case "file":
      return (
        <svg className={`${common} text-slate-500`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9Z" />
          <path d="M14 3v6h6" />
        </svg>
      );
    case "eye":
      return (
        <svg className={common} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6-10-6-10-6Z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      );
    case "download":
      return (
        <svg className={common} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M12 4v12" />
          <path d="m7 11 5 5 5-5" />
          <path d="M5 20h14" />
        </svg>
      );
    case "trash":
      return (
        <svg className={common} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M3 6h18" />
          <path d="M8 6V4h8v2" />
          <path d="m19 6-1 14H6L5 6" />
          <path d="M10 11v5" />
          <path d="M14 11v5" />
        </svg>
      );
    default:
      return (
        <svg className={common} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M12 20h9" />
          <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" />
        </svg>
      );
  }
}

function workspaceBreadcrumbs(path: string) {
  const parts = path.split("/").filter(Boolean);
  return parts.map((part, index) => ({
    label: part,
    path: parts.slice(0, index + 1).join("/"),
  }));
}

function workspaceDownloadName(entry: TeamWorkspaceFileEntry) {
  return entry.type === "directory" ? `${entry.name}.zip` : entry.name;
}

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function workspaceLinkToRelativePath(raw: string) {
  const normalized = raw.trim().replace(/\\/g, "/").replace(/[，。；;,.、)）\]}]+$/g, "");
  if (normalized === "/team") {
    return "";
  }
  if (normalized.startsWith("/team/")) {
    return normalized.slice("/team/".length);
  }
  const relativeTeamMatch = normalized.match(/^\.?\/?team\/(.+)$/i);
  if (relativeTeamMatch) {
    return relativeTeamMatch[1];
  }
  const liteSharedMatch = normalized.match(/^\/workspaces\/teams\/user-\d+\/team-\d+-shared\/(.+)$/i);
  if (liteSharedMatch) {
    return liteSharedMatch[1];
  }
  return normalized.replace(/^\/+/, "");
}

function isPreviewableWorkspacePath(path: string) {
  return /\.(md|txt|json)$/i.test(path.trim());
}

function isTeamWorkspaceLink(path: string) {
  const normalized = path.trim().replace(/\\/g, "/");
  return (
    normalized.startsWith("/team/") ||
    /^\.?\/?team\/.+/i.test(normalized) ||
    /^\/workspaces\/teams\/user-\d+\/team-\d+-shared\//i.test(normalized)
  );
}

function formatWorkspaceSize(size: number) {
  if (!Number.isFinite(size) || size <= 0) {
    return "0 B";
  }
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KB`;
  }
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function formatWorkspaceModified(value?: string) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString([], {
    year: "numeric",
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function DescriptionPreview({ text }: { text?: string }) {
  const [expanded, setExpanded] = useState(false);
  const normalized = (text || "").trim();
  if (!normalized) {
    return <span className="text-sm text-gray-400">-</span>;
  }

  const lines = normalized.split(/\r?\n/);
  const previewLines = lines.slice(0, 5);
  const previewText = previewLines.join("\n");
  const hasMore = lines.length > 5 || normalized.length > 280;

  return (
    <div className="group rounded-xl border border-[#f1e7e1] bg-[#fffaf7] px-3 py-2.5 text-sm leading-6 text-gray-700 shadow-[0_10px_22px_-22px_rgba(72,44,24,0.45)]">
      <div className={expanded ? "" : "max-h-[7.5rem] overflow-hidden"}>
        <MarkdownContent text={expanded || !hasMore ? normalized : previewText} compact />
      </div>
      {hasMore && (
        <button
          type="button"
          onClick={() => setExpanded((current) => !current)}
          className="mt-2 inline-flex items-center rounded-full border border-[#eadfd8] bg-white px-2.5 py-1 text-xs font-medium text-[#8b5a45] transition hover:border-[#ef6b4a] hover:text-[#dc2626]"
        >
          {expanded ? "收起" : `展开 ${Math.max(lines.length - previewLines.length, 1)} 行`}
        </button>
      )}
    </div>
  );
}

function CollaborationPanel({
  team,
  groups,
  members,
  memberById,
  leaderMemberId,
  currentUserLabel,
  currentUserKey,
  taskPrompt,
  dispatching,
  dispatchError,
  historyLoading,
  historyError,
  hasMoreHistory,
  activeGroupKey,
  onTaskPromptChange,
  onDispatch,
  onLoadMoreHistory,
  onSelectGroup,
  sidePanelView,
  onSidePanelViewChange,
  onWorkspaceFileOpen,
}: {
  team: TeamDetails["team"];
  groups: CollaborationGroup[];
  members: TeamMember[];
  memberById: Map<number, TeamMember>;
  leaderMemberId?: string;
  currentUserLabel: string;
  currentUserKey: string;
  taskPrompt: string;
  dispatching: boolean;
  dispatchError: string | null;
  historyLoading: boolean;
  historyError: string | null;
  hasMoreHistory: boolean;
  activeGroupKey?: string;
  onTaskPromptChange: (value: string) => void;
  onDispatch: (event: React.FormEvent) => void;
  onLoadMoreHistory: () => void;
  onSelectGroup: (groupKey: string) => void;
  sidePanelView: TeamSidePanelView;
  onSidePanelViewChange: (view: TeamSidePanelView) => void;
  onWorkspaceFileOpen?: (path: string) => void;
}) {
  const messages = buildTeamChatMessages(
    groups,
    memberById,
    leaderMemberId,
    currentUserLabel,
    currentUserKey,
  );
  const onlineCount = members.filter(
    (member) => !["offline", "deleted", "deleting"].includes(member.status),
  ).length;
  const messageAnchorRefs = useRef(new Map<string, HTMLDivElement | null>());
  const firstMessageByGroup = useMemo(() => {
    const result = new Map<string, string>();
    for (const message of messages) {
      if (message.threadKey && !result.has(message.threadKey)) {
        result.set(message.threadKey, message.id);
      }
    }
    return result;
  }, [messages]);
  const queryAnchors = useMemo(
    () =>
      groups
        .filter((group) => isUserQuestionAnchorGroup(group))
        .sort((a, b) => groupStartTime(a) - groupStartTime(b)),
    [groups],
  );
  const handleSelectAnchor = (groupKey: string) => {
    onSelectGroup(groupKey);
    window.setTimeout(() => {
      const target = messageAnchorRefs.current.get(groupKey);
      target?.scrollIntoView({ behavior: "smooth", block: "start" });
    }, 50);
  };

  return (
    <section className="app-panel relative flex h-full min-h-0 flex-col overflow-hidden rounded-[22px]">
      <div className="shrink-0 border-b border-[#e8e8e8] bg-white px-4 py-3">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <h2 className="text-base font-semibold leading-6 text-gray-950">团队群聊</h2>
            <div className="mt-0.5 truncate text-xs text-gray-500">
              Team #{team.id} · {team.status}
            </div>
            <div className="mt-1 flex items-center gap-2 text-xs text-gray-500">
              <span className="h-2 w-2 rounded-full bg-emerald-400" />
              <span>{onlineCount}人在线</span>
            </div>
          </div>
          <div className="flex shrink-0 rounded-full border border-[#eadfd8] bg-[#fff8f5] p-1">
            {([
              ["kanban", "看板"],
              ["files", "文件"],
            ] as const).map(([view, label]) => (
              <button
                key={view}
                type="button"
                onClick={() => onSidePanelViewChange(view)}
                className={`rounded-full px-3 py-1 text-xs font-medium transition ${
                  sidePanelView === view
                    ? "bg-white text-gray-950 shadow-sm"
                    : "text-gray-500 hover:text-gray-800"
                }`}
              >
                {label}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div
        className="min-h-0 flex-1 overflow-auto bg-[#f5f5f5]"
        onScroll={(event) => {
          if (event.currentTarget.scrollTop <= 24 && hasMoreHistory && !historyLoading) {
            void onLoadMoreHistory();
          }
        }}
      >
        {messages.length === 0 ? (
          <div className="space-y-5 px-4 py-5">
            <div className="p-6 text-center text-xs text-gray-500">暂无群聊消息。</div>
          </div>
        ) : (
          <div className="space-y-5 px-4 py-5">
            {(hasMoreHistory || historyLoading || historyError) && (
              <div className="space-y-2 text-center">
                {hasMoreHistory && (
                  <button
                    type="button"
                    disabled={historyLoading}
                    onClick={() => void onLoadMoreHistory()}
                    className="inline-flex items-center gap-2 rounded-full border border-[#dddddd] bg-white px-4 py-2 text-xs font-medium text-gray-500 shadow-sm transition hover:border-gray-300 hover:text-gray-700 disabled:cursor-wait disabled:opacity-70"
                  >
                    <span className="text-base leading-none">↑</span>
                    <span>{historyLoading ? "加载历史消息中..." : "向上滑动或点击查看更多历史消息"}</span>
                  </button>
                )}
                {!hasMoreHistory && historyLoading && (
                  <span className="inline-flex rounded-full border border-[#dddddd] bg-white px-4 py-2 text-xs text-gray-500 shadow-sm">
                    加载历史消息中...
                  </span>
                )}
                {historyError && (
                  <div className="text-xs text-red-600">{historyError}</div>
                )}
              </div>
            )}
            <TimeDivider value={messages[0]?.time} />
            {messages.map((message) => {
              const isFirstGroupMessage =
                message.threadKey &&
                firstMessageByGroup.get(message.threadKey) === message.id;
              return (
                <div
                  key={message.id}
                  ref={(node) => {
                    if (!message.threadKey || !isFirstGroupMessage) {
                      return;
                    }
                    if (node) {
                      messageAnchorRefs.current.set(message.threadKey, node);
                    } else {
                      messageAnchorRefs.current.delete(message.threadKey);
                    }
                  }}
                  className={isFirstGroupMessage ? "scroll-mt-4" : undefined}
                >
                  {message.kind === "system" ? (
                    <SystemChatLine message={message} />
                  ) : (
                    <TeamChatMessageRow
                      message={message}
                      onWorkspaceFileOpen={onWorkspaceFileOpen}
                    />
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {queryAnchors.length >= 3 && (
        <QuestionAnchorRail
          groups={queryAnchors}
          activeGroupKey={activeGroupKey}
          onSelect={handleSelectAnchor}
        />
      )}

      <div className="shrink-0 border-t border-[#dddddd] bg-white px-3 py-2.5">
        {dispatchError && (
          <div className="mb-2 rounded-lg border border-red-100 bg-red-50 px-3 py-2 text-xs text-red-700">
            {dispatchError}
          </div>
        )}
        <form onSubmit={onDispatch} className="flex items-end gap-2">
          <textarea
            value={taskPrompt}
            onChange={(event) => onTaskPromptChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                event.currentTarget.form?.requestSubmit();
              }
            }}
            rows={1}
            placeholder="发送消息..."
            className="max-h-20 min-h-[34px] flex-1 resize-none rounded-full border border-[#d9d9d9] bg-white px-4 py-1.5 text-xs leading-5 text-gray-900 outline-none transition focus:border-[#9ca3af] focus:ring-2 focus:ring-gray-100"
          />
          <button
            type="submit"
            disabled={dispatching || !taskPrompt.trim()}
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-[#1f2937] text-white transition hover:bg-[#111827] disabled:cursor-not-allowed disabled:bg-gray-300"
            aria-label="发送任务"
            title="发送任务"
          >
            {dispatching ? (
              <span className="h-4 w-4 animate-spin rounded-full border-2 border-white/30 border-t-white" />
            ) : (
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M5 12h14m-6-6 6 6-6 6" />
              </svg>
            )}
          </button>
        </form>
      </div>
    </section>
  );
}

function InteractionProcessPanel({
  group,
  workItems = [],
  memberById,
  leaderMemberId,
  compact = false,
  expanded: controlledExpanded,
  onExpandedChange,
  heightClass,
  showToggle = true,
  onDetailSizeChange,
  onWorkspaceFileOpen,
  communicationMode,
}: {
  group?: CollaborationGroup;
  workItems?: TeamWorkItem[];
  memberById: Map<number, TeamMember>;
  leaderMemberId?: string;
  communicationMode?: string;
  compact?: boolean;
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
  heightClass?: string;
  showToggle?: boolean;
  onDetailSizeChange?: (size: KanbanDetailSize) => void;
  onWorkspaceFileOpen?: (path: string) => void;
}) {
  const memberByKey = new Map(
    [...memberById.values()].map((member) => [member.member_key, member]),
  );
  const steps = group
    ? buildProcessSteps(group, memberById, memberByKey, leaderMemberId)
    : [];
  const peerMode = isPeerCommunicationMode(communicationMode);
  const peerRoot = peerMode && isRootTaskTargetLeader(group, memberById, leaderMemberId);
  const finalResult = group ? processFinalResult(group, steps, peerRoot) : "";
  const visualStatus = group ? processVisualStatus(group, finalResult, steps, peerRoot) : "idle";
  const rootWorkItems = group?.task
    ? workItems.filter((item) => item.root_task_id === group.task?.id)
    : [];
  const rootControlPlaneTask = isControlPlaneTeamTask(group?.task);
  const leaderMediatedMode = isLeaderMediatedCommunicationMode(communicationMode);
  const authoritativeLeaderFlow = leaderMediatedMode && !peerRoot && !!group?.task;
  const leaderLedgerSummary = authoritativeLeaderFlow
    ? summarizeLeaderWorkItems(rootWorkItems, group?.task?.status, rootControlPlaneTask)
    : undefined;
  const progress = group
    ? authoritativeLeaderFlow
      ? leaderLedgerSummary?.progress || 0
      : processProgress(group, steps, visualStatus, peerRoot)
    : 0;
  const isTerminal = ["succeeded", "failed", "stale"].includes(visualStatus);
  const statusText = workflowStatusText(group?.task?.workflow_state) || processStatusText(visualStatus);
  const title = group?.task ? taskTitleText(group.task) : group?.title || "等待任务";
  const queryText = group?.task
    ? taskPromptText(group.task) || group.title
    : group?.items.find((item) => item.content)?.content || "";
  const columns = authoritativeLeaderFlow
    ? buildLeaderMediatedKanbanColumns(group, rootWorkItems, memberById, finalResult, rootControlPlaneTask)
    : buildKanbanColumns(group, steps, finalResult, visualStatus);
  const peerModel = peerRoot
    ? buildPeerCollaborationModel(group, steps, memberById, leaderMemberId)
    : undefined;
  const peerLanes = peerModel?.lanes || [];
  const decompositionItems = peerRoot
    ? buildPeerDecompositionItems(peerLanes)
    : buildDecompositionItems(columns);
  const kanbanCounts = {
    todo: peerRoot ? peerLanes.filter((lane) => lane.status === "idle" || lane.status === "waiting").length : columns.todo.length,
    doing: peerRoot ? peerLanes.filter((lane) => lane.status === "working").length : columns.doing.length,
    done: peerRoot ? peerLanes.filter((lane) => lane.status === "done" || lane.status === "blocked").length : columns.done.length,
  };
  const peerCards = peerLanes.map((lane) => lane.card).filter(Boolean) as KanbanTaskCard[];
  const allCards = peerRoot ? peerCards : [...columns.todo, ...columns.doing, ...columns.done];
  const defaultCardId =
    allCards.find((card) => card.column === "doing")?.id ||
    allCards.find((card) => card.column === "done")?.id ||
    allCards.find((card) => card.column === "todo")?.id ||
    "";
  const [selectedCardId, setSelectedCardId] = useState(defaultCardId);
  const [internalExpanded, setInternalExpanded] = useState(false);
  const expanded = controlledExpanded ?? internalExpanded;
  const setExpanded = onExpandedChange ?? setInternalExpanded;
  const selectedCard =
    allCards.find((card) => card.id === selectedCardId) ||
    allCards.find((card) => card.id === defaultCardId);
  const selectedDetailSize = kanbanDetailSizeForText(selectedCard?.detail || selectedCard?.summary || finalResult || "");
  const leaderWorkspaceSize = peerMode
    ? "short"
    : leaderKanbanWorkspaceSize(selectedDetailSize, decompositionItems.length, columns);

  useEffect(() => {
    setSelectedCardId(defaultCardId);
  }, [defaultCardId, group?.key]);

  useEffect(() => {
    if (!onDetailSizeChange) {
      return;
    }
    if (peerMode) {
      return;
    }
    onDetailSizeChange(leaderWorkspaceSize);
  }, [leaderWorkspaceSize, onDetailSizeChange, peerMode]);
  const progressStyle =
    visualStatus === "failed" || visualStatus === "stale"
      ? "from-rose-500 via-orange-400 to-amber-300"
      : isTerminal
        ? "from-slate-400 via-cyan-400 to-emerald-400"
        : "from-slate-500 via-sky-500 to-cyan-400";
  const routeMembers = group?.route || [];

  return (
    <section className={`app-panel cm-tech-panel flex flex-col overflow-hidden rounded-[22px] transition-[height] duration-300 ${heightClass || (compact ? (expanded ? "h-[760px]" : "h-[480px]") : "h-[420px]")}`}>
      <div className={`cm-tech-header text-slate-800 ${compact ? "px-4 py-3" : "px-4 py-3.5"}`}>
        <div className="relative z-[1] flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="relative flex h-2.5 w-2.5 shrink-0">
                {group && !isTerminal && (
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-sky-400 opacity-45" />
                )}
                <span
                  className={`relative inline-flex h-2.5 w-2.5 rounded-full ${
                    !group
                      ? "bg-slate-400"
                      : isTerminal
                        ? "bg-emerald-300"
                        : "cm-tech-breathe bg-sky-500"
                  }`}
                />
              </span>
              <span className="truncate text-xs font-semibold uppercase tracking-[0.18em] text-slate-600">
                {peerRoot ? "Peer Collaboration" : "Execution Kanban"}
              </span>
              <span className="shrink-0 rounded-full bg-white/75 px-2 py-0.5 text-[11px] font-medium text-slate-600 shadow-sm ring-1 ring-slate-300/80 backdrop-blur">
                {statusText}
              </span>
            </div>
            <div className="mt-2 text-sm font-semibold leading-5">{title}</div>
            <div className="mt-1 line-clamp-2 text-[11px] leading-4 text-slate-600">
              {queryText || "用户提交 query 后，这里会展示拆解、执行和汇总。"}
            </div>
            <div className="mt-1.5 flex max-w-full flex-nowrap items-center gap-1 overflow-hidden text-[10px] leading-4 text-slate-500">
              {routeMembers.length > 0 ? (
                routeMembers.map((member, index) => (
                  <React.Fragment key={`${group?.key || "idle"}-header-route-${member}-${index}`}>
                    {index > 0 && <span className="text-slate-400">→</span>}
                    <span className="max-w-[128px] truncate rounded-full bg-white/75 px-1.5 py-0.5 text-slate-600 shadow-sm ring-1 ring-slate-300/80 backdrop-blur">
                      {displayMemberName(member, memberByKey, leaderMemberId)}
                    </span>
                  </React.Fragment>
                ))
              ) : (
                <span className="rounded-full bg-white/75 px-1.5 py-0.5 text-slate-600 shadow-sm ring-1 ring-slate-300/80 backdrop-blur">
                  Idle
                </span>
              )}
            </div>
          </div>
          <div className="min-w-[150px] max-w-[176px] shrink-0 text-right">
            {leaderLedgerSummary ? (
              <>
                <div title={leaderLedgerSummary.phase} className="truncate whitespace-nowrap text-sm font-semibold leading-5">
                  {leaderLedgerSummary.phase}
                </div>
                <div title={leaderLedgerSummary.deliveryLabel || `成员交付 ${leaderLedgerSummary.delivered}/${leaderLedgerSummary.total}`} className="mt-1 truncate whitespace-nowrap text-[11px] text-slate-500">
                  {leaderLedgerSummary.deliveryLabel || `成员交付 ${leaderLedgerSummary.delivered}/${leaderLedgerSummary.total}`}
                </div>
                {leaderLedgerSummary.artifactLabel && (
                  <div title={leaderLedgerSummary.artifactLabel} className="mt-0.5 truncate whitespace-nowrap text-[11px] text-slate-500">
                    {leaderLedgerSummary.artifactLabel}
                  </div>
                )}
              </>
            ) : (
              <>
                <div className="text-xl font-semibold leading-none">{progress}%</div>
                <div className="mt-1 text-[11px] text-slate-500">overall</div>
              </>
            )}
            {compact && showToggle && (
              <button
                type="button"
                onClick={() => setExpanded(!expanded)}
                className="mt-2 rounded-full bg-white/75 px-2 py-0.5 text-[11px] text-slate-600 shadow-sm ring-1 ring-slate-300/80 transition hover:bg-white hover:text-slate-900"
              >
                {expanded ? "收起" : "展开"}
              </button>
            )}
          </div>
        </div>
        <div className="relative z-[1] mt-3 h-1.5 overflow-hidden rounded-full bg-slate-300/70 shadow-inner">
          <div
            className={`cm-tech-progress h-full rounded-full bg-gradient-to-r ${progressStyle} shadow-[0_0_12px_rgba(56,189,248,0.45)] transition-all duration-700`}
            style={{ width: `${progress}%` }}
          />
        </div>
      </div>

      <div className={`cm-tech-workspace min-h-0 flex-1 ${expanded ? "flex flex-col gap-2 overflow-hidden px-3 py-2.5" : "space-y-3 overflow-auto px-4 py-3"}`}>
        {compact && !expanded ? (
          <div className="space-y-3">
            <div className="cm-tech-surface rounded-2xl p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-slate-400">
                    当前任务
                  </div>
                  <div className="mt-1 line-clamp-2 text-xs leading-5 text-slate-700">
                    {queryText || "Idle，等待新的团队任务。"}
                  </div>
                </div>
                <div className="grid shrink-0 grid-cols-3 gap-1 text-center text-[10px]">
                  <KanbanCount label="T" value={kanbanCounts.todo} tone="todo" />
                  <KanbanCount label="D" value={kanbanCounts.doing} tone="doing" />
                  <KanbanCount label="✓" value={kanbanCounts.done} tone="done" />
                </div>
              </div>
              <div className="mt-3 space-y-2">
                {decompositionItems.length === 0 ? (
                  <div className="cm-tech-subtle rounded-xl border-dashed px-3 py-4 text-center text-xs text-slate-400">
                    暂无拆解步骤
                  </div>
                ) : (
                  decompositionItems.slice(0, 3).map((item) => (
                    <div key={item.id} className="cm-tech-subtle rounded-xl px-3 py-2 transition duration-200 hover:-translate-y-0.5 hover:border-slate-400/70">
                      <div className="flex items-center justify-between gap-2">
                        <div className="min-w-0 truncate text-xs font-semibold text-slate-800">{item.title}</div>
                        <span className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-medium ${item.badgeClass}`}>
                          {item.status}
                        </span>
                      </div>
                      <div className="mt-1 truncate text-[11px] text-slate-500">
                        {item.route}
                      </div>
                      {item.summary && (
                        <div className="mt-1 line-clamp-2 text-[11px] leading-4 text-slate-500">
                          {item.summary}
                        </div>
                      )}
                    </div>
                  ))
                )}
              </div>
            </div>
          </div>
        ) : peerRoot ? (
          <>
        <PeerCollaborationMatrix
          model={peerModel}
          queryText={queryText}
          statusText={statusText}
          visualStatus={visualStatus}
          progress={progress}
          selectedCardId={selectedCard?.id}
          onSelect={setSelectedCardId}
        />

        <div className={`cm-tech-surface flex min-h-[112px] min-w-0 flex-col overflow-hidden rounded-2xl p-2.5 ${kanbanDetailPanelMaxHeight(selectedDetailSize)}`}>
          <div className="mb-1.5 flex shrink-0 items-center justify-between gap-3">
            <div>
              <div className="text-xs font-semibold text-slate-800">
                {selectedCard ? "卡片详情" : "汇总结果"}
              </div>
              <div className="mt-0.5 text-[11px] text-slate-400">
                点击 Kanban 卡片可切换查看细节
              </div>
            </div>
            <span className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${statusStyle(visualStatus)}`}>
              {statusText}
            </span>
          </div>
          <div className="min-h-0">
            {selectedCard ? (
              <KanbanCardDetail
                card={selectedCard}
                size={selectedDetailSize}
                onWorkspaceFileOpen={onWorkspaceFileOpen}
              />
            ) : finalResult ? (
              <div className={`overflow-auto pb-4 pr-1 text-xs leading-5 text-slate-700 ${kanbanDetailBodyMaxHeight(selectedDetailSize)}`}>
                <MarkdownContent
                  text={finalResult}
                  compact
                  onWorkspaceFileOpen={onWorkspaceFileOpen}
                />
              </div>
            ) : (
              <div className="text-xs leading-5 text-slate-500">
                当前空闲。新的团队任务出现后，这里会自动切换到执行过程。
              </div>
            )}
          </div>
        </div>
          </>
        ) : (
          <>
        <div className="cm-tech-surface shrink-0 rounded-2xl p-2">
          <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(144px,172px)]">
            <div className="min-w-0">
              <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-slate-400">
                总任务 Query
              </div>
              <div className="mt-1 line-clamp-1 text-xs leading-5 text-slate-700">
                {queryText || "Idle，等待新的团队任务。"}
              </div>
              <div className="cm-tech-subtle mt-2 rounded-xl p-1.5">
                <div className="mb-1 flex items-center justify-between gap-2">
                  <span className="text-[11px] font-semibold text-slate-700">任务拆解</span>
                  <span className="text-[10px] text-slate-400">{decompositionItems.length} 项</span>
                </div>
                {decompositionItems.length === 0 ? (
                  <div className="text-[11px] leading-5 text-slate-400">
                    等待 Leader 拆解并派发子任务。
                  </div>
                ) : (
                  <div className="space-y-1">
                    {decompositionItems.map((item) => (
                      <div
                        key={item.id}
                        className="rounded-lg border border-white/80 bg-white/75 px-2 py-1 shadow-[0_1px_4px_rgba(15,23,42,0.05)] transition duration-200 hover:border-sky-200/80 hover:bg-white"
                      >
                        <div className="flex items-center justify-between gap-2">
                          <div className="min-w-0 truncate text-[11px] font-medium text-slate-700">
                            {item.title}
                          </div>
                          <span className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-medium ${item.badgeClass}`}>
                            {item.status}
                          </span>
                        </div>
                        {item.summary && (
                          <div className="mt-1 line-clamp-1 text-[10px] leading-4 text-slate-500">{item.summary}</div>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
            <div className="cm-tech-subtle min-w-0 rounded-xl px-3 py-2">
              <div title={statusText} className={`max-w-full truncate whitespace-nowrap rounded-full border px-2 py-0.5 text-[11px] font-medium ${statusStyle(visualStatus)}`}>
                {statusText}
              </div>
              {leaderLedgerSummary ? (
                <>
                  <div title={leaderLedgerSummary.phase} className="mt-2 truncate whitespace-nowrap text-[13px] font-semibold leading-5 text-slate-900">
                    {leaderLedgerSummary.phase}
                  </div>
                  <div title={`交付 ${leaderLedgerSummary.delivered}/${leaderLedgerSummary.total} · ${leaderLedgerSummary.blockers > 0 ? `${leaderLedgerSummary.blockers} 个阻塞` : "无阻塞"}`} className="mt-1 truncate whitespace-nowrap text-[10px] leading-4 text-slate-400">
                    交付 {leaderLedgerSummary.delivered}/{leaderLedgerSummary.total} · {leaderLedgerSummary.blockers > 0 ? `${leaderLedgerSummary.blockers} 个阻塞` : "无阻塞"}
                  </div>
                </>
              ) : (
                <>
                  <div className="mt-2 text-xl font-semibold leading-none text-slate-900">{progress}%</div>
                  <div className="mt-1 text-[10px] uppercase tracking-[0.14em] text-slate-400">overall</div>
                </>
              )}
              <div className="mt-2 grid grid-cols-3 gap-1 text-center text-[10px]">
                <KanbanCount label="T" value={kanbanCounts.todo} tone="todo" />
                <KanbanCount label="D" value={kanbanCounts.doing} tone="doing" />
                <KanbanCount label="✓" value={kanbanCounts.done} tone="done" />
              </div>
            </div>
          </div>
        </div>

        <div className="shrink-0 overflow-hidden">
          <div className="grid grid-cols-3 gap-2">
            <KanbanColumn
              title="Todo"
              subtitle="已拆解 / 待领取"
              cards={columns.todo}
              tone="todo"
              selectedCardId={selectedCard?.id}
              onSelect={setSelectedCardId}
            />
            <KanbanColumn
              title="Doing"
              subtitle="执行中 / 有进展"
              cards={columns.doing}
              tone="doing"
              selectedCardId={selectedCard?.id}
              onSelect={setSelectedCardId}
            />
            <KanbanColumn
              title="Done"
              subtitle="已完成 / 已反馈"
              cards={columns.done}
              tone="done"
              selectedCardId={selectedCard?.id}
              onSelect={setSelectedCardId}
            />
          </div>
        </div>

        <div className={`cm-tech-surface flex min-h-[112px] min-w-0 shrink-0 flex-col overflow-hidden rounded-2xl p-2.5 ${kanbanDetailPanelMaxHeight(selectedDetailSize)}`}>
          <div className="mb-1.5 flex shrink-0 items-center justify-between gap-3">
            <div>
              <div className="text-xs font-semibold text-slate-800">
                {selectedCard ? "卡片详情" : "汇总结果"}
              </div>
              <div className="mt-0.5 text-[11px] text-slate-400">
                点击 Kanban 卡片可切换查看细节
              </div>
            </div>
            <span className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${statusStyle(visualStatus)}`}>
              {statusText}
            </span>
          </div>
          <div className="min-h-0">
            {selectedCard ? (
              <KanbanCardDetail
                card={selectedCard}
                size={selectedDetailSize}
                onWorkspaceFileOpen={onWorkspaceFileOpen}
              />
            ) : finalResult ? (
              <div className={`overflow-auto pb-4 pr-1 text-xs leading-5 text-slate-700 ${kanbanDetailBodyMaxHeight(selectedDetailSize)}`}>
                <MarkdownContent
                  text={finalResult}
                  compact
                  onWorkspaceFileOpen={onWorkspaceFileOpen}
                />
              </div>
            ) : (
              <div className="text-xs leading-5 text-slate-500">
                当前空闲。新的团队任务出现后，这里会自动切换到执行过程。
              </div>
            )}
          </div>
        </div>
          </>
        )}
      </div>
    </section>
  );
}

function QuestionAnchorRail({
  groups,
  activeGroupKey,
  onSelect,
}: {
  groups: CollaborationGroup[];
  activeGroupKey?: string;
  onSelect: (groupKey: string) => void;
}) {
  const visibleGroups = groups.slice(-12);
  const hiddenCount = Math.max(groups.length - visibleGroups.length, 0);

  return (
    <div className="absolute right-3 top-1/2 z-20 hidden -translate-y-1/2 xl:block">
      <div className="group/anchors relative flex items-center">
        <div className="flex flex-col items-end gap-2.5 px-1 py-2">
          {hiddenCount > 0 && (
            <div className="mb-0.5 h-px w-7 bg-slate-300/70" title={`还有 ${hiddenCount} 条更早的问题`} />
          )}
          {visibleGroups.map((group) => {
            const active = group.key === activeGroupKey;
            return (
              <button
                key={group.key}
                type="button"
                title={groupQueryText(group)}
                onClick={() => onSelect(group.key)}
                className={`h-px rounded-full transition-all ${
                  active
                    ? "w-9 bg-slate-950"
                    : "w-7 bg-slate-300 hover:w-9 hover:bg-slate-700"
                }`}
              />
            );
          })}
        </div>

        <div className="pointer-events-none absolute right-10 top-1/2 w-80 -translate-y-1/2 opacity-0 transition duration-150 group-hover/anchors:pointer-events-auto group-hover/anchors:opacity-100">
          <div className="rounded-2xl border border-slate-200 bg-white/95 p-2.5 shadow-[0_24px_60px_-34px_rgba(15,23,42,0.75)] backdrop-blur">
            <div className="mb-2 flex items-center justify-between gap-2 px-1">
              <div className="text-[11px] font-semibold uppercase tracking-[0.16em] text-slate-400">
                历史问题
              </div>
              <div className="text-[11px] text-slate-400">{groups.length} 条</div>
            </div>
            <div className="max-h-80 space-y-1 overflow-auto pr-1">
              {groups.map((group, index) => {
                const active = group.key === activeGroupKey;
                return (
                  <button
                    key={group.key}
                    type="button"
                    onClick={() => onSelect(group.key)}
                    className={`w-full rounded-xl border px-3 py-2 text-left transition ${
                      active
                        ? "border-slate-900 bg-slate-950 text-white shadow-sm"
                        : "border-transparent bg-slate-50 text-slate-700 hover:border-slate-200 hover:bg-white"
                    }`}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className={`text-[10px] ${active ? "text-slate-300" : "text-slate-400"}`}>
                        Q{index + 1}
                      </span>
                      <span
                        className={`rounded-full border px-1.5 py-0.5 text-[10px] ${
                          active ? "border-white/20 text-slate-200" : statusStyle(group.status)
                        }`}
                      >
                        {group.status}
                      </span>
                    </div>
                    <div className="mt-1 line-clamp-2 text-xs leading-5">
                      {groupQueryText(group)}
                    </div>
                    <div className={`mt-1 text-[10px] ${active ? "text-slate-300" : "text-slate-400"}`}>
                      {formatDateTime(new Date(groupStartTime(group)).toISOString())}
                    </div>
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function selectActiveProcessGroup(groups: CollaborationGroup[]) {
  const userQuestionGroups = groups.filter((group) => isUserQuestionAnchorGroup(group));
  const candidates = userQuestionGroups.length > 0 ? userQuestionGroups : groups;
  return (
    candidates.find((item) =>
      ["pending", "dispatched", "running", "observed", "replied"].includes(item.status),
    ) || candidates[0]
  );
}

function isPeerCommunicationMode(mode?: string) {
  const normalized = (mode || "").trim().toLowerCase();
  return normalized === "peer_assisted" || normalized === "full_mesh";
}

function isLeaderMediatedCommunicationMode(mode?: string) {
  return (mode || "").trim().toLowerCase() === "leader_mediated";
}

function isRootTaskTargetLeader(
  group: CollaborationGroup | undefined,
  memberById: Map<number, TeamMember>,
  leaderMemberId?: string,
) {
  if (!group?.task) {
    return false;
  }
  const target = memberById.get(group.task.target_member_id);
  if (!target) {
    return false;
  }
  return (
    target.member_key === leaderMemberId ||
    target.member_key === "leader" ||
    target.role.toLowerCase().includes("leader")
  );
}

type ProcessStep = {
  id: string;
  workId?: string;
  actor: string;
  actorKey?: string;
  to: string;
  toKey?: string;
  eventType: string;
  status?: string;
  phase?: string;
  content: string;
  progress?: number;
  time: number;
  memberTerminalOnly?: boolean;
  assignmentResultOnly?: boolean;
  leaderMediatedRouteViolation?: boolean;
};

type KanbanColumnKey = "todo" | "doing" | "done";

type KanbanTaskCard = {
  id: string;
  column: KanbanColumnKey;
  title: string;
  summary: string;
  detail?: string;
  owner: string;
  target?: string;
  eventType: string;
  time: number;
  progress?: number;
  statusLabel: string;
};

type KanbanColumns = Record<KanbanColumnKey, KanbanTaskCard[]>;

type LeaderLedgerSummary = {
  phase: string;
  delivered: number;
  total: number;
  blockers: number;
  progress: number;
  deliveryLabel?: string;
  artifactLabel?: string;
};

type PeerLaneStatus = "idle" | "waiting" | "working" | "done" | "blocked";

type PeerCollaborationLane = {
  id: string;
  label: string;
  role: string;
  status: PeerLaneStatus;
  statusLabel: string;
  summary: string;
  currentTask?: string;
  deliverable?: string;
  waitingOn?: string;
  dependencies: string[];
  card?: KanbanTaskCard;
};

type PeerDependencyEdge = {
  id: string;
  from: string;
  to: string;
  label: string;
  status: PeerLaneStatus;
};

type PeerFlowEdge = {
  id: string;
  from: string;
  to: string;
  status: PeerLaneStatus;
};

type PeerCollaborationModel = {
  lanes: PeerCollaborationLane[];
  dependencies: PeerDependencyEdge[];
  flow: PeerFlowEdge[];
  phaseLabel: string;
  completionRule: string;
  stats: {
    waiting: number;
    working: number;
    completed: number;
    blocked: number;
  };
};

type DecompositionItem = {
  id: string;
  title: string;
  route: string;
  summary: string;
  status: string;
  badgeClass: string;
};

function buildProcessSteps(
  group: CollaborationGroup,
  memberById: Map<number, TeamMember>,
  memberByKey: Map<string, TeamMember>,
  leaderMemberId?: string,
): ProcessStep[] {
  const steps: ProcessStep[] = [];
  if (group.task) {
    const target =
      memberById.get(group.task.target_member_id)?.member_key ||
      `#${group.task.target_member_id}`;
    steps.push({
      id: `task-dispatch-${group.task.id}`,
      actor: "ClawManager",
      actorKey: "ClawManager",
      to: displayMemberName(target, memberByKey, leaderMemberId),
      toKey: target,
      eventType: "task_assigned",
      content: taskPromptText(group.task) || taskTitleText(group.task),
      time: new Date(group.task.created_at).getTime(),
    });
  }

  for (const item of group.items) {
    if (isProtocolNoiseItem(item) || isAssignmentHeartbeatItem(item)) {
      continue;
    }
    const stepMeta = item.collaborationStep;
    const actorKey = payloadText(stepMeta, ["actor"]) || item.actor || item.from || "system";
    const targetKey = payloadText(stepMeta, ["target"]) || item.to;
    const actor = displayMemberName(actorKey, memberByKey, leaderMemberId);
    const to = targetKey ? displayMemberName(targetKey, memberByKey, leaderMemberId) : "";
    const stepType = payloadText(stepMeta, ["type"]) || item.eventType;
    const stepStatus = payloadText(stepMeta, ["status"]);
    const stepTitle = payloadText(stepMeta, ["title"]);
    const terminalResult = isTerminalResultEventType(item.eventType) || stepType === "result" || stepType === "blocker";
    const stepSummary = terminalResult
      ? payloadText(stepMeta, ["content", "detail", "summary"])
      : payloadText(stepMeta, ["summary", "content", "detail"]);
    steps.push({
      id: `event-step-${item.event.id}`,
      workId: payloadText(stepMeta, ["workId", "work_id", "id"]),
      actor,
      actorKey,
      to,
      toKey: targetKey,
      eventType: stepType,
      status: stepStatus,
      phase: payloadText(stepMeta, ["phase"]),
      content: stepSummary || item.content || stepTitle || chatFallbackText(item, payloadNumber(item.payload, ["progress"]), payloadText(item.payload, ["status"])),
      progress: payloadNumber(stepMeta, ["progress"]) || payloadNumber(item.payload, ["progress"]),
      time: item.timeMs,
      memberTerminalOnly: payloadBool(item.payload, ["memberTerminalOnly", "member_terminal_only"]) === true,
      assignmentResultOnly: payloadBool(item.payload, ["assignmentResultOnly", "assignment_result_only"]) === true,
      leaderMediatedRouteViolation: payloadBool(item.payload, ["leaderMediatedRouteViolation", "leader_mediated_route_violation"]) === true,
    });
  }

  return steps
    .filter((step) => Number.isFinite(step.time))
    .sort((a, b) => a.time - b.time || a.id.localeCompare(b.id));
}

function isProtocolNoiseItem(item: CollaborationItem) {
  const normalizedContent = item.content.trim().toLowerCase();
  const compact = normalizedContent.replace(/\s+/g, "");
  const visibleRaw = payloadText(item.payload, ["visibleToChat", "visible_to_chat"]).toLowerCase();
  const explicitlyHidden = ["false", "0", "no", "off"].includes(visibleRaw);
  const chatPolicy = payloadText(item.payload, ["chatPolicy", "chat_policy"]).toLowerCase();
  if (isBusinessChatItem(item)) {
    return false;
  }
  return (
    chatPolicy === "hidden" ||
    (explicitlyHidden && !["visible", "replaceable", "warning"].includes(chatPolicy)) ||
    item.eventType === "member_result_confirmed" ||
    item.eventType === "inbound" ||
    normalizedContent === "inbound" ||
    compact === "teammessage" ||
    compact === "任务下发teammessage" ||
    normalizedContent === "redis team task completed" ||
    normalizedContent === "redis team task processing completed" ||
    normalizedContent === "redis team task failed"
  );
}

function hasMeaningfulChatBody(item: CollaborationItem) {
  const body = item.content.trim().toLowerCase().replace(/\s+/g, " ");
  return Boolean(body) && ![
    "inbound",
    "task_received",
    "task_started",
    "redis team task received",
    "redis team task started",
    "redis team task processing completed",
    "agent turn is still running",
    "still running",
    "status unchanged",
    "no change",
  ].includes(body);
}

function isBusinessChatItem(item: CollaborationItem) {
  const eventKind = payloadText(item.payload, ["eventKind", "event_kind", "kind"]).toLowerCase();
  const businessKinds = new Set([
    "leader_plan", "worker_plan", "worker_progress", "leader_synthesis", "leader_synthesis_reminder", "leader_decision_reminder",
    "agent_narrative", "agent_plan", "agent_assignment", "agent_handoff", "agent_progress", "agent_delivery", "agent_review", "agent_synthesis",
    "completion_deferred", "completion_candidate", "completion_validation_warning", "assignment_recovery_started", "assignment_reissued", "assignment_recovery_exhausted",
  ]);
  if (businessKinds.has(eventKind)) {
    return hasMeaningfulChatBody(item);
  }
  if (payloadBool(item.payload, ["leaderDispatchOnly", "leader_dispatch_only", "assignmentResultOnly", "assignment_result_only"]) === true) {
    return hasMeaningfulChatBody(item);
  }
  return [
    "outbound", "team_send", "task_assigned", "peer_request", "peer_handoff", "peer_review_request", "peer_reply", "reply", "completion_proposed", "task_completed", "completion", "message_warning", "task_progress", "progress",
  ].includes(item.eventType) && hasMeaningfulChatBody(item);
}

function buildKanbanColumns(
  group: CollaborationGroup | undefined,
  steps: ProcessStep[],
  finalResult: string,
  visualStatus: string,
): KanbanColumns {
  const columns: KanbanColumns = { todo: [], doing: [], done: [] };
  if (!group) {
    return columns;
  }
  const cardByWorkKey = new Map<string, KanbanTaskCard>();
  const delegatedTargets = steps
    .filter((step) =>
      (step.eventType === "task_assigned" || step.eventType === "outbound" || step.eventType === "team_send" || step.eventType === "assignment") &&
      step.to &&
      !isLeaderLikeName(step.to),
    )
    .map((step) => step.to);

  for (const step of steps) {
    if (isDispatchOnlyLeaderTerminalStep(step) || step.leaderMediatedRouteViolation) {
      continue;
    }
    const workKey = kanbanWorkKey(step, delegatedTargets, steps);
    const previous = cardByWorkKey.get(workKey);
    const column = kanbanColumnForStep(step, visualStatus, steps);
    const stepStatus = (step.status || "").toLowerCase();
    const successfulStep = ["succeeded", "success", "completed", "complete", "done", "finished", "ok"].includes(stepStatus);
    const card: KanbanTaskCard = {
      id: previous?.id || `kanban-${workKey}`,
      column,
      title: kanbanStepTitle(step, previous),
      summary: step.content,
      owner: step.actor,
      target: step.to,
      eventType: step.eventType,
      time: step.time,
      progress: step.progress,
      statusLabel: successfulStep
        ? "已完成"
        : eventVerb(step.eventType),
    };
    if (!previous || step.time >= previous.time) {
      cardByWorkKey.set(workKey, card);
    }
  }

  for (const card of cardByWorkKey.values()) {
    columns[card.column].push(card);
  }

  if (finalResult) {
    columns.done.push({
      id: "kanban-final-result",
      column: "done",
      title: "汇总总任务结果",
      summary: finalResult,
      owner: "Leader",
      eventType: visualStatus === "failed" ? "task_failed" : "task_completed",
      time: Math.max(...steps.map((step) => step.time), Date.now()),
      progress: visualStatus === "failed" ? undefined : 100,
      statusLabel: visualStatus === "failed" ? "失败汇总" : "最终汇总",
    });
  }

  (Object.keys(columns) as KanbanColumnKey[]).forEach((key) => {
    columns[key] = columns[key]
      .filter((card, index, list) => list.findIndex((item) => item.id === card.id) === index)
      .sort((a, b) => a.time - b.time || a.id.localeCompare(b.id));
  });

  return columns;
}

function kanbanWorkKey(step: ProcessStep, delegatedTargets: string[] = [], steps: ProcessStep[] = []) {
  if (step.workId) {
    return sanitizeKanbanKey(step.workId);
  }
  if (step.eventType === "assignment" || step.eventType === "outbound" || step.eventType === "task_assigned") {
    return sanitizeKanbanKey(step.to || step.actor || "assignment");
  }
  if ((isCompletionEvidenceStep(step, steps) || isFailureEvidenceStep(step)) && isLeaderLikeName(step.actor)) {
    const target = delegatedTargets.find((candidate) =>
      mentionsDelegatedTarget(step.content, new Set([candidate])),
    );
    if (target) {
      return sanitizeKanbanKey(target);
    }
  }
  return sanitizeKanbanKey(step.actor || step.to || step.id);
}

function sanitizeKanbanKey(value: string) {
  return value.toLowerCase().replace(/[^a-z0-9\u4e00-\u9fa5]+/gi, "-").replace(/^-+|-+$/g, "") || "task";
}

function isTerminalEventType(eventType: string) {
  return [
    "result",
    "task_completed",
    "completion",
    "blocker",
    "task_failed",
    "message_failed",
    "task_stale",
  ].includes(eventType);
}

function kanbanColumnForStep(step: ProcessStep, visualStatus: string, steps: ProcessStep[] = []): KanbanColumnKey {
  if (["succeeded", "success", "completed", "complete", "done", "finished", "ok", "failed", "failure", "error", "blocked", "stale"].includes((step.status || "").toLowerCase())) {
    return "done";
  }
  if (isCompletionEvidenceStep(step, steps) || isFailureEvidenceStep(step)) {
    return "done";
  }
  if (step.eventType === "assignment" || step.eventType === "peer_request" || step.eventType === "peer_handoff" || step.eventType === "peer_review_request") {
    return "todo";
  }
  if (step.eventType === "peer_reply" || step.eventType === "ack") {
    return "doing";
  }
  if (step.eventType === "reply") {
    return "doing";
  }
  if (
    step.eventType === "task_received" ||
    step.eventType === "task_started" ||
    step.eventType === "progress" ||
    step.eventType === "task_progress"
  ) {
    return "doing";
  }
  if (visualStatus === "running" && step.eventType !== "task_assigned" && step.eventType !== "outbound") {
    return "doing";
  }
  return "todo";
}

function buildWorkItemKanbanColumns(
  workItems: TeamWorkItem[],
  memberById: Map<number, TeamMember>,
): KanbanColumns {
  const columns: KanbanColumns = { todo: [], doing: [], done: [] };
  for (const item of workItems) {
    if (isHeartbeatWorkItem(item)) {
      continue;
    }
    const column: KanbanColumnKey =
      item.status === "succeeded" || item.status === "failed" || item.status === "stale"
        ? "done"
        : item.status === "running"
          ? "doing"
          : "todo";
    const owner = item.owner_member_id
      ? memberById.get(item.owner_member_id)?.display_name || memberById.get(item.owner_member_id)?.member_key || "未分配"
      : "未分配";
    const resultSummary = item.result
      ? payloadTextDeep(item.result, ["summary", "title", "status"])
      : "";
    const resultDetail = item.result
      ? payloadTextDeep(item.result, [
          "resultMarkdown",
          "result_markdown",
          "result",
          "answer",
          "text",
          "message",
          "summary",
        ])
      : "";
    columns[column].push({
      id: `work-item-${item.id}`,
      column,
      title: item.title,
      summary: resultSummary || resultDetail || (item.depends_on?.length ? `等待：${item.depends_on.join("、")}` : item.title),
      detail: resultDetail || resultSummary,
      owner,
      eventType:
        item.status === "succeeded"
          ? "task_completed"
          : item.status === "failed" || item.status === "stale"
            ? "task_failed"
            : item.status === "running"
              ? "task_progress"
              : "task_assigned",
      time: new Date(item.updated_at).getTime(),
      progress: item.status === "succeeded" ? 100 : item.status === "running" ? 50 : undefined,
      statusLabel:
        item.status === "succeeded"
          ? "已完成"
          : item.status === "failed"
            ? "失败"
            : item.status === "stale"
              ? "超时"
              : item.status === "running"
                ? "执行中"
                : item.status === "dispatched"
                  ? "已分派"
                  : "待分派",
    });
  }
  (Object.keys(columns) as KanbanColumnKey[]).forEach((key) => {
    columns[key].sort((a, b) => a.time - b.time || a.id.localeCompare(b.id));
  });
  return columns;
}

function isTerminalWorkItem(item: TeamWorkItem) {
  return item.status === "succeeded" || item.status === "failed" || item.status === "stale";
}

function isHeartbeatWorkItem(item: TeamWorkItem) {
  const text = `${item.work_id} ${item.title}`.toLowerCase();
  return text.includes("assignment_heartbeat") ||
    text.includes("heartbeat") ||
    text.includes("运行心跳");
}

function isLeaderWorkItem(item: TeamWorkItem, memberById: Map<number, TeamMember>) {
  if (item.work_id === "leader-final-synthesis" || item.work_id.startsWith("assignment-leader-")) {
    return true;
  }
  const member = item.owner_member_id ? memberById.get(item.owner_member_id) : undefined;
  const key = (member?.member_key || "").toLowerCase();
  const role = (member?.role || "").toLowerCase();
  return key === "leader" || role === "leader" || role.includes("leader");
}

function collapseLeaderMediatedWorkItems(
  workItems: TeamWorkItem[],
  memberById: Map<number, TeamMember>,
  rootStatus?: TeamTask["status"],
) {
  let hasLeaderFinal = rootStatus === "succeeded" || rootStatus === "failed" || rootStatus === "stale";
  const latestByCurrentSlot = new Map<string, TeamWorkItem>();
  for (const item of workItems) {
    if (item.superseded_by) {
      continue;
    }
    if (item.work_id === "leader-final-synthesis" && isTerminalWorkItem(item)) {
      hasLeaderFinal = true;
    }
    const slotKey = isLeaderWorkItem(item, memberById)
      ? `leader:${item.work_id}`
      : item.owner_member_id
        ? `owner:${item.owner_member_id}`
        : `work:${item.assignment_id || item.canonical_work_id || item.work_id}`;
    const previous = latestByCurrentSlot.get(slotKey);
    if (
      !previous ||
      new Date(item.updated_at).getTime() > new Date(previous.updated_at).getTime() ||
      (item.updated_at === previous.updated_at && item.id > previous.id)
    ) {
      latestByCurrentSlot.set(slotKey, item);
    }
  }
  return Array.from(latestByCurrentSlot.values()).filter((item) => {
    if (!isTerminalWorkItem(item) && hasLeaderFinal && isLeaderWorkItem(item, memberById)) {
      return false;
    }
    if (!isTerminalWorkItem(item) && item.work_id.startsWith("assignment-leader-")) {
      return false;
    }
    return true;
  });
}

function buildLeaderMediatedKanbanColumns(
  group: CollaborationGroup | undefined,
  workItems: TeamWorkItem[],
  memberById: Map<number, TeamMember>,
  finalResult: string,
  controlPlaneTask: boolean,
): KanbanColumns {
  if (workItems.length > 0) {
    return buildWorkItemKanbanColumns(collapseLeaderMediatedWorkItems(workItems, memberById, group?.task?.status), memberById);
  }
  const columns: KanbanColumns = { todo: [], doing: [], done: [] };
  const task = group?.task;
  if (!task) {
    return columns;
  }
  const terminal = task.status === "succeeded" || task.status === "failed" || task.status === "stale";
  const column: KanbanColumnKey = terminal ? "done" : task.status === "pending" ? "todo" : "doing";
  const detail =
    finalResult ||
    payloadTextDeep(task.result, ["resultMarkdown", "result_markdown", "result", "answer", "summary"]) ||
    payloadTextDeep(task.payload, ["resultMarkdown", "result_markdown", "result", "answer", "summary"]);
  const title = controlPlaneTask
    ? "控制面快照"
    : terminal
      ? "Root 任务终态"
      : "等待协作台账";
  const summary = detail ||
    (controlPlaneTask
      ? task.status === "succeeded"
        ? "团队介绍和 bootstrap 快照已生成。"
        : task.status === "failed" || task.status === "stale"
          ? task.error_message || "控制面快照未完成。"
          : "Leader 正在生成团队介绍，不需要派发成员任务。"
      : task.status === "succeeded"
        ? "Root 任务已由后端标记完成。"
        : task.status === "failed" || task.status === "stale"
          ? task.error_message || "Root 任务未完成。"
          : "后端尚未收到 assignment ledger；不会根据 raw events 推断进度。");
  columns[column].push({
    id: `leader-root-${task.id}`,
    column,
    title,
    summary,
    detail,
    owner: "Leader",
    eventType: task.status === "failed" || task.status === "stale"
      ? "task_failed"
      : task.status === "succeeded"
        ? "task_completed"
        : "task_progress",
    time: new Date(task.updated_at || task.created_at).getTime(),
    progress: task.status === "succeeded" ? 100 : undefined,
    statusLabel:
      task.status === "succeeded"
        ? "已完成"
        : task.status === "failed"
          ? "失败"
          : task.status === "stale"
            ? "超时"
            : controlPlaneTask
              ? "生成中"
              : "等待台账",
  });
  return columns;
}

function summarizeLeaderWorkItems(workItems: TeamWorkItem[], rootStatus?: TeamTask["status"], controlPlaneTask = false): LeaderLedgerSummary {
  if (workItems.length === 0) {
    if (controlPlaneTask) {
      const completed = rootStatus === "succeeded";
      const blocked = rootStatus === "failed" || rootStatus === "stale";
      return {
        phase: completed ? "已完成" : blocked ? "需要修复" : "Leader 执行中",
        delivered: completed ? 1 : 0,
        total: 0,
        blockers: blocked ? 1 : 0,
        progress: completed ? 100 : blocked ? 35 : 45,
        deliveryLabel: "成员交付 无需派发",
        artifactLabel: completed ? "产物 已生成" : blocked ? "产物 未通过" : "产物 等待生成",
      };
    }
    const completed = rootStatus === "succeeded";
    const blocked = rootStatus === "failed" || rootStatus === "stale";
    return {
      phase: completed ? "已完成" : blocked ? "需要修复" : "等待权威状态",
      delivered: 0,
      total: 0,
      blockers: blocked ? 1 : 0,
      progress: completed ? 100 : blocked ? 20 : 12,
      deliveryLabel: "成员交付 等待台账",
      artifactLabel: completed ? "产物 已记录" : blocked ? "产物 未通过" : "产物 未确认",
    };
  }
  const memberItems = workItems.filter((item) => item.owner_member_id && item.work_id !== "leader-final-synthesis");
  const owners = new Map<number, TeamWorkItem[]>();
  for (const item of memberItems) {
    if (!item.owner_member_id) {
      continue;
    }
    const list = owners.get(item.owner_member_id) || [];
    list.push(item);
    owners.set(item.owner_member_id, list);
  }
  let delivered = 0;
  let blockers = 0;
  for (const list of owners.values()) {
    if (list.some((item) => item.status === "failed" || item.status === "stale")) {
      blockers += 1;
      continue;
    }
    if (list.some((item) => item.status === "succeeded")) {
      delivered += 1;
    }
  }
  const total = owners.size;
  const leaderFinal = workItems.some((item) => item.work_id === "leader-final-synthesis" && item.status === "succeeded");
  let phase = "Leader 规划中";
  if (rootStatus === "succeeded" || leaderFinal) {
    phase = "已完成";
  } else if (blockers > 0) {
    phase = "需要修复";
  } else if (total > 0 && delivered >= total) {
    phase = "等待 Leader 汇总";
  } else if (total > 0) {
    phase = "等待成员交付";
  }
  const progress =
    rootStatus === "succeeded" || leaderFinal
      ? 100
      : total > 0
        ? Math.max(12, Math.min(90, Math.round(24 + (delivered / total) * 56 + (blockers > 0 ? 10 : 0))))
        : 12;
  return { phase, delivered, total, blockers, progress };
}

function buildPeerCollaborationModel(
  group: CollaborationGroup | undefined,
  steps: ProcessStep[],
  memberById: Map<number, TeamMember>,
  leaderMemberId?: string,
): PeerCollaborationModel {
  const memberByKey = new Map(
    [...memberById.values()].map((member) => [member.member_key, member]),
  );
  const lanes = new Map<string, PeerCollaborationLane>();
  const dependencies: PeerDependencyEdge[] = [];
  const addDependency = (
    fromKey: string,
    toKey: string,
    step: ProcessStep,
    status: PeerLaneStatus,
  ) => {
    if (!fromKey || !toKey || fromKey === toKey || fromKey === "ClawManager") {
      return;
    }
    const id = `${fromKey}->${toKey}-${step.id}`;
    dependencies.push({
      id,
      from: displayMemberName(fromKey, memberByKey, leaderMemberId),
      to: displayMemberName(toKey, memberByKey, leaderMemberId),
      label: step.content || eventVerb(step.eventType),
      status,
    });
  };
  const ensureLane = (memberKey: string) => {
    const normalized = memberKey || "system";
    const member = memberByKey.get(normalized);
    const existing = lanes.get(normalized);
    if (existing) {
      return existing;
    }
    const lane: PeerCollaborationLane = {
      id: normalized,
      label: displayMemberName(normalized, memberByKey, leaderMemberId),
      role: member?.role || "",
      status: member?.status === "busy" ? "working" : "idle",
      statusLabel: member?.status === "busy" ? "执行中" : "空闲",
      summary: member?.last_summary || "等待协作任务。",
      currentTask: member?.last_summary || "",
      deliverable: "",
      dependencies: [],
    };
    lanes.set(normalized, lane);
    return lane;
  };

  [...memberById.values()]
    .sort((a, b) => Number(b.member_key === leaderMemberId || b.role.toLowerCase().includes("leader")) - Number(a.member_key === leaderMemberId || a.role.toLowerCase().includes("leader")) || a.id - b.id)
    .forEach((member) => ensureLane(member.member_key));

  const setLaneCard = (
    lane: PeerCollaborationLane,
    step: ProcessStep,
    status: PeerLaneStatus,
    statusLabel: string,
    title: string,
  ) => {
    if (lane.status === "done" && status !== "blocked") {
      return;
    }
    if (lane.status === "blocked" && status !== "done") {
      return;
    }
    lane.status = status;
    lane.statusLabel = statusLabel;
    lane.summary = step.content || title;
    if (status === "working" || status === "waiting") {
      lane.currentTask = step.content || title;
    }
    if (status === "done") {
      lane.deliverable = step.content || title;
    }
    lane.card = {
      id: `peer-lane-${lane.id}`,
      column: status === "done" ? "done" : status === "idle" ? "todo" : "doing",
      title,
      summary: lane.summary,
      owner: lane.label,
      target: step.to,
      eventType: step.eventType,
      time: step.time,
      progress: status === "done" ? 100 : step.progress,
      statusLabel,
    };
  };

  for (const step of steps) {
    const actorKey = step.actorKey || step.actor;
    const targetKey = step.toKey || "";
    if (targetKey && !isLeaderLikeName(targetKey)) {
      ensureLane(targetKey);
    }
    if (actorKey && actorKey !== "ClawManager" && !isLeaderLikeName(actorKey)) {
      ensureLane(actorKey);
    }

    if (["assignment", "task_assigned", "outbound", "peer_handoff", "peer_request", "peer_review_request"].includes(step.eventType)) {
      if (targetKey) {
        const targetLane = ensureLane(targetKey);
        if (actorKey && actorKey !== "ClawManager") {
          const dependencyLabel = displayMemberName(actorKey, memberByKey, leaderMemberId);
          if (!targetLane.dependencies.includes(dependencyLabel)) {
            targetLane.dependencies.push(dependencyLabel);
          }
        }
        setLaneCard(targetLane, step, "waiting", "待响应", `${targetLane.label} 收到协作请求`);
        addDependency(actorKey, targetKey, step, "waiting");
      }
      if (actorKey && !isLeaderLikeName(actorKey) && targetKey) {
        const actorLane = ensureLane(actorKey);
        actorLane.waitingOn = displayMemberName(targetKey, memberByKey, leaderMemberId);
        setLaneCard(actorLane, step, "working", "等待协作", `${actorLane.label} 等待 ${actorLane.waitingOn}`);
      }
      continue;
    }

    if (isFailureEvidenceStep(step)) {
      const lane = ensureLane(actorKey);
      setLaneCard(lane, step, "blocked", "阻塞", `${lane.label} 遇到阻塞`);
      continue;
    }

    if (isCompletionEvidenceStep(step, steps)) {
      const lane = ensureLane(actorKey);
      setLaneCard(lane, step, "done", step.memberTerminalOnly ? "成员交付" : "已交付", `${lane.label} 交付结果`);
      continue;
    }

    if (["task_received", "task_started", "progress", "task_progress", "reply", "peer_reply", "ack"].includes(step.eventType)) {
      const lane = ensureLane(actorKey);
      setLaneCard(lane, step, "working", "进行中", `${lane.label} 更新进展`);
    }
  }

  if (group?.task) {
    const target = memberById.get(group.task.target_member_id);
    if (target) {
      ensureLane(target.member_key);
    }
  }

  const laneList = [...lanes.values()];
  const stats = {
    waiting: laneList.filter((lane) => lane.status === "waiting").length,
    working: laneList.filter((lane) => lane.status === "working").length,
    completed: laneList.filter((lane) => lane.status === "done").length,
    blocked: laneList.filter((lane) => lane.status === "blocked").length,
  };
  const rootCompleted = Boolean(latestRootCompletionEvidenceStep(steps, true));
  const phaseLabel = rootCompleted
    ? "Leader 最终汇总完成"
    : stats.blocked > 0
      ? "存在阻塞等待处理"
      : stats.working > 0
        ? "成员协作执行中"
        : stats.waiting > 0
          ? "等待成员响应"
          : stats.completed > 0
            ? "等待 Leader 汇总"
            : "等待任务拆解";

  return {
    lanes: laneList,
    dependencies: dependencies.slice(-8),
    flow: compactPeerFlow(dependencies),
    phaseLabel,
    completionRule: "只有 Leader 最终汇总并关闭根任务后，整体进度才进入 100%。成员交付只更新对应泳道。",
    stats,
  };
}

function compactPeerFlow(dependencies: PeerDependencyEdge[]): PeerFlowEdge[] {
  const deduped = new Map<string, PeerFlowEdge>();
  for (const edge of dependencies) {
    const key = `${edge.from}->${edge.to}`;
    deduped.set(key, {
      id: key,
      from: edge.from,
      to: edge.to,
      status: edge.status,
    });
  }
  return [...deduped.values()].slice(-6);
}

function peerProgressStats(steps: ProcessStep[]) {
  const touched = new Set<string>();
  const completed = new Set<string>();
  for (const step of steps) {
    const actor = step.actorKey || step.actor;
    const target = step.toKey || "";
    if (target && !isLeaderLikeName(target)) {
      touched.add(target);
    }
    if (actor && actor !== "ClawManager" && !isLeaderLikeName(actor)) {
      touched.add(actor);
      if (isCompletionEvidenceStep(step, steps)) {
        completed.add(actor);
      }
    }
  }
  return { total: touched.size, completed: completed.size };
}

function kanbanStepTitle(step: ProcessStep, previous?: KanbanTaskCard) {
  if (previous && isTerminalEventType(step.eventType)) {
    return `${previous.target || previous.owner} 反馈结果`;
  }
  switch (step.eventType) {
    case "assignment":
      return step.to ? `拆解给 ${step.to}` : "拆解子任务";
    case "peer_request":
      return step.to ? `${step.actor} 请求 ${step.to} 协作` : "协作请求";
    case "peer_handoff":
      return step.to ? `${step.actor} 交接给 ${step.to}` : "协作交接";
    case "peer_review_request":
      return step.to ? `${step.actor} 请求 ${step.to} 评审` : "评审请求";
    case "peer_reply":
      return `${step.actor} 协作反馈`;
    case "ack":
      return `${step.actor} 接收任务`;
    case "result":
      return `${step.actor} 交付结果`;
    case "blocker":
      return `${step.actor} 遇到阻塞`;
    case "outbound":
    case "task_assigned":
      return step.to ? `拆解给 ${step.to}` : "拆解子任务";
    case "task_received":
      return `${step.actor} 领取任务`;
    case "task_started":
      return `${step.actor} 开始执行`;
    case "progress":
    case "task_progress":
      return `${step.actor} 更新进展`;
    case "reply":
    case "completion":
      return `${step.actor} 反馈结果`;
    case "task_completed":
      return `${step.actor} 完成任务`;
    case "task_failed":
    case "message_failed":
      return `${step.actor} 执行失败`;
    case "task_stale":
      return "任务超时";
    default:
      return previous?.title || eventVerb(step.eventType);
  }
}

function buildDecompositionItems(columns: KanbanColumns): DecompositionItem[] {
  const cards = [...columns.todo, ...columns.doing, ...columns.done].filter(
    (card) => card.id !== "kanban-final-result",
  );
  return cards.slice(0, 5).map((card) => ({
    id: card.id,
    title: card.title,
    route: card.target && card.target !== card.owner ? `${card.owner} → ${card.target}` : "",
    summary: card.summary,
    status: card.statusLabel,
    badgeClass: kanbanCardStyle(card).badge,
  }));
}

function buildPeerDecompositionItems(lanes: PeerCollaborationLane[]): DecompositionItem[] {
  return lanes
    .filter((lane) => lane.card || lane.status !== "idle")
    .slice(0, 6)
    .map((lane) => ({
      id: lane.id,
      title: `${lane.label}${lane.role ? ` (${lane.role})` : ""}`,
      route: lane.waitingOn ? `等待 ${lane.waitingOn}` : "",
      summary: lane.summary,
      status: lane.statusLabel,
      badgeClass: peerLaneStatusClass(lane.status),
    }));
}

function kanbanDetailSizeForText(value: string): KanbanDetailSize {
  const normalized = value.trim();
  if (!normalized) {
    return "short";
  }
  const lineCount = normalized.split(/\r?\n/).filter((line) => line.trim()).length;
  const weightedLength = normalized.length + lineCount * 44;
  if (weightedLength > 760) {
    return "long";
  }
  if (weightedLength > 220) {
    return "medium";
  }
  return "short";
}

function leaderKanbanWorkspaceSize(
  detailSize: KanbanDetailSize,
  decompositionCount: number,
  columns: Record<KanbanColumnKey, KanbanTaskCard[]>,
): KanbanDetailSize {
  const largestColumn = Math.max(columns.todo.length, columns.doing.length, columns.done.length);
  if (detailSize === "long" || decompositionCount > 4 || largestColumn > 4) {
    return "long";
  }
  if (detailSize === "medium" || decompositionCount > 2 || largestColumn > 2) {
    return "medium";
  }
  return "short";
}

function kanbanDetailPanelMaxHeight(size: KanbanDetailSize) {
  switch (size) {
    case "long":
      return "max-h-[520px]";
    case "medium":
      return "max-h-[360px]";
    default:
      return "max-h-[180px]";
  }
}

function kanbanDetailBodyMaxHeight(size: KanbanDetailSize) {
  switch (size) {
    case "long":
      return "max-h-[360px]";
    case "medium":
      return "max-h-[220px]";
    default:
      return "max-h-[88px]";
  }
}

function processProgress(
  group: CollaborationGroup,
  steps: ProcessStep[],
  visualStatus = group.status,
  peerRoot = false,
) {
  if (visualStatus === "succeeded") {
    return 100;
  }
  if (visualStatus === "failed" || visualStatus === "stale") {
    return 92;
  }
  const explicit = stepsProgress(steps);
  if (explicit > 0) {
    return Math.min(explicit, 88);
  }
  if (peerRoot) {
    const laneStats = peerProgressStats(steps);
    if (laneStats.total > 0) {
      const base = laneStats.completed > 0 ? 42 : 28;
      const weighted = Math.round(base + (laneStats.completed / laneStats.total) * 42);
      return Math.min(weighted, 88);
    }
  }
  if (hasWorkerContentEvidence(steps)) {
    return 82;
  }
  if (visualStatus === "running") {
    return 66;
  }
  if (visualStatus === "dispatched" || visualStatus === "replied") {
    return 38;
  }
  if (group.status === "running") {
    return 58;
  }
  if (group.status === "dispatched" || group.status === "replied") {
    return 34;
  }
  return steps.length > 0 ? 24 : 0;
}

function stepsProgress(steps: ProcessStep[]) {
  return steps.reduce(
    (max, step) =>
      isDispatchOnlyLeaderTerminalStep(step)
        ? max
        : Math.max(max, step.progress || 0),
    0,
  );
}

function isDispatchOnlyLeaderTerminalStep(step: ProcessStep) {
  return (isTerminalEventType(step.eventType) || step.eventType === "reply") &&
    isLeaderLikeName(step.actor) &&
    isDispatchOnlyResult(step.content);
}

function latestRootCompletionEvidenceStep(steps: ProcessStep[], peerRoot = false) {
  return [...steps]
    .reverse()
    .find((step) => isRootCompletionEvidenceStep(step, steps, peerRoot));
}

function latestOutcomeEvidence(steps: ProcessStep[]) {
  for (const step of [...steps].reverse()) {
    if (isCompletionEvidenceStep(step, steps)) {
      return { status: "succeeded" as const, step };
    }
    if (step.eventType === "task_stale") {
      return { status: "stale" as const, step };
    }
    if (isFailureEvidenceStep(step)) {
      return { status: "failed" as const, step };
    }
  }
  return undefined;
}

function isRootCompletionEvidenceStep(
  step: ProcessStep,
  steps: ProcessStep[] = [],
  peerRoot = false,
) {
  if (!isCompletionEvidenceStep(step, steps)) {
    return false;
  }
  if (step.memberTerminalOnly || step.assignmentResultOnly || step.leaderMediatedRouteViolation || isDispatchOnlyResult(step.content)) {
    return false;
  }
  if (!peerRoot) {
    return !hasDelegatedWorkBeforeStep(step, steps) || isLeaderLikeName(step.actor);
  }
  return isLeaderLikeName(step.actor);
}

function isCompletionEvidenceStep(step: ProcessStep, steps: ProcessStep[] = []) {
  if (!step.content || step.memberTerminalOnly || step.leaderMediatedRouteViolation || isDispatchOnlyResult(step.content) || isDispatchOnlyLeaderTerminalStep(step)) {
    return false;
  }
  if (step.assignmentResultOnly) {
    return false;
  }
  if (["succeeded", "success", "completed", "complete", "done", "finished", "ok"].includes((step.status || "").toLowerCase())) {
    return true;
  }
  if (isFinalResultText(step.content)) {
    return true;
  }
  if (step.eventType === "task_completed" || step.eventType === "completion") {
    return true;
  }
  if (step.eventType === "reply" && isSubstantiveFinalAnswerText(step.content)) {
    if (!isLeaderLikeName(step.actor)) {
      return false;
    }
    return !hasDelegatedWorkBeforeStep(step, steps) || hasNonLeaderCompletionBeforeStep(step, steps);
  }
  return false;
}

function hasDelegatedWorkBeforeStep(step: ProcessStep, steps: ProcessStep[]) {
  return steps.some(
    (candidate) =>
      candidate.time <= step.time &&
      candidate.to &&
      !isLeaderLikeName(candidate.to) &&
      (
        candidate.eventType === "task_assigned" ||
        candidate.eventType === "outbound" ||
        candidate.status === "dispatched" ||
        isDispatchOnlyResult(candidate.content)
      ),
  );
}

function hasNonLeaderCompletionBeforeStep(step: ProcessStep, steps: ProcessStep[]) {
  return steps.some(
    (candidate) =>
      candidate.time <= step.time &&
      !isLeaderLikeName(candidate.actor) &&
      !isDispatchOnlyLeaderTerminalStep(candidate) &&
      (isFinalResultText(candidate.content) ||
        candidate.eventType === "task_completed" ||
        candidate.eventType === "completion"),
  );
}

function isFailureEvidenceStep(step: ProcessStep) {
  if (["succeeded", "success", "completed", "complete", "done", "finished", "ok", "warning"].includes((step.status || "").toLowerCase())) {
    return false;
  }
  if (!step.content && step.eventType !== "task_failed" && step.eventType !== "message_failed") {
    return false;
  }
  if (step.content && isFinalResultText(step.content)) {
    return false;
  }
  if (step.eventType === "task_failed") {
    return step.content
      ? /error|failed|failure|exception|timeout|forbidden|失败|错误|异常|超时/.test(step.content.toLowerCase())
      : true;
  }
  if (step.eventType !== "message_failed") {
    return false;
  }
  const normalized = step.content.toLowerCase();
  return /error|failed|failure|exception|timeout|forbidden|失败|错误|异常|超时/.test(normalized);
}

function hasRuntimeActivityEvidence(steps: ProcessStep[]) {
  return steps.some((step) =>
    ["task_received", "task_started", "progress", "task_progress"].includes(step.eventType) ||
    (step.eventType === "reply" && !isDispatchOnlyLeaderTerminalStep(step)),
  );
}

function hasWorkerContentEvidence(steps: ProcessStep[]) {
  return steps.some((step) =>
    step.eventType === "reply" &&
    Boolean(step.content.trim()) &&
    !isLeaderLikeName(step.actor) &&
    !isFinalResultText(step.content),
  );
}

function isFinalResultText(value: string) {
  const normalized = value.trim().replace(/\s+/g, " ");
  const compact = normalized.replace(/\s+/g, "");
  if (!normalized || isDispatchOnlyResult(normalized)) {
    return false;
  }
  return (
    /\[DONE\]/i.test(normalized) ||
    /task completed/i.test(normalized) ||
    compact.includes("任务结果反馈") ||
    compact.includes("任务输出") ||
    compact.includes("查询完成") ||
    compact.startsWith("已完成") ||
    compact.includes("已完成任务") ||
    compact.includes("完成任务") ||
    compact.includes("完成管道交付")
  );
}

function isSubstantiveFinalAnswerText(value: string) {
  const normalized = value.trim().replace(/\s+/g, " ");
  const compact = normalized.replace(/\s+/g, "");
  if (!normalized || isDispatchOnlyResult(normalized)) {
    return false;
  }
  if (
    /^(收到|好的|好|ok|okay|处理中|正在|准备|等待|我将|让我|先看|稍等)/i.test(normalized) ||
    compact.includes("现在整理") ||
    compact.includes("正在整理") ||
    compact.includes("稍后") ||
    compact.includes("等待其") ||
    compact.includes("派单")
  ) {
    return false;
  }
  if (isFinalResultText(normalized)) {
    return true;
  }
  return compact.length >= 36 || /[#*>|`]|。|：|:/.test(normalized) && compact.length >= 24;
}

function processFinalResult(group: CollaborationGroup, steps: ProcessStep[] = [], peerRoot = false) {
  const rootCompletion = latestRootCompletionEvidenceStep(steps, peerRoot);
  const latestOutcome = latestOutcomeEvidence(steps);
  if (peerRoot && !rootCompletion) {
    return "";
  }
  if (group.task && group.task.status !== "succeeded" && !rootCompletion) {
    return "";
  }
  const finalStep =
    rootCompletion ||
    (latestOutcome?.status === "succeeded" &&
    !peerRoot &&
    (!hasDelegatedWorkBeforeStep(latestOutcome.step, steps) || isLeaderLikeName(latestOutcome.step.actor))
      ? latestOutcome.step
      : undefined);
  if (finalStep?.content) {
    return finalStep.content;
  }
  if (group.task) {
    const taskResult = taskResultText(group.task);
    if (taskResult && !isDispatchOnlyResult(taskResult) && (group.task.status === "succeeded" || isFinalResultText(taskResult))) {
      return taskResult;
    }
  }
  return "";
}

function processVisualStatus(
  group: CollaborationGroup,
  finalResult: string,
  steps: ProcessStep[] = [],
  peerRoot = false,
) {
  const rootCompletion = latestRootCompletionEvidenceStep(steps, peerRoot);
  const latestOutcome = latestOutcomeEvidence(steps);
  if (finalResult || rootCompletion) {
    return "succeeded";
  }
  if (!peerRoot && latestOutcome?.status === "succeeded" && !hasDelegatedWorkBeforeStep(latestOutcome.step, steps)) {
    return "succeeded";
  }
  if (group.task?.status === "succeeded" && !peerRoot && !isDispatchOnlyResult(taskResultText(group.task))) {
    return "succeeded";
  }
  if (group.task?.status === "failed") {
    return "failed";
  }
  if (group.task?.status === "stale") {
    return "stale";
  }
  if (group.task?.status === "running") {
    return "running";
  }
  if (group.task?.status === "dispatched") {
    return "dispatched";
  }
  if (latestOutcome?.status === "failed") {
    return "failed";
  }
  if (latestOutcome?.status === "stale" || group.status === "stale") {
    return "stale";
  }
  if (hasWorkerContentEvidence(steps) || hasRuntimeActivityEvidence(steps)) {
    return "running";
  }
  if (steps.some((step) => step.eventType === "task_assigned" || step.eventType === "outbound")) {
    return "dispatched";
  }
  return group.status === "succeeded" ? "running" : group.status;
}

function mentionsDelegatedTarget(content: string, targets: Set<string>) {
  const normalized = content.toLowerCase();
  for (const target of targets) {
    const compactTarget = target.toLowerCase();
    const memberHint = compactTarget.match(/\(([^)]+)\)/)?.[1] || compactTarget;
    if (normalized.includes(compactTarget) || normalized.includes(memberHint)) {
      return true;
    }
  }
  return false;
}

function isLeaderLikeName(value: string) {
  const normalized = value.toLowerCase();
  return normalized === "clawmanager" || normalized.includes("leader") || normalized.includes("(leader)");
}

function isDispatchOnlyResult(value: string) {
  const normalized = value.trim().replace(/\s+/g, "");
  const readable = value.trim().replace(/\s+/g, " ");
  const lower = readable.toLowerCase();
  if (!normalized) {
    return true;
  }
  const finalMarkers = [
    "任务结果反馈",
    "任务输出",
    "最终回答",
    "最终方案",
    "最终总结",
    "汇总如下",
    "结果如下",
    "已完成并",
    "产出摘要",
    "final answer",
    "final synthesis",
    "task result",
    "result summary",
  ];
  if (finalMarkers.some((marker) => normalized.includes(marker.replace(/\s+/g, "")) || lower.includes(marker))) {
    return false;
  }
  return (
    normalized === "结果已反馈。" ||
    normalized === "结果已反馈" ||
    normalized.toLowerCase() === "redisteamtaskcompleted" ||
    normalized.toLowerCase() === "redisteamtaskprocessingcompleted" ||
    normalized.toLowerCase() === "redisteamtaskfailed" ||
    normalized.includes("在线空闲，派单") ||
    normalized.includes("在线空闲,派单") ||
    normalized.includes("任务分派") ||
    normalized.includes("任务下发") ||
    normalized.includes("分派给") ||
    normalized.includes("派发给") ||
    normalized.includes("交给") ||
    normalized.includes("用户想让你") ||
    normalized.includes("$CLAWMANAGER_TEAM_SHARED_DIR") ||
    (normalized.includes("共享目录") && normalized.includes("规范路径")) ||
    (normalized.includes("完成后") && (normalized.includes("回传") || normalized.includes("返回给我") || normalized.includes("通知我") || normalized.includes("交付给我"))) ||
    normalized.includes("已派发") ||
    normalized.includes("等待其查询并交付结果") ||
    lower.includes("assigned to") ||
    lower.includes("handoff") ||
    (lower.includes("please ") &&
      (lower.includes("write") || lower.includes("complete") || lower.includes("return") || lower.includes("report back")) &&
      (lower.includes("designer") || lower.includes("pm") || lower.includes("architect") || lower.includes("worker")))
  );
}

function processStatusText(status: string) {
  switch (status) {
    case "idle":
      return "空闲";
    case "pending":
      return "等待调度";
    case "dispatched":
      return "已下发";
    case "running":
      return "执行中";
    case "replied":
      return "已有反馈";
    case "succeeded":
      return "已完成";
    case "failed":
      return "失败";
    case "stale":
      return "超时";
    default:
      return status || "观察中";
  }
}

function PeerCollaborationMatrix({
  model,
  queryText,
  statusText,
  visualStatus,
  progress,
  selectedCardId,
  onSelect,
}: {
  model?: PeerCollaborationModel;
  queryText: string;
  statusText: string;
  visualStatus: string;
  progress: number;
  selectedCardId?: string;
  onSelect: (cardId: string) => void;
}) {
  const lanes = model?.lanes || [];
  const visible = lanes.filter((lane) => lane.role || lane.card || lane.status !== "idle");
  const chain = model?.flow.length ? model.flow : model?.dependencies.map((edge) => ({
    id: edge.id,
    from: edge.from,
    to: edge.to,
    status: edge.status,
  })) || [];
  return (
    <div className="shrink-0 rounded-2xl border border-slate-200 bg-white p-2.5 shadow-sm">
      <div className="mb-2 rounded-xl border border-slate-100 bg-slate-50/80 px-2.5 py-2">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-slate-400">
                Root Query
              </span>
              <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusStyle(visualStatus)}`}>
                {statusText}
              </span>
              {model?.phaseLabel && (
                <span className="rounded-full bg-cyan-50 px-2 py-0.5 text-[10px] font-medium text-cyan-700">
                  {model.phaseLabel}
                </span>
              )}
            </div>
            <div className="mt-1 line-clamp-1 text-xs leading-5 text-slate-700">
              {queryText || "Idle，等待新的团队任务。"}
            </div>
            <div className="mt-1.5 flex min-w-0 flex-wrap items-center gap-1">
              {chain.length > 0 ? (
                chain.slice(0, 6).map((edge, index) => (
                  <React.Fragment key={edge.id}>
                    {index > 0 && <span className="text-[10px] text-slate-300">→</span>}
                    <span className={`max-w-[150px] truncate rounded-full px-2 py-0.5 text-[10px] ${peerLaneStatusClass(edge.status)}`}>
                      {edge.from} → {edge.to}
                    </span>
                  </React.Fragment>
                ))
              ) : (
                <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] text-slate-500">
                  等待 peer handoff
                </span>
              )}
            </div>
          </div>
          <div className="shrink-0 rounded-xl border border-white bg-white px-2.5 py-1.5 text-right shadow-sm">
            <div className="text-xl font-semibold leading-none text-slate-900">{progress}%</div>
            <div className="mt-0.5 text-[10px] uppercase tracking-[0.14em] text-slate-400">overall</div>
          </div>
        </div>
      </div>
      <div className="mb-2 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <div className="text-xs font-semibold text-slate-800">成员协作板</div>
          </div>
          <div className="mt-0.5 text-[11px] leading-4 text-slate-400">
            {model?.completionRule || "等待成员协作事件。"}
          </div>
        </div>
        <div className="grid shrink-0 grid-cols-4 gap-1 text-center text-[10px]">
          <span className="rounded-lg bg-amber-50 px-2 py-1 text-amber-700">{model?.stats.waiting || 0} 待</span>
          <span className="rounded-lg bg-sky-50 px-2 py-1 text-sky-700">{model?.stats.working || 0} 做</span>
          <span className="rounded-lg bg-emerald-50 px-2 py-1 text-emerald-700">{model?.stats.completed || 0} 交</span>
          <span className="rounded-lg bg-rose-50 px-2 py-1 text-rose-700">{model?.stats.blocked || 0} 阻</span>
        </div>
      </div>
      <div className="space-y-1.5">
        {visible.length === 0 ? (
          <div className="rounded-xl border border-dashed border-slate-200 bg-slate-50 px-3 py-4 text-center text-xs text-slate-400">
            暂无成员协作事件
          </div>
        ) : (
          visible.map((lane) => {
            const card = lane.card;
            const selected = card && selectedCardId === card.id;
            return (
              <button
                key={lane.id}
                type="button"
                disabled={!card}
                onClick={() => card && onSelect(card.id)}
                className={`grid min-w-0 grid-cols-[minmax(92px,0.85fr)_minmax(0,1.45fr)_minmax(0,1.1fr)] gap-2 rounded-xl border px-2 py-1.5 text-left transition ${
                  selected
                    ? "border-slate-500 bg-slate-50 shadow-sm"
                    : "border-slate-200 bg-white hover:border-slate-300"
                } ${card ? "" : "cursor-default"}`}
              >
                <div className="min-w-0">
                  <div className="min-w-0">
                    <div className="truncate text-xs font-semibold text-slate-900">{lane.label}</div>
                    <div className="mt-0.5 truncate text-[10px] text-slate-400">{lane.role || "member"}</div>
                  </div>
                  <span className={`mt-1 inline-flex rounded-full px-2 py-0.5 text-[10px] font-medium ${peerLaneStatusClass(lane.status)}`}>
                    {lane.statusLabel}
                  </span>
                </div>
                <div className="min-w-0">
                  <div className="text-[10px] font-medium text-slate-400">当前任务</div>
                  <div className="mt-0.5 line-clamp-1 text-[11px] leading-4 text-slate-600">
                    {lane.currentTask || lane.summary || "空闲，等待任务。"}
                  </div>
                  {(lane.waitingOn || lane.dependencies.length > 0) && (
                    <div className="mt-1 flex flex-wrap gap-1">
                      {lane.waitingOn && (
                        <span className="rounded-full bg-amber-50 px-2 py-0.5 text-[10px] text-amber-700">
                          等待 {lane.waitingOn}
                        </span>
                      )}
                      {lane.dependencies.slice(0, 2).map((dependency) => (
                        <span key={`${lane.id}-${dependency}`} className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] text-slate-500">
                          来自 {dependency}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
                <div className="min-w-0">
                  <div className="text-[10px] font-medium text-slate-400">交付 / 证据</div>
                  <div className="mt-0.5 line-clamp-1 text-[11px] leading-4 text-slate-600">
                    {lane.deliverable || (lane.status === "done" ? lane.summary : "尚未正式交付")}
                  </div>
                </div>
              </button>
            );
          })
        )}
      </div>
    </div>
  );
}

function workflowStatusText(state?: string) {
  switch ((state || "").toLowerCase()) {
    case "planning":
      return "Leader 正在规划";
    case "executing":
      return "阶段执行中";
    case "awaiting_phase_results":
      return "等待本阶段交付";
    case "awaiting_leader_decision":
      return "等待 Leader 决定下一阶段";
    case "synthesizing":
      return "Leader 正在汇总";
    case "completion_pending":
      return "等待完成确认";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    default:
      return "";
  }
}

function peerLaneStatusClass(status: PeerLaneStatus) {
  switch (status) {
    case "done":
      return "bg-emerald-100 text-emerald-700";
    case "blocked":
      return "bg-rose-100 text-rose-700";
    case "working":
      return "bg-sky-100 text-sky-700";
    case "waiting":
      return "bg-amber-100 text-amber-700";
    default:
      return "bg-slate-100 text-slate-500";
  }
}

function KanbanColumn({
  title,
  subtitle,
  cards,
  tone,
  selectedCardId,
  onSelect,
}: {
  title: string;
  subtitle: string;
  cards: KanbanTaskCard[];
  tone: KanbanColumnKey;
  selectedCardId?: string;
  onSelect: (id: string) => void;
}) {
  const style = kanbanColumnStyle(tone);
  return (
    <div className={`min-h-[138px] rounded-xl border p-1.5 shadow-[0_10px_26px_-22px_rgba(15,23,42,0.5)] transition-all duration-300 hover:-translate-y-0.5 hover:shadow-[0_16px_32px_-22px_rgba(15,23,42,0.48)] ${style.shell}`}>
      <div className="mb-1 flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            <span className={`h-1.5 w-1.5 rounded-full shadow-[0_0_8px_currentColor] ${style.dot}`} />
            <h3 className="text-[11px] font-semibold text-slate-900">{title}</h3>
          </div>
          <p className="mt-0.5 text-[10px] leading-3 text-slate-500">{subtitle}</p>
        </div>
        <span className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${style.count}`}>
          {cards.length}
        </span>
      </div>

      <div className="max-h-[230px] space-y-1 overflow-y-auto pr-1">
        {cards.length === 0 ? (
          <div className="rounded-lg border border-dashed border-slate-300/80 bg-white/55 px-2 py-2.5 text-center text-[11px] leading-5 text-slate-400 backdrop-blur-sm">
            暂无卡片
          </div>
        ) : (
          cards.map((card) => (
            <KanbanCard
              key={card.id}
              card={card}
              selected={selectedCardId === card.id}
              onSelect={() => onSelect(card.id)}
            />
          ))
        )}
      </div>
    </div>
  );
}

function KanbanCard({
  card,
  selected,
  onSelect,
}: {
  card: KanbanTaskCard;
  selected: boolean;
  onSelect: () => void;
}) {
  const style = kanbanCardStyle(card);
  return (
    <button
      type="button"
      onClick={onSelect}
      className={`group w-full rounded-lg border bg-[linear-gradient(145deg,rgba(255,255,255,0.96),rgba(241,245,249,0.9))] px-1.5 py-1 text-left text-xs shadow-[0_5px_14px_-12px_rgba(15,23,42,0.7)] transition duration-200 hover:-translate-y-0.5 hover:border-sky-300/80 hover:shadow-[0_10px_22px_-14px_rgba(14,116,144,0.5)] ${
        selected ? "border-sky-400/80 ring-2 ring-sky-200/70" : style.border
      }`}
    >
      <div className="flex items-start gap-1.5">
        <span className={`mt-1 h-5 w-1 shrink-0 rounded-full ${style.bar}`} />
        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-1.5">
            <div className="line-clamp-1 text-[11px] font-semibold leading-4 text-slate-900">
              {card.title}
            </div>
            {card.column === "doing" && (
              <span className="relative mt-1 flex h-2 w-2 shrink-0">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-sky-400 opacity-60" />
                <span className="relative inline-flex h-2 w-2 rounded-full bg-sky-500" />
              </span>
            )}
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-1">
            <span className={`rounded-full px-1.5 py-0.5 text-[10px] font-medium leading-none ${style.badge}`}>
              {card.statusLabel}
            </span>
            {card.progress !== undefined && (
              <span className="text-[10px] text-slate-400">{card.progress}%</span>
            )}
          </div>
        </div>
      </div>
    </button>
  );
}

function KanbanCardDetail({
  card,
  size,
  onWorkspaceFileOpen,
}: {
  card: KanbanTaskCard;
  size: KanbanDetailSize;
  onWorkspaceFileOpen?: (path: string) => void;
}) {
  const style = kanbanCardStyle(card);
  return (
    <div className="cm-tech-subtle flex min-h-0 flex-col rounded-xl p-2.5">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-xs font-semibold leading-5 text-slate-900">{card.title}</div>
          <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-[11px] text-slate-500">
            <span className={`rounded-full px-2 py-0.5 font-medium ${style.badge}`}>
              {card.statusLabel}
            </span>
            <span>{card.owner}</span>
            {card.target && card.target !== card.owner && (
              <>
                <span className="text-slate-300">→</span>
                <span>{card.target}</span>
              </>
            )}
          </div>
        </div>
        <span className="shrink-0 text-[11px] text-slate-400">{formatChatTime(card.time)}</span>
      </div>
      <div className={`mt-2 min-h-0 overflow-auto pb-5 pr-1 text-xs leading-5 text-slate-700 ${kanbanDetailBodyMaxHeight(size)}`}>
        <MarkdownContent
          text={card.detail || card.summary || "暂无详情。"}
          compact
          onWorkspaceFileOpen={onWorkspaceFileOpen}
        />
      </div>
    </div>
  );
}

function KanbanCount({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: KanbanColumnKey;
}) {
  const style = kanbanColumnStyle(tone);
  return (
    <div className={`rounded-lg border px-1.5 py-1 shadow-[0_1px_0_rgba(255,255,255,0.9)_inset] ${style.count}`}>
      <div className="font-semibold leading-none">{value}</div>
      <div className="mt-0.5 opacity-75">{label}</div>
    </div>
  );
}

function kanbanColumnStyle(tone: KanbanColumnKey) {
  switch (tone) {
    case "todo":
      return {
        shell: "border-slate-300/80 bg-[linear-gradient(145deg,rgba(250,251,252,0.96),rgba(232,237,242,0.88))]",
        dot: "bg-slate-400 text-slate-400",
        count: "border-slate-300/80 bg-white/65 text-slate-600",
      };
    case "doing":
      return {
        shell: "border-sky-200/80 bg-[linear-gradient(145deg,rgba(248,251,253,0.97),rgba(225,235,244,0.9))]",
        dot: "cm-tech-breathe bg-sky-500 text-sky-500",
        count: "border-sky-200/80 bg-sky-50/80 text-sky-700",
      };
    case "done":
      return {
        shell: "border-emerald-200/70 bg-[linear-gradient(145deg,rgba(249,252,251,0.97),rgba(226,239,236,0.88))]",
        dot: "bg-emerald-500 text-emerald-500",
        count: "border-emerald-200/80 bg-emerald-50/80 text-emerald-700",
      };
  }
}

function kanbanCardStyle(card: KanbanTaskCard) {
  const statusLabel = card.statusLabel.toLowerCase();
  const failedLabel = /失败|阻塞|failed|failure|error|blocked|stale/.test(statusLabel);
  const successfulLabel = /已完成|完成任务|最终汇总|completed|succeeded|success/.test(statusLabel);
  if ((failedLabel && !successfulLabel) || card.eventType === "task_stale") {
    return {
      border: "border-rose-100",
      bar: "bg-rose-500",
      badge: "bg-rose-100 text-rose-700",
    };
  }
  if (card.column === "done") {
    return {
      border: "border-emerald-100",
      bar: "bg-emerald-500",
      badge: "bg-emerald-100 text-emerald-700",
    };
  }
  if (card.column === "doing") {
    return {
      border: "border-sky-100",
      bar: "bg-sky-500",
      badge: "bg-sky-100 text-sky-700",
    };
  }
  return {
    border: "border-amber-100",
    bar: "bg-amber-400",
    badge: "bg-amber-100 text-amber-700",
  };
}

type TeamChatMessage = {
  id: string;
  kind: "member" | "system";
  sender: string;
  senderKey: string;
  content: string;
  time: number;
  sequence?: number;
  tone?: "normal" | "leader" | "assignment" | "feedback" | "error";
  dedupeKey?: string;
  threadKey?: string;
  sortPhase?: number;
};

const TEAM_CHAT_WAIT_DIGEST_MS = 3 * 60 * 1000;

function buildTeamChatMessages(
  groups: CollaborationGroup[],
  memberById: Map<number, TeamMember>,
  leaderMemberId?: string,
  currentUserLabel = "当前用户",
  currentUserKey = "current-user",
) {
  const messages: TeamChatMessage[] = [];
  const memberByKey = new Map(
    [...memberById.values()].map((member) => [member.member_key, member]),
  );
  for (const group of groups) {
    const groupMessages: TeamChatMessage[] = [];
    const firstItemTime = group.items.reduce(
      (current, item) => Math.min(current, item.timeMs),
      Number.POSITIVE_INFINITY,
    );
    const taskCreatedAt = group.task ? new Date(group.task.created_at).getTime() : Number.NaN;
    const taskSequenceBase = Number.isFinite(taskCreatedAt)
      ? taskCreatedAt * 1000
      : Number.isFinite(firstItemTime)
        ? firstItemTime * 1000 - 100
        : group.latestAt * 1000;
    if (group.task) {
      const target =
        memberById.get(group.task.target_member_id)?.member_key ||
        `#${group.task.target_member_id}`;
      const targetLabel = displayMemberName(target, memberByKey, leaderMemberId);
      const creatorKey = taskCreatorKey(group.task);
      const prompt = taskPromptText(group.task) || group.title;
      const assignmentSenderKey =
        creatorKey === currentUserKey || creatorKey === "user"
          ? currentUserKey
          : creatorKey;
      const assignmentSender =
        assignmentSenderKey === currentUserKey
          ? currentUserLabel
          : displayMemberName(creatorKey, memberByKey, leaderMemberId);
      groupMessages.push({
        id: `task-${group.task.id}`,
        kind: "member",
        sender: assignmentSender,
        senderKey: assignmentSenderKey,
        content: `@${targetLabel} ${prompt}\n任务：${group.task.message_id || group.label}`,
        time: new Date(group.task.created_at).getTime(),
        sequence: taskSequenceBase,
        tone: "assignment",
        dedupeKey: `assignment:${group.task.message_id || group.task.id}`,
        threadKey: group.key,
        sortPhase: 0,
      });
      const resultSummary = taskResultText(group.task);
      if (resultSummary && group.items.length === 0) {
        groupMessages.push({
          id: `task-result-${group.task.id}`,
          kind: "member",
          sender: targetLabel,
          senderKey: target,
          content: `任务结果反馈：\n${resultSummary}`,
          time: new Date(group.task.finished_at || group.task.updated_at).getTime(),
          sequence: taskSequenceBase + 0.2,
          tone: "feedback",
          dedupeKey: `feedback:${group.task.message_id || group.task.id}:${normalizeChatDedupeContent(resultSummary)}`,
          threadKey: group.key,
          sortPhase: 2,
        });
      }
      if (group.task.error_message && group.items.length === 0) {
        groupMessages.push({
          id: `task-error-${group.task.id}`,
          kind: "member",
          sender: targetLabel,
          senderKey: target,
          content: `失败：${group.task.error_message}`,
          time: new Date(group.task.updated_at).getTime(),
          sequence: taskSequenceBase + 0.2,
          tone: "error",
          dedupeKey: `error:${group.task.message_id || group.task.id}:${normalizeChatDedupeContent(group.task.error_message)}`,
          threadKey: group.key,
          sortPhase: 2,
        });
      }
    }

    const taskResult = taskResultText(group.task);
    for (const item of group.items) {
      if (
        isTaskDispatchEcho(item, group.task) ||
        isProtocolProgressEcho(item) ||
        isProtocolNoiseItem(item) ||
        isAssignmentHeartbeatItem(item)
      ) {
        continue;
      }
      const message = chatMessageFromItem(item, memberByKey, leaderMemberId);
      if (message) {
        if (group.task) {
          const minSequence = taskSequenceBase + 0.1 + (message.sortPhase || 1) / 10;
          message.sequence =
            typeof message.sequence === "number" && Number.isFinite(message.sequence)
              ? Math.max(message.sequence, minSequence)
              : minSequence;
        }
        groupMessages.push(message);
      }
    }
    const waitDigest = heartbeatWaitDigestMessage(group, groupMessages, memberByKey, leaderMemberId);
    if (waitDigest) {
      groupMessages.push(waitDigest);
    }
    if (
      group.task?.status === "succeeded" &&
      taskResult &&
      group.items.length > 0 &&
      !group.items.some((item) => normalizeChatDedupeContent(item.content).includes(normalizeChatDedupeContent(taskResult).slice(0, 120)))
    ) {
      const target =
        memberById.get(group.task.target_member_id)?.member_key ||
        `#${group.task.target_member_id}`;
      groupMessages.push({
        id: `task-result-full-${group.task.id}`,
        kind: "member",
        sender: displayMemberName(target, memberByKey, leaderMemberId),
        senderKey: target,
        content: `任务结果反馈：\n${taskResult}`,
        time: new Date(group.task.finished_at || group.task.updated_at).getTime(),
        sequence: taskSequenceBase + 0.3,
        tone: "feedback",
        dedupeKey: `feedback-full:${group.task.message_id || group.task.id}:${normalizeChatDedupeContent(taskResult)}`,
        threadKey: group.key,
        sortPhase: 2,
      });
    }
    messages.push(...groupMessages);
  }
  const sortedMessages = messages
    .filter((message) => Number.isFinite(message.time))
    .sort(compareTeamChatMessages);
  return dedupeTeamChatMessages(sortedMessages);
}

function compareTeamChatMessages(a: TeamChatMessage, b: TeamChatMessage) {
  const aSequence = typeof a.sequence === "number" && Number.isFinite(a.sequence) ? a.sequence : undefined;
  const bSequence = typeof b.sequence === "number" && Number.isFinite(b.sequence) ? b.sequence : undefined;
  if (aSequence !== undefined && bSequence !== undefined && aSequence !== bSequence) {
    return aSequence - bSequence;
  }
  return a.time - b.time || a.id.localeCompare(b.id);
}

function itemMessageID(item: CollaborationItem) {
  return payloadText(item.payload, ["messageId", "message_id"]) || item.event.message_id || "";
}

function isTaskDispatchEcho(item: CollaborationItem, task?: TeamTask) {
  if (!task) {
    return false;
  }
  if (item.eventType !== "outbound" && item.eventType !== "task_assigned") {
    return false;
  }
  if (payloadBool(item.payload, ["leaderDispatchOnly", "leader_dispatch_only"]) === true) {
    return false;
  }
  const rawActor = payloadText(item.payload, ["from", "source"]).toLowerCase();
  if (rawActor && rawActor !== "clawmanager" && rawActor !== "system") {
    return false;
  }
  if (!rawActor && item.event.member_id) {
    return false;
  }
  const stepActor = payloadText(item.collaborationStep, ["actor", "from", "sourceMemberId"]).toLowerCase();
  const stepContent = payloadText(item.collaborationStep, ["content", "detail", "text"]);
  if (stepContent && stepActor && stepActor !== "clawmanager" && stepActor !== "system") {
    return false;
  }
  return item.event.task_id === task.id || itemMessageID(item) === task.message_id;
}

function isProtocolProgressEcho(item: CollaborationItem) {
  if (isUserVisibleProcessItem(item) || isBusinessChatItem(item)) {
    return false;
  }
  return ["task_received", "task_started", "progress", "task_progress"].includes(
    item.eventType,
  );
}

function isAssignmentHeartbeatItem(item: CollaborationItem) {
  const eventKind = payloadText(item.payload, ["eventKind", "event_kind", "kind"]).toLowerCase();
  return eventKind === "assignment_heartbeat" || item.eventType === "assignment_heartbeat";
}

function isTerminalGroup(group: CollaborationGroup) {
  const status = group.task?.status?.toLowerCase();
  return status === "succeeded" || status === "failed" || status === "stale";
}

function heartbeatWaitDigestMessage(
  group: CollaborationGroup,
  groupMessages: TeamChatMessage[],
  memberByKey: Map<string, TeamMember>,
  leaderMemberId?: string,
): TeamChatMessage | null {
  if (isTerminalGroup(group)) {
    return null;
  }
  const heartbeatItems = group.items
    .filter(isAssignmentMonitorDigestItem)
    .filter((item) => Number.isFinite(item.timeMs))
    .sort((a, b) => a.timeMs - b.timeMs || a.event.id - b.event.id);
  if (heartbeatItems.length === 0) {
    return null;
  }
  const latestHeartbeat = heartbeatItems[heartbeatItems.length - 1];
  const taskCreatedTime = group.task ? new Date(group.task.created_at).getTime() : Number.NEGATIVE_INFINITY;
  const lastRealMessageTime = groupMessages.reduce(
    (latest, message) => Math.max(latest, message.time),
    Number.isFinite(taskCreatedTime) ? taskCreatedTime : Number.NEGATIVE_INFINITY,
  );
  if (
    Number.isFinite(lastRealMessageTime) &&
    latestHeartbeat.timeMs - lastRealMessageTime < TEAM_CHAT_WAIT_DIGEST_MS
  ) {
    return null;
  }
  if (groupMessages.some((message) => message.time > latestHeartbeat.timeMs)) {
    return null;
  }
  const activeActors = uniqueRecentHeartbeatActors(heartbeatItems, latestHeartbeat.timeMs, memberByKey, leaderMemberId);
  const content = activeActors.length > 0
    ? `${activeActors.join("、")} 仍在执行，Agent 正在继续处理当前回合。系统会继续监控，有新的计划、进度或结果时会自动更新。`
    : "任务仍在执行，Agent 正在继续处理当前回合。系统会继续监控，有新的计划、进度或结果时会自动更新。";
  return {
    id: `heartbeat-digest-${group.key}`,
    kind: "system",
    sender: "系统",
    senderKey: "system",
    content,
    time: latestHeartbeat.timeMs,
    sequence: latestHeartbeat.timeMs * 1000 + 0.6,
    tone: "normal",
    dedupeKey: `heartbeat-digest:${group.key}`,
    threadKey: group.key,
    sortPhase: 1,
  };
}

function uniqueRecentHeartbeatActors(
  heartbeatItems: CollaborationItem[],
  latestHeartbeatTime: number,
  memberByKey: Map<string, TeamMember>,
  leaderMemberId?: string,
) {
  const actors: string[] = [];
  const seen = new Set<string>();
  for (const item of heartbeatItems) {
    if (latestHeartbeatTime - item.timeMs > TEAM_CHAT_WAIT_DIGEST_MS) {
      continue;
    }
    const key = item.actor || item.from || payloadText(item.payload, ["memberId", "member_id"]);
    if (!key || seen.has(key)) {
      continue;
    }
    seen.add(key);
    actors.push(displayMemberName(key, memberByKey, leaderMemberId));
  }
  return actors.slice(0, 3);
}

function isUserVisibleProcessItem(item: CollaborationItem) {
  const eventKind = payloadText(item.payload, ["eventKind", "event_kind", "kind"]).toLowerCase();
  const visibleRaw = payloadText(item.payload, ["visibleToChat", "visible_to_chat"]).toLowerCase();
  const explicitlyHidden = ["false", "0", "no", "off"].includes(visibleRaw);
  const explicitlyVisible = payloadBool(item.payload, ["visibleToChat", "visible_to_chat"]) === true;
  const chatPolicy = payloadText(item.payload, ["chatPolicy", "chat_policy"]).toLowerCase();
  if (["visible", "replaceable", "warning"].includes(chatPolicy)) {
    return Boolean(item.content.trim());
  }
  if (chatPolicy === "hidden" || chatPolicy === "digest") {
    return false;
  }
  if (isAssignmentMonitorDigestItem(item)) {
    return false;
  }
  if (isBusinessChatItem(item)) {
    return true;
  }
  const processKinds = new Set([
    "leader_plan",
    "worker_plan",
    "worker_progress",
    "leader_synthesis",
    "completion_candidate",
    "completion_validation_warning",
    "assignment_recovery_started",
    "assignment_reissued",
    "assignment_recovery_exhausted",
  ]);
  if (processKinds.has(eventKind) || processKinds.has(item.eventType)) {
    return !explicitlyHidden;
  }
  if (isAssignmentHeartbeatItem(item)) {
    return false;
  }
  return (explicitlyVisible || !explicitlyHidden) && hasMeaningfulChatBody(item) && !isLowValueProtocolContent(item.content);
}

function isAssignmentMonitorDigestItem(item: CollaborationItem) {
  const eventKind = payloadText(item.payload, ["eventKind", "event_kind", "kind"]).toLowerCase();
  const chatPolicy = payloadText(item.payload, ["chatPolicy", "chat_policy"]).toLowerCase();
  if (["visible", "replaceable", "warning"].includes(chatPolicy)) {
    return false;
  }
  return (
    isAssignmentHeartbeatItem(item) ||
    eventKind === "assignment_check_requested" ||
    eventKind === "assignment_check_result" ||
    item.eventType === "assignment_check_requested" ||
    item.eventType === "assignment_check_result"
  );
}

function isLowValueProtocolContent(content: string) {
  const normalized = content.trim().toLowerCase();
  return (
    normalized === "task_received" ||
    normalized === "task_started" ||
    normalized === "redis team task started" ||
    normalized === "redis team task processing completed"
  );
}

function chatMessageFromItem(
  item: CollaborationItem,
  memberByKey: Map<string, TeamMember>,
  leaderMemberId?: string,
): TeamChatMessage | null {
  const status = payloadText(item.payload, ["status"]);
  const progress = payloadNumber(item.payload, ["progress"]);
  const senderKey = item.actor || item.from || "system";
  const senderLabel = displayMemberName(senderKey, memberByKey, leaderMemberId);
  const targetLabel = item.to
    ? displayMemberName(item.to, memberByKey, leaderMemberId)
    : "";
  const isAssignmentEvent =
    item.eventType === "outbound" ||
    item.eventType === "task_assigned" ||
    item.eventType === "team_send" ||
    item.eventType === "peer_request" ||
    item.eventType === "peer_handoff" ||
    item.eventType === "peer_review_request";
  const hasContent = Boolean(item.content.trim());
  const isFeedbackEvent =
    isWorkerToLeaderMessage(senderKey, item.to, leaderMemberId) ||
    isWorkerFeedbackEvent(item, senderKey, leaderMemberId, hasContent);
  const eventKind = payloadText(item.payload, ["eventKind", "event_kind", "kind"]).toLowerCase();
  const isSystemProcess =
    senderKey === "clawmanager-monitor" ||
    eventKind === "assignment_check_requested" ||
    item.eventType === "assignment_check_requested" ||
    eventKind === "assignment_recovery_started" ||
    eventKind === "assignment_reissued";
  const isSystem = item.eventType === "task_stale" || isSystemProcess || (isAssignmentEvent && !hasContent);
  const fallbackContent =
    isAssignmentEvent && !hasContent
      ? assignmentEventFallback(item, senderLabel, targetLabel, isFeedbackEvent)
      : chatFallbackText(item, progress, status);
  const content = item.content || fallbackContent;
  const isTerminalFeedback = isTerminalFeedbackItem(item, content, isFeedbackEvent);
  return {
    id: `event-${item.event.id}`,
    kind: isSystem ? "system" : "member",
    sender: isSystem ? "系统" : senderLabel,
    senderKey,
    content,
    time: item.timeMs,
    sequence: item.timeMs * 1000 + (item.event.sequence_no || item.event.id) / 1000000,
    tone:
      isAssignmentEvent && hasContent
        ? isFeedbackEvent
          ? "feedback"
          : "assignment"
        : isAssignmentEvent && isFeedbackEvent
          ? "feedback"
        : item.eventType === "task_failed" || item.eventType === "message_failed" || eventKind === "assignment_recovery_exhausted"
          ? "error"
          : item.eventType === "completion_deferred" || eventKind === "completion_deferred" || eventKind === "completion_rejected" || eventKind === "completion_needs_confirmation"
            ? "error"
          : item.eventType.startsWith("peer_")
            ? "assignment"
          : senderKey === leaderMemberId || senderKey === "ClawManager"
            ? "leader"
            : "normal",
    dedupeKey: chatItemDedupeKey(item, senderKey, content, isAssignmentEvent, isFeedbackEvent),
    threadKey: item.taskKey,
    sortPhase: chatItemSortPhase(item, isAssignmentEvent, isTerminalFeedback),
  };
}

function chatItemSortPhase(
  item: CollaborationItem,
  isAssignmentEvent: boolean,
  isTerminalFeedback: boolean,
) {
  if (isAssignmentEvent && !isTerminalFeedback) {
    return 0;
  }
  if (
    isTerminalFeedback ||
    item.eventType === "task_stale" ||
    item.eventType === "task_failed" ||
    item.eventType === "message_failed"
  ) {
    return 2;
  }
  return 1;
}

function isTerminalFeedbackItem(
  item: CollaborationItem,
  content: string,
  isFeedbackEvent: boolean,
) {
  if (
    item.eventType === "task_completed" ||
    item.eventType === "completion" ||
    item.eventType === "task_failed" ||
    item.eventType === "message_failed"
  ) {
    return true;
  }
  return isFeedbackEvent && finalFeedbackContentPattern.test(content);
}

const finalFeedbackContentPattern =
  /\bDONE\b|team_complete_task|任务核心结果|完整详细产出|结果已反馈|已完成|执行完成|完成任务/;

function dedupeTeamChatMessages(messages: TeamChatMessage[]) {
  const lastReplaceable = new Map<string, number>();
  messages.forEach((message, index) => {
    if (message.dedupeKey?.startsWith("replaceable:")) {
      lastReplaceable.set(message.dedupeKey, index);
    }
  });
  const seen = new Set<string>();
  return messages.filter((message, index) => {
    if (!message.dedupeKey) {
      return true;
    }
    if (message.dedupeKey.startsWith("replaceable:")) {
      return lastReplaceable.get(message.dedupeKey) === index;
    }
    if (seen.has(message.dedupeKey)) {
      return false;
    }
    seen.add(message.dedupeKey);
    return true;
  });
}

function chatItemDedupeKey(
  item: CollaborationItem,
  senderKey: string,
  content: string,
  isAssignmentEvent: boolean,
  isFeedbackEvent: boolean,
) {
  const eventKind = payloadText(item.payload, ["eventKind", "event_kind", "kind"]).toLowerCase();
  const messageId =
    payloadTextDeep(item.payload, ["messageId", "message_id", "inReplyTo", "in_reply_to"]) ||
    item.event.message_id ||
    "";
  const taskId =
    payloadTextDeep(item.payload, ["rootTaskId", "root_task_id", "taskId", "task_id", "runtimeTaskId"]) ||
    (item.event.task_id ? canonicalTaskKey(item.event.task_id) : item.taskKey);
  const assignmentId =
    payloadTextDeep(item.payload, ["assignmentId", "assignment_id", "workId", "work_id"]);
  const displayKey = payloadTextDeep(item.payload, ["displayKey", "display_key"]);
  // Only root-final/completion display keys represent a singleton business
  // fact. Older worker-plan/progress keys can be shared by different workers
  // when assignmentId was absent, so content identity must win for them.
  if (displayKey.startsWith("root-final:") || displayKey.startsWith("completion:")) {
    return displayKey;
  }
  const contentHash =
    payloadTextDeep(item.payload, ["contentHash", "content_hash"]) ||
    chatContentHash(normalizeChatDedupeContent(content));
  const resultScope =
    payloadTextDeep(item.payload, ["completionId", "completion_id"]) ||
    payloadTextDeep(item.payload, ["sourceCompletionId", "source_completion_id"]) ||
    payloadTextDeep(item.payload, ["sourceEventId", "source_event_id"]) ||
    payloadTextDeep(item.payload, ["sourceMessageId", "source_message_id"]);
  const contentKey = normalizeChatDedupeContent(content);
  if (isAssignmentMonitorDigestItem(item) || eventKind === "assignment_check_result") {
    return `monitor:${item.taskKey}:${senderKey}`;
  }
  if (isAssignmentEvent) {
    return `assignment:${taskId}:${senderKey}:${item.to || ""}:${assignmentId || messageId}:${contentHash}`;
  }
  if (isFeedbackEvent) {
    // Result IDs are transport/audit identifiers. Chat de-duplication is based
    // on the actual business content, so a corrected second result remains
    // visible while a re-delivered identical result does not appear twice.
    return `feedback:${taskId}:${senderKey}:${item.to || ""}:${assignmentId}:${contentHash}`;
  }
  if (item.eventType === "task_completed" || item.eventType === "completion" || item.eventType === "reply") {
    return `feedback:${taskId}:${senderKey}:${item.to || ""}:${assignmentId}:${contentHash || resultScope || chatContentHash(contentKey)}`;
  }
  if (isBusinessChatItem(item)) {
    return `narrative:${taskId}:${senderKey}:${item.to || ""}:${assignmentId}:${eventKind}:${contentHash}`;
  }
  return "";
}

function normalizeChatDedupeContent(content: string) {
  return content.trim().replace(/\s+/g, " ").slice(0, 240);
}

function chatContentHash(content: string) {
  let hash = 2166136261;
  for (let index = 0; index < content.length; index++) {
    hash ^= content.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(16).padStart(8, "0");
}

function assignmentEventFallback(
  item: CollaborationItem,
  senderLabel: string,
  targetLabel: string,
  isFeedbackEvent = false,
) {
  const title = isFeedbackEvent ? "任务结果反馈事件" : "任务派发事件";
  const parts = [`${title}：${senderLabel}${targetLabel ? ` → ${targetLabel}` : ""}`];
  const taskId =
    payloadTextDeep(item.payload, ["taskId", "task_id", "runtimeTaskId"]) ||
    item.taskLabel;
  const messageId =
    payloadTextDeep(item.payload, ["messageId", "message_id"]) ||
    item.event.message_id ||
    "";
  if (taskId) {
    parts.push(`任务：${taskId}`);
  }
  if (messageId) {
    parts.push(`消息：${messageId}`);
  }
  parts.push("该事件未携带正文，无法展示任务内容。");
  return parts.join("\n");
}

function isWorkerToLeaderMessage(
  senderKey: string,
  targetKey?: string,
  leaderMemberId?: string,
) {
  if (!targetKey) {
    return false;
  }
  const normalizedLeader = leaderMemberId || "leader";
  const targetIsLeader = targetKey === normalizedLeader || targetKey === "leader";
  const senderIsLeader =
    senderKey === normalizedLeader ||
    senderKey === "leader" ||
    senderKey === "ClawManager";
  return targetIsLeader && !senderIsLeader;
}

function isLeaderMemberKey(memberKey: string, leaderMemberId?: string) {
  const normalizedLeader = leaderMemberId || "leader";
  return (
    memberKey === normalizedLeader ||
    memberKey === "leader" ||
    memberKey === "ClawManager"
  );
}

function isWorkerFeedbackEvent(
  item: CollaborationItem,
  senderKey: string,
  leaderMemberId: string | undefined,
  hasContent: boolean,
) {
  if (isLeaderMemberKey(senderKey, leaderMemberId)) {
    return false;
  }
  if (
    item.eventType === "reply" ||
    item.eventType === "completion" ||
    item.eventType === "task_completed"
  ) {
    return true;
  }
  if (!hasContent) {
    return false;
  }
  return /\bDONE\b|@Leader|team_complete_task|任务核心结果|结果|完成/.test(item.content);
}

function displayMemberName(
  memberKey: string,
  memberByKey: Map<string, TeamMember>,
  leaderMemberId?: string,
) {
  const member = memberByKey.get(memberKey);
  if (member) {
    return `${member.display_name || member.member_key}（${member.member_key}）`;
  }
  if (memberKey === leaderMemberId) {
    return `Leader（${memberKey}）`;
  }
  if (memberKey === "ClawManager") {
    return "ClawManager（system）";
  }
  if (memberKey === "user") {
    return "User";
  }
  if (memberKey.startsWith("user-")) {
    return `User #${memberKey.slice("user-".length)}`;
  }
  return memberKey;
}

function TimeDivider({ value }: { value?: number }) {
  if (!value) {
    return null;
  }
  return (
    <div className="flex items-center justify-center gap-3 text-xs text-gray-500">
      <span className="h-px w-8 bg-gray-300" />
      <span>{formatChatTime(value)}</span>
      <span className="h-px w-8 bg-gray-300" />
    </div>
  );
}

function TeamChatMessageRow({
  message,
  onWorkspaceFileOpen,
}: {
  message: TeamChatMessage;
  onWorkspaceFileOpen?: (path: string) => void;
}) {
  const bubbleClass =
    message.tone === "assignment"
      ? "relative overflow-hidden border border-amber-200 bg-gradient-to-br from-amber-50 via-white to-orange-50 text-gray-950 shadow-[0_14px_28px_-22px_rgba(180,83,9,0.8)]"
      : message.tone === "feedback"
      ? "relative overflow-hidden border border-emerald-200 bg-gradient-to-br from-emerald-50 via-white to-green-50 text-gray-950 shadow-[0_14px_28px_-22px_rgba(5,150,105,0.55)]"
      : message.tone === "error"
      ? "border border-red-100 bg-red-50 text-red-800"
      : "bg-white text-gray-950";
  const isAssignment = message.tone === "assignment";
  const isFeedback = message.tone === "feedback";
  const markerClass = isFeedback
    ? "border-emerald-100 text-emerald-700"
    : "border-amber-100 text-amber-700";
  const markerDotClass = isFeedback ? "bg-emerald-400" : "bg-amber-400";
  const markerDotSolidClass = isFeedback ? "bg-emerald-500" : "bg-amber-500";
  return (
    <div className="flex items-start gap-3">
      <MemberAvatar name={message.senderKey} />
      <div className="min-w-0 flex-1">
        <div className="mb-1 flex items-center gap-2">
          <span className="truncate text-xs font-medium text-gray-500">{message.sender}</span>
          <span className="shrink-0 text-xs text-gray-400">{formatChatTime(message.time)}</span>
        </div>
        <div className={`inline-block max-w-[92%] rounded-lg px-3.5 py-2.5 text-sm leading-6 shadow-sm ${bubbleClass}`}>
          {(isAssignment || isFeedback) && (
            <div className={`mb-2 flex items-center gap-2 border-b pb-2 text-[11px] font-semibold uppercase tracking-[0.12em] ${markerClass}`}>
              <span className="relative flex h-2 w-2">
                <span className={`absolute inline-flex h-full w-full animate-ping rounded-full opacity-60 ${markerDotClass}`} />
                <span className={`relative inline-flex h-2 w-2 rounded-full ${markerDotSolidClass}`} />
              </span>
              <span>{isFeedback ? "任务结果反馈" : "任务下发"}</span>
            </div>
          )}
          <MarkdownContent
            text={message.content}
            compact
            onWorkspaceFileOpen={onWorkspaceFileOpen}
          />
        </div>
      </div>
    </div>
  );
}

function SystemChatLine({ message }: { message: TeamChatMessage }) {
  const systemClass =
    message.tone === "feedback"
      ? "bg-emerald-50 text-emerald-700 ring-1 ring-emerald-100"
      : "bg-gray-200 text-gray-500";
  const timeClass = message.tone === "feedback" ? "text-emerald-500/80" : "text-gray-400";
  return (
    <div className="flex justify-center">
      <div className={`max-w-[86%] rounded-2xl px-3 py-1.5 text-center text-[11px] leading-5 ${systemClass}`}>
        <div className={`mb-0.5 text-[10px] ${timeClass}`}>{formatChatTime(message.time)}</div>
        {message.content}
      </div>
    </div>
  );
}

function MemberAvatar({ name }: { name: string }) {
  const label = avatarLabel(name);
  const isLeader = name.toLowerCase().includes("leader") || name === "ClawManager";
  return (
    <div
      className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-full border text-xs font-semibold shadow-sm ${
        isLeader
          ? "border-slate-300 bg-gradient-to-br from-slate-100 to-slate-300 text-slate-700"
          : "border-sky-200 bg-gradient-to-br from-sky-100 to-cyan-200 text-sky-800"
      }`}
    >
      {label}
    </div>
  );
}

function avatarLabel(name: string) {
  const normalized = name.replace(/^team-[^-]+-/, "").replace(/[^a-zA-Z0-9]/g, "");
  return (normalized || "AI").slice(0, 2).toUpperCase();
}

function formatChatTime(value: number) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function chatFallbackText(
  item: CollaborationItem,
  progress?: number,
  status?: string,
) {
  switch (item.eventType) {
    case "outbound":
    case "task_assigned":
      return item.to ? `发布了给 ${item.to} 的任务分工。` : "发布了任务分工。";
    case "task_received":
      return "已领取任务。";
    case "task_started":
      return "开始执行任务。";
    case "progress":
    case "task_progress":
      return progress === undefined ? "进度已更新。" : `当前进度 ${progress}%。`;
    case "reply":
    case "completion":
    case "task_completed":
      return "结果已反馈。";
    case "task_failed":
    case "message_failed":
      return payloadText(item.payload, ["error", "error_message", "diagnostic"]) || "任务执行失败。";
    case "task_stale":
      return "长时间没有新的进展。";
    default:
      return status ? `状态更新：${status}` : eventVerb(item.eventType);
  }
}

function MarkdownContent({
  text,
  compact = false,
  onWorkspaceFileOpen,
}: {
  text: string;
  compact?: boolean;
  onWorkspaceFileOpen?: (path: string) => void;
}) {
  const lines = text.split(/\r?\n/);
  const nodes: React.ReactNode[] = [];

  for (let index = 0; index < lines.length; index++) {
    if (isMarkdownTableStart(lines, index)) {
      const tableLines = [lines[index]];
      const separator = lines[index + 1];
      let rowIndex = index + 2;
      while (rowIndex < lines.length && splitMarkdownTableRow(lines[rowIndex]).length > 1) {
        tableLines.push(lines[rowIndex]);
        rowIndex++;
      }
      nodes.push(
        <MarkdownTable
          key={`table-${index}`}
          headerLine={tableLines[0]}
          separatorLine={separator}
          rowLines={tableLines.slice(1)}
          keyPrefix={`table-${index}`}
          onWorkspaceFileOpen={onWorkspaceFileOpen}
        />,
      );
      index = rowIndex - 1;
      continue;
    }

    nodes.push(renderMarkdownLine(lines[index], index, compact, onWorkspaceFileOpen));
  }

  return <div className={compact ? "space-y-1.5" : "space-y-2"}>{nodes}</div>;
}

function renderMarkdownLine(
  line: string,
  index: number,
  compact: boolean,
  onWorkspaceFileOpen?: (path: string) => void,
) {
  const trimmed = line.trim();
  if (!trimmed) {
    return <div key={index} className={compact ? "h-0.5" : "h-1"} />;
  }
  if (/^-{3,}$/.test(trimmed)) {
    return <hr key={index} className="border-[#eadfd8]" />;
  }
  const heading = trimmed.match(/^(#{1,4})\s+(.+)$/);
  if (heading) {
    return (
      <div key={index} className="font-semibold text-gray-900">
        {renderInlineMarkdown(heading[2] || "", `h-${index}`, onWorkspaceFileOpen)}
      </div>
    );
  }
  const ordered = trimmed.match(/^(\d+)[.)]\s+(.+)$/);
  if (ordered) {
    return (
      <div key={index} className="flex gap-2">
        <span className="mt-0.5 inline-flex h-5 min-w-[1.25rem] shrink-0 items-center justify-center rounded-full border border-[#eadfd8] bg-white px-1 text-[11px] font-semibold text-[#8b5a45]">
          {ordered[1]}
        </span>
        <span className="min-w-0">{renderInlineMarkdown(ordered[2] || "", `o-${index}`, onWorkspaceFileOpen)}</span>
      </div>
    );
  }
  const bullet = trimmed.match(/^[-*]\s+(.+)$/);
  if (bullet) {
    return (
      <div key={index} className="flex gap-2">
        <span className="mt-[0.65em] h-1.5 w-1.5 shrink-0 rounded-full bg-gray-400" />
        <span>{renderInlineMarkdown(bullet[1] || "", `b-${index}`, onWorkspaceFileOpen)}</span>
      </div>
    );
  }
  return (
    <p key={index} className="whitespace-pre-wrap break-words">
      {renderInlineMarkdown(line, `p-${index}`, onWorkspaceFileOpen)}
    </p>
  );
}

function isMarkdownTableStart(lines: string[], index: number) {
  if (index + 1 >= lines.length) {
    return false;
  }
  const header = splitMarkdownTableRow(lines[index]);
  const separator = splitMarkdownTableRow(lines[index + 1]);
  if (header.length < 2 || separator.length < 2) {
    return false;
  }
  return separator.every((cell) => /^:?-{3,}:?$/.test(cell.trim()));
}

function splitMarkdownTableRow(line?: string) {
  if (!line || !line.includes("|")) {
    return [];
  }
  const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
  return trimmed.split("|").map((cell) => cell.trim());
}

function MarkdownTable({
  headerLine,
  separatorLine,
  rowLines,
  keyPrefix,
  onWorkspaceFileOpen,
}: {
  headerLine: string;
  separatorLine: string;
  rowLines: string[];
  keyPrefix: string;
  onWorkspaceFileOpen?: (path: string) => void;
}) {
  const headers = splitMarkdownTableRow(headerLine);
  const alignments = splitMarkdownTableRow(separatorLine).map((cell) => {
    const trimmed = cell.trim();
    if (trimmed.startsWith(":") && trimmed.endsWith(":")) {
      return "text-center";
    }
    if (trimmed.endsWith(":")) {
      return "text-right";
    }
    return "text-left";
  });
  const rows = rowLines
    .map(splitMarkdownTableRow)
    .filter((row) => row.length > 0);

  return (
    <div className="my-2 max-w-full overflow-x-auto rounded-lg border border-[#e5e7eb] bg-white">
      <table className="min-w-full border-collapse text-xs leading-5">
        <thead className="bg-[#fafafa] text-gray-700">
          <tr>
            {headers.map((header, cellIndex) => (
              <th
                key={`${keyPrefix}-h-${cellIndex}`}
                className={`border-b border-[#e5e7eb] px-2.5 py-2 font-semibold ${alignments[cellIndex] || "text-left"}`}
              >
                {renderInlineMarkdown(header, `${keyPrefix}-h-${cellIndex}`, onWorkspaceFileOpen)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-[#f0f0f0]">
          {rows.map((row, rowIndex) => (
            <tr key={`${keyPrefix}-r-${rowIndex}`} className="align-top">
              {headers.map((_, cellIndex) => (
                <td
                  key={`${keyPrefix}-r-${rowIndex}-${cellIndex}`}
                  className={`px-2.5 py-2 text-gray-800 ${alignments[cellIndex] || "text-left"}`}
                >
                  {renderInlineMarkdown(row[cellIndex] || "", `${keyPrefix}-r-${rowIndex}-${cellIndex}`, onWorkspaceFileOpen)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function renderInlineMarkdown(
  text: string,
  keyPrefix: string,
  onWorkspaceFileOpen?: (path: string) => void,
) {
  const nodes: React.ReactNode[] = [];
  const pattern = /(`[^`]+`|\/workspaces\/teams\/user-\d+\/team-\d+-shared\/[^\s`<>"')\]}]+|\/team\/[^\s`<>"')\]}]+|\.?\/?team\/[^\s`<>"')\]}]+|\*\*[^*]+\*\*|\*[^*]+\*)/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      nodes.push(text.slice(lastIndex, match.index));
    }
    const token = match[0];
    const key = `${keyPrefix}-${match.index}`;
    if (token.startsWith("`")) {
      const codeValue = token.slice(1, -1);
      const workspacePath = workspaceLinkToRelativePath(codeValue);
      if (
        onWorkspaceFileOpen &&
        isTeamWorkspaceLink(codeValue) &&
        isPreviewableWorkspacePath(workspacePath)
      ) {
        nodes.push(
          <button
            key={key}
            type="button"
            onClick={() => onWorkspaceFileOpen(codeValue)}
            className="rounded bg-cyan-50 px-1 py-0.5 font-mono text-xs font-semibold text-cyan-700 underline decoration-cyan-300 underline-offset-2 hover:bg-cyan-100"
          >
            {codeValue}
          </button>,
        );
      } else {
        nodes.push(
          <code key={key} className="rounded bg-white px-1 py-0.5 font-mono text-xs text-gray-700">
            {codeValue}
          </code>,
        );
      }
    } else if (isTeamWorkspaceLink(token)) {
      const displayToken = token.replace(/[，。；;,.、)）\]}]+$/g, "");
      const suffix = token.slice(displayToken.length);
      const workspacePath = workspaceLinkToRelativePath(displayToken);
      if (onWorkspaceFileOpen && isPreviewableWorkspacePath(workspacePath)) {
        nodes.push(
          <button
            key={key}
            type="button"
            onClick={() => onWorkspaceFileOpen(displayToken)}
            className="rounded-md bg-cyan-50 px-1.5 py-0.5 font-mono text-xs font-semibold text-cyan-700 underline decoration-cyan-300 underline-offset-2 hover:bg-cyan-100"
          >
            {displayToken}
          </button>,
        );
        if (suffix) {
          nodes.push(suffix);
        }
      } else {
        nodes.push(token);
      }
    } else if (token.startsWith("**")) {
      nodes.push(
        <strong key={key} className="font-semibold text-gray-900">
          {token.slice(2, -2)}
        </strong>,
      );
    } else {
      nodes.push(
        <em key={key} className="italic">
          {token.slice(1, -1)}
        </em>,
      );
    }
    lastIndex = pattern.lastIndex;
  }
  if (lastIndex < text.length) {
    nodes.push(text.slice(lastIndex));
  }
  return nodes;
}

export function TasksPanel({
  tasks,
  memberById,
  leaderMemberId,
}: {
  tasks: TeamTask[];
  memberById: Map<number, TeamMember>;
  leaderMemberId?: string;
}) {
  return (
    <section className="app-panel overflow-hidden">
      <div className="border-b border-[#f1e7e1] px-5 py-4">
        <h2 className="text-lg font-semibold text-gray-900">任务编排</h2>
        <p className="mt-1 text-sm text-gray-500">
          看任务从 ClawManager 进入哪个成员、执行到哪一步、最后产出或失败原因是什么。
        </p>
      </div>
      <div className="max-h-[640px] overflow-auto">
        {tasks.length === 0 ? (
          <div className="p-6 text-sm text-gray-500">暂无任务。</div>
        ) : (
          <ul className="space-y-4 p-5">
            {tasks.map((task) => {
              const target =
                memberById.get(task.target_member_id)?.member_key ||
                `#${task.target_member_id}`;
              const title = taskTitleText(task);
              const prompt = taskPromptText(task);
              const intent = taskIntentText(task.payload);
              const resultSummary =
                payloadText(task.result, ["summary", "result", "message"]) ||
                payloadText(task.payload, ["result", "answer"]);
              return (
                <li
                  key={task.id}
                  className="rounded-2xl border border-[#f1e1d8] bg-white p-4 shadow-sm"
                >
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-semibold text-gray-900">
                          #{task.id} {title}
                        </span>
                        <span
                          className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${statusStyle(task.status)}`}
                        >
                          {task.status}
                        </span>
                      </div>
                      <div className="mt-3 flex flex-wrap items-center gap-2 text-sm">
                        <MemberPill label="发起" value="ClawManager" />
                        <span className="text-gray-300">→</span>
                        <MemberPill
                          label={target === leaderMemberId ? "Leader" : "目标"}
                          value={target}
                        />
                        {intent && <MemberPill label="意图" value={intent} />}
                      </div>
                    </div>
                    <div className="shrink-0 text-right text-xs text-gray-500">
                      <div>创建 {formatDateTime(task.created_at)}</div>
                      {task.started_at && (
                        <div className="mt-1">
                          开始 {formatDateTime(task.started_at)}
                        </div>
                      )}
                      {task.finished_at && (
                        <div className="mt-1">
                          结束 {formatDateTime(task.finished_at)}
                        </div>
                      )}
                    </div>
                  </div>

                  {prompt && (
                    <div className="mt-4 rounded-xl bg-[#fff8f5] px-4 py-3 text-sm leading-6 text-gray-700">
                      {prompt}
                    </div>
                  )}

                  {task.error_message && (
                    <div className="mt-3 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                      {task.error_message}
                    </div>
                  )}

                  {resultSummary && (
                    <div className="mt-3 rounded-xl border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-800">
                      {resultSummary}
                    </div>
                  )}

                  <details className="mt-3">
                    <summary className="cursor-pointer text-xs font-medium text-gray-500">
                      调试数据 · {task.message_id}
                    </summary>
                    <pre className="mt-2 max-h-40 overflow-auto rounded-lg bg-gray-50 p-3 text-xs text-gray-600">
                      {compactJson(task.payload)}
                    </pre>
                  </details>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </section>
  );
}

export function EventsPanel({
  events,
  memberById,
}: {
  events: TeamEvent[];
  memberById: Map<number, TeamMember>;
}) {
  return (
    <section className="app-panel overflow-hidden">
      <div className="border-b border-[#f1e7e1] px-5 py-4">
        <h2 className="text-lg font-semibold text-gray-900">协作时间线</h2>
        <p className="mt-1 text-sm text-gray-500">
          按时间显示成员收到、转派、开始、失败或完成任务的过程。
        </p>
      </div>
      <div className="max-h-[640px] overflow-auto">
        {events.length === 0 ? (
          <div className="p-6 text-sm text-gray-500">暂无事件。</div>
        ) : (
          <ol className="relative space-y-4 p-5 before:absolute before:left-7 before:top-6 before:h-[calc(100%-3rem)] before:w-px before:bg-[#eadfd8]">
            {events.map((event) => {
              const member = memberKeyFromEvent(event, memberById);
              const from = payloadText(event.payload, ["from"]);
              const to = payloadText(event.payload, ["to", "memberId"]);
              const intent = taskIntentText(event.payload);
              const summary =
                payloadText(event.payload, [
                  "summary",
                  "lastSummary",
                  "diagnostic",
                  "error",
                  "error_message",
                  "message",
                ]) || payloadText(event.payload, ["prompt", "title"]);
              return (
                <li key={event.id} className="relative pl-9">
                  <div
                    className={`absolute left-0 top-1 flex h-4 w-4 items-center justify-center rounded-full border-2 bg-white ${eventTone(event.event_type)}`}
                  />
                  <div className="rounded-2xl border border-[#f1e1d8] bg-white p-4 shadow-sm">
                    <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                      <div>
                        <div className="flex flex-wrap items-center gap-2">
                          <span
                            className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${eventTone(event.event_type)}`}
                          >
                            {eventVerb(event.event_type)}
                          </span>
                          <span className="font-medium text-gray-900">
                            {member}
                          </span>
                          {event.task_id && (
                            <span className="text-sm text-gray-500">
                              任务 #{event.task_id}
                            </span>
                          )}
                        </div>
                        <div className="mt-2 flex flex-wrap items-center gap-2 text-sm">
                          {from && <MemberPill label="从" value={from} />}
                          {from && to && <span className="text-gray-300">→</span>}
                          {to && <MemberPill label="到" value={to} />}
                          {intent && <MemberPill label="意图" value={intent} />}
                        </div>
                      </div>
                      <div className="shrink-0 text-right text-xs text-gray-500">
                        <div>{formatDateTime(event.occurred_at || event.created_at)}</div>
                        {event.redis_stream_id && (
                          <div className="mt-1 font-mono">{event.redis_stream_id}</div>
                        )}
                      </div>
                    </div>

                    {summary && (
                      <div className="mt-3 rounded-xl bg-gray-50 px-4 py-3 text-sm leading-6 text-gray-700">
                        {summary}
                      </div>
                    )}

                    <details className="mt-3">
                      <summary className="cursor-pointer text-xs font-medium text-gray-500">
                        原始事件
                      </summary>
                      <pre className="mt-2 max-h-36 overflow-auto rounded-lg bg-gray-50 p-3 text-xs text-gray-600">
                        {compactJson(event.payload)}
                      </pre>
                    </details>
                  </div>
                </li>
              );
            })}
          </ol>
        )}
      </div>
    </section>
  );
}

function MemberPill({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-[#eadfd8] bg-white px-2.5 py-1 text-xs text-gray-600">
      <span className="text-gray-400">{label}</span>
      <span className="font-medium text-gray-800">{value}</span>
    </span>
  );
}

export default TeamDetailPage;
