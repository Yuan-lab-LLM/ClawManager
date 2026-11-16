import type {
  ClusterResourceOverview,
  ResourceSummary,
} from '../../../services/adminService';
export type Coverage = {
  status: 'ready' | 'partial' | 'unavailable';
  note?: string;
};
export interface AdminOverview {
  timezone: string;
  window: { start: string; end: string };
  total_users: number | null;
  total_instances: number | null;
  active_users: number | null;
  active_instances: number | null;
  idle_instances: number | null;
  idle_total: number | null;
  usage: {
    total_tokens: number | null;
    conversations: number | null;
    success_rate: number | null;
    average_response_ms: number | null;
    currencies: Array<{ currency: string; amount: number; complete: boolean }>;
  };
  history: Array<{
    at: string;
    tokens: number | null;
    conversations: number | null;
  }>;
  models: Array<{ name: string; tokens: number }>;
  types: Array<{
    type: string;
    instances: number;
    runtime_pods: number;
    ready_pods: number;
  }>;
  coverage: Record<string, Coverage>;
}
export type ResourceKey = 'cpu' | 'memory' | 'disk';
export type ResourceReading = ResourceSummary & {
  used?: number | null;
  observed_at?: string | null;
};
export type ClusterOverview = Omit<
  ClusterResourceOverview,
  'cpu' | 'memory' | 'disk' | 'nodes'
> &
  Record<ResourceKey, ResourceReading> & {
    nodes: Array<
      Omit<
        ClusterResourceOverview['nodes'][number],
        'cpu' | 'memory' | 'disk'
      > &
        Record<ResourceKey, ResourceReading>
    >;
  };
export type ResourceSample = { at: string } & Record<
  ResourceKey,
  { actual: number | null; requested: number | null }
>;
export type Slice = { name: string; count: number; share: number; key: string };
