import React, { useEffect, useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";
import { useI18n } from "../contexts/I18nContext";

type Props = {
  storageKey: string;
  title: string;
  icon: React.ReactNode;
  defaultCollapsed?: boolean;
  summary?: React.ReactNode;
  headerActions?: React.ReactNode;
  onExpandedChange?: (expanded: boolean) => void;
  contentClassName?: string;
  children: React.ReactNode;
};

function readStoredCollapsed(storageKey: string, defaultCollapsed: boolean): boolean {
  try {
    const stored = localStorage.getItem(storageKey);
    if (stored === "true") {
      return true;
    }
    if (stored === "false") {
      return false;
    }
  } catch {
    // ignore storage failures
  }
  return defaultCollapsed;
}

export default function InstanceCollapsiblePanel({
  storageKey,
  title,
  icon,
  defaultCollapsed = true,
  summary,
  headerActions,
  onExpandedChange,
  contentClassName,
  children,
}: Props) {
  const { t } = useI18n();
  const [collapsed, setCollapsed] = useState(() => readStoredCollapsed(storageKey, defaultCollapsed));

  const toggle = () => {
    setCollapsed((current) => {
      const next = !current;
      onExpandedChange?.(!next);
      return next;
    });
  };

  useEffect(() => {
    onExpandedChange?.(!collapsed);
  }, [collapsed, onExpandedChange]);

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, String(collapsed));
    } catch {
      // ignore storage failures
    }
  }, [collapsed, storageKey]);

  return (
    <section className={`cm-surface shrink-0 px-4 ${collapsed ? "py-2" : "py-3"}`}>
      <div className="flex items-start justify-between gap-3">
        <button
          type="button"
          onClick={toggle}
          className="flex min-w-0 flex-1 items-start gap-2 text-left"
          aria-expanded={!collapsed}
        >
          <span className="mt-0.5 shrink-0">{icon}</span>
          <span className="min-w-0">
            <span className="flex flex-wrap items-center gap-2">
              <h2 className="text-sm font-semibold text-slate-950">{title}</h2>
              {collapsed ? (
                <ChevronDown className="h-4 w-4 text-slate-400" aria-hidden />
              ) : (
                <ChevronUp className="h-4 w-4 text-slate-400" aria-hidden />
              )}
            </span>
            {collapsed && summary ? (
              <span className="mt-1 block text-xs text-slate-500">{summary}</span>
            ) : null}
          </span>
        </button>
        <div className="flex shrink-0 items-center gap-2">
          {!collapsed ? headerActions : null}
          <button
            type="button"
            onClick={toggle}
            className="rounded-md border border-slate-200 px-2 py-1 text-xs text-slate-600 hover:bg-slate-50"
          >
            {collapsed ? t("instances.panelExpand") : t("instances.panelCollapse")}
          </button>
        </div>
      </div>
      {!collapsed ? (
        <div className={contentClassName ?? "mt-3"}>{children}</div>
      ) : null}
    </section>
  );
}
