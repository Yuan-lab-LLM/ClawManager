package services

import (
	"context"
	"errors"
	"testing"
)

type mergeBlockingUpgradeGuard struct{ calls []int }

func (*mergeBlockingUpgradeGuard) ValidateInstanceDeletion(context.Context, int) error {
	return ErrWorkspaceUpgradeBusy
}
func (g *mergeBlockingUpgradeGuard) WithInstanceMutation(_ context.Context, id int, run func() error) error {
	g.calls = append(g.calls, id)
	return ErrWorkspaceUpgradeBusy
}

func TestMergedLifecycleRetainsUpstreamRecoveryFence(t *testing.T) {
	for _, operation := range []string{"start", "stop", "restart", "restart_environment"} {
		t.Run(operation, func(t *testing.T) {
			guard := &mergeBlockingUpgradeGuard{}
			s := &instanceService{instanceRepo: newV2LifecycleInstanceRepo(), runtimeUpgradeGuard: guard}
			var err error
			switch operation {
			case "start":
				err = s.Start(17)
			case "stop":
				err = s.Stop(17)
			case "restart":
				err = s.Restart(17)
			case "restart_environment":
				err = s.RestartWithEnvironment(17, map[string]string{"LOG_LEVEL": "debug"}, nil)
			}
			if !errors.Is(err, ErrWorkspaceUpgradeBusy) || len(guard.calls) != 1 || guard.calls[0] != 17 {
				t.Fatalf("operation bypassed or reacquired recovery fence: calls=%v, err=%v", guard.calls, err)
			}
		})
	}
}
