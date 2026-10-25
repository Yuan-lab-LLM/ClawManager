package northbound

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNorthboundYAMLArtifactsParse(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, relative := range []string{
		filepath.Join("docs", "northbound-openapi.yaml"),
		filepath.Join("deployments", "k8s", "northbound", "gateway.yaml"),
		filepath.Join("deployments", "k8s", "northbound", "core-patch.yaml"),
		filepath.Join("deployments", "k8s", "northbound", "secrets.example.yaml"),
	} {
		path := filepath.Join(root, relative)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		for document := 1; ; document++ {
			var node yaml.Node
			err := decoder.Decode(&node)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("parse %s document %d: %v", relative, document, err)
			}
		}
	}
}

func TestNorthboundOpenAPIContainsShareLinkPaths(t *testing.T) {
	path := filepath.Join("..", "..", "..", "docs", "northbound-openapi.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read northbound OpenAPI: %v", err)
	}
	content := string(raw)
	for _, route := range []string{
		"/api/northbound/v1/pro-instances:",
		"/api/northbound/v1/pro-instances/{id}:",
		"/api/northbound/v1/lite-instances/{id}/restart:",
		"/api/northbound/v1/lite-instances/{id}/reset:",
		"/api/northbound/v1/lite-instances/{id}/delete:",
		"/api/northbound/v1/pro-instances/{id}/restart:",
		"/api/northbound/v1/pro-instances/{id}/reset:",
		"/api/northbound/v1/pro-instances/{id}/delete:",
		"/api/northbound/v1/lite-instances/{id}/external-access/password:",
		"/api/northbound/v1/lite-instances/{id}/external-access/share-link/reset:",
		"/api/northbound/v1/lite-instances/{id}/external-access/password/reset:",
	} {
		if !strings.Contains(content, route) {
			t.Fatalf("northbound OpenAPI is missing %s", route)
		}
	}
	if !strings.Contains(content, ScopeShareLinkReset) {
		t.Fatalf("northbound OpenAPI is missing scope %s", ScopeShareLinkReset)
	}
	if !strings.Contains(content, ScopeShareLinkManage) {
		t.Fatalf("northbound OpenAPI is missing scope %s", ScopeShareLinkManage)
	}
	if !strings.Contains(content, ScopeProCreate) || !strings.Contains(content, ScopeProRead) {
		t.Fatalf("northbound OpenAPI is missing Pro instance scopes")
	}
	for _, required := range []string{
		"required: [name, owner, type]",
		"required: [id, name, owner, type, instance_mode, runtime_type, status, created_at, updated_at]",
		"Exact, case-sensitive owner identifier",
		"enum: [openclaw, hermes, opencode, deepseek-harness]",
		"resolves the enabled DESKTOP image saved in ClawManager",
		"Optional user-facing alias",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("northbound OpenAPI is missing owner contract %q", required)
		}
	}
	for _, scope := range []string{ScopeLiteRestart, ScopeLiteReset, ScopeLiteDelete, ScopeProRestart, ScopeProReset, ScopeProDelete} {
		if !strings.Contains(content, scope) {
			t.Fatalf("northbound OpenAPI is missing lifecycle scope %s", scope)
		}
	}
}
