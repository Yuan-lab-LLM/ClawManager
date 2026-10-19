# Northbound API deployment add-on

For an in-place upgrade of an existing Kubernetes or K3s installation, follow
the [Chinese upgrade guide](../../../docs/northbound-upgrade-guide.md) before
applying this add-on.

This add-on keeps the existing ClawManager HTTP service private and adds a
separately deployed northbound gateway. The supplied Service publishes only the
gateway through NodePort `38443`; restrict that port with the partner IP
allow-list and firewall/WAF policy. Core remains private and must not be exposed.

If an Ingress supplies `X-Forwarded-For`, set `NORTHBOUND_TRUSTED_PROXIES` to
the explicit comma-separated proxy IPs or CIDRs. Leaving it empty is the safe
default and causes application rate limits to use the direct peer address.

## Prerequisites

1. Deploy a ClawManager image that contains both
   `/usr/local/bin/clawreef-server` and
   `/usr/local/bin/clawreef-northbound-gateway`.
2. Roll out Core first so migrations `045_add_northbound_api.sql` and
   `047_add_instance_owner.sql` are applied.
3. Issue separate certificates for:
   - the public gateway server;
   - the gateway's Core client identity;
   - the Core server, whose SAN includes
     `clawmanager-northbound-core.clawmanager-system.svc.cluster.local`.
4. Use a dedicated CA for the gateway-to-Core mTLS identity. Core must not trust
   a general-purpose client CA on port 9002.
5. Generate the JWE key with at least 3072 RSA bits:

   ```sh
   openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out private.pem
   ```

## Required Secrets

Create these Secrets without committing key material:

```text
clawmanager-northbound-secrets             opaque configuration secrets
clawmanager-northbound-gateway-tls         tls.crt, tls.key (public server)
clawmanager-northbound-gateway-client-tls  tls.crt, tls.key (mTLS client)
clawmanager-northbound-core-tls            tls.crt, tls.key (Core server)
clawmanager-northbound-core-ca             ca.crt (verifies Core)
clawmanager-northbound-gateway-client-ca   ca.crt (verifies Gateway client)
clawmanager-northbound-jwe                 private.pem (RSA 3072+)
clawmanager-iei-sso                        aes-key, aes-iv, session-secret
```

Use independent, cryptographically random values of at least 32 bytes for the
northbound JWT secret, refresh-token pepper, and internal JWT secret. Do not
reuse the existing web JWT secret. `secrets.example.yaml` documents the key
names but must not be applied with its placeholder values.

For the IEI page, `aes-key` is the exact 16-byte key agreed with the unified
platform, `aes-iv` is the exact 16-byte business-system identifier (the example
uses `CLAWMANAGETOKENS`), and `session-secret` is an independent random value of
at least 32 bytes. The AES key from the integration document must be injected
as a Secret and must not be committed to this repository.

The gateway deliberately connects without running migrations. Give it a
dedicated database account with only the permissions it needs after Core has
applied migrations:

```sql
GRANT SELECT ON clawmanager.users TO 'clawmanager_northbound'@'%';
GRANT SELECT, INSERT, UPDATE ON clawmanager.northbound_auth_challenges TO 'clawmanager_northbound'@'%';
GRANT SELECT, INSERT, UPDATE ON clawmanager.northbound_sessions TO 'clawmanager_northbound'@'%';
GRANT SELECT ON clawmanager.northbound_admin_settings TO 'clawmanager_northbound'@'%';
GRANT SELECT ON clawmanager.northbound_caller_policies TO 'clawmanager_northbound'@'%';
GRANT INSERT ON clawmanager.audit_events TO 'clawmanager_northbound'@'%';
```

The account has no access to operations, instances, instance secrets, runtime
tokens, schema migrations, or Kubernetes resources.

## Install

Patch the existing Core Deployment, then deploy the gateway and network
boundaries:

```sh
kubectl patch deployment clawmanager-app -n clawmanager-system \
  --type strategic --patch-file core-patch.yaml
kubectl rollout status deployment/clawmanager-app -n clawmanager-system
kubectl apply -f gateway.yaml
kubectl rollout status deployment/clawmanager-northbound-gateway -n clawmanager-system
```

The northbound endpoint is available at
`https://<approved-node-address>:38443`. Do not expose ports `9001`, `9002`, or
the existing management backend as part of this change.

The IEI user-facing entry remains on the existing ClawManager HTTPS endpoint:
`https://<management-ip>:<management-port>/ieisystem/list-instances?token=...`.
It is not served from northbound gateway port `38443`.

The Core patch intentionally creates no public Service. Port 9002 is available
only through `clawmanager-northbound-core` (`ClusterIP`), and the NetworkPolicy
allows it only from pods labelled `app=clawmanager-northbound-gateway`. Gateway
egress does not allow port 9001.

Before adding public DNS, validate all four negative network cases from the
design: public access to 9001/9002, gateway access to 9001, and access to 9002
with a non-gateway client certificate must all fail.

The API contract is in `docs/northbound-openapi.yaml`; the Python client showing
the JWE challenge flow is in `examples/northbound_client.py`.
