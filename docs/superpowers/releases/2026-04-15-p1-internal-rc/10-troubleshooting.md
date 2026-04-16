# Troubleshooting

| Symptom | Check | Likely cause | Classification |
| --- | --- | --- | --- |
| Direct QA fails but runtime is healthy | `curl -sk "$CM_API/admin/models" -H "Authorization: Bearer $CM_ADMIN_TOKEN"` | missing active normal model, missing secure model, or bad provider credential | classify as model-bootstrap blocker, not runtime blocker |
| `skill-scanner` hits `ImagePullBackOff` immediately after repo-default deploy on Mac/ARM | `kubectl describe pod -n clawmanager-system -l app=skill-scanner` | `ghcr.io/yuan-lab-llm/skill-scanner:latest` has no `linux/arm64` manifest; use the documented local validation override path | classify as repo-default ARM image boundary, not `9001` or gateway regression |
| fresh replay reaches `/healthz` and admin login, but should not continue to validation-instance bring-up | `curl -sk "$CM_API/admin/models" -H "Authorization: Bearer $CM_ADMIN_TOKEN"` | operator has not yet created one active normal model and one active secure model | classify as documented operator prerequisite, not hidden DB reuse or unexplained startup failure |
