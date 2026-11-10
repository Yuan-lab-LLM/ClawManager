package services

import (
	"testing"
	"time"

	"clawreef/internal/repository"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestModelActivityControllerReconcilesOnlyVerifiedScopes(t *testing.T) {
	for _, oldProcessLive := range []bool{false, true} {
		t.Run(map[bool]string{false: "old-process-exited", true: "old-process-still-terminating"}[oldProcessLive], func(t *testing.T) {
			s := idleLifecycleDB(t)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := s.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(1,7,'opencode','gateway','lite','stopped',2)`)
			store := repository.NewInstanceAutoStopRepository(s)
			if err := store.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			anchor := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
			exec(`UPDATE instance_model_activity SET last_gateway_activity_at=? WHERE instance_id=1`, anchor)
			exec(`INSERT INTO instance_model_activity_requests(request_id,instance_id,generation,started_at,lease_expires_at,last_heartbeat_at) VALUES('legacy',1,2,?,NULL,NULL),('expired',1,2,?,?,?),('healthy',1,2,?,?,?)`, anchor, anchor, anchor.Add(time.Minute), anchor, anchor, time.Now().UTC().Add(time.Minute), time.Now().UTC())
			deployment, rs, pod := legacyProofObjects()
			pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(anchor.Add(5 * time.Minute))
			objects := []runtime.Object{deployment, rs, pod}
			if oldProcessLive {
				objects = append(objects, legacyProofOldReplica(true)(deployment, rs, pod)...)
			}
			controller := &InstanceAutoStopService{store: store, lifecycle: &instanceService{}, kube: fake.NewSimpleClientset(objects...)}
			controller.SetApplicationPodIdentity("cm", pod.Name)
			exec(`UPDATE instance_auto_stop_settings SET check_interval_seconds=60,idle_timeout_seconds=300,next_check_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND`)
			if err := controller.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			activity, err := store.GetActivity(t.Context(), 1)
			want := 1
			if oldProcessLive {
				want = 2
			}
			if err != nil || activity.ActiveRequests != want || !activity.LastGatewayActivityAt.Equal(anchor) {
				t.Fatalf("activity=%+v want active=%d err=%v", activity, want, err)
			}
			row, err := s.SQL().QueryRow(`SELECT COUNT(*) FROM instance_model_activity_requests WHERE request_id='expired' AND completion_reason='lease_expired' AND finished_at IS NOT NULL`)
			if err != nil {
				t.Fatal(err)
			}
			var count int
			if err = row.Scan(&count); err != nil || count != 1 {
				t.Fatalf("expired receipt=%d err=%v", count, err)
			}
		})
	}
}
