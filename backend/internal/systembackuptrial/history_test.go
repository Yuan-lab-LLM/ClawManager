package systembackuptrial

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	staging "clawreef/internal/systembackuptrial/envelope"
	domain "clawreef/internal/systembackuptrial/records"
)

const historyFixture = `{"format":"clawmanager-trial-hermes-history.v1","user_id":1,"instance_id":7,"transcript_sha256":"ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01","sessions":[{"id":"session","messages":[{"role":"user","content":"fixture prompt"},{"role":"assistant","content":"fixture reply"}]}]}`

func TestTrialTransfersSealedContentAndLegacyArtifacts(t *testing.T) {
	ctx := context.Background()
	snapshot := historySQLFixture()
	digest, err := domain.Digest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	master := bytes.Repeat([]byte{0x21}, 32)
	for _, variant := range []string{"legacy", "hermes", "workspace", "combined", "misbound_history", "misbound_workspace"} {
		t.Run(variant, func(t *testing.T) {
			source, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			target, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			m := manifest{SourceUUID: "source-mysql", SHA256: digest, Counts: counts(snapshot)}
			var payload, files []byte
			if variant == "hermes" || variant == "combined" || variant == "misbound_history" {
				payload = []byte(historyFixture)
				description, err := describeHistory(payload, snapshot)
				if err != nil || description.Sessions != 1 || description.Messages != 2 {
					t.Fatalf("invalid history description: %+v %v", description, err)
				}
				if variant == "misbound_history" {
					description.UserID++
				}
				m.Hermes = &description
			}
			if variant == "workspace" || variant == "combined" || variant == "misbound_workspace" {
				var description workspaceManifest
				files, description = workspaceFixture(t)
				if variant == "misbound_workspace" {
					description.InstanceID++
				}
				m.Workspace = &description
			}
			id := newID()
			b := binding(id)
			receipt, err := staging.SealFile(ctx, source, b.Path, taskKey(master, id), b, func(w io.Writer) error {
				if err := json.NewEncoder(w).Encode(m); err != nil {
					return err
				}
				if _, err := w.Write(payload); err != nil {
					return err
				}
				if _, err := w.Write(files); err != nil {
					return err
				}
				return domain.Encode(w, snapshot)
			})
			if err != nil {
				t.Fatal(err)
			}
			ciphertext, err := source.ReadFile(receipt.Name)
			if err != nil || bytes.Contains(ciphertext, []byte("fixture prompt")) || bytes.Contains(ciphertext, []byte("fixture input")) {
				t.Fatal("source ciphertext unavailable or contains plaintext")
			}
			if err := target.WriteFile(receipt.Name, ciphertext, 0400); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(artifact{Receipt: receipt})
			if err != nil {
				t.Fatal(err)
			}
			if err := target.WriteFile(id+".receipt", raw, 0400); err != nil {
				t.Fatal(err)
			}
			keyFile := filepath.Join(t.TempDir(), "trial-key")
			if err := os.WriteFile(keyFile, []byte(strings.Repeat("21", 32)), 0600); err != nil {
				t.Fatal(err)
			}
			settings := map[string]string{
				"RESTORE_ISOLATED": "true", "RESTORE_UUID": "isolated-target", "KEY_FILE": keyFile, "READ_DIR": target.Name(),
			}
			service := New(func(name string) string { return settings[strings.TrimPrefix(name, "SYSTEM_BACKUP_TRIAL_")] })
			preview := service.checkRestoreArtifact(ctx, id)
			wantMissing := map[string]string{
				"hermes": "hermes_history", "combined": "hermes_history", "workspace": "workspace_files",
				"misbound_history": "restore_artifact_readback", "misbound_workspace": "restore_artifact_readback",
			}[variant]
			wantReadback := !strings.HasPrefix(variant, "misbound_")
			if preview.BackupID != id || preview.Missing != wantMissing || preview.Readback != wantReadback {
				t.Fatalf("unexpected artifact preflight: %+v", preview)
			}
			if (wantReadback && preview.CiphertextSHA256 != receipt.SHA256) || (!wantReadback && preview.CiphertextSHA256 != "") {
				t.Fatalf("preflight advertised the wrong ciphertext digest: %+v", preview)
			}
			if variant == "legacy" {
				dsnFile := filepath.Join(t.TempDir(), "target-dsn")
				if err := os.WriteFile(dsnFile, []byte("fixture:fixture@tcp(127.0.0.1:1)/cm_drill_trial_fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				settings["RESTORE_DSN_FILE"], settings["RESTORE_READ_DSN_FILE"] = dsnFile, dsnFile
				settings["RESTORE_NAMESPACE"], settings["RESTORE_STORAGE_CLASS"] = "cm-drill-fixture", "fixture"
				wrong := Task{BackupID: id, expectedCiphertextSHA256: strings.Repeat("0", 64)}
				if err := service.restore(ctx, &wrong); !errors.Is(err, errArtifactChanged) || wrong.Stage != "readback" || wrong.Readback {
					t.Fatalf("restore accepted a ciphertext that differed from preflight: %v (%+v)", err, wrong)
				}
				settings["RESTORE_UUID"] = m.SourceUUID
				if same := service.checkRestoreArtifact(ctx, id); same.Missing != "restore_isolation" || same.Readback {
					t.Fatalf("source database admitted as target: %+v", same)
				}
				settings["RESTORE_UUID"] = "isolated-target"
				if missing := service.checkRestoreArtifact(ctx, newID()); missing.Missing != "restore_artifact_missing" {
					t.Fatalf("missing artifact not reported: %+v", missing)
				}
			}
			store := storage{reader: target}
			imported, err := store.metadata(id)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := store.snapshot(ctx, id, imported, master)
			if variant == "misbound_history" || variant == "misbound_workspace" {
				if !errors.Is(err, errReadback) {
					t.Fatalf("misbound history accepted: %v", err)
				}
				return
			}
			if err != nil || bundle.manifest.SHA256 != digest || !bytes.Equal(bundle.hermes, payload) || !bytes.Equal(bundle.workspace, files) {
				t.Fatalf("target readback did not match the original artifact: %v", err)
			}
		})
	}
}

func TestTrialHermesHistoryRejectsWrongOwner(t *testing.T) {
	payload := strings.Replace(historyFixture, `"user_id":1`, `"user_id":42`, 1)
	if _, err := describeHistory([]byte(payload), historySQLFixture()); !errors.Is(err, errHistory) {
		t.Fatalf("history from a different owner accepted: %v", err)
	}
}

func TestTrialHermesAdapterUsesPrivateSocketAndChecksReceipt(t *testing.T) {
	dir, err := os.MkdirTemp("", "h-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "hermes.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	history, err := describeHistory([]byte(historyFixture), historySQLFixture())
	if err != nil {
		t.Fatal(err)
	}
	target := historyTarget{socket: socket, root: "/test/hermes-workspaces"}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Trial-Workspace-Root") != target.root {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != historyFixture {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		result := history.historyReceipt
		switch r.URL.Path {
		case "/restore/1/7", "/preflight/1/7":
		case "/verify/1/7":
			result.Messages = 0
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := target.ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err := target.run(ctx, "restore", history, []byte(historyFixture)); err != nil {
		t.Fatal(err)
	}
	if err := target.run(ctx, "preflight", history, []byte(historyFixture)); err != nil {
		t.Fatal(err)
	}
	if err := target.run(ctx, "verify", history, []byte(historyFixture)); !errors.Is(err, errHistory) {
		t.Fatalf("mismatched response accepted: %v", err)
	}
}

func historySQLFixture() domain.Snapshot {
	columns := map[string]string{
		"users":                        "id username email password_hash role is_active created_at updated_at auth_provider login_alias external_id",
		"user_quotas":                  "id user_id max_instances max_cpu_cores max_memory_gb max_storage_gb max_gpu_count created_at updated_at",
		"instances":                    "id user_id owner name alias description type runtime_type runtime_variant instance_mode cpu_cores memory_gb disk_gb gpu_enabled gpu_type gpu_count os_type os_version image_registry image_tag environment_overrides_json storage_class mount_path openclaw_config_snapshot_id created_at updated_at",
		"openclaw_injection_snapshots": "id instance_id user_id mode rendered_manifest_json rendered_env_json status created_at updated_at",
		"instance_config_revisions":    "id instance_id revision_no content_json checksum status published_by published_at activated_at created_at updated_at",
		"instance_desired_state":       "id instance_id desired_config_revision_id updated_by updated_at created_at",
	}
	data := map[string]map[string]string{
		"users":     {"id": "1", "username": "admin", "auth_provider": "local", "password_hash": "fixture", "role": "admin", "is_active": "1"},
		"instances": {"id": "7", "user_id": "1", "type": "hermes", "runtime_type": "gateway", "instance_mode": "lite"},
	}
	snapshot := domain.Snapshot{Version: domain.Version}
	for name, columnList := range columns {
		table := domain.Table{Name: name, Engine: "InnoDB", PrimaryKey: []string{"id"}}
		row := make(domain.Row)
		for _, column := range strings.Fields(columnList) {
			table.Columns = append(table.Columns, domain.Column{Name: column, Type: "varchar(255)", Nullable: true})
			if value, ok := data[name][column]; ok {
				row[column] = domain.Text(value)
			} else {
				row[column] = domain.Cell{Null: true}
			}
		}
		if data[name] != nil {
			table.Rows = []domain.Row{row}
		}
		snapshot.Tables = append(snapshot.Tables, table)
	}
	return snapshot
}
