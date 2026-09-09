package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"
)

func TestFixtureRPCFieldMirrorMatchesCurrentBFF(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	policy := filepath.Join(filepath.Dir(file), "../../../backend/internal/services/hermes_desktop_policy.go")
	source, err := parser.ParseFile(token.NewFileSet(), policy, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	ast.Inspect(source, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "hermesDesktopRPCFields" {
			return true
		}
		literal, ok := spec.Values[0].(*ast.CompositeLit)
		if !ok {
			t.Fatal("BFF field table is no longer a literal; update fixture review")
		}
		for _, element := range literal.Elts {
			entry := element.(*ast.KeyValueExpr)
			key, _ := strconv.Unquote(entry.Key.(*ast.BasicLit).Value)
			value, _ := strconv.Unquote(entry.Value.(*ast.BasicLit).Value)
			want[key] = value
		}
		return false
	})
	if len(want) == 0 || !reflect.DeepEqual(fixtureRPCFields, want) {
		t.Fatal("fixture RPC fields drifted from the current BFF; do not widen the BFF to fix fixture tests")
	}
}

func TestFixtureRejectsRendererShapesTheBFFWouldDeny(t *testing.T) {
	for _, tc := range []struct {
		method string
		params map[string]any
		want   string
	}{
		{"prompt.submit", map[string]any{"runtime_id": "fixture-live", "text": "synthetic"}, "extra_runtime_id"},
		{"prompt.submit", map[string]any{"text": "synthetic"}, "missing_session_id"},
		{"prompt.submit", map[string]any{"session_id": "fixture-live", "text": "synthetic", "queued": "true"}, "boolean_type"},
		{"session.create", map[string]any{"source": "ssh"}, "source"},
		{"session.create", map[string]any{"profile": "other"}, "profile"},
		{"session.create", map[string]any{"cwd": "/private"}, "cwd"},
		{"session.activate", map[string]any{"session_id": "fixture-live", "cols": float64(10)}, "cols"},
		{"config.set", map[string]any{"session_id": "fixture-live", "key": "model", "value": "fixture-model --provider fixture --global"}, "session_model_scope"},
		{"approval.received", map[string]any{"session_id": "fixture-live"}, "request_id"},
		{"session.info", map[string]any{"session_id": "fixture-live"}, "method"},
	} {
		t.Run(tc.method+"/"+tc.want, func(t *testing.T) {
			if got := fixtureValidateRPC(tc.method, tc.params); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFixtureAcceptsKnownRendererShapesWithoutWideningPolicy(t *testing.T) {
	for _, tc := range []struct {
		method string
		params map[string]any
	}{
		{"session.create", map[string]any{"source": "desktop", "profile": "default", "cwd": "", "cols": float64(96), "model": "fixture-model", "provider": "fixture", "fast": false}},
		{"session.resume", map[string]any{"session_id": "fixture-history", "source": "desktop", "omit_messages": true}},
		{"prompt.submit", map[string]any{"session_id": "fixture-live", "text": "synthetic", "interrupted": false, "queued": false}},
		{"session.activate", map[string]any{"session_id": "fixture-live", "cols": float64(96)}},
		{"config.set", map[string]any{"session_id": "fixture-live", "key": "model", "value": "fixture-model --provider fixture --session"}},
		{"approval.received", map[string]any{"session_id": "fixture-live", "request_id": "fixture-request"}},
		{"gateway.ping", map[string]any{}},
	} {
		t.Run(tc.method, func(t *testing.T) {
			if got := fixtureValidateRPC(tc.method, tc.params); got != "" {
				t.Fatalf("unexpected mismatch %q", got)
			}
		})
	}
}
