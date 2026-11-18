import { useEffect, useRef, type ReactNode } from "react";
import { X } from "lucide-react";
import { useI18n } from "../../contexts/I18nContext";
import {
  slotState,
  formatCPU,
  formatMemory,
  type SlotState,
} from "../../lib/runtimeManagement";
import type { RuntimePod, RuntimeSlot } from "../../types/runtimePool";

export const buttonClass = "app-button-secondary disabled:cursor-not-allowed";
export const primaryClass = "app-button-primary disabled:cursor-not-allowed";
export const inputClass = "app-input min-w-0";
const slotStyles: Record<SlotState, string> = {
  running: "border-emerald-100 bg-emerald-50 text-emerald-600",
  starting: "border-blue-100 bg-blue-50 text-blue-600",
  migrating: "border-amber-200 bg-amber-50 text-amber-800",
  error: "border-red-100 bg-red-50 text-red-500",
  releasing: "border-cyan-200 bg-cyan-50 text-cyan-800",
  idle: "border-slate-200 bg-slate-50 text-slate-400",
  unavailable: "border-slate-300 bg-slate-200 text-slate-500",
  unknown: "border-amber-200 bg-amber-50 text-amber-800",
};
const symbols: Record<SlotState, string> = {
  running: "●",
  starting: "○",
  migrating: "↗",
  error: "!",
  idle: "−",
  releasing: "…",
  unavailable: "×",
  unknown: "?",
};

