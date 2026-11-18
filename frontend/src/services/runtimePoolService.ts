import axios from "axios";
import api from "./api";
import type {
  RuntimeRecoveryInspection,
  RuntimeAutoStopSettings,
  UpdateRuntimeAutoStopSettings,
  RuntimeAutoStopActivity,
  RuntimeGateway,
  RuntimePod,
  RuntimeRollout,
  RuntimeType,
  RuntimeUpgradeDetails,
  StartRuntimeRolloutRequest,
  RuntimeUpgradePreflightResult,
  RuntimePodEvent,
  RuntimeSchedulingRequest,
  RuntimeSchedulingOperation,
  RuntimeSchedulingPreflight,
  RuntimePodPage,
  RuntimePodPageRequest,
  RuntimeManagedPool,
  RuntimeScaleInPlan,
  RuntimeScaleOptions,
  RuntimeScaleResult,
  CreateRuntimePodRequest,
  RuntimeBalancePlan,
} from "../types/runtimePool";

export interface RuntimePreparationOperation {
  id: string;
  kind: string;
  runtime_type: RuntimeType;
  status: string;
  phase?: string;
  error_message?: string;
}

export const runtimePoolService = {
  async prepareRollout(runtimeType: RuntimeType, signal?: AbortSignal, observer?: {
    onOperation: (id: string, operation: RuntimePreparationOperation) => void;
    onStatus?: (operation: RuntimePreparationOperation) => void;
    cancelOperation?: (id: string) => Promise<void>;
  }): Promise<string> {
    signal?.throwIfAborted();
    // This mutation must never be replayed after an unknown response.
    const response = await api.post('/admin/runtime-rollouts/prepare', { runtime_type: runtimeType }, { signal });
    type WakeOperation = RuntimePreparationOperation;
    let operation = response.data.data?.operation as WakeOperation | undefined;
    const id = operation?.id;
    if (!id || typeof id !== 'string') throw new Error('Runtime preparation response is incomplete; check scheduling history before retrying');
    if (operation?.kind !== 'rollout_wake' || operation.runtime_type !== runtimeType) throw new Error(`Runtime preparation #${id}: receipt identity does not match`);
    observer?.onOperation(id, operation);
    const deadline = Date.now() + 15 * 60 * 1000;
    try {
      while (true) {
        signal?.throwIfAborted();
        if (operation) {
          if (operation.id !== id || operation.kind !== 'rollout_wake' || operation.runtime_type !== runtimeType) {
            throw new Error('Runtime preparation identity does not match');
          }
          observer?.onStatus?.(operation);
          if (operation.status === 'completed') return id;
          if (!['pending', 'starting', 'processing'].includes(operation.status)) {
            throw new Error(operation.error_message || `${operation.status} / ${operation.phase || 'preparation'}`);
          }
        }
        if (Date.now() >= deadline) throw new Error('Timed out; check scheduling history before retrying');
        await new Promise<void>((resolve, reject) => {
          const cancel = () => { clearTimeout(timer); reject(signal?.reason); };
          const timer = setTimeout(() => { signal?.removeEventListener('abort', cancel); resolve(); }, 2000);
          signal?.addEventListener('abort', cancel, { once: true });
        });
        signal?.throwIfAborted();
        const remaining = deadline - Date.now();
        if (remaining <= 0) throw new Error('Timed out; check scheduling history before retrying');
        const history = await api.get('/admin/runtime-scheduling', { signal, timeout: Math.min(15000, remaining) });
        const items: WakeOperation[] = Array.isArray(history.data.data?.items) ? history.data.data.items : [];
        operation = items.find(item => item.id === id);
      }
    } catch (error) {
      if (signal?.aborted) {
        const cancellable = operation && (['pending', 'starting', 'processing'].includes(operation.status) || (operation.status === 'completed' && operation.phase === 'ready'));
        if (cancellable) await (observer?.cancelOperation?.(id) ?? api.post(`/admin/runtime-scheduling/${encodeURIComponent(id)}/cancel`)).catch(() => {});
        throw error;
      }
      throw new Error(`Runtime preparation #${id}: ${axios.isAxiosError(error) ? error.response?.data?.error || error.message : error instanceof Error ? error.message : String(error)}`);
    }
  },
  async inspectInstanceRecovery(id: number): Promise<RuntimeRecoveryInspection> {
    const response = await api.get(`/admin/instances/${id}/runtime-recovery`);
    return response.data.data;
  },
  async retryInstanceRecovery(id: number, generation: number): Promise<RuntimeRecoveryInspection> {
    const response = await api.post(`/admin/instances/${id}/runtime-recovery`, { generation });
    return response.data.data;
  },
  async recheckPod(id: number, podUid: string): Promise<RuntimePod> {
    const response = await api.post(`/admin/runtime-pods/${id}/recheck`, { pod_uid: podUid });
    return response.data.data;
  },
  async retryOperation(id: string): Promise<RuntimeSchedulingOperation> {
    const response = await api.post(`/admin/runtime-scheduling/${encodeURIComponent(id)}/retry`);
    return response.data.data;
  },
  async getAutoStopSettings(signal?: AbortSignal): Promise<RuntimeAutoStopSettings> {
    const response = await api.get('/admin/runtime-auto-stop', { signal });
    return response.data.data;
  },

  async updateAutoStopSettings(request: UpdateRuntimeAutoStopSettings): Promise<RuntimeAutoStopSettings> {
    const response = await api.put('/admin/runtime-auto-stop', request);
    return response.data.data;
  },

  async setInstanceAutoStopDisabled(id: number, disabled: boolean): Promise<RuntimeAutoStopActivity> {
    const response = await api.put(`/admin/instances/${id}/auto-stop`, { auto_stop_disabled: disabled });
    return response.data.data;
  },

  async listManagedPools(signal?: AbortSignal): Promise<{ items: RuntimeManagedPool[] }> {
    const response = await api.get('/admin/runtime-pod-pools', { signal });
    return response.data.data;
  },

  async listScaleOptions(signal?: AbortSignal): Promise<RuntimeScaleOptions> {
    const response = await api.get('/admin/runtime-pods/scale-options', { signal });
    return response.data.data;
  },

  async createPod(request: CreateRuntimePodRequest): Promise<RuntimeScaleResult> {
    const response = await api.post('/admin/runtime-pods', request);
    return response.data.data;
  },

  async preflightScaleIn(request: CreateRuntimePodRequest): Promise<RuntimeScaleInPlan> {
    const response = await api.post('/admin/runtime-pods/scale-in/preflight', request);
    return response.data.data;
  },

  async scaleIn(request: CreateRuntimePodRequest & { plan_fingerprint: string }): Promise<RuntimeSchedulingOperation> {
    const response = await api.post('/admin/runtime-pods/scale-in', request);
    return response.data.data;
  },

  async deletePod(id: number, podUid: string): Promise<void> {
    await api.delete(`/admin/runtime-pods/${id}`, { data: { pod_uid: podUid } });
  },

  async deletePodPool(name: string, namespace: string, deploymentUid: string): Promise<void> {
    await api.delete(`/admin/runtime-pod-pools/${encodeURIComponent(name)}`, { data: { namespace, deployment_uid: deploymentUid } });
  },

  async planBalance(runtimeType: RuntimeType | 'all'): Promise<RuntimeBalancePlan> {
    const response = await api.post('/admin/runtime-balance/plan', { runtime_type: runtimeType });
    return response.data.data;
  },

  async executeBalance(runtimeType: RuntimeType | 'all', planFingerprint: string): Promise<RuntimeSchedulingOperation> {
    const response = await api.post('/admin/runtime-balance', { runtime_type: runtimeType, plan_fingerprint: planFingerprint });
    return response.data.data;
  },

  async setScheduling(podId: number, enabled: boolean): Promise<void> {
    await api.post(`/admin/runtime-pods/${podId}/scheduling`, { enabled });
  },

  async listEvents(podId: number): Promise<RuntimePodEvent[]> {
    const response = await api.get(`/admin/runtime-pods/${podId}/events`);
    return response.data.data?.events ?? [];
  },

  async schedulingHistory(): Promise<{ items: RuntimeSchedulingOperation[]; supported_modes: string[]; force_unavailable_reason?: string }> {
    const response = await api.get('/admin/runtime-scheduling');
    return response.data.data;
  },

  async cancelScheduling(id: string): Promise<void> {
    await api.post(`/admin/runtime-scheduling/${encodeURIComponent(id)}/cancel`);
  },

  async preflightScheduling(request: RuntimeSchedulingRequest): Promise<RuntimeSchedulingPreflight> {
    const endpoint = '/admin/runtime-scheduling/preflight';
    const response = await api.post(endpoint, request).catch((error: unknown) => {
      if (
        !axios.isAxiosError(error) ||
        error.code !== 'ERR_NETWORK' ||
        error.response ||
        error.config?.url !== endpoint ||
        ('_retry' in error.config && error.config._retry)
      ) throw error;
      return api.post(endpoint, request);
    });
    return response.data.data;
  },

  async schedule(request: RuntimeSchedulingRequest): Promise<RuntimeSchedulingOperation> {
    const response = await api.post('/admin/runtime-scheduling', request);
    return response.data.data;
  },

  async evict(podId: number, data: { mode: "graceful" | "force" | "recovery" | "orphan-recovery"; source_pod_uid?: string; instance_ids?: number[]; grace_period_seconds?: number; concurrency?: number }): Promise<RuntimeSchedulingOperation> {
    const response = await api.post(`/admin/runtime-pods/${podId}/evict`, data);
    return response.data.data;
  },
  async listPodsPage(params: RuntimePodPageRequest, signal?: AbortSignal): Promise<RuntimePodPage> {
    const response = await api.get("/admin/runtime-pods", { params, signal, timeout: 15000 });
    return response.data.data;
  },

  async listPods(runtimeType?: RuntimeType, signal?: AbortSignal): Promise<RuntimePod[]> {
    const response = await api.get("/admin/runtime-pods", {
      params: runtimeType ? { runtime_type: runtimeType } : undefined,
      signal,
      timeout: 15000,
    });
    const pods = response.data.data?.pods;
    return Array.isArray(pods) ? pods : [];
  },

  async listGateways(podId: number): Promise<RuntimeGateway[]> {
    const response = await api.get(`/admin/runtime-pods/${podId}/gateways`);
    const gateways = response.data.data?.gateways;
    return Array.isArray(gateways) ? gateways : [];
  },

  async drainPod(podId: number): Promise<void> {
    await api.post(`/admin/runtime-pods/${podId}/drain`);
  },

  async startRollout(data: StartRuntimeRolloutRequest & { preparation_id?: string }): Promise<RuntimeRollout> {
    const response = await api.post("/admin/runtime-rollouts", data);
    const rollout = response.data.data?.rollout as RuntimeRollout | undefined;
    if (!rollout || typeof rollout.id !== "number") {
      throw new Error("Runtime rollout response is incomplete");
    }
    return rollout;
  },

  async getRollout(id: number): Promise<RuntimeUpgradeDetails> {
    const response = await api.get(`/admin/runtime-rollouts/${id}`);
    const details = response.data.data as RuntimeUpgradeDetails | null | undefined;
    if (!details?.rollout || typeof details.rollout.id !== "number") {
      throw new Error("Runtime rollout details response is incomplete");
    }
    return {
      ...details,
      items: Array.isArray(details.items) ? details.items : [],
      audits: Array.isArray(details.audits) ? details.audits : [],
    };
  },

  async listRollouts(runtimeType?: RuntimeType): Promise<RuntimeRollout[]> {
    const response = await api.get("/admin/runtime-rollouts", {
      params: runtimeType ? { runtime_type: runtimeType } : undefined,
    });
    const items = response.data.data?.items;
    return Array.isArray(items) ? items : [];
  },

  async preflightOpenClawRollout(data: Omit<StartRuntimeRolloutRequest, "runtime_type" | "preflight_id"> & { preparation_id?: string }): Promise<RuntimeUpgradePreflightResult> {
    const response = await api.post("/admin/runtime-rollouts/preflight", data);
    const result = response.data.data as RuntimeUpgradePreflightResult | null | undefined;
    if (!result || typeof result.passed !== "boolean") {
      throw new Error("Runtime rollout preflight response is incomplete");
    }
    return {
      ...result,
      blockers: Array.isArray(result.blockers) ? result.blockers : [],
      warnings: Array.isArray(result.warnings) ? result.warnings : [],
      required_capabilities: Array.isArray(result.required_capabilities) ? result.required_capabilities : [],
    };
  },
};
