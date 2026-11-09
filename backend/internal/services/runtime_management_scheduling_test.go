package services

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"clawreef/internal/services/k8s"
)

type schedulingResumePods struct {
	*managementInventoryPods
	events     *[]string
	failResume bool
}

func (r *schedulingResumePods) SetSchedulingPaused(ctx context.Context, id int64, paused bool) error {
	if paused {
		*r.events = append(*r.events, "pause")
	} else {
		*r.events = append(*r.events, "unpause")
		if r.failResume {
			return errors.New("maintenance admission denied")
		}
	}
	return r.managementInventoryPods.SetSchedulingPaused(ctx, id, paused)
}

type schedulingResumeDeployments struct {
	*managementInventoryDeployments
	events *[]string
	err    error
	target k8s.RuntimePodControllerIdentity
}

func (d *schedulingResumeDeployments) EnableRuntimePodScheduling(_ context.Context, target k8s.RuntimePodControllerIdentity) error {
	*d.events = append(*d.events, "enable-live-pool")
	d.target = target
	return d.err
}
func TestRuntimeManagementSchedulingResumeSynchronizesLiveGate(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		kubeError, admissionError bool
		want                      []string
	}{
		{"ordinary disabled pool", false, false, []string{"pause", "enable-live-pool", "unpause"}},
		{"Kubernetes failure stays paused", true, false, []string{"pause", "enable-live-pool"}},
		{"maintenance wins", false, true, []string{"pause", "enable-live-pool", "unpause"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d := managementInventoryFixture()
			events := []string{}
			pods := &schedulingResumePods{managementInventoryPods: s.scheduler.podRepo.(*managementInventoryPods), events: &events, failResume: tc.admissionError}
			kube := &schedulingResumeDeployments{managementInventoryDeployments: d, events: &events}
			if tc.kubeError {
				kube.err = errors.New("live Deployment gate update failed")
			}
			s.scheduler.podRepo = pods
			s.scheduler.deployments = kube
			err := s.Scheduling(context.Background(), 9, true)
			if (err != nil) != (tc.kubeError || tc.admissionError) {
				t.Fatalf("resume error=%v", err)
			}
			if !reflect.DeepEqual(events, tc.want) {
				t.Fatalf("events=%v, want %v: live deployment gate must be restored before admission", events, tc.want)
			}
			p, _ := pods.GetByID(context.Background(), 9)
			if p.SchedulingPaused != (tc.kubeError || tc.admissionError) {
				t.Fatalf("unsafe pause state=%v", p.SchedulingPaused)
			}
			if kube.target.PodUID != "uid-9" || kube.target.PodName != p.PodName || kube.target.Namespace != p.Namespace || kube.target.DeploymentName != p.DeploymentName || kube.target.RuntimeType != p.RuntimeType {
				t.Fatalf("missing live identity proof: %+v", kube.target)
			}
		})
	}
}
func TestRuntimeManagementSchedulingPauseNeedsNoKubernetesMutation(t *testing.T) {
	s, _ := managementInventoryFixture()
	if err := s.Scheduling(context.Background(), 9, false); err != nil {
		t.Fatal(err)
	}
	p, _ := s.scheduler.podRepo.GetByID(context.Background(), 9)
	if !p.SchedulingPaused {
		t.Fatal("not paused")
	}
}
func TestRuntimeManagementSchedulingResumeRejectsUnavailableIdentity(t *testing.T) {
	for _, kind := range []string{"draining", "stale", "missing UID", "missing Kubernetes control"} {
		t.Run(kind, func(t *testing.T) {
			s, d := managementInventoryFixture()
			events := []string{}
			if kind != "missing Kubernetes control" {
				s.scheduler.deployments = &schedulingResumeDeployments{managementInventoryDeployments: d, events: &events}
			}
			p, _ := s.scheduler.podRepo.GetByID(context.Background(), 9)
			switch kind {
			case "draining":
				p.Draining = true
			case "stale":
				p.State = "unknown"
			case "missing UID":
				p.PodUID = nil
			}
			if err := s.Scheduling(context.Background(), 9, true); err == nil {
				t.Fatal("reported success without a safe live scheduling target")
			}
		})
	}
}

// Existing repository tests supply Kubernetes admission explicitly; lifecycle
// mutation and maintenance exclusions still use the real MySQL repositories.
func withSchedulingResumeTestControl(s *RuntimeManagementService) {
	d, _ := s.scheduler.deployments.(*fakeRuntimeDeploymentService)
	if d == nil {
		d = &fakeRuntimeDeploymentService{}
	}
	events := []string{}
	s.scheduler.deployments = &schedulingResumeDeployments{managementInventoryDeployments: &managementInventoryDeployments{fakeRuntimeDeploymentService: d}, events: &events}
}
