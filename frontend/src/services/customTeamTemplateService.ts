import api from "./api";
import type { CustomTeamTemplate } from "../types/customTeamTemplate";

const unwrap = (response: { data: { data: CustomTeamTemplate } }) =>
  response.data.data;

export const customTeamTemplateService = {
  list: async (): Promise<CustomTeamTemplate[]> => {
    const response = await api.get("/custom-team-templates");
    return response.data.data.templates || [];
  },

  get: async (id: number): Promise<CustomTeamTemplate> =>
    unwrap(await api.get(`/custom-team-templates/${id}`)),

  generate: async (intent: string, memberCount?: number): Promise<CustomTeamTemplate> =>
    unwrap(
      await api.post("/custom-team-templates", {
        intent,
        member_count: memberCount,
      }),
    ),

  rename: async (
    id: number,
    name: string,
    expectedRevision: number,
  ): Promise<CustomTeamTemplate> =>
    unwrap(
      await api.put(`/custom-team-templates/${id}`, {
        name,
        expected_revision: expectedRevision,
      }),
    ),

  revise: async (
    id: number,
    name: string,
    intent: string,
    memberCount: number | undefined,
    expectedRevision: number,
  ): Promise<CustomTeamTemplate> =>
    unwrap(
      await api.post(`/custom-team-templates/${id}/revise`, {
        name,
        intent,
        member_count: memberCount,
        expected_revision: expectedRevision,
      }),
    ),

  adjustMember: async (
    id: number,
    memberId: string,
    instruction: string,
    expectedRevision: number,
  ): Promise<CustomTeamTemplate> =>
    unwrap(
      await api.post(
        `/custom-team-templates/${id}/members/${encodeURIComponent(memberId)}/adjust`,
        { instruction, expected_revision: expectedRevision },
      ),
    ),

  regenerateMember: async (
    id: number,
    memberId: string,
    expectedRevision: number,
  ): Promise<CustomTeamTemplate> =>
    unwrap(
      await api.post(
        `/custom-team-templates/${id}/members/${encodeURIComponent(memberId)}/regenerate`,
        { expected_revision: expectedRevision },
      ),
    ),

  regenerate: async (
    id: number,
    expectedRevision: number,
  ): Promise<CustomTeamTemplate> =>
    unwrap(
      await api.post(`/custom-team-templates/${id}/regenerate`, {
        expected_revision: expectedRevision,
      }),
    ),

  delete: async (id: number): Promise<void> => {
    await api.delete(`/custom-team-templates/${id}`);
  },
};
