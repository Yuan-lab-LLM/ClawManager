# Nine-node production deployment

This directory is independent from `deployments/k8s/cluster/clawmanager.yaml`.
The original cluster bundle must remain unchanged.

## Fixed topology

- Control plane: `k8s-master`; it must not have the Longhorn default-disk label.
- Workers: `k8s-worker1` through `k8s-worker8`.
- Longhorn data path: `/var/lib/longhorn` on each worker root LVM.
- Reserved per worker: 900 GiB (`966367641600` bytes).
- Workspace NFS: `10.130.224.6:/mnt/nfs6/sample/clawmanager/workspaces`.
- MySQL backup NFS: `10.130.224.6:/mnt/nfs6/sample/clawmanager/mysql-backups`.
- Longhorn backup NFS: `10.130.224.6:/mnt/nfs6/sample/clawmanager/longhorn-backups`.

Before deployment, verify that 900 GiB is still close to 25% of the actual
filesystem size on every worker. Do not apply the node manifest if a worker has
a materially smaller filesystem.

## Required host preparation

1. Install the NFSv4 client package on every node.
2. Confirm `/var/lib/longhorn` resolves to the local root filesystem, not NFS.
3. Confirm the three NFS server directories exist and have the required access.
4. Configure containerd on every node to pull the HTTP registry
   `10.130.15.40:5000`; this is host configuration and cannot be expressed by a
   Kubernetes workload YAML.
5. Confirm `k8s-master` has no `node.longhorn.io/create-default-disk` label.
6. Confirm a real IEI client resolves both `10-130-15-40.nip.io` and a generated
   runtime hostname such as `opencode-1.10-130-15-40.nip.io` to `10.130.15.40`.
7. Confirm the same client trusts the internal Root CA without a browser
   certificate exception.

## Production Secret

`20-clawmanager-production.yaml` does not contain a Secret or example password.
Create `clawmanager-secrets` in `clawmanager-system` before applying the main
manifest. It must contain these keys:

- `mysql-root-password`
- `mysql-password`
- `jwt-secret`
- `runtime-agent-control-token`
- `runtime-agent-report-token`
- `openclaw-gateway-token`
- `minio-root-user`
- `minio-root-password`
- `minio-access-key`
- `minio-secret-key`
- `mysql-backup-password`
- `mysql-binlog-password` (only required if the optional binlog archiver is enabled)

Keep real values outside Git and outside generated YAML.

## Browser HTTPS entry and Runtime public origins

OpenCode Lite and DeepSeek Harness Lite use root-relative browser resources and
therefore require a dedicated origin for every instance. This site fixes the
templates to:

```text
https://opencode-{instance_id}.10-130-15-40.nip.io:30443/
https://deepseek-harness-{instance_id}.10-130-15-40.nip.io:30443/
```

This changes only browser routing. Instance tokens, Runtime bindings, gateway
ports and workspace paths remain isolated by owner and instance ID. The public
NodePort `30443` targets nginx HTTPS port `8443`. The TLS Secret remains mounted,
and `IEISYSTEM_COOKIE_SECURE=true` keeps IEI owner sessions HTTPS-only.

The northbound Gateway remains a separate HTTPS API for system integrators, and
Gateway-to-Core traffic remains mTLS. Its certificates are independent from the
browser portal certificate.

Use this profile only on the approved private network. HTTPS does not replace
northbound mTLS, bearer-token authorization, owner checks, ShareLink controls or
instance access tokens.

## Apply order

1. `kubectl apply -f 00-worker-longhorn-nodes.yaml`
2. Confirm only the eight workers carry the Longhorn disk label.
3. `kubectl apply -f 10-longhorn-production.yaml`
4. Wait for Longhorn and the `longhorn` StorageClass to be Ready.
5. Create the production `clawmanager-secrets` Secret.
6. `kubectl apply -f 20-clawmanager-production.yaml`
7. Confirm MySQL, Redis, MinIO and Workspace PVCs are Bound.
8. `kubectl apply -f 30-mysql-backup-storage.yaml`
9. `kubectl apply -f 40-longhorn-backup-target.yaml`
10. Confirm ClawManager, MySQL, Redis, MinIO and Longhorn are Ready. The backup
    resources below are deliberately outside the application startup path.
11. `kubectl apply -f 31-mysql-full-backup-cronjob.yaml`
12. Wait for Job `mysql-production-backup-users` to succeed.
13. Create one initial full backup from the CronJob and wait for it to succeed:
    `kubectl create job --from=cronjob/mysql-production-full-backup mysql-production-full-backup-initial -n clawmanager-system`
