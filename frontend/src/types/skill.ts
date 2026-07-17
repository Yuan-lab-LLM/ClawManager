export interface Skill {
  id: number;
  user_id: number;
  skill_key: string;
  name: string;
  description?: string;
  status: string;
  source_type: string;
  risk_level: string;
  scan_status?: string;
  last_scanned_at?: string;
  current_version_id?: number;
  current_version_no?: number;
  content_hash?: string;
  archive_hash?: string;
  instance_count: number;
  visibility?: string;
  published_at?: string;
  published_by?: number;
  tags?: SkillHubTag[];
  publishable?: boolean;
  publish_blocked_reason?: string;
  package_collect_error?: string;
  package_materialize_status?: string;
  package_materialize_error?: string;
  owner_username?: string;
  created_at: string;
  updated_at: string;
}

export interface SkillHubTag {
  id: number;
  tag_key: string;
  name: string;
  description?: string;
  sort_order: number;
  admin_only: boolean;
}

export interface SkillHubCatalogResponse {
  items: Skill[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

export type SkillImportConflictType = 'none' | 'unchanged' | 'content_changed';

export interface SkillImportPreviewItem {
  directory_name: string;
  skill_key: string;
  content_hash: string;
  conflict_type: SkillImportConflictType;
  existing_skill_id?: number;
  existing_name?: string;
  current_version_no?: number;
  suggested_skill_key?: string;
}

export type SkillImportDecisionAction = 'new_version' | 'save_as_new' | 'skip';

export interface SkillImportDecision {
  directory_name: string;
  action: SkillImportDecisionAction;
  skill_key?: string;
}

export type SkillImportResultAction = 'created' | 'versioned' | 'unchanged' | 'saved_as_new';

export interface SkillImportResultItem {
  skill: Skill;
  action: SkillImportResultAction;
  previous_version_no?: number;
  directory_name: string;
}

export interface SkillVersion {
  id: number;
  skill_id: number;
  blob_id: number;
  version_no: number;
  source_type: string;
  content_hash: string;
  archive_hash: string;
  object_key: string;
  file_name: string;
  risk_level: string;
  created_at: string;
}

export interface SkillScanResult {
  id: number;
  blob_id: number;
  engine: string;
  risk_level: string;
  status: string;
  summary?: string;
  findings?: Record<string, unknown>;
  scanned_at?: string;
}

export interface InstanceSkill {
  id: number;
  instance_id: number;
  skill_id: number;
  skill_version_id?: number;
  source_type: string;
  install_path?: string;
  workspace_dir?: string;
  observed_hash?: string;
  status: string;
  last_seen_at?: string;
  removed_at?: string;
  skill?: Skill;
}
