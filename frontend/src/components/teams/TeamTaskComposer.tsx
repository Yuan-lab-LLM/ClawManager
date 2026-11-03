import { useEffect, useId, useRef, useState } from 'react';
import { AtSign, ChevronDown, Send, X } from 'lucide-react';
import { beforePageRefresh } from '../PageVersionNotice';
import type { TeamMember } from '../../types/team';
import TeamBadge from './TeamBadge';
import { memberIcon } from '../../lib/teamIcons';
import './team-task-composer.css';

export default function TeamTaskComposer({ members, leaderKey, targetKey, onTargetChange, value, onChange, sending, error, reference, onClearReference, onSubmit }: {
  members: TeamMember[]; leaderKey?: string; targetKey: string; onTargetChange: (key: string) => void;
  value: string; onChange: (value: string) => void; sending: boolean; error: string | null;
  reference?: { id?: number; title: string }; onClearReference: () => void; onSubmit: (event: React.FormEvent) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [index, setIndex] = useState(0);
  const root = useRef<HTMLFormElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  const query = useRef<HTMLInputElement>(null);
  const listId = useId();
  useEffect(() => {
    const guard = (event: Event) => { if (value.trim() || sending) event.preventDefault(); };
    window.addEventListener(beforePageRefresh, guard);
    return () => window.removeEventListener(beforePageRefresh, guard);
  }, [value, sending]);
  const leader = members.find(member => member.member_key === leaderKey) || members.find(member => ['leader', 'team-leader'].includes(member.role));
  const selected = members.find(member => member.member_key === targetKey) || (!targetKey ? leader : undefined);
  const single = !!selected && selected.id !== leader?.id;
  const available = members.filter(member => !['deleted', 'deleting'].includes(member.status));
  const options = available.filter(member => `${member.display_name} ${member.member_key} ${member.role}`.toLowerCase().includes(search.toLowerCase()));
  const activeIndex = Math.min(index, Math.max(0, options.length - 1));
  const close = () => { setOpen(false); setSearch(''); setIndex(0); requestAnimationFrame(() => input.current?.focus()); };
  const select = (member: TeamMember) => { onTargetChange(member.member_key === leader?.member_key ? '' : member.member_key); close(); };
  useEffect(() => {
    if (!open) return;
    query.current?.focus();
    const outside = (event: PointerEvent) => { if (root.current && !root.current.contains(event.target as Node)) setOpen(false); };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [open]);
  useEffect(() => {
    const element = input.current;
    if (!element) return;
    element.style.height = 'auto';
    element.style.height = `${Math.min(160, Math.max(40, element.scrollHeight))}px`;
    element.style.overflowY = element.scrollHeight > 160 ? 'auto' : 'hidden';
  }, [value]);
  const keyboard = (event: React.KeyboardEvent) => {
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); setIndex(current => options.length ? (current + (event.key === 'ArrowDown' ? 1 : -1) + options.length) % options.length : 0); }
    if (event.key === 'Enter') { event.preventDefault(); if (options[activeIndex]) select(options[activeIndex]); }
  };
  return <div className="team-task-composer" data-single-member={single}>
    {error && <div className="team-composer-error" role="alert">{error}</div>}
    <form ref={root} onSubmit={onSubmit} aria-label="发送团队任务">
      <div className="team-composer-context">
        <button type="button" className="team-recipient-trigger" disabled={sending} aria-label="选择任务执行成员" aria-expanded={open} aria-controls={listId} onClick={() => { setOpen(current => !current); setSearch(''); setIndex(0); }}>
          {selected ? <TeamBadge iconKey={memberIcon(selected)} size="workflow" /> : <AtSign size={17} />}
          <strong>@{selected?.display_name || (targetKey ? '成员已失效' : 'Leader')}</strong><span>{single ? '单成员' : '团队协作'}</span><ChevronDown size={14} />
        </button>
        {single && reference && <span className="team-reference-chip" title={reference.title}><span>参考：{reference.title}</span><button type="button" aria-label="移除参考任务" disabled={sending} onClick={onClearReference}><X size={14}/></button></span>}
        {open && <div className="team-recipient-popover" onKeyDown={keyboard}>
          <input ref={query} value={search} onChange={event => { setSearch(event.target.value); setIndex(0); }} placeholder="搜索成员…" aria-label="搜索任务执行成员" role="combobox" aria-expanded="true" aria-controls={listId} aria-activedescendant={options.length ? `${listId}-${options[activeIndex].id}` : undefined} />
          <div id={listId} role="listbox" aria-label="任务执行成员">{options.map((member, optionIndex) => <button type="button" role="option" id={`${listId}-${member.id}`} key={member.id} aria-selected={selected?.id === member.id} data-highlighted={activeIndex === optionIndex} onMouseEnter={() => setIndex(optionIndex)} onClick={() => select(member)}>
            <TeamBadge iconKey={memberIcon(member)} size="workflow" /><span className="team-recipient-details"><strong>{member.display_name}</strong><small>{member.member_key} · {member.id === leader?.id ? 'Leader / 团队协作' : 'Worker / 单成员'}</small></span><em>{member.status === 'busy' ? '忙碌' : member.status === 'offline' ? '离线' : member.status === 'failed' ? '异常' : '成员'}</em>
          </button>)}{options.length === 0 && <p>没有匹配的成员</p>}</div>
        </div>}
      </div>
      <div className="team-composer-input-row">
        <textarea ref={input} value={value} disabled={sending} aria-label="任务内容" rows={1} placeholder={single ? '让这位成员回答，或继续修改之前的产物…' : '发送任务，输入 @ 选择成员…'}
          onChange={event => {
            const next = event.target.value;
            if (next === '@' && !value) { setOpen(true); setSearch(''); setIndex(0); return; }
            onChange(next);
          }}
          onKeyDown={event => {
            if (open) { keyboard(event); return; }
            const cursor = event.currentTarget.selectionStart;
            if (event.key === '@' && !event.nativeEvent.isComposing && (cursor === 0 || /\s/.test(value[cursor - 1] || ''))) {
              event.preventDefault(); setOpen(true); setSearch(''); setIndex(0); return;
            }
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing && event.keyCode !== 229) { event.preventDefault(); if (!sending) event.currentTarget.form?.requestSubmit(); }
          }} />
        <button type="submit" className="team-composer-send" disabled={sending || !value.trim() || !selected} aria-label="发送任务" title="发送任务">{sending ? <span className="team-composer-spinner" /> : <Send size={18}/>}</button>
      </div>
    </form>
  </div>;
}