14. `kubectl apply -f 41-longhorn-recurring-job.yaml`
15. `kubectl apply -f 42-longhorn-backup-assignment.yaml`
16. `kubectl apply -f 33-mysql-restore-verification-job.yaml`. This Job is
    suspended and does not run until an operator explicitly unsuspends it.
17. Run `scripts/bootstrap-northbound-secrets.sh` once as a cluster
    administrator. It generates independent random application keys, a
    restricted MySQL account, the JWE key, a public Gateway CA/certificate and
    a dedicated internal mTLS CA/certificate set. Secret values are written
    only to Kubernetes Secrets; the distributable public CA is written under
    `/home/hxc/nine-node-production/northbound-client/`.
18. Re-apply `20-clawmanager-production.yaml` to enable the Core mTLS listener
    and IEI owner session, then apply `50-northbound-production.yaml`.
19. Wait for both `clawmanager-app` and
    `clawmanager-northbound-gateway` rollouts. The management portal uses HTTPS
    NodePort `30443`, and the system-to-system Northbound API uses HTTPS NodePort
    `32343`.

Do not wait for backup workloads when determining whether ClawManager is Ready.
A backup or archive failure must raise an operational alert, but must not block
ClawManager startup, northbound requests, instance creation, or Runtime pools.

## Backup policy

- MySQL full backup: daily at 02:00 Asia/Shanghai, no overlapping Jobs, maximum
  runtime six hours, seven successful backup directories retained on NFS.
- Kubernetes keeps three successful and three failed CronJob objects. These Job
  history limits do not control NFS backup retention.
- `32-mysql-binlog-archiver.yaml` is an optional disaster-recovery enhancement
  and has `replicas: 0` by default. The mirrored Oracle MySQL 8.4.8 server image
  does not contain `mysqlbinlog`, so this Deployment must not be enabled until a
  validated MySQL 8.4 client image is available and a restore drill succeeds.
- Without the optional NFS binlog archive, recovery from total loss of the
  MySQL Longhorn volume is limited to the latest usable full or Longhorn backup;
  point-in-time recovery after that backup is not guaranteed.
- Longhorn backup: daily at 01:00 UTC (09:00 Asia/Shanghai), retain seven,
  concurrency two. This is after the maximum MySQL full-backup window.
- Longhorn assignment includes MySQL, Redis, MinIO and real ClawManager instance
  PVCs. A PVC labelled `clawmanager.io/data-class=cache` is explicitly excluded.
- `33-mysql-restore-verification-job.yaml` restores only to an isolated
  `emptyDir`; it never connects to or overwrites the production database.

MySQL backup and binlog users require TLS. The current ClawManager database
client does not expose a TLS option, so this site does not enable global MySQL
`require_secure_transport`. Enabling global enforcement requires an application
database-client change and a new ClawManager image first.

## Production image registry

All active workload images in this site use `10.130.15.40:5000`. The application,
managed Runtime images, MySQL, Redis, MinIO, Skill Scanner, all Longhorn
components and `kubectl:v1.31.14` were checked against the remote
Registry manifests on 2026-08-24. All 24 target digests were present and all
image configs reported `linux/amd64`.

The database bootstrap also stores the site-local images for the supported Lite
and Pro runtimes. This prevents a fresh installation from reverting those
managed Runtime paths to an external default image.

## Required checks

- Master has no schedulable Longhorn disk.
- Longhorn disks exist only on the eight workers.
- MySQL/MinIO/Redis PVC sizes are 500 GiB/500 GiB/20 GiB.
- Workspace is 8 TiB, RWX, `nfs-static`, NFSv4.2 and `Retain`.
- `log_bin=ON`, `binlog_expire_logs_seconds=604800`, and
  `max_binlog_size=134217728`.
- `server_id=1`, `gtid_mode=ON`, and `enforce_gtid_consistency=ON`.
- The backup-user bootstrap Job succeeds and both dedicated users require SSL.
- The first full backup has a valid checksum, manifest, GTID set and binlog
  coordinate; the binlog archiver becomes Ready afterwards.
- Only durable Longhorn volumes carry the
  `recurring-job-group.longhorn.io/production-durable=enabled` label.
- A Team shared PVC binds to an NFS PV below the configured workspace path.
- No production YAML contains example or real secret values.
- OpenCode and DeepSeek Harness templates are non-empty and contain exactly one
  `{instance_id}` placeholder.
- An ordinary browser opens the portal and both generated HTTPS runtime
  hostnames through the approved TLS certificate chain, without mixed content.
- Two instances of the same Runtime retain distinct cookies, Runtime bindings
  and workspace directories.
