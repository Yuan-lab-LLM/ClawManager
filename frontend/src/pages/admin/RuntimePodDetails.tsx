import { useState } from "react";
import { ArrowLeft } from "lucide-react";
import { useI18n } from "../../contexts/I18nContext";
import {
  summarizePod,
  slotState,
  formatCPU,
  formatMemory,
  isSchedulable,
  canManageScheduling,
} from "../../lib/runtimeManagement";
import type { RuntimePod } from "../../types/runtimePool";
import {
  buttonClass,
  primaryClass,
  inputClass,
  Metric,
  ResourceBar,
  SlotGrid,
  Legend,
} from "./RuntimeManagementUI";

export default function RuntimePodDetails({
  pod,
  onBack,
  onSchedule,
  onPause,
  onEvents,
  busy,
}: {
  pod: RuntimePod;
  onBack: () => void;
  onSchedule: (ids: number[]) => void;
  onPause: () => void;
  onEvents: () => void;
  busy: boolean;
}) {
  const { t } = useI18n();
  const [selectedSlot, setSelectedSlot] = useState<number>();
  const [selection, setSelection] = useState<number[]>([]);
  const [filter, setFilter] = useState("all");
  const [search, setSearch] = useState("");
  const [instanceFilter, setInstanceFilter] = useState("all");
  const counts = summarizePod(pod);
  const slots = pod.slots ?? [];
  const chosen = slots.find((slot) => slot.index === selectedSlot);
  const rows = slots.filter(
    (slot) =>
      (filter === "all" ||
        (filter === "bound"
          ? !!slot.instance_id
          : filter === "idle"
            ? slotState(slot) === "idle"
            : !slot.ready)) &&
      (instanceFilter === "all" || slotState(slot) === instanceFilter) &&
      [
        slot.container_name,
        slot.instance_name,
        slot.instance_id,
        slot.owner,
      ].some((value) =>
        String(value ?? "")
          .toLowerCase()
          .includes(search.toLowerCase()),
      ),
  );
  const boundRows = rows.flatMap((slot) =>
    slot.instance_id ? [slot.instance_id] : [],
  );
  const selectedIds = selection.filter((id) =>
    slots.some((slot) => slot.instance_id === id),
  );
  return (
    <div className="space-y-5">
      <button
        type="button"
        onClick={onBack}
        className="inline-flex items-center gap-1.5 text-xs text-slate-500 hover:text-slate-900"
      >
        <ArrowLeft className="h-4 w-4" />
        {t("runtimePods.back")}
      </button>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-slate-900">
            {pod.pod_name}
          </h1>
          <p className="mt-1 text-xs text-slate-500">
            {t("runtimePods.node")}: {pod.node_name || t("runtimePods.unknown")} / {pod.pod_ip || "—"} · {pod.capacity} Agent
            · {pod.storage_class || "—"}
          </p>
        </div>
        <div className="flex gap-2">
          <span
            className={`self-center rounded px-2 py-1 text-xs ${isSchedulable(pod) ? "bg-emerald-50 text-emerald-700" : "bg-amber-50 text-amber-700"}`}
          >
            {t(`runtimePods.${isSchedulable(pod) ? "enabled" : "disabled"}`)}
          </span>
          <button
            type="button"
            className={buttonClass}
            disabled={!canManageScheduling(pod) || busy}
            onClick={onPause}
          >
            {t(
              `runtimePods.${pod.scheduling_enabled === false || pod.scheduling_paused ? "resume" : "pause"}`,
            )}
          </button>
          <button
            type="button"
            className={primaryClass}
            disabled={!selectedIds.length}
            onClick={() => onSchedule(selectedIds)}
          >
            {t("runtimePods.batch")}
            {selectedIds.length ? ` (${selectedIds.length})` : ""}
          </button>
        </div>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Metric
          label={t("runtimePods.agentReady")}
          value={`${counts.ready} / ${counts.capacity}`}
          detail={`${t("runtimePods.unavailable")} ${counts.unavailable}`}
          warning={counts.unavailable > 0}
        />
        <Metric
          label={t("runtimePods.occupied")}
          value={`${counts.occupied} / ${counts.capacity}`}
          detail={`${t("runtimePods.running")} ${counts.running} · ${t("runtimePods.error")} ${counts.error}`}
        />
        <Metric
          label={t("runtimePods.cpu")}
          value={`${formatCPU(pod.cpu_millis_used)} / ${formatCPU(pod.cpu_millis_limit)}`}
          detail={t("runtimePods.capacityHelp")}
        />
        <Metric
          label={t("runtimePods.memory")}
          value={`${formatMemory(pod.memory_bytes_used)} / ${formatMemory(pod.memory_bytes_limit)}`}
          detail={t("runtimePods.capacityHelp")}
        />
      </div>
      <section className="cm-surface space-y-4 p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">
            {t("runtimePods.distribution")}
          </h2>
          <Legend />
        </div>
        <div className="flex gap-8">
          <ResourceBar pod={pod} />
          <ResourceBar pod={pod} memory />
        </div>
        <SlotGrid
          pod={pod}
          selected={selectedSlot}
          onSelect={(slot) => setSelectedSlot(slot.index)}
        />
        {chosen && (
          <div className="flex flex-wrap items-center justify-between gap-2 rounded bg-slate-50 p-3 text-xs">
            <span>
              {t("runtimePods.slot")} {chosen.index} · {chosen.container_name} ·
              Agent {t(`runtimePods.${chosen.ready ? "ready" : "unavailable"}`)}{" "}
              ·{" "}
              {chosen.instance_name ||
                (chosen.instance_id
                  ? `#${chosen.instance_id}`
                  : t("runtimePods.unbound"))}{" "}
              ·{" "}
              {chosen.error_message ||
                chosen.reason ||
                t(`runtimePods.${slotState(chosen)}`)}
            </span>
            <div className="flex gap-2">
              <button type="button" className={buttonClass} onClick={onEvents}>
                {t("runtimePods.events")}
              </button>
              {chosen.instance_id && (
                <button
                  type="button"
                  className={buttonClass}
                  onClick={() => onSchedule([chosen.instance_id!])}
                >
                  {t("runtimePods.manual")}
                </button>
              )}
            </div>
          </div>
        )}
      </section>
      <section className="cm-surface overflow-hidden">
        <div className="flex flex-wrap justify-between gap-3 border-b border-slate-100 p-4">
          <div className="flex gap-1">
            {(["all", "bound", "idle", "unavailable"] as const).map((value) => (
              <button
                key={value}
                type="button"
                onClick={() => setFilter(value)}
                aria-pressed={filter === value}
                className={`rounded px-3 py-1.5 text-xs ${filter === value ? "bg-slate-900 text-white" : "text-slate-500 hover:bg-slate-100"}`}
              >
                {t(`runtimePods.${value}`)}{" "}
                {value === "all"
                  ? counts.capacity
                  : value === "bound"
                    ? counts.occupied
                    : counts[value]}
              </button>
            ))}
          </div>
          <div className="flex flex-wrap gap-2">
            <select
              aria-label={t("runtimePods.instanceState")}
              className={inputClass}
              value={instanceFilter}
              onChange={(event) => setInstanceFilter(event.target.value)}
            >
              {["all", "running", "starting", "migrating", "error"].map(
                (value) => (
                  <option key={value} value={value}>
                    {t(`runtimePods.${value}`)}
                  </option>
                ),
              )}
            </select>
            <input
              aria-label={t("runtimePods.search")}
              placeholder={t("runtimePods.search")}
              className={inputClass}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full whitespace-nowrap text-left text-xs">
            <thead className="bg-slate-50 text-slate-500">
              <tr>
                <th className="p-3">
                  <input
                    type="checkbox"
                    aria-label={t("runtimePods.selectInstances")}
                    disabled={!boundRows.length}
                    checked={
                      boundRows.length > 0 &&
                      boundRows.every((id) => selectedIds.includes(id))
                    }
                    onChange={(event) =>
                      setSelection(
                        event.target.checked
                          ? [...new Set([...selectedIds, ...boundRows])]
                          : selectedIds.filter((id) => !boundRows.includes(id)),
                      )
                    }
                  />
                </th>
                {[
                  "slot",
                  "container",
                  "containerState",
                  "instanceOwner",
                  "instanceState",
                  "cpu",
                  "memory",
                  "reason",
                  "actions",
                ].map((key) => (
                  <th key={key} className="px-3 py-3 font-medium">
                    {t(`runtimePods.${key}`)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {rows.map((slot) => (
                <tr
                  key={slot.index}
                  className={
                    selectedSlot === slot.index
                      ? "bg-blue-50/50"
                      : "hover:bg-slate-50"
                  }
                >
                  <td className="p-3">
                    <input
                      type="checkbox"
                      aria-label={`${t("runtimePods.slot")} ${slot.index}`}
                      disabled={!slot.instance_id}
                      checked={
                        !!slot.instance_id &&
                        selectedIds.includes(slot.instance_id)
                      }
                      onChange={(event) => {
                        const id = slot.instance_id!;
                        setSelection(
                          event.target.checked
                            ? [...selection, id]
                            : selection.filter((value) => value !== id),
                        );
                      }}
                    />
                  </td>
                  <td className="px-3 py-3">
                    <button
                      type="button"
                      className="font-medium hover:underline"
                      onClick={() => setSelectedSlot(slot.index)}
                    >
                      {String(slot.index).padStart(2, "0")}
                    </button>
                  </td>
                  <td className="px-3 py-3 font-mono text-slate-500">
                    {slot.container_name}
                  </td>
                  <td
                    className={`px-3 py-3 ${slot.ready ? "text-emerald-600" : "text-amber-600"}`}
                  >
                    {t(`runtimePods.${slot.ready ? "ready" : "unavailable"}`)}
                  </td>
                  <td className="px-3 py-3">
                    {slot.instance_id ? (
                      <>
                        {slot.instance_name || `#${slot.instance_id}`}{" "}
                        <span className="text-slate-400">
                          #{slot.instance_id} / {slot.owner || "—"}
                        </span>
                      </>
                    ) : (
                      <span className="text-slate-400">
                        {t("runtimePods.unbound")}
                      </span>
                    )}
                  </td>
                  <td
                    className={`px-3 py-3 ${slotState(slot) === "error" ? "text-red-600" : "text-slate-600"}`}
                  >
                    {slot.instance_id
                      ? t(`runtimePods.${slotState({ ...slot, ready: true })}`)
                      : "—"}
                  </td>
                  <td className="px-3 py-3 tabular-nums">
                    {formatCPU(slot.cpu_millis_used)}
                  </td>
                  <td className="px-3 py-3 tabular-nums">
                    {formatMemory(slot.memory_bytes_used)}
                  </td>
                  <td
                    className="max-w-56 truncate px-3 py-3 text-slate-500"
                    title={slot.error_message || slot.reason}
                  >
                    {slot.error_message ||
                      slot.reason ||
                      (slot.restart_count
                        ? t("runtimePods.restarts", {
                            count: slot.restart_count,
                          })
                        : "—")}
                  </td>
                  <td className="px-3 py-3">
                    <div className="flex gap-3">
                      {slot.instance_id && (
                        <button
                          type="button"
                          className="text-slate-700 hover:underline"
                          onClick={() => onSchedule([slot.instance_id!])}
                        >
                          {t("runtimePods.manual")}
                        </button>
                      )}
                      <button
                        type="button"
                        className="text-slate-500 hover:underline"
                        onClick={onEvents}
                      >
                        {t("runtimePods.events")}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="flex justify-between border-t border-slate-100 p-3 text-xs text-slate-500">
          <span>
            {t("runtimePods.podCount", { count: 1, shown: rows.length })} Agent
            · {t("runtimePods.selected", { count: selectedIds.length })}
          </span>
          <span>
            {rows.length} / {counts.capacity}
          </span>
        </div>
      </section>
    </div>
  );
}
