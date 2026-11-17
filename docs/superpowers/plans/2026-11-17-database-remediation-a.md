# Database remediation batch A implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver batch A of the approved database remediation design: correct fees and pagination, bound Redis reads, and make event-consumption failures recoverable.

**Architecture:** Keep existing API response structures and repository constructors. Add SQL-backed reporting methods to concrete repositories, with optional capabilities for existing test doubles; replace production full-history loading. Keep reliable Team history intact while fixing read and cursor handling. Later batches implement persistent logging/body storage, retention and monthly archives, then reliable Redis retention.

**Tech Stack:** Go 1.26.1, MySQL 8.4, Redis 7, upper/db, existing Kubernetes test cluster.

## Global Constraints

- Work in the user-created `database` branch; no additional checkout or worktree is needed.
- 本地仅源码/文档编辑、允许的无产物类型检查和浏览器验证；测试、二进制、前端生产包与镜像构建安排在 10.130.81.54。
- No deployment, existing data deletion, Runtime image change, or modification of operational safeguards.
- Preserve budget/settlement/identity and storage boundaries. Correct display accounting without rewriting settled ledger facts.
- Tests must fail for the intended regression before production changes, then pass on server54. Test MySQL/Redis resources must be disposable and isolated from application data.
- Keep compatibility for current API consumers and test doubles. Do not silently truncate historical data or long bodies.
- Review each task and the combined changes; report unimplemented design requirements explicitly.

## Task 1: Redis response and batch limits (E5)

**Files:** Modify `backend/internal/services/team_redis.go`; add `backend/internal/services/team_redis_budget_test.go`.

**Interfaces:** Preserve `readRedisReply(*bufio.Reader) (interface{}, error)` and `redisBus.XRead`. Add private parsing helpers only if needed.

- [ ] Add failing tests using local-in-test-process TCP listeners and in-memory RESP payloads: XREAD includes COUNT; reject oversized bulk, aggregate response, excess elements/depth, malformed negative lengths, incorrect CRLF; close rejected pooled connections and succeed on a fresh connection.
- [ ] Run the new tests on server54 before implementation; retain RED output.
- [ ] Implement COUNT 100, a shared whole-response byte budget (8 MiB), element and recursion limits, and bounded line reads. Validate lengths before allocation and account for nested headers and payloads. Preserve nil handling. Parsing failures must leave pooled connection non-reusable.

```go
// Apply the budget once per top-level response, not once per nested value.
// XREAD argument shape:
args := []string{"XREAD", "COUNT", "100", "BLOCK", strconv.Itoa(blockMillis), "STREAMS", key, lastID}
```

- [ ] Run focused Redis and existing managed-Redis tests on server54; report commands and RED/GREEN evidence.
- [ ] Review existing Runtime notification retention and reliable Team streams remain unchanged.

## Task 2: Durable Team event handoff and cursor errors (E3)

**Files:** Modify `backend/internal/services/team_service.go`, `team_execution_recovery.go`, and focused recovery tests. Add repository helpers/tests if a narrow durable handoff requires them.

**Interfaces:** Preserve `projectConsumedTeamEvent(... ) bool`. Return false if the cursor cannot be durably committed; do not mutate the caller's team cursor before success. Keep existing outbox recovery lane and original stream identity.

- [ ] Write a regression in which projection/replay intent succeeds but `UpdateTeam` fails; assert false and original in-memory cursor. Add duplicate/restart and outbox-failure cases.
- [ ] Run RED on server54.
- [ ] Commit a copy of the team and only update the in-memory cursor after repository success:

```go
next := *team
next.RedisEventsLastID = message.ID
next.UpdatedAt = time.Now().UTC()
if err := s.repo.UpdateTeam(&next); err != nil {
    return false
}
team.RedisEventsLastID = next.RedisEventsLastID
team.UpdatedAt = next.UpdatedAt
return true
```

