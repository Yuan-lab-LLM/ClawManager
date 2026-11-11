import { useWorkbenchServices } from "./WorkbenchServices";
import { useId, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  GitBranch,
  Folder,
  ChevronRight,
  Home,
  Plus,
  RefreshCw,
  GitCommitHorizontal,
  Check,
  Minus,
  X,
  FileCode,
  FileImage,
  File,
  UserRound,
} from "lucide-react";
import { useI18n } from "../../contexts/I18nContext";
import {
  workbenchError,
} from "../../services/instanceWorkbenchService";
import type { GitChange, GitRequest } from "../../types/workbench";
import type { GitViewState } from "./state";
import { bulkPaths, changePaths, changeStats, imagePreviewPath } from "./gitPresentation";
import { GitDiff } from "./GitDiff";
interface Props {
  instanceId: number;
  generation: number;
  enabled: boolean;
  visible: boolean;
  view: GitViewState;
  onChange: (patch: Partial<GitViewState>) => void;
}
export function GitProjects({
  instanceId,
  generation,
  enabled,
  visible,
  view,
  onChange,
}: Props) {
  const { t, locale } = useI18n();
  const { service, workspace: workspaceService, scope, canWrite, workspaceKey } = useWorkbenchServices();
  const fileScope = workspaceKey ?? instanceId;
  const viewID = useId();
  const cache = useQueryClient();
  const [identity, setIdentity] = useState({ repo: null as string | null, open: false, name: "", email: "", error: "", loading: false });
  const {
    selected,
    folderPath,
    adding: requestedAdding,
    path,
    name,
    url,
    branch,
    creatingBranch,
    message,
    mode,
    busy,
    error,
    selection,
  } = view;
  const prefix = ["workbench-git", scope, instanceId, generation];
  const projects = useQuery({
    queryKey: [...prefix, "projects"],
    queryFn: ({ signal }) =>
      service.git(instanceId, generation, { operation: "projects" }, signal),
    enabled: enabled && visible,
    staleTime: 15000,
  });
  const projectList = projects.data?.projects ?? [];
  const currentPath =
    selected !== null && projectList.some((p) => p.path === selected)
      ? selected
      : (projectList[0]?.path ?? null);
  const repository = projectList.find((p) => p.path === currentPath);
  const adding = requestedAdding ?? (
    canWrite && currentPath === null && !projects.isLoading && !projects.error ? "import" : null
  );
  const folders = useQuery({
    queryKey: ["workspace", fileScope, folderPath],
    queryFn: () => workspaceService.list(instanceId, folderPath),
    enabled: enabled && visible && !!adding,
    staleTime: 0,
    retry: false,
  });
  const hasGit = folders.data?.some((entry) => entry.name === ".git") ?? false;
  const navigateFolder = (next: string) =>
    onChange({
      folderPath: next,
      ...(adding === "import" ? { path: next } : {}),
      error: "",
    });

  const details = useQuery({
    queryKey: [...prefix, currentPath, "details"],
    queryFn: async ({ signal }) => {
      const request = (operation: string) =>
        service.git(
          instanceId,
          generation,
          { operation, path: currentPath!, limit: 30 },
          signal,
        );
      const [status, branches, history] = await Promise.all([
        request("status"),
        request("branches"),
        request("history"),
      ]);
      return {
        status: status.status!,
        branches: branches.branches ?? [],
        history: history.history ?? [],
      };
    },
    enabled: enabled && visible && currentPath !== null,
    staleTime: 10000,
  });
  const changes = details.data?.status.changes ?? [];
  const selectedFile = selection?.repo === currentPath ? selection : null;
  const selectedChange = changes.find((change) => change.path === selectedFile?.path);
  const diffPaths = selectedFile
    ? selectedFile.staged && selectedChange
      ? changePaths(selectedChange, true)
      : [selectedFile.path]
    : [];
  const diff = useQuery({
    queryKey: [
      ...prefix,
      currentPath,
      "diff",
      diffPaths,
      selectedFile?.staged,
    ],
    queryFn: ({ signal }) =>
      service.git(
        instanceId,
        generation,
        {
          operation: "diff",
          path: currentPath!,
          paths: diffPaths,
          staged: selectedFile!.staged,
        },
        signal,
      ),
    enabled: enabled && visible && !!selectedFile && mode === "changes",
  });
  const refresh = async () => {
    await cache.invalidateQueries({ queryKey: prefix });
  };
  const perform = async (request: GitRequest) => {
    if (!canWrite) return false;
    onChange({ busy: true, error: "" });
    try {
      const result = await service.git(instanceId, generation, request);
      if (result.project) onChange({ selected: result.project.path });
      await refresh();
      if (request.operation === "clone") {
        await cache.invalidateQueries({ queryKey: ["workspace", fileScope] });
        await cache.invalidateQueries({ queryKey: ["workspace-search", fileScope] });
      }
      return true;
    } catch (err) {
      const data = (err as { response?: { data?: { code?: string; error_code?: string; error?: string | { code?: string } } } })?.response?.data;
      if (request.operation === "commit" && (data?.code === "missing_identity" || data?.error_code === "missing_identity" || (typeof data?.error === "object" && data.error?.code === "missing_identity") || /missing_identity|author identity unknown|unable to auto-detect email/i.test(workbenchError(err)))) {
        setIdentity(previous => ({ ...previous, ...(previous.repo !== currentPath ? { name: "", email: "" } : {}), repo: currentPath, open: true, loading: false, error: t("workbench.identityRequired") }));
        onChange({ error: t("workbench.identityRequired") });
      } else onChange({ error: workbenchError(err) });
      return false;
    } finally {
      onChange({ busy: false });
    }
  };
  const staged = changes.filter((c) => c.staged);
  const unstaged = changes.filter(
    (c) => c.untracked || !!c.worktree_status.trim(),
  );
  const openIdentity = async () => {
    const repo = currentPath;
    setIdentity({ repo, open: true, name: "", email: "", error: "", loading: true });
    try {
      const result = await service.git(instanceId, generation, { operation: "identity", path: repo! });
      setIdentity(previous => previous.repo === repo && previous.loading ? { ...previous, name: result.identity?.name ?? "", email: result.identity?.email ?? "", loading: false } : previous);
    } catch {
      setIdentity(previous => previous.repo === repo ? { ...previous, loading: false } : previous);
    }
  };
  const files = (items: GitChange[], isStaged: boolean) => {
    const paths = bulkPaths(items, isStaged);
    return <div className="wb-change-group">
      <div className="wb-section-label">
        {t(`workbench.${isStaged ? "staged" : "unstaged"}`)}<span>{items.length}</span>
        <button className="wb-button wb-bulk" disabled={busy || !canWrite || !paths?.length}
          title={!paths ? t("workbench.bulkLimit") : undefined}
          onClick={() => paths && void perform({ operation: isStaged ? "unstage" : "stage", path: currentPath!, paths })}>
          {isStaged ? <Minus size={13} /> : <Plus size={13} />}{t(`workbench.${isStaged ? "unstageAll" : "stageAll"}`)}
        </button>
      </div>
      {!paths && <p className="wb-warning">{t("workbench.bulkLimit")}</p>}
      {items.map(change => {
        const stats = changeStats(change, isStaged);
        const status = change.untracked ? "untracked" : ({ A: "added", M: "modified", D: "deleted", R: "renamed", C: "copied", U: "conflicted", T: "typeChanged" }[isStaged ? change.index_status : change.worktree_status] ?? "modified");
        const parent = change.path.split("/").slice(0, -1).join("/");
        const image = imagePreviewPath("", change.path);
        const Icon = image ? FileImage : /\.(ts|tsx|js|jsx|go|py|json|md|css|html|yaml|yml)$/i.test(change.path) ? FileCode : File;
        return <div key={change.path} className={`wb-change-row ${selectedFile?.path === change.path && selectedFile.staged === isStaged ? "selected" : ""}`}>
          <button className="wb-file-choice" onClick={() => onChange({ selection: { repo: currentPath!, path: change.path, staged: isStaged } })}
            title={change.previous_path ? `${change.previous_path} → ${change.path}` : change.path}>
            <Icon size={15} className="wb-file-type" aria-hidden="true" />
            <span className="wb-file-path"><strong>{change.path.split("/").pop()}</strong>{parent && <small>{parent}</small>}</span>
            <span className={`wb-git-status ${change.untracked ? "untracked" : ""}`}>{t(`workbench.${status}`)}</span>
            {change.binary ? <span className="wb-file-marker">{t("workbench.binary")}</span> : stats ?
              <span className="wb-line-count"><b>+{stats.additions}</b> −{stats.deletions}</span> :
              <span className="wb-file-marker" title={t("workbench.unknownStats")}>—</span>}
          </button>
          <button className="wb-icon" disabled={busy || !canWrite} title={t(`workbench.${isStaged ? "unstage" : "stage"}`)}
            aria-label={`${t(`workbench.${isStaged ? "unstage" : "stage"}`)} ${change.path}`}
            onClick={() => void perform({ operation: isStaged ? "unstage" : "stage", path: currentPath!, paths: changePaths(change, isStaged) })}>
            {isStaged ? <Minus size={14} /> : <Plus size={14} />}
          </button>
        </div>;
      })}
    </div>;
  };
  return (
    <div className="wb-git">
      <div className="wb-project-bar">
        <label className="sr-only" htmlFor={`wb-project-${viewID}`}>
          {t("workbench.selectProject")}
        </label>
        <select
          id={`wb-project-${viewID}`}
          value={currentPath ?? ""}
          disabled={!projectList.length || busy}
          onChange={(e) => {
            onChange({ selected: e.target.value, selection: null });
          }}
        >
          {!projectList.length && (
            <option value="">{t("workbench.noProjects")}</option>
          )}
          {projectList.map((p) => (
            <option key={p.path} value={p.path}>
              {p.name} · {p.path || t("workbench.root")}
            </option>
          ))}
        </select>
        <button
          className="wb-icon"
          title={t("workbench.refresh")}
          aria-label={t("workbench.refresh")}
          disabled={busy || projects.isFetching || details.isFetching}
          onClick={() => void refresh()}
        >
          <RefreshCw
            size={15}
            className={
              projects.isFetching || details.isFetching ? "animate-spin" : ""
            }
          />
        </button>
        <button
          className="wb-button"
          disabled={!canWrite} onClick={() => onChange({ adding: "import", path: folderPath, error: "" })}
        >
          <Plus size={14} />
          {t("workbench.addProject")}
        </button>
      </div>
      {(error || projects.error || details.error) && (
        <div className="wb-error" role="alert">
          {error || workbenchError(projects.error || details.error)}
        </div>
      )}
      {busy && (
        <div className="wb-notice" role="status">
          <RefreshCw size={13} className="animate-spin" />
          {t("workbench.loading")}
        </div>
      )}
      {canWrite && adding && (
        <form
          className="wb-add-project"
          onSubmit={async (e) => {
            e.preventDefault();
            if (
              await perform({
                operation: adding,
                path,
                name: name.trim(),
                url: adding === "clone" ? url.trim() : undefined,
              })
            ) {
              onChange({ adding: null, path: "", url: "", name: "" });
            }
          }}
        >
          <div className="wb-form-heading">
            <div className="wb-segment">
              <button
                type="button"
                className={adding === "clone" ? "active" : ""}
                onClick={() => onChange({ adding: "clone", error: "" })}
              >
                {t("workbench.clone")}
              </button>
              <button
                type="button"
                className={adding === "import" ? "active" : ""}
                disabled={!canWrite} onClick={() => onChange({ adding: "import", path: folderPath, error: "" })}
              >
                {t("workbench.import")}
              </button>
            </div>
            {requestedAdding && (
              <button
                type="button"
                className="wb-icon"
                aria-label={t("workbench.cancel")}
                onClick={() => onChange({ adding: null })}
                disabled={busy || !canWrite}
              >
                <X size={16} />
              </button>
            )}
          </div>
          {adding === "clone" && (
            <label>
              {t("workbench.repositoryURL")}
              <input
                autoFocus
                required
                placeholder="https://github.com/owner/project.git"
                value={url}
                onChange={(e) => onChange({ url: e.target.value })}
              />
            </label>
          )}
          {!repository && <p className="wb-folder-intro">{t("workbench.browseHint")}</p>}
          <div className="wb-folder-picker" role="group" aria-label={t("workbench.workspaceFolders")}>
            <div className="wb-folder-toolbar">
              <button type="button" className="wb-icon" title={t("workbench.root")} aria-label={t("workbench.root")}
                disabled={busy || !canWrite} onClick={() => { navigateFolder(""); void cache.invalidateQueries({ queryKey: ["workspace", fileScope, ""], exact: true }); }}>
                <Home size={15} />
              </button>
              <button type="button" className="wb-icon" title={t("workbench.parentFolder")} aria-label={t("workbench.parentFolder")}
                disabled={busy || !folderPath} onClick={() => navigateFolder(folderPath.split("/").slice(0, -1).join("/"))}>
                <ArrowUp size={15} />
              </button>
              <code title={folderPath || t("workbench.root")}>{folderPath || t("workbench.root")}</code>
              <button type="button" className="wb-icon" title={t("workbench.refreshFolders")} aria-label={t("workbench.refreshFolders")}
                disabled={busy || folders.isFetching} onClick={() => void folders.refetch()}>
                <RefreshCw size={15} className={folders.isFetching ? "animate-spin" : ""} />
              </button>
            </div>
            {folders.error ? (
              <div className="wb-error" role="alert">{workbenchError(folders.error)}</div>
            ) : folders.isLoading ? (
              <div className="wb-loading" role="status">{t("workbench.loading")}</div>
            ) : (
              <>
                <div className="wb-folder-list">
                  {folders.data?.filter((entry) => entry.is_dir && entry.name !== ".git").map((entry) => (
                    <button type="button" key={entry.path} disabled={busy || !canWrite} className="wb-folder-row"
                      aria-label={t("workbench.openFolder", { name: entry.name })} title={entry.path}
                      onClick={() => navigateFolder(entry.path)}>
                      <Folder size={15} /><span>{entry.name}</span><ChevronRight size={14} />
                    </button>
                  ))}
                  {folders.data && !folders.data.some((entry) => entry.is_dir && entry.name !== ".git") && (
                    <p className="wb-folder-hint">{t("workbench.noFolders")}</p>
                  )}
                </div>
                {adding === "import" && folders.data && (
                  <p className="wb-folder-hint" role="status">{t(hasGit ? "workbench.repositoryFound" : "workbench.notRepository")}</p>
                )}
              </>
            )}
            {adding === "clone" && (
              <button type="button" className="wb-button" disabled={busy || !folders.data || !!folders.error}
                onClick={() => {
                  const dirname = url.trim().split(/[/:]/).pop()?.replace(/\.git$/, "") || path.split("/").filter(Boolean).pop() || "project";
                  onChange({ path: [folderPath, dirname].filter(Boolean).join("/") });
                }}>{t("workbench.useCloneParent")}</button>
            )}
          </div>

          <label>
            {t("workbench.projectPath")}
            <input
              autoFocus={requestedAdding === "import"}
              required={adding === "clone"}
              aria-label={t("workbench.projectPath")}
              aria-describedby={`wb-project-path-hint-${viewID}`}
              placeholder={adding === "clone" ? "projects/my-project" : "."}
              value={path}
              onChange={(e) => onChange({ path: e.target.value })}
            />
            <small id={`wb-project-path-hint-${viewID}`}>
              {t(adding === "clone" ? "workbench.clonePathHint" : "workbench.pathHint")}
            </small>
          </label>
          <label>
            {t("workbench.projectName")}
            <input
              value={name}
              onChange={(e) => onChange({ name: e.target.value })}
            />
          </label>
          <button className="wb-button primary" disabled={busy || (adding === "import" && path === folderPath && (!hasGit || !!folders.error || folders.isFetching))}>
            {busy
              ? t(adding === "clone" ? "workbench.cloning" : "workbench.saving")
              : t(`workbench.${adding}`)}
          </button>
        </form>
      )}
      {repository && (
        <>
          <div className="wb-repository-meta">
            <code title={repository.path}>
              {repository.path || t("workbench.root")}
            </code>
            {repository.remote && (
              <span title={repository.remote}>{repository.remote}</span>
            )}
          </div>
          <div className="wb-git-toolbar">
            <GitBranch size={14} />
            <label className="sr-only" htmlFor={`wb-branch-${viewID}`}>
              {t("workbench.branch")}
            </label>
            <select
              id={`wb-branch-${viewID}`}
              value={details.data?.status.branch ?? ""}
              disabled={busy || !canWrite || !details.data}
              onChange={(e) =>
                void perform({
                  operation: "checkout",
                  path: currentPath!,
                  branch: e.target.value,
                })
              }
            >
              {details.data?.status.detached && (
                <option value={details.data.status.branch}>
                  {t("workbench.detached")}
                </option>
              )}
              {details.data &&
                !details.data.status.detached &&
                !details.data.branches.some(
                  (b) => !b.remote && b.name === details.data.status.branch,
                ) && (
                  <option value={details.data.status.branch}>
                    {details.data.status.branch}
                  </option>
                )}
              {details.data?.branches
                .filter((b) => !b.remote)
                .map((b) => (
                  <option key={b.name} value={b.name}>
                    {b.name}
                  </option>
                ))}
            </select>
            <button
              className="wb-icon"
              title={t("workbench.newBranch")}
              aria-label={t("workbench.newBranch")}
              disabled={busy || !canWrite}
              onClick={() => onChange({ creatingBranch: !creatingBranch })}
            >
              <Plus size={14} />
            </button>
            <span className="wb-spacer" />
            <button
              className="wb-button"
              disabled={busy || !canWrite}
              onClick={() =>
                void perform({ operation: "pull", path: currentPath! })
              }
            >
              <ArrowDown size={13} />
              {t("workbench.pull")}
            </button>
            <button
              className="wb-button"
              disabled={busy || !canWrite}
              onClick={() =>
                void perform({ operation: "push", path: currentPath! })
              }
            >
              <ArrowUp size={13} />
              {t("workbench.push")}
            </button>
          </div>
          {canWrite && creatingBranch && (
            <form
              className="wb-inline-form"
              onSubmit={async (e) => {
                e.preventDefault();
                if (
                  await perform({
                    operation: "create-branch",
                    path: currentPath!,
                    branch: branch.trim(),
                  })
                ) {
                  onChange({ branch: "", creatingBranch: false });
                }
              }}
            >
              <input
                autoFocus
                required
                aria-label={t("workbench.branchName")}
                placeholder={t("workbench.branchName")}
                value={branch}
                onChange={(e) => onChange({ branch: e.target.value })}
              />
              <button className="wb-button" disabled={busy || !canWrite}>
                {t("workbench.create")}
              </button>
              <button
                type="button"
                className="wb-icon"
                aria-label={t("workbench.cancel")}
                onClick={() => onChange({ creatingBranch: false })}
              >
                <X size={14} />
              </button>
            </form>
          )}
          <div className="wb-git-modes">
            <button
              className={mode === "changes" ? "active" : ""}
              onClick={() => onChange({ mode: "changes" })}
            >
              {t("workbench.changes")} <span>{changes.length}</span>
            </button>
            <button
              className={mode === "history" ? "active" : ""}
              onClick={() => onChange({ mode: "history" })}
            >
              {t("workbench.history")}
            </button>
            <span className="wb-spacer" />
            {details.data && (
              <small>
                ↑{details.data.status.ahead} ↓{details.data.status.behind}
              </small>
            )}
          </div>
          {details.isLoading && (
            <div className="wb-loading">{t("workbench.loading")}</div>
          )}
          {mode === "changes" ? (
            <div className="wb-changes-body">
              <div className="wb-changes-list" role="region" aria-label={t("workbench.fileChanges")} tabIndex={0}>
                {staged.length > 0 && files(staged, true)}
                {unstaged.length > 0 && files(unstaged, false)}
                {!changes.length && details.data && (
                  <div className="wb-clean">
                    <Check size={17} />
                    {t("workbench.clean")}
                  </div>
                )}
              </div>
              {staged.length + unstaged.length > 4 && <div className="wb-scroll-hint">{t("workbench.scrollChanges")}</div>}
              <div className="wb-diff-panel">
                {selectedFile ? (
                  <>
                    <div className="wb-diff-heading">
                      <code>{selectedFile.path}</code>
                      <small>
                        {t(
                          selectedFile.staged
                            ? "workbench.staged"
                            : "workbench.unstaged",
                        )}
                      </small>
                    </div>
                    {diff.isFetching ? (
                      <div className="wb-loading">{t("workbench.loading")}</div>
                    ) : diff.error ? (
                      <div className="wb-error" role="alert">
                        {workbenchError(diff.error)}
                      </div>
                    ) : (
                      <GitDiff key={`${currentPath}:${selectedFile.path}:${selectedFile.staged}:${diff.dataUpdatedAt}:${locale}`} instanceId={instanceId}
                        imagePath={imagePreviewPath(currentPath!, selectedFile.path, diff.data?.preview_path)}
                        binary={!!diff.data?.binary || !!selectedChange?.binary} diff={diff.data?.diff ?? ""} />
                    )}
                    {diff.data?.truncated && (
                      <p className="wb-warning">{t("workbench.truncated")}</p>
                    )}
                  </>
                ) : (
                  <div className="wb-diff-placeholder">
                    {t("workbench.diffHint")}
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="wb-history">
              {details.data?.history.length ? (
                details.data.history.map((commit) => (
                  <article key={commit.hash}>
                    <GitCommitHorizontal size={16} />
                    <div>
                      <strong>{commit.message}</strong>
                      <small>
                        {commit.author} · <code>{commit.hash.slice(0, 8)}</code>{" "}
                        · {new Date(commit.timestamp * 1000).toLocaleString()}
                      </small>
                    </div>
                  </article>
                ))
              ) : (
                <div className="wb-empty">{t("workbench.noHistory")}</div>
              )}
            </div>
          )}
          {canWrite && identity.open && identity.repo === currentPath && (
            <form className="wb-identity-form" onSubmit={async e => {
              e.preventDefault();
              if (!identity.name.trim() || !identity.email.trim()) { setIdentity(previous => ({...previous,error:t("workbench.identityRequired")})); return; }
              onChange({busy:true,error:""});
              setIdentity(previous => ({...previous,error:""}));
              try {
                await service.git(instanceId, generation, {operation:"set-identity",path:currentPath!,name:identity.name.trim(),email:identity.email.trim()});
                setIdentity(previous => ({...previous,open:false}));
              } catch (err) { setIdentity(previous => ({...previous,error:workbenchError(err)})); }
              finally { onChange({busy:false}); }
            }}>
              <div className="wb-form-heading"><strong>{t("workbench.commitIdentity")}</strong>
                <button type="button" className="wb-icon" disabled={busy || !canWrite} aria-label={t("workbench.cancel")}
                  onClick={() => setIdentity(previous => ({...previous,open:false}))}><X size={15}/></button>
              </div>
              <small>{t("workbench.identityHint")}</small>
              {identity.error && <div className="wb-error" role="alert">{identity.error}</div>}
              <label>{t("workbench.identityName")}<input required autoComplete="name" disabled={busy || !canWrite || identity.loading} value={identity.name}
                onChange={e => setIdentity(previous => ({...previous,name:e.target.value}))}/></label>
              <label>{t("workbench.identityEmail")}<input required type="email" autoComplete="email" disabled={busy || !canWrite || identity.loading} value={identity.email}
                onChange={e => setIdentity(previous => ({...previous,email:e.target.value}))}/></label>
              <button className="wb-button" disabled={busy || !canWrite || identity.loading}>{t(identity.loading ? "workbench.loading" : busy ? "workbench.saving" : "workbench.saveIdentity")}</button>
            </form>
          )}
          {canWrite && <form
            className="wb-commit-form"
            onSubmit={async (e) => {
              e.preventDefault();
              if (
                await perform({
                  operation: "commit",
                  path: currentPath!,
                  message: message.trim(),
                })
              ) {
                onChange({ message: "", selection: null });
              }
            }}
          >
            <label className="sr-only" htmlFor={`wb-commit-${viewID}`}>
              {t("workbench.commitMessage")}
            </label>
            <textarea
              id={`wb-commit-${viewID}`}
              rows={2}
              value={message}
              placeholder={t("workbench.commitMessage")}
              onChange={(e) => onChange({ message: e.target.value })}
              maxLength={64000}
            />
            <div>
              <small>{t("workbench.commitHint")}</small>
              <button type="button" className="wb-icon" disabled={busy || !canWrite} title={t("workbench.commitIdentity")} aria-label={t("workbench.commitIdentity")}
                onClick={() => void openIdentity()}><UserRound size={16}/></button>
              <button
                className="wb-button primary"
                disabled={busy || !staged.length || !message.trim()}
              >
                <GitCommitHorizontal size={14} />
                {t("workbench.commit")}{" "}
                {staged.length > 0 && `(${staged.length})`}
              </button>
            </div>
          </form>}
        </>
      )}
    </div>
  );
}
