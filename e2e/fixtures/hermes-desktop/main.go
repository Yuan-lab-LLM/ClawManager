// A loopback-only protocol fixture for manual renderer QA. No real Hermes,
// database, credentials, tools, or model calls are used here.
// Run from backend/: go run ../e2e/fixtures/hermes-desktop/main.go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const prefix = "/api/v1/instances/1/hermes-desktop"

type fixturePending struct {
	Kind      string
	RequestID string
	Question  string
	Payload   map[string]any
	DenyOnly  bool
}

type fixtureSession struct {
	StoredID  string
	RuntimeID string
	Title     string
	Messages  []any
	Running   bool
	Pending   *fixturePending
	Model     string
}

// Diagnostics contain categories and counters only: never message text, RPC
// params, request queries, cookies, ticket URLs or browser error strings.
type fixtureDiagnostics struct {
	mu     sync.Mutex
	counts map[string]int
}

func (d *fixtureDiagnostics) record(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.counts[key]; !exists && len(d.counts) >= 256 {
		key = "diagnostics.additional_categories"
	}
	d.counts[key]++
}

var fixtureSafeAssetPath = regexp.MustCompile(`^/hermes-desktop-web/(assets|fonts|sounds|backgrounds|emotes)/[A-Za-z0-9_./-]{1,200}$`)
var fixtureSafeRPCMethod = regexp.MustCompile(`^[a-z][a-z0-9_]{0,19}(\.[a-z][a-z0-9_]{0,19}){1,3}$`)
var fixtureSafeStackFunction = regexp.MustCompile(`^[A-Za-z0-9_$<>.]{0,80}$`)

// Finite mirror of backend/internal/services/hermes_desktop_policy.go. This
// fixture does not authorize a real Runtime; mismatches are diagnostic failures,
// never reasons to widen the production BFF. Values/unknown key text are not logged.
var fixtureRPCFields = map[string]string{
	"ping": "", "setup.status": "", "setup.runtime_check": "provider",
	"session.create":   "source cols profile cwd model provider reasoning_effort fast",
	"session.resume":   "session_id source cols profile defer_history omit_messages lazy",
	"session.activate": "session_id cols omit_messages", "session.usage": "session_id", "session.close": "session_id",
	"model.options": "session_id explicit_only refresh", "config.set": "session_id key value confirm_expensive_model",
	"session.list": "limit", "session.status": "session_id", "session.history": "session_id", "session.interrupt": "session_id",
	"session.events.since": "session_id last_seen", "prompt.submit": "session_id text interrupted queued",
	"approval.respond": "session_id request_id choice", "approval.pending": "session_id", "approval.received": "session_id request_id",
	"clarify.respond": "session_id request_id question_id answer",
}

