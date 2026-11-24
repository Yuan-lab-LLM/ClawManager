import { useEffect, useState } from 'react';
import api from '../../../services/api';

export type TrialHermesSource = { user_id: number; instance_id: number };
type Session = { id: string; title: string };
type Inventory = TrialHermesSource & { sessions: Session[]; next?: string };

const sessionLimit = 64;
const button = 'app-button-secondary max-md:min-h-11';

export default function TrialHermesHistoryPicker({ source, selected, onChange, disabled, zh }: {
  source: TrialHermesSource;
  selected: string[];
  onChange: (ids: string[]) => void;
  disabled: boolean;
  zh: boolean;
}) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [next, setNext] = useState<string>();
  const [request, setRequest] = useState({ after: '' });
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    api.get<Inventory>('/admin/system-backup/trial/backup-history', {
      signal: controller.signal,
      params: request.after ? { after: request.after } : undefined,
    }).then(({ data }) => {
      if (controller.signal.aborted) return;
      if (data.user_id !== source.user_id || data.instance_id !== source.instance_id) {
        setFailed(true);
        return;
      }
      setSessions((current) => request.after ? [...current, ...data.sessions] : data.sessions);
      setNext(data.next);
    }).catch(() => {
      if (!controller.signal.aborted) setFailed(true);
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false);
    });
    return () => controller.abort();
  }, [request, source.user_id, source.instance_id]);

  function load(after: string) {
    setLoading(true);
    setFailed(false);
    setRequest({ after });
  }

  return <div className="space-y-2 rounded-[var(--cm-radius-panel)] border border-[var(--cm-border-subtle)] p-3" aria-labelledby="trial-hermes-sessions">
    <h4 id="trial-hermes-sessions" className="font-medium">
      {zh ? 'Hermes 会话' : 'Hermes sessions'} · #{source.instance_id}
    </h4>
    {loading && <p role="status">{zh ? '正在读取会话…' : 'Loading sessions…'}</p>}
    {failed && <div className="flex flex-wrap items-center gap-2">
      <p role="alert">{zh ? '无法读取会话。请检查源端采集配置后重试。' : 'Cannot load sessions. Check the source capture configuration and retry.'}</p>
      <button type="button" className={button} disabled={disabled || loading} onClick={() => load(request.after)}>
        {zh ? '重试会话列表' : 'Retry session list'}
      </button>
    </div>}
    {!loading && !failed && sessions.length === 0 && <p>{zh ? '此实例暂无可备份会话。' : 'This instance has no sessions to back up.'}</p>}
    {sessions.length > 0 && <>
      <p>{zh ? `最多选择 ${sessionLimit} 场，恢复所需的关联会话会一并备份。`
        : `Select up to ${sessionLimit} sessions. Related sessions needed for restore are included.`}</p>
      <div className="max-h-52 space-y-1 overflow-y-auto">
        {sessions.map((session) => {
          const checked = selected.includes(session.id);
          return <label key={session.id} className="flex items-start gap-2">
            <input type="checkbox" checked={checked} disabled={disabled || (!checked && selected.length >= sessionLimit)}
              onChange={() => onChange(checked ? selected.filter((id) => id !== session.id) : [...selected, session.id])} />
            <span className="min-w-0 break-words">
              {session.title && <span className="block font-medium">{session.title}</span>}
              <span className={session.title ? 'text-xs text-[var(--cm-muted)]' : undefined}>{session.id}</span>
            </span>
          </label>;
        })}
      </div>
      {next && !failed && <button type="button" className={button} disabled={disabled || loading} onClick={() => load(next)}>
        {zh ? '更多会话' : 'More sessions'}
      </button>}
    </>}
  </div>;
}
