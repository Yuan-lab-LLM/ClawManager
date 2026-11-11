package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestQueueDeletePersistsNativeIntentBeforeSlowCleanup(t *testing.T) {
	repo := newV2LifecycleInstanceRepo()
	repo.byID[77] = &models.Instance{ID: 77, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "creating", RuntimeGeneration: 1}
	entered, release := make(chan struct{}), make(chan struct{})
	service := &instanceService{instanceRepo: repo, bindingRepo: newFakeRuntimeBindingRepo(), agentClient: &fakeRuntimeAgentClient{}, resourceCleanup: func(context.Context, int) error {
		close(entered)
		<-release
		return errors.New("cleanup must remain pending")
	}}
	if err := service.QueueDelete(77); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("background cleanup did not begin")
	}
	if repo.byID[77].Status != "deleting" || len(repo.deleted) != 0 {
		close(release)
		t.Fatal("deletion intent was not durable before cleanup")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		service.deletionMu.Lock()
		_, active := service.deletionsInFlight[77]
		service.deletionMu.Unlock()
		if !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background cleanup did not release its guard")
		}
		time.Sleep(time.Millisecond)
	}
	if repo.byID[77] == nil || repo.byID[77].Status != "deleting" || len(repo.deleted) != 0 {
		t.Fatal("cleanup failure erased the durable retry intent")
	}
}

func TestQueuedDeletionCannotBeOverwrittenByLifecycleActions(t *testing.T) {
	// Hold every worker before queue admission to exercise the post-202 window.
	for i := 0; i < cap(instanceDeletionWorkers); i++ {
		instanceDeletionWorkers <- struct{}{}
	}
	repo := newV2LifecycleInstanceRepo()
	repo.byID[78] = &models.Instance{ID: 78, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "stopped", RuntimeGeneration: 1}
	service := &instanceService{instanceRepo: repo, bindingRepo: newFakeRuntimeBindingRepo(), agentClient: &fakeRuntimeAgentClient{}, resourceCleanup: func(context.Context, int) error { return errors.New("retry cleanup later") }}
	t.Cleanup(func() {
		for i := 0; i < cap(instanceDeletionWorkers); i++ {
			<-instanceDeletionWorkers
		}
		deadline := time.Now().Add(time.Second)
		for {
			service.deletionMu.Lock()
			_, active := service.deletionsInFlight[78]
			service.deletionMu.Unlock()
			if !active {
				break
			}
			if time.Now().After(deadline) {
				t.Error("queued worker did not exit")
				break
			}
			time.Sleep(time.Millisecond)
		}
	})
	if err := service.QueueDelete(78); err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct {
		name string
		run  func() error
	}{
		{"start", func() error { return service.Start(78) }},
		{"stop", func() error { return service.Stop(78) }},
		{"restart", func() error { return service.Restart(78) }},
		{"restart-environment", func() error { return service.RestartWithEnvironment(78, map[string]string{"LOG_LEVEL": "debug"}, nil) }},
		{"reset", func() error { return service.Reset(78) }},
	} {
		t.Run(action.name, func(t *testing.T) {
			if err := action.run(); !errors.Is(err, models.ErrInstanceStopping) {
				t.Errorf("queued deletion was not fenced: %v", err)
			}
			if repo.byID[78] == nil || repo.byID[78].Status != "deleting" || repo.byID[78].RuntimeGeneration != 1 {
				t.Fatalf("accepted deletion intent changed: %+v", repo.byID[78])
			}
		})
	}
}
