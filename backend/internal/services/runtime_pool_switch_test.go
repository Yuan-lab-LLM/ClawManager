package services

import (
	"clawreef/internal/repository"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPoolSwitchWorkspaceEvidence(t *testing.T) {
	for _, kind := range []string{"absent", "project", "session", "config", "symlink", "native-root-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "workspace")
			if kind == "native-root-symlink" {
				if err := os.MkdirAll(filepath.Join(root, "home"), 0700); err != nil {
					t.Fatal(err)
				}
			} else if kind != "absent" {
				if err := os.MkdirAll(filepath.Join(root, "home/.openclaw/agents/main/sessions"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			path := ""
			switch kind {
			case "project":
				path = "report.md"
			case "session":
				path = "home/.openclaw/agents/main/sessions/old.jsonl"
			case "config":
				path = "home/.openclaw/openclaw.json"
			case "symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "home/.openclaw/linked")); err != nil {
					t.Skip("symlink privilege unavailable")
				}
			case "native-root-symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "home/.openclaw")); err != nil {
					t.Skip("symlink privilege unavailable")
				}
			}
			if path != "" {
				if err := os.WriteFile(filepath.Join(root, path), []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := unstartedOpenClawWorkspace(root)
			if err != nil || got != (kind == "absent" || kind == "project") {
				t.Fatalf("eligible=%t err=%v", got, err)
			}
		})
	}
}

func TestPoolSwitchRejectsMigrationOnlyImages(t *testing.T) {
	for _, key := range []string{"CLAWMANAGER_OPENCLAW_UPGRADE_ONLY", "CLAWMANAGER_OPENCLAW_UPGRADE_WORKER_ENABLED", "CLAWMANAGER_OPENCLAW_SERVING", "CLAWMANAGER_OPENCLAW_SOURCE_COMPATIBILITY", "CLAWMANAGER_OPENCLAW_AUTHENTICATED_INGRESS"} {
		if validateOpenClawServingImage(map[string]string{key: "true"}) == nil {
			t.Fatal(key)
		}
	}
	if err := validateOpenClawServingImage(map[string]string{"CLAWMANAGER_OPENCLAW_VERSION": "2026.10.2"}); err != nil {
		t.Fatal(err)
	}
}

func TestPoolSwitchMySQLBrokenPoolAndUnstartedInstances(t *testing.T) {
	sess := upgradeFlowDB(t)
	ctx := context.Background()
	repo := repository.NewRuntimeRolloutRepository(sess)
	s := NewRuntimeUpgradeService(sess, repo, repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), nil, t.TempDir(), "")
	// No Agent or Pod exists: repairing an unused pool must not need a heartbeat.
	a, err := s.assessOpenClawPoolSwitch(ctx, 0)
	if err != nil || !a.Eligible {
		t.Fatal(a, err)
	}
	if _, err = sess.SQL().Exec(`INSERT INTO instances(id,user_id,type,instance_mode,status,runtime_generation)VALUES(557,1,'openclaw','lite','error',1)`); err != nil {
		t.Fatal(err)
	}
	a, err = s.assessOpenClawPoolSwitch(ctx, 0)
	if err != nil || !a.Eligible || len(a.Waiting) != 1 {
		t.Fatal(a, err)
	}
	if _, err = sess.SQL().Exec(`UPDATE instances SET runtime_generation=2 WHERE id=557`); err != nil {
		t.Fatal(err)
	}
	a, err = s.assessOpenClawPoolSwitch(ctx, 0)
	if err != nil || a.Eligible {
		t.Fatal("old data must use migration", a, err)
	}
}
