package services

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type upgradeStopStoreFake struct {
	repository.RuntimeUpgradeStopRepository
	u                     models.RuntimeUpgrade
	stop                  *models.RuntimeUpgradeStop
	events                *[]string
	prepareErr, recordErr error
}

func (s *upgradeStopStoreFake) Get(context.Context, int) (*models.RuntimeUpgrade, error) {
	v := s.u
	return &v, nil
}
func (s *upgradeStopStoreFake) GetStop(context.Context, int) (*models.RuntimeUpgradeStop, error) {
	if s.stop == nil {
		return nil, nil
	}
	v := *s.stop
	return &v, nil
}
func (s *upgradeStopStoreFake) PrepareStop(_ context.Context, v models.RuntimeUpgradeStop) error {
	*s.events = append(*s.events, "prepare")
	if s.prepareErr != nil {
		return s.prepareErr
	}
	if s.stop != nil && !s.stop.SameTarget(v) {
		return models.ErrRuntimeUpgradeConflict
	}
	s.stop = &v
	s.u.State = "stop_intent"
	return nil
}
func (s *upgradeStopStoreFake) RecordStopConfirmation(_ context.Context, v models.RuntimeUpgradeStop) error {
	*s.events = append(*s.events, "record")
	if s.recordErr != nil {
		return s.recordErr
	}
	s.stop = &v
	s.u.State = "process_group_confirmed"
	return nil
}
func (s *upgradeStopStoreFake) MarkRecoveryRequired(context.Context, int, int, int, string) error {
	*s.events = append(*s.events, "recovery")
	s.u.State = "recovery_required"
	return nil
}

type upgradeStopInstancesFake struct {
	repository.InstanceRepository
	instance models.Instance
}

func (s *upgradeStopInstancesFake) GetByID(int) (*models.Instance, error) {
	v := s.instance
	return &v, nil
}

type upgradeStopBindingsFake struct {
	repository.InstanceRuntimeBindingRepository
	binding models.InstanceRuntimeBinding
}

func (s *upgradeStopBindingsFake) GetByInstanceID(context.Context, int) (*models.InstanceRuntimeBinding, error) {
	v := s.binding
	return &v, nil
}

type upgradeStopPodsFake struct {
	repository.RuntimePodRepository
	pod models.RuntimePod
}

func (s *upgradeStopPodsFake) GetByID(context.Context, int64) (*models.RuntimePod, error) {
	v := s.pod
	return &v, nil
}

type upgradeStopAgentFake struct {
	events           *[]string
	caps             RuntimeAgentStopCapabilities
	receipt          RuntimeAgentStopReceipt
	err              error
	calls            int
	restartAfterStop bool
}