- [ ] Trace projection deduplication and partial writes. Before performing business projection, persist a stable replay intent if existing non-atomic paths cannot guarantee recovery. On failure retain this intent; resolve it only after successful idempotent projection. No Redis trimming is enabled in this task. Clearly document any remaining transactional projection limitation.
- [ ] Run focused Team recovery and backup-gate tests, then self-review failure and restart behavior.

## Task 3: Correct fee aggregation and narrow reporting reads (E1)

**Files:** `backend/internal/repository/cost_record_repository.go`, `model_invocation_repository.go`; `backend/internal/services/ai_observability_service.go`; new MySQL and service regression tests.

- [ ] Add a disposable-MySQL fixture with two invocations sharing a trace, failed/successful attempts and multiple fee records; assert aggregate equals the sum of eligible fee records once. Add service regression for model cost breakdown with repeated traces and different models.
- [ ] Run RED remotely.
- [ ] Replace multiplicative JOIN with an EXISTS predicate or unique invocation association. Preserve time and instance/session filtering. Exact invocation IDs take precedence; legacy rows without IDs require bounded, explicit compatibility semantics.

```sql
SELECT cr.session_id, SUM(cr.estimated_cost)
FROM cost_records cr
WHERE cr.instance_id = ? AND EXISTS (
  SELECT 1 FROM model_invocations mi
  WHERE mi.trace_id = cr.trace_id AND mi.instance_id = cr.instance_id
    AND mi.status <> 'blocked'
)
GROUP BY cr.session_id
```

- [ ] In detail views count each fee once, attribute it using its own model identity, and retain token/call semantics. Do not add the entire trace fee for each invocation.
- [ ] Remove request/response payloads from reporting-specific invocation projections; preserve detail methods that require bodies.
- [ ] Verify RED/GREEN and relevant existing session/cost tests on server54.

## Task 4: SQL-backed audit/session/admin paging (E2 and query performance)

**Files:** New focused reporting repository/service files; wire through existing concrete repository capabilities; existing reporting tests and new disposable-MySQL integration tests. Migrations only for evidenced useful indexes.

- [ ] Add test data where a matching audit row lies beyond 1,000 newest rows. Check accurate total and page, trace grouping, event-only fallback, stable ordering and status/model/search combinations.
- [ ] Add session list tests where only the page is materialized and admin overview is batched rather than one aggregate per instance. Preserve global summary versus filtered page semantics and managed-runtime scope.
- [ ] Run RED remotely.
- [ ] Query SQL for total and current page with stable timestamp/ID ordering, filter before LIMIT, and load only page metadata. Keep existing API response shape. Avoid SQL string interpolation of user values.

```sql
-- Values are parameters; deterministic ordering is required.
ORDER BY last_activity_at DESC, session_id DESC
LIMIT ? OFFSET ?
```

- [ ] Add bounded context/query timeouts. For existing numbered-page APIs retain accurate offset behavior in this compatible batch; stable cursor interfaces and incrementally maintained summaries must be tracked as follow-on implementation, not claimed complete merely because Go memory is bounded.
- [ ] Check actual MySQL execution plans and ordinary/empty/search/deep-page behavior with isolated data. No fabricated P95 or 10,000-instance capacity claims.
- [ ] Run reporting suites and relevant server handler tests on server54.

## Task 5: Review, evidence and next batches

- [ ] Run `git diff --check` locally. Run Go package tests and focused race checks on server54; record exact commands, counts, failures and skips.
- [ ] Independently review E1/E2/E3/E5 and integration behavior, fix important findings and rerun only affected checks.
- [ ] Update the design status and add a report under `docs/superpowers/tests/` containing implemented behavior, remaining scope, cluster evidence and rollout requirements.
- [ ] Continue with separate plans for B (E4/E6, body storage and safe maintenance), C (archival/monthly layout and recovery), and D (reliable Redis retention and capacity). Do not mark the full design complete after batch A.

## Progress

- [x] User approved implementation of the design, including discovered correctness issues.
- [x] Local branch and server54 access inspected; existing Go toolchain/cache located.
- [ ] Task 1.
- [ ] Task 2.
- [ ] Task 3.
- [ ] Task 4.
- [ ] Task 5.
