package services

// The pinned Desktop renderer speaks the existing Hermes REST/JSON-RPC APIs.
// These adapters project one CM-owned Runtime, never enumerate host profiles or
// expose Electron's machine-wide capabilities.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var hermesDesktopSources = regexp.MustCompile(`^[A-Za-z0-9_-]+(,[A-Za-z0-9_-]+)*$`)
var hermesDesktopModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./:@+-]{0,255}$`)
var hermesDesktopModelSwitch = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_./:@+-]{0,255}) --provider ([A-Za-z0-9][A-Za-z0-9_./:@+-]{0,255}) --session$`)
var errHermesDesktopUnboundSession = errors.New("session not found; resume required")

func hermesDesktopPick(object map[string]json.RawMessage, keys string) map[string]json.RawMessage {
	result := map[string]json.RawMessage{}
	for _, key := range strings.Fields(keys) {
		if value, ok := object[key]; ok {
			result[key] = value
		}
	}
	return result
}

func hermesDesktopPickScalars(object map[string]json.RawMessage, keys string) map[string]json.RawMessage {
	result := hermesDesktopPick(object, keys)
	for key, raw := range result {
		var value any
		if json.Unmarshal(raw, &value) != nil {
			delete(result, key)
			continue
		}
		switch value.(type) {
		case nil, string, bool, float64:
		default:
			delete(result, key)
		}
	}
	return result
}

func hermesDesktopSessionRow(raw json.RawMessage) (json.RawMessage, error) {
	var row map[string]json.RawMessage
	if json.Unmarshal(raw, &row) != nil || row == nil {
		return nil, ErrHermesDesktopUpstream
	}
	var id string
	if json.Unmarshal(row["id"], &id) != nil || !hermesDesktopSessionID.MatchString(id) {
		return nil, ErrHermesDesktopUpstream
	}
	safe := hermesDesktopPickScalars(row, "id title preview source model started_at ended_at last_active message_count input_tokens output_tokens tool_call_count is_active archived pinned unread actual_cost_usd estimated_cost_usd parent_session_id _lineage_root_id handoff_platform handoff_state")
	// This is a CM routing label, not a claim about the Runtime home basename.
	safe["profile"] = json.RawMessage(`"default"`)
	safe["is_default_profile"] = json.RawMessage("true")
	return json.Marshal(safe)
}