export function SlotGrid({
  pod,
  selected,
  onSelect,
  compact = false,
}: {
  pod: RuntimePod;
  selected?: number;
  onSelect: (slot: RuntimeSlot) => void;
  compact?: boolean;
}) {
  const { t } = useI18n();
  const slots = pod.slots ?? [];
  if (!slots.length)
    return (
      <p className={compact ? "shrink-0 text-xs leading-4 text-slate-500" : "rounded bg-slate-50 p-3 text-xs text-slate-500"}>
        {t("runtimePods.noInventory")}
      </p>
    );
  return (
    <div
      className={compact ? `runtime-slot-grid grid shrink-0 max-h-10 ${slots.length > 8 ? "min-h-[38px]" : ""} grid-cols-8 gap-0.5 overflow-y-auto overscroll-contain` : "grid grid-cols-4 gap-1.5 sm:grid-cols-8"}
      aria-label={t("runtimePods.distribution")}
    >
      {slots.map((slot) => (
        <button
          key={slot.index}
          type="button"
          aria-pressed={selected === slot.index}
          aria-label={`${t("runtimePods.slot")} ${slot.index} · ${t(`runtimePods.${slotState(slot)}`)}`}
          title={`${t("runtimePods.slot")} ${slot.index} · ${slot.container_name} · ${t(`runtimePods.${slotState(slot)}`)}${slot.instance_id ? ` · ${slot.instance_name ?? "#" + slot.instance_id}` : ""}${slot.expected_instance_id ? ` · ${t("runtimePods.expectedBinding")} #${slot.expected_instance_id}` : ""}${slotState(slot) === "releasing" ? ` · ${t("runtimePods.releasingHelp")}` : slot.observation_message ? ` · ${slot.observation_message}` : ""}`}
          onClick={(event) => {
            event.stopPropagation();
            onSelect(slot);
          }}
          className={`flex items-center justify-between rounded-sm border ${compact ? "px-0.5 py-0 text-xs leading-4" : "px-2 py-1 text-xs"} font-medium ${slotStyles[slotState(slot)]} ${selected === slot.index ? "ring-2 ring-slate-700 ring-offset-1" : ""}`}
        >
          <span>{String(slot.index).padStart(2, "0")}</span>
          {!compact && <span aria-hidden="true">{symbols[slotState(slot)]}</span>}
        </button>
      ))}
    </div>
  );
}

export function SlotOverview({ pod }: { pod: RuntimePod }) {
  const { t } = useI18n();
  const slots = pod.agent_reported === false ? [] : pod.slots ?? [];
  if (!slots.length) return <span className="runtime-slot-overview runtime-no-inventory">{t("runtimePods.noInventory")}</span>;
  const counts = Object.keys(slotStyles).map(state => ({state:state as SlotState, count:slots.filter(slot => slotState(slot) === state).length}));
  const missing = Math.max(0, pod.capacity - slots.length);
  const description = [...counts.filter(item => item.count).map(item => `${t(`runtimePods.${item.state}`)} ${item.count}`), ...(missing ? [`${t("runtimePods.unknown")} ${missing}`] : [])].join(" · ");
  return <div className={`runtime-slot-overview ${pod.capacity <= 16 ? "runtime-slot-individual" : "runtime-slot-segmented"}`} role="img" aria-label={`${t("runtimePods.distribution")}: ${description}`} title={description}>
    {pod.capacity <= 16 ? slots.map(slot => <span key={slot.index} className={`runtime-slot-mark runtime-slot-${slotState(slot)}`} />) : counts.filter(item => item.count).map(item => <span key={item.state} className={`runtime-slot-mark runtime-slot-${item.state}`} style={{flex:item.count}} />)}
    {missing > 0 && <span className="runtime-slot-mark runtime-slot-unknown" style={{flex:missing}} />}
  </div>;
}

export function Legend({ compact = false }: { compact?: boolean }) {
  const { t } = useI18n();
  return (
    <div className={`flex flex-wrap gap-y-1 text-slate-500 ${compact ? "gap-x-2 text-xs" : "gap-x-4 text-xs"}`}>
      {Object.keys(slotStyles).map((state) => (
        <span key={state} className="inline-flex items-center gap-1.5">
          <span
            aria-hidden="true"
            className={`h-2 w-2 rounded-full ${state === "running" ? "bg-emerald-500" : state === "starting" ? "bg-blue-500" : state === "migrating" ? "bg-amber-600" : state === "error" ? "bg-red-500" : state === "releasing" ? "bg-cyan-500" : state === "unknown" ? "bg-amber-500" : state === "idle" ? "bg-slate-300" : "bg-slate-500"}`}
          />
          {t(`runtimePods.${state}`)}
        </span>
      ))}
    </div>
  );
}

export function Metric({
  label,
  value,
  detail,
  warning,
  compact = false,
}: {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  warning?: boolean;
  compact?: boolean;
}) {
  return (
    <div className={`cm-surface min-w-0 ${compact ? "runtime-metric p-2" : "p-4"}`} title={typeof detail === "string" ? detail : undefined}>
      <div className={compact ? "flex items-center justify-between gap-1" : ""}>
      <div className="text-xs text-slate-500">{label}</div>
      <div className={`${compact ? "text-lg leading-5" : "mt-1.5 text-2xl"} whitespace-nowrap font-semibold tabular-nums tracking-tight text-slate-900`}>
        {value}
      </div>
      </div>
      {detail && (
        <div
          className={`${compact ? "sr-only" : "mt-1 text-xs"} ${warning ? "text-amber-800" : "text-slate-500"}`}
        >
          {detail}
        </div>
      )}
    </div>
  );
}

export function ResourceBar({
  pod,
  memory = false,
  compact = false,
}: {
  pod: RuntimePod;
  memory?: boolean;
  compact?: boolean;
}) {
  const { t } = useI18n();
  const used = memory ? pod.memory_bytes_used : pod.cpu_millis_used;
  const limit = memory ? pod.memory_bytes_limit : pod.cpu_millis_limit;
  const percentage =
    limit && used !== undefined && Number.isFinite(used)
      ? Math.min(100, Math.max(0, (used / limit) * 100))
      : undefined;
  const format = memory ? formatMemory : formatCPU;
  return (
    <div className="min-w-0 flex-1" title={t("runtimePods.capacityHelp")}>
      <div className={`flex justify-between gap-2 ${compact ? "mb-0.5 text-xs leading-3" : "mb-2 text-xs"}`}>
        <span className="text-slate-500">
          {t(`runtimePods.${memory ? "memory" : "cpu"}`)}
        </span>
        <span
          className={`font-medium tabular-nums ${percentage && percentage >= 85 ? "text-amber-800" : "text-slate-800"}`}
        >
          {format(used)} / {format(limit)}
          {percentage !== undefined && memory && !compact
            ? ` · ${Math.round(percentage)}%`
            : ""}
        </span>
      </div>
      <div className={`${compact ? "h-0.5" : "h-1"} rounded bg-slate-100`}>
        {percentage !== undefined && (
          <div
            className={`h-full rounded ${percentage >= 85 ? "bg-amber-500" : "bg-blue-500"}`}
            style={{ width: `${percentage}%` }}
          />
        )}
      </div>
    </div>
  );
}

export function RuntimeDialog({
  title,
  children,
  onClose,
  footer,
  wide = false,
  closeOnBackdrop = true,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  footer?: ReactNode;
  wide?: boolean;
  closeOnBackdrop?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const backdropPointer = useRef(false);
  const { t } = useI18n();
  useEffect(() => {
    const dialog = ref.current;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialog?.showModal();
    return () => {
      dialog?.close();
      if (previous?.isConnected) previous.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      aria-labelledby="runtime-dialog-title"
      onCancel={(event) => { event.preventDefault(); onClose(); }}
      onPointerDown={(event) => {
        const bounds = event.currentTarget.getBoundingClientRect();
        backdropPointer.current = event.target === event.currentTarget &&
          (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom);
      }}
      onClick={(event) => {
        const bounds = event.currentTarget.getBoundingClientRect();
        const outside = event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom;
        if (closeOnBackdrop && backdropPointer.current && event.target === event.currentTarget && outside) onClose();
        backdropPointer.current = false;
      }}
      className={`m-auto max-h-[90dvh] ${wide ? "w-[min(95vw,880px)]" : "w-[min(95vw,640px)]"} overflow-hidden rounded-md border border-slate-200 bg-white p-0 text-slate-800 shadow-lg backdrop:bg-slate-950/40 open:flex open:flex-col`}
    >
      <div className="flex shrink-0 items-center justify-between border-b border-slate-100 px-4 py-3 sm:px-6">
        <h2 id="runtime-dialog-title" className="text-sm font-semibold">
          {title}
        </h2>
        <button
          type="button"
          onClick={onClose}
          aria-label={t("runtimePods.close")}
          className="rounded p-1 hover:bg-slate-100"
        >
          <X className="h-5 w-5" />
        </button>
      </div>
      <div className="min-h-0 space-y-5 overflow-y-auto overscroll-contain p-4 sm:p-6">{children}</div>
      {footer && <div className="flex shrink-0 items-center justify-end gap-2 border-t border-slate-100 bg-white px-4 py-3 sm:px-6">{footer}</div>}
    </dialog>
  );
}

export function ErrorMessage({ children }: { children: ReactNode }) {
  return (
    <div
      role="alert"
      className="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700"
    >
      {children}
    </div>
  );
}
