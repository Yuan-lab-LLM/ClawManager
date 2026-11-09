package services

import (
	"context"
	"net/http"
	"testing"

	"clawreef/internal/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOlderFailoverIntentDoesNotBlockAuthoritativeNewFailedGeneration(t *testing.T) {
	for _, scenario := range []string{"manual", "new-failover", "missing-binding", "unpublished", "assignment-owner-changed", "missing-publication", "different-owner", "same-generation"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			f := newPendingSlotReplayFixture(t)
			f.seed(t, 2, "running")
			f.binding.GatewayID = "gw-7-2"
			f.binding.State = "error"
			f.instance.Status = "error"
			prior := models.RuntimeFailoverIntent{InstanceID: 7, UserID: 2, Generation: 1, RuntimeType: RuntimeTypeOpenClaw, SourcePodID: 8, SourcePodUID: "old-uid", SourceEndpoint: "https://10.0.0.3:19090", GatewayID: "gw-7-1", GatewayPort: 20000, WorkspacePath: f.binding.WorkspacePath, Attempts: 6}
			repo := &failoverIntentInstanceRepo{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo(), intents: map[int]models.RuntimeFailoverIntent{7: prior}}
			repo.byID[7] = &f.instance
			f.scheduler.instanceRepo = repo
			switch scenario {
			case "missing-binding":
				delete(f.bindings.bindings, 7)
			case "unpublished":
				f.binding.GatewayID = "pending-7-2"
			case "assignment-owner-changed":
				cm, _ := f.kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
				cm.Data["user_id"] = "999"
				_, _ = f.kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
			case "missing-publication":
				_ = f.kube.CoreV1().ConfigMaps("runtime").Delete(ctx, "lite-instance-7", metav1.DeleteOptions{})
			case "different-owner":
				f.instance.UserID = 999
			case "same-generation":
				prior.Generation = 2
				prior.GatewayID = "gw-7-2"
				repo.intents[7] = prior
			}
			stops := 0
			f.client.httpClient = &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) { stops++; return nil, context.DeadlineExceeded })}
			if scenario == "new-failover" {
				err := f.scheduler.beginOwnedRuntimeFailover(ctx, repo, f.instance, *f.binding, *f.pod, "new failed source")
				if err == nil || repo.intents[7].Generation != 2 || stops != 1 {
					t.Fatalf("new source did not acquire its own intent: state=%+v stops=%d err=%v", repo.intents[7], stops, err)
				}
				return
			}
			handled, err := f.scheduler.RetryOwnedRuntimeFailoverLocked(ctx, f.instance)
			if scenario == "manual" {
				if handled || err != nil || len(repo.intents) != 0 || stops != 0 {
					t.Fatalf("old intent blocked current generation recovery: handled=%v err=%v intents=%+v stops=%d", handled, err, repo.intents, stops)
				}
			} else {
				if !handled || err == nil || len(repo.intents) != 1 || stops != 0 {
					t.Fatalf("uncertain supersession discarded old ownership: handled=%v err=%v intents=%+v stops=%d", handled, err, repo.intents, stops)
				}
			}
		})
	}
}

func (r *failoverIntentInstanceRepo) RetireRuntimeFailover(_ context.Context, prior, successor models.RuntimeFailoverIntent) (bool, error) {
	if r.intents[prior.InstanceID].Generation != prior.Generation || successor.Generation <= prior.Generation || successor.UserID != prior.UserID {
		return false, nil
	}
	delete(r.intents, prior.InstanceID)
	return true, nil
}
