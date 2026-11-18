package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHermesDesktopV0225PickerAliases(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/password-login" {
			desktopTestLogin(t, w, r)
			return
		}
		if r.URL.Path != "/api/model/options" || r.URL.Query().Get("explicit_only") != "1" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write([]byte(`{"provider":"auto","model":"auto","providers":[{"slug":"managed","authenticated":true,"aliases":["custom:managed","named","../bad"],"models":["model-1"]},{"slug":"unconfigured","authenticated":false,"aliases":["custom:unconfigured"],"models":["secret-model"]}]}`))
	}))
	defer upstream.Close()
	s := desktopFixture(t, upstream.URL)
	target, _, err := s.resolve(context.Background(), 45, 123)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"managed", "custom:managed", "named"} {
		for _, frame := range [][]byte{desktopRPCFrame(1, "session.create", map[string]any{"model": "model-1", "provider": provider}), desktopRPCFrame(2, "config.set", map[string]any{"session_id": "live", "key": "model", "value": "model-1 --provider " + provider + " --session"})} {
			filtered, e := hermesDesktopFilterRPC(frame)
			if e != nil || !s.desktopRPCModelAllowed(context.Background(), target, filtered) {
				t.Fatalf("advertised picker pair rejected: %s %v", provider, e)
			}
		}
	}
	for _, pair := range [][2]string{{"custom:managed", "not-advertised"}, {"custom:unconfigured", "secret-model"}, {"custom:custom:managed", "model-1"}, {"../bad", "model-1"}} {
		if s.desktopModelAllowed(context.Background(), target, pair[0], pair[1]) {
			t.Fatalf("unadvertised or unauthenticated pair accepted: %v", pair)
		}
	}
}

func TestHermesDesktopV0225PickerProfile(t *testing.T) {
	for _, profile := range []string{"default", "current"} {
		for _, refresh := range []bool{false, true} {
			frame, err := hermesDesktopFilterRPC(desktopRPCFrame(1, "model.options", map[string]any{"profile": profile, "explicit_only": true, "refresh": refresh}))
			if err != nil {
				t.Fatal(err)
			}
			var packet struct {
				Params map[string]any `json:"params"`
			}
			if err = json.Unmarshal(frame, &packet); err != nil {
				t.Fatal(err)
			}
			if _, exists := packet.Params["profile"]; exists || packet.Params["explicit_only"] != true || packet.Params["refresh"] != refresh {
				t.Fatalf("unsafe picker normalization: %s", frame)
			}
			if err = newHermesDesktopRPCScope().admit(frame); err != nil {
				t.Fatalf("pre-session picker rejected: %v", err)
			}
		}
	}
	for _, profile := range []any{"other", "all", "../other", true, nil} {
		if _, err := hermesDesktopFilterRPC(desktopRPCFrame(1, "model.options", map[string]any{"profile": profile})); !errors.Is(err, ErrHermesDesktopForbidden) {
			t.Fatalf("foreign/malformed profile accepted: %v", profile)
		}
	}
}

