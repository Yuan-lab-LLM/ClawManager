# Deploy Preflight

## Fresh-Cluster Rule
- Start from an empty `k3d` cluster for P1 replay.
- Do not reuse prior `clawmanager-system` or `clawmanager-user-*` namespaces.
- Do not rely on residual MySQL data, residual PVCs, or prior admin model records.

## Source of Truth
- Primary deploy truth: `deployments/k3s/clawmanager.yaml`
- Supporting operator guide: `docs/k3s-local-setup.md`
- Deploy-critical drift check target: `deployments/k8s/clawmanager.yaml`

## Local Resource Envelope
| Item | P1 minimum |
| --- | --- |
| First OpenClaw validation instance | `memory_gb=3` |
| Initial OpenClaw runtime image | `ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434` |
| Local ARM skill-scanner validation image | `docker.io/library/skill-scanner:dev-arm64-pkt16a-20260415160730` |
| Gateway control path | `http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001` |

## Required Operator Inputs
| Input | How supplied | Repo rule |
| --- | --- | --- |
| External model base URL | operator env / admin UI / admin API | never hardcode |
| External model API key | operator env / admin UI / admin API | never commit |
| Secure model selection | operator model record | never assume live DB carry-over |

## Mac/ARM Fresh Replay Truth
- `P1-PKT-19` reproduced the earliest repo-default boundary from a fresh cluster: `ghcr.io/yuan-lab-llm/skill-scanner:latest` fails on `linux/arm64` with `no match for platform in manifest: not found`.
- For local `R3-A-U3-F2-P1` validation on Mac/ARM, the documented live-only override path is:
  - import `ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434`
  - import `docker.io/library/skill-scanner:dev-arm64-pkt16a-20260415160730`
  - `kubectl set image deployment/skill-scanner -n clawmanager-system skill-scanner=docker.io/library/skill-scanner:dev-arm64-pkt16a-20260415160730`
- This local override path does not promote repo-default deploy truth to PASS.
- Even after the live-only ARM overrides, do not start validation-instance bring-up until the operator has created one active normal model and one active secure model.

## Prohibited Shortcuts
- no repo-external oral steps
- no undocumented manual DB edits
- no hidden live-state reuse
