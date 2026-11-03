import axios from "axios";
import api from "./api";
import type {
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

export const runtimePoolService = {
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

  async evict(podId: number, data: { mode: string; grace_period_seconds: number; concurrency: number }): Promise<RuntimeSchedulingOperation> {
    const response = await api.post(`/admin/runtime-pods/${podId}/evict`, data);
    return response.data.data;
  },
  async listPodsPage(params: RuntimePodPageRequest, signal?: AbortSignal): Promise<RuntimePodPage> {
    const response = await api.get("/admin/runtime-pods", { params, signal });
    return response.data.data;
  },

  async listPods(runtimeType?: RuntimeType, signal?: AbortSignal): Promise<RuntimePod[]> {
    const response = await api.get("/admin/runtime-pods", {
      params: runtimeType ? { runtime_type: runtimeType } : undefined,
      signal,
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

  async startRollout(data: StartRuntimeRolloutRequest): Promise<RuntimeRollout> {
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

  async preflightOpenClawRollout(data: Omit<StartRuntimeRolloutRequest, "runtime_type" | "preflight_id">): Promise<RuntimeUpgradePreflightResult> {
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
