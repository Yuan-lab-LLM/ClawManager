package services

import (
	"context"
	"strings"
	"testing"
)

func TestRuntimeOrphanAdmissionRejectsQueuedAndReplacementSourcesMySQL(t *testing.T) {
	for _, kind := range []string{"queued", "replacement", "batch", "retry"} {
		t.Run(kind, func(t *testing.T) {
			s, _, backend := orphanFlowFixture(t)
			ctx := context.Background()
			exec := func(q string, args ...interface{}) {
				t.Helper()
				if _, e := s.sess.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec("INSERT INTO users(id) VALUES(1)")
			if kind == "replacement" {
				exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(166,1,'opencode','gateway','lite','creating',1,NOW())")
			} else {
				exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'opencode','gateway','lite','stopped',4,NOW())")
			}
			id := 101
			state := "queued"
			payload := `{"instance_id":101,"new_name":"replacement"}`
			if kind == "replacement" {
				id = 166
			}
			if kind == "batch" || kind == "retry" {
				state = "completed"
			}
			exec(`INSERT INTO northbound_operations(operation_id,user_id,session_id,operation_type,idempotency_key_hash,request_hash,request_payload,status,available_at,instance_id,created_at,updated_at) VALUES('reset-op',1,'session','reset','idem','hash',?,?,NOW(),?,NOW(),NOW())`, payload, state, id)
			if kind == "batch" || kind == "retry" {
				exec(`INSERT INTO instance_lifecycle_batches(batch_id,user_id,action,status,request_hash,created_at,updated_at,policy_version,retry_round) VALUES('batch',1,'reset','running','hash',NOW(),NOW(),2,0)`)
				itemState := "active"
				if kind == "retry" {
					itemState = "failed"
				}
				exec(`INSERT INTO instance_lifecycle_batch_items(batch_id,source_id,source_name,runtime_type,operation_id,runtime_pod_id,state) VALUES('batch',101,'source','opencode','reset-op',1,?)`, itemState)
			}
			if _, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil); e == nil || !strings.Contains(e.Error(), "lifecycle") {
				t.Fatalf("unfinished source lifecycle admitted: %v", e)
			}
			var count int
			row, e := s.sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations")
			if e != nil {
				t.Fatal(e)
			}
			if e = row.Scan(&count); e != nil {
				t.Fatal(e)
			}
			if count != 0 || backend.retireCalls != 0 {
				t.Fatal("queued source received orphan stop effects")
			}
		})
	}
}
