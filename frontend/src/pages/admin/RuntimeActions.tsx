import { useRef, useState } from "react";
import axios from "axios";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, AlertTriangle } from "lucide-react";
import { useI18n } from "../../contexts/I18nContext";
import {
  availableTargets,
  formatCPU,
  formatMemory,
  runtimeError,
  summarizePod,
  slotState,
  canManageSlot,
  canEvictPod,
  recoveryInstanceIds,
  failedMoveRetrySelections,
} from "../../lib/runtimeManagement";
import { runtimePoolService } from "../../services/runtimePoolService";
import { formatInstanceType } from "../../types/instance";
import type {
  RuntimePod,
  RuntimeSchedulingPreflight,
  RuntimeSchedulingRequest,
  RuntimeType,
} from "../../types/runtimePool";
import {
  buttonClass,
  primaryClass,
  inputClass,
  ErrorMessage,
  RuntimeDialog,
} from "./RuntimeManagementUI";

const runtimeTypes: RuntimeType[] = ["openclaw", "hermes", "opencode", "deepseek-harness"];

export default function RuntimeActions({
  kind,
  pod,
  initialIds,
  verifiedRecoveryIds = [],
  initialEvictionMode = "graceful",
  retryOperationId,
  pods,
  onClose,
  onSubmitted,
  onRetry,
}: {
  kind: "schedule" | "evict" | "events" | "history";
  pod?: RuntimePod;
  initialIds?: number[];
  verifiedRecoveryIds?: number[];
  initialEvictionMode?: "graceful" | "force" | "recovery" | "orphan-recovery";
  retryOperationId?: string;
  pods: RuntimePod[];
  onClose: () => void;
  onSubmitted: () => void;
  onRetry: (ids: number[], operationId: string) => void;
}) {
  const { t } = useI18n();
  const history = useQuery({
    queryKey: ["runtime-scheduling"],
    queryFn: () => runtimePoolService.schedulingHistory(),
    enabled: kind === "history" || kind === "evict" || !!retryOperationId,
    refetchInterval: kind === "history" ? 3000 : false,
  });
  const instances = [
    ...new Map(
      pods.flatMap((source) =>
        (source.slots ?? [])
          .filter(canManageSlot)
          .map((item) => [item.instance_id!, { slot: item, source }] as const),
      ).concat(retryOperationId
        ? failedMoveRetrySelections(pods, history.data?.items.find(item => item.id === retryOperationId))
            .map(item => [item.slot.instance_id, item] as const)
        : []),
    ).values(),
  ];
  const initialSelection = instances.filter((item) => initialIds?.includes(item.slot.instance_id!));
  const initialResolved = initialSelection.length === (initialIds?.length ?? 0);
  const initialType = pod?.runtime_type ?? (
    initialResolved && initialSelection.length > 0 &&
    initialSelection.every((item) => item.source.runtime_type === initialSelection[0].source.runtime_type)
      ? initialSelection[0].source.runtime_type : ""
  );
  const invalidInitial = initialResolved && !!initialIds?.length && (
    !initialType || initialSelection.some((item) =>
      item.source.runtime_type !== initialType || (!!pod && item.source.id !== pod.id),
    )
  );
  const [selectedType, setSelectedType] = useState<RuntimeType | "">(initialType);
  const [inferInitialType, setInferInitialType] = useState(!!initialIds?.length && !initialResolved);
  const [typeVersion, setTypeVersion] = useState(0);
  const typeVersionRef = useRef(0);
  const [ids, setIds] = useState(initialEvictionMode === "recovery" ? initialIds ?? [] : invalidInitial ? [] : initialIds ?? []);
  const [targetId, setTargetId] = useState("");
  const [slot, setSlot] = useState("");
  const [preflight, setPreflight] = useState<{
    key: string;
    result: RuntimeSchedulingPreflight;
  }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | undefined>(invalidInitial ? t("runtimePods.invalidInitialSelection") : undefined);
  const [grace, setGrace] = useState(30);
  const [concurrency, setConcurrency] = useState(2);
  const [acknowledged, setAcknowledged] = useState(false);
  const [evictionMode, setEvictionMode] = useState<"graceful" | "force" | "recovery" | "orphan-recovery">(
    initialEvictionMode,
  );
  if (inferInitialType && initialResolved) {
    setInferInitialType(false);
    setSelectedType(initialType);
    if (invalidInitial) setIds([]);
    setError(invalidInitial ? t("runtimePods.invalidInitialSelection") : undefined);
  }
  const forceSupported =
    history.data?.supported_modes.includes("force") === true;
  const events = useQuery({
    queryKey: ["runtime-pod-events", pod?.id],
    queryFn: () => runtimePoolService.listEvents(pod!.id),
    enabled: kind === "events" && !!pod,
    refetchInterval: kind === "events" ? 5000 : false,
  });
  const selected = instances.filter((item) =>
    ids.includes(item.slot.instance_id!),
  );
  const sources = [
    ...new Map(
      (selected.length ? selected.map((item) => item.source) : pod ? [pod] : [])
        .map((source) => [source.id, source]),
    ).values(),
  ];
  const selectionMatchesType = !!selectedType && ids.length > 0 &&
    selected.length === ids.length && selected.every((item) =>
      item.source.runtime_type === selectedType && (!pod || item.source.id === pod.id),
    );
  const candidates = kind === "schedule"
    ? selectionMatchesType ? availableTargets(pods.filter((item) => item.runtime_type === selectedType), sources) : []
    : availableTargets(pods, sources);
  const target = candidates.find((item) => item.id === Number(targetId));
  const request: RuntimeSchedulingRequest = {
    ...(retryOperationId ? { retry_operation_id: retryOperationId } : {}),
    instance_ids: ids,
    target_pod_id: Number(targetId),
    ...(slot && ids.length === 1 ? { target_slot: Number(slot) } : {}),
  };
  const orphanMode = evictionMode === "orphan-recovery";
  const orphanRequest: RuntimeSchedulingRequest = { instance_ids: [], target_pod_id: 0, mode: "orphan-recovery", source_pod_id: pod?.id, source_pod_uid: pod?.pod_uid };
  const fingerprint = JSON.stringify({
    evictionMode,
    orphanSource: orphanMode ? orphanRequest : undefined,
    selectedType,
    typeVersion,
    request,
    sources: selected.map((item) => [
      item.source.id,
      item.slot.generation,
      item.slot.gateway_id,
    ]),
    target: target && [
      target.id,
      target.image_ref,
      target.image_digest,
      target.cpu_millis_limit,
      target.memory_bytes_limit,
      target.scheduling_enabled,
      target.slots
        ?.filter((item) => slotState(item) === "idle")
        .map((item) => item.index),
    ],
  });
  const validated = preflight?.key === fingerprint && preflight.result.allowed;
  const canCheck =
    selectionMatchesType &&
    !!target &&
    target.runtime_type === selectedType &&
    summarizePod(target).available >= ids.length;
  const occupied = pod ? summarizePod(pod).occupied : 0;
  const incoming = candidates.reduce(
    (count, item) => count + summarizePod(item).available,
    0,
  );
  const insufficientEvictionCapacity =
    evictionMode === "graceful" && incoming < occupied;
  const recoverySupported = history.data?.supported_modes.includes("recovery") === true;
  const recoveryIds = new Set([...(pod ? recoveryInstanceIds(pod) : []), ...verifiedRecoveryIds.filter((id) => Number.isSafeInteger(id) && id > 0)]);
  const recoverySelectionValid = ids.every((id) => recoveryIds.has(id));
  const orphanSupported = history.data?.supported_modes.includes("orphan-recovery") === true;
  const evictionAllowed = !!pod && (orphanMode ? pod.id > 0 && !!pod.pod_uid && orphanSupported : evictionMode === "recovery"
    ? pod.id > 0 && recoverySupported && recoverySelectionValid
    : canEvictPod(pod));
  const submit = async (check = false) => {
    if (kind === "evict" && (!evictionAllowed || (!check && (!acknowledged || (orphanMode && !validated))) || busy || insufficientEvictionCapacity || (evictionMode === "force" && !forceSupported))) return;
    if (kind === "schedule" && (
      typeVersion !== typeVersionRef.current || !canCheck || busy || (!check && !validated)
    )) return;
    setBusy(true);
    setError(undefined);
    try {
      if (check) {
        setAcknowledged(false);
        const result = await runtimePoolService.preflightScheduling(orphanMode ? orphanRequest : request);
        if (typeVersion === typeVersionRef.current) setPreflight({ key: fingerprint, result });
        return;
      }
      if (kind === "evict" && pod)
        await runtimePoolService.evict(pod.id, {
          mode: evictionMode,
          ...(orphanMode ? { source_pod_uid: pod.pod_uid, instance_ids: [] } : evictionMode === "recovery" ? { instance_ids: ids } : { grace_period_seconds: grace, concurrency }),
        });
      else await runtimePoolService.schedule(request);
      if (kind !== "schedule" || typeVersion === typeVersionRef.current) onSubmitted();
    } catch (err) {
      if (kind === "schedule" && typeVersion !== typeVersionRef.current) return;
      setError(
        kind === "evict" &&
          axios.isAxiosError(err) &&
          !err.response &&
          ["ERR_NETWORK", "ECONNABORTED", "ETIMEDOUT"].includes(err.code ?? "")
          ? t("runtimePods.evictionOutcomeUnknown")
          : runtimeError(err),
      );
      setPreflight(undefined);
    } finally {
      if (kind !== "schedule" || typeVersion === typeVersionRef.current) setBusy(false);
    }
  };
  const cancelOperation = async (id: string) => {
    setBusy(true);
    setError(undefined);
    try {
      await runtimePoolService.cancelScheduling(id);
      await history.refetch();
    } catch (err) {
      setError(runtimeError(err));
    } finally {
      setBusy(false);
    }
  };
  const retryOperation = async (id: string) => {
    if (busy) return;
    setBusy(true); setError(undefined);
    try { await runtimePoolService.retryOperation(id); await history.refetch(); }
    catch (err) { setError(runtimeError(err)); }
    finally { setBusy(false); }
  };
  const titleKey =
    kind === "schedule"
      ? "manual"
      : kind === "evict"
        ? "evictTitle"
        : kind === "history"
          ? "history"
          : "events";
  return (
    <RuntimeDialog title={t(`runtimePods.${titleKey}`)} onClose={onClose}>
      {error && <ErrorMessage>{error}</ErrorMessage>}
      {kind === "schedule" && (
        <>
          <section className="space-y-3">
            <h3 className="text-sm font-semibold">{t("runtimePods.selectRuntimeType")}</h3>
            <select
              className={`${inputClass} w-full`}
              aria-label={t("runtimePods.chooseRuntimeType")}
              value={selectedType}
              disabled={busy || !!pod}
              onChange={(event) => {
                const next = event.target.value as RuntimeType | "";
                if ((next && !runtimeTypes.includes(next)) || (pod && next !== pod.runtime_type)) return;
                typeVersionRef.current++;
                setTypeVersion(typeVersionRef.current);
                setSelectedType(next);
                setInferInitialType(false);
                setIds([]);
                setTargetId("");
                setSlot("");
                setPreflight(undefined);
                setError(undefined);
                setBusy(false);
              }}
            >
              <option value="">{t("runtimePods.chooseRuntimeType")}</option>
              {(pod ? [pod.runtime_type] : runtimeTypes).map((type) => (
                <option key={type} value={type}>{formatInstanceType(type)}</option>
              ))}
            </select>
          </section>
          <section className="space-y-3">
            <h3 className="text-sm font-semibold">
              {t("runtimePods.selectInstances")}
            </h3>
            <div className="max-h-44 space-y-2 overflow-y-auto rounded border border-slate-200 p-3">
              {!selectedType && <p role="status" className="text-xs text-slate-500">{t("runtimePods.chooseTypeFirst")}</p>}
              {instances
                .filter(
                  (item) =>
                    item.source.runtime_type === selectedType &&
                    (!pod || item.source.id === pod.id),
                )
                .map((item) => (
                  <label
                    key={item.slot.instance_id}
                    className="flex items-start gap-3 rounded p-1 text-xs hover:bg-slate-50"
                  >
                    <input
                      type="checkbox"
                      checked={ids.includes(item.slot.instance_id!)}
                      disabled={busy}
                      onChange={(event) => {
                        if (item.source.runtime_type !== selectedType || (pod && item.source.id !== pod.id)) return;
                        setIds(
                          event.target.checked
                            ? [...ids, item.slot.instance_id!]
                            : ids.filter((id) => id !== item.slot.instance_id),
                        );
                        setSlot("");
                      }}
                    />
                    <span>
                      <span className="font-medium">
                        {item.slot.instance_name || `#${item.slot.instance_id}`}{" "}
                        #{item.slot.instance_id}
                      </span>
                      <span className="mt-1 block text-slate-500">
                        {item.slot.owner || "—"} ·{" "}
                        {formatInstanceType(item.source.runtime_type)} ·{" "}
                        {item.source.pod_name} / {t("runtimePods.slot")}{" "}
                        {item.slot.index} · {t("runtimePods.node")}: {" "}
                        {item.source.node_name || t("runtimePods.unknown")}
                      </span>
                    </span>
                  </label>
                ))}
            </div>
            <p className="text-xs text-slate-500">
              {t("runtimePods.selected", { count: ids.length })}
            </p>
          </section>
          <section className="space-y-3">
            <h3 className="text-sm font-semibold">{t("runtimePods.target")}</h3>
            <select
              className={`${inputClass} w-full`}
              aria-label={t("runtimePods.chooseTarget")}
              value={targetId}
              disabled={busy || !selectionMatchesType}
              onChange={(event) => {
                setTargetId(event.target.value);
                setSlot("");
              }}
            >
              <option value="">{t("runtimePods.chooseTarget")}</option>
              {sources.length > 0 && (
                <optgroup label={t("runtimePods.currentPods")}>
                  {sources.map((item) => (
                    <option key={item.id} value={item.id} disabled>
                      {t("runtimePods.currentPod")} · {item.pod_name} ·{" "}
                      {t("runtimePods.node")}: {item.node_name || t("runtimePods.unknown")} ·{" "}
                      {t("runtimePods.incoming")} {summarizePod(item).available} /{" "}
                      {item.capacity}
                    </option>
                  ))}
                </optgroup>
              )}
              <optgroup label={t("runtimePods.targetPods")}>
                {candidates.map((item) => (
                  <option
                    key={item.id}
                    value={item.id}
                    disabled={summarizePod(item).available < ids.length}
                  >
                    {item.pod_name} · {t("runtimePods.node")}: {item.node_name || t("runtimePods.unknown")} ·{" "}
                    {t("runtimePods.incoming")} {summarizePod(item).available} /{" "}
                    {item.capacity}
                  </option>
                ))}
              </optgroup>
            </select>
            {selectionMatchesType && candidates.length === 0 && (
              <p className="text-xs text-amber-700">
                {t("runtimePods.noTargets")}
              </p>
            )}
            {target && (
              <>
                <p className="text-xs text-slate-500">
                  CPU {formatCPU(target.cpu_millis_used)} /{" "}
                  {formatCPU(target.cpu_millis_limit)} ·{" "}
                  {t("runtimePods.memory")}{" "}
                  {formatMemory(target.memory_bytes_used)} /{" "}
                  {formatMemory(target.memory_bytes_limit)} ·{" "}
                  {target.storage_class || "—"}
                </p>
                <label className="block space-y-2 text-xs">
                  <span>{t("runtimePods.targetSlot")}</span>
                  <select
                    className={`${inputClass} w-full`}
                    value={slot}
                    aria-label={t("runtimePods.targetSlot")}
                    disabled={ids.length !== 1 || busy}
                    onChange={(event) => setSlot(event.target.value)}
                  >
                    <option value="">{t("runtimePods.automaticSlot")}</option>
                    {target.slots
                      ?.filter((item) => slotState(item) === "idle")
                      .map((item) => (
                        <option key={item.index} value={item.index}>
                          {t("runtimePods.slot")} {item.index} ·{" "}
                          {item.container_name} · {t("runtimePods.ready")}
                        </option>
                      ))}
                  </select>
                </label>
              </>
            )}
          </section>
          <section className="space-y-3">
            <h3 className="text-sm font-semibold">{t("runtimePods.checks")}</h3>
            {!preflight && (
              <p role="status" className="text-xs text-slate-500">
                {t("runtimePods.needsValidation")}
              </p>
            )}
            {preflight && (
              <>
                {preflight.key !== fingerprint && (
                  <p role="status" className="text-xs text-amber-700">
                    {t("runtimePods.validationStale")}
                  </p>
                )}
                {preflight.result.allowed ? (
                  validated ? (
                    <p className="flex items-center gap-2 rounded bg-emerald-50 p-3 text-xs text-emerald-700">
                      <CheckCircle2 className="h-4 w-4" />
                      {t("runtimePods.checkPassed")}
                    </p>
                  ) : null
                ) : (
                  <ErrorMessage>
                    {preflight.result.blockers?.join(" · ")}
                  </ErrorMessage>
                )}
                {preflight.result.warnings?.map((message) => (
                  <p key={message} className="text-xs text-amber-700">
                    {message}
                  </p>
                ))}
              </>
            )}
            <p className="text-xs leading-5 text-slate-500">
              {t("runtimePods.stopCheck")}
            </p>
            <div className="rounded bg-amber-50 p-3 text-xs leading-5 text-amber-700">
              {t("runtimePods.migrationHelp")}
            </div>
            <p className="text-xs text-slate-400">
              {t("runtimePods.sequence")}
            </p>
          </section>
          <div className="flex flex-wrap justify-end gap-2 border-t border-slate-100 pt-4">
            <button type="button" className={buttonClass} onClick={onClose}>
              {t("runtimePods.cancel")}
            </button>
            <button
              type="button"
              className={buttonClass}
              disabled={!canCheck || busy}
              onClick={() => void submit(true)}
            >
              {t("runtimePods.checkTarget")}
            </button>
            <button
              type="button"
              className={primaryClass}
              disabled={!validated || !canCheck || busy}
              onClick={() => void submit()}
            >
              {t(
                busy ? "runtimePods.startingOp" : "runtimePods.confirmSchedule",
              )}
            </button>
          </div>
        </>
      )}
      {kind === "evict" && pod && (
        <form className="space-y-5" onSubmit={(event) => event.preventDefault()}>
          <p className="text-xs leading-5 text-slate-500">
            {t("runtimePods.evictDescription")}
          </p>
          {!evictionAllowed && (
            <p role="status" className="rounded bg-amber-50 p-3 text-xs text-amber-800">
              {t("runtimePods.reconciliationBlocked")}
            </p>
          )}
          <div className="rounded border border-slate-200 p-4">
            <p className="text-sm font-medium">{pod.pod_name}</p>
            <p className="mt-2 text-xs text-slate-500">
              {formatInstanceType(pod.runtime_type)} · {t("runtimePods.node")}: {" "}
              {pod.node_name || t("runtimePods.unknown")} ·{" "}
              {pod.capacity} Agent · {summarizePod(pod).occupied}{" "}
              {t("runtimePods.bound")}
            </p>
          </div>
          <div
            className={`rounded border p-4 ${evictionMode === "graceful" ? "border-blue-200 bg-blue-50/50" : "border-slate-200"}`}
          >
            <label className="flex items-center gap-2 text-sm font-medium">
              <input
                type="radio"
                name="eviction-mode"
                value="graceful"
                checked={evictionMode === "graceful"}
                disabled={busy}
                onChange={() => {
                  setEvictionMode("graceful");
                  setAcknowledged(false);
                  setError(undefined);
                }}
              />
              {t("runtimePods.graceful")}
            </label>
            <p className="ml-5 mt-2 text-xs leading-5 text-slate-500">
              {t("runtimePods.gracefulHelp")}
            </p>
          </div>
          <div
            className={`rounded border p-4 ${!forceSupported ? "border-slate-200 text-slate-400" : evictionMode === "force" ? "border-red-300 bg-red-50/50" : "border-slate-200"}`}
          >
            <label className="flex items-center gap-2 text-sm font-medium">
              <input
                type="radio"
                name="eviction-mode"
                value="force"
                checked={evictionMode === "force"}
                disabled={busy || !forceSupported}
                onChange={() => {
                  setEvictionMode("force");
                  setAcknowledged(false);
                  setError(undefined);
                  if (grace < 30) setGrace(30);
                }}
              />
              {t("runtimePods.force")}
            </label>
            <p className="ml-5 mt-2 text-xs leading-5">
              {t(
                forceSupported
                  ? "runtimePods.forceHelp"
                  : "runtimePods.forceUnavailable",
              )}
            </p>
          </div>
          <div className={`rounded border p-4 ${evictionMode === "recovery" ? "border-amber-300 bg-amber-50" : "border-slate-200"}`}>
            <label className="flex items-center gap-2 text-sm font-medium">
              <input type="radio" name="eviction-mode" value="recovery" checked={evictionMode === "recovery"} disabled={busy || !recoverySupported}
                onChange={() => { setEvictionMode("recovery"); setAcknowledged(false); setError(undefined); }} />
              {t("runtimePods.faultRecovery")}
            </label>
            <p className="ml-5 mt-2 text-xs leading-5">{t("runtimePods.faultRecoveryHelp")}</p>
            {!recoverySupported && <p role="status" className="mt-2 text-xs">{t("runtimePods.faultRecoveryUnavailable")}</p>}
            {evictionMode === "recovery" && <>
              <p className="mt-3 text-xs leading-5">{ids.length ? t("runtimePods.faultRecoverySelected", { ids: ids.join(", #") }) : t("runtimePods.faultRecoveryDefault")}</p>
              {!recoverySelectionValid && <ErrorMessage>{t("runtimePods.invalidInitialSelection")}</ErrorMessage>}
            </>}
          </div>
          <div className={`rounded border p-4 ${orphanMode ? "border-amber-300 bg-amber-50" : "border-slate-200"}`}>
            <label className="flex items-center gap-2 text-sm font-medium">
              <input type="radio" name="eviction-mode" value="orphan-recovery" checked={orphanMode} disabled={busy || !orphanSupported} onChange={() => { setEvictionMode("orphan-recovery"); setAcknowledged(false); setError(undefined); setPreflight(undefined); }} />
              {t("runtimePods.orphanRecovery")}
            </label>
            <p className="ml-5 mt-2 text-xs leading-5">{t("runtimePods.orphanRecoveryHelp")}</p>
            {!orphanSupported && <p role="status" className="mt-2 text-xs">{t("runtimePods.faultRecoveryUnavailable")}</p>}
            {orphanMode && <>
              <p className="mt-2 break-all text-xs text-slate-600">Pod UID: {pod.pod_uid || "—"}</p>
              <button type="button" className={`${buttonClass} mt-3`} disabled={busy || !evictionAllowed} onClick={() => void submit(true)}>{t("runtimePods.checkOrphanSource")}</button>
              {preflight?.key === fingerprint && (preflight.result.allowed ? <p role="status" className="mt-2 text-xs text-emerald-700">{t("runtimePods.orphanSourceVerified")}</p> : <div className="mt-2" role="status">{preflight.result.blockers.map((reason, i) => <ErrorMessage key={i}>{reason}</ErrorMessage>)}</div>)}
            </>}
          </div>
          {evictionMode !== "recovery" && !orphanMode && <div className="grid grid-cols-2 gap-3">
            <label className="space-y-2 text-xs">
              <span className="block">{t("runtimePods.grace")}</span>
              <select
                className={`${inputClass} w-full`}
                value={grace}
                onChange={(event) => setGrace(Number(event.target.value))}
              >
                {(evictionMode === "force"
                  ? [30, 60, 120, 300]
                  : [15, 30, 60, 120, 300]
                ).map((value) => (
                  <option key={value} value={value}>
                    {value} {t("runtimePods.seconds")}
                  </option>
                ))}
              </select>
            </label>
            <label className="space-y-2 text-xs">
              <span className="block">{t("runtimePods.concurrency")}</span>
              <select
                className={`${inputClass} w-full`}
                value={concurrency}
                onChange={(event) => setConcurrency(Number(event.target.value))}
              >
                {[1, 2, 3, 4, 5].map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </label>
          </div>}
          <p className="text-xs text-slate-500">
            {t("runtimePods.incoming")}: {incoming}
          </p>
          {insufficientEvictionCapacity && (
            <p role="status" className="rounded bg-amber-50 p-3 text-xs leading-5 text-amber-700">
              {t("runtimePods.gracefulCapacityRequired", { required: occupied, available: incoming })}
            </p>
          )}
          {evictionMode === "force" && (
            <p
              role="status"
              className="rounded bg-red-50 p-3 text-xs leading-5 text-red-700"
            >
              {t("runtimePods.forceWarning")}
            </p>
          )}
          <div className="flex items-start gap-2 rounded bg-amber-50 p-3 text-xs leading-5 text-amber-700">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            {t("runtimePods.evictWarning")}
          </div>
          <p className="text-xs text-slate-500">{t("runtimePods.pauseHelp")}</p>
          <label className="flex items-start gap-2 text-xs leading-5">
            <input
              type="checkbox"
              className="mt-1"
              checked={acknowledged}
              onChange={(event) => {
                setAcknowledged(event.target.checked);
                setError(undefined);
              }}
            />
            {t(orphanMode ? "runtimePods.orphanRecoveryAcknowledge" : evictionMode === "recovery" ? "runtimePods.faultRecoveryAcknowledge" : "runtimePods.acknowledge")}
          </label>
          <div className="flex justify-end gap-2 border-t border-slate-100 pt-4">
            <button type="button" className={buttonClass} onClick={onClose}>
              {t("runtimePods.cancel")}
            </button>
            <button
              type="button"
              className={`${primaryClass} !border-red-600 !bg-red-600`}
              disabled={!evictionAllowed || !acknowledged || busy || (orphanMode && !validated) || (evictionMode !== "recovery" && !orphanMode && !occupied) || insufficientEvictionCapacity || (evictionMode === "force" && !forceSupported)}
              onClick={() => void submit()}
            >
              {t(busy ? "runtimePods.startingOp" : "runtimePods.startEviction")}{" "}
              {orphanMode ? "" : evictionMode === "recovery" ? ids.length ? `(${ids.length})` : "" : `(${summarizePod(pod).occupied})`}
            </button>
          </div>
        </form>
      )}
      {kind === "events" && (
        <>
          <p className="break-all text-xs text-slate-500">{pod?.pod_name}</p>
          {events.isLoading && (
            <p className="text-sm text-slate-500">{t("runtimePods.loading")}</p>
          )}
          {events.isError && (
            <ErrorMessage>{runtimeError(events.error)}</ErrorMessage>
          )}
          {events.data?.length === 0 && (
            <p className="py-5 text-center text-sm text-slate-400">
              {t("runtimePods.noEvents")}
            </p>
          )}
          <div className="space-y-3">
            {events.data?.map((event, index) => (
              <div
                key={index}
                className={`rounded border p-3 text-xs ${event.type === "Warning" ? "border-amber-200 bg-amber-50" : "border-slate-200"}`}
              >
                <div className="flex justify-between gap-2 font-medium">
                  <span>{event.reason}</span>
                  <span className="text-slate-400">{event.count ?? 1}×</span>
                </div>
                <p className="mt-2 break-words leading-5 text-slate-500">
                  {event.message}
                </p>
                <p className="mt-1 text-slate-400">
                  {event.last_seen_at
                    ? new Date(event.last_seen_at).toLocaleString()
                    : ""}
                </p>
              </div>
            ))}
          </div>
        </>
      )}
      {kind === "history" && (
        <>
          <p className="text-xs text-slate-500">
            {t("runtimePods.recordHelp")}
          </p>
          {history.isLoading && (
            <p className="text-sm text-slate-500">{t("runtimePods.loading")}</p>
          )}
          {history.isError && (
            <ErrorMessage>{runtimeError(history.error)}</ErrorMessage>
          )}
          {history.data?.items.length === 0 && (
            <p className="py-5 text-center text-sm text-slate-400">
              {t("runtimePods.noRecords")}
            </p>
          )}
          <div className="space-y-4">
            {history.data?.items.map((operation) => (
              <section
                key={operation.id}
                className="rounded border border-slate-200 p-4"
              >
                <div className="flex flex-wrap justify-between gap-2 text-xs">
                  <span className="font-medium">
                    {t(`runtimePods.${operation.kind || "operation"}`)}{" "}
                    <span className="font-mono text-slate-400">
                      {operation.id.replace(/^runtime_mgmt_/, "").slice(0, 8)}
                    </span>
                  </span>
                  <span
                    className={`rounded px-2 py-1 ${operation.status === "completed" ? "bg-emerald-50 text-emerald-700" : operation.status === "failed" || operation.status === "partial_failure" || operation.status === "needs_attention" ? "bg-red-50 text-red-600" : "bg-blue-50 text-blue-600"}`}
                  >
                    {t(`runtimePods.${operation.status}`)}
                  </span>
                </div>
                <p className="mt-1 text-xs text-slate-400">
                  {operation.created_at
                    ? new Date(operation.created_at).toLocaleString()
                    : ""}
                </p>
                {operation.phase && <p className="mt-2 text-xs text-slate-600">{t("runtimePods.recoveryPhase")}: {operation.phase}</p>}
                {operation.error_message && <ErrorMessage>{operation.error_message}</ErrorMessage>}
                {["processing", "needs_attention"].includes(operation.status) && <button type="button" className={`${buttonClass} mt-3`} disabled={busy || operation.retry_requested} onClick={() => void retryOperation(operation.id)}>{t(operation.retry_requested ? "runtimePods.recoveryQueued" : "runtimePods.recoveryContinue")}</button>}
                <div className="mt-3 divide-y divide-slate-100">
                  {operation.items?.map((item) => (
                    <div
                      key={item.instance_id}
                      className="flex flex-wrap justify-between gap-2 py-2 text-xs"
                    >
                      <span>{item.orphan_recovery ? `Pod #${item.source_pod_id}` : `#${item.instance_id}`}</span>
                      <span className="text-slate-500">
                        {t(`runtimePods.${item.state}`)}
                      </span>
                      {item.orphan_recovery?.phase && <p className="w-full break-words text-slate-600">{t("runtimePods.recoveryPhase")}: {item.orphan_recovery.phase}</p>}
                      {item.recovery?.phase && <p className="w-full break-words text-slate-600">{t("runtimePods.recoveryPhase")}: {item.recovery.phase}</p>}
                      {item.error_message && (
                        <p className="w-full break-words leading-5 text-red-600">
                          {item.error_message}
                        </p>
                      )}
                      {item.state === "failed" && !item.recovery && (
                        instances.some(
                          (current) =>
                            current.slot.instance_id === item.instance_id,
                        ) || failedMoveRetrySelections(pods, operation).some(current => current.slot.instance_id === item.instance_id)) && (
                          <button
                            type="button"
                            className="text-slate-700 underline"
                            onClick={() => onRetry([item.instance_id], operation.id)}
                          >
                            {t("runtimePods.retry")}
                          </button>
                        )}
                    </div>
                  ))}
                </div>
                {(operation.items?.some((item) => item.recovery || item.orphan_recovery)
                  ? ["pending", "processing", "needs_attention"].includes(operation.status) && operation.items.every((item) => (item.recovery?.phase ?? item.orphan_recovery?.phase) === "planned")
                  : ["pending", "processing"].includes(operation.status) ||
                    (operation.kind === "scale_in" && ["starting", "needs_attention"].includes(operation.status))) && (
                  <button
                    type="button"
                    className={`${buttonClass} mt-3`}
                    disabled={busy || operation.cancel_requested}
                    onClick={() => void cancelOperation(operation.id)}
                  >
                    {t(
                      operation.cancel_requested
                        ? "runtimePods.cancelling"
                        : "runtimePods.stopFuture",
                    )}
                  </button>
                )}
              </section>
            ))}
          </div>
        </>
      )}
    </RuntimeDialog>
  );
}