func (a *upgradeStopAgentFake) StopCapabilities(context.Context, string) (*RuntimeAgentStopCapabilities, error) {
	*a.events = append(*a.events, "capabilities")
	v := a.caps
	if a.restartAfterStop && a.calls > 0 {
		v.AgentEpoch = "22222222222222222222222222222222"
	}
	return &v, nil
}
func (a *upgradeStopAgentFake) ConfirmGatewayStop(ctx context.Context, _ string, r RuntimeAgentStopRequest) (*RuntimeAgentStopReceipt, error) {
	*a.events = append(*a.events, "stop")
	a.calls++
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded stop")
	}
	if r.PodUID != a.caps.PodUID || r.AgentEpoch != a.caps.AgentEpoch || r.OperationID != a.receipt.OperationID {
		panic("retargeted request")
	}
	if r.ExpectedPID != 1234 {
		panic("stop request lost expected pid")
	}
	v := a.receipt
	return &v, a.err
}
func upgradeStopFixture() (*RuntimeUpgradeStopCoordinator, *upgradeStopStoreFake, *upgradeStopAgentFake, *[]string) {
	req, receipt := stopClientFixture()
	events := []string{}
	store := &upgradeStopStoreFake{u: models.RuntimeUpgrade{InstanceID: req.InstanceID, OwnerID: req.UserID, Generation: req.Generation, Operation: req.OperationID, State: "reserved"}, events: &events}
	instances := &upgradeStopInstancesFake{instance: models.Instance{ID: req.InstanceID, UserID: req.UserID, RuntimeGeneration: req.Generation, Type: "opencode", Status: "running"}}
	bindings := &upgradeStopBindingsFake{binding: models.InstanceRuntimeBinding{InstanceID: req.InstanceID, Generation: req.Generation, RuntimePodID: 9, GatewayID: req.GatewayID, GatewayPID: &receipt.PID, State: "running"}}
	endpoint := "http://agent:8080"
	pods := &upgradeStopPodsFake{pod: models.RuntimePod{ID: 9, PodUID: &req.PodUID, AgentEndpoint: &endpoint}}
	agent := &upgradeStopAgentFake{events: &events, receipt: receipt, caps: RuntimeAgentStopCapabilities{Protocol: 1, PodUID: req.PodUID, AgentEpoch: req.AgentEpoch, GracefulStop: true, ExpectedPIDGuard: true, ConfirmationScope: "process_group"}}
	return NewRuntimeUpgradeStopCoordinator(store, instances, bindings, pods, agent), store, agent, &events
}
func runUpgradeStop(c *RuntimeUpgradeStopCoordinator) (*models.RuntimeUpgradeStop, error) {
	r, _ := stopClientFixture()
	return c.StopReserved(context.Background(), r.InstanceID, r.UserID, r.Generation, r.OperationID)
}
func TestUpgradeStopPersistsIntentBeforeHTTPAndReceiptBeforeSuccess(t *testing.T) {
	c, s, a, events := upgradeStopFixture()
	r, err := runUpgradeStop(c)
	if err != nil || r == nil || r.ConfirmationScope != "process_group" || s.u.State != "process_group_confirmed" {
		t.Fatalf("%+v %v", r, err)
	}
	if want := []string{"capabilities", "prepare", "stop", "capabilities", "record"}; !reflect.DeepEqual(*events, want) {
		t.Fatal(*events)
	}
	// New coordinator, same database state: do not return a historical receipt
	// without contacting and validating the exact same Agent again.
	c = NewRuntimeUpgradeStopCoordinator(s, c.instances, c.bindings, c.pods, a)
	if _, err = runUpgradeStop(c); err != nil || a.calls != 2 {
		t.Fatalf("retry: %v %d", err, a.calls)
	}
}
func TestUpgradeStopFailureBoundaries(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*RuntimeUpgradeStopCoordinator, *upgradeStopStoreFake, *upgradeStopAgentFake)
		calls int
		state string
	}{
		{"legacy agent", func(_ *RuntimeUpgradeStopCoordinator, _ *upgradeStopStoreFake, a *upgradeStopAgentFake) {
			a.caps.GracefulStop = false
		}, 0, "reserved"},
		{"no pid guard", func(_ *RuntimeUpgradeStopCoordinator, _ *upgradeStopStoreFake, a *upgradeStopAgentFake) {
			a.caps.ExpectedPIDGuard = false
		}, 0, "reserved"},
		{"intent write failure", func(_ *RuntimeUpgradeStopCoordinator, s *upgradeStopStoreFake, _ *upgradeStopAgentFake) {
			s.prepareErr = errors.New("db down")
		}, 0, "reserved"},
		{"timeout", func(_ *RuntimeUpgradeStopCoordinator, _ *upgradeStopStoreFake, a *upgradeStopAgentFake) {
			a.err = context.DeadlineExceeded
		}, 1, "stop_intent"},
		{"http client receipt rejection", func(_ *RuntimeUpgradeStopCoordinator, _ *upgradeStopStoreFake, a *upgradeStopAgentFake) {
			a.err = errors.Join(ErrRuntimeStopUnconfirmed, ErrRuntimeStopReceiptMismatch)
		}, 1, "recovery_required"},
		{"receipt db failure", func(_ *RuntimeUpgradeStopCoordinator, s *upgradeStopStoreFake, _ *upgradeStopAgentFake) {
			s.recordErr = errors.New("commit unknown")
		}, 1, "stop_intent"},
		{"wrong receipt pid", func(_ *RuntimeUpgradeStopCoordinator, _ *upgradeStopStoreFake, a *upgradeStopAgentFake) {
			a.receipt.PID++
		}, 1, "recovery_required"},
		{"wrong receipt operation", func(_ *RuntimeUpgradeStopCoordinator, _ *upgradeStopStoreFake, a *upgradeStopAgentFake) {
			a.receipt.Generation++
		}, 1, "recovery_required"},
		{"agent restart during stop", func(_ *RuntimeUpgradeStopCoordinator, _ *upgradeStopStoreFake, a *upgradeStopAgentFake) {
			a.restartAfterStop = true
		}, 1, "recovery_required"},
		{"wrong owner", func(c *RuntimeUpgradeStopCoordinator, _ *upgradeStopStoreFake, _ *upgradeStopAgentFake) {
			c.instances.(*upgradeStopInstancesFake).instance.UserID++
		}, 0, "reserved"},
		{"no pinned target", func(_ *RuntimeUpgradeStopCoordinator, s *upgradeStopStoreFake, _ *upgradeStopAgentFake) {
			s.u.State = "stop_intent"
		}, 0, "recovery_required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, s, a, _ := upgradeStopFixture()
			test.setup(c, s, a)
			r, err := runUpgradeStop(c)
			if err == nil || r != nil || a.calls != test.calls || s.u.State != test.state {
				t.Fatalf("result=%+v err=%v calls=%d state=%s", r, err, a.calls, s.u.State)
			}
		})
	}
}
func TestUpgradeStopLostResponseRetryAndTargetChange(t *testing.T) {
	for _, change := range []string{"none", "endpoint", "pod", "epoch", "pid"} {
		t.Run(change, func(t *testing.T) {
			c, s, a, events := upgradeStopFixture()
			a.err = context.DeadlineExceeded
			if _, err := runUpgradeStop(c); err == nil {
				t.Fatal("timeout accepted")
			}
			a.err = nil
			*events = nil
			switch change {
			case "endpoint":
				v := "http://other:8080"
				c.pods.(*upgradeStopPodsFake).pod.AgentEndpoint = &v
			case "pod":
				v := "new-pod"
				c.pods.(*upgradeStopPodsFake).pod.PodUID = &v
			case "epoch":
				a.caps.AgentEpoch = "22222222222222222222222222222222"
			case "pid":
				v := 4321
				c.bindings.(*upgradeStopBindingsFake).binding.GatewayPID = &v
			}
			_, err := runUpgradeStop(c)
			if change == "none" {
				if err != nil || a.calls != 2 {
					t.Fatal(err, a.calls)
				}
				return
			}
			if err == nil || a.calls != 1 || s.u.State != "recovery_required" {
				t.Fatalf("retarget: %v %d %s", err, a.calls, s.u.State)
			}
			if change != "epoch" && !reflect.DeepEqual(*events, []string{"recovery"}) {
				t.Fatal("contacted changed target", *events)
			}
		})
	}
}
func TestUpgradeStopCancellationBeforeIntentNeverSendsStop(t *testing.T) {
	c, s, a, _ := upgradeStopFixture()
	s.prepareErr = context.Canceled
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, _ := stopClientFixture()
	if _, err := c.StopReserved(ctx, r.InstanceID, r.UserID, r.Generation, r.OperationID); !errors.Is(err, context.Canceled) || a.calls != 0 {
		t.Fatal(err, a.calls)
	}
}
