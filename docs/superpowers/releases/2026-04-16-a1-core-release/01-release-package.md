# A1 Release Package

This document defines the smallest honest handoff bundle for the current A1 core-chain release.

## Goal

Hand another operator exactly enough material to reproduce the default Mac/ARM release path without silently depending on:

- `skill-scanner`
- FastSkill
- frozen `U3`
- repo-embedded provider credentials

## What To Deliver

Deliver these artifacts together:

1. repository checkout for the chosen release commit or working tree snapshot
2. `00-START-HERE.md`
3. `deployments/k3s/clawmanager.yaml`
4. `docs/k3s-local-setup.md`
5. `docs/superpowers/releases/2026-04-16-a1-core-release/README.md`
6. `docs/superpowers/releases/2026-04-16-a1-core-release/02-release-checklist.md`
7. `docs/superpowers/releases/2026-04-16-a1-core-release/03-bootstrap-contract.md`
8. `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md`
9. `docs/superpowers/releases/2026-04-16-a1-core-release/05-external-test-guide.md`
10. `docs/superpowers/releases/2026-04-16-a1-core-release/06-live-demo-record.md`
11. `docs/manual-skill-import.md` for the optional manual Skill validation lane

## Required Image References

The operator needs access to these runtime images:

- ClawManager app: `ghcr.io/yuan-lab-llm/clawmanager:latest`
- MySQL: `mysql:8.4.8`
- OpenClaw ARM runtime bootstrap: `ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434`

The default release package does not require `skill-scanner`.

## Required Operator Inputs

The package is incomplete without operator-owned runtime values:

- one active normal model
- one active secure model
- provider base URL
- provider model name
- provider API key

These values must be injected at runtime by the operator. They must not be committed into the repo, manifests, or release notes.

## Handoff Contract

The release owner should state these boundaries explicitly when handing off:

1. default release scope ends at control plane, runtime bootstrap, model bootstrap, and instance or QA mainline readiness
2. `clawmanager-gateway` must keep the validated `9001 -> 9001` Service path
3. `https://...:8443` is not the active remediation path
4. the canonical A1 success path is one single OpenClaw instance plus one direct-QA proof
5. `/api/v1/admin/skills` being empty does not block this release
6. scanner and FastSkill validation are deferred scope

## Recommended Bundle Shape

Use this shape when publishing or sending the package:

```text
ClawManager-A1-core-chain/
├── 00-START-HERE.md
├── deployments/k3s/clawmanager.yaml
├── docs/k3s-local-setup.md
├── docs/manual-skill-import.md
├── docs/superpowers/releases/2026-04-16-a1-core-release/
│   ├── README.md
│   ├── 01-release-package.md
│   ├── 02-release-checklist.md
│   ├── 03-bootstrap-contract.md
│   ├── 04-acceptance-path.md
│   ├── 05-external-test-guide.md
│   └── 06-live-demo-record.md
└── evidence/live/                       # optional copied live evidence
```

The default external-test bundle does not need a full repo snapshot as long as it contains the exact manifest, the exact docs above, the required image references, and the operator-owned model inputs.

If you want to generate this bundle directly from the workspace, use:

```bash
./scripts/release/package-a1-core-release.sh
```

## Honest Stop Rule

Do not claim full release readiness beyond A1 when either of these is still true:

- operator model records are missing
- someone is expecting scanner or FastSkill behavior from the default path
