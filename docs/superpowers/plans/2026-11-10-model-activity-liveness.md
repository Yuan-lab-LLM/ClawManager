# Model activity liveness implementation

Continue in the existing clean isolated worktree. The user approved implementation and staging deployment; commit/push to `preview`, no PR.

1. Repository and migration: add `109_add_model_activity_leases.sql` with nullable lease expiry and heartbeat plus completion reason. Extend the existing disposable SQL fixture. Write failing real SQL tests for expired scopes, healthy renewal, late renewal/finish, stale generation and legacy retention/reconciliation. Implement lease admission, renewal, expiry recovery, and fresh-only active counts under instance-row locking. Cleanup must not refresh the idle clock.
2. Gateway: update `ModelActivityRecorder` and propagate its cancelable lease context through `model_activity.go`, both chat entry points and image execution. Add fake-clock tests with `testing/synctest` for renewal, retry, watchdog cancellation, request cancellation and idempotent completion. Retain outer HTTP scope deduplication and quota ordering.
3. Controller: add focused legacy-rollout proof tests using fake Kubernetes Pods, including old terminating Pods, multiple replicas, identity mismatch and unavailable proof. Reconcile before due candidates; wire application identity from the existing namespace and application Pod. Keep explicit active/legacy blockers and preserve manual/automatic stop fencing.
4. Verify and release: run focused model/repository/gateway/handler/controller regressions and migration checks, then a focused independent review. Build and deploy only the CM application using the existing staging packaging process. Observe expired legacy request recovery and normal idle-stop completion for instance 118. Preserve persistent workspace, Git and TLS identities. Record acceptance, remove disposable SQL/build artifacts, and synchronize `preview`.

Commands (repository backend, explicit Go toolchain):

```sh
GOROOT=/usr/local/go /usr/local/go/bin/go test ./internal/models ./internal/aigateway ./internal/repository -run 'TestModelActivity|TestAutoStop' -count=1
GOROOT=/usr/local/go /usr/local/go/bin/go test -overlay=/tmp/cm-runtime-recovery-tests/overlay.json ./internal/services ./internal/handlers -run 'TestModelActivity|TestAutoStop|TestIdle|TestGatewayActivity' -count=1
```

Enable real repository SQL tests with `TEST_IDLE_MYSQL_PORT` pointing at an isolated local MariaDB/MySQL fixture; never use the application database as a test fixture. The existing Darwin overlay excludes the unrelated Linux-only NFS test compilation issue.
