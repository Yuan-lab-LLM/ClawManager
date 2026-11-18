# Model activity liveness and automatic sleep

Approved by the user on 2026-11-10: real model activity determines the idle window; running requests need liveness verification; disconnected requests and exited processes must not leave permanent blockers. Web previews, terminals and browsing do not count as model activity.

Use renewable request leases in the existing shared AI Gateway. A request admits a 90-second lease, renews every 15 seconds while its execution context is alive, and records the latest verified activity. Transient renewal failures can retry within the lease. A local watchdog cancels inference before the durable lease expires. Completed, canceled and expired scopes cannot be renewed or revived. All public chat, streaming, image and HTTP admission paths use the same context and scope.

The idle controller reconciles expired scopes on due scans. Reconciliation records an expiry reason, preserves the last real activity timestamp, and keeps the instance/generation checks and transaction ordering shared with stop claims. A currently leased request continues to protect long generations. Failed completion writes are recoverable through expiry.

Legacy rows have no lease and stay protected during a mixed-version rollout. The controller may close them only after Kubernetes confirms all application Pods in its controller scope run the same current image and the previous processes have exited. Use a creation-time cutoff so this proof cannot close later unidentified scopes. Unavailable proof keeps the blocker explicit. No direct business database repair is used.

Stage the additive migration with the application, retain the configured 60-second scan and 300-second idle threshold, and verify instance 118 sleeps through the normal controller. Code and Git data survive; the instance's debug service stops under the user's existing sleep policy. No Runtime migration or unrelated scheduling change is included.
