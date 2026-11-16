package services

import (
	"clawreef/internal/repository"
	"context"
	"encoding/json"
	"github.com/upper/db/v4"
	"testing"
)

func TestRuntimeTypedControlAdmissionMySQL(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"scale_in", "migration", "balance", "eviction", "rollout_wake"} {
		t.Run(kind, func(t *testing.T) {
			sess := runtimeManagementMySQLDB(t)
			service := runtimeManagementMySQLService(sess)
			if _, err := sess.SQL().Exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('held',?,'needs_attention','{}','[{"source_placement":{"runtime_type":"deepseek-harness"}}]',NOW(),NOW())`, kind); err != nil {
				t.Fatal(err)
			}
			maintenance := &RuntimePodMaintenanceService{sess: sess}
			for _, runtimeType := range []string{"openclaw", "deepseek-harness"} {
				for name, check := range map[string]func() error{
					"scale and wake": func() error { return service.scaleInAdmission(ctx, sess, runtimeType) },
					"create": func() error {
						return maintenance.createAdmission(ctx, runtimeType, func(db.Session) error { return nil })
					},
					"delete":      func() error { return maintenance.admissionTx(ctx, func(db.Session) error { return nil }, runtimeType) },
					"preparation": func() error { return repository.CheckRuntimePreparationReservationsForType(ctx, sess, "", runtimeType) },
				} {
					err := check()
					blocked := runtimeType == "deepseek-harness" && (name != "preparation" || kind == "rollout_wake") && (name != "create" || kind == "scale_in" || kind == "rollout_wake")
					if (err != nil) != blocked {
						t.Fatalf("%s %s blocked=%v: %v", runtimeType, name, blocked, err)
					}
				}
			}
			if _, err := service.enqueue(ctx, "migration", RuntimeSchedulingRequest{}, []RuntimeSchedulingItem{{SourcePlacement: &RuntimeEvictionPlacement{RuntimeType: "openclaw"}}}, nil); err != nil {
				t.Fatalf("DSH blocked OpenClaw scheduling: %v", err)
			}
		})
	}
}

func TestRuntimeTypedReadyPreparationMySQL(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	if _, err := sess.SQL().Exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('ready','rollout_wake','completed',JSON_OBJECT('runtime_type','deepseek-harness','phase','ready','ready_until_unix',UNIX_TIMESTAMP()+3600),'[]',NOW(),NOW())`); err != nil {
		t.Fatal(err)
	}
	service := runtimeManagementMySQLService(sess)
	if err := service.scaleInAdmission(ctx, sess, "openclaw"); err != nil {
		t.Fatalf("DSH reservation blocked OpenClaw: %v", err)
	}
	if err := service.scaleInAdmission(ctx, sess, "deepseek-harness"); err == nil {
		t.Fatal("same-type reservation admitted")
	}
	if _, err := sess.SQL().Exec(`UPDATE runtime_management_operations SET request_json='{}' WHERE id='ready'`); err != nil {
		t.Fatal(err)
	}
	// A still-active unknown legacy journal blocks every type explicitly.
	if _, err := sess.SQL().Exec(`UPDATE runtime_management_operations SET status='needs_attention' WHERE id='ready'`); err != nil {
		t.Fatal(err)
	}
	if err := service.scaleInAdmission(ctx, sess, "openclaw"); err == nil {
		t.Fatal("unknown legacy operation admitted")
	}
}

func TestRuntimePrepareIgnoresOtherTypeHeldOperationMySQL(t *testing.T) {
	sess, s, _, _, _ := completedRolloutSleepFixture(t)
	if _, err := sess.SQL().Exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('held-dsh','eviction','needs_attention','{}','[{"source_placement":{"runtime_type":"deepseek-harness"}}]',NOW(),NOW())`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareRollout(context.Background(), "hermes", nil); err != nil {
		t.Fatalf("DSH blocked Hermes preparation: %v", err)
	}
}

func TestRuntimeHeldScaleInDoesNotBlockOtherTypeWorkerMySQL(t *testing.T) {
	sess, s, _, _, _ := completedRolloutSleepFixture(t)
	journal := runtimePodScaleInJournal{RuntimePodScaleInRequest: RuntimePodScaleInRequest{RuntimeType: "deepseek-harness"}, Phase: "observing"}
	raw, _ := json.Marshal(journal)
	if _, err := sess.SQL().Exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('held-dsh','scale_in','needs_attention',?,'[]',NOW(),NOW())`, string(raw)); err != nil {
		t.Fatal(err)
	}
	op, err := s.PrepareRollout(context.Background(), "hermes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, _, _, err := s.runtimeManagementOperation(context.Background(), op.ID)
	if err != nil || loaded.Status != "starting" {
		t.Fatalf("other-type pending preparation did not advance: %+v %v", loaded, err)
	}
	var status string
	row, _ := sess.SQL().QueryRow("SELECT status FROM runtime_management_operations WHERE id='held-dsh'")
	if err = row.Scan(&status); err != nil || status != "needs_attention" {
		t.Fatalf("held protection changed: %s %v", status, err)
	}
}
