package db

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRuntimeManifestsAreValidYAML(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, manifest := range deploymentRuntimeManifests(repoRoot) {
		t.Run(manifest, func(t *testing.T) {
			file, err := os.Open(manifest)
			if err != nil {
				t.Fatalf("open manifest: %v", err)
			}
			defer file.Close()

			decoder := yaml.NewDecoder(file)
			documents := 0
			for {
				var document any
				err := decoder.Decode(&document)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("parse manifest document %d: %v", documents+1, err)
				}
				if document != nil {
					documents++
				}
			}
			if documents == 0 {
				t.Fatal("manifest contains no YAML documents")
			}
		})
	}
}

func TestDeploymentManifestsConfigureHermesDesktopWeb(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	wantDeployments := map[string]string{
		"clawmanager-app": "clawmanager-app",
		"hermes-runtime":  "runtime",
	}
	const (
		wantOrigin      = "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001"
		wantHermesImage = "ghcr.io/yuan-lab-llm/agentsruntime/hermes-lite@sha256:4fd519962d0afe796b36dade6f8e66379e7030a2c6f3c62b4a6574352b2eac44"
	)

	for _, manifest := range deploymentRuntimeManifests(repoRoot) {
		t.Run(manifest, func(t *testing.T) {
			file, err := os.Open(manifest)
			if err != nil {
				t.Fatalf("open manifest: %v", err)
			}
			defer file.Close()

			found := make(map[string]bool, len(wantDeployments))
			decoder := yaml.NewDecoder(file)
			for {
				var document struct {
					Kind     string `yaml:"kind"`
					Metadata struct {
						Name string `yaml:"name"`
					} `yaml:"metadata"`
					Spec struct {
						Template struct {
							Spec struct {
								Containers []struct {
									Name  string `yaml:"name"`
									Image string `yaml:"image"`
									Env   []struct {
										Name  string `yaml:"name"`
										Value string `yaml:"value"`
									} `yaml:"env"`
								} `yaml:"containers"`
							} `yaml:"spec"`
						} `yaml:"template"`
					} `yaml:"spec"`
				}
				if err := decoder.Decode(&document); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("parse manifest: %v", err)
				}

				containerName, wanted := wantDeployments[document.Metadata.Name]
				if document.Kind != "Deployment" || !wanted {
					continue
				}
				for _, container := range document.Spec.Template.Spec.Containers {
					if container.Name != containerName {
						continue
					}
					found[document.Metadata.Name] = true
					environment := make(map[string]string, len(container.Env))
					for _, variable := range container.Env {
						environment[variable.Name] = variable.Value
					}
					if environment["CLAWMANAGER_HERMES_DESKTOP_WEB_ENABLED"] != "true" {
						t.Fatalf("%s must explicitly enable Hermes Desktop Web", document.Metadata.Name)
					}
					if got := environment["CLAWMANAGER_CONTROL_UI_ORIGIN"]; got != wantOrigin {
						t.Fatalf("%s has unexpected Hermes control UI origin %q", document.Metadata.Name, got)
					}
					if document.Metadata.Name == "hermes-runtime" {
						if container.Image != wantHermesImage {
							t.Fatalf("Hermes runtime must use immutable image %q, got %q", wantHermesImage, container.Image)
						}
						if got := environment["CLAWMANAGER_RUNTIME_IMAGE_REF"]; got != wantHermesImage {
							t.Fatalf("Hermes runtime must report immutable image ref %q, got %q", wantHermesImage, got)
						}
						wantProxyCIDR := "10.244.0.0/16"
						if strings.Contains(filepath.Clean(manifest), filepath.Join("deployments", "k3s")) {
							wantProxyCIDR = "10.42.0.0/16"
						}
						if got := environment["CLAWMANAGER_TRUSTED_PROXY_CIDRS"]; got != wantProxyCIDR {
							t.Fatalf("Hermes runtime has unexpected trusted proxy CIDR %q", got)
						}
					}
				}
			}

			for deployment := range wantDeployments {
				if !found[deployment] {
					t.Fatalf("manifest does not contain configured %s deployment", deployment)
				}
			}
		})
	}
}

