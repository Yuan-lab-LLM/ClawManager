# Operator Runbook

## Runtime and Model Bootstrap Checklist
1. log in as admin through `/api/v1/auth/login` and capture `CM_ADMIN_TOKEN`
2. on Mac/ARM local validation, import the documented ARM dev images into the current `k3d` cluster:

```bash
k3d image import \
  ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434 \
  docker.io/library/skill-scanner:dev-arm64-pkt16a-20260415160730 \
  -c clawmanager
```

3. on Mac/ARM local validation, override the live `skill-scanner` deployment image:

```bash
kubectl set image deployment/skill-scanner \
  -n clawmanager-system \
  skill-scanner=docker.io/library/skill-scanner:dev-arm64-pkt16a-20260415160730

kubectl rollout status deployment/skill-scanner -n clawmanager-system --timeout=180s
```

4. set the `openclaw` system image to the documented ARM dev bootstrap image:

```bash
CM_BASE_URL=https://localhost:30443

curl -sk -X PUT "$CM_BASE_URL/api/v1/system-settings/images" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"instance_type":"openclaw","display_name":"OpenClaw ARM Dev Bootstrap","image":"ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434"}'
```

5. create one active normal model and one active secure model through `/api/v1/admin/models`
6. verify `/api/v1/admin/models` returns both records before any validation-instance or direct QA claim

## Honest Stop Rule
- If `/api/v1/admin/models` is still empty, stop before validation-instance bring-up.
- Do not treat empty operator model state as a runtime failure.
- Do not claim fresh-replay readiness beyond app / gateway / admin ready when the operator model step has not been completed.
