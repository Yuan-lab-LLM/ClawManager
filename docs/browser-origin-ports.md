# Chrome browser confirmation without certificate installation

This optional mode keeps the manager, OpenCode/DeepSeek Harness native interfaces
and web previews on one HTTPS hostname, using different public ports. All ports
serve the existing manager certificate. A fresh Chrome profile confirms that
hostname once in the browser; no certificate files or local CA are installed.
Chrome can expire or clear its exception. Other browsers are not supported.

Test cluster manager: `https://10.130.81.54:30443`; application pool: `31600-31727`.
A configured nip.io hostname also works, but use that exact hostname consistently:
an exception for an IP address does not approve a different nip.io hostname.
Set both environment variables on the existing manager deployment:

```
CLAWMANAGER_BROWSER_ORIGIN=https://10.130.81.54:30443
CLAWMANAGER_BROWSER_PORT_RANGE=31600-31727
```

When these variables are enabled, exposing nginx8444 through a matching Service
is required. The manager frontend Service on30443 does not expose the application
port pool. Missing NodePorts cause connection failures that the browser's
certificate probe can display as a certificate-confirmation prompt; confirming a
certificate cannot repair an unreachable port.

For server54, apply the checked-in [browser-origin Service](../deployments/k8s/sites/server54/browser-origins.yaml)
alongside the application deployment, including after cluster reinstallation:

```sh
kubectl apply --dry-run=server -f deployments/k8s/sites/server54/browser-origins.yaml
kubectl apply -f deployments/k8s/sites/server54/browser-origins.yaml
kubectl -n clawmanager-system get service clawmanager-browser-origins
kubectl -n clawmanager-system get endpointslice -l kubernetes.io/service-name=clawmanager-browser-origins
```

Check that every configured public port maps to8444, ready endpoints exist,
and an assigned native origin's `/__clawmanager_cert_check` returns204. Compare
its TLS certificate with30443 and validate OpenCode/DSH in a browser that has
confirmed only the manager hostname. Do not use `ignoreHTTPSErrors` as evidence
that this confirmation flow works. Unassigned origins and unauthenticated native
requests must continue to fail closed.

The following example only prints a manifest;
adapt namespace, selector and ports before applying it elsewhere.

```python
import json
print(json.dumps({
    "apiVersion": "v1", "kind": "Service",
    "metadata": {"name": "clawmanager-browser-origins", "namespace": "clawmanager-system"},
    "spec": {"type": "NodePort", "selector": {"app": "clawmanager-app"},
             "ports": [{"name": f"browser-{p}", "port": p, "targetPort": 8444,
                        "nodePort": p, "protocol": "TCP"} for p in range(31600, 31728)]}
}, indent=2))
```

Each native instance and distinct preview service target gets a permanent port
in `browser_origin_ports` (migration102). Paths share a service target's port.
Limits:512 ports per configured pool;32 distinct preview targets per instance.
Deleted instances retain tombstones. Ports are never reassigned to a different
instance identity. Exhaustion returns an error; changing ranges must retain
existing assigned ports or use a new browser hostname.

Back up this table as business metadata (backup contract13). Restoring an older
backup must not rewind port ownership under a previously used hostname: preserve
or merge the ledger, or use a fresh hostname and unused range. Contract12 backups
remain readable with their matching older server version.

Previews use Chrome credentialless iframes and temporary storage. Switching
instance documents resets that storage. Popups are disabled; the external button
opens a trusted manager viewer. Native OpenCode/DeepSeek Harness frames retain
port-specific browser storage and must remain cookie-free applications. Platform
cookies are removed upstream; application Set-Cookie headers are rejected on
native ports. Other native runtime types retain their existing gateway behavior.

nginx accepts the measured128-native-cookie header budget. The8444 listener fails
closed if the feature is disabled or Host does not match the configured pool.
Logs redact capability bootstrap paths and gateway tokens. Disabling both
variables restores legacy URL issuance. Keep the ledger if this hostname and
pool may be enabled again.
