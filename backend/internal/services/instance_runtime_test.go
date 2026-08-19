package services

import "testing"

func TestDefaultImagePullPolicy_Default(t *testing.T) {
	t.Setenv("IMAGE_PULL_POLICY", "")
	got := defaultImagePullPolicy()
	if got != "IfNotPresent" {
		t.Fatalf("expected IfNotPresent, got %q", got)
	}
}

func TestDefaultImagePullPolicy_IgnoresEnvOverride(t *testing.T) {
	for _, envValue := range []string{"Always", "Never", "IfNotPresent", "   "} {
		t.Run(envValue, func(t *testing.T) {
			t.Setenv("IMAGE_PULL_POLICY", envValue)
			got := defaultImagePullPolicy()
			if got != "IfNotPresent" {
				t.Fatalf("expected IfNotPresent, got %q", got)
			}
		})
	}
}

func TestBuildRuntimeConfig_HermesUsesWebtopDefaults(t *testing.T) {
	config := buildRuntimeConfig("hermes", "hermes", "latest", nil, nil)

	if config.Port != 3001 {
		t.Fatalf("expected Hermes port 3001, got %d", config.Port)
	}
	if config.MountPath != "/config" {
		t.Fatalf("expected Hermes mount path /config, got %q", config.MountPath)
	}
	if config.Env["HERMES_HOME"] != "/config/.hermes" {
		t.Fatalf("expected Hermes HERMES_HOME /config/.hermes, got %q", config.Env["HERMES_HOME"])
	}
	openCodeConfig := buildRuntimeConfig("opencode", "opencode", "latest", nil, nil)
	if openCodeConfig.Env["CLAWMANAGER_SKILL_DIR"] != "/config/workspace/.opencode/skills" {
		t.Fatalf("expected OpenCode skill root /config/workspace/.opencode/skills, got %q", openCodeConfig.Env["CLAWMANAGER_SKILL_DIR"])
	}
	if config.Env["SUBFOLDER"] != "/" {
		t.Fatalf("expected Hermes default SUBFOLDER /, got %q", config.Env["SUBFOLDER"])
	}
	if config.Env["KASM_SVC_SEND_CUT_TEXT"] != kasmClipboardSendEnabled {
		t.Fatalf("expected Hermes to enable outbound clipboard sync, got %q", config.Env["KASM_SVC_SEND_CUT_TEXT"])
	}
	if config.Env["KASM_SVC_ACCEPT_CUT_TEXT"] != kasmClipboardAcceptEnabled {
		t.Fatalf("expected Hermes to enable inbound clipboard sync, got %q", config.Env["KASM_SVC_ACCEPT_CUT_TEXT"])
	}
	assertSelkiesClipboardEnabled(t, config.Env)
	if !usesWebtopImage("hermes") {
		t.Fatalf("expected Hermes to use webtop proxy behavior")
	}
}

func TestBuildRuntimeConfig_OpenClawEnablesDesktopClipboardSync(t *testing.T) {
	config := buildRuntimeConfig("openclaw", "openclaw", "latest", nil, nil)

	if config.Env["KASM_SVC_SEND_CUT_TEXT"] != kasmClipboardSendEnabled {
		t.Fatalf("expected OpenClaw to enable outbound clipboard sync, got %q", config.Env["KASM_SVC_SEND_CUT_TEXT"])
	}
	if config.Env["KASM_SVC_ACCEPT_CUT_TEXT"] != kasmClipboardAcceptEnabled {
		t.Fatalf("expected OpenClaw to enable inbound clipboard sync, got %q", config.Env["KASM_SVC_ACCEPT_CUT_TEXT"])
	}
	assertSelkiesClipboardEnabled(t, config.Env)
}

func TestBuildRuntimeConfig_WorkbuddyUsesManagedWebtopDefaults(t *testing.T) {
	config := buildRuntimeConfig("workbuddy", "workbuddy", "latest", nil, nil)

	if config.Image != defaultSystemImageSettings["workbuddy"] {
		t.Fatalf("expected Workbuddy default image %q, got %q", defaultSystemImageSettings["workbuddy"], config.Image)
	}
	if config.Port != 3001 {
		t.Fatalf("expected Workbuddy port 3001, got %d", config.Port)
	}
	if config.MountPath != "/config" {
		t.Fatalf("expected Workbuddy mount path /config, got %q", config.MountPath)
	}
	if config.Env["TITLE"] != "Workbuddy" || config.Env["SUBFOLDER"] != "/" {
		t.Fatalf("expected Workbuddy Webtop environment, got %#v", config.Env)
	}
	if !usesWebtopImage("workbuddy") {
		t.Fatalf("expected Workbuddy to use Webtop proxy behavior")
	}
	if !usesHTTPSUpstream("workbuddy") {
		t.Fatalf("expected Workbuddy to use HTTPS upstream proxying")
	}
	assertSelkiesClipboardEnabled(t, config.Env)
}

func TestBuildRuntimeConfig_DeepSeekHarnessUsesManagedWebtopDefaults(t *testing.T) {
	config := buildRuntimeConfig(RuntimeTypeDeepSeekHarness, RuntimeTypeDeepSeekHarness, "latest", nil, nil)

	if config.Image != defaultSystemImageSettings[RuntimeTypeDeepSeekHarness] {
		t.Fatalf("DeepSeek Harness image = %q", config.Image)
	}
	if config.Port != 3001 || config.MountPath != "/config" {
		t.Fatalf("unexpected DeepSeek Harness runtime config: %#v", config)
	}
	if config.Env["DSH_HOME"] != "/config/.dsh" || config.Env["TITLE"] != "DeepSeek Harness Pro" {
		t.Fatalf("unexpected DeepSeek Harness environment: %#v", config.Env)
	}
	if !usesWebtopImage(RuntimeTypeDeepSeekHarness) || !usesHTTPSUpstream(RuntimeTypeDeepSeekHarness) {
		t.Fatal("DeepSeek Harness Pro must use Webtop HTTPS proxy behavior")
	}
}

func assertSelkiesClipboardEnabled(t *testing.T, env map[string]string) {
	t.Helper()
	for _, key := range []string{
		"SELKIES_CLIPBOARD_ENABLED",
		"SELKIES_CLIPBOARD_IN_ENABLED",
		"SELKIES_CLIPBOARD_OUT_ENABLED",
	} {
		if got := env[key]; got != selkiesClipboardEnabled {
			t.Fatalf("expected %s=%q, got %q", key, selkiesClipboardEnabled, got)
		}
	}
}
