import { useEffect, useState } from 'react';
import { userService } from '../services/userService';
import type { User } from '../types/user';

export type ModelAccessDraft = { visibility_scope: 'all' | 'selected_users'; allowed_user_ids: number[] };
interface Props {
  scope?: ModelAccessDraft['visibility_scope'];
  userIds?: number[];
  disabled?: boolean;
  label: string;
  onChange: (value: ModelAccessDraft) => void;
}

// This edits a draft only. The enclosing model form owns persistence/versioning.
export default function ModelAccessEditor({ scope = 'all', userIds = [], disabled, label, onChange }: Props) {
  const [open, setOpen] = useState(false);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');
  useEffect(() => {
    if (!open) return;
    let disposed = false;
    setLoading(true); setError('');
    void (async () => {
      try {
        const all: User[] = [];
        for (let page = 1; ; page++) {
          const result = await userService.getUsers(page, 100);
          if (disposed) return;
          all.push(...result.users);
          if (all.length >= result.total || result.users.length === 0) break;
          if (page >= 100) throw new Error('用户过多，请缩小授权管理范围后重试');
        }
        if (!disposed) setUsers([...new Map(all.map((user) => [user.id, user])).values()]);
      } catch { if (!disposed) setError('用户列表加载失败，请关闭后重试；已选授权不会被清空。'); }
      finally { if (!disposed) setLoading(false); }
    })();
    return () => { disposed = true; };
  }, [open]);
  const query = search.trim().toLowerCase();
  const filtered = users.filter((user) => (user.email + ' ' + user.username).toLowerCase().includes(query));
  const toggle = (id: number, checked: boolean) => onChange({
    visibility_scope: 'selected_users',
    allowed_user_ids: checked ? [...new Set([...userIds, id])] : userIds.filter((value) => value !== id),
  });
  return <div className="min-w-40 text-xs">
    <select aria-label={label + ' 可见范围'} className="app-input w-full" disabled={disabled} value={scope}
      onChange={(event) => {
        const value = event.target.value as ModelAccessDraft['visibility_scope'];
        onChange({ visibility_scope: value, allowed_user_ids: value === 'all' ? [] : userIds });
        setOpen(value === 'selected_users');
      }}>
      <option value="all">全员可用</option><option value="selected_users">指定用户</option>
    </select>
    {scope === 'selected_users' && <>
      <button type="button" className="mt-2 text-sky-700" disabled={disabled} aria-expanded={open}
        onClick={() => setOpen(!open)}>已选 {userIds.length} 人 · {open ? '收起' : '选择用户'}</button>
      {userIds.length === 0 && <p role="alert" className="mt-1 text-red-700">请选择至少一位用户</p>}
      {open && <div className="mt-2 space-y-2 rounded border bg-white p-2">
        <input className="app-input w-full" type="search" aria-label={label + ' 搜索用户邮箱或用户名'}
          placeholder="搜索邮箱 / 用户名" value={search} onChange={(e) => setSearch(e.target.value)} />
        {loading && <p role="status">正在加载用户…</p>}
        {error && <p role="alert" className="text-red-700">{error}</p>}
        <div className="max-h-48 overflow-auto">
          {filtered.map((user) => <label key={user.id} className="flex items-start gap-2 py-1">
            <input type="checkbox" checked={userIds.includes(user.id)} disabled={disabled || (!user.is_active && !userIds.includes(user.id))}
              onChange={(e) => toggle(user.id, e.target.checked)} />
            <span className="break-all">{user.email || user.username} ({user.username}){!user.is_active && ' · 已停用'}</span>
          </label>)}
          {!loading && !error && filtered.length === 0 && <p>没有匹配用户</p>}
          {!loading && !error && userIds.filter((id) => !users.some((user) => user.id === id)).map((id) =>
            <label key={id} className="flex gap-2 py-1"><input type="checkbox" checked disabled={disabled}
              onChange={() => toggle(id, false)} />用户 #{id}（已不可用，可取消）</label>)}
        </div>
      </div>}
    </>}
  </div>;
}
