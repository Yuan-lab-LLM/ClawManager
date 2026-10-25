import api from "./api";
import type {
  RuntimeGateway,
  RuntimePod,
  RuntimeRollout,
  RuntimeType,
  RuntimeUpgradeDetails,
  StartRuntimeRolloutRequest,
  RuntimeUpgradePreflightResult,
} from "../types/runtimePool";

export const runtimePoolService = {
  async listPods(runtimeType?: RuntimeType): Promise<RuntimePod[]> {
    const response = await api.get("/admin/runtime-pods", {
      params: runtimeType ? { runtime_type: runtimeType } : undefined,
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
