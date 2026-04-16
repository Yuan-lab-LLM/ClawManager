# Release Checklist

## Gate 1 / Gate 2 Replay
- [ ] create a fresh `k3d` cluster and capture cluster creation evidence
- [ ] apply `deployments/k3s/clawmanager.yaml` and record the earliest repo-default boundary
- [ ] on Mac/ARM, if `skill-scanner` fails with `no match for platform in manifest`, run only the documented live-only ARM override path
- [ ] verify `mysql`, `clawmanager-app`, and `skill-scanner` all reach `Running`
- [ ] verify `https://localhost:30443/healthz` returns `200`
- [ ] verify admin login succeeds
- [ ] verify `/api/v1/system-settings/images` contains the OpenClaw ARM dev bootstrap image on the local validation path

## Operator Bootstrap
- [ ] create one active normal model
- [ ] create one active secure model
- [ ] verify `/api/v1/admin/models` returns both records before any validation-instance or direct-QA claim
- [ ] stop honestly if operator model material is unavailable

## Capability and Closure
- [ ] do not claim Source B or full `U3` validation while `/api/v1/admin/skills` remains empty
- [ ] do not promote live-only ARM overrides to repo-default truth
- [ ] record fresh replay evidence and Gate status in `05-gate-record.md` and `06-evidence-index.md`
