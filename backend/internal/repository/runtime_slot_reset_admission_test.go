package repository

import (
	"context"
	"errors"
	"testing"

	"clawreef/internal/models"
)

func TestSlotResetRepositoryAdmissionMySQL(t *testing.T) {
	for _, scenario := range []string{"active-reservation", "other-port-reservation", "retired-report", "replacement-pod", "successor-report", "stale-running", "stale-error", "publish-old-port", "changed-without-tombstone", "current-canonical", "pending-alias", "legacy-binding-health"} {
		t.Run(scenario, func(t *testing.T) {
			r := protectedReportRepository(t)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := r.sess.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("INSERT INTO runtime_slot_reset_requests(request_id,operation_id,pod_uid,slot,source_pod_id,gateway_port,instance_id,source_generation,source_gateway_id,active,created_at) VALUES('req','op','pod-current','slot-0',5,20000,7,2,'gw-7-2',1,NOW(6))")
			report := models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 5, RuntimeType: "hermes", GatewayID: "gw-7-2", GatewayPort: 20000, WorkspacePath: "/workspaces/hermes/user-3/instance-7", State: "running", Generation: 2}
			ctx := context.Background()
			switch scenario {
			case "active-reservation", "other-port-reservation":
				report.GatewayID = "pending-7-2"
				report.State = "creating"
				if scenario == "other-port-reservation" {
					report.GatewayPort += 3
				}
				err := r.Create(ctx, &report)
				if errors.Is(err, ErrRuntimeSlotResetFenced) != (scenario == "active-reservation") || scenario == "other-port-reservation" && err != nil {
					t.Fatalf("unexpected reservation result: %v", err)
				}
			case "retired-report", "replacement-pod", "successor-report", "pending-alias":
				exec("UPDATE runtime_slot_reset_requests SET active=NULL")
				uid := "pod-current"
				if scenario == "pending-alias" {
					exec("UPDATE runtime_slot_reset_requests SET source_gateway_id='pending-7-2'")
				}
				if scenario == "replacement-pod" {
					uid = "replacement"
					exec("UPDATE runtime_pods SET pod_uid=? WHERE id=5", uid)
				}
				if scenario == "successor-report" {
					report.GatewayPort += 3
				}
				ok, e := r.ReconcileUnboundProtectedReportedGateway(ctx, &report, 3, uid, 3)
				if e != nil || ok != (scenario != "retired-report" && scenario != "pending-alias") {
					t.Fatalf("unexpected report outcome accepted=%v err=%v", ok, e)
				}
			case "stale-running", "stale-error", "changed-without-tombstone", "current-canonical", "legacy-binding-health":
				if scenario == "changed-without-tombstone" || scenario == "current-canonical" || scenario == "legacy-binding-health" {
					exec("DELETE FROM runtime_slot_reset_requests")
				}
				exec("UPDATE runtime_slot_reset_requests SET active=NULL")
				current := report
				current.GatewayPort += 3
				if e := r.create(ctx, &current); e != nil {
					t.Fatal(e)
				}
				old := report
				old.ID = current.ID
				if scenario == "current-canonical" || scenario == "legacy-binding-health" {
					old = current
					report = current
					exec("ALTER TABLE runtime_management_operations ADD id VARCHAR(64)")
					exec("CREATE TABLE instance_lifecycle_manual_guards(instance_id INT PRIMARY KEY,token VARCHAR(64))")
				}
				if scenario == "stale-error" || scenario == "changed-without-tombstone" || scenario == "current-canonical" {
					report.State = "error"
				}
				if scenario == "legacy-binding-health" {
					report.WorkspacePath = ""
					report.State = "error"
				}
				if e := r.ApplyReportedGateway(ctx, old, report, report.State); e != nil {
					t.Fatal(e)
				}
				got, e := r.GetByInstanceID(ctx, 7)
				if e != nil {
					t.Fatal(e)
				}
				want := "running"
				if scenario == "current-canonical" {
					want = "error"
				}
				var status string
				row, e := r.sess.SQL().QueryRow("SELECT status FROM instances WHERE id=7")
				if e != nil {
					t.Fatal(e)
				}
				if e = row.Scan(&status); e != nil {
					t.Fatal(e)
				}
				wantBinding := want
				if scenario == "legacy-binding-health" {
					wantBinding = "error"
				}
				if got == nil || got.GatewayPort != current.GatewayPort || got.State != wantBinding || status != want {
					t.Fatalf("successor modified: %+v %v", got, e)
				}
			case "publish-old-port":
				exec("ALTER TABLE runtime_pods ADD pod_name VARCHAR(64) DEFAULT 'pod'")
				if e := r.create(ctx, &report); e != nil {
					t.Fatal(e)
				}
				uid := "pod-current"
				called := false
				err := WithRuntimePodAssignmentAdmission(ctx, r.sess, models.RuntimePod{PodUID: &uid, Namespace: "runtime", PodName: "pod", DeploymentName: "pool", RuntimeType: "hermes"}, 7, 2, 20000, func() error { called = true; return nil })
				if called || err == nil {
					t.Fatalf("fenced source published: called=%v err=%v", called, err)
				}
			}
		})
	}
}

