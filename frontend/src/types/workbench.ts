import type { BrowserOriginMetadata } from "../lib/browserOrigin";
export interface WorkbenchCapabilities extends BrowserOriginMetadata {
  generation: number;
  workspace: string;
  git: boolean;
  terminal: boolean;
  preview: boolean;
  processes: boolean;
}
export interface TerminalSession {
  id: string;
  name: string;
  cwd: string;
  generation: number;
  created_at: string;
  exited: boolean;
  exit_code?: number;
}
export interface TerminalOutput {
  data: string;
  cursor: number;
  truncated: boolean;
  exited: boolean;
}
export interface WorkbenchProcess {
  can_stop: boolean;
  can_restart: boolean;
  id: string;
  name: string;
  command: string;
  cwd: string;
  state: string;
  source: string;
  session_id?: string;
  started_at: string;
}
export interface GitProject {
  path: string;
  name: string;
  remote?: string;
}
export interface GitChange {
  path: string;
  previous_path?: string;
  index_status: string;
  worktree_status: string;
  staged: boolean;
  untracked: boolean;
  binary?: boolean;
  stats_known?: boolean;
  staged_additions?: number;
  staged_deletions?: number;
  worktree_additions?: number;
  worktree_deletions?: number;
  additions: number;
  deletions: number;
}
export interface GitStatus {
  branch: string;
  detached: boolean;
  ahead: number;
  behind: number;
  changes: GitChange[];
}
export interface GitBranch {
  name: string;
  upstream?: string;
  current: boolean;
  remote: boolean;
}
export interface GitCommit {
  hash: string;
  author: string;
  author_email: string;
  message: string;
  timestamp: number;
}
export interface GitRequest {
  operation: string;
  path?: string;
  name?: string;
  email?: string;
  url?: string;
  branch?: string;
  paths?: string[];
  staged?: boolean;
  message?: string;
  limit?: number;
}
export interface GitResponse {
  projects?: GitProject[];
  project?: GitProject;
  status?: GitStatus;
  branches?: GitBranch[];
  history?: GitCommit[];
  diff?: string;
  binary?: boolean;
  preview_path?: string;
  identity?: { name: string; email: string; scope: "repository" };
  truncated?: boolean;
  message?: string;
}

export interface WorkbenchPreview {
  id: string;
  url: string;
  generation: number;
  viewer_url?: string;
  credentialless?: boolean;
}
