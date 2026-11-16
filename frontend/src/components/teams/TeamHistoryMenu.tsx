import { useEffect, useRef } from "react";
import { useI18n } from "../../contexts/I18nContext";
import { Check, ChevronDown, History } from "lucide-react";

export default function TeamHistoryMenu({ items, activeKey, onSelect }: {
  items: { key: string; title: string; time: string }[];
  activeKey?: string;
  onSelect: (key: string) => void;
}) {
  const { t } = useI18n();
  const details = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const outside = (event: PointerEvent) => { if (details.current && !details.current.contains(event.target as Node)) details.current.open = false; };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, []);
  return <details ref={details} className="team-history-menu" onKeyDown={event => {
    if (event.key === "Escape" && details.current?.open) { event.stopPropagation(); details.current.open = false; details.current.querySelector("summary")?.focus(); }
  }}>
    <summary aria-label={t("teams.history")}><History size={15} /><span>{t("teams.history")}</span><ChevronDown size={13} /></summary>
    <div className="team-history-popover" aria-label={t("teams.historyList")}>
      <header><strong>{t("teams.history")}</strong><span>{t("teams.loaded", { count: items.length })}</span></header>
      <div className="team-history-options">{[...items].reverse().map(item => <button type="button" key={item.key} aria-current={activeKey === item.key ? "true" : undefined}
        onClick={() => { if (details.current) { details.current.open = false; details.current.querySelector("summary")?.focus(); } onSelect(item.key); }}>
        <span className="team-history-option-title">{activeKey === item.key && <Check size={13} />}<span>{item.title}</span></span>
        <small>{item.time}</small>
      </button>)}</div>
    </div>
  </details>;
}
