# Acceptance Matrix

| Scope | Row / Gate | Minimal Success Path | Status | Evidence Path | Notes |
| --- | --- | --- | --- | --- | --- |
| Gate | Gate 1 | fresh deploy from empty cluster | BLOCKED | `06-evidence-index.md` | fresh replay from zero is now evidenced, but repo-default Mac/ARM deploy still stops at `skill-scanner` image pull because the official image has no `linux/arm64` manifest |
| Gate | Gate 2 | runtime online + direct QA | BLOCKED | `06-evidence-index.md` | fresh replay reaches `app` / `gateway` / `admin` ready after the documented live-only ARM overrides, then stops honestly at operator model bootstrap before validation-instance or direct QA |
| Gate | Gate 3 | all frozen U3 rows validated | BLOCKED | `06-evidence-index.md` | Source A is frozen at `0` rows and Source B still surfaces `0` rows even after a real agent inventory report on instance `6` |
| Gate | Gate 4 | runbook + quick-start + troubleshooting verified | BLOCKED | `06-evidence-index.md` | operator docs are now aligned to the honest Mac/ARM stopping point, but the packet remains blocked by Gate 1 / Gate 2 / Gate 3 and therefore cannot claim an end-to-end validated operator flow |
| Gate | Gate 5 | evidence index + gate record + checklist complete | BLOCKED | `06-evidence-index.md` | closure artifacts are now substantially in place, but the packet remains a blocked internal RC rather than a PASS release packet |
