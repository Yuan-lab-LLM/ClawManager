# Smart Model Routing — Design Proposal

> **PR**: #96  
> **Issue**: #68 (Problems 1, 2, 3)  
> **Author**: hippoley  
> **Date**: 2026-04-28  
> **Status**: In Review

---

## 1. Problem Statement

The current LLM Gateway has three limitations identified in Issue #68:

| # | Problem | Impact |
|---|---------|--------|
| 1 | `ListAvailableModels()` returns only a hardcoded "Auto" entry | Users cannot see or select specific models |
| 2 | Auto-selection always picks the first non-secure model | No priority control, no load distribution |
| 3 | A single provider failure returns an error immediately | No resilience — one provider down = all requests fail |

### Out of Scope (deferred to future PRs)

Problems 4–8 from Issue #68 (rate limiting, cost tracking, per-user quotas, model-level circuit breakers, and request queuing) are **not** addressed in this PR.

---

## 2. Design Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        AI Gateway Service                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────┐    ┌──────────────────┐    ┌───────────────┐  │
│  │ ListModels   │    │ prepareChatReq   │    │ resolveModel  │  │
│  │ (Auto + all) │    │ (auth + resolve) │    │ (auto/named)  │  │
│  └──────────────┘    └────────┬─────────┘    └───────────────┘  │
│                               │                                  │
│                    ┌──────────▼──────────┐                       │
│                    │  selectAutoModel()  │                       │
│                    │  Priority + Random  │                       │
│                    └──────────┬──────────┘                       │
│                               │                                  │
│              ┌────────────────┼────────────────┐                 │
│              ▼                                 ▼                  │
│   ┌──────────────────┐              ┌──────────────────┐        │
│   │  dispatchCall()  │              │ dispatchStream() │        │
│   │  (non-streaming) │              │   (streaming)    │        │
│   └────────┬─────────┘              └────────┬─────────┘        │
│            │                                  │                   │
│            ▼                                  ▼                   │
│   ┌──────────────────┐              ┌──────────────────┐        │
│   │callWithFallback()│              │streamWithFallback│        │
│   │ 5xx / conn error │              │ conn error only  │        │
│   │ max 2 retries    │              │ max 2 retries    │        │
│   └──────────────────┘              └──────────────────┘        │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. Detailed Design

### 3.1 Model Listing (Problem 1)

**Before**: `ListAvailableModels()` returned a single hardcoded `Auto` entry regardless of how many models were configured.

**After**: Returns `Auto` as the first entry, followed by **all active models** from the database. This allows the frontend to display a model picker where users can either let the gateway decide ("Auto") or pin a specific model.

```go
func (s *service) ListAvailableModels() ([]AvailableModel, error) {
    items, _ := s.modelRepo.ListActive()
    if len(items) == 0 { return []AvailableModel{}, nil }

    result := []AvailableModel{ {DisplayName: "Auto", Provider: "gateway"} }
    for _, item := range items {
        result = append(result, AvailableModel{...})
    }
    return result, nil
}
```

**Design decision**: When no active models exist, we return an empty list (not even "Auto") because Auto with zero backends is meaningless.

---

### 3.2 Priority-Based Selection with Load Balancing (Problem 2)

#### Schema Change

```sql
ALTER TABLE llm_models
  ADD COLUMN priority INT NOT NULL DEFAULT 0 AFTER is_active;
```

- Higher value = higher priority (e.g., `priority=100` is preferred over `priority=10`)
- Default `0` ensures backward compatibility — existing models start equal
- Migration is idempotent: guarded by `duplicate column name` error check

#### Selection Algorithm

```
selectAutoModelExcluding(excludeIDs):
  1. Load all active models (already sorted by -priority, -is_secure, display_name)
  2. Filter out secure models → candidates
  3. Filter out excluded IDs (used by fallback)
  4. If no non-secure candidates remain → include secure models as fallback
  5. Collect highest-priority group (all models sharing max priority value)
  6. Random pick among the top group → simple load balancing
```

**Why random instead of round-robin?**
- Stateless: no shared counter needed across gateway replicas
- Sufficient for our scale (2–5 models)
- Avoids coordination overhead in multi-replica deployments

**Why filter secure models?**
- Secure models are reserved for compliance-sensitive workloads
- Auto-routing should prefer non-secure (general-purpose) models
- Secure models are only used as last resort when all non-secure models are excluded/down

