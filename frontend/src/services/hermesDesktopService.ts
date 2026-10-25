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
