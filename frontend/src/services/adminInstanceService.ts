import api from "./api";
import type { InstanceListResponse } from "../types/instance";

// adminInstanceService: admin-console surface for cross-user instance
// listing. Backed by /admin/instances, which is gated by the admin
// middleware on the backend. The regular instanceService.getInstances
// hits /instances and is always caller-scoped regardless of role.
export const adminInstanceService = {
  getInstances: async (
    page: number = 1,
    limit: number = 20,
  ): Promise<InstanceListResponse> => {
    const response = await api.get("/admin/instances", {
      params: { page, limit },
    });
    return response.data.data;
  },
};
