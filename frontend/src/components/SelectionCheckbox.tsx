import { useEffect, useRef } from 'react';

interface Props {
  checked: boolean;
  mixed?: boolean;
  disabled?: boolean;
  label: string;
  onChange: (checked: boolean) => void;
}

export default function SelectionCheckbox({ checked, mixed = false, disabled, label, onChange }: Props) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => { if (ref.current) ref.current.indeterminate = mixed; }, [mixed]);
  return <input ref={ref} type="checkbox" className="h-4 w-4 accent-sky-600" checked={checked}
    aria-label={label} aria-checked={mixed ? 'mixed' : checked} disabled={disabled}
    onChange={(event) => onChange(event.target.checked)} />;
}