func hermesDesktopProjectHTTP(path string, body []byte) ([]byte, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || object == nil {
		return nil, ErrHermesDesktopUpstream
	}
	var safe map[string]json.RawMessage
	switch {
	case path == "/status":
		safe = hermesDesktopPickScalars(object, "version model provider active_sessions gateway_state")
	case path == "/config" || path == "/config/defaults":
		// This Runtime Pod is isolated to the owning CM instance. Preserve the
		// complete shape so every schema-backed field can load and save. The
		// response sanitizer below still removes credentials and secret values.
		safe = object
	case path == "/config/schema":
		var fields map[string]map[string]json.RawMessage
		if json.Unmarshal(object["fields"], &fields) != nil || fields == nil {
			return nil, ErrHermesDesktopUpstream
		}
		projected := make(map[string]map[string]json.RawMessage, len(fields))
		for key, field := range fields {
			if key == "" || len(key) > 256 {
				continue
			}
			entry := hermesDesktopPick(field, "category description options searchable clearable type")
			var category, description, fieldType string
			if raw := entry["category"]; len(raw) > 0 && (json.Unmarshal(raw, &category) != nil || len(category) > 128) {
				delete(entry, "category")
			}
			if raw := entry["description"]; len(raw) > 0 && (json.Unmarshal(raw, &description) != nil || len(description) > 4096) {
				delete(entry, "description")
			}
			if raw := entry["type"]; len(raw) > 0 && (json.Unmarshal(raw, &fieldType) != nil || !strings.Contains(" boolean list number select string text ", " "+fieldType+" ")) {
				delete(entry, "type")
			}
			for _, booleanKey := range []string{"searchable", "clearable"} {
				var value bool
				if raw := entry[booleanKey]; len(raw) > 0 && json.Unmarshal(raw, &value) != nil {
					delete(entry, booleanKey)
				}
			}
			if raw := entry["options"]; len(raw) > 0 {
				var options []any
				if json.Unmarshal(raw, &options) != nil || len(options) > 10000 {
					delete(entry, "options")
				} else {
					valid := true
					for _, option := range options {
						switch option.(type) {
						case nil, string, bool, float64:
						default:
							valid = false
						}
					}
					if !valid {
						delete(entry, "options")
					}
				}
			}
			projected[key] = entry
		}
		safe = map[string]json.RawMessage{}
		safe["fields"], _ = json.Marshal(projected)
		var order []string
		if json.Unmarshal(object["category_order"], &order) == nil && len(order) <= 256 {
			safe["category_order"], _ = json.Marshal(order)
		}
	case path == "/model/info":
		safe = hermesDesktopPickScalars(object, "model provider auto_context_length config_context_length effective_context_length")
	case path == "/profiles":
		var profiles []map[string]json.RawMessage
		if json.Unmarshal(object["profiles"], &profiles) != nil {
			return nil, ErrHermesDesktopUpstream
		}
		for _, profile := range profiles {
			var isDefault bool
			_ = json.Unmarshal(profile["is_default"], &isDefault)
			if !isDefault {
				continue
			}
			row := hermesDesktopPickScalars(profile, "display_name has_env is_default model name provider skill_count")
			row["name"] = json.RawMessage(`"default"`)
			row["path"] = json.RawMessage(`""`)
			safe = map[string]json.RawMessage{}
			safe["profiles"], _ = json.Marshal([]map[string]json.RawMessage{row})
			break
		}
		if safe == nil {
			safe = map[string]json.RawMessage{"profiles": json.RawMessage(`[]`)}
		}
	case path == "/model/options":
		safe = hermesDesktopPickScalars(object, "model provider")
		var providers []map[string]json.RawMessage
		if json.Unmarshal(object["providers"], &providers) != nil {
			return nil, ErrHermesDesktopUpstream
		}
		rows := []map[string]json.RawMessage{}
		for _, provider := range providers {
			var slug string
			_ = json.Unmarshal(provider["slug"], &slug)
			if !hermesDesktopModelName.MatchString(slug) {
				continue
			}
			row := hermesDesktopPickScalars(provider, "slug name is_current authenticated total_models is_user_defined warning auth_type key_env api_url free_tier")
			for _, key := range []string{"models", "featured_models", "unavailable_models"} {
				var names []string
				if json.Unmarshal(provider[key], &names) == nil && names != nil {
					row[key], _ = json.Marshal(names)
				}
			}
			if pricing := provider["pricing"]; len(pricing) > 0 {
				row["pricing"] = pricing
			}
			var aliases []string
			if json.Unmarshal(provider["aliases"], &aliases) == nil {
				valid := []string{}
				for _, alias := range aliases {
					if hermesDesktopModelName.MatchString(alias) && len(valid) < 32 {
						valid = append(valid, alias)
					}
				}
				row["aliases"], _ = json.Marshal(valid)
			}
			var capabilities map[string]map[string]json.RawMessage
			var names []string
			if json.Unmarshal(provider["capabilities"], &capabilities) == nil && json.Unmarshal(provider["models"], &names) == nil {
				projected := map[string]map[string]bool{}
				for _, name := range names {
					if !hermesDesktopModelName.MatchString(name) || len(projected) >= 10000 {
						continue
					}
					flags := map[string]bool{}
					for _, field := range []string{"fast", "reasoning", "can_disable_reasoning"} {
						var value bool
						if raw := capabilities[name][field]; len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &value) == nil {
							flags[field] = value
						}
					}
					if len(flags) > 0 {
						projected[name] = flags
					}
				}
				row["capabilities"], _ = json.Marshal(projected)
			}
			rows = append(rows, row)
		}
		safe["providers"], _ = json.Marshal(rows)
	case path == "/sessions":
		safe = hermesDesktopPickScalars(object, "total limit offset")
		var rows []json.RawMessage
		if json.Unmarshal(object["sessions"], &rows) != nil {
			return nil, ErrHermesDesktopUpstream
		}
		projected := make([]json.RawMessage, 0, len(rows))
		for _, row := range rows {
			value, err := hermesDesktopSessionRow(row)
			if err != nil {
				return nil, err
			}
			projected = append(projected, value)
		}
		safe["sessions"], _ = json.Marshal(projected)
	case strings.HasSuffix(path, "/messages"):
		safe = hermesDesktopPickScalars(object, "session_id")
		var pagination map[string]json.RawMessage
		if json.Unmarshal(object["pagination"], &pagination) == nil && pagination != nil {
			safe["pagination"], _ = json.Marshal(hermesDesktopPickScalars(pagination, "limit offset order returned"))
		}
		var messages []map[string]json.RawMessage
		if json.Unmarshal(object["messages"], &messages) != nil {
			return nil, ErrHermesDesktopUpstream
		}
		rows := make([]map[string]json.RawMessage, 0, len(messages))
		for _, message := range messages {
			var role string
			_ = json.Unmarshal(message["role"], &role)
			if role == "system" {
				// The stock renderer advances pagination by messages.length.
				// Keep the physical row count without exposing a system prompt.
				hidden := hermesDesktopPickScalars(message, "id row_id timestamp")
				hidden["role"], hidden["content"], hidden["display_kind"] = json.RawMessage(`"system"`), json.RawMessage(`""`), json.RawMessage(`"hidden"`)
				rows = append(rows, hidden)
				continue
			}
			rows = append(rows, hermesDesktopPick(message, "id row_id role content display_content text timestamp tool_call_id tool_calls tool_name name args context reasoning reasoning_content reasoning_details codex_reasoning_items display_kind display_metadata"))
		}
		safe["messages"], _ = json.Marshal(rows)
	default:
		return hermesDesktopSessionRow(body)
	}
	return json.Marshal(safe)
}

