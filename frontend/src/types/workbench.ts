export interface WorkbenchCapabilities {
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
  truncated?: boolean;
  message?: string;
}
