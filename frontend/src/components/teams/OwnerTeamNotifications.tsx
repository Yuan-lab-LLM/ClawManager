import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { CircleAlert, CircleCheck, Clock3, LoaderCircle, X } from 'lucide-react';
import { ownerTeamService, type TeamOperation } from '../../services/ownerTeamService';
import { OwnerTeamDialog } from './OwnerTeamDialog';
import './owner-team-feedback.css';

type Kind = 'success' | 'error' | 'pending' | 'warning';
type RecordEntry = { id: string; kind: Kind; title: string; message: string; at: string; operationID?: string; attemptCount?: number; terminal?: boolean };
type Notice = Omit<RecordEntry, 'at'> & { actions?: { label: string; onClick: () => void }[] };
type Feedback = {
  notify: (notice: Notice) => void;
  dismiss: (id?: string) => void;
  openHistory: () => void;
  setOperationHandler: (handler: ((operation: TeamOperation) => void) | null) => void;
};
export const OwnerTeamFeedbackContext = createContext<Feedback | null>(null);
export function useOwnerTeamFeedback() { const feedback = useContext(OwnerTeamFeedbackContext); if (!feedback) throw new Error('Owner Team feedback provider is required'); return feedback; }

export function OwnerTeamNotifications({ owner, children }: { owner: string; children: ReactNode }) {
  const storageKey = `owner-team-notices:${owner}`;
  const [history, setHistory] = useState<RecordEntry[]>(() => {
    try {
      const saved = JSON.parse(sessionStorage.getItem(storageKey) || '[]');
      return Array.isArray(saved) ? saved.filter((item: RecordEntry) => item && typeof item.id === 'string' && ['success', 'error', 'pending', 'warning'].includes(item.kind) && typeof item.title === 'string' && typeof item.message === 'string' && typeof item.at === 'string').slice(0, 20) : [];
    } catch { return []; }
  });
  const [notice, setNotice] = useState<Notice | null>(null), [showHistory, setShowHistory] = useState(false), [inspecting, setInspecting] = useState('');
  const records = useRef(history), handler = useRef<((operation: TeamOperation) => void) | null>(null), alive = useRef(true);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const dismiss = useCallback((id?: string) => setNotice(current => !id || current?.id === id ? null : current), []);
  const notify = useCallback((next: Notice) => {
    if (!alive.current) return;
    const previous = records.current.find(item => item.id === next.id);
    // Fence stale attempts while allowing an explicitly retried operation to
    // enter its next attempt. A failed status query is not an operation terminal.
    if (previous && ((next.attemptCount ?? 0) < (previous.attemptCount ?? 0) || (previous.terminal && !next.terminal && (next.attemptCount ?? 0) <= (previous.attemptCount ?? 0)))) return;
    const entry: RecordEntry = { id: next.id, kind: next.kind, title: next.title, message: next.message.slice(0, 4000), operationID: next.operationID, attemptCount: next.attemptCount, terminal: next.terminal, at: new Date().toISOString() };
    const updated = [entry, ...records.current.filter(item => item.id !== next.id)].slice(0, 20);
    records.current = updated; setHistory(updated); setNotice(next);
    try { sessionStorage.setItem(storageKey, JSON.stringify(updated)); } catch { /* Feedback remains available in memory. */ }
  }, [storageKey]);
  const setOperationHandler = useCallback((next: ((operation: TeamOperation) => void) | null) => { handler.current = next; }, []);
  useEffect(() => { alive.current = true; return () => { alive.current = false; clearTimeout(timer.current); }; }, []);
  const armDismiss = useCallback(() => {
    clearTimeout(timer.current);
    if (notice?.kind === 'success') timer.current = setTimeout(() => dismiss(notice.id), 5000);
  }, [notice, dismiss]);
  useEffect(() => { armDismiss(); return () => clearTimeout(timer.current); }, [armDismiss]);
  const value = useMemo(() => ({ notify, dismiss, openHistory: () => setShowHistory(true), setOperationHandler }), [notify, dismiss, setOperationHandler]);
  const inspect = async (entry: RecordEntry) => {
    if (!entry.operationID) { setNotice({ ...entry }); setShowHistory(false); return; }
    setInspecting(entry.id);
    try {
      const operation = await ownerTeamService.operation(entry.operationID);
      if (!alive.current) return;
      if (handler.current) handler.current(operation);
      else notify({ id: entry.id, operationID: entry.operationID, attemptCount: operation.attempt_count, terminal: ['succeeded', 'failed'].includes(operation.status), kind: operation.status === 'succeeded' ? 'success' : operation.status === 'failed' ? 'error' : 'pending', title: operation.status === 'succeeded' ? '操作已完成' : operation.status === 'failed' ? '操作未完成' : '操作仍在进行', message: operation.error_message || '已读取后台最新状态。' });
      setShowHistory(false);
    } catch { if (alive.current) setNotice({ id: `query:${entry.id}`, kind: 'warning', title: '暂时无法读取进度', message: '原操作记录已保留，请稍后重新检查。' }); }
    finally { if (alive.current) setInspecting(''); }
  };
  return <OwnerTeamFeedbackContext.Provider value={value}>{children}
    {notice && createPortal(<aside className="ot-floating-notice" data-kind={notice.kind} role={notice.kind === 'error' ? 'alert' : 'status'} aria-live="polite" onMouseEnter={() => clearTimeout(timer.current)} onMouseLeave={armDismiss} onFocusCapture={() => clearTimeout(timer.current)} onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) armDismiss(); }}>
      <span className="ot-notice-symbol">{notice.kind === 'success' ? <CircleCheck size={19}/> : notice.kind === 'pending' ? <LoaderCircle size={19}/> : <CircleAlert size={19}/>}</span>
      <div className="ot-notice-content"><strong>{notice.title}</strong><p>{notice.message}</p>{!!notice.actions?.length && <div className="ot-notice-actions">{notice.actions.map(action => <button type="button" key={action.label} onClick={action.onClick}>{action.label}</button>)}</div>}</div>
      <button className="ot-notice-close" type="button" aria-label="关闭通知" onClick={() => dismiss()}><X size={16}/></button>
    </aside>, document.body)}
    {showHistory && <OwnerTeamDialog title="操作记录" onClose={() => setShowHistory(false)}><div className="ot-form-scroll"><p className="ot-muted">当前账户在此浏览器中的最近操作。关闭通知不会取消后台操作。</p>{!history.length && <p className="ot-empty">暂无操作记录</p>}{history.map(entry => <article className="ot-notice-record" key={entry.id}><div><strong>{entry.title}</strong><small>{new Date(entry.at).toLocaleString('zh-CN')}</small></div><p>{entry.message}</p><button type="button" className="ot-secondary" disabled={!!inspecting} onClick={() => void inspect(entry)}><Clock3 size={14}/>{inspecting === entry.id ? '读取中…' : entry.operationID ? '检查最新进度' : '查看详情'}</button></article>)}</div><footer><button type="button" className="ot-secondary" onClick={() => setShowHistory(false)}>关闭</button></footer></OwnerTeamDialog>}
  </OwnerTeamFeedbackContext.Provider>;
}
