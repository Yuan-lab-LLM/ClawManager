# A1 Release Checklist

Use this checklist when packaging or publishing the A1 core-chain release.

## Release Scope Guard

- [ ] release statement says A1 core-chain only
- [ ] release statement explicitly excludes `skill-scanner`, FastSkill, and frozen `U3`
- [ ] release statement does not claim `/api/v1/admin/skills` population
- [ ] release statement preserves the validated `9001 -> 9001` control path
- [ ] release statement defines one single-instance path plus one direct-QA proof as the A1 acceptance chain

## Package Contents

- [ ] include `00-START-HERE.md`
- [ ] include `deployments/k3s/clawmanager.yaml`
- [ ] include `docs/k3s-local-setup.md`
- [ ] include `docs/superpowers/releases/2026-04-16-a1-core-release/README.md`
- [ ] include `docs/superpowers/releases/2026-04-16-a1-core-release/01-release-package.md`
- [ ] include `docs/superpowers/releases/2026-04-16-a1-core-release/03-bootstrap-contract.md`
- [ ] include `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md`
- [ ] include `docs/superpowers/releases/2026-04-16-a1-core-release/05-external-test-guide.md`
- [ ] include `docs/superpowers/releases/2026-04-16-a1-core-release/06-live-demo-record.md`
- [ ] include `docs/manual-skill-import.md` as optional manual Skill lane
- [ ] include exact image references for ClawManager, MySQL, and OpenClaw ARM bootstrap
- [ ] create one archive bundle with `./scripts/release/package-a1-core-release.sh`

## Secrets And Inputs

- [ ] no provider API keys are written into repo files
- [ ] no provider API keys are written into manifests
- [ ] handoff notes state that the operator must supply one active normal model and one active secure model
- [ ] handoff notes state that provider base URL, model name, and API key are operator-owned inputs

## Bootstrap Contract Guard

- [ ] exact `PUT /api/v1/system-settings/images` step is present for the ARM runtime image bootstrap
- [ ] exact `PUT /api/v1/admin/models` upsert path is present for both normal and secure models
- [ ] `GET /api/v1/admin/models` verification is required before any instance-ready or direct-QA claim

## Acceptance Path Guard

- [ ] first validation instance is frozen to the documented single-instance OpenClaw profile
- [ ] first validation instance path uses `memory_gb=3`
- [ ] acceptance requires `status=running`, `infra_status=ready`, `agent_status=online`, and `openclaw_status=running`
- [ ] acceptance requires one direct-QA proof after runtime readiness
- [ ] if direct QA fails because models or provider credentials are missing, classify it as a model-bootstrap blocker

## Evidence Guard

- [ ] keep the existing manifest syntax evidence at `/tmp/a1-core-release-dryrun.txt`
- [ ] do not rerun broad validation unless one of the shipped files changes again
- [ ] if the manifest changes later, rerun only `kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml`

## Deferred Scope Guard

- [ ] do not reopen the `https://...:8443` hypothesis without new contradicting evidence
- [ ] do not reintroduce scanner into the default Mac/ARM release path
- [ ] do not promote live-only scanner overrides to release-default truth
