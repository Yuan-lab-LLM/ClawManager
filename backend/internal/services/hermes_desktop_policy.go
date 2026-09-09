package services

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var hermesDesktopSessionID = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,160}$`)
var hermesDesktopAPIPath = regexp.MustCompile(`^/[A-Za-z0-9_.:-]+(?:/[A-Za-z0-9_.:-]+)*$`)
var hermesDesktopQueryKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

var hermesDesktopManagedPrefixes = []string{
	"/actions", "/analytics", "/audio", "/cron", "/curator", "/env", "/gateway",
	"/git", "/hermes", "/learning", "/mcp", "/memory", "/messaging", "/model",
	"/ops", "/pairing", "/plugins", "/providers", "/skills", "/tools", "/webhooks",
}

func hermesDesktopManagedAPI(method, path string, q url.Values) bool {
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch && method != http.MethodDelete {
		return false
	}
	if !hermesDesktopAPIPath.MatchString(path) {
		return false
	}
	managed := false
	for _, prefix := range hermesDesktopManagedPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			managed = true
			break
		}
	}
	if !managed || len(q) > 32 {
		return false
	}
	for key, values := range q {
		if !hermesDesktopQueryKey.MatchString(key) || len(values) != 1 || len(values[0]) > 4096 {
			return false
		}
	}
	return true
}

func hermesDesktopReasoningAllowed(value string) bool {
	switch value {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}

// Every allowed route is verified against the pinned upstream web_server.py
// and web_routers/sessions.py. Config reads are projected UI fields; profiles
// are single-instance CM labels, never upstream filesystem selectors.
func hermesDesktopHTTPAllowed(method, path string, q url.Values) bool {
	if method == http.MethodPut && path == "/config" && len(q) == 0 {
		return true
	}
	if method == http.MethodPatch || method == http.MethodDelete {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 || parts[0] != "sessions" || !hermesDesktopSessionID.MatchString(parts[1]) {
			return false
		}
		for key, values := range q {
			if key != "profile" || len(values) != 1 || (values[0] != "default" && values[0] != "current") {
				return false
			}
		}
		return true
	}
	if hermesDesktopManagedAPI(method, path, q) && path != "/model/options" {
		return true
	}
	if method != http.MethodGet {
		return false
	}
	allowed := map[string]bool{}
	switch path {
	case "/status", "/model/info", "/config", "/config/defaults", "/config/schema", "/profiles":
	case "/model/options":
		allowed["explicit_only"] = true
		allowed["refresh"] = true
		allowed["include_unconfigured"] = true
	case "/sessions", "/profiles/sessions":
		for _, key := range []string{"limit", "offset", "min_messages", "archived", "order", "source", "exclude_sources"} {
			allowed[key] = true
		}
	case "/profiles/sessions/sidebar":
		for _, key := range []string{"recents_profile", "recents_limit", "cron_limit", "messaging_limit", "recents_exclude", "messaging_exclude"} {
			allowed[key] = true
		}
	default:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if (len(parts) != 2 && len(parts) != 3) || parts[0] != "sessions" || !hermesDesktopSessionID.MatchString(parts[1]) || (len(parts) == 3 && parts[2] != "messages") {
			return false
		}
		if strings.Contains(" search stats empty owner-backfill bulk-delete import prune ", " "+parts[1]+" ") {
			return false
		}
		if len(parts) == 3 {
			for _, key := range []string{"limit", "offset", "order", "include_compacted"} {
				allowed[key] = true
			}
		}
	}
	allowed["profile"] = true
	for key, values := range q {
		if !allowed[key] || len(values) != 1 {
			return false
		}
		v := values[0]
		switch key {
		case "limit", "offset", "min_messages", "recents_limit", "cron_limit", "messaging_limit":
			n, err := strconv.Atoi(v)
			max := 10000
			if key == "limit" || strings.HasSuffix(key, "_limit") {
				max = 200
				if strings.HasSuffix(path, "/messages") {
					max = 500
				} else if path == "/sessions" || strings.HasPrefix(path, "/profiles/sessions") {
					max = 100
				}
			}
			if err != nil || n < 0 || n > max {
				return false
			}
		case "explicit_only", "refresh", "include_unconfigured", "include_compacted":
			if v != "1" && v != "true" {
				return false
			}
		case "archived":
			if v != "exclude" && v != "include" && v != "only" {
				return false
			}
		case "order":
			if v != "recent" && v != "created" && v != "latest" && v != "oldest" {
				return false
			}
		case "profile", "recents_profile":
			if v != "default" && v != "current" && !(v == "all" && (key == "recents_profile" || path == "/profiles/sessions")) {
				return false
			}
		case "source", "exclude_sources", "recents_exclude", "messaging_exclude":
			if len(v) > 256 || !hermesDesktopSources.MatchString(v) || (key == "source" && strings.Contains(v, ",")) {
				return false
			}
		}
	}
	return true
}

// Parameter allowlists are as important as method allowlists: session.create
// also accepts cwd, profile, seed messages and hosted-room fields upstream.
// Those could reach another profile/workspace in a shared Runtime Pod.
var hermesDesktopRPCFields = map[string]string{
	"ping":                 "",
	"setup.status":         "",
	"setup.runtime_check":  "provider",
	"session.create":       "source cols profile cwd model provider reasoning_effort fast",
	"session.resume":       "session_id source cols profile defer_history omit_messages lazy",
	"session.activate":     "session_id cols omit_messages",
	"session.usage":        "session_id",
	"session.close":        "session_id",
	"model.options":        "session_id explicit_only refresh",
	"config.set":           "session_id key value confirm_expensive_model",
	"config.get":           "key profile",
	"plugins.manage":       "action key enable profile identifier force",
	"session.list":         "limit",
	"session.status":       "session_id",
	"session.history":      "session_id",
	"session.interrupt":    "session_id",
	"session.events.since": "session_id last_seen",
	"prompt.submit":        "session_id text interrupted queued",
	"approval.respond":     "session_id request_id choice",
	"approval.pending":     "session_id",
	"approval.received":    "session_id request_id",
	"clarify.respond":      "session_id request_id question_id answer",
}

func hermesDesktopFilterRPC(frame []byte) ([]byte, error) {
	var packet struct {
		JSONRPC string                     `json:"jsonrpc"`
		ID      json.RawMessage            `json:"id"`
		Method  string                     `json:"method"`
		Params  map[string]json.RawMessage `json:"params"`
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&packet) != nil || packet.JSONRPC != "2.0" || len(packet.ID) == 0 || len(packet.ID) > 256 {
		return nil, ErrHermesDesktopForbidden
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, ErrHermesDesktopForbidden
	}
	// The shared Desktop client calls gateway.ping while the pinned server
	// registers ping. Normalize this transport-only alias at the boundary.
	if packet.Method == "gateway.ping" {
		packet.Method = "ping"
	}
	fields, ok := hermesDesktopRPCFields[packet.Method]
	if !ok {
		return nil, ErrHermesDesktopForbidden
	}
	allowedFields := strings.Fields(fields)
	for key, value := range packet.Params {
		if !slices.Contains(allowedFields, key) {
			return nil, ErrHermesDesktopForbidden
		}
		switch key {
		case "session_id", "request_id", "question_id", "source", "model", "provider", "text", "choice", "answer", "key", "value", "profile", "cwd", "reasoning_effort", "action", "identifier":
			var v string
			if json.Unmarshal(value, &v) != nil {
				return nil, ErrHermesDesktopForbidden
			}
			if key == "session_id" && !hermesDesktopSessionID.MatchString(v) {
				return nil, ErrHermesDesktopForbidden
			}
			if key == "source" && v != "web" && v != "desktop" {
				return nil, ErrHermesDesktopForbidden
			}
			if key == "profile" && v != "default" && v != "current" {
				return nil, ErrHermesDesktopForbidden
			}
			if key == "cwd" && v != "" {
				return nil, ErrHermesDesktopForbidden
			}
			if (key == "model" || key == "provider") && !hermesDesktopModelName.MatchString(v) {
				return nil, ErrHermesDesktopForbidden
			}
			if key == "reasoning_effort" && !hermesDesktopReasoningAllowed(v) {
				return nil, ErrHermesDesktopForbidden
			}
			if key == "choice" && v != "once" && v != "deny" {
				return nil, ErrHermesDesktopForbidden
			}
			if key != "text" && key != "answer" && len(v) > 512 {
				return nil, ErrHermesDesktopForbidden
			}
		case "last_seen", "limit", "cols":
			var v int
			if json.Unmarshal(value, &v) != nil || v < 0 || (key == "limit" && v > 100) {
				return nil, ErrHermesDesktopForbidden
			}
			if key == "cols" && (v < 20 || v > 500) {
				return nil, ErrHermesDesktopForbidden
			}
		case "defer_history", "omit_messages", "lazy", "fast", "explicit_only", "refresh", "confirm_expensive_model", "interrupted", "queued", "enable", "force":
			var v bool
			if json.Unmarshal(value, &v) != nil {
				return nil, ErrHermesDesktopForbidden
			}
		}
	}
	if slices.Contains(allowedFields, "session_id") && packet.Method != "model.options" {
		if _, ok := packet.Params["session_id"]; !ok {
			return nil, ErrHermesDesktopForbidden
		}
	}
	if packet.Method == "config.set" {
		var key, value string
		_ = json.Unmarshal(packet.Params["key"], &key)
		_ = json.Unmarshal(packet.Params["value"], &value)
		valid := key == "model" && hermesDesktopModelSwitch.FindStringSubmatch(value) != nil
		if key == "reasoning" {
			valid = hermesDesktopReasoningAllowed(value)
		}
		if key == "fast" {
			valid = value == "fast" || value == "normal"
		}
		if !valid {
			return nil, ErrHermesDesktopForbidden
		}
		if _, exists := packet.Params["confirm_expensive_model"]; exists && key != "model" {
			return nil, ErrHermesDesktopForbidden
		}
	}
	if packet.Method == "config.get" {
		var key string
		if json.Unmarshal(packet.Params["key"], &key) != nil || key != "profile" {
			return nil, ErrHermesDesktopForbidden
		}
	}
	if packet.Method == "plugins.manage" {
		var action string
		if json.Unmarshal(packet.Params["action"], &action) != nil || !slices.Contains([]string{"list", "toggle", "install"}, action) {
			return nil, ErrHermesDesktopForbidden
		}
		if action == "toggle" {
			var key string
			if json.Unmarshal(packet.Params["key"], &key) != nil || key == "" || len(key) > 512 {
				return nil, ErrHermesDesktopForbidden
			}
		}
		if action == "install" {
			var identifier string
			if json.Unmarshal(packet.Params["identifier"], &identifier) != nil || identifier == "" || len(identifier) > 2048 {
				return nil, ErrHermesDesktopForbidden
			}
		}
	}
	if packet.Method == "approval.received" || packet.Method == "approval.respond" {
		var requestID string
		if json.Unmarshal(packet.Params["request_id"], &requestID) != nil || !hermesDesktopSessionID.MatchString(requestID) {
			return nil, ErrHermesDesktopForbidden
		}
	}
	if packet.Method == "approval.respond" {
		if _, ok := packet.Params["choice"]; !ok {
			return nil, ErrHermesDesktopForbidden
		}
	}
	if packet.Method == "model.options" {
		if packet.Params == nil {
			packet.Params = map[string]json.RawMessage{}
		}
		packet.Params["explicit_only"] = json.RawMessage("true")
	}
	if packet.Method == "session.create" || packet.Method == "session.resume" {
		if packet.Params == nil {
			packet.Params = map[string]json.RawMessage{}
		}
		// Upstream automatically adds native desktop_ui tools for source=desktop.
		// A browser must use the web platform even when reusing Desktop widgets.
		packet.Params["source"] = json.RawMessage(`"web"`)
		// The owning CM instance already selects the isolated Runtime home.
		// A renderer profile label must never select another filesystem tree.
		delete(packet.Params, "profile")
		delete(packet.Params, "cwd")
	}
	return json.Marshal(packet)
}

func hermesDesktopSanitize(body []byte, password string, cookies []*http.Cookie, extraSecrets ...string) ([]byte, error) {
	secrets := append([]string{password}, extraSecrets...)
	for _, cookie := range cookies {
		secrets = append(secrets, cookie.Value)
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return nil, ErrHermesDesktopUpstream
	}
	var clean func(any) any
	clean = func(v any) any {
		switch item := v.(type) {
		case map[string]any:
			for key, child := range item {
				lower := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
				compact := strings.ReplaceAll(lower, "_", "")
				if compact == "token" || compact == "key" || strings.Contains(compact, "ticket") || strings.Contains(compact, "password") || strings.Contains(compact, "secret") || strings.Contains(compact, "credential") || strings.Contains(compact, "cookie") || strings.Contains(compact, "authorization") || strings.Contains(compact, "apikey") || strings.HasSuffix(compact, "token") {
					delete(item, key)
					continue
				}
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