func (s *HermesDesktopService) desktopRead(ctx context.Context, target *hermesDesktopTarget, path string, query url.Values) ([]byte, error) {
	q := url.Values{}
	for key, values := range query {
		if key != "profile" {
			q[key] = append([]string(nil), values...)
		}
	}
	if path == "/model/options" {
		q.Set("explicit_only", "1")
	}
	body, _, cookies, err := s.upstreamRequest(ctx, target, http.MethodGet, "/api"+path, q.Encode())
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	sessionProjection := len(parts) >= 2 && len(parts) <= 3 && parts[0] == "sessions" && hermesDesktopSessionID.MatchString(parts[1]) && (len(parts) == 2 || parts[2] == "messages")
	project := path == "/status" || path == "/config" || path == "/config/defaults" || path == "/config/schema" || path == "/model/info" || path == "/model/options" || path == "/profiles" || path == "/sessions" || sessionProjection
	if project {
		body, err = hermesDesktopProjectHTTP(path, body)
		if err != nil {
			return nil, err
		}
	}
	if path == "/config/schema" || path == "/config" || path == "/config/defaults" {
		secrets := []string{*target.instance.AccessToken}
		for _, cookie := range cookies {
			secrets = append(secrets, cookie.Value)
		}
		return hermesDesktopRedactStringValues(body, secrets...)
	}
	if !project {
		secrets := []string{*target.instance.AccessToken}
		for _, cookie := range cookies {
			secrets = append(secrets, cookie.Value)
		}
		return hermesDesktopRedactStringValues(body, secrets...)
	}
	return hermesDesktopSanitize(body, *target.instance.AccessToken, cookies)
}

// Schema keys describe configuration fields and must remain intact even when
// their names contain words such as token or key. Only secret values are redacted.
func hermesDesktopRedactStringValues(body []byte, secrets ...string) ([]byte, error) {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return nil, ErrHermesDesktopUpstream
	}
	var clean func(any) any
	clean = func(v any) any {
		switch item := v.(type) {
		case map[string]any:
			for key, child := range item {
				item[key] = clean(child)
			}
			return item
		case []any:
			for i := range item {
				item[i] = clean(item[i])
			}
			return item
		case string:
			for _, secret := range secrets {
				if secret != "" {
					item = strings.ReplaceAll(item, secret, "[redacted]")
				}
			}
			return item
		default:
			return v
		}
	}
	return json.Marshal(clean(value))
}

