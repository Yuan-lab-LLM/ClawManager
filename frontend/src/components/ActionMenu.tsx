import { useId, type ReactNode } from 'react';
import { ChevronDown, MoreHorizontal } from 'lucide-react';
import { useI18n } from '../contexts/I18nContext';
import { useManagementPopover } from './useManagementPopover';
export interface ActionMenuItem { id: string; label: string; onSelect: () => void; disabled?: boolean; destructive?: boolean }
export interface ActionMenuProps { label: string; items: readonly ActionMenuItem[]; textTrigger?: boolean; disabled?: boolean; icon?: ReactNode }
export default function ActionMenu({ label, items, textTrigger = false, disabled = false, icon }: ActionMenuProps) {
  const { t } = useI18n();
  const id = useId();
  const { trigger, panel, items: itemRefs, open, active, setActive, select, triggerKeyDown, panelKeyDown, onToggle, toggle } = useManagementPopover(items, () => 0, index => items[index].onSelect(), disabled);
  return <><button ref={trigger} type="button" className={textTrigger ? "app-button-secondary" : "cm-icon-button"} disabled={disabled} title={textTrigger ? undefined : label} aria-label={label} aria-haspopup="menu" aria-expanded={open} aria-controls={id} onClick={toggle} onKeyDown={triggerKeyDown}>{textTrigger ? <>{label}<ChevronDown size={16} /></> : icon ?? <MoreHorizontal size={16} />}</button>
    <div ref={panel} id={id} popover="auto" role="menu" aria-label={label} className="cm-management-popup cm-control-popup" onToggle={onToggle} onKeyDown={panelKeyDown}>
      {items.length ? items.map((item, index) => <button key={item.id} ref={element => { itemRefs.current[index] = element; }} type="button" role="menuitem" aria-disabled={item.disabled || undefined} tabIndex={index === active ? 0 : -1} className={`cm-popup-item ${item.destructive ? 'cm-popup-danger' : ''}`} onClick={() => select(index)} onFocus={() => setActive(index)}>{item.label}{item.destructive && <span className="cm-visually-hidden"> {t('controls.destructiveAction')}</span>}</button>) : <div className="cm-popup-empty">{t('controls.noActions')}</div>}
    </div></>;
}
