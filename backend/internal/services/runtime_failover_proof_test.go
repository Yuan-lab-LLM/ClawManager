package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestSlotFailoverRequiresConfirmedStopBeforeAdvancingGeneration(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		name := "unconfirmed"
		if confirmed {
			name = "confirmed"
		}
		t.Run(name, func(t *testing.T) {
			instances := newFakeRuntimeInstanceRepo()
			instances.byID[17] = &models.Instance{ID: 17, RuntimeGeneration: 3, Status: "running"}
			bindings := newFakeRuntimeBindingRepo()
			binding := models.InstanceRuntimeBinding{InstanceID: 17, RuntimePodID: 9, Generation: 3, GatewayID: "gw-17-3", State: "running"}
			bindings.bindings[17] = &binding
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: {ID: 9, PodUID: stringPtrOrNil("source-uid"), AgentEndpoint: stringPtrOrNil("https://10.0.0.9:19090")}}}
			stopErr := errors.New("source process teardown is unconfirmed")
			base := &fakeRuntimeAgentClient{}
			if !confirmed {
				base.deleteErr = stopErr
			}
			agent := &strictStopRuntimeAgent{fakeRuntimeAgentClient: base, strict: true}
			scheduler := NewRuntimeScheduler(instances, pods, bindings, nil, agent, nil, nil, nil, time.Second)
			err := scheduler.failoverBinding(context.Background(), 9, binding, "heartbeat lost")
			if !confirmed {
				if !errors.Is(err, stopErr) {
					t.Fatalf("failover ignored missing teardown evidence: %v", err)
				}
				if bindings.bindings[17] == nil || pods.releases[9] != 0 || len(instances.runtimeStates) != 0 {
					t.Fatal("unconfirmed source changed SQL generation, binding, or slot ownership")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(base.deleteRequests) != 1 || bindings.bindings[17] != nil || pods.releases[9] != 1 || instances.runtimeStates[17].generation != 4 {
				t.Fatal("confirmed failover did not retire source before advancing")
			}
		})
	}
}