func (s *HermesDesktopService) desktopSidebar(ctx context.Context, target *hermesDesktopTarget, query url.Values) ([]byte, error) {
	result := map[string]any{}
	for _, section := range []string{"recents", "cron", "messaging"} {
		limit := query.Get(section + "_limit")
		if limit == "" {
			limit = "40"
		}
		q := url.Values{"limit": {limit}, "offset": {"0"}, "order": {"recent"}, "archived": {"exclude"}, "min_messages": {"1"}}
		if section == "cron" {
			q.Set("source", "cron")
		} else if exclude := query.Get(section + "_exclude"); exclude != "" {
			q.Set("exclude_sources", exclude)
		}
		body, err := s.desktopRead(ctx, target, "/sessions", q)
		if err != nil {
			return nil, err // Never turn unavailable real history into fake empty success.
		}
		var page struct {
			Sessions []json.RawMessage `json:"sessions"`
			Total    int               `json:"total"`
		}
		if json.Unmarshal(body, &page) != nil {
			return nil, ErrHermesDesktopUpstream
		}
		n, _ := strconv.Atoi(limit)
		result[section] = map[string]any{"sessions": page.Sessions, "profiles_truncated": map[string]bool{"default": page.Total > n}}
	}
	return json.Marshal(result)
}

type hermesDesktopCatalogEntry struct {
	expires   time.Time
	choices   map[string]bool
	providers map[string]bool
}

func (s *HermesDesktopService) desktopModelAllowed(ctx context.Context, target *hermesDesktopTarget, provider, model string) bool {
	entry, ok := s.desktopCatalog(ctx, target)
	return ok && entry.choices[provider+"\x00"+model]
}

