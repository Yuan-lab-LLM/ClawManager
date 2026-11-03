import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { FolderOpen, PanelLeftClose, Settings2, X } from "lucide-react";
import type { Team, TeamMember } from "../../types/team";
import { MemberCards, MemberInfoDialog, MemberPager, MemberRail } from "./TeamMembers";
import { memberPageSize } from "./teamMemberPresentation";
import "./team-chat-workspace.css";
import "./team-continuous-chat.css";

export type TeamWorkspacePanel = "closed" | "kanban" | "files";

// Width is the workspace's usable width, not the browser viewport.
export function workspaceMode(width: number) {
  return width >= 1540 ? "dual" : width >= 1248 ? "single" : "drawer";
}

export default function TeamChatWorkspace({ members, team, leaderKey, renderMemberDetails, chat, renderKanban, files }: {
  members: TeamMember[];
  team: Team;
  leaderKey?: string;
  renderMemberDetails: (member: TeamMember) => ReactNode;
  chat: ReactNode;
  renderKanban: (mini: boolean, open: () => void) => ReactNode;
  files: ReactNode | ((active: boolean) => ReactNode);
}) {
  const root = useRef<HTMLDivElement>(null);
  const leftPanel = useRef<HTMLElement>(null);
  const rightPanel = useRef<HTMLElement>(null);
  const lastTrigger = useRef<HTMLElement | null>(null);
  const membersPanelId = useId();
  const [width, setWidth] = useState(0);
  const [height, setHeight] = useState(0);
  const [memberPage, setMemberPage] = useState(0);
  const [memberInfo, setMemberInfo] = useState<number | "team" | null>(null);
  const [left, setLeft] = useState(false);
  const [right, setRight] = useState<TeamWorkspacePanel>("closed");
  const [filesVisited, setFilesVisited] = useState(false);
  const [lastSide, setLastSide] = useState<"left" | "right">("right");
  const mode = workspaceMode(width);
  // Leave space for the rail too, so both states can always show the same page.
  const pageSize = memberPageSize(Math.max(0, height - 110), members.length);
  const pages = Math.max(1, Math.ceil(members.length / pageSize));
  const page = Math.min(memberPage, pages - 1);
  const start = page * pageSize;
  const pageMembers = members.slice(start, start + pageSize);
  const inspectedMember = members.find(member => member.id === memberInfo);
  useLayoutEffect(() => {
    const node = root.current;
    if (!node) return;
    const observer = new ResizeObserver(([entry]) => { setWidth(entry.contentRect.width); setHeight(entry.contentRect.height); });
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (mode !== "dual" && left && right !== "closed") {
      if (lastSide === "left") setRight("closed"); else setLeft(false);
    }
  }, [mode, left, right, lastSide]);
  const rememberTrigger = () => {
    if (document.activeElement instanceof HTMLElement) lastTrigger.current = document.activeElement;
  };
  const close = () => {
    setLeft(false);
    setRight("closed");
    restoreFocus();
  };
  const restoreFocus = () => requestAnimationFrame(() => {
    if (lastTrigger.current?.isConnected && lastTrigger.current.getClientRects().length) lastTrigger.current.focus();
    else root.current?.querySelector<HTMLButtonElement>('[aria-label="展开任务看板"]')?.focus();
  });
  const openRight = (panel: "kanban" | "files") => {
    rememberTrigger();
    setLastSide("right");
    if (mode !== "dual") setLeft(false);
    setRight(current => current === panel ? "closed" : panel);
    if (panel === "files") setFilesVisited(true);
  };
  const drawerOpen = mode === "drawer" && (left || right !== "closed");
  useEffect(() => {
    if (!drawerOpen) return;
    const panel = left ? leftPanel.current : rightPanel.current;
    panel?.querySelector<HTMLButtonElement>("button")?.focus();
    const onKey = (event: KeyboardEvent) => {
      // File previews have their own modal layer and keyboard lifecycle.
      if (document.querySelector('[data-workspace-preview], [data-workspace-editor], [data-team-member-dialog]')) return;
      if (event.key === "Escape") { event.preventDefault(); close(); }
      if (event.key !== "Tab" || !panel) return;
      const controls = [...panel.querySelectorAll<HTMLElement>('button:not([disabled]), a[href], input, textarea, select, summary')]
        .filter(node => node.getClientRects().length > 0);
      const first = controls[0], last = controls[controls.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [drawerOpen, left, right]);

  return <div ref={root} className="team-chat-workspace" data-mode={mode} data-left={left} data-right={right}>
    <div className="team-member-rail" inert={drawerOpen}>
      <MemberRail members={pageMembers} start={start} leaderKey={leaderKey} total={members.length} expanded={left} panelId={membersPanelId} onDetails={setMemberInfo} onOpen={() => {
        rememberTrigger(); setLastSide("left"); setLeft(value => !value); if (mode !== "dual") setRight("closed");
      }} pager={<MemberPager page={page} pages={pages} onPage={setMemberPage} />} />
    </div>
    <aside id={membersPanelId} ref={leftPanel} hidden={!left} className="team-members-panel" role={mode === "drawer" ? "dialog" : undefined} aria-modal={mode === "drawer" ? true : undefined} aria-label="成员与团队配置">
      <header><strong>成员 <span className="team-roster-count">{members.length}</span></strong><div className="team-roster-header-actions"><button type="button" aria-label="团队配置" title="团队配置" onClick={() => setMemberInfo("team")}><Settings2 size={17} /></button><button type="button" aria-label="收起成员" onClick={() => { setLeft(false); restoreFocus(); }}><PanelLeftClose size={18} /></button></div></header>
      <div className="team-members-body"><MemberCards members={pageMembers} start={start} leaderKey={leaderKey} onDetails={setMemberInfo} /></div>
      <MemberPager page={page} pages={pages} onPage={setMemberPage} />
    </aside>
    <main className="team-chat-main" inert={drawerOpen}>{chat}</main>
    <aside ref={rightPanel} className="team-tools-panel" role={drawerOpen && right !== "closed" ? "dialog" : undefined} aria-modal={drawerOpen && right !== "closed" ? true : undefined} aria-label="任务看板与文件" inert={drawerOpen && left}>
      <header className="team-tools-toolbar">
        {right !== "closed" && <strong>{right === "kanban" ? "任务看板" : "共享文件"}</strong>}
        {right !== "closed" && <button type="button" onClick={() => openRight("kanban")} aria-pressed={right === "kanban"}>看板</button>}
        <button type="button" onClick={() => openRight("files")} aria-expanded={right === "files"}><FolderOpen size={17} /><span>文件</span></button>
        {right !== "closed" && <button type="button" aria-label="收起右侧面板" onClick={() => { setRight("closed"); restoreFocus(); }}><X size={18} /></button>}
      </header>
      <div className="team-kanban-host" hidden={right === "files"}>{renderKanban(right === "closed", () => openRight("kanban"))}</div>
      <div className="team-files-host" hidden={right !== "files"}>{filesVisited && (typeof files === "function" ? files(right === "files" && width > 0) : files)}</div>
    </aside>
    {drawerOpen && <button type="button" className="team-drawer-backdrop" aria-label="关闭工具抽屉" onClick={close} />}
    {(memberInfo === "team" || inspectedMember) && <MemberInfoDialog member={inspectedMember} team={team} renderDetails={renderMemberDetails} onClose={() => setMemberInfo(null)} />}
  </div>;
}
