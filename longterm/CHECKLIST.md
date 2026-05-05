# Session Checklist

## Start-of-session
- [ ] Run `pwd` and confirm workspace path.
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
- [ ] Update only `passes` field for E2E-evidenced feature(s).
- [ ] Update `claude-progress.txt` (done, issues, next feature).
- [ ] Confirm no migration constraints were violated.
- [ ] Ensure no known critical breakage remains.
- [ ] Commit with descriptive message.

Boundary note:
- This checklist defines the minimum project-memory handshake for a session.
- It does not replace the feature-delivery authority (`spec规范`) or the runtime execution authority (`superpower skill`).
