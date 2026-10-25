package services

import (
	"context"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestBuildBaseDirExpr_UsesEnvVarWithFallback(t *testing.T) {
	got := buildBaseDirExpr()
	want := "${CLAWMANAGER_AGENT_PERSISTENT_DIR:-/config}"
	if got != want {
		t.Fatalf("buildBaseDirExpr() = %q, want %q", got, want)
	}
}

func TestBuildExportCommand_UsesBaseDirNotHome(t *testing.T) {
	cmd := buildExportCommand()
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-lc" {
		t.Fatalf("unexpected command shape: %#v", cmd)
	}
	script := cmd[2]

	if !strings.Contains(script, "CLAWMANAGER_AGENT_PERSISTENT_DIR") {
		t.Errorf("expected CLAWMANAGER_AGENT_PERSISTENT_DIR in script, got: %s", script)
	}
	if strings.Contains(script, "HOME") || strings.Contains(script, "/home/user") {
		t.Errorf("export script must not depend on $HOME or /home/user, got: %s", script)
	}
	if !strings.Contains(script, "exit 42") {
		t.Errorf("expected `exit 42` for empty-workspace branch, got: %s", script)
	}
	if !strings.Contains(script, "tar czf -") {
		t.Errorf("expected `tar czf -` streaming, got: %s", script)
	}
}

func TestBuildExportCommand_QuotesConfigDirName(t *testing.T) {
	cmd := buildExportCommand()
	quoted := shellQuote(openclawConfigDirName)
	if !strings.Contains(cmd[2], quoted) {
		t.Errorf("expected quoted %q in script, got: %s", quoted, cmd[2])
	}
}

func TestBuildHermesExportCommand_ExportsHermesDirectoryFromConfig(t *testing.T) {
	cmd := buildHermesExportCommand()
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-lc" {
		t.Fatalf("unexpected command shape: %#v", cmd)
	}
	script := cmd[2]

	if !strings.Contains(script, `base_dir="/config"`) {
		t.Errorf("expected Hermes export to use /config as archive base, got: %s", script)
	}
	if !strings.Contains(script, `target_dir="$base_dir/.hermes"`) {
		t.Errorf("expected Hermes export target to be /config/.hermes, got: %s", script)
	}
	if !strings.Contains(script, shellQuote(hermesConfigDirName)) {
		t.Errorf("expected quoted .hermes in script, got: %s", script)
	}
}

func TestBuildImportCommand_UsesSuAbc(t *testing.T) {
	cmd := buildImportCommand()
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-lc" {
		t.Fatalf("unexpected command shape: %#v", cmd)
	}
	script := cmd[2]

	if !strings.Contains(script, "su abc -s /bin/sh -c") {
		t.Errorf("expected `su abc -s /bin/sh -c` wrap to land files as uid 1000, got: %s", script)
	}
	for _, want := range []string{"archive.tar.gz", "tar tzf", "tar xzf", ".preserved.", "mv \"$new_target\" \"$target_dir\""} {
		if !strings.Contains(script, want) {
			t.Errorf("expected atomic import fragment %q, got: %s", want, script)
		}
	}
}

func TestBuildImportCommand_NoHomeReference(t *testing.T) {
	cmd := buildImportCommand()
	script := cmd[2]
	if strings.Contains(script, "HOME") || strings.Contains(script, "/home/user") {
		t.Errorf("import script must not depend on $HOME or /home/user, got: %s", script)
	}
	if !strings.Contains(script, "CLAWMANAGER_AGENT_PERSISTENT_DIR") {
		t.Errorf("expected CLAWMANAGER_AGENT_PERSISTENT_DIR in import script, got: %s", script)
	}
}

func TestBuildHermesImportCommand_AtomicallyPreservesPreviousDirectory(t *testing.T) {
	cmd := buildHermesImportCommand()
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-lc" {
		t.Fatalf("unexpected command shape: %#v", cmd)
	}
	script := cmd[2]

	if !strings.Contains(script, `base_dir="/config"`) {
		t.Errorf("expected Hermes import to use /config as archive base, got: %s", script)
	}
	if strings.Contains(script, `rm -rf "$target_dir"`) {
		t.Errorf("Hermes import must not remove the /config/.hermes mount point, got: %s", script)
	}
	for _, want := range []string{"archive.tar.gz", "tar tzf", "tar xzf", `.hermes.preserved.`, `mv "$new_target" "$target_dir"`} {
		if !strings.Contains(script, want) {
			t.Errorf("expected atomic Hermes import fragment %q, got: %s", want, script)
		}
	}
	if !strings.Contains(script, `chown -R abc:abc "$target_dir"`) {
		t.Errorf("expected Hermes import to restore runtime user ownership, got: %s", script)
	}
}

func TestWorkspaceSpecForLiteUsesRuntimeWorkspacePath(t *testing.T) {
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.byID[123] = &models.Instance{
		ID:                123,
		UserID:            45,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		RuntimeGeneration: 2,
	}
	service := &openClawTransferService{instanceRepo: instanceRepo}

	spec, err := service.workspaceSpecForInstance(context.Background(), 45, 123, openClawWorkspaceSpec())
	if err != nil {
		t.Fatalf("workspaceSpecForInstance returned error: %v", err)
	}
	if got, want := spec.baseDirExpr, RuntimeWorkspacePath(RuntimeTypeOpenClaw, 45, 123); got != want {
		t.Fatalf("base dir = %q, want %q", got, want)
	}
}

func TestWorkspaceSpecForProKeepsDesktopConfigPath(t *testing.T) {
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.byID[124] = &models.Instance{
		ID:           124,
		UserID:       45,
		Type:         RuntimeTypeOpenClaw,
		RuntimeType:  RuntimeBackendDesktop,
		InstanceMode: InstanceModePro,
	}
	service := &openClawTransferService{instanceRepo: instanceRepo}

	spec, err := service.workspaceSpecForInstance(context.Background(), 45, 124, openClawWorkspaceSpec())
	if err != nil {
		t.Fatalf("workspaceSpecForInstance returned error: %v", err)
	}
	if got, want := spec.baseDirExpr, buildBaseDirExpr(); got != want {
		t.Fatalf("base dir = %q, want %q", got, want)
	}
}

func TestShellQuote_HandlesSingleQuotes(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "''"},
		{"abc", "'abc'"},
		{"a'b", `'a'"'"'b'`},
		{".openclaw", "'.openclaw'"},
	}
	for _, tc := range cases {
		got := shellQuote(tc.in)
		if got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
