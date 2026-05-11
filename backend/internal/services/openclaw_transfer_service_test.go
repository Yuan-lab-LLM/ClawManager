package services

import (
	"strings"
	"testing"
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
	if !strings.Contains(script, "tar xzf -") {
		t.Errorf("expected `tar xzf -` in extract, got: %s", script)
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

func TestBuildHermesImportCommand_PreservesMountedHermesDirectory(t *testing.T) {
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
	if !strings.Contains(script, `find "$target_dir" -mindepth 1 -maxdepth 1`) {
		t.Errorf("expected Hermes import to clear contents below mount point, got: %s", script)
	}
	if !strings.Contains(script, "tar xzf -") {
		t.Errorf("expected `tar xzf -` in extract, got: %s", script)
	}
	if !strings.Contains(script, `chown -R abc:abc "$target_dir"`) {
		t.Errorf("expected Hermes import to restore runtime user ownership, got: %s", script)
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
