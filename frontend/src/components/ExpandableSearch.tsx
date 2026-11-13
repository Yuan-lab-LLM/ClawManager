import { useRef, useState } from 'react';
import { Search, X } from 'lucide-react';
import { useI18n } from '../contexts/I18nContext';

export interface ExpandableSearchProps {
  value: string;
  onChange: (value: string) => void;
  label: string;
  placeholder: string;
}
export default function ExpandableSearch({ value, onChange, label, placeholder }: ExpandableSearchProps) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const visible = expanded || value.length > 0;
  return <div className="cm-expandable-search">
    <button ref={trigger} type="button" className="cm-icon-button" aria-label={label} aria-expanded={visible} hidden={visible} onClick={() => { setExpanded(true); requestAnimationFrame(() => input.current?.focus()); }}><Search size={15} /></button>
    <div className="cm-search-field" hidden={!visible}>
      <Search size={15} aria-hidden="true" />
      <input ref={input} aria-label={label} value={value} placeholder={placeholder} onChange={event => { setExpanded(true); onChange(event.target.value); }} onKeyDown={event => {
        if (event.key !== 'Escape') return;
        event.stopPropagation();
        event.preventDefault();
        if (!value) { setExpanded(false); requestAnimationFrame(() => trigger.current?.focus()); }
      }} />
      {value && <button type="button" className="cm-icon-button" aria-label={t('controls.clearSearch')} onClick={() => { setExpanded(true); onChange(''); input.current?.focus(); }}><X size={14} /></button>}
    </div>
  </div>;
}
