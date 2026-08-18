export interface CustomTeamMemberSpec {
  memberId: string;
  displayName: string;
  role: string;
  isLeader: boolean;
  summary: string;
  mission: string;
  responsibilities: string[];
  boundaries: string[];
  expectedInputs: string[];
  deliverables: string[];
  acceptanceCriteria: string[];
  collaborationNotes: string[];
  capabilityTags: string[];
}

export interface CustomTeamTemplateSpec {
  schemaVersion: number;
  name: string;
  summary: string;
  resolvedMemberCount: number;
  members: CustomTeamMemberSpec[];
}

export interface CustomTeamTemplate {
  id: number;
  name: string;
  intent: string;
  requested_member_count?: number;
  resolved_member_count: number;
  revision: number;
  spec: CustomTeamTemplateSpec;
  created_at: string;
  updated_at: string;
}
