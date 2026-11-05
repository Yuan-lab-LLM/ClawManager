import { useId } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  GitBranch,
  Plus,
  RefreshCw,
  GitCommitHorizontal,
  Check,
  Minus,
  X,
} from "lucide-react";
import { useI18n } from "../../contexts/I18nContext";
import {
  instanceWorkbenchService as service,
  workbenchError,
} from "../../services/instanceWorkbenchService";
import type { GitChange, GitRequest } from "../../types/workbench";
import type { GitViewState } from "./state";
interface Props {
  instanceId: number;
  generation: number;
  enabled: boolean;
  visible: boolean;
  view: GitViewState;
  onChange: (patch: Partial<GitViewState>) => void;
}
function stagedPaths(change: GitChange) {
  return change.index_status === "R" && change.previous_path
    ? [change.previous_path, change.path]
    : [change.path];
}
export function GitProjects({
  instanceId,
  generation,
  enabled,
  visible,
  view,
  onChange,
}: Props) {
  const { t } = useI18n();
  const viewID = useId();
  const cache = useQueryClient();
  const {
    selected,
    adding,
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
  const prefix = ["workbench-git", instanceId, generation];
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
      ? stagedPaths(selectedChange)
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
    onChange({ busy: true, error: "" });
    try {
      const result = await service.git(instanceId, generation, request);
      if (result.project) onChange({ selected: result.project.path });
      await refresh();
      return true;
    } catch (err) {
      onChange({ error: workbenchError(err) });
      return false;
    } finally {
      onChange({ busy: false });
    }
  };
  const staged = changes.filter((c) => c.staged);
  const unstaged = changes.filter(
    (c) => c.untracked || !!c.worktree_status.trim(),
  );
  const files = (items: GitChange[], isStaged: boolean) => (
    <div className="wb-change-group">
      <div className="wb-section-label">
        {t(`workbench.${isStaged ? "staged" : "unstaged"}`)}
        <span>{items.length}</span>
      </div>
      {items.map((change) => (
        <div
          key={change.path}
          className={`wb-change-row ${selectedFile?.path === change.path && selectedFile.staged === isStaged ? "selected" : ""}`}
        >
          <button
            className="wb-file-choice"
            onClick={() =>
              onChange({
                selection: {
                  repo: currentPath!,
                  path: change.path,
                  staged: isStaged,
                },
              })
            }
            title={
              change.previous_path
                ? `${change.previous_path} → ${change.path}`
                : change.path
            }
          >
            <span
              className={`wb-git-status ${change.untracked ? "untracked" : ""}`}
            >
              {isStaged
                ? change.index_status
                : change.untracked
                  ? "U"
                  : change.worktree_status}
            </span>
            <span className="wb-file-path">{change.path}</span>
            {!(change.staged && change.worktree_status.trim()) && (
              <span className="wb-line-count">
                <b>+{change.additions}</b> −{change.deletions}
              </span>
            )}
          </button>
          <button
            className="wb-icon"
            disabled={busy}
            title={t(`workbench.${isStaged ? "unstage" : "stage"}`)}
            aria-label={`${t(`workbench.${isStaged ? "unstage" : "stage"}`)} ${change.path}`}
            onClick={() =>
              void perform({
                operation: isStaged ? "unstage" : "stage",
                path: currentPath!,
                paths: isStaged ? stagedPaths(change) : [change.path],
              })
            }
          >
            {isStaged ? <Minus size={14} /> : <Plus size={14} />}
          </button>
        </div>
      ))}
    </div>
  );
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
          onClick={() => onChange({ adding: "clone" })}
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
      {adding && (
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
                onClick={() => onChange({ adding: "clone" })}
              >
                {t("workbench.clone")}
              </button>
              <button
                type="button"
                className={adding === "import" ? "active" : ""}
                onClick={() => onChange({ adding: "import" })}
              >
                {t("workbench.import")}
              </button>
            </div>
            <button
              type="button"
              className="wb-icon"
              aria-label={t("workbench.cancel")}
              onClick={() => onChange({ adding: null })}
              disabled={busy}
            >
              <X size={16} />
            </button>
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
          <label>
            {t("workbench.projectPath")}
            <input
              autoFocus={adding === "import"}
              required={adding === "clone"}
              aria-label={t("workbench.projectPath")}
              aria-describedby={`wb-project-path-hint-${viewID}`}
              placeholder={adding === "clone" ? "projects/my-project" : "."}
              value={path}
              onChange={(e) => onChange({ path: e.target.value })}
            />
            <small id={`wb-project-path-hint-${viewID}`}>
              {t("workbench.pathHint")}
            </small>
          </label>
          <label>
            {t("workbench.projectName")}
            <input
              value={name}
              onChange={(e) => onChange({ name: e.target.value })}
            />
          </label>
          <button className="wb-button primary" disabled={busy}>
            {busy
              ? t(adding === "clone" ? "workbench.cloning" : "workbench.saving")
              : t(`workbench.${adding}`)}
          </button>
        </form>
      )}
      {!repository && !adding && !projects.isLoading && (
        <div className="wb-empty">
          <GitBranch size={30} />
          <h3>{t("workbench.noProjects")}</h3>
          <p>{t("workbench.pathHint")}</p>
          <button
            className="wb-button primary"
            onClick={() => onChange({ adding: "clone" })}
          >
            {t("workbench.addProject")}
          </button>
        </div>
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
              disabled={busy || !details.data}
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
              disabled={busy}
              onClick={() => onChange({ creatingBranch: !creatingBranch })}
            >
              <Plus size={14} />
            </button>
            <span className="wb-spacer" />
            <button
              className="wb-button"
              disabled={busy}
              onClick={() =>
                void perform({ operation: "pull", path: currentPath! })
              }
            >
              <ArrowDown size={13} />
              {t("workbench.pull")}
            </button>
            <button
              className="wb-button"
              disabled={busy}
              onClick={() =>
                void perform({ operation: "push", path: currentPath! })
              }
            >
              <ArrowUp size={13} />
              {t("workbench.push")}
            </button>
          </div>
          {creatingBranch && (
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
              <button className="wb-button" disabled={busy}>
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
              <div className="wb-changes-list">
                {staged.length > 0 && files(staged, true)}
                {unstaged.length > 0 && files(unstaged, false)}
                {!changes.length && details.data && (
                  <div className="wb-clean">
                    <Check size={17} />
                    {t("workbench.clean")}
                  </div>
                )}
              </div>
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
                      <pre className="wb-diff">
                        {diff.data?.diff
                          ? diff.data.diff.split("\n").map((line, i) => (
                              <span
                                key={i}
                                className={
                                  line.startsWith("+") &&
                                  !line.startsWith("+++")
                                    ? "addition"
                                    : line.startsWith("-") &&
                                        !line.startsWith("---")
                                      ? "deletion"
                                      : line.startsWith("@@")
                                        ? "hunk"
                                        : ""
                                }
                              >
                                {line}
                                {"\n"}
                              </span>
                            ))
                          : t("workbench.noDiff")}
                      </pre>
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
          <form
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
              <button
                className="wb-button primary"
                disabled={busy || !staged.length || !message.trim()}
              >
                <GitCommitHorizontal size={14} />
                {t("workbench.commit")}{" "}
                {staged.length > 0 && `(${staged.length})`}
              </button>
            </div>
          </form>
        </>
      )}
    </div>
  );
}
