import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { KeyboardEvent, ToggleEvent } from 'react';

// Both listboxes and menus share native top-layer placement and roving focus.
export function useManagementPopover(options: readonly { label: string; disabled?: boolean }[], initialIndex: () => number, onSelect: (index: number) => void, disabled = false) {
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const items = useRef<(HTMLElement | null)[]>([]);
  const [open, setOpen] = useState(false);
  const [active, updateActive] = useState(0);
  const focusedItem = useRef<HTMLElement | null>(null);
  const setActive = (index: number) => {
    focusedItem.current = items.current[index] ?? null;
    updateActive(index);
  };
  const search = useRef({ text: '', time: 0 });
  useEffect(() => { if (disabled && panel.current?.matches(':popover-open')) panel.current.hidePopover(); }, [disabled]);
  const position = () => {
    if (!trigger.current || !panel.current) return;
    const rect = trigger.current.getBoundingClientRect();
    const popup = panel.current;
    if (!rect.width || !rect.height || rect.bottom < 0 || rect.top > innerHeight || rect.right < 0 || rect.left > innerWidth) { popup.hidePopover(); return; }
    const gap = 4;
    const margin = 8;
    popup.style.maxWidth = `${Math.max(0, innerWidth - margin * 2)}px`;
    popup.style.minWidth = `${Math.min(rect.width, innerWidth - margin * 2)}px`;
    const below = innerHeight - rect.bottom - gap - margin;
    const above = rect.top - gap - margin;
    const up = below < Math.min(popup.scrollHeight, 280) && above > below;
    popup.style.maxHeight = `${Math.max(0, Math.min(280, up ? above : below))}px`;
    const bounds = popup.getBoundingClientRect();
    popup.style.left = `${Math.max(margin, Math.min(rect.left, innerWidth - bounds.width - margin))}px`;
    popup.style.top = `${Math.max(margin, up ? rect.top - gap - bounds.height : rect.bottom + gap)}px`;
  };
  useEffect(() => {
    if (!open) return;
    const reposition = () => position();
    window.addEventListener('resize', reposition);
    document.addEventListener('scroll', reposition, true);
    const observer = new ResizeObserver(reposition);
    if (trigger.current) observer.observe(trigger.current);
    return () => { window.removeEventListener('resize', reposition); document.removeEventListener('scroll', reposition, true); observer.disconnect(); };
  }, [open]);
  const focus = (index: number) => {
    setActive(index);
    items.current[index]?.focus({ preventScroll: true });
    items.current[index]?.scrollIntoView({ block: 'nearest' });
  };
  // React preserves keyed nodes when live inventory moves; retain that focused identity.
  useLayoutEffect(() => {
    if (!open) return;
    const index = items.current.findIndex((item, index) => item === focusedItem.current && !!options[index] && !options[index].disabled);
    const next = index >= 0 ? index : options.findIndex(option => !option.disabled);
    if (next >= 0) {
      if (next !== active || document.activeElement !== items.current[next]) focus(next);
    } else {
      if (active !== -1) setActive(-1);
      panel.current?.setAttribute('tabindex', '-1');
      panel.current?.focus();
    }
  });
  const close = (restore = true) => {
    panel.current?.hidePopover();
    setOpen(false);
    if (restore) trigger.current?.focus();
  };
  const show = (last = false) => {
    if (disabled) return;
    panel.current?.showPopover();
    setOpen(true);
    position();
    const enabled = options.map((option, index) => option.disabled ? -1 : index).filter(index => index >= 0);
    const selected = initialIndex();
    const index = last ? enabled.at(-1) : enabled.includes(selected) ? selected : enabled[0];
    if (index !== undefined) focus(index);
    else { panel.current?.setAttribute('tabindex', '-1'); panel.current?.focus(); }
  };
  const select = (index: number) => {
    if (disabled || !options[index] || options[index].disabled) return;
    close();
    onSelect(index);
  };
  const triggerKeyDown = (event: KeyboardEvent) => {
    if (!['ArrowDown', 'ArrowUp'].includes(event.key)) return;
    event.preventDefault();
    show(event.key === 'ArrowUp');
  };
  const panelKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); return; }
    if (event.key === 'Tab') { close(false); return; }
    const focused = items.current.findIndex(item => item === document.activeElement);
    if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); if (focused >= 0) select(focused); return; }
    const enabled = options.map((option, index) => option.disabled ? -1 : index).filter(index => index >= 0);
    if (!enabled.length) return;
    const current = enabled.indexOf(focused);
    const movement: Record<string, number> = { ArrowDown: enabled[(current + 1) % enabled.length], ArrowUp: enabled[(current - 1 + enabled.length) % enabled.length], Home: enabled[0], End: enabled[enabled.length - 1] };
    if (event.key in movement) { event.preventDefault(); focus(movement[event.key]); return; }
    if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      const now = Date.now();
      search.current.text = (now - search.current.time < 700 ? search.current.text : '') + event.key.toLocaleLowerCase();
      search.current.time = now;
      const text = [...search.current.text].every(character => character === search.current.text[0]) ? search.current.text[0] : search.current.text;
      const ordered = [...enabled.slice(current + 1), ...enabled.slice(0, current + 1)];
      const next = ordered.find(index => options[index].label.toLocaleLowerCase().startsWith(text));
      if (next !== undefined) { event.preventDefault(); focus(next); }
    }
  };
  const onToggle = (event: ToggleEvent<HTMLDivElement>) => { setOpen(event.newState === 'open'); };
  return { trigger, panel, items, open, active, setActive, select, triggerKeyDown, panelKeyDown, onToggle, toggle: () => open ? close() : show() };
}