func TestNineNodeProductionDatabaseLoadControls(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	manifest := filepath.Join(repoRoot, "deployments", "k8s", "sites", "nine-node-production", "20-clawmanager-production.yaml")
	file, err := os.Open(manifest)
	if err != nil {
		t.Fatalf("open production manifest: %v", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	for document := 1; ; document++ {
		var value any
		err := decoder.Decode(&value)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("parse production manifest document %d: %v", document, err)
		}
	}

	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("read production manifest: %v", err)
	}
	// Keep the deployment assertion portable across Git checkouts that use
	// either LF or CRLF line endings.
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	for _, want := range []string{
		`--max-connections=800`,
		`--binlog-expire-logs-seconds=259200`,
		"strategy:\n    type: Recreate\n  selector:\n    matchLabels:\n      app: mysql",
		`name: DB_MAX_OPEN_CONNS`,
		`name: DB_MAX_IDLE_CONNS`,
		`name: DB_CONN_MAX_LIFETIME`,
		`name: DB_CONN_MAX_IDLE_TIME`,
		`name: SKILL_REPORT_PERSISTENCE_ENABLED`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("production manifest must contain %q", want)
		}
	}
}

func TestNineNodeProductionUsesPinnedClawManagerImage(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	want := "10.130.15.40:5000/clawmanager@sha256:0fe32d900babed48ea224a1043d140a28335db9220c108128b8fb2f47b8c34de"
	for _, relativePath := range []string{
		filepath.Join("deployments", "k8s", "sites", "nine-node-production", "20-clawmanager-production.yaml"),
		filepath.Join("deployments", "k8s", "sites", "nine-node-production", "50-northbound-production.yaml"),
	} {
		manifest := filepath.Join(repoRoot, relativePath)
		raw, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatalf("read production manifest %s: %v", manifest, err)
		}
		if !strings.Contains(string(raw), "image: "+want) {
			t.Fatalf("production manifest %s must use pinned image %s", manifest, want)
		}
	}
}

func TestRuntimeManifestsStartHermesRuntime(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, manifest := range deploymentRuntimeManifests(repoRoot) {
		t.Run(manifest, func(t *testing.T) {
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			pattern := regexp.MustCompile(`(?s)name:\s+hermes-runtime.*?spec:\s+replicas:\s+([0-9]+)`)
			matches := pattern.FindSubmatch(raw)
			if len(matches) != 2 {
				t.Fatalf("could not find hermes-runtime replicas in %s", manifest)
			}
			if string(matches[1]) != "1" {
				t.Fatalf("expected hermes-runtime replicas 1 in %s, got %s", manifest, matches[1])
			}
		})
	}
}

func TestRuntimeManifestsStartDeepSeekHarnessRuntime(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, manifest := range deploymentRuntimeManifests(repoRoot) {
		t.Run(manifest, func(t *testing.T) {
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			pattern := regexp.MustCompile(`(?s)name:\s+deepseek-harness-runtime.*?spec:\s+replicas:\s+([0-9]+)`)
			matches := pattern.FindSubmatch(raw)
			if len(matches) != 2 {
				t.Fatalf("could not find deepseek-harness-runtime replicas in %s", manifest)
			}
			if string(matches[1]) != "1" {
				t.Fatalf("expected deepseek-harness-runtime replicas 1 in %s, got %s", manifest, matches[1])
			}
		})
	}
}

func TestRuntimeManifestsExposeDeepSeekHarnessPublicURLTemplate(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, manifest := range deploymentRuntimeManifests(repoRoot) {
		t.Run(manifest, func(t *testing.T) {
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			if !strings.Contains(string(raw), "name: CLAWMANAGER_DEEPSEEK_HARNESS_PUBLIC_URL_TEMPLATE") {
				t.Fatalf("manifest %s must expose the DeepSeek Harness public URL template", manifest)
			}
		})
	}
}

