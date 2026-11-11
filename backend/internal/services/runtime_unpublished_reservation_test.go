package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
)

func TestUnpublishedFirstReservationRetiresOnlyAbsentUnpublishedSource(t *testing.T) {
	for _, scenario := range []string{"absent", "publication-race", "source-present", "source-read-error", "published-cm", "credential", "unlabelled-credential", "later-generation", "changed-owner", "changed-generation", "changed-binding", "changed-pod", "gateway-pid", "manual-guard", "history", "protected-marker", "pending-failover", "failover-begin-race", "failover-save-race", "failover-user", "failover-generation", "failover-pod", "failover-uid", "failover-endpoint", "failover-gateway", "failover-port", "failover-runtime", "failover-workspace", "failover-stopped", "failover-published", "failover-credential"} {
		t.Run(scenario, func(t *testing.T) {
			s, kube, helper := recoveryFlowFixture(t, "creating")
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := s.sess.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			migration, err := os.ReadFile("../db/migrations/068_add_workspace_write_intents.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range strings.Split(string(migration), ";") {
				if strings.TrimSpace(statement) != "" {
					exec(statement)
				}
			}
			exec("ALTER TABLE instances ADD COLUMN runtime_security_profile VARCHAR(64) NOT NULL DEFAULT ''")
			exec("UPDATE instances SET runtime_generation=1,workspace_path='/workspaces/opencode/user-1/instance-101' WHERE id=101")
			exec("UPDATE instance_runtime_bindings SET generation=1,gateway_id='pending-101-1',state='creating' WHERE instance_id=101")
			ctx := context.Background()
			if err := kube.CoreV1().ConfigMaps("runtime").Delete(ctx, "lite-instance-101", metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := kube.CoreV1().Secrets("runtime").Delete(ctx, "lite-instance-101-g4", metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			if scenario != "source-present" {
				if err := kube.CoreV1().Pods("runtime").Delete(ctx, "pool-old", metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			instance, err := s.scheduler.instanceRepo.GetByID(101)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := s.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
			if err != nil {
				t.Fatal(err)
			}
			pendingFailover := models.RuntimeFailoverIntent{InstanceID: 101, UserID: 1, Generation: 1, RuntimeType: "opencode", SourcePodID: 1, SourcePodUID: "old", SourceEndpoint: "https://10.1.0.2:19090", GatewayID: "pending-101-1", GatewayPort: 20000, WorkspacePath: "/workspaces/opencode/user-1/instance-101", Reason: "heartbeat lost"}
			store := s.scheduler.instanceRepo.(repository.RuntimeFailoverRepository)
			hasFailover := scenario == "pending-failover" || strings.HasPrefix(scenario, "failover-") && scenario != "failover-begin-race"
			if hasFailover {
				if _, e := store.BeginRuntimeFailover(ctx, pendingFailover); e != nil {
					t.Fatal(e)
				}
			}
			var publicationDone chan error
			published := false
			switch scenario {
			case "publication-race", "failover-begin-race", "failover-save-race":
				pod, getErr := s.scheduler.podRepo.GetByID(ctx, binding.RuntimePodID)
				if getErr != nil {
					t.Fatal(getErr)
				}
				publicationDone = make(chan error, 1)
				kube.PrependReactor("get", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
					started := make(chan struct{})
					go func() {
						close(started)
						switch scenario {
						case "failover-begin-race":
							_, e := store.BeginRuntimeFailover(ctx, pendingFailover)
							publicationDone <- e
						case "failover-save-race":
							late := pendingFailover
							late.SourceStopped = true
							publicationDone <- store.SaveRuntimeFailover(ctx, late)
						default:
							publicationDone <- repository.WithRuntimePodAssignmentAdmission(ctx, s.sess, *pod, 101, 1, func() error { published = true; return nil })
						}
					}()
					<-started
					select {
					case e := <-publicationDone:
						t.Fatalf("publication crossed rollback admission fence: %v", e)
					case <-time.After(50 * time.Millisecond):
					}
					return false, nil, nil
				})
			case "source-read-error":
				kube.PrependReactor("get", "pods", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("unavailable") })
			case "published-cm", "failover-published":
				_, err = kube.CoreV1().ConfigMaps("runtime").Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-101"}}, metav1.CreateOptions{})
			case "credential", "unlabelled-credential", "failover-credential":
				name := "lite-instance-101-g1"
				labels := map[string]string{"clawmanager.io/instance-id": "101"}
				if scenario == "unlabelled-credential" {
					name = "lite-instance-101-g99"
					labels = nil
				}
				_, err = kube.CoreV1().Secrets("runtime").Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}, metav1.CreateOptions{})
			case "later-generation":
				instance.RuntimeGeneration = 2
				binding.Generation = 2
				binding.GatewayID = "pending-101-2"
			case "changed-owner":
				exec("UPDATE instances SET user_id=9 WHERE id=101")
			case "changed-generation":
				exec("UPDATE instances SET runtime_generation=2 WHERE id=101")
			case "protected-marker":
				// Publication evidence must be checked from SQL, even if the caller's snapshot is stale.
				exec("UPDATE instances SET runtime_security_profile='isolated-network-v1' WHERE id=101")
			case "changed-binding":
				exec("UPDATE instance_runtime_bindings SET gateway_port=20006 WHERE instance_id=101")
			case "changed-pod":
				exec("UPDATE runtime_pods SET pod_uid=NULL WHERE id=1")
			case "gateway-pid":
				exec("UPDATE instance_runtime_bindings SET gateway_pid=123 WHERE instance_id=101")
			case "manual-guard":
				exec("INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(101,'other',NOW())")
			case "failover-user":
				exec("UPDATE runtime_failover_intents SET user_id=2 WHERE instance_id=101")
			case "failover-generation":
				exec("UPDATE runtime_failover_intents SET generation=2 WHERE instance_id=101")
			case "failover-pod":
				exec("UPDATE runtime_failover_intents SET source_pod_id=2 WHERE instance_id=101")
			case "failover-uid":
				exec("UPDATE runtime_failover_intents SET source_pod_uid='OLD' WHERE instance_id=101")
			case "failover-endpoint":
				exec("UPDATE runtime_failover_intents SET source_endpoint='https://10.1.0.3:19090' WHERE instance_id=101")
			case "failover-gateway":
				exec("UPDATE runtime_failover_intents SET gateway_id='gw-101-1' WHERE instance_id=101")
			case "failover-port":
				exec("UPDATE runtime_failover_intents SET gateway_port=20003 WHERE instance_id=101")
			case "failover-runtime":
				exec("UPDATE runtime_failover_intents SET runtime_type='OPENCODE' WHERE instance_id=101")
			case "failover-workspace":
				exec("UPDATE runtime_failover_intents SET workspace_path='/workspaces/opencode/user-2/instance-101' WHERE instance_id=101")
			case "failover-stopped":
				exec("UPDATE runtime_failover_intents SET source_stopped=TRUE WHERE instance_id=101")
			case "history":
				exec("INSERT INTO runtime_failover_intents(instance_id,user_id,generation,runtime_type,source_pod_id,source_pod_uid,source_endpoint,gateway_id,gateway_port,workspace_path,reason,updated_at) VALUES(101,1,1,'opencode',1,'old','https://10.1.0.2:19090','gw-101-1',20000,'/workspaces/opencode/user-1/instance-101','unknown',NOW())")
			}
			if err != nil {
				t.Fatal(err)
			}
			release, err := s.scheduler.instanceRepo.(interface {
				AcquireLifecycleLock(context.Context, int) (func(), error)
			}).AcquireLifecycleLock(ctx, 101)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			err = s.scheduler.retryUnpublishedPendingSlot(ctx, s.scheduler.agentClient.(*slotRuntimeAgentClient), *instance, binding)
			var count, slots, generation int
			row, _ := s.sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=101")
			if e := row.Scan(&count); e != nil {
				t.Fatal(e)
			}
			row, _ = s.sess.SQL().QueryRow("SELECT used_slots FROM runtime_pods WHERE id=1")
			if e := row.Scan(&slots); e != nil {
				t.Fatal(e)
			}
			row, _ = s.sess.SQL().QueryRow("SELECT runtime_generation FROM instances WHERE id=101")
			if e := row.Scan(&generation); e != nil {
				t.Fatal(e)
			}
			if publicationDone != nil {
				select {
				case e := <-publicationDone:
					if e == nil || published {
						t.Fatal("delayed publisher bypassed removed reservation", e)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("publication remained blocked after rollback")
				}
			}
			success := scenario == "absent" || scenario == "publication-race" || scenario == "pending-failover" || scenario == "failover-begin-race" || scenario == "failover-save-race"
			if success {
				if err != nil || count != 0 || slots != 1 || generation != 1 {
					t.Fatalf("unpublished reservation stuck: err=%v binding=%d slots=%d generation=%d", err, count, slots, generation)
				}
			} else if err == nil || count != 1 || slots != 2 {
				t.Fatalf("unsafe rollback %s: err=%v binding=%d slots=%d", scenario, err, count, slots)
			}
			if hasFailover || scenario == "failover-begin-race" {
				intent, e := store.GetRuntimeFailover(ctx, 101)
				if e != nil {
					t.Fatal(e)
				}
				if success && intent != nil || !success && intent == nil {
					t.Fatalf("wrong failover retirement: success=%v intent=%+v", success, intent)
				}
			}
			if len(helper.scopes) != 0 {
				t.Fatal("unpublished reservation manufactured a stop request")
			}
			if scenario == "protected-marker" {
				var profile string
				row, _ = s.sess.SQL().QueryRow("SELECT runtime_security_profile FROM instances WHERE id=101")
				if e := row.Scan(&profile); e != nil || profile != "isolated-network-v1" || generation != 1 {
					t.Fatalf("publication evidence changed: profile=%q generation=%d err=%v", profile, generation, e)
				}
			}
			if _, err = kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-102", metav1.GetOptions{}); err != nil {
				t.Fatal("neighbor changed", err)
			}
		})
	}
}