func TestUnknownSlotResetRequiresDurableReservationMySQL(t *testing.T) {
	for _, scenario := range []string{"protected-unbound", "legacy-unbound", "legacy-replaces-pending", "same-pod-other-port", "new-pod-uid", "ordinary-create", "new-published-report", "exact-protected-pending"} {
		t.Run(scenario, func(t *testing.T) {
			r := protectedReportRepository(t)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := r.sess.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("ALTER TABLE runtime_management_operations ADD id VARCHAR(64), ADD items_json LONGTEXT")
			exec("CREATE TABLE instance_lifecycle_manual_guards(instance_id INT PRIMARY KEY,token VARCHAR(64))")
			exec("INSERT INTO runtime_slot_reset_requests(request_id,operation_id,pod_uid,slot,source_pod_id,gateway_port,instance_id,source_generation,source_gateway_id,active,created_at) VALUES('unknown','reset','pod-current','slot-0',5,20000,0,0,'',NULL,NOW(6))")
			exec("UPDATE instances SET status='creating' WHERE id=7")
			report := models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 5, RuntimeType: "hermes", GatewayID: "gw-7-2", GatewayPort: 20000, WorkspacePath: "/workspaces/hermes/user-3/instance-7", State: "running", Generation: 2}
			ctx := t.Context()
			mustAccept := false
			var accepted bool
			var err error
			switch scenario {
			case "legacy-replaces-pending", "legacy-unbound":
				if scenario == "legacy-replaces-pending" {
					pending := report
					pending.GatewayID = "pending-7-2"
					pending.State = "creating"
					pending.GatewayPort += 3
					if e := r.create(ctx, &pending); e != nil {
						t.Fatal(e)
					}
				}
				accepted, err = r.ReconcileReportedGateway(ctx, &report, 2, 3)
			case "protected-unbound", "new-pod-uid", "same-pod-other-port":
				uid := "pod-current"
				if scenario == "new-pod-uid" {
					uid = "replacement"
					exec("UPDATE runtime_pods SET pod_uid=? WHERE id=5", uid)
				}
				if scenario == "same-pod-other-port" {
					report.GatewayPort += 3
				}
				mustAccept = scenario != "protected-unbound"
				accepted, err = r.ReconcileUnboundProtectedReportedGateway(ctx, &report, 3, uid, 3)
			case "ordinary-create", "new-published-report", "exact-protected-pending":
				pending := report
				pending.GatewayID = "pending-7-2"
				pending.State = "creating"
				if err = r.Create(ctx, &pending); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "new-published-report":
					if e := r.UpdateGatewayAssignment(ctx, 7, 2, report.GatewayID, nil, "running", nil); e != nil {
						t.Fatal(e)
					}
					report.ID = pending.ID
					err = r.ApplyReportedGateway(ctx, report, report, "running")
					accepted = err == nil
				case "exact-protected-pending":
					accepted, err = r.ReconcilePendingReportedGateway(ctx, &report, 3, 3)
				default:
					accepted = true
				}
				mustAccept = true
			}
			if err != nil || accepted != mustAccept {
				t.Fatalf("report accepted=%v, want=%v, err=%v", accepted, mustAccept, err)
			}
			got, e := r.GetByInstanceID(ctx, 7)
			if e != nil {
				t.Fatal(e)
			}
			if !mustAccept {
				if scenario == "legacy-replaces-pending" {
					if got == nil || got.GatewayID != "pending-7-2" || got.GatewayPort != 20003 {
						t.Fatalf("adjacent pending binding changed: %+v", got)
					}
				} else if got != nil {
					t.Fatalf("old binding was recreated: %+v", got)
				}
				var status string
				row, e := r.sess.SQL().QueryRow("SELECT status FROM instances WHERE id=7")
				if e != nil {
					t.Fatal(e)
				}
				if e = row.Scan(&status); e != nil || status != "creating" {
					t.Fatalf("stale report changed status=%s err=%v", status, e)
				}
			} else if got == nil {
				t.Fatal("new binding missing")
			}
		})
	}
}