func TestRuntimeManifestsExposeOpenCodePublicURLTemplate(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, manifest := range deploymentRuntimeManifests(repoRoot) {
		t.Run(manifest, func(t *testing.T) {
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			if !strings.Contains(string(raw), "name: CLAWMANAGER_OPENCODE_PUBLIC_URL_TEMPLATE") {
				t.Fatalf("manifest %s must expose the OpenCode public URL template", manifest)
			}
		})
	}
}

func TestDesktopAuthAcceptsDedicatedRuntimeInstanceVariable(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deployments", "nginx", "njs", "desktop_auth.js"))
	if err != nil {
		t.Fatalf("read desktop auth script: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "r.variables.inst_id || r.variables.runtime_inst_id") {
		t.Fatal("desktop auth must accept both path-based and dedicated-origin instance variables")
	}
	queryPreference := strings.Index(text, "validateTokenCandidate(r, readQueryToken(r), key, false)")
	cookieFallback := strings.Index(text, "var cookieTokens = readCookieTokens(r)")
	if queryPreference < 0 || cookieFallback < 0 || queryPreference >= cookieFallback {
		t.Fatal("desktop auth must prefer a fresh query capability before a stale runtime cookie")
	}
	if !strings.Contains(text, "var cookieTokens = readCookieTokens(r)") ||
		!strings.Contains(text, "for (var i = 0; i < cookieTokens.length; i++)") {
		t.Fatal("desktop auth must try every same-name runtime cookie so a stale legacy cookie cannot shadow a partitioned cookie")
	}
}

func TestNginxRoutesOpenCodeDedicatedOrigins(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deployments", "nginx", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx config: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		`server_name ~^opencode-(?<runtime_inst_id>[0-9]+)\..+$;`,
		"proxy_set_header X-ClawManager-Runtime-Origin opencode;",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("nginx config must contain %q", want)
		}
	}
}

func TestNginxWorkersAreBoundedOnLargeClusterNodes(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deployments", "nginx", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx config: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "worker_processes auto;") {
		t.Fatal("nginx must not derive its worker count from every CPU on a large Kubernetes node")
	}
	if !strings.Contains(text, "worker_processes 8;") {
		t.Fatal("nginx must use the reviewed bounded worker count")
	}
}

func TestNginxDedicatedRuntimeOriginsExposeCertificateConfirmation(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deployments", "nginx", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx config: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"location = /__clawmanager_cert_check",
		"location = /__clawmanager_cert_trust",
		"window.setTimeout(function(){window.history.back();},150);",
	} {
		if count := strings.Count(text, want); count != 2 {
			t.Fatalf("nginx dedicated runtime origins must contain %q twice, got %d", want, count)
		}
	}
	for _, want := range []string{
		`add_header Cache-Control "no-store" always;`,
		`add_header X-Frame-Options "DENY" always;`,
		`add_header Content-Security-Policy "default-src 'none';`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("nginx certificate confirmation must contain %q", want)
		}
	}
}

func TestNginxAgentRoutesSupportSilentRenewalAndLongRunningWork(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deployments", "nginx", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx config: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"location = /__clawmanager_access_refresh",
		"proxy_set_header X-ClawManager-Access-Refresh-Token $dedicated_refresh_token;",
	} {
		if count := strings.Count(text, want); count != 2 {
			t.Fatalf("nginx dedicated runtime origins must contain %q twice, got %d", want, count)
		}
	}
	for _, want := range []string{
		"proxy_read_timeout 86400s;",
		"proxy_send_timeout 86400s;",
	} {
		if count := strings.Count(text, want); count != 4 {
			t.Fatalf("nginx agent routes must contain %q four times, got %d", want, count)
		}
	}
}

