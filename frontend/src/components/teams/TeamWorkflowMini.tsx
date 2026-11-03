import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ArrowDown, ArrowUpRight, Check, Circle, GitBranch, LoaderCircle, Pause, TriangleAlert } from "lucide-react";
import type { TeamMember, TeamWorkItem } from "../../types/team";
import TeamBadge from './TeamBadge';
import { memberIcon } from '../../lib/teamIcons';

type WorkflowCard = { id: string; owner: string; title: string; summary: string; detail?: string; column: "todo" | "doing" | "done"; statusLabel: string; eventType: string; time: number };
export type WorkflowNode = WorkflowCard & { role?: string; iconKey?: string; active: boolean; tone: "active" | "done" | "waiting" | "error"; dependsOn: string[]; dependencyNames: string[] };

// Reuse the existing Kanban cards: no second completion/assignment projection.
// Only an explicit dependency may draw a directional link. Otherwise the
// vertical rail is a chronological dispatch timeline, never a claimed DAG.
export function buildMiniWorkflow(cards: WorkflowCard[], items: TeamWorkItem[], members: TeamMember[], terminal: boolean) {
  const nodes: WorkflowNode[] = cards.map(card => {
    const item = items.find(item => card.id === `work-item-${item.id}`);
    const namedMembers = members.filter(member => member.display_name === card.owner);
    const member = item?.owner_member_id ? members.find(member => member.id === item.owner_member_id) : members.find(member => member.member_key === card.owner) || (namedMembers.length === 1 ? namedMembers[0] : undefined);
    const failed = ["task_failed", "task_stale", "message_failed", "blocker", "dlq"].includes(card.eventType);
    const active = !terminal && !failed && card.column === "doing" && (item ? item.status === "running" : !/等待|waiting|blocked/i.test(card.statusLabel));
    const dependencies = (item?.depends_on || []).flatMap(key => items.filter(candidate => candidate.work_id === key || candidate.assignment_id === key || candidate.canonical_work_id === key));
    return { ...card, time: item?.created_at ? new Date(item.created_at).getTime() : card.time, role: member?.role, iconKey: member ? memberIcon(member) : 'unknown', active, tone: failed ? "error" : active ? "active" : card.column === "done" ? "done" : "waiting",
      dependsOn: dependencies.map(item => `work-item-${item.id}`).filter(id => cards.some(card => card.id === id)),
      dependencyNames: dependencies.map(item => item.title) };
  });
  const ordered: WorkflowNode[] = [];
  const remaining = [...nodes].sort((a, b) => a.time - b.time || a.id.localeCompare(b.id));
  while (remaining.length) {
    const index = remaining.findIndex(node => node.dependsOn.every(id => ordered.some(previous => previous.id === id)));
    // Cyclic/incomplete historical ledgers stay visible; do not invent an order.
    ordered.push(remaining.splice(index < 0 ? 0 : index, 1)[0]);
  }
  return ordered;
}

