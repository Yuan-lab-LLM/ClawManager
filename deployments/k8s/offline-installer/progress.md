# Offline Installer Progress

- [x] Skip completed release downloads when lock/ref, archive checksum and RepoTags
  agree; latest also requires matching recorded source digest. Twelve local tests
  pass, including corrupt archives and changed latest digests. Old latest exports
  without source metadata are conservatively rebuilt once.

- [x] Limit optional release catalog to application images; keep infrastructure
  entries in images.lock and document the separate deb/image build responsibilities.

- [x] Resolve public GHCR business latest to a date release with matching manifest
  digest; pull by digest and save under concrete tag. Real read-only GHCR metadata
  check resolved OpenClaw Lite to v2026.9.13; no image layers or servers changed.

- [x] Replace per-image CLI configuration with a complete release-images.conf
  catalog, batch pull/save/lock updates, group selection, and read-only preview;
  include public business releases and existing infrastructure dependency versions.

- [x] Add optional GHCR preparation command: explicit version pull, verified
  single-image archive, checksum, and backed-up per-key lock update; local mocked
  Docker tests cover success/retry, failures, invalid inputs, and concurrent edits.

- [x] Confirm Ubuntu 22.04/24.04, one control-plane + N worker topology.
- [x] Confirm explicit single-node/cluster storage profile.
- [x] Confirm SSH orchestration from the control-plane.
- [x] Confirm one shared-layer image archive and offline-only target environment.
- [x] Confirm installer-owned partial kubeadm cleanup and resume behavior.
- [x] Add version, node, image, and package directory conventions.
- [x] Implement SSH preparation and passwordless-sudo validation.
- [x] Implement bundle validation, offline-policy rendering, and creation without changing ClawManager image configuration.
- [x] Implement idempotent node/Kubernetes installation and installer-owned retry state.
- [x] Implement Calico, Longhorn, and ClawManager rollout checks.
- [x] Pass Bash syntax, ShellCheck, Python compile, and static manifest/lock checks.
- [x] Align the local ClawManager/OpenClaw/Hermes/Workbuddy tar tags with both K8s manifests.
- [x] Add MySQL bootstrap mappings for the offline runtime image tags.
- [x] Package MySQL, MinIO, and Redis into one local platform input tar.
- [x] Build the Jammy deb repository and pass a network-disabled Ubuntu 22.04 package installation test.
- [x] Build and SHA256-verify the full Ubuntu 22.04 amd64 offline archive with release runtime image tags.
- [x] Make manifest rendering and image validation portable across Windows CRLF and Linux LF line endings.
- [x] Ignore the kubelet package's empty manifest directory and `.kubelet-keep` when detecting partial kubeadm state.
- [x] Wait for the Tigera operator to create its CRDs before checking their Established condition.
- [x] Treat the single-node Longhorn-storage skip path as a successful no-op under `set -e`.
- [x] Run a real Ubuntu 22.04 single-node acceptance test, including interrupted-step resume and a completed-install rerun.
- [x] Pin the bundled MinIO image to `RELEASE.2025-09-07T16-13-09Z` in the image lock and both offline manifests.
- [x] Reinitialize MySQL from an empty single-node hostPath, verify the seeded `admin` login, and re-register runtime pods after the database reset.
- [x] Add separate Jammy/Noble release profiles and version-specific build entry points.
- [x] Split Jammy/Noble into version directories while sharing images, manifests, configuration, and installer code.
- [x] Document user-replaceable ClawManager/Runtime archives separately from the fixed shared platform dependency archive.
- [x] Build and index the Noble amd64 deb repository with all required Kubernetes, containerd, storage, and system dependencies.
- [x] Reuse the verified Ubuntu 22.04 combined image archive without Docker load/save when the image lock is unchanged.
- [x] Build and SHA256-verify the full Ubuntu 24.04 amd64 offline archive.
- [x] Run a real Ubuntu 24.04 single-node r2 acceptance test from cleared Kubernetes,
  containerd, Longhorn, installer state, and hostPath data; verify local PV storage,
  absence of Longhorn resources, seeded admin login, and a completed-install rerun.
- [ ] Run a real Ubuntu 22.04 multi-node acceptance test.
- [x] Run a real Ubuntu 24.04 two-node acceptance test on Ubuntu 24.04.3 amd64,
  including SSH orchestration, Kubernetes/Calico/Longhorn/ClawManager rollout,
  MySQL initialization, admin API login, and a completed-install rerun without
  replacing the MySQL Pod or PVCs.
- [x] Repeat the Ubuntu 24.04 r2 acceptance from cleared Kubernetes, containerd,
  Longhorn, installer state, and worker image cache; verify a fresh 22,927,010,816-byte
  worker archive transfer before kubeadm initialization.
- [x] Use an explicit containerd 2.x config v3 with a healthy CRI v1 plugin and
  restart containerd after installer-owned kubeadm reset to avoid a CRI socket race.
- [x] Add an Ubuntu 24.04 two-node acceptance checklist covering offline install, Longhorn single replica, runtime creation, rerun, and diagnostics.
- [x] Add a read-only all-node preflight and Ubuntu 24.04 preparation guide.
- [x] Make runtime image validation dynamic instead of requiring fixed OpenClaw/Hermes/Workbuddy keys.
- [x] Reserve optional image-lock slots for OpenClaw Pro/Lite, Hermes Pro/Lite,
  DSH Pro/Lite, OpenCode Pro/Lite, and Workbuddy Internal; skip empty Runtime
  slots while retaining strict validation for active and platform images.
- [x] Make `images.lock` the single release image map; render platform/Runtime
  YAML and bootstrap SQL in staging without changing `is_enabled`.
- [x] Add local-only grouped image export with `--group`, exact RepoTag/archive
  checks, missing-image failure, and release-tagged input tar names.
- [x] Add `--release-tag` output naming; require locally supplied application images.
- [x] Fill the infrastructure image preparation gap in the main bundle builder:
  reuse local images, pull missing Kubernetes/Calico/Longhorn images at locked
  versions with three attempts, validate architecture, and retain a single final archive.
  Local mocked checks cover cache hits, all three families, retry, download failure,
  missing application images, and wrong architecture. No server changes or full
  network bundle build were performed for this change.
