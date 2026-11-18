import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useI18n } from "../../contexts/I18nContext";
import { runtimeError } from "../../lib/runtimeManagement";
import { runtimePoolService } from "../../services/runtimePoolService";
import type { RuntimePod, RuntimeSchedulingOperation, RuntimeSlot, RuntimeSlotResetJournal } from "../../types/runtimePool";
import { RuntimeDialog } from "./RuntimeManagementUI";

export type RuntimeSlotResetSource = ReturnType<typeof slotResetSource>;

export function slotResetSource(pod: RuntimePod, slot: RuntimeSlot) {
  return {
    podId: pod.id,
    podUID: pod.pod_uid ?? "",
    podName: pod.pod_name,
    slot: slot.container_name,
    observedIdentity: JSON.stringify([
      pod.node_name, slot.restart_count, slot.instance_id,
      slot.expected_instance_id, slot.generation, slot.gateway_id,
    ]),
  };
}

export function RuntimeSlotResetProgress({ journal, status }: { journal: RuntimeSlotResetJournal; status: string }) {
  const { t } = useI18n();
  const phase = journal.phase === "released" && status !== "completed" ? "waiting_worker" : journal.phase;
  const awaitsBinding = ["planned", "stopping", "source_stopped"].includes(phase);
  const recovery = journal.recovery_action === "reschedule" && (awaitsBinding || !journal.recovery_state)
    ? "unconfirmed"
    : journal.recovery_state || journal.recovery_action || "unconfirmed";
  return (
    <div className="w-full min-w-0 space-y-2 break-words text-sm">
      <dl className="grid gap-x-4 gap-y-1 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
        <dt className="font-semibold">{t("slotReset.sourceProgress")}</dt>
        <dd>
          <p>{t(`slotReset.phase.${phase}`)}</p>
          {phase === "waiting_worker" && <p className="text-[var(--cm-muted)]">{t("slotReset.phase.committed")}</p>}
        </dd>
        <dt className="font-semibold">{t("slotReset.instanceProgress")}{journal.instance_id ? ` #${journal.instance_id}` : ""}</dt>
        <dd>{t(`slotReset.recovery.${recovery}`)}</dd>
      </dl>
      {journal.error_message && <p role="alert" className="text-[var(--cm-error)]">{journal.error_message}</p>}
    </div>
  );
}

function matchingJournal(operation: RuntimeSchedulingOperation | null | undefined, source: RuntimeSlotResetSource) {
  const journal = operation?.items?.[0]?.slot_reset;
  if (operation?.kind !== "slot_reset" || !journal || journal.slot !== source.slot ||
    (source.podUID && journal.pod_uid !== source.podUID)) return;
  return journal;
}

function readRequestId(key: string) {
  try { return sessionStorage.getItem(key); } catch { return null; }
}

