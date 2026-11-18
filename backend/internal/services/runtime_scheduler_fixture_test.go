package services

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
)

// Older unit tests focus on ports, lifecycle and uncertain starts and omitted
// Kubernetes identity entirely. Supply explicit matching live identities there;
// identity/staleness tests use NewRuntimeScheduler directly with their own data.
func newLiveFixtureRuntimeScheduler(instances repository.InstanceRepository, pods repository.RuntimePodRepository, bindings repository.InstanceRuntimeBindingRepository, rollouts repository.RuntimeRolloutRepository, agent RuntimeAgentClient, events RuntimeEventService, leader RuntimeLeaderService, deployments k8s.RuntimeDeploymentService, tick time.Duration, options ...RuntimeSchedulerOption) *RuntimeScheduler {
	s := NewRuntimeScheduler(instances, pods, bindings, rollouts, agent, events, leader, deployments, tick, options...)
	r, ok := pods.(*fakeRuntimePodRepo)
	if !ok {
		return s
	}
	d, ok := deployments.(*fakeRuntimeDeploymentService)
	if !ok {
		return s
	}
	normalize := func(p *models.RuntimePod) {
		if p.Namespace == "" {
			p.Namespace = s.runtimeNamespace
		}
		if p.DeploymentName == "" {
			p.DeploymentName = defaultRuntimeDeploymentName(p.RuntimeType)
		}
		if p.PodName == "" {
			p.PodName = fmt.Sprintf("fixture-pod-%d", p.ID)
		}
		if p.PodUID == nil {
			p.PodUID = ptrString(fmt.Sprintf("fixture-uid-%d", p.ID))
		}
		if p.PodIP == nil {
			p.PodIP = ptrString(fmt.Sprintf("10.0.%d.%d", p.ID/250, (p.ID%250)+1))
		}
		if p.AgentEndpoint != nil {
			u, _ := url.Parse(*p.AgentEndpoint)
			if u != nil {
				u.Host = *p.PodIP + ":19090"
				*p.AgentEndpoint = u.String()
			}
		}
		if p.ImageRef == "" {
			p.ImageRef = "registry/runtime:fixture"
		}
		if p.LastSeenAt == nil {
			now := time.Now().UTC()
			p.LastSeenAt = &now
		}
	}
	for i := range r.schedulable {
		normalize(&r.schedulable[i])
	}
	if r.pods == nil {
		r.pods = map[int64]*models.RuntimePod{}
	}
	for _, p := range r.schedulable {
		if r.pods[p.ID] == nil {
			copy := p
			r.pods[p.ID] = &copy
		}
	}
	for _, p := range r.pods {
		normalize(p)
	}
	reported, _ := r.List(context.Background(), "")
	if len(reported) == 0 {
		reported = r.schedulable
	}
	if len(d.pods) == 0 {
		for _, p := range reported {
			d.pods = append(d.pods, k8s.RuntimeDeploymentPod{RuntimeType: p.RuntimeType, Namespace: p.Namespace, DeploymentName: p.DeploymentName, PodName: p.PodName, PodUID: stringValue(p.PodUID), PodIP: p.PodIP, ImageRef: p.ImageRef, State: "ready", DesiredReplicas: 1})
		}
	} else {
		for i := range d.pods {
			live := &d.pods[i]
			for _, p := range reported {
				if live.RuntimeType != p.RuntimeType || live.DeploymentName != p.DeploymentName {
					continue
				}
				if live.PodName == "" {
					live.PodName = p.PodName
				}
				if live.PodUID == "" {
					live.PodUID = stringValue(p.PodUID)
				}
				if live.PodIP == nil {
					live.PodIP = p.PodIP
				}
				if live.ImageRef == "" {
					live.ImageRef = p.ImageRef
				}
				break
			}
		}
	}
	return s
}
