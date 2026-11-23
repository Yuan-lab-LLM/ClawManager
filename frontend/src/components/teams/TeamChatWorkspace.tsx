import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { Columns3, FolderOpen, Maximize2, Minimize2, PanelLeftClose, Settings2, SquareTerminal, X } from "lucide-react";
import { useI18n } from "../../contexts/I18nContext";
import type { Team, TeamMember } from "../../types/team";
import { MemberCards, MemberInfoDialog, MemberPager, MemberRail } from "./TeamMembers";
import { memberPageSize } from "./teamMemberPresentation";
import "./team-chat-workspace.css";
import "./team-continuous-chat.css";
import "./team-classic-presentation.css";
import "./owner-team-chat-tools.css";

export type TeamWorkspacePanel = "closed" | "kanban" | "files";
export type TeamWorkspaceTool = "files" | "browser" | "terminal" | "git";
export type TeamWorkspaceProgram = Exclude<TeamWorkspaceTool, "files">;

// Width is the workspace's usable width, not the browser viewport.
export function workspaceMode(width: number, northbound = false) {
  return width >= 1540 ? "dual" : width >= (northbound ? 1024 : 1248) ? "single" : "drawer";
}

export default function TeamChatWorkspace({ members, team, owner, leaderKey, renderMemberDetails, chat, renderKanban, files }: {
  members: TeamMember[];
  team: Team;
  owner?: string;
  leaderKey?: string;
  renderMemberDetails: (member: TeamMember) => ReactNode;
  chat: (headerTools: ReactNode) => ReactNode;
  renderKanban: (mini: boolean, open: () => void) => ReactNode;
  files: ReactNode | ((active: boolean, tool: TeamWorkspaceTool, onProgramChange?: (program: TeamWorkspaceProgram) => void) => ReactNode);
}) {
  const { t } = useI18n();
  const root = useRef<HTMLDivElement>(null);
  const leftPanel = useRef<HTMLElement>(null);
  const rightPanel = useRef<HTMLElement>(null);
  const lastTrigger = useRef<HTMLElement | null>(null);
  const membersPanelId = useId();
  const toolsPanelId = useId();
  const [width, setWidth] = useState(0);
  const [height, setHeight] = useState(0);
  const [memberPage, setMemberPage] = useState(0);
  const [memberInfo, setMemberInfo] = useState<number | "team" | null>(null);
  const [left, setLeft] = useState(false);
  const [right, setRight] = useState<TeamWorkspacePanel>("closed");
  const [filesVisited, setFilesVisited] = useState(false);
  const [tool, setTool] = useState<TeamWorkspaceTool>("files");
  const [program, setProgram] = useState<TeamWorkspaceProgram>("browser");
  const [boardWide, setBoardWide] = useState(false);
  const [lastSide, setLastSide] = useState<"left" | "right">("right");
  const mode = workspaceMode(width, !!owner);
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
    else root.current?.querySelector<HTMLButtonElement>('.team-workflow-heading')?.focus();
  });
  const openRight = (panel: "kanban" | "files") => {
    rememberTrigger();
    setLastSide("right");
    if (mode !== "dual") setLeft(false);
    setRight(current => current === panel ? "closed" : panel);
    if (panel === "files") { setFilesVisited(true); setTool("files"); }
    if (owner && panel === "kanban" && right === "closed") setBoardWide(false);
  };
  const showOwnerTools = (view: "kanban" | "files" | "instance") => {
    setRight(view === "kanban" ? "kanban" : "files");
    if (view !== "kanban") { setFilesVisited(true); setTool(view === "instance" ? program : "files"); }
  };
  const view = right === "files" && tool !== "files" ? "instance" : right;
  const boardWidthLabel = t(boardWide ? "teams.narrowBoard" : "teams.widenBoard");
  const drawerOpen = mode === "drawer" && (left || right !== "closed");
  useEffect(() => {
    if (!drawerOpen) return;
    const panel = left ? leftPanel.current : rightPanel.current;
    panel?.querySelector<HTMLButtonElement>("button")?.focus();
    const onKey = (event: KeyboardEvent) => {
      // File previews have their own modal layer and keyboard lifecycle.
      if (event.defaultPrevented || document.querySelector('[data-workspace-preview], [data-workspace-editor], [data-team-member-dialog], [data-team-file-dialog][open]')) return;
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

  return <div ref={root} className="team-chat-workspace" data-team-presentation="classic" data-mode={mode} data-left={left} data-right={right} data-owner-view={owner ? view : undefined} data-board-wide={owner && boardWide ? "true" : undefined}>
    <div className="team-member-rail" inert={drawerOpen}>
      <MemberRail members={pageMembers} start={start} leaderKey={leaderKey} total={members.length} expanded={left} panelId={membersPanelId} onDetails={setMemberInfo} onOpen={() => {
        rememberTrigger(); setLastSide("left"); setLeft(value => !value); if (mode !== "dual") setRight("closed");
      }} pager={<MemberPager page={page} pages={pages} onPage={setMemberPage} />} />
    </div>
    <aside id={membersPanelId} ref={leftPanel} hidden={!left} className="team-members-panel" role={mode === "drawer" ? "dialog" : undefined} aria-modal={mode === "drawer" ? true : undefined} aria-label={t("teams.membersLabel")}>
      <header><strong>{t("teams.members")} <span className="team-roster-count">{members.length}</span></strong><div className="team-roster-header-actions"><button type="button" aria-label={t("teams.settings")} title={t("teams.settings")} onClick={() => setMemberInfo("team")}><Settings2 size={17} /></button><button type="button" aria-label={t("teams.collapseMembers")} onClick={() => { setLeft(false); restoreFocus(); }}><PanelLeftClose size={18} /></button></div></header>
      <div className="team-members-body"><MemberCards members={pageMembers} start={start} leaderKey={leaderKey} onDetails={setMemberInfo} /></div>
      <MemberPager page={page} pages={pages} onPage={setMemberPage} />
    </aside>
    <main className="team-chat-main" inert={drawerOpen}>{chat(
      <button type="button" className="team-header-files-trigger" onClick={() => {
        if (owner && right === "files" && tool !== "files") showOwnerTools("files"); else openRight("files");
      }} aria-expanded={owner ? view === "files" : right === "files"} aria-controls={toolsPanelId}><FolderOpen size={17} /><span>{t("teams.files")}</span></button>
    )}</main>
    <aside id={toolsPanelId} ref={rightPanel} className="team-tools-panel" role={drawerOpen && right !== "closed" ? "dialog" : undefined} aria-modal={drawerOpen && right !== "closed" ? true : undefined} aria-label={t("teams.toolsLabel")} inert={drawerOpen && left}>
      <header className="team-tools-toolbar" hidden={right === "closed"}>
        {owner ? <>
          <div className="owner-team-tool-views" role="group" aria-label={t("teams.toolsLabel")}>
            <button type="button" onClick={() => showOwnerTools("kanban")} aria-pressed={view === "kanban"}><Columns3 size={15} /><span>{t("teams.boardTab")}</span></button>
            <button type="button" onClick={() => showOwnerTools("files")} aria-pressed={view === "files"}><FolderOpen size={15} /><span>{t("teams.files")}</span></button>
            <button type="button" onClick={() => showOwnerTools("instance")} aria-pressed={view === "instance"}><SquareTerminal size={15} /><span>{t("teams.instanceTools")}</span></button>
          </div>
          {view === "kanban" && width >= 480 && <button type="button" className="owner-team-board-resize" aria-label={boardWidthLabel} title={boardWidthLabel} aria-pressed={boardWide} onClick={() => setBoardWide(wide => !wide)}>{boardWide ? <Minimize2 size={16} /> : <Maximize2 size={16} />}</button>}
        </> : <>
          {right !== "closed" && <button type="button" onClick={() => openRight("kanban")} aria-pressed={right === "kanban"}>{t("teams.boardTab")}</button>}
          {(["files", "browser", "terminal"] as const).map(kind => <button type="button" key={kind} onClick={() => { if (right !== "files") openRight("files"); setTool(kind); }} aria-pressed={right === "files" && tool === kind}>{t(`workbench.${kind === "browser" ? "web" : kind}`)}</button>)}
        </>}
        {right !== "closed" && <button type="button" aria-label={t("teams.collapseTools")} onClick={() => { setRight("closed"); restoreFocus(); }}><X size={18} /></button>}
      </header>
      <div className="team-kanban-host" hidden={right === "files"}>{renderKanban(right === "closed", () => openRight("kanban"))}</div>
      <div className="team-files-host" hidden={right !== "files"}>{filesVisited && (typeof files === "function" ? files(right === "files" && width > 0, tool, owner ? kind => { setProgram(kind); setTool(kind); } : undefined) : files)}</div>
    </aside>
    {drawerOpen && <button type="button" className="team-drawer-backdrop" aria-label={t("teams.closeDrawer")} onClick={close} />}
    {(memberInfo === "team" || inspectedMember) && <MemberInfoDialog member={inspectedMember} team={team} renderDetails={renderMemberDetails} onClose={() => setMemberInfo(null)} />}
  </div>;
}