---

### 3.3 Provider Fallback (Problem 3)

#### 3.3.1 Non-Streaming Path

```
ChatCompletions(req):
  1. prepareChatRequest() → resolve model, auth, build prepared request
  2. dispatchCall(prepared) → route to provider (OpenAI/Anthropic/etc.)
  3. If response is 5xx:
     → callWithFallback(prepared)
       - Pick next model via selectAutoModelExcluding({failed_model_id})
       - Record audit event (gateway.request.fallback)
       - Retry dispatchCall() with new model
       - Max 2 retries, accumulating excluded IDs
  4. Return first successful response, or original error if all exhausted
```

**Trigger conditions for non-streaming fallback:**
- HTTP 5xx from provider (server error)
- Connection-level failure (timeout, refused, DNS failure)

**Non-trigger conditions (no fallback):**
- HTTP 4xx (client error — bad request, auth failure, rate limit)
- Successful response (any 2xx)

#### 3.3.2 Streaming Path

Streaming fallback is **narrower** than non-streaming because of HTTP's write-once semantics:

```
StreamChatCompletions(req, w):
  1. prepareChatRequest()
  2. dispatchStream(prepared, w)
  3. If error is *errProviderConnection* (connection failed BEFORE headers written):
     → streamWithFallback(prepared, w)
       - Same logic as callWithFallback but for streaming
       - Each retry calls dispatchStream() with new model
       - If error is NOT errProviderConnection → stop (headers already sent)
  4. If error is anything else → return error (cannot retry)
```

**Key insight**: Once HTTP response headers are written to the client (status code sent), we cannot "take back" the response and try a different model. The `errProviderConnection` sentinel type ensures we only retry when the failure happened at the TCP/TLS layer, before any bytes were sent to the client.

#### 3.3.3 The `errProviderConnection` Sentinel

```go
type errProviderConnection struct {
    wrapped error
}
func (e *errProviderConnection) Error() string { return e.wrapped.Error() }
func (e *errProviderConnection) Unwrap() error { return e.wrapped }
```

This error is returned by `streamOpenAICompatible()` and `streamAnthropic()` when `httpClient.Do()` fails (i.e., the HTTP request never reached the provider or the connection was refused/timed out).

It is **not** returned when:
- The provider returns a non-2xx status (headers already written in streaming)
- The stream breaks mid-transfer (partial data already sent to client)

#### 3.3.4 Audit Trail

Every fallback attempt records an audit event:

```go
AuditEvent{
    EventType:    "gateway.request.fallback",
    TrafficClass: models.TrafficClassLLM,
    Severity:     models.AuditSeverityWarn,
    Message:      "Falling back from model X to model Y (attempt N)",
}
```

This provides full observability into fallback behavior for debugging and capacity planning.

---

## 4. Data Flow Diagrams

### 4.1 Non-Streaming Request Flow

```
Client → ChatCompletions()
           │
           ├─ prepareChatRequest()
           │    ├─ resolveRequestedModel("auto" or "model-name")
           │    ├─ selectAutoModel() [if auto]
           │    └─ build preparedChatRequest struct
           │
           ├─ dispatchCall(prepared)
           │    ├─ OpenAI path → callOpenAICompatible()
           │    └─ Anthropic path → callAnthropic()
           │
           ├─ [if 5xx] callWithFallback(prepared)
           │    ├─ selectAutoModelExcluding({model_1})
           │    ├─ audit: gateway.request.fallback
           │    ├─ dispatchCall(prepared) with model_2
           │    ├─ [if still 5xx] selectAutoModelExcluding({model_1, model_2})
           │    ├─ audit: gateway.request.fallback
           │    └─ dispatchCall(prepared) with model_3
           │
           └─ Return response to client
```

### 4.2 Streaming Request Flow

```
Client → StreamChatCompletions()
           │
           ├─ prepareChatRequest()
           │
           ├─ dispatchStream(prepared, w)
           │    ├─ httpClient.Do() fails → return errProviderConnection
           │    └─ httpClient.Do() succeeds → stream to client (no retry possible)
           │
           ├─ [if errProviderConnection] streamWithFallback(prepared, w)
           │    ├─ selectAutoModelExcluding({model_1})
           │    ├─ audit: gateway.request.fallback
           │    ├─ dispatchStream(prepared, w) with model_2
           │    │    ├─ errProviderConnection → continue loop
           │    │    └─ other error or success → return
           │    └─ [max 2 retries]
           │
           └─ Return to client
```