func TestHermesDesktopV0225ServeConversationLifecycle(t *testing.T) {
	scope := newHermesDesktopRPCScope()
	admit := func(id int, method string, p map[string]any) []byte {
		t.Helper()
		f, e := hermesDesktopFilterRPC(desktopRPCFrame(id, method, p))
		if e != nil {
			t.Fatalf("filter %s: %v", method, e)
		}
		if e = scope.admit(f); e != nil {
			t.Fatalf("admit %s: %v", method, e)
		}
		return f
	}
	observe := func(id int, result any, errValue any) {
		t.Helper()
		r := map[string]any{"jsonrpc": "2.0", "id": id}
		if errValue != nil {
			r["error"] = errValue
		} else {
			r["result"] = result
		}
		b, _ := json.Marshal(r)
		if _, e := scope.observe(b); e != nil {
			t.Fatalf("observe: %v", e)
		}
	}
	f := admit(1, "session.create", map[string]any{"source": "desktop", "cols": 96, "profile": "default", "cwd": "", "cwd_explicit": false, "provider": "custom:managed", "model": "model-1", "reasoning_effort": "high", "fast": false, "service_tier": "ultrafast", "idempotency_key": "create-intent-1"})
	if strings.Contains(string(f), `"cwd`) || strings.Contains(string(f), `"profile"`) || !strings.Contains(string(f), `"source":"web"`) {
		t.Fatalf("unscoped create: %s", f)
	}
	observe(1, map[string]any{"session_id": "live-1", "stored_session_id": "stored-1"}, nil)
	// Current renderer intentionally probes serve with an identity, then retries
	// once only on its pre-handler 4000 refusal. CM must preserve that contract.
	f = admit(2, "prompt.submit", map[string]any{"session_id": "live-1", "text": "hello", "submission_id": "send-1", "profile": "default"})
	if !strings.Contains(string(f), `"submission_id":"send-1"`) || strings.Contains(string(f), `"profile"`) {
		t.Fatalf("identity/scope lost: %s", f)
	}
	observe(2, nil, map[string]any{"code": 4000, "message": "invalid params for prompt.submit: submission_id: extra inputs are not permitted"})
	admit(3, "prompt.submit", map[string]any{"session_id": "live-1", "text": "hello"})
	observe(3, map[string]any{"status": "streaming"}, nil)
	admit(4, "session.interrupt", map[string]any{"session_id": "live-1"})
	observe(4, map[string]any{"interrupted": true}, nil)
	fresh := newHermesDesktopRPCScope()
	frame, _ := hermesDesktopFilterRPC(desktopRPCFrame(5, "prompt.submit", map[string]any{"session_id": "live-1", "text": "unbound"}))
	if !errors.Is(fresh.admit(frame), errHermesDesktopUnboundSession) {
		t.Fatal("new socket bypassed resume")
	}
	scope = fresh
	admit(6, "session.events.since", map[string]any{"session_id": "live-1", "last_seen": 3, "replay_epoch": "epoch-1", "profile": "current"})
	observe(6, map[string]any{"events": []any{}, "open_requests": []any{}}, nil)
	admit(7, "session.resume", map[string]any{"session_id": "stored-1", "source": "desktop", "profile": "default", "cols": 96, "defer_history": true, "inline_images": false})
	observe(7, map[string]any{"session_id": "live-2", "stored_session_id": "stored-1", "messages": []any{}}, nil)
	admit(8, "session.activate", map[string]any{"session_id": "live-2", "profile": "default", "omit_messages": true})
	observe(8, map[string]any{"session_id": "live-2"}, nil)
	admit(9, "session.close", map[string]any{"session_id": "live-2"})
	observe(9, map[string]any{"closed": true}, nil)
	frame, _ = hermesDesktopFilterRPC(desktopRPCFrame(10, "prompt.submit", map[string]any{"session_id": "live-2", "text": "closed"}))
	if !errors.Is(scope.admit(frame), errHermesDesktopUnboundSession) {
		t.Fatal("closed session remained authorized")
	}
}

func TestHermesDesktopV0225ConversationBoundary(t *testing.T) {
	for _, tc := range []struct {
		method string
		p      map[string]any
	}{
		{"prompt.submit", map[string]any{"session_id": "live", "text": "hi", "profile": "other"}},
		{"prompt.submit", map[string]any{"session_id": "live", "text": "hi", "submission_id": map[string]any{"path": "/"}}},
		{"prompt.submit", map[string]any{"session_id": "live", "text": "hi", "submission_id": strings.Repeat("a", 161)}},
		{"session.create", map[string]any{"source": "gui", "request_id": "canonical"}},
		{"session.create", map[string]any{"cwd": "/other", "cwd_explicit": false}},
		{"session.create", map[string]any{"cwd": "", "cwd_explicit": "false"}},
		{"session.create", map[string]any{"service_tier": "arbitrary"}},
		{"session.events.since", map[string]any{"session_id": "live", "last_seen": 1, "replay_epoch": "../other"}},
		{"prompt.submit", map[string]any{"session_id": "live", "text": "hi", "_hosted_task": map[string]any{}}},
	} {
		if _, err := hermesDesktopFilterRPC(desktopRPCFrame(1, tc.method, tc.p)); !errors.Is(err, ErrHermesDesktopForbidden) {
			t.Fatalf("unsafe frame accepted: %s %v", tc.method, tc.p)
		}
	}
}