func (s *HermesDesktopService) desktopCatalog(ctx context.Context, target *hermesDesktopTarget) (hermesDesktopCatalogEntry, bool) {
	// Cache is per instance, generation, endpoint AND managed credential hash,
	// not per browser-supplied profile. No external/unconfigured provider wins.
	key := target.authKey()
	cacheKey := hermesGatewayCacheKey{allocation: key, target: target.url.String(), credential: sha256.Sum256([]byte(*target.instance.AccessToken))}
	s.mu.Lock()
	entry := s.catalog[cacheKey]
	s.mu.Unlock()
	if time.Now().Before(entry.expires) {
		return entry, true
	}
	body, err := s.desktopRead(ctx, target, "/model/options", url.Values{"explicit_only": {"1"}})
	if err != nil {
		return hermesDesktopCatalogEntry{}, false
	}
	var payload struct {
		Model     string `json:"model"`
		Provider  string `json:"provider"`
		Providers []struct {
			Slug          string   `json:"slug"`
			Models        []string `json:"models"`
			Authenticated bool     `json:"authenticated"`
		} `json:"providers"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return hermesDesktopCatalogEntry{}, false
	}
	entry = hermesDesktopCatalogEntry{expires: time.Now().Add(30 * time.Second), choices: map[string]bool{}, providers: map[string]bool{}}
	// Some supported providers expose only their already-configured default
	// until their inventory finishes loading. That exact pair remains valid;
	// missing catalogue data never authorizes arbitrary model/provider strings.
	if hermesDesktopModelName.MatchString(payload.Provider) && hermesDesktopModelName.MatchString(payload.Model) {
		entry.choices[payload.Provider+"\x00"+payload.Model] = true
		entry.providers[payload.Provider] = true
	}
	for _, p := range payload.Providers {
		if !p.Authenticated && p.Slug != payload.Provider {
			continue
		}
		if hermesDesktopModelName.MatchString(p.Slug) && len(entry.providers) < 512 {
			entry.providers[p.Slug] = true
		}
		for _, m := range p.Models {
			if hermesDesktopModelName.MatchString(m) && len(entry.choices) < 10000 {
				entry.choices[p.Slug+"\x00"+m] = true
			}
		}
	}
	s.mu.Lock()
	if len(s.catalog) >= 512 {
		clear(s.catalog)
	}
	s.catalog[cacheKey] = entry
	s.mu.Unlock()
	return entry, true
}

func (s *HermesDesktopService) desktopRPCModelAllowed(ctx context.Context, target *hermesDesktopTarget, frame []byte) bool {
	var request struct {
		Method string `json:"method"`
		Params struct {
			Key      string `json:"key"`
			Model    string `json:"model"`
			Provider string `json:"provider"`
			Value    string `json:"value"`
		} `json:"params"`
	}
	if json.Unmarshal(frame, &request) != nil {
		return false
	}
	if request.Method == "config.set" {
		if request.Params.Key == "reasoning" || request.Params.Key == "fast" {
			return true // Exact values and the live-session binding are checked separately.
		}
		parts := hermesDesktopModelSwitch.FindStringSubmatch(request.Params.Value)
		return len(parts) == 3 && s.desktopModelAllowed(ctx, target, parts[2], parts[1])
	}
	if request.Method == "session.create" && (request.Params.Model != "" || request.Params.Provider != "") {
		return request.Params.Model != "" && request.Params.Provider != "" && s.desktopModelAllowed(ctx, target, request.Params.Provider, request.Params.Model)
	}
	if request.Method == "setup.runtime_check" && request.Params.Provider != "" {
		entry, ok := s.desktopCatalog(ctx, target)
		return ok && entry.providers[request.Params.Provider]
	}
	return true
}

// A socket may activate/close/change only live IDs whose create/resume reply it
// received. Reconnecting renderers re-resume stored IDs; warm activation of an
// unknown runtime fails, invoking the renderer's existing resume fallback.
type hermesDesktopRPCScope struct {
	mu        sync.Mutex
	live      map[string]bool
	pending   map[string]hermesDesktopPendingRPC
	approvals map[string]map[string]bool // session -> request -> deny-only replay
}
type hermesDesktopPendingRPC struct{ method, sessionID string }

func newHermesDesktopRPCScope() *hermesDesktopRPCScope {
	return &hermesDesktopRPCScope{live: map[string]bool{}, pending: map[string]hermesDesktopPendingRPC{}, approvals: map[string]map[string]bool{}}
}

func (scope *hermesDesktopRPCScope) admit(frame []byte) error {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			SessionID string `json:"session_id"`
			RequestID string `json:"request_id"`
			Choice    string `json:"choice"`
		} `json:"params"`
	}
	if json.Unmarshal(frame, &request) != nil {
		return ErrHermesDesktopForbidden
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if len(scope.pending) >= 256 {
		return ErrHermesDesktopForbidden
	}
	if _, exists := scope.pending[string(request.ID)]; exists {
		return ErrHermesDesktopForbidden
	}
	// Reads are scoped by the CM-authenticated isolated target. Replay runs
	// immediately on reconnect, before the App's stored-ID resume completes.
	if request.Params.SessionID != "" && request.Method != "session.resume" && request.Method != "session.history" && request.Method != "session.status" && request.Method != "session.events.since" && !scope.live[request.Params.SessionID] {
		return errHermesDesktopUnboundSession
	}
	if request.Method == "approval.received" || request.Method == "approval.respond" {
		denyOnly, known := scope.approvals[request.Params.SessionID][request.Params.RequestID]
		if !known || (request.Method == "approval.respond" && denyOnly && request.Params.Choice != "deny") {
			return ErrHermesDesktopForbidden
		}
	}
	scope.pending[string(request.ID)] = hermesDesktopPendingRPC{request.Method, request.Params.SessionID}
	return nil
}

func (scope *hermesDesktopRPCScope) observe(frame []byte) ([]byte, error) {
	var packet map[string]json.RawMessage
	if json.Unmarshal(frame, &packet) != nil {
		return nil, ErrHermesDesktopUpstream
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if string(packet["method"]) == `"event"` {
		return scope.projectApprovalEvent(packet)
	}
	request, found := scope.pending[string(packet["id"])]
	if !found {
		return frame, nil
	}
	delete(scope.pending, string(packet["id"]))
	if len(packet["error"]) > 0 {
		if request.method == "setup.status" || request.method == "setup.runtime_check" || request.method == "approval.pending" || request.method == "approval.received" {
			// These upstream handlers stringify auth/approval exceptions. Keep
			// the real failure, not the provider's raw exception or credential.
			packet["error"] = json.RawMessage(`{"code":-32000,"message":"Runtime readiness or approval check failed"}`)
			return json.Marshal(packet)
		}
		return frame, nil
	}
	var result struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(packet["result"], &result)
	if request.method == "session.create" || request.method == "session.resume" {
		if !hermesDesktopSessionID.MatchString(result.SessionID) || len(scope.live) >= 512 {
			return nil, ErrHermesDesktopUpstream
		}
		scope.live[result.SessionID] = true
	}
	if request.method == "session.close" {
		delete(scope.live, request.sessionID)
		delete(scope.approvals, request.sessionID)
	}
	if request.method == "model.options" {
		projected, err := hermesDesktopProjectHTTP("/model/options", packet["result"])
		if err != nil {
			return nil, err
		}
		packet["result"] = projected
		return json.Marshal(packet)
	}
	if request.method == "setup.status" || request.method == "setup.runtime_check" || request.method == "approval.received" {
		projected, err := hermesDesktopProjectCheck(request.method, packet["result"])
		if err != nil {
			return nil, err
		}
		packet["result"] = projected
		return json.Marshal(packet)
	}
	var object map[string]json.RawMessage
	if request.method == "approval.pending" || request.method == "session.events.since" || request.method == "session.create" || request.method == "session.resume" || request.method == "session.activate" {
		if json.Unmarshal(packet["result"], &object) != nil || object == nil {
			return nil, ErrHermesDesktopUpstream
		}
		if request.method == "approval.pending" {
			var approvals []json.RawMessage
			if json.Unmarshal(object["approvals"], &approvals) != nil || len(approvals) > 256 {
				return nil, ErrHermesDesktopUpstream
			}
			projected := make([]json.RawMessage, 0, len(approvals))
			for _, approval := range approvals {
				safe, err := scope.projectApproval(request.sessionID, approval, true)
				if err != nil {
					return nil, err
				}
				projected = append(projected, safe)
			}
			body, _ := json.Marshal(projected)
			object = map[string]json.RawMessage{"approvals": body}
		}
		if raw, ok := object["pending_approval"]; ok && string(raw) != "null" {
			sid := result.SessionID
			if sid == "" {
				sid = request.sessionID
			}
			safe, err := scope.projectApproval(sid, raw, false)
			if err != nil {
				return nil, err
			}
			object["pending_approval"] = safe
		}
		if request.method == "session.events.since" && len(object["events"]) > 0 {
			var events []map[string]json.RawMessage
			if json.Unmarshal(object["events"], &events) != nil {
				return nil, ErrHermesDesktopUpstream
			}
			projected := make([]json.RawMessage, 0, len(events))
			for _, event := range events {
				// event_replay.events_since returns the bare frame.params,
				// not a JSON-RPC notification envelope. Preserve that shape
				// for the stock client's dispatchIfNewer/seq watermark logic.
				var sid string
				if json.Unmarshal(event["session_id"], &sid) != nil || sid != request.sessionID {
					return nil, ErrHermesDesktopUpstream
				}
				safe, err := scope.projectApprovalEventParams(event)
				if err != nil {
					return nil, err
				}
				projected = append(projected, safe)
			}
			object["events"], _ = json.Marshal(projected)
		}
		packet["result"], _ = json.Marshal(object)
		return json.Marshal(packet)
	}
	return frame, nil
}

func hermesDesktopProjectCheck(method string, raw json.RawMessage) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, ErrHermesDesktopUpstream
	}
	key := "ok"
	if method == "setup.status" {
		key = "provider_configured"
	} else if method == "approval.received" {
		key = "acknowledged"
	}
	var value bool
	if string(object[key]) != "true" && string(object[key]) != "false" {
		return nil, ErrHermesDesktopUpstream
	}
	_ = json.Unmarshal(object[key], &value)
	result := map[string]any{key: value}
	if method == "setup.runtime_check" && !value {
		result["error"] = "The configured runtime is not ready. Check provider configuration in ClawManager."
	}
	return json.Marshal(result)
}

// Callers hold scope.mu. approval.pending in the pinned Runtime returns raw
// queue data without _redact_approval_command. Never echo its command/details
// or permit blind approval; the user must deny and request a new live prompt.
func (scope *hermesDesktopRPCScope) projectApproval(sid string, raw json.RawMessage, denyOnly bool) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	var requestID string
	if !hermesDesktopSessionID.MatchString(sid) || json.Unmarshal(raw, &object) != nil || object == nil || json.Unmarshal(object["request_id"], &requestID) != nil || !hermesDesktopSessionID.MatchString(requestID) {
		return nil, ErrHermesDesktopUpstream
	}
	count := 0
	for _, requests := range scope.approvals {
		count += len(requests)
	}
	if _, exists := scope.approvals[sid][requestID]; !exists && count >= 1024 {
		return nil, ErrHermesDesktopUpstream
	}
	if scope.approvals[sid] == nil {
		scope.approvals[sid] = map[string]bool{}
	}
	var command string
	if !denyOnly && (json.Unmarshal(object["command"], &command) != nil || strings.TrimSpace(command) == "") {
		// A malformed live/resume payload must never offer blind approval.
		denyOnly = true
	}
	if choices, exists := object["choices"]; exists && !denyOnly {
		var values []string
		if json.Unmarshal(choices, &values) != nil {
			return nil, ErrHermesDesktopUpstream
		}
		denyOnly = true
		for _, choice := range values {
			if choice == "once" {
				denyOnly = false
			}
		}
	}
	// Once raw replay is deny-only, another event for the same request cannot
	// re-enable it. A new approval must have a new server-generated request ID.
	denyOnly = denyOnly || scope.approvals[sid][requestID]
	scope.approvals[sid][requestID] = denyOnly
	safe := hermesDesktopPickScalars(object, "request_id smart_denied")
	safe["allow_permanent"], safe["allow_session"] = json.RawMessage("false"), json.RawMessage("false")
	if denyOnly {
		safe["choices"] = json.RawMessage(`["deny"]`)
		safe["command"] = json.RawMessage(`"Command hidden: this Runtime did not provide safe approval details."`)
		safe["description"] = json.RawMessage(`"Deny this request and retry to receive a new live approval prompt."`)
	} else {
		safe["choices"] = json.RawMessage(`["once","deny"]`)
		for _, key := range []string{"command", "description"} {
			var value string
			if json.Unmarshal(object[key], &value) == nil {
				safe[key] = object[key]
			}
		}
	}
	return json.Marshal(safe)
}

func (scope *hermesDesktopRPCScope) projectApprovalEvent(packet map[string]json.RawMessage) ([]byte, error) {
	var params map[string]json.RawMessage
	if json.Unmarshal(packet["params"], &params) != nil {
		return json.Marshal(packet)
	}
	safe, err := scope.projectApprovalEventParams(params)
	if err != nil {
		return nil, err
	}
	packet["params"] = safe
	return json.Marshal(packet)
}

func (scope *hermesDesktopRPCScope) projectApprovalEventParams(params map[string]json.RawMessage) ([]byte, error) {
	if string(params["type"]) != `"approval.request"` {
		return json.Marshal(params)
	}
	var sid string
	_ = json.Unmarshal(params["session_id"], &sid)
	safe, err := scope.projectApproval(sid, params["payload"], false)
	if err != nil {
		return nil, err
	}
	params["payload"] = safe
	return json.Marshal(params)
}