func TestDesktopAuthAllowsDedicatedOriginTokenRotation(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deployments", "nginx", "njs", "desktop_auth.js"))
	if err != nil {
		t.Fatalf("read desktop auth script: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"validateTokenCandidate(r, readQueryToken(r), key, false)",
		"var cookieTokens = readCookieTokens(r)",
		"validateTokenCandidate(r, cookieTokens[i], key, false)",
		"validateTokenCandidate(r, queryToken, key, true)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("desktop auth must support managed query-token rotation; missing %q", want)
		}
	}
}

func TestRuntimeManifestsSeedLiteDefaultImages(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, manifest := range append(deploymentRuntimeManifests(repoRoot), filepath.Join(repoRoot, "backend", "deployments", "k8s", "clawreef-incluster.yaml")) {
		t.Run(manifest, func(t *testing.T) {
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			for _, image := range []string{
				"ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:latest",
				"ghcr.io/yuan-lab-llm/agentsruntime/hermes-lite:latest",
				"ghcr.io/yuan-lab-llm/agentsruntime/deepseek-harness-lite:latest",
			} {
				if !strings.Contains(string(raw), image) {
					t.Fatalf("manifest %s must seed lite image %s", manifest, image)
				}
			}
		})
	}
}

func TestRuntimeManifestsExposeOpenClawGatewayOnPodIP(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, manifest := range deploymentRuntimeManifests(repoRoot) {
		t.Run(manifest, func(t *testing.T) {
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			text := string(raw)
			want := "/usr/local/bin/openclaw gateway run --allow-unconfigured --auth token --bind lan --force"
			if !strings.Contains(text, want) {
				t.Fatalf("manifest %s must expose OpenClaw gateway on the pod network with %q", manifest, want)
			}
			if strings.Contains(text, "--auth token --bind auto --force") {
				t.Fatalf("manifest %s must not use OpenClaw --bind auto because it can bind to loopback inside runtime pods", manifest)
			}
		})
	}
}

func TestMySQLManifestsBoundBinaryLogDiskUsage(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	manifests := append(
		deploymentRuntimeManifests(repoRoot),
		filepath.Join(repoRoot, "backend", "deployments", "k8s", "clawreef-incluster.yaml"),
	)
	for _, manifest := range manifests {
		t.Run(manifest, func(t *testing.T) {
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			for _, option := range []string{
				"--binlog-expire-logs-seconds=259200",
				"--max-binlog-size=134217728",
			} {
				if !strings.Contains(string(raw), option) {
					t.Fatalf("manifest %s must configure MySQL option %s", manifest, option)
				}
			}
		})
	}
}

func deploymentRuntimeManifests(repoRoot string) []string {
	return []string{
		filepath.Join(repoRoot, "deployments", "k8s", "cluster", "clawmanager.yaml"),
		filepath.Join(repoRoot, "deployments", "k8s", "single-node", "clawmanager.yaml"),
		filepath.Join(repoRoot, "deployments", "k3s", "cluster", "clawmanager.yaml"),
		filepath.Join(repoRoot, "deployments", "k3s", "single-node", "clawmanager.yaml"),
	}
}

func TestNginxRoutesOpenCodePerInstanceOrigins(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deployments", "nginx", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx config: %v", err)
	}
	text := string(raw)
	for _, required := range []string{
		"server_name ~^opencode-(?<runtime_inst_id>[0-9]+)\\..+$;",
		"X-ClawManager-Runtime-Origin opencode",
		"/api/v1/instances/$runtime_inst_id/proxy$1",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("nginx config must contain %q", required)
		}
	}
	if got := strings.Count(text, "proxy_set_header X-ClawManager-Runtime-Origin \"\";"); got < 2 {
		t.Fatalf("public nginx routes must clear the dedicated-origin marker, got %d guards", got)
	}
}
