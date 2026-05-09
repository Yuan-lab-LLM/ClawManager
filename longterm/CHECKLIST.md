# Session Checklist

## Start-of-session
- [ ] Run `pwd` and confirm workspace path.
- [ ] Read `AGENTS.md` and `UnifiedFramework/15-Reusable-Minimum-Kernel-Starter-Template.md`.
- [ ] Read `app_spec.md`.
- [ ] Re-check migration constraints in `app_spec.md` (if attached to existing project).
- [ ] Read `feature_list.json`.
- [ ] Read `claude-progress.txt`.
- [ ] Read `git log --oneline -20`.
- [ ] Run `init.sh` and verify app/server starts.
- [ ] Re-test 1-2 already-passing core features.

## During session
- [ ] Work on one unblocked `passes: false` feature only.
- [ ] Test through end-to-end user flow.
- [ ] Capture concrete verification evidence (steps/results).
- [ ] If regression appears, fix regression first.

## End-of-session
- [ ] Confirm fresh acceptance evidence exists before any completion claim.
- [ ] Update only `passes` field for E2E-evidenced feature(s), and only when explicitly approved.
- [ ] Update `claude-progress.txt` only when explicitly approved for the current gate.
- [ ] Confirm no migration constraints were violated.
- [ ] Ensure no known critical breakage remains.
- [ ] Commit with descriptive message only when commit approval is explicit.

Boundary note:
- This checklist defines the minimum project-memory handshake for a session.
- It does not replace the feature-delivery authority (`spec规范`) or the runtime execution authority (`superpower skill`).
- `UnifiedFramework/ledger/` is a non-authoritative recovery surface, not acceptance evidence.
