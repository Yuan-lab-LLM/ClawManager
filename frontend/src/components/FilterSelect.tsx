import { useId } from 'react';
import { ChevronDown } from 'lucide-react';
import { useI18n } from '../contexts/I18nContext';
import { useManagementPopover } from './useManagementPopover';

export interface FilterOption { value: string; label: string; disabled?: boolean }
export interface FilterSelectProps { label: string; value: string; options: readonly FilterOption[]; onChange: (value: string) => void; disabled?: boolean }
export default function FilterSelect({ label, value, options, onChange, disabled }: FilterSelectProps) {
  const { t } = useI18n();
  const id = useId();
  const { trigger, panel, items: itemRefs, open, active, setActive, select, triggerKeyDown, panelKeyDown, onToggle, toggle } = useManagementPopover(options, () => options.findIndex(option => option.value === value), index => onChange(options[index].value), disabled);
  const selected = options.find(option => option.value === value);
  return <>
    <button ref={trigger} type="button" className="app-button-secondary cm-filter-trigger" aria-label={`${label}: ${selected?.label ?? t('controls.noOptions')}`} aria-haspopup="listbox" aria-expanded={open} aria-controls={id} disabled={disabled} onClick={toggle} onKeyDown={triggerKeyDown}><span>{selected?.label ?? label}</span><ChevronDown size={13} aria-hidden="true" /></button>
    <div ref={panel} id={id} popover="auto" role="listbox" aria-label={label} className="cm-management-popup cm-control-popup" onToggle={onToggle} onKeyDown={panelKeyDown}>
      {options.length ? options.map((option, index) => <div key={option.value} ref={element => { itemRefs.current[index] = element; }} role="option" aria-selected={option.value === value} aria-disabled={option.disabled || undefined} tabIndex={index === active ? 0 : -1} className="cm-popup-item" onClick={() => select(index)} onFocus={() => setActive(index)}>{option.label}</div>) : <div className="cm-popup-empty">{t('controls.noOptions')}</div>}
    </div>
  </>;
}
