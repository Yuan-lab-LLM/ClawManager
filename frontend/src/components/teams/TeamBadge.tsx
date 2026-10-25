import { useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { BadgeHelp, Blocks, BookOpen, BriefcaseBusiness, ChartNoAxesCombined, ClipboardCheck, ClipboardList, CodeXml, Crown, Database, FlaskConical, GitPullRequest, ListFilter, Newspaper, Palette, PanelsTopLeft, PencilLine, PlugZap, Presentation, RotateCcw, ServerCog, Settings2, Shield, ShieldCheck, UserRound, type LucideIcon } from 'lucide-react';
import { iconEntry, workerIconChoices } from '../../lib/teamIcons';
import './team-badges.css';

const glyphs: Record<string, LucideIcon> = { BadgeHelp, Blocks, BookOpen, BriefcaseBusiness, ChartNoAxesCombined, ClipboardCheck, ClipboardList, CodeXml, Crown, Database, FlaskConical, GitPullRequest, ListFilter, Newspaper, Palette, PanelsTopLeft, PencilLine, PlugZap, Presentation, ServerCog, Settings2, Shield, ShieldCheck, UserRound };
function positionAvatarOptions(popup: HTMLDivElement, avatar: HTMLButtonElement) {
  const anchor = avatar.getBoundingClientRect(), box = popup.getBoundingClientRect();
  popup.style.left = `${Math.max(12, Math.min(anchor.left, window.innerWidth - box.width - 12))}px`;
  popup.style.top = `${Math.max(12, Math.min(anchor.bottom + 8 + box.height <= window.innerHeight - 12 ? anchor.bottom + 8 : anchor.top - box.height - 8, window.innerHeight - box.height - 12))}px`;
}
export default function TeamBadge({ iconKey, size = 'roster', ordinal, className = '' }: { iconKey: string; size?: 'chat'|'roster'|'workflow'|'preview'; ordinal?: number; className?: string }) {
  const entry = iconEntry(iconKey), Glyph = glyphs[entry.glyph] || BriefcaseBusiness;
  const identity = ['leader','user','system','unknown'].includes(entry.key) ? entry.key : 'worker';
  return <span className={`team-functional-badge ${className}`} data-icon-key={entry.key} data-identity={identity} data-size={size} data-leader={entry.key === 'leader'} aria-hidden="true"><Glyph />{ordinal !== undefined && <small>{ordinal}</small>}</span>;
}

export function TeamIconPicker({ value, autoKey, disabled, onChange, label = '职能图标', ordinal }: { value?: string; autoKey: string; disabled?: boolean; onChange: (value: string) => void; label?: string; ordinal?: number }) {
  const id = useId(), trigger = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const reposition = (event: Event) => {
      if (event.target instanceof Node && panel.current?.contains(event.target)) return;
      if (panel.current?.matches(':popover-open') && trigger.current) positionAvatarOptions(panel.current, trigger.current);
    };
    window.addEventListener('resize', reposition);
    window.addEventListener('scroll', reposition, true);
    return () => { window.removeEventListener('resize', reposition); window.removeEventListener('scroll', reposition, true); };
  }, []);
  const toggle = () => {
    const popup = panel.current, avatar = trigger.current;
    if (!popup || !avatar || disabled) return;
    if (popup.matches(':popover-open')) { popup.hidePopover(); return; }
    popup.showPopover();
    positionAvatarOptions(popup, avatar);
    popup.querySelector<HTMLButtonElement>('button[aria-pressed="true"]')?.focus({ preventScroll: true });
  };
  const choose = (key: string) => {
    if (disabled) return;
    panel.current?.hidePopover();
    trigger.current?.focus({ preventScroll: true });
    if (key !== (value || '')) onChange(key);
  };
  return <>
    <button ref={trigger} type="button" className="team-avatar-picker-trigger" aria-label={label} aria-haspopup="dialog" aria-expanded={open} aria-controls={id} disabled={disabled} title={`点击更换头像 · ${iconEntry(value || autoKey).label}`} onClick={toggle}>
      <TeamBadge iconKey={value || autoKey} size="preview" ordinal={ordinal} />
    </button>
    {createPortal(<div ref={panel} id={id} popover="auto" role="dialog" aria-label={`${label}选项`} className="team-avatar-picker-options" onToggle={event => setOpen(event.newState === 'open')} onKeyDown={event => {
      if (!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown','Home','End'].includes(event.key)) return;
      const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not([disabled])')];
      const columns = getComputedStyle(event.currentTarget).gridTemplateColumns.split(' ').length;
      const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowLeft' ? -1 : event.key === 'ArrowRight' ? 1 : event.key === 'ArrowUp' ? -columns : columns) + buttons.length) % buttons.length;
      event.preventDefault(); buttons[next]?.focus();
    }}>
      <button type="button" disabled={disabled} aria-label={`自动匹配 · ${iconEntry(autoKey).label}`} aria-pressed={!value} title={`自动匹配 · ${iconEntry(autoKey).label}`} data-icon-choice="auto" onClick={() => choose('')}><TeamBadge iconKey={autoKey} size="preview" /><RotateCcw className="team-avatar-picker-auto" aria-hidden="true" /></button>
      {workerIconChoices.map(entry => <button key={entry.key} type="button" disabled={disabled} aria-label={entry.label} aria-pressed={value === entry.key} title={entry.label} data-icon-choice={entry.key} onClick={() => choose(entry.key)}><TeamBadge iconKey={entry.key} size="preview" /></button>)}
    </div>, document.body)}
  </>;
}
