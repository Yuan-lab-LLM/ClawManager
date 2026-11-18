import api from "./api";

export interface HermesDesktopBootstrap {
  available: boolean;
  reason?: string;
  instance_id: number;
  renderer_url?: string;
  api_base?: string;
  expires_at?: string;
  versions?: {
    hermes_ref?: string;
    hermes_commit?: string;
    bridge_version?: string;
  };
  capabilities?: string[];
}

export const hermesDesktopService = {
  async recentSession(instanceId: number, signal?: AbortSignal): Promise<string | null> {
    const response = await api.get(`/api/v1/instances/${instanceId}/hermes-desktop/api/sessions`, {
      baseURL: window.location.origin, params: { limit: 1, order: "recent" }, signal, timeout: 5000,
    });
    const id = response.data?.sessions?.[0]?.id;
    return typeof id === "string" && /^[A-Za-z0-9_:-]{1,160}$/.test(id) ? id : null;
  },
  async probe(instanceId: number, signal?: AbortSignal): Promise<HermesDesktopBootstrap> {
    const response = await api.get(`/api/v1/instances/${instanceId}/hermes-desktop/bootstrap`, {
      baseURL: window.location.origin,
      signal,
    });
    return response.data.data;
  },
  async bootstrap(instanceId: number, signal?: AbortSignal): Promise<HermesDesktopBootstrap> {
    // Always use the frontend origin: the HttpOnly cookie must reach the iframe's BFF.
    const response = await api.post(`/api/v1/instances/${instanceId}/hermes-desktop/bootstrap`, null, {
      baseURL: window.location.origin,
      signal,
    });
    return response.data.data;
  },
};
