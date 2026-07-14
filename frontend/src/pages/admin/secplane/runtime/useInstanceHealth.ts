import { useCallback, useEffect, useState } from 'react';
import { adminInstanceService } from '../../../../services/adminInstanceService';
import type { Instance } from '../../../../types/instance';

// Shared instance-health fetch for the runtime scenario pages. Returns the
// full list plus pre-computed counts so pages can warn the operator before
// they dispatch policy to instances that are stopped / errored / missing.
// An unhealthy target makes the apply command sit pending forever, because
// the agent on a dead pod can't poll the command queue.

export interface InstanceHealth {
  instances: Instance[];
  loading: boolean;
  error: string | null;
  healthy: Instance[];
  unhealthy: Instance[];
  reload: () => Promise<void>;
}

export function useInstanceHealth(pageSize = 200): InstanceHealth {
  const [instances, setInstances] = useState<Instance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await adminInstanceService.getInstances(1, pageSize);
      setInstances(data.instances ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setInstances([]);
    } finally {
      setLoading(false);
    }
  }, [pageSize]);

  useEffect(() => {
    reload();
  }, [reload]);

  const healthy = instances.filter((i) => i.status === 'running');
  const unhealthy = instances.filter((i) => i.status !== 'running');

  return { instances, loading, error, healthy, unhealthy, reload };
}
