import { useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitBranch, Globe, SquareTerminal } from "lucide-react";
import { useI18n } from "../../contexts/I18nContext";
import { useAuthStore } from "../../stores/authStore";
import { useTeamPageEnvironment } from "./TeamPageEnvironment";
import { ScopedInstanceWorkbench } from "../workbench/ScopedInstanceWorkbench";
import { instanceService } from "../../services/instanceService";
import { instanceWorkbenchService } from "../../services/instanceWorkbenchService";
import { workspaceService } from "../../services/workspaceService";
import { ieiSystemService, ieiSystemWorkspaceService, ownerWorkbenchService } from "../../services/ieiSystemService";
import type { TeamMember } from "../../types/team";
import type { TeamWorkspaceProgram, TeamWorkspaceTool } from "./TeamChatWorkspace";
import "./team-workspace-tools.css";

export default function TeamWorkspaceTools({ teamId, members, leaderKey, active, tool, files, onProgramChange }: {
  teamId: number; members: TeamMember[]; leaderKey?: string; active: boolean; tool: TeamWorkspaceTool; files: (active: boolean) => ReactNode;
  onProgramChange?: (program: TeamWorkspaceProgram) => void;
}) {
  const { owner } = useTeamPageEnvironment();
  const cmUser = useAuthStore(s => s.user?.id);
  const scope = owner ? `iei:${owner}:team:${teamId}` : `cm:${cmUser ?? "signed-out"}:team:${teamId}`;
  // Only non-secret navigation preferences. Preserve the chosen execution
  // member across document partition reloads, independently of chat recipients.
  const storageKey = `team-tools:v1:${scope}`;
  const [choice, setChoice] = useState<{ instance?: number }>(() => {
    try { const saved = JSON.parse(sessionStorage.getItem(storageKey) || "null"); return { instance: Number.isSafeInteger(saved?.instance) ? saved.instance : undefined }; } catch { return {}; }
  });
  const { t, locale } = useI18n();
  const zh = locale === "zh";
  const leader = members.find(m => m.member_key === leaderKey) || members.find(m => m.role === "leader");
  // Do not silently execute on another Worker when the Leader has no binding.
  const member = members.find(m => m.instance_id === choice.instance) || (choice.instance ? undefined : leader);
  const instanceId = member?.instance_id;
  useEffect(() => { try { sessionStorage.setItem(storageKey, JSON.stringify(choice)); } catch { /* Storage may be unavailable; the current selection remains usable. */ } }, [storageKey, choice]);
  const metadata = useQuery({
    queryKey: ["team-tool-instance", scope, instanceId],
    queryFn: async () => {
      const item = await (owner ? ieiSystemService.getInstance(instanceId!) : instanceService.getInstance(instanceId!));
      return { id: item.id, runtime_generation: item.runtime_generation, status: item.status };
    },
    enabled: active && tool !== "files" && !!instanceId && !!(owner || cmUser), retry: false,
    refetchInterval: active && tool !== "files" ? 5000 : false,
  });
  const instance = metadata.data;
  return <section className="team-workspace-tools" aria-label={zh ? "团队工作区工具" : "Team workspace tools"}>
    <div className="team-shared-files-view" hidden={tool !== "files"}>{files(active && tool === "files")}</div>
    <div className="team-member-tools-view" hidden={tool === "files"}>
      <label className="team-tool-executor"><span>{zh ? "运行于" : "Run in"}</span><select aria-label={zh ? "工具运行成员" : "Tool execution member"} value={instanceId || ""} onChange={e => setChoice(current => ({ ...current, instance: Number(e.target.value) }))}>
        {!instanceId && <option value="">{zh ? "请选择可用成员" : "Select a member"}</option>}
        {members.filter(m => m.instance_id && m.status !== "deleted" && m.status !== "deleting").map(m => <option key={m.id} value={m.instance_id}>{m.display_name} · {m.runtime_type} #{m.instance_id}</option>)}
      </select></label>
      {onProgramChange && <div className="team-workspace-tool-tabs" role="group" aria-label={zh ? "实例工具类型" : "Instance tool type"}>
        <button type="button" onClick={() => onProgramChange("git")} aria-pressed={tool === "git"}><GitBranch size={15}/>{t("workbench.git")}</button>
        <button type="button" onClick={() => onProgramChange("browser")} aria-pressed={tool === "browser"}><Globe size={15} />{t("workbench.web")}</button>
        <button type="button" onClick={() => onProgramChange("terminal")} aria-pressed={tool === "terminal"}><SquareTerminal size={15} />{t("workbench.terminal")}</button>
      </div>}
      {!instanceId ? <p className="team-tool-message" role="status">{zh ? "当前成员尚未绑定实例，请选择成员。" : "The selected member has no instance binding."}</p> : metadata.error ? <div className="team-tool-message" role="alert"><p>{zh ? "无法读取成员实例，已有文件和会话保留。" : "Unable to read this instance. Files and sessions are retained."}</p><button type="button" className="app-button-secondary" onClick={() => void metadata.refetch()}>{t("workbench.retry")}</button></div> : !instance ? <p className="team-tool-message" role="status">{t("workbench.loading")}</p> : <ScopedInstanceWorkbench
        key={`${scope}:${instanceId}`} scope={scope} instanceId={instanceId}
        runtimeGeneration={instance.runtime_generation ?? 0} isRunning={instance.status === "running" && !metadata.isError}
        visible={active && tool !== "files"} presentation="portal" programKind={tool === "git" ? "git" : tool === "terminal" ? "terminal" : "browser"} hideHeader
        service={owner ? ownerWorkbenchService : instanceWorkbenchService} workspace={owner ? ieiSystemWorkspaceService : workspaceService} canWrite={instance.status === "running"}
      />}
    </div>
  </section>;
}