var fixtureSessionID = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,160}$`)
var fixtureModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./:@+-]{0,255}$`)
var fixtureModelSwitch = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_./:@+-]{0,255}) --provider ([A-Za-z0-9][A-Za-z0-9_./:@+-]{0,255}) --session$`)

func fixtureValidateRPC(method string, params map[string]any) string {
	if method == "gateway.ping" {
		method = "ping"
	}
	fields, allowed := fixtureRPCFields[method]
	if !allowed {
		return "method"
	}
	for key, value := range params {
		if !strings.Contains(" "+fields+" ", " "+key+" ") {
			if key == "runtime_id" {
				return "extra_runtime_id"
			}
			return "extra_field"
		}
		switch key {
		case "session_id", "request_id", "question_id", "source", "model", "provider", "text", "choice", "answer", "key", "value", "profile", "cwd", "reasoning_effort":
			v, ok := value.(string)
			if !ok {
				return "string_type"
			}
			if key == "session_id" && !fixtureSessionID.MatchString(v) {
				return "session_id"
			}
			if key == "source" && v != "web" && v != "desktop" {
				return "source"
			}
			if key == "profile" && v != "default" && v != "current" {
				return "profile"
			}
			if key == "cwd" && v != "" {
				return "cwd"
			}
			if (key == "model" || key == "provider") && !fixtureModelName.MatchString(v) {
				return "model_name"
			}
			if key == "reasoning_effort" && !strings.Contains(" none minimal low medium high xhigh max ", " "+v+" ") {
				return "reasoning_effort"
			}
			if key == "choice" && v != "once" && v != "deny" {
				return "choice"
			}
			if key != "text" && key != "answer" && len(v) > 512 {
				return "string_length"
			}
		case "last_seen", "limit", "cols":
			v, ok := value.(float64)
			if !ok || v != float64(int64(v)) || v < 0 || v > float64(1<<53) {
				return "integer_type"
			}
			if key == "limit" && v > 100 {
				return "limit"
			}
			if key == "cols" && (v < 20 || v > 500) {
				return "cols"
			}
		case "defer_history", "omit_messages", "lazy", "fast", "explicit_only", "refresh", "confirm_expensive_model", "interrupted", "queued":
			if _, ok := value.(bool); !ok {
				return "boolean_type"
			}
		}
	}
	if strings.Contains(fields, "session_id") && method != "model.options" {
		if _, exists := params["session_id"]; !exists {
			return "missing_session_id"
		}
	}
	if method == "config.set" {
		value, _ := params["value"].(string)
		if params["key"] != "model" || !fixtureModelSwitch.MatchString(value) {
			return "session_model_scope"
		}
	}
	if method == "approval.received" || method == "approval.respond" {
		id, _ := params["request_id"].(string)
		if !fixtureSessionID.MatchString(id) {
			return "request_id"
		}
	}
	return ""
}

func fixtureSessionRow(session *fixtureSession) map[string]any {
	return map[string]any{
		"id": session.StoredID, "title": session.Title, "source": "web", "profile": "default",
		"message_count": len(session.Messages), "started_at": time.Now().Unix(), "last_active": time.Now().Unix(),
		"ended_at": nil, "model": session.Model, "is_active": session.Running, "input_tokens": 0, "output_tokens": 0,
		"tool_call_count": 0, "preview": "Local synthetic fixture — no model or tool execution", "cwd": nil,
	}
}

func fixtureRuntimeInfo(session *fixtureSession) map[string]any {
	return map[string]any{"model": session.Model, "provider": "fixture", "running": session.Running,
		"version": "0.21.0", "desktop_contract": 6, "tools": map[string]any{}, "skills": []any{}, "approval_mode": "manual"}
}

const fixtureDiagnosticsJS = `(() => {
  const safePath = value => {
    try { const url = new URL(value, location.origin);
      return url.origin === location.origin && /^\/hermes-desktop-web\/(assets|fonts|sounds|backgrounds|emotes)\/[A-Za-z0-9_./-]{1,200}$/.test(url.pathname) ? url.pathname : '';
    } catch { return ''; }
  };
  const stackFrames = error => {
    if (typeof error?.stack !== 'string') return [];
    return error.stack.split('\n').flatMap(line => {
      const match = line.match(/(https?:\/\/[^\s)]+):(\d+):(\d+)/);
      const path = match ? safePath(match[1]) : '';
      if (!path) return [];
      const fn = line.match(/^\s*at\s+([A-Za-z0-9_$<>.]{1,80})\s/);
      return [{path, line: Number(match[2]), column: Number(match[3]), fn: fn?.[1] || ''}];
    }).slice(0, 10);
  };
  const send = (kind, path = '', error = undefined) => { void fetch('/fixture-diagnostics', { method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({kind, path, stack: stackFrames(error)}), cache: 'no-store', credentials: 'omit' }).catch(() => {}); };
  const classify = error => {
    if (error?.code === 'desktop_browser_unsupported') return 'unsupported';
    if (typeof error?.message === 'string' && /is not a function/.test(error.message)) return 'not-function';
    return ['TypeError', 'ReferenceError', 'SyntaxError', 'RangeError'].includes(error?.name) ? error.name : 'other';
  };
  addEventListener('error', event => {
    if (event instanceof ErrorEvent) send('browser.error.' + classify(event.error), safePath(event.filename), event.error);
    else {
      const tag = event.target?.tagName?.toLowerCase();
      send('browser.resource.' + (['img', 'script', 'link', 'audio', 'video'].includes(tag) ? tag : 'other'), safePath(event.target?.src || event.target?.href));
    }
  }, true);
  addEventListener('unhandledrejection', event => send('browser.rejection.' + classify(event.reason), '', event.reason));
  for (const key of ['error', 'warn']) {
    const original = console[key].bind(console);
    console[key] = (...args) => {
      send('browser.console.' + key);
      const error = args.find(value => value instanceof Error);
      const messages = args.flatMap(value => typeof value === 'string' ? [value] : value instanceof Error ? [value.message] : []);
      if (messages.some(value => value.includes('Maximum update depth exceeded'))) send('browser.react.update-depth', '', error || new Error());
      if (messages.some(value => value.includes('getSnapshot should be cached'))) send('browser.react.snapshot-uncached', '', error || new Error());
      original(...args);
    };
  }
  addEventListener('message', event => {
    const frame = document.querySelector('iframe');
    if (event.origin !== location.origin || event.source !== frame?.contentWindow || event.data?.instanceId !== 1) return;
    if (event.data.type === 'clawmanager:hermes-desktop:ready') send('renderer.ready');
    if (event.data.type === 'clawmanager:hermes-desktop:error') send('renderer.error');
  });
})();`

func main() {
	assets := flag.String("assets", "../frontend/public/hermes-desktop-web", "compiled renderer directory")
	port := flag.Int("port", 9327, "loopback port")
	flag.Parse()
	root, err := filepath.Abs(*assets)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	diagnostics := &fixtureDiagnostics{counts: map[string]int{}}
	assetsHandler := http.StripPrefix("/hermes-desktop-web/", http.FileServer(http.Dir(root)))
	mux.HandleFunc("/hermes-desktop-web/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hermes-desktop-web/" || r.URL.Path == "/hermes-desktop-web/index.html" {
			body, readErr := os.ReadFile(filepath.Join(root, "index.html"))
			if readErr != nil {
				http.Error(w, "Compile the true upstream renderer before starting this fixture", http.StatusServiceUnavailable)
				return
			}
			// Only this local fixture HTML receives diagnostics. The production
			// JavaScript bundles, source and layout are served byte-for-byte.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			fmt.Fprint(w, strings.Replace(string(body), "<head>", `<head><script src="/fixture-diagnostics.js"></script>`, 1))
			return
		}
		assetsHandler.ServeHTTP(w, r)
	})
	var stateMu sync.Mutex
	sequence := 0
	sessions := map[string]*fixtureSession{
		"fixture-history": {
			StoredID: "fixture-history", RuntimeID: "fixture-history-live", Title: "已保存的测试会话", Model: "fixture-model",
			Messages: []any{
				map[string]any{"role": "user", "content": "这是什么页面？"},
				map[string]any{"role": "assistant", "content": "这是 **本地协议测试**。发送 `fixture approval` 测试批准/拒绝、`fixture clarify` 测试补充回答、`fixture wait` 测试停止。\n\n```text\nNo model call / no tools executed\n```"},
			},
		},
	}
	sessionOrder := []string{"fixture-history"}
	findRuntime := func(runtimeID string) *fixtureSession {
		for _, session := range sessions {
			if session.RuntimeID == runtimeID {
				return session
			}
		}
		return nil
	}
	writeJSON := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(data)
	}
	mux.HandleFunc("/fixture-diagnostics.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprint(w, fixtureDiagnosticsJS)
	})
	mux.HandleFunc("/fixture-diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			diagnostics.mu.Lock()
			defer diagnostics.mu.Unlock()
			writeJSON(w, map[string]any{"synthetic_only": true, "counters": diagnostics.counts})
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Origin") != fmt.Sprintf("http://127.0.0.1:%d", *port) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var report struct {
			Kind  string `json:"kind"`
			Path  string `json:"path"`
			Stack []struct {
				Path     string `json:"path"`
				Line     int    `json:"line"`
				Column   int    `json:"column"`
				Function string `json:"fn"`
			} `json:"stack"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&report) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if report.Path != "" && (!fixtureSafeAssetPath.MatchString(report.Path) || strings.Contains(report.Path, "..")) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(report.Stack) > 10 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, frame := range report.Stack {
			if !fixtureSafeAssetPath.MatchString(frame.Path) || strings.Contains(frame.Path, "..") || !fixtureSafeStackFunction.MatchString(frame.Function) || frame.Line <= 0 || frame.Line > 1000000 || frame.Column <= 0 || frame.Column > 10000000 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		switch report.Kind {
		case "browser.error.other", "browser.error.TypeError", "browser.error.ReferenceError", "browser.error.SyntaxError", "browser.error.RangeError", "browser.error.not-function", "browser.error.unsupported",
			"browser.rejection.other", "browser.rejection.TypeError", "browser.rejection.ReferenceError", "browser.rejection.SyntaxError", "browser.rejection.RangeError", "browser.rejection.not-function", "browser.rejection.unsupported",
			"browser.resource.img", "browser.resource.script", "browser.resource.link", "browser.resource.audio", "browser.resource.video", "browser.resource.other",
			"browser.console.error", "browser.console.warn", "browser.react.update-depth", "browser.react.snapshot-uncached", "renderer.ready", "renderer.error":
			key := report.Kind
			if report.Path != "" {
				key += " " + report.Path
			}
			diagnostics.record(key)
			for _, frame := range report.Stack {
				diagnostics.record(fmt.Sprintf("stack.%s %s:%d:%d %s", report.Kind, frame.Path, frame.Line, frame.Column, frame.Function))
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	mux.HandleFunc(prefix+"/session", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "data": map[string]any{
			"available": true, "instance_id": 1, "api_base": prefix,
			"capabilities": []string{"chat", "sessions"},
			"expires_at":   time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
			"versions":     map[string]string{"bridge_version": "1", "hermes_ref": "v2026.8.31", "hermes_commit": "29112bef099274229cadff79cdff7bf7b99c4b77"},
		}})
	})
	mux.HandleFunc(prefix+"/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"version": "0.21.0", "model": "fixture-model", "provider": "fixture", "status": "running"})
	})
	fixtureConfig := map[string]any{"agent": map[string]any{"reasoning_effort": "medium"}, "display": map[string]any{"timestamps": false}, "stt": map[string]any{"enabled": false}}
	for _, path := range []string{"/api/config", "/api/config/defaults"} {
		mux.HandleFunc(prefix+path, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, fixtureConfig) })
	}
	modelOptions := func(model string) map[string]any {
		return map[string]any{"model": model, "provider": "fixture", "providers": []any{map[string]any{
			"name": "Synthetic fixture (no model calls)", "slug": "fixture", "is_current": true,
			"models": []string{"fixture-model", "fixture-alternate"}, "authenticated": true, "total_models": 2,
		}}}
	}
	mux.HandleFunc(prefix+"/api/model/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"model": "fixture-model", "provider": "fixture"})
	})
	mux.HandleFunc(prefix+"/api/model/options", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, modelOptions("fixture-model")) })
	listSessions := func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		defer stateMu.Unlock()
		items := []any{}
		for i := len(sessionOrder) - 1; i >= 0; i-- {
			session := sessions[sessionOrder[i]]
			if source := r.URL.Query().Get("source"); source != "" && source != "web" {
				continue
			}
			if r.URL.Query().Get("archived") == "only" || strings.Contains(","+r.URL.Query().Get("exclude_sources")+",", ",web,") {
				continue
			}
			items = append(items, fixtureSessionRow(session))
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 100 {
			limit = 100
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset < 0 {
			offset = 0
		}
		total := len(items)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		writeJSON(w, map[string]any{"sessions": items[offset:end], "total": total, "limit": limit, "offset": offset, "profile_totals": map[string]int{"default": total}})
	}
	mux.HandleFunc(prefix+"/api/sessions", listSessions)
	mux.HandleFunc(prefix+"/api/profiles/sessions", listSessions)
	mux.HandleFunc(prefix+"/api/profiles/sessions/sidebar", func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		defer stateMu.Unlock()
		items := []any{}
		for i := len(sessionOrder) - 1; i >= 0; i-- {
			items = append(items, fixtureSessionRow(sessions[sessionOrder[i]]))
		}
		writeJSON(w, map[string]any{"recents": map[string]any{"sessions": items, "profiles_truncated": map[string]bool{"default": false}},
			"cron": map[string]any{"sessions": []any{}}, "messaging": map[string]any{"sessions": []any{}}})
	})
	mux.HandleFunc(prefix+"/api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/api/sessions/"), "/")
		stateMu.Lock()
		defer stateMu.Unlock()
		if len(parts) == 1 && sessions[parts[0]] != nil {
			writeJSON(w, fixtureSessionRow(sessions[parts[0]]))
			return
		}
		if len(parts) != 2 || parts[1] != "messages" || sessions[parts[0]] == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"session_id": parts[0], "messages": sessions[parts[0]].Messages})
	})
	mux.HandleFunc(prefix+"/ws-ticket", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "data": map[string]any{
			"url": prefix + "/ws?ticket=fixture-only", "expires_at": time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339),
		}})
	})
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") == fmt.Sprintf("http://127.0.0.1:%d", *port)
	}}
	mux.HandleFunc(prefix+"/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(1 << 20)
		diagnostics.record("ws.open")
		// All data frames are written by this one read loop. The short delays
		// below expose visible streaming without a background socket writer.
		send := func(data any) {
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = conn.WriteJSON(data)
		}
		event := func(kind, sid string, payload any) {
			send(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": kind, "session_id": sid, "payload": payload}})
		}
		event("gateway.ready", "", map[string]any{"version": "0.21.0"})
		for {
			var frame struct {
				JSONRPC string         `json:"jsonrpc"`
				ID      any            `json:"id"`
				Method  string         `json:"method"`
				Params  map[string]any `json:"params"`
			}
			if conn.ReadJSON(&frame) != nil {
				return
			}
			failure := func(message string) {
				send(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "error": map[string]any{"code": -32602, "message": message}})
			}
			if frame.JSONRPC != "2.0" || frame.ID == nil {
				failure("Fixture requires a JSON-RPC 2.0 request with an ID")
				continue
			}
			if reason := fixtureValidateRPC(frame.Method, frame.Params); reason != "" {
				method := "unknown"
				if fixtureSafeRPCMethod.MatchString(frame.Method) || frame.Method == "ping" {
					method = frame.Method
				}
				diagnostics.record("rpc.policy_mismatch." + method + "." + reason)
				failure("Synthetic fixture rejected a request incompatible with the CM BFF policy: " + reason)
				continue
			}
			method := frame.Method
			if method == "gateway.ping" {
				method = "ping"
			}
			for key := range frame.Params {
				diagnostics.record("rpc.shape." + method + "." + key)
			}
			if frame.Method == "session.create" || frame.Method == "session.resume" {
				if frame.Params == nil {
					frame.Params = map[string]any{}
				}
				frame.Params["source"] = "web"
				delete(frame.Params, "profile")
				delete(frame.Params, "cwd")
			}
			sid, _ := frame.Params["session_id"].(string)
			switch frame.Method {
			case "setup.status", "setup.runtime_check", "approval.pending", "approval.received":
				diagnostics.record("rpc." + frame.Method)
			case "ping", "gateway.ping", "session.create", "session.resume", "session.activate", "session.usage", "session.close", "session.list", "session.history", "session.events.since", "session.info", "session.status", "model.options", "config.set", "prompt.submit", "session.interrupt", "approval.respond", "clarify.respond":
				diagnostics.record("rpc." + frame.Method)
			default:
				label := "rpc.unsupported"
				if fixtureSafeRPCMethod.MatchString(frame.Method) {
					label += "." + frame.Method
				}
				diagnostics.record(label)
			}
			result := map[string]any{}
			var session *fixtureSession
			var pending *fixturePending
			var completion string
			var normalReply bool
			var start bool
			var interrupted bool
			stateMu.Lock()
			switch frame.Method {
			case "setup.status":
				result = map[string]any{"provider_configured": true}
			case "setup.runtime_check":
				provider, _ := frame.Params["provider"].(string)
				result = map[string]any{"ok": provider == "" || provider == "fixture"}
				if result["ok"] != true {
					result["error"] = "Only the synthetic fixture provider is configured"
				}
			case "approval.pending", "approval.received":
				session = findRuntime(sid)
				if session == nil {
					stateMu.Unlock()
					failure("Known synthetic live session required")
					continue
				}
				if frame.Method == "approval.received" {
					result = map[string]any{"acknowledged": session.Pending != nil && session.Pending.Kind == "approval" && frame.Params["request_id"] == session.Pending.RequestID}
				} else {
					approvals := []any{}
					if session.Pending != nil && session.Pending.Kind == "approval" {
						// Match the BFF's conservative replay ledger: an approval
						// recovered without safe command content cannot later be allowed.
						session.Pending.DenyOnly = true
						approvals = append(approvals, map[string]any{"request_id": session.Pending.RequestID, "session_id": session.RuntimeID,
							"command": "", "description": "ClawManager 重连恢复审批：命令已隐藏，请拒绝后重试实时审批。", "allow_permanent": false, "choices": []string{"deny"}})
					}
					result = map[string]any{"approvals": approvals}
				}
			case "session.create":
				// The real CM BFF maps Desktop's source label to web and removes
				// the default profile label. This fixture never executes either.
				if source := frame.Params["source"]; source != "web" && source != "desktop" {
					stateMu.Unlock()
					failure("Fixture requires a managed web/desktop renderer source label")
					continue
				}
				if cwd, _ := frame.Params["cwd"].(string); cwd != "" {
					stateMu.Unlock()
					failure("Fixture does not permit a native working directory")
					continue
				}
				if profile, _ := frame.Params["profile"].(string); profile != "" && profile != "default" && profile != "current" {
					stateMu.Unlock()
					failure("Fixture only has the default managed profile")
					continue
				}
				model := "fixture-model"
				if chosen, _ := frame.Params["model"].(string); chosen != "" {
					if chosen != "fixture-model" && chosen != "fixture-alternate" {
						stateMu.Unlock()
						failure("Unknown synthetic model")
						continue
					}
					model = chosen
				}
				sequence++
				session = &fixtureSession{StoredID: fmt.Sprintf("fixture-new-%d", sequence), RuntimeID: fmt.Sprintf("fixture-live-%d", sequence), Title: fmt.Sprintf("测试会话 %d", sequence), Model: model, Messages: []any{}}
				sessions[session.StoredID] = session
				sessionOrder = append(sessionOrder, session.StoredID)
				result = map[string]any{"session_id": session.RuntimeID, "stored_session_id": session.StoredID, "message_count": len(session.Messages), "messages": session.Messages, "info": fixtureRuntimeInfo(session)}
			case "session.resume":
				session = sessions[sid]
				if (frame.Params["source"] != "web" && frame.Params["source"] != "desktop") || session == nil {
					stateMu.Unlock()
					failure("Fixture resume requires a managed source and a known stored session ID")
					continue
				}
				result = map[string]any{"session_id": session.RuntimeID, "stored_session_id": session.StoredID, "resumed": session.StoredID, "message_count": len(session.Messages), "messages": session.Messages, "running": session.Running, "info": fixtureRuntimeInfo(session)}
				if session.Pending != nil && (session.Pending.Kind == "approval" || session.Pending.Kind == "clarify") {
					result["pending_"+session.Pending.Kind] = session.Pending.Payload
				}
			case "model.options":
				model := "fixture-model"
				if sid != "" {
					session = findRuntime(sid)
					if session == nil {
						stateMu.Unlock()
						failure("Unknown synthetic live session")
						continue
					}
					model = session.Model
				}
				result = modelOptions(model)
			case "session.list":
				items := []any{}
				for _, storedID := range sessionOrder {
					items = append(items, fixtureSessionRow(sessions[storedID]))
				}
				result = map[string]any{"sessions": items, "total": len(items)}
			case "session.activate", "session.usage", "session.close", "session.history", "config.set", "session.events.since", "session.info", "session.status", "prompt.submit", "session.interrupt", "approval.respond", "clarify.respond":
				session = findRuntime(sid)
				if session == nil {
					stateMu.Unlock()
					failure("Fixture operation requires a known runtime session ID, not a stored ID")
					continue
				}
				switch frame.Method {
				case "session.activate":
					result = map[string]any{"session_id": session.RuntimeID, "resumed": session.StoredID, "message_count": len(session.Messages), "messages": session.Messages, "running": session.Running, "info": fixtureRuntimeInfo(session)}
				case "session.usage":
					result = map[string]any{"calls": 0, "input": 0, "output": 0, "total": 0, "cost_usd": 0}
				case "session.close":
					session.Running = false
					session.Pending = nil
					result = map[string]any{"status": "closed"}
				case "session.history":
					result = map[string]any{"messages": session.Messages, "session_id": session.RuntimeID}
				case "config.set":
					value, _ := frame.Params["value"].(string)
					model := strings.TrimSuffix(value, " --provider fixture --session")
					if frame.Params["key"] != "model" || model == value || (model != "fixture-model" && model != "fixture-alternate") {
						stateMu.Unlock()
						failure("Fixture only permits synthetic session-scoped model selection")
						continue
					}
					session.Model = model
					result = map[string]any{"ok": true, "model": model, "provider": "fixture", "deferred": false}
				case "session.events.since":
					result["events"] = []any{}
					result["epoch"] = "synthetic-fixture"
					result["truncated"] = false
				case "session.info", "session.status":
					result = fixtureRuntimeInfo(session)
				case "prompt.submit":
					text, ok := frame.Params["text"].(string)
					if !ok || strings.TrimSpace(text) == "" || session.Running {
						stateMu.Unlock()
						failure("Fixture needs nonempty text and an idle session")
						continue
					}
					session.Messages = append(session.Messages, map[string]any{"role": "user", "content": text})
					session.Running = true
					start = true
					requestID := fmt.Sprintf("fixture-request-%d-%d", sequence, len(session.Messages))
					switch strings.TrimSpace(text) {
					case "/approval", "fixture approval":
						pending = &fixturePending{Kind: "approval", RequestID: requestID, Payload: map[string]any{"request_id": requestID, "description": "模拟审批：仅测试确认界面，不执行任何命令。", "command": "fixture.preview --dry-run (synthetic, never executed)", "allow_permanent": false, "choices": []string{"once", "deny"}}}
					case "/clarify", "fixture clarify":
						pending = &fixturePending{Kind: "clarify", RequestID: requestID, Question: "fixture-language", Payload: map[string]any{"request_id": requestID, "questions": []any{map[string]any{"qid": "fixture-language", "question": "模拟提问：请选择测试回复语言，或输入任意补充回答。", "choices": []string{"中文", "English"}}}}}
					case "/wait", "fixture wait":
						pending = &fixturePending{Kind: "wait", RequestID: requestID}
					default:
						normalReply = true
						completion = "**连接成功。** 这是一条分段发送的测试回复，用于验证 Desktop 的流式显示和 Markdown。"
					}
					session.Pending = pending
					result["status"] = "accepted"
				case "approval.respond":
					choice, _ := frame.Params["choice"].(string)
					if session.Pending == nil || session.Pending.Kind != "approval" || frame.Params["request_id"] != session.Pending.RequestID || (choice != "once" && choice != "deny") || (session.Pending.DenyOnly && choice != "deny") {
						stateMu.Unlock()
						failure("Fixture approval requires its pending request ID and choice once or deny")
						continue
					}
					completion = "模拟审批已拒绝。没有执行命令或调用工具。"
					if choice == "once" {
						completion = "模拟审批已仅允许这一次。审批状态已完成；没有执行命令或调用工具。"
					}
					result["status"] = "accepted"
				case "clarify.respond":
					answer, ok := frame.Params["answer"].(string)
					if !ok || session.Pending == nil || session.Pending.Kind != "clarify" || frame.Params["request_id"] != session.Pending.RequestID || frame.Params["question_id"] != session.Pending.Question {
						stateMu.Unlock()
						failure("Fixture clarification requires its pending request ID, question ID and a string answer")
						continue
					}
					if answer == "" {
						completion = "模拟提问已跳过。没有调用模型或工具。"
					} else {
						completion = "模拟提问已收到回答：" + answer + "。没有调用模型或工具。"
					}
					result["status"] = "accepted"
				case "session.interrupt":
					if session.Running {
						completion = "模拟等待已停止。没有执行后台命令、模型请求或工具。"
						interrupted = true
					}
					result["status"] = "interrupted"
				}
			case "ping", "gateway.ping":
			default:
				stateMu.Unlock()
				send(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "error": map[string]any{"code": -32601, "message": "Unsupported fixture method"}})
				continue
			}
			stateMu.Unlock()
			send(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result})
			if start {
				event("message.start", sid, map[string]any{})
			}
			if pending != nil {
				if pending.Kind == "wait" {
					event("status.update", sid, map[string]any{"message": "模拟任务等待中，请点击“停止”完成取消测试；没有后台任务执行。"})
				} else {
					event(pending.Kind+".request", sid, pending.Payload)
				}
			}
			if normalReply {
				event("tool.start", sid, map[string]any{"tool": "fixture", "name": "fixture", "tool_call_id": "test-tool", "args": map[string]any{}})
				time.Sleep(100 * time.Millisecond)
				event("tool.complete", sid, map[string]any{"tool": "fixture", "name": "fixture", "tool_call_id": "test-tool", "result": "Synthetic tool output — no command executed."})
				for _, part := range []string{"**连接成功。** ", "这是一条分段发送的测试回复，", "用于验证 Desktop 的流式显示和 Markdown。"} {
					time.Sleep(100 * time.Millisecond)
					event("message.delta", sid, map[string]any{"text": part, "delta": part})
				}
			}
			if completion != "" {
				stateMu.Lock()
				session.Messages = append(session.Messages, map[string]any{"role": "assistant", "content": completion})
				session.Running = false
				session.Pending = nil
				stateMu.Unlock()
				status := "complete"
				if interrupted {
					status = "interrupted"
				}
				event("message.complete", sid, map[string]any{"text": completion, "status": status})
				event("session.info", sid, fixtureRuntimeInfo(session))
			}
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>Hermes Web 协议测试</title><script src="/fixture-diagnostics.js"></script></head><body style="margin:0;font-family:system-ui"><header style="height:42px;background:#fff6d9;padding:8px;box-sizing:border-box">仅本地合成测试 · fixture approval / fixture clarify / fixture wait · 不调用模型或工具 · 非集群验收</header><iframe title="Hermes Desktop Web fixture" src="/hermes-desktop-web/?instance_id=1" style="width:100%;height:calc(100vh - 42px);border:0"></iframe></body></html>`)
	})
	address := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("Hermes fixture (synthetic data only): http://%s", address)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTTP labels deliberately exclude query strings and stored session IDs.
		path := strings.TrimPrefix(r.URL.Path, prefix)
		switch path {
		case "/session", "/ws-ticket", "/ws", "/api/status", "/api/config", "/api/config/defaults", "/api/model/info", "/api/model/options", "/api/sessions", "/api/profiles/sessions", "/api/profiles/sessions/sidebar":
			diagnostics.record("http." + r.Method + " " + path)
		default:
			if strings.HasPrefix(path, "/api/sessions/") {
				label := "/api/sessions/:id"
				if strings.HasSuffix(path, "/messages") {
					label += "/messages"
				}
				diagnostics.record("http." + r.Method + " " + label)
			} else if strings.HasPrefix(path, "/api/") {
				diagnostics.record("http.unsupported")
			}
		}
		mux.ServeHTTP(w, r)
	})
	log.Fatal((&http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}