export default function RuntimeSlotResetDialog({ accountId, source, sourceChanged, onClose, onChanged, onHistory }: {
  accountId: number;
  source: RuntimeSlotResetSource;
  sourceChanged: boolean;
  onClose: () => void;
  onChanged: () => void;
  onHistory: () => void;
}) {
  const { t } = useI18n();
  const client = useQueryClient();
  const storageKey = `cm-slot-reset:${accountId}:${source.podId}:${source.podUID}:${source.slot}`;
  const [requestId, setRequestId] = useState(() => readRequestId(storageKey));
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string>();
  const [retryError, setRetryError] = useState<string>();
  const [retryBusy, setRetryBusy] = useState(false);
  const inFlight = useRef(false);
  const preview = useQuery({
    queryKey: ["runtime-slot-reset-preview", accountId, source.podId, source.podUID, source.slot, source.observedIdentity],
    queryFn: ({ signal }) => runtimePoolService.preflightSlotReset(source.podId, source.slot, { source_pod_uid: source.podUID }, signal),
    enabled: !requestId && !sourceChanged,
    retry: false,
    staleTime: Infinity,
    gcTime: 0,
    refetchOnMount: "always",
  });
  const lookup = useQuery({
    queryKey: ["runtime-scheduling", "slot-reset", accountId, requestId],
    queryFn: () => runtimePoolService.slotResetOperation(requestId!),
    enabled: !!requestId && !submitting,
    refetchInterval: 3000,
    refetchIntervalInBackground: true,
    retry: false,
  });
  const operation = lookup.data;
  const journal = matchingJournal(operation, source);
  const itemError = operation?.items?.[0]?.error_message ?? operation?.error_message;
  const preflight = preview.data;
  const approved = preflight?.allowed && preflight.pod_uid && preflight.slot === source.slot &&
    (!source.podUID || preflight.pod_uid === source.podUID) && preflight.container_id && preflight.scope_fingerprint &&
    preflight.recovery_action;
  const canConfirm = approved && !preview.isFetching && !preview.isError && !submitError && !sourceChanged;

  const submit = async () => {
    if (!canConfirm || !preflight || inFlight.current || requestId) return;
    inFlight.current = true;
    setSubmitting(true);
    setSubmitError(undefined);
    let persisted = false;
    try {
      const id = crypto.randomUUID();
      sessionStorage.setItem(storageKey, id);
      persisted = true;
      setRequestId(id);
      const result = await runtimePoolService.resetSlot(source.podId, source.slot, {
        source_pod_uid: preflight.pod_uid!,
        source_container_id: preflight.container_id!,
        scope_fingerprint: preflight.scope_fingerprint!,
        request_id: id,
      });
      if (matchingJournal(result, source)?.container_id === preflight.container_id) {
        client.setQueryData(["runtime-scheduling", "slot-reset", accountId, id], result);
      }
      onChanged();
    } catch (error) {
      const status = (error as { response?: { status?: number } })?.response?.status;
      if (!persisted || (status && status >= 400 && status < 500 && status !== 408)) {
        if (persisted) sessionStorage.removeItem(storageKey);
        setRequestId(null);
        setSubmitError(runtimeError(error));
      }
    } finally {
      inFlight.current = false;
      setSubmitting(false);
    }
  };

  const retry = async () => {
    if (!operation || !journal || inFlight.current) return;
    inFlight.current = true;
    setRetryBusy(true);
    setRetryError(undefined);
    try {
      await runtimePoolService.retryOperation(operation.id);
      await lookup.refetch();
      onChanged();
    } catch (error) {
      setRetryError(runtimeError(error));
    } finally {
      setRetryBusy(false);
      inFlight.current = false;
    }
  };
  const close = (history = false) => {
    if (inFlight.current) return;
    if (journal?.phase === "released" && operation?.status === "completed") sessionStorage.removeItem(storageKey);
    if (history) onHistory();
    else onClose();
  };
  const recheck = () => {
    setSubmitError(undefined);
    void preview.refetch();
  };
  const errorText = submitError ?? (preview.isError ? runtimeError(preview.error) : undefined);
  return (
    <RuntimeDialog title={t("slotReset.title")} onClose={() => close()} closeOnBackdrop={false} fluent busy={submitting || retryBusy}
      footer={(
        <>
          <button type="button" className="app-button-secondary" autoFocus disabled={submitting || retryBusy} onClick={() => close()}>
            {t(requestId ? "runtimePods.close" : "slotReset.cancel")}
          </button>
          {requestId ? (
            <button type="button" className="app-button-secondary" disabled={submitting || retryBusy} onClick={() => close(true)}>
              {t("slotReset.history")}
            </button>
          ) : (
            <button type="button" className="app-button-primary cm-button-danger" data-testid="runtime-slot-reset-confirm"
              disabled={!canConfirm || submitting} onClick={() => void submit()}>
              {t("slotReset.confirm")}
            </button>
          )}
        </>
      )}
    >
      <div className="space-y-4 break-words text-sm">
        <p className="font-semibold">{source.podName} · {source.slot}</p>
        {requestId ? (
          <>
            {submitting ? <p role="status">{t("slotReset.submitting")}</p> : journal ? (
              <div aria-live="polite" className="space-y-3">
                <p className="font-semibold">{t("slotReset.operation")} <code className="[overflow-wrap:anywhere]">{operation!.id}</code></p>
                <RuntimeSlotResetProgress journal={journal} status={operation!.status} />
                {itemError && itemError !== journal.error_message && <p role="alert" className="text-[var(--cm-error)]">{itemError}</p>}
                {["processing", "needs_attention"].includes(operation!.status) && (
                  <button type="button" className="app-button-secondary" disabled={retryBusy || operation!.retry_requested} onClick={() => void retry()}>
                    {t(operation!.retry_requested ? "runtimePods.recoveryQueued" : "runtimePods.recoveryContinue")}
                  </button>
                )}
                {retryError && <p role="alert" className="text-[var(--cm-error)]">{retryError}</p>}
              </div>
            ) : (
              <div role="status" className="space-y-2">
                <p className="rounded-[var(--cm-radius-control)] bg-[var(--cm-warning-background)] p-3 text-[var(--cm-warning)]">{t("slotReset.uncertain")}</p>
                <p>{t("slotReset.lookup")} <code className="[overflow-wrap:anywhere]">{requestId}</code></p>
              </div>
            )}
            {lookup.isError && <p role="alert" className="text-[var(--cm-error)]">{runtimeError(lookup.error)}</p>}
          </>
        ) : sourceChanged ? (
          <p role="alert" className="text-[var(--cm-error)]">{t("slotReset.sourceChanged")}</p>
        ) : (
          <>
            {preview.isFetching && <p role="status">{t("slotReset.checking")}</p>}
            {errorText && (
              <div role="alert" className="space-y-2 text-[var(--cm-error)]">
                <p>{errorText}</p>
                <button type="button" className="app-button-secondary" onClick={recheck}>{t("slotReset.recheck")}</button>
              </div>
            )}
            {preflight && (
              <>
                {approved && (
                  <div className="space-y-1 rounded-[var(--cm-radius-control)] bg-[var(--cm-warning-background)] p-3 text-[var(--cm-warning)]">
                    <p>{t("slotReset.impact")}</p>
                    <p>{t(`slotReset.action.${preflight.recovery_action}`, { id: preflight.instance_id ?? "—" })}</p>
                  </div>
                )}
                {preflight.runtime_type && (
                  <dl className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-x-4 gap-y-1 sm:grid-cols-[minmax(0,3fr)_minmax(0,7fr)]">
                    <dt>{t("slotReset.runtime")}</dt><dd>{preflight.runtime_type}</dd>
                    <dt>{t("slotReset.node")}</dt><dd>{preflight.node_name || "—"}</dd>
                    <dt>{t("slotReset.podUID")}</dt><dd className="font-mono [overflow-wrap:anywhere]">{preflight.pod_uid || "—"}</dd>
                    <dt>{t("slotReset.container")}</dt><dd className="font-mono [overflow-wrap:anywhere]">{preflight.container_id || "—"}</dd>
                    <dt>{t("slotReset.oldInstance")}</dt><dd>{preflight.instance_id ? `#${preflight.instance_id}${preflight.generation ? ` · ${t("slotReset.generation", { generation: preflight.generation })}` : ""}` : t("slotReset.unknownOwner")}</dd>
                    <dt>{t("slotReset.current")}</dt><dd>{preflight.current_pod_id ? `Pod #${preflight.current_pod_id} · ${preflight.current_slot || "—"}` : "—"}</dd>
                  </dl>
                )}
                {preflight.blockers?.length ? (
                  <ul role="alert" className="list-disc space-y-1 rounded-[var(--cm-radius-control)] bg-[var(--cm-error-background)] py-3 pl-8 pr-3 text-[var(--cm-error)]">
                    {preflight.blockers.map((reason, index) => <li key={index}>{reason}</li>)}
                  </ul>
                ) : !approved && <p role="alert" className="text-[var(--cm-error)]">{t("slotReset.unverified")}</p>}
                {!approved && !errorText && !preview.isFetching && <button type="button" className="app-button-secondary" onClick={recheck}>{t("slotReset.recheck")}</button>}
              </>
            )}
          </>
        )}
      </div>
    </RuntimeDialog>
  );
}
