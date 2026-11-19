# Database and Redis capacity budgets

The canonical K8s/K3s manifests, offline manifests and nine-node site set explicit
resource limits. These are deployment settings, not claims of measured capacity.
For the 10,000-instance design, Team count is a separate input.

| Process | Admission / pool | Container limit | Notes |
| --- | --- | --- | --- |
| MySQL | 800 connections; 4 GiB InnoDB pool | 4 CPU / 8 GiB | 256 MiB global TempTable RAM; bounded per-session read/sort/join buffers |
| Redis | 1 GiB maxmemory; noeviction | 2 CPU / 3 GiB | Room for allocator overhead and AOF rewrite copy-on-write; never evict reliable data |
| Core replicas (3) | 80 business + 64 backup-boundary connections per replica | existing app limit | 432 aggregate before separate control pools |
| Northbound replicas (2) | 30 business + 64 backup-boundary connections per replica | existing gateway limit | 188 aggregate before separate control pools |
| Control / verification / operations | reserve 180 connections | included in MySQL total | Individual control pools are capped at four; verify actual replica inventory before rollout |

Generic manifests use the existing 50-connection business default. A deployment
with additional replicas must reduce per-process pools or revise the aggregate
budget before scaling. Replica count is not an instruction to increase every
pool. Standalone/small-node operators may lower limits and buffer pools together;
the displayed requirements must fit the node before applying these manifests.

Reliable Team streams have independent entry, payload, byte and namespace limits.
The runtime notification stream is limited to 10,000 entries, 24 hours and 16 KiB
per notification; large notifications retain identity and request a SQL refresh.
Its maximum serialized payload budget is approximately 160 MiB before Redis
overhead. Team quotas share the global Redis budget; a few Teams do not imply
10,000 Team namespaces or a dedicated Redis server per instance.

No automatic eviction is used to recover memory. Backpressure leaves accepted
messages intact. The leader reconciles expired state, trims only proved delivered
prefixes, and archives old DLQ/deleted-Team data before removal. During AOF rewrite,
observe used_memory, used_memory_rss, current_cow_peak, rejected writes and cgroup
memory; recorded test results belong in the functional HTML report. Raising
maxmemory to the container limit removes the rewrite/restart headroom.
