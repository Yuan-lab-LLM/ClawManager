package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

func TestRuntimePoolOpenCodeNativeReadViewAllPhasesMySQL(t *testing.T) {
	port, err := strconv.Atoi(os.Getenv("TEST_RUNTIME_MYSQL_PORT"))
	if err != nil || port < 1 || port > 65535 {
		t.Skip("marked disposable MySQL port required")
	}
	password := os.Getenv("TEST_RUNTIME_MYSQL_PASSWORD")
	root, err := sql.Open("mysql", fmt.Sprintf("root:%s@tcp(127.0.0.1:%d)/mysql?parseTime=true", password, port))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var marker int
	if err = root.QueryRow(`SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='cm_database_test_marker'`).Scan(&marker); err != nil || marker != 1 {
		t.Fatal("marked disposable MySQL required", err)
	}
	name := fmt.Sprintf("cm_oc_readview_%d", time.Now().UnixNano())
	if _, err = root.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	defer root.Exec("DROP DATABASE " + name)
	sess, err := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Password: password, Database: name, MaxOpenConns: 4, MaxIdleConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	raw, err := os.ReadFile("../services/testdata/opencode_rollout14_preserve.json")
	if err != nil {
		t.Fatal(err)
	}
	preflight := string(raw)
	var base map[string]any
	if err = json.Unmarshal(raw, &base); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewRuntimeRolloutRepository(sess)
	r := &models.RuntimeRollout{ID: 14, RuntimeType: "opencode", Status: "running", Phase: "opencode_native_preserve", TargetImageRef: base["target_image"].(string), PreflightJSON: &preflight, BatchSize: 1, MaxUnavailable: 2}
	if err = repo.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	svc := services.NewRuntimeUpgradeService(sess, repo, &runtimePoolHandlerPodRepo{}, &runtimePoolHandlerBindingRepo{}, nil, "/fixtures", "")
	h := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{}, &runtimePoolHandlerBindingRepo{}, repo, nil, &runtimePoolHandlerEvents{})
	h.SetUpgradeService(svc)
	router := runtimePoolHandlerRouter(1, "admin", h)
	router.GET("/api/v1/admin/runtime-rollouts/:id", h.GetRollout)
	stages := []string{"standby", "standby_ready", "quiesce", "preserve", "activate", "restart", "switch", "retire", "verify", "rollback_stop", "rollback_restore", "rollback_restart", "rollback_retire", "rollback_verify", "force_zero", "force_replace", "force_ready"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			var p map[string]any
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			p["stage"] = stage
			if strings.HasPrefix(stage, "force_") {
				p["force_retry"] = map[string]any{"audit_id": 1, "request_id": "81e247de-ef46-41ad-853e-0bb20524303e", "actor_user_id": 1, "old_target_image_ref": base["target_image"], "target_image_ref": base["target_image"], "native_evidence": "fixture-private-do-not-return", "lease_evidence": "fixture-private-do-not-return"}
			}
			if stage == "standby" || strings.HasPrefix(stage, "force_") {
				p["targets"] = []any{}
				p["target_controllers"] = []any{}
			}
			b, _ := json.Marshal(p)
			checkpoint := string(b)
			if err := repo.(interface {
				SaveRolloutCheckpoint(context.Context, int64, string) error
			}).SaveRolloutCheckpoint(context.Background(), r.ID, checkpoint); err != nil {
				t.Fatal(err)
			}
			message := ""
			if stage == "preserve" {
				message = "OpenCode native payload must be an immutable root-owned regular file"
			}
			if _, err := sess.SQL().Exec("UPDATE runtime_rollouts SET error_message=? WHERE id=?", message, r.ID); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/runtime-rollouts/%d", r.ID), nil))
			if rec.Code != 200 {
				t.Fatalf("GET native stage=%s returned %d: %s", stage, rec.Code, rec.Body.String())
			}
			var envelope struct {
				Data struct {
					Native        map[string]any `json:"native_operation"`
					PoolProgress  any            `json:"pool_progress"`
					PoolOperation any            `json:"pool_operation"`
					Execution     any            `json:"execution"`
				}
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			v := envelope.Data.Native
			if v == nil || v["stage"] != stage || v["protection_held"] != true || v["blocked"] != (stage == "preserve") {
				t.Fatalf("wrong native view: %s", rec.Body.String())
			}
			if envelope.Data.PoolProgress != nil || envelope.Data.PoolOperation != nil || envelope.Data.Execution != nil {
				t.Fatal("native read view borrowed generic/OpenClaw execution policy")
			}
			if strings.HasPrefix(stage, "force_") {
				attempt, ok := v["force_retry"].(map[string]any)
				if !ok || attempt["confirmed"] != true || attempt["request_id"] != "81e247de-ef46-41ad-853e-0bb20524303e" || attempt["stage"] != stage {
					t.Fatalf("lost force observation: %+v", v)
				}
			}
			if stage != "standby" && !strings.HasPrefix(stage, "force_") {
				targets, ok := v["targets"].([]any)
				if !ok || len(targets) != 1 || targets[0].(map[string]any)["pod_uid"] != "91fcf4b2-ffc9-42a7-af95-00ae38aba9cc" {
					t.Fatalf("lost actual target identity: %+v", v)
				}
			}
			if stage == "preserve" {
				rows := v["instances"].([]any)
				if len(rows) != 2 || rows[0].(map[string]any)["state"] != "quiesced" || v["error_message"] != message {
					t.Fatalf("blocked source state absent: %+v", v)
				}
			}
			if strings.Contains(rec.Body.String(), "fixture-private-do-not-return") || strings.Contains(rec.Body.String(), "private-workspace-do-not-return") {
				t.Fatal("native view leaked private checkpoint payload")
			}
			latest, err := repo.GetByID(context.Background(), r.ID)
			if err != nil || latest.Status != "running" || !svc.InstanceBlocked(context.Background(), "opencode", 395) {
				t.Fatalf("GET changed active native guard: %+v %v", latest, err)
			}
			stored, err := repo.(interface {
				LoadRolloutCheckpoint(context.Context, int64) (string, error)
			}).LoadRolloutCheckpoint(context.Background(), r.ID)
			if err != nil || stored != checkpoint {
				t.Fatal("GET changed frozen native checkpoint")
			}
		})
	}
}