export default function TeamWorkflowMini({ nodes, taskId, status, title, terminal, onOpen, onSelect, handoff, freshHandoff, executionLabel }: {
  executionLabel?: string;
  nodes: WorkflowNode[];
  taskId?: number;
  status: string;
  title: string;
  terminal: boolean;
  onOpen?: () => void;
  onSelect: (id: string) => void;
  handoff?: { id: number; from: string; to: string; verb: string };
  freshHandoff: boolean;
}) {
  const tooltipId = useId();
  const [hovered, setHovered] = useState<{ id: string; x: number; y: number; width: number } | null>(null);
  const current = nodes.find(node => node.id === hovered?.id);
  const hoverTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(hoverTimer.current), []);
  const clearHover = () => { clearTimeout(hoverTimer.current); setHovered(null); };
  const show = (node: WorkflowNode, element: HTMLElement) => {
    clearTimeout(hoverTimer.current);
    const rect = element.getBoundingClientRect();
    const width = Math.min(310, window.innerWidth - 24);
    setHovered({ id: node.id, width, x: Math.max(12, Math.min(rect.left - width - 12, window.innerWidth - width - 12)), y: Math.max(12, Math.min(rect.top - 12, window.innerHeight - 290)) });
  };
  const activeCount = nodes.filter(node => node.active).length;
  return <section className="team-workflow-mini" aria-label="任务工作流缩略" onKeyDown={event => { if (event.key === "Escape") { event.stopPropagation(); clearHover(); } }}>
    <button className="team-workflow-heading" type="button" onClick={onOpen} aria-label="展开任务看板">
      <span><GitBranch size={14} />执行流程 <small>{taskId ? `#${taskId}` : ""}</small></span><ArrowUpRight size={14} />
    </button>
    <div className="team-workflow-status"><span>{status}</span>{activeCount > 1 && <b>{activeCount} 人并行</b>}</div>
    <div className="team-workflow-order">{executionLabel || (nodes.some(node => node.dependsOn.length) ? "按任务依赖" : "按派发记录")}</div>
    <ol className="team-workflow-nodes">
      {nodes.map((node, index) => <li key={node.id} data-tone={node.tone} data-active={node.active}>
        {index > 0 && <span className="team-workflow-connector" data-linked={node.dependsOn.includes(nodes[index - 1].id)} aria-hidden="true">{node.dependsOn.includes(nodes[index - 1].id) && <ArrowDown size={12} />}</span>}
        <button type="button" className="team-workflow-node" aria-label={`${node.owner}：${node.statusLabel}，打开看板`} aria-describedby={hovered?.id === node.id ? tooltipId : undefined}
          onMouseEnter={event => show(node, event.currentTarget)} onMouseLeave={() => { hoverTimer.current = setTimeout(clearHover, 120); }}
          onFocus={event => show(node, event.currentTarget)} onBlur={clearHover}
          onClick={() => { clearHover(); onSelect(node.id); }}>
          <span className="team-workflow-marker" aria-hidden="true">{node.tone === "active" ? <LoaderCircle size={13} /> : node.tone === "done" ? <Check size={12} /> : node.tone === "error" ? <TriangleAlert size={12} /> : terminal ? <Pause size={11} /> : <Circle size={10} />}</span>
          <span className="team-workflow-person"><span className="team-workflow-identity"><TeamBadge iconKey={node.iconKey || 'unknown'} size="workflow" /><strong>{node.owner}</strong></span><small>{node.role || "成员"}</small></span>
          <span className="team-workflow-state">{node.active ? "执行中" : node.statusLabel}</span>
        </button>
      </li>)}
    </ol>
    {nodes.length === 0 && <button type="button" className="team-workflow-empty" onClick={onOpen}>{taskId ? "等待任务拆解" : "暂无任务"}</button>}
    {handoff && <div key={handoff.id} className="team-workflow-handoff" data-fresh={freshHandoff} role="group" tabIndex={0} aria-label={`最近传递：${handoff.from} → ${handoff.to} · ${handoff.verb}`} title={`${handoff.from} → ${handoff.to} · ${handoff.verb}`}>
      <span className="team-workflow-handoff-caption">最近传递</span>
      <div className="team-workflow-handoff-route">
        <div className="team-workflow-handoff-row"><span>发出</span><strong>{handoff.from}</strong></div>
        <ArrowDown className="team-workflow-handoff-arrow" size={14} aria-hidden="true" />
        <div className="team-workflow-handoff-row"><span>接收</span><strong>{handoff.to}</strong></div>
      </div>
    </div>}
    {hovered && current && createPortal(<div id={tooltipId} role="tooltip" className="team-workflow-tooltip" style={{ left: hovered.x, top: hovered.y, width: hovered.width }}
      onMouseEnter={() => clearTimeout(hoverTimer.current)} onMouseLeave={clearHover}>
      <div className="team-workflow-tooltip-caption">{current.owner} · {current.role || "成员"}</div>
      <strong>{current.title}</strong>
      <div className="team-workflow-tooltip-status">{current.statusLabel}{terminal && current.column !== "done" ? " · 根任务已结束，保留原执行记录" : ""}</div>
      <p>{current.summary || current.detail || "尚无进展摘要"}</p>
      {current.dependencyNames.length > 0 && <p>依赖：{current.dependencyNames.join("、")}</p>}
      <div className="team-workflow-tooltip-footer">{title}<br />点击节点打开完整 Kanban</div>
    </div>, document.body)}
  </section>;
}
