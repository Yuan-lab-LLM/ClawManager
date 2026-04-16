# 2026-04-16 A1 Core-Chain Release

This document defines the current shippable release scope for ClawManager on local Mac/ARM.

## Canonical Artifacts

- `../../../../00-START-HERE.md`: the single-file dumb-proof operator entry
- `README.md`: release scope and boundary
- `01-release-package.md`: what to hand to another operator
- `02-release-checklist.md`: narrow release closeout checklist
- `03-bootstrap-contract.md`: exact ARM runtime + model bootstrap contract
- `04-acceptance-path.md`: single-instance + direct-QA acceptance path
- `05-external-test-guide.md`: step-by-step guide for external testers
- `06-live-demo-record.md`: current live demonstration summary and evidence map
- `../../../k3s-local-setup.md`: operator deployment tutorial

## Release Scope

This release includes:

- K3S / k3d deployment on macOS
- ClawManager control plane startup
- admin login
- `clawmanager-gateway` agent control path over `9001`
- OpenClaw ARM runtime bootstrap through system image settings
- operator model bootstrap through documented normal + secure model records
- one single OpenClaw validation instance on the documented local profile
- one direct-QA proof after runtime readiness and model bootstrap

This release does **not** include:

- `skill-scanner`
- FastSkill discovery
- frozen `U3` capability validation
- any claim that `/api/v1/admin/skills` is populated

## Default Deployment Truth

The default K3S release manifest is:

- `deployments/k3s/clawmanager.yaml`

For this release, the manifest keeps `skill-scanner` disabled by default so Mac/ARM deployments do not block on the upstream scanner image lacking `linux/arm64`.

## Required Runtime Components

The release path depends on these components:

- ClawManager app image: `ghcr.io/yuan-lab-llm/clawmanager:latest`
- MySQL image: `mysql:8.4.8`
- OpenClaw ARM runtime bootstrap image: `ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434`

`skill-scanner` is not required for the default release path.

## Operator Inputs

The operator must provide:

- one active normal model
- one active secure model
- provider base URL, provider model name, and API key at runtime

These are required release inputs, not repo defaults.

## Packaging Guidance

To ship this release to another operator, provide:

- the ClawManager repository
- `deployments/k3s/clawmanager.yaml`
- `docs/k3s-local-setup.md`
- this release summary
- `03-bootstrap-contract.md`
- `04-acceptance-path.md`
- access to the required runtime images
- operator-owned model credentials and bootstrap values

Do not package secrets into the repo or manifests.

For the fastest operator path, start with `../../../../00-START-HERE.md`.
For the exact handoff bundle, use `01-release-package.md`. For the exact bootstrap and acceptance chain, use `03-bootstrap-contract.md` and `04-acceptance-path.md`.
For a tester-facing walkthrough, use `05-external-test-guide.md`. For the current demo baseline and copied evidence files, use `06-live-demo-record.md`.

## Archived Deferred Scope

The blocked internal RC investigation for FastSkill / `U3` remains archived under:

- `docs/superpowers/releases/2026-04-15-p1-internal-rc/`

That packet is retained as evidence, not as the default release contract for A1.
