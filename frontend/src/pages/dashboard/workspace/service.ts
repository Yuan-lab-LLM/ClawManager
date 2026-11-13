import api from '../../../services/api';
import type { Dashboard, Range } from './types';
export async function getWorkspace(range: Range, signal: AbortSignal): Promise<Dashboard> {
 const response = await api.get<{ data: Dashboard }>('/dashboard/workspace', { params: { range }, signal });
 return response.data.data;
}
export async function getWorkspaceUsage(range: Range, signal: AbortSignal): Promise<import('./types').UsageDashboard> {
 const response = await api.get<{data: import('./types').UsageDashboard}>('/dashboard/workspace/usage', {params:{range},signal});
 return response.data.data;
}
