export type Range = 'today' | '7d' | '30d';
export type AgentState = 'busy' | 'idle' | 'running' | 'sleeping' | 'starting' | 'failed' | 'offline' | 'unknown';
export interface Coverage { status: 'ready' | 'partial' | 'unavailable'; note?: string; since?: string }
export interface Agent {
 id: number; name: string; type: string; instance_mode: string; status: string; state: AgentState;
 team_id?: number; team_name?: string; session_id?: string; progress: number | null;
 current_task_id?: string; current_task_title?: string;
 cpu_cores: number; memory_gb: number; storage_gb: number; created_at: string; updated_at: string;
}
export interface Task {
 key: string; source: 'team' | 'runtime'; id: string; team_id?: number; instance_id?: number; session_id?: string;
 title: string; agent_name: string; status: 'pending' | 'running' | 'succeeded' | 'failed' | 'ended';
 progress: number | null; error_message?: string; result_excerpt?: string;
 created_at: string; updated_at: string; finished_at?: string;
}
export interface Currency { currency: string; amount: number | null; previous_amount: number | null; estimated: boolean; complete: boolean; recorded_calls: number }
export interface Observation extends Record<AgentState, number | null> {
 at: string; total: number | null; allocated_cpu: number | null; allocated_memory_gb: number | null;
 max_cpu: number | null; max_memory_gb: number | null; eligible_slots: number | null;
}
export interface Dashboard {
 recent_agents: Array<Agent & {last_used_at: string; activity_kind: 'model' | 'chat' | 'team' | 'runtime'}>;
 recent_truncated: boolean;
 range: Range; timezone: string; as_of: string;
 window: { start: string; end: string; previous_start: string; previous_end: string };
 instances: Agent[]; instances_total: number; instances_truncated: boolean; tasks: Task[]; tasks_truncated: boolean;
 state_counts: Record<AgentState, number>;
 usage: { total_tokens: number | null; prompt_tokens: number | null; completion_tokens: number | null; invocation_count: number | null; previous_tokens: number | null; currencies: Currency[] };
 usage_history: Array<{ at: string; tokens: number | null; calls: number | null; costs: Record<string, number | null> }>;
 agent_usage: Array<{ instance_id: number; tokens: number; costs: Record<string, number> }>;
 capacity: { eligible_slots: number | null; runtimes: Array<{ type: string; available: number | null }>; allocated_cpu: number; allocated_memory_gb: number; allocated_storage_gb: number; max_instances: number | null; max_cpu: number | null; max_memory_gb: number | null; max_storage_gb: number | null };
 observations: Observation[];
 activity: Array<{ date: string; events: number; calls: number; tasks: number; tokens: number; observed: boolean }>;
 coverage: Record<'recent' | 'instances' | 'tasks' | 'usage' | 'costs' | 'capacity' | 'observations' | 'activity', Coverage>;
}
export type UsageDashboard = Pick<Dashboard, 'range' | 'timezone' | 'as_of' | 'window' | 'usage' | 'usage_history' | 'agent_usage'> & {coverage: Pick<Dashboard['coverage'], 'usage' | 'costs'>};
