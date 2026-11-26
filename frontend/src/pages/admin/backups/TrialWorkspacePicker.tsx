import { useEffect, useState } from 'react';
import api from '../../../services/api';

export type TrialWorkspaceSource = {
  user_id: number;
  instance_id: number;
  runtime: string;
  relative_root: string;
};

type DirectoryPage = TrialWorkspaceSource & { relative_dir: string; directories: string[]; next?: string };
const button = 'app-button-secondary max-md:min-h-11';

export default function TrialWorkspacePicker({ source, selected, onChange, disabled, zh }: {
  source: TrialWorkspaceSource;
  selected: string;
  onChange: (dir: string) => void;
  disabled: boolean;
  zh: boolean;
}) {
  const [request, setRequest] = useState<{ dir: string; after?: string }>({ dir: source.relative_root });
  const [page, setPage] = useState<DirectoryPage>();
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    api.get<DirectoryPage>('/admin/system-backup/trial/backup-workspaces', {
      signal: controller.signal,
      params: request,
    }).then(({ data }) => {
      if (controller.signal.aborted) return;
      if (data.user_id !== source.user_id || data.instance_id !== source.instance_id || data.runtime !== source.runtime ||
        data.relative_root !== source.relative_root || data.relative_dir !== request.dir) {
        setFailed(true);
        return;
      }
      setPage((current) => request.after && current?.relative_dir === data.relative_dir
        ? { ...data, directories: [...current.directories, ...data.directories] }
        : data);
    }).catch(() => {
      if (!controller.signal.aborted) setFailed(true);
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false);
    });
    return () => controller.abort();
  }, [request, source.instance_id, source.relative_root, source.runtime, source.user_id]);

  function open(dir: string, after?: string) {
    setLoading(true);
    setFailed(false);
    setRequest({ dir, after });
  }

  const shown = page?.relative_dir === request.dir ? page : undefined;
  return <section aria-labelledby="trial-workspace-directory" className="space-y-2 rounded-[var(--cm-radius-panel)] border border-[var(--cm-border-subtle)] p-3">
    <h4 id="trial-workspace-directory" className="font-medium">
      {zh ? '工作区目录' : 'Workspace directory'} · #{source.instance_id}
    </h4>
    <div className="flex flex-wrap items-center gap-2">
      {request.dir !== source.relative_root && <button type="button" className={button} disabled={disabled || loading}
        onClick={() => open(request.dir.slice(0, request.dir.lastIndexOf('/')))}>
        {zh ? '上一级' : 'Parent directory'}
      </button>}
      <code className="break-all" aria-label={zh ? '当前目录' : 'Current directory'}>{request.dir}</code>
    </div>
    {loading && <p role="status">{zh ? '正在读取目录…' : 'Loading directories…'}</p>}
    {failed && <div className="flex flex-wrap items-center gap-2">
      <p role="alert">{zh ? '无法读取此目录。' : 'Cannot open this directory.'}</p>
      <button type="button" className={button} disabled={disabled} onClick={() => open(request.dir, request.after)}>
        {zh ? '重试目录列表' : 'Retry directory list'}
      </button>
    </div>}
    {shown && <>
      <label className="flex items-center gap-2">
        <input type="checkbox" checked={selected === shown.relative_dir} disabled={disabled || loading}
          onChange={(event) => onChange(event.target.checked ? shown.relative_dir : '')} />
        {zh ? '备份当前目录及子目录' : 'Back up this directory and its subdirectories'}
      </label>
      {shown.directories.length > 0 && <ul className="max-h-52 space-y-1 overflow-y-auto">
        {shown.directories.map((name) => <li key={name}>
          <button type="button" className="break-all rounded px-1 text-left underline disabled:opacity-40"
            disabled={disabled || loading} onClick={() => open(shown.relative_dir + '/' + name)}>{name}</button>
        </li>)}
      </ul>}
      {shown.next && !failed && <button type="button" className={button} disabled={disabled || loading}
        onClick={() => open(shown.relative_dir, shown.next)}>{zh ? '更多目录' : 'More directories'}</button>}
    </>}
    {selected && <p role="status">{zh ? '已选目录 ' : 'Selected directory '}<code className="break-all">{selected}</code></p>}
  </section>;
}