---

## 5. Schema Changes

| Table | Column | Type | Default | Description |
|-------|--------|------|---------|-------------|
| `llm_models` | `priority` | `INT NOT NULL` | `0` | Higher = preferred for auto-selection |

**Migration strategy**: Inline `ALTER TABLE` in repository `EnsureSchema()`, guarded by duplicate-column-name check. This is idempotent and safe for repeated application.

**Ordering impact**: Repository `List()` and `ListActive()` now sort by `-priority, -is_secure, display_name` (previously `-is_secure, display_name`).

---

## 6. Configuration & Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `maxFallbackRetries` | `2` | Maximum number of alternate models to try |
| `autoModelID` | `"auto"` | Reserved model identifier for auto-routing |

No new environment variables or config files are introduced. The priority is managed per-model via the existing admin API.

---

## 7. Error Handling Strategy

| Scenario | Behavior |
|----------|----------|
| Provider returns 2xx | Success, return response |
| Provider returns 4xx | Return error to client (no retry — client's fault) |
| Provider returns 5xx (non-streaming) | Trigger fallback with alternate model |
| Provider connection fails (non-streaming) | Trigger fallback with alternate model |
| Provider connection fails (streaming, pre-header) | Trigger streaming fallback |
| Provider returns 5xx (streaming, headers sent) | Return error (cannot retry) |
| Stream breaks mid-transfer | Return error (partial data already sent) |
| All fallback models exhausted | Return original error to client |
| No active models configured | Return "no active models" error |

---

## 8. Test Coverage

9 new unit tests added in `service_test.go`:

| Test | Validates |
|------|-----------|
| `TestListAvailableModelsReturnsAutoAndAllActive` | Auto + all active models returned |
| `TestListAvailableModelsEmptyWhenNoActive` | Empty list when no models active |
| `TestSelectAutoModelPicksHighestPriority` | Priority-based selection works |
| `TestSelectAutoModelSkipsSecure` | Secure models excluded from auto |
| `TestSelectAutoModelFallsBackToSecureWhenNoNonSecure` | Secure fallback when no alternatives |
| `TestSelectAutoModelDistributesAmongSamePriority` | Random load balancing verified (300 iterations) |
| `TestSelectAutoModelExcludingSkipsExcluded` | Exclusion set works for fallback |
| `TestSelectAutoModelExcludingAllReturnsError` | Error when all models excluded |
| `TestErrProviderConnectionIsDetectable` | Sentinel error type works with `errors.As` |

---

## 9. Security Considerations

- **No new attack surface**: Fallback logic reuses existing auth and governance checks
- **Secure model isolation**: Auto-routing prefers non-secure models; secure models only used as last resort
- **Audit trail**: All fallback events are recorded for compliance review
- **No credential leakage**: Each model's API key is resolved independently via existing `resolveAPIKey()` path

---

## 10. Performance Impact

- **Latency (happy path)**: Zero additional overhead — same single provider call
- **Latency (fallback)**: Additional round-trip per retry (max 2), but only on failure
- **Memory**: Negligible — small `excludeIDs` map (max 3 entries)
- **Database**: No additional queries during fallback — model list is already loaded in `selectAutoModelExcluding()`

---

## 11. Future Work (Issue #68, Problems 4–8)

| Problem | Description | Planned Approach |
|---------|-------------|-----------------|
| 4 | Per-model rate limiting | Token bucket per model ID |
| 5 | Cost tracking & budgets | Accumulate token usage per request |
| 6 | Per-user quotas | User-level daily/monthly limits |
| 7 | Model-level circuit breaker | Track failure rate, auto-disable unhealthy models |
| 8 | Request queuing | Queue when all models at capacity |

These will be addressed in subsequent PRs to keep each change focused and reviewable.

---

## 12. Summary

This PR transforms the LLM Gateway from a single-model passthrough into a resilient, priority-aware routing layer:

1. **Visibility**: Users can now see and select from all available models
2. **Control**: Operators set priority to influence routing without code changes
3. **Resilience**: Automatic failover to alternate providers on failure
4. **Observability**: Full audit trail of routing decisions and fallbacks
5. **Safety**: Streaming fallback is conservative — only retries when safe to do so

